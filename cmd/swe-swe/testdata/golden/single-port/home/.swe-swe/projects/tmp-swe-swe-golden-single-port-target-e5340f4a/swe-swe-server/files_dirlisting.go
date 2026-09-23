package main

import (
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// filesDirListingFix makes the Files pane show a folder's contents even when
// the folder holds an index.html.
//
// md-serve answers such a folder with the index.html page itself (that is
// what a static web server should do). The Files pane is a file browser,
// though, and an agent asked for an ad-hoc web app writes its index.html into
// the project folder -- the folder the pane opens on -- so the pane turned
// into a second copy of the app. For a browser asking for a folder whose
// index.html exists, this adds md-serve's ?listing=1. Folders without one keep
// md-serve's README-with-listing page; requests that already carry a query,
// and non-browser fetches, pass through untouched.
//
// basePath is the prefix the request still carries ("/proxy/{uuid}/files" in
// the path form, "" on the per-port listener); workDir is what md-serve serves.
func filesDirListingFix(basePath, workDir string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if (r.Method == http.MethodGet || r.Method == http.MethodHead) &&
			r.URL.RawQuery == "" &&
			strings.Contains(r.Header.Get("Accept"), "text/html") {
			rest, ok := strings.CutPrefix(r.URL.Path, basePath)
			// Only a clean folder path: "/", "/site/". Anything with ".."
			// or doubled slashes is left for md-serve to judge.
			if ok && strings.HasSuffix(rest, "/") && (rest == "/" || path.Clean(rest)+"/" == rest) {
				index := filepath.Join(workDir, filepath.FromSlash(rest), "index.html")
				if fi, err := os.Stat(index); err == nil && fi.Mode().IsRegular() {
					r = r.Clone(r.Context())
					r.URL.RawQuery = "listing=1"
				}
			}
		}
		next.ServeHTTP(w, r)
	})
}
