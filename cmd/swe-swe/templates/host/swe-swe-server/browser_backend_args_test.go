package main

// Chromium launch-arg construction: tunnel-mode allocations pass empty
// hostResolverRules, which MUST result in no --host-resolver-rules flag at
// all (chromium then resolves localhost/*.lvh.me to its own loopback, where
// the reverse tunnel binds).

import (
	"strings"
	"testing"
)

func TestBuildChromiumArgsResolverRules(t *testing.T) {
	withRules := buildChromiumArgs(6020, "/tmp/profile", "MAP localhost 10.0.0.9, MAP *.localhost 10.0.0.9")
	found := false
	for _, a := range withRules {
		if a == "--host-resolver-rules=MAP localhost 10.0.0.9, MAP *.localhost 10.0.0.9" {
			found = true
		}
	}
	if !found {
		t.Errorf("direct mode args missing resolver rules: %v", withRules)
	}

	without := buildChromiumArgs(6020, "/tmp/profile", "")
	for _, a := range without {
		if strings.Contains(a, "--host-resolver-rules") {
			t.Errorf("tunnel mode (empty rules) still passes %q", a)
		}
	}

	// Core args present in both shapes.
	for _, args := range [][]string{withRules, without} {
		var hasCDP, hasProfile bool
		for _, a := range args {
			if a == "--remote-debugging-port=6020" {
				hasCDP = true
			}
			if a == "--user-data-dir=/tmp/profile" {
				hasProfile = true
			}
		}
		if !hasCDP || !hasProfile {
			t.Errorf("args missing core flags: %v", args)
		}
	}
}
