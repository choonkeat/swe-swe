package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestParseArgs(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		wantErr bool
	}{
		{
			name:    "valid args",
			args:    []string{"--init-method", "POST", "--init-url", "http://localhost/start", "--", "echo", "hello"},
			wantErr: false,
		},
		{
			name:    "with headers and body",
			args:    []string{"--init-method", "POST", "--init-url", "http://localhost/start", "--init-header", "Content-Type: application/json", "--init-request-body", "{}", "--", "echo"},
			wantErr: false,
		},
		{
			name:    "missing separator",
			args:    []string{"--init-method", "POST", "--init-url", "http://localhost/start"},
			wantErr: true,
		},
		{
			name:    "empty command after separator",
			args:    []string{"--init-method", "POST", "--init-url", "http://localhost/start", "--"},
			wantErr: true,
		},
		{
			name:    "unknown flag",
			args:    []string{"--unknown", "value", "--", "echo"},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := parseArgs(tt.args)
			if (err != nil) != tt.wantErr {
				t.Errorf("parseArgs() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestJSONRPCMethod(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{`{"jsonrpc":"2.0","method":"initialize","id":1}`, "initialize"},
		{`{"jsonrpc":"2.0","method":"tools/list","id":2}`, "tools/list"},
		{`{"jsonrpc":"2.0","method":"tools/call","id":3}`, "tools/call"},
		{`not json`, ""},
		{`{"jsonrpc":"2.0","id":1}`, ""},
	}

	for _, tt := range tests {
		got := jsonRPCMethod([]byte(tt.input))
		if got != tt.want {
			t.Errorf("jsonRPCMethod(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestDoInit(t *testing.T) {
	var mu sync.Mutex
	var receivedMethod string
	var receivedBody string
	var receivedHeaders http.Header
	var callCount int

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		callCount++
		receivedMethod = r.Method
		receivedHeaders = r.Header.Clone()
		body, _ := io.ReadAll(r.Body)
		receivedBody = string(body)
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"started"}`))
	}))
	defer server.Close()

	cfg := config{
		initMethod:      "POST",
		initURL:         server.URL + "/api/session/test-uuid/browser/start",
		initHeaders:     []string{"Content-Type: application/json", "X-Custom: test-value"},
		initRequestBody: `{"key":"value"}`,
	}

	err := doInit(cfg)
	if err != nil {
		t.Fatalf("doInit() error: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()

	if receivedMethod != "POST" {
		t.Errorf("expected method POST, got %s", receivedMethod)
	}
	if receivedBody != `{"key":"value"}` {
		t.Errorf("expected body %q, got %q", `{"key":"value"}`, receivedBody)
	}
	if receivedHeaders.Get("Content-Type") != "application/json" {
		t.Errorf("expected Content-Type header, got %q", receivedHeaders.Get("Content-Type"))
	}
	if receivedHeaders.Get("X-Custom") != "test-value" {
		t.Errorf("expected X-Custom header, got %q", receivedHeaders.Get("X-Custom"))
	}
	if callCount != 1 {
		t.Errorf("expected 1 call, got %d", callCount)
	}
}

// fastInitRetries shrinks the retry waits so tests exercise the loop without
// sleeping for seconds.
func fastInitRetries(t *testing.T, attempts int) {
	t.Helper()
	oldAttempts, oldDelay, oldFactor := initAttempts, initRetryDelay, initRetryFactor
	initAttempts, initRetryDelay, initRetryFactor = attempts, time.Millisecond, 1
	t.Cleanup(func() {
		initAttempts, initRetryDelay, initRetryFactor = oldAttempts, oldDelay, oldFactor
	})
}

func TestDoInitFailure(t *testing.T) {
	fastInitRetries(t, 3)

	var mu sync.Mutex
	var callCount int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		callCount++
		mu.Unlock()
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("internal error"))
	}))
	defer server.Close()

	cfg := config{
		initMethod: "POST",
		initURL:    server.URL + "/fail",
	}

	err := doInit(cfg)
	if err == nil {
		t.Fatal("expected error for 500 response")
	}

	// 5xx is transient (the backend may be mid-restart): all attempts used.
	mu.Lock()
	defer mu.Unlock()
	if callCount != 3 {
		t.Errorf("expected 3 attempts for a 5xx, got %d", callCount)
	}
}

// A 4xx means a bad key or an unknown session -- retrying cannot help, so it
// must fail on the first attempt.
func TestDoInitClientErrorNotRetried(t *testing.T) {
	fastInitRetries(t, 3)

	var mu sync.Mutex
	var callCount int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		callCount++
		mu.Unlock()
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte("Unauthorized"))
	}))
	defer server.Close()

	err := doInit(config{initMethod: "POST", initURL: server.URL + "/fail"})
	if err == nil {
		t.Fatal("expected error for 401 response")
	}

	mu.Lock()
	defer mu.Unlock()
	if callCount != 1 {
		t.Errorf("expected 1 attempt for a 4xx, got %d", callCount)
	}
}

// The real-world case: swe-swe-browser-backend was down when the agent made
// its first browser tool call, and came back a moment later.
func TestDoInitRetriesUntilBackendRecovers(t *testing.T) {
	fastInitRetries(t, 4)

	var mu sync.Mutex
	var callCount int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		callCount++
		n := callCount
		mu.Unlock()
		if n < 3 {
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte("dial tcp 172.17.0.1:9333: connect: connection refused"))
			return
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"started"}`))
	}))
	defer server.Close()

	if err := doInit(config{initMethod: "POST", initURL: server.URL + "/init"}); err != nil {
		t.Fatalf("doInit() should have succeeded on the 3rd attempt: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if callCount != 3 {
		t.Errorf("expected 3 attempts, got %d", callCount)
	}
}

// Regression: a failed init must not latch initDone. Before the fix, one
// failure (backend down) left the session browser-less forever because every
// later tools/call skipped init.
func TestRunRetriesInitOnLaterToolsCall(t *testing.T) {
	fastInitRetries(t, 1) // no in-call retries: isolate the per-tools/call retry

	var mu sync.Mutex
	var initCalls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		initCalls++
		n := initCalls
		mu.Unlock()
		if n == 1 {
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte("browser backend down"))
			return
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"started"}`))
	}))
	defer server.Close()

	cfg := config{
		initMethod: "POST",
		initURL:    server.URL + "/init",
		command:    []string{"cat"},
	}

	messages := []map[string]interface{}{
		{"jsonrpc": "2.0", "method": "tools/call", "id": 1, "params": map[string]interface{}{"name": "browser_navigate"}},
		{"jsonrpc": "2.0", "method": "tools/call", "id": 2, "params": map[string]interface{}{"name": "browser_navigate"}},
		{"jsonrpc": "2.0", "method": "tools/call", "id": 3, "params": map[string]interface{}{"name": "browser_snapshot"}},
	}
	var input bytes.Buffer
	for _, msg := range messages {
		line, _ := json.Marshal(msg)
		input.Write(line)
		input.Write([]byte("\n"))
	}

	var output, stderr bytes.Buffer
	if err := run(cfg, &input, &output, &stderr); err != nil {
		t.Fatalf("run() error: %v", err)
	}

	// Call 1 fails, call 2 retries and succeeds, call 3 skips init.
	mu.Lock()
	defer mu.Unlock()
	if initCalls != 2 {
		t.Errorf("expected 2 init calls (fail then retry-success), got %d", initCalls)
	}
}

func TestRunMessageRouting(t *testing.T) {
	var mu sync.Mutex
	var initCalls int

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		initCalls++
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"started"}`))
	}))
	defer server.Close()

	cfg := config{
		initMethod: "POST",
		initURL:    server.URL + "/init",
		command:    []string{"cat"}, // cat echoes stdin to stdout
	}

	// Build input: initialize, tools/list, tools/call, tools/call
	messages := []map[string]interface{}{
		{"jsonrpc": "2.0", "method": "initialize", "id": 1},
		{"jsonrpc": "2.0", "method": "tools/list", "id": 2},
		{"jsonrpc": "2.0", "method": "tools/call", "id": 3, "params": map[string]interface{}{"name": "browser_navigate"}},
		{"jsonrpc": "2.0", "method": "tools/call", "id": 4, "params": map[string]interface{}{"name": "browser_click"}},
	}

	var input bytes.Buffer
	for _, msg := range messages {
		line, _ := json.Marshal(msg)
		input.Write(line)
		input.Write([]byte("\n"))
	}

	var output bytes.Buffer
	var stderr bytes.Buffer

	err := run(cfg, &input, &output, &stderr)
	if err != nil {
		t.Fatalf("run() error: %v", err)
	}

	// Verify all 4 messages were forwarded to cat and echoed back
	outputLines := strings.Split(strings.TrimSpace(output.String()), "\n")
	if len(outputLines) != 4 {
		t.Errorf("expected 4 output lines, got %d: %v", len(outputLines), outputLines)
	}

	// Verify init was called exactly once (on first tools/call)
	mu.Lock()
	if initCalls != 1 {
		t.Errorf("expected 1 init call, got %d", initCalls)
	}
	mu.Unlock()

	// Verify each output matches the input
	for i, msg := range messages {
		if i >= len(outputLines) {
			break
		}
		expected, _ := json.Marshal(msg)
		if outputLines[i] != string(expected) {
			t.Errorf("output line %d mismatch:\ngot:  %s\nwant: %s", i, outputLines[i], string(expected))
		}
	}
}

func TestRunNoToolsCall(t *testing.T) {
	var mu sync.Mutex
	var initCalls int

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		initCalls++
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	cfg := config{
		initMethod: "POST",
		initURL:    server.URL + "/init",
		command:    []string{"cat"},
	}

	// Only initialize and tools/list -- no tools/call
	messages := []map[string]interface{}{
		{"jsonrpc": "2.0", "method": "initialize", "id": 1},
		{"jsonrpc": "2.0", "method": "tools/list", "id": 2},
	}

	var input bytes.Buffer
	for _, msg := range messages {
		line, _ := json.Marshal(msg)
		input.Write(line)
		input.Write([]byte("\n"))
	}

	var output, stderr bytes.Buffer
	err := run(cfg, &input, &output, &stderr)
	if err != nil {
		t.Fatalf("run() error: %v", err)
	}

	// Init should NOT have been called
	mu.Lock()
	if initCalls != 0 {
		t.Errorf("expected 0 init calls, got %d", initCalls)
	}
	mu.Unlock()
}
