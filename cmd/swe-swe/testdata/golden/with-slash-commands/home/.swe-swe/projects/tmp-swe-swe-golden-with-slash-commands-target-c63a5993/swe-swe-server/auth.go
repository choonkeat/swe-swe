package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"html"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
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

	// Global ceiling on failed login attempts across ALL throttle keys in a
	// window. The per-key limiter can be sidestepped by an attacker who
	// rotates a forged identifier (e.g. X-Forwarded-For) so every attempt
	// lands in its own bucket; this ceiling is the backstop that no per-key
	// trick can dodge. Sized well above any plausible legitimate burst but
	// far below brute-force speed.
	authGlobalRateLimitMax = 200
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

// authGlobalLimiter is the global failed-attempt backstop. See
// authGlobalRateLimitMax.
var authGlobalLimiter = &authGlobalRateLimiter{}

// authGlobalRateLimiter is a simple sliding-window counter of failed login
// attempts, not keyed by anything. It exists so a brute-forcer who defeats the
// per-key limiter (by rotating X-Forwarded-For) is still capped.
type authGlobalRateLimiter struct {
	mu    sync.Mutex
	times []time.Time
}

// allow returns true if fewer than max failed attempts occurred in the window.
func (g *authGlobalRateLimiter) allow(max int) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	cutoff := time.Now().Add(-authRateLimitWindow)
	recent := g.times[:0]
	for _, t := range g.times {
		if t.After(cutoff) {
			recent = append(recent, t)
		}
	}
	g.times = recent
	return len(recent) < max
}

// record adds a failed attempt.
func (g *authGlobalRateLimiter) record() {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.times = append(g.times, time.Now())
}

// loginThrottleKey returns the identifier used to bucket login attempts.
//
// By default this is the transport peer address (RemoteAddr host), which the
// client cannot forge. X-Forwarded-For is consulted ONLY when
// SWE_TRUST_FORWARDED_FOR=true, i.e. the operator has confirmed a trusted
// proxy fronts the server and sets that header. Trusting X-Forwarded-For
// unconditionally lets an attacker rotate the value to dodge the per-key
// limiter entirely (see authGlobalRateLimiter for the backstop).
func loginThrottleKey(r *http.Request) string {
	if os.Getenv("SWE_TRUST_FORWARDED_FOR") == "true" {
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			if idx := strings.Index(xff, ","); idx != -1 {
				xff = xff[:idx]
			}
			return strings.TrimSpace(xff)
		}
	}
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		return host
	}
	return r.RemoteAddr
}

// safeRedirect constrains a post-login redirect target to a same-origin,
// rooted path. Anything else -- absolute URLs, protocol-relative ("//host"),
// backslash-smuggled ("/\host"), scheme-bearing ("javascript:..."), or values
// that don't start with a single "/" -- collapses to "/". This closes the
// open-redirect / login-token-relay vector on the trusted origin.
func safeRedirect(target string) string {
	if target == "" {
		return "/"
	}
	// Must be rooted, and must not be protocol-relative or backslash-smuggled.
	if !strings.HasPrefix(target, "/") ||
		strings.HasPrefix(target, "//") ||
		strings.HasPrefix(target, "/\\") {
		return "/"
	}
	// Defense in depth: parse and reject anything that carries a scheme or host.
	if u, err := url.Parse(target); err != nil || u.IsAbs() || u.Host != "" {
		return "/"
	}
	return target
}

// checkWebSocketOrigin is the Upgrader.CheckOrigin allow-list. It permits:
//   - requests with no Origin header (non-browser clients), and
//   - browser requests whose Origin host matches the request host, the live
//     tunnel apex, or a "{port}.{apex}" per-port subdomain.
//
// Everything else is rejected, closing the cross-site WebSocket hijacking
// vector that "return true" left open.
func checkWebSocketOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true // non-browser client; no Origin to validate
	}
	u, err := url.Parse(origin)
	if err != nil || u.Host == "" {
		return false
	}
	host := u.Hostname()
	// Same host as the request (covers localhost / LAN / apex, with or
	// without a port on r.Host).
	if reqHost, _, err := net.SplitHostPort(r.Host); err == nil {
		if host == reqHost {
			return true
		}
	} else if host == r.Host {
		return true
	}
	// Tunnel apex and any "{port}.{apex}" per-port subdomain.
	if ph := getLiveTunnelHostname(); ph != "" {
		if host == ph || strings.HasSuffix(host, "."+ph) {
			return true
		}
	}
	return false
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

// resolveCookieSecure decides the Secure flag for the session cookie.
// Prefer per-request X-Forwarded-Proto set by a fronting proxy (Traefik,
// Fly, Railway) so requests that bypass the proxy -- e.g. a direct hit on
// the swe-swe-server HTTP port over Tailscale -- correctly issue non-Secure
// cookies. Fall back to SWE_COOKIE_SECURE only when no proxy sets the
// header (rare PaaS that terminates TLS without forwarded headers, or a
// user fronting the server with custom TLS that omits the header).
//
// Intentionally does not gate on source IP: in tunnel mode, browser TLS
// is forwarded through a tunnel client on localhost, so the source IP at
// swe-swe-server is always 127.0.0.1 even for "real" HTTPS traffic. Trust
// X-Forwarded-Proto, not the connection peer.
func resolveCookieSecure(r *http.Request) bool {
	if p := r.Header.Get("X-Forwarded-Proto"); p != "" {
		return p == "https"
	}
	if v := os.Getenv("SWE_COOKIE_SECURE"); v != "" {
		return v == "true"
	}
	return false
}

// resolveCookieDomain decides the Domain attribute for the session cookie.
// In tunnel mode (publicHostname non-empty) the browser lands on
// "{port}.{publicHostname}" subdomains -- the auth cookie must be set
// with Domain=publicHostname so it is sent across all per-port subdomains.
//
// But tunnel mode and local access are not mutually exclusive: with
// --tunnel-local-ports the same server also answers on localhost:{port}
// and LAN addresses. A cookie stamped Domain={tunnel-hostname} is rejected
// by the browser on a localhost login (RFC 6265 requires Domain to
// domain-match the request host), silently breaking that login. So we
// only pin the cookie to the apex when the browser actually reached us
// via the tunnel hostname (or a subdomain of it); any other host --
// localhost, 127.0.0.1, a LAN IP -- gets a host-only cookie (Domain="").
// Legacy mode (publicHostname empty) is always host-only.
//
// requestHost is r.Host and may carry a :port suffix, which we strip.
//
// Per RFC 6265 a leading "." in Domain is deprecated and stripped on
// parse, so Domain=foo.example.com already matches bar.foo.example.com.
// We omit the dot for clean wire output -- net/http's Cookie.String()
// also strips it.
func resolveCookieDomain(publicHostname, requestHost string) string {
	if publicHostname == "" {
		return ""
	}
	host := requestHost
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	if host == publicHostname || strings.HasSuffix(host, "."+publicHostname) {
		return publicHostname
	}
	return ""
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

	// Rate limit check. The per-key bucket throttles a single source; the
	// global ceiling backstops an attacker who rotates the throttle key
	// (e.g. a spoofed X-Forwarded-For) to dodge the per-key limiter.
	clientKey := loginThrottleKey(r)

	if !authLoginLimiter.allow(clientKey) || !authGlobalLimiter.allow(authGlobalRateLimitMax) {
		log.Printf("Rate limited: key=%s", clientKey)
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusTooManyRequests)
		w.Write([]byte(authRenderLoginForm(redirectURL, "Too many attempts. Please wait a few minutes.")))
		return
	}

	// Constant-time password comparison
	passwordMatch := subtle.ConstantTimeCompare([]byte(password), []byte(secret)) == 1
	if password == "" || !passwordMatch {
		authLoginLimiter.record(clientKey)
		authGlobalLimiter.record()
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(authRenderLoginForm(redirectURL, "Invalid password")))
		return
	}

	http.SetCookie(w, &http.Cookie{
		Name:     authCookieName,
		Value:    authSignCookie(secret),
		Path:     "/",
		Domain:   resolveCookieDomain(getLiveTunnelHostname(), r.Host),
		MaxAge:   authCookieMaxAge,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   resolveCookieSecure(r),
	})

	// Redirect to original URL or home. safeRedirect rejects off-site
	// targets so a successful login can't be used to bounce the user to an
	// attacker origin (open redirect).
	http.Redirect(w, r, safeRedirect(redirectURL), http.StatusFound)
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
// Exempt paths: /swe-swe-auth/login, /swe-swe-auth/verify, /ssl/*, /mcp,
// /api/session/*, /api/autocomplete/* (these use API key auth instead).
func authMiddleware(next http.Handler, secret string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path

		// Exempt paths that don't require authentication
		// (API key-authenticated routes handle their own auth)
		if path == "/swe-swe-auth/login" ||
			path == "/swe-swe-auth/verify" ||
			strings.HasPrefix(path, "/ssl/") ||
			path == "/mcp" ||
			(strings.HasPrefix(path, "/api/session/") && strings.HasSuffix(path, "/browser/start")) ||
			strings.HasPrefix(path, "/api/autocomplete/") {
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

// requireAuthCookie wraps a handler with cookie-only auth.
//
// Used for the per-session port-based proxies (preview/23000, agent-chat/24000,
// vnc/27000). In legacy/Traefik mode these listeners were reachable only via
// Traefik with ForwardAuth checking the cookie before forwarding. In tunnel
// mode, tunneld dials the per-port listeners directly inside the container
// with no auth gate, so we have to enforce auth here.
//
// Differs from authMiddleware in two ways:
//   - 401 instead of redirect on missing/invalid cookie. The proxies are loaded
//     cross-origin into iframes and a relative /swe-swe-auth/login redirect
//     would resolve to {port}.{publicHostname} which doesn't serve the auth
//     handlers (those are bound to the apex 1977 mux).
//   - /__probe__ is exempt so the existing client-side reachability probe in
//     terminal-ui.js keeps working without credentials.
//
// If secret is empty (no SWE_SWE_PASSWORD), returns next unwrapped -- harmless
// no-op for compose-mode setups where Traefik fronts everything.
func requireAuthCookie(secret string, next http.Handler) http.Handler {
	if secret == "" {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/__probe__" {
			next.ServeHTTP(w, r)
			return
		}
		cookie, err := r.Cookie(authCookieName)
		if err != nil || !authVerifyCookie(cookie.Value, secret) {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// setupEmbeddedAuth registers the login handler and wraps the default mux with auth middleware.
// Returns the handler to use for the HTTP server.
// If password is empty, returns nil (no auth needed -- compose mode with Traefik handles it).
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
