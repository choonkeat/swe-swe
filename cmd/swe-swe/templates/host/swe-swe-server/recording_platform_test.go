package main

import (
	"strings"
	"testing"
)

// PTY recording wraps the session in `script`. util-linux (Linux) supports the
// separate timing/input/output files for timed playback; BSD/macOS `script`
// does not, so on darwin we fall back to a plain combined-output recording so
// sessions still start.
func TestScriptWrapperCommand(t *testing.T) {
	log, timing, input, cmd := "/r/s.log", "/r/s.timing", "/r/s.input", "claude --foo"

	linux := scriptWrapperCommand("linux", log, timing, input, cmd)
	for _, want := range []string{"-f", "-T " + `"/r/s.timing"`, "-I " + `"/r/s.input"`, "-O " + `"/r/s.log"`, "-c " + `"claude --foo"`} {
		if !strings.Contains(linux, want) {
			t.Errorf("linux script command missing %q in: %s", want, linux)
		}
	}

	mac := scriptWrapperCommand("darwin", log, timing, input, cmd)
	// BSD form: no util-linux-only flags; records to the log file; runs via bash -c.
	for _, bad := range []string{"-f ", "-T ", "-I ", "-O "} {
		if strings.Contains(mac, bad) {
			t.Errorf("darwin script command must not use util-linux flag %q: %s", bad, mac)
		}
	}
	if !strings.Contains(mac, `"/r/s.log"`) {
		t.Errorf("darwin script command should record to the log file: %s", mac)
	}
	if !strings.Contains(mac, `/bin/bash -c "claude --foo"`) {
		t.Errorf("darwin script command should run the command via bash -c: %s", mac)
	}
}
