package main

import (
	"os/exec"
	"testing"
)

// Agent View availability drives whether the UI offers the tab at all. Local
// mode needs the display stack installed; remote mode trusts the backend; off
// is always unavailable. Missing-stack must report unavailable rather than
// letting the server attempt a doomed spawn and 500.
func TestAgentViewAvailable(t *testing.T) {
	origLook, origBackend := lookPath, agentViewBackend
	defer func() { lookPath, agentViewBackend = origLook, origBackend }()

	allPresent := func(string) (string, error) { return "/usr/bin/x", nil }
	missing := func(want string) func(string) (string, error) {
		return func(n string) (string, error) {
			if n == want {
				return "", exec.ErrNotFound
			}
			return "/usr/bin/" + n, nil
		}
	}

	// local + full stack -> available
	lookPath, agentViewBackend = allPresent, "local"
	if !agentViewAvailable() {
		t.Error("local with full stack: want available")
	}

	// local + missing Xvfb -> unavailable
	lookPath = missing("Xvfb")
	if browserStackAvailable() || agentViewAvailable() {
		t.Error("local missing Xvfb: want unavailable")
	}

	// chromium-browser fallback satisfies the chromium requirement
	lookPath = missing("chromium")
	if !browserStackAvailable() {
		t.Error("chromium-browser fallback should satisfy the chromium requirement")
	}

	// off -> never available
	lookPath, agentViewBackend = allPresent, "off"
	if agentViewAvailable() {
		t.Error("off: want unavailable")
	}

	// remote URL -> available regardless of local stack
	lookPath, agentViewBackend = missing("chromium-browser"), "https://box:9333"
	if !agentViewRemote() {
		t.Error("https URL should be detected as remote")
	}
	if !agentViewAvailable() {
		t.Error("remote: want available even with no local stack")
	}
}

func TestResolveAgentViewBackend(t *testing.T) {
	origBackend := agentViewBackend
	defer func() { agentViewBackend = origBackend }()

	// Isolate from the ambient environment: dev containers now export
	// SWE_AGENT_VIEW (compose passthrough), which would leak into the
	// empty-flag cases below. Empty string == unset for Getenv checks.
	t.Setenv("SWE_AGENT_VIEW", "")

	// empty -> defaults to local
	resolveAgentViewBackend("", false)
	if agentViewBackend != "local" {
		t.Errorf("empty -> %q, want local", agentViewBackend)
	}
	// explicit flag wins
	resolveAgentViewBackend("off", true)
	if agentViewBackend != "off" {
		t.Errorf("flag off -> %q, want off", agentViewBackend)
	}
	// env applies only when the flag was not passed
	t.Setenv("SWE_AGENT_VIEW", "https://box:9333")
	resolveAgentViewBackend("local", false)
	if agentViewBackend != "https://box:9333" {
		t.Errorf("env -> %q, want the env URL", agentViewBackend)
	}
	resolveAgentViewBackend("local", true)
	if agentViewBackend != "local" {
		t.Errorf("flag passed should ignore env -> %q, want local", agentViewBackend)
	}
}
