package main

import (
	"context"
	"fmt"
	htmlpkg "html"
	"log"
	"net"
	"net/http"
	"os"
	"strings"
	"time"
)

func firstNonEmpty(vs ...string) string {
	for _, v := range vs {
		if v != "" {
			return v
		}
	}
	return ""
}

// resolveListenAddr implements swe-swe-server's listen-address decision rule,
// with --bind / SWE_BIND letting ops restrict swe-swe-server to localhost so
// only the tunnel client can reach it:
//
//  1. --bind explicit              -> use it
//  2. --addr explicit              -> use it (legacy alias)
//  3. SWE_BIND set                 -> use it
//  4. SWE_PORT set                 -> ":${SWE_PORT}"  (host defaults to 0.0.0.0)
//  5. $PORT set                    -> ":${PORT}"  (PaaS; no landing server)
//  6. nothing set                  -> ":1977"
//
// --bind/SWE_BIND take a full host:port (e.g. "127.0.0.1:1977"); --addr is
// kept as an alias so existing Dockerfile CMDs and `swe-swe init` output
// keep working byte-identically.
//
// When $PORT is set AND differs from the effective listen port, the landing
// server binds $PORT; otherwise landingAddr is "" and no landing server runs.
func resolveListenAddr(flagBind, flagAddr, envSweBind, envSwePort, envPort string) (listenAddr, landingAddr string) {
	listenFromPort := false
	switch {
	case flagBind != "":
		listenAddr = flagBind
	case flagAddr != "":
		listenAddr = flagAddr
	case envSweBind != "":
		listenAddr = envSweBind
	case envSwePort != "":
		listenAddr = ":" + envSwePort
	case envPort != "":
		listenAddr = ":" + envPort
		listenFromPort = true
	default:
		listenAddr = ":1977"
	}
	if envPort != "" && !listenFromPort && !addrPortEquals(listenAddr, envPort) {
		landingAddr = ":" + envPort
	}
	return listenAddr, landingAddr
}

// addrPortEquals reports whether listenAddr's port component equals port.
// Accepts ":1977", "0.0.0.0:1977", "[::]:1977".
func addrPortEquals(listenAddr, port string) bool {
	_, p, err := net.SplitHostPort(listenAddr)
	if err != nil {
		return false
	}
	return p == port
}

// startLandingServer runs a tiny HTTP server on landingAddr advertising that
// swe-swe is reachable via its tunnel.  GET /health returns 200 for PaaS
// health probes; all other paths return a minimal HTML page.  Never exposes
// swe-swe's actual login form on this listener -- that's the whole point.
//
// If SWE_LANDING_DISABLE=1 is set, every path returns 200 OK with no body
// (PaaS health probes pass, but nothing about swe-swe leaks).
//
// The server's Shutdown is hooked to ctx so it stops cleanly when the main
// server shuts down.  Returns the http.Server for tests; callers may ignore.
func startLandingServer(ctx context.Context, landingAddr, sweListenAddr string) *http.Server {
	if landingAddr == "" {
		return nil
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("OK"))
	})
	if os.Getenv("SWE_LANDING_DISABLE") == "1" {
		mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		})
	} else {
		mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
			// Render per-request so the tunnel hostname (which can
			// change after startup) is reflected immediately. Cheap
			// -- this server only handles PaaS health probes plus
			// the occasional human visit; ms-level rendering is fine.
			body := renderLandingHTML(sweListenAddr, getLiveTunnelHostname())
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = w.Write([]byte(body))
		})
	}

	srv := &http.Server{Addr: landingAddr, Handler: mux}

	go func() {
		defer recoverGoroutine("landing server")
		log.Printf("[landing] serving on %s (swe-swe listens on %s)", landingAddr, sweListenAddr)
		if err := srv.ListenAndServe(); err != http.ErrServerClosed {
			log.Printf("[landing] stopped: %v", err)
		}
	}()

	go func() {
		defer recoverGoroutine("landing shutdown")
		<-ctx.Done()
		sctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(sctx)
	}()

	return srv
}

// renderLandingHTML returns the placeholder page served on $PORT.  Keeps
// everything inline (no templates, no assets) so it's impossible to leak
// swe-swe state through this server.
//
// liveTunnelHost (when non-empty) is the hostname currently registered
// with the tunneld; the page links to https://{port}.{liveTunnelHost}/
// so the operator can click straight through to their UI.  Empty
// string falls back to a "tunnel not yet registered" message.
func renderLandingHTML(sweListenAddr string, liveTunnelHost string) string {
	learnMore := os.Getenv("SWE_LANDING_URL")
	if learnMore == "" {
		learnMore = "https://swe-swe.netlify.app"
	}
	swePort := strings.TrimPrefix(sweListenAddr, ":")
	if i := strings.LastIndex(swePort, ":"); i >= 0 {
		swePort = swePort[i+1:]
	}

	body := `<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<title>swe-swe</title>
<style>
body { font-family: system-ui, sans-serif; max-width: 40em; margin: 3em auto; padding: 0 1em; line-height: 1.5; color: #222; }
code { background: #f4f4f4; padding: 0.1em 0.3em; border-radius: 3px; }
a { color: #0366d6; font-weight: 600; }
.tunnel { background: #ecfdf5; border-left: 4px solid #10b981; padding: 0.75em 1em; margin: 1em 0; border-radius: 4px; }
</style>
</head>
<body>
<h1>swe-swe is running</h1>
`

	if liveTunnelHost != "" {
		// Tunnel mode is live -- give the operator a click-through.
		tunnelURL := fmt.Sprintf("https://%s.%s/",
			htmlpkg.EscapeString(swePort), htmlpkg.EscapeString(liveTunnelHost))
		body += `<div class="tunnel">
<p><strong>Open your swe-swe at <a href="` + tunnelURL + `">` + tunnelURL + `</a></strong></p>
<p>This public URL is a placeholder. The real UI is reached through the tunnel above; auth (cookie-scoped to the tunnel hostname) gates access.</p>
</div>
`
	} else {
		// No tunnel registered yet -- generic placeholder copy.
		body += `<p>This public URL is a placeholder. No tunnel is registered yet; once a tunnel hostname is assigned the real UI becomes reachable through it.</p>
`
	}

	body += `<p>Learn more: <a href="` + htmlpkg.EscapeString(learnMore) + `">` + htmlpkg.EscapeString(learnMore) + `</a></p>
</body>
</html>
`
	return body
}
