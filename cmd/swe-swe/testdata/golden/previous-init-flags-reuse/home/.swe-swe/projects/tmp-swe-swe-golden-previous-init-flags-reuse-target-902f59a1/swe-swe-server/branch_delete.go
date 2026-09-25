// branch_delete.go -- the branch cards' write side: delete a branch, undo it,
// remove a leftover folder, switch the workspace back to the default branch
// (tasks/2026-09-25-branch-cards.md, phase 3).
//
// Rules this file keeps:
//   - Nothing here ever contacts a remote. Deletes are local only.
//   - A delete re-runs the phase 2 checks itself; it never trusts what the
//     dialog showed when it opened.
//   - Safe order: the folder goes first, then the branch. If the folder can't
//     be removed, the branch stays.
//   - Something to lose (or "can't tell") needs confirmed=true.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

// branchRefusal is a request the server says no to on purpose; the message is
// the one line the dialog shows. Handlers answer it with 409.
type branchRefusal string

func (r branchRefusal) Error() string { return string(r) }

// branchUndo is what the dialog sends back to undo a delete.
type branchUndo struct {
	Branch string `json:"branch"`
	SHA    string `json:"sha"`
	Folder string `json:"folder,omitempty"`
}

type branchDeleteResult struct {
	Deleted      bool        `json:"deleted,omitempty"`
	NeedsConfirm bool        `json:"needsConfirm,omitempty"`
	Reason       string      `json:"reason,omitempty"`
	Undoable     bool        `json:"undoable"`
	Undo         *branchUndo `json:"undo,omitempty"`
}

var shaPattern = regexp.MustCompile(`^[0-9a-f]{40}([0-9a-f]{24})?$`)

func gitIn(dir string, args ...string) ([]byte, error) {
	return exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput()
}

// validateBranchName refuses names git would reject or read as an option.
func validateBranchName(repo, name string) error {
	if name == "" || strings.HasPrefix(name, "-") {
		return branchRefusal("That is not a valid branch name.")
	}
	if _, err := gitIn(repo, "check-ref-format", "--branch", name); err != nil {
		return branchRefusal("That is not a valid branch name.")
	}
	return nil
}

// worktreesContainer is the folder that holds repo's branch folders.
func worktreesContainer(repo string) string {
	return filepath.Dir(resolveWorkingDirectory(repo, "x"))
}

// deleteBranch deletes a local branch and its folder, in that safe order.
func deleteBranch(repo, branch string, confirmed bool, live map[string]bool) (branchDeleteResult, error) {
	if err := validateBranchName(repo, branch); err != nil {
		return branchDeleteResult{}, err
	}
	f, err := gatherBranchFacts(repo, defaultBranchGit(repo), live)
	if err != nil {
		return branchDeleteResult{}, err
	}
	// A name can have a local card and online cards; only the local one counts.
	var card *branchCard
	onlineOnly := false
	for _, c := range buildBranchCards(f).Cards {
		if c.Name != branch {
			continue
		}
		switch c.Kind {
		case "workspace":
			return branchDeleteResult{}, branchRefusal("This is the branch the workspace is on.")
		case "local":
			card = &c
		case "online":
			onlineOnly = true
		}
	}
	if card == nil {
		if onlineOnly {
			return branchDeleteResult{}, branchRefusal(reasonOnlineOnly)
		}
		return branchDeleteResult{}, errBranchNotFound
	}
	if !card.Deletable {
		return branchDeleteResult{}, branchRefusal(card.NoDeleteReason)
	}

	checks, err := checkBranches(repo, defaultBranchCheckGit, live, branch)
	if err != nil {
		return branchDeleteResult{}, err
	}
	check := checks[0]
	if check.InUse {
		return branchDeleteResult{}, branchRefusal(reasonInUse)
	}

	out := branchDeleteResult{Undoable: !(check.Folder != "" && (check.FolderEdits > 0 || !check.CanTell))}
	var reasons []string
	switch {
	case !check.CanTell:
		reasons = append(reasons, "Couldn't check what would be lost.")
	default:
		if check.UnsavedCommits > 0 {
			reasons = append(reasons, fmt.Sprintf("%d saved change(s) exist only on this branch.", check.UnsavedCommits))
		}
		if check.FolderEdits > 0 {
			reasons = append(reasons, fmt.Sprintf("%d unsaved edit(s) in its folder will be lost and can't be undone.", check.FolderEdits))
		}
	}
	if len(reasons) > 0 && !confirmed {
		out.NeedsConfirm = true
		out.Reason = strings.Join(reasons, " ")
		return out, nil
	}

	shaOut, err := gitIn(repo, "rev-parse", "--verify", "refs/heads/"+branch)
	if err != nil {
		return branchDeleteResult{}, fmt.Errorf("rev-parse %s: %v: %s", branch, err, shaOut)
	}
	out.Undo = &branchUndo{Branch: branch, SHA: strings.TrimSpace(string(shaOut)), Folder: check.Folder}

	if check.Folder != "" {
		args := []string{"worktree", "remove"}
		if confirmed {
			args = append(args, "--force")
		}
		if o, err := gitIn(repo, append(args, check.Folder)...); err != nil {
			return branchDeleteResult{}, fmt.Errorf("removing folder %s (branch kept): %v: %s", check.Folder, err, o)
		}
	}
	if o, err := gitIn(repo, "branch", "-D", branch); err != nil {
		return branchDeleteResult{}, fmt.Errorf("deleting branch %s: %v: %s", branch, err, o)
	}
	log.Printf("Branch card delete: %s in %s (sha %s, folder %q)", branch, repo, out.Undo.SHA, check.Folder)
	out.Deleted = true
	return out, nil
}

// undoBranchDelete brings a deleted branch back at its saved point, then its
// folder if it had one.
func undoBranchDelete(repo string, u branchUndo) error {
	if err := validateBranchName(repo, u.Branch); err != nil {
		return err
	}
	if !shaPattern.MatchString(u.SHA) {
		return branchRefusal("That saved point is not valid.")
	}
	if u.Folder != "" {
		folder := filepath.Clean(u.Folder)
		if filepath.Dir(folder) != worktreesContainer(repo) {
			return branchRefusal("That folder is outside this repo's folders area.")
		}
		if _, err := os.Stat(folder); err == nil {
			return branchRefusal("A folder with that name exists again.")
		}
		u.Folder = folder
	}
	if _, err := gitIn(repo, "rev-parse", "--verify", "--quiet", "refs/heads/"+u.Branch); err == nil {
		return branchRefusal("A branch with that name exists again.")
	}
	if _, err := gitIn(repo, "cat-file", "-e", u.SHA+"^{commit}"); err != nil {
		return branchRefusal("That saved point is no longer on this box.")
	}
	if o, err := gitIn(repo, "branch", u.Branch, u.SHA); err != nil {
		return fmt.Errorf("restoring branch %s: %v: %s", u.Branch, err, o)
	}
	if u.Folder != "" {
		if o, err := gitIn(repo, "worktree", "add", u.Folder, u.Branch); err != nil {
			return fmt.Errorf("restoring folder %s (branch restored): %v: %s", u.Folder, err, o)
		}
	}
	log.Printf("Branch card undo: %s in %s at %s (folder %q)", u.Branch, repo, u.SHA, u.Folder)
	return nil
}

// removeLeftover removes a folder in the folders area whose branch is gone.
// Always needs confirmed; there is no undo.
func removeLeftover(repo, folder string, confirmed bool, live map[string]bool) (branchDeleteResult, error) {
	folder = filepath.Clean(folder)
	if filepath.Dir(folder) != worktreesContainer(repo) {
		return branchDeleteResult{}, branchRefusal("That folder is outside this repo's folders area.")
	}
	f, err := gatherBranchFacts(repo, defaultBranchGit(repo), live)
	if err != nil {
		return branchDeleteResult{}, err
	}
	var leftover *leftoverFolder
	for _, l := range buildBranchCards(f).Leftovers {
		if l.Folder == folder {
			leftover = &l
			break
		}
	}
	if leftover == nil {
		return branchDeleteResult{}, branchRefusal("That folder still belongs to a branch.")
	}
	if leftover.InUse {
		return branchDeleteResult{}, branchRefusal("A live session is using this folder.")
	}
	if !confirmed {
		return branchDeleteResult{NeedsConfirm: true, Reason: "Its files will be deleted and can't be undone."}, nil
	}
	if err := os.RemoveAll(folder); err != nil {
		return branchDeleteResult{}, fmt.Errorf("removing leftover %s: %v", folder, err)
	}
	if o, err := gitIn(repo, "worktree", "prune"); err != nil {
		log.Printf("git worktree prune after removing %s failed: %v: %s", folder, err, o)
	}
	log.Printf("Branch card leftover removed: %s", folder)
	return branchDeleteResult{Deleted: true}, nil
}

// switchToDefault checks out the default branch in the workspace. Returns the
// branch it switched to.
func switchToDefault(repo string, live map[string]bool) (string, error) {
	f, err := gatherBranchFacts(repo, defaultBranchGit(repo), live)
	if err != nil {
		return "", err
	}
	def := buildBranchCards(f).DefaultBranch
	if f.Current == def {
		return def, nil
	}
	if live[repo] {
		return "", branchRefusal("A live session is using the workspace.")
	}
	for _, wt := range f.Worktrees {
		if wt.Branch == def && wt.Path != repo {
			return "", branchRefusal(def + " is open in another folder: " + wt.Path)
		}
	}
	if n, ok := countGit(defaultBranchCheckGit, repo, []string{"status", "--porcelain"}, func(out string) (int, error) {
		return len(strings.FieldsFunc(out, func(r rune) bool { return r == '\n' })), nil
	}); !ok || n > 0 {
		return "", branchRefusal("The workspace has unsaved edits.")
	}
	ctx, cancel := context.WithTimeout(context.Background(), branchCheckTimeout)
	defer cancel()
	// Local only: when the default exists only as origin's copy, git makes a
	// local branch from the copy already on this box; it does not fetch.
	args := []string{"-C", repo, "checkout", "-q", def}
	if o, err := exec.CommandContext(ctx, "git", args...).CombinedOutput(); err != nil {
		return "", fmt.Errorf("checkout %s: %v: %s", def, err, o)
	}
	log.Printf("Branch card switch-back: %s now on %s", repo, def)
	return def, nil
}

// writeBranchWriteError maps a delete/undo/leftover/switch error to HTTP.
func writeBranchWriteError(w http.ResponseWriter, what string, err error) {
	var refusal branchRefusal
	switch {
	case errors.As(err, &refusal):
		writeJSONStatus(w, http.StatusConflict, map[string]string{"error": string(refusal)})
	case errors.Is(err, errBranchNotFound):
		writeJSONStatus(w, http.StatusNotFound, map[string]string{"error": "No such branch"})
	default:
		log.Printf("Branch card %s failed: %v", what, err)
		writeJSONStatus(w, http.StatusInternalServerError, map[string]string{"error": "Failed to " + what})
	}
}

// decodeBranchWrite parses a POST body into v and validates its path.
func decodeBranchWrite(w http.ResponseWriter, r *http.Request, v any, path *string) bool {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return false
	}
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		writeJSONStatus(w, http.StatusBadRequest, map[string]string{"error": "Invalid request body"})
		return false
	}
	p, ok := branchAPIRepoPath(*path)
	if !ok {
		writeJSONStatus(w, http.StatusBadRequest, map[string]string{"error": "Invalid repository path"})
		return false
	}
	*path = p
	return true
}

// handleBranchDeleteAPI handles POST /api/repo/branch-delete
// {path, branch, confirmed}.
func handleBranchDeleteAPI(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Path      string `json:"path"`
		Branch    string `json:"branch"`
		Confirmed bool   `json:"confirmed"`
	}
	if !decodeBranchWrite(w, r, &req, &req.Path) {
		return
	}
	res, err := deleteBranch(req.Path, req.Branch, req.Confirmed, liveSessionWorkDirs())
	if err != nil {
		writeBranchWriteError(w, "delete branch", err)
		return
	}
	writeJSONStatus(w, http.StatusOK, res)
}

// handleBranchUndoAPI handles POST /api/repo/branch-undo
// {path, branch, sha, folder}.
func handleBranchUndoAPI(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Path string `json:"path"`
		branchUndo
	}
	if !decodeBranchWrite(w, r, &req, &req.Path) {
		return
	}
	if err := undoBranchDelete(req.Path, req.branchUndo); err != nil {
		writeBranchWriteError(w, "undo delete", err)
		return
	}
	writeJSONStatus(w, http.StatusOK, map[string]bool{"restored": true})
}

// handleLeftoverRemoveAPI handles POST /api/repo/leftover-remove
// {path, folder, confirmed}.
func handleLeftoverRemoveAPI(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Path      string `json:"path"`
		Folder    string `json:"folder"`
		Confirmed bool   `json:"confirmed"`
	}
	if !decodeBranchWrite(w, r, &req, &req.Path) {
		return
	}
	res, err := removeLeftover(req.Path, req.Folder, req.Confirmed, liveSessionWorkDirs())
	if err != nil {
		writeBranchWriteError(w, "remove folder", err)
		return
	}
	writeJSONStatus(w, http.StatusOK, res)
}

// handleSwitchDefaultAPI handles POST /api/repo/switch-default {path}.
func handleSwitchDefaultAPI(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Path string `json:"path"`
	}
	if !decodeBranchWrite(w, r, &req, &req.Path) {
		return
	}
	branch, err := switchToDefault(req.Path, liveSessionWorkDirs())
	if err != nil {
		writeBranchWriteError(w, "switch branch", err)
		return
	}
	writeJSONStatus(w, http.StatusOK, map[string]string{"branch": branch})
}
