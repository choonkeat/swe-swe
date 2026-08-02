package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// withFakeClock pins the backend's clock so reaper tests never sleep.
func withFakeClock(bb *browserBackend, start time.Time) *fakeClock {
	c := &fakeClock{t: start}
	bb.now = c.Now
	return c
}

type fakeClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *fakeClock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

func mustAlloc(t *testing.T, bb *browserBackend, id string) {
	t.Helper()
	rr := httptest.NewRecorder()
	bb.ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "/sessions",
		strings.NewReader(`{"sessionId":"`+id+`"}`)))
	if rr.Code != http.StatusOK {
		t.Fatalf("alloc %s: got %d (%s)", id, rr.Code, rr.Body.String())
	}
}

func sessionIDs(bb *browserBackend) map[string]bool {
	bb.mu.Lock()
	defer bb.mu.Unlock()
	out := make(map[string]bool, len(bb.sessions))
	for id := range bb.sessions {
		out[id] = true
	}
	return out
}

// An abandoned session (the swe-swe host died without its DELETE) is reclaimed
// once it passes the idle timeout -- that is the whole point of the reaper.
func TestReapIdleFreesAbandonedSession(t *testing.T) {
	withStubStarter(t)
	bb := newBrowserBackend(2, "", "h")
	bb.idleTimeout = 30 * time.Minute
	clock := withFakeClock(bb, time.Now())

	mustAlloc(t, bb, "abandoned")
	clock.advance(29 * time.Minute)
	if got := bb.reapIdle(); len(got) != 0 {
		t.Fatalf("reaped before the timeout: %v", got)
	}
	clock.advance(2 * time.Minute)
	if got := bb.reapIdle(); len(got) != 1 || got[0] != "abandoned" {
		t.Fatalf("reaped = %v, want [abandoned]", got)
	}
	if sessionIDs(bb)["abandoned"] {
		t.Error("reaped session still holds its slot")
	}
	// Slot is genuinely free again.
	mustAlloc(t, bb, "fresh")
}

// The headline guarantee of change 1: a browser the agent is actually driving
// is never reaped, no matter how long the CDP websocket has been open.
func TestReapIdleSparesBrowserInUse(t *testing.T) {
	withStubStarter(t)
	bb := newBrowserBackend(3, "", "h")
	bb.idleTimeout = 30 * time.Minute
	clock := withFakeClock(bb, time.Now())

	mustAlloc(t, bb, "open-websocket")
	mustAlloc(t, bb, "recent-request")
	mustAlloc(t, bb, "idle")

	bb.mu.Lock()
	// One session holds a long-lived CDP websocket: no NEW requests for hours,
	// but plainly in use.
	bb.sessions["open-websocket"].procs.cdpInFlight.Store(1)
	bb.sessions["open-websocket"].procs.cdpLastActive.Store(clock.Now().UnixNano())
	bb.mu.Unlock()

	clock.advance(3 * time.Hour)

	// Another session's agent made a request one minute ago.
	bb.mu.Lock()
	bb.sessions["recent-request"].procs.markCDPActivity()
	bb.sessions["recent-request"].procs.cdpLastActive.Store(clock.Now().Add(-time.Minute).UnixNano())
	bb.mu.Unlock()

	reaped := bb.reapIdle()
	if len(reaped) != 1 || reaped[0] != "idle" {
		t.Fatalf("reaped = %v, want only [idle]", reaped)
	}
	live := sessionIDs(bb)
	if !live["open-websocket"] {
		t.Error("reaped a session with an open CDP websocket")
	}
	if !live["recent-request"] {
		t.Error("reaped a session whose agent used it a minute ago")
	}
}

// A live reverse tunnel is use, too: the session is wired to a running client.
func TestReapIdleSparesActiveTunnel(t *testing.T) {
	withStubStarter(t)
	bb := newBrowserBackend(2, "", "h")
	bb.idleTimeout = time.Minute
	clock := withFakeClock(bb, time.Now())

	mustAlloc(t, bb, "tunnelled")
	bb.mu.Lock()
	bb.sessions["tunnelled"].tunnelActive = true
	bb.mu.Unlock()

	clock.advance(time.Hour)
	if got := bb.reapIdle(); len(got) != 0 {
		t.Fatalf("reaped a session with a live tunnel: %v", got)
	}
}

// POST /sessions/{id}/touch is the keepalive the swe-swe host sends while a
// human watches the VNC pane -- traffic the backend cannot otherwise see.
func TestTouchKeepsSessionAlive(t *testing.T) {
	withStubStarter(t)
	bb := newBrowserBackend(2, "", "h")
	bb.idleTimeout = 30 * time.Minute
	clock := withFakeClock(bb, time.Now())

	mustAlloc(t, bb, "watched")
	for i := 0; i < 4; i++ {
		clock.advance(20 * time.Minute)
		rr := httptest.NewRecorder()
		bb.ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "/sessions/watched/touch", nil))
		if rr.Code != http.StatusNoContent {
			t.Fatalf("touch: got %d, want 204", rr.Code)
		}
		if got := bb.reapIdle(); len(got) != 0 {
			t.Fatalf("reaped a touched session: %v", got)
		}
	}
	// Stop touching and it goes the way of any abandoned session.
	clock.advance(31 * time.Minute)
	if got := bb.reapIdle(); len(got) != 1 || got[0] != "watched" {
		t.Fatalf("reaped = %v, want [watched] once the watcher left", got)
	}

	rr := httptest.NewRecorder()
	bb.ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "/sessions/watched/touch", nil))
	if rr.Code != http.StatusNotFound {
		t.Errorf("touch on a freed session: got %d, want 404", rr.Code)
	}
}

// idleTimeout 0 is the documented off switch.
func TestReapIdleDisabled(t *testing.T) {
	withStubStarter(t)
	bb := newBrowserBackend(2, "", "h")
	bb.idleTimeout = 0
	clock := withFakeClock(bb, time.Now())
	mustAlloc(t, bb, "forever")
	clock.advance(30 * 24 * time.Hour)
	if got := bb.reapIdle(); len(got) != 0 {
		t.Fatalf("reaped with reaping disabled: %v", got)
	}
}

func TestResolveBrowserBackendIdle(t *testing.T) {
	t.Setenv("SWE_BROWSER_BACKEND_IDLE", "")
	if got := resolveBrowserBackendIdle(defaultBrowserBackendIdle, false); got != defaultBrowserBackendIdle {
		t.Errorf("default = %s, want %s", got, defaultBrowserBackendIdle)
	}
	t.Setenv("SWE_BROWSER_BACKEND_IDLE", "5m")
	if got := resolveBrowserBackendIdle(defaultBrowserBackendIdle, false); got != 5*time.Minute {
		t.Errorf("env = %s, want 5m", got)
	}
	// An explicit flag wins over the env.
	if got := resolveBrowserBackendIdle(time.Hour, true); got != time.Hour {
		t.Errorf("flag = %s, want 1h", got)
	}
	// "0" disables.
	t.Setenv("SWE_BROWSER_BACKEND_IDLE", "0")
	if got := resolveBrowserBackendIdle(defaultBrowserBackendIdle, false); got != 0 {
		t.Errorf("env 0 = %s, want 0", got)
	}
	// Garbage falls back to the default rather than silently disabling.
	t.Setenv("SWE_BROWSER_BACKEND_IDLE", "half an hour")
	if got := resolveBrowserBackendIdle(defaultBrowserBackendIdle, false); got != defaultBrowserBackendIdle {
		t.Errorf("bad env = %s, want the default %s", got, defaultBrowserBackendIdle)
	}
}

// The CDP forwarder's activity tracker must count an in-flight request for its
// whole life -- that is what protects a long-lived websocket from the reaper.
func TestCDPActivityTracker(t *testing.T) {
	b := &browserProcs{}
	if _, busy := b.lastCDPActivity(); busy {
		t.Error("fresh browserProcs reports busy")
	}
	entered := make(chan struct{})
	release := make(chan struct{})
	h := b.cdpActivityTracker(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(entered)
		<-release
	}))
	go h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/json/version", nil))
	<-entered
	if _, busy := b.lastCDPActivity(); !busy {
		t.Error("in-flight CDP request not counted as busy")
	}
	close(release)
	deadline := time.Now().Add(2 * time.Second)
	for {
		at, busy := b.lastCDPActivity()
		if !busy {
			if at.IsZero() {
				t.Error("no activity timestamp after a completed request")
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("still busy after the handler returned")
		}
		time.Sleep(5 * time.Millisecond)
	}
}
