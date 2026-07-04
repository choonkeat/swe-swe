package main

import "testing"

// envValue returns (value, present) for the LAST occurrence of key in a
// KEY=VALUE slice, matching exec semantics where the last assignment wins.
func envValue(env []string, key string) (string, bool) {
	prefix := key + "="
	val, found := "", false
	for _, e := range env {
		if len(e) >= len(prefix) && e[:len(prefix)] == prefix {
			val, found = e[len(prefix):], true
		}
	}
	return val, found
}

// TestBuildSessionEnv_AgentChatDisableGate locks in the per-session gate that
// swe-swe-server puts on the built-in AskUserQuestion tool via the
// AGENT_CHAT_DISABLE env var (read by the PreToolUse hook in entrypoint.sh):
//
//   - chat session -> AGENT_CHAT_DISABLE unset, so the hook blocks
//     AskUserQuestion and forces the agent onto the agent-chat send_message
//     MCP tool (the web chat UI is the user's surface, the TUI menu is not).
//   - terminal (any non-"chat") session -> AGENT_CHAT_DISABLE=1, so the
//     built-in tool is allowed (the local TUI IS the user's surface).
//
// The browser e2e (agent-browser.spec.js) proves "chat works" but not this
// gate: a reboot regression where chat sessions wrongly inherit =1, or a stale
// value leaks through os.Environ, would still let the agent answer -- just via
// the wrong path (a hung AskUserQuestion menu the chat user never sees). So we
// assert the invariant directly at its source, buildSessionEnv.
//
// SID and WorkDir are left empty so buildSessionEnv skips per-session
// gitconfig, token env, and .swe-swe/env loading; only the gate is exercised.
func TestBuildSessionEnv_AgentChatDisableGate(t *testing.T) {
	t.Run("chat session leaves AGENT_CHAT_DISABLE unset", func(t *testing.T) {
		env := buildSessionEnv(SessionEnvParams{SessionMode: "chat"})
		if v, ok := envValue(env, "AGENT_CHAT_DISABLE"); ok {
			t.Fatalf("chat session must not set AGENT_CHAT_DISABLE, got =%q", v)
		}
	})

	t.Run("terminal session sets AGENT_CHAT_DISABLE=1", func(t *testing.T) {
		env := buildSessionEnv(SessionEnvParams{SessionMode: "terminal"})
		if v, ok := envValue(env, "AGENT_CHAT_DISABLE"); !ok || v != "1" {
			t.Fatalf("terminal session must set AGENT_CHAT_DISABLE=1, got %q (present=%v)", v, ok)
		}
	})

	// The reboot risk: swe-swe-server is itself launched with
	// AGENT_CHAT_DISABLE already present in its environment (e.g. inherited
	// from an enclosing process or a prior boot). filterEnv must strip that
	// stale value before the gate re-adds it conditionally, so a chat session
	// never silently inherits =1.
	t.Run("stale inherited AGENT_CHAT_DISABLE is stripped for chat sessions", func(t *testing.T) {
		t.Setenv("AGENT_CHAT_DISABLE", "1")
		env := buildSessionEnv(SessionEnvParams{SessionMode: "chat"})
		if v, ok := envValue(env, "AGENT_CHAT_DISABLE"); ok {
			t.Fatalf("chat session must strip inherited AGENT_CHAT_DISABLE, got =%q", v)
		}
	})
}

// TestResolveStagedMode guards the "new"-session staging fix. Regression:
// POST /api/session/new stages the creation intent with assistant only and
// echoes the requested mode onto the redirect query; the WS handler that
// materializes the session then replaces its params with the staged intent.
// Before the fix that override dropped the query's session mode, so a
// "Start Chat" POST (session=chat) materialized as a terminal session --
// AGENT_CHAT_DISABLE=1, the agent-chat sidecar never bound its port, and chat
// was dead. resolveStagedMode preserves the query mode when the staged intent
// left it unset, while never clobbering an explicit staged mode (fork path).
func TestResolveStagedMode(t *testing.T) {
	cases := []struct {
		name       string
		stagedMode string
		urlMode    string
		want       string
	}{
		// The regression: "new" staged assistant only (empty mode); the
		// redirect query carried session=chat.
		{"new-staging chat falls back to query mode", "", "chat", "chat"},
		{"new-staging terminal falls back to query mode", "", "terminal", "terminal"},
		{"empty staged and empty query stays empty (terminal default)", "", "", ""},
		// Fork stages SessionMode explicitly; the query must never clobber it.
		{"explicit staged chat wins over query", "chat", "terminal", "chat"},
		{"explicit staged chat wins over empty query", "chat", "", "chat"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := resolveStagedMode(tc.stagedMode, tc.urlMode); got != tc.want {
				t.Fatalf("resolveStagedMode(%q, %q) = %q, want %q", tc.stagedMode, tc.urlMode, got, tc.want)
			}
		})
	}
}
