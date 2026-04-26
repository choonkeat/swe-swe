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
		want           string
	}{
		{"empty (legacy mode -> host-only)", "", ""},
		{"single label", "abc-tunnel.example.com", "abc-tunnel.example.com"},
		{"two labels", "tunnel.example.com", "tunnel.example.com"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := resolveCookieDomain(tc.publicHostname); got != tc.want {
				t.Errorf("resolveCookieDomain(%q) = %q, want %q", tc.publicHostname, got, tc.want)
			}
		})
	}
}

// TestAuthLoginPostHandlerSetsCookieDomain verifies the Set-Cookie header
// from a successful login response carries the right Domain attribute:
// host-only in legacy mode, "." + serverPublicHostname in tunnel mode.
// This is the load-bearing claim for cross-subdomain cookie sharing -- a
// browser at https://3000.{hostname} only sends the auth cookie if the
// Set-Cookie response on https://{root}.{hostname} included the matching
// Domain attribute.
func TestAuthLoginPostHandlerSetsCookieDomain(t *testing.T) {
	// Snapshot + restore the package-level config var so subtests don't
	// leak into each other (or into the rest of the suite).
	saved := serverPublicHostname
	t.Cleanup(func() { serverPublicHostname = saved })

	cases := []struct {
		name        string
		hostname    string
		wantDomain  string
	}{
		{"legacy mode -> no Domain attr", "", ""},
		{"tunnel mode -> Domain matches publicHostname", "fake-tunnel.example.com", "fake-tunnel.example.com"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			serverPublicHostname = tc.hostname

			form := strings.NewReader("password=test-secret")
			req := httptest.NewRequest("POST", "/swe-swe-auth/login", form)
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
