package main

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeResolveEventsFile writes the given bubble lines (already-marshalled JSON) into
// a temp .events.jsonl and returns its path.
func writeResolveEventsFile(t *testing.T, lines []string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "events.jsonl")
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0644); err != nil {
		t.Fatalf("write events: %v", err)
	}
	return path
}

// writeClaudeJSONL writes a synthetic claude .jsonl with the given lines.
func writeClaudeJSONL(t *testing.T, lines []string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "claude.jsonl")
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0644); err != nil {
		t.Fatalf("write jsonl: %v", err)
	}
	return path
}

// mustJSON marshals v to a one-line JSON string.
func mustJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return string(b)
}

func TestFindNthMCPToolCall_Claude_HappyPath(t *testing.T) {
	jsonl := writeClaudeJSONL(t, []string{
		mustJSON(t, map[string]any{"type": "user", "message": map[string]any{"content": []any{}}}),
		mustJSON(t, map[string]any{
			"type": "assistant",
			"message": map[string]any{
				"content": []any{
					map[string]any{"type": "tool_use", "id": "toolu_1", "name": "mcp__swe-swe-agent-chat__send_message", "input": map[string]any{"text": "hi"}},
				},
			},
		}),
		mustJSON(t, map[string]any{
			"type": "assistant",
			"message": map[string]any{
				"content": []any{
					map[string]any{"type": "tool_use", "id": "toolu_2", "name": "mcp__swe-swe-agent-chat__check_messages"},
				},
			},
		}),
		mustJSON(t, map[string]any{
			"type": "assistant",
			"message": map[string]any{
				"content": []any{
					map[string]any{"type": "tool_use", "id": "toolu_3", "name": "mcp__swe-swe-agent-chat__send_message", "input": map[string]any{"text": "bye"}},
				},
			},
		}),
	})

	id, err := findNthMCPToolCall(jsonl, "claude", "send_message", 1)
	if err != nil || id != "toolu_1" {
		t.Errorf("1st send_message: got (%q, %v), want (toolu_1, nil)", id, err)
	}
	id, err = findNthMCPToolCall(jsonl, "claude", "send_message", 2)
	if err != nil || id != "toolu_3" {
		t.Errorf("2nd send_message: got (%q, %v), want (toolu_3, nil)", id, err)
	}
	id, err = findNthMCPToolCall(jsonl, "claude", "check_messages", 1)
	if err != nil || id != "toolu_2" {
		t.Errorf("1st check_messages: got (%q, %v), want (toolu_2, nil)", id, err)
	}
}

func TestFindNthMCPToolCall_Claude_NotEnough(t *testing.T) {
	jsonl := writeClaudeJSONL(t, []string{
		mustJSON(t, map[string]any{
			"type": "assistant",
			"message": map[string]any{
				"content": []any{
					map[string]any{"type": "tool_use", "id": "toolu_only", "name": "mcp__swe-swe-agent-chat__send_message"},
				},
			},
		}),
	})

	_, err := findNthMCPToolCall(jsonl, "claude", "send_message", 5)
	if err == nil || !strings.Contains(err.Error(), "only 1 of 5") {
		t.Errorf("want 'only 1 of 5' error, got %v", err)
	}
}

func TestFindNthMCPToolCall_Codex_HappyPath(t *testing.T) {
	jsonl := writeClaudeJSONL(t, []string{
		mustJSON(t, map[string]any{
			"type":    "response_item",
			"payload": map[string]any{"type": "function_call", "call_id": "call_A", "namespace": "mcp__swe_swe_agent_chat__", "name": "send_message", "arguments": `{"text":"hi"}`},
		}),
		mustJSON(t, map[string]any{
			"type":    "response_item",
			"payload": map[string]any{"type": "function_call", "call_id": "call_B", "namespace": "mcp__swe_swe_agent_chat__", "name": "check_messages"},
		}),
		mustJSON(t, map[string]any{
			"type":    "response_item",
			"payload": map[string]any{"type": "function_call", "call_id": "call_C", "namespace": "mcp__swe_swe_agent_chat__", "name": "send_message"},
		}),
		// Wrong namespace -- not counted.
		mustJSON(t, map[string]any{
			"type":    "response_item",
			"payload": map[string]any{"type": "function_call", "call_id": "call_X", "namespace": "mcp__some_other__", "name": "send_message"},
		}),
	})

	id, err := findNthMCPToolCall(jsonl, "codex", "send_message", 2)
	if err != nil || id != "call_C" {
		t.Errorf("2nd codex send_message: got (%q, %v), want (call_C, nil)", id, err)
	}
}

func TestResolveBubbleAnchor_StampedAgentMessage(t *testing.T) {
	events := writeResolveEventsFile(t, []string{
		mustJSON(t, bubbleEvent{Type: "agentMessage", Seq: 1, Text: "x", AgentToolName: "send_message", AgentToolSeq: 1}),
		mustJSON(t, bubbleEvent{Type: "userMessage", Seq: 2, ID: "u1", Text: "y"}),
		mustJSON(t, bubbleEvent{Type: "userMessagesConsumed", Seq: 3, IDs: []string{"u1"}, AgentToolName: "check_messages", AgentToolSeq: 1}),
		mustJSON(t, bubbleEvent{Type: "agentMessage", Seq: 4, Text: "z", AgentToolName: "send_message", AgentToolSeq: 2}),
	})
	jsonl := writeClaudeJSONL(t, []string{
		mustJSON(t, map[string]any{
			"type": "assistant",
			"message": map[string]any{
				"content": []any{
					map[string]any{"type": "tool_use", "id": "toolu_A", "name": "mcp__swe-swe-agent-chat__send_message"},
				},
			},
		}),
		mustJSON(t, map[string]any{
			"type": "assistant",
			"message": map[string]any{
				"content": []any{
					map[string]any{"type": "tool_use", "id": "toolu_B", "name": "mcp__swe-swe-agent-chat__check_messages"},
				},
			},
		}),
		mustJSON(t, map[string]any{
			"type": "assistant",
			"message": map[string]any{
				"content": []any{
					map[string]any{"type": "tool_use", "id": "toolu_C", "name": "mcp__swe-swe-agent-chat__send_message"},
				},
			},
		}),
	})

	resolved, err := resolveBubbleAnchor(events, jsonl, "claude", 4, "after")
	if err != nil {
		t.Fatalf("resolve seq=4: %v", err)
	}
	if resolved.AnchorID != "toolu_C" {
		t.Errorf("AnchorID: got %q, want toolu_C", resolved.AnchorID)
	}
	if resolved.ResolverUsed != "stamp" {
		t.Errorf("ResolverUsed: got %q, want stamp", resolved.ResolverUsed)
	}
	if resolved.BubbleKind != "agentMessage" {
		t.Errorf("BubbleKind: got %q, want agentMessage", resolved.BubbleKind)
	}
	if resolved.ToolName != "send_message" {
		t.Errorf("ToolName: got %q, want send_message", resolved.ToolName)
	}
}

func TestResolveBubbleAnchor_StampedUserMessage_LinksViaConsumed(t *testing.T) {
	events := writeResolveEventsFile(t, []string{
		mustJSON(t, bubbleEvent{Type: "userMessage", Seq: 1, ID: "u1", Text: "hi"}),
		mustJSON(t, bubbleEvent{Type: "userMessagesConsumed", Seq: 2, IDs: []string{"u1"}, AgentToolName: "check_messages", AgentToolSeq: 1}),
	})
	jsonl := writeClaudeJSONL(t, []string{
		mustJSON(t, map[string]any{
			"type": "assistant",
			"message": map[string]any{
				"content": []any{
					map[string]any{"type": "tool_use", "id": "toolu_CM", "name": "mcp__swe-swe-agent-chat__check_messages"},
				},
			},
		}),
	})

	resolved, err := resolveBubbleAnchor(events, jsonl, "claude", 1, "after")
	if err != nil {
		t.Fatalf("resolve userMessage: %v", err)
	}
	if resolved.AnchorID != "toolu_CM" {
		t.Errorf("AnchorID: got %q, want toolu_CM", resolved.AnchorID)
	}
}

func TestResolveBubbleAnchor_UserMessageNotDrained_ReturnsErrBubbleNotDrained(t *testing.T) {
	events := writeResolveEventsFile(t, []string{
		mustJSON(t, bubbleEvent{Type: "userMessage", Seq: 1, ID: "u1", Text: "just-sent-not-yet-drained"}),
	})
	jsonl := writeClaudeJSONL(t, []string{})

	_, err := resolveBubbleAnchor(events, jsonl, "claude", 1, "after")
	if !errors.Is(err, ErrBubbleNotDrained) {
		t.Errorf("want ErrBubbleNotDrained, got %v", err)
	}
}

func TestResolveBubbleAnchor_BubbleNotFound(t *testing.T) {
	events := writeResolveEventsFile(t, []string{
		mustJSON(t, bubbleEvent{Type: "userMessage", Seq: 1, ID: "u1", Text: "hi"}),
	})
	jsonl := writeClaudeJSONL(t, []string{})

	_, err := resolveBubbleAnchor(events, jsonl, "claude", 999, "after")
	if err == nil || !strings.Contains(err.Error(), "bubble seq not found") {
		t.Errorf("want bubble-not-found error, got %v", err)
	}
}

func TestResolveBubbleAnchor_LegacyTextFallback_AgentBubble(t *testing.T) {
	uniqueText := "alpha-beta-gamma-this-is-a-distinctive-sentence-many-chars"
	events := writeResolveEventsFile(t, []string{
		mustJSON(t, bubbleEvent{Type: "agentMessage", Seq: 1, Text: uniqueText /* no stamp */}),
	})
	// Codex rollout containing the same text in a function_call arguments
	// blob. (Channels-mode claude can't be tested here because agent
	// bubbles in channels mode have no .jsonl correlate by definition.)
	jsonl := writeClaudeJSONL(t, []string{
		mustJSON(t, map[string]any{
			"type": "response_item",
			"payload": map[string]any{
				"type":      "function_call",
				"call_id":   "call_legacy",
				"namespace": "mcp__swe_swe_agent_chat__",
				"name":      "send_message",
				"arguments": `{"text":"` + uniqueText + `"}`,
			},
		}),
	})

	resolved, err := resolveBubbleAnchor(events, jsonl, "codex", 1, "after")
	if err != nil {
		t.Fatalf("resolve via text fallback: %v", err)
	}
	if resolved.AnchorID != "call_legacy" {
		t.Errorf("AnchorID: got %q, want call_legacy", resolved.AnchorID)
	}
	if resolved.ResolverUsed != "text" {
		t.Errorf("ResolverUsed: got %q, want text", resolved.ResolverUsed)
	}
}

func TestResolveBubbleAnchor_LegacyTextFallback_TextTooShort(t *testing.T) {
	events := writeResolveEventsFile(t, []string{
		mustJSON(t, bubbleEvent{Type: "agentMessage", Seq: 1, Text: "ok" /* short, no stamp */}),
	})
	jsonl := writeClaudeJSONL(t, []string{})

	_, err := resolveBubbleAnchor(events, jsonl, "codex", 1, "after")
	if err == nil || !strings.Contains(err.Error(), "too short") {
		t.Errorf("want text-too-short error, got %v", err)
	}
}

func TestResolveBubbleAnchor_LegacyTextFallback_ChannelsAgentBubble(t *testing.T) {
	events := writeResolveEventsFile(t, []string{
		mustJSON(t, bubbleEvent{Type: "agentMessage", Seq: 1, Text: "a-very-distinctive-long-channels-mode-agent-reply"}),
	})
	jsonl := writeClaudeJSONL(t, []string{})

	_, err := resolveBubbleAnchor(events, jsonl, "claude", 1, "after")
	if !errors.Is(err, ErrChannelsAgentBubble) {
		t.Errorf("want ErrChannelsAgentBubble, got %v", err)
	}
}

func TestBuildForkResumeArgs(t *testing.T) {
	tests := []struct {
		name      string
		assistant string
		extra     string
		newID     string
		want      string
	}{
		{"claude empty extra", "claude", "", "abc", "--resume abc"},
		{"claude existing extra", "claude", "--dangerously-load-development-channels server:swe-swe-agent-chat", "abc", "--dangerously-load-development-channels server:swe-swe-agent-chat --resume abc"},
		{"codex empty extra", "codex", "", "xyz", "resume xyz"},
		{"codex existing extra", "codex", "--yolo", "xyz", "--yolo resume xyz"},
		{"unknown agent falls through to claude shape", "gemini", "", "abc", "--resume abc"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := buildForkResumeArgs(tc.assistant, tc.extra, tc.newID); got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}
