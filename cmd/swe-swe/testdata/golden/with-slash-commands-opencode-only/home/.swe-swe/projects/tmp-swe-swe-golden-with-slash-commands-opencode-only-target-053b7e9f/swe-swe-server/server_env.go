// server_env.go -- in-memory server-wide environment variables.
//
// The homepage Settings dialog holds a "Session environment" textarea whose
// KEY=VALUE blob is POSTed here and exported to every session spawned
// afterwards (buildSessionEnv). It is the server-wide counterpart of the
// per-session repo env (env_store.go) and of the checked-in .swe-swe/env, and
// it ranks below both: a session-scoped value or a .swe-swe/env line wins on
// collision.
//
// Memory-only, never written to disk, never logged. The browser keeps the raw
// blob in localStorage (per origin) and re-sends it on page load, so an
// ephemeral host gets its env back after a reload without re-entry.
//
// Tunnel secrets are deliberately NOT accepted here: SWE_TUNNEL_* keys are
// server-process configuration (see tunnel_runtime.go) and must never reach
// an agent's environment, so they are dropped and reported back to the UI.
package main

import (
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"sync"
)

var (
	serverEnvRaw string
	serverEnvMu  sync.RWMutex
)

// setServerEnv replaces the stored blob. A blank blob clears it.
func setServerEnv(raw string) {
	serverEnvMu.Lock()
	defer serverEnvMu.Unlock()
	if isBlank(raw) {
		serverEnvRaw = ""
		return
	}
	serverEnvRaw = raw
}

func getServerEnvRaw() string {
	serverEnvMu.RLock()
	defer serverEnvMu.RUnlock()
	return serverEnvRaw
}

// isServerEnvReservedKey extends the per-session reserved set with the tunnel
// namespace: the server-wide pane feeds agents, and tunnel secrets must not.
func isServerEnvReservedKey(k string) bool {
	return isReservedEnvKey(k) || strings.HasPrefix(k, "SWE_TUNNEL_")
}

// serverEnvVars parses the stored blob into KEY=VALUE entries for a session
// being built, dropping reserved keys (returned separately). Values expand
// $VAR against lookup, the session env built so far. nil,nil when unset.
func serverEnvVars(lookup func(string) string) (kept, dropped []string) {
	raw := getServerEnvRaw()
	if raw == "" {
		return nil, nil
	}
	kept = parseEnvLines(raw, lookup, isServerEnvReservedKey, &dropped)
	return kept, dropped
}

// serverEnvCount is how many exportable vars are stored; never the values.
func serverEnvCount() int {
	kept, _ := serverEnvVars(os.Getenv)
	return len(kept)
}

// handleServerEnvAPI serves /api/server/env for the homepage Settings dialog.
//
//	GET  -> {"count": n}                       (values are never returned)
//	POST {"raw": "KEY=VALUE\n..."} -> {"count": n, "dropped": [reserved keys]}
//
// Cookie-gated like the other /api/server/* routes and denied to shared-session
// guests (scopedPathAllowed).
func handleServerEnvAPI(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	switch r.Method {
	case http.MethodGet:
		json.NewEncoder(w).Encode(map[string]any{"count": serverEnvCount()})
	case http.MethodPost:
		var payload struct {
			Raw string `json:"raw"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&payload); err != nil {
			http.Error(w, `{"error":"invalid JSON body"}`, http.StatusBadRequest)
			return
		}
		setServerEnv(payload.Raw)
		kept, dropped := serverEnvVars(os.Getenv)
		if dropped == nil {
			dropped = []string{}
		}
		json.NewEncoder(w).Encode(map[string]any{"count": len(kept), "dropped": dropped})
	default:
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
	}
}
