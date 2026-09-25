package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// offline points origin at a remote that leaves a marker file if anything
// contacts it, and fails the test at cleanup if the marker exists. Deletes,
// undo, leftover removal and switch-back must never go online.
func offline(t *testing.T, repo string) {
	t.Helper()
	marker := filepath.Join(t.TempDir(), "went-online")
	gitT(t, repo, "config", "protocol.ext.allow", "always")
	gitT(t, repo, "remote", "set-url", "origin", "ext::sh -c touch% "+marker)
	t.Cleanup(func() {
		if _, err := os.Stat(marker); err == nil {
			t.Errorf("a command contacted the online copy")
		}
	})
}

func branchExists(repo, branch string) bool {
	return exec.Command("git", "-C", repo, "rev-parse", "--verify", "--quiet", "refs/heads/"+branch).Run() == nil
}

func dirExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

func wantRefusal(t *testing.T, err error, what string) {
	t.Helper()
	var r branchRefusal
	if !errors.As(err, &r) {
		t.Errorf("%s: err = %v, want a refusal", what, err)
	}
}

func TestDeleteBranch(t *testing.T) {
	repo, container := newCheckRepo(t)
	offline(t, repo)

	t.Run("plain branch goes", func(t *testing.T) {
		gitT(t, repo, "branch", "p")
		res, err := deleteBranch(repo, "p", false, nil)
		if err != nil || !res.Deleted || !res.Undoable {
			t.Fatalf("res=%+v err=%v", res, err)
		}
		if branchExists(repo, "p") {
			t.Errorf("branch p still exists")
		}
	})

	t.Run("branch with a folder loses both", func(t *testing.T) {
		folder := filepath.Join(container, "w")
		gitT(t, repo, "worktree", "add", "-q", "-b", "w", folder)
		res, err := deleteBranch(repo, "w", false, nil)
		if err != nil || !res.Deleted || res.Undo.Folder != folder {
			t.Fatalf("res=%+v err=%v", res, err)
		}
		if branchExists(repo, "w") || dirExists(folder) {
			t.Errorf("branch or folder still exists")
		}
	})

	t.Run("something to lose, unconfirmed: needs confirm, nothing removed", func(t *testing.T) {
		gitT(t, repo, "checkout", "-q", "-b", "lose")
		commitOn(t, repo, "only here")
		gitT(t, repo, "checkout", "-q", "main")
		res, err := deleteBranch(repo, "lose", false, nil)
		if err != nil || res.Deleted || !res.NeedsConfirm || res.Reason == "" || !res.Undoable {
			t.Fatalf("res=%+v err=%v", res, err)
		}
		if !branchExists(repo, "lose") {
			t.Fatalf("branch removed without confirm")
		}
		res, err = deleteBranch(repo, "lose", true, nil)
		if err != nil || !res.Deleted || !res.Undoable {
			t.Fatalf("confirmed: res=%+v err=%v", res, err)
		}
	})

	t.Run("folder edits, confirmed: deleted and can't be undone", func(t *testing.T) {
		folder := filepath.Join(container, "dirty")
		gitT(t, repo, "worktree", "add", "-q", "-b", "dirty", folder)
		writeFile(t, filepath.Join(folder, "a.txt"), "edited\n")
		res, err := deleteBranch(repo, "dirty", false, nil)
		if err != nil || !res.NeedsConfirm || res.Undoable {
			t.Fatalf("unconfirmed: res=%+v err=%v", res, err)
		}
		res, err = deleteBranch(repo, "dirty", true, nil)
		if err != nil || !res.Deleted || res.Undoable {
			t.Fatalf("confirmed: res=%+v err=%v", res, err)
		}
		if dirExists(folder) || branchExists(repo, "dirty") {
			t.Errorf("folder or branch still exists")
		}
	})

	t.Run("refusals", func(t *testing.T) {
		folder := filepath.Join(container, "busy")
		gitT(t, repo, "worktree", "add", "-q", "-b", "busy", folder)
		_, err := deleteBranch(repo, "busy", true, map[string]bool{folder: true})
		wantRefusal(t, err, "in use")

		_, err = deleteBranch(repo, "main", true, nil)
		wantRefusal(t, err, "default branch")

		gitT(t, repo, "checkout", "-q", "-b", "cur")
		_, err = deleteBranch(repo, "cur", true, nil)
		wantRefusal(t, err, "workspace's own branch")
		gitT(t, repo, "checkout", "-q", "main")

		gitT(t, repo, "update-ref", "refs/remotes/origin/online", "HEAD")
		_, err = deleteBranch(repo, "online", true, nil)
		wantRefusal(t, err, "online only")

		for _, bad := range []string{"-rf", "a..b", ""} {
			_, err = deleteBranch(repo, bad, true, nil)
			wantRefusal(t, err, "bad name "+bad)
		}
		if !branchExists(repo, "busy") || !branchExists(repo, "main") || !branchExists(repo, "cur") {
			t.Errorf("a refused branch was removed")
		}
	})

	t.Run("safe order: folder removal fails, branch stays", func(t *testing.T) {
		folder := filepath.Join(container, "locked")
		gitT(t, repo, "worktree", "add", "-q", "-b", "locked", folder)
		gitT(t, repo, "worktree", "lock", folder)
		if _, err := deleteBranch(repo, "locked", true, nil); err == nil {
			t.Fatalf("want an error when the folder can't be removed")
		}
		if !branchExists(repo, "locked") {
			t.Errorf("branch was removed although its folder could not be")
		}
	})
}

func TestUndoBranchDelete(t *testing.T) {
	repo, container := newCheckRepo(t)
	offline(t, repo)

	gitT(t, repo, "checkout", "-q", "-b", "gone")
	commitOn(t, repo, "only here")
	sha := gitT(t, repo, "rev-parse", "HEAD")
	gitT(t, repo, "checkout", "-q", "main")
	folder := filepath.Join(container, "gone")
	gitT(t, repo, "worktree", "add", "-q", folder, "gone")

	res, err := deleteBranch(repo, "gone", true, nil)
	if err != nil || !res.Deleted {
		t.Fatalf("delete: res=%+v err=%v", res, err)
	}
	if res.Undo.SHA != sha {
		t.Errorf("undo sha = %s, want %s", res.Undo.SHA, sha)
	}
	if err := undoBranchDelete(repo, *res.Undo); err != nil {
		t.Fatalf("undo: %v", err)
	}
	if got := gitT(t, repo, "rev-parse", "refs/heads/gone"); got != sha {
		t.Errorf("branch back at %s, want %s (with its only-here work)", got, sha)
	}
	if !dirExists(filepath.Join(folder, "a.txt")) {
		t.Errorf("folder not back")
	}

	wantRefusal(t, undoBranchDelete(repo, *res.Undo), "branch exists again")
	wantRefusal(t, undoBranchDelete(repo, branchUndo{Branch: "x", SHA: "0123456789012345678901234567890123456789"}), "unknown sha")
	wantRefusal(t, undoBranchDelete(repo, branchUndo{Branch: "y", SHA: sha, Folder: "/etc/y"}), "folder outside the folders area")
}

func TestRemoveLeftover(t *testing.T) {
	repo, container := newCheckRepo(t)
	offline(t, repo)
	stray := filepath.Join(container, "stray")
	if err := os.MkdirAll(stray, 0755); err != nil {
		t.Fatal(err)
	}

	res, err := removeLeftover(repo, stray, false, nil)
	if err != nil || !res.NeedsConfirm || res.Undoable || !dirExists(stray) {
		t.Fatalf("unconfirmed: res=%+v err=%v", res, err)
	}
	_, err = removeLeftover(repo, stray, true, map[string]bool{stray: true})
	wantRefusal(t, err, "in use")
	res, err = removeLeftover(repo, stray, true, nil)
	if err != nil || !res.Deleted || dirExists(stray) {
		t.Fatalf("confirmed: res=%+v err=%v", res, err)
	}

	known := filepath.Join(container, "known")
	gitT(t, repo, "worktree", "add", "-q", "-b", "known", known)
	for _, bad := range []string{"/etc", filepath.Join(container, "..", "workspace"), known, repo} {
		_, err := removeLeftover(repo, bad, true, nil)
		wantRefusal(t, err, "not a leftover: "+bad)
	}
	if !dirExists(known) || !dirExists(repo) {
		t.Errorf("a refused folder was removed")
	}
}

func TestSwitchToDefault(t *testing.T) {
	repo, container := newCheckRepo(t)
	offline(t, repo)
	gitT(t, repo, "checkout", "-q", "-b", "cur")

	_, err := switchToDefault(repo, map[string]bool{repo: true})
	wantRefusal(t, err, "in use")

	writeFile(t, filepath.Join(repo, "a.txt"), "edited\n")
	_, err = switchToDefault(repo, nil)
	wantRefusal(t, err, "unsaved edits")
	gitT(t, repo, "checkout", "--", "a.txt")

	// New files a session left behind don't block it; checkout keeps them.
	writeFile(t, filepath.Join(repo, "left-behind.txt"), "x\n")

	other := filepath.Join(container, "m")
	gitT(t, repo, "worktree", "add", "-q", other, "main")
	_, err = switchToDefault(repo, nil)
	wantRefusal(t, err, "default open in another folder")
	gitT(t, repo, "worktree", "remove", other)

	branch, err := switchToDefault(repo, nil)
	if err != nil || branch != "main" {
		t.Fatalf("switch: %q %v", branch, err)
	}
	if got := gitT(t, repo, "symbolic-ref", "--short", "HEAD"); got != "main" {
		t.Errorf("workspace on %s, want main", got)
	}
	if !dirExists(filepath.Join(repo, "left-behind.txt")) {
		t.Errorf("the new file was lost by the switch")
	}
}

// The HTTP wiring: needs-confirm is a 200 with a reason, a refusal is a 409.
func TestBranchDeleteAPI(t *testing.T) {
	repo := newBranchRefreshRepo(t, "")
	gitT(t, repo, "checkout", "-q", "-b", "feat")
	commitOn(t, repo, "one")
	gitT(t, repo, "checkout", "-q", "main")

	post := func(body any) *httptest.ResponseRecorder {
		b, _ := json.Marshal(body)
		rec := httptest.NewRecorder()
		handleBranchDeleteAPI(rec, httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(b)))
		return rec
	}

	rec := post(map[string]any{"path": repo, "branch": "feat"})
	var res branchDeleteResult
	json.Unmarshal(rec.Body.Bytes(), &res)
	if rec.Code != http.StatusOK || !res.NeedsConfirm {
		t.Errorf("unconfirmed: %d %s", rec.Code, rec.Body.String())
	}
	if rec := post(map[string]any{"path": repo, "branch": "main", "confirmed": true}); rec.Code != http.StatusConflict {
		t.Errorf("default branch: %d %s, want 409", rec.Code, rec.Body.String())
	}
	rec = post(map[string]any{"path": repo, "branch": "feat", "confirmed": true})
	res = branchDeleteResult{}
	json.Unmarshal(rec.Body.Bytes(), &res)
	if rec.Code != http.StatusOK || !res.Deleted || res.Undo == nil {
		t.Errorf("confirmed: %d %s", rec.Code, rec.Body.String())
	}
}
