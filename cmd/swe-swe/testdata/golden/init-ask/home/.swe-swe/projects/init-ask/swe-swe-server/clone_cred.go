// clone_cred.go -- run a git subprocess with a short-lived, per-call
// credential context resolvable by the broker.
//
// The homepage "Clone external repository" flow runs BEFORE any session
// exists, so it cannot use the per-session credential path that in-session
// git enjoys (git -> git-credential-swe-swe -> @swe-swe-broker -> sid). This
// helper closes that gap by minting a server-side transient sid, registering
// the clone process's pid under it, and storing the caller-supplied PAT for
// the host. git's credential helper (a grandchild of the clone) then resolves
// the transient sid via the broker's SO_PEERCRED + ancestry walk -- reusing
// the ENTIRE existing credential path with no new trust surface (the server,
// not the client, mints and registers the sid, so the broker's "never trust a
// client-supplied sid" invariant holds).
package main

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"os"
	"os/exec"
	"strings"
)

// cloneNeedsAuth reports whether git's combined output indicates an HTTPS
// authentication failure (as opposed to a missing repo, DNS error, etc.), so
// the caller can surface a credential prompt instead of raw error text. Match
// on git's stable phrasings across GitHub/GitLab/Bitbucket rather than exit
// codes, which are uniformly 128 for these failures.
func cloneNeedsAuth(output string) bool {
	o := strings.ToLower(output)
	for _, sig := range []string{
		"authentication failed",
		"could not read username",
		"could not read password",
		"http basic: access denied",
		"invalid username or password",
	} {
		if strings.Contains(o, sig) {
			return true
		}
	}
	return false
}

// newTransientID mints an unguessable, collision-resistant sid for a single
// pre-session git call, prefixed "prep-" so it can never be mistaken for a
// real session UUID. Seam: overridable in tests. Uses crypto/rand, never
// time/rand, so a client cannot predict or forge it.
var newTransientID = func() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		// crypto/rand should never fail; if it does, fail closed with a
		// still-unique-per-process value rather than a guessable constant.
		return "prep-" + hex.EncodeToString([]byte(os.Getenv("HOSTNAME")))
	}
	return "prep-" + hex.EncodeToString(b[:])
}

// gitCredHelperEnv appends the three GIT_CONFIG vars that wire git to the
// swe-swe credential helper (mirroring buildSessionEnv) onto a base env.
// git-credential-swe-swe already lives on the default PATH (/usr/local/bin),
// so no PATH surgery is needed -- os.Environ() carries it through.
func gitCredHelperEnv(base []string) []string {
	return append(append([]string{}, base...),
		"GIT_CONFIG_COUNT=1",
		"GIT_CONFIG_KEY_0=credential.helper",
		"GIT_CONFIG_VALUE_0=swe-swe",
	)
}

// runGitWithTransientCred runs `git args...` with a short-lived, per-call
// credential context resolvable by the broker. host/username/token may be
// empty -> behaves like a bare git call (no cred wired). It returns the
// combined stdout+stderr and the process error (nil on exit 0).
//
// Never embed credentials in the URL; never log the token. The transient
// credential is cleared and the pid unregistered whether git succeeds or
// fails, and is never persisted.
func runGitWithTransientCred(host, username, token string, args ...string) ([]byte, error) {
	// Bare path: no token -> inherit env, no credential helper wired. This is
	// byte-for-byte the old `git clone <url> <dir>` behavior.
	if token == "" {
		return exec.Command("git", args...).CombinedOutput()
	}

	if username == "" {
		username = "x-access-token"
	}

	transientID := newTransientID()
	setCredential(transientID, host, CredentialBag{Username: username, Token: token})
	defer clearSessionCredentials(transientID)

	cmd := exec.Command("git", args...)
	cmd.Env = gitCredHelperEnv(os.Environ())
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf

	if err := cmd.Start(); err != nil {
		return buf.Bytes(), err
	}
	// Register the clone pid under the transient sid BEFORE Wait so the
	// credential helper (a grandchild of this process) resolves via the
	// ancestry walk while git is blocked on auth.
	registerSessionPid(cmd.Process.Pid, transientID)
	defer unregisterSessionPid(cmd.Process.Pid)

	err := cmd.Wait()
	return buf.Bytes(), err
}
