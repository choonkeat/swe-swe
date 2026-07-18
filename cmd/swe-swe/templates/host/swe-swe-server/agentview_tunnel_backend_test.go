package main

// Tests for the backend side of the Agent View reverse tunnel: the
// declarative bind manager, the /sessions/{id}/tunnel WS endpoint, and the
// tunnel-mode allocation flag.

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func sortedInts(in []int) []int {
	out := append([]int(nil), in...)
	sort.Ints(out)
	return out
}

// freeLoopbackPort grabs an OS-assigned port and releases it for reuse.
func freeLoopbackPort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close()
	return port
}

func dialOK(port int) bool {
	c, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), 500*time.Millisecond)
	if err != nil {
		return false
	}
	c.Close()
	return true
}

func TestTunnelBindReconcile(t *testing.T) {
	reservedPort := freeLoopbackPort(t)
	pA := freeLoopbackPort(t)
	pB := freeLoopbackPort(t)

	reserved := func(p int) string {
		if p == reservedPort {
			return "reserved"
		}
		return ""
	}
	bm := newTunnelBindManager("sess-1", reserved, nil)
	t.Cleanup(bm.closeAll)

	// Bind new + refuse reserved.
	bound, refused := bm.reconcile([]int{pA, pB, reservedPort})
	if !reflect.DeepEqual(sortedInts(bound), sortedInts([]int{pA, pB})) {
		t.Errorf("bound = %v, want %v", bound, []int{pA, pB})
	}
	if len(refused) != 1 || refused[0].Port != reservedPort || refused[0].Reason != "reserved" {
		t.Errorf("refused = %+v, want reserved %d", refused, reservedPort)
	}
	if !dialOK(pA) || !dialOK(pB) {
		t.Fatalf("bound ports %d/%d not accepting", pA, pB)
	}

	// Reconcile away pB: its listener closes, pA stays.
	bound, refused = bm.reconcile([]int{pA})
	if !reflect.DeepEqual(bound, []int{pA}) || len(refused) != 0 {
		t.Errorf("after removal: bound=%v refused=%+v", bound, refused)
	}
	if dialOK(pB) {
		t.Errorf("port %d still accepting after reconcile removed it", pB)
	}
	if !dialOK(pA) {
		t.Errorf("port %d stopped accepting though still desired", pA)
	}

	// Cross-session: first bind wins, second session is refused loudly.
	bm2 := newTunnelBindManager("sess-2", reserved, nil)
	t.Cleanup(bm2.closeAll)
	bound2, refused2 := bm2.reconcile([]int{pA})
	if len(bound2) != 0 || len(refused2) != 1 || refused2[0].Port != pA || refused2[0].Reason != "in-use" {
		t.Errorf("cross-session dup: bound=%v refused=%+v, want in-use %d", bound2, refused2, pA)
	}

	// Teardown releases the claim; the other session can now bind it.
	bm.closeAll()
	if dialOK(pA) {
		t.Errorf("port %d still accepting after closeAll", pA)
	}
	bound2, refused2 = bm2.reconcile([]int{pA})
	if !reflect.DeepEqual(bound2, []int{pA}) || len(refused2) != 0 {
		t.Errorf("rebind after release: bound=%v refused=%+v", bound2, refused2)
	}
}

func TestTunnelAllocTunnelTrueSkipsResolverRules(t *testing.T) {
	calls := withStubStarter(t)
	bb := newBrowserBackend(2, "", "browser-box")

	rr := httptest.NewRecorder()
	bb.ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "/sessions",
		strings.NewReader(`{"sessionId":"s1","tunnel":true,"resolveLocalhostTo":"203.0.113.7"}`)))
	if rr.Code != http.StatusOK {
		t.Fatalf("create: got %d (body %q)", rr.Code, rr.Body.String())
	}
	var resp allocResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if !resp.Tunnel {
		t.Error("allocResponse.Tunnel = false, want true")
	}
	if got := calls.hostResolverRules[0]; got != "" {
		t.Errorf("tunnel-mode chromium got host-resolver-rules %q, want none", got)
	}

	// Direct mode unchanged: rules still built.
	rr2 := httptest.NewRecorder()
	bb.ServeHTTP(rr2, httptest.NewRequest(http.MethodPost, "/sessions",
		strings.NewReader(`{"sessionId":"s2","resolveLocalhostTo":"203.0.113.7"}`)))
	var resp2 allocResponse
	json.Unmarshal(rr2.Body.Bytes(), &resp2)
	if resp2.Tunnel {
		t.Error("direct-mode allocResponse.Tunnel = true, want false")
	}
	if got := calls.hostResolverRules[1]; !strings.Contains(got, "MAP localhost 203.0.113.7") {
		t.Errorf("direct-mode resolver rules = %q, want MAP entries", got)
	}
}

// tunnelTestServer boots a browserBackend behind httptest with a permissive
// peer guard (unit tests connect from the test process, not chromium).
func tunnelTestServer(t *testing.T, token string) (*browserBackend, *httptest.Server) {
	t.Helper()
	withStubStarter(t)
	bb := newBrowserBackend(2, token, "")
	bb.tunnelGuard = func(sess *backendSession, c net.Conn) error { return nil }
	srv := httptest.NewServer(bb)
	t.Cleanup(srv.Close)
	return bb, srv
}

func wsURL(srv *httptest.Server, path string) string {
	return "ws" + strings.TrimPrefix(srv.URL, "http") + path
}

func dialTunnel(t *testing.T, srv *httptest.Server, id, token string) (*websocket.Conn, *http.Response, error) {
	t.Helper()
	hdr := http.Header{}
	if token != "" {
		hdr.Set("Authorization", "Bearer "+token)
	}
	return websocket.DefaultDialer.Dial(wsURL(srv, "/sessions/"+id+"/tunnel"), hdr)
}

func createBackendSession(t *testing.T, srv *httptest.Server, token, id string, tunnel bool) {
	t.Helper()
	body := fmt.Sprintf(`{"sessionId":%q,"tunnel":%v}`, id, tunnel)
	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/sessions", strings.NewReader(body))
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("create %s: %d %s", id, resp.StatusCode, b)
	}
}

func TestTunnelEndpointAuth404And409(t *testing.T) {
	_, srv := tunnelTestServer(t, "sekret")
	createBackendSession(t, srv, "sekret", "s1", true)

	// No token -> 401 (upgrade refused).
	if _, resp, err := dialTunnel(t, srv, "s1", ""); err == nil {
		t.Error("tunnel dial without token succeeded, want 401")
	} else if resp == nil || resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("tunnel dial without token: resp=%+v, want 401", resp)
	}

	// Unknown session -> 404.
	if _, resp, err := dialTunnel(t, srv, "nope", "sekret"); err == nil {
		t.Error("tunnel dial for unknown session succeeded, want 404")
	} else if resp == nil || resp.StatusCode != http.StatusNotFound {
		t.Errorf("tunnel dial unknown session: resp=%+v, want 404", resp)
	}

	// First tunnel connects; a second concurrent one is refused with 409.
	ws1, _, err := dialTunnel(t, srv, "s1", "sekret")
	if err != nil {
		t.Fatalf("first tunnel dial: %v", err)
	}
	defer ws1.Close()
	if _, resp, err := dialTunnel(t, srv, "s1", "sekret"); err == nil {
		t.Error("second concurrent tunnel succeeded, want 409")
	} else if resp == nil || resp.StatusCode != http.StatusConflict {
		t.Errorf("second concurrent tunnel: resp=%+v, want 409", resp)
	}

	// After the first closes, a new tunnel may connect (reconnect path).
	ws1.Close()
	deadline := time.Now().Add(2 * time.Second)
	for {
		ws2, _, err := dialTunnel(t, srv, "s1", "sekret")
		if err == nil {
			ws2.Close()
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("reconnect after close never accepted: %v", err)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestTunnelEndpointSyncBindStreamAndTeardown(t *testing.T) {
	bb, srv := tunnelTestServer(t, "sekret")
	_ = bb
	createBackendSession(t, srv, "sekret", "s1", true)

	ws, _, err := dialTunnel(t, srv, "s1", "sekret")
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()

	// Drive the client side with the shared mux over the real WS conn.
	type opened struct {
		s    *tunnelStream
		port int
	}
	openedCh := make(chan opened, 1)
	ctlCh := make(chan tunnelControl, 4)
	mux := newTunnelMux(ws, func(s *tunnelStream, port int) {
		openedCh <- opened{s, port}
	}, func(c tunnelControl) { ctlCh <- c })
	go mux.run()
	defer mux.close()

	port := freeLoopbackPort(t)
	if err := mux.sendSync([]int{port}); err != nil {
		t.Fatal(err)
	}
	select {
	case c := <-ctlCh:
		if c.Op != "sync-result" || !reflect.DeepEqual(c.Bound, []int{port}) || len(c.Refused) != 0 {
			t.Fatalf("sync-result = %+v, want bound [%d]", c, port)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no sync-result")
	}

	// A TCP connection to the backend's bound loopback port must produce an
	// open frame and a working byte stream back to us.
	tcp, err := net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		t.Fatal(err)
	}
	defer tcp.Close()
	var op opened
	select {
	case op = <-openedCh:
	case <-time.After(2 * time.Second):
		t.Fatal("no open frame after TCP connect")
	}
	if op.port != port {
		t.Errorf("open frame port = %d, want %d", op.port, port)
	}

	// TCP -> stream direction.
	if _, err := tcp.Write([]byte("ping")); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 4)
	if _, err := io.ReadFull(op.s, buf); err != nil {
		t.Fatalf("stream read: %v", err)
	}
	if string(buf) != "ping" {
		t.Errorf("stream got %q", buf)
	}
	// Stream -> TCP direction.
	if _, err := op.s.Write([]byte("pong")); err != nil {
		t.Fatal(err)
	}
	if _, err := io.ReadFull(tcp, buf); err != nil {
		t.Fatalf("tcp read: %v", err)
	}
	if string(buf) != "pong" {
		t.Errorf("tcp got %q", buf)
	}

	// DELETE the session: tunnel WS dies and the loopback port stops accepting.
	req, _ := http.NewRequest(http.MethodDelete, srv.URL+"/sessions/s1", nil)
	req.Header.Set("Authorization", "Bearer sekret")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("delete: %d", resp.StatusCode)
	}
	deadline := time.Now().Add(2 * time.Second)
	for dialOK(port) {
		if time.Now().After(deadline) {
			t.Fatal("tunnel-bound port still accepting after session delete")
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestBackendReservedPortReason(t *testing.T) {
	bb := newBrowserBackend(2, "", "")
	bb.servicePort = 9333
	for _, p := range []int{9333, cdpPortStart, cdpPortEnd, vncPortStart, vncPortEnd,
		cdpPortEnd + (cdpPortEnd - cdpPortStart + 1), vncPortEnd + (vncPortEnd - vncPortStart + 1)} {
		if got := bb.reservedPortReason(p); got != "reserved" {
			t.Errorf("reservedPortReason(%d) = %q, want reserved", p, got)
		}
	}
	if got := bb.reservedPortReason(3000); got != "" {
		t.Errorf("reservedPortReason(3000) = %q, want allowed", got)
	}
}
