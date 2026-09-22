package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
)

// apiBase returns the REST API root for a host. github.com uses
// api.github.com; GitHub Enterprise uses <host>/api/v3. PRCTX_GITHUB_API_BASE
// overrides both, for installs that don't follow either layout.
func (githubProvider) apiBase(host string) string {
	if base := strings.TrimSuffix(strings.TrimSpace(os.Getenv("PRCTX_GITHUB_API_BASE")), "/"); base != "" {
		return base
	}
	if host == "github.com" {
		return "https://api.github.com"
	}
	return "https://" + host + "/api/v3"
}

// post issues an authenticated REST POST and unmarshals the response into out
// (out may be nil to discard the body).
func (p githubProvider) post(ref PRRef, path string, in, out interface{}) error {
	tok, err := p.token()
	if err != nil {
		return err
	}
	payload, err := json.Marshal(in)
	if err != nil {
		return err
	}
	req, err := http.NewRequest(http.MethodPost, p.apiBase(ref.Host)+path, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+tok)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	req.Header.Set("Content-Type", "application/json")

	resp, err := ghClient.Do(req)
	if err != nil {
		return fmt.Errorf("POST %s: %w", path, err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("POST %s: %s: %s", path, resp.Status, string(body))
	}
	if out != nil {
		if err := json.Unmarshal(body, out); err != nil {
			return fmt.Errorf("decode POST %s response: %w", path, err)
		}
	}
	return nil
}

const addReplyMutation = `
mutation($threadId:ID!, $body:String!) {
  addPullRequestReviewThreadReply(input:{pullRequestReviewThreadId:$threadId, body:$body}) {
    comment { databaseId }
  }
}`

func (p githubProvider) PostReply(ref PRRef, threadID, body string) (int64, error) {
	var resp struct {
		Data struct {
			AddReply struct {
				Comment struct {
					DatabaseID int64 `json:"databaseId"`
				} `json:"comment"`
			} `json:"addPullRequestReviewThreadReply"`
		} `json:"data"`
	}
	vars := map[string]interface{}{"threadId": threadID, "body": body}
	if err := p.graphql(ref, addReplyMutation, vars, &resp); err != nil {
		return 0, err
	}
	return resp.Data.AddReply.Comment.DatabaseID, nil
}

const resolveThreadMutation = `
mutation($threadId:ID!) {
  resolveReviewThread(input:{threadId:$threadId}) {
    thread { id }
  }
}`

func (p githubProvider) ResolveThread(ref PRRef, threadID string) error {
	var resp struct {
		Data struct {
			Resolve struct {
				Thread struct {
					ID string `json:"id"`
				} `json:"thread"`
			} `json:"resolveReviewThread"`
		} `json:"data"`
	}
	vars := map[string]interface{}{"threadId": threadID}
	return p.graphql(ref, resolveThreadMutation, vars, &resp)
}

func (p githubProvider) PostComment(ref PRRef, a Anchor, body string) (int64, error) {
	side := a.Side
	if side == "" {
		side = "RIGHT"
	}
	in := map[string]interface{}{
		"body":      body,
		"commit_id": a.HeadSHA,
		"path":      a.Path,
		"line":      a.Line,
		"side":      side,
	}
	var resp struct {
		ID int64 `json:"id"`
	}
	endpoint := fmt.Sprintf("/repos/%s/%s/pulls/%d/comments", ref.Owner, ref.Repo, ref.Number)
	if err := p.post(ref, endpoint, in, &resp); err != nil {
		return 0, err
	}
	return resp.ID, nil
}

func (p githubProvider) review(ref PRRef, event, body string) error {
	in := map[string]interface{}{"event": event}
	if body != "" {
		in["body"] = body
	}
	endpoint := fmt.Sprintf("/repos/%s/%s/pulls/%d/reviews", ref.Owner, ref.Repo, ref.Number)
	return p.post(ref, endpoint, in, nil)
}

func (p githubProvider) Approve(ref PRRef, body string) error {
	return p.review(ref, "APPROVE", body)
}

func (p githubProvider) Reject(ref PRRef, body string) error {
	return p.review(ref, "REQUEST_CHANGES", body)
}
