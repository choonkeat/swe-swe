package main

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"
)

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

// TestDefaultChatExportEnv locks in the presence-checked default for the
// streaming chat-log export (agent-chat >= 0.8.14). The default is appended
// AFTER buildSessionEnv's user layers (Settings textarea, .swe-swe/env), where
// last-wins semantics would let a blind append clobber user overrides -- so
// the helper must skip the append whenever the key is already PRESENT, even
// with an empty value: AGENT_CHAT_EXPORT_DIR= (empty) is the user's explicit
// opt-out (agent-chat treats empty as disabled), and a custom path is a
// relocation. Terminal sessions never reach the helper (the call site sits
// inside materializeSession's SessionMode=="chat" block, next to the
// AGENT_CHAT_EVENT_LOG append).
func TestDefaultChatExportEnv(t *testing.T) {
	base := []string{"PATH=/usr/bin", "AGENT_CHAT_EVENT_LOG=/rec/session-x.events.jsonl"}

	t.Run("absent key gains the workDir default", func(t *testing.T) {
		got := defaultChatExportEnv(base, "/repos/foo")
		if v, ok := envValue(got, "AGENT_CHAT_EXPORT_DIR"); !ok || v != "/repos/foo/agent-chats" {
			t.Fatalf("want AGENT_CHAT_EXPORT_DIR=/repos/foo/agent-chats, got %q (present=%v)", v, ok)
		}
	})

	t.Run("custom path is preserved", func(t *testing.T) {
		env := append(append([]string{}, base...), "AGENT_CHAT_EXPORT_DIR=/repos/foo/docs/chats")
		got := defaultChatExportEnv(env, "/repos/foo")
		if len(got) != len(env) {
			t.Fatalf("env must be unchanged, len %d -> %d", len(env), len(got))
		}
		if v, _ := envValue(got, "AGENT_CHAT_EXPORT_DIR"); v != "/repos/foo/docs/chats" {
			t.Fatalf("custom path clobbered: got %q", v)
		}
	})

	t.Run("explicit empty value is an opt-out, not a missing key", func(t *testing.T) {
		env := append(append([]string{}, base...), "AGENT_CHAT_EXPORT_DIR=")
		got := defaultChatExportEnv(env, "/repos/foo")
		if len(got) != len(env) {
			t.Fatalf("opt-out env must be unchanged, len %d -> %d", len(env), len(got))
		}
		if v, ok := envValue(got, "AGENT_CHAT_EXPORT_DIR"); !ok || v != "" {
			t.Fatalf("opt-out must survive as empty-present, got %q (present=%v)", v, ok)
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

// TestHandleNewSessionAPI_StagesFullWiring guards the higher-severity half of
// the staging regression. The WS handler that materializes a "new" session
// replaces its URL-derived params with the staged intent, so any dialog field
// left unstaged is silently lost. When pwd was dropped, getOrCreateSession fell
// back to baseRepo "/workspace" -- so every new session opened in the wrong
// directory, and its name (derived from the working directory) collapsed to a
// bare UUID. This asserts the "new" POST stages the full wiring: name, pwd
// (RepoPath), branch, session mode, and extra_args.
func TestHandleNewSessionAPI_StagesFullWiring(t *testing.T) {
	saved := availableAssistants
	availableAssistants = []AssistantConfig{{Binary: "opencode"}}
	defer func() { availableAssistants = saved }()

	form := url.Values{
		"assistant":  {"opencode"},
		"session":    {"chat"},
		"name":       {"My Session"},
		"pwd":        {"/workspace/project-x"},
		"branch":     {"feature/new thing"},
		"extra_args": {"--channels server:agent-chat"},
	}
	req := httptest.NewRequest(http.MethodPost, "/api/session/new", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()

	handleNewSessionAPI(rec, req)

	if rec.Code != http.StatusFound {
		t.Fatalf("status = %d, want 302 redirect; body=%q", rec.Code, rec.Body.String())
	}
	loc := rec.Header().Get("Location")
	m := regexp.MustCompile(`/session/([0-9a-f-]{36})`).FindStringSubmatch(loc)
	if m == nil {
		t.Fatalf("redirect Location %q carries no session UUID", loc)
	}
	staged, ok := takePendingSession(m[1])
	if !ok {
		t.Fatalf("no staged intent found for minted UUID %s", m[1])
	}
	p := staged.params
	if p.Name != "My Session" {
		t.Errorf("staged Name = %q, want %q", p.Name, "My Session")
	}
	// The crux: the working directory must survive so it isn't defaulted to
	// /workspace (which also collapses the derived name).
	if p.RepoPath != "/workspace/project-x" {
		t.Errorf("staged RepoPath = %q, want %q", p.RepoPath, "/workspace/project-x")
	}
	if p.SessionMode != "chat" {
		t.Errorf("staged SessionMode = %q, want chat", p.SessionMode)
	}
	if p.ExtraArgs != "--channels server:agent-chat" {
		t.Errorf("staged ExtraArgs = %q, want the dialog value", p.ExtraArgs)
	}
	// Branch is sanitized through deriveBranchName (spaces -> hyphens).
	if want := deriveBranchName("feature/new thing"); p.Branch != want {
		t.Errorf("staged Branch = %q, want %q", p.Branch, want)
	}
}
