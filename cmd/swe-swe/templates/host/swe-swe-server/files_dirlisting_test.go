package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

// TestFilesDirListingFix -- md-serve answers a folder that holds an index.html
// with that page instead of the folder's contents. An agent asked for an
// ad-hoc web app writes exactly that file into the project folder, which is
// the folder the Files pane opens on, so the pane became a copy of the app.
// The Files pane is a file browser: for such a folder it must ask md-serve for
// the listing (?listing=1), and leave every other request alone.
func TestFilesDirListingFix(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "index.html"), []byte("<h1>app</h1>"), 0644)
	os.MkdirAll(filepath.Join(dir, "docs"), 0755)
	os.WriteFile(filepath.Join(dir, "docs", "README.md"), []byte("# docs"), 0644)
	os.MkdirAll(filepath.Join(dir, "site"), 0755)
	os.WriteFile(filepath.Join(dir, "site", "index.html"), []byte("<h1>site</h1>"), 0644)

	var gotURI string
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { gotURI = r.URL.RequestURI() })

	const base = "/proxy/sess/files"
	cases := []struct {
		name, basePath, uri, accept, want string
	}{
		{"root with index.html", base, base + "/", "text/html", base + "/?listing=1"},
		{"subfolder with index.html", base, base + "/site/", "text/html,*/*", base + "/site/?listing=1"},
		{"port form (no base path)", "", "/", "text/html", "/?listing=1"},
		{"folder without index.html keeps its README page", base, base + "/docs/", "text/html", base + "/docs/"},
		{"the index.html file itself", base, base + "/index.html", "text/html", base + "/index.html"},
		{"explicit query is respected", base, base + "/?pretty=1", "text/html", base + "/?pretty=1"},
		{"non-browser fetch", base, base + "/", "application/json", base + "/"},
		{"no escaping the folder", base, base + "/../", "text/html", base + "/../"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := filesDirListingFix(tc.basePath, dir, next)
			r := httptest.NewRequest("GET", tc.uri, nil)
			r.Header.Set("Accept", tc.accept)
			h.ServeHTTP(httptest.NewRecorder(), r)
			if gotURI != tc.want {
				t.Errorf("forwarded %q, want %q", gotURI, tc.want)
			}
		})
	}
}
