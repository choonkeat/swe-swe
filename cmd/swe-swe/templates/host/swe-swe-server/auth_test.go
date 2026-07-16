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

// --- Scoped cookie (share-a-session) primitive ---

// A scoped cookie round-trips: signing with a scope and verifying returns
// that same scope and validates.
func TestAuthSignScopedCookieRoundTrip(t *testing.T) {
	secret := "master-secret"
	scope := "session-uuid-abc"
	cookie := authSignScopedCookie(secret, scope)
	gotScope, ok := authVerifyCookieScoped(cookie, secret)
	if !ok {
		t.Fatalf("scoped cookie should verify; got valid=false")
	}
	if gotScope != scope {
		t.Errorf("scope = %q, want %q", gotScope, scope)
	}
}

// A legacy 2-part cookie (no scope) still verifies and reports an empty
// scope -- backward compatibility for every full-user cookie already issued.
func TestAuthVerifyCookieScopedLegacyUnscoped(t *testing.T) {
	secret := "master-secret"
	legacy := authSignCookie(secret) // unchanged 2-part format
	gotScope, ok := authVerifyCookieScoped(legacy, secret)
	if !ok {
		t.Fatalf("legacy unscoped cookie should verify")
	}
	if gotScope != "" {
		t.Errorf("legacy cookie scope = %q, want empty", gotScope)
	}
	// authSignScopedCookie with empty scope must be byte-identical to the
	// legacy signer so the wire format does not change for full users.
	// (Cannot compare values directly -- timestamps differ -- but the shape
	// must be 2-part.)
	if parts := strings.Split(authSignScopedCookie(secret, ""), authCookieDelimiter); len(parts) != 2 {
		t.Errorf("empty-scope cookie has %d parts, want 2 (legacy shape)", len(parts))
	}
}

// Tampering with the scope segment of a signed cookie must fail verification:
// the guest does not know the master secret, so they cannot re-sign a scope
// they were not granted.
func TestAuthVerifyCookieScopedTamperScope(t *testing.T) {
	secret := "master-secret"
	cookie := authSignScopedCookie(secret, "session-A")
	parts := strings.Split(cookie, authCookieDelimiter)
	if len(parts) != 3 {
		t.Fatalf("scoped cookie should have 3 parts, got %d", len(parts))
	}
	// Swap the scope to another session, keep the original signature.
	forged := parts[0] + authCookieDelimiter + "session-B" + authCookieDelimiter + parts[2]
	if _, ok := authVerifyCookieScoped(forged, secret); ok {
		t.Error("cookie with tampered scope must not verify")
	}
}

// A scoped cookie signed under one secret must not verify under another.
func TestAuthVerifyCookieScopedWrongSecret(t *testing.T) {
	cookie := authSignScopedCookie("secret-a", "session-A")
	if _, ok := authVerifyCookieScoped(cookie, "secret-b"); ok {
		t.Error("scoped cookie must not verify with wrong secret")
	}
}

// Expiry is enforced on scoped cookies exactly as on legacy ones.
func TestAuthVerifyCookieScopedExpired(t *testing.T) {
	secret := "master-secret"
	scope := "session-A"
	pastTS := fmt.Sprintf("%d", time.Now().Unix()-int64(authCookieMaxAge)-100)
	sig := authComputeHMAC(pastTS+authCookieDelimiter+scope, secret)
	expired := pastTS + authCookieDelimiter + scope + authCookieDelimiter + sig
	if _, ok := authVerifyCookieScoped(expired, secret); ok {
		t.Error("expired scoped cookie must not verify")
	}
}

// The plain authVerifyCookie bool wrapper still works for both shapes.
func TestAuthVerifyCookieAcceptsScopedShape(t *testing.T) {
	secret := "master-secret"
	if !authVerifyCookie(authSignScopedCookie(secret, "session-A"), secret) {
		t.Error("authVerifyCookie should accept a valid scoped cookie")
	}
	if !authVerifyCookie(authSignCookie(secret), secret) {
		t.Error("authVerifyCookie should accept a valid legacy cookie")
	}
}

func TestScopeAllows(t *testing.T) {
	cases := []struct {
		scope string
		uuid  string
		want  bool
	}{
		{"", "any-session", true},      // unscoped full user: everything allowed
		{"", "", true},                 // unscoped, no uuid
		{"sess-1", "sess-1", true},     // guest reaching own session
		{"sess-1", "sess-2", false},    // guest reaching another session
		{"sess-1", "", false},          // guest on a uuid-less-but-guarded path
	}
	for _, c := range cases {
		if got := scopeAllows(c.scope, c.uuid); got != c.want {
			t.Errorf("scopeAllows(%q, %q) = %v, want %v", c.scope, c.uuid, got, c.want)
		}
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
		// Non-tunnel wildcard preview: login lands on a reach sub-app
		// origin, so the cookie is pinned to the reach (lvh.me) and thus
		// reaches every {name}-{port}.lvh.me sibling. This is the
		// wildcard-under-password fix.
		{"wildcard preview login -> Domain=reach", "", "app1-3000.lvh.me:23000", "lvh.me"},
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

func TestAuthLogoutHandlerClearsCookieAndRedirects(t *testing.T) {
	saved := getLiveTunnelHostname()
	t.Cleanup(func() { setLiveTunnelHostname(saved) })
	setLiveTunnelHostname("")

	req := httptest.NewRequest("GET", "/swe-swe-auth/logout", nil)
	req.Host = "example.com"
	rec := httptest.NewRecorder()

	authLogoutHandler()(rec, req)

	if rec.Code != http.StatusFound {
		t.Fatalf("status = %d, want 302; body=%q", rec.Code, rec.Body.String())
	}
	if loc := rec.Header().Get("Location"); loc != "/swe-swe-auth/login" {
		t.Errorf("Location = %q, want /swe-swe-auth/login", loc)
	}

	var auth *http.Cookie
	for _, c := range rec.Result().Cookies() {
		if c.Name == authCookieName {
			auth = c
			break
		}
	}
	if auth == nil {
		t.Fatalf("logout did not emit a %q cookie to clear", authCookieName)
	}
	if auth.Value != "" {
		t.Errorf("cleared cookie Value = %q, want empty", auth.Value)
	}
	// MaxAge < 0 tells net/http to emit Max-Age=0, expiring the cookie.
	if auth.MaxAge >= 0 {
		t.Errorf("cleared cookie MaxAge = %d, want negative (expire now)", auth.MaxAge)
	}
	// Non-reach host -> host-only clear (Domain must match the login cookie).
	if auth.Domain != "" {
		t.Errorf("cleared cookie Domain = %q, want empty for a non-reach host", auth.Domain)
	}
}

// TestAuthLogoutHandlerClearsPreviewReachDomain locks in that logout mirrors the
// login cookie's Domain in wildcard preview: the clearing Set-Cookie must carry
// Domain=reach, else (RFC 6265 matches on name+domain+path) the browser keeps the
// Domain=lvh.me login cookie and the guest is never actually logged out.
func TestAuthLogoutHandlerClearsPreviewReachDomain(t *testing.T) {
	saved := getLiveTunnelHostname()
	t.Cleanup(func() { setLiveTunnelHostname(saved) })
	setLiveTunnelHostname("")

	req := httptest.NewRequest("GET", "/swe-swe-auth/logout", nil)
	req.Host = "app1-3000.lvh.me:23000"
	rec := httptest.NewRecorder()

	authLogoutHandler()(rec, req)

	var auth *http.Cookie
	for _, c := range rec.Result().Cookies() {
		if c.Name == authCookieName {
			auth = c
			break
		}
	}
	if auth == nil {
		t.Fatalf("logout did not emit a %q cookie to clear", authCookieName)
	}
	if auth.Domain != "lvh.me" {
		t.Errorf("cleared cookie Domain = %q, want lvh.me", auth.Domain)
	}
	if auth.MaxAge >= 0 {
		t.Errorf("cleared cookie MaxAge = %d, want negative (expire now)", auth.MaxAge)
	}
}

// A shared-session guest (scoped cookie) must be able to reach the logout
// endpoint: it is on the exempt list, so authMiddleware never applies the
// guest boxing to it.
func TestAuthMiddlewareExemptsLogout(t *testing.T) {
	secret := "test-password"
	served := false
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		served = true
		w.WriteHeader(http.StatusOK)
	})
	handler := authMiddleware(inner, secret)

	// Scoped guest cookie -- boxed to some other session.
	req := httptest.NewRequest("GET", "/swe-swe-auth/logout", nil)
	req.AddCookie(&http.Cookie{
		Name:  authCookieName,
		Value: authSignScopedCookie(secret, "some-session-uuid"),
	})
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if !served {
		t.Fatalf("logout path was not passed through; status=%d body=%q", rec.Code, rec.Body.String())
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

// scopeIs builds a requireAuthCookie authorizer that admits full/unscoped users
// (scope "") and a guest scoped to owningUUID -- the pre-predicate behavior, for
// tests that only exercise requireAuthCookie's generic auth plumbing.
func scopeIs(owningUUID string) func(string) bool {
	return func(scope string) bool { return scope == "" || scope == owningUUID }
}

func TestRequireAuthCookieRejectsUnauthenticated(t *testing.T) {
	secret := "test-password"
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("protected"))
	})
	handler := requireAuthCookie(secret, scopeIs("owner-sess"), inner)

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
	handler := requireAuthCookie(secret, scopeIs("owner-sess"), inner)

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
	handler := requireAuthCookie(secret, scopeIs("owner-sess"), inner)

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
	handler := requireAuthCookie("", scopeIs("owner-sess"), inner)

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

func TestProxyPreviewMCPPath(t *testing.T) {
	cases := []struct {
		path     string
		wantUUID string
		wantOK   bool
	}{
		{"/proxy/abc-123/preview/mcp", "abc-123", true},
		{"/proxy//preview/mcp", "", false},
		{"/proxy/abc/123/preview/mcp", "", false},
		{"/proxy/abc-123/preview/", "", false},
		{"/proxy/abc-123/preview/mcp/extra", "", false},
		{"/proxy/abc-123/preview/__agent-reverse-proxy-debug__/open", "", false},
		{"/api/autocomplete/abc-123", "", false},
	}
	for _, c := range cases {
		uuid, ok := proxyPreviewMCPPath(c.path)
		if ok != c.wantOK || uuid != c.wantUUID {
			t.Errorf("proxyPreviewMCPPath(%q) = (%q, %v), want (%q, %v)", c.path, uuid, ok, c.wantUUID, c.wantOK)
		}
	}
}

func TestAuthMiddlewarePreviewMCPKeyAuth(t *testing.T) {
	secret := "test-password"
	reached := false
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reached = true
		w.WriteHeader(http.StatusOK)
	})
	handler := authMiddleware(inner, secret)

	uuid := "preview-mcp-uuid"
	key := issueSessionKey(uuid)
	otherKey := issueSessionKey("some-other-uuid")
	base := "/proxy/" + uuid + "/preview/mcp"

	// No key -> unauthorized (not a cookie redirect), inner never reached.
	reached = false
	req := httptest.NewRequest("POST", base, nil)
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
	req = httptest.NewRequest("POST", base+"?key="+key, nil)
	rr = httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK || !reached {
		t.Errorf("valid key: expected 200 + inner reached, got %d reached=%v", rr.Code, reached)
	}

	// Key belonging to a different session -> unauthorized.
	reached = false
	req = httptest.NewRequest("POST", base+"?key="+otherKey, nil)
	rr = httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusUnauthorized {
		t.Errorf("cross-session key: expected 401, got %d", rr.Code)
	}
	if reached {
		t.Error("cross-session key: inner handler should not be reached")
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
