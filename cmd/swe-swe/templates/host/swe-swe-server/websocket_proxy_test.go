package main

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/gorilla/websocket"
)

// TestWebSocketProxyRelay tests that the proxy correctly relays WebSocket
// upgrade requests to the backend and bidirectional messages work.
func TestWebSocketProxyRelay(t *testing.T) {
	// 1. Start a backend server that upgrades WebSocket connections and echoes with a prefix
	// The prefix proves the message actually round-tripped through the backend
	const backendPrefix = "echo:"
	upgrader := websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool { return true },
	}
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Logf("backend upgrade error: %v", err)
			return
		}
		defer conn.Close()
		for {
			mt, msg, err := conn.ReadMessage()
			if err != nil {
				return
			}
			if err := conn.WriteMessage(mt, append([]byte(backendPrefix), msg...)); err != nil {
				return
			}
		}
	}))
	defer backend.Close()

	// 2. Start the proxy server pointing at the backend
	backendURL, _ := url.Parse(backend.URL)
	state := &previewProxyState{
		defaultTarget:  backendURL,

	}
	proxyMux := http.NewServeMux()
	proxyMux.HandleFunc("/", handleProxyRequest(state))
	proxy := httptest.NewServer(proxyMux)
	defer proxy.Close()

	// 3. Connect to the proxy with a WebSocket client
	wsURL := "ws" + strings.TrimPrefix(proxy.URL, "http") + "/"
	dialer := websocket.Dialer{}
	conn, resp, err := dialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("WebSocket dial through proxy failed: %v (resp=%v)", err, resp)
	}
	defer conn.Close()

	// 4. Send a message and expect the backend's prefixed echo back
	testMsg := "hello websocket"
	if err := conn.WriteMessage(websocket.TextMessage, []byte(testMsg)); err != nil {
		t.Fatalf("Failed to write message: %v", err)
	}

	_, msg, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("Failed to read echo message: %v", err)
	}
	expected := backendPrefix + testMsg
	if string(msg) != expected {
		t.Errorf("Expected %q, got %q", expected, string(msg))
	}
}

// TestNormalHTTPThroughProxy verifies that normal HTTP requests still work
// through the proxy (regression test).
func TestNormalHTTPThroughProxy(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"status":"ok"}`))
	}))
	defer backend.Close()

	backendURL, _ := url.Parse(backend.URL)
	state := &previewProxyState{
		defaultTarget:  backendURL,

	}
	proxyMux := http.NewServeMux()
	proxyMux.HandleFunc("/", handleProxyRequest(state))
	proxy := httptest.NewServer(proxyMux)
	defer proxy.Close()

	resp, err := http.Get(proxy.URL + "/")
	if err != nil {
		t.Fatalf("HTTP GET through proxy failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Errorf("Expected 200, got %d", resp.StatusCode)
	}
}

// TestWebSocketProxyBackendDown verifies that the proxy returns an HTTP error
// when the backend is not listening (instead of hanging or panicking).
func TestWebSocketProxyBackendDown(t *testing.T) {
	// Point proxy at a port that nothing is listening on
	backendURL, _ := url.Parse("http://127.0.0.1:1") // port 1 should be closed
	state := &previewProxyState{
		defaultTarget:  backendURL,
	}
	proxyMux := http.NewServeMux()
	proxyMux.HandleFunc("/", handleProxyRequest(state))
	proxy := httptest.NewServer(proxyMux)
	defer proxy.Close()

	// Attempt WebSocket dial — should get an error, not hang
	wsURL := "ws" + strings.TrimPrefix(proxy.URL, "http") + "/"
	dialer := websocket.Dialer{}
	_, resp, err := dialer.Dial(wsURL, nil)
	if err == nil {
		t.Fatal("Expected WebSocket dial to fail when backend is down")
	}
	if resp != nil && resp.StatusCode != http.StatusBadGateway {
		t.Errorf("Expected 502 Bad Gateway, got %d", resp.StatusCode)
	}
}
