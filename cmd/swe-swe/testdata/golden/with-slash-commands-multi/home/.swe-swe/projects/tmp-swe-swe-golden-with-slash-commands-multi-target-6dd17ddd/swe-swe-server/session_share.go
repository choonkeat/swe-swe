// session_share.go -- shared-session links.
//
// Lets a full user share ONE live session with an external guest via a unique
// link plus a password. The guest logs in with that password and receives a
// SCOPED auth cookie (see auth.go: authSignScopedCookie) that boxes them into
// exactly this session -- they cannot reach the homepage's other sessions,
// create sessions, or open recordings. Enforcement of that boxing lives in the
// auth gate (authMiddleware) and the per-port proxies (requireAuthCookie).
//
// The share password lives on Session.SharePassword (in-memory), so ending the
// session revokes the share. There is no persistence and no separate revoke.
package main

import (
	crypto_rand "crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/url"
	"os"
	"strings"
)

// generateSharePassword returns a fresh high-entropy share password (192 bits,
// 48 hex chars) so brute force is infeasible under the login rate limiter.
func generateSharePassword() string {
	buf := make([]byte, 24)
	crypto_rand.Read(buf)
	return hex.EncodeToString(buf)
}

// enableSessionShare returns sess's share password, generating and storing one
// on first call. Idempotent: repeated calls return the same password so a
// re-shared link keeps working.
func enableSessionShare(sess *Session) string {
	sess.mu.Lock()
	defer sess.mu.Unlock()
	if sess.SharePassword == "" {
		sess.SharePassword = generateSharePassword()
	}
	return sess.SharePassword
}

// validSessionShareLogin reports whether password is the share password of the
// live session identified by scope. False if the session is gone, sharing was
// never enabled, or the password does not match. Constant-time compare.
func validSessionShareLogin(scope, password string) bool {
	if scope == "" || password == "" {
		return false
	}
	sessionsMu.RLock()
	sess, ok := sessions[scope]
	sessionsMu.RUnlock()
	if !ok {
		return false
	}
	sess.mu.RLock()
	stored := sess.SharePassword
	sess.mu.RUnlock()
	if stored == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(password), []byte(stored)) == 1
}

// requestCookieScope returns the session scope carried by the request's auth
// cookie ("" for a full/unscoped user, an absent/invalid cookie, or compose
// mode where SWE_SWE_PASSWORD is unset and auth is fronted externally).
func requestCookieScope(r *http.Request) string {
	secret := os.Getenv("SWE_SWE_PASSWORD")
	if secret == "" {
		return ""
	}
	cookie, err := r.Cookie(authCookieName)
	if err != nil {
		return ""
	}
	scope, valid := authVerifyCookieScoped(cookie.Value, secret)
	if !valid {
		return ""
	}
	return scope
}

// buildShareURL builds the absolute login link a guest opens: it pre-fills the
// scope (this session) and a post-login redirect to the session page.
func buildShareURL(r *http.Request, sess *Session) string {
	scheme := "http"
	if resolveCookieSecure(r) {
		scheme = "https"
	}
	redirect := "/session/" + sess.UUID
	if sess.Assistant != "" {
		redirect += "?assistant=" + url.QueryEscape(sess.Assistant)
	}
	login := "/swe-swe-auth/login?scope=" + url.QueryEscape(sess.UUID) +
		"&redirect=" + url.QueryEscape(redirect)
	return scheme + "://" + r.Host + login
}

// firstPathSegment returns the first "/"-delimited segment of s.
func firstPathSegment(s string) string {
	if i := strings.IndexByte(s, '/'); i >= 0 {
		return s[:i]
	}
	return s
}

// sessionUUIDFromPath extracts the session UUID a request path targets, for the
// route shapes that resolve to one live session. ok is false for paths that
// carry no session UUID (static assets, /terminal-ui.js, /ssl/*, favicon, ...).
func sessionUUIDFromPath(path string) (string, bool) {
	switch {
	case strings.HasPrefix(path, "/session/"):
		return firstPathSegment(path[len("/session/"):]), true
	case strings.HasPrefix(path, "/ws/"):
		return firstPathSegment(path[len("/ws/"):]), true
	case strings.HasPrefix(path, "/proxy/"):
		return firstPathSegment(path[len("/proxy/"):]), true
	case strings.HasPrefix(path, "/api/session/"):
		return firstPathSegment(path[len("/api/session/"):]), true
	}
	return "", false
}

// scopedRequestAllowed decides whether a shared-session guest (a non-empty
// cookie scope) may make this request. It is the single guest policy, called
// from authMiddleware only when scope != "". The "/" homepage is handled by
// the caller (redirect to the guest's own session) and never reaches here.
//
// Default DENY: anything that could reach another session, spawn/fork/list
// sessions, browse repos/worktrees, or replay a recording is rejected. Only
// the guest's own session paths and UUID-less assets are allowed.
func scopedRequestAllowed(scope string, r *http.Request) bool {
	return scopedPathAllowed(scope, r.URL.Path)
}

// scopedPathAllowed is the guest policy for a single path, shared by the
// embedded gate (authMiddleware, via scopedRequestAllowed) and the Traefik
// ForwardAuth gate (authVerifyHandler, via scopedVerifyAllowed).
//
// This governs paths served by swe-swe-server: every non-session path here is
// a static asset / plumbing route, so UUID-less paths default ALLOW. (The
// Traefik dashboard is NOT a swe-swe-server path -- scopedVerifyAllowed denies
// it before delegating here.)
func scopedPathAllowed(scope, path string) bool {
	// Never for a guest: recordings (any), session spawn/fork, the
	// repo/worktree management APIs (which enumerate or create other work),
	// and server shutdown.
	switch {
	case strings.HasPrefix(path, "/recording/"),
		strings.HasPrefix(path, "/api/recording/"),
		path == "/api/session/new",
		strings.HasPrefix(path, "/api/fork/"),
		path == "/api/worktrees",
		path == "/api/worktree/check",
		path == "/api/repos",
		path == "/api/repo/prepare",
		path == "/api/repo/branches",
		path == "/api/server/shutdown":
		return false
	}

	// UUID-bearing session paths (/session, /ws, /proxy, /api/session): allow
	// only the guest's own session.
	if uuid, ok := sessionUUIDFromPath(path); ok {
		return scopeAllows(scope, uuid)
	}

	// Everything else is a UUID-less asset/plumbing path (embedded static
	// files, /terminal-ui.js, /ssl/*, /favicon.ico, /swe-swe-auth/*). These
	// expose no other session, and the guest's own session page needs them.
	return true
}

// traefikDashboardPrefixes are the path prefixes routed to Traefik's own
// dashboard (api@internal) in compose mode -- see traefik-dynamic.yml's
// "dashboard" router. Those routes are fronted by ForwardAuth -> /verify but
// NOT by swe-swe-server, so authMiddleware never sees them; scopedVerifyAllowed
// denies them for a shared-session guest.
var traefikDashboardPrefixes = []string{
	"/dashboard",
	"/api/http",
	"/api/tcp",
	"/api/entrypoints",
	"/api/overview",
}

// scopedVerifyAllowed is the guest policy for the Traefik ForwardAuth gate
// (/swe-swe-auth/verify). It first denies the Traefik dashboard (a direct
// backend that bypasses swe-swe-server), then delegates to the same policy the
// embedded gate uses. "/" is allowed here; swe-swe-server boxes it to the
// guest's own session downstream.
func scopedVerifyAllowed(scope, path string) bool {
	for _, p := range traefikDashboardPrefixes {
		if path == p || strings.HasPrefix(path, p+"/") || strings.HasPrefix(path, p) {
			return false
		}
	}
	if path == "" || path == "/" {
		return true
	}
	return scopedPathAllowed(scope, path)
}

// scopedHomeTarget builds the target the homepage sends a guest to: their own
// live session, with its assistant so /session/{uuid} does not bounce back to
// "/" (which would infinite-loop). ok is false when the session is gone
// (ended) -- the caller surfaces that instead of redirecting into a loop.
func scopedHomeTarget(scope string) (target string, ok bool) {
	sessionsMu.RLock()
	sess, exists := sessions[scope]
	assistant := ""
	if exists {
		assistant = sess.Assistant
	}
	sessionsMu.RUnlock()
	if !exists {
		return "", false
	}
	target = "/session/" + scope
	if assistant != "" {
		target += "?assistant=" + url.QueryEscape(assistant)
	}
	return target, true
}

// handleSessionShareAPI handles POST /api/session/{uuid}/share. It returns the
// guest login link and share password for the session as JSON. Owner-only: a
// request carrying a scoped (guest) cookie is forbidden, so a guest cannot mint
// further shares even for their own session.
func handleSessionShareAPI(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}
	if requestCookieScope(r) != "" {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	// Parse UUID from path: /api/session/{uuid}/share
	path := strings.TrimPrefix(r.URL.Path, "/api/session/")
	path = strings.TrimSuffix(path, "/share")
	sessionUUID := path
	if sessionUUID == "" {
		http.Error(w, "Missing session UUID", http.StatusBadRequest)
		return
	}

	sessionsMu.RLock()
	sess, exists := sessions[sessionUUID]
	sessionsMu.RUnlock()
	if !exists {
		http.Error(w, "Session not found", http.StatusNotFound)
		return
	}

	// NOTE: never log the share password -- it is a live credential.
	resp := map[string]string{
		"url":      buildShareURL(r, sess),
		"password": enableSessionShare(sess),
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}
