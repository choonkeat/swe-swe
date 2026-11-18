// branch_check.go -- the "soon" facts behind the branch cards: what deleting a
// branch would lose (tasks/2026-09-25-branch-cards.md, phase 2).
//
// Two checks per branch:
//   - unsaved commits: commits on the branch that are in neither the default
//     branch nor any remote-tracking ref, i.e. saved work that exists nowhere
//     else. Shown as "N unsaved".
//   - folder edits: `git status --porcelain` lines in the branch's folder.
//     New files count; ignored files don't.
//
// A single-branch check (the card the user picked) also sizes the branch's
// folder, so a slow delete is expected rather than a surprise. The size never
// affects canTell: not knowing it is fine and does not block a delete.
//
// Safety rule: a check that fails or runs past branchCheckTimeout sets
// canTell=false, which the dialog treats as "something would be lost".
package main

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"log"
	"net/http"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// branchCheckTimeout bounds each git call of a check. Only the picked card
// is checked now (the user is watching "Checking..."), so it can afford to
// wait: 5 s cut off `git status` on a 1.4 GB branch folder, which then
// showed "Couldn't check what would be lost". Var so tests can shorten it.
var branchCheckTimeout = 30 * time.Second

// branchCheckParallel caps how many checks run at once for check-all.
const branchCheckParallel = 4

var errBranchNotFound = errors.New("no such branch")

type branchCheck struct {
	Branch         string `json:"branch"`
	Folder         string `json:"folder,omitempty"`
	UnsavedCommits int    `json:"unsavedCommits"`
	FolderEdits    int    `json:"folderEdits"`
	InUse          bool   `json:"inUse,omitempty"`
	CanTell        bool   `json:"canTell"`
	// FolderBytes is set (>= 0) only when the folder was sized in time; nil
	// means no folder, or "can't tell size".
	FolderBytes *int64 `json:"folderBytes,omitempty"`
}

// branchCheckGit runs git in dir; injectable so tests can stall or slow it.
type branchCheckGit func(ctx context.Context, dir string, args ...string) ([]byte, error)

func defaultBranchCheckGit(ctx context.Context, dir string, args ...string) ([]byte, error) {
	return runGit(ctx, gitCall{Dir: dir, Args: args})
}

// checkBranches runs both checks for one local branch (only != "") or for
// every local branch (only == ""), results sorted by branch name. Always
// fresh: nothing is cached between calls. Ending ctx (the user picked another
// card) stops the git calls still running.
func checkBranches(ctx context.Context, repoPath string, git branchCheckGit, live map[string]bool, only string) ([]branchCheck, error) {
	f, err := gatherBranchFacts(repoPath, defaultBranchGit(repoPath), live)
	if err != nil {
		return nil, err
	}
	defaultBranch := buildBranchCards(f).DefaultBranch

	local := map[string]bool{}
	for _, b := range f.Local {
		local[b] = true
	}
	folderOf := map[string]string{}
	for _, wt := range f.Worktrees {
		if wt.Branch != "" {
			folderOf[wt.Branch] = wt.Path
		}
	}

	var targets []string
	if only != "" {
		if !local[only] {
			return nil, errBranchNotFound
		}
		targets = []string{only}
	} else {
		targets = append(targets, f.Local...)
		sort.Strings(targets)
	}

	results := make([]branchCheck, len(targets))
	sem := make(chan struct{}, branchCheckParallel)
	var wg sync.WaitGroup
	for i, branch := range targets {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			c := branchCheck{Branch: branch, Folder: folderOf[branch], CanTell: true}
			c.InUse = c.Folder != "" && live[c.Folder]

			args := []string{"rev-list", "--count", "refs/heads/" + branch, "--not", "--remotes"}
			if branch != defaultBranch && local[defaultBranch] {
				args = append(args, "refs/heads/"+defaultBranch)
			}
			if n, ok := countGit(ctx, git, repoPath, args, func(out string) (int, error) {
				return strconv.Atoi(strings.TrimSpace(out))
			}); ok {
				c.UnsavedCommits = n
			} else {
				c.CanTell = false
			}

			if c.Folder != "" {
				if n, ok := countGit(ctx, git, c.Folder, []string{"status", "--porcelain"}, func(out string) (int, error) {
					return len(strings.FieldsFunc(out, func(r rune) bool { return r == '\n' })), nil
				}); ok {
					c.FolderEdits = n
				} else {
					c.CanTell = false
				}
				// The workspace itself is never deleted with a branch.
				if only != "" && c.Folder != repoPath {
					if n, ok := folderSize(ctx, c.Folder); ok {
						c.FolderBytes = &n
					}
				}
			}
			results[i] = c
		}()
	}
	wg.Wait()
	return results, nil
}

// countGit runs one bounded git call and parses a count from its output.
// ok=false on failure or timeout ("can't tell").
func countGit(parent context.Context, git branchCheckGit, dir string, args []string, parse func(string) (int, error)) (int, bool) {
	ctx, cancel := context.WithTimeout(parent, branchCheckTimeout)
	defer cancel()
	out, err := git(ctx, dir, args...)
	if err != nil {
		log.Printf("Branch check %v in %s failed (can't tell): %v", args[:2], dir, err)
		return 0, false
	}
	n, err := parse(string(out))
	if err != nil {
		return 0, false
	}
	return n, true
}

// folderSize adds up the file sizes under dir, ignored files included (they
// are deleted too), without following symlinks. Bounded by
// branchCheckTimeout; ok=false when it runs out or fails.
func folderSize(parent context.Context, dir string) (int64, bool) {
	ctx, cancel := context.WithTimeout(parent, branchCheckTimeout)
	defer cancel()
	var total int64
	err := filepath.WalkDir(dir, func(_ string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if d.Type().IsRegular() {
			info, err := d.Info()
			if err != nil {
				return err
			}
			total += info.Size()
		}
		return nil
	})
	if err != nil {
		log.Printf("Branch check: sizing %s failed (can't tell size): %v", dir, err)
		return 0, false
	}
	return total, true
}

// branchAPIRepoPath cleans a request's repo path and allows only the default
// workspace or something under reposDir. "" means the default workspace.
// Cleaning first, so "/repos/x/../../etc" is judged as "/etc".
func branchAPIRepoPath(p string) (string, bool) {
	if p == "" {
		p = workspaceDir
	}
	p = filepath.Clean(p)
	if p != workspaceDir && !strings.HasPrefix(p, reposDir+"/") {
		return "", false
	}
	return p, true
}

func writeJSONStatus(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

// decodeBranchCheckRequest parses the shared {path, branch} POST body.
func decodeBranchCheckRequest(w http.ResponseWriter, r *http.Request) (repoPath, branch string, ok bool) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return "", "", false
	}
	var req struct {
		Path   string `json:"path"`
		Branch string `json:"branch"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONStatus(w, http.StatusBadRequest, map[string]string{"error": "Invalid request body"})
		return "", "", false
	}
	repoPath, valid := branchAPIRepoPath(req.Path)
	if !valid {
		writeJSONStatus(w, http.StatusBadRequest, map[string]string{"error": "Invalid repository path"})
		return "", "", false
	}
	return repoPath, req.Branch, true
}

// handleBranchCheckAPI handles POST /api/repo/branch-check {path, branch}:
// both checks plus the folder size for one branch, fresh, each time the user
// picks its card. The request's ctx ends when the dialog drops the request (a
// different card was picked), which stops the check's git calls.
func handleBranchCheckAPI(w http.ResponseWriter, r *http.Request) {
	repoPath, branch, ok := decodeBranchCheckRequest(w, r)
	if !ok {
		return
	}
	if branch == "" {
		writeJSONStatus(w, http.StatusBadRequest, map[string]string{"error": "Missing branch"})
		return
	}
	checks, err := checkBranches(r.Context(), repoPath, defaultBranchCheckGit, liveSessionWorkDirs(), branch)
	if errors.Is(err, errBranchNotFound) {
		writeJSONStatus(w, http.StatusNotFound, map[string]string{"error": "No such branch"})
		return
	}
	if err != nil {
		log.Printf("Branch check failed for %s %s: %v", repoPath, branch, err)
		writeJSONStatus(w, http.StatusInternalServerError, map[string]string{"error": "Failed to check branch"})
		return
	}
	writeJSONStatus(w, http.StatusOK, checks[0])
}

// handleBranchCheckAllAPI handles POST /api/repo/branch-check-all {path}:
// both checks for every local branch. The dialog no longer calls it: on a
// repo with ~90 branches it ran ~180 git commands at once and exhausted the
// box's open files. Kept for older dialogs until the git queue lands.
func handleBranchCheckAllAPI(w http.ResponseWriter, r *http.Request) {
	repoPath, _, ok := decodeBranchCheckRequest(w, r)
	if !ok {
		return
	}
	checks, err := checkBranches(r.Context(), repoPath, defaultBranchCheckGit, liveSessionWorkDirs(), "")
	if err != nil {
		log.Printf("Branch check-all failed for %s: %v", repoPath, err)
		writeJSONStatus(w, http.StatusInternalServerError, map[string]string{"error": "Failed to check branches"})
		return
	}
	writeJSONStatus(w, http.StatusOK, map[string]any{"checks": checks})
}
