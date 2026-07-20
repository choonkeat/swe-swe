package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

// Shared HTTP client. Per the project rule, never construct a per-request
// http.Transport: reuse one client with a bounded idle pool.
var ghClient = &http.Client{
	Timeout: 30 * time.Second,
	Transport: &http.Transport{
		MaxIdleConnsPerHost: 10,
		IdleConnTimeout:     90 * time.Second,
	},
}

type githubProvider struct{}

// ghPermHint appends a token-permission tip when the server response looks like
// an authorization failure, so a failed flush explains what to fix.
func ghPermHint(msg string) string {
	m := strings.ToLower(msg)
	if strings.Contains(m, "not accessible by") ||
		strings.Contains(m, "resource not accessible") ||
		strings.Contains(m, "must have push access") ||
		strings.Contains(m, "forbidden") {
		return "\nhint: this token lacks write access. GitHub needs \"Pull requests: Read and write\" (fine-grained PAT) or the \"repo\" scope (classic PAT). See `prctx` (no args) for the full list."
	}
	return ""
}

func (githubProvider) token() (string, error) {
	t := strings.TrimSpace(os.Getenv("GITHUB_TOKEN"))
	if t == "" {
		t = strings.TrimSpace(os.Getenv("GH_TOKEN"))
	}
	if t == "" {
		return "", fmt.Errorf("GITHUB_TOKEN is not set")
	}
	return t, nil
}

// graphqlEndpoint returns the GraphQL URL for a host. github.com uses
// api.github.com/graphql; GitHub Enterprise uses <host>/api/graphql.
// PRCTX_GITHUB_GRAPHQL_URL overrides both.
func (githubProvider) graphqlEndpoint(host string) string {
	if u := strings.TrimSpace(os.Getenv("PRCTX_GITHUB_GRAPHQL_URL")); u != "" {
		return u
	}
	if host == "github.com" {
		return "https://api.github.com/graphql"
	}
	return "https://" + host + "/api/graphql"
}

// graphql issues a single GraphQL query and unmarshals data into out.
func (p githubProvider) graphql(ref PRRef, query string, vars map[string]interface{}, out interface{}) error {
	tok, err := p.token()
	if err != nil {
		return err
	}
	payload, err := json.Marshal(map[string]interface{}{"query": query, "variables": vars})
	if err != nil {
		return err
	}
	req, err := http.NewRequest(http.MethodPost, p.graphqlEndpoint(ref.Host), bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+tok)
	req.Header.Set("Content-Type", "application/json")

	resp, err := ghClient.Do(req)
	if err != nil {
		return fmt.Errorf("graphql request: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("graphql: %s: %s%s", resp.Status, strings.TrimSpace(string(body)), ghPermHint(string(body)))
	}
	// GraphQL returns 200 even for query errors; surface them explicitly.
	var envelope struct {
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	if err := json.Unmarshal(body, &envelope); err == nil && len(envelope.Errors) > 0 {
		return fmt.Errorf("graphql: %s%s", envelope.Errors[0].Message, ghPermHint(envelope.Errors[0].Message))
	}
	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("decode graphql response: %w", err)
	}
	return nil
}

const reviewThreadsQuery = `
query($owner:String!, $repo:String!, $number:Int!, $cursor:String) {
  repository(owner:$owner, name:$repo) {
    pullRequest(number:$number) {
      headRefName
      headRefOid
      baseRefOid
      reviewThreads(first:100, after:$cursor) {
        pageInfo { hasNextPage endCursor }
        nodes {
          id
          isResolved
          path
          line
          originalLine
          diffSide
          comments(first:100) {
            nodes {
              databaseId
              commit { oid }
              body
              author { login }
            }
          }
        }
      }
      comments(first:100) {
        nodes { databaseId body author { login } }
      }
    }
  }
}`

type gqlComment struct {
	DatabaseID int64 `json:"databaseId"`
	Commit     struct {
		Oid string `json:"oid"`
	} `json:"commit"`
	Body   string `json:"body"`
	Author struct {
		Login string `json:"login"`
	} `json:"author"`
}

type gqlThread struct {
	ID           string `json:"id"`
	IsResolved   bool   `json:"isResolved"`
	Path         string `json:"path"`
	Line         int    `json:"line"`
	OriginalLine int    `json:"originalLine"`
	DiffSide     string `json:"diffSide"`
	Comments     struct {
		Nodes []gqlComment `json:"nodes"`
	} `json:"comments"`
}

type gqlNote struct {
	DatabaseID int64  `json:"databaseId"`
	Body       string `json:"body"`
	Author     struct {
		Login string `json:"login"`
	} `json:"author"`
}

type gqlPullRequest struct {
	HeadRefName   string `json:"headRefName"`
	HeadRefOid    string `json:"headRefOid"`
	BaseRefOid    string `json:"baseRefOid"`
	ReviewThreads struct {
		PageInfo struct {
			HasNextPage bool   `json:"hasNextPage"`
			EndCursor   string `json:"endCursor"`
		} `json:"pageInfo"`
		Nodes []gqlThread `json:"nodes"`
	} `json:"reviewThreads"`
	Comments struct {
		Nodes []gqlNote `json:"nodes"`
	} `json:"comments"`
}

func (p githubProvider) Fetch(ref PRRef) (*Review, error) {
	rev := &Review{}
	var threads []gqlThread
	cursor := ""
	firstPage := true

	for {
		vars := map[string]interface{}{
			"owner":  ref.Owner,
			"repo":   ref.Repo,
			"number": ref.Number,
		}
		if cursor != "" {
			vars["cursor"] = cursor
		} else {
			vars["cursor"] = nil
		}

		var resp struct {
			Data struct {
				Repository struct {
					PullRequest gqlPullRequest `json:"pullRequest"`
				} `json:"repository"`
			} `json:"data"`
		}
		if err := p.graphql(ref, reviewThreadsQuery, vars, &resp); err != nil {
			return nil, err
		}
		pr := resp.Data.Repository.PullRequest

		if firstPage {
			rev.Branch = pr.HeadRefName
			rev.HeadSHA = pr.HeadRefOid
			rev.BaseSHA = pr.BaseRefOid
			for _, n := range pr.Comments.Nodes {
				rev.Notes = append(rev.Notes, Note{ID: n.DatabaseID, Author: n.Author.Login, Body: n.Body})
			}
			firstPage = false
		}
		threads = append(threads, pr.ReviewThreads.Nodes...)

		if !pr.ReviewThreads.PageInfo.HasNextPage {
			break
		}
		cursor = pr.ReviewThreads.PageInfo.EndCursor
	}

	rev.Threads = mapThreads(threads)
	return rev, nil
}

// mapThreads converts GraphQL review threads into the provider-agnostic model.
// Thread.ID is the GraphQL node id (needed by the resolveReviewThread mutation
// in the write slice); the root comment's databaseId is preserved on its
// Comment so the REST reply path can still address it.
func mapThreads(nodes []gqlThread) []Thread {
	threads := make([]Thread, 0, len(nodes))
	for _, n := range nodes {
		if len(n.Comments.Nodes) == 0 {
			continue
		}
		root := n.Comments.Nodes[0]
		line := n.Line
		if line == 0 {
			line = n.OriginalLine
		}
		t := Thread{
			ID:        n.ID,
			Path:      n.Path,
			Line:      line,
			Side:      n.DiffSide,
			CommitSHA: root.Commit.Oid,
			Resolved:  n.IsResolved,
		}
		for _, c := range n.Comments.Nodes {
			t.Comments = append(t.Comments, Comment{ID: c.DatabaseID, Author: c.Author.Login, Body: c.Body})
		}
		threads = append(threads, t)
	}
	return threads
}
