package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// helper: send one JSON-RPC line through runMCP and return all response lines
func mcpExchange(t *testing.T, lines ...string) []mcpResponse {
	t.Helper()
	input := strings.Join(lines, "\n") + "\n"
	var out bytes.Buffer
	runMCP(strings.NewReader(input), &out, "ws://localhost:0/nonexistent")
	var responses []mcpResponse
	for _, line := range strings.Split(strings.TrimSpace(out.String()), "\n") {
		if line == "" {
			continue
		}
		var resp mcpResponse
		if err := json.Unmarshal([]byte(line), &resp); err != nil {
			t.Fatalf("failed to parse response line %q: %v", line, err)
		}
		responses = append(responses, resp)
	}
	return responses
}

func TestMCPInitialize(t *testing.T) {
	responses := mcpExchange(t, `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`)
	if len(responses) != 1 {
		t.Fatalf("expected 1 response, got %d", len(responses))
	}
	resp := responses[0]
	if resp.Error != nil {
		t.Fatalf("unexpected error: %+v", resp.Error)
	}
	result, ok := resp.Result.(map[string]interface{})
	if !ok {
		t.Fatalf("expected map result, got %T", resp.Result)
	}
	if v, _ := result["protocolVersion"].(string); v != "2025-11-25" {
		t.Errorf("protocolVersion = %q, want %q", v, "2025-11-25")
	}
	caps, _ := result["capabilities"].(map[string]interface{})
	if caps == nil {
		t.Fatal("missing capabilities")
	}
	if _, ok := caps["tools"]; !ok {
		t.Error("missing tools capability")
	}
	info, _ := result["serverInfo"].(map[string]interface{})
	if info == nil {
		t.Fatal("missing serverInfo")
	}
	if name, _ := info["name"].(string); name != "swe-swe-preview" {
		t.Errorf("serverInfo.name = %q, want %q", name, "swe-swe-preview")
	}
}

func TestMCPToolsList(t *testing.T) {
	responses := mcpExchange(t,
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`,
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}`,
	)
	// initialize response + tools/list response (notification has no response)
	if len(responses) != 2 {
		t.Fatalf("expected 2 responses, got %d", len(responses))
	}
	resp := responses[1] // tools/list response
	if resp.Error != nil {
		t.Fatalf("unexpected error: %+v", resp.Error)
	}
	result, ok := resp.Result.(map[string]interface{})
	if !ok {
		t.Fatalf("expected map result, got %T", resp.Result)
	}
	tools, ok := result["tools"].([]interface{})
	if !ok {
		t.Fatalf("expected tools array, got %T", result["tools"])
	}
	if len(tools) != 2 {
		t.Fatalf("expected 2 tools, got %d", len(tools))
	}
	names := make(map[string]bool)
	for _, tool := range tools {
		m, _ := tool.(map[string]interface{})
		name, _ := m["name"].(string)
		names[name] = true
		// Verify inputSchema exists
		if m["inputSchema"] == nil {
			t.Errorf("tool %q missing inputSchema", name)
		}
		// Verify description exists
		desc, _ := m["description"].(string)
		if desc == "" {
			t.Errorf("tool %q missing description", name)
		}
	}
	if !names["preview_query"] {
		t.Error("missing tool preview_query")
	}
	if !names["preview_listen"] {
		t.Error("missing tool preview_listen")
	}
}

func TestMCPToolsCallUnknown(t *testing.T) {
	responses := mcpExchange(t,
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"nonexistent","arguments":{}}}`,
	)
	if len(responses) != 1 {
		t.Fatalf("expected 1 response, got %d", len(responses))
	}
	resp := responses[0]
	if resp.Error == nil {
		t.Fatal("expected error response")
	}
	if resp.Error.Code != -32602 {
		t.Errorf("error code = %d, want -32602", resp.Error.Code)
	}
}

func TestMCPToolsCallPreviewQueryNoConnection(t *testing.T) {
	responses := mcpExchange(t,
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"preview_query","arguments":{"selector":"h1"}}}`,
	)
	if len(responses) != 1 {
		t.Fatalf("expected 1 response, got %d", len(responses))
	}
	resp := responses[0]
	if resp.Error != nil {
		t.Fatalf("unexpected JSON-RPC error: %+v", resp.Error)
	}
	result, ok := resp.Result.(map[string]interface{})
	if !ok {
		t.Fatalf("expected map result, got %T", resp.Result)
	}
	isError, _ := result["isError"].(bool)
	if !isError {
		t.Error("expected isError=true in tool result")
	}
}

func TestMCPToolsCallPreviewListenNoConnection(t *testing.T) {
	responses := mcpExchange(t,
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"preview_listen","arguments":{"duration_seconds":1}}}`,
	)
	if len(responses) != 1 {
		t.Fatalf("expected 1 response, got %d", len(responses))
	}
	resp := responses[0]
	if resp.Error != nil {
		t.Fatalf("unexpected JSON-RPC error: %+v", resp.Error)
	}
	result, ok := resp.Result.(map[string]interface{})
	if !ok {
		t.Fatalf("expected map result, got %T", resp.Result)
	}
	isError, _ := result["isError"].(bool)
	if !isError {
		t.Error("expected isError=true in tool result")
	}
}

func TestMCPParseError(t *testing.T) {
	responses := mcpExchange(t, `{not valid json}`)
	if len(responses) != 1 {
		t.Fatalf("expected 1 response, got %d", len(responses))
	}
	resp := responses[0]
	if resp.Error == nil {
		t.Fatal("expected error response")
	}
	if resp.Error.Code != -32700 {
		t.Errorf("error code = %d, want -32700", resp.Error.Code)
	}
}

func TestMCPMethodNotFound(t *testing.T) {
	responses := mcpExchange(t, `{"jsonrpc":"2.0","id":1,"method":"unknown/method","params":{}}`)
	if len(responses) != 1 {
		t.Fatalf("expected 1 response, got %d", len(responses))
	}
	resp := responses[0]
	if resp.Error == nil {
		t.Fatal("expected error response")
	}
	if resp.Error.Code != -32601 {
		t.Errorf("error code = %d, want -32601", resp.Error.Code)
	}
}

func TestMCPPing(t *testing.T) {
	responses := mcpExchange(t, `{"jsonrpc":"2.0","id":42,"method":"ping","params":{}}`)
	if len(responses) != 1 {
		t.Fatalf("expected 1 response, got %d", len(responses))
	}
	resp := responses[0]
	if resp.Error != nil {
		t.Fatalf("unexpected error: %+v", resp.Error)
	}
	// ping should return an empty result object
	result, ok := resp.Result.(map[string]interface{})
	if !ok {
		t.Fatalf("expected map result, got %T", resp.Result)
	}
	if len(result) != 0 {
		t.Errorf("expected empty result, got %v", result)
	}
}

// wsUpgrader for test servers
var testUpgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

func TestMCPToolsCallPreviewQueryIntegration(t *testing.T) {
	// Start a mock WebSocket debug server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := testUpgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Logf("upgrade error: %v", err)
			return
		}
		defer conn.Close()

		// Read the query message
		_, msg, err := conn.ReadMessage()
		if err != nil {
			t.Logf("read error: %v", err)
			return
		}

		// Parse query to get the ID
		var query struct {
			T        string `json:"t"`
			ID       string `json:"id"`
			Selector string `json:"selector"`
		}
		json.Unmarshal(msg, &query)

		// Send back a mock DOM result
		result := fmt.Sprintf(`{"t":"queryResult","id":"%s","selector":"%s","found":true,"text":"Hello World","html":"<h1>Hello World</h1>","visible":true}`, query.ID, query.Selector)
		conn.WriteMessage(websocket.TextMessage, []byte(result))
	}))
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")

	input := fmt.Sprintf(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"preview_query","arguments":{"selector":"h1"}}}`)
	var out bytes.Buffer
	runMCP(strings.NewReader(input+"\n"), &out, wsURL)

	var responses []mcpResponse
	for _, line := range strings.Split(strings.TrimSpace(out.String()), "\n") {
		if line == "" {
			continue
		}
		var resp mcpResponse
		if err := json.Unmarshal([]byte(line), &resp); err != nil {
			t.Fatalf("failed to parse: %v", err)
		}
		responses = append(responses, resp)
	}

	if len(responses) != 1 {
		t.Fatalf("expected 1 response, got %d", len(responses))
	}
	resp := responses[0]
	if resp.Error != nil {
		t.Fatalf("unexpected error: %+v", resp.Error)
	}
	result, ok := resp.Result.(map[string]interface{})
	if !ok {
		t.Fatalf("expected map result, got %T", resp.Result)
	}
	isError, _ := result["isError"].(bool)
	if isError {
		t.Fatal("expected isError=false")
	}
	content, _ := result["content"].([]interface{})
	if len(content) == 0 {
		t.Fatal("expected content")
	}
	first, _ := content[0].(map[string]interface{})
	text, _ := first["text"].(string)
	if !strings.Contains(text, "Hello World") {
		t.Errorf("expected text to contain 'Hello World', got %q", text)
	}
}

func TestMCPToolsCallPreviewListenIntegration(t *testing.T) {
	// Start a mock WebSocket debug server that sends messages
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := testUpgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Logf("upgrade error: %v", err)
			return
		}
		defer conn.Close()

		// Send a few messages with small delays
		messages := []string{
			`{"t":"console","level":"log","message":"App started"}`,
			`{"t":"console","level":"error","message":"Something went wrong"}`,
			`{"t":"network","method":"GET","url":"http://localhost:3000/api","status":200}`,
		}
		for _, msg := range messages {
			conn.WriteMessage(websocket.TextMessage, []byte(msg))
			time.Sleep(50 * time.Millisecond)
		}
		// Keep connection open until client disconnects
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	}))
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")

	// Use short duration so test completes quickly
	input := fmt.Sprintf(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"preview_listen","arguments":{"duration_seconds":1}}}`)
	var out bytes.Buffer
	runMCP(strings.NewReader(input+"\n"), &out, wsURL)

	var responses []mcpResponse
	for _, line := range strings.Split(strings.TrimSpace(out.String()), "\n") {
		if line == "" {
			continue
		}
		var resp mcpResponse
		if err := json.Unmarshal([]byte(line), &resp); err != nil {
			t.Fatalf("failed to parse: %v", err)
		}
		responses = append(responses, resp)
	}

	if len(responses) != 1 {
		t.Fatalf("expected 1 response, got %d", len(responses))
	}
	resp := responses[0]
	if resp.Error != nil {
		t.Fatalf("unexpected error: %+v", resp.Error)
	}
	result, ok := resp.Result.(map[string]interface{})
	if !ok {
		t.Fatalf("expected map result, got %T", resp.Result)
	}
	isError, _ := result["isError"].(bool)
	if isError {
		t.Fatal("expected isError=false")
	}
	content, _ := result["content"].([]interface{})
	if len(content) == 0 {
		t.Fatal("expected content")
	}
	first, _ := content[0].(map[string]interface{})
	text, _ := first["text"].(string)
	if !strings.Contains(text, "App started") {
		t.Errorf("expected collected messages to contain 'App started', got %q", text)
	}
	if !strings.Contains(text, "Something went wrong") {
		t.Errorf("expected collected messages to contain 'Something went wrong', got %q", text)
	}
}
