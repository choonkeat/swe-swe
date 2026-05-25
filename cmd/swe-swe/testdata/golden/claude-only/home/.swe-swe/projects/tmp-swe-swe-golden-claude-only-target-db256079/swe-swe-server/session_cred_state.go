// session_cred_state.go -- connect-time + on-change snapshot of the
// per-session credential / signing state.
//
// The signing key auto-wires on reconnect (the browser re-sends it under
// a per-device trust gate), and its "On" badge is driven by server acks.
// HTTPS PAT + author identity historically did NOT push their state on
// connect: the panel only learned the truth after a manual Save. This
// snapshot closes that gap -- the server tells every connecting (and, on
// change, every already-connected) browser exactly what it holds, so the
// Settings panel reflects real server state without a "Save just in case".
//
// The snapshot carries no secrets: only host names, whether an author
// email is set, the signing-key fingerprint (already user-facing), any
// local .git/config signing overrides, and the computed signing-active
// verdict + reason.
package main

import "sync"

// sessionCredStateMu makes the compound credential update in the
// set_credentials handler (creds + author + signing key, three separate
// store maps) atomic with respect to a snapshot read, so a snapshot can
// never observe a half-applied update (e.g. new author but old key).
// Held only around the in-memory store reads/writes -- never across a
// file write or a network send.
var sessionCredStateMu sync.Mutex

// computeSigningState derives whether SSH commit signing will actually
// verify locally, and if not, the single most actionable reason. Pure
// function so it is table-testable.
//
// Priority order: a missing key blocks everything; a local .git/config
// signing override silently wins over the per-session GIT_CONFIG_GLOBAL
// so it is the next most important to surface; then a missing principal
// email (without which no allowed_signers file can be written and
// verification fails). "passphrase needed this session" is a browser-only
// state (the key simply is not in the server store) and is supplied by
// the frontend, not here.
func computeSigningState(hasKey, emailResolvable bool, localOverrides string) (active bool, reason string) {
	switch {
	case !hasKey:
		return false, "no signing key"
	case localOverrides != "":
		return false, "local .git/config override"
	case !emailResolvable:
		return false, "no email"
	default:
		return true, ""
	}
}

// buildSessionCredState assembles the session_cred_state message for a
// session. It snapshots the three in-memory stores under
// sessionCredStateMu so the verdict cannot reflect a half-applied
// compound update. readLocalSigningOverrides is a pure .git/config file
// read (no subprocess), safe to run while holding the lock.
//
// emailResolvable is the session author email OR (Phase 2) the workdir's
// effective git email, so a repo with a local identity but no Save still
// reads as signing-active. cachedEffectiveGitEmail forks git at most once
// per session and is called outside sessionCredStateMu.
func buildSessionCredState(sid, workDir string) map[string]any {
	sessionCredStateMu.Lock()
	hosts := listCredentialHosts(sid)
	author, _ := getAuthor(sid)
	key, hasKey := getSigningKey(sid)
	sessionCredStateMu.Unlock()

	fingerprint := ""
	if hasKey {
		fingerprint = key.Fingerprint
	}
	authorEmailSet := author.Email != ""
	emailResolvable := authorEmailSet || cachedEffectiveGitEmail(sid, workDir) != ""
	localOverrides := readLocalSigningOverrides(workDir)
	active, reason := computeSigningState(hasKey, emailResolvable, localOverrides)

	return map[string]any{
		"type":                    "session_cred_state",
		"hosts":                   hosts,
		"author_email_set":        authorEmailSet,
		"signing_fingerprint":     fingerprint,
		"local_gpg_overrides":     localOverrides,
		"signing_active":          active,
		"signing_inactive_reason": reason,
	}
}
