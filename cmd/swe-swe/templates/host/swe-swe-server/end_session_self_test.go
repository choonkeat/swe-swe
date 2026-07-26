package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// endSessionSelfHarness stands up the real orchestration tool set behind the
// real auth middleware, so end_session is exercised through the same path an
// agent uses. Returns a connected client session speaking as sid.
func endSessionSelfHarness(t *testing.T, sid string) *mcp.ClientSession {
	t.Helper()
	key := issueSessionKey(sid)
	t.Cleanup(func() { clearSessionKey(sid) })

	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0"}, nil)
	if err := registerOrchestrationTools(srv); err != nil {
		t.Fatalf("registerOrchestrationTools: %v", err)
	}
	handler := mcp.NewStreamableHTTPHandler(
		func(r *http.Request) *mcp.Server { return srv },
		&mcp.StreamableHTTPOptions{Stateless: true},
	)
	ts := httptest.NewServer(mcpAuthMiddleware(handler))
	t.Cleanup(ts.Close)

	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "0"}, nil)
	cs, err := client.Connect(context.Background(), &mcp.StreamableClientTransport{Endpoint: ts.URL + "/?key=" + key}, nil)
	if err != nil {
		t.Fatalf("client connect: %v", err)
	}
	t.Cleanup(func() { cs.Close() })
	return cs
}

// An agent ending itself must not have to name its own UUID: the chat-log
// commit flow tells it to call end_session with no arguments, and the target
// comes from the unforgeable per-session auth key instead.
func TestEndSessionToolSelfEndsWithoutUUID(t *testing.T) {
	sid := "self-end-sid"
	sess := &Session{UUID: sid}
	swapSessions(t, map[string]*Session{sid: sess})
	torn := make(chan string, 1)
	swapEndTeardown(t, func(u string) error { torn <- u; return nil })

	cs := endSessionSelfHarness(t, sid)
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "end_session",
		Arguments: map[string]any{},
	})
	if err != nil {
		t.Fatalf("call end_session: %v", err)
	}
	if res.IsError {
		t.Fatalf("end_session returned an error: %+v", res.Content)
	}
	if got := <-torn; got != sid {
		t.Errorf("tore down %q, want the calling session %q", got, sid)
	}
}

// Naming another session explicitly still works -- this is how the homepage and
// supervising agents end sessions they did not create.
func TestEndSessionToolStillAcceptsExplicitUUID(t *testing.T) {
	caller, other := "caller-sid", "other-sid"
	swapSessions(t, map[string]*Session{
		caller: {UUID: caller},
		other:  {UUID: other},
	})
	torn := make(chan string, 1)
	swapEndTeardown(t, func(u string) error { torn <- u; return nil })

	cs := endSessionSelfHarness(t, caller)
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "end_session",
		Arguments: map[string]any{"uuid": other},
	})
	if err != nil {
		t.Fatalf("call end_session: %v", err)
	}
	if res.IsError {
		t.Fatalf("end_session returned an error: %+v", res.Content)
	}
	if got := <-torn; got != other {
		t.Errorf("tore down %q, want the named session %q", got, other)
	}
}
