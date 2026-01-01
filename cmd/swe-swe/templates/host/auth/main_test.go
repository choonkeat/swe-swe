package main

import "testing"

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
