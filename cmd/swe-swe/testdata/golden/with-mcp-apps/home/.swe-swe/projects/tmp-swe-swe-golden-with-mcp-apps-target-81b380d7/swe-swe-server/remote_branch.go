// remote_branch.go -- the New Session dialog's new-branch-name check.
//
// The dialog no longer downloads every remote when it opens, so a branch that
// exists online but was never downloaded looks like a free name. Starting a
// session on it would create a separate new branch with the same name. Before
// creating a new branch, the dialog asks the remote about that one name.
//
// It must never hang: GIT_TERMINAL_PROMPT=0 (fail fast without a login),
// remoteFetchTimeout for the whole check, and git is killed on timeout or
// when the dialog cancels (the request's ctx ends).
package main

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"os/exec"
	"slices"
	"strings"
)

// remoteBranchResult: Exists says the remote has the branch; Fetched says it
// was downloaded as refs/remotes/<remote>/<name>, so the dialog's next branch
// listing shows it as an online card.
type remoteBranchResult struct {
	Exists  bool `json:"exists"`
	Fetched bool `json:"fetched"`
}

var errRemoteUnreachable = errors.New("remote unreachable")

// checkRemoteBranch asks remote whether it has branch name and, if so,
// downloads just that branch. errRemoteUnreachable when git fails for any
// reason other than "no such branch" (network, login, timeout).
func checkRemoteBranch(ctx context.Context, repo, remote, name, credHost, credUsername, credToken string) (remoteBranchResult, error) {
	ctx, cancel := context.WithTimeout(ctx, remoteFetchTimeout)
	defer cancel()

	ref := "refs/heads/" + name
	out, err := runGitWithTransientCredContext(ctx, repo, credHost, credUsername, credToken,
		"ls-remote", "--exit-code", "--heads", remote, ref)
	var exit *exec.ExitError
	if errors.As(err, &exit) && exit.ExitCode() == 2 && ctx.Err() == nil {
		return remoteBranchResult{}, nil // --exit-code: no matching ref
	}
	if err != nil {
		log.Printf("Remote branch check: ls-remote %s %s in %s failed: %v, output: %s", remote, name, repo, err, strings.TrimSpace(string(out)))
		return remoteBranchResult{}, errRemoteUnreachable
	}

	out, err = runGitWithTransientCredContext(ctx, repo, credHost, credUsername, credToken,
		"fetch", remote, "+"+ref+":refs/remotes/"+remote+"/"+name)
	if err != nil {
		log.Printf("Remote branch check: fetch %s %s in %s failed: %v, output: %s", remote, name, repo, err, strings.TrimSpace(string(out)))
		return remoteBranchResult{Exists: true}, errRemoteUnreachable
	}
	return remoteBranchResult{Exists: true, Fetched: true}, nil
}

// handleRemoteBranchAPI handles POST /api/repo/remote-branch
// {path, remote, name, credHost, credUsername, credToken}. Credentials ride
// in the body, never the URL. 502 when the remote can't be reached in time;
// the dialog then offers "Start anyway".
func handleRemoteBranchAPI(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		Path         string `json:"path"`
		Remote       string `json:"remote"`
		Name         string `json:"name"`
		CredHost     string `json:"credHost"`
		CredUsername string `json:"credUsername"`
		CredToken    string `json:"credToken"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONStatus(w, http.StatusBadRequest, map[string]string{"error": "Invalid request body"})
		return
	}
	repo, ok := branchAPIRepoPath(req.Path)
	if !ok {
		writeJSONStatus(w, http.StatusBadRequest, map[string]string{"error": "Invalid repository path"})
		return
	}
	if err := validateBranchName(repo, req.Name); err != nil {
		writeJSONStatus(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	out, err := gitRead(repo, "remote")
	if err != nil || !slices.Contains(strings.Fields(string(out)), req.Remote) {
		writeJSONStatus(w, http.StatusBadRequest, map[string]string{"error": "No such remote"})
		return
	}
	res, err := checkRemoteBranch(r.Context(), repo, req.Remote, req.Name, req.CredHost, req.CredUsername, req.CredToken)
	if err != nil {
		writeJSONStatus(w, http.StatusBadGateway, map[string]string{
			"error": "Couldn't reach " + req.Remote + " to check if this name is taken.",
		})
		return
	}
	writeJSONStatus(w, http.StatusOK, res)
}
