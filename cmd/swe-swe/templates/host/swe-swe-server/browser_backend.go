package main

import (
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"strings"
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

// startRemoteAgentView is wired to the swe-swe/browser-backend allocation API
// in remoteAgentView (Phase 5d). Defined here so the dispatcher type-checks.
var startRemoteAgentView = func(sess *Session) (string, error) {
	return "", fmt.Errorf("remote agent-view backend %q not yet wired", agentViewBackend)
}

// stopRemoteAgentView mirrors startRemoteAgentView (Phase 5d).
var stopRemoteAgentView = func(sess *Session) {}

// browserProcs holds the four OS processes backing one isolated Agent View
// browser (Xvfb + chromium + x11vnc + websockify) plus its chromium profile
// dir. It is the shared core behind both the in-process session backend
// (local mode) and the standalone browser-backend service.
type browserProcs struct {
	pids    []int
	dataDir string
}

// startBrowserProcs launches the Agent View stack for an isolated instance
// identified by id, on the given X display and ports (cdpPort = chromium remote
// debugging; vncPort = websockify/noVNC; vncInternalPort = x11vnc raw). On any
// step failing, processes started so far are killed before returning.
func startBrowserProcs(id string, display, cdpPort, vncPort, vncInternalPort int) (*browserProcs, error) {
	b := &browserProcs{}
	displayStr := fmt.Sprintf(":%d", display)

	// 1. Xvfb on a unique display, Unix socket only (no TCP).
	xvfbCmd := exec.Command("Xvfb", displayStr, "-screen", "0", "1024x768x24", "-nolisten", "tcp")
	if err := xvfbCmd.Start(); err != nil {
		return nil, fmt.Errorf("failed to start Xvfb on display %s: %w", displayStr, err)
	}
	xvfbPID := xvfbCmd.Process.Pid
	trackPid(xvfbPID)
	b.pids = append(b.pids, xvfbPID)
	log.Printf("Started Xvfb on display %s (PID %d) for browser %s", displayStr, xvfbPID, id)
	go func() {
		defer recoverGoroutine(fmt.Sprintf("Xvfb wait (PID %d, browser %s)", xvfbPID, id))
		defer untrackPid(xvfbPID)
		if err := xvfbCmd.Wait(); err != nil {
			log.Printf("Xvfb exited with error (PID %d, browser %s): %v", xvfbPID, id, err)
		} else {
			log.Printf("Xvfb exited normally (PID %d, browser %s)", xvfbPID, id)
		}
	}()
	time.Sleep(500 * time.Millisecond)

	// 2. Chromium with remote debugging. Each instance gets its own
	// --user-data-dir to avoid Chrome's singleton profile lock (which would
	// make all but the first instance delegate to the first and exit).
	chromiumBinary := "chromium"
	if _, err := exec.LookPath("chromium"); err != nil {
		chromiumBinary = "chromium-browser" // fallback name on some distros
	}
	userDataDir := fmt.Sprintf("/tmp/chromium-session-%s", id)
	b.dataDir = userDataDir
	chromeCmd := exec.Command(chromiumBinary,
		"--no-sandbox",
		"--test-type",
		"--disable-gpu",
		"--disable-software-rasterizer",
		"--disable-dev-shm-usage",
		"--remote-debugging-address=0.0.0.0",
		fmt.Sprintf("--remote-debugging-port=%d", cdpPort),
		fmt.Sprintf("--user-data-dir=%s", userDataDir),
		"--remote-allow-origins=*",
		"--window-size=1024,768",
		"--start-maximized",
	)
	chromeCmd.Env = append(os.Environ(), fmt.Sprintf("DISPLAY=%s", displayStr))
	if err := chromeCmd.Start(); err != nil {
		b.stop()
		return nil, fmt.Errorf("failed to start Chromium on CDP port %d: %w", cdpPort, err)
	}
	chromePID := chromeCmd.Process.Pid
	trackPid(chromePID)
	b.pids = append(b.pids, chromePID)
	log.Printf("Started Chromium on CDP port %d, display %s (PID %d) for browser %s", cdpPort, displayStr, chromePID, id)
	go func() {
		defer recoverGoroutine(fmt.Sprintf("Chromium wait (PID %d, browser %s)", chromePID, id))
		defer untrackPid(chromePID)
		if err := chromeCmd.Wait(); err != nil {
			log.Printf("Chromium exited with error (PID %d, browser %s): %v", chromePID, id, err)
		} else {
			log.Printf("Chromium exited normally (PID %d, browser %s)", chromePID, id)
		}
	}()
	time.Sleep(1 * time.Second)

	// 3. x11vnc on an internal raw-VNC port consumed by noVNC.
	x11vncCmd := exec.Command("x11vnc",
		"-display", displayStr,
		"-forever",
		"-shared",
		"-nopw",
		"-rfbport", fmt.Sprintf("%d", vncInternalPort),
		"-xkb",
	)
	if err := x11vncCmd.Start(); err != nil {
		b.stop()
		return nil, fmt.Errorf("failed to start x11vnc on port %d: %w", vncInternalPort, err)
	}
	x11vncPID := x11vncCmd.Process.Pid
	trackPid(x11vncPID)
	b.pids = append(b.pids, x11vncPID)
	log.Printf("Started x11vnc on port %d, display %s (PID %d) for browser %s", vncInternalPort, displayStr, x11vncPID, id)
	go func() {
		defer recoverGoroutine(fmt.Sprintf("x11vnc wait (PID %d, browser %s)", x11vncPID, id))
		defer untrackPid(x11vncPID)
		if err := x11vncCmd.Wait(); err != nil {
			log.Printf("x11vnc exited with error (PID %d, browser %s): %v", x11vncPID, id, err)
		} else {
			log.Printf("x11vnc exited normally (PID %d, browser %s)", x11vncPID, id)
		}
	}()

	// 4. websockify (noVNC) bridging the WebSocket vncPort to raw vncInternalPort.
	noVNCCmd := exec.Command("websockify",
		"--web", "/usr/share/novnc",
		fmt.Sprintf("%d", vncPort),
		fmt.Sprintf("localhost:%d", vncInternalPort),
	)
	if err := noVNCCmd.Start(); err != nil {
		b.stop()
		return nil, fmt.Errorf("failed to start noVNC proxy on port %d: %w", vncPort, err)
	}
	noVNCPID := noVNCCmd.Process.Pid
	trackPid(noVNCPID)
	b.pids = append(b.pids, noVNCPID)
	log.Printf("Started noVNC proxy on port %d -> localhost:%d (PID %d) for browser %s", vncPort, vncInternalPort, noVNCPID, id)
	go func() {
		defer recoverGoroutine(fmt.Sprintf("noVNC wait (PID %d, browser %s)", noVNCPID, id))
		defer untrackPid(noVNCPID)
		if err := noVNCCmd.Wait(); err != nil {
			log.Printf("noVNC exited with error (PID %d, browser %s): %v", noVNCPID, id, err)
		} else {
			log.Printf("noVNC exited normally (PID %d, browser %s)", noVNCPID, id)
		}
	}()

	return b, nil
}

// stop kills all processes for this browser and removes its profile dir.
func (b *browserProcs) stop() {
	for _, pid := range b.pids {
		if err := syscall.Kill(pid, syscall.SIGKILL); err != nil {
			if !errors.Is(err, syscall.ESRCH) {
				log.Printf("Failed to kill browser process PID %d: %v", pid, err)
			}
		} else {
			log.Printf("[KILL] Killed browser process PID %d (server PID %d)", pid, os.Getpid())
		}
	}
	b.pids = nil
	if b.dataDir != "" {
		if err := os.RemoveAll(b.dataDir); err != nil {
			log.Printf("Failed to clean up browser data dir %s: %v", b.dataDir, err)
		}
		b.dataDir = ""
	}
}
