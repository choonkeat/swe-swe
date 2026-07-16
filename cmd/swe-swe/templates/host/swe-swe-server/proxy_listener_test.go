package main

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"testing"
	"time"
)

// TestSessionPerPortServerShutdown -- mirrors Session.Close()'s shutdown
// pattern for the three per-port proxy servers (preview/agent-chat/vnc).
// Catches the case where a session ends but its listener leaks, which would
// stop the next session in the same port range from binding (leading to
// silent "preview proxy port unavailable" log lines and broken iframes).
func TestSessionPerPortServerShutdown(t *testing.T) {
	const n = 3
	servers := make([]*http.Server, n)
	addrs := make([]string, n)

	for i := 0; i < n; i++ {
		ln, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatalf("bind %d: %v", i, err)
		}
		addrs[i] = ln.Addr().String()
		srv := &http.Server{Handler: http.NotFoundHandler()}
		servers[i] = srv
		go func() { _ = srv.Serve(ln) }()
	}

	// Confirm all three accept connections.
	for _, addr := range addrs {
		conn, err := net.DialTimeout("tcp", addr, time.Second)
		if err != nil {
			t.Fatalf("listener %s not accepting: %v", addr, err)
		}
		conn.Close()
	}

	// Shut them down -- exact pattern Session.Close uses.
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	for _, srv := range servers {
		if err := srv.Shutdown(ctx); err != nil {
			t.Errorf("Shutdown returned error: %v", err)
		}
	}

	// Confirm the listeners are gone -- a fresh dial should fail.
	for _, addr := range addrs {
		conn, err := net.DialTimeout("tcp", addr, 200*time.Millisecond)
		if err == nil {
			conn.Close()
			t.Errorf("listener %s still accepting after Shutdown -- session-end leak", addr)
		}
	}
}

// TestRequireAuthCookieWithAgentChatProxy -- end-to-end chain:
// corsWrapper -> requireAuthCookie -> agentChatProxyHandler.
// Verifies upstream is unreachable without a valid cookie, reachable with
// one, and that /__probe__ short-circuits (returns 200 with the marker
// header) before either the auth wrap or the upstream sees the request.
func TestRequireAuthCookieWithAgentChatProxy(t *testing.T) {
	upstreamHits := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamHits++
		w.Header().Set("X-Upstream", "1")
		w.WriteHeader(http.StatusOK)
		fmt.Fprintln(w, "upstream-body")
	}))
	defer upstream.Close()
	target, _ := url.Parse(upstream.URL)

	secret := "proxy-test-secret"
	handler := corsWrapper(requireAuthCookie(secret, scopeIs("test-session"), agentChatProxyHandler(target)))

	// 1. No cookie -> 401, upstream not touched.
	req := httptest.NewRequest("GET", "/", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusUnauthorized {
		t.Errorf("no-cookie: expected 401, got %d", rr.Code)
	}
	if rr.Header().Get("X-Upstream") != "" {
		t.Errorf("no-cookie: upstream reached")
	}
	if upstreamHits != 0 {
		t.Errorf("no-cookie: upstream hit count=%d, expected 0", upstreamHits)
	}

	// 2. Valid cookie -> 200, upstream touched, marker header present.
	req = httptest.NewRequest("GET", "/", nil)
	req.AddCookie(&http.Cookie{Name: authCookieName, Value: authSignCookie(secret)})
	rr = httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Errorf("with-cookie: expected 200, got %d", rr.Code)
	}
	if rr.Header().Get("X-Upstream") == "" {
		t.Errorf("with-cookie: upstream not reached")
	}
	if upstreamHits != 1 {
		t.Errorf("with-cookie: upstream hit count=%d, expected 1", upstreamHits)
	}

	// 3. /__probe__ no cookie -> 200 from corsWrapper short-circuit.
	upstreamHits = 0
	req = httptest.NewRequest("GET", "/__probe__", nil)
	rr = httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Errorf("/__probe__: expected 200, got %d", rr.Code)
	}
	if rr.Header().Get("X-Agent-Reverse-Proxy") == "" {
		t.Errorf("/__probe__: missing X-Agent-Reverse-Proxy marker")
	}
	if upstreamHits != 0 {
		t.Errorf("/__probe__: upstream hit when probe should short-circuit")
	}
}

// TestVNCReverseProxyDirectorRewritesHost -- VNC-specific test: the Director
// must rewrite Host so websockify sees its own origin. Without this, a
// Host header carrying the {port}.{publicHostname} value could trip
// virtual-host filters or surface in logs as a confusing client identity.
func TestVNCReverseProxyDirectorRewritesHost(t *testing.T) {
	gotHost := ""
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHost = r.Host
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()
	target, _ := url.Parse(upstream.URL)

	rp := httputil.NewSingleHostReverseProxy(target)
	rp.Director = func(req *http.Request) {
		req.URL.Scheme = "http"
		req.URL.Host = target.Host
		req.Host = target.Host
	}

	req := httptest.NewRequest("GET", "/vnc_lite.html", nil)
	req.Host = "27000.swe-swe-test-abc-tunnel.example.com"
	rr := httptest.NewRecorder()
	rp.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200 from upstream, got %d", rr.Code)
	}
	if gotHost != target.Host {
		t.Errorf("Host rewrite: got %q, want %q", gotHost, target.Host)
	}
}

// TestVNCAuthWrapBlocksWebSocketUpgradeWithoutCookie -- WebSocket upgrade
// requests without a valid cookie must be rejected at the auth wrap before
// any bytes reach the upstream. The upgrade path is the one most likely
// to bypass auth in real-world reverse-proxy chains because the handshake
// happens before the proxied response.
func TestVNCAuthWrapBlocksWebSocketUpgradeWithoutCookie(t *testing.T) {
	upstreamReached := false
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamReached = true
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()
	target, _ := url.Parse(upstream.URL)

	rp := httputil.NewSingleHostReverseProxy(target)
	handler := requireAuthCookie("vnc-secret", scopeIs("test-session"), rp)

	req := httptest.NewRequest("GET", "/websockify", nil)
	req.Header.Set("Connection", "Upgrade")
	req.Header.Set("Upgrade", "websocket")
	req.Header.Set("Sec-WebSocket-Version", "13")
	req.Header.Set("Sec-WebSocket-Key", "dGhlIHNhbXBsZSBub25jZQ==")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Errorf("WS upgrade no-cookie: expected 401, got %d", rr.Code)
	}
	if upstreamReached {
		t.Errorf("WS upgrade no-cookie: upstream reached -- auth bypass on upgrade path")
	}
}

// TestVNCAuthWrapAllowsWebSocketUpgradeWithCookie -- complementary positive
// case: a valid cookie permits the upgrade request to reach the upstream.
// The upstream here returns 200 (not a real upgrade) -- we're only testing
// that the auth wrap does not block the request when the cookie is good.
func TestVNCAuthWrapAllowsWebSocketUpgradeWithCookie(t *testing.T) {
	upstreamReached := false
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamReached = true
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()
	target, _ := url.Parse(upstream.URL)

	rp := httputil.NewSingleHostReverseProxy(target)
	secret := "vnc-secret"
	handler := requireAuthCookie(secret, scopeIs("test-session"), rp)

	req := httptest.NewRequest("GET", "/websockify", nil)
	req.AddCookie(&http.Cookie{Name: authCookieName, Value: authSignCookie(secret)})
	req.Header.Set("Connection", "Upgrade")
	req.Header.Set("Upgrade", "websocket")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if !upstreamReached {
		t.Errorf("WS upgrade with cookie: upstream NOT reached (auth wrap rejected valid cookie)")
	}
}
