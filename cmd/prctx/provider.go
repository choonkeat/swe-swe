package main

// Provider-agnostic review model. A GitHub PR and a GitLab MR both reduce to
// this shape: a branch at some head sha, a set of inline review Threads anchored
// to (path, line, side, commit), and top-level discussion Notes.
//
// The adapter is the ONLY provider-specific code. Everything above it (state
// store, rendering, staging) is shared.

// Comment is a single message inside a Thread or a top-level Note.
type Comment struct {
	ID     int64  `json:"id"`
	Author string `json:"author"`
	Body   string `json:"body"`
}

// Thread is an inline review discussion anchored to a position in the diff.
type Thread struct {
	// ID is the provider's stable thread identifier (GitHub: root review
	// comment id; GitLab: discussion id). Replies anchor to this, never to a
	// recomputed line.
	ID        string    `json:"id"`
	Path      string    `json:"path"`
	Line      int       `json:"line"`
	Side      string    `json:"side"` // "RIGHT" (added/context) or "LEFT" (removed)
	CommitSHA string    `json:"commit_sha"`
	Resolved  bool      `json:"resolved"`
	Comments  []Comment `json:"comments"`

	// Local staging (never sent until flush).
	PendingReply   string `json:"pending_reply,omitempty"`
	PendingResolve bool   `json:"pending_resolve,omitempty"`
	// PostedReplyID is the idempotency stamp: once set, flush skips this reply.
	PostedReplyID int64 `json:"posted_reply_id,omitempty"`
}

// Note is a top-level (non-inline) PR/MR discussion comment.
type Note struct {
	ID     int64  `json:"id"`
	Author string `json:"author"`
	Body   string `json:"body"`
}

// Review is the full snapshot pulled by Fetch.
type Review struct {
	Branch  string   `json:"branch"`
	BaseSHA string   `json:"base_sha"`
	HeadSHA string   `json:"head_sha"`
	Threads []Thread `json:"threads"`
	Notes   []Note   `json:"notes"`
}

// Provider is the adapter interface. The only provider-specific code lives
// behind it; everything else (state, render, staging, flush orchestration) is
// shared. Each write method maps to one atomic upstream action.
type Provider interface {
	Fetch(ref PRRef) (*Review, error)
	// PostReply adds a reply to an existing thread; returns the new comment id.
	PostReply(ref PRRef, threadID, body string) (int64, error)
	// PostComment creates a new inline comment (its own thread) anchored to
	// (path, line, side) on commitSHA; returns the new comment id.
	PostComment(ref PRRef, path string, line int, side, commitSHA, body string) (int64, error)
	// ResolveThread marks a thread resolved.
	ResolveThread(ref PRRef, threadID string) error
	// Approve / Reject set the review verdict (separate, atomic, never bundled
	// with comments). body is optional context.
	Approve(ref PRRef, body string) error
	Reject(ref PRRef, body string) error
}
