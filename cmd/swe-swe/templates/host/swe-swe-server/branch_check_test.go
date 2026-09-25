package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// newCheckRepo builds <root>/repo/workspace cloned from <root>/src, with a
// tracked a.txt and a .gitignore that ignores ignored.txt. Returns the repo
// and its worktrees container.
func newCheckRepo(t *testing.T) (repo, container string) {
	t.Helper()
	root := t.TempDir()
	src := filepath.Join(root, "src")
	repo = filepath.Join(root, "repo", "workspace")
	container = filepath.Join(root, "repo", "worktrees")
	if err := os.MkdirAll(src, 0755); err != nil {
		t.Fatal(err)
	}
	gitT(t, src, "init", "-q", "-b", "main")
	writeFile(t, filepath.Join(src, "a.txt"), "a\n")
	writeFile(t, filepath.Join(src, ".gitignore"), "ignored.txt\n")
	gitT(t, src, "add", ".")
	gitT(t, src, "commit", "-q", "-m", "init")
	gitT(t, root, "clone", "-q", src, repo)
	return repo, container
}

func commitOn(t *testing.T, dir, msg string) {
	t.Helper()
	gitT(t, dir, "commit", "-q", "--allow-empty", "-m", msg)
}

func checkOne(t *testing.T, repo, branch string, git branchCheckGit, live map[string]bool) branchCheck {
	t.Helper()
	checks, err := checkBranches(context.Background(), repo, git, live, branch)
	if err != nil {
		t.Fatalf("checkBranches(%s): %v", branch, err)
	}
	if len(checks) != 1 {
		t.Fatalf("checks = %+v, want exactly %s", checks, branch)
	}
	return checks[0]
}

// Check 1: saved work that exists nowhere else.
func TestUnsavedCommitCount(t *testing.T) {
	repo, _ := newCheckRepo(t)

	gitT(t, repo, "checkout", "-q", "-b", "feat")
	commitOn(t, repo, "one")
	commitOn(t, repo, "two")
	gitT(t, repo, "checkout", "-q", "main")

	c := checkOne(t, repo, "feat", defaultBranchCheckGit, nil)
	if !c.CanTell || c.UnsavedCommits != 2 {
		t.Errorf("local-only work: %+v, want 2 unsaved", c)
	}

	gitT(t, repo, "push", "-q", "origin", "feat")
	if c := checkOne(t, repo, "feat", defaultBranchCheckGit, nil); c.UnsavedCommits != 0 {
		t.Errorf("after sending it online: %+v, want 0", c)
	}

	gitT(t, repo, "checkout", "-q", "-b", "merged")
	commitOn(t, repo, "three")
	gitT(t, repo, "checkout", "-q", "main")
	if c := checkOne(t, repo, "merged", defaultBranchCheckGit, nil); c.UnsavedCommits != 1 {
		t.Fatalf("before merge: %+v, want 1", c)
	}
	gitT(t, repo, "merge", "-q", "--no-edit", "merged")
	if c := checkOne(t, repo, "merged", defaultBranchCheckGit, nil); c.UnsavedCommits != 0 {
		t.Errorf("after merging into main: %+v, want 0", c)
	}
}

// Check 2: unsaved edits in the branch's folder; ignored files don't count.
func TestFolderEditCount(t *testing.T) {
	repo, container := newCheckRepo(t)
	folder := filepath.Join(container, "w")
	gitT(t, repo, "worktree", "add", "-q", "-b", "w", folder)

	if c := checkOne(t, repo, "w", defaultBranchCheckGit, nil); !c.CanTell || c.FolderEdits != 0 || c.Folder != folder {
		t.Errorf("clean folder: %+v, want 0 edits in %s", c, folder)
	}
	writeFile(t, filepath.Join(folder, "a.txt"), "changed\n")
	if c := checkOne(t, repo, "w", defaultBranchCheckGit, nil); c.FolderEdits != 1 {
		t.Errorf("changed file: %+v, want 1", c)
	}
	writeFile(t, filepath.Join(folder, "new.txt"), "new\n")
	if c := checkOne(t, repo, "w", defaultBranchCheckGit, nil); c.FolderEdits != 2 {
		t.Errorf("plus a new file: %+v, want 2", c)
	}
	writeFile(t, filepath.Join(folder, "ignored.txt"), "x\n")
	if c := checkOne(t, repo, "w", defaultBranchCheckGit, nil); c.FolderEdits != 2 {
		t.Errorf("plus an ignored file: %+v, want still 2", c)
	}
}

// A single-branch check sizes the folder, ignored files included; checking
// every branch does not (too slow across many folders).
func TestBranchCheckFolderSize(t *testing.T) {
	repo, container := newCheckRepo(t)
	folder := filepath.Join(container, "w")
	gitT(t, repo, "worktree", "add", "-q", "-b", "w", folder)
	c := checkOne(t, repo, "w", defaultBranchCheckGit, nil)
	if c.FolderBytes == nil {
		t.Fatalf("%+v, want a folder size", c)
	}
	before := *c.FolderBytes
	writeFile(t, filepath.Join(folder, "ignored.txt"), strings.Repeat("x", 1000))
	c = checkOne(t, repo, "w", defaultBranchCheckGit, nil)
	if c.FolderBytes == nil || *c.FolderBytes != before+1000 {
		t.Errorf("after 1000 ignored bytes: %+v, want %d", c, before+1000)
	}

	if c := checkOne(t, repo, "main", defaultBranchCheckGit, nil); c.FolderBytes != nil {
		t.Errorf("no folder of its own: %+v, want no size", c)
	}
	all, err := checkBranches(context.Background(), repo, defaultBranchCheckGit, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range all {
		if c.FolderBytes != nil {
			t.Errorf("check-all %s: %+v, want no size", c.Branch, c)
		}
	}
}

// Ending the caller's ctx (the user picked another card) stops the check
// without waiting for branchCheckTimeout.
func TestBranchCheckStopsWhenCallerGivesUp(t *testing.T) {
	repo, container := newCheckRepo(t)
	gitT(t, repo, "worktree", "add", "-q", "-b", "w", filepath.Join(container, "w"))
	stuck := func(ctx context.Context, dir string, args ...string) ([]byte, error) {
		if args[0] == "status" {
			<-ctx.Done()
			return nil, ctx.Err()
		}
		return defaultBranchCheckGit(ctx, dir, args...)
	}
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(50*time.Millisecond, cancel)
	start := time.Now()
	checks, err := checkBranches(ctx, repo, stuck, nil, "w")
	if err != nil {
		t.Fatal(err)
	}
	if checks[0].CanTell {
		t.Errorf("%+v, want can't tell", checks[0])
	}
	if time.Since(start) > branchCheckTimeout/2 {
		t.Errorf("took %v, the caller's cancel did not stop it", time.Since(start))
	}
}

func TestBranchCheckInUse(t *testing.T) {
	repo, container := newCheckRepo(t)
	folder := filepath.Join(container, "w")
	gitT(t, repo, "worktree", "add", "-q", "-b", "w", folder)
	c := checkOne(t, repo, "w", defaultBranchCheckGit, map[string]bool{folder: true})
	if !c.InUse {
		t.Errorf("%+v, want in use", c)
	}
}

// A check that fails or runs past branchCheckTimeout answers "can't tell".
func TestBranchCheckTimeoutCantTell(t *testing.T) {
	repo, container := newCheckRepo(t)
	gitT(t, repo, "worktree", "add", "-q", "-b", "w", filepath.Join(container, "w"))

	old := branchCheckTimeout
	branchCheckTimeout = 50 * time.Millisecond
	t.Cleanup(func() { branchCheckTimeout = old })

	stuck := func(ctx context.Context, dir string, args ...string) ([]byte, error) {
		if args[0] == "status" {
			<-ctx.Done()
			return nil, ctx.Err()
		}
		return defaultBranchCheckGit(ctx, dir, args...)
	}
	start := time.Now()
	c := checkOne(t, repo, "w", stuck, nil)
	if c.CanTell {
		t.Errorf("%+v, want can't tell", c)
	}
	if time.Since(start) > 2*time.Second {
		t.Errorf("took %v, the timeout did not cut it off", time.Since(start))
	}

	failing := func(ctx context.Context, dir string, args ...string) ([]byte, error) {
		if args[0] == "rev-list" {
			return nil, exec.ErrNotFound
		}
		return defaultBranchCheckGit(ctx, dir, args...)
	}
	if c := checkOne(t, repo, "w", failing, nil); c.CanTell {
		t.Errorf("failed check: %+v, want can't tell", c)
	}
}

// Checking all branches runs 4 folders side by side, not one after another.
// The 4 folders: the workspace itself (on main) plus w1..w3.
func TestBranchCheckAllRunsFoldersConcurrently(t *testing.T) {
	repo, container := newCheckRepo(t)
	for _, b := range []string{"w1", "w2", "w3"} {
		gitT(t, repo, "worktree", "add", "-q", "-b", b, filepath.Join(container, b))
	}
	const delay = 300 * time.Millisecond
	slow := func(ctx context.Context, dir string, args ...string) ([]byte, error) {
		if args[0] == "status" {
			time.Sleep(delay)
		}
		return defaultBranchCheckGit(ctx, dir, args...)
	}
	start := time.Now()
	checks, err := checkBranches(context.Background(), repo, slow, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	elapsed := time.Since(start)
	if len(checks) != 4 { // main + w1..w3
		t.Errorf("checks = %d, want 4: %+v", len(checks), checks)
	}
	if elapsed > 2*delay {
		t.Errorf("4 slow folders took %v, want about %v (side by side)", elapsed, delay)
	}
}

func TestBranchCheckAPI(t *testing.T) {
	repo := newBranchRefreshRepo(t, "")
	gitT(t, repo, "checkout", "-q", "-b", "feat")
	commitOn(t, repo, "one")
	gitT(t, repo, "checkout", "-q", "main")

	post := func(h http.HandlerFunc, body any) *httptest.ResponseRecorder {
		b, _ := json.Marshal(body)
		rec := httptest.NewRecorder()
		h(rec, httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(b)))
		return rec
	}

	rec := post(handleBranchCheckAPI, map[string]string{"path": repo, "branch": "feat"})
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	var one branchCheck
	json.Unmarshal(rec.Body.Bytes(), &one)
	if one.Branch != "feat" || one.UnsavedCommits != 1 || !one.CanTell {
		t.Errorf("branch-check = %+v", one)
	}

	if rec := post(handleBranchCheckAPI, map[string]string{"path": repo, "branch": "nope"}); rec.Code != http.StatusNotFound {
		t.Errorf("unknown branch: status %d, want 404", rec.Code)
	}
	if rec := post(handleBranchCheckAPI, map[string]string{"path": "/etc", "branch": "feat"}); rec.Code != http.StatusBadRequest {
		t.Errorf("path outside repos: status %d, want 400", rec.Code)
	}
	if rec := post(handleBranchCheckAPI, map[string]string{"path": filepath.Join(repo, "..", "..", ".."), "branch": "feat"}); rec.Code != http.StatusBadRequest {
		t.Errorf("path with ..: status %d, want 400", rec.Code)
	}

	rec = post(handleBranchCheckAllAPI, map[string]string{"path": repo})
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	var all struct {
		Checks []branchCheck `json:"checks"`
	}
	json.Unmarshal(rec.Body.Bytes(), &all)
	names := []string{}
	for _, c := range all.Checks {
		names = append(names, c.Branch)
	}
	if strings.Join(names, ",") != "feat,main" {
		t.Errorf("branch-check-all branches = %v, want feat,main", names)
	}
}
