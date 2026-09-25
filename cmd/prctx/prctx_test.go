package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestMapDiscussions(t *testing.T) {
	pos := &glPosition{NewPath: "a.go", NewLine: 12, HeadSHA: "abc"}
	inline := glDiscussion{
		ID: "disc1",
		Notes: []glNote{
			{ID: 1, Body: "inline root", Type: "DiffNote", Resolved: true, Position: pos},
			{ID: 2, Body: "inline reply", Type: "DiffNote", Position: pos},
		},
	}
	general := glDiscussion{
		ID:    "disc2",
		Notes: []glNote{{ID: 3, Body: "general note"}},
	}
	// Removed-line comment: NewLine 0 -> falls back to old path/line, side LEFT.
	removed := glDiscussion{
		ID: "disc3",
		Notes: []glNote{
			{ID: 4, Body: "on removed line", Type: "DiffNote",
				Position: &glPosition{OldPath: "b.go", OldLine: 7, HeadSHA: "def"}},
		},
	}

	threads, notes := mapDiscussions([]glDiscussion{inline, general, removed})
	if len(threads) != 2 {
		t.Fatalf("got %d threads, want 2", len(threads))
	}
	if len(notes) != 1 || notes[0].Body != "general note" {
		t.Fatalf("notes = %+v, want one general note", notes)
	}
	if threads[0].ID != "disc1" || !threads[0].Resolved || len(threads[0].Comments) != 2 {
		t.Errorf("thread 0: id=%s resolved=%v comments=%d, want disc1/true/2",
			threads[0].ID, threads[0].Resolved, len(threads[0].Comments))
	}
	if threads[0].Path != "a.go" || threads[0].Line != 12 || threads[0].Side != "RIGHT" {
		t.Errorf("thread 0 anchor = %s:%d %s, want a.go:12 RIGHT", threads[0].Path, threads[0].Line, threads[0].Side)
	}
	if threads[1].Path != "b.go" || threads[1].Line != 7 || threads[1].Side != "LEFT" {
		t.Errorf("thread 1 anchor = %s:%d %s, want b.go:7 LEFT", threads[1].Path, threads[1].Line, threads[1].Side)
	}
}

func TestProviderFor(t *testing.T) {
	cases := []struct {
		name string
		ref  PRRef
		want string // "" means expect an error
	}{
		{"github.com", PRRef{Host: "github.com"}, "main.githubProvider"},
		{"gitlab.com", PRRef{Host: "gitlab.com"}, "main.gitlabProvider"},
		{"gitlab subdomain", PRRef{Host: "gitlab.example.org"}, "main.gitlabProvider"},
		{"github subdomain", PRRef{Host: "github.example.org"}, "main.githubProvider"},
		{"unknown host", PRRef{Host: "bitbucket.org"}, ""},
		// A custom domain is resolvable when the url named the kind.
		{"custom host, pull url", PRRef{Host: "git.corp.example", Kind: kindGitHub}, "main.githubProvider"},
		{"custom host, mr url", PRRef{Host: "scm.corp.example", Kind: kindGitLab}, "main.gitlabProvider"},
	}
	for _, c := range cases {
		p, err := providerFor(c.ref)
		if c.want == "" {
			if err == nil {
				t.Errorf("%s: expected error, got %T", c.name, p)
			}
			continue
		}
		if err != nil {
			t.Errorf("%s: unexpected error %v", c.name, err)
			continue
		}
		if got := fmt.Sprintf("%T", p); got != c.want {
			t.Errorf("%s: got %s, want %s", c.name, got, c.want)
		}
	}
}

// A bare number against a custom domain has no url to learn the kind from; the
// env lists are the only signal, and they outrank the host-name heuristics.
func TestProviderForEnvHosts(t *testing.T) {
	t.Setenv("PRCTX_GITHUB_HOSTS", "git.corp.example, https://code.corp.example/")
	t.Setenv("PRCTX_GITLAB_HOSTS", "GITLAB-MIRROR.corp.example")

	cases := []struct {
		host string
		want string
	}{
		{"git.corp.example", "main.githubProvider"},
		{"code.corp.example", "main.githubProvider"},
		{"gitlab-mirror.corp.example", "main.gitlabProvider"},
	}
	for _, c := range cases {
		p, err := providerFor(PRRef{Host: c.host})
		if err != nil {
			t.Errorf("providerFor(%q): unexpected error %v", c.host, err)
			continue
		}
		if got := fmt.Sprintf("%T", p); got != c.want {
			t.Errorf("providerFor(%q) = %s, want %s", c.host, got, c.want)
		}
	}
}

// An explicit env list wins over a misleading host name.
func TestProviderForEnvOverridesHostname(t *testing.T) {
	t.Setenv("PRCTX_GITHUB_HOSTS", "gitlab.corp.example")
	p, err := providerFor(PRRef{Host: "gitlab.corp.example"})
	if err != nil {
		t.Fatalf("unexpected error %v", err)
	}
	if got := fmt.Sprintf("%T", p); got != "main.githubProvider" {
		t.Errorf("got %s, want main.githubProvider", got)
	}
}

// --token-env is stripped from args wherever it appears, in either spelling.
func TestExtractTokenEnv(t *testing.T) {
	cases := []struct {
		name     string
		in       []string
		wantArgs []string
		wantEnv  string
	}{
		{"absent", []string{"fetch", "7"}, []string{"fetch", "7"}, ""},
		{"before command", []string{"--token-env", "MY_TOK", "fetch", "7"}, []string{"fetch", "7"}, "MY_TOK"},
		{"after command", []string{"fetch", "--token-env", "MY_TOK", "7"}, []string{"fetch", "7"}, "MY_TOK"},
		{"equals form", []string{"fetch", "--token-env=MY_TOK", "7"}, []string{"fetch", "7"}, "MY_TOK"},
		{"dangling value", []string{"fetch", "--token-env"}, []string{"fetch"}, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			tokenEnvOverride = ""
			t.Cleanup(func() { tokenEnvOverride = "" })
			got := extractTokenEnv(c.in)
			if fmt.Sprint(got) != fmt.Sprint(c.wantArgs) {
				t.Errorf("args = %v, want %v", got, c.wantArgs)
			}
			if tokenEnvOverride != c.wantEnv {
				t.Errorf("tokenEnvOverride = %q, want %q", tokenEnvOverride, c.wantEnv)
			}
		})
	}
}

// The override env var wins over the provider defaults; the error names every
// var consulted.
func TestLookupTokenOverride(t *testing.T) {
	t.Setenv("GITLAB_TOKEN", "default-tok")
	t.Setenv("MY_TOK", "override-tok")
	tokenEnvOverride = "MY_TOK"
	t.Cleanup(func() { tokenEnvOverride = "" })

	if got, _ := (gitlabProvider{}).token(); got != "override-tok" {
		t.Errorf("gitlab token = %q, want override-tok", got)
	}

	// Unset override falls back to the provider default.
	tokenEnvOverride = "ABSENT_TOK"
	if got, _ := (gitlabProvider{}).token(); got != "default-tok" {
		t.Errorf("gitlab token = %q, want default-tok", got)
	}

	// GITLAB_TOKEN unset: fall back to swe-swe's copy of the HTTPS token.
	t.Setenv("GITLAB_TOKEN", "")
	t.Setenv("SWE_SWE_GITLAB_HTTPS_TOKEN", "https-tok")
	if got, _ := (gitlabProvider{}).token(); got != "https-tok" {
		t.Errorf("gitlab token = %q, want https-tok", got)
	}

	// Nothing set anywhere: the error lists every name tried, override first.
	t.Setenv("SWE_SWE_GITLAB_HTTPS_TOKEN", "")
	_, err := (gitlabProvider{}).token()
	if err == nil || err.Error() != "ABSENT_TOK / GITLAB_TOKEN / SWE_SWE_GITLAB_HTTPS_TOKEN is not set" {
		t.Errorf("err = %v, want \"ABSENT_TOK / GITLAB_TOKEN / SWE_SWE_GITLAB_HTTPS_TOKEN is not set\"", err)
	}
}

// Without an override, GitHub reads GITHUB_TOKEN, GH_TOKEN, then swe-swe's
// HTTPS-token copy, and the error names all three.
func TestLookupTokenDefaults(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "")
	t.Setenv("SWE_SWE_GITHUB_HTTPS_TOKEN", "https-tok")
	t.Setenv("GH_TOKEN", "gh-tok")
	tokenEnvOverride = ""
	if got, _ := (githubProvider{}).token(); got != "gh-tok" {
		t.Errorf("github token = %q, want gh-tok", got)
	}
	// GH_TOKEN unset: fall back to swe-swe's copy of the HTTPS token.
	t.Setenv("GH_TOKEN", "")
	if got, _ := (githubProvider{}).token(); got != "https-tok" {
		t.Errorf("github token = %q, want https-tok", got)
	}
	t.Setenv("SWE_SWE_GITHUB_HTTPS_TOKEN", "")
	_, err := (githubProvider{}).token()
	if err == nil || err.Error() != "GITHUB_TOKEN / GH_TOKEN / SWE_SWE_GITHUB_HTTPS_TOKEN is not set" {
		t.Errorf("err = %v, want \"GITHUB_TOKEN / GH_TOKEN / SWE_SWE_GITHUB_HTTPS_TOKEN is not set\"", err)
	}
}

func TestAPIBaseOverrides(t *testing.T) {
	t.Setenv("PRCTX_GITHUB_API_BASE", "https://git.corp.example/api/v3/")
	t.Setenv("PRCTX_GITHUB_GRAPHQL_URL", "https://git.corp.example/api/graphql")
	t.Setenv("PRCTX_GITLAB_API_BASE", "https://scm.corp.example/gitlab/api/v4")

	if got := (githubProvider{}).apiBase("github.com"); got != "https://git.corp.example/api/v3" {
		t.Errorf("github apiBase = %q", got)
	}
	if got := (githubProvider{}).graphqlEndpoint("github.com"); got != "https://git.corp.example/api/graphql" {
		t.Errorf("github graphqlEndpoint = %q", got)
	}
	if got := (gitlabProvider{}).apiBase("gitlab.com"); got != "https://scm.corp.example/gitlab/api/v4" {
		t.Errorf("gitlab apiBase = %q", got)
	}
}

// Without overrides, enterprise hosts get the conventional API layouts.
func TestAPIBaseDefaults(t *testing.T) {
	if got := (githubProvider{}).apiBase("github.com"); got != "https://api.github.com" {
		t.Errorf("github.com apiBase = %q", got)
	}
	if got := (githubProvider{}).apiBase("git.corp.example"); got != "https://git.corp.example/api/v3" {
		t.Errorf("enterprise apiBase = %q", got)
	}
	if got := (githubProvider{}).graphqlEndpoint("git.corp.example"); got != "https://git.corp.example/api/graphql" {
		t.Errorf("enterprise graphqlEndpoint = %q", got)
	}
	if got := (gitlabProvider{}).apiBase("scm.corp.example"); got != "https://scm.corp.example/api/v4" {
		t.Errorf("gitlab apiBase = %q", got)
	}
}

func TestParseRef(t *testing.T) {
	cases := []struct {
		in   string
		want PRRef
	}{
		{"https://github.com/owner/repo/pull/42",
			PRRef{Host: "github.com", Kind: kindGitHub, Owner: "owner", Repo: "repo", Number: 42}},
		{"https://gitlab.com/group/sub/repo/-/merge_requests/7",
			PRRef{Host: "gitlab.com", Kind: kindGitLab, Owner: "group/sub", Repo: "repo", Number: 7}},
		// Custom domains carry the same self-describing url shape.
		{"https://git.corp.example/team/api/pull/9",
			PRRef{Host: "git.corp.example", Kind: kindGitHub, Owner: "team", Repo: "api", Number: 9}},
		{"https://scm.corp.example/team/api/-/merge_requests/3",
			PRRef{Host: "scm.corp.example", Kind: kindGitLab, Owner: "team", Repo: "api", Number: 3}},
	}
	for _, c := range cases {
		got, err := parseRef(c.in)
		if err != nil {
			t.Fatalf("parseRef(%q): %v", c.in, err)
		}
		if got != c.want {
			t.Errorf("parseRef(%q) = %+v, want %+v", c.in, got, c.want)
		}
	}
}

func TestParseRefRejectsGarbage(t *testing.T) {
	if _, err := parseRef("not-a-url-or-number"); err == nil {
		t.Fatal("expected error for non-url non-number arg")
	}
}

func TestMapThreads(t *testing.T) {
	var n0, n1 gqlThread
	n0.ID = "PRRT_aaa"
	n0.IsResolved = true
	n0.Path = "a.go"
	n0.Line = 10
	n0.DiffSide = "RIGHT"
	c0 := gqlComment{DatabaseID: 1, Body: "root one"}
	c0.Commit.Oid = "abc"
	c1 := gqlComment{DatabaseID: 2, Body: "reply to one"}
	n0.Comments.Nodes = []gqlComment{c0, c1}

	n1.ID = "PRRT_bbb"
	n1.Path = "b.go"
	n1.OriginalLine = 5
	n1.DiffSide = "LEFT"
	c2 := gqlComment{DatabaseID: 3, Body: "root two"}
	c2.Commit.Oid = "def"
	n1.Comments.Nodes = []gqlComment{c2}

	threads := mapThreads([]gqlThread{n0, n1})
	if len(threads) != 2 {
		t.Fatalf("got %d threads, want 2", len(threads))
	}
	if threads[0].ID != "PRRT_aaa" || !threads[0].Resolved || len(threads[0].Comments) != 2 {
		t.Errorf("thread 0: id=%s resolved=%v comments=%d, want id=PRRT_aaa resolved=true comments=2",
			threads[0].ID, threads[0].Resolved, len(threads[0].Comments))
	}
	// Line falls back to OriginalLine when Line is 0.
	if threads[1].Line != 5 {
		t.Errorf("thread 1 line = %d, want 5 (from OriginalLine)", threads[1].Line)
	}
}

// A GitLab 401/403 explains the scope needed, and names the Settings HTTPS
// token only when that fallback is the token actually in use.
func TestGitLabPermHint(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"error":"insufficient_scope"}`))
	}))
	defer srv.Close()
	t.Setenv("PRCTX_GITLAB_API_BASE", srv.URL)
	tokenEnvOverride = ""
	ref := PRRef{Host: "gitlab.example.com", Owner: "o", Repo: "r"}

	t.Setenv("GITLAB_TOKEN", "")
	t.Setenv("SWE_SWE_GITLAB_HTTPS_TOKEN", "git-only")
	_, err := (gitlabProvider{}).do(ref, "GET", "/x", nil, nil)
	if err == nil || !strings.Contains(err.Error(), `"api" scope`) || !strings.Contains(err.Error(), "SWE_SWE_GITLAB_HTTPS_TOKEN") {
		t.Errorf("fallback token err = %v, want api-scope hint naming SWE_SWE_GITLAB_HTTPS_TOKEN", err)
	}

	t.Setenv("GITLAB_TOKEN", "mine")
	_, err = (gitlabProvider{}).do(ref, "GET", "/x", nil, nil)
	if err == nil || !strings.Contains(err.Error(), `"api" scope`) || strings.Contains(err.Error(), "SWE_SWE_GITLAB_HTTPS_TOKEN") {
		t.Errorf("own token err = %v, want api-scope hint without the fallback note", err)
	}

	if h := glPermHint(http.StatusNotFound); h != "" {
		t.Errorf("glPermHint(404) = %q, want none", h)
	}
}
