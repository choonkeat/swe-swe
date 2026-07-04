package main

import (
	"strings"
	"testing"
)

func TestFirstNonEmpty(t *testing.T) {
	if got := firstNonEmpty("", "", "x", "y"); got != "x" {
		t.Errorf("firstNonEmpty = %q, want x", got)
	}
	if got := firstNonEmpty("", ""); got != "" {
		t.Errorf("firstNonEmpty(empties) = %q, want empty", got)
	}
}

// buildSessionEnv must derive BROWSER + the session PATH prefixes from the
// configurable sweHomeDir / workspaceDir rather than hardcoded container
// paths, so a dockerless host run wires the dumped bin/ and project dirs.
func TestBuildSessionEnvUsesConfiguredPaths(t *testing.T) {
	oldHome, oldWS := sweHomeDir, workspaceDir
	defer func() { sweHomeDir, workspaceDir = oldHome, oldWS }()
	sweHomeDir = "/tmp/sweh/.swe-swe"
	workspaceDir = "/tmp/ws"

	var browser, path string
	for _, e := range buildSessionEnv(SessionEnvParams{}) {
		if strings.HasPrefix(e, "BROWSER=") {
			browser = strings.TrimPrefix(e, "BROWSER=")
		}
		if strings.HasPrefix(e, "PATH=") {
			path = strings.TrimPrefix(e, "PATH=")
		}
	}

	if want := "/tmp/sweh/.swe-swe/bin/swe-swe-open"; browser != want {
		t.Errorf("BROWSER = %q, want %q", browser, want)
	}
	if !strings.Contains(path, "/tmp/sweh/.swe-swe/bin") {
		t.Errorf("PATH %q missing sweHome bin dir", path)
	}
	if !strings.Contains(path, "/tmp/ws/.swe-swe/proxy") {
		t.Errorf("PATH %q missing workspace proxy dir", path)
	}
}
