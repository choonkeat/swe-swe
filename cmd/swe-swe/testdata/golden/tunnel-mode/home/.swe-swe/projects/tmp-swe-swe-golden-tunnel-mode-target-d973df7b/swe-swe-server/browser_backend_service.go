package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"strings"
	"sync"
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
//	POST   /sessions            -> {sessionId, host, cdpPort, vncPort}
//	DELETE /sessions/{id}        -> 204
//	GET    /sessions/{id}/ready  -> 200 (websockify up) | 503
//	GET    /health               -> {sessions, max}
//
// Auth: a shared bearer token (SWE_BROWSER_BACKEND_TOKEN) guards the /sessions
// routes so a public box is not an open browser relay. /health is open.

// browserProcsStarter is indirected so the service's allocation logic can be
// unit-tested without spawning a real Xvfb/chromium/x11vnc/websockify stack.
var browserProcsStarter = startBrowserProcs

type backendSession struct {
	id      string
	slot    int
	cdpPort int
	vncPort int
	procs   *browserProcs
}

// browserBackend is the allocator state for the service.
type browserBackend struct {
	mu            sync.Mutex
	sessions      map[string]*backendSession
	maxSessions   int
	token         string // bearer token; empty = no auth (not for public boxes)
	advertiseHost string // host clients should dial for CDP/VNC ports
}

func newBrowserBackend(maxSessions int, token, advertiseHost string) *browserBackend {
	if maxSessions <= 0 {
		maxSessions = vncPortEnd - vncPortStart + 1
	}
	return &browserBackend{
		sessions:      make(map[string]*backendSession),
		maxSessions:   maxSessions,
		token:         token,
		advertiseHost: advertiseHost,
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
}

func (bb *browserBackend) handleCreate(w http.ResponseWriter, r *http.Request) {
	var req struct {
		SessionID string `json:"sessionId"`
		// ResolveLocalhostTo overrides where chromium's "localhost" points
		// (e.g. behind NAT). Defaults to the allocation request's source
		// address -- the swe-swe host as this backend sees it.
		ResolveLocalhostTo string `json:"resolveLocalhostTo"`
	}
	// Body is optional; ignore decode errors on an empty body.
	_ = json.NewDecoder(r.Body).Decode(&req)
	resolveLocalhostTo := req.ResolveLocalhostTo
	if resolveLocalhostTo == "" {
		if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
			resolveLocalhostTo = host
		}
	}

	bb.mu.Lock()
	// Idempotency first: a re-POST for a live id must return that instance
	// even when the pool is at capacity (its own slot is what filled it).
	if req.SessionID != "" {
		if existing, ok := bb.sessions[req.SessionID]; ok {
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
	bb.sessions[id] = &backendSession{id: id, slot: slot, cdpPort: cdpPort, vncPort: vncPort}
	bb.mu.Unlock()

	procs, err := browserProcsStarter(id, display, cdpPort, cdpInternal, vncPort, vncInternal, resolveLocalhostTo)
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
	if sess.procs != nil {
		sess.procs.stop()
	}
	log.Printf("browser-backend: freed %s", id)
	w.WriteHeader(http.StatusNoContent)
}

func (bb *browserBackend) handleReady(w http.ResponseWriter, id string) {
	bb.mu.Lock()
	sess, ok := bb.sessions[id]
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
func runBrowserBackend(addr string, maxSessions int, token, advertiseHost string) error {
	if !browserStackAvailable() {
		return fmt.Errorf("browser-backend mode requires the display stack (Xvfb/chromium/x11vnc/websockify) -- none found on PATH")
	}
	bb := newBrowserBackend(maxSessions, token, advertiseHost)
	log.Printf("browser-backend: listening on %s (max %d sessions, auth=%v, advertise=%q)",
		addr, bb.maxSessions, token != "", advertiseHost)
	return http.ListenAndServe(addr, bb)
}
