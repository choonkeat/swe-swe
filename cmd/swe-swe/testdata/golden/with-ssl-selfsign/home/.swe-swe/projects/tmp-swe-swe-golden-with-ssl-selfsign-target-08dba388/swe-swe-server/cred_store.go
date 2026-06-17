// cred_store.go -- in-memory per-session credential store.
//
// Populated by the WebSocket handler when the browser sends a
// "set_credentials" message (already scoped to the session via the WS
// auth). Read by the broker's "get" op when the credential helper inside
// the container asks for a host's credentials.
//
// Lifecycle: cleared when the session ends (killSessionProcessGroup).
// Survival across server restarts is out of scope for v1; users re-enter.
//
// Browser-write-only: there is no API for reading credentials back out
// to the browser. Once stored, credentials only flow OUT to git via the
// broker socket.
package main

import (
	"log"
	"sync"
)

// CredentialBag is what the server stores per (session, host).
type CredentialBag struct {
	Username string // HTTPS basic-auth username (defaults to "x-access-token" when served if empty)
	Token    string // HTTPS basic-auth password / PAT (the secret)
}

// AuthorIdent is the git commit identity for a session. Lives at the
// session level (not per-host) since author identity isn't host-specific.
type AuthorIdent struct {
	Name  string
	Email string
}

var (
	// sessionCreds[sid][host] -> CredentialBag
	sessionCreds   = map[string]map[string]CredentialBag{}
	sessionCredsMu sync.RWMutex

	// sessionAuthor[sid] -> AuthorIdent. Written by the WS set_credentials
	// handler; consumed by writeSessionGitconfig to populate the per-session
	// GIT_CONFIG_GLOBAL file.
	sessionAuthor   = map[string]AuthorIdent{}
	sessionAuthorMu sync.RWMutex
)

func setCredential(sid, host string, c CredentialBag) {
	if sid == "" || host == "" {
		return
	}
	sessionCredsMu.Lock()
	defer sessionCredsMu.Unlock()
	if sessionCreds[sid] == nil {
		sessionCreds[sid] = map[string]CredentialBag{}
	}
	sessionCreds[sid][host] = c
}

func getCredential(sid, host string) (CredentialBag, bool) {
	sessionCredsMu.RLock()
	defer sessionCredsMu.RUnlock()
	if m, ok := sessionCreds[sid]; ok {
		if c, ok := m[host]; ok {
			return c, true
		}
	}
	return CredentialBag{}, false
}

func clearSessionCredentials(sid string) {
	if sid == "" {
		return
	}
	sessionCredsMu.Lock()
	delete(sessionCreds, sid)
	sessionCredsMu.Unlock()

	sessionAuthorMu.Lock()
	delete(sessionAuthor, sid)
	sessionAuthorMu.Unlock()

	clearSigningKey(sid)
	clearSessionEffectiveEmail(sid)
}

func setAuthor(sid string, ident AuthorIdent) {
	if sid == "" {
		return
	}
	sessionAuthorMu.Lock()
	defer sessionAuthorMu.Unlock()
	if ident.Name == "" && ident.Email == "" {
		delete(sessionAuthor, sid)
		return
	}
	sessionAuthor[sid] = ident
}

func getAuthor(sid string) (AuthorIdent, bool) {
	sessionAuthorMu.RLock()
	defer sessionAuthorMu.RUnlock()
	a, ok := sessionAuthor[sid]
	return a, ok
}

// inheritSessionCredentials copies a parent session's git auth state --
// HTTPS credentials (per host), author identity, and SSH signing key --
// onto a freshly created child session, then regenerates the child's
// per-session gitconfig so commit signing and credential-helper lookups
// work immediately without the user re-entering anything.
//
// Copying is strictly one-way (parent -> child). The caller MUST have
// authenticated the parent identity; create_session derives it from the
// unforgeable per-session MCP auth key (see mcp_authkey.go), never from a
// client-supplied argument. No-op when the parent is empty/unknown or when
// parent and child are the same session.
func inheritSessionCredentials(parentUUID, childUUID, childWorkDir string) {
	if parentUUID == "" || childUUID == "" || parentUUID == childUUID {
		return
	}

	// Snapshot the parent's per-host credentials under the read lock, then
	// write them to the child outside it (setCredential takes the write lock).
	sessionCredsMu.RLock()
	var hosts map[string]CredentialBag
	if m, ok := sessionCreds[parentUUID]; ok {
		hosts = make(map[string]CredentialBag, len(m))
		for h, c := range m {
			hosts[h] = c
		}
	}
	sessionCredsMu.RUnlock()

	inherited := false
	for h, c := range hosts {
		setCredential(childUUID, h, c)
		inherited = true
	}
	if a, ok := getAuthor(parentUUID); ok {
		setAuthor(childUUID, a)
		inherited = true
	}
	if k, ok := getSigningKey(parentUUID); ok {
		setSigningKey(childUUID, k)
		inherited = true
	}
	if !inherited {
		return
	}

	// Regenerate the child gitconfig so the inherited author/signing key
	// take effect. The env-build step already wrote a baseline file; this
	// layers the inherited [user]/signing config on top.
	if err := writeSessionGitconfig(childUUID, childWorkDir); err != nil {
		log.Printf("Session %s: gitconfig rewrite after credential inheritance failed: %v", childUUID, err)
	}
	log.Printf("Session %s inherited git credentials/signing from parent %s", childUUID, parentUUID)
}

// listCredentialHosts returns the hosts for which sid has credentials.
// Returned slice may be empty. Used by the UI to show "credentials set
// for X" without revealing the secret values.
func listCredentialHosts(sid string) []string {
	sessionCredsMu.RLock()
	defer sessionCredsMu.RUnlock()
	m, ok := sessionCreds[sid]
	if !ok {
		return nil
	}
	hosts := make([]string, 0, len(m))
	for h := range m {
		hosts = append(hosts, h)
	}
	return hosts
}
