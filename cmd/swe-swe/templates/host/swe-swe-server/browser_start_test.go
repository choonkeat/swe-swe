package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

func TestHandleBrowserStartAPI(t *testing.T) {
	// Save and restore global sessions map
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

	t.Run("GET returns 405", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/session/"+testUUID+"/browser/start", nil)
		w := httptest.NewRecorder()
		handleBrowserStartAPI(w, req)
		if w.Code != http.StatusMethodNotAllowed {
			t.Errorf("expected 405, got %d", w.Code)
		}
	})

	t.Run("unknown UUID returns 404", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/session/unknown-uuid/browser/start", nil)
		w := httptest.NewRecorder()
		handleBrowserStartAPI(w, req)
		if w.Code != http.StatusNotFound {
			t.Errorf("expected 404, got %d", w.Code)
		}
	})

	t.Run("POST returns 500 when browser cannot start", func(t *testing.T) {
		sess := &Session{
			UUID:        testUUID,
			PreviewPort: 3000,
			VNCPort:     7000,
		}
		sessionsMu.Lock()
		sessions[testUUID] = sess
		sessionsMu.Unlock()

		// Clear PATH so Xvfb binary cannot be found
		origPath := os.Getenv("PATH")
		os.Setenv("PATH", "")
		defer os.Setenv("PATH", origPath)

		req := httptest.NewRequest(http.MethodPost, "/api/session/"+testUUID+"/browser/start", nil)
		w := httptest.NewRecorder()
		handleBrowserStartAPI(w, req)
		if w.Code != http.StatusInternalServerError {
			t.Errorf("expected 500, got %d", w.Code)
		}
		if sess.BrowserStarted {
			t.Error("expected BrowserStarted to remain false after failed start")
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

		req := httptest.NewRequest(http.MethodPost, "/api/session/"+testUUID+"/browser/start", nil)
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
