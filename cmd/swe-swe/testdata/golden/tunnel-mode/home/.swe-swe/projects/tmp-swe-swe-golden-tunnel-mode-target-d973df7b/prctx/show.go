package main

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

// render writes a human/agent-readable view of the review state. This is the
// agent's read surface: threads with stable ids, anchors, comments, and any
// locally staged drafts. The diff itself is not duplicated here -- it lives in
// the worktree via git.
func render(w io.Writer, s *State) {
	fmt.Fprintf(w, "%s/%s#%d  branch=%s  head=%s\n", s.Ref.Owner, s.Ref.Repo, s.Ref.Number, s.Branch, shortSHA(s.HeadAtFetch))
	fmt.Fprintf(w, "%d thread(s), %d note(s), %d staged draft(s)\n\n", len(s.Threads), len(s.Notes), len(s.Drafts))

	for _, t := range s.Threads {
		status := "unresolved"
		if t.Resolved {
			status = "resolved"
		}
		fmt.Fprintf(w, "## thread %s  %s:%d  [%s]\n", t.ID, t.Path, t.Line, status)
		for _, c := range t.Comments {
			fmt.Fprintf(w, "  %s: %s\n", c.Author, oneLine(c.Body))
		}
		if t.PendingReply != "" {
			fmt.Fprintf(w, "  > staged reply: %s\n", oneLine(t.PendingReply))
		}
		if t.PendingResolve {
			fmt.Fprintf(w, "  > staged: resolve\n")
		}
		fmt.Fprintln(w)
	}

	if len(s.Notes) > 0 {
		fmt.Fprintln(w, "## top-level notes")
		for _, n := range s.Notes {
			fmt.Fprintf(w, "  %s: %s\n", n.Author, oneLine(n.Body))
		}
		fmt.Fprintln(w)
	}

	if len(s.Drafts) > 0 {
		fmt.Fprintln(w, "## staged new comments")
		for _, d := range s.Drafts {
			fmt.Fprintf(w, "  [%s] %s:%d  %s\n", d.ID, d.Path, d.Line, oneLine(d.Body))
		}
	}
}

func renderJSON(w io.Writer, s *State) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(s)
}

func oneLine(s string) string {
	s = strings.ReplaceAll(s, "\r\n", " ")
	s = strings.ReplaceAll(s, "\n", " ")
	if len(s) > 120 {
		return s[:117] + "..."
	}
	return s
}

func shortSHA(s string) string {
	if len(s) > 8 {
		return s[:8]
	}
	return s
}
