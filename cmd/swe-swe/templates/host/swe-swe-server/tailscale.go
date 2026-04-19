package main

import (
	"context"
	"fmt"
	htmlpkg "html"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// tailscaleConfig captures Tailscale settings resolved from flags and env.
// AuthKey unset == feature dormant (single-container behavior unchanged).
type tailscaleConfig struct {
	Disabled bool
	AuthKey  string
	Hostname string
	StateDir string
}

// resolveTailscaleConfig merges flag values with env-var fallbacks.  Flags
// win; env fills the gap.  Default state dir is /var/lib/tailscale per the
// design doc; override via --tailscale-state-dir or TS_STATE_DIR.
func resolveTailscaleConfig(flagAuthKey, flagHostname, flagStateDir string, flagDisable bool) tailscaleConfig {
	return tailscaleConfig{
		Disabled: flagDisable || os.Getenv("TS_DISABLE") == "1",
		AuthKey:  firstNonEmpty(flagAuthKey, os.Getenv("TS_AUTHKEY")),
		Hostname: firstNonEmpty(flagHostname, os.Getenv("TS_HOSTNAME")),
		StateDir: firstNonEmpty(flagStateDir, os.Getenv("TS_STATE_DIR"), "/var/lib/tailscale"),
	}
}

func firstNonEmpty(vs ...string) string {
	for _, v := range vs {
		if v != "" {
			return v
		}
	}
	return ""
}

// resolveListenAddr implements the decision rule from www/swe-swe-tailscale.md:
//
//  1. --addr explicit              -> use it
//  2. --addr unset, SWE_PORT set   -> ":${SWE_PORT}"
//  3. --addr unset, SWE_PORT unset,
//     $PORT set                    -> ":${PORT}"  (no landing server)
//  4. nothing set                  -> ":9898"
//
// When $PORT is set AND differs from the effective listen port, the landing
// server binds $PORT; otherwise landingAddr is "" and no landing server runs.
func resolveListenAddr(flagAddr, envSwePort, envPort string) (listenAddr, landingAddr string) {
	listenFromPort := false
	switch {
	case flagAddr != "":
		listenAddr = flagAddr
	case envSwePort != "":
		listenAddr = ":" + envSwePort
	case envPort != "":
		listenAddr = ":" + envPort
		listenFromPort = true
	default:
		listenAddr = ":9898"
	}
	if envPort != "" && !listenFromPort && !addrPortEquals(listenAddr, envPort) {
		landingAddr = ":" + envPort
	}
	return listenAddr, landingAddr
}

// addrPortEquals reports whether listenAddr's port component equals port.
// Accepts ":9898", "0.0.0.0:9898", "[::]:9898".
func addrPortEquals(listenAddr, port string) bool {
	_, p, err := net.SplitHostPort(listenAddr)
	if err != nil {
		return false
	}
	return p == port
}

// startTailscale launches tailscaled in userspace-networking mode and runs
// `tailscale up` to join the tailnet.  No-op if cfg.AuthKey is empty or
// cfg.Disabled is true -- the single-container behavior stays byte-identical.
//
// Errors are logged but not fatal: if Tailscale fails to start, swe-swe still
// serves normally on its listen port.  The goal is "best-effort Tailscale
// bootstrap," not "refuse to start unless tailnet is up."
//
// Lifecycle: ctx cancellation (via serverCtx in main) signals tailscaled's
// exec.CommandContext to send SIGKILL.  A goroutine captures Wait() with PID
// and exit status logged per the CLAUDE.md no-silent-Wait rule.
func startTailscale(ctx context.Context, cfg tailscaleConfig) {
	if cfg.Disabled {
		if cfg.AuthKey != "" {
			log.Printf("[tailscale] disabled via TS_DISABLE / --tailscale-disable (auth key present but ignored)")
		}
		return
	}
	if cfg.AuthKey == "" {
		return
	}
	if _, err := exec.LookPath("tailscaled"); err != nil {
		log.Printf("[tailscale] TS_AUTHKEY is set but tailscaled binary not found in PATH: %v", err)
		return
	}
	if err := os.MkdirAll(cfg.StateDir, 0o700); err != nil {
		log.Printf("[tailscale] cannot create state dir %q: %v -- aborting tailnet join", cfg.StateDir, err)
		return
	}

	sockPath := filepath.Join(cfg.StateDir, "tailscaled.sock")
	statePath := filepath.Join(cfg.StateDir, "tailscaled.state")

	daemonArgs := []string{
		"--tun=userspace-networking",
		"--socket=" + sockPath,
		"--state=" + statePath,
	}
	daemon := exec.CommandContext(ctx, "tailscaled", daemonArgs...)
	daemon.Stdout = os.Stdout
	daemon.Stderr = os.Stderr
	if err := daemon.Start(); err != nil {
		log.Printf("[tailscale] start tailscaled failed: %v", err)
		return
	}
	pid := daemon.Process.Pid
	log.Printf("[tailscale] tailscaled started: pid=%d socket=%s state=%s", pid, sockPath, statePath)
	trackPid(pid)

	// Wait goroutine -- required by CLAUDE.md "no silent cmd.Wait()" rule.
	// Logs name, PID, and exit status so tailscaled crashes are visible.
	go func() {
		defer recoverGoroutine("tailscaled Wait")
		defer untrackPid(pid)
		err := daemon.Wait()
		log.Printf("[tailscale] tailscaled exited: pid=%d err=%v", pid, err)
	}()

	if err := waitForTailscaleSocket(ctx, sockPath, 30*time.Second); err != nil {
		log.Printf("[tailscale] tailscaled socket never appeared: %v", err)
		return
	}

	upArgs := []string{"--socket=" + sockPath, "up", "--authkey=" + cfg.AuthKey}
	if cfg.Hostname != "" {
		upArgs = append(upArgs, "--hostname="+cfg.Hostname)
	}
	upCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	up := exec.CommandContext(upCtx, "tailscale", upArgs...)
	up.Stdout = os.Stdout
	up.Stderr = os.Stderr
	if err := up.Run(); err != nil {
		log.Printf("[tailscale] `tailscale up` failed: %v", err)
		return
	}
	log.Printf("[tailscale] joined tailnet (hostname=%q)", cfg.Hostname)
}

// waitForTailscaleSocket polls for the tailscaled unix socket to appear.
// tailscaled creates it once the daemon is ready to accept `tailscale up`.
func waitForTailscaleSocket(ctx context.Context, path string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		if _, err := os.Stat(path); err == nil {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("socket %q did not appear within %s", path, timeout)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(200 * time.Millisecond):
		}
	}
}

// startLandingServer runs a tiny HTTP server on landingAddr advertising that
// swe-swe is reachable via Tailscale.  GET /health returns 200 for PaaS
// health probes; all other paths return a minimal HTML page.  Never exposes
// swe-swe's actual login form on this listener -- that's the whole point.
//
// If SWE_LANDING_DISABLE=1 is set, every path returns 200 OK with no body
// (PaaS health probes pass, but nothing about swe-swe leaks).
//
// The server's Shutdown is hooked to ctx so it stops cleanly when the main
// server shuts down.  Returns the http.Server for tests; callers may ignore.
func startLandingServer(ctx context.Context, landingAddr, sweListenAddr string, cfg tailscaleConfig) *http.Server {
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
		body := renderLandingHTML(sweListenAddr, cfg)
		mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
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
func renderLandingHTML(sweListenAddr string, cfg tailscaleConfig) string {
	learnMore := os.Getenv("SWE_LANDING_URL")
	if learnMore == "" {
		learnMore = "https://swe-swe.netlify.app"
	}
	swePort := strings.TrimPrefix(sweListenAddr, ":")
	if i := strings.LastIndex(swePort, ":"); i >= 0 {
		swePort = swePort[i+1:]
	}
	reach := ""
	if cfg.Hostname != "" {
		reach = fmt.Sprintf("<code>%s:%s</code>", htmlpkg.EscapeString(cfg.Hostname), htmlpkg.EscapeString(swePort))
	} else {
		reach = fmt.Sprintf("your tailnet hostname on port <code>%s</code>", htmlpkg.EscapeString(swePort))
	}
	return `<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<title>swe-swe</title>
<style>
body { font-family: system-ui, sans-serif; max-width: 40em; margin: 3em auto; padding: 0 1em; line-height: 1.5; color: #222; }
code { background: #f4f4f4; padding: 0.1em 0.3em; border-radius: 3px; }
a { color: #0366d6; }
</style>
</head>
<body>
<h1>swe-swe is running</h1>
<p>This public URL is a placeholder. Reach the real UI via Tailscale at ` + reach + `.</p>
<p>Learn more: <a href="` + htmlpkg.EscapeString(learnMore) + `">` + htmlpkg.EscapeString(learnMore) + `</a></p>
</body>
</html>
`
}
