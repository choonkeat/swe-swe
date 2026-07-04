package main

import (
	"fmt"
	"io"
	"os/exec"
	"strings"
)

// gitHead returns the local working tree's HEAD sha, or "" if it can't be
// determined (e.g. not in a git repo). The CLI never writes git; this is a
// read-only anchor check.
func gitHead() string {
	out, err := exec.Command("git", "rev-parse", "HEAD").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// flushGitCheck warns when the local HEAD differs from the fetched PR head, so
// the user knows comments will anchor to the pushed/fetched state, not their
// uncommitted/unpushed local edits. Returns an error unless force is set.
func flushGitCheck(w io.Writer, s *State, force bool) error {
	head := gitHead()
	if head == "" || s.HeadAtFetch == "" || head == s.HeadAtFetch {
		return nil
	}
	fmt.Fprintf(w, "warning: local HEAD %s differs from fetched PR head %s.\n", shortSHA(head), shortSHA(s.HeadAtFetch))
	fmt.Fprintf(w, "         comments anchor to the fetched/pushed state, not your local edits.\n")
	if !force {
		return fmt.Errorf("refusing to flush; push and re-fetch, or re-run with --force")
	}
	fmt.Fprintf(w, "         proceeding anyway (--force).\n")
	return nil
}

// flush posts all staged replies, new comments, and resolves upstream, in that
// order. It is idempotent: each item carries a posted-id stamp, so a re-run
// after a partial failure resumes instead of double-posting. State is saved
// after every successful post.
func flush(w io.Writer, prov Provider, s *State, force bool) error {
	if err := flushGitCheck(w, s, force); err != nil {
		return err
	}

	posted := 0

	// 1. Replies to existing threads.
	for i := range s.Threads {
		t := &s.Threads[i]
		if t.PendingReply == "" || t.PostedReplyID != 0 {
			continue
		}
		id, err := prov.PostReply(s.Ref, t.ID, t.PendingReply)
		if err != nil {
			return fmt.Errorf("reply to thread %s: %w", t.ID, err)
		}
		t.PostedReplyID = id
		if err := saveState(s); err != nil {
			return err
		}
		fmt.Fprintf(w, "posted reply to thread %s (comment %d)\n", t.ID, id)
		posted++
	}

	// 2. New inline comments.
	for i := range s.Drafts {
		d := &s.Drafts[i]
		if d.PostedID != 0 {
			continue
		}
		anchor := Anchor{
			Path:     d.Path,
			Line:     d.Line,
			Side:     "RIGHT",
			BaseSHA:  s.BaseSHA,
			HeadSHA:  s.HeadAtFetch,
			StartSHA: s.StartSHA,
		}
		id, err := prov.PostComment(s.Ref, anchor, d.Body)
		if err != nil {
			return fmt.Errorf("post comment %s (%s:%d): %w", d.ID, d.Path, d.Line, err)
		}
		d.PostedID = id
		if err := saveState(s); err != nil {
			return err
		}
		fmt.Fprintf(w, "posted comment %s -> %s:%d (comment %d)\n", d.ID, d.Path, d.Line, id)
		posted++
	}

	// 3. Resolves.
	for i := range s.Threads {
		t := &s.Threads[i]
		if !t.PendingResolve || t.Resolved {
			continue
		}
		if err := prov.ResolveThread(s.Ref, t.ID); err != nil {
			return fmt.Errorf("resolve thread %s: %w", t.ID, err)
		}
		t.Resolved = true
		t.PendingResolve = false
		if err := saveState(s); err != nil {
			return err
		}
		fmt.Fprintf(w, "resolved thread %s\n", t.ID)
		posted++
	}

	if posted == 0 {
		fmt.Fprintln(w, "nothing staged to flush")
	} else {
		fmt.Fprintf(w, "flushed %d item(s)\n", posted)
	}
	return nil
}
