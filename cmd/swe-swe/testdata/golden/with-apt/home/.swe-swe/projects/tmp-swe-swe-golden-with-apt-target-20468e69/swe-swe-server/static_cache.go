package main

import "net/http"

// noCacheStatic makes the browser check back before reusing a saved copy of
// swe-swe's own page code.
//
// The page links its scripts and styles with ?v={Version}, but a server built
// without a version stamp -- every no-Docker install -- says "dev" for every
// release, and the ES modules terminal-ui.js imports carry no query at all.
// Safari kept serving its saved copies across upgrades, so an iPad went on
// running old Preview code. The files are small; fetching them fresh is cheap.
func noCacheStatic(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-cache")
		next.ServeHTTP(w, r)
	})
}
