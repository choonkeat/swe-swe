package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// TestMCPCallerIdentityEndToEnd is the decisive check that the caller's
// session identity survives the full hop: HTTP request -> mcpAuthMiddleware
// (context injection) -> MCP SDK StreamableHTTPHandler -> tool handler ctx.
// This is exactly the mechanism create_session relies on to inherit the
// *calling* session's git credentials, so it is exercised against the real
// SDK (real client over real HTTP), not mocked.
func TestMCPCallerIdentityEndToEnd(t *testing.T) {
	sid := "e2e-caller-sid"
	key := issueSessionKey(sid)
	defer clearSessionKey(sid)

	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0"}, nil)
	type whoamiIn struct{}
	type whoamiOut struct{}
	mcp.AddTool(srv, &mcp.Tool{Name: "whoami", Description: "echo the caller session"},
		func(ctx context.Context, req *mcp.CallToolRequest, _ whoamiIn) (*mcp.CallToolResult, whoamiOut, error) {
			return &mcp.CallToolResult{
				Content: []mcp.Content{&mcp.TextContent{Text: callerSessionFromContext(ctx)}},
			}, whoamiOut{}, nil
		})

	handler := mcp.NewStreamableHTTPHandler(
		func(r *http.Request) *mcp.Server { return srv },
		&mcp.StreamableHTTPOptions{Stateless: true},
	)
	ts := httptest.NewServer(mcpAuthMiddleware(handler))
	defer ts.Close()

	ctx := context.Background()
	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "0"}, nil)
	transport := &mcp.StreamableClientTransport{Endpoint: ts.URL + "/?key=" + key}
	cs, err := client.Connect(ctx, transport, nil)
	if err != nil {
		t.Fatalf("client connect: %v", err)
	}
	defer cs.Close()

	res, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: "whoami"})
	if err != nil {
		t.Fatalf("call tool: %v", err)
	}
	if res.IsError {
		t.Fatalf("tool returned error: %+v", res.Content)
	}
	var got string
	for _, c := range res.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			got += tc.Text
		}
	}
	if strings.TrimSpace(got) != sid {
		t.Fatalf("caller identity did not propagate end-to-end: got %q want %q", got, sid)
	}
}

// TestMCPUnknownKeyRejectedEndToEnd confirms the middleware rejects an
// unrecognized key before the SDK handler runs, over real HTTP.
func TestMCPUnknownKeyRejectedEndToEnd(t *testing.T) {
	handlerRan := false
	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0"}, nil)
	type emptyIn struct{}
	type emptyOut struct{}
	mcp.AddTool(srv, &mcp.Tool{Name: "ping", Description: "noop"},
		func(ctx context.Context, req *mcp.CallToolRequest, _ emptyIn) (*mcp.CallToolResult, emptyOut, error) {
			handlerRan = true
			return &mcp.CallToolResult{}, emptyOut{}, nil
		})
	handler := mcp.NewStreamableHTTPHandler(
		func(r *http.Request) *mcp.Server { return srv },
		&mcp.StreamableHTTPOptions{Stateless: true},
	)
	ts := httptest.NewServer(mcpAuthMiddleware(handler))
	defer ts.Close()

	// A direct HTTP POST with a bogus key must be rejected with 401.
	resp, err := http.Post(ts.URL+"/?key=bogus", "application/json", strings.NewReader("{}"))
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401 for unknown key, got %d", resp.StatusCode)
	}
	if handlerRan {
		t.Fatal("tool handler ran despite unauthorized key")
	}
}
