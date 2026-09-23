package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// Staged drafts and the "current PR" pointer used to live in one place per
// machine: two sessions on different branches shared them, so a bare
// `prctx flush` in one session could post the other session's drafts to the
// other session's PR. Both are now kept per local git branch.

// gitRepo makes a throwaway repo with an origin remote, chdirs into it for
// the rest of the test, and returns a function that checks out a branch.
func gitRepo(t *testing.T) func(branch string) {
	t.Helper()
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	dir := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "-q", "-b", "main")
	run("-c", "user.email=t@t", "-c", "user.name=t", "commit", "-q", "--allow-empty", "-m", "init")
	run("remote", "add", "origin", "git@github.com:acme/widgets.git")
	t.Chdir(dir)
	return func(branch string) {
		t.Helper()
		run("checkout", "-q", "-B", branch)
	}
}

func TestDraftsAreKeptPerBranch(t *testing.T) {
	checkout := gitRepo(t)
	ref := PRRef{Host: "github.com", Owner: "acme", Repo: "widgets", Number: 12}

	checkout("feature-a")
	if err := saveState(&State{Ref: ref, Drafts: []Draft{{ID: "d1", Path: "a.go", Line: 1, Body: "from a"}}}); err != nil {
		t.Fatal(err)
	}

	checkout("feature-b")
	if _, err := loadState(ref); err == nil {
		t.Fatal("branch feature-b sees state staged on feature-a")
	}
	if err := saveState(&State{Ref: ref, Drafts: []Draft{{ID: "d1", Path: "b.go", Line: 2, Body: "from b"}}}); err != nil {
		t.Fatal(err)
	}

	checkout("feature-a")
	s, err := loadState(ref)
	if err != nil {
		t.Fatalf("feature-a lost its state: %v", err)
	}
	if len(s.Drafts) != 1 || s.Drafts[0].Body != "from a" {
		t.Errorf("feature-a drafts = %+v, want only its own", s.Drafts)
	}
}

func TestCurrentPRIsKeptPerBranch(t *testing.T) {
	checkout := gitRepo(t)
	checkout("feature-a")
	if err := saveCurrent(PRRef{Host: "github.com", Owner: "acme", Repo: "widgets", Number: 12}); err != nil {
		t.Fatal(err)
	}
	checkout("feature-b")
	if err := saveCurrent(PRRef{Host: "github.com", Owner: "acme", Repo: "widgets", Number: 15}); err != nil {
		t.Fatal(err)
	}
	checkout("feature-a")
	got, err := loadCurrent()
	if err != nil {
		t.Fatal(err)
	}
	if got.Number != 12 {
		t.Errorf("feature-a current PR = %d, want 12 (feature-b's fetch of 15 must not move it)", got.Number)
	}
}

// State saved before this change sits in the old per-machine place. A branch
// picks it up when the saved PR's branch is the one checked out, so drafts
// staged before an upgrade are not lost -- and no other branch inherits them.
func TestLegacyStateCarriesOverToItsOwnBranch(t *testing.T) {
	checkout := gitRepo(t)
	ref := PRRef{Host: "github.com", Owner: "acme", Repo: "widgets", Number: 12}
	legacy := filepath.Join(os.Getenv("XDG_STATE_HOME"), "prctx", "github.com", ref.slug(), "12.json")
	os.MkdirAll(filepath.Dir(legacy), 0o755)
	os.WriteFile(legacy, []byte(`{"ref":{"host":"github.com","owner":"acme","repo":"widgets","number":12},"branch":"feature-a","drafts":[{"id":"d1","path":"a.go","line":1,"body":"old"}]}`), 0o644)

	checkout("feature-b")
	if _, err := loadState(ref); err == nil {
		t.Error("feature-b inherited drafts staged for feature-a's PR branch")
	}
	checkout("feature-a")
	s, err := loadState(ref)
	if err != nil {
		t.Fatalf("feature-a did not pick up its pre-upgrade state: %v", err)
	}
	if len(s.Drafts) != 1 || s.Drafts[0].Body != "old" {
		t.Errorf("drafts = %+v, want the pre-upgrade one", s.Drafts)
	}
}

// Outside a git repo there is no branch to key by; prctx keeps working from
// the old per-machine place rather than refusing.
func TestOutsideGitRepoUsesMachineWideState(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	t.Chdir(t.TempDir())
	ref := PRRef{Host: "github.com", Owner: "acme", Repo: "widgets", Number: 7}
	if err := saveState(&State{Ref: ref}); err != nil {
		t.Fatal(err)
	}
	if _, err := loadState(ref); err != nil {
		t.Fatalf("loadState outside a repo: %v", err)
	}
	if err := saveCurrent(ref); err != nil {
		t.Fatal(err)
	}
	if got, err := loadCurrent(); err != nil || got.Number != 7 {
		t.Fatalf("loadCurrent outside a repo = %+v, %v", got, err)
	}
}
