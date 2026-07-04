package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// State is the local review state for one PR/MR. It lives OUTSIDE the worktree
// so it is never accidentally committed. Code changes are git's job; this file
// only tracks review threads, notes, staged drafts, and idempotency stamps.
type State struct {
	Ref     PRRef    `json:"ref"`
	Branch  string   `json:"branch"`
	BaseSHA string   `json:"base_sha"`
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

// statePath returns the on-disk location for a ref's state, keyed by host and
// owner-repo so multiple repos/PRs never collide. Honors XDG_STATE_HOME.
func statePath(ref PRRef) (string, error) {
	base := os.Getenv("XDG_STATE_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("locate home dir: %w", err)
		}
		base = filepath.Join(home, ".local", "state")
	}
	dir := filepath.Join(base, "prctx", ref.Host, ref.slug())
	return filepath.Join(dir, fmt.Sprintf("%d.json", ref.Number)), nil
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

func loadState(ref PRRef) (*State, error) {
	p, err := statePath(ref)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(p)
	if err != nil {
		if os.IsNotExist(err) {
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
