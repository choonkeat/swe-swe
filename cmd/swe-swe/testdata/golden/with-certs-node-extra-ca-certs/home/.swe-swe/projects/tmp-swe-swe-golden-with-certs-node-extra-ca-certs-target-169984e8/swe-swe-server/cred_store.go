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

import "sync"

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
