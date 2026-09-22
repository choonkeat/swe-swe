package main

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"sync/atomic"
	"syscall"
	"time"
)

// Agent View (the agent-drivable Chromium shown over VNC) is the only tab that
// needs a heavy, non-bundleable stack (Xvfb/chromium/x11vnc/websockify). The
// backend selects where that stack runs:
//
//	"local"  -- spawn the stack in-process per session (default; current behavior)
//	"off"    -- Agent View disabled; the tab is hidden, the other tabs unaffected
//	<url>    -- remote: offload to a swe-swe/browser-backend service
//
// Set via -agent-view or SWE_AGENT_VIEW. A lean dockerless host with no display
// stack runs "local" but reports the tab unavailable rather than 500ing.
var agentViewBackend = "local"

// browserBackendToken is the bearer token sent to a remote backend so a
// public box is not an open browser relay. From SWE_BROWSER_BACKEND_TOKEN.
var browserBackendToken = ""

// lookPath is indirected so tests can simulate hosts with or without the
// display stack installed.
var lookPath = exec.LookPath

// browserStackAvailable reports whether the local display stack is installed.
// chromium ships under two common binary names; either satisfies it.
func browserStackAvailable() bool {
	if _, err := lookPath("Xvfb"); err != nil {
		return false
	}
	chromiumOK := false
	for _, name := range []string{"chromium", "chromium-browser"} {
		if _, err := lookPath(name); err == nil {
			chromiumOK = true
			break
		}
	}
	if !chromiumOK {
		return false
	}
	if _, err := lookPath("x11vnc"); err != nil {
		return false
	}
	if _, err := lookPath("websockify"); err != nil {
		return false
	}
	return true
}

// agentViewRemote reports whether the configured backend is a remote URL.
func agentViewRemote() bool {
	return strings.HasPrefix(agentViewBackend, "http://") ||
		strings.HasPrefix(agentViewBackend, "https://")
}

// agentViewAvailable reports whether the Agent View tab can be served, so the
// UI can hide it instead of showing a broken "Starting browser..." placeholder.
// Remote mode trusts the backend; local mode requires the stack on this host.
func agentViewAvailable() bool {
	switch {
	case agentViewBackend == "off" || agentViewBackend == "":
		return false
	case agentViewRemote():
		return true
	default: // local
		return browserStackAvailable()
	}
}

// resolveAgentViewBackend applies flag -> env -> default. Called from main()
// after flag.Parse(). flagWasSet mirrors flagPassed("agent-view").
func resolveAgentViewBackend(flagVal string, flagWasSet bool) {
	v := flagVal
	if !flagWasSet {
		if env := os.Getenv("SWE_AGENT_VIEW"); env != "" {
			v = env
		}
	}
	if v == "" {
		v = "local"
	}
	agentViewBackend = v
	browserBackendToken = os.Getenv("SWE_BROWSER_BACKEND_TOKEN")
}

// startSessionAgentView brings up the Agent View browser for a session via the
// configured backend and reports a status string for the start API:
//
//	"unavailable"     -- backend off / local stack missing (not an error)
//	"started"         -- browser is up
//	(err non-nil)     -- a real failure the caller should surface as 500
func startSessionAgentView(sess *Session) (status string, err error) {
	if !agentViewAvailable() {
		log.Printf("Agent View unavailable (backend=%q) for session %s -- tab hidden", agentViewBackend, sess.UUID)
		return "unavailable", nil
	}
	if agentViewRemote() {
		return startRemoteAgentView(sess)
	}
	if err := startSessionBrowser(sess); err != nil {
		return "", err
	}
	return "started", nil
}

// stopSessionAgentView tears down whichever backend a session used.
func stopSessionAgentView(sess *Session) {
	if sess.RemoteBrowserID != "" {
		stopRemoteAgentView(sess)
		return
	}
	stopSessionBrowser(sess)
}

// startRemoteAgentView / stopRemoteAgentView are implemented in
// browser_backend_remote.go (Phase 5d).

// browserProcs holds the four OS processes backing one isolated Agent View
// browser (Xvfb + chromium + x11vnc + websockify) plus its chromium profile
// dir and the CDP forwarder. It is the shared core behind both the in-process
// session backend (local mode) and the standalone browser-backend service.
type browserProcs struct {
	pids    []int
	dataDir string
	cdpSrv  *http.Server
	// cdpLastActive / cdpInFlight track REAL agent use of this browser: every
	// request through the CDP forwarder is the agent driving chromium. The
	// browser-backend's idle reaper consults them so a browser someone is
	// actually using is never reaped (browser_backend_service.go). In-flight
	// matters as much as the timestamp: Playwright holds ONE long-lived CDP
	// websocket for the whole session, so a busy agent can go a long time
	// between new requests while never being idle.
	cdpLastActive atomic.Int64 // unix nanos; 0 = never
	cdpInFlight   atomic.Int64
}

// markCDPActivity stamps "the agent touched this browser just now".
func (b *browserProcs) markCDPActivity() {
	b.cdpLastActive.Store(time.Now().UnixNano())
}

// lastCDPActivity reports when the agent last drove this browser, and whether
// a CDP request/websocket is open right now (which outranks any timestamp).
func (b *browserProcs) lastCDPActivity() (t time.Time, busy bool) {
	if ns := b.cdpLastActive.Load(); ns != 0 {
		t = time.Unix(0, ns)
	}
	return t, b.cdpInFlight.Load() > 0
}

// cdpActivityTracker wraps the CDP forwarder so every request -- including the
// long-lived websocket, whose handler stays in ServeHTTP for its whole life --
// counts as use.
func (b *browserProcs) cdpActivityTracker(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b.markCDPActivity()
		b.cdpInFlight.Add(1)
		defer func() {
			b.cdpInFlight.Add(-1)
			b.markCDPActivity()
		}()
		next.ServeHTTP(w, r)
	})
}

// buildChromiumArgs assembles the chromium launch argv. hostResolverRules ==
// "" (local mode, or a TUNNEL-mode remote allocation) means NO
// --host-resolver-rules flag at all: loopback hostnames then resolve to
// chromium's own loopback, which is exactly where the reverse tunnel binds.
func buildChromiumArgs(cdpInternalPort int, userDataDir, hostResolverRules string) []string {
	args := []string{
		"--no-sandbox",
		"--test-type",
		"--disable-gpu",
		"--disable-software-rasterizer",
		"--disable-dev-shm-usage",
		// Loopback-only regardless of flags (headful chromium); the CDP
		// forwarder exposes it on cdpPort.
		fmt.Sprintf("--remote-debugging-port=%d", cdpInternalPort),
		fmt.Sprintf("--user-data-dir=%s", userDataDir),
		"--remote-allow-origins=*",
		"--window-size=1024,768",
		"--start-maximized",
	}
	if hostResolverRules != "" {
		args = append(args, "--host-resolver-rules="+hostResolverRules)
	}
	return args
}

// startBrowserProcs launches the Agent View stack for an isolated instance
// identified by id, on the given X display and ports (cdpPort = chromium remote
// debugging as consumed by clients; cdpInternalPort = where chromium actually
// listens, see below; vncPort = websockify/noVNC; vncInternalPort = x11vnc
// raw). On any step failing, processes started so far are killed before
// returning.
//
// Headful chromium (ours runs under Xvfb) IGNORES --remote-debugging-address
// and always binds CDP to loopback -- so a remote backend's advertised CDP
// port was never reachable cross-host. Chromium therefore listens on
// 127.0.0.1:cdpInternalPort and a reverse proxy serves 0.0.0.0:cdpPort,
// rewriting the /json discovery bodies so debugger URLs keep pointing at
// cdpPort (mirrors the x11vnc internal / websockify external VNC split).
//
// hostResolverRules, when non-empty, is passed verbatim as chromium's
// --host-resolver-rules. A REMOTE backend uses it to map loopback-style dev
// hostnames (localhost, *.lvh.me, ...) back to the swe-swe host so pages the
// agent opens there reach the dev server, not this box -- see
// buildLoopbackResolverRules. IP-literal URLs (127.0.0.1) bypass the resolver
// and are out of scope. Local mode passes "".
// diedDuringStartup reports whether a just-started process has already exited,
// reading the one-shot channel its Wait goroutine writes to. Non-blocking: an
// empty channel means the process is still alive, which is the healthy case.
// A clean exit (nil from Wait) is still a death here -- these processes are
// supposed to outlive startup -- so it maps to an explicit error.
func diedDuringStartup(died <-chan error) error {
	select {
	case err := <-died:
		if err == nil {
			return fmt.Errorf("exited with status 0")
		}
		return err
	default:
		return nil
	}
}

// Tuning for the supervised VNC-side processes (x11vnc, websockify). A
// replacement browser reuses the port slot of the browser it replaces, often
// within ~2s of that one being killed, so the first start can lose the race
// for the port and exit. These were unsupervised: the Agent View pane then
// stayed blank for the rest of the session while chromium and CDP looked
// perfectly healthy, with nothing in the log to say why.
var (
	vncStartAttempts    = 4
	vncStartSettle      = 300 * time.Millisecond
	vncStartTimeout     = 5 * time.Second
	superviseRetryDelay = 500 * time.Millisecond
	healthPollInterval  = 100 * time.Millisecond
	// Grace period between "the port answered" and the final liveness check:
	// a process that binds and then exits (python websockify takes ~1s to get
	// that far) must not be mistaken for a healthy one.
	healthConfirmDelay = 250 * time.Millisecond
	// How long a start attempt waits for its port to be released by whatever
	// held it before. See waitPortFree.
	portFreeTimeout = 2 * time.Second
)

// killBrowserProc kills a browser stack process AND its process group. The
// group matters: python websockify forks a child that inherits the listening
// VNC socket and survives a SIGKILL aimed at the parent alone. That orphan
// then holds the VNC port, so the next browser allocated into the same slot
// could not bind it -- the Agent View pane stayed blank while chromium and CDP
// looked healthy. Every process here is started with Setpgid, so pgid == pid.
func killBrowserProc(pid int, what string) {
	if pid <= 0 {
		return
	}
	if err := syscall.Kill(-pid, syscall.SIGKILL); err != nil && !errors.Is(err, syscall.ESRCH) {
		log.Printf("Failed to kill %s process group PGID %d: %v", what, pid, err)
	}
	if err := syscall.Kill(pid, syscall.SIGKILL); err != nil {
		if !errors.Is(err, syscall.ESRCH) {
			log.Printf("Failed to kill %s PID %d: %v", what, pid, err)
		}
		return
	}
	log.Printf("[KILL] Killed %s PID %d (+group) (server PID %d)", what, pid, os.Getpid())
}

// waitPortFree blocks until nothing is listening on addr. A start attempt runs
// this first so a leftover listener can never be mistaken for the process we
// just launched: the leftover answers the readiness probe while our own
// process is dying on "address already in use".
func waitPortFree(addr string, timeout time.Duration) error {
	start := time.Now()
	for {
		conn, err := net.DialTimeout("tcp", addr, 200*time.Millisecond)
		if err != nil {
			return nil
		}
		conn.Close()
		if time.Since(start) >= timeout {
			return fmt.Errorf("%s is still held by another process after %s", addr, timeout)
		}
		time.Sleep(healthPollInterval)
	}
}

// waitProcHealthy blocks until a just-started process is demonstrably usable,
// died, or ran out of time. Usable means: still running AND, when readyAddr is
// non-empty, accepting connections there -- a process that is alive but never
// bound its port is precisely the blank-pane failure, so aliveness alone is
// not enough.
func waitProcHealthy(died <-chan error, readyAddr string, minSettle, timeout time.Duration) error {
	start := time.Now()
	for {
		if err := diedDuringStartup(died); err != nil {
			return err
		}
		if time.Since(start) >= minSettle {
			ready := readyAddr == ""
			if !ready {
				if conn, err := net.DialTimeout("tcp", readyAddr, 200*time.Millisecond); err == nil {
					conn.Close()
					ready = true
				}
			}
			if ready {
				time.Sleep(healthConfirmDelay)
				if err := diedDuringStartup(died); err != nil {
					return err
				}
				return nil
			}
		}
		if time.Since(start) >= timeout {
			if readyAddr == "" {
				return fmt.Errorf("still not up after %s", timeout)
			}
			return fmt.Errorf("nothing listening on %s after %s", readyAddr, timeout)
		}
		time.Sleep(healthPollInterval)
	}
}

// startSupervisedProc starts one of the browser stack's long-lived helper
// processes, confirms it came up (see waitProcHealthy), and retries when it
// did not. newCmd builds a fresh *exec.Cmd per attempt. Every attempt and
// every exit reason is logged; the returned error names the process so an
// allocation failure is self-explaining rather than a silently degraded
// browser.
func (b *browserProcs) startSupervisedProc(name, id string, newCmd func() *exec.Cmd, readyAddr string, attempts int, minSettle, timeout time.Duration) error {
	if attempts < 1 {
		attempts = 1
	}
	var lastErr error
	for attempt := 1; attempt <= attempts; attempt++ {
		if attempt > 1 {
			time.Sleep(superviseRetryDelay)
		}
		if readyAddr != "" {
			if err := waitPortFree(readyAddr, portFreeTimeout); err != nil {
				lastErr = err
				log.Printf("%s cannot start for browser %s (attempt %d/%d): %v", name, id, attempt, attempts, err)
				continue
			}
		}
		cmd := newCmd()
		// Own process group, so killBrowserProc can take the whole tree down.
		cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
		if err := cmd.Start(); err != nil {
			lastErr = err
			log.Printf("Failed to start %s for browser %s (attempt %d/%d): %v", name, id, attempt, attempts, err)
			continue
		}
		pid := cmd.Process.Pid
		trackPid(pid)
		b.pids = append(b.pids, pid)
		died := make(chan error, 1)
		go func() {
			defer recoverGoroutine(fmt.Sprintf("%s wait (PID %d, browser %s)", name, pid, id))
			defer untrackPid(pid)
			err := cmd.Wait()
			died <- err
			if err != nil {
				log.Printf("%s exited with error (PID %d, browser %s): %v", name, pid, id, err)
			} else {
				log.Printf("%s exited normally (PID %d, browser %s)", name, pid, id)
			}
		}()
		if err := waitProcHealthy(died, readyAddr, minSettle, timeout); err != nil {
			lastErr = err
			log.Printf("%s did not come up (PID %d, browser %s, attempt %d/%d): %v", name, pid, id, attempt, attempts, err)
			// Kill an alive-but-unusable attempt so the retry gets the port
			// back instead of competing with its own leftover.
			killBrowserProc(pid, name)
			continue
		}
		log.Printf("Started %s (PID %d) for browser %s", name, pid, id)
		return nil
	}
	return fmt.Errorf("%s never came up for browser %s after %d attempt(s): %w", name, id, attempts, lastErr)
}

func startBrowserProcs(id string, display, cdpPort, cdpInternalPort, vncPort, vncInternalPort int, hostResolverRules string) (*browserProcs, error) {
	b := &browserProcs{}
	displayStr := fmt.Sprintf(":%d", display)

	// 1. Xvfb on a unique display, Unix socket only (no TCP).
	xvfbCmd := exec.Command("Xvfb", displayStr, "-screen", "0", "1024x768x24", "-nolisten", "tcp")
	// Own process group: see killBrowserProc.
	xvfbCmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := xvfbCmd.Start(); err != nil {
		return nil, fmt.Errorf("failed to start Xvfb on display %s: %w", displayStr, err)
	}
	xvfbPID := xvfbCmd.Process.Pid
	trackPid(xvfbPID)
	b.pids = append(b.pids, xvfbPID)
	log.Printf("Started Xvfb on display %s (PID %d) for browser %s", displayStr, xvfbPID, id)
	xvfbDied := make(chan error, 1)
	go func() {
		defer recoverGoroutine(fmt.Sprintf("Xvfb wait (PID %d, browser %s)", xvfbPID, id))
		defer untrackPid(xvfbPID)
		err := xvfbCmd.Wait()
		xvfbDied <- err
		if err != nil {
			log.Printf("Xvfb exited with error (PID %d, browser %s): %v", xvfbPID, id, err)
		} else {
			log.Printf("Xvfb exited normally (PID %d, browser %s)", xvfbPID, id)
		}
	}()
	time.Sleep(500 * time.Millisecond)
	// An Xvfb that is already gone means the display is unusable (stale
	// /tmp/.X<n>-lock is the usual cause). Fail the allocation here: letting
	// it through hands the caller a session whose CDP port never answers.
	if err := diedDuringStartup(xvfbDied); err != nil {
		b.stop()
		return nil, fmt.Errorf("Xvfb on display %s exited immediately (%v) -- stale X lock or display in use", displayStr, err)
	}

	// 2. Chromium with remote debugging. Each instance gets its own
	// --user-data-dir to avoid Chrome's singleton profile lock (which would
	// make all but the first instance delegate to the first and exit).
	chromiumBinary := "chromium"
	if _, err := exec.LookPath("chromium"); err != nil {
		chromiumBinary = "chromium-browser" // fallback name on some distros
	}
	userDataDir := fmt.Sprintf("/tmp/chromium-session-%s", id)
	b.dataDir = userDataDir
	chromeCmd := exec.Command(chromiumBinary, buildChromiumArgs(cdpInternalPort, userDataDir, hostResolverRules)...)
	chromeCmd.Env = append(os.Environ(), fmt.Sprintf("DISPLAY=%s", displayStr))
	// Own process group: chromium's zygote and renderer children must die with
	// it, not linger holding the profile dir. See killBrowserProc.
	chromeCmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := chromeCmd.Start(); err != nil {
		b.stop()
		return nil, fmt.Errorf("failed to start Chromium on CDP port %d: %w", cdpPort, err)
	}
	chromePID := chromeCmd.Process.Pid
	trackPid(chromePID)
	b.pids = append(b.pids, chromePID)
	log.Printf("Started Chromium on CDP port %d, display %s (PID %d) for browser %s", cdpPort, displayStr, chromePID, id)
	chromeDied := make(chan error, 1)
	go func() {
		defer recoverGoroutine(fmt.Sprintf("Chromium wait (PID %d, browser %s)", chromePID, id))
		defer untrackPid(chromePID)
		err := chromeCmd.Wait()
		chromeDied <- err
		if err != nil {
			log.Printf("Chromium exited with error (PID %d, browser %s): %v", chromePID, id, err)
		} else {
			log.Printf("Chromium exited normally (PID %d, browser %s)", chromePID, id)
		}
	}()
	time.Sleep(1 * time.Second)
	// Same reasoning as Xvfb: a chromium that is already gone leaves the CDP
	// port dead, so refuse the allocation instead of advertising it.
	if err := diedDuringStartup(chromeDied); err != nil {
		b.stop()
		return nil, fmt.Errorf("Chromium on CDP port %d exited immediately (%v) -- display %s unusable or profile locked", cdpPort, err, displayStr)
	}

	// 2b. CDP forwarder: expose chromium's loopback-only CDP on cdpPort for
	// all interfaces. A reverse proxy (not a raw TCP splice) so the /json*
	// discovery bodies can keep advertising cdpPort -- downstream consumers
	// (playwright MCP, the remote client's own rewriting proxy) never see the
	// internal port. httputil.ReverseProxy passes the CDP websocket upgrade
	// through.
	internalCDP := fmt.Sprintf("127.0.0.1:%d", cdpInternalPort)
	externalCDP := fmt.Sprintf("127.0.0.1:%d", cdpPort)
	cdpTarget := &url.URL{Scheme: "http", Host: internalCDP}
	cdpProxy := httputil.NewSingleHostReverseProxy(cdpTarget)
	cdpProxy.Director = func(req *http.Request) {
		req.URL.Scheme = "http"
		req.URL.Host = internalCDP
		req.Host = internalCDP
	}
	cdpProxy.ModifyResponse = func(resp *http.Response) error {
		if !strings.Contains(resp.Header.Get("Content-Type"), "application/json") {
			return nil
		}
		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			return err
		}
		body = rewriteCDPHosts(body, internalCDP, externalCDP)
		resp.Body = io.NopCloser(bytes.NewReader(body))
		resp.ContentLength = int64(len(body))
		resp.Header.Set("Content-Length", fmt.Sprintf("%d", len(body)))
		return nil
	}
	cdpLn, err := net.Listen("tcp", fmt.Sprintf(":%d", cdpPort))
	if err != nil {
		b.stop()
		return nil, fmt.Errorf("CDP forwarder listen on %d: %w", cdpPort, err)
	}
	b.cdpSrv = &http.Server{Handler: b.cdpActivityTracker(cdpProxy)}
	go func() {
		defer recoverGoroutine(fmt.Sprintf("CDP forwarder for browser %s", id))
		if err := b.cdpSrv.Serve(cdpLn); err != nil && err != http.ErrServerClosed {
			log.Printf("CDP forwarder error (browser %s): %v", id, err)
		}
	}()
	log.Printf("Started CDP forwarder :%d -> %s for browser %s", cdpPort, internalCDP, id)

	// 3. x11vnc on an internal raw-VNC port consumed by noVNC. Supervised:
	// see startSupervisedProc.
	if err := b.startSupervisedProc(
		fmt.Sprintf("x11vnc on port %d, display %s", vncInternalPort, displayStr),
		id,
		func() *exec.Cmd {
			return exec.Command("x11vnc",
				"-display", displayStr,
				"-forever",
				"-shared",
				"-nopw",
				"-rfbport", fmt.Sprintf("%d", vncInternalPort),
				"-xkb",
			)
		},
		fmt.Sprintf("127.0.0.1:%d", vncInternalPort),
		vncStartAttempts, vncStartSettle, vncStartTimeout,
	); err != nil {
		b.stop()
		return nil, err
	}

	// 4. websockify (noVNC) bridging the WebSocket vncPort to raw
	// vncInternalPort. Supervised for the same reason as x11vnc.
	if err := b.startSupervisedProc(
		fmt.Sprintf("noVNC proxy on port %d -> localhost:%d", vncPort, vncInternalPort),
		id,
		func() *exec.Cmd {
			return exec.Command("websockify",
				"--web", "/usr/share/novnc",
				fmt.Sprintf("%d", vncPort),
				fmt.Sprintf("localhost:%d", vncInternalPort),
			)
		},
		fmt.Sprintf("127.0.0.1:%d", vncPort),
		vncStartAttempts, vncStartSettle, vncStartTimeout,
	); err != nil {
		b.stop()
		return nil, err
	}

	return b, nil
}

// stop kills all processes for this browser and removes its profile dir.
func (b *browserProcs) stop() {
	if b.cdpSrv != nil {
		b.cdpSrv.Close()
		b.cdpSrv = nil
	}
	for _, pid := range b.pids {
		killBrowserProc(pid, "browser process")
	}
	b.pids = nil
	if b.dataDir != "" {
		if err := os.RemoveAll(b.dataDir); err != nil {
			log.Printf("Failed to clean up browser data dir %s: %v", b.dataDir, err)
		}
		b.dataDir = ""
	}
}
