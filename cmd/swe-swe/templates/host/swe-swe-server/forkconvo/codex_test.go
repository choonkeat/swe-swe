package forkconvo

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeCodexRollout(t *testing.T, dir, sessionID string, lines []string) string {
	t.Helper()
	day := filepath.Join(dir, "sessions", "2026", "05", "27")
	if err := os.MkdirAll(day, 0755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(day, "rollout-2026-05-27T00-00-00-"+sessionID+".jsonl")
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0644); err != nil {
		t.Fatalf("write rollout: %v", err)
	}
	return path
}

func codexFunctionCall(t *testing.T, namespace, name, callID string) string {
	return mustJSON(t, map[string]any{
		"type": "response_item",
		"payload": map[string]any{
			"type":      "function_call",
			"namespace": namespace,
			"name":      name,
			"call_id":   callID,
		},
	})
}

func codexFunctionCallOutput(t *testing.T, callID, output string) string {
	return mustJSON(t, map[string]any{
		"type": "response_item",
		"payload": map[string]any{
			"type":    "function_call_output",
			"call_id": callID,
			"output":  output,
		},
	})
}

// TestForkCodex_DropsTrailingFunctionCallOutput is codex's analogue of
// the claude test: forking at the last send_message must NOT include
// the matching function_call_output (which would be the user's reply,
// the PENDING-ACTION line). The fork .jsonl's tail should be the
// function_call itself.
func TestForkCodex_DropsTrailingFunctionCallOutput(t *testing.T) {
	codexHome := t.TempDir()
	t.Setenv("CODEX_HOME", codexHome)

	sid := "11111111-1111-1111-1111-111111111111"
	directive := "User responded: DROP EVERYTHING"
	lines := []string{
		codexFunctionCall(t, chatMCPNamespaceCodex, "send_message", "call_1"),
		codexFunctionCallOutput(t, "call_1", directive),
	}
	_ = writeCodexRollout(t, codexHome, sid, lines)

	res, err := Fork(Opts{
		Agent:           AgentCodex,
		SourceSessionID: sid,
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
		t.Errorf("fork rollout still contains the trailing user directive %q:\n%s", directive, body)
	}
	if strings.Contains(string(body), `"function_call_output"`) {
		t.Errorf("fork rollout still contains a function_call_output line; expected the cut to land at function_call:\n%s", body)
	}
	// AnchorUUID is the call_id for codex (its per-agent identifier).
	if res.AnchorUUID != "call_1" {
		t.Errorf("Result.AnchorUUID=%q, want call_1", res.AnchorUUID)
	}
}

func TestCodexIsTailActive(t *testing.T) {
	cases := []struct {
		name  string
		lines []string
		want  bool
	}{
		{
			name: "idle: all function_calls resolved",
			lines: []string{
				codexFunctionCall(t, "shell", "exec", "call_b1"),
				codexFunctionCallOutput(t, "call_b1", "ok"),
				codexFunctionCall(t, chatMCPNamespaceCodex, "send_message", "call_s1"),
				codexFunctionCallOutput(t, "call_s1", "User responded: ok"),
			},
			want: false,
		},
		{
			name: "waiting: chat call parked",
			lines: []string{
				codexFunctionCall(t, "shell", "exec", "call_b1"),
				codexFunctionCallOutput(t, "call_b1", "ok"),
				codexFunctionCall(t, chatMCPNamespaceCodex, "send_message", "call_s1"),
			},
			want: false,
		},
		{
			name: "active: non-chat call pending",
			lines: []string{
				codexFunctionCall(t, chatMCPNamespaceCodex, "send_message", "call_s1"),
				codexFunctionCallOutput(t, "call_s1", "User responded: do X"),
				codexFunctionCall(t, "shell", "exec", "call_b1"),
				// no output for call_b1 -> ACTIVE
			},
			want: true,
		},
		{
			// Regression (mirror of the Claude ToolSearch-reorder case):
			// a function_call_output line written ahead of its function_call
			// line must still pair up. Order-independent set-difference -> NOT
			// active.
			name: "idle: output line precedes its call line",
			lines: []string{
				codexFunctionCall(t, chatMCPNamespaceCodex, "send_message", "call_s1"),
				codexFunctionCallOutput(t, "call_s1", "User responded: go"),
				codexFunctionCallOutput(t, "call_b1", "result"), // output FIRST
				codexFunctionCall(t, "shell", "exec", "call_b1"), // call SECOND
			},
			want: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "rollout.jsonl")
			if err := os.WriteFile(path, []byte(strings.Join(tc.lines, "\n")+"\n"), 0644); err != nil {
				t.Fatal(err)
			}
			got, err := CodexIsTailActive(path)
			if err != nil {
				t.Fatalf("CodexIsTailActive: %v", err)
			}
			if got != tc.want {
				t.Errorf("got %v, want %v", got, tc.want)
			}
		})
	}
}

func TestCodexFindLastChatReply(t *testing.T) {
	codexHome := t.TempDir()
	t.Setenv("CODEX_HOME", codexHome)
	sid := "22222222-2222-2222-2222-222222222222"
	lines := []string{
		codexFunctionCall(t, chatMCPNamespaceCodex, "send_message", "call_1"),
		codexFunctionCallOutput(t, "call_1", "User responded: first"),
		codexFunctionCall(t, chatMCPNamespaceCodex, "send_message", "call_2"),
		codexFunctionCallOutput(t, "call_2", "User responded: second"),
	}
	path := writeCodexRollout(t, codexHome, sid, lines)
	got, err := codexFindLastChatReply(path, "send_message")
	if err != nil {
		t.Fatalf("codexFindLastChatReply: %v", err)
	}
	if got != "call_2" {
		t.Errorf("got %q, want call_2 (most recent send_message's call_id)", got)
	}
}
