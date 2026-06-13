package main

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func httptestFormBody(s string) io.Reader { return strings.NewReader(s) }

func uniqAddr(i int) string { return fmt.Sprintf("198.18.%d.%d:%d", (i/250)%250, i%250, 10000+i) }

func uniqIP(i int) string { return fmt.Sprintf("100.64.%d.%d", (i/250)%250, i%250) }

// --- #1: login throttle key must not trust X-Forwarded-For by default ---

func TestLoginThrottleKeyDefaultUsesPeer(t *testing.T) {
	cases := []struct {
		name       string
		remoteAddr string
		xff        string
		want       string
	}{
		{"no xff -> peer host", "203.0.113.7:5555", "", "203.0.113.7"},
		{"spoofed xff ignored by default", "203.0.113.7:5555", "10.9.9.9", "203.0.113.7"},
		{"multi xff ignored by default", "203.0.113.7:5555", "10.9.9.9, 8.8.8.8", "203.0.113.7"},
		{"remoteaddr without port", "203.0.113.7", "", "203.0.113.7"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest("POST", "/swe-swe-auth/login", nil)
			req.RemoteAddr = tc.remoteAddr
			if tc.xff != "" {
				req.Header.Set("X-Forwarded-For", tc.xff)
			}
			if got := loginThrottleKey(req); got != tc.want {
				t.Errorf("loginThrottleKey = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestLoginThrottleKeyTrustsForwardedForWhenConfigured(t *testing.T) {
	t.Setenv("SWE_TRUST_FORWARDED_FOR", "true")
	cases := []struct {
		name string
		xff  string
		want string
	}{
		{"single", "198.51.100.4", "198.51.100.4"},
		{"multi -> first", "198.51.100.4, 10.0.0.1", "198.51.100.4"},
		{"spaces trimmed", "  198.51.100.4 , 10.0.0.1", "198.51.100.4"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest("POST", "/swe-swe-auth/login", nil)
			req.RemoteAddr = "127.0.0.1:1"
			req.Header.Set("X-Forwarded-For", tc.xff)
			if got := loginThrottleKey(req); got != tc.want {
				t.Errorf("loginThrottleKey = %q, want %q", got, tc.want)
			}
		})
	}
}

// --- #1b: global failed-attempt ceiling, independent of per-key bucket ---

func TestAuthGlobalRateLimiter(t *testing.T) {
	g := &authGlobalRateLimiter{}
	const max = 5
	for i := 0; i < max; i++ {
		if !g.allow(max) {
			t.Fatalf("attempt %d should be allowed (under ceiling)", i)
		}
		g.record()
	}
	if g.allow(max) {
		t.Errorf("attempt past the ceiling must be blocked even with a fresh per-key bucket")
	}
}

// The whole point of #1: an attacker who rotates X-Forwarded-For (so every
// request lands in its own per-IP bucket) must still be stopped by the
// global ceiling.
func TestLoginGlobalCeilingStopsSpoofedXFFBrute(t *testing.T) {
	// Isolate global state for this test.
	saved := authGlobalLimiter
	authGlobalLimiter = &authGlobalRateLimiter{}
	t.Cleanup(func() { authGlobalLimiter = saved })

	secret := "correct-horse"
	blocked := false
	for i := 0; i < authGlobalRateLimitMax+5; i++ {
		req := httptest.NewRequest("POST", "/swe-swe-auth/login",
			httptestFormBody("password=wrong"))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		// Unique spoofed XFF + unique peer per request: defeats any
		// per-key limiter, so only the global ceiling can stop this.
		req.RemoteAddr = uniqAddr(i)
		req.Header.Set("X-Forwarded-For", uniqIP(i))
		rec := httptest.NewRecorder()
		authLoginPostHandler(rec, req, secret)
		if rec.Code == http.StatusTooManyRequests {
			blocked = true
			break
		}
	}
	if !blocked {
		t.Errorf("brute force with rotating X-Forwarded-For was never rate limited (global ceiling missing/ineffective)")
	}
}

// --- #2: WebSocket origin allow-list ---

func TestCheckWebSocketOrigin(t *testing.T) {
	saved := getLiveTunnelHostname()
	t.Cleanup(func() { setLiveTunnelHostname(saved) })
	setLiveTunnelHostname("tunnel.example.com")

	cases := []struct {
		name        string
		origin      string
		requestHost string
		want        bool
	}{
		{"no origin header (non-browser client)", "", "tunnel.example.com", true},
		{"same host as request", "https://app.local:1977", "app.local:1977", true},
		{"tunnel apex", "https://tunnel.example.com", "tunnel.example.com", true},
		{"per-port subdomain", "https://3000.tunnel.example.com", "tunnel.example.com", true},
		{"cross-site attacker", "https://evil.example", "tunnel.example.com", false},
		{"suffix lookalike", "https://eviltunnel.example.com", "tunnel.example.com", false},
		{"malformed origin", "://nope", "tunnel.example.com", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/ws/x", nil)
			req.Host = tc.requestHost
			if tc.origin != "" {
				req.Header.Set("Origin", tc.origin)
			}
			if got := checkWebSocketOrigin(req); got != tc.want {
				t.Errorf("checkWebSocketOrigin(origin=%q host=%q) = %v, want %v",
					tc.origin, tc.requestHost, got, tc.want)
			}
		})
	}
}

// checkWebSocketOrigin with no live tunnel must still allow same-host.
func TestCheckWebSocketOriginNoTunnel(t *testing.T) {
	saved := getLiveTunnelHostname()
	t.Cleanup(func() { setLiveTunnelHostname(saved) })
	setLiveTunnelHostname("")

	req := httptest.NewRequest("GET", "/ws/x", nil)
	req.Host = "localhost:1977"
	req.Header.Set("Origin", "http://localhost:1977")
	if !checkWebSocketOrigin(req) {
		t.Error("same-host origin must be allowed even without a live tunnel")
	}

	req2 := httptest.NewRequest("GET", "/ws/x", nil)
	req2.Host = "localhost:1977"
	req2.Header.Set("Origin", "http://evil.example")
	if checkWebSocketOrigin(req2) {
		t.Error("cross-site origin must be rejected without a live tunnel")
	}
}

// --- #3: open-redirect-safe redirect target ---

func TestSafeRedirect(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"", "/"},
		{"/", "/"},
		{"/session/abc?x=1", "/session/abc?x=1"},
		{"/swe-swe-auth/verify", "/swe-swe-auth/verify"},
		{"https://evil.example", "/"},
		{"http://evil.example/x", "/"},
		{"//evil.example", "/"},
		{"/\\evil.example", "/"},
		{"\\\\evil.example", "/"},
		{"javascript:alert(1)", "/"},
		{"evil.example", "/"},
		{" /sneaky", "/"},
	}
	for _, tc := range cases {
		if got := safeRedirect(tc.in); got != tc.want {
			t.Errorf("safeRedirect(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// A successful login with an off-site redirect target must land on "/",
// never bounce the freshly-authenticated user to an attacker origin.
func TestLoginRejectsOffsiteRedirect(t *testing.T) {
	secret := "open-sesame"
	req := httptest.NewRequest("POST", "/swe-swe-auth/login",
		httptestFormBody("password=open-sesame&redirect=https://evil.example"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.RemoteAddr = "203.0.113.55:9000"
	rec := httptest.NewRecorder()

	authLoginPostHandler(rec, req, secret)

	if rec.Code != http.StatusFound {
		t.Fatalf("status = %d, want 302; body=%q", rec.Code, rec.Body.String())
	}
	if loc := rec.Header().Get("Location"); loc != "/" {
		t.Errorf("Location = %q, want %q (off-site redirect must be rejected)", loc, "/")
	}
}

func TestLoginKeepsLocalRedirect(t *testing.T) {
	secret := "open-sesame"
	req := httptest.NewRequest("POST", "/swe-swe-auth/login",
		httptestFormBody("password=open-sesame&redirect=/session/abc"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.RemoteAddr = "203.0.113.56:9000"
	rec := httptest.NewRecorder()

	authLoginPostHandler(rec, req, secret)

	if rec.Code != http.StatusFound {
		t.Fatalf("status = %d, want 302; body=%q", rec.Code, rec.Body.String())
	}
	if loc := rec.Header().Get("Location"); loc != "/session/abc" {
		t.Errorf("Location = %q, want %q (local redirect must be preserved)", loc, "/session/abc")
	}
}
