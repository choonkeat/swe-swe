package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// stubStarter replaces the real process spawn so the allocation API can be
// tested without a display stack. It records the ports it was handed.
func withStubStarter(t *testing.T) *starterCalls {
	t.Helper()
	calls := &starterCalls{}
	orig := browserProcsStarter
	browserProcsStarter = func(id string, display, cdpPort, cdpInternalPort, vncPort, vncInternalPort int, resolveLocalhostTo string) (*browserProcs, error) {
		calls.resolveLocalhostTo = append(calls.resolveLocalhostTo, resolveLocalhostTo)
		return &browserProcs{}, nil // no real processes
	}
	t.Cleanup(func() { browserProcsStarter = orig })
	return calls
}

type starterCalls struct {
	resolveLocalhostTo []string
}

func TestBrowserBackendCreateAndDelete(t *testing.T) {
	withStubStarter(t)
	bb := newBrowserBackend(2, "", "browser-box")

	// Create allocates a session with CDP/VNC ports and the advertise host.
	rr := httptest.NewRecorder()
	bb.ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "/sessions",
		strings.NewReader(`{"sessionId":"s1"}`)))
	if rr.Code != http.StatusOK {
		t.Fatalf("create: got %d, want 200 (body %q)", rr.Code, rr.Body.String())
	}
	var resp allocResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.SessionID != "s1" || resp.Host != "browser-box" {
		t.Errorf("alloc = %+v, want id=s1 host=browser-box", resp)
	}
	if resp.CDPPort != cdpPortStart || resp.VNCPort != vncPortStart {
		t.Errorf("alloc ports = cdp %d vnc %d, want %d/%d", resp.CDPPort, resp.VNCPort, cdpPortStart, vncPortStart)
	}

	// Idempotent: same id returns the same allocation.
	rr2 := httptest.NewRecorder()
	bb.ServeHTTP(rr2, httptest.NewRequest(http.MethodPost, "/sessions", strings.NewReader(`{"sessionId":"s1"}`)))
	var resp2 allocResponse
	json.Unmarshal(rr2.Body.Bytes(), &resp2)
	if resp2.CDPPort != resp.CDPPort {
		t.Errorf("idempotent create changed ports: %d -> %d", resp.CDPPort, resp2.CDPPort)
	}

	// Delete frees the slot.
	rrDel := httptest.NewRecorder()
	bb.ServeHTTP(rrDel, httptest.NewRequest(http.MethodDelete, "/sessions/s1", nil))
	if rrDel.Code != http.StatusNoContent {
		t.Errorf("delete: got %d, want 204", rrDel.Code)
	}
	rrDel2 := httptest.NewRecorder()
	bb.ServeHTTP(rrDel2, httptest.NewRequest(http.MethodDelete, "/sessions/s1", nil))
	if rrDel2.Code != http.StatusNotFound {
		t.Errorf("delete unknown: got %d, want 404", rrDel2.Code)
	}
}

func TestBrowserBackendResolveLocalhost(t *testing.T) {
	calls := withStubStarter(t)
	bb := newBrowserBackend(4, "", "h")

	// Default: derived from the allocation request's source address.
	req1 := httptest.NewRequest(http.MethodPost, "/sessions", strings.NewReader(`{"sessionId":"a"}`))
	req1.RemoteAddr = "203.0.113.7:51234"
	bb.ServeHTTP(httptest.NewRecorder(), req1)

	// Explicit body override wins (NAT case).
	req2 := httptest.NewRequest(http.MethodPost, "/sessions", strings.NewReader(`{"sessionId":"b","resolveLocalhostTo":"198.51.100.9"}`))
	req2.RemoteAddr = "203.0.113.7:51235"
	bb.ServeHTTP(httptest.NewRecorder(), req2)

	want := []string{"203.0.113.7", "198.51.100.9"}
	if len(calls.resolveLocalhostTo) != 2 ||
		calls.resolveLocalhostTo[0] != want[0] || calls.resolveLocalhostTo[1] != want[1] {
		t.Errorf("starter resolveLocalhostTo = %v, want %v", calls.resolveLocalhostTo, want)
	}
}

func TestBrowserBackendCapacity(t *testing.T) {
	withStubStarter(t)
	bb := newBrowserBackend(1, "", "h")

	rr := httptest.NewRecorder()
	bb.ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "/sessions", strings.NewReader(`{"sessionId":"a"}`)))
	if rr.Code != http.StatusOK {
		t.Fatalf("first create: %d", rr.Code)
	}
	// Second create exceeds maxSessions=1 -> 503 back-pressure.
	rr2 := httptest.NewRecorder()
	bb.ServeHTTP(rr2, httptest.NewRequest(http.MethodPost, "/sessions", strings.NewReader(`{"sessionId":"b"}`)))
	if rr2.Code != http.StatusServiceUnavailable {
		t.Errorf("over-capacity create: got %d, want 503", rr2.Code)
	}

	// Re-POST for the LIVE id at capacity is idempotent, not 503 -- its own
	// slot is what filled the pool.
	rr3 := httptest.NewRecorder()
	bb.ServeHTTP(rr3, httptest.NewRequest(http.MethodPost, "/sessions", strings.NewReader(`{"sessionId":"a"}`)))
	if rr3.Code != http.StatusOK {
		t.Errorf("idempotent re-create at capacity: got %d, want 200", rr3.Code)
	}
}

func TestBrowserBackendAuth(t *testing.T) {
	withStubStarter(t)
	bb := newBrowserBackend(2, "secret", "h")

	// No token -> 401 on /sessions.
	rr := httptest.NewRecorder()
	bb.ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "/sessions", strings.NewReader(`{}`)))
	if rr.Code != http.StatusUnauthorized {
		t.Errorf("no token: got %d, want 401", rr.Code)
	}
	// Correct bearer token -> allowed.
	req := httptest.NewRequest(http.MethodPost, "/sessions", strings.NewReader(`{"sessionId":"x"}`))
	req.Header.Set("Authorization", "Bearer secret")
	rr2 := httptest.NewRecorder()
	bb.ServeHTTP(rr2, req)
	if rr2.Code != http.StatusOK {
		t.Errorf("with token: got %d, want 200", rr2.Code)
	}
	// /health is open even with auth configured.
	rrH := httptest.NewRecorder()
	bb.ServeHTTP(rrH, httptest.NewRequest(http.MethodGet, "/health", nil))
	if rrH.Code != http.StatusOK {
		t.Errorf("health: got %d, want 200", rrH.Code)
	}
}
