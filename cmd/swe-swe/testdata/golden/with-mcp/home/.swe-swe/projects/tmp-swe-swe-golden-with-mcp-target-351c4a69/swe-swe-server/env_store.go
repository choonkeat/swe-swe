// env_store.go -- in-memory per-session repo environment variables.
//
// Populated by the WebSocket handler when the browser sends a "set_env"
// message (already scoped to the session via the WS auth). The raw KEY=VALUE
// blob is stored verbatim and parsed at spawn time by buildSessionEnv, so
// $VAR references expand against the full session env being built.
//
// Like the credential store, this is memory-only: values are never written to
// disk and never committed. The browser holds the raw blob in localStorage and
// re-sends it under the same per-repo trust gate as the HTTPS PAT, so the vars
// auto-restore on reconnect without re-entry.
//
// Lifecycle: cleared when the session ends (clearSessionCredentials).
package main

import (
	"os"
	"sync"
)

// reservedEnvKeys are managed by swe-swe -- they wire the credential broker,
// the proxies, the ports, the per-session gitconfig, and the theme. The repo
// env-vars textarea must not override them, so they are dropped at parse time
// (and reported back to the UI so the user knows they were ignored).
var reservedEnvKeys = map[string]struct{}{
	"PATH":               {},
	"HOME":               {},
	"TERM":               {},
	"PORT":               {},
	"BROWSER":            {},
	"AGENT_CHAT_PORT":    {},
	"AGENT_CHAT_DISABLE": {},
	"PUBLIC_PORT":        {},
	"BROWSER_CDP_PORT":   {},
	"BROWSER_VNC_PORT":   {},
	"COLORFGBG":          {},
	"GH_TOKEN":           {},
	"GITLAB_TOKEN":       {},
	"GIT_CONFIG_COUNT":   {},
	"GIT_CONFIG_KEY_0":   {},
	"GIT_CONFIG_VALUE_0": {},
	"GIT_CONFIG_GLOBAL":  {},
}

func isReservedEnvKey(k string) bool {
	_, ok := reservedEnvKeys[k]
	return ok
}

var (
	// sessionEnvRaw[sid] -> raw KEY=VALUE blob from the Settings textarea.
	sessionEnvRaw = map[string]string{}
	sessionEnvMu  sync.RWMutex
)

func setSessionEnv(sid, raw string) {
	if sid == "" {
		return
	}
	sessionEnvMu.Lock()
	defer sessionEnvMu.Unlock()
	// A blank/whitespace blob clears the store rather than persisting an
	// empty entry, so "delete everything then Save" behaves as expected.
	if isBlank(raw) {
		delete(sessionEnvRaw, sid)
		return
	}
	sessionEnvRaw[sid] = raw
}

func getSessionEnvRaw(sid string) (string, bool) {
	sessionEnvMu.RLock()
	defer sessionEnvMu.RUnlock()
	raw, ok := sessionEnvRaw[sid]
	return raw, ok
}

func clearSessionEnv(sid string) {
	if sid == "" {
		return
	}
	sessionEnvMu.Lock()
	delete(sessionEnvRaw, sid)
	sessionEnvMu.Unlock()
}

// sessionEnvVars parses the session's stored blob into KEY=VALUE entries,
// dropping reserved keys (returned separately so the UI can report which were
// ignored). Values expand $VAR against `lookup` -- the session env being built
// in buildSessionEnv -- so a var can reference the session PATH etc. Returns
// nil,nil for a session with no stored env.
func sessionEnvVars(sid string, lookup func(string) string) (kept, dropped []string) {
	raw, ok := getSessionEnvRaw(sid)
	if !ok {
		return nil, nil
	}
	kept = parseEnvLines(raw, lookup, isReservedEnvKey, &dropped)
	return kept, dropped
}

// sessionEnvCount reports how many non-reserved vars a session has stored,
// for the Settings nav badge / cred-state snapshot. Never exposes values.
func sessionEnvCount(sid string) int {
	kept, _ := sessionEnvVars(sid, os.Getenv)
	return len(kept)
}

// inheritSessionEnv copies a parent session's raw env blob onto a freshly
// created child (one-way, parent -> child), mirroring
// inheritSessionCredentials so a create_session child gets the same repo env
// without the user re-entering it. No-op when parent is empty/unknown or when
// parent and child are the same session.
func inheritSessionEnv(parentSID, childSID string) {
	if parentSID == "" || childSID == "" || parentSID == childSID {
		return
	}
	if raw, ok := getSessionEnvRaw(parentSID); ok {
		setSessionEnv(childSID, raw)
	}
}

// isBlank reports whether s is empty or only whitespace.
func isBlank(s string) bool {
	for _, r := range s {
		if r != ' ' && r != '\t' && r != '\n' && r != '\r' {
			return false
		}
	}
	return true
}
