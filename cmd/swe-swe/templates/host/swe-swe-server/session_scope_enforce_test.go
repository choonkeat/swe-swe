package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestScopedRequestAllowed(t *testing.T) {
	const scope = "sess-1"
	cases := []struct {
		path string
		want bool
	}{
		// Own session: allowed.
		{"/session/sess-1", true},
		{"/session/sess-1?assistant=claude", true}, // query is ignored by path match
		{"/ws/sess-1", true},
		{"/proxy/sess-1/preview/", true},
		{"/proxy/sess-1/agentchat/foo", true},
		{"/api/session/sess-1/end", true},
		{"/api/session/sess-1/vnc-ready", true},
		{"/api/session/sess-1/files-ready", true},
		// Another session: denied.
		{"/session/sess-2", false},
		{"/ws/sess-2", false},
		{"/proxy/sess-2/preview/", false},
		{"/api/session/sess-2/end", false},
		// Spawn / fork / enumerate: denied.
		{"/api/session/new", false},
		{"/api/fork/sess-1", false},
		{"/api/worktrees", false},
		{"/api/worktree/check", false},
		{"/api/repos", false},
		{"/api/repo/prepare", false},
		{"/api/repo/branches", false},
		// Server shutdown: never.
		{"/api/server/shutdown", false},
		// Recordings: never.
		{"/recording/anything", false},
		{"/recording/sess-1", false}, // even a same-name recording UUID is out
		{"/api/recording/foo", false},
		// UUID-less assets/plumbing: allowed.
		{"/static/app.js", true},
		{"/terminal-ui.js", true},
		{"/favicon.ico", true},
		{"/ssl/ca.crt", true},
		{"/swe-swe-auth/login", true},
	}
	for _, c := range cases {
		req := httptest.NewRequest("GET", c.path, nil)
		if got := scopedRequestAllowed(scope, req); got != c.want {
			t.Errorf("scopedRequestAllowed(%q, %q) = %v, want %v", scope, c.path, got, c.want)
		}
	}
}

// End-to-end through authMiddleware: a scoped guest is boxed into one session.
func TestAuthMiddlewareScopedGuest(t *testing.T) {
	const secret = "master"
	const scope = "guest-1"
	// A live session for the guest to be boxed into (homepage redirect needs it).
	registerTestSession(t, scope, &Session{Assistant: "claude"})

	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("inner"))
	})
	handler := authMiddleware(inner, secret)
	guestCookie := &http.Cookie{Name: authCookieName, Value: authSignScopedCookie(secret, scope)}

	req := func(path string) *http.Request {
		r := httptest.NewRequest("GET", path, nil)
		r.AddCookie(guestCookie)
		return r
	}

	// Own session -> reaches inner.
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req("/session/guest-1?assistant=claude"))
	if rr.Code != http.StatusOK || rr.Body.String() != "inner" {
		t.Errorf("own session: code=%d body=%q, want 200 inner", rr.Code, rr.Body.String())
	}

	// Another session -> 403.
	rr = httptest.NewRecorder()
	handler.ServeHTTP(rr, req("/session/other-session"))
	if rr.Code != http.StatusForbidden {
		t.Errorf("other session: code=%d, want 403", rr.Code)
	}

	// WebSocket for another session -> 403.
	rr = httptest.NewRecorder()
	handler.ServeHTTP(rr, req("/ws/other-session"))
	if rr.Code != http.StatusForbidden {
		t.Errorf("other ws: code=%d, want 403", rr.Code)
	}

	// Recording -> 403.
	rr = httptest.NewRecorder()
	handler.ServeHTTP(rr, req("/recording/anything"))
	if rr.Code != http.StatusForbidden {
		t.Errorf("recording: code=%d, want 403", rr.Code)
	}

	// Spawn a new session -> 403.
	rr = httptest.NewRecorder()
	handler.ServeHTTP(rr, req("/api/session/new"))
	if rr.Code != http.StatusForbidden {
		t.Errorf("new session: code=%d, want 403", rr.Code)
	}

	// Homepage -> 302 redirect into the guest's own session.
	rr = httptest.NewRecorder()
	handler.ServeHTTP(rr, req("/"))
	if rr.Code != http.StatusFound {
		t.Fatalf("homepage: code=%d, want 302", rr.Code)
	}
	if loc := rr.Header().Get("Location"); !strings.HasPrefix(loc, "/session/guest-1") {
		t.Errorf("homepage redirect Location=%q, want /session/guest-1...", loc)
	}

	// A static asset the guest's page needs -> allowed.
	rr = httptest.NewRecorder()
	handler.ServeHTTP(rr, req("/static/x.js"))
	if rr.Code != http.StatusOK {
		t.Errorf("static asset: code=%d, want 200", rr.Code)
	}
}

// A full (unscoped) user is completely unaffected by the guest policy.
func TestAuthMiddlewareUnscopedUserUnaffected(t *testing.T) {
	const secret = "master"
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("inner"))
	})
	handler := authMiddleware(inner, secret)
	full := &http.Cookie{Name: authCookieName, Value: authSignCookie(secret)}

	for _, path := range []string{"/", "/session/any-session", "/recording/x", "/api/session/new", "/api/repos"} {
		r := httptest.NewRequest("GET", path, nil)
		r.AddCookie(full)
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, r)
		if rr.Code != http.StatusOK {
			t.Errorf("unscoped user path %q: code=%d, want 200 (unaffected)", path, rr.Code)
		}
	}
}

// A guest returning after the session ended gets 410 on the homepage, not an
// infinite redirect loop.
func TestAuthMiddlewareScopedGuestSessionEnded(t *testing.T) {
	const secret = "master"
	const scope = "ended-session" // deliberately NOT registered -> "gone"
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	handler := authMiddleware(inner, secret)

	r := httptest.NewRequest("GET", "/", nil)
	r.AddCookie(&http.Cookie{Name: authCookieName, Value: authSignScopedCookie(secret, scope)})
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, r)
	if rr.Code != http.StatusGone {
		t.Errorf("ended-session homepage: code=%d, want 410", rr.Code)
	}
}

func TestScopedVerifyAllowed(t *testing.T) {
	const scope = "sess-1"
	cases := []struct {
		path string
		want bool
	}{
		// Traefik dashboard (direct backend behind ForwardAuth) -> denied.
		{"/dashboard", false},
		{"/dashboard/", false},
		{"/api/http/routers", false},
		{"/api/tcp/services", false},
		{"/api/entrypoints", false},
		{"/api/overview", false},
		// Homepage + own session + assets -> allowed (swe-swe-server boxes "/"").
		{"/", true},
		{"/session/sess-1", true},
		{"/ws/sess-1", true},
		{"/static/app.js", true},
		{"/terminal-ui.js", true},
		{"/api/autocomplete/somekey", true}, // guest session page needs it
		// Other session / recordings / spawn / repos -> denied.
		{"/session/sess-2", false},
		{"/recording/x", false},
		{"/api/session/new", false},
		{"/api/repos", false},
	}
	for _, c := range cases {
		if got := scopedVerifyAllowed(scope, c.path); got != c.want {
			t.Errorf("scopedVerifyAllowed(%q, %q) = %v, want %v", scope, c.path, got, c.want)
		}
	}
}

// The Traefik ForwardAuth gate (/verify) boxes a guest too: valid cookie but a
// forbidden X-Forwarded-Uri -> 403; a full user is unaffected.
func TestAuthVerifyHandlerScoped(t *testing.T) {
	const secret = "master"
	const scope = "vsess"
	h := authVerifyHandler(secret)

	verify := func(cookieVal, forwardedURI string) int {
		r := httptest.NewRequest("GET", "/swe-swe-auth/verify", nil)
		r.Header.Set("X-Forwarded-Uri", forwardedURI)
		if cookieVal != "" {
			r.AddCookie(&http.Cookie{Name: authCookieName, Value: cookieVal})
		}
		rr := httptest.NewRecorder()
		h(rr, r)
		return rr.Code
	}

	guest := authSignScopedCookie(secret, scope)
	full := authSignCookie(secret)

	// Guest: own session + homepage OK; dashboard + other session forbidden.
	if c := verify(guest, "/session/vsess?assistant=x"); c != http.StatusOK {
		t.Errorf("guest own session: %d, want 200", c)
	}
	if c := verify(guest, "/"); c != http.StatusOK {
		t.Errorf("guest homepage: %d, want 200", c)
	}
	if c := verify(guest, "/dashboard"); c != http.StatusForbidden {
		t.Errorf("guest dashboard: %d, want 403", c)
	}
	if c := verify(guest, "/api/http/routers"); c != http.StatusForbidden {
		t.Errorf("guest traefik api: %d, want 403", c)
	}
	if c := verify(guest, "/session/other"); c != http.StatusForbidden {
		t.Errorf("guest other session: %d, want 403", c)
	}

	// Full user: everything the cookie is valid for passes, including the
	// dashboard (unaffected by the guest policy).
	if c := verify(full, "/dashboard"); c != http.StatusOK {
		t.Errorf("full user dashboard: %d, want 200", c)
	}
	if c := verify(full, "/session/anything"); c != http.StatusOK {
		t.Errorf("full user session: %d, want 200", c)
	}

	// No cookie -> 302 redirect to login (unchanged).
	if c := verify("", "/dashboard"); c != http.StatusFound {
		t.Errorf("no cookie: %d, want 302", c)
	}
}

// The per-port proxy guard's plumbing: it exempts /__probe__, 401s a
// missing/invalid cookie, and defers the scope decision to the supplied
// authorizer. (Port-ownership semantics are covered by
// TestRequireAuthCookiePortOwnership.)
func TestRequireAuthCookieScoped(t *testing.T) {
	const secret = "master"
	const owning = "owning-session"
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	// Authorizer standing in for "this port belongs to `owning`": a full user
	// (empty scope) or a guest scoped to `owning` passes; any other scope fails.
	handler := requireAuthCookie(secret, func(scope string) bool {
		return scope == "" || scope == owning
	}, inner)

	// Scoped cookie for THIS port's session -> allowed.
	r := httptest.NewRequest("GET", "/foo", nil)
	r.AddCookie(&http.Cookie{Name: authCookieName, Value: authSignScopedCookie(secret, owning)})
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, r)
	if rr.Code != http.StatusOK {
		t.Errorf("own-session scoped cookie: code=%d, want 200", rr.Code)
	}

	// Scoped cookie for a DIFFERENT session -> 401 (cannot reach another
	// session's preview/vnc/files port).
	r = httptest.NewRequest("GET", "/foo", nil)
	r.AddCookie(&http.Cookie{Name: authCookieName, Value: authSignScopedCookie(secret, "some-other-session")})
	rr = httptest.NewRecorder()
	handler.ServeHTTP(rr, r)
	if rr.Code != http.StatusUnauthorized {
		t.Errorf("cross-session scoped cookie: code=%d, want 401", rr.Code)
	}

	// Full (unscoped) user -> allowed on any port.
	r = httptest.NewRequest("GET", "/foo", nil)
	r.AddCookie(&http.Cookie{Name: authCookieName, Value: authSignCookie(secret)})
	rr = httptest.NewRecorder()
	handler.ServeHTTP(rr, r)
	if rr.Code != http.StatusOK {
		t.Errorf("unscoped user: code=%d, want 200", rr.Code)
	}
}
