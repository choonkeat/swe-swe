package main

// Tests for the client (swe-swe box) side of the Agent View reverse tunnel:
// port sources (/proc/net/tcp mirror parser, Procfile math, exclude list),
// tunnel-mode allocation payload, and a one-machine end-to-end proving
// accept -> WS stream -> dial-back plus mirror-driven sync updates and
// reconnect.

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

// Fixture lines mirror real /proc/net/tcp formatting (header + entries).
// 0100007F:0BB8 = 127.0.0.1:3000, 00000000:1F90 = 0.0.0.0:8080,
// 0508A8C0:270F = 192.168.8.5:9999. st 0A = LISTEN, 01 = ESTABLISHED.
const procNetTCPFixture = `  sl  local_address rem_address   st tx_queue rx_queue tr tm->when retrnsmt   uid  timeout inode
   0: 0100007F:0BB8 00000000:0000 0A 00000000:00000000 00:00000000 00000000  1000        0 12345 1 0000000000000000 100 0 0 10 0
   1: 00000000:1F90 00000000:0000 0A 00000000:00000000 00:00000000 00000000  1000        0 12346 1 0000000000000000 100 0 0 10 0
   2: 0100007F:0D05 0100007F:1F90 01 00000000:00000000 00:00000000 00000000  1000        0 12347 1 0000000000000000 100 0 0 10 0
   3: 0508A8C0:270F 00000000:0000 0A 00000000:00000000 00:00000000 00000000  1000        0 12348 1 0000000000000000 100 0 0 10 0
`

// ::1:4321 listening, :: (wildcard) :5173 listening, established ignored.
const procNetTCP6Fixture = `  sl  local_address                         remote_address                        st tx_queue rx_queue tr tm->when retrnsmt   uid  timeout inode
   0: 00000000000000000000000001000000:10E1 00000000000000000000000000000000:0000 0A 00000000:00000000 00:00000000 00000000  1000        0 22345 1 0000000000000000 100 0 0 10 0
   1: 00000000000000000000000000000000:1435 00000000000000000000000000000000:0000 0A 00000000:00000000 00:00000000 00000000  1000        0 22346 1 0000000000000000 100 0 0 10 0
   2: 00000000000000000000000001000000:0D06 00000000000000000000000001000000:1435 01 00000000:00000000 00:00000000 00000000  1000        0 22347 1 0000000000000000 100 0 0 10 0
`

func TestParseProcNetTCPListeners(t *testing.T) {
	got := parseProcNetTCPListeners(procNetTCPFixture)
	// 3000 (127.0.0.1 LISTEN) and 8080 (wildcard LISTEN); NOT 3333 (established),
	// NOT 9999 (specific non-loopback IP -- not reachable via our dial-back).
	if !reflect.DeepEqual(sortedInts(got), []int{3000, 8080}) {
		t.Errorf("v4 listeners = %v, want [3000 8080]", got)
	}

	got6 := parseProcNetTCPListeners(procNetTCP6Fixture)
	// 4321 (::1 LISTEN) and 5173 (:: wildcard LISTEN).
	if !reflect.DeepEqual(sortedInts(got6), []int{4321, 5173}) {
		t.Errorf("v6 listeners = %v, want [4321 5173]", got6)
	}

	if got := parseProcNetTCPListeners("garbage\nlines\n"); len(got) != 0 {
		t.Errorf("garbage parsed to %v, want none", got)
	}
}

func TestParseTunnelExcludePorts(t *testing.T) {
	ranges := parseTunnelExcludePorts("6080, 7000-7019, 9000 -9019,invalid,42")
	want := []tunnelPortRange{{6080, 6080}, {7000, 7019}, {9000, 9019}, {42, 42}}
	if !reflect.DeepEqual(ranges, want) {
		t.Errorf("ranges = %+v, want %+v", ranges, want)
	}
	for _, tc := range []struct {
		port int
		want bool
	}{{6080, true}, {6081, false}, {7000, true}, {7019, true}, {7020, false}, {42, true}, {3000, false}} {
		if got := tunnelPortExcluded(ranges, tc.port); got != tc.want {
			t.Errorf("excluded(%d) = %v, want %v", tc.port, got, tc.want)
		}
	}
	if got := parseTunnelExcludePorts(""); got != nil {
		t.Errorf("empty csv = %+v, want nil", got)
	}
}

func TestDefaultTunnelExcludePortsCoverInternalPools(t *testing.T) {
	ranges := defaultTunnelExcludePorts()
	// Internal plumbing must be excluded from the mirror...
	for _, p := range []int{agentChatPortStart, publicPortEnd, cdpPortStart,
		cdpPortEnd + (cdpPortEnd - cdpPortStart + 1), vncPortStart, filesPortEnd,
		proxyPortOffset + previewPortStart, proxyPortOffset + filesPortEnd} {
		if !tunnelPortExcluded(ranges, p) {
			t.Errorf("internal port %d not excluded by default", p)
		}
	}
	// ...but app-facing preview ports and common dev ports must NOT be.
	for _, p := range []int{previewPortStart, previewPortEnd, 8080, 5173, 8000} {
		if tunnelPortExcluded(ranges, p) {
			t.Errorf("app port %d wrongly excluded by default", p)
		}
	}
}

func TestProcfileServicePorts(t *testing.T) {
	dir := t.TempDir()
	procfile := `# comment
web: npm run dev
api: ./api serve
worker: ./worker
`
	if err := os.WriteFile(filepath.Join(dir, "Procfile"), []byte(procfile), 0644); err != nil {
		t.Fatal(err)
	}
	// Same math as swe-run assignPorts: primary (web) = base, i-th other =
	// base + 5000 + i*20.
	got := procfileServicePorts(dir, 3000)
	if !reflect.DeepEqual(sortedInts(got), []int{3000, 8000, 8020}) {
		t.Errorf("ports = %v, want [3000 8000 8020]", got)
	}

	// No web service: first line is primary.
	if err := os.WriteFile(filepath.Join(dir, "Procfile"), []byte("api: ./api\ndb: ./db\n"), 0644); err != nil {
		t.Fatal(err)
	}
	got = procfileServicePorts(dir, 3001)
	if !reflect.DeepEqual(sortedInts(got), []int{3001, 8001}) {
		t.Errorf("no-web ports = %v, want [3001 8001]", got)
	}

	// Missing Procfile -> nil.
	if got := procfileServicePorts(filepath.Join(dir, "nope"), 3000); got != nil {
		t.Errorf("missing Procfile -> %v, want nil", got)
	}
}

func TestTunnelAllocPayload(t *testing.T) {
	t.Setenv("SWE_AGENT_VIEW_LOCALHOST", "203.0.113.9")
	t.Setenv("SWE_AGENT_VIEW_LOOPBACK_DOMAINS", "myapp.test")

	direct := buildAllocPayload("sess-1", false)
	if direct["resolveLocalhostTo"] != "203.0.113.9" {
		t.Errorf("direct payload missing resolveLocalhostTo: %+v", direct)
	}
	if _, ok := direct["tunnel"]; ok {
		t.Errorf("direct payload has tunnel flag: %+v", direct)
	}

	// Tunnel mode: tunnel:true, resolver overrides ignored (logged, not sent).
	tun := buildAllocPayload("sess-1", true)
	if tun["tunnel"] != true {
		t.Errorf("tunnel payload missing tunnel:true: %+v", tun)
	}
	if _, ok := tun["resolveLocalhostTo"]; ok {
		t.Errorf("tunnel payload should ignore SWE_AGENT_VIEW_LOCALHOST: %+v", tun)
	}
	if _, ok := tun["loopbackDomains"]; ok {
		t.Errorf("tunnel payload should ignore SWE_AGENT_VIEW_LOOPBACK_DOMAINS: %+v", tun)
	}
}

func TestTunnelClientEndToEnd(t *testing.T) {
	bb, srv := tunnelTestServer(t, "sekret")
	createBackendSession(t, srv, "sekret", "s1", true)

	// The app the tunnel should reach lives on 127.0.0.2 so the backend can
	// bind the SAME port number on 127.0.0.1 of this one machine (in
	// production these are two different hosts' loopbacks).
	appLn, err := net.Listen("tcp", "127.0.0.2:0")
	if err != nil {
		t.Skipf("cannot bind 127.0.0.2 (loopback /8 unavailable): %v", err)
	}
	port := appLn.Addr().(*net.TCPAddr).Port
	app := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "app-ok %s", r.URL.Path)
	})}
	go app.Serve(appLn)
	t.Cleanup(func() { app.Close() })

	oldDial := tunnelDialAddr
	oldInterval := tunnelMirrorInterval
	tunnelDialAddr = "127.0.0.2"
	tunnelMirrorInterval = 100 * time.Millisecond
	t.Cleanup(func() {
		tunnelDialAddr = oldDial
		tunnelMirrorInterval = oldInterval
	})

	// --- Part 1: MIRROR-driven discovery. No static ports at all: the
	// /proc/net/tcp mirror must find the app listening on loopback and sync
	// it; the backend then binds 127.0.0.1:<port> and serves it end-to-end.
	client := startAgentViewTunnelClient(srv.URL, "sekret", "s1", nil, nil)
	t.Cleanup(client.Stop)

	get := func() (string, error) {
		c := http.Client{Timeout: 2 * time.Second}
		resp, err := c.Get(fmt.Sprintf("http://127.0.0.1:%d/hello", port))
		if err != nil {
			return "", err
		}
		defer resp.Body.Close()
		b, err := io.ReadAll(resp.Body)
		return string(b), err
	}
	var body string
	deadline := time.Now().Add(5 * time.Second)
	for {
		if body, err = get(); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("backend port never served through tunnel (mirror discovery): %v", err)
		}
		time.Sleep(50 * time.Millisecond)
	}
	if body != "app-ok /hello" {
		t.Fatalf("tunneled response = %q, want app-ok /hello", body)
	}
	found := false
	for _, p := range client.currentSyncPorts() {
		if p == port {
			found = true
		}
	}
	if !found {
		t.Errorf("mirror never put port %d into the sync set: %v", port, client.currentSyncPorts())
	}

	// Force-close the tunnel server-side: the client must reconnect,
	// re-sync, and the port must serve again.
	bb.mu.Lock()
	stop := bb.sessions["s1"].tunnelStop
	bb.mu.Unlock()
	if stop == nil {
		t.Fatal("no tunnelStop registered on live session")
	}
	stop()
	deadline = time.Now().Add(10 * time.Second)
	for {
		if body, err = get(); err == nil && body == "app-ok /hello" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("tunnel never recovered after server-side close: body=%q err=%v", body, err)
		}
		time.Sleep(100 * time.Millisecond)
	}
	client.Stop()
	deadline = time.Now().Add(5 * time.Second)
	for dialOK(port) {
		if time.Now().After(deadline) {
			t.Fatal("backend listener still up after client stop")
		}
		time.Sleep(50 * time.Millisecond)
	}

	// --- Part 2: declarative REMOVAL. One-machine caveat: with the backend
	// bound on 127.0.0.1:<port>, the mirror would re-discover the backend's
	// OWN listener (impossible in production where the two loopbacks are
	// different machines), so this client excludes the port from the mirror
	// and drives it via static -- clearing static must drop it from the next
	// sync and close the backend listener.
	client2 := startAgentViewTunnelClient(srv.URL, "sekret", "s1",
		[]int{port}, []tunnelPortRange{{port, port}})
	t.Cleanup(client2.Stop)
	deadline = time.Now().Add(10 * time.Second)
	for {
		if body, err = get(); err == nil && body == "app-ok /hello" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("static-driven bind never served: body=%q err=%v", body, err)
		}
		time.Sleep(100 * time.Millisecond)
	}

	client2.setStaticPortsForTest(nil)
	deadline = time.Now().Add(10 * time.Second)
	for dialOK(port) {
		if time.Now().After(deadline) {
			t.Fatal("backend listener still up after static port removed from sync")
		}
		time.Sleep(100 * time.Millisecond)
	}
	for _, p := range client2.currentSyncPorts() {
		if p == port {
			t.Errorf("port %d still in sync set after removal", port)
		}
	}
}

func TestTunnelClientRefusedWarnOnce(t *testing.T) {
	c := &agentViewTunnelClient{warned: map[int]string{}}
	if !c.shouldWarnRefusal(6080, "reserved") {
		t.Error("first refusal should warn")
	}
	if c.shouldWarnRefusal(6080, "reserved") {
		t.Error("repeat refusal should not warn again")
	}
	if !c.shouldWarnRefusal(6080, "in-use") {
		t.Error("changed reason should warn again")
	}
	if !c.shouldWarnRefusal(7000, "reserved") {
		t.Error("different port should warn")
	}
}
