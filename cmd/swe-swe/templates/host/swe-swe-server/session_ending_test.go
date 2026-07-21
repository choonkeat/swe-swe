package main

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"testing"
	"time"
)

// swapSessions replaces the global sessions map for the duration of a test.
func swapSessions(t *testing.T, m map[string]*Session) {
	t.Helper()
	sessionsMu.Lock()
	orig := sessions
	sessions = m
	sessionsMu.Unlock()
	t.Cleanup(func() {
		sessionsMu.Lock()
		sessions = orig
		sessionsMu.Unlock()
	})
}

// swapEndTeardown stubs the background teardown so a test can assert on the
// handler's own behavior without killing processes or shutting down proxies.
func swapEndTeardown(t *testing.T, fn func(string) error) {
	t.Helper()
	orig := endSessionTeardown
	endSessionTeardown = fn
	t.Cleanup(func() { endSessionTeardown = orig })
}

// runningCmd starts a process that stays alive for the test, so the session
// looks live (ProcessState nil) to the listing filters.
func runningCmd(t *testing.T) *exec.Cmd {
	t.Helper()
	cmd := exec.Command("sh", "-c", "sleep 30")
	if err := cmd.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}
	t.Cleanup(func() {
		cmd.Process.Kill()
		cmd.Wait()
	})
	return cmd
}

// markEnding is the one-shot latch that makes teardown idempotent: the first
// caller owns the teardown, every later caller must be told "already in
// flight" so a double-click cannot start two concurrent teardowns of the same
// session.
func TestMarkEndingIsOneShot(t *testing.T) {
	s := &Session{UUID: "s1"}
	if s.isEnding() {
		t.Fatal("a fresh session must not be ending")
	}
	if !s.markEnding() {
		t.Error("first markEnding must win the latch")
	}
	if !s.isEnding() {
		t.Error("isEnding must report true once latched")
	}
	if s.markEnding() {
		t.Error("second markEnding must lose the latch (teardown already in flight)")
	}
}

// Ending a parent cascades to its children: endSessionByUUID tears children
// down too, so they must stop accepting joins at the same instant the parent
// does -- not once their own teardown happens to get around to them.
func TestMarkSessionEndingCascadesToChildren(t *testing.T) {
	parent := &Session{UUID: "parent"}
	child := &Session{UUID: "child", ParentUUID: "parent"}
	other := &Session{UUID: "other"}
	swapSessions(t, map[string]*Session{"parent": parent, "child": child, "other": other})

	if err := markSessionEnding("parent"); err != nil {
		t.Fatalf("markSessionEnding: %v", err)
	}
	if !parent.isEnding() {
		t.Error("parent must be marked ending")
	}
	if !child.isEnding() {
		t.Error("child must be marked ending (it is torn down with the parent)")
	}
	if other.isEnding() {
		t.Error("an unrelated session must not be marked ending")
	}

	if err := markSessionEnding("parent"); !errors.Is(err, errAlreadyEnding) {
		t.Errorf("second markSessionEnding: want errAlreadyEnding, got %v", err)
	}
}

func TestMarkSessionEndingUnknownUUID(t *testing.T) {
	swapSessions(t, map[string]*Session{})
	if err := markSessionEnding("nope"); err == nil || errors.Is(err, errAlreadyEnding) {
		t.Errorf("unknown uuid must be a plain not-found error, got %v", err)
	}
}

// The rejoin hole: the session deliberately stays in the sessions map until
// teardown finishes so its ports stay reserved, and reapable() is still false
// during the SIGTERM grace period. Without an ending check, a reconnect in
// that window attaches to a session whose PTY, proxies and browser are being
// torn down.
//
// ParentUUID is set only to skip getOrCreateSession's top-level memory guard,
// which is irrelevant to what this test asserts.
func TestGetOrCreateSessionRefusesEndingSession(t *testing.T) {
	ending := &Session{UUID: "u1", ParentUUID: "p", ending: true}
	swapSessions(t, map[string]*Session{"u1": ending})

	for _, allowCreate := range []bool{false, true} {
		sess, _, err := getOrCreateSession(SessionParams{UUID: "u1", ParentUUID: "p"}, allowCreate)
		if !errors.Is(err, errSessionGone) {
			t.Errorf("allowCreate=%v: want errSessionGone, got sess=%v err=%v", allowCreate, sess, err)
		}
		if sess != nil {
			t.Errorf("allowCreate=%v: must not hand back a session being torn down", allowCreate)
		}
	}
}

// The whole point of the change: the HTTP request must not block for the
// teardown. Teardown can take 3-5s for a plain session and 35s+ when a remote
// browser backend is unreachable, and it is unbounded if a process will not
// die -- the user should be back on the homepage long before any of that.
func TestHandleSessionEndAPIReturnsImmediately(t *testing.T) {
	sess := &Session{UUID: "slow"}
	swapSessions(t, map[string]*Session{"slow": sess})

	started := make(chan string, 1)
	release := make(chan struct{})
	swapEndTeardown(t, func(uuid string) error {
		started <- uuid
		<-release // simulate a teardown that takes ~forever
		return nil
	})
	defer close(release)

	done := make(chan int, 1)
	go func() {
		w := httptest.NewRecorder()
		handleSessionEndAPI(w, httptest.NewRequest(http.MethodPost, "/api/session/slow/end", nil))
		done <- w.Code
	}()

	select {
	case code := <-done:
		if code != http.StatusAccepted {
			t.Errorf("want 202 Accepted, got %d", code)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("handleSessionEndAPI blocked on teardown -- it must return before teardown completes")
	}

	select {
	case uuid := <-started:
		if uuid != "slow" {
			t.Errorf("teardown ran for %q, want \"slow\"", uuid)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("teardown was never started in the background")
	}

	if !sess.isEnding() {
		t.Error("session must be marked ending before the response is written")
	}
}

// A second end request (double-click, or two browser tabs) must be accepted
// without starting a second concurrent teardown.
func TestHandleSessionEndAPIIsIdempotent(t *testing.T) {
	swapSessions(t, map[string]*Session{"dup": {UUID: "dup"}})

	var calls int32
	teardowns := make(chan struct{}, 4)
	swapEndTeardown(t, func(uuid string) error {
		teardowns <- struct{}{}
		return nil
	})

	for i := 0; i < 3; i++ {
		w := httptest.NewRecorder()
		handleSessionEndAPI(w, httptest.NewRequest(http.MethodPost, "/api/session/dup/end", nil))
		if w.Code != http.StatusAccepted {
			t.Fatalf("request %d: want 202, got %d", i, w.Code)
		}
	}

	time.Sleep(200 * time.Millisecond)
	close(teardowns)
	for range teardowns {
		calls++
	}
	if calls != 1 {
		t.Errorf("teardown ran %d times, want exactly 1", calls)
	}
}

// The MCP tool must not block either: teardown is unbounded if a process will
// not die, and a blocking tool call wedges the CALLING agent, not just a
// browser tab.
func TestEndSessionToolDoesNotBlock(t *testing.T) {
	sess := &Session{UUID: "mcp"}
	swapSessions(t, map[string]*Session{"mcp": sess})

	release := make(chan struct{})
	started := make(chan struct{}, 1)
	swapEndTeardown(t, func(string) error {
		started <- struct{}{}
		<-release
		return nil
	})
	defer close(release)

	done := make(chan string, 1)
	go func() {
		text, err := endSessionTool("mcp")
		if err != nil {
			t.Errorf("endSessionTool: %v", err)
		}
		done <- text
	}()

	select {
	case text := <-done:
		if text == "" {
			t.Error("tool must report what it did")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("end_session blocked on teardown -- it must return once the session is latched")
	}

	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("teardown was never started in the background")
	}
	if !sess.isEnding() {
		t.Error("session must be latched as ending before the tool returns")
	}
}

func TestEndSessionToolUnknownUUID(t *testing.T) {
	swapSessions(t, map[string]*Session{})
	swapEndTeardown(t, func(string) error { return nil })
	if _, err := endSessionTool("nope"); err == nil {
		t.Error("unknown uuid must be an error, not a silent success")
	}
}

// Ending sessions stay listed, flagged, so a caller can tell "tearing down"
// apart from "never existed" -- and knows not to try joining it.
func TestListSessionsReportsEnding(t *testing.T) {
	swapSessions(t, map[string]*Session{
		"live":  {UUID: "live", Cmd: runningCmd(t)},
		"dying": {UUID: "dying", Cmd: runningCmd(t), ending: true},
	})

	got := map[string]bool{}
	for _, s := range listSessionsSnapshot() {
		got[s.UUID] = s.Ending
	}
	if ending, ok := got["live"]; !ok || ending {
		t.Errorf("live: want listed and not ending, got listed=%v ending=%v", ok, ending)
	}
	if ending, ok := got["dying"]; !ok || !ending {
		t.Errorf("dying: want listed with ending=true, got listed=%v ending=%v", ok, ending)
	}
}

// The homepage is server-rendered with no polling, so an ending card can only
// disappear on its own if there is something to poll. This is that endpoint.
func TestLiveSessionsAPI(t *testing.T) {
	swapSessions(t, map[string]*Session{
		"alive":  {UUID: "alive"},
		"dying":  {UUID: "dying", ending: true},
		"child":  {UUID: "child", ParentUUID: "alive"},
		"zombie": {UUID: "zombie", Cmd: finishedCmd(t)},
	})

	w := httptest.NewRecorder()
	handleLiveSessionsAPI(w, httptest.NewRequest(http.MethodGet, "/api/sessions/live", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", w.Code)
	}

	var resp struct {
		Sessions []struct {
			UUID   string `json:"uuid"`
			Ending bool   `json:"ending"`
		} `json:"sessions"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}

	got := map[string]bool{}
	for _, s := range resp.Sessions {
		got[s.UUID] = s.Ending
	}
	if ending, ok := got["alive"]; !ok || ending {
		t.Errorf("alive: want present and not ending, got present=%v ending=%v", ok, ending)
	}
	if ending, ok := got["dying"]; !ok || !ending {
		t.Errorf("dying: want present and ending (so the card can show a terminating state), got present=%v ending=%v", ok, ending)
	}
	if _, ok := got["child"]; ok {
		t.Error("child sessions are not homepage cards -- they must not appear")
	}
	if _, ok := got["zombie"]; ok {
		t.Error("a session whose process already exited must not appear (the homepage hides it too)")
	}
}
