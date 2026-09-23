package main

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// TestProxyRouteAllowsSameOriginFraming -- every /proxy/{uuid}/... response is
// shown inside a pane iframe on the same origin, so it must say so itself.
//
// A front proxy that adds "X-Frame-Options: deny" to any response lacking one
// (seen live: a Cloudflare Access gateway) otherwise blanks every pane in
// single-port mode, where each pane is served from this path form. Sending our
// own SAMEORIGIN keeps that gateway from adding its deny, and frame-ancestors
// 'self' wins even if it adds one anyway: browsers ignore X-Frame-Options
// when a CSP frame-ancestors directive is present.
func TestProxyRouteAllowsSameOriginFraming(t *testing.T) {
	const uuid = "frame-hdr-sess"

	// Stand-in for agent-chat: an upstream that forbids framing and carries
	// its own CSP, which must survive alongside ours.
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Content-Security-Policy", "img-src 'self'")
		w.Write([]byte("chat"))
	}))
	defer upstream.Close()
	target, _ := url.Parse(upstream.URL)

	sess := &Session{Assistant: "claude"}
	registerTestSession(t, uuid, sess)
	sessMux := http.NewServeMux()
	sessMux.Handle("/proxy/"+uuid+"/agentchat/", http.StripPrefix(
		"/proxy/"+uuid+"/agentchat", agentChatProxyHandler(target)))
	sess.SessionMux = sessMux

	srv := httptest.NewServer(http.HandlerFunc(handleProxyRoute))
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/proxy/" + uuid + "/agentchat/")
	if err != nil {
		t.Fatalf("GET agentchat: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET agentchat: got %d, want 200", resp.StatusCode)
	}

	if got := resp.Header.Values("X-Frame-Options"); len(got) != 1 || got[0] != "SAMEORIGIN" {
		t.Errorf("X-Frame-Options = %q, want exactly [SAMEORIGIN]", got)
	}
	csp := strings.Join(resp.Header.Values("Content-Security-Policy"), " | ")
	if !strings.Contains(csp, "frame-ancestors 'self'") {
		t.Errorf("Content-Security-Policy = %q, want it to include frame-ancestors 'self'", csp)
	}
	if !strings.Contains(csp, "img-src 'self'") {
		t.Errorf("Content-Security-Policy = %q, upstream's own policy was dropped", csp)
	}
}
