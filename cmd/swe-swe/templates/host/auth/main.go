package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"strings"
	"time"
)

const (
	cookieName      = "swe_swe_session"
	cookieDelimiter = "|"
)

// secret is the password used for authentication and cookie signing.
// Set from SWE_SWE_PASSWORD environment variable in main().
var secret string

// signCookie creates an HMAC-signed cookie value.
// Format: "timestamp|hmac-signature"
func signCookie(secret string) string {
	timestamp := fmt.Sprintf("%d", time.Now().Unix())
	signature := computeHMAC(timestamp, secret)
	return timestamp + cookieDelimiter + signature
}

// verifyCookie validates an HMAC-signed cookie value.
func verifyCookie(cookie, secret string) bool {
	if cookie == "" {
		return false
	}

	parts := strings.SplitN(cookie, cookieDelimiter, 2)
	if len(parts) != 2 {
		return false
	}

	timestamp := parts[0]
	signature := parts[1]

	expectedSignature := computeHMAC(timestamp, secret)
	return hmac.Equal([]byte(signature), []byte(expectedSignature))
}

// computeHMAC generates an HMAC-SHA256 signature.
func computeHMAC(data, secret string) string {
	h := hmac.New(sha256.New, []byte(secret))
	h.Write([]byte(data))
	return hex.EncodeToString(h.Sum(nil))
}

// verifyHandler checks the session cookie and returns 200 (valid) or 401 (invalid).
// Used by Traefik ForwardAuth middleware.
func verifyHandler(w http.ResponseWriter, r *http.Request) {
	cookie, err := r.Cookie(cookieName)
	if err != nil || !verifyCookie(cookie.Value, secret) {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	w.WriteHeader(http.StatusOK)
}

func main() {
}
