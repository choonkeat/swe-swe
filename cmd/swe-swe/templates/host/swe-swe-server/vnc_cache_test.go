package main

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"testing"
)

// The viewer page must not be kept by the browser: a stale vnc_lite.html hid
// a fixed viewer from an iPad pane after the browser-backend was rebuilt.
func TestVNCReverseProxyViewerNoCache(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("ok"))
	}))
	defer upstream.Close()
	u, _ := url.Parse(upstream.URL)
	port, _ := strconv.Atoi(u.Port())
	rp := newVNCReverseProxy(&Session{}, port)

	for path, want := range map[string]string{
		"/vnc_lite.html": "no-cache",
		"/core/rfb.js":   "",
		"/websockify":    "",
	} {
		rec := httptest.NewRecorder()
		rp.ServeHTTP(rec, httptest.NewRequest("GET", path, nil))
		if got := rec.Header().Get("Cache-Control"); got != want {
			t.Errorf("GET %s: Cache-Control %q, want %q", path, got, want)
		}
	}
}
