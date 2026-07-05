package forkconvo

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeClaudeJSONL writes a synthetic claude .jsonl with the given lines
// and returns its absolute path. Each `line` must already be valid JSON.
func writeClaudeJSONL(t *testing.T, lines []string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "claude.jsonl")
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0644); err != nil {
		t.Fatalf("write jsonl: %v", err)
	}
	return path
}

func mustJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return string(b)
}

// asstWithToolUse builds an assistant event line containing one tool_use.
func asstWithToolUse(t *testing.T, evUUID, toolName, toolUseID string) string {
	return mustJSON(t, map[string]any{
		"type": "assistant",
		"uuid": evUUID,
		"message": map[string]any{
			"content": []any{
				map[string]any{"type": "tool_use", "id": toolUseID, "name": toolName, "input": map[string]any{}},
			},
		},
	})
}

// asstWithBashToolUse builds an assistant event line containing one Bash
// tool_use running the given shell command (the MCP-less agent-chat shape).
func asstWithBashToolUse(t *testing.T, evUUID, command, toolUseID string) string {
	return mustJSON(t, map[string]any{
		"type": "assistant",
		"uuid": evUUID,
		"message": map[string]any{
			"content": []any{
				map[string]any{"type": "tool_use", "id": toolUseID, "name": "Bash", "input": map[string]any{"command": command, "description": "test"}},
			},
		},
	})
}

// userToolResult builds a user event line that resolves a tool_use_id.
func userToolResult(t *testing.T, evUUID, toolUseID, text string) string {
	return mustJSON(t, map[string]any{
		"type": "user",
		"uuid": evUUID,
		"message": map[string]any{
			"content": []any{
				map[string]any{"type": "tool_result", "tool_use_id": toolUseID, "content": []any{
					map[string]any{"type": "text", "text": text},
				}},
			},
		},
	})
}

// asstTextOnly builds an assistant event with a plain text turn (no tool_use).
func asstTextOnly(t *testing.T, evUUID, text string) string {
	return mustJSON(t, map[string]any{
		"type": "assistant",
		"uuid": evUUID,
		"message": map[string]any{
			"content": []any{
				map[string]any{"type": "text", "text": text},
			},
		},
	})
}

// TestClaudeFindLastChatReply_ReturnsAssistantEventUUID is the central B1
// property: the anchor is the ASSISTANT event's uuid (not the user
// tool_result's), so cutting there yields a WAITING-tail fork.
func TestClaudeFindLastChatReply_ReturnsAssistantEventUUID(t *testing.T) {
	jsonl := writeClaudeJSONL(t, []string{
		asstWithToolUse(t, "evt-asst-1", "mcp__swe-swe-agent-chat__send_message", "toolu_1"),
		userToolResult(t, "evt-user-1", "toolu_1", "User responded: next directive"),
	})
	got, err := claudeFindLastChatReply(jsonl, "send_message")
	if err != nil {
		t.Fatalf("claudeFindLastChatReply: %v", err)
	}
	if got != "evt-asst-1" {
		t.Errorf("got anchor uuid %q, want assistant event uuid %q", got, "evt-asst-1")
	}
}

// TestClaudeFindLastChatReply_MostRecentWins ensures we take the latest
// send_message even when earlier ones also exist.
func TestClaudeFindLastChatReply_MostRecentWins(t *testing.T) {
	jsonl := writeClaudeJSONL(t, []string{
		asstWithToolUse(t, "evt-asst-1", "mcp__swe-swe-agent-chat__send_message", "toolu_1"),
		userToolResult(t, "evt-user-1", "toolu_1", "User responded: first"),
		asstWithToolUse(t, "evt-asst-2", "mcp__swe-swe-agent-chat__send_message", "toolu_2"),
		userToolResult(t, "evt-user-2", "toolu_2", "User responded: second"),
	})
	got, err := claudeFindLastChatReply(jsonl, "send_message")
	if err != nil {
		t.Fatalf("claudeFindLastChatReply: %v", err)
	}
	if got != "evt-asst-2" {
		t.Errorf("got %q, want %q (most recent send_message's assistant event)", got, "evt-asst-2")
	}
}

// TestClaudeFindLastChatReply_DanglingToolUseStillResolves verifies that
// when the source's tail is WAITING (send_message tool_use with no matching
// tool_result yet -- the safest fork point), we still return that
// assistant event. Pre-B1 code required the pair and would fall back to
// an older matched send_message.
func TestClaudeFindLastChatReply_DanglingToolUseStillResolves(t *testing.T) {
	jsonl := writeClaudeJSONL(t, []string{
		asstWithToolUse(t, "evt-asst-1", "mcp__swe-swe-agent-chat__send_message", "toolu_1"),
		userToolResult(t, "evt-user-1", "toolu_1", "User responded: first"),
		// Tail: latest send_message has no matching tool_result yet.
		asstWithToolUse(t, "evt-asst-2", "mcp__swe-swe-agent-chat__send_message", "toolu_2"),
	})
	got, err := claudeFindLastChatReply(jsonl, "send_message")
	if err != nil {
		t.Fatalf("claudeFindLastChatReply: %v", err)
	}
	if got != "evt-asst-2" {
		t.Errorf("got %q, want %q (dangling tool_use is the freshest safe anchor)", got, "evt-asst-2")
	}
}

// TestClaudeFindLastChatReply_FallbackToCheckMessages covers the
// channels-mode runtime where the agent's text reply is streamed without
// a send_message tool_use, so the only agent-chat tool_use the .jsonl
// contains is check_messages.
func TestClaudeFindLastChatReply_FallbackToCheckMessages(t *testing.T) {
	jsonl := writeClaudeJSONL(t, []string{
		asstWithToolUse(t, "evt-asst-1", "mcp__swe-swe-agent-chat__check_messages", "toolu_cm1"),
		userToolResult(t, "evt-user-1", "toolu_cm1", "[]"),
	})
	got, err := claudeFindLastChatReply(jsonl, "send_message")
	if err != nil {
		t.Fatalf("claudeFindLastChatReply: %v", err)
	}
	if got != "evt-asst-1" {
		t.Errorf("got %q, want %q (fallback to check_messages assistant event)", got, "evt-asst-1")
	}
}

// TestClaudeFindLastChatReply_MCPLessBashCLI covers MCP-less sessions,
// where agent-chat is reached through the `mcp` CLI inside Bash tool_uses
// and the .jsonl contains ZERO native mcp__swe-swe-agent-chat__* entries.
// Regression: /api/fork on such recordings failed with "no
// mcp__swe-swe-agent-chat__* tool_use found".
func TestClaudeFindLastChatReply_MCPLessBashCLI(t *testing.T) {
	jsonl := writeClaudeJSONL(t, []string{
		asstWithBashToolUse(t, "evt-asst-1", `mcp swe-swe-agent-chat send_message --text "hello" --first_quick_reply "ok"`, "toolu_1"),
		userToolResult(t, "evt-user-1", "toolu_1", "User responded: first"),
		asstWithBashToolUse(t, "evt-asst-2", `mcp swe-swe-agent-chat send_message --text "done" --first_quick_reply "thanks"`, "toolu_2"),
	})
	got, err := claudeFindLastChatReply(jsonl, "send_message")
	if err != nil {
		t.Fatalf("claudeFindLastChatReply: %v", err)
	}
	if got != "evt-asst-2" {
		t.Errorf("got %q, want %q (most recent CLI send_message)", got, "evt-asst-2")
	}
}

// TestClaudeFindLastChatReply_MCPLessFallback: a session whose only
// agent-chat CLI call is check_messages still resolves (fallback), same as
// the native channels-mode fallback.
func TestClaudeFindLastChatReply_MCPLessFallback(t *testing.T) {
	jsonl := writeClaudeJSONL(t, []string{
		asstWithBashToolUse(t, "evt-asst-1", "mcp swe-swe-agent-chat check_messages", "toolu_cm1"),
		userToolResult(t, "evt-user-1", "toolu_cm1", `{"queue":"empty"}`),
	})
	got, err := claudeFindLastChatReply(jsonl, "send_message")
	if err != nil {
		t.Fatalf("claudeFindLastChatReply: %v", err)
	}
	if got != "evt-asst-1" {
		t.Errorf("got %q, want %q (fallback to CLI check_messages)", got, "evt-asst-1")
	}
}

// TestClaudeFindLastChatReply_QuotedMentionIsNotAnInvocation: a Bash call
// that merely mentions the CLI inside a quoted string (grep pattern, echo)
// must not be mistaken for an agent-chat call and steal the anchor.
func TestClaudeFindLastChatReply_QuotedMentionIsNotAnInvocation(t *testing.T) {
	jsonl := writeClaudeJSONL(t, []string{
		asstWithBashToolUse(t, "evt-asst-1", `mcp swe-swe-agent-chat send_message --text "hi" --first_quick_reply "ok"`, "toolu_1"),
		userToolResult(t, "evt-user-1", "toolu_1", "User responded: search the code"),
		asstWithBashToolUse(t, "evt-asst-2", `grep -rn "mcp swe-swe-agent-chat" /workspace`, "toolu_2"),
		userToolResult(t, "evt-user-2", "toolu_2", "no matches"),
	})
	got, err := claudeFindLastChatReply(jsonl, "send_message")
	if err != nil {
		t.Fatalf("claudeFindLastChatReply: %v", err)
	}
	if got != "evt-asst-1" {
		t.Errorf("got %q, want %q (quoted mention in grep must not become the anchor)", got, "evt-asst-1")
	}
}

func TestClaudeFindLastChatReply_NoChatToolUse(t *testing.T) {
	jsonl := writeClaudeJSONL(t, []string{
		asstTextOnly(t, "evt-asst-1", "plain text reply, no tool_use"),
	})
	_, err := claudeFindLastChatReply(jsonl, "send_message")
	if err == nil {
		t.Fatal("expected error when no agent-chat tool_use exists")
	}
}

func TestClaudeBashAgentChatTool(t *testing.T) {
	cases := []struct {
		command  string
		wantTool string
		wantOK   bool
	}{
		{`mcp swe-swe-agent-chat send_message --text "hi" --first_quick_reply "ok"`, "send_message", true},
		{`mcp swe-swe-agent-chat check_messages`, "check_messages", true},
		{`cd /workspace && mcp swe-swe-agent-chat send_progress --text "working"`, "send_progress", true},
		// Server named but no tool token (bare docs dump / flag first).
		{`mcp swe-swe-agent-chat`, "", true},
		{`mcp swe-swe-agent-chat --remind-help-text-throttle 0`, "", true},
		// Quoted mentions keep their quote char attached to the field.
		{`grep -rn "mcp swe-swe-agent-chat" /workspace`, "", false},
		{`echo 'mcp swe-swe-agent-chat send_message'`, "", false},
		// Other servers and unrelated commands.
		{`mcp swe-swe list_sessions`, "", false},
		{`make test`, "", false},
		{``, "", false},
	}
	for _, tc := range cases {
		tool, ok := claudeBashAgentChatTool(tc.command)
		if tool != tc.wantTool || ok != tc.wantOK {
			t.Errorf("claudeBashAgentChatTool(%q) = (%q, %v), want (%q, %v)", tc.command, tool, ok, tc.wantTool, tc.wantOK)
		}
	}
}

func TestClaudeFindAssistantEventByToolUseID(t *testing.T) {
	jsonl := writeClaudeJSONL(t, []string{
		asstWithToolUse(t, "evt-1", "Bash", "toolu_bash"),
		asstWithToolUse(t, "evt-2", "mcp__swe-swe-agent-chat__send_message", "toolu_send"),
		asstWithToolUse(t, "evt-3", "Read", "toolu_read"),
	})
	got, err := claudeFindAssistantEventByToolUseID(jsonl, "toolu_send")
	if err != nil {
		t.Fatalf("lookup: %v", err)
	}
	if got != "evt-2" {
		t.Errorf("got %q, want evt-2", got)
	}
	if _, err := claudeFindAssistantEventByToolUseID(jsonl, "toolu_nope"); err == nil {
		t.Error("expected error for missing tool_use_id")
	}
}

// TestClaudeIsTailActive covers the ACTIVE-tail guard. The agent is
// considered ACTIVE only for non-chat tool_uses that lack a matching
// tool_result. Parked agent-chat calls (send_message/check_messages
// waiting on user) are NOT active -- they're the safe fork point.
func TestClaudeIsTailActive(t *testing.T) {
	cases := []struct {
		name  string
		lines []string
		want  bool
	}{
		{
			name: "idle: all tool_uses resolved",
			lines: []string{
				asstWithToolUse(t, "e1", "Bash", "tb1"),
				userToolResult(t, "e2", "tb1", "ok"),
				asstWithToolUse(t, "e3", "mcp__swe-swe-agent-chat__send_message", "ts1"),
				userToolResult(t, "e4", "ts1", "User responded: ok"),
			},
			want: false,
		},
		{
			name: "waiting: send_message parked (no result yet)",
			lines: []string{
				asstWithToolUse(t, "e1", "Bash", "tb1"),
				userToolResult(t, "e2", "tb1", "ok"),
				asstWithToolUse(t, "e3", "mcp__swe-swe-agent-chat__send_message", "ts1"),
			},
			want: false,
		},
		{
			name: "active: bash mid-run with no result",
			lines: []string{
				asstWithToolUse(t, "e1", "mcp__swe-swe-agent-chat__send_message", "ts1"),
				userToolResult(t, "e2", "ts1", "User responded: do X"),
				asstWithToolUse(t, "e3", "Bash", "tb1"),
				// no tool_result for tb1 yet -> ACTIVE
			},
			want: true,
		},
		{
			name: "active: multiple bash calls, one still pending",
			lines: []string{
				asstWithToolUse(t, "e1", "Bash", "tb1"),
				asstWithToolUse(t, "e2", "Bash", "tb2"),
				userToolResult(t, "e3", "tb1", "ok"),
				// tb2 unresolved -> ACTIVE
			},
			want: true,
		},
		{
			// MCP-less mode: the blocking send_message is a Bash call
			// running the `mcp` CLI, result-less until the user replies.
			// That's the WAITING state, not mid-work.
			name: "waiting: MCP-less CLI send_message parked (no result yet)",
			lines: []string{
				asstWithBashToolUse(t, "e1", `make test`, "tb1"),
				userToolResult(t, "e2", "tb1", "ok"),
				asstWithBashToolUse(t, "e3", `mcp swe-swe-agent-chat send_message --text "done" --first_quick_reply "ok"`, "ts1"),
			},
			want: false,
		},
		{
			name: "active: MCP-less plain bash mid-run with no result",
			lines: []string{
				asstWithBashToolUse(t, "e1", `mcp swe-swe-agent-chat send_message --text "hi" --first_quick_reply "ok"`, "ts1"),
				userToolResult(t, "e2", "ts1", "User responded: do X"),
				asstWithBashToolUse(t, "e3", `make build`, "tb1"),
				// no tool_result for tb1 yet -> ACTIVE
			},
			want: true,
		},
		{
			// Regression: ToolSearch (deferred-tool loader) flushes its
			// tool_result event to the .jsonl BEFORE its own tool_use line
			// (result ts later, but written first). An order-sensitive walk
			// (delete-on-result, add-on-use) miscounts this as pending and
			// reports ACTIVE -> /api/fork 409s forever on any settled
			// recording that ever used ToolSearch. The pairing is order-
			// independent: the result exists, so the tail is NOT active.
			name: "idle: tool_result line precedes its tool_use line (ToolSearch reorder)",
			lines: []string{
				asstWithToolUse(t, "e1", "mcp__swe-swe-agent-chat__send_message", "ts1"),
				userToolResult(t, "e2", "ts1", "User responded: go"),
				userToolResult(t, "e3", "ts2", "search results"), // result FIRST
				asstWithToolUse(t, "e4", "ToolSearch", "ts2"),     // use SECOND
			},
			want: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := writeClaudeJSONL(t, tc.lines)
			got, err := ClaudeIsTailActive(path)
			if err != nil {
				t.Fatalf("ClaudeIsTailActive: %v", err)
			}
			if got != tc.want {
				t.Errorf("got %v, want %v", got, tc.want)
			}
		})
	}
}

// TestForkClaude_DropsTrailingUserDirective is the end-to-end property:
// forking a PENDING-ACTION source must NOT include the trailing user
// tool_result in the fork .jsonl (that's the runaway-causing line).
func TestForkClaude_DropsTrailingUserDirective(t *testing.T) {
	// Build a real claude session layout: tempdir/<sid>.jsonl, with
	// CLAUDE_HOME pointing at the tempdir so findClaudeSession can locate
	// it. claudeProjectsRoot() uses CLAUDE_HOME/projects.
	dir := t.TempDir()
	projects := filepath.Join(dir, "projects", "workspace")
	if err := os.MkdirAll(projects, 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CLAUDE_HOME", dir)

	srcID := "00000000-0000-0000-0000-00000000abcd"
	srcPath := filepath.Join(projects, srcID+".jsonl")
	directive := "User responded: DELETE EVERYTHING NOW"
	lines := []string{
		asstTextOnly(t, "evt-0", "intro"),
		asstWithToolUse(t, "evt-asst-1", "mcp__swe-swe-agent-chat__send_message", "toolu_1"),
		userToolResult(t, "evt-user-1", "toolu_1", directive),
	}
	if err := os.WriteFile(srcPath, []byte(strings.Join(lines, "\n")+"\n"), 0644); err != nil {
		t.Fatal(err)
	}

	res, err := Fork(Opts{
		Agent:           AgentClaude,
		SourceSessionID: srcID,
		Anchor:          AnchorLastChatReply,
	})
	if err != nil {
		t.Fatalf("Fork: %v", err)
	}
	body, err := os.ReadFile(res.NewSourcePath)
	if err != nil {
		t.Fatalf("read fork: %v", err)
	}
	if strings.Contains(string(body), directive) {
		t.Errorf("fork .jsonl still contains the trailing user directive %q -- the runaway shape was NOT neutralized:\n%s", directive, body)
	}
	// The fork should end at the assistant event (last line's type=assistant).
	flines := strings.Split(strings.TrimRight(string(body), "\n"), "\n")
	last := flines[len(flines)-1]
	var lastEv struct {
		Type string `json:"type"`
		UUID string `json:"uuid"`
	}
	if err := json.Unmarshal([]byte(last), &lastEv); err != nil {
		t.Fatalf("parse last line: %v", err)
	}
	if lastEv.Type != "assistant" {
		t.Errorf("fork .jsonl last line type=%q, want %q", lastEv.Type, "assistant")
	}
	if lastEv.UUID != "evt-asst-1" {
		t.Errorf("fork .jsonl last line uuid=%q, want %q", lastEv.UUID, "evt-asst-1")
	}
	// Sanity: AnchorUUID in the result is the assistant event uuid too.
	if res.AnchorUUID != "evt-asst-1" {
		t.Errorf("Result.AnchorUUID=%q, want %q", res.AnchorUUID, "evt-asst-1")
	}
}

// TestForkClaude_BubbleAnchorTranslatesToolUseID covers the previously
// broken bubble path: fork_resolve.go hands forkconvo a tool_use_id, and
// forkClaude must translate it to the enclosing assistant event's uuid
// before claudeCopyUntil (which matches against ev.uuid) is invoked.
func TestForkClaude_BubbleAnchorTranslatesToolUseID(t *testing.T) {
	dir := t.TempDir()
	projects := filepath.Join(dir, "projects", "workspace")
	if err := os.MkdirAll(projects, 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CLAUDE_HOME", dir)

	srcID := "00000000-0000-0000-0000-00000000bcde"
	srcPath := filepath.Join(projects, srcID+".jsonl")
	lines := []string{
		asstWithToolUse(t, "evt-asst-1", "mcp__swe-swe-agent-chat__send_message", "toolu_first"),
		userToolResult(t, "evt-user-1", "toolu_first", "User responded: first"),
		asstWithToolUse(t, "evt-asst-2", "mcp__swe-swe-agent-chat__send_message", "toolu_second"),
		userToolResult(t, "evt-user-2", "toolu_second", "User responded: second"),
	}
	if err := os.WriteFile(srcPath, []byte(strings.Join(lines, "\n")+"\n"), 0644); err != nil {
		t.Fatal(err)
	}

	// Bubble-anchored: caller targets the FIRST send_message (tool_use_id
	// "toolu_first") and expects the fork to end at evt-asst-1, not the
	// later evt-asst-2. With the old code this would error ("anchor uuid
	// toolu_first not present") because copyUntil compared the
	// tool_use_id against ev.uuid.
	res, err := Fork(Opts{
		Agent:           AgentClaude,
		SourceSessionID: srcID,
		Anchor:          "toolu_first",
	})
	if err != nil {
		t.Fatalf("Fork: %v", err)
	}
	body, err := os.ReadFile(res.NewSourcePath)
	if err != nil {
		t.Fatalf("read fork: %v", err)
	}
	if strings.Contains(string(body), "toolu_second") {
		t.Error("fork .jsonl includes the later send_message (toolu_second); should have stopped at toolu_first")
	}
	if res.AnchorUUID != "evt-asst-1" {
		t.Errorf("Result.AnchorUUID=%q, want evt-asst-1", res.AnchorUUID)
	}
}
