package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

// The browser-backend service is the network-facing allocator that a lean
// (dockerless) swe-swe-server offloads Agent View to when run with
// -agent-view=<url>. It is the SAME binary in a different mode
// (`swe-swe-server -mode browser-backend`), so it reuses startBrowserProcs and
// the chromium/novnc stack already in the image. It allocates an isolated
// Chromium per session (own profile + display) and exposes, per session, a CDP
// endpoint (for the agent's Playwright MCP) and a VNC/noVNC stream (for the
// human).
//
// Contract (the remote client in browser_backend_remote.go calls this):
//
//	POST   /sessions             -> {sessionId, host, cdpPort, vncPort}
//	DELETE /sessions/{id}        -> 204
//	GET    /sessions/{id}/ready  -> 200 (websockify up) | 503
//	POST   /sessions/{id}/touch  -> 204 | 404 (keepalive; see the reaper below)
//	GET    /health               -> {sessions, max}
//
// Auth: a shared bearer token (SWE_BROWSER_BACKEND_TOKEN) guards the /sessions
// routes so a public box is not an open browser relay. /health is open.

// browserProcsStarter is indirected so the service's allocation logic can be
// unit-tested without spawning a real Xvfb/chromium/x11vnc/websockify stack.
var browserProcsStarter = startBrowserProcs

// defaultLoopbackDomains are the dev hostnames that conventionally resolve to
// loopback and so must be remapped to the swe-swe host when chromium runs on
// a remote backend. Each entry maps both the bare name and its subdomains
// (*.lvh.me tenants etc). Deliberately NOT *.nip.io / *.sslip.io -- those
// encode arbitrary IPs (10.0.0.5.nip.io) that must keep resolving normally.
var defaultLoopbackDomains = []string{"localhost", "lvh.me", "localtest.me"}

// buildLoopbackResolverRules renders a chromium --host-resolver-rules value
// mapping every domain (bare + wildcard) to addr. Empty addr or domains ->
// empty string (no flag).
func buildLoopbackResolverRules(domains []string, addr string) string {
	if addr == "" {
		return ""
	}
	var rules []string
	for _, d := range domains {
		d = strings.TrimSpace(strings.TrimPrefix(d, "*."))
		if d == "" {
			continue
		}
		rules = append(rules,
			fmt.Sprintf("MAP %s %s", d, addr),
			fmt.Sprintf("MAP *.%s %s", d, addr))
	}
	return strings.Join(rules, ", ")
}

type backendSession struct {
	id      string
	slot    int
	cdpPort int
	vncPort int
	procs   *browserProcs
	// tunnel: the session was allocated in reverse-tunnel mode -- chromium
	// runs without resolver rules and the swe-swe box dials /tunnel.
	tunnel bool
	// tunnelActive guards the one-concurrent-tunnel rule; tunnelStop closes
	// the live tunnel (WS + listeners) on session teardown.
	tunnelActive bool
	tunnelStop   func()
	// lastUsed is the last time a CLIENT touched this session over the
	// allocation API (create, /ready, /touch, tunnel connect). Agent traffic
	// does not come through here -- it hits the per-session CDP forwarder --
	// so idleFor() also consults procs. Guarded by browserBackend.mu.
	lastUsed time.Time
}

// idleFor reports how long nothing has used this session, and true only if it
// is genuinely idle. Anything in flight (an open CDP websocket, a live reverse
// tunnel) reports not-idle regardless of timestamps: the whole point of the
// reaper is to reclaim ABANDONED slots, never to pull a browser out from under
// an agent that is using it. Caller holds bb.mu.
func (s *backendSession) idleFor(now time.Time) (time.Duration, bool) {
	if s.tunnelActive {
		return 0, false
	}
	last := s.lastUsed
	if s.procs != nil {
		cdpAt, busy := s.procs.lastCDPActivity()
		if busy {
			return 0, false
		}
		if cdpAt.After(last) {
			last = cdpAt
		}
	}
	if last.IsZero() {
		return 0, false
	}
	return now.Sub(last), true
}

// browserBackend is the allocator state for the service.
type browserBackend struct {
	mu            sync.Mutex
	sessions      map[string]*backendSession
	maxSessions   int
	token         string // bearer token; empty = no auth (not for public boxes)
	advertiseHost string // host clients should dial for CDP/VNC ports
	// servicePort is this service's own listen port (reserved from tunnel
	// binds); 0 when unknown.
	servicePort int
	// tunnelGuard vets connections accepted on tunnel-bound loopback ports.
	// Defaults to the platform tunnelPeerGuard; injectable for tests.
	tunnelGuard func(*backendSession, net.Conn) error
	// idleTimeout is how long a session may sit unused before the reaper frees
	// it; 0 disables reaping entirely. Set from -browser-backend-idle.
	idleTimeout time.Duration
	// now is time.Now, indirected so reaper tests need not sleep.
	now func() time.Time
}

// defaultBrowserBackendIdle is the shipped -browser-backend-idle value. Long
// enough that a human who wandered off mid-task still finds their browser;
// short enough that a crashed client's slot returns the same afternoon.
const defaultBrowserBackendIdle = 30 * time.Minute

func newBrowserBackend(maxSessions int, token, advertiseHost string) *browserBackend {
	if maxSessions <= 0 {
		maxSessions = vncPortEnd - vncPortStart + 1
	}
	return &browserBackend{
		sessions:      make(map[string]*backendSession),
		maxSessions:   maxSessions,
		token:         token,
		advertiseHost: advertiseHost,
		tunnelGuard:   tunnelPeerGuard,
		now:           time.Now,
	}
}

// authOK reports whether the request carries the configured bearer token. When
// no token is configured, all requests pass (single-tenant / trusted network).
func (bb *browserBackend) authOK(r *http.Request) bool {
	if bb.token == "" {
		return true
	}
	h := r.Header.Get("Authorization")
	return h == "Bearer "+bb.token
}

// allocSlot finds a free slot index [0,maxSessions); -1 if full. Caller holds mu.
func (bb *browserBackend) allocSlot() int {
	used := make(map[int]bool, len(bb.sessions))
	for _, s := range bb.sessions {
		used[s.slot] = true
	}
	for i := 0; i < bb.maxSessions; i++ {
		if !used[i] {
			return i
		}
	}
	return -1
}

type allocResponse struct {
	SessionID string `json:"sessionId"`
	Host      string `json:"host"`
	CDPPort   int    `json:"cdpPort"`
	VNCPort   int    `json:"vncPort"`
	// Tunnel echoes tunnel-mode allocation: the backend expects the client
	// to dial /sessions/{id}/tunnel and skipped chromium resolver rules.
	Tunnel bool `json:"tunnel,omitempty"`
}

func (bb *browserBackend) handleCreate(w http.ResponseWriter, r *http.Request) {
	var req struct {
		SessionID string `json:"sessionId"`
		// ResolveLocalhostTo overrides where chromium's loopback-style dev
		// hostnames point (e.g. behind NAT). Defaults to the allocation
		// request's source address -- the swe-swe host as this backend sees it.
		ResolveLocalhostTo string `json:"resolveLocalhostTo"`
		// LoopbackDomains overrides defaultLoopbackDomains (each maps bare +
		// wildcard) for projects using other loopback DNS schemes.
		LoopbackDomains []string `json:"loopbackDomains"`
		// Tunnel requests reverse-tunnel mode: loopback hostnames resolve
		// to THIS box (where the tunnel binds real listeners), so chromium
		// gets no resolver rules at all.
		Tunnel bool `json:"tunnel"`
	}
	// Body is optional; ignore decode errors on an empty body.
	_ = json.NewDecoder(r.Body).Decode(&req)
	resolveLocalhostTo := req.ResolveLocalhostTo
	if resolveLocalhostTo == "" {
		if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
			resolveLocalhostTo = host
		}
	}
	loopbackDomains := req.LoopbackDomains
	if len(loopbackDomains) == 0 {
		loopbackDomains = defaultLoopbackDomains
	}
	hostResolverRules := buildLoopbackResolverRules(loopbackDomains, resolveLocalhostTo)
	if req.Tunnel {
		hostResolverRules = ""
	}

	bb.mu.Lock()
	// Idempotency first: a re-POST for a live id must return that instance
	// even when the pool is at capacity (its own slot is what filled it).
	if req.SessionID != "" {
		if existing, ok := bb.sessions[req.SessionID]; ok {
			existing.lastUsed = bb.timeNow()
			bb.mu.Unlock()
			bb.writeAlloc(w, existing)
			return
		}
	}
	slot := bb.allocSlot()
	if slot < 0 {
		bb.mu.Unlock()
		http.Error(w, "browser backend at capacity", http.StatusServiceUnavailable)
		return
	}
	id := req.SessionID
	if id == "" {
		id = fmt.Sprintf("bb-%d", slot)
	}
	cdpPort := cdpPortStart + slot
	// Internal ports sit one range-size above their public counterparts:
	// chromium's loopback-only CDP and x11vnc's raw VNC.
	cdpInternal := cdpPort + (cdpPortEnd - cdpPortStart + 1)
	vncPort := vncPortStart + slot
	vncInternal := vncPort + (vncPortEnd - vncPortStart + 1)
	display := slot + 10 // avoid :0 (the host's own display)
	// Reserve the slot before the slow start so concurrent creates don't race
	// onto the same ports.
	bb.sessions[id] = &backendSession{id: id, slot: slot, cdpPort: cdpPort, vncPort: vncPort, tunnel: req.Tunnel, lastUsed: bb.timeNow()}
	bb.mu.Unlock()

	procs, err := browserProcsStarter(id, display, cdpPort, cdpInternal, vncPort, vncInternal, hostResolverRules)
	if err != nil {
		bb.mu.Lock()
		delete(bb.sessions, id)
		bb.mu.Unlock()
		log.Printf("browser-backend: start failed for %s: %v", id, err)
		http.Error(w, fmt.Sprintf("failed to start browser: %v", err), http.StatusInternalServerError)
		return
	}
	bb.mu.Lock()
	bb.sessions[id].procs = procs
	// The stack takes seconds to come up; re-stamp so the idle clock starts
	// when the browser is actually usable, not when it was requested.
	bb.sessions[id].lastUsed = bb.timeNow()
	sess := bb.sessions[id]
	bb.mu.Unlock()
	log.Printf("browser-backend: allocated %s (slot %d, cdp %d, vnc %d)", id, slot, cdpPort, vncPort)
	bb.writeAlloc(w, sess)
}

func (bb *browserBackend) writeAlloc(w http.ResponseWriter, s *backendSession) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(allocResponse{
		SessionID: s.id,
		Host:      bb.advertiseHost,
		CDPPort:   s.cdpPort,
		VNCPort:   s.vncPort,
		Tunnel:    s.tunnel,
	})
}

func (bb *browserBackend) handleDelete(w http.ResponseWriter, id string) {
	bb.mu.Lock()
	sess, ok := bb.sessions[id]
	if ok {
		delete(bb.sessions, id)
	}
	bb.mu.Unlock()
	if !ok {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	if sess.tunnelStop != nil {
		// Closes the tunnel WS; its handler then tears down the loopback
		// listeners and streams (no orphans on session end).
		sess.tunnelStop()
	}
	if sess.procs != nil {
		sess.procs.stop()
	}
	log.Printf("browser-backend: freed %s", id)
	w.WriteHeader(http.StatusNoContent)
}

// timeNow is bb.now with a nil-safe fallback (zero-value browserBackends in
// older tests never went through newBrowserBackend).
func (bb *browserBackend) timeNow() time.Time {
	if bb.now != nil {
		return bb.now()
	}
	return time.Now()
}

// handleTouch is the explicit keepalive: "someone is still using this session,
// do not reap it". The swe-swe host calls it while a human has the Agent View
// (VNC) pane open -- that traffic terminates in websockify, not in any Go
// handler here, so it is otherwise invisible to the reaper.
func (bb *browserBackend) handleTouch(w http.ResponseWriter, id string) {
	bb.mu.Lock()
	sess, ok := bb.sessions[id]
	if ok {
		sess.lastUsed = bb.timeNow()
	}
	bb.mu.Unlock()
	if !ok {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// reapIdle frees every session idle for longer than idleTimeout and returns
// their ids. Teardown runs OUTSIDE bb.mu: stop() kills processes and deletes a
// profile dir, which must not block allocation.
func (bb *browserBackend) reapIdle() []string {
	if bb.idleTimeout <= 0 {
		return nil
	}
	now := bb.timeNow()
	var reaped []string
	var doomed []*backendSession
	bb.mu.Lock()
	for id, sess := range bb.sessions {
		idle, isIdle := sess.idleFor(now)
		if !isIdle || idle < bb.idleTimeout {
			continue
		}
		delete(bb.sessions, id)
		doomed = append(doomed, sess)
		reaped = append(reaped, id)
		log.Printf("browser-backend: reaping %s (slot %d) -- idle %s (limit %s)",
			id, sess.slot, idle.Round(time.Second), bb.idleTimeout)
	}
	bb.mu.Unlock()
	for _, sess := range doomed {
		if sess.tunnelStop != nil {
			sess.tunnelStop()
		}
		if sess.procs != nil {
			sess.procs.stop()
		}
	}
	return reaped
}

// freeAll tears every live session down and empties the table, returning the
// ids it freed. Same teardown as handleDelete/reapIdle, applied to all of them.
//
// This exists for shutdown. Without it a SIGTERM (docker stop, a systemd
// restart, the e2e harness killing the process) ends THIS process only and
// abandons every browser stack it started: Xvfb, chromium, x11vnc and
// websockify keep running and keep holding their VNC/CDP ports. The next
// backend then cannot bind those slots, and since startSupervisedProc refuses
// a held port, allocation fails outright -- observed as Agent View never
// appearing after a backend restart.
func (bb *browserBackend) freeAll() []string {
	bb.mu.Lock()
	doomed := make([]*backendSession, 0, len(bb.sessions))
	freed := make([]string, 0, len(bb.sessions))
	for id, sess := range bb.sessions {
		doomed = append(doomed, sess)
		freed = append(freed, id)
		delete(bb.sessions, id)
	}
	bb.mu.Unlock()
	// Outside bb.mu, same as reapIdle: stop() kills process groups and deletes
	// profile dirs.
	for _, sess := range doomed {
		if sess.tunnelStop != nil {
			sess.tunnelStop()
		}
		if sess.procs != nil {
			sess.procs.stop()
		}
	}
	return freed
}

// startReaper runs reapIdle on a ticker until stop closes. No-op when idle
// reaping is disabled.
func (bb *browserBackend) startReaper(stop <-chan struct{}) {
	if bb.idleTimeout <= 0 {
		log.Printf("browser-backend: idle reaping disabled")
		return
	}
	// Check often enough that a freed slot is available promptly, without
	// waking up pointlessly on a long timeout.
	interval := bb.idleTimeout / 6
	if interval < 10*time.Second {
		interval = 10 * time.Second
	}
	if interval > time.Minute {
		interval = time.Minute
	}
	log.Printf("browser-backend: idle reaper on (timeout %s, checking every %s)", bb.idleTimeout, interval)
	go func() {
		defer recoverGoroutine("browser-backend idle reaper")
		t := time.NewTicker(interval)
		defer t.Stop()
		for {
			select {
			case <-stop:
				return
			case <-t.C:
				bb.reapIdle()
			}
		}
	}()
}

func (bb *browserBackend) handleReady(w http.ResponseWriter, id string) {
	bb.mu.Lock()
	sess, ok := bb.sessions[id]
	if ok {
		sess.lastUsed = bb.timeNow()
	}
	bb.mu.Unlock()
	if !ok {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	if up, _ := probePort(sess.vncPort); up {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"ready":true}`))
		return
	}
	w.WriteHeader(http.StatusServiceUnavailable)
	w.Write([]byte(`{"ready":false}`))
}

func (bb *browserBackend) handleHealth(w http.ResponseWriter) {
	bb.mu.Lock()
	n := len(bb.sessions)
	bb.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	fmt.Fprintf(w, `{"sessions":%d,"max":%d}`, n, bb.maxSessions)
}

// ServeHTTP routes the allocation API. Kept as a method so tests can exercise
// it via httptest without binding a real listener.
func (bb *browserBackend) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/health" {
		bb.handleHealth(w)
		return
	}
	if !bb.authOK(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	switch {
	case r.URL.Path == "/sessions" && r.Method == http.MethodPost:
		bb.handleCreate(w, r)
	case strings.HasPrefix(r.URL.Path, "/sessions/"):
		rest := strings.TrimPrefix(r.URL.Path, "/sessions/")
		if id := strings.TrimSuffix(rest, "/ready"); id != rest {
			if r.Method != http.MethodGet {
				http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
				return
			}
			bb.handleReady(w, id)
			return
		}
		if id := strings.TrimSuffix(rest, "/touch"); id != rest {
			if r.Method != http.MethodPost {
				http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
				return
			}
			bb.handleTouch(w, id)
			return
		}
		if id := strings.TrimSuffix(rest, "/tunnel"); id != rest {
			if r.Method != http.MethodGet {
				http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
				return
			}
			bb.handleTunnel(w, r, id)
			return
		}
		if r.Method != http.MethodDelete {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		bb.handleDelete(w, rest)
	default:
		http.Error(w, "not found", http.StatusNotFound)
	}
}

// runBrowserBackend starts the allocation service on addr and blocks. Invoked
// from main() when -mode browser-backend is set.
func runBrowserBackend(addr string, maxSessions int, token, advertiseHost string, idleTimeout time.Duration) error {
	if !browserStackAvailable() {
		return fmt.Errorf("browser-backend mode requires the display stack (Xvfb/chromium/x11vnc/websockify) -- none found on PATH")
	}
	bb := newBrowserBackend(maxSessions, token, advertiseHost)
	bb.idleTimeout = idleTimeout
	if _, portStr, err := net.SplitHostPort(addr); err == nil {
		if p, err := strconv.Atoi(portStr); err == nil {
			bb.servicePort = p
		}
	}
	log.Printf("browser-backend: listening on %s (max %d sessions, auth=%v, advertise=%q, idle=%s)",
		addr, bb.maxSessions, token != "", advertiseHost, idleTimeout)
	// main() only reaches startSubreaper() on the UI-server path, so this mode
	// had no zombie reaper at all. It needs one MORE than the UI server does:
	// every allocation leaves a websockify child that killBrowserProc kills but
	// nobody wait4()s, and this process runs for weeks.
	startSubreaper()
	bb.startReaper(nil)
	// Free every browser stack before exiting. A plain ListenAndServe dies on
	// SIGTERM with its browsers still running, and those orphans hold the very
	// ports the next backend needs (see freeAll).
	srv := &http.Server{Addr: addr, Handler: bb}
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(sigs)
	serveErr := make(chan error, 1)
	go func() { serveErr <- srv.ListenAndServe() }()
	select {
	case err := <-serveErr:
		return err
	case sig := <-sigs:
		log.Printf("browser-backend: %s -- tearing down browsers before exit", sig)
		freed := bb.freeAll()
		log.Printf("browser-backend: freed %d session(s) on shutdown", len(freed))
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := srv.Shutdown(ctx); err != nil {
			log.Printf("browser-backend: shutdown: %v", err)
		}
		return nil
	}
}

// resolveBrowserBackendIdle applies flag -> env -> default for the idle reap
// timeout. "0" (either source) disables reaping.
func resolveBrowserBackendIdle(flagVal time.Duration, flagWasSet bool) time.Duration {
	if !flagWasSet {
		if env := strings.TrimSpace(os.Getenv("SWE_BROWSER_BACKEND_IDLE")); env != "" {
			d, err := time.ParseDuration(env)
			if err != nil {
				log.Printf("browser-backend: ignoring SWE_BROWSER_BACKEND_IDLE=%q: %v", env, err)
				return defaultBrowserBackendIdle
			}
			return d
		}
		return defaultBrowserBackendIdle
	}
	return flagVal
}
