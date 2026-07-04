package main

import (
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestHandleVNCReadyAPI covers the readiness probe in both modes: local
// (dial localhost:VNCPort) and remote (dial sess.RemoteVNCTarget -- the same
// target the VNC proxy uses). The remote path regressed to a permanent 503
// before the RemoteVNCTarget branch existed, which stalled Agent View on
// remote backends.
func TestHandleVNCReadyAPI(t *testing.T) {
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

	probe := func(uuid string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, "/api/session/"+uuid+"/vnc-ready", nil)
		w := httptest.NewRecorder()
		handleVNCReadyAPI(w, req)
		return w
	}

	setSession := func(uuid string, sess *Session) {
		sessionsMu.Lock()
		sessions[uuid] = sess
		sessionsMu.Unlock()
	}

	t.Run("unknown session returns 404", func(t *testing.T) {
		if w := probe("nope"); w.Code != http.StatusNotFound {
			t.Errorf("got %d, want 404", w.Code)
		}
	})

	t.Run("no VNC configured returns 503", func(t *testing.T) {
		setSession("bare", &Session{UUID: "bare"})
		if w := probe("bare"); w.Code != http.StatusServiceUnavailable {
			t.Errorf("got %d, want 503", w.Code)
		}
	})

	t.Run("local listening returns ready", func(t *testing.T) {
		ln, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		defer ln.Close()
		port := ln.Addr().(*net.TCPAddr).Port
		setSession("local", &Session{UUID: "local", VNCPort: port})
		if w := probe("local"); w.Code != http.StatusOK {
			t.Errorf("got %d, want 200", w.Code)
		}
	})

	t.Run("remote target probed even with VNCPort zero", func(t *testing.T) {
		ln, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		defer ln.Close()
		setSession("remote", &Session{UUID: "remote", RemoteVNCTarget: ln.Addr().String()})
		if w := probe("remote"); w.Code != http.StatusOK {
			t.Errorf("got %d, want 200", w.Code)
		}
	})

	t.Run("remote target down returns 503 not-ready", func(t *testing.T) {
		// Grab a port that is free, then close it so the dial fails fast.
		ln, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		addr := ln.Addr().String()
		ln.Close()
		setSession("remote-down", &Session{UUID: "remote-down", RemoteVNCTarget: addr})
		if w := probe("remote-down"); w.Code != http.StatusServiceUnavailable {
			t.Errorf("got %d, want 503", w.Code)
		}
	})
}
