package main

import (
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestHandleVNCReadyAPI covers the readiness probe in both modes: local
// (probe localhost:VNCPort) and remote (probe sess.RemoteVNCTarget -- the same
// target the VNC proxy uses). The remote path regressed to a permanent 503
// before the RemoteVNCTarget branch existed, which stalled Agent View on
// remote backends.
//
// The probe must require an HTTP 200 for /vnc_lite.html, not a mere TCP
// accept: port forwarders in the path (docker-proxy for a published backend
// port on Docker for Mac, Lima's forwards) accept connects while the real
// websockify is still down, and a premature ready lets the Agent View iframe
// commit an empty 502 document it never retries.
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

	// A stand-in websockify: serves vnc_lite.html like the real one does.
	websockify := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/vnc_lite.html" {
			w.Write([]byte("<html>noVNC</html>"))
			return
		}
		http.NotFound(w, r)
	}))
	defer websockify.Close()
	websockifyPort := websockify.Listener.Addr().(*net.TCPAddr).Port

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

	t.Run("local serving returns ready", func(t *testing.T) {
		setSession("local", &Session{UUID: "local", VNCPort: websockifyPort})
		if w := probe("local"); w.Code != http.StatusOK {
			t.Errorf("got %d, want 200", w.Code)
		}
	})

	t.Run("remote target probed even with VNCPort zero", func(t *testing.T) {
		setSession("remote", &Session{UUID: "remote", RemoteVNCTarget: websockify.Listener.Addr().String()})
		if w := probe("remote"); w.Code != http.StatusOK {
			t.Errorf("got %d, want 200", w.Code)
		}
	})

	t.Run("accepting but non-serving port is NOT ready (docker-proxy lie)", func(t *testing.T) {
		// A listener that accepts and immediately closes -- the docker-proxy /
		// Lima-forward behavior when the container listener is down. The old
		// dial-based probe wrongly reported ready here.
		ln, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		defer ln.Close()
		go func() {
			for {
				conn, err := ln.Accept()
				if err != nil {
					return
				}
				conn.Close()
			}
		}()
		setSession("half-open", &Session{UUID: "half-open", RemoteVNCTarget: ln.Addr().String()})
		if w := probe("half-open"); w.Code != http.StatusServiceUnavailable {
			t.Errorf("got %d, want 503", w.Code)
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
