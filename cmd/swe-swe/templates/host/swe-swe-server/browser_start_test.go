package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"testing"
)

func TestHandleBrowserStartAPI(t *testing.T) {
	// Save and restore the global sessions map.
	sessionsMu.Lock()
	origSessions := sessions
	sessions = make(map[string]*Session)
	sessionsMu.Unlock()
	defer func() {
		sessionsMu.Lock()
		sessions = origSessions
		sessionsMu.Unlock()
	}()

	testUUID := "test-session-1234"
	// Issue a deterministic per-session auth key for the test session.
	registerTestSessionKey(t, testUUID, testAPIKey)

	t.Run("GET returns 405", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/session/"+testUUID+"/browser/start?key="+testAPIKey, nil)
		w := httptest.NewRecorder()
		handleBrowserStartAPI(w, req)
		if w.Code != http.StatusMethodNotAllowed {
			t.Errorf("expected 405, got %d", w.Code)
		}
	})

	t.Run("missing API key returns 401", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/session/"+testUUID+"/browser/start", nil)
		w := httptest.NewRecorder()
		handleBrowserStartAPI(w, req)
		if w.Code != http.StatusUnauthorized {
			t.Errorf("expected 401, got %d", w.Code)
		}
	})

	t.Run("wrong API key returns 401", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/session/"+testUUID+"/browser/start?key=wrong-key", nil)
		w := httptest.NewRecorder()
		handleBrowserStartAPI(w, req)
		if w.Code != http.StatusUnauthorized {
			t.Errorf("expected 401, got %d", w.Code)
		}
	})

	t.Run("unknown UUID returns 404", func(t *testing.T) {
		// Authenticate as unknown-uuid (valid key) so the request passes auth
		// and reaches the session lookup, which then misses -> 404.
		registerTestSessionKey(t, "unknown-uuid", "unknown-key")
		req := httptest.NewRequest(http.MethodPost, "/api/session/unknown-uuid/browser/start?key=unknown-key", nil)
		w := httptest.NewRecorder()
		handleBrowserStartAPI(w, req)
		if w.Code != http.StatusNotFound {
			t.Errorf("expected 404, got %d", w.Code)
		}
	})

	t.Run("POST returns 200 unavailable when the display stack is absent", func(t *testing.T) {
		// Graceful degradation (Phase 5): a lean host with no Xvfb/chromium/
		// x11vnc/websockify must report Agent View unavailable so the UI hides
		// the tab -- NOT 500 on a doomed spawn. The other tabs are unaffected.
		sess := &Session{
			UUID:        testUUID,
			PreviewPort: 3000,
			VNCPort:     7000,
		}
		sessionsMu.Lock()
		sessions[testUUID] = sess
		sessionsMu.Unlock()

		// Simulate a host without the display stack installed.
		origLook, origBackend := lookPath, agentViewBackend
		lookPath = func(string) (string, error) { return "", exec.ErrNotFound }
		agentViewBackend = "local"
		defer func() { lookPath, agentViewBackend = origLook, origBackend }()

		req := httptest.NewRequest(http.MethodPost, "/api/session/"+testUUID+"/browser/start?key="+testAPIKey, nil)
		w := httptest.NewRecorder()
		handleBrowserStartAPI(w, req)
		if w.Code != http.StatusOK {
			t.Errorf("expected 200, got %d", w.Code)
		}
		var resp map[string]string
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("failed to parse response: %v", err)
		}
		if resp["status"] != "unavailable" {
			t.Errorf("expected status=unavailable, got %q", resp["status"])
		}
		if sess.BrowserStarted {
			t.Error("expected BrowserStarted to remain false when unavailable")
		}
	})

	t.Run("already started returns 200 with already_started", func(t *testing.T) {
		// Set up session with BrowserStarted = true
		sessionsMu.Lock()
		sessions[testUUID] = &Session{
			UUID:           testUUID,
			BrowserStarted: true,
		}
		sessionsMu.Unlock()

		req := httptest.NewRequest(http.MethodPost, "/api/session/"+testUUID+"/browser/start?key="+testAPIKey, nil)
		w := httptest.NewRecorder()
		handleBrowserStartAPI(w, req)
		if w.Code != http.StatusOK {
			t.Errorf("expected 200, got %d", w.Code)
		}
		var resp map[string]string
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("failed to parse response: %v", err)
		}
		if resp["status"] != "already_started" {
			t.Errorf("expected status=already_started, got %q", resp["status"])
		}
	})
}
