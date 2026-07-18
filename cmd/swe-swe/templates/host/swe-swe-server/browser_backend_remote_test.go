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
	"time"
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

// A remote backend publishes its CDP ports on the same 6000-6019 numbers a
// session's local CDP proxy would otherwise bind. VM managers like Lima
// reflect guest loopback listeners onto the host loopback and route
// guest->host dials through it, so a shared number turns the proxy into a
// dial-to-self loop. Remote mode must therefore allocate session CDP ports
// outside the published range.
func TestFindAvailablePortQuintupleRemoteCDPOffset(t *testing.T) {
	oldBackend := agentViewBackend
	defer func() { agentViewBackend = oldBackend }()

	agentViewBackend = "local"
	_, _, _, cdpLocal, _, err := findAvailablePortQuintuple()
	if err != nil {
		t.Fatalf("local quintuple: %v", err)
	}
	if cdpLocal < cdpPortStart || cdpLocal > cdpPortEnd {
		t.Errorf("local mode CDP port %d outside %d-%d", cdpLocal, cdpPortStart, cdpPortEnd)
	}

	agentViewBackend = "http://backend.example:9333"
	_, _, _, cdpRemote, _, err := findAvailablePortQuintuple()
	if err != nil {
		t.Fatalf("remote quintuple: %v", err)
	}
	if cdpRemote != cdpLocal+remoteCDPProxyOffset {
		t.Errorf("remote mode CDP port = %d, want %d (local %d + offset %d)",
			cdpRemote, cdpLocal+remoteCDPProxyOffset, cdpLocal, remoteCDPProxyOffset)
	}
	if cdpRemote >= cdpPortStart && cdpRemote <= cdpPortEnd+cdpPortEnd-cdpPortStart+1 {
		t.Errorf("remote mode CDP port %d still inside the published/internal range", cdpRemote)
	}

	// The tunnel mirror must never re-export the offset proxy listener.
	excluded := false
	for _, r := range defaultTunnelExcludePorts() {
		if cdpRemote >= r.Lo && cdpRemote <= r.Hi {
			excluded = true
			break
		}
	}
	if !excluded {
		t.Errorf("remote CDP proxy port %d not covered by defaultTunnelExcludePorts", cdpRemote)
	}
}

// withRemoteAgentViewGlobals points the remote Agent View globals at a test
// backend URL and restores them on cleanup.
func withRemoteAgentViewGlobals(t *testing.T, backendURL, token string, tunnel bool) {
	t.Helper()
	oldBackend, oldToken, oldTunnel := agentViewBackend, browserBackendToken, agentViewTunnelMode
	agentViewBackend, browserBackendToken, agentViewTunnelMode = backendURL, token, tunnel
	t.Cleanup(func() {
		agentViewBackend, browserBackendToken, agentViewTunnelMode = oldBackend, oldToken, oldTunnel
	})
}

// The headline re-allocation flow: a backend "container restart" (fresh
// browserBackend behind the same URL, in-memory allocation table gone) must
// not orphan a live session -- the tunnel client's 404 triggers a re-POST of
// /sessions, the CDP proxy retargets the new slot's ports, and a fresh tunnel
// client connects to the new backend, all without session teardown.
func TestRemoteAgentViewReallocateAfterBackendRestart(t *testing.T) {
	withStubStarter(t)

	mkBackend := func() *browserBackend {
		bb := newBrowserBackend(2, "sekret", "")
		bb.tunnelGuard = func(sess *backendSession, c net.Conn) error { return nil }
		return bb
	}
	var cur atomic.Pointer[browserBackend]
	bb1 := mkBackend()
	cur.Store(bb1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cur.Load().ServeHTTP(w, r)
	}))
	defer srv.Close()
	withRemoteAgentViewGlobals(t, srv.URL, "sekret", true)

	sess := &Session{UUID: "u1", CDPPort: freeLoopbackPort(t)}
	status, err := startRemoteAgentView(sess)
	if err != nil || status != "started" {
		t.Fatalf("startRemoteAgentView: status=%q err=%v", status, err)
	}
	t.Cleanup(func() { stopRemoteAgentView(sess) })

	sess.mu.RLock()
	oldClient := sess.AgentViewTunnel
	sess.mu.RUnlock()
	if oldClient == nil {
		t.Fatal("no tunnel client after tunnel-mode allocation")
	}
	if got := *sess.remoteCDPTarget.Load(); got != fmt.Sprintf("127.0.0.1:%d", cdpPortStart) {
		t.Fatalf("initial CDP target = %q, want slot-0 port %d", got, cdpPortStart)
	}

	// Wait for the tunnel to actually connect to bb1.
	waitTunnelActive := func(bb *browserBackend, id string) {
		t.Helper()
		deadline := time.Now().Add(5 * time.Second)
		for {
			bb.mu.Lock()
			s, ok := bb.sessions[id]
			active := ok && s.tunnelActive
			bb.mu.Unlock()
			if active {
				return
			}
			if time.Now().After(deadline) {
				t.Fatalf("tunnel for %s never became active", id)
			}
			time.Sleep(20 * time.Millisecond)
		}
	}
	waitTunnelActive(bb1, "u1")

	// "Restart" the backend: a fresh instance with an empty allocation table
	// takes over the URL. A dummy session occupies slot 0 so the re-allocation
	// must land on slot 1 -- different ports prove the CDP/VNC retarget.
	bb2 := mkBackend()
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/sessions", strings.NewReader(`{"sessionId":"dummy"}`))
	req.Header.Set("Authorization", "Bearer sekret")
	bb2.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("dummy alloc on bb2: %d", rr.Code)
	}
	cur.Store(bb2)
	// Kill the old backend's live tunnel WS, as its process death would.
	bb1.mu.Lock()
	stop := bb1.sessions["u1"].tunnelStop
	bb1.mu.Unlock()
	if stop == nil {
		t.Fatal("no tunnelStop on bb1 session")
	}
	stop()

	// The session must recover: new tunnel client, slot-1 targets, same id.
	deadline := time.Now().Add(15 * time.Second)
	for {
		sess.mu.RLock()
		newClient := sess.AgentViewTunnel
		vnc := sess.RemoteVNCTarget
		sess.mu.RUnlock()
		if newClient != nil && newClient != oldClient {
			if want := fmt.Sprintf("127.0.0.1:%d", vncPortStart+1); vnc != want {
				t.Errorf("RemoteVNCTarget = %q, want %q", vnc, want)
			}
			if got, want := *sess.remoteCDPTarget.Load(), fmt.Sprintf("127.0.0.1:%d", cdpPortStart+1); got != want {
				t.Errorf("CDP target after re-allocation = %q, want %q", got, want)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("session never re-allocated after backend restart")
		}
		time.Sleep(50 * time.Millisecond)
	}
	if !sess.BrowserStarted {
		t.Error("BrowserStarted flipped false across re-allocation")
	}
	waitTunnelActive(bb2, "u1")
}

// A session already closed (or superseded) must abort re-allocation without
// touching the backend.
func TestReallocateAbortsWhenSessionClosed(t *testing.T) {
	var posts atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			posts.Add(1)
		}
		http.Error(w, "should not be reached", http.StatusInternalServerError)
	}))
	defer srv.Close()
	withRemoteAgentViewGlobals(t, srv.URL, "", true)

	lost := newAgentViewTunnelClient(srv.URL, "", "u1", nil, nil)
	sess := &Session{UUID: "u1"}
	sess.closed = true
	sess.AgentViewTunnel = lost
	reallocateRemoteAgentView(sess, lost)
	if n := posts.Load(); n != 0 {
		t.Errorf("closed session still POSTed /sessions %d times", n)
	}

	// Superseded: AgentViewTunnel no longer points at the lost client.
	sess2 := &Session{UUID: "u2"}
	sess2.AgentViewTunnel = nil
	reallocateRemoteAgentView(sess2, lost)
	if n := posts.Load(); n != 0 {
		t.Errorf("superseded client still POSTed /sessions %d times", n)
	}
}

// Teardown racing a successful re-allocation: the fresh (now ownerless)
// allocation must be freed on the backend, and the session left untouched.
func TestReallocateFreesAllocationWhenClosedMidFlight(t *testing.T) {
	posted := make(chan struct{})
	release := make(chan struct{})
	deleted := make(chan string, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPost:
			close(posted)
			<-release
			json.NewEncoder(w).Encode(allocResponse{SessionID: "u1", CDPPort: 6001, VNCPort: 6101, Tunnel: true})
		case http.MethodDelete:
			deleted <- strings.TrimPrefix(r.URL.Path, "/sessions/")
			w.WriteHeader(http.StatusNoContent)
		}
	}))
	defer srv.Close()
	withRemoteAgentViewGlobals(t, srv.URL, "", true)

	lost := newAgentViewTunnelClient(srv.URL, "", "u1", nil, nil)
	sess := &Session{UUID: "u1"}
	sess.AgentViewTunnel = lost

	done := make(chan struct{})
	go func() {
		reallocateRemoteAgentView(sess, lost)
		close(done)
	}()
	<-posted
	// Session teardown wins the race while the allocation POST is in flight.
	sess.mu.Lock()
	sess.closed = true
	sess.mu.Unlock()
	close(release)

	select {
	case id := <-deleted:
		if id != "u1" {
			t.Errorf("freed allocation %q, want u1", id)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("ownerless fresh allocation never freed")
	}
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("reallocate goroutine never returned")
	}
	if sess.RemoteVNCTarget != "" || sess.AgentViewTunnel != lost {
		t.Errorf("closed session was re-wired: vnc=%q tunnel-changed=%v",
			sess.RemoteVNCTarget, sess.AgentViewTunnel != lost)
	}
}

// Stop() on the lost client (session teardown) must cancel a re-allocation
// stuck retrying against a still-down backend.
func TestReallocateCanceledByStopDuringBackoff(t *testing.T) {
	var posts atomic.Int32
	firstPost := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && posts.Add(1) == 1 {
			close(firstPost)
		}
		http.Error(w, "still restarting", http.StatusServiceUnavailable)
	}))
	defer srv.Close()
	withRemoteAgentViewGlobals(t, srv.URL, "", true)

	lost := newAgentViewTunnelClient(srv.URL, "", "u1", nil, nil)
	sess := &Session{UUID: "u1"}
	sess.AgentViewTunnel = lost

	done := make(chan struct{})
	go func() {
		reallocateRemoteAgentView(sess, lost)
		close(done)
	}()
	<-firstPost
	lost.Stop()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("reallocate not canceled by Stop during backoff")
	}
}
