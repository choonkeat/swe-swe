package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

// fakeChromium serves the one CDP discovery endpoint the proxy rewrites,
// tagging its reply so a test can tell which browser answered.
func fakeChromium(t *testing.T, tag string) (host string, port int, close func()) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"tag":%q,"webSocketDebuggerUrl":"ws://%s/devtools/browser/X"}`, tag, r.Host)
	}))
	h, p, _ := net.SplitHostPort(strings.TrimPrefix(srv.URL, "http://"))
	return h, atoiOrZero(p), srv.Close
}

// stubBackendURL is a stand-in browser backend that only has to answer the
// teardown DELETE. Allocation itself is stubbed at remoteAllocate. Using a real
// URL (not a made-up hostname) keeps teardown from waiting on DNS.
func stubBackendURL(t *testing.T) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}

// withStubAllocate replaces the backend allocation call for the duration of a
// test.
func withStubAllocate(t *testing.T, fn func(sessionID string) (*allocResponse, error)) *atomic.Int32 {
	t.Helper()
	var calls atomic.Int32
	orig := remoteAllocate
	remoteAllocate = func(backendURL, token, sessionID string) (*allocResponse, error) {
		calls.Add(1)
		return fn(sessionID)
	}
	t.Cleanup(func() { remoteAllocate = orig })
	return &calls
}

// Change 2: the agent's browser vanished (idle reaped / backend restarted).
// The next CDP request must transparently re-allocate and be replayed against
// the fresh browser -- the agent sees a pause, not a failure.
func TestCDPProxyRecoversWhenRemoteBrowserGone(t *testing.T) {
	withRemoteAgentViewGlobals(t, stubBackendURL(t), "", false)
	freshHost, freshPort, closeFresh := fakeChromium(t, "fresh")
	defer closeFresh()

	// The original browser's port has nothing listening: exactly what a reaped
	// browser looks like from here.
	deadPort := freeLoopbackPort(t)
	sess := &Session{UUID: "u1", CDPPort: freeLoopbackPort(t)}
	if err := wireRemoteSession(sess, "127.0.0.1", deadPort, 7001, "rid-old"); err != nil {
		t.Fatalf("wireRemoteSession: %v", err)
	}
	defer stopRemoteAgentView(sess)

	calls := withStubAllocate(t, func(sessionID string) (*allocResponse, error) {
		return &allocResponse{SessionID: "rid-new", Host: freshHost, CDPPort: freshPort, VNCPort: 7002}, nil
	})

	resp, err := http.Get(fmt.Sprintf("http://localhost:%d/json/version", sess.CDPPort))
	if err != nil {
		t.Fatalf("get through proxy: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", resp.StatusCode, body)
	}
	if !strings.Contains(string(body), `"tag":"fresh"`) {
		t.Errorf("request not replayed against the fresh browser: %s", body)
	}
	// Still rewritten to the local proxy, so Playwright follows it back here.
	if !strings.Contains(string(body), fmt.Sprintf("ws://localhost:%d/devtools/browser/X", sess.CDPPort)) {
		t.Errorf("debugger URL not rewritten after recovery: %s", body)
	}
	if n := calls.Load(); n != 1 {
		t.Errorf("allocated %d times, want exactly 1", n)
	}
	if sess.RemoteBrowserID != "rid-new" {
		t.Errorf("RemoteBrowserID = %q, want rid-new", sess.RemoteBrowserID)
	}
	if want := fmt.Sprintf("%s:7002", freshHost); sess.RemoteVNCTarget != want {
		t.Errorf("RemoteVNCTarget = %q, want %q -- the VNC pane must follow too", sess.RemoteVNCTarget, want)
	}
}

// Concurrent CDP requests hitting the same dead browser must allocate ONE
// replacement between them, not one each (which would leak browsers).
func TestCDPProxyRecoveryAllocatesOnce(t *testing.T) {
	withRemoteAgentViewGlobals(t, stubBackendURL(t), "", false)
	freshHost, freshPort, closeFresh := fakeChromium(t, "fresh")
	defer closeFresh()

	sess := &Session{UUID: "u1", CDPPort: freeLoopbackPort(t)}
	if err := wireRemoteSession(sess, "127.0.0.1", freeLoopbackPort(t), 7001, "rid-old"); err != nil {
		t.Fatalf("wireRemoteSession: %v", err)
	}
	defer stopRemoteAgentView(sess)

	calls := withStubAllocate(t, func(sessionID string) (*allocResponse, error) {
		return &allocResponse{SessionID: "rid-new", Host: freshHost, CDPPort: freshPort, VNCPort: 7002}, nil
	})

	const n = 8
	done := make(chan string, n)
	for i := 0; i < n; i++ {
		go func() {
			resp, err := http.Get(fmt.Sprintf("http://localhost:%d/json/version", sess.CDPPort))
			if err != nil {
				done <- "err: " + err.Error()
				return
			}
			body, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			done <- string(body)
		}()
	}
	for i := 0; i < n; i++ {
		if got := <-done; !strings.Contains(got, `"tag":"fresh"`) {
			t.Errorf("request %d did not reach the fresh browser: %s", i, got)
		}
	}
	if got := calls.Load(); got != 1 {
		t.Errorf("allocated %d times for one loss, want 1", got)
	}
}

// Change 3: when no browser can be brought back, the agent must get a sentence
// it can act on instead of a bare dial error.
func TestCDPProxyExplainsUnrecoverableFailure(t *testing.T) {
	withRemoteAgentViewGlobals(t, stubBackendURL(t), "", false)
	sess := &Session{UUID: "u1", CDPPort: freeLoopbackPort(t)}
	if err := wireRemoteSession(sess, "127.0.0.1", freeLoopbackPort(t), 7001, "rid-old"); err != nil {
		t.Fatalf("wireRemoteSession: %v", err)
	}
	defer stopRemoteAgentView(sess)

	withStubAllocate(t, func(sessionID string) (*allocResponse, error) {
		return nil, fmt.Errorf("browser backend at capacity")
	})

	resp, err := http.Get(fmt.Sprintf("http://localhost:%d/json/version", sess.CDPPort))
	if err != nil {
		t.Fatalf("get through proxy: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadGateway {
		t.Errorf("status = %d, want 502", resp.StatusCode)
	}
	var payload struct {
		Error     string `json:"error"`
		Message   string `json:"message"`
		Cause     string `json:"cause"`
		Recovered bool   `json:"recovered"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatalf("decode error body: %v", err)
	}
	if payload.Recovered {
		t.Error("recovered=true when nothing was recovered")
	}
	if !strings.Contains(payload.Message, "could not be restarted") {
		t.Errorf("message does not say what happened: %q", payload.Message)
	}
	if !strings.Contains(payload.Cause, "at capacity") {
		t.Errorf("cause lost the underlying reason: %q", payload.Cause)
	}
}

// A replacement that is ALSO unreachable must not loop: one replay, then the
// explanatory failure.
func TestCDPProxyRetriesOnlyOnce(t *testing.T) {
	withRemoteAgentViewGlobals(t, stubBackendURL(t), "", false)
	sess := &Session{UUID: "u1", CDPPort: freeLoopbackPort(t)}
	if err := wireRemoteSession(sess, "127.0.0.1", freeLoopbackPort(t), 7001, "rid-old"); err != nil {
		t.Fatalf("wireRemoteSession: %v", err)
	}
	defer stopRemoteAgentView(sess)

	deadAgain := freeLoopbackPort(t)
	calls := withStubAllocate(t, func(sessionID string) (*allocResponse, error) {
		return &allocResponse{SessionID: "rid-new", Host: "127.0.0.1", CDPPort: deadAgain, VNCPort: 7002}, nil
	})

	resp, err := http.Get(fmt.Sprintf("http://localhost:%d/json/version", sess.CDPPort))
	if err != nil {
		t.Fatalf("get through proxy: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusBadGateway {
		t.Errorf("status = %d, want 502 (body %s)", resp.StatusCode, body)
	}
	if n := calls.Load(); n != 1 {
		t.Errorf("allocated %d times, want 1 -- the retry must not re-allocate again", n)
	}
}
