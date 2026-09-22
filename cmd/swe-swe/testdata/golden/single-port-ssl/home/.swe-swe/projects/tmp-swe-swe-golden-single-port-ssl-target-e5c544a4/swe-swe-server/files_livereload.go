package main

import (
	"net/http"
	"strings"
)

// filesLivereloadPathFix makes md-serve's live reload work behind the Files
// path proxy. md-serve's page polls `/_md-serve-livereload?path=` +
// location.pathname; agentproxy re-prefixes the URL, but the pathname in the
// query still starts with basePath, a path md-serve does not serve, so every
// poll 404'd and the pane never refreshed. Strip basePath from that one query
// parameter on that one endpoint.
func filesLivereloadPathFix(basePath string, next http.Handler) http.Handler {
	pollPath := basePath + "/_md-serve-livereload"
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == pollPath {
			q := r.URL.Query()
			if p := q.Get("path"); p == basePath || strings.HasPrefix(p, basePath+"/") {
				rest := strings.TrimPrefix(p, basePath)
				if rest == "" {
					rest = "/"
				}
				q.Set("path", rest)
				r = r.Clone(r.Context())
				r.URL.RawQuery = q.Encode()
			}
		}
		next.ServeHTTP(w, r)
	})
}
