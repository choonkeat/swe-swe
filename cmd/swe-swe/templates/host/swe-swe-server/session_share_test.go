package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// registerTestSession inserts sess into the global registry under uuid and
// removes it when the test ends.
func registerTestSession(t *testing.T, uuid string, sess *Session) {
	t.Helper()
	sess.UUID = uuid
	sessionsMu.Lock()
	sessions[uuid] = sess
	sessionsMu.Unlock()
	t.Cleanup(func() {
		sessionsMu.Lock()
		delete(sessions, uuid)
		sessionsMu.Unlock()
	})
}

func TestEnableSessionShareIdempotent(t *testing.T) {
	sess := &Session{UUID: "s-1"}
	pw1 := enableSessionShare(sess)
	pw2 := enableSessionShare(sess)
	if pw1 == "" {
		t.Fatal("share password should not be empty")
	}
	if pw1 != pw2 {
		t.Errorf("enableSessionShare not idempotent: %q vs %q", pw1, pw2)
	}
	if len(pw1) < 32 {
		t.Errorf("share password too short (%d chars); want >=32 for >=128 bits entropy", len(pw1))
	}
}

func TestValidSessionShareLogin(t *testing.T) {
	sess := &Session{}
	registerTestSession(t, "share-sess", sess)
	pw := enableSessionShare(sess)

	if !validSessionShareLogin("share-sess", pw) {
		t.Error("correct share password should validate")
	}
	if validSessionShareLogin("share-sess", "wrong") {
		t.Error("wrong share password must not validate")
	}
	if validSessionShareLogin("no-such-session", pw) {
		t.Error("unknown session scope must not validate")
	}

	// Sharing off (no password set) must never validate, even with empty pw.
	off := &Session{}
	registerTestSession(t, "share-off", off)
	if validSessionShareLogin("share-off", "") {
		t.Error("empty share password (sharing off) must not validate")
	}
}

func TestHandleSessionShareAPI(t *testing.T) {
	sess := &Session{Assistant: "claude"}
	registerTestSession(t, "api-sess", sess)

	// POST -> 200 + JSON {url, password}
	req := httptest.NewRequest("POST", "/api/session/api-sess/share", nil)
	req.Host = "example.com"
	rr := httptest.NewRecorder()
	handleSessionShareAPI(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%q", rr.Code, rr.Body.String())
	}
	var resp struct {
		URL      string `json:"url"`
		Password string `json:"password"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("bad JSON: %v", err)
	}
	if resp.Password == "" {
		t.Error("expected a share password")
	}
	if !strings.Contains(resp.URL, "scope=api-sess") {
		t.Errorf("share url missing scope: %q", resp.URL)
	}
	if !strings.Contains(resp.URL, "/swe-swe-auth/login") {
		t.Errorf("share url should point at login: %q", resp.URL)
	}

	// Wrong method -> 405
	rr = httptest.NewRecorder()
	handleSessionShareAPI(rr, httptest.NewRequest("GET", "/api/session/api-sess/share", nil))
	if rr.Code != http.StatusMethodNotAllowed {
		t.Errorf("GET status = %d, want 405", rr.Code)
	}

	// Unknown session -> 404
	rr = httptest.NewRecorder()
	handleSessionShareAPI(rr, httptest.NewRequest("POST", "/api/session/ghost/share", nil))
	if rr.Code != http.StatusNotFound {
		t.Errorf("unknown session status = %d, want 404", rr.Code)
	}
}

// A shared-session guest -- even one holding a valid scoped cookie for THIS
// session -- must never be able to mint further share links.
func TestHandleSessionShareAPIRejectsScopedGuest(t *testing.T) {
	t.Setenv("SWE_SWE_PASSWORD", "master")
	sess := &Session{Assistant: "claude"}
	registerTestSession(t, "guest-sess", sess)

	req := httptest.NewRequest("POST", "/api/session/guest-sess/share", nil)
	req.AddCookie(&http.Cookie{Name: authCookieName, Value: authSignScopedCookie("master", "guest-sess")})
	rr := httptest.NewRecorder()
	handleSessionShareAPI(rr, req)
	if rr.Code != http.StatusForbidden {
		t.Errorf("scoped guest status = %d, want 403", rr.Code)
	}
}

func TestAuthLoginPostScopedValid(t *testing.T) {
	secret := "master-secret"
	sess := &Session{}
	registerTestSession(t, "login-sess", sess)
	pw := enableSessionShare(sess)

	form := url.Values{}
	form.Set("password", pw)
	form.Set("scope", "login-sess")
	form.Set("redirect", "/session/login-sess?assistant=claude")
	req := httptest.NewRequest("POST", "/swe-swe-auth/login", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.RemoteAddr = "127.0.0.1:5599"
	rr := httptest.NewRecorder()

	authLoginPostHandler(rr, req, secret)

	if rr.Code != http.StatusFound {
		t.Fatalf("status = %d, want 302; body=%q", rr.Code, rr.Body.String())
	}
	var scoped *http.Cookie
	for _, c := range rr.Result().Cookies() {
		if c.Name == authCookieName {
			scoped = c
		}
	}
	if scoped == nil {
		t.Fatal("no auth cookie set")
	}
	gotScope, ok := authVerifyCookieScoped(scoped.Value, secret)
	if !ok || gotScope != "login-sess" {
		t.Errorf("cookie scope = %q valid=%v, want scope=login-sess valid=true", gotScope, ok)
	}
}

func TestAuthLoginPostScopedWrongPassword(t *testing.T) {
	secret := "master-secret"
	sess := &Session{}
	registerTestSession(t, "login-sess-2", sess)
	enableSessionShare(sess)

	form := url.Values{}
	form.Set("password", "not-the-share-password")
	form.Set("scope", "login-sess-2")
	req := httptest.NewRequest("POST", "/swe-swe-auth/login", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.RemoteAddr = "127.0.0.1:5600"
	rr := httptest.NewRecorder()
	authLoginPostHandler(rr, req, secret)
	if rr.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", rr.Code)
	}
}

// A scoped login for a non-existent session must be rejected with the SAME
// generic error as a wrong password -- never leak which sessions exist.
func TestAuthLoginPostScopedUnknownSession(t *testing.T) {
	secret := "master-secret"
	form := url.Values{}
	form.Set("password", "anything")
	form.Set("scope", "does-not-exist")
	req := httptest.NewRequest("POST", "/swe-swe-auth/login", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.RemoteAddr = "127.0.0.1:5601"
	rr := httptest.NewRecorder()
	authLoginPostHandler(rr, req, secret)
	if rr.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", rr.Code)
	}
	if !strings.Contains(rr.Body.String(), "Invalid password") {
		t.Error("unknown-session scoped login should return the generic Invalid password")
	}
}

// The master password must still log a full (unscoped) user in even when a
// scope field is absent -- existing behavior unchanged.
func TestAuthLoginPostMasterStillUnscoped(t *testing.T) {
	secret := "master-secret"
	form := url.Values{}
	form.Set("password", secret)
	req := httptest.NewRequest("POST", "/swe-swe-auth/login", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.RemoteAddr = "127.0.0.1:5602"
	rr := httptest.NewRecorder()
	authLoginPostHandler(rr, req, secret)
	if rr.Code != http.StatusFound {
		t.Fatalf("status = %d, want 302", rr.Code)
	}
	for _, c := range rr.Result().Cookies() {
		if c.Name == authCookieName {
			scope, ok := authVerifyCookieScoped(c.Value, secret)
			if !ok || scope != "" {
				t.Errorf("master login cookie scope = %q valid=%v, want unscoped", scope, ok)
			}
		}
	}
}

func TestAuthRenderLoginFormScopeField(t *testing.T) {
	withScope := authRenderLoginForm("/session/x", "sess-42", "")
	if !strings.Contains(withScope, `name="scope"`) || !strings.Contains(withScope, "sess-42") {
		t.Error("login form missing hidden scope field for a scoped share link")
	}
	plain := authRenderLoginForm("", "", "")
	if strings.Contains(plain, `name="scope"`) {
		t.Error("unscoped login form should not contain a scope field")
	}
}
