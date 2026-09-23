package main

import (
	"encoding/json"
	"fmt"
	"net"
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
		// The real MCP-less form: the `mcp` CLI takes the full server name.
		name: "command-line send via the mcp CLI's full server name passes",
		lines: []string{
			tlUserTyped("do the thing"),
			tlToolUse("Bash", map[string]any{"command": "mcp swe-swe-agent-chat send_message --text hi"}),
		},
		want: allowed,
	}, {
		name: "command-line progress via the mcp CLI chained after a command passes",
		lines: []string{
			tlUserTyped("do the thing"),
			tlToolUse("Bash", map[string]any{"command": "go test ./... ; mcp swe-swe-agent-chat send_progress --text done"}),
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

// runStopGuardMcpLess runs the guard as an MCP-less session sees it: a socket
// dir holding agent-chat's socket, and `mcp` / `curl` stubs first on PATH.
// mcpExit is what `mcp swe-swe-agent-chat` returns (0 = chat answers);
// curlExit is what the restart call returns. It reports the exit code, the
// guard's stderr, and the arguments curl was called with ("" = not called).
func runStopGuardMcpLess(t *testing.T, mcpExit, curlExit int) (int, string, string) {
	t.Helper()
	if _, err := exec.LookPath("jq"); err != nil {
		t.Skip("jq not installed; the guard no-ops without it")
	}
	dir := t.TempDir()
	// Short, not under t.TempDir(): a unix socket path is capped at 108 bytes.
	sockDir, err := os.MkdirTemp("", "sg")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(sockDir) })
	// A real socket file, left behind with nothing listening.
	ln, err := net.Listen("unix", filepath.Join(sockDir, "swe-swe-agent-chat.sock"))
	if err != nil {
		t.Fatal(err)
	}
	ln.(*net.UnixListener).SetUnlinkOnClose(false)
	ln.Close()

	bin := filepath.Join(dir, "bin")
	os.MkdirAll(bin, 0o755)
	curlLog := filepath.Join(dir, "curl.log")
	os.WriteFile(filepath.Join(bin, "mcp"), []byte(fmt.Sprintf("#!/bin/sh\nexit %d\n", mcpExit)), 0o755)
	os.WriteFile(filepath.Join(bin, "curl"), []byte(fmt.Sprintf("#!/bin/sh\necho \"$@\" >> %q\nexit %d\n", curlLog, curlExit)), 0o755)

	tp := filepath.Join(dir, "transcript.jsonl")
	os.WriteFile(tp, []byte(tlUserTyped("do the thing")+"\n"+tlAssistantText("answered in the terminal only")+"\n"), 0o644)

	cmd := exec.Command("/bin/sh", filepath.Join("hook-scripts", "swe-swe-stop-guard.sh"))
	cmd.Stdin = strings.NewReader(mustJSON(map[string]any{"transcript_path": tp, "stop_hook_active": false}))
	cmd.Env = append(filterEnv(os.Environ(), "AGENT_CHAT_PORT", "AGENT_CHAT_DISABLE", "PATH"),
		"PATH="+bin+":"+os.Getenv("PATH"),
		"SWE_MCP_DIR="+sockDir,
		"SESSION_UUID=sess-1",
		"MCP_AUTH_KEY=key-1",
		"SWE_SERVER_PORT=1977",
	)
	var stderr strings.Builder
	cmd.Stderr = &stderr
	runErr := cmd.Run()
	code := 0
	if ee, ok := runErr.(*exec.ExitError); ok {
		code = ee.ExitCode()
	} else if runErr != nil {
		t.Fatalf("running guard: %v", runErr)
	}
	called, _ := os.ReadFile(curlLog)
	return code, stderr.String(), string(called)
}

// Seen live on a no-Docker box: agent-chat's helper had stopped, its socket
// file was left behind, and the guard -- which only checked that the file
// exists -- kept ordering the agent to message a chat that could not hear it.
// The guard now asks whether chat answers, and when it does not, has swe-swe
// restart it before telling the agent to resend.
func TestStopGuardRestartsDeadAgentChat(t *testing.T) {
	t.Run("chat answers: the usual block, no restart", func(t *testing.T) {
		code, stderr, curl := runStopGuardMcpLess(t, 0, 0)
		if code != blocked || !strings.Contains(stderr, "BLOCKED:") {
			t.Fatalf("code %d stderr %q, want the usual block", code, stderr)
		}
		if curl != "" {
			t.Errorf("restart called although chat answers: %q", curl)
		}
	})
	t.Run("chat dead: restarted, then the agent is told to resend", func(t *testing.T) {
		code, stderr, curl := runStopGuardMcpLess(t, 1, 0)
		if code != blocked {
			t.Fatalf("code %d, want blocked so the agent resends", code)
		}
		if !strings.Contains(curl, "/api/session/sess-1/mcp-less/restart") ||
			!strings.Contains(curl, "name=swe-swe-agent-chat") || !strings.Contains(curl, "key=key-1") ||
			!strings.Contains(curl, "localhost:1977") {
			t.Errorf("restart call = %q", curl)
		}
		if !strings.Contains(stderr, "restarted") {
			t.Errorf("stderr %q, want it to say chat was restarted", stderr)
		}
	})
	t.Run("chat dead and restart fails: let the turn end", func(t *testing.T) {
		code, _, curl := runStopGuardMcpLess(t, 1, 22)
		if curl == "" {
			t.Error("restart was not attempted")
		}
		if code != allowed {
			t.Errorf("code %d, want allowed: blocking on a chat nobody can reach just loops", code)
		}
	})
}
