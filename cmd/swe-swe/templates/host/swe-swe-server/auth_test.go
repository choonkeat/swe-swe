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
