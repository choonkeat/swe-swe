package main

// agentview_tunnel_client.go -- the swe-swe box's half of the Agent View
// reverse tunnel. When the server runs with -agent-view=<url> AND
// -agent-view-tunnel, each session's remote allocation is followed by an
// OUTBOUND WebSocket dial to <backend>/sessions/<id>/tunnel (same trust
// direction as swe-swe-tunnel: this box needs ZERO inbound reachability).
//
// The client keeps a declarative port set synced to the backend:
//
//  1. Static: the server's own port + the session preview port (+ Procfile
//     ports, computed by the caller) -- always bound, no discovery race.
//  2. Mirror: /proc/net/tcp{,6} polled every tunnelMirrorInterval for LISTEN
//     sockets on loopback/wildcard, so ad-hoc `npm run dev` servers appear
//     with zero configuration. Filtered by an exclude list
//     (SWE_AGENT_VIEW_TUNNEL_EXCLUDE_PORTS or defaults covering swe-swe's own
//     per-session plumbing pools). Off Linux the mirror is naturally absent.
//
// Open frames from the backend dial 127.0.0.1:<port> here and pipe. Tunnel
// death never kills the session: Agent View pages just fail until the
// reconnect loop (1s..30s capped backoff) restores the tunnel and re-syncs.

import (
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// errTunnelAllocationLost marks a tunnel dial refused with 404: the backend is
// UP but does not know our allocation (it restarted and lost its in-memory
// table). Blind retry can never succeed -- the session must re-allocate.
// Deliberately 404-only: 401/403 are auth failures re-allocating with the same
// token cannot fix, and 409 (duplicate tunnel) resolves itself on retry.
var errTunnelAllocationLost = errors.New("backend does not know this allocation")

var (
	// tunnelDialAddr is where open frames dial back. Production: this box's
	// own loopback. Var so the one-machine e2e can target 127.0.0.2.
	tunnelDialAddr = "127.0.0.1"
	// tunnelMirrorInterval is the /proc poll + sync cadence.
	tunnelMirrorInterval = 2 * time.Second
	// tunnelReconnectMax caps the reconnect backoff.
	tunnelReconnectMax = 30 * time.Second
	// tunnelServerPort is this server's own listen port (static source);
	// set from main() after resolveListenAddr.
	tunnelServerPort int
)

type tunnelPortRange struct{ Lo, Hi int }

// parseTunnelExcludePorts parses a CSV of ports and lo-hi ranges
// ("6080,7000-7019"). Unparseable entries are dropped.
func parseTunnelExcludePorts(csv string) []tunnelPortRange {
	var out []tunnelPortRange
	for _, part := range strings.Split(csv, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if lo, hi, found := strings.Cut(part, "-"); found {
			l, err1 := strconv.Atoi(strings.TrimSpace(lo))
			h, err2 := strconv.Atoi(strings.TrimSpace(hi))
			if err1 == nil && err2 == nil && l > 0 && h >= l {
				out = append(out, tunnelPortRange{l, h})
			}
			continue
		}
		if p, err := strconv.Atoi(part); err == nil && p > 0 {
			out = append(out, tunnelPortRange{p, p})
		}
	}
	return out
}

func tunnelPortExcluded(ranges []tunnelPortRange, port int) bool {
	for _, r := range ranges {
		if port >= r.Lo && port <= r.Hi {
			return true
		}
	}
	return false
}

// defaultTunnelExcludePorts covers swe-swe's own per-session plumbing so the
// mirror does not sync internal listeners: agent-chat, public, CDP + internal,
// VNC + internal, files pools, and every proxy-fleet band. Preview ports are
// deliberately NOT excluded -- those are the app.
func defaultTunnelExcludePorts() []tunnelPortRange {
	cdpSize := cdpPortEnd - cdpPortStart + 1
	vncSize := vncPortEnd - vncPortStart + 1
	r := []tunnelPortRange{
		{agentChatPortStart, agentChatPortEnd},
		{publicPortStart, publicPortEnd},
		// Through cdpPortEnd + 2*cdpSize: covers the local-chromium internal
		// range AND the remote-mode CDP proxy listeners (remoteCDPProxyOffset).
		{cdpPortStart, cdpPortEnd + 2*cdpSize},
		{vncPortStart, vncPortEnd + vncSize},
		{filesPortStart, filesPortEnd},
	}
	for _, band := range [][2]int{
		{previewPortStart, previewPortEnd},
		{agentChatPortStart, agentChatPortEnd},
		{publicPortStart, publicPortEnd},
		{cdpPortStart, cdpPortEnd},
		{vncPortStart, vncPortEnd},
		{filesPortStart, filesPortEnd},
	} {
		r = append(r, tunnelPortRange{proxyPortOffset + band[0], proxyPortOffset + band[1]})
	}
	return r
}

// tunnelExcludePortsFromEnv returns the operator override
// (SWE_AGENT_VIEW_TUNNEL_EXCLUDE_PORTS) or the defaults.
func tunnelExcludePortsFromEnv() []tunnelPortRange {
	if v := os.Getenv("SWE_AGENT_VIEW_TUNNEL_EXCLUDE_PORTS"); v != "" {
		return parseTunnelExcludePorts(v)
	}
	return defaultTunnelExcludePorts()
}

// parseProcNetTCPListeners extracts ports in LISTEN state (st 0A) bound to
// loopback or wildcard addresses from /proc/net/tcp{,6} content. Addresses
// are hex, u32-group little-endian; specific non-loopback binds are skipped
// (our dial-back to 127.0.0.1 could not reach them anyway).
func parseProcNetTCPListeners(content string) []int {
	var ports []int
	seen := map[int]bool{}
	for _, line := range strings.Split(content, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 4 || fields[3] != "0A" {
			continue
		}
		ip, port, ok := parseProcNetHexAddr(fields[1])
		if !ok || port == 0 || seen[port] {
			continue
		}
		if !ip.IsLoopback() && !ip.IsUnspecified() {
			continue
		}
		seen[port] = true
		ports = append(ports, port)
	}
	return ports
}

// parseProcNetHexAddr decodes /proc/net/tcp's "HEXIP:HEXPORT" address column
// (v4: one little-endian u32; v6: four little-endian u32 groups). Shared with
// the linux-only peer guard; lives here (no build tag) so the mirror parser
// stays testable on every platform.
func parseProcNetHexAddr(s string) (net.IP, int, bool) {
	ipHex, portHex, found := strings.Cut(s, ":")
	if !found {
		return nil, 0, false
	}
	port64, err := strconv.ParseUint(portHex, 16, 16)
	if err != nil {
		return nil, 0, false
	}
	if len(ipHex) != 8 && len(ipHex) != 32 {
		return nil, 0, false
	}
	raw := make([]byte, len(ipHex)/2)
	for i := 0; i < len(raw); i++ {
		b, err := strconv.ParseUint(ipHex[i*2:i*2+2], 16, 8)
		if err != nil {
			return nil, 0, false
		}
		raw[i] = byte(b)
	}
	ip := make(net.IP, len(raw))
	for group := 0; group < len(raw); group += 4 {
		for i := 0; i < 4; i++ {
			ip[group+i] = raw[group+3-i]
		}
	}
	return ip, int(port64), true
}

// mirrorListeningPorts reads the real /proc tables. On platforms without
// them (macOS dev builds) it returns nil -- static + Procfile still work.
func mirrorListeningPorts(exclude []tunnelPortRange) []int {
	var ports []int
	for _, table := range []string{"/proc/net/tcp", "/proc/net/tcp6"} {
		data, err := os.ReadFile(table)
		if err != nil {
			continue
		}
		for _, p := range parseProcNetTCPListeners(string(data)) {
			if !tunnelPortExcluded(exclude, p) {
				ports = append(ports, p)
			}
		}
	}
	return ports
}

// procfileServicePorts mirrors swe-run's deterministic port assignment
// (cmd/swe-run/ports.go assignPorts): primary service (explicit "web", else
// first) gets the session base; the i-th other service gets base+5000+i*20.
// Pre-binding these at tunnel start avoids the mirror's discovery race for
// declared services. Missing/empty Procfile -> nil.
func procfileServicePorts(workDir string, base int) []int {
	data, err := os.ReadFile(filepath.Join(workDir, "Procfile"))
	if err != nil || base <= 0 {
		return nil
	}
	var names []string
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		name, _, found := strings.Cut(line, ":")
		name = strings.TrimSpace(name)
		if !found || name == "" {
			continue
		}
		names = append(names, name)
	}
	if len(names) == 0 {
		return nil
	}
	primary := names[0]
	for _, n := range names {
		if n == "web" {
			primary = "web"
			break
		}
	}
	ports := []int{base}
	i := 0
	for _, n := range names {
		if n == primary {
			continue
		}
		p := base + 5000 + i*20
		if p >= 1024 && p <= 65535 {
			ports = append(ports, p)
		}
		i++
	}
	return ports
}

// agentViewTunnelClient maintains one session's reverse tunnel: dial,
// declarative sync, dial-back, reconnect.
type agentViewTunnelClient struct {
	backendURL string
	token      string
	sessionID  string
	excludes   []tunnelPortRange

	stop     chan struct{}
	stopOnce sync.Once

	// onAllocationLost, when set (before start()), is invoked ON the run
	// goroutine -- which then exits -- after a dial fails with
	// errTunnelAllocationLost. The callback owns recovery (re-allocate and
	// start a REPLACEMENT client); this client is dead but its stop channel
	// stays open so session teardown can still cancel the recovery via Stop().
	onAllocationLost func()

	mu       sync.Mutex
	static   []int
	lastSync []int
	warned   map[int]string
}

// newAgentViewTunnelClient builds a client without starting it, so callers can
// set onAllocationLost before the run goroutine exists.
func newAgentViewTunnelClient(backendURL, token, sessionID string, staticPorts []int, excludes []tunnelPortRange) *agentViewTunnelClient {
	return &agentViewTunnelClient{
		backendURL: backendURL,
		token:      token,
		sessionID:  sessionID,
		excludes:   excludes,
		stop:       make(chan struct{}),
		static:     append([]int(nil), staticPorts...),
		warned:     map[int]string{},
	}
}

func (c *agentViewTunnelClient) start() {
	go func() {
		defer recoverGoroutine(fmt.Sprintf("agent-view tunnel client %s", c.sessionID))
		c.run()
	}()
}

func startAgentViewTunnelClient(backendURL, token, sessionID string, staticPorts []int, excludes []tunnelPortRange) *agentViewTunnelClient {
	c := newAgentViewTunnelClient(backendURL, token, sessionID, staticPorts, excludes)
	c.start()
	return c
}

// Stop ends the tunnel (session teardown). Idempotent.
func (c *agentViewTunnelClient) Stop() {
	c.stopOnce.Do(func() { close(c.stop) })
}

func (c *agentViewTunnelClient) currentSyncPorts() []int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]int(nil), c.lastSync...)
}

func (c *agentViewTunnelClient) setStaticPortsForTest(ports []int) {
	c.mu.Lock()
	c.static = append([]int(nil), ports...)
	c.mu.Unlock()
}

// shouldWarnRefusal dedups refusal warnings per port until the reason changes.
func (c *agentViewTunnelClient) shouldWarnRefusal(port int, reason string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.warned[port] == reason {
		return false
	}
	c.warned[port] = reason
	return true
}

// desiredPorts is the union of static + mirror (deduped, sorted). Static
// ports bypass the exclude list -- the caller asked for them explicitly.
func (c *agentViewTunnelClient) desiredPorts() []int {
	set := map[int]bool{}
	c.mu.Lock()
	static := append([]int(nil), c.static...)
	c.mu.Unlock()
	for _, p := range static {
		if p > 0 {
			set[p] = true
		}
	}
	for _, p := range mirrorListeningPorts(c.excludes) {
		set[p] = true
	}
	ports := make([]int, 0, len(set))
	for p := range set {
		ports = append(ports, p)
	}
	sort.Ints(ports)
	return ports
}

// run is the reconnect loop. Backoff 1s..tunnelReconnectMax, reset after any
// connection that reached the sync stage.
func (c *agentViewTunnelClient) run() {
	backoff := time.Second
	for {
		select {
		case <-c.stop:
			return
		default:
		}
		connected, err := c.runOnce()
		select {
		case <-c.stop:
			return
		default:
		}
		if errors.Is(err, errTunnelAllocationLost) && c.onAllocationLost != nil {
			log.Printf("agent-view tunnel %s: %v -- handing off to re-allocation", c.sessionID, err)
			c.onAllocationLost()
			return
		}
		if connected {
			backoff = time.Second
		}
		log.Printf("agent-view tunnel %s: disconnected (%v); reconnecting in %s", c.sessionID, err, backoff)
		select {
		case <-c.stop:
			return
		case <-time.After(backoff):
		}
		backoff *= 2
		if backoff > tunnelReconnectMax {
			backoff = tunnelReconnectMax
		}
	}
}

// runOnce dials the tunnel and serves it until it dies. Returns whether the
// dial succeeded (for backoff reset) and the terminal error.
func (c *agentViewTunnelClient) runOnce() (bool, error) {
	wsURL := strings.TrimRight(c.backendURL, "/") + "/sessions/" + c.sessionID + "/tunnel"
	if strings.HasPrefix(wsURL, "http") {
		wsURL = "ws" + strings.TrimPrefix(wsURL, "http")
	}
	hdr := http.Header{}
	if c.token != "" {
		hdr.Set("Authorization", "Bearer "+c.token)
	}
	dialer := websocket.Dialer{HandshakeTimeout: 10 * time.Second}
	conn, resp, err := dialer.Dial(wsURL, hdr)
	if err != nil {
		if resp != nil && resp.StatusCode == http.StatusNotFound {
			return false, fmt.Errorf("dial: %s: %w", resp.Status, errTunnelAllocationLost)
		}
		return false, fmt.Errorf("dial: %w", err)
	}

	// Liveness: ping every 30s; two missed pongs (75s silence) kill the
	// conn via read deadline and trigger reconnect. Any read (data or pong)
	// proves liveness, so the deadline resets in both paths.
	const pingInterval = 30 * time.Second
	const deadAfter = 2*pingInterval + 15*time.Second
	conn.SetReadDeadline(time.Now().Add(deadAfter))
	conn.SetPongHandler(func(string) error {
		conn.SetReadDeadline(time.Now().Add(deadAfter))
		return nil
	})

	mux := newTunnelMux(conn, tunnelClientOnOpen, func(ctl tunnelControl) {
		if ctl.Op != "sync-result" {
			return
		}
		for _, ref := range ctl.Refused {
			if c.shouldWarnRefusal(ref.Port, ref.Reason) {
				log.Printf("agent-view tunnel %s: WARNING backend refused port %d (%s) -- Agent View pages on that port will not load", c.sessionID, ref.Port, ref.Reason)
			}
		}
	})

	done := make(chan struct{})
	go func() {
		defer recoverGoroutine("agent-view tunnel sync loop")
		ticker := time.NewTicker(tunnelMirrorInterval)
		pinger := time.NewTicker(pingInterval)
		defer ticker.Stop()
		defer pinger.Stop()
		var sent []int
		syncNow := func() {
			ports := c.desiredPorts()
			if slicesEqualInts(ports, sent) {
				return
			}
			if err := mux.sendSync(ports); err != nil {
				return
			}
			sent = ports
			c.mu.Lock()
			c.lastSync = ports
			c.mu.Unlock()
		}
		syncNow()
		for {
			select {
			case <-done:
				return
			case <-c.stop:
				mux.close()
				return
			case <-ticker.C:
				syncNow()
			case <-pinger.C:
				conn.WriteControl(websocket.PingMessage, nil, time.Now().Add(10*time.Second))
			}
		}
	}()

	err = mux.run()
	close(done)
	mux.close()
	return true, err
}

// tunnelClientOnOpen answers a backend open frame: dial the local service and
// pipe. Dial failure -> immediate close frame (via stream.Close).
func tunnelClientOnOpen(s *tunnelStream, port int) {
	conn, err := net.DialTimeout("tcp", net.JoinHostPort(tunnelDialAddr, strconv.Itoa(port)), 5*time.Second)
	if err != nil {
		log.Printf("agent-view tunnel: dial-back %s:%d failed: %v", tunnelDialAddr, port, err)
		s.Close()
		return
	}
	tunnelPipe(s, conn)
}

func slicesEqualInts(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
