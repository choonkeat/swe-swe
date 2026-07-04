package main

import (
	"strings"
	"testing"
)

func TestParseKnownAgentSessionID_ClaudeSessionIDFlag(t *testing.T) {
	got := parseKnownAgentSessionID("claude", []string{"--session-id", "abc-def"})
	if got != "abc-def" {
		t.Errorf("got %q, want abc-def", got)
	}
}

func TestParseKnownAgentSessionID_ResumeFlag(t *testing.T) {
	got := parseKnownAgentSessionID("claude", []string{"--resume", "11111111-1111-1111-1111-111111111111"})
	if got != "11111111-1111-1111-1111-111111111111" {
		t.Errorf("got %q", got)
	}
}

func TestParseKnownAgentSessionID_CodexResumeSubcommand(t *testing.T) {
	id := "22222222-2222-2222-2222-222222222222"
	got := parseKnownAgentSessionID("codex", []string{"resume", id})
	if got != id {
		t.Errorf("got %q, want %s", got, id)
	}
}

func TestParseKnownAgentSessionID_CodexResumeThreadName(t *testing.T) {
	// Codex accepts thread names too; we must not confuse them with UUIDs.
	got := parseKnownAgentSessionID("codex", []string{"resume", "my-thread-name"})
	if got != "" {
		t.Errorf("got %q for non-UUID, want empty", got)
	}
}

func TestParseKnownAgentSessionID_NoFlag(t *testing.T) {
	got := parseKnownAgentSessionID("claude", []string{"--dangerously-skip-permissions"})
	if got != "" {
		t.Errorf("got %q, want empty", got)
	}
}

func TestInjectAgentSessionID_ClaudeAppendsFlag(t *testing.T) {
	args := []string{"--dangerously-skip-permissions"}
	newArgs, id := injectAgentSessionID("claude", args)
	if id == "" {
		t.Fatal("expected non-empty id")
	}
	if !looksLikeUUID(id) {
		t.Errorf("id %q is not a UUID", id)
	}
	// Verify the flag was appended with the same id.
	found := false
	for i := 0; i < len(newArgs)-1; i++ {
		if newArgs[i] == "--session-id" && newArgs[i+1] == id {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("--session-id %q not present in %v", id, newArgs)
	}
}

func TestInjectAgentSessionID_SkipsWhenContinueFlag(t *testing.T) {
	args := []string{"--continue"}
	newArgs, id := injectAgentSessionID("claude", args)
	if id != "" {
		t.Errorf("got id %q, want empty (--continue should suppress)", id)
	}
	if len(newArgs) != len(args) {
		t.Errorf("args changed unexpectedly: %v", newArgs)
	}
}

func TestInjectAgentSessionID_SkipsWhenResumeFlag(t *testing.T) {
	args := []string{"--resume", "some-uuid"}
	_, id := injectAgentSessionID("claude", args)
	if id != "" {
		t.Errorf("got id %q, want empty (--resume should suppress)", id)
	}
}

func TestInjectAgentSessionID_NoOpForCodex(t *testing.T) {
	// Codex has no fresh-session-id flag; should fall through to watch.
	_, id := injectAgentSessionID("codex", []string{})
	if id != "" {
		t.Errorf("got id %q, want empty", id)
	}
}

func TestInjectAgentSessionID_NoOpForPi(t *testing.T) {
	// Pi's --session behaviour on fresh ids is unverified; we leave it to
	// the watch path for now.
	_, id := injectAgentSessionID("pi", []string{})
	if id != "" {
		t.Errorf("got id %q, want empty", id)
	}
}

func TestExtractAgentSessionIDFromPath_Claude(t *testing.T) {
	got := extractAgentSessionIDFromPath("claude", "/home/x/.claude/projects/-workspace/aaaa-bbbb.jsonl")
	if got != "aaaa-bbbb" {
		t.Errorf("got %q", got)
	}
}

func TestExtractAgentSessionIDFromPath_Pi(t *testing.T) {
	got := extractAgentSessionIDFromPath("pi", "/home/x/.pi/agent/sessions/aaaa-bbbb.jsonl")
	if got != "aaaa-bbbb" {
		t.Errorf("got %q", got)
	}
}

func TestExtractAgentSessionIDFromPath_CodexRollout(t *testing.T) {
	got := extractAgentSessionIDFromPath("codex",
		"/home/x/.codex/sessions/2026/05/22/rollout-2026-05-22T10-15-30-11111111-1111-1111-1111-111111111111.jsonl")
	want := "11111111-1111-1111-1111-111111111111"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestExtractAgentSessionIDFromPath_CodexNonRolloutReturnsEmpty(t *testing.T) {
	got := extractAgentSessionIDFromPath("codex", "/home/x/.codex/sessions/random.jsonl")
	if got != "" {
		t.Errorf("got %q, want empty", got)
	}
}

func TestLooksLikeUUID(t *testing.T) {
	cases := map[string]bool{
		"11111111-1111-1111-1111-111111111111": true,
		"abcdef00-abcd-abcd-abcd-abcdef000000": true,
		"my-thread-name":                       false,
		"too-short":                            false,
		"11111111111111111111111111111111":     false, // no dashes
		"11111111-1111-1111-1111-11111111111X": false, // non-hex
	}
	for in, want := range cases {
		if got := looksLikeUUID(in); got != want {
			t.Errorf("looksLikeUUID(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestHasRestartFlag(t *testing.T) {
	cases := map[string]bool{
		"--continue":     true,
		"-c":             true,
		"--from-pr":      true,
		"--fork-session": true,
		"--unrelated":    false,
	}
	for flag, want := range cases {
		got := hasRestartFlag([]string{flag})
		if got != want {
			t.Errorf("hasRestartFlag(%q) = %v, want %v", flag, got, want)
		}
	}
}

func TestAgentSessionDir_HonorsHomeOverride(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("CLAUDE_HOME", tmp)
	got := agentSessionDir("claude", "/repos/example")
	if !strings.HasPrefix(got, tmp) {
		t.Errorf("got %q, want prefix %q", got, tmp)
	}
	if !strings.HasSuffix(got, "-repos-example") {
		t.Errorf("encoded workdir missing in %q", got)
	}
}

// TestAgentSessionDir_ReplacesDotsInWorkdir guards the bug where a workdir
// containing "." (e.g. a github.com-... repo path) was encoded with only "/"
// replaced, leaving the dot intact -- so it never matched Claude's actual
// ~/.claude/projects/<dir> folder name (which replaces "." with "-" too),
// and fork/resume could not locate the rollout.
func TestAgentSessionDir_ReplacesDotsInWorkdir(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("CLAUDE_HOME", tmp)
	got := agentSessionDir("claude", "/repos/github.com-choonkeat-x/workspace")
	want := tmp + "/projects/-repos-github-com-choonkeat-x-workspace"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}
