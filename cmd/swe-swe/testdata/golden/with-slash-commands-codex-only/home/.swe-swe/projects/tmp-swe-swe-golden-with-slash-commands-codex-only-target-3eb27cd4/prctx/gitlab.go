package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
)

// gitlabProvider implements Provider against the GitLab REST API (v4). A GitLab
// MR maps onto the same model as a GitHub PR: discussions are Threads (inline
// ones carry a position), individual general notes are top-level Notes.
type gitlabProvider struct{}

// apiBase returns the REST API root for a host. Self-hosted GitLab normally
// lives at <host>/api/v4; PRCTX_GITLAB_API_BASE covers installs served from a
// subpath or over plain http.
func (gitlabProvider) apiBase(host string) string {
	if base := strings.TrimSuffix(strings.TrimSpace(os.Getenv("PRCTX_GITLAB_API_BASE")), "/"); base != "" {
		return base
	}
	return "https://" + host + "/api/v4"
}

func (gitlabProvider) token() (string, error) {
	t := strings.TrimSpace(os.Getenv("GITLAB_TOKEN"))
	if t == "" {
		return "", fmt.Errorf("GITLAB_TOKEN is not set")
	}
	return t, nil
}

// projectID is the URL-encoded full path GitLab accepts as :id.
func (gitlabProvider) projectID(ref PRRef) string {
	return url.PathEscape(ref.Owner + "/" + ref.Repo)
}

// do performs an authenticated request and unmarshals a successful body into
// out (out may be nil). It returns the raw response for header inspection.
func (p gitlabProvider) do(ref PRRef, method, path string, in, out interface{}) (*http.Response, error) {
	tok, err := p.token()
	if err != nil {
		return nil, err
	}
	var bodyReader io.Reader
	if in != nil {
		payload, err := json.Marshal(in)
		if err != nil {
			return nil, err
		}
		bodyReader = bytes.NewReader(payload)
	}
	req, err := http.NewRequest(method, p.apiBase(ref.Host)+path, bodyReader)
	if err != nil {
		return nil, err
	}
	req.Header.Set("PRIVATE-TOKEN", tok)
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := ghClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%s %s: %w", method, path, err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return resp, fmt.Errorf("%s %s: %s: %s", method, path, resp.Status, strings.TrimSpace(string(body)))
	}
	if out != nil {
		if err := json.Unmarshal(body, out); err != nil {
			return resp, fmt.Errorf("decode %s %s: %w", method, path, err)
		}
	}
	return resp, nil
}

type glMR struct {
	SourceBranch string `json:"source_branch"`
	SHA          string `json:"sha"`
	DiffRefs     struct {
		BaseSHA  string `json:"base_sha"`
		HeadSHA  string `json:"head_sha"`
		StartSHA string `json:"start_sha"`
	} `json:"diff_refs"`
}

type glPosition struct {
	NewPath string `json:"new_path"`
	OldPath string `json:"old_path"`
	NewLine int    `json:"new_line"`
	OldLine int    `json:"old_line"`
	HeadSHA string `json:"head_sha"`
}

type glNote struct {
	ID       int64  `json:"id"`
	Body     string `json:"body"`
	Type     string `json:"type"` // "DiffNote" for inline, null/"" for general
	Resolved bool   `json:"resolved"`
	Author   struct {
		Username string `json:"username"`
	} `json:"author"`
	Position *glPosition `json:"position"`
}

type glDiscussion struct {
	ID    string   `json:"id"`
	Notes []glNote `json:"notes"`
}

func (p gitlabProvider) Fetch(ref PRRef) (*Review, error) {
	pid := p.projectID(ref)

	var mr glMR
	if _, err := p.do(ref, http.MethodGet,
		fmt.Sprintf("/projects/%s/merge_requests/%d", pid, ref.Number), nil, &mr); err != nil {
		return nil, err
	}

	discussions, err := p.fetchDiscussions(ref, pid)
	if err != nil {
		return nil, err
	}

	rev := &Review{
		Branch:   mr.SourceBranch,
		BaseSHA:  mr.DiffRefs.BaseSHA,
		HeadSHA:  mr.DiffRefs.HeadSHA,
		StartSHA: mr.DiffRefs.StartSHA,
	}
	if rev.HeadSHA == "" {
		rev.HeadSHA = mr.SHA
	}
	rev.Threads, rev.Notes = mapDiscussions(discussions)
	return rev, nil
}

func (p gitlabProvider) fetchDiscussions(ref PRRef, pid string) ([]glDiscussion, error) {
	var all []glDiscussion
	for page := 1; ; page++ {
		var batch []glDiscussion
		path := fmt.Sprintf("/projects/%s/merge_requests/%d/discussions?per_page=100&page=%d", pid, ref.Number, page)
		if _, err := p.do(ref, http.MethodGet, path, nil, &batch); err != nil {
			return nil, err
		}
		all = append(all, batch...)
		if len(batch) < 100 {
			break
		}
	}
	return all, nil
}

// mapDiscussions splits GitLab discussions into inline Threads (notes carry a
// position) and top-level Notes (general discussion).
func mapDiscussions(discussions []glDiscussion) ([]Thread, []Note) {
	var threads []Thread
	var notes []Note

	for _, d := range discussions {
		if len(d.Notes) == 0 {
			continue
		}
		root := d.Notes[0]
		if root.Position == nil {
			// General (non-inline) discussion -> top-level notes.
			for _, n := range d.Notes {
				notes = append(notes, Note{ID: n.ID, Author: n.Author.Username, Body: n.Body})
			}
			continue
		}

		path := root.Position.NewPath
		line := root.Position.NewLine
		side := "RIGHT"
		if line == 0 {
			path = root.Position.OldPath
			line = root.Position.OldLine
			side = "LEFT"
		}
		t := Thread{
			ID:        d.ID,
			Path:      path,
			Line:      line,
			Side:      side,
			CommitSHA: root.Position.HeadSHA,
			Resolved:  root.Resolved,
		}
		for _, n := range d.Notes {
			t.Comments = append(t.Comments, Comment{ID: n.ID, Author: n.Author.Username, Body: n.Body})
		}
		threads = append(threads, t)
	}
	return threads, notes
}

func (p gitlabProvider) PostReply(ref PRRef, threadID, body string) (int64, error) {
	pid := p.projectID(ref)
	path := fmt.Sprintf("/projects/%s/merge_requests/%d/discussions/%s/notes", pid, ref.Number, threadID)
	var resp struct {
		ID int64 `json:"id"`
	}
	if _, err := p.do(ref, http.MethodPost, path, map[string]interface{}{"body": body}, &resp); err != nil {
		return 0, err
	}
	return resp.ID, nil
}

func (p gitlabProvider) PostComment(ref PRRef, a Anchor, body string) (int64, error) {
	pid := p.projectID(ref)
	position := map[string]interface{}{
		"position_type": "text",
		"base_sha":      a.BaseSHA,
		"head_sha":      a.HeadSHA,
		"start_sha":     a.StartSHA,
		"new_path":      a.Path,
		"new_line":      a.Line,
	}
	in := map[string]interface{}{"body": body, "position": position}
	path := fmt.Sprintf("/projects/%s/merge_requests/%d/discussions", pid, ref.Number)
	var resp struct {
		Notes []struct {
			ID int64 `json:"id"`
		} `json:"notes"`
	}
	if _, err := p.do(ref, http.MethodPost, path, in, &resp); err != nil {
		return 0, err
	}
	if len(resp.Notes) > 0 {
		return resp.Notes[0].ID, nil
	}
	return 0, nil
}

func (p gitlabProvider) ResolveThread(ref PRRef, threadID string) error {
	pid := p.projectID(ref)
	path := fmt.Sprintf("/projects/%s/merge_requests/%d/discussions/%s?resolved=true", pid, ref.Number, threadID)
	_, err := p.do(ref, http.MethodPut, path, nil, nil)
	return err
}

func (p gitlabProvider) Approve(ref PRRef, body string) error {
	pid := p.projectID(ref)
	if body != "" {
		if err := p.postNote(ref, pid, body); err != nil {
			return err
		}
	}
	_, err := p.do(ref, http.MethodPost,
		fmt.Sprintf("/projects/%s/merge_requests/%d/approve", pid, ref.Number), nil, nil)
	return err
}

// Reject: GitLab has no portable REQUEST_CHANGES verdict across versions. The
// most broadly available equivalent is to unapprove and leave a note, so a
// rejection is unambiguous to traditional GitLab reviewers.
func (p gitlabProvider) Reject(ref PRRef, body string) error {
	pid := p.projectID(ref)
	if body != "" {
		if err := p.postNote(ref, pid, body); err != nil {
			return err
		}
	}
	_, err := p.do(ref, http.MethodPost,
		fmt.Sprintf("/projects/%s/merge_requests/%d/unapprove", pid, ref.Number), nil, nil)
	return err
}

func (p gitlabProvider) postNote(ref PRRef, pid, body string) error {
	path := fmt.Sprintf("/projects/%s/merge_requests/%d/notes", pid, ref.Number)
	_, err := p.do(ref, http.MethodPost, path, map[string]interface{}{"body": body}, nil)
	return err
}
