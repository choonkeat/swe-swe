package main

import (
	"context"
	"encoding/json"
	"os/exec"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// orchToolClient wires registerOrchestrationTools to an in-memory MCP client
// whose calls carry callerUUID as the authenticated calling session, the
// same identity mcpAuthMiddleware injects for real /mcp requests.
func orchToolClient(t *testing.T, callerUUID string) *mcp.ClientSession {
	t.Helper()
	server := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.0.0"}, nil)
	if err := registerOrchestrationTools(server); err != nil {
		t.Fatalf("registerOrchestrationTools: %v", err)
	}
	ct, st := mcp.NewInMemoryTransports()
	ctx := withCallerSession(context.Background(), callerUUID)
	if _, err := server.Connect(ctx, st, nil); err != nil {
		t.Fatalf("server connect: %v", err)
	}
	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "0.0.0"}, nil)
	cs, err := client.Connect(context.Background(), ct, nil)
	if err != nil {
		t.Fatalf("client connect: %v", err)
	}
	t.Cleanup(func() { cs.Close() })
	return cs
}

// fakeChatSession registers an in-memory session that looks like it has an
// agent chat and a browser attached (so no nudge goroutine is spawned).
func fakeChatSession(t *testing.T, uuid string) {
	t.Helper()
	sess := &Session{UUID: uuid, AgentChatPort: 4001, Cmd: &exec.Cmd{}, wsClients: map[*SafeConn]bool{{}: true}}
	sessionsMu.Lock()
	sessions[uuid] = sess
	sessionsMu.Unlock()
	t.Cleanup(func() {
		sessionsMu.Lock()
		delete(sessions, uuid)
		sessionsMu.Unlock()
	})
}

func callToolText(t *testing.T, cs *mcp.ClientSession, name string, args map[string]any) (string, error) {
	t.Helper()
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		return "", err
	}
	var sb strings.Builder
	for _, c := range res.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			sb.WriteString(tc.Text)
		}
	}
	if res.IsError {
		return "", &toolError{sb.String()}
	}
	return sb.String(), nil
}

type toolError struct{ msg string }

func (e *toolError) Error() string { return e.msg }

// The receiving agent can only reply if the message names the sender. That
// name must come from the server, so an agent that never read the docs (or
// one that lies) still produces a truthful signoff.
func TestSendChatMessageIsServerStampedWithSenderAddress(t *testing.T) {
	withLocalUnique(t, "here")
	fakeChatSession(t, "recv-1")
	var delivered string
	swapOrchestrator(t, func(_ int, tool string, args any) (string, error) {
		if tool == "send_chat_message" {
			delivered = args.(map[string]string)["text"]
		}
		return `{"ok":true}`, nil
	})
	cs := orchToolClient(t, "sender-1")

	// Fully qualified local address resolves to the in-memory session.
	if _, err := callToolText(t, cs, "send_chat_message", map[string]any{"uuid": "here/recv-1", "text": "hello"}); err != nil {
		t.Fatalf("send_chat_message: %v", err)
	}
	want := "hello\n\n(via send_chat_message from here/sender-1)"
	if delivered != want {
		t.Fatalf("delivered %q, want %q", delivered, want)
	}

	// Bare uuid keeps meaning this box.
	delivered = ""
	if _, err := callToolText(t, cs, "send_chat_message", map[string]any{"uuid": "recv-1", "text": "again"}); err != nil {
		t.Fatalf("send_chat_message bare uuid: %v", err)
	}
	if !strings.HasSuffix(delivered, "(via send_chat_message from here/sender-1)") {
		t.Fatalf("bare-uuid delivery missing signoff: %q", delivered)
	}
}

// A foreign unique is unreachable until the relay phase lands: the error must
// say so, and must not be mistaken for "session not found".
func TestSessionToolsRejectForeignUniqueAsUnreachable(t *testing.T) {
	withLocalUnique(t, "here")
	fakeChatSession(t, "recv-1")
	swapOrchestrator(t, func(_ int, _ string, _ any) (string, error) { return `{}`, nil })
	cs := orchToolClient(t, "sender-1")
	cases := map[string]map[string]any{
		"send_chat_message":  {"uuid": "there/recv-1", "text": "x"},
		"get_chat_history":   {"uuid": "there/recv-1"},
		"get_session_output": {"uuid": "there/recv-1"},
		"send_session_input": {"uuid": "there/recv-1", "text": "x"},
		"set_session_name":   {"uuid": "there/recv-1", "name": "n"},
		"end_session":        {"uuid": "there/recv-1"},
	}
	for tool, args := range cases {
		_, err := callToolText(t, cs, tool, args)
		if err == nil || !strings.Contains(err.Error(), "unreachable: there") {
			t.Errorf("%s: err=%v, want unreachable: there", tool, err)
		}
	}
}

func TestListSessionsReportsAddress(t *testing.T) {
	withLocalUnique(t, "here")
	fakeChatSession(t, "recv-1")
	cs := orchToolClient(t, "sender-1")
	out, err := callToolText(t, cs, "list_sessions", nil)
	if err != nil {
		t.Fatalf("list_sessions: %v", err)
	}
	var got []struct {
		UUID    string `json:"uuid"`
		Address string `json:"address"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("unmarshal %q: %v", out, err)
	}
	for _, s := range got {
		if s.UUID == "recv-1" {
			if s.Address != "here/recv-1" {
				t.Fatalf("address = %q, want here/recv-1", s.Address)
			}
			return
		}
	}
	t.Fatalf("recv-1 missing from %s", out)
}
