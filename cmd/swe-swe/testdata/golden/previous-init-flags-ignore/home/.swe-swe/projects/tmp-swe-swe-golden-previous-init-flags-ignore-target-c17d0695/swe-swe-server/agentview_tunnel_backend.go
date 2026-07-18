package main

// agentview_tunnel_backend.go -- backend half of the Agent View reverse
// tunnel. The swe-swe box dials GET /sessions/{id}/tunnel (bearer-authed,
// same trust direction as swe-swe-tunnel) and sends declarative
// {"op":"sync","ports":[...]} frames. For each synced port this side binds a
// REAL listener on ITS OWN loopback, so chromium on this box resolves
// localhost / *.lvh.me / *.localtest.me naturally -- no --host-resolver-rules
// -- and every accepted connection is shuffled down the WebSocket as one mux
// stream, which the swe-swe side replays against its 127.0.0.1:<port>.
//
// Bind rules (tasks/2026-07-18-agent-view-reverse-tunnel.md):
//   - 127.0.0.1 only, never wildcard.
//   - Refuse (reported in sync-result, never silent) the backend's own
//     service port and the CDP/VNC ranges incl. their internal halves.
//   - First bind wins across sessions; the loser is told "in-use".
//   - Accepted connections pass a peer guard (tunnelPeerGuard: /proc-based
//     peer pid + ancestry against the session's browser process tree on
//     Linux; fail-open elsewhere for dev builds).
//   - Session teardown closes listeners, streams, and the WS. No orphans.

import (
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"sort"
	"sync"

	"github.com/gorilla/websocket"
)

// tunnelPortOwners is the cross-session first-bind-wins registry: port ->
// owning backend session id.
var (
	tunnelPortOwnersMu sync.Mutex
	tunnelPortOwners   = map[int]string{}
)

func claimTunnelPort(port int, sid string) bool {
	tunnelPortOwnersMu.Lock()
	defer tunnelPortOwnersMu.Unlock()
	if owner, taken := tunnelPortOwners[port]; taken && owner != sid {
		return false
	}
	tunnelPortOwners[port] = sid
	return true
}

func releaseTunnelPort(port int, sid string) {
	tunnelPortOwnersMu.Lock()
	defer tunnelPortOwnersMu.Unlock()
	if tunnelPortOwners[port] == sid {
		delete(tunnelPortOwners, port)
	}
}

// tunnelBindManager owns the loopback listeners of ONE tunnel connection and
// reconciles them against each declarative sync.
type tunnelBindManager struct {
	sessionID string
	// reserved returns a refusal reason for ports this backend must never
	// bind ("" = allowed). Injectable for tests.
	reserved func(int) string
	// guard vets each accepted connection before it becomes a stream; nil
	// allows all. Injectable for tests.
	guard func(net.Conn) error
	// onAccept receives guarded connections; it opens the mux stream and
	// pipes. Set by the tunnel handler before the first sync can arrive.
	onAccept func(port int, c net.Conn)

	mu        sync.Mutex
	listeners map[int]net.Listener
	closed    bool
}

func newTunnelBindManager(sessionID string, reserved func(int) string, guard func(net.Conn) error) *tunnelBindManager {
	return &tunnelBindManager{
		sessionID: sessionID,
		reserved:  reserved,
		guard:     guard,
		listeners: make(map[int]net.Listener),
	}
}

// reconcile makes the bound set match ports: binds missing, closes removed,
// refuses reserved / cross-session-duplicate / unbindable ports. Idempotent;
// safe to re-run on every sync and reconnect.
func (bm *tunnelBindManager) reconcile(ports []int) (bound []int, refused []tunnelRefusal) {
	desired := make(map[int]bool, len(ports))
	for _, p := range ports {
		desired[p] = true
	}
	bm.mu.Lock()
	defer bm.mu.Unlock()
	if bm.closed {
		for _, p := range ports {
			refused = append(refused, tunnelRefusal{Port: p, Reason: "tunnel-closed"})
		}
		return nil, refused
	}
	for p, ln := range bm.listeners {
		if !desired[p] {
			ln.Close()
			delete(bm.listeners, p)
			releaseTunnelPort(p, bm.sessionID)
		}
	}
	// Deterministic order so refusal logs and sync-results are stable.
	want := make([]int, 0, len(desired))
	for p := range desired {
		want = append(want, p)
	}
	sort.Ints(want)
	for _, p := range want {
		if _, ok := bm.listeners[p]; ok {
			continue
		}
		if bm.reserved != nil {
			if reason := bm.reserved(p); reason != "" {
				refused = append(refused, tunnelRefusal{Port: p, Reason: reason})
				continue
			}
		}
		if !claimTunnelPort(p, bm.sessionID) {
			refused = append(refused, tunnelRefusal{Port: p, Reason: "in-use"})
			continue
		}
		ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", p))
		if err != nil {
			releaseTunnelPort(p, bm.sessionID)
			log.Printf("tunnel[%s]: bind 127.0.0.1:%d failed: %v", bm.sessionID, p, err)
			refused = append(refused, tunnelRefusal{Port: p, Reason: "bind-failed"})
			continue
		}
		bm.listeners[p] = ln
		go bm.acceptLoop(p, ln)
	}
	for p := range bm.listeners {
		bound = append(bound, p)
	}
	sort.Ints(bound)
	return bound, refused
}

func (bm *tunnelBindManager) acceptLoop(port int, ln net.Listener) {
	defer recoverGoroutine(fmt.Sprintf("tunnel accept loop %s:%d", bm.sessionID, port))
	for {
		c, err := ln.Accept()
		if err != nil {
			return // listener closed by reconcile/closeAll
		}
		if bm.guard != nil {
			if err := bm.guard(c); err != nil {
				log.Printf("tunnel[%s]: rejected connection on %d from %s: %v",
					bm.sessionID, port, c.RemoteAddr(), err)
				c.Close()
				continue
			}
		}
		bm.mu.Lock()
		accept := bm.onAccept
		bm.mu.Unlock()
		if accept == nil {
			c.Close()
			continue
		}
		go accept(port, c)
	}
}

// setOnAccept installs the stream-opening callback (handler wires it to the
// live mux after the WS upgrade).
func (bm *tunnelBindManager) setOnAccept(fn func(int, net.Conn)) {
	bm.mu.Lock()
	bm.onAccept = fn
	bm.mu.Unlock()
}

// closeAll tears down every listener and releases the port claims. Idempotent.
func (bm *tunnelBindManager) closeAll() {
	bm.mu.Lock()
	defer bm.mu.Unlock()
	bm.closed = true
	for p, ln := range bm.listeners {
		ln.Close()
		delete(bm.listeners, p)
		releaseTunnelPort(p, bm.sessionID)
	}
}

// tunnelPipe copies both directions between a mux stream and a TCP conn,
// closing both once either side finishes.
func tunnelPipe(a, b io.ReadWriteCloser) {
	done := make(chan struct{}, 2)
	go func() {
		defer recoverGoroutine("tunnel pipe a->b")
		io.Copy(b, a)
		done <- struct{}{}
	}()
	go func() {
		defer recoverGoroutine("tunnel pipe b->a")
		io.Copy(a, b)
		done <- struct{}{}
	}()
	<-done
	a.Close()
	b.Close()
	<-done
}

// reservedPortReason names why the tunnel must not bind port on this backend:
// its own service port plus the CDP/VNC pools (external and internal halves).
func (bb *browserBackend) reservedPortReason(port int) string {
	if port == bb.servicePort && port != 0 {
		return "reserved"
	}
	cdpSize := cdpPortEnd - cdpPortStart + 1
	if port >= cdpPortStart && port <= cdpPortEnd+cdpSize {
		return "reserved"
	}
	vncSize := vncPortEnd - vncPortStart + 1
	if port >= vncPortStart && port <= vncPortEnd+vncSize {
		return "reserved"
	}
	return ""
}

// tunnelUpgrader upgrades /sessions/{id}/tunnel. Origin checks are moot: the
// dialer is the swe-swe server (bearer-authed), not a browser.
var tunnelUpgrader = websocket.Upgrader{
	CheckOrigin: func(*http.Request) bool { return true },
}

// handleTunnel serves one reverse-tunnel WebSocket for a session. Auth is
// checked by ServeHTTP before dispatch.
func (bb *browserBackend) handleTunnel(w http.ResponseWriter, r *http.Request, id string) {
	bb.mu.Lock()
	sess, ok := bb.sessions[id]
	if !ok {
		bb.mu.Unlock()
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	if sess.tunnelActive {
		bb.mu.Unlock()
		http.Error(w, "tunnel already connected for session", http.StatusConflict)
		return
	}
	sess.tunnelActive = true
	guard := bb.tunnelGuard
	bb.mu.Unlock()

	clearActive := func() {
		bb.mu.Lock()
		if cur, ok := bb.sessions[id]; ok && cur == sess {
			cur.tunnelActive = false
			cur.tunnelStop = nil
		}
		bb.mu.Unlock()
	}

	conn, err := tunnelUpgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("tunnel[%s]: upgrade failed: %v", id, err)
		clearActive()
		return
	}

	binds := newTunnelBindManager(id, bb.reservedPortReason, func(c net.Conn) error {
		if guard == nil {
			return nil
		}
		return guard(sess, c)
	})
	var mux *tunnelMux
	mux = newTunnelMux(conn, nil, func(ctl tunnelControl) {
		if ctl.Op != "sync" {
			return
		}
		bound, refused := binds.reconcile(ctl.Ports)
		if err := mux.sendSyncResult(bound, refused); err != nil {
			log.Printf("tunnel[%s]: sync-result send failed: %v", id, err)
		}
	})
	binds.setOnAccept(func(port int, c net.Conn) {
		s, err := mux.openStream(port)
		if err != nil {
			log.Printf("tunnel[%s]: open stream for :%d failed: %v", id, port, err)
			c.Close()
			return
		}
		tunnelPipe(s, c)
	})

	bb.mu.Lock()
	// Session may have been deleted between upgrade and here.
	if cur, ok := bb.sessions[id]; !ok || cur != sess {
		bb.mu.Unlock()
		binds.closeAll()
		mux.close()
		return
	}
	sess.tunnelStop = func() { mux.close() }
	bb.mu.Unlock()

	log.Printf("tunnel[%s]: connected from %s", id, r.RemoteAddr)
	err = mux.run()
	binds.closeAll()
	clearActive()
	log.Printf("tunnel[%s]: closed: %v", id, err)
}
