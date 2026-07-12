package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"

	agentproxy "github.com/choonkeat/agent-reverse-proxy"
)

// buildPreviewListenerHandler assembles the exact per-session preview listener
// handler chain used in production (see main.go): corsWrapper -> requireAuthCookie
// -> previewVhostPinHandler -> portPreviewProxy(hooks). Returns the handler and
// the fixed-target proxy so tests can drive the whole stack in-process.
func buildPreviewListenerHandler(t *testing.T, sess *Session, secret string, fixedTarget *url.URL) http.Handler {
	t.Helper()
	proxy, err := agentproxy.New(agentproxy.Config{
		Target:     fixedTarget,
		ToolPrefix: "preview",
		NoInject:   true,
		ResolveTarget: func(inboundHost string) (*url.URL, string, bool) {
			return previewResolveTarget(inboundHost, sess)
		},
		CookieDomainRewrite: previewCookieDomainRewrite,
	})
	if err != nil {
		t.Fatalf("New preview proxy: %v", err)
	}
	return corsWrapper(requireAuthCookie(secret, sess.UUID, previewVhostPinHandler(sess, proxy)))
}

func authedCookie(secret string) *http.Cookie {
	return &http.Cookie{Name: authCookieName, Value: authSignCookie(secret)}
}

// TestPreviewListenerWildcardChain drives the full auth-wrapped listener with a
// browser-facing "app1-{port}.<reach>" request and asserts the backend on
// {port} sees the rewritten logical Host, and that Set-Cookie Domain is
// rewritten logical(.lvh.me) -> reach(.<reach>).
func TestPreviewListenerWildcardChain(t *testing.T) {
	const secret = "integration-secret"
	var gotHost string
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHost = r.Host
		if r.URL.Path == "/set-cookie" {
			http.SetCookie(w, &http.Cookie{Name: "sid", Value: "1", Domain: ".lvh.me", Path: "/"})
		}
		w.Write([]byte("vhost-echo"))
	}))
	defer backend.Close()
	bu, _ := url.Parse(backend.URL)
	port, _ := strconv.Atoi(bu.Port())
	if port < 1024 {
		t.Skipf("ephemeral backend port %d < 1024", port)
	}

	sess := &Session{UUID: "11111111-2222-3333-4444-555555555555", PreviewPort: 65000}
	fixed := &url.URL{Scheme: "http", Host: fmt.Sprintf("localhost:%d", sess.PreviewPort)}
	h := buildPreviewListenerHandler(t, sess, secret, fixed)

	reach := "127-0-0-1.sslip.io"
	inboundHost := fmt.Sprintf("app1-%d.%s:23000", port, reach)

	// Authed demux request -> reaches backend with logical Host.
	req := httptest.NewRequest("GET", "/", nil)
	req.Host = inboundHost
	req.AddCookie(authedCookie(secret))
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("demux GET: code %d, want 200", rr.Code)
	}
	if want := fmt.Sprintf("app1.lvh.me:%d", port); gotHost != want {
		t.Errorf("upstream Host = %q, want %q", gotHost, want)
	}

	// Cookie reach-rewrite: logical .lvh.me -> reach .127-0-0-1.sslip.io.
	req = httptest.NewRequest("GET", "/set-cookie", nil)
	req.Host = inboundHost
	req.AddCookie(authedCookie(secret))
	rr = httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	var sidHdr string
	for _, sc := range rr.Result().Header["Set-Cookie"] {
		if strings.HasPrefix(sc, "sid=") {
			sidHdr = sc
		}
	}
	if !strings.Contains(sidHdr, "Domain="+reach) {
		t.Errorf("sid Set-Cookie = %q, want Domain=%s (reach-rewritten)", sidHdr, reach)
	}
}

// TestPreviewListenerProbeExemptAndAuth asserts the reach probe path
// (/__probe__) is served without a cookie and carries X-Agent-Reverse-Proxy,
// while any other path without a valid cookie is rejected 401.
func TestPreviewListenerProbeExemptAndAuth(t *testing.T) {
	const secret = "integration-secret"
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("ok"))
	}))
	defer backend.Close()
	bu, _ := url.Parse(backend.URL)

	sess := &Session{UUID: "11111111-2222-3333-4444-555555555555", PreviewPort: 65000}
	h := buildPreviewListenerHandler(t, sess, secret, bu)

	// Unauthenticated /__probe__ on a probe subdomain -> reaches the proxy,
	// which stamps X-Agent-Reverse-Proxy (this is what the reach probe checks).
	req := httptest.NewRequest("GET", "/__probe__", nil)
	req.Host = "probe-abc123.127-0-0-1.sslip.io:23000"
	req.Header.Set("Origin", "http://localhost:9780")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Header().Get("X-Agent-Reverse-Proxy") == "" {
		t.Errorf("/__probe__ missing X-Agent-Reverse-Proxy header (reach probe would fail)")
	}

	// Unauthenticated non-probe path -> 401 (nothing reaches the proxy).
	req = httptest.NewRequest("GET", "/", nil)
	req.Host = "app1-5000.127-0-0-1.sslip.io:23000"
	rr = httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusUnauthorized {
		t.Errorf("unauth GET /: code %d, want 401", rr.Code)
	}
}

// TestPreviewListenerPinnedChain drives the pinned-mode flow through the full
// chain: POST the pin (authed), then a label-less bare-origin request routes to
// the pinned target with the rewritten Host.
func TestPreviewListenerPinnedChain(t *testing.T) {
	const secret = "integration-secret"
	var gotHost string
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHost = r.Host
		w.Write([]byte("pinned"))
	}))
	defer backend.Close()
	bu, _ := url.Parse(backend.URL)
	port, _ := strconv.Atoi(bu.Port())
	if port < 1024 {
		t.Skipf("ephemeral backend port %d < 1024", port)
	}

	sess := &Session{UUID: "11111111-2222-3333-4444-555555555555", PreviewPort: 65000}
	fixed := &url.URL{Scheme: "http", Host: fmt.Sprintf("localhost:%d", sess.PreviewPort)}
	h := buildPreviewListenerHandler(t, sess, secret, fixed)

	// POST the pin (authed).
	body := strings.NewReader(fmt.Sprintf(`{"name":"app1","port":%d}`, port))
	req := httptest.NewRequest("POST", "/__agent-reverse-proxy-debug__/vhost-pin", body)
	req.Host = "myhost:23000"
	req.AddCookie(authedCookie(secret))
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("POST pin: code %d, want 200 (body %q)", rr.Code, rr.Body.String())
	}

	// Label-less bare-origin request now routes to the pinned target.
	req = httptest.NewRequest("GET", "/", nil)
	req.Host = "myhost:23000"
	req.AddCookie(authedCookie(secret))
	rr = httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if body := rr.Body.String(); body != "pinned" {
		t.Errorf("body = %q, want pinned", body)
	}
	if want := fmt.Sprintf("app1.lvh.me:%d", port); gotHost != want {
		t.Errorf("upstream Host = %q, want %q", gotHost, want)
	}
}
