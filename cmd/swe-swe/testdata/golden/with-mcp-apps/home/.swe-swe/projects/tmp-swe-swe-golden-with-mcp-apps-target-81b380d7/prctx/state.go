package main

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// State is the local review state for one PR/MR. It lives OUTSIDE the worktree
// so it is never accidentally committed. Code changes are git's job; this file
// only tracks review threads, notes, staged drafts, and idempotency stamps.
type State struct {
	Ref      PRRef  `json:"ref"`
	Branch   string `json:"branch"`
	BaseSHA  string `json:"base_sha"`
	StartSHA string `json:"start_sha"`
	// HeadAtFetch is the head sha when we last fetched. flush compares it (and
	// live git HEAD) to warn about unpushed local commits.
	HeadAtFetch string   `json:"head_at_fetch"`
	Threads     []Thread `json:"threads"`
	Notes       []Note   `json:"notes"`
	Drafts      []Draft  `json:"drafts"`
}

// Draft is a NEW inline comment staged locally. The diff position is resolved
// at flush time (against current HEAD), not here, so it survives code changes
// between staging and flush.
type Draft struct {
	ID   string `json:"id"`
	Path string `json:"path"`
	Line int    `json:"line"`
	Body string `json:"body"`
	// PostedID is the idempotency stamp: once set, flush skips this draft.
	PostedID int64 `json:"posted_id,omitempty"`
}

// Where state lives. It stays OUTSIDE the worktree, under
// $XDG_STATE_HOME/prctx (default ~/.local/state/prctx), and inside a git repo
// it is kept per local branch: two sessions on different branches used to
// share one set of staged drafts and one "current PR", so a bare `prctx flush`
// in one could post the other's drafts to the other's PR. Outside a git repo
// there is no branch to key by and the old per-machine layout is used.

// localBranch returns the branch checked out in the current directory's git
// repo ("HEAD" when detached), or "" outside a git repo.
func localBranch() string {
	out, err := exec.Command("git", "rev-parse", "--abbrev-ref", "HEAD").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// repoIdentity names the local repo for the per-branch "current PR" pointer:
// its origin remote, else its top-level directory.
func repoIdentity() string {
	if out, err := exec.Command("git", "remote", "get-url", "origin").Output(); err == nil {
		if u := strings.TrimSpace(string(out)); u != "" {
			return u
		}
	}
	out, _ := exec.Command("git", "rev-parse", "--show-toplevel").Output()
	return strings.TrimSpace(string(out))
}

// legacyStatePath is the pre-branch location: one file per PR per machine.
func legacyStatePath(ref PRRef) (string, error) {
	base, err := stateBase()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, ref.Host, ref.slug(), fmt.Sprintf("%d.json", ref.Number)), nil
}

// statePath returns the on-disk location for a ref's state on the checked-out
// branch, keyed by host and owner-repo so multiple repos/PRs never collide.
func statePath(ref PRRef) (string, error) {
	branch := localBranch()
	if branch == "" {
		return legacyStatePath(ref)
	}
	base, err := stateBase()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, ref.Host, ref.slug(), fmt.Sprintf("%d", ref.Number),
		"branch-"+url.PathEscape(branch)+".json"), nil
}

func saveState(s *State) error {
	p, err := statePath(s.Ref)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return fmt.Errorf("create state dir: %w", err)
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal state: %w", err)
	}
	if err := os.WriteFile(p, data, 0o644); err != nil {
		return fmt.Errorf("write state %s: %w", p, err)
	}
	return nil
}

// stateBase returns the prctx state root directory.
func stateBase() (string, error) {
	base := os.Getenv("XDG_STATE_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("locate home dir: %w", err)
		}
		base = filepath.Join(home, ".local", "state")
	}
	return filepath.Join(base, "prctx"), nil
}

// currentPath is where the last-touched ref is remembered: per repo and
// branch inside a git repo, one per machine outside.
func currentPath() (string, error) {
	base, err := stateBase()
	if err != nil {
		return "", err
	}
	branch := localBranch()
	if branch == "" {
		return filepath.Join(base, "current.json"), nil
	}
	return filepath.Join(base, "current", url.PathEscape(repoIdentity()), "branch-"+url.PathEscape(branch)+".json"), nil
}

// saveCurrent records the last-touched ref so subsequent commands can omit it.
func saveCurrent(ref PRRef) error {
	p, err := currentPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(ref, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(p, data, 0o644)
}

// loadCurrent reads the last-touched ref for the checked-out branch. A ref
// remembered before per-branch pointers existed is still honoured -- but only
// when that PR was fetched for this very branch.
func loadCurrent() (PRRef, error) {
	p, err := currentPath()
	if err != nil {
		return PRRef{}, err
	}
	data, err := os.ReadFile(p)
	if err != nil {
		if !os.IsNotExist(err) {
			return PRRef{}, err
		}
		if ref, ok := legacyCurrentForBranch(); ok {
			return ref, nil
		}
		return PRRef{}, fmt.Errorf("no current PR -- pass a PR url/number or run `prctx fetch` first")
	}
	var ref PRRef
	if err := json.Unmarshal(data, &ref); err != nil {
		return PRRef{}, err
	}
	return ref, nil
}

// legacyCurrentForBranch returns the pre-upgrade machine-wide current ref when
// its saved state belongs to the checked-out branch.
func legacyCurrentForBranch() (PRRef, bool) {
	branch := localBranch()
	if branch == "" {
		return PRRef{}, false
	}
	base, err := stateBase()
	if err != nil {
		return PRRef{}, false
	}
	data, err := os.ReadFile(filepath.Join(base, "current.json"))
	if err != nil {
		return PRRef{}, false
	}
	var ref PRRef
	if json.Unmarshal(data, &ref) != nil {
		return PRRef{}, false
	}
	if s, err := readLegacyState(ref); err == nil && s.Branch == branch {
		return ref, true
	}
	return PRRef{}, false
}

func readLegacyState(ref PRRef) (*State, error) {
	p, err := legacyStatePath(ref)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(p)
	if err != nil {
		return nil, err
	}
	var s State
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, fmt.Errorf("parse state %s: %w", p, err)
	}
	return &s, nil
}

func loadState(ref PRRef) (*State, error) {
	p, err := statePath(ref)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(p)
	if err != nil {
		if os.IsNotExist(err) {
			// State staged before per-branch storage carries over to the
			// branch the PR was fetched for, and to no other.
			if branch := localBranch(); branch != "" {
				if s, lerr := readLegacyState(ref); lerr == nil && s.Branch == branch {
					return s, nil
				}
			}
			return nil, fmt.Errorf("no local state for %s/%s#%d -- run `prctx fetch` first", ref.Owner, ref.Repo, ref.Number)
		}
		return nil, fmt.Errorf("read state %s: %w", p, err)
	}
	var s State
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, fmt.Errorf("parse state %s: %w", p, err)
	}
	return &s, nil
}
