package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"html"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	authCookieName      = "swe_swe_session"
	authCookieDelimiter = "|"
	authCookieMaxAge    = 7 * 24 * 60 * 60 // 7 days in seconds

	// Rate limiting for login endpoint
	authRateLimitWindow  = 5 * time.Minute
	authRateLimitMax     = 10 // max attempts per IP per window
	authRateLimitCleanup = 10 * time.Minute
)

// authLoginLimiter tracks failed login attempts per IP for rate limiting.
var authLoginLimiter = &authRateLimiter{
	attempts: make(map[string][]time.Time),
}

type authRateLimiter struct {
	mu       sync.Mutex
	attempts map[string][]time.Time
}

// allow returns true if the IP is allowed to attempt login.
func (rl *authRateLimiter) allow(ip string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()
	cutoff := now.Add(-authRateLimitWindow)

	// Filter to only recent attempts
	recent := rl.attempts[ip][:0]
	for _, t := range rl.attempts[ip] {
		if t.After(cutoff) {
			recent = append(recent, t)
		}
	}
	rl.attempts[ip] = recent

	return len(recent) < authRateLimitMax
}

// record adds a failed attempt for the IP.
func (rl *authRateLimiter) record(ip string) {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	rl.attempts[ip] = append(rl.attempts[ip], time.Now())
}

// cleanup removes expired entries to prevent memory growth.
func (rl *authRateLimiter) cleanup() {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	cutoff := time.Now().Add(-authRateLimitWindow)
	for ip, attempts := range rl.attempts {
		recent := attempts[:0]
		for _, t := range attempts {
			if t.After(cutoff) {
				recent = append(recent, t)
			}
		}
		if len(recent) == 0 {
			delete(rl.attempts, ip)
		} else {
			rl.attempts[ip] = recent
		}
	}
}

// authSignCookie creates an HMAC-signed cookie value.
// Format: "timestamp|hmac-signature"
func authSignCookie(secret string) string {
	timestamp := fmt.Sprintf("%d", time.Now().Unix())
	signature := authComputeHMAC(timestamp, secret)
	return timestamp + authCookieDelimiter + signature
}

// authVerifyCookie validates an HMAC-signed cookie value and checks expiry.
func authVerifyCookie(cookie, secret string) bool {
	if cookie == "" {
		return false
	}

	parts := strings.SplitN(cookie, authCookieDelimiter, 2)
	if len(parts) != 2 {
		return false
	}

	timestamp := parts[0]
	signature := parts[1]

	// Verify HMAC signature
	expectedSignature := authComputeHMAC(timestamp, secret)
	if !hmac.Equal([]byte(signature), []byte(expectedSignature)) {
		return false
	}

	// Verify timestamp hasn't expired
	ts, err := strconv.ParseInt(timestamp, 10, 64)
	if err != nil {
		return false
	}
	if time.Now().Unix()-ts > int64(authCookieMaxAge) {
		return false
	}

	return true
}

// authComputeHMAC generates an HMAC-SHA256 signature.
func authComputeHMAC(data, secret string) string {
	h := hmac.New(sha256.New, []byte(secret))
	h.Write([]byte(data))
	return hex.EncodeToString(h.Sum(nil))
}

// authRenderLoginForm generates the login HTML with optional redirect value and error message
func authRenderLoginForm(redirectURL, errorMsg string) string {
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
        input[type="text"],
        input[type="password"] {
            width: 100%%;
            padding: 16px;
            font-size: 16px;
            border: 1px solid #ddd;
            border-radius: 4px;
            margin-bottom: 16px;
        }
        input[type="text"]:focus,
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
        .footer { margin-top: 24px; text-align: center; }
        .footer a { color: #999; font-size: 13px; text-decoration: none; }
        .footer a:hover { color: #666; text-decoration: underline; }
    </style>
</head>
<body>
    <div class="container">
        <h1>swe-swe</h1>
        %s
        <form method="POST" action="/swe-swe-auth/login" id="login-form">
            %s
            <input type="text" name="username" id="username" placeholder="Your name" autocomplete="username">
            <input type="password" name="password" id="password" autocomplete="current-password" placeholder="Password" required autofocus>
            <button type="submit">Login</button>
        </form>
        <div class="footer"><a href="https://swe-swe.netlify.app" target="_blank">swe-swe.netlify.app</a></div>
    </div>
    <script>
        // localStorage key matches terminal-ui.js
        const USERNAME_KEY = 'swe-swe-username';
        const usernameInput = document.getElementById('username');
        const form = document.getElementById('login-form');

        // On load: prefill from localStorage if available
        try {
            const savedName = localStorage.getItem(USERNAME_KEY);
            if (savedName) {
                usernameInput.value = savedName;
            }
        } catch (e) {
            console.warn('Could not read localStorage:', e);
        }

        // On submit: save to localStorage (or clear if empty)
        form.addEventListener('submit', function() {
            try {
                const name = usernameInput.value.trim();
                if (name) {
                    localStorage.setItem(USERNAME_KEY, name);
                } else {
                    localStorage.removeItem(USERNAME_KEY);
                }
            } catch (e) {
                console.warn('Could not write localStorage:', e);
            }
        });
    </script>
</body>
</html>`, errorHTML, redirectField)
}

// authLoginHandler handles GET (show form) and POST (validate password) requests.
func authLoginHandler(password string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			authLoginPostHandler(w, r, password)
			return
		}
		redirectURL := r.URL.Query().Get("redirect")
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(authRenderLoginForm(redirectURL, "")))
	}
}

// authLoginPostHandler validates password, sets cookie, and redirects.
func authLoginPostHandler(w http.ResponseWriter, r *http.Request, secret string) {
	if err := r.ParseForm(); err != nil {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(authRenderLoginForm("", "Invalid request")))
		return
	}

	password := r.FormValue("password")
	redirectURL := r.FormValue("redirect")

	// Rate limit check
	clientIP := r.Header.Get("X-Forwarded-For")
	if clientIP == "" {
		clientIP = r.RemoteAddr
	}
	// Use first IP if X-Forwarded-For contains multiple
	if idx := strings.Index(clientIP, ","); idx != -1 {
		clientIP = strings.TrimSpace(clientIP[:idx])
	}

	if !authLoginLimiter.allow(clientIP) {
		log.Printf("Rate limited: ip=%s", clientIP)
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusTooManyRequests)
		w.Write([]byte(authRenderLoginForm(redirectURL, "Too many attempts. Please wait a few minutes.")))
		return
	}

	// Constant-time password comparison
	passwordMatch := subtle.ConstantTimeCompare([]byte(password), []byte(secret)) == 1
	if password == "" || !passwordMatch {
		authLoginLimiter.record(clientIP)
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(authRenderLoginForm(redirectURL, "Invalid password")))
		return
	}

	// Determine if request is over HTTPS
	isSecure := r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https"
	http.SetCookie(w, &http.Cookie{
		Name:     authCookieName,
		Value:    authSignCookie(secret),
		Path:     "/",
		MaxAge:   authCookieMaxAge,
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

// authVerifyHandler checks the session cookie and returns 200 (valid) or redirects to login.
// Used by Traefik ForwardAuth middleware in compose mode.
func authVerifyHandler(secret string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie(authCookieName)
		if err != nil || !authVerifyCookie(cookie.Value, secret) {
			// Build redirect URL using forwarded headers (Traefik sends these)
			redirectURI := r.Header.Get("X-Forwarded-Uri")
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
			w.Header().Set("Location", loginURL)
			w.WriteHeader(http.StatusFound)
			return
		}
		w.WriteHeader(http.StatusOK)
	}
}

// authMiddleware wraps an http.Handler with cookie-based authentication.
// Unauthenticated requests are redirected to /swe-swe-auth/login.
// Exempt paths: /swe-swe-auth/login, /swe-swe-auth/verify, /ssl/*, /mcp
func authMiddleware(next http.Handler, secret string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path

		// Exempt paths that don't require authentication
		if path == "/swe-swe-auth/login" ||
			path == "/swe-swe-auth/verify" ||
			strings.HasPrefix(path, "/ssl/") ||
			path == "/mcp" {
			next.ServeHTTP(w, r)
			return
		}

		// Check session cookie
		cookie, err := r.Cookie(authCookieName)
		if err != nil || !authVerifyCookie(cookie.Value, secret) {
			// Build redirect URL
			redirectURI := r.URL.RequestURI()
			if redirectURI == "" {
				redirectURI = "/"
			}
			loginURL := "/swe-swe-auth/login?redirect=" + url.QueryEscape(redirectURI)
			http.Redirect(w, r, loginURL, http.StatusFound)
			return
		}

		next.ServeHTTP(w, r)
	})
}

// setupEmbeddedAuth registers the login handler and wraps the default mux with auth middleware.
// Returns the handler to use for the HTTP server.
// If password is empty, returns nil (no auth needed — compose mode with Traefik handles it).
func setupEmbeddedAuth(password string) http.Handler {
	if password == "" {
		return nil
	}

	// Start periodic cleanup of rate limiter state
	go func() {
		for {
			time.Sleep(authRateLimitCleanup)
			authLoginLimiter.cleanup()
		}
	}()

	// Register auth handlers on the default mux
	http.HandleFunc("/swe-swe-auth/login", authLoginHandler(password))
	http.HandleFunc("/swe-swe-auth/verify", authVerifyHandler(password))

	// Wrap default mux with auth middleware
	return authMiddleware(http.DefaultServeMux, password)
}
