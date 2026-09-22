package main

import (
	"fmt"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
)

// Provider kinds. Kind is what decides which adapter runs; the host name is
// only ever a hint, so self-hosted installs on any domain work.
const (
	kindGitHub = "github"
	kindGitLab = "gitlab"
)

// PRRef identifies a pull request / merge request on a provider host.
type PRRef struct {
	Host string // e.g. "github.com", or a self-hosted domain
	// Kind is "github", "gitlab", or "" when unknown. A PR/MR URL names it
	// outright (/pull/ vs /-/merge_requests/); a bare number does not.
	Kind   string `json:"kind,omitempty"`
	Owner  string
	Repo   string
	Number int
}

func (r PRRef) slug() string { return r.Owner + "-" + r.Repo }

var (
	// https://github.com/owner/repo/pull/123  (and gitlab .../-/merge_requests/123)
	reURL = regexp.MustCompile(`^https?://([^/]+)/(.+?)/(pull|-/merge_requests|merge_requests)/(\d+)`)
	// git@github.com:owner/repo.git
	reSSH = regexp.MustCompile(`^git@([^:]+):(.+?)(?:\.git)?$`)
	// https://github.com/owner/repo(.git)
	reHTTP = regexp.MustCompile(`^https?://([^/]+)/(.+?)(?:\.git)?$`)
)

// parseRef resolves a CLI argument into a PRRef. The argument is either a full
// PR/MR URL, or a bare number (in which case host+owner+repo come from the
// git "origin" remote).
func parseRef(arg string) (PRRef, error) {
	if m := reURL.FindStringSubmatch(arg); m != nil {
		n, err := strconv.Atoi(m[4])
		if err != nil {
			return PRRef{}, fmt.Errorf("parse PR number from %q: %w", arg, err)
		}
		owner, repo := splitOwnerRepo(m[2])
		kind := kindGitLab
		if m[3] == "pull" {
			kind = kindGitHub
		}
		return PRRef{Host: m[1], Kind: kind, Owner: owner, Repo: repo, Number: n}, nil
	}

	n, err := strconv.Atoi(arg)
	if err != nil {
		return PRRef{}, fmt.Errorf("argument %q is neither a PR URL nor a number", arg)
	}
	ref, err := refFromOrigin()
	if err != nil {
		return PRRef{}, err
	}
	ref.Number = n
	return ref, nil
}

// refFromOrigin reads host/owner/repo from the git "origin" remote. It does not
// set Number.
func refFromOrigin() (PRRef, error) {
	out, err := exec.Command("git", "remote", "get-url", "origin").Output()
	if err != nil {
		return PRRef{}, fmt.Errorf("read git origin remote: %w", err)
	}
	url := strings.TrimSpace(string(out))
	if m := reSSH.FindStringSubmatch(url); m != nil {
		owner, repo := splitOwnerRepo(m[2])
		return PRRef{Host: m[1], Owner: owner, Repo: repo}, nil
	}
	if m := reHTTP.FindStringSubmatch(url); m != nil {
		owner, repo := splitOwnerRepo(m[2])
		return PRRef{Host: m[1], Owner: owner, Repo: repo}, nil
	}
	return PRRef{}, fmt.Errorf("unrecognized origin remote url: %q", url)
}

// splitOwnerRepo splits "owner/repo" or "group/subgroup/repo" into owner and
// repo, keeping any GitLab subgroup path as part of owner.
func splitOwnerRepo(path string) (owner, repo string) {
	path = strings.TrimSuffix(path, ".git")
	i := strings.LastIndex(path, "/")
	if i < 0 {
		return path, ""
	}
	return path[:i], path[i+1:]
}
