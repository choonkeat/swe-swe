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
// Safety rule: a check that fails or runs past branchCheckTimeout sets
// canTell=false, which the dialog treats as "something would be lost".
package main

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// branchCheckTimeout bounds each git call of a check. Var so tests can
// shorten it.
var branchCheckTimeout = 5 * time.Second

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
}

// branchCheckGit runs git in dir; injectable so tests can stall or slow it.
type branchCheckGit func(ctx context.Context, dir string, args ...string) ([]byte, error)

func defaultBranchCheckGit(ctx context.Context, dir string, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, "git", append([]string{"-C", dir}, args...)...).Output()
}

// checkBranches runs both checks for one local branch (only != "") or for
// every local branch (only == ""), results sorted by branch name. Always
// fresh: nothing is cached between calls.
func checkBranches(repoPath string, git branchCheckGit, live map[string]bool, only string) ([]branchCheck, error) {
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
			if n, ok := countGit(git, repoPath, args, func(out string) (int, error) {
				return strconv.Atoi(strings.TrimSpace(out))
			}); ok {
				c.UnsavedCommits = n
			} else {
				c.CanTell = false
			}

			if c.Folder != "" {
				if n, ok := countGit(git, c.Folder, []string{"status", "--porcelain"}, func(out string) (int, error) {
					return len(strings.FieldsFunc(out, func(r rune) bool { return r == '\n' })), nil
				}); ok {
					c.FolderEdits = n
				} else {
					c.CanTell = false
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
func countGit(git branchCheckGit, dir string, args []string, parse func(string) (int, error)) (int, bool) {
	ctx, cancel := context.WithTimeout(context.Background(), branchCheckTimeout)
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
// both checks for one branch, fresh, for each [x] tap.
func handleBranchCheckAPI(w http.ResponseWriter, r *http.Request) {
	repoPath, branch, ok := decodeBranchCheckRequest(w, r)
	if !ok {
		return
	}
	if branch == "" {
		writeJSONStatus(w, http.StatusBadRequest, map[string]string{"error": "Missing branch"})
		return
	}
	checks, err := checkBranches(repoPath, defaultBranchCheckGit, liveSessionWorkDirs(), branch)
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
// both checks for every local branch, to fill the tags after the dialog opens.
func handleBranchCheckAllAPI(w http.ResponseWriter, r *http.Request) {
	repoPath, _, ok := decodeBranchCheckRequest(w, r)
	if !ok {
		return
	}
	checks, err := checkBranches(repoPath, defaultBranchCheckGit, liveSessionWorkDirs(), "")
	if err != nil {
		log.Printf("Branch check-all failed for %s: %v", repoPath, err)
		writeJSONStatus(w, http.StatusInternalServerError, map[string]string{"error": "Failed to check branches"})
		return
	}
	writeJSONStatus(w, http.StatusOK, map[string]any{"checks": checks})
}
