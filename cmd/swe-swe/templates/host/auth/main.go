package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log"
	"net/http"
	"os"
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

const loginFormHTML = `<!DOCTYPE html>
<html>
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>Login - swe-swe</title>
</head>
<body>
    <form method="POST" action="/swe-swe-auth/login">
        <input type="password" name="password" autocomplete="current-password" placeholder="Password" required>
        <button type="submit">Login</button>
    </form>
</body>
</html>`

// loginHandler handles GET (show form) and POST (validate password) requests.
func loginHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPost {
		loginPostHandler(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(loginFormHTML))
}

// loginPostHandler validates password, sets cookie, and redirects.
func loginPostHandler(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}

	password := r.FormValue("password")
	if password == "" || password != secret {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}

	// Set session cookie with security attributes
	isSecure := r.Header.Get("X-Forwarded-Proto") == "https"
	http.SetCookie(w, &http.Cookie{
		Name:     cookieName,
		Value:    signCookie(secret),
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   isSecure,
	})

	// Redirect to home
	http.Redirect(w, r, "/", http.StatusFound)
}

func main() {
	secret = os.Getenv("SWE_SWE_PASSWORD")
	if secret == "" {
		log.Fatal("SWE_SWE_PASSWORD environment variable is required")
	}

	http.HandleFunc("/swe-swe-auth/verify", verifyHandler)
	http.HandleFunc("/swe-swe-auth/login", loginHandler)

	log.Println("auth service listening on :4180")
	log.Fatal(http.ListenAndServe(":4180", nil))
}
