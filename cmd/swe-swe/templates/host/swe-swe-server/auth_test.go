package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestAuthSignAndVerifyCookie(t *testing.T) {
	secret := "test-secret-123"
	cookie := authSignCookie(secret)
	if !authVerifyCookie(cookie, secret) {
		t.Error("authVerifyCookie should return true for a freshly signed cookie")
	}
}

func TestAuthVerifyCookieWrongSecret(t *testing.T) {
	cookie := authSignCookie("secret-a")
	if authVerifyCookie(cookie, "secret-b") {
		t.Error("authVerifyCookie should return false for wrong secret")
	}
}

func TestAuthVerifyCookieExpired(t *testing.T) {
	// Create a cookie with a timestamp in the past (beyond 7 days)
	secret := "test-secret"
	pastTimestamp := time.Now().Unix() - int64(authCookieMaxAge) - 100
	tsStr := fmt.Sprintf("%d", pastTimestamp)
	signature := authComputeHMAC(tsStr, secret)
	expiredCookie := tsStr + authCookieDelimiter + signature

	if authVerifyCookie(expiredCookie, secret) {
		t.Error("authVerifyCookie should return false for expired cookie")
	}
}

func TestAuthVerifyCookieEmpty(t *testing.T) {
	if authVerifyCookie("", "secret") {
		t.Error("authVerifyCookie should return false for empty cookie")
	}
}

func TestAuthVerifyCookieMalformed(t *testing.T) {
	if authVerifyCookie("no-delimiter", "secret") {
		t.Error("authVerifyCookie should return false for malformed cookie")
	}
}

func TestResolveCookieSecure(t *testing.T) {
	cases := []struct {
		name          string
		xfp           string
		envVar        string
		wantSecure    bool
	}{
		{"proxy says https", "https", "", true},
		{"proxy says http", "http", "", false},
		{"proxy header wins over env=true (tailnet bypass of TLS proxy)", "http", "true", false},
		{"proxy header wins over env=false", "https", "false", true},
		{"no proxy, env=true", "", "true", true},
		{"no proxy, env=false", "", "false", false},
		{"no proxy, no env -- default insecure", "", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("SWE_COOKIE_SECURE", tc.envVar)
			req := httptest.NewRequest("POST", "/swe-swe-auth/login", nil)
			if tc.xfp != "" {
				req.Header.Set("X-Forwarded-Proto", tc.xfp)
			}
			if got := resolveCookieSecure(req); got != tc.wantSecure {
				t.Errorf("resolveCookieSecure: got %v, want %v", got, tc.wantSecure)
			}
		})
	}
}

// TestResolveCookieSecureIgnoresSourceIP guards against a future change
// that gates Secure on RemoteAddr / X-Forwarded-For. Tunnel mode forwards
// browser TLS through a localhost tunnel client, so the source IP at
// swe-swe-server is always 127.0.0.1 even for genuine HTTPS traffic.
// Trust X-Forwarded-Proto, not the connection peer.
func TestResolveCookieSecureIgnoresSourceIP(t *testing.T) {
	cases := []struct {
		name       string
		remoteAddr string
		xff        string
	}{
		{"localhost peer (tunnel-mode pattern)", "127.0.0.1:54321", ""},
		{"localhost peer with localhost xff", "127.0.0.1:54321", "127.0.0.1"},
		{"public peer", "203.0.113.42:443", ""},
		{"private peer with public xff", "10.0.0.5:443", "203.0.113.42"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest("POST", "/swe-swe-auth/login", nil)
			req.RemoteAddr = tc.remoteAddr
			if tc.xff != "" {
				req.Header.Set("X-Forwarded-For", tc.xff)
			}
			req.Header.Set("X-Forwarded-Proto", "https")
			if got := resolveCookieSecure(req); !got {
				t.Errorf("resolveCookieSecure with X-Forwarded-Proto=https returned %v; secure must depend only on the proto header, not source IP", got)
			}
		})
	}
}

func TestResolveCookieDomain(t *testing.T) {
	cases := []struct {
		name           string
		publicHostname string
		requestHost    string
		want           string
	}{
		// Legacy mode: always host-only regardless of how reached.
		{"empty (legacy mode -> host-only)", "", "tunnel.example.com", ""},
		{"empty (legacy mode, localhost)", "", "localhost:8080", ""},
		// Tunnel mode reached via the apex hostname or a per-port
		// subdomain: pin the cookie to the apex for cross-subdomain auth.
		{"apex host exact", "tunnel.example.com", "tunnel.example.com", "tunnel.example.com"},
		{"per-port subdomain", "tunnel.example.com", "3000.tunnel.example.com", "tunnel.example.com"},
		{"per-port subdomain with port", "tunnel.example.com", "3000.tunnel.example.com:443", "tunnel.example.com"},
		// Tunnel mode but reached locally (--tunnel-local-ports): the
		// browser host does not domain-match the apex, so a host-only
		// cookie is the only one the browser will accept.
		{"localhost with port -> host-only", "tunnel.example.com", "localhost:8080", ""},
		{"127.0.0.1 with port -> host-only", "tunnel.example.com", "127.0.0.1:8080", ""},
		{"LAN IP -> host-only", "tunnel.example.com", "192.168.1.50:8080", ""},
		// Guard against naive suffix matching: a host that merely ends
		// with the apex string but is not a subdomain must not match.
		{"suffix lookalike -> host-only", "tunnel.example.com", "eviltunnel.example.com", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := resolveCookieDomain(tc.publicHostname, tc.requestHost); got != tc.want {
				t.Errorf("resolveCookieDomain(%q, %q) = %q, want %q", tc.publicHostname, tc.requestHost, got, tc.want)
			}
		})
	}
}

// TestAuthLoginPostHandlerSetsCookieDomain verifies the Set-Cookie header
// from a successful login response carries the right Domain attribute:
// host-only in legacy mode (no live tunnel), the assigned hostname in
// tunnel mode. This is the load-bearing claim for cross-subdomain cookie
// sharing -- a browser at https://3000.{hostname} only sends the auth
// cookie if the Set-Cookie response on https://{root}.{hostname}
// included the matching Domain attribute.
func TestAuthLoginPostHandlerSetsCookieDomain(t *testing.T) {
	// Snapshot + restore the live tunnel hostname so subtests don't
	// leak into each other (or into the rest of the suite).
	saved := getLiveTunnelHostname()
	t.Cleanup(func() { setLiveTunnelHostname(saved) })

	cases := []struct {
		name        string
		hostname    string
		requestHost string
		wantDomain  string
	}{
		{"legacy mode -> no Domain attr", "", "example.com", ""},
		{"tunnel mode via apex -> Domain matches publicHostname", "fake-tunnel.example.com", "3000.fake-tunnel.example.com", "fake-tunnel.example.com"},
		// --tunnel-local-ports: tunnel is live but the browser logs in
		// over localhost. The cookie must be host-only or the browser
		// rejects it (the regression this guards against).
		{"tunnel live but localhost login -> host-only", "fake-tunnel.example.com", "localhost:8080", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			setLiveTunnelHostname(tc.hostname)

			form := strings.NewReader("password=test-secret")
			req := httptest.NewRequest("POST", "/swe-swe-auth/login", form)
			req.Host = tc.requestHost
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			// Use a unique RemoteAddr so the rate limiter does not reject
			// successive subtests.
			req.RemoteAddr = "127.0.0.1:" + tc.name
			rec := httptest.NewRecorder()

			authLoginPostHandler(rec, req, "test-secret")

			if rec.Code != http.StatusFound {
				t.Fatalf("status = %d, want 302; body=%q", rec.Code, rec.Body.String())
			}

			cookies := rec.Result().Cookies()
			var auth *http.Cookie
			for _, c := range cookies {
				if c.Name == authCookieName {
					auth = c
					break
				}
			}
			if auth == nil {
				t.Fatalf("auth cookie %q not in Set-Cookie; got %d cookies", authCookieName, len(cookies))
			}
			if auth.Domain != tc.wantDomain {
				t.Errorf("auth cookie Domain = %q, want %q", auth.Domain, tc.wantDomain)
			}
		})
	}
}

func TestAuthMiddlewareRedirectsUnauthenticated(t *testing.T) {
	secret := "test-password"
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("protected"))
	})

	handler := authMiddleware(inner, secret)

	req := httptest.NewRequest("GET", "/some/page", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusFound {
		t.Errorf("expected 302, got %d", rr.Code)
	}
	location := rr.Header().Get("Location")
	if !strings.Contains(location, "/swe-swe-auth/login") {
		t.Errorf("expected redirect to login, got %s", location)
	}
}

func TestAuthMiddlewarePassesAuthenticated(t *testing.T) {
	secret := "test-password"
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("protected"))
	})

	handler := authMiddleware(inner, secret)

	req := httptest.NewRequest("GET", "/some/page", nil)
	req.AddCookie(&http.Cookie{
		Name:  authCookieName,
		Value: authSignCookie(secret),
	})
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rr.Code)
	}
	if rr.Body.String() != "protected" {
		t.Errorf("expected 'protected', got %q", rr.Body.String())
	}
}

func TestRequireAuthCookieRejectsUnauthenticated(t *testing.T) {
	secret := "test-password"
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("protected"))
	})
	handler := requireAuthCookie(secret, inner)

	req := httptest.NewRequest("GET", "/some/page", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rr.Code)
	}
}

func TestRequireAuthCookieAllowsAuthenticated(t *testing.T) {
	secret := "test-password"
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("protected"))
	})
	handler := requireAuthCookie(secret, inner)

	req := httptest.NewRequest("GET", "/some/page", nil)
	req.AddCookie(&http.Cookie{Name: authCookieName, Value: authSignCookie(secret)})
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rr.Code)
	}
	if rr.Body.String() != "protected" {
		t.Errorf("expected 'protected', got %q", rr.Body.String())
	}
}

func TestRequireAuthCookieProbeBypass(t *testing.T) {
	secret := "test-password"
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Agent-Reverse-Proxy", "1")
		w.WriteHeader(http.StatusOK)
	})
	handler := requireAuthCookie(secret, inner)

	req := httptest.NewRequest("GET", "/__probe__", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected 200 (probe is exempt), got %d", rr.Code)
	}
	if rr.Header().Get("X-Agent-Reverse-Proxy") == "" {
		t.Errorf("inner handler not reached -- probe not exempt")
	}
}

func TestRequireAuthCookieEmptySecretIsNoop(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("public"))
	})
	handler := requireAuthCookie("", inner)

	req := httptest.NewRequest("GET", "/some/page", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected 200 (no-op when secret empty), got %d", rr.Code)
	}
}

func TestAuthMiddlewareExemptPaths(t *testing.T) {
	secret := "test-password"
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	handler := authMiddleware(inner, secret)

	exemptPaths := []string{
		"/swe-swe-auth/login",
		"/ssl/ca.crt",
		"/mcp",
		"/api/session/some-uuid/browser/start",
		"/api/autocomplete/some-uuid",
	}

	for _, path := range exemptPaths {
		req := httptest.NewRequest("GET", path, nil)
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)

		if rr.Code != http.StatusOK {
			t.Errorf("path %s should be exempt from auth, got %d", path, rr.Code)
		}
	}
}

func TestProxyOpenControlPath(t *testing.T) {
	cases := []struct {
		path     string
		wantUUID string
		wantOK   bool
	}{
		{"/proxy/abc-123/preview/__agent-reverse-proxy-debug__/open", "abc-123", true},
		{"/proxy//preview/__agent-reverse-proxy-debug__/open", "", false},
		{"/proxy/abc/123/preview/__agent-reverse-proxy-debug__/open", "", false},
		{"/proxy/abc-123/preview/", "", false},
		{"/proxy/abc-123/preview/__agent-reverse-proxy-debug__/open/extra", "", false},
		{"/api/autocomplete/abc-123", "", false},
	}
	for _, c := range cases {
		uuid, ok := proxyOpenControlPath(c.path)
		if ok != c.wantOK || uuid != c.wantUUID {
			t.Errorf("proxyOpenControlPath(%q) = (%q, %v), want (%q, %v)", c.path, uuid, ok, c.wantUUID, c.wantOK)
		}
	}
}

func TestAuthMiddlewareOpenControlKeyAuth(t *testing.T) {
	secret := "test-password"
	reached := false
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reached = true
		w.WriteHeader(http.StatusOK)
	})
	handler := authMiddleware(inner, secret)

	uuid := "open-ctl-uuid"
	key := issueSessionKey(uuid)
	otherKey := issueSessionKey("some-other-uuid")
	base := "/proxy/" + uuid + "/preview/__agent-reverse-proxy-debug__/open?url=https%3A%2F%2Fexample.com"

	// No key -> unauthorized, no cookie redirect, inner never reached.
	reached = false
	req := httptest.NewRequest("GET", base, nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusUnauthorized {
		t.Errorf("no key: expected 401, got %d", rr.Code)
	}
	if reached {
		t.Error("no key: inner handler should not be reached")
	}

	// Valid key scoped to this session -> passes through.
	reached = false
	req = httptest.NewRequest("GET", base+"&key="+key, nil)
	rr = httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK || !reached {
		t.Errorf("valid key: expected 200 + inner reached, got %d reached=%v", rr.Code, reached)
	}

	// Key belonging to a different session -> unauthorized.
	reached = false
	req = httptest.NewRequest("GET", base+"&key="+otherKey, nil)
	rr = httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusUnauthorized {
		t.Errorf("cross-session key: expected 401, got %d", rr.Code)
	}
	if reached {
		t.Error("cross-session key: inner handler should not be reached")
	}
}

func TestAuthLoginPostWrongPassword(t *testing.T) {
	secret := "correct-password"
	form := url.Values{}
	form.Set("password", "wrong-password")

	req := httptest.NewRequest("POST", "/swe-swe-auth/login", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rr := httptest.NewRecorder()

	authLoginPostHandler(rr, req, secret)

	if rr.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rr.Code)
	}
	if !strings.Contains(rr.Body.String(), "Invalid password") {
		t.Error("expected 'Invalid password' in response body")
	}
}

func TestAuthLoginPostCorrectPassword(t *testing.T) {
	secret := "correct-password"
	form := url.Values{}
	form.Set("password", "correct-password")

	req := httptest.NewRequest("POST", "/swe-swe-auth/login", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rr := httptest.NewRecorder()

	authLoginPostHandler(rr, req, secret)

	if rr.Code != http.StatusFound {
		t.Errorf("expected 302, got %d", rr.Code)
	}
	cookies := rr.Result().Cookies()
	found := false
	for _, c := range cookies {
		if c.Name == authCookieName {
			found = true
			if !authVerifyCookie(c.Value, secret) {
				t.Error("cookie should be valid")
			}
		}
	}
	if !found {
		t.Error("expected auth cookie to be set")
	}
}
