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
