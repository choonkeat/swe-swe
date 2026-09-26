package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// findCard returns the card of the given kind/remote/name, or nil.
func findCard(res branchCardsResult, kind, remote, name string) *branchCard {
	for i := range res.Cards {
		c := &res.Cards[i]
		if c.Kind == kind && c.Remote == remote && c.Name == name {
			return c
		}
	}
	return nil
}

func mustCard(t *testing.T, res branchCardsResult, kind, remote, name string) *branchCard {
	t.Helper()
	c := findCard(res, kind, remote, name)
	if c == nil {
		t.Fatalf("no %s card %q (remote %q); cards: %+v", kind, name, remote, res.Cards)
	}
	return c
}

// baseFacts is a repo at /r/workspace on main, with origin/HEAD -> main.
func baseFacts() branchFacts {
	return branchFacts{
		RepoPath:   "/r/workspace",
		Remotes:    []string{"origin"},
		Local:      []string{"main"},
		Remote:     map[string][]string{"origin": {"main"}},
		OriginHead: "main",
		Current:    "main",
		Worktrees:  []gitWorktree{{Path: "/r/workspace", Branch: "main"}},
	}
}

// One test per row of the decision table in
// mockups/lo-fi/2026-09/24-new-session-branch-cards.html.
func TestBuildBranchCards(t *testing.T) {
	t.Run("plain branch: card with x", func(t *testing.T) {
		f := baseFacts()
		f.Local = append(f.Local, "feat")
		res := buildBranchCards(f)
		c := mustCard(t, res, "local", "", "feat")
		if !c.Deletable || c.Folder != "" || c.InUse || c.OddName || c.Default {
			t.Errorf("plain card = %+v", *c)
		}
	})

	t.Run("has folder: card carries the folder, still has x", func(t *testing.T) {
		f := baseFacts()
		f.Local = append(f.Local, "feat")
		f.Worktrees = append(f.Worktrees, gitWorktree{Path: "/r/worktrees/feat", Branch: "feat"})
		f.FoldersOnDisk = []string{"/r/worktrees/feat"}
		res := buildBranchCards(f)
		c := mustCard(t, res, "local", "", "feat")
		if c.Folder != "/r/worktrees/feat" || !c.Deletable {
			t.Errorf("has-folder card = %+v", *c)
		}
		if len(res.Leftovers) != 0 {
			t.Errorf("a folder with its branch is not a leftover: %+v", res.Leftovers)
		}
	})

	t.Run("in use: no x, one-line reason", func(t *testing.T) {
		f := baseFacts()
		f.Local = append(f.Local, "feat")
		f.Worktrees = append(f.Worktrees, gitWorktree{Path: "/r/worktrees/feat", Branch: "feat"})
		f.LiveWorkDirs = map[string]bool{"/r/worktrees/feat": true}
		res := buildBranchCards(f)
		c := mustCard(t, res, "local", "", "feat")
		if !c.InUse || c.Deletable || c.NoDeleteReason == "" {
			t.Errorf("in-use card = %+v", *c)
		}
	})

	t.Run("odd name: starts with any remote name plus slash", func(t *testing.T) {
		f := baseFacts()
		f.Remotes = []string{"origin", "upstream"}
		f.Local = append(f.Local, "origin/typo", "upstream/x", "originals")
		res := buildBranchCards(f)
		for _, name := range []string{"origin/typo", "upstream/x"} {
			if c := mustCard(t, res, "local", "", name); !c.OddName || !c.Deletable {
				t.Errorf("%s = %+v, want odd name and deletable", name, *c)
			}
		}
		if c := mustCard(t, res, "local", "", "originals"); c.OddName {
			t.Errorf("originals is not an odd name: %+v", *c)
		}
	})

	t.Run("online only: one group per remote, no x", func(t *testing.T) {
		f := baseFacts()
		f.Remotes = []string{"origin", "upstream"}
		f.Remote = map[string][]string{
			"origin":   {"main", "a", "shared"},
			"upstream": {"b", "shared"},
		}
		res := buildBranchCards(f)
		for _, rc := range [][2]string{{"origin", "a"}, {"origin", "shared"}, {"upstream", "b"}, {"upstream", "shared"}} {
			c := mustCard(t, res, "online", rc[0], rc[1])
			if c.Deletable || c.NoDeleteReason == "" {
				t.Errorf("online card %v = %+v, want no x with a reason", rc, *c)
			}
		}
		if findCard(res, "online", "origin", "main") != nil {
			t.Errorf("main exists on this box, so it has no online card")
		}
	})

	t.Run("workspace card: shows its branch, which gets no card of its own", func(t *testing.T) {
		f := baseFacts()
		f.Local = append(f.Local, "feat")
		f.Current = "feat"
		res := buildBranchCards(f)
		w := mustCard(t, res, "workspace", "", "feat")
		if w.Deletable || !w.NotDefault {
			t.Errorf("workspace card = %+v, want no x and not-default", *w)
		}
		if findCard(res, "local", "", "feat") != nil {
			t.Errorf("the workspace's own branch must not get its own card")
		}
		m := mustCard(t, res, "local", "", "main")
		if !m.Default || m.Deletable {
			t.Errorf("main = %+v, want default and no x", *m)
		}
	})

	t.Run("workspace card on the default branch is not flagged", func(t *testing.T) {
		res := buildBranchCards(baseFacts())
		if w := mustCard(t, res, "workspace", "", "main"); w.NotDefault {
			t.Errorf("workspace on main = %+v", *w)
		}
	})

	t.Run("workspace in use by a live session", func(t *testing.T) {
		f := baseFacts()
		f.LiveWorkDirs = map[string]bool{"/r/workspace": true}
		if w := mustCard(t, buildBranchCards(f), "workspace", "", "main"); !w.InUse {
			t.Errorf("workspace = %+v, want in use", *w)
		}
	})

	t.Run("default saved on this box: only that branch loses its x", func(t *testing.T) {
		f := baseFacts()
		f.OriginHead = "trunk"
		f.Local = []string{"trunk", "main", "master"}
		f.Current = "trunk"
		res := buildBranchCards(f)
		if res.DefaultBranch != "trunk" || res.DefaultGuessed {
			t.Errorf("default = %q guessed=%v, want trunk, not guessed", res.DefaultBranch, res.DefaultGuessed)
		}
		for _, name := range []string{"main", "master"} {
			if c := mustCard(t, res, "local", "", name); !c.Deletable || c.Default {
				t.Errorf("%s = %+v, want deletable when the default is known to be trunk", name, *c)
			}
		}
	})

	t.Run("default not saved yet: guess main, no x on main or master", func(t *testing.T) {
		f := baseFacts()
		f.OriginHead = ""
		f.Local = []string{"main", "master", "x"}
		f.Current = "x"
		res := buildBranchCards(f)
		if res.DefaultBranch != "main" || !res.DefaultGuessed {
			t.Errorf("default = %q guessed=%v, want main, guessed", res.DefaultBranch, res.DefaultGuessed)
		}
		for _, name := range []string{"main", "master"} {
			if c := mustCard(t, res, "local", "", name); c.Deletable || c.NoDeleteReason == "" {
				t.Errorf("%s = %+v, want no x while the default is a guess", name, *c)
			}
		}
	})

	t.Run("default not saved, only master exists: guess master", func(t *testing.T) {
		f := baseFacts()
		f.OriginHead = ""
		f.Local = []string{"master"}
		f.Remote = map[string][]string{}
		f.Current = "master"
		res := buildBranchCards(f)
		if res.DefaultBranch != "master" || !res.DefaultGuessed {
			t.Errorf("default = %q guessed=%v, want master, guessed", res.DefaultBranch, res.DefaultGuessed)
		}
	})

	t.Run("leftover folder: on disk, git no longer knows it", func(t *testing.T) {
		f := baseFacts()
		f.Local = append(f.Local, "feat")
		f.Worktrees = append(f.Worktrees,
			gitWorktree{Path: "/r/worktrees/feat", Branch: "feat"},
			gitWorktree{Path: "/r/worktrees/ghost", Branch: "ghost"}, // branch ref gone
		)
		f.FoldersOnDisk = []string{"/r/worktrees/feat", "/r/worktrees/gone", "/r/worktrees/ghost"}
		f.LiveWorkDirs = map[string]bool{"/r/worktrees/gone": true}
		res := buildBranchCards(f)
		if len(res.Leftovers) != 2 {
			t.Fatalf("leftovers = %+v, want gone and ghost", res.Leftovers)
		}
		byFolder := map[string]leftoverFolder{}
		for _, l := range res.Leftovers {
			byFolder[l.Folder] = l
		}
		if l, ok := byFolder["/r/worktrees/gone"]; !ok || !l.InUse || l.Branch != "" {
			t.Errorf("gone = %+v", l)
		}
		if l, ok := byFolder["/r/worktrees/ghost"]; !ok || l.Branch != "ghost" {
			t.Errorf("ghost = %+v", l)
		}
		if findCard(res, "local", "", "ghost") != nil {
			t.Errorf("a missing branch gets no card")
		}
	})

	t.Run("branch open in the main checkout: no x", func(t *testing.T) {
		f := baseFacts()
		f.RepoPath = "/r/worktrees/feat" // dialog opened on a linked worktree
		f.Local = append(f.Local, "feat")
		f.Current = "feat"
		f.Worktrees = append(f.Worktrees, gitWorktree{Path: "/r/worktrees/feat", Branch: "feat"})
		res := buildBranchCards(f)
		if c := mustCard(t, res, "local", "", "main"); c.Deletable {
			t.Errorf("main (open in the main checkout) = %+v, want no x", *c)
		}
	})
}

// newCardsRepo builds a real repo with n extra local branches and an origin.
func newCardsRepo(t *testing.T, n int) string {
	t.Helper()
	root := t.TempDir()
	src := filepath.Join(root, "src")
	repo := filepath.Join(root, "repo", "workspace")
	if err := os.MkdirAll(src, 0755); err != nil {
		t.Fatal(err)
	}
	gitT(t, src, "init", "-q", "-b", "main")
	gitT(t, src, "commit", "-q", "--allow-empty", "-m", "init")
	gitT(t, root, "clone", "-q", src, repo)
	for i := 0; i < n; i++ {
		gitT(t, repo, "branch", "b"+strconv.Itoa(i))
	}
	return repo
}

func gitT(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

// Instant means a fixed number of git calls however many branches there are.
func TestGatherBranchFactsFixedGitCalls(t *testing.T) {
	count := func(n int) int {
		repo := newCardsRepo(t, n)
		calls := 0
		runner := func(args ...string) ([]byte, error) {
			calls++
			return defaultBranchGit(repo)(args...)
		}
		f, err := gatherBranchFacts(repo, runner, nil)
		if err != nil {
			t.Fatalf("gather: %v", err)
		}
		if len(f.Local) != n+1 {
			t.Fatalf("local branches = %d, want %d", len(f.Local), n+1)
		}
		return calls
	}
	few, many := count(3), count(30)
	if few != many {
		t.Errorf("git calls: 3 branches = %d, 30 branches = %d; want the same", few, many)
	}
	if few > 6 {
		t.Errorf("git calls = %d, want at most 6", few)
	}
}

// Real repo: folders, a leftover, an online-only branch and the saved default
// all come through gatherBranchFacts into the cards.
func TestGatherBranchFactsRealRepo(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "src")
	repo := filepath.Join(root, "repo", "workspace")
	container := filepath.Join(root, "repo", "worktrees")
	if err := os.MkdirAll(src, 0755); err != nil {
		t.Fatal(err)
	}
	gitT(t, src, "init", "-q", "-b", "trunk")
	gitT(t, src, "commit", "-q", "--allow-empty", "-m", "init")
	gitT(t, src, "branch", "online-only")
	gitT(t, root, "clone", "-q", src, repo)
	gitT(t, repo, "branch", "feat")
	gitT(t, repo, "worktree", "add", "-q", filepath.Join(container, "feat"), "feat")
	if err := os.MkdirAll(filepath.Join(container, "stray"), 0755); err != nil {
		t.Fatal(err)
	}

	f, err := gatherBranchFacts(repo, defaultBranchGit(repo), nil)
	if err != nil {
		t.Fatalf("gather: %v", err)
	}
	res := buildBranchCards(f)

	if res.DefaultBranch != "trunk" || res.DefaultGuessed {
		t.Errorf("default = %q guessed=%v, want trunk from origin/HEAD", res.DefaultBranch, res.DefaultGuessed)
	}
	mustCard(t, res, "workspace", "", "trunk")
	if c := mustCard(t, res, "local", "", "feat"); c.Folder != filepath.Join(container, "feat") {
		t.Errorf("feat folder = %q", c.Folder)
	}
	mustCard(t, res, "online", "origin", "online-only")
	if len(res.Leftovers) != 1 || res.Leftovers[0].Folder != filepath.Join(container, "stray") {
		t.Errorf("leftovers = %+v, want only stray", res.Leftovers)
	}
}

// The background refresh saves the remote's default branch, so the next
// (offline) listing knows it instead of guessing.
func TestBranchRefreshSavesDefaultBranch(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "src")
	repo := filepath.Join(root, "repo", "workspace")
	if err := os.MkdirAll(src, 0755); err != nil {
		t.Fatal(err)
	}
	gitT(t, src, "init", "-q", "-b", "trunk")
	gitT(t, src, "commit", "-q", "--allow-empty", "-m", "init")
	gitT(t, root, "clone", "-q", src, repo)
	gitT(t, repo, "remote", "set-head", "origin", "-d")

	f, err := gatherBranchFacts(repo, defaultBranchGit(repo), nil)
	if err != nil {
		t.Fatal(err)
	}
	if res := buildBranchCards(f); !res.DefaultGuessed {
		t.Fatalf("before refresh: default should be a guess, got %q", res.DefaultBranch)
	}

	if out, err := runBranchFetch(repo, "", "", "", ""); err != nil {
		t.Fatalf("refresh: %v\n%s", err, out)
	}

	f, err = gatherBranchFacts(repo, defaultBranchGit(repo), nil)
	if err != nil {
		t.Fatal(err)
	}
	if res := buildBranchCards(f); res.DefaultBranch != "trunk" || res.DefaultGuessed {
		t.Errorf("after refresh: default = %q guessed=%v, want trunk, saved", res.DefaultBranch, res.DefaultGuessed)
	}
}

// The branches API keeps its old "branches" list and adds the cards.
func TestRepoBranchesAPIReturnsCards(t *testing.T) {
	repo := newBranchRefreshRepo(t, "")
	gitT(t, repo, "branch", "feat")

	req := httptest.NewRequest(http.MethodGet, "/api/repo/branches?path="+repo, nil)
	rec := httptest.NewRecorder()
	handleRepoBranchesAPI(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Branches       []string         `json:"branches"`
		Cards          []branchCard     `json:"cards"`
		Leftovers      []leftoverFolder `json:"leftovers"`
		Remotes        []string         `json:"remotes"`
		DefaultBranch  string           `json:"defaultBranch"`
		DefaultGuessed bool             `json:"defaultGuessed"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if strings.Join(resp.Branches, ",") != "feat,main" {
		t.Errorf("branches = %v, want the old list unchanged", resp.Branches)
	}
	res := branchCardsResult{Cards: resp.Cards}
	mustCard(t, res, "workspace", "", "main")
	mustCard(t, res, "local", "", "feat")
	if resp.Remotes == nil {
		t.Errorf("remotes should be an empty list, not missing")
	}
	if resp.Leftovers == nil {
		t.Errorf("leftovers should be an empty list, not missing")
	}
	if resp.DefaultBranch != "main" || !resp.DefaultGuessed {
		t.Errorf("default = %q guessed=%v, want main guessed (no remote)", resp.DefaultBranch, resp.DefaultGuessed)
	}
}
