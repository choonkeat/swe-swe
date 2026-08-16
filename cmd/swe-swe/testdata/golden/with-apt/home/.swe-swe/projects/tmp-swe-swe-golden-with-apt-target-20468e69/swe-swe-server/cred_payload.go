// cred_payload.go -- flattening a set_credentials payload into the per-host
// credential updates it asks for.
//
// A set_credentials message used to carry exactly one host. That made the
// browser's session-start auto-restore pick ONE host (the workspace's origin
// remote) and silently drop every other token it held, so a user whose PAT
// was saved under a different forge saw their credentials "wiped" on every
// new session.
//
// The browser now sends every host it holds in a single message:
// `additional_hosts` rides alongside the primary one. It has to be one
// message rather than N, because each set_credentials also sets the session
// author and rewrites the per-session gitconfig -- N messages would rewrite N
// times and (with the author only on the first) blank the identity on the
// rest.
package main

import "strings"

// maxAdditionalCredHosts caps how many extra hosts one message may carry, so
// a browser that has accumulated tokens for many forges cannot turn every
// session start into an unbounded write loop. Mirrors MAX_ADDITIONAL_HOSTS in
// static/modules/cred-autosend.js.
const maxAdditionalCredHosts = 16

// additionalHostCred is one extra (host, credentials) pair in a
// set_credentials payload. Author identity and signing key are session-level
// and stay on the top-level payload.
type additionalHostCred struct {
	Host     string `json:"host"`
	Username string `json:"username"`
	Token    string `json:"token"`
}

// hostCredential is a resolved credential update ready for setCredential.
type hostCredential struct {
	Host string
	Bag  CredentialBag
}

// credentialUpdates flattens a set_credentials payload into the ordered list
// of per-host updates to apply. The primary host always comes first and is
// always included (an empty token there is the caller clearing it, which the
// manual Save path relies on). Additional hosts are trimmed, and any with no
// host, no token, or a host already seen are dropped -- first occurrence wins,
// so a replayed or malformed tail can never overwrite the primary entry.
func credentialUpdates(host, username, token string, additional []additionalHostCred) []hostCredential {
	primary := strings.TrimSpace(host)
	if primary == "" {
		primary = "github.com"
	}
	out := []hostCredential{{
		Host: primary,
		Bag: CredentialBag{
			Username: strings.TrimSpace(username),
			Token:    token,
		},
	}}
	seen := map[string]bool{primary: true}
	for _, a := range additional {
		if len(out) > maxAdditionalCredHosts {
			break
		}
		h := strings.TrimSpace(a.Host)
		if h == "" || a.Token == "" || seen[h] {
			continue
		}
		seen[h] = true
		out = append(out, hostCredential{
			Host: h,
			Bag: CredentialBag{
				Username: strings.TrimSpace(a.Username),
				Token:    a.Token,
			},
		})
	}
	return out
}
