package main

import (
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestHandleFilesReadyAPI covers the same-origin readiness probe the Files pane
// polls before loading its iframe. Without it, the iframe src was set the moment
// the files proxy port arrived, so a slow md-serve cold start (launched via
// `swe-npx -y @choonkeat/md-serve@latest`) left the pane blank until a manual
// reload. Mirrors handleVNCReadyAPI: 200 when md-serve is listening on the
// session's FilesPort, 503 otherwise.
func TestHandleFilesReadyAPI(t *testing.T) {
	sessionsMu.Lock()
	orig := sessions
	sessions = make(map[string]*Session)
	sessionsMu.Unlock()
	defer func() {
		sessionsMu.Lock()
		sessions = orig
		sessionsMu.Unlock()
	}()

	const uuid = "files-ready-test"
	reg := func(sess *Session) {
		sessionsMu.Lock()
		sessions[uuid] = sess
		sessionsMu.Unlock()
	}

	t.Run("non-GET returns 405", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/session/"+uuid+"/files-ready", nil)
		w := httptest.NewRecorder()
		handleFilesReadyAPI(w, req)
		if w.Code != http.StatusMethodNotAllowed {
			t.Fatalf("got %d, want 405", w.Code)
		}
	})

	t.Run("unknown UUID returns 404", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/session/nope/files-ready", nil)
		w := httptest.NewRecorder()
		handleFilesReadyAPI(w, req)
		if w.Code != http.StatusNotFound {
			t.Fatalf("got %d, want 404", w.Code)
		}
	})

	t.Run("FilesPort unset returns 503", func(t *testing.T) {
		reg(&Session{UUID: uuid, FilesPort: 0})
		req := httptest.NewRequest(http.MethodGet, "/api/session/"+uuid+"/files-ready", nil)
		w := httptest.NewRecorder()
		handleFilesReadyAPI(w, req)
		if w.Code != http.StatusServiceUnavailable {
			t.Fatalf("got %d, want 503", w.Code)
		}
	})

	t.Run("md-serve not listening returns 503 ready:false", func(t *testing.T) {
		// Bind then immediately release to get a (very likely) free port.
		ln, err := net.Listen("tcp", "localhost:0")
		if err != nil {
			t.Fatalf("listen: %v", err)
		}
		port := ln.Addr().(*net.TCPAddr).Port
		ln.Close()
		reg(&Session{UUID: uuid, FilesPort: port})
		req := httptest.NewRequest(http.MethodGet, "/api/session/"+uuid+"/files-ready", nil)
		w := httptest.NewRecorder()
		handleFilesReadyAPI(w, req)
		if w.Code != http.StatusServiceUnavailable {
			t.Fatalf("got %d, want 503", w.Code)
		}
		if !strings.Contains(w.Body.String(), `"ready":false`) {
			t.Fatalf("body = %q, want ready:false", w.Body.String())
		}
	})

	t.Run("md-serve listening returns 200 ready:true", func(t *testing.T) {
		ln, err := net.Listen("tcp", "localhost:0")
		if err != nil {
			t.Fatalf("listen: %v", err)
		}
		defer ln.Close()
		port := ln.Addr().(*net.TCPAddr).Port
		reg(&Session{UUID: uuid, FilesPort: port})
		req := httptest.NewRequest(http.MethodGet, "/api/session/"+uuid+"/files-ready", nil)
		w := httptest.NewRecorder()
		handleFilesReadyAPI(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("got %d, want 200", w.Code)
		}
		if !strings.Contains(w.Body.String(), `"ready":true`) {
			t.Fatalf("body = %q, want ready:true", w.Body.String())
		}
	})
}
