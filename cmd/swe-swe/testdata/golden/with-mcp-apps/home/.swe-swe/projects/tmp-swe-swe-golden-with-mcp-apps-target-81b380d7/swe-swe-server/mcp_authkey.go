// mcp_authkey.go -- per-session MCP auth keys.
//
// Every session is issued its own random auth key at spawn time, injected
// into the agent's environment as MCP_AUTH_KEY. The orchestration /mcp
// endpoint and the per-session HTTP APIs (autocomplete, browser start)
// authenticate by looking the presented key up in mcpKeyToSession, which
// both authorizes the request AND identifies the calling session.
//
// This is what lets create_session inherit the *calling* session's git
// credentials safely: the parent identity is derived from the unforgeable
// per-session key, never from a client-supplied argument. An agent only
// ever holds its own key, so it can only ever act as itself -- it cannot
// name another session to steal that session's PAT or signing key.
package main

import (
	"context"
	crypto_rand "crypto/rand"
	"encoding/hex"
	"net/http"
	"sync"
)

var (
	mcpKeyToSession = map[string]string{} // key -> session UUID
	mcpSessionToKey = map[string]string{} // session UUID -> key
	mcpKeyMu        sync.RWMutex
)

// issueSessionKey returns the per-session MCP auth key for sid, generating
// (and recording) one on first call. Idempotent per sid so a session that
// is recreated under the same UUID keeps a stable key.
func issueSessionKey(sid string) string {
	if sid == "" {
		return ""
	}
	mcpKeyMu.Lock()
	defer mcpKeyMu.Unlock()
	if k, ok := mcpSessionToKey[sid]; ok {
		return k
	}
	buf := make([]byte, 32)
	crypto_rand.Read(buf)
	k := hex.EncodeToString(buf)
	mcpKeyToSession[k] = sid
	mcpSessionToKey[sid] = k
	return k
}

// sessionForKey maps a presented MCP auth key back to the session UUID it
// was issued to. ok is false for an empty or unrecognized key.
func sessionForKey(key string) (string, bool) {
	if key == "" {
		return "", false
	}
	mcpKeyMu.RLock()
	defer mcpKeyMu.RUnlock()
	sid, ok := mcpKeyToSession[key]
	return sid, ok
}

// clearSessionKey drops a session's auth-key mapping on session end.
func clearSessionKey(sid string) {
	if sid == "" {
		return
	}
	mcpKeyMu.Lock()
	defer mcpKeyMu.Unlock()
	if k, ok := mcpSessionToKey[sid]; ok {
		delete(mcpKeyToSession, k)
		delete(mcpSessionToKey, sid)
	}
}

// callerSessionCtxKey is the context key under which the authenticated
// calling-session UUID is stored for orchestration MCP tool handlers.
type callerSessionCtxKey struct{}

func withCallerSession(ctx context.Context, sid string) context.Context {
	return context.WithValue(ctx, callerSessionCtxKey{}, sid)
}

// callerSessionFromContext returns the authenticated calling-session UUID
// injected by mcpAuthMiddleware, or "" when the request did not pass
// through that middleware.
func callerSessionFromContext(ctx context.Context) string {
	sid, _ := ctx.Value(callerSessionCtxKey{}).(string)
	return sid
}

// mcpAuthMiddleware authenticates orchestration /mcp requests by their
// per-session key and injects the resolved caller session UUID into the
// request context so tool handlers (create_session) can act on behalf of
// the caller. Requests with a missing or unknown key are rejected.
func mcpAuthMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sid, ok := sessionForKey(r.URL.Query().Get("key"))
		if !ok {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r.WithContext(withCallerSession(r.Context(), sid)))
	})
}

// sessionKeyMatchesPath authorizes a per-session HTTP API request: the
// presented key must map to the same session UUID that appears in the
// request path. This scopes autocomplete / browser-start so one session
// can never drive another's.
func sessionKeyMatchesPath(r *http.Request, pathUUID string) bool {
	sid, ok := sessionForKey(r.URL.Query().Get("key"))
	return ok && sid == pathUUID
}
