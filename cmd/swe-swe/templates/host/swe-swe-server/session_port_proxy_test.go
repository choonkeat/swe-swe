package main

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// Layer 2 -- the per-port auth gate authorizes a shared-session guest by the
// CURRENT owner of the proxy port (their live session maps to it), NOT a session
// UUID captured when the listener was created. This is what lets a legitimate
// guest through when an earlier, now-ended session left an orphaned listener
// squatting the port the guest's live session reuses: the listener's original
// owner UUID is irrelevant; only the guest's live session-to-port mapping is.
func TestRequireAuthCookiePortOwnership(t *testing.T) {
	const secret = "master"
	const proxyPort = 24001
	// A live session that currently owns agent-chat proxy port 24001 (acPort 4001).
	registerTestSession(t, "live-owner", &Session{UUID: "live-owner", AgentChatPort: 4001})
	// A live session mapping to a DIFFERENT agent-chat port.
	registerTestSession(t, "other-live", &Session{UUID: "other-live", AgentChatPort: 4002})

	authorized := func(scope string) bool {
		return scopeOwnsProxyPort(scope, proxyPort, func(s *Session) int {
			return agentChatProxyPort(s.AgentChatPort)
		})
	}
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })
	handler := requireAuthCookie(secret, authorized, inner)

	code := func(cookieVal string) int {
		r := httptest.NewRequest("GET", "/foo", nil)
		if cookieVal != "" {
			r.AddCookie(&http.Cookie{Name: authCookieName, Value: cookieVal})
		}
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, r)
		return rr.Code
	}

	// Guest scoped to the live session that maps to this port -> allowed, no
	// matter which (possibly dead) session created the listener.
	if c := code(authSignScopedCookie(secret, "live-owner")); c != http.StatusOK {
		t.Errorf("guest of live port owner: code=%d, want 200", c)
	}
	// Guest scoped to a live session that maps to a DIFFERENT port -> 401.
	if c := code(authSignScopedCookie(secret, "other-live")); c != http.StatusUnauthorized {
		t.Errorf("guest of a different port's session: code=%d, want 401", c)
	}
	// Guest scoped to a session that no longer exists -> 401.
	if c := code(authSignScopedCookie(secret, "ended-ghost")); c != http.StatusUnauthorized {
		t.Errorf("guest of ended session: code=%d, want 401", c)
	}
	// Full (unscoped) user -> allowed on any port.
	if c := code(authSignCookie(secret)); c != http.StatusOK {
		t.Errorf("unscoped user: code=%d, want 200", c)
	}
	// No cookie -> 401.
	if c := code(""); c != http.StatusUnauthorized {
		t.Errorf("no cookie: code=%d, want 401", c)
	}
	// /__probe__ is exempt (client reachability probe) -> 200 without a cookie.
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, httptest.NewRequest("GET", "/__probe__", nil))
	if rr.Code != http.StatusOK {
		t.Errorf("/__probe__ exempt: code=%d, want 200", rr.Code)
	}
}

// scopeOwnsProxyPort: empty scope is always allowed; a live scope is allowed only
// for the port its session maps to; an unknown (ended) scope is denied.
func TestScopeOwnsProxyPort(t *testing.T) {
	registerTestSession(t, "s-a", &Session{UUID: "s-a", AgentChatPort: 4005})
	portOf := func(s *Session) int { return agentChatProxyPort(s.AgentChatPort) }
	if !scopeOwnsProxyPort("", 24005, portOf) {
		t.Error("empty scope (full user) should always be allowed")
	}
	if !scopeOwnsProxyPort("s-a", 24005, portOf) {
		t.Error("live session should own its own proxy port")
	}
	if scopeOwnsProxyPort("s-a", 24006, portOf) {
		t.Error("live session must NOT own a different proxy port")
	}
	if scopeOwnsProxyPort("gone", 24005, portOf) {
		t.Error("unknown/ended session scope must be denied")
	}
}

// Layer 1 -- a per-port proxy listener started for a session that was already
// closed (the session is registered in the sessions map before its listeners are
// wired, so a fast teardown can race listener setup) must NOT be stored on the
// session and must be shut down immediately. Otherwise it outlives the session
// and squats the port forever, and its stale auth gate 401s later guests.
func TestTrackProxyServerSelfTeardownWhenClosed(t *testing.T) {
	s := &Session{UUID: "closed-race"}
	s.mu.Lock()
	s.closed = true // simulate Close() having already run
	s.mu.Unlock()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr := ln.Addr().String()
	srv := &http.Server{}
	serveDone := make(chan struct{})
	go func() { srv.Serve(ln); close(serveDone) }()

	s.trackProxyServer(srv, func(s *Session, x *http.Server) { s.AgentChatProxyServer = x })

	if s.AgentChatProxyServer != nil {
		t.Fatal("listener stored on an already-closed session -> would leak/squat the port")
	}
	select {
	case <-serveDone:
	case <-time.After(3 * time.Second):
		t.Fatal("Serve did not return; listener leaked")
	}
	ln2, err := net.Listen("tcp", addr)
	if err != nil {
		t.Fatalf("port not freed after self-teardown: %v", err)
	}
	ln2.Close()
}

// A listener tracked on a live (not-closed) session is stored so Close() can
// later shut it down; a nil server (bind-failure path) is a no-op.
func TestTrackProxyServerStoresWhenOpen(t *testing.T) {
	s := &Session{UUID: "open"}
	srv := &http.Server{}
	s.trackProxyServer(srv, func(s *Session, x *http.Server) { s.AgentChatProxyServer = x })
	if s.AgentChatProxyServer != srv {
		t.Fatal("listener not stored on a live session")
	}
	s.trackProxyServer(nil, func(s *Session, x *http.Server) { s.AgentChatProxyServer = x })
	if s.AgentChatProxyServer != srv {
		t.Fatal("nil track must be a no-op")
	}
}

// startProxyListener binds synchronously (so the returned server always has a
// live listener for Close to shut down) and returns nil when the port is taken.
func TestStartProxyListenerBindsAndShutsDown(t *testing.T) {
	// Grab a free port, release it, reuse the number for the listener.
	probe, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := probe.Addr().String()
	probe.Close()

	srv := startProxyListener("test", "sess", addr, http.NotFoundHandler())
	if srv == nil {
		t.Fatal("startProxyListener returned nil for a free port")
	}
	// Port is bound: a second bind fails, and startProxyListener returns nil.
	if got := startProxyListener("test", "sess", addr, http.NotFoundHandler()); got != nil {
		got.Shutdown(context.Background())
		t.Fatal("startProxyListener should return nil when the port is already taken")
	}
	// Shutdown frees the port.
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	srv.Shutdown(ctx)
	var freed bool
	for i := 0; i < 50; i++ {
		if l, err := net.Listen("tcp", addr); err == nil {
			l.Close()
			freed = true
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if !freed {
		t.Fatal("port not freed after Shutdown")
	}
}
