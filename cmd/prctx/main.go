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
//   - It only READS git (HEAD sha) to warn about unpushed local commits before
//     flush.
package main

import (
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
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
	case "reply":
		return cmdReply(rest)
	case "comment":
		return cmdComment(rest)
	case "resolve":
		return cmdResolve(rest)
	case "drop":
		return cmdDrop(rest)
	case "flush":
		return cmdFlush(rest)
	case "approve":
		return cmdVerdict(rest, true)
	case "reject":
		return cmdVerdict(rest, false)
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

Read:
  prctx fetch <pr-url|number>          pull review threads into local state, then show
  prctx show [<pr>] [--json]           render staged review state

Stage (local only, nothing sent):
  prctx reply [<pr>] <thread-id> <body>
  prctx comment [<pr>] <file>:<line> <body>
  prctx resolve [<pr>] <thread-id>
  prctx drop [<pr>] <thread-id|draft-id>

Sync:
  prctx flush [<pr>] [--force]         post staged replies/comments/resolves
  prctx approve [<pr>] [--body <text>] set verdict: approve
  prctx reject  [<pr>] [--body <text>] set verdict: request changes

<pr> is a PR/MR url or number; omit it to use the last-fetched PR.
Env: GITHUB_TOKEN (or GH_TOKEN) for GitHub; GITLAB_TOKEN for GitLab.
`)
}

// providerFor selects the adapter for a ref's host. Self-hosted GitLab is
// detected by a "gitlab." host prefix; self-hosted GitHub Enterprise is not
// auto-detected yet.
func providerFor(ref PRRef) (Provider, error) {
	switch {
	case ref.Host == "github.com":
		return githubProvider{}, nil
	case ref.Host == "gitlab.com" || strings.HasPrefix(ref.Host, "gitlab."):
		return gitlabProvider{}, nil
	default:
		return nil, fmt.Errorf("unsupported host %q (github.com and gitlab.com supported)", ref.Host)
	}
}

var reDigits = regexp.MustCompile(`^\d+$`)

// isRefToken reports whether arg looks like a PR ref (url or bare number) as
// opposed to a thread id (PRRT_...), file:line, or draft id (d1).
func isRefToken(arg string) bool {
	return reURL.MatchString(arg) || reDigits.MatchString(arg)
}

// splitRef resolves the optional leading <pr> argument. If present it is parsed
// and recorded as current; otherwise the last-fetched ref is used. Returns the
// ref and the remaining (non-ref) args.
func splitRef(args []string) (PRRef, []string, error) {
	if len(args) > 0 && isRefToken(args[0]) {
		ref, err := parseRef(args[0])
		if err != nil {
			return PRRef{}, nil, err
		}
		_ = saveCurrent(ref)
		return ref, args[1:], nil
	}
	ref, err := loadCurrent()
	return ref, args, err
}

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

	// Preserve any locally staged drafts/replies across a re-fetch.
	s := &State{Ref: ref}
	if prior, err := loadState(ref); err == nil {
		s.Drafts = prior.Drafts
		mergePending(rev.Threads, prior.Threads)
	}
	s.Branch = rev.Branch
	s.BaseSHA = rev.BaseSHA
	s.StartSHA = rev.StartSHA
	s.HeadAtFetch = rev.HeadSHA
	s.Threads = rev.Threads
	s.Notes = rev.Notes

	if err := saveState(s); err != nil {
		return err
	}
	if err := saveCurrent(ref); err != nil {
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
	rest := args[:0:0]
	for _, a := range args {
		if a == "--json" {
			asJSON = true
			continue
		}
		rest = append(rest, a)
	}
	ref, _, err := splitRef(rest)
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

func cmdReply(args []string) error {
	ref, rest, err := splitRef(args)
	if err != nil {
		return err
	}
	if len(rest) < 2 {
		return fmt.Errorf("usage: prctx reply [<pr>] <thread-id> <body>")
	}
	threadID := rest[0]
	body := strings.Join(rest[1:], " ")
	s, err := loadState(ref)
	if err != nil {
		return err
	}
	t := findThread(s, threadID)
	if t == nil {
		return fmt.Errorf("no thread %q in local state (run `prctx show`)", threadID)
	}
	t.PendingReply = body
	if err := saveState(s); err != nil {
		return err
	}
	fmt.Printf("staged reply to thread %s\n", threadID)
	return nil
}

func cmdComment(args []string) error {
	ref, rest, err := splitRef(args)
	if err != nil {
		return err
	}
	if len(rest) < 2 {
		return fmt.Errorf("usage: prctx comment [<pr>] <file>:<line> <body>")
	}
	path, line, err := parseFileLine(rest[0])
	if err != nil {
		return err
	}
	body := strings.Join(rest[1:], " ")
	s, err := loadState(ref)
	if err != nil {
		return err
	}
	d := Draft{ID: nextDraftID(s), Path: path, Line: line, Body: body}
	s.Drafts = append(s.Drafts, d)
	if err := saveState(s); err != nil {
		return err
	}
	fmt.Printf("staged comment %s -> %s:%d\n", d.ID, path, line)
	return nil
}

func cmdResolve(args []string) error {
	ref, rest, err := splitRef(args)
	if err != nil {
		return err
	}
	if len(rest) != 1 {
		return fmt.Errorf("usage: prctx resolve [<pr>] <thread-id>")
	}
	s, err := loadState(ref)
	if err != nil {
		return err
	}
	t := findThread(s, rest[0])
	if t == nil {
		return fmt.Errorf("no thread %q in local state", rest[0])
	}
	t.PendingResolve = true
	if err := saveState(s); err != nil {
		return err
	}
	fmt.Printf("staged resolve for thread %s\n", rest[0])
	return nil
}

func cmdDrop(args []string) error {
	ref, rest, err := splitRef(args)
	if err != nil {
		return err
	}
	if len(rest) != 1 {
		return fmt.Errorf("usage: prctx drop [<pr>] <thread-id|draft-id>")
	}
	id := rest[0]
	s, err := loadState(ref)
	if err != nil {
		return err
	}
	if t := findThread(s, id); t != nil {
		t.PendingReply = ""
		t.PendingResolve = false
		if err := saveState(s); err != nil {
			return err
		}
		fmt.Printf("dropped staged reply/resolve on thread %s\n", id)
		return nil
	}
	for i, d := range s.Drafts {
		if d.ID == id {
			s.Drafts = append(s.Drafts[:i], s.Drafts[i+1:]...)
			if err := saveState(s); err != nil {
				return err
			}
			fmt.Printf("dropped draft %s\n", id)
			return nil
		}
	}
	return fmt.Errorf("no thread or draft %q in local state", id)
}

func cmdFlush(args []string) error {
	force := false
	rest := args[:0:0]
	for _, a := range args {
		if a == "--force" {
			force = true
			continue
		}
		rest = append(rest, a)
	}
	ref, rest, err := splitRef(rest)
	if err != nil {
		return err
	}
	if len(rest) != 0 {
		return fmt.Errorf("usage: prctx flush [<pr>] [--force]")
	}
	prov, err := providerFor(ref)
	if err != nil {
		return err
	}
	s, err := loadState(ref)
	if err != nil {
		return err
	}
	return flush(os.Stdout, prov, s, force)
}

func cmdVerdict(args []string, approve bool) error {
	var body string
	rest := args[:0:0]
	for i := 0; i < len(args); i++ {
		if args[i] == "--body" && i+1 < len(args) {
			body = args[i+1]
			i++
			continue
		}
		rest = append(rest, args[i])
	}
	ref, rest, err := splitRef(rest)
	if err != nil {
		return err
	}
	if len(rest) != 0 {
		return fmt.Errorf("unexpected arguments: %v", rest)
	}
	prov, err := providerFor(ref)
	if err != nil {
		return err
	}
	if approve {
		if err := prov.Approve(ref, body); err != nil {
			return err
		}
		fmt.Printf("approved %s/%s#%d\n", ref.Owner, ref.Repo, ref.Number)
		return nil
	}
	if err := prov.Reject(ref, body); err != nil {
		return err
	}
	fmt.Printf("requested changes on %s/%s#%d\n", ref.Owner, ref.Repo, ref.Number)
	return nil
}

func findThread(s *State, id string) *Thread {
	for i := range s.Threads {
		if s.Threads[i].ID == id {
			return &s.Threads[i]
		}
	}
	return nil
}

// parseFileLine splits "path/to/file:123" into ("path/to/file", 123).
func parseFileLine(arg string) (string, int, error) {
	i := strings.LastIndex(arg, ":")
	if i < 0 {
		return "", 0, fmt.Errorf("expected <file>:<line>, got %q", arg)
	}
	line, err := strconv.Atoi(arg[i+1:])
	if err != nil {
		return "", 0, fmt.Errorf("invalid line number in %q: %w", arg, err)
	}
	return arg[:i], line, nil
}

// nextDraftID returns the next free draft id (d1, d2, ...).
func nextDraftID(s *State) string {
	max := 0
	for _, d := range s.Drafts {
		if strings.HasPrefix(d.ID, "d") {
			if n, err := strconv.Atoi(d.ID[1:]); err == nil && n > max {
				max = n
			}
		}
	}
	return "d" + strconv.Itoa(max+1)
}
