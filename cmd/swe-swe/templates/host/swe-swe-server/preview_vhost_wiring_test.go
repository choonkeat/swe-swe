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

// newPreviewProxyForTest builds a port-based preview proxy wired with the real
// vhost hooks against the given session, mirroring the production wiring.
func newPreviewProxyForTest(t *testing.T, sess *Session) *agentproxy.Proxy {
	t.Helper()
	p, err := agentproxy.New(agentproxy.Config{
		Target:     &url.URL{Scheme: "http", Host: fmt.Sprintf("localhost:%d", sess.PreviewPort)},
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
	return p
}

// TestPreviewProxyResolveTargetWiring proves the wired ResolveTarget hook makes
// a browser-facing "app1-{port}.<reach>" request reach the loopback backend on
// {port} with the upstream Host rewritten to the logical vhost.
func TestPreviewProxyResolveTargetWiring(t *testing.T) {
	var gotHost string
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHost = r.Host
		http.SetCookie(w, &http.Cookie{Name: "sid", Value: "1", Domain: ".lvh.me", Path: "/"})
		w.Write([]byte("backend"))
	}))
	defer backend.Close()
	bu, _ := url.Parse(backend.URL)
	port, _ := strconv.Atoi(bu.Port())
	if port < 1024 {
		t.Skipf("ephemeral backend port %d < 1024, cannot encode as label", port)
	}

	sess := &Session{PreviewPort: 65000} // fixed target, unused when hook resolves
	proxy := newPreviewProxyForTest(t, sess)

	req := httptest.NewRequest("GET", "/", nil)
	req.Host = fmt.Sprintf("app1-%d.x.sslip.io:23000", port)
	rr := httptest.NewRecorder()
	proxy.ServeHTTP(rr, req)

	if body := rr.Body.String(); body != "backend" {
		t.Errorf("body = %q, want backend (request should reach loopback :%d)", body, port)
	}
	wantHost := fmt.Sprintf("app1.lvh.me:%d", port)
	if gotHost != wantHost {
		t.Errorf("upstream Host = %q, want %q", gotHost, wantHost)
	}

	// Cookie Domain rewritten logical(.lvh.me) -> reach(.x.sslip.io). Go's
	// stdlib drops the leading dot on the wire.
	var sidHdr string
	for _, h := range rr.Result().Header["Set-Cookie"] {
		if strings.HasPrefix(h, "sid=") {
			sidHdr = h
		}
	}
	if !strings.Contains(sidHdr, "Domain=x.sslip.io") {
		t.Errorf("sid Set-Cookie = %q, want Domain=x.sslip.io", sidHdr)
	}
}

// TestPreviewProxyLegacyFallback proves that a label-less/localhost request with
// no pin falls back to the fixed target with the clobbered Host (nothing breaks).
func TestPreviewProxyLegacyFallback(t *testing.T) {
	var gotHost string
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHost = r.Host
		w.Write([]byte("legacy"))
	}))
	defer backend.Close()
	bu, _ := url.Parse(backend.URL)

	// Fixed target is the backend; single-label Host => hook returns ok=false.
	sess := &Session{PreviewPort: 65000}
	p, err := agentproxy.New(agentproxy.Config{
		Target:     bu,
		ToolPrefix: "preview",
		NoInject:   true,
		ResolveTarget: func(inboundHost string) (*url.URL, string, bool) {
			return previewResolveTarget(inboundHost, sess)
		},
		CookieDomainRewrite: previewCookieDomainRewrite,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	req := httptest.NewRequest("GET", "/", nil)
	req.Host = "localhost:23000" // single label -> legacy
	rr := httptest.NewRecorder()
	p.ServeHTTP(rr, req)

	if gotHost != bu.Host {
		t.Errorf("upstream Host = %q, want %q (clobbered to fixed target)", gotHost, bu.Host)
	}
}
