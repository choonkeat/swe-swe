package main

import (
	"os/exec"
	"strings"
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

// When the tab cannot work, the UI explains why instead of hiding it, so the
// server must tell "switched off" from "programs missing" and name exactly
// which programs are missing (chromium counts once, under either name).
func TestAgentViewUnavailableReason(t *testing.T) {
	origLook, origBackend := lookPath, agentViewBackend
	defer func() { lookPath, agentViewBackend = origLook, origBackend }()

	without := func(absent ...string) func(string) (string, error) {
		return func(n string) (string, error) {
			for _, a := range absent {
				if n == a {
					return "", exec.ErrNotFound
				}
			}
			return "/usr/bin/" + n, nil
		}
	}

	cases := []struct {
		name        string
		backend     string
		absent      []string
		wantReason  string
		wantMissing string
	}{
		{"local, all present", "local", nil, "", ""},
		{"off", "off", nil, "off", ""},
		{"off wins over missing programs", "off", []string{"Xvfb"}, "off", ""},
		{"remote ignores the local stack", "http://box:9333", []string{"Xvfb", "chromium", "chromium-browser"}, "", ""},
		{"one missing", "local", []string{"x11vnc"}, "missing", "x11vnc"},
		{"chromium-browser alone is enough", "local", []string{"chromium"}, "", ""},
		{"no chromium under either name", "local", []string{"chromium", "chromium-browser", "websockify"}, "missing", "chromium,websockify"},
		{"all missing, in a fixed order", "local", []string{"websockify", "x11vnc", "chromium", "chromium-browser", "Xvfb"}, "missing", "Xvfb,chromium,x11vnc,websockify"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			lookPath, agentViewBackend = without(c.absent...), c.backend
			if got := agentViewUnavailableReason(); got != c.wantReason {
				t.Errorf("reason = %q, want %q", got, c.wantReason)
			}
			if got := strings.Join(missingBrowserPrograms(), ","); c.backend == "local" && got != c.wantMissing {
				t.Errorf("missing = %q, want %q", got, c.wantMissing)
			}
			if got, want := agentViewAvailable(), c.wantReason == ""; got != want {
				t.Errorf("agentViewAvailable = %v, want %v (must agree with the reason)", got, want)
			}
		})
	}
}

// A failed browser/start must reach the tab as a reason, or it says
// "Starting browser..." forever. Remote failures name the backend address
// (host:port only -- never the scheme's credentials); local ones cannot.
func TestSessionAgentViewStatusAfterFailedStart(t *testing.T) {
	origLook, origBackend := lookPath, agentViewBackend
	defer func() { lookPath, agentViewBackend = origLook, origBackend }()
	lookPath = func(n string) (string, error) { return "/usr/bin/" + n, nil }

	cases := []struct {
		name        string
		backend     string
		failed      bool
		wantReason  string
		wantAddress string
	}{
		{"remote, not started yet", "http://box:9333", false, "", ""},
		{"remote, start failed", "http://user:pw@box:9333/path", true, "unreachable", "box:9333"},
		{"local, start failed", "local", true, "failed", ""},
		{"off wins over a failed start", "off", true, "off", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			agentViewBackend = c.backend
			s := &Session{agentViewStartFailed: c.failed}
			reason, missing, address := s.agentViewStatus()
			if reason != c.wantReason || address != c.wantAddress {
				t.Errorf("status = (%q, %q), want (%q, %q)", reason, address, c.wantReason, c.wantAddress)
			}
			if missing == nil {
				t.Error("missing must be an empty list, not null, in the JSON")
			}
		})
	}
}
