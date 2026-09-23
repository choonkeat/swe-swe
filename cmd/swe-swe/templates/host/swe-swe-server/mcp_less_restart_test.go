package main

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// Seen live on a no-Docker box: a session's chat helper stopped, nothing
// started it again (a container restart is the only backstop, and there is no
// container), and its leftover socket kept the agent believing chat was up.
// A single helper can now be restarted on its own, for its session only, with
// a cap so one that keeps crashing cannot loop forever.

func newStubFleet(t *testing.T, mode string) (*mcpLessFleet, string, string) {
	t.Helper()
	logPath := filepath.Join(t.TempDir(), "invocations.log")
	old := mcpCliProxyBin
	mcpCliProxyBin = stubProxyBin(t, logPath)
	t.Cleanup(func() { mcpCliProxyBin = old })
	sockDir := filepath.Join(t.TempDir(), "mcp")
	f := newMcpLessFleet(mode, sockDir, append(os.Environ(), "MCP_STUB_LOG="+logPath), t.TempDir())
	if err := f.launchAll(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(f.stop)
	return f, logPath, sockDir
}

func TestMcpLessFleetRestart(t *testing.T) {
	f, logPath, sockDir := newStubFleet(t, "chat")
	waitForLines(t, logPath, 4)

	old := f.proc("swe-swe-agent-chat")
	if old == nil {
		t.Fatal("agent-chat proxy not tracked")
	}
	// The leftover socket from the stopped helper.
	stale := filepath.Join(sockDir, "swe-swe-agent-chat.sock")
	if err := os.WriteFile(stale, nil, 0o600); err != nil {
		t.Fatal(err)
	}

	if err := f.restart("swe-swe-agent-chat", time.Now()); err != nil {
		t.Fatalf("restart: %v", err)
	}

	lines := waitForLines(t, logPath, 5)
	if !strings.Contains(lines[4], "--name swe-swe-agent-chat") {
		t.Errorf("fifth launch = %q, want the agent-chat proxy again", lines[4])
	}
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Errorf("leftover socket still there (err %v); the new helper must bind a fresh one", err)
	}
	newer := f.proc("swe-swe-agent-chat")
	if newer == nil || newer == old {
		t.Fatal("restart did not replace the tracked process")
	}
	// The old helper is gone (the reaper has waited on it, so signal 0 fails).
	deadline := time.Now().Add(3 * time.Second)
	for syscall.Kill(old.Process.Pid, 0) == nil && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if syscall.Kill(old.Process.Pid, 0) == nil {
		t.Error("old agent-chat proxy still running after restart")
	}
}

func TestMcpLessFleetRestartLimit(t *testing.T) {
	f, _, _ := newStubFleet(t, "chat")
	now := time.Now()
	for i := 0; i < mcpLessRestartLimit; i++ {
		if err := f.restart("swe-swe-agent-chat", now.Add(time.Duration(i)*time.Second)); err != nil {
			t.Fatalf("restart %d: %v", i+1, err)
		}
	}
	err := f.restart("swe-swe-agent-chat", now.Add(10*time.Second))
	if !errors.Is(err, errMcpLessRestartLimit) {
		t.Fatalf("restart past the cap: err = %v, want errMcpLessRestartLimit", err)
	}
	// Once the window has passed, it is allowed again.
	if err := f.restart("swe-swe-agent-chat", now.Add(mcpLessRestartWindow+time.Minute)); err != nil {
		t.Fatalf("restart after the window: %v", err)
	}
}

func TestMcpLessFleetRestartUnknown(t *testing.T) {
	f, _, _ := newStubFleet(t, "terminal")
	// A terminal session has no chat helper to restart.
	if err := f.restart("swe-swe-agent-chat", time.Now()); !errors.Is(err, errMcpLessUnknownProxy) {
		t.Fatalf("err = %v, want errMcpLessUnknownProxy", err)
	}
}

func TestHandleMcpLessRestartAPI(t *testing.T) {
	const uuid = "mcp-less-restart-sess"
	const key = "k-mcp-less-restart"
	f, _, _ := newStubFleet(t, "chat")
	registerTestSession(t, uuid, &Session{UUID: uuid, McpLessFleet: f})
	registerTestSessionKey(t, uuid, key)
	registerTestSession(t, "no-fleet-sess", &Session{UUID: "no-fleet-sess"})
	registerTestSessionKey(t, "no-fleet-sess", "k-no-fleet")

	post := func(path string) int {
		w := httptest.NewRecorder()
		handleMcpLessRestartAPI(w, httptest.NewRequest(http.MethodPost, path, nil))
		return w.Code
	}
	base := "/api/session/" + uuid + "/mcp-less/restart"
	if code := post(base + "?name=swe-swe-agent-chat&key=wrong"); code != http.StatusUnauthorized {
		t.Errorf("wrong key: got %d, want 401", code)
	}
	if code := post(base + "?name=swe-swe-agent-chat&key=" + key); code != http.StatusOK {
		t.Errorf("valid restart: got %d, want 200", code)
	}
	if code := post(base + "?name=nope&key=" + key); code != http.StatusNotFound {
		t.Errorf("unknown helper: got %d, want 404", code)
	}
	if code := post("/api/session/no-fleet-sess/mcp-less/restart?name=swe-swe-agent-chat&key=k-no-fleet"); code != http.StatusConflict {
		t.Errorf("session without MCP-less helpers: got %d, want 409", code)
	}
	w := httptest.NewRecorder()
	handleMcpLessRestartAPI(w, httptest.NewRequest(http.MethodGet, base+"?name=swe-swe-agent-chat&key="+key, nil))
	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("GET: got %d, want 405", w.Code)
	}
}

// The Stop hook calls the restart from inside the session with the session's
// MCP key and no browser cookie; the cookie gate must let it through to the
// handler's own key check rather than bounce it to the login page.
func TestMcpLessRestartSkipsCookieGate(t *testing.T) {
	reached := false
	h := authMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { reached = true }), "secret")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/session/some-uuid/mcp-less/restart?name=swe-swe-agent-chat&key=k", nil))
	if !reached {
		t.Fatalf("cookie gate answered %d instead of passing the request to the handler", w.Code)
	}
}

// restart_agent_chat lets the agent itself bring chat back when a chat command
// fails mid-turn. It only exists in MCP-less mode (natively, the agent's own
// MCP client owns agent-chat), and it restarts the CALLER's helper -- never
// another session's.
func TestRestartAgentChatTool(t *testing.T) {
	listTools := func(t *testing.T) map[string]bool {
		cs := orchToolClient(t, "whoever")
		res, err := cs.ListTools(context.Background(), nil)
		if err != nil {
			t.Fatal(err)
		}
		names := map[string]bool{}
		for _, tool := range res.Tools {
			names[tool.Name] = true
		}
		return names
	}

	t.Run("not offered with native MCP", func(t *testing.T) {
		t.Setenv("SWE_MCP_LESS", "")
		if listTools(t)["restart_agent_chat"] {
			t.Error("restart_agent_chat offered outside MCP-less mode")
		}
	})

	t.Run("restarts the caller's agent-chat helper", func(t *testing.T) {
		t.Setenv("SWE_MCP_LESS", "1")
		const uuid = "restart-tool-sess"
		f, logPath, _ := newStubFleet(t, "chat")
		waitForLines(t, logPath, 4)
		registerTestSession(t, uuid, &Session{UUID: uuid, McpLessFleet: f})

		if !listTools(t)["restart_agent_chat"] {
			t.Fatal("restart_agent_chat not offered in MCP-less mode")
		}
		cs := orchToolClient(t, uuid)
		text, err := callToolText(t, cs, "restart_agent_chat", map[string]any{})
		if err != nil {
			t.Fatalf("restart_agent_chat: %v", err)
		}
		if !strings.Contains(text, "restarted") {
			t.Errorf("reply = %q, want it to say chat was restarted", text)
		}
		lines := waitForLines(t, logPath, 5)
		if !strings.Contains(lines[4], "--name swe-swe-agent-chat") {
			t.Errorf("fifth launch = %q, want agent-chat", lines[4])
		}
	})
}
