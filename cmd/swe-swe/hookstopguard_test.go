package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Transcript line helpers. The Stop guard reads the transcript as JSONL, so
// each helper emits exactly one line in the shape Claude Code writes.

func tlUserTyped(text string) string {
	return mustJSON(map[string]any{
		"type":    "user",
		"message": map[string]any{"content": text},
	})
}

func tlToolResult(text string) string {
	return mustJSON(map[string]any{
		"type": "user",
		"message": map[string]any{"content": []any{
			map[string]any{"type": "tool_result", "content": text},
		}},
	})
}

func tlToolUse(name string, input map[string]any) string {
	if input == nil {
		input = map[string]any{}
	}
	return mustJSON(map[string]any{
		"type": "assistant",
		"message": map[string]any{"content": []any{
			map[string]any{"type": "tool_use", "name": name, "input": input},
		}},
	})
}

func tlAssistantText(text string) string {
	return mustJSON(map[string]any{
		"type": "assistant",
		"message": map[string]any{"content": []any{
			map[string]any{"type": "text", "text": text},
		}},
	})
}

func mustJSON(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return string(b)
}

// runStopGuard writes the lines as a transcript, runs the shipped hook script
// against it, and reports the exit code (0 = allowed to stop, 2 = blocked).
func runStopGuard(t *testing.T, lines []string) int {
	t.Helper()
	if _, err := exec.LookPath("jq"); err != nil {
		t.Skip("jq not installed; the guard no-ops without it")
	}
	dir := t.TempDir()
	tp := filepath.Join(dir, "transcript.jsonl")
	body := strings.Join(lines, "\n") + "\n"
	if err := os.WriteFile(tp, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	script := filepath.Join("hook-scripts", "swe-swe-stop-guard.sh")
	cmd := exec.Command("/bin/sh", script)
	cmd.Stdin = strings.NewReader(mustJSON(map[string]any{
		"transcript_path":  tp,
		"stop_hook_active": false,
	}))
	// AGENT_CHAT_PORT makes the guard believe this session has a chat channel;
	// SWE_MCP_DIR must stay unset so the socket branch is not taken.
	cmd.Env = append(os.Environ(), "AGENT_CHAT_PORT=4000")
	cmd.Env = filterEnv(cmd.Env, "SWE_MCP_DIR", "AGENT_CHAT_DISABLE")
	var stderr strings.Builder
	cmd.Stderr = &stderr
	err := cmd.Run()
	code := 0
	if ee, ok := err.(*exec.ExitError); ok {
		code = ee.ExitCode()
	} else if err != nil {
		t.Fatalf("running guard: %v", err)
	}
	if code == 2 && !strings.Contains(stderr.String(), "BLOCKED:") {
		t.Fatalf("exit 2 without a BLOCKED instruction on stderr: %q", stderr.String())
	}
	return code
}

func filterEnv(env []string, drop ...string) []string {
	out := env[:0:0]
	for _, kv := range env {
		keep := true
		for _, d := range drop {
			if strings.HasPrefix(kv, d+"=") {
				keep = false
			}
		}
		if keep {
			out = append(out, kv)
		}
	}
	return out
}

const (
	allowed = 0
	blocked = 2
)

func TestStopGuard(t *testing.T) {
	const sendTool = "mcp__swe-swe-agent-chat__send_message"
	const progressTool = "mcp__swe-swe-agent-chat__send_progress"
	const drawTool = "mcp__swe-swe-agent-chat__draw"
	const verbalTool = "mcp__swe-swe-agent-chat__send_verbal_reply"
	const checkTool = "mcp__swe-swe-agent-chat__check_messages"

	tests := []struct {
		name  string
		lines []string
		want  int
	}{{
		// The bug this guard exists for: the agent answered in the terminal
		// only, which the user never sees.
		name: "terminal only answer is blocked",
		lines: []string{
			tlUserTyped("do the thing"),
			tlToolUse("Bash", map[string]any{"command": "ls"}),
			tlAssistantText("here is your answer"),
		},
		want: blocked,
	}, {
		name: "native send_message passes",
		lines: []string{
			tlUserTyped("do the thing"),
			tlToolUse(sendTool, map[string]any{"text": "done"}),
		},
		want: allowed,
	}, {
		name: "progress update passes",
		lines: []string{
			tlUserTyped("do the thing"),
			tlToolUse(progressTool, map[string]any{"text": "working"}),
		},
		want: allowed,
	}, {
		name: "draw passes",
		lines: []string{
			tlUserTyped("sketch it"),
			tlToolUse(drawTool, map[string]any{"svg": "<svg/>"}),
		},
		want: allowed,
	}, {
		name: "verbal reply passes",
		lines: []string{
			tlUserTyped("say it"),
			tlToolUse(verbalTool, map[string]any{"text": "spoken"}),
		},
		want: allowed,
	}, {
		name: "command-line send at command start passes",
		lines: []string{
			tlUserTyped("do the thing"),
			tlToolUse("Bash", map[string]any{"command": "mcp agent-chat send_message --text hi"}),
		},
		want: allowed,
	}, {
		name: "command-line send chained after another command passes",
		lines: []string{
			tlUserTyped("do the thing"),
			tlToolUse("Bash", map[string]any{"command": "make build && mcp agent-chat send_message --text hi"}),
		},
		want: allowed,
	}, {
		// The tightening: the tool name appearing as an argument is not a send.
		name: "tool name as a search argument is blocked",
		lines: []string{
			tlUserTyped("do the thing"),
			tlToolUse("Bash", map[string]any{"command": "grep -r 'agent-chat send_message' ."}),
		},
		want: blocked,
	}, {
		// The spoof the old text scan fell for: a log line that merely prints
		// the tool name.
		name: "log line naming the tool is blocked",
		lines: []string{
			tlUserTyped("do the thing"),
			tlToolResult("[info] calling mcp__swe-swe-agent-chat__send_message ... ok"),
		},
		want: blocked,
	}, {
		name: "empty check_messages queue is an allowed silent turn",
		lines: []string{
			tlUserTyped("anything for me?"),
			tlToolUse(checkTool, nil),
			tlToolResult(`{"queue":"empty"}`),
		},
		want: allowed,
	}, {
		// Turn-boundary fix: the chat reply arrives as a tool_result, so the
		// send that preceded it must not cover the new turn.
		name: "send before a chat reply does not cover the next turn",
		lines: []string{
			tlUserTyped("first task"),
			tlToolUse(sendTool, map[string]any{"text": "done, anything else?"}),
			tlToolResult("User responded: yes, second task"),
			tlToolUse("Bash", map[string]any{"command": "ls"}),
		},
		want: blocked,
	}, {
		name: "send after a chat reply passes",
		lines: []string{
			tlUserTyped("first task"),
			tlToolUse(sendTool, map[string]any{"text": "done, anything else?"}),
			tlToolResult("User responded: yes, second task"),
			tlToolUse(sendTool, map[string]any{"text": "second task done"}),
		},
		want: allowed,
	}, {
		name: "send before a check_messages delivery does not cover the next turn",
		lines: []string{
			tlUserTyped("first task"),
			tlToolUse(sendTool, map[string]any{"text": "done"}),
			tlToolResult("User said: now do the second thing"),
			tlToolUse("Bash", map[string]any{"command": "ls"}),
		},
		want: blocked,
	}, {
		// An ordinary tool_result must not start a turn, or every tool call
		// would reset the window and nothing would ever be allowed through.
		name: "ordinary tool results do not restart the turn",
		lines: []string{
			tlUserTyped("do the thing"),
			tlToolUse(sendTool, map[string]any{"text": "done"}),
			tlToolResult("some ordinary command output"),
		},
		want: allowed,
	}, {
		name: "malformed transcript lines are skipped not fatal",
		lines: []string{
			tlUserTyped("do the thing"),
			`{"type":"assistant", TRUNCATED`,
			tlToolUse(sendTool, map[string]any{"text": "done"}),
		},
		want: allowed,
	}}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := runStopGuard(t, tt.lines); got != tt.want {
				t.Errorf("exit code = %d, want %d", got, tt.want)
			}
		})
	}
}

// stop_hook_active means this stop was already blocked once; the guard must
// let the second attempt through so the agent cannot be trapped in a loop.
func TestStopGuardOneNudgePerTurn(t *testing.T) {
	if _, err := exec.LookPath("jq"); err != nil {
		t.Skip("jq not installed; the guard no-ops without it")
	}
	dir := t.TempDir()
	tp := filepath.Join(dir, "transcript.jsonl")
	body := tlUserTyped("do the thing") + "\n" + tlAssistantText("terminal only") + "\n"
	if err := os.WriteFile(tp, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("/bin/sh", filepath.Join("hook-scripts", "swe-swe-stop-guard.sh"))
	cmd.Stdin = strings.NewReader(mustJSON(map[string]any{
		"transcript_path":  tp,
		"stop_hook_active": true,
	}))
	cmd.Env = filterEnv(append(os.Environ(), "AGENT_CHAT_PORT=4000"), "SWE_MCP_DIR", "AGENT_CHAT_DISABLE")
	if err := cmd.Run(); err != nil {
		t.Errorf("stop_hook_active should exit 0, got %v", err)
	}
}
