// Command prctx is a standalone, provider-agnostic helper for working a GitHub
// PR or GitLab MR locally: pull the review (diff lives in the worktree via git;
// comments come here), stage replies/comments/resolves, then flush them back
// upstream. Verdicts (approve/reject) are separate atomic commands.
//
// It knows nothing about swe-swe; swe-swe only supplies the token (via env,
// from the credentials modal) and thin slash commands that shell out.
//
// Boundaries:
//   - The CLI never runs git write commands. `git push` is your separate step.
//   - It only READS git (HEAD sha, diff) to anchor comments and to warn about
//     unpushed local commits before flush.
//
// First vertical slice: GitHub `fetch` and `show`.
package main

import (
	"fmt"
	"os"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "prctx: "+err.Error())
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		usage()
		return fmt.Errorf("no command given")
	}
	cmd, rest := args[0], args[1:]
	switch cmd {
	case "fetch":
		return cmdFetch(rest)
	case "show":
		return cmdShow(rest)
	case "-h", "--help", "help":
		usage()
		return nil
	default:
		usage()
		return fmt.Errorf("unknown command %q", cmd)
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `prctx - PR/MR review helper

Usage:
  prctx fetch <pr-url|number>   pull review threads into local state, then show
  prctx show [--json]           render staged review state

Env:
  GITHUB_TOKEN (or GH_TOKEN)    token for GitHub API calls

Planned (next slice): reply, comment, resolve, drop, flush, approve, reject.
`)
}

// providerFor selects the adapter for a ref's host. GitLab lands next slice.
func providerFor(ref PRRef) (Provider, error) {
	switch {
	case ref.Host == "github.com" || hostIsGitHubEnterprise(ref.Host):
		return githubProvider{}, nil
	default:
		return nil, fmt.Errorf("unsupported host %q (only GitHub is implemented so far)", ref.Host)
	}
}

// hostIsGitHubEnterprise is a placeholder; for now we only auto-detect
// github.com. Enterprise hosts can be forced via fetch once the adapter is
// confirmed. Returning false keeps behavior conservative.
func hostIsGitHubEnterprise(host string) bool { return false }

func cmdFetch(args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("usage: prctx fetch <pr-url|number>")
	}
	ref, err := parseRef(args[0])
	if err != nil {
		return err
	}
	prov, err := providerFor(ref)
	if err != nil {
		return err
	}
	rev, err := prov.Fetch(ref)
	if err != nil {
		return err
	}

	// Preserve any locally staged drafts/replies across a re-fetch by merging
	// onto prior state if it exists.
	s := &State{Ref: ref}
	if prior, err := loadState(ref); err == nil {
		s.Drafts = prior.Drafts
		mergePending(rev.Threads, prior.Threads)
	}
	s.Branch = rev.Branch
	s.BaseSHA = rev.BaseSHA
	s.HeadAtFetch = rev.HeadSHA
	s.Threads = rev.Threads
	s.Notes = rev.Notes

	if err := saveState(s); err != nil {
		return err
	}
	render(os.Stdout, s)
	return nil
}

// mergePending carries staged-but-not-flushed reply/resolve drafts from prior
// state onto freshly fetched threads (matched by thread ID).
func mergePending(fresh, prior []Thread) {
	by := map[string]Thread{}
	for _, t := range prior {
		by[t.ID] = t
	}
	for i := range fresh {
		if p, ok := by[fresh[i].ID]; ok {
			fresh[i].PendingReply = p.PendingReply
			fresh[i].PendingResolve = p.PendingResolve
			fresh[i].PostedReplyID = p.PostedReplyID
		}
	}
}

func cmdShow(args []string) error {
	asJSON := false
	var refArg string
	for _, a := range args {
		switch a {
		case "--json":
			asJSON = true
		default:
			refArg = a
		}
	}

	var ref PRRef
	var err error
	if refArg != "" {
		ref, err = parseRef(refArg)
	} else {
		// No ref given: fall back to origin without a number is ambiguous, so
		// require an explicit ref for show.
		return fmt.Errorf("usage: prctx show <pr-url|number> [--json]")
	}
	if err != nil {
		return err
	}
	s, err := loadState(ref)
	if err != nil {
		return err
	}
	if asJSON {
		return renderJSON(os.Stdout, s)
	}
	render(os.Stdout, s)
	return nil
}
