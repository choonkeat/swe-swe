package main

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	agentproxy "github.com/choonkeat/agent-reverse-proxy"
)

// md-serve's live-reload poller sends `?path=` + location.pathname. Behind the
// Files path proxy that pathname carries the /proxy/{uuid}/files prefix, which
// md-serve has never heard of, so every poll 404'd and the pane never
// refreshed. The wrapper must hand md-serve the path it actually serves.
func TestFilesLivereloadStripsProxyPrefix(t *testing.T) {
	const base = "/proxy/abc-123/files"

	var gotPath, gotQuery string
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotQuery = r.URL.Query().Get("path")
	}))
	defer backend.Close()
	target, _ := url.Parse(backend.URL)

	proxy, err := agentproxy.New(agentproxy.Config{
		BasePath:   base,
		Target:     target,
		ToolPrefix: "files",
		NoInject:   true,
	})
	if err != nil {
		t.Fatalf("agentproxy.New: %v", err)
	}
	h := filesLivereloadPathFix(base, proxy)

	for _, tc := range []struct{ pagePath, want string }{
		{base + "/", "/"},
		{base, "/"},
		{base + "/docs/README.md", "/docs/README.md"},
		// Already relative to md-serve (the per-port form): left alone.
		{"/docs/", "/docs/"},
	} {
		req := httptest.NewRequest("GET", base+"/_md-serve-livereload?path="+url.QueryEscape(tc.pagePath), nil)
		h.ServeHTTP(httptest.NewRecorder(), req)
		if gotPath != "/_md-serve-livereload" {
			t.Errorf("page %q: backend path = %q, want /_md-serve-livereload", tc.pagePath, gotPath)
		}
		if gotQuery != tc.want {
			t.Errorf("page %q: backend ?path = %q, want %q", tc.pagePath, gotQuery, tc.want)
		}
	}
}

// Everything that is not the live-reload poll passes through untouched.
func TestFilesLivereloadLeavesOtherRequestsAlone(t *testing.T) {
	const base = "/proxy/abc-123/files"
	var gotQuery string
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
	})
	req := httptest.NewRequest("GET", base+"/search?path=%2Fproxy%2Fabc-123%2Ffiles%2Fx", nil)
	filesLivereloadPathFix(base, next).ServeHTTP(httptest.NewRecorder(), req)
	if gotQuery != "path=%2Fproxy%2Fabc-123%2Ffiles%2Fx" {
		t.Errorf("non-livereload query rewritten: %q", gotQuery)
	}
}
