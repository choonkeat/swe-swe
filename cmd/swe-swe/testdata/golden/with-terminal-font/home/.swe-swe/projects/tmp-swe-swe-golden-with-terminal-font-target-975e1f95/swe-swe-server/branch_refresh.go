// branch_refresh.go -- the New Session dialog's background branch refresh.
//
// The refresh runs in the server process, which owns no session and therefore
// no credentials: per-session HTTPS tokens live in sessionCreds keyed by
// session id and only reach git through the broker. So a private HTTPS remote
// used to fail every refresh with "Unable to fetch latest changes". The
// browser already holds the user's token (localStorage "swe-swe-creds:<host>",
// the same entry the clone flow writes), so it hands it over on the refresh
// POST and the server borrows it for exactly one `git fetch` via the existing
// transient-credential path.
//
// Two invariants this file exists to keep:
//   - The refresh can never delay creating a session: it is bounded by
//     branchFetchTimeout and always soft-fails to the cached branch list.
//   - The token never leaves the request body: never a query parameter (those
//     land in access logs), never the response, never a log line.
package main

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"
)

// branchFetchTimeout bounds the refresh's `git fetch`. A stalled TLS connect
// otherwise pins a git process and an HTTP request indefinitely, and holds git
// ref locks that make a concurrent `git worktree add` fail. Var, not const, so
// tests can shorten it.
var branchFetchTimeout = 20 * time.Second

// errBranchFetchTimeout marks a refresh cut off by branchFetchTimeout, so the
// warning can say "timed out" instead of blaming credentials.
var errBranchFetchTimeout = errors.New("branch fetch timed out")

// httpsRemoteHost returns the lowercased host of an HTTPS(-ish) git remote,
// or "" for SSH, local, and unparseable remotes. Only HTTP(S) remotes use a
// username/token pair, so only they should make the dialog look for one.
// Lowercased to match the browser's credential key, which
// static/modules/clone-cred-host.js also lowercases.
func httpsRemoteHost(remote string) string {
	remote = strings.TrimSpace(remote)
	lower := strings.ToLower(remote)
	if !strings.HasPrefix(lower, "https://") && !strings.HasPrefix(lower, "http://") {
		return ""
	}
	return strings.ToLower(parseRemoteHost(remote))
}

// repoHTTPSRemoteHost reports the HTTPS host of repoPath's origin remote (""
// when there is none, or it is not HTTPS). Purely local git call.
func repoHTTPSRemoteHost(repoPath string) string {
	originURL, err := getRepoOriginURL(repoPath)
	if err != nil {
		return ""
	}
	return httpsRemoteHost(originURL)
}

// branchRefreshWarning turns a failed refresh into the one line the dialog
// shows. Soft-fail is the contract: the cached branch list is returned
// regardless, so every wording here ends with the user still able to proceed.
func branchRefreshWarning(output string, err error, host string) string {
	if errors.Is(err, errBranchFetchTimeout) {
		return "Refresh timed out. Using cached branches."
	}
	if cloneNeedsAuth(output) {
		if host != "" {
			return "Sign in to refresh branches: HTTPS credentials for " + host + " are missing or rejected. Using cached branches."
		}
		return "Sign in to refresh branches: HTTPS credentials missing or rejected. Using cached branches."
	}
	return "Unable to fetch latest changes. Using cached branches."
}

// runBranchFetch freshens remote refs for repoPath, borrowing the caller's
// HTTPS credentials for the duration of the call. Bounded by
// branchFetchTimeout; a cut-off fetch returns errBranchFetchTimeout so the
// caller can word the warning accordingly. Fetch only -- this endpoint never
// touches the working tree.
//
// Concurrent calls for the same repo share one `git fetch`: two fetches racing
// on the same refs make the loser fail with "cannot lock ref ... is at X but
// expected Y" whenever the remote has new commits, which the dialog showed as
// "Unable to fetch latest changes". Two overlap easily -- aborting the
// browser's request does not stop the server's fetch, so reopening the dialog
// or re-picking the repo starts a second one.
func runBranchFetch(repoPath, credHost, credUsername, credToken string) ([]byte, error) {
	branchFetchMu.Lock()
	if call, ok := branchFetchInFlight[repoPath]; ok {
		branchFetchMu.Unlock()
		<-call.done
		return call.out, call.err
	}
	call := &branchFetchCall{done: make(chan struct{})}
	branchFetchInFlight[repoPath] = call
	branchFetchMu.Unlock()

	call.out, call.err = runBranchFetchOnce(repoPath, credHost, credUsername, credToken)

	branchFetchMu.Lock()
	delete(branchFetchInFlight, repoPath)
	branchFetchMu.Unlock()
	close(call.done)
	return call.out, call.err
}

// branchFetchCall is one in-flight runBranchFetch; out and err are set before
// done is closed.
type branchFetchCall struct {
	done chan struct{}
	out  []byte
	err  error
}

var (
	branchFetchMu       sync.Mutex
	branchFetchInFlight = map[string]*branchFetchCall{}
)

func runBranchFetchOnce(repoPath, credHost, credUsername, credToken string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), branchFetchTimeout)
	defer cancel()

	out, err := runGitWithTransientCredContext(ctx, credHost, credUsername, credToken,
		"-C", repoPath, "fetch", "--all")
	if err != nil && ctx.Err() != nil {
		return out, errBranchFetchTimeout
	}
	return out, err
}
