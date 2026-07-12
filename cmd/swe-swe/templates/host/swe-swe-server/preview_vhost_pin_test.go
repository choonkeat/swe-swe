package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
)

// sentinelNext records whether the pin handler delegated to the wrapped proxy.
type sentinelNext struct{ called bool }

func (s *sentinelNext) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.called = true
	w.WriteHeader(http.StatusTeapot)
}

func doPin(t *testing.T, h http.Handler, method, jsonBody string) *httptest.ResponseRecorder {
	t.Helper()
	var body *strings.Reader
	if jsonBody != "" {
		body = strings.NewReader(jsonBody)
	} else {
		body = strings.NewReader("")
	}
	req := httptest.NewRequest(method, "/__agent-reverse-proxy-debug__/vhost-pin", body)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	return rr
}

func TestVhostPinEndpoint(t *testing.T) {
	sess := &Session{PreviewPort: 8080}
	next := &sentinelNext{}
	h := previewVhostPinHandler(sess, next)

	// POST valid -> 200, pin set.
	rr := doPin(t, h, http.MethodPost, `{"name":"app1","port":5000}`)
	if rr.Code != http.StatusOK {
		t.Fatalf("POST valid: code %d, want 200 (body %q)", rr.Code, rr.Body.String())
	}
	if pin := sess.getVhostPin(); pin == nil || pin.Name != "app1" || pin.Port != 5000 {
		t.Fatalf("after POST, pin = %+v, want {app1 5000}", pin)
	}

	// GET -> current pin.
	rr = doPin(t, h, http.MethodGet, "")
	var got struct {
		Pinned bool   `json:"pinned"`
		Name   string `json:"name"`
		Port   int    `json:"port"`
	}
	json.Unmarshal(rr.Body.Bytes(), &got)
	if !got.Pinned || got.Name != "app1" || got.Port != 5000 {
		t.Errorf("GET pin = %+v, want pinned app1:5000", got)
	}

	// DELETE -> cleared.
	rr = doPin(t, h, http.MethodDelete, "")
	if rr.Code != http.StatusNoContent {
		t.Errorf("DELETE: code %d, want 204", rr.Code)
	}
	if sess.getVhostPin() != nil {
		t.Errorf("after DELETE, pin still set")
	}

	// GET after delete -> pinned:false.
	rr = doPin(t, h, http.MethodGet, "")
	got = struct {
		Pinned bool   `json:"pinned"`
		Name   string `json:"name"`
		Port   int    `json:"port"`
	}{}
	json.Unmarshal(rr.Body.Bytes(), &got)
	if got.Pinned {
		t.Errorf("GET after DELETE = %+v, want pinned:false", got)
	}

	// Invalid inputs -> 400.
	for _, tc := range []struct {
		name, body string
	}{
		{"port-too-low", `{"name":"app1","port":80}`},
		{"port-too-high", `{"name":"app1","port":99999}`},
		{"bad-name", `{"name":"-bad","port":5000}`},
		{"numeric-name", `{"name":"5000","port":5000}`},
		{"bad-json", `{not json`},
	} {
		rr = doPin(t, h, http.MethodPost, tc.body)
		if rr.Code != http.StatusBadRequest {
			t.Errorf("POST %s: code %d, want 400", tc.name, rr.Code)
		}
	}
	if sess.getVhostPin() != nil {
		t.Errorf("invalid POSTs must not set a pin")
	}

	// Non-pin path delegates to next.
	req := httptest.NewRequest(http.MethodGet, "/some/other/path", nil)
	rr = httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if !next.called || rr.Code != http.StatusTeapot {
		t.Errorf("non-pin path should delegate to next (called=%v code=%d)", next.called, rr.Code)
	}
}

// TestVhostPinRouting proves that after pinning, a label-less (single-label
// bare origin) request routes to the pinned target with the rewritten Host,
// and that clearing the pin restores the legacy fixed-target fallback.
func TestVhostPinRouting(t *testing.T) {
	var gotHost string
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHost = r.Host
		w.Write([]byte("pinned-backend"))
	}))
	defer backend.Close()
	bu, _ := url.Parse(backend.URL)
	port, _ := strconv.Atoi(bu.Port())
	if port < 1024 {
		t.Skipf("ephemeral backend port %d < 1024", port)
	}

	sess := &Session{PreviewPort: 65000}
	proxy := newPreviewProxyForTest(t, sess)

	// Pin app1 -> backend port.
	sess.setVhostPin("app1", port)

	// Label-less bare origin request.
	req := httptest.NewRequest("GET", "/", nil)
	req.Host = "myhost:23000" // single label -> no vhost prefix -> pin applies
	rr := httptest.NewRecorder()
	proxy.ServeHTTP(rr, req)

	if body := rr.Body.String(); body != "pinned-backend" {
		t.Errorf("body = %q, want pinned-backend (should reach pinned target)", body)
	}
	if want := fmt.Sprintf("app1.lvh.me:%d", port); gotHost != want {
		t.Errorf("upstream Host = %q, want %q", gotHost, want)
	}

	// Clearing the pin restores legacy fallback (fixed target, clobbered Host).
	sess.clearVhostPin()
	fixedBackend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHost = r.Host
		w.Write([]byte("fixed"))
	}))
	defer fixedBackend.Close()
	fb, _ := url.Parse(fixedBackend.URL)
	sess2 := &Session{PreviewPort: 65000}
	p2, _ := newPreviewProxyForTestTarget(t, sess2, fb)
	req2 := httptest.NewRequest("GET", "/", nil)
	req2.Host = "myhost:23000"
	rr2 := httptest.NewRecorder()
	p2.ServeHTTP(rr2, req2)
	if gotHost != fb.Host {
		t.Errorf("after clear, upstream Host = %q, want %q (legacy clobber)", gotHost, fb.Host)
	}
}
