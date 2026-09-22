package main

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
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

// TestVNCSameOriginPathRoute -- the live Agent View must also be reachable
// from the MAIN listener at /proxy/{uuid}/vnc/, so a box with exactly one
// reachable port (no wildcard DNS, no tunnel) can still show it. Preview,
// Agent Chat and Files already have this same-origin path form; Agent View was
// the last pane that could only be reached on its own port.
//
// Exercises the full production chain: authMiddleware -> handleProxyRoute ->
// Session.SessionMux -> the VNC reverse proxy, covering the noVNC page, the
// websockify upgrade, and both auth gates.
func TestVNCSameOriginPathRoute(t *testing.T) {
	const secret = "vnc-path-secret"
	const uuid = "vnc-path-sess"
	const pageBody = "novnc-page"
	const echoPrefix = "echo:"

	// Stand-in for websockify: serves the noVNC page and echoes on the socket.
	wsUpgrader := websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}
	websockify := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/vnc_lite.html":
			w.Write([]byte(pageBody))
		case "/websockify":
			conn, err := wsUpgrader.Upgrade(w, r, nil)
			if err != nil {
				t.Logf("websockify double upgrade error: %v", err)
				return
			}
			defer conn.Close()
			for {
				mt, msg, err := conn.ReadMessage()
				if err != nil {
					return
				}
				if err := conn.WriteMessage(mt, append([]byte(echoPrefix), msg...)); err != nil {
					return
				}
			}
		default:
			http.NotFound(w, r)
		}
	}))
	defer websockify.Close()

	wsURL, _ := url.Parse(websockify.URL)
	vncPort, err := strconv.Atoi(wsURL.Port())
	if err != nil {
		t.Fatalf("parse websockify port from %q: %v", websockify.URL, err)
	}

	sess := &Session{Assistant: "claude"}
	registerTestSession(t, uuid, sess)
	sessMux := http.NewServeMux()
	registerVNCPathRoute(sessMux, sess, newVNCReverseProxy(sess, vncPort))
	sess.SessionMux = sessMux

	srv := httptest.NewServer(authMiddleware(http.HandlerFunc(handleProxyRoute), secret))
	defer srv.Close()

	base := srv.URL + "/proxy/" + uuid + "/vnc"
	// Do not follow the login redirect: we want to see the redirect itself.
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	get := func(path string, cookie *http.Cookie) *http.Response {
		t.Helper()
		req, _ := http.NewRequest("GET", base+path, nil)
		if cookie != nil {
			req.AddCookie(cookie)
		}
		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("GET %s: %v", path, err)
		}
		return resp
	}

	fullCookie := &http.Cookie{Name: authCookieName, Value: authSignCookie(secret)}

	// a. The noVNC page is served through the path form.
	resp := get("/vnc_lite.html", fullCookie)
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("GET /vnc_lite.html: got %d, want 200", resp.StatusCode)
	}
	if string(body) != pageBody {
		t.Errorf("GET /vnc_lite.html: body %q, want %q", string(body), pageBody)
	}

	// b. The websockify upgrade round-trips through the path form.
	dialer := websocket.Dialer{}
	hdr := http.Header{"Cookie": []string{fullCookie.Name + "=" + fullCookie.Value}}
	conn, dialResp, err := dialer.Dial("ws"+strings.TrimPrefix(base, "http")+"/websockify", hdr)
	if err != nil {
		t.Fatalf("websockify dial through path route failed: %v (resp=%v)", err, dialResp)
	}
	defer conn.Close()
	if err := conn.WriteMessage(websocket.TextMessage, []byte("rfb")); err != nil {
		t.Fatalf("websockify write: %v", err)
	}
	_, msg, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("websockify read: %v", err)
	}
	if want := echoPrefix + "rfb"; string(msg) != want {
		t.Errorf("websockify echo = %q, want %q", string(msg), want)
	}

	// c. No cookie: refused on both the page and the upgrade.
	resp = get("/vnc_lite.html", nil)
	resp.Body.Close()
	if resp.StatusCode == http.StatusOK {
		t.Errorf("GET /vnc_lite.html with no cookie: got 200 -- auth bypass on the path route")
	}
	if _, _, err := dialer.Dial("ws"+strings.TrimPrefix(base, "http")+"/websockify", nil); err == nil {
		t.Errorf("websockify upgrade with no cookie succeeded -- auth bypass on the upgrade path")
	}

	// d. A guest shared a DIFFERENT session must not reach this one.
	guest := &http.Cookie{Name: authCookieName, Value: authSignScopedCookie(secret, "some-other-session")}
	resp = get("/vnc_lite.html", guest)
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("GET /vnc_lite.html as a guest of another session: got %d, want 403", resp.StatusCode)
	}
}

// TestVNCPortListenerAnswersProbe -- the per-port VNC listener must answer the
// same /__probe__ reachability check preview/agent-chat/files answer. Without
// it the browser cannot tell a working VNC port from a blocked one, so the
// port candidate would always lose and Agent View would fall through to the
// (slower, same-origin) path form even on a box where the port works.
//
// Built through newVNCPortHandler -- the same call main.go makes -- so the
// wrap cannot drift out from under this test.
func TestVNCPortListenerAnswersProbe(t *testing.T) {
	upstreamHits := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamHits++
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()
	wsURL, _ := url.Parse(upstream.URL)
	vncPort, err := strconv.Atoi(wsURL.Port())
	if err != nil {
		t.Fatalf("parse upstream port from %q: %v", upstream.URL, err)
	}

	sess := &Session{UUID: "vnc-probe-sess", Assistant: "claude"}
	handler := newVNCPortHandler(sess, vncProxyPort(vncPort), newVNCReverseProxy(sess, vncPort), "vnc-secret")

	req := httptest.NewRequest("GET", "/__probe__", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("/__probe__ on the VNC listener: got %d, want 200", rr.Code)
	}
	if rr.Header().Get("X-Agent-Reverse-Proxy") == "" {
		t.Errorf("/__probe__ on the VNC listener: missing X-Agent-Reverse-Proxy marker -- the port form is undetectable, so Agent View can never pick it")
	}
	if upstreamHits != 0 {
		t.Errorf("/__probe__ reached websockify (%d hits); it must short-circuit", upstreamHits)
	}
}
