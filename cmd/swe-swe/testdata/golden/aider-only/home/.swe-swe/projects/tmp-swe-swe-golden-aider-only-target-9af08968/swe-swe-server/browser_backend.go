package main

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"strings"
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
