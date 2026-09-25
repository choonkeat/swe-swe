package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// Only HTTPS remotes need a typed-in token, so only they surface a host. An
// SSH remote must NOT: the dialog would otherwise ask for credentials git
// never uses. Lowercased to match the browser's storage key, which
// static/modules/clone-cred-host.js lowercases.
func TestHTTPSRemoteHost(t *testing.T) {
	cases := []struct {
		url  string
		want string
	}{
		{"https://github.com/acme/private.git", "github.com"},
		{"https://x-access-token@github.com/acme/private.git", "github.com"},
		{"https://gitlab.example.com:8443/acme/private.git", "gitlab.example.com"},
		{"http://gitlab.internal/acme/x.git", "gitlab.internal"},
		{"https://GitHub.COM/acme/x.git", "github.com"},
		{"  https://github.com/acme/x.git  ", "github.com"},
		{"git@github.com:acme/private.git", ""},
		{"ssh://git@gitlab.example.com/acme/x.git", ""},
		{"file:///local/path/repo", ""},
		{"/tmp/local.git", ""},
		{"", ""},
	}
	for _, c := range cases {
		if got := httpsRemoteHost(c.url); got != c.want {
			t.Errorf("httpsRemoteHost(%q) = %q, want %q", c.url, got, c.want)
		}
	}
}

// newBranchRefreshRepo builds a git repo whose origin is remoteURL (empty ->
// no remote at all), rooted under a temp reposDir so the handlers' path check
// accepts it.
func newBranchRefreshRepo(t *testing.T, remoteURL string) string {
	t.Helper()

	reposRoot := t.TempDir()
	repoPath := filepath.Join(reposRoot, "testrepo", "workspace")
	if err := os.MkdirAll(repoPath, 0755); err != nil {
		t.Fatalf("mkdir repo: %v", err)
	}

	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = repoPath
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v failed: %v\n%s", args, err, out)
		}
	}
	run("init", "-q", "-b", "main")
	run("config", "user.email", "test@example.com")
	run("config", "user.name", "Test User")
	run("commit", "-q", "--allow-empty", "-m", "init")
	if remoteURL != "" {
		run("remote", "add", "origin", remoteURL)
	}

	oldReposDir := reposDir
	reposDir = reposRoot
	t.Cleanup(func() { reposDir = oldReposDir })

	return repoPath
}

func branchesPostRequest(t *testing.T, body string) map[string]interface{} {
	t.Helper()

	req := httptest.NewRequest(http.MethodPost, "/api/repo/branches", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handleRepoBranchesAPI(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.StatusCode)
	}

	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	return result
}

// The dialog needs to know which host to look a saved token up under, so both
// the prepare and the branches responses carry remoteHost for HTTPS remotes.
func TestRemoteHostSurfacedToDialog(t *testing.T) {
	t.Run("branches: https remote", func(t *testing.T) {
		repoPath := newBranchRefreshRepo(t, "https://github.com/acme/private.git")
		result := branchesRequest(t, "path="+url.QueryEscape(repoPath))
		if result["remoteHost"] != "github.com" {
			t.Errorf("expected remoteHost github.com, got %v", result["remoteHost"])
		}
	})

	t.Run("branches: ssh remote surfaces no host", func(t *testing.T) {
		repoPath := newBranchRefreshRepo(t, "git@github.com:acme/private.git")
		result := branchesRequest(t, "path="+url.QueryEscape(repoPath))
		if host, ok := result["remoteHost"]; ok && host != "" {
			t.Errorf("expected no remoteHost for ssh remote, got %v", host)
		}
	})

	t.Run("prepare: https remote", func(t *testing.T) {
		repoPath := newBranchRefreshRepo(t, "https://github.com/acme/private.git")
		result := prepareWorkspaceRequest(t, repoPath)
		if result["remoteHost"] != "github.com" {
			t.Errorf("expected remoteHost github.com, got %v", result["remoteHost"])
		}
	})

	t.Run("prepare: no remote", func(t *testing.T) {
		repoPath := newBranchRefreshRepo(t, "")
		result := prepareWorkspaceRequest(t, repoPath)
		if host, ok := result["remoteHost"]; ok && host != "" {
			t.Errorf("expected no remoteHost, got %v", host)
		}
	})
}

// POST is the credential-carrying form of the branches endpoint: the token
// travels in the body, never the URL (query strings land in access logs).
func TestBranchesPostRefresh(t *testing.T) {
	t.Run("no credentials: same soft-fail as GET", func(t *testing.T) {
		repoPath := newBranchRefreshRepo(t, filepath.Join(t.TempDir(), "no-such-remote.git"))
		result := branchesPostRequest(t, `{"path": `+strconv.Quote(repoPath)+`, "fetch": true}`)

		if names := branchNames(t, result); len(names) == 0 || names[0] != "main" {
			t.Errorf("expected cached branch list starting with 'main', got %v", names)
		}
		if warning, _ := result["warning"].(string); warning == "" {
			t.Errorf("expected a warning for the unreachable remote, got none")
		}
	})

	t.Run("fetch omitted: purely local, no warning", func(t *testing.T) {
		repoPath := newBranchRefreshRepo(t, filepath.Join(t.TempDir(), "no-such-remote.git"))
		result := branchesPostRequest(t, `{"path": `+strconv.Quote(repoPath)+`}`)

		if warning, ok := result["warning"]; ok {
			t.Errorf("expected no warning without fetch, got %v", warning)
		}
	})

	t.Run("credentials reach git without leaking into the response", func(t *testing.T) {
		repoPath := newBranchRefreshRepo(t, "https://127.0.0.1:1/acme/private.git")
		body := `{"path": ` + strconv.Quote(repoPath) + `, "fetch": true,` +
			`"credHost": "127.0.0.1", "credUsername": "x-access-token", "credToken": "s3cr3t-pat"}`
		result := branchesPostRequest(t, body)

		raw, err := json.Marshal(result)
		if err != nil {
			t.Fatalf("marshal response: %v", err)
		}
		if strings.Contains(string(raw), "s3cr3t-pat") {
			t.Errorf("token leaked into the response: %s", raw)
		}
		if names := branchNames(t, result); len(names) == 0 || names[0] != "main" {
			t.Errorf("expected cached branches despite the failed fetch, got %v", names)
		}
	})

	t.Run("auth failure gets its own actionable warning", func(t *testing.T) {
		repoPath := newBranchRefreshRepo(t, "https://github.com/acme/private.git")
		got := branchRefreshWarning("fatal: Authentication failed for 'https://github.com/acme/private.git/'", nil, "github.com")
		if !strings.Contains(strings.ToLower(got), "sign in") {
			t.Errorf("expected a sign-in warning, got %q", got)
		}
		if !strings.Contains(got, "github.com") {
			t.Errorf("expected the host in the warning, got %q", got)
		}
		_ = repoPath
	})

	t.Run("non-auth failure keeps the cached-branches wording", func(t *testing.T) {
		got := branchRefreshWarning("fatal: unable to access: Could not resolve host", nil, "")
		if !strings.Contains(got, "Using cached branches") {
			t.Errorf("expected the cached-branches wording, got %q", got)
		}
	})
}

// A stalled fetch must never pin the request: it is cut off and answered with
// the cached list, so Create is never waiting on the network.
func TestBranchRefreshTimesOut(t *testing.T) {
	old := branchFetchTimeout
	branchFetchTimeout = 150 * time.Millisecond
	t.Cleanup(func() { branchFetchTimeout = old })

	// A remote that accepts nothing: git blocks on connect until cut off.
	repoPath := newBranchRefreshRepo(t, "https://10.255.255.1/acme/private.git")

	done := make(chan map[string]interface{}, 1)
	go func() {
		done <- branchesPostRequest(t, `{"path": `+strconv.Quote(repoPath)+`, "fetch": true}`)
	}()

	select {
	case result := <-done:
		if names := branchNames(t, result); len(names) == 0 || names[0] != "main" {
			t.Errorf("expected cached branches after timeout, got %v", names)
		}
		if warning, _ := result["warning"].(string); warning == "" {
			t.Errorf("expected a warning after the timeout, got none")
		}
	case <-time.After(10 * time.Second):
		t.Fatal("branch refresh did not return: the fetch is not bounded by a timeout")
	}
}

// The timeout warning names the timeout, so the user knows to retry rather
// than hunt for a credential problem.
func TestBranchRefreshTimeoutWarning(t *testing.T) {
	got := branchRefreshWarning("", errBranchFetchTimeout, "github.com")
	if !strings.Contains(strings.ToLower(got), "timed out") {
		t.Errorf("expected a timeout warning, got %q", got)
	}
}

// Two refreshes of the same repo at once must both succeed. Unshared, the two
// `git fetch`es race on the same refs and the loser fails with "cannot lock
// ref" whenever the remote has new commits -- the dialog's "Unable to fetch
// latest changes".
func TestBranchRefreshConcurrentFetchesShareOne(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "src")
	clone := filepath.Join(root, "clone")
	git := func(dir string, args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	if err := os.MkdirAll(src, 0755); err != nil {
		t.Fatal(err)
	}
	git(src, "init", "-q", "-b", "main")
	git(src, "commit", "-q", "--allow-empty", "-m", "init")
	for i := 0; i < 30; i++ {
		git(src, "branch", "b"+strconv.Itoa(i))
	}
	git(root, "clone", "-q", src, clone)

	for round := 0; round < 3; round++ {
		// New commits on every branch, so each fetch has refs to update.
		for i := 0; i < 30; i++ {
			git(src, "update-ref", "refs/heads/b"+strconv.Itoa(i),
				mustGitOutput(t, src, "commit-tree", "-m", "r"+strconv.Itoa(round), "-p", "b"+strconv.Itoa(i), "HEAD^{tree}"))
		}

		errs := make(chan error, 2)
		for n := 0; n < 2; n++ {
			go func() {
				out, err := runBranchFetch(clone, "", "", "")
				if err != nil {
					err = fmt.Errorf("%v: %s", err, out)
				}
				errs <- err
			}()
		}
		for n := 0; n < 2; n++ {
			if err := <-errs; err != nil {
				t.Fatalf("round %d: concurrent refresh failed: %v", round, err)
			}
		}
	}
}

func mustGitOutput(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com")
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git %v: %v", args, err)
	}
	return strings.TrimSpace(string(out))
}
