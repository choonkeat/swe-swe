package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"html"
	"log"
	"net/http"
	"net/url"
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

// verifyHandler checks the session cookie and returns 200 (valid) or redirects to login (invalid).
// Used by Traefik ForwardAuth middleware.
func verifyHandler(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	originalURI := r.Header.Get("X-Forwarded-Uri")
	userAgent := r.Header.Get("User-Agent")

	cookie, err := r.Cookie(cookieName)
	if err != nil || !verifyCookie(cookie.Value, secret) {
		// Log failed auth attempts for debugging
		uaSnippet := userAgent
		if len(uaSnippet) > 50 {
			uaSnippet = uaSnippet[:50]
		}
		log.Printf("Auth failed: uri=%s cookie_present=%v ua=%s elapsed=%v",
			originalURI, err == nil, uaSnippet, time.Since(start))

		// Build absolute redirect URL using forwarded host/proto
		redirectURI := originalURI
		if redirectURI == "" {
			redirectURI = "/"
		}
		scheme := r.Header.Get("X-Forwarded-Proto")
		if scheme == "" {
			scheme = "http"
		}
		host := r.Header.Get("X-Forwarded-Host")
		if host == "" {
			host = r.Host
		}
		loginPath := "/swe-swe-auth/login?redirect=" + url.QueryEscape(redirectURI)
		loginURL := scheme + "://" + host + loginPath
		// Set Location header manually to avoid Go converting to internal host
		w.Header().Set("Location", loginURL)
		w.WriteHeader(http.StatusFound)
		return
	}

	// Log WebSocket auth for performance analysis
	if strings.HasPrefix(originalURI, "/ws/") {
		log.Printf("Auth OK (WebSocket): uri=%s elapsed=%v", originalURI, time.Since(start))
	}
	w.WriteHeader(http.StatusOK)
}

// renderLoginForm generates the login HTML with optional redirect value and error message
func renderLoginForm(redirectURL, errorMsg string) string {
	redirectField := ""
	if redirectURL != "" {
		redirectField = fmt.Sprintf(`<input type="hidden" name="redirect" value="%s">`, html.EscapeString(redirectURL))
	}
	errorHTML := ""
	if errorMsg != "" {
		errorHTML = fmt.Sprintf(`<div class="error">%s</div>`, html.EscapeString(errorMsg))
	}
	return fmt.Sprintf(`<!DOCTYPE html>
<html>
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>Login - swe-swe</title>
    <style>
        * { box-sizing: border-box; }
        body {
            font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif;
            background: #f5f5f5;
            margin: 0;
            padding: 20px;
            min-height: 100vh;
            display: flex;
            align-items: center;
            justify-content: center;
        }
        .container {
            background: white;
            padding: 40px;
            border-radius: 8px;
            box-shadow: 0 2px 10px rgba(0,0,0,0.1);
            width: 100%%;
            max-width: 400px;
        }
        h1 {
            margin: 0 0 24px 0;
            font-size: 24px;
            text-align: center;
            color: #333;
        }
        .error {
            background: #fee;
            color: #c00;
            padding: 12px;
            border-radius: 4px;
            margin-bottom: 16px;
            text-align: center;
        }
        input[type="password"] {
            width: 100%%;
            padding: 16px;
            font-size: 16px;
            border: 1px solid #ddd;
            border-radius: 4px;
            margin-bottom: 16px;
        }
        input[type="password"]:focus {
            outline: none;
            border-color: #007bff;
            box-shadow: 0 0 0 3px rgba(0,123,255,0.1);
        }
        button {
            width: 100%%;
            padding: 16px;
            font-size: 16px;
            background: #007bff;
            color: white;
            border: none;
            border-radius: 4px;
            cursor: pointer;
            -webkit-tap-highlight-color: transparent;
        }
        button:hover { background: #0056b3; }
        button:active { background: #004085; }
    </style>
</head>
<body>
    <div class="container">
        <h1>swe-swe</h1>
        %s
        <form method="POST" action="/swe-swe-auth/login">
            %s
            <input type="text" name="username" id="username" value="admin" autocomplete="username" readonly>
            <input type="password" name="password" id="password" autocomplete="current-password" placeholder="Password" required autofocus>
            <button type="submit">Login</button>
        </form>
    </div>
</body>
</html>`, errorHTML, redirectField)
}

// loginHandler handles GET (show form) and POST (validate password) requests.
func loginHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPost {
		loginPostHandler(w, r)
		return
	}
	redirectURL := r.URL.Query().Get("redirect")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(renderLoginForm(redirectURL, "")))
}

// loginPostHandler validates password, sets cookie, and redirects.
func loginPostHandler(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(renderLoginForm("", "Invalid request")))
		return
	}

	password := r.FormValue("password")
	redirectURL := r.FormValue("redirect")
	if password == "" || password != secret {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(renderLoginForm(redirectURL, "Invalid password")))
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

	// Redirect to original URL or home
	if redirectURL == "" {
		redirectURL = "/"
	}
	http.Redirect(w, r, redirectURL, http.StatusFound)
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
