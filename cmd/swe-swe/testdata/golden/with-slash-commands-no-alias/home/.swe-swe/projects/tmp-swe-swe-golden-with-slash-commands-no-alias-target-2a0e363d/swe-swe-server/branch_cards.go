// branch_cards.go -- the "instant facts" behind the New Session dialog's
// branch cards (tasks/2026-09-25-branch-cards.md, phase 1).
//
// Split in two so the rules are testable without git:
//   - gatherBranchFacts: a fixed number of local git calls (never the network),
//     however many branches the repo has. That is what keeps the dialog instant.
//   - buildBranchCards: pure rules, one per row of the decision table in
//     mockups/lo-fi/2026-09/24-new-session-branch-cards.html.
//
// The default branch comes from refs/remotes/origin/HEAD, which the background
// refresh saves (see runBranchFetchOnce). Until it is saved the default is a
// guess (main, else master) and neither of those two gets an [x].
package main

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

// gitWorktree is one entry of `git worktree list --porcelain`. Branch is ""
// for a detached HEAD.
type gitWorktree struct {
	Path   string
	Branch string
}

// branchFacts is everything buildBranchCards needs, gathered up front.
type branchFacts struct {
	RepoPath      string              // the checkout the dialog is on
	Remotes       []string            // `git remote` order
	Local         []string            // refs/heads short names
	Remote        map[string][]string // remote -> branch names on it (no HEAD)
	OriginHead    string              // branch refs/remotes/origin/HEAD points at; "" = not saved
	Current       string              // RepoPath's branch; "" = detached
	Worktrees     []gitWorktree       // git's own list, main checkout first
	FoldersOnDisk []string            // dirs in this repo's worktrees container
	LiveWorkDirs  map[string]bool     // work dirs of live sessions
}

// branchCard is one card in the dialog.
//   - kind "workspace": the checkout as it is; Name is its branch.
//   - kind "local": a branch on this box, other than the workspace's own.
//   - kind "online": a branch only on Remote.
type branchCard struct {
	Kind           string `json:"kind"`
	Name           string `json:"name"`
	Remote         string `json:"remote,omitempty"`
	Folder         string `json:"folder,omitempty"`
	InUse          bool   `json:"inUse,omitempty"`
	OddName        bool   `json:"oddName,omitempty"`
	Default        bool   `json:"default,omitempty"`
	NotDefault     bool   `json:"notDefault,omitempty"`
	Deletable      bool   `json:"deletable"`
	NoDeleteReason string `json:"noDeleteReason,omitempty"`
}

// leftoverFolder is a folder in the worktrees container whose branch is gone
// (Branch set when git still lists the folder under a deleted branch).
type leftoverFolder struct {
	Folder string `json:"folder"`
	Branch string `json:"branch,omitempty"`
	InUse  bool   `json:"inUse,omitempty"`
}

type branchCardsResult struct {
	Cards          []branchCard     `json:"cards"`
	Leftovers      []leftoverFolder `json:"leftovers"`
	Remotes        []string         `json:"remotes"`
	DefaultBranch  string           `json:"defaultBranch"`
	DefaultGuessed bool             `json:"defaultGuessed"`
}

// One line each, shown when a greyed [x] is tapped.
const (
	reasonInUse         = "A live session is using this branch."
	reasonDefault       = "This is the default branch."
	reasonMaybeDefault  = "This may be the default branch."
	reasonMainCheckout  = "This branch is open in the main checkout."
	reasonOnlineOnly    = "This branch is online only; deleting it would affect everyone."
	reasonWorkspaceCard = "This is the workspace itself."
)

// buildBranchCards applies the decision table to facts. No git, no I/O.
func buildBranchCards(f branchFacts) branchCardsResult {
	localSet := map[string]bool{}
	for _, b := range f.Local {
		localSet[b] = true
	}

	res := branchCardsResult{Cards: []branchCard{}, Leftovers: []leftoverFolder{}, Remotes: append([]string{}, f.Remotes...)}
	res.DefaultBranch = f.OriginHead
	if res.DefaultBranch == "" {
		res.DefaultGuessed = true
		res.DefaultBranch = "main"
		if !hasBranchAnywhere(f, "main") && hasBranchAnywhere(f, "master") {
			res.DefaultBranch = "master"
		}
	}

	res.Cards = append(res.Cards, branchCard{
		Kind:           "workspace",
		Name:           f.Current,
		InUse:          f.LiveWorkDirs[f.RepoPath],
		NotDefault:     f.Current != res.DefaultBranch,
		NoDeleteReason: reasonWorkspaceCard,
	})

	// branch -> folder, skipping the checkout the dialog is on.
	folderOf := map[string]string{}
	mainCheckout := ""
	gitPaths := map[string]string{} // folder -> branch, as git lists it
	for i, wt := range f.Worktrees {
		if i == 0 {
			mainCheckout = wt.Path
		}
		gitPaths[wt.Path] = wt.Branch
		if wt.Path != f.RepoPath && wt.Branch != "" {
			folderOf[wt.Branch] = wt.Path
		}
	}

	local := append([]string(nil), f.Local...)
	sort.Strings(local)
	for _, name := range local {
		if name == f.Current {
			continue // shown as the workspace card
		}
		c := branchCard{
			Kind:    "local",
			Name:    name,
			Folder:  folderOf[name],
			OddName: hasRemotePrefix(name, f.Remotes),
			Default: name == res.DefaultBranch,
		}
		c.InUse = c.Folder != "" && f.LiveWorkDirs[c.Folder]
		switch {
		case c.Default:
			c.NoDeleteReason = reasonDefault
		case res.DefaultGuessed && (name == "main" || name == "master"):
			c.NoDeleteReason = reasonMaybeDefault
		case c.Folder != "" && c.Folder == mainCheckout:
			c.NoDeleteReason = reasonMainCheckout
		case c.InUse:
			c.NoDeleteReason = reasonInUse
		default:
			c.Deletable = true
		}
		res.Cards = append(res.Cards, c)
	}

	for _, remote := range f.Remotes {
		names := append([]string(nil), f.Remote[remote]...)
		sort.Strings(names)
		for _, name := range names {
			if localSet[name] {
				continue
			}
			res.Cards = append(res.Cards, branchCard{
				Kind:           "online",
				Name:           name,
				Remote:         remote,
				NoDeleteReason: reasonOnlineOnly,
			})
		}
	}

	folders := append([]string(nil), f.FoldersOnDisk...)
	sort.Strings(folders)
	for _, dir := range folders {
		branch, known := gitPaths[dir]
		switch {
		case !known:
			res.Leftovers = append(res.Leftovers, leftoverFolder{Folder: dir, InUse: f.LiveWorkDirs[dir]})
		case branch != "" && !localSet[branch]:
			res.Leftovers = append(res.Leftovers, leftoverFolder{Folder: dir, Branch: branch, InUse: f.LiveWorkDirs[dir]})
		}
	}
	return res
}

func hasBranchAnywhere(f branchFacts, name string) bool {
	for _, b := range f.Local {
		if b == name {
			return true
		}
	}
	for _, names := range f.Remote {
		for _, b := range names {
			if b == name {
				return true
			}
		}
	}
	return false
}

// hasRemotePrefix reports an "odd name": a local branch named like a
// remote-tracking ref ("origin/typo"), usually a slip when creating it.
func hasRemotePrefix(name string, remotes []string) bool {
	for _, r := range remotes {
		if strings.HasPrefix(name, r+"/") {
			return true
		}
	}
	return false
}

// branchGit runs one git command against a repo; injectable so tests can
// count calls.
type branchGit func(args ...string) ([]byte, error)

func defaultBranchGit(repoPath string) branchGit {
	return func(args ...string) ([]byte, error) {
		return exec.Command("git", append([]string{"-C", repoPath}, args...)...).Output()
	}
}

// gatherBranchFacts reads everything buildBranchCards needs with four local
// git calls, independent of branch count. Never contacts a remote.
func gatherBranchFacts(repoPath string, git branchGit, liveWorkDirs map[string]bool) (branchFacts, error) {
	f := branchFacts{RepoPath: repoPath, Remote: map[string][]string{}, LiveWorkDirs: liveWorkDirs}

	out, err := git("remote")
	if err != nil {
		return f, err
	}
	f.Remotes = strings.Fields(string(out))
	// Longest remote name first, so "up/stream" wins over "up" when both exist.
	byLength := append([]string(nil), f.Remotes...)
	sort.SliceStable(byLength, func(i, j int) bool { return len(byLength[i]) > len(byLength[j]) })

	out, err = git("for-each-ref", "--format=%(refname)%00%(symref)", "refs/heads", "refs/remotes")
	if err != nil {
		return f, err
	}
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		ref, symref, _ := strings.Cut(line, "\x00")
		if name, ok := strings.CutPrefix(ref, "refs/heads/"); ok {
			f.Local = append(f.Local, name)
			continue
		}
		rest, ok := strings.CutPrefix(ref, "refs/remotes/")
		if !ok {
			continue
		}
		for _, remote := range byLength {
			name, ok := strings.CutPrefix(rest, remote+"/")
			if !ok {
				continue
			}
			if name == "HEAD" {
				if remote == "origin" {
					f.OriginHead = strings.TrimPrefix(symref, "refs/remotes/origin/")
				}
			} else {
				f.Remote[remote] = append(f.Remote[remote], name)
			}
			break
		}
	}

	out, err = git("worktree", "list", "--porcelain")
	if err != nil {
		return f, err
	}
	f.Worktrees = parseWorktreePorcelain(string(out))

	// Exit status 1 = detached HEAD, which is a valid state, not an error.
	out, err = git("symbolic-ref", "-q", "--short", "HEAD")
	var exitErr *exec.ExitError
	if err != nil && !errors.As(err, &exitErr) {
		return f, err
	}
	f.Current = strings.TrimSpace(string(out))

	container := filepath.Dir(resolveWorkingDirectory(repoPath, "x"))
	if entries, err := os.ReadDir(container); err == nil {
		for _, e := range entries {
			if e.IsDir() {
				f.FoldersOnDisk = append(f.FoldersOnDisk, filepath.Join(container, e.Name()))
			}
		}
	}
	return f, nil
}

// parseWorktreePorcelain parses `git worktree list --porcelain`.
func parseWorktreePorcelain(out string) []gitWorktree {
	var list []gitWorktree
	for _, block := range strings.Split(strings.TrimSpace(out), "\n\n") {
		var wt gitWorktree
		for _, line := range strings.Split(block, "\n") {
			if p, ok := strings.CutPrefix(line, "worktree "); ok {
				wt.Path = filepath.Clean(p)
			} else if b, ok := strings.CutPrefix(line, "branch refs/heads/"); ok {
				wt.Branch = b
			}
		}
		if wt.Path != "" {
			list = append(list, wt)
		}
	}
	return list
}

// liveSessionWorkDirs returns the work dirs of every live session. A session
// with no WorkDir runs in the default workspace.
func liveSessionWorkDirs() map[string]bool {
	dirs := map[string]bool{}
	sessionsMu.RLock()
	defer sessionsMu.RUnlock()
	for _, sess := range sessions {
		dir := sess.WorkDir
		if dir == "" {
			dir = workspaceDir
		}
		dirs[filepath.Clean(dir)] = true
	}
	return dirs
}

// branchCardsFor gathers and builds the cards for repoPath.
func branchCardsFor(repoPath string) (branchCardsResult, error) {
	f, err := gatherBranchFacts(repoPath, defaultBranchGit(repoPath), liveSessionWorkDirs())
	if err != nil {
		return branchCardsResult{}, err
	}
	return buildBranchCards(f), nil
}
