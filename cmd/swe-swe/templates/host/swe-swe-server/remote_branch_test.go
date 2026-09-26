package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"path/filepath"
	"testing"
)

func hasRef(repo, ref string) bool {
	return exec.Command("git", "-C", repo, "rev-parse", "--verify", "-q", ref).Run() == nil
}

// A branch that is online but was never downloaded is found and downloaded,
// so the dialog can offer it instead of creating a same-named new branch.
func TestCheckRemoteBranch(t *testing.T) {
	repo, _ := newCheckRepo(t)
	src := filepath.Join(filepath.Dir(filepath.Dir(repo)), "src")
	gitT(t, src, "branch", "online-only")

	if hasRef(repo, "refs/remotes/origin/online-only") {
		t.Fatal("test setup: online-only already downloaded")
	}
	res, err := checkRemoteBranch(context.Background(), repo, "origin", "online-only", "", "", "")
	if err != nil || !res.Exists || !res.Fetched {
		t.Fatalf("online-only: %+v, %v; want exists+fetched", res, err)
	}
	if !hasRef(repo, "refs/remotes/origin/online-only") {
		t.Error("online-only was not downloaded")
	}

	res, err = checkRemoteBranch(context.Background(), repo, "origin", "brand-new", "", "", "")
	if err != nil || res.Exists {
		t.Errorf("brand-new: %+v, %v; want not exists", res, err)
	}

	gitT(t, repo, "remote", "set-url", "origin", filepath.Join(t.TempDir(), "gone"))
	if _, err := checkRemoteBranch(context.Background(), repo, "origin", "brand-new", "", "", ""); !errors.Is(err, errRemoteUnreachable) {
		t.Errorf("unreachable remote: err = %v, want errRemoteUnreachable", err)
	}
}

func TestRemoteBranchAPI(t *testing.T) {
	repo := newBranchRefreshRepo(t, "")
	post := func(body any) *httptest.ResponseRecorder {
		b, _ := json.Marshal(body)
		rec := httptest.NewRecorder()
		handleRemoteBranchAPI(rec, httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(b)))
		return rec
	}
	if rec := post(map[string]string{"path": repo, "remote": "nope", "name": "x"}); rec.Code != http.StatusBadRequest {
		t.Errorf("unknown remote: status %d, want 400", rec.Code)
	}
	if rec := post(map[string]string{"path": repo, "remote": "origin", "name": "-x"}); rec.Code != http.StatusBadRequest {
		t.Errorf("option-like name: status %d, want 400", rec.Code)
	}
	if rec := post(map[string]string{"path": "/etc", "remote": "origin", "name": "x"}); rec.Code != http.StatusBadRequest {
		t.Errorf("path outside repos: status %d, want 400", rec.Code)
	}
}

// Fetching one remote leaves the others alone.
func TestRunBranchFetchOneRemote(t *testing.T) {
	repo, _ := newCheckRepo(t)
	root := filepath.Dir(filepath.Dir(repo))
	src := filepath.Join(root, "src")
	other := filepath.Join(root, "other")
	gitT(t, root, "clone", "-q", "--bare", src, other)
	gitT(t, repo, "remote", "add", "other", other)
	gitT(t, src, "branch", "on-origin")
	gitT(t, other, "branch", "on-other", "main")

	if out, err := runBranchFetch(repo, "origin", "", "", ""); err != nil {
		t.Fatalf("fetch origin: %v %s", err, out)
	}
	if !hasRef(repo, "refs/remotes/origin/on-origin") {
		t.Error("origin's branch not downloaded")
	}
	if hasRef(repo, "refs/remotes/other/on-other") {
		t.Error("fetching origin also downloaded other")
	}
}

// Picking an already-cloned repo via "Clone external repository" must not
// download anything: the old blocking `fetch --all` had no time limit and
// hard-failed on an unreachable remote.
func TestPrepareCloneOfExistingRepoDoesNotDownload(t *testing.T) {
	root := t.TempDir()
	old := reposDir
	reposDir = root
	t.Cleanup(func() { reposDir = old })

	url := "https://example.invalid/acme/private.git"
	repo := filepath.Join(root, sanitizeRepoURL(url), "workspace")
	gitT(t, root, "init", "-q", "-b", "main", repo)
	gitT(t, repo, "remote", "add", "origin", url)

	rec := httptest.NewRecorder()
	handleRepoPrepareClone(rec, url, "", "", "")
	var resp map[string]any
	json.Unmarshal(rec.Body.Bytes(), &resp)
	if rec.Code != http.StatusOK || resp["error"] != nil || resp["path"] != repo {
		t.Fatalf("status %d, reply %v; want 200 with path %s", rec.Code, resp, repo)
	}
	if resp["justCloned"] != false || resp["hasRemote"] != true {
		t.Errorf("reply %v; want justCloned=false, hasRemote=true", resp)
	}
}
