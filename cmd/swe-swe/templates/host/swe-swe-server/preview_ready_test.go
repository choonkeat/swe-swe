package main

import (
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestHandlePreviewReadyAPI -- the Preview pane asks this whether the app is
// listening on the session's PORT, so it can reload itself when the app comes
// up. It cannot rely on the proxy's own "start your app" page doing that: a
// gateway in front may swap that page (status 502) for one of its own, which
// never reloads, and the pane's readiness probe gives up after 10 tries.
func TestHandlePreviewReadyAPI(t *testing.T) {
	const uuid = "preview-ready-test"

	ln, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	upPort := ln.Addr().(*net.TCPAddr).Port
	defer ln.Close()

	ln2, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	downPort := ln2.Addr().(*net.TCPAddr).Port
	ln2.Close()

	get := func(path string) int {
		w := httptest.NewRecorder()
		handlePreviewReadyAPI(w, httptest.NewRequest(http.MethodGet, path, nil))
		return w.Code
	}

	if code := get("/api/session/nope/preview-ready"); code != http.StatusNotFound {
		t.Errorf("unknown session: got %d, want 404", code)
	}

	registerTestSession(t, uuid, &Session{UUID: uuid, PreviewPort: downPort})
	if code := get("/api/session/" + uuid + "/preview-ready"); code != http.StatusServiceUnavailable {
		t.Errorf("app down: got %d, want 503", code)
	}

	registerTestSession(t, uuid, &Session{UUID: uuid, PreviewPort: upPort})
	if code := get("/api/session/" + uuid + "/preview-ready"); code != http.StatusOK {
		t.Errorf("app up: got %d, want 200", code)
	}
}
