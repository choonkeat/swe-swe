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

func TestClaudeFindLastChatReply_NoChatToolUse(t *testing.T) {
	jsonl := writeClaudeJSONL(t, []string{
		asstTextOnly(t, "evt-asst-1", "plain text reply, no tool_use"),
	})
	_, err := claudeFindLastChatReply(jsonl, "send_message")
	if err == nil {
		t.Fatal("expected error when no agent-chat tool_use exists")
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
