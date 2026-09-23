package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/fstest"
)

// TestStaticAssetsRevalidate -- the page code is cache-busted with ?v={Version},
// but a server built without a version stamp (every no-Docker install) says
// "dev" for every release, and the ES modules terminal-ui.js imports carry no
// query at all. Safari kept its saved copies across releases, so an iPad went
// on running old Preview code after two upgrades. Every static response must
// tell the browser to check back before reusing its copy.
func TestStaticAssetsRevalidate(t *testing.T) {
	h := noCacheStatic(http.FileServer(http.FS(fstest.MapFS{
		"modules/preview-check.js": {Data: []byte("export {}")},
	})))
	for _, path := range []string{"/modules/preview-check.js", "/modules/preview-check.js?v=dev"} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		if w.Code != http.StatusOK {
			t.Fatalf("%s: got %d", path, w.Code)
		}
		if got := w.Header().Get("Cache-Control"); got != "no-cache" {
			t.Errorf("%s: Cache-Control = %q, want no-cache", path, got)
		}
	}
}
