package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestGitLabAPI covers the decision logic of the GitLab Test-connection
// probe against an httptest server: a 200 with a username is "Connected
// as", 401/403 are definitive failures (handled=true), and 404 means
// "not a GitLab API surface" so the caller falls back (handled=false).
// Also asserts the PAT rides in the PRIVATE-TOKEN header.
func TestGitLabAPI(t *testing.T) {
	t.Run("200 with username", func(t *testing.T) {
		var gotToken string
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			gotToken = r.Header.Get("PRIVATE-TOKEN")
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"username":"octocat"}`))
		}))
		defer srv.Close()
		handled, ok, msg := testGitLabAPI(context.Background(), srv.URL, "glpat-xyz")
		if !handled || !ok {
			t.Fatalf("got handled=%v ok=%v, want true/true", handled, ok)
		}
		if msg != "Connected as @octocat (GitLab)" {
			t.Errorf("msg: got %q", msg)
		}
		if gotToken != "glpat-xyz" {
			t.Errorf("PRIVATE-TOKEN header: got %q, want glpat-xyz", gotToken)
		}
	})

	t.Run("401 is a definitive failure", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusUnauthorized)
		}))
		defer srv.Close()
		handled, ok, msg := testGitLabAPI(context.Background(), srv.URL, "bad")
		if !handled || ok {
			t.Fatalf("got handled=%v ok=%v, want true/false", handled, ok)
		}
		if msg != "Invalid credentials (HTTP 401)" {
			t.Errorf("msg: got %q", msg)
		}
	})

	t.Run("404 means fall back to generic", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNotFound)
		}))
		defer srv.Close()
		handled, _, _ := testGitLabAPI(context.Background(), srv.URL, "x")
		if handled {
			t.Errorf("got handled=true, want false (caller should fall back)")
		}
	})

	t.Run("network error falls back", func(t *testing.T) {
		// Unroutable URL -> Do() errors -> handled=false.
		handled, _, _ := testGitLabAPI(context.Background(), "http://127.0.0.1:0/api/v4/user", "x")
		if handled {
			t.Errorf("got handled=true on network error, want false")
		}
	})
}
