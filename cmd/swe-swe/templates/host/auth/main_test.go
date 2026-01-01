package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSignCookie_ProducesNonEmptySignature(t *testing.T) {
	secret := "test-secret"
	cookie := signCookie(secret)
	if cookie == "" {
		t.Error("signCookie should produce non-empty string")
	}
}

func TestVerifyCookie_ValidSignature_ReturnsTrue(t *testing.T) {
	secret := "test-secret"
	cookie := signCookie(secret)
	if !verifyCookie(cookie, secret) {
		t.Error("verifyCookie should return true for valid signature")
	}
}

func TestVerifyCookie_TamperedValue_ReturnsFalse(t *testing.T) {
	secret := "test-secret"
	cookie := signCookie(secret)
	tamperedCookie := cookie + "tampered"
	if verifyCookie(tamperedCookie, secret) {
		t.Error("verifyCookie should return false for tampered cookie")
	}
}

func TestVerifyCookie_WrongSecret_ReturnsFalse(t *testing.T) {
	secret := "test-secret"
	wrongSecret := "wrong-secret"
	cookie := signCookie(secret)
	if verifyCookie(cookie, wrongSecret) {
		t.Error("verifyCookie should return false for wrong secret")
	}
}

func TestVerifyCookie_EmptyString_ReturnsFalse(t *testing.T) {
	secret := "test-secret"
	if verifyCookie("", secret) {
		t.Error("verifyCookie should return false for empty string")
	}
}

func TestVerifyCookie_MalformedCookie_ReturnsFalse(t *testing.T) {
	secret := "test-secret"
	if verifyCookie("no-delimiter-here", secret) {
		t.Error("verifyCookie should return false for malformed cookie")
	}
}

// Verify endpoint tests

func TestVerifyHandler_NoCookie_Returns401(t *testing.T) {
	secret = "test-secret"
	req := httptest.NewRequest(http.MethodGet, "/swe-swe-auth/verify", nil)
	w := httptest.NewRecorder()

	verifyHandler(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected status 401, got %d", w.Code)
	}
}

func TestVerifyHandler_InvalidCookie_Returns401(t *testing.T) {
	secret = "test-secret"
	req := httptest.NewRequest(http.MethodGet, "/swe-swe-auth/verify", nil)
	req.AddCookie(&http.Cookie{Name: cookieName, Value: "invalid-cookie"})
	w := httptest.NewRecorder()

	verifyHandler(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected status 401, got %d", w.Code)
	}
}

func TestVerifyHandler_ValidCookie_Returns200(t *testing.T) {
	secret = "test-secret"
	validCookie := signCookie(secret)
	req := httptest.NewRequest(http.MethodGet, "/swe-swe-auth/verify", nil)
	req.AddCookie(&http.Cookie{Name: cookieName, Value: validCookie})
	w := httptest.NewRecorder()

	verifyHandler(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}
}

// Login GET endpoint tests

func TestLoginGetHandler_Returns200(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/swe-swe-auth/login", nil)
	w := httptest.NewRecorder()

	loginHandler(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}
}

func TestLoginGetHandler_ReturnsHTML(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/swe-swe-auth/login", nil)
	w := httptest.NewRecorder()

	loginHandler(w, req)

	contentType := w.Header().Get("Content-Type")
	if !strings.Contains(contentType, "text/html") {
		t.Errorf("expected Content-Type text/html, got %s", contentType)
	}
}

func TestLoginGetHandler_ContainsPasswordField(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/swe-swe-auth/login", nil)
	w := httptest.NewRecorder()

	loginHandler(w, req)

	body := w.Body.String()
	if !strings.Contains(body, `type="password"`) {
		t.Error("expected password input field in HTML")
	}
	if !strings.Contains(body, `name="password"`) {
		t.Error("expected password field with name='password'")
	}
}

func TestLoginGetHandler_ContainsSubmitButton(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/swe-swe-auth/login", nil)
	w := httptest.NewRecorder()

	loginHandler(w, req)

	body := w.Body.String()
	if !strings.Contains(body, `type="submit"`) {
		t.Error("expected submit button in HTML")
	}
}

// Login POST endpoint tests

func TestLoginPostHandler_WrongPassword_Returns401(t *testing.T) {
	secret = "correct-password"
	body := strings.NewReader("password=wrong-password")
	req := httptest.NewRequest(http.MethodPost, "/swe-swe-auth/login", body)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()

	loginHandler(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected status 401, got %d", w.Code)
	}
}

func TestLoginPostHandler_EmptyPassword_Returns401(t *testing.T) {
	secret = "correct-password"
	body := strings.NewReader("password=")
	req := httptest.NewRequest(http.MethodPost, "/swe-swe-auth/login", body)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()

	loginHandler(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected status 401, got %d", w.Code)
	}
}

func TestLoginPostHandler_CorrectPassword_SetsCookie(t *testing.T) {
	secret = "correct-password"
	body := strings.NewReader("password=correct-password")
	req := httptest.NewRequest(http.MethodPost, "/swe-swe-auth/login", body)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()

	loginHandler(w, req)

	cookies := w.Result().Cookies()
	var sessionCookie *http.Cookie
	for _, c := range cookies {
		if c.Name == cookieName {
			sessionCookie = c
			break
		}
	}
	if sessionCookie == nil {
		t.Error("expected session cookie to be set")
	}
}

func TestLoginPostHandler_CorrectPassword_Redirects(t *testing.T) {
	secret = "correct-password"
	body := strings.NewReader("password=correct-password")
	req := httptest.NewRequest(http.MethodPost, "/swe-swe-auth/login", body)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()

	loginHandler(w, req)

	if w.Code != http.StatusFound {
		t.Errorf("expected status 302, got %d", w.Code)
	}
	location := w.Header().Get("Location")
	if location != "/" {
		t.Errorf("expected redirect to /, got %s", location)
	}
}
