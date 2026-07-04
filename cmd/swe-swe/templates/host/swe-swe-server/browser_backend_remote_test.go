package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAllocateRemoteBrowser(t *testing.T) {
	var gotAuth string
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		if r.Method != http.MethodPost || r.URL.Path != "/sessions" {
			t.Errorf("unexpected req %s %s", r.Method, r.URL.Path)
		}
		json.NewEncoder(w).Encode(allocResponse{SessionID: "s1", Host: "box", CDPPort: 6001, VNCPort: 7001})
	}))
	defer backend.Close()

	alloc, err := allocateRemoteBrowser(backend.URL, "tok", "s1")
	if err != nil {
		t.Fatalf("allocate: %v", err)
	}
	if alloc.CDPPort != 6001 || alloc.VNCPort != 7001 || alloc.Host != "box" {
		t.Errorf("alloc = %+v", alloc)
	}
	if gotAuth != "Bearer tok" {
		t.Errorf("auth header = %q, want Bearer tok", gotAuth)
	}
}

func TestAllocateRemoteBrowserError(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "at capacity", http.StatusServiceUnavailable)
	}))
	defer backend.Close()
	if _, err := allocateRemoteBrowser(backend.URL, "", "s1"); err == nil {
		t.Error("expected error on 503")
	}
}

func TestRewriteCDPHosts(t *testing.T) {
	in := `{"webSocketDebuggerUrl":"ws://box:6001/devtools/page/AB","url":"http://box:6001/x","alt":"ws://127.0.0.1:6001/y"}`
	out := string(rewriteCDPHosts([]byte(in), "box:6001", "localhost:6000"))
	if strings.Contains(out, "box:6001") || strings.Contains(out, "127.0.0.1:6001") {
		t.Errorf("remote host not fully rewritten: %s", out)
	}
	if !strings.Contains(out, "ws://localhost:6000/devtools/page/AB") {
		t.Errorf("ws URL not rewritten to local: %s", out)
	}
}

// End-to-end: wire a session to a fake remote CDP server and confirm the local
// CDP proxy forwards /json/version and rewrites the debugger host back to the
// local port (so Playwright follows it through the proxy).
func TestWireRemoteSessionCDPProxy(t *testing.T) {
	// Pick a free local port for the session's CDP proxy.
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	localCDPPort := l.Addr().(*net.TCPAddr).Port
	l.Close()

	// Fake remote chromium CDP endpoint: echoes its own host in the debugger URL.
	var remoteHostPort string
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"webSocketDebuggerUrl":"ws://%s/devtools/browser/X"}`, remoteHostPort)
	}))
	defer remote.Close()
	host, port, _ := net.SplitHostPort(strings.TrimPrefix(remote.URL, "http://"))
	remoteHostPort = host + ":" + port
	remotePort := atoiOrZero(port)

	sess := &Session{UUID: "u1", CDPPort: localCDPPort}
	if err := wireRemoteSession(sess, host, remotePort, 7001, "rid1"); err != nil {
		t.Fatalf("wireRemoteSession: %v", err)
	}
	defer stopRemoteAgentView(sess)

	if sess.RemoteVNCTarget != fmt.Sprintf("%s:7001", host) {
		t.Errorf("RemoteVNCTarget = %q", sess.RemoteVNCTarget)
	}
	if !sess.BrowserStarted {
		t.Error("BrowserStarted should be true after wiring")
	}

	// Fetch through the local proxy; the debugger host must be rewritten local.
	resp, err := http.Get(fmt.Sprintf("http://localhost:%d/json/version", localCDPPort))
	if err != nil {
		t.Fatalf("get through proxy: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	got := string(body)
	if !strings.Contains(got, fmt.Sprintf("ws://localhost:%d/devtools/browser/X", localCDPPort)) {
		t.Errorf("debugger URL not rewritten to local proxy: %s", got)
	}
}

func atoiOrZero(s string) int {
	n := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0
		}
		n = n*10 + int(c-'0')
	}
	return n
}
