package main

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

func specNames(specs []proxySpec) map[string]bool {
	m := map[string]bool{}
	for _, s := range specs {
		m[s.Name] = true
	}
	return m
}

// TestMcpLessProxySpecs pins the gating rule: swe-swe-server launches the whole
// fleet per session, but the agent-chat proxy only for chat sessions.
func TestMcpLessProxySpecs(t *testing.T) {
	t.Run("chat session includes agent-chat and the full fleet", func(t *testing.T) {
		specs := mcpLessProxySpecs("chat")
		if len(specs) != 5 {
			t.Fatalf("chat: want 5 specs, got %d (%v)", len(specs), specNames(specs))
		}
		for _, want := range []string{"swe-swe-agent-chat", "swe-swe-playwright", "swe-swe-preview", "swe-swe-whiteboard", "swe-swe"} {
			if !specNames(specs)[want] {
				t.Errorf("chat fleet missing %q", want)
			}
		}
	})

	t.Run("terminal session omits agent-chat", func(t *testing.T) {
		specs := mcpLessProxySpecs("terminal")
		if specNames(specs)["swe-swe-agent-chat"] {
			t.Error("terminal session must NOT launch the agent-chat proxy")
		}
		if len(specs) != 4 {
			t.Fatalf("terminal: want 4 specs, got %d (%v)", len(specs), specNames(specs))
		}
	})

	t.Run("empty/default mode is treated as non-chat", func(t *testing.T) {
		if specNames(mcpLessProxySpecs(""))["swe-swe-agent-chat"] {
			t.Error("default (terminal) mode must NOT launch the agent-chat proxy")
		}
	})

	t.Run("every spec is well-formed", func(t *testing.T) {
		for _, s := range mcpLessProxySpecs("chat") {
			if s.Name == "" || len(s.Argv) == 0 {
				t.Errorf("malformed spec: %+v", s)
			}
			if s.socketName() != s.Name+".sock" {
				t.Errorf("socket name for %q = %q", s.Name, s.socketName())
			}
		}
	})
}

// stubProxyBin writes a tiny script that records its argv (one line per
// invocation) to $MCP_STUB_LOG then blocks, standing in for mcp-cli-proxy so we
// can assert what launchMcpLessFleet spawned without real npx children.
func stubProxyBin(t *testing.T, logPath string) string {
	t.Helper()
	dir := t.TempDir()
	bin := filepath.Join(dir, "mcp-cli-proxy")
	// exec the sleep so the process we kill IS the sleep (no orphaned child
	// holding the inherited stdout pipe open, which would stall `go test`).
	script := "#!/bin/sh\nprintf 'cwd=%s %s\\n' \"$(pwd)\" \"$*\" >> \"" + logPath + "\"\nexec sleep 60\n"
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return bin
}

func waitForLines(t *testing.T, path string, want int) []string {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		b, _ := os.ReadFile(path)
		lines := strings.Split(strings.TrimSpace(string(b)), "\n")
		if len(b) > 0 && len(lines) >= want {
			return lines
		}
		time.Sleep(20 * time.Millisecond)
	}
	b, _ := os.ReadFile(path)
	t.Fatalf("timed out waiting for %d proxy invocations; got:\n%s", want, b)
	return nil
}

// TestLaunchMcpLessFleet proves the launcher spawns exactly the gated set with
// the right --name/--socket/-- argv, creates the socket dir, and that
// stopMcpLessFleet kills them.
func TestLaunchMcpLessFleet(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "invocations.log")
	old := mcpCliProxyBin
	mcpCliProxyBin = stubProxyBin(t, logPath)
	t.Cleanup(func() { mcpCliProxyBin = old })

	sockDir := filepath.Join(t.TempDir(), "run", "mcp", "uuid-123")
	env := append(os.Environ(), "MCP_STUB_LOG="+logPath)
	workDir := t.TempDir()

	cmds, err := launchMcpLessFleet("chat", sockDir, env, workDir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { stopMcpLessFleet(cmds) })

	if len(cmds) != 5 {
		t.Fatalf("want 5 started proxies, got %d", len(cmds))
	}
	if _, err := os.Stat(sockDir); err != nil {
		t.Errorf("socket dir not created: %v", err)
	}

	lines := waitForLines(t, logPath, 5)
	joined := strings.Join(lines, "\n")
	// Every proxy must run with the session's workDir as cwd -- cwd-dependent
	// tools (agent-chat autocomplete, export_chat_md) rely on it.
	for _, line := range lines {
		if !strings.HasPrefix(line, "cwd="+workDir+" ") {
			t.Errorf("proxy not launched in session workDir %q: %s", workDir, line)
		}
	}
	for _, name := range []string{"swe-swe-agent-chat", "swe-swe-playwright", "swe-swe-preview", "swe-swe-whiteboard", "swe-swe"} {
		wantSock := filepath.Join(sockDir, name+".sock")
		if !strings.Contains(joined, "--name "+name+" ") {
			t.Errorf("no invocation named %q in:\n%s", name, joined)
		}
		if !strings.Contains(joined, "--socket "+wantSock+" ") {
			t.Errorf("proxy %q missing socket %q in:\n%s", name, wantSock, joined)
		}
	}

	// Teardown kills them: after stop, each pid is eventually reaped.
	stopMcpLessFleet(cmds)
	for _, c := range cmds {
		pid := c.Process.Pid
		gone := false
		for i := 0; i < 100; i++ {
			if err := c.Process.Signal(syscall.Signal(0)); err != nil {
				gone = true
				break
			}
			time.Sleep(20 * time.Millisecond)
		}
		if !gone {
			t.Errorf("proxy pid %d still alive after stop", pid)
		}
	}
}

// TestLaunchMcpLessFleet_TerminalOmitsAgentChat proves a terminal session's
// launched fleet excludes agent-chat.
func TestLaunchMcpLessFleet_TerminalOmitsAgentChat(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "invocations.log")
	old := mcpCliProxyBin
	mcpCliProxyBin = stubProxyBin(t, logPath)
	t.Cleanup(func() { mcpCliProxyBin = old })

	sockDir := filepath.Join(t.TempDir(), "mcp")
	// Empty workDir = inherit the server's cwd (a session with no workDir).
	cmds, err := launchMcpLessFleet("terminal", sockDir, append(os.Environ(), "MCP_STUB_LOG="+logPath), "")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { stopMcpLessFleet(cmds) })

	if len(cmds) != 4 {
		t.Fatalf("terminal: want 4 started proxies, got %d", len(cmds))
	}
	lines := waitForLines(t, logPath, 4)
	if strings.Contains(strings.Join(lines, "\n"), "swe-swe-agent-chat") {
		t.Error("terminal fleet must not launch agent-chat")
	}
}
