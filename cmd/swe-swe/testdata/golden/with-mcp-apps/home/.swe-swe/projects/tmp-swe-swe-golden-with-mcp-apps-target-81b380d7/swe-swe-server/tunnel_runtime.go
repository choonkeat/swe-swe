// tunnel_runtime.go -- runtime (re)configuration of the tunnel supervisor.
//
// Boot-time flags/env are one way to configure the tunnel; on an ephemeral
// host with no place for secrets the operator instead pastes the tunnel
// config into the homepage Settings dialog after boot. POST /api/server/tunnel
// takes the server URL, unique and identity key, stops any running supervisor
// and starts a fresh one; GET reports state and the public URL so the homepage
// can show "connecting" and then the link.
//
// The identity key is handed to the swe-swe-tunnel child through its own
// environment only (startExecChild). It is never logged and never enters a
// session's environment: buildSessionEnv strips SWE_TUNNEL_IDENTITY_KEY.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"net/url"
	"strings"
	"sync"
)

// tunnelRuntime owns the supervisor lifecycle so a later configure can cancel
// the previous instance. base carries the boot-time, non-secret settings
// (binary path, listen address, client cert) every instance shares.
type tunnelRuntime struct {
	mu         sync.Mutex
	base       tunnelSupervisorOpts
	cancel     context.CancelFunc
	configured bool
	serverURL  string
	unique     string
}

var liveTunnelRuntime = &tunnelRuntime{}

// initTunnelRuntime records the shared, non-secret supervisor settings.
// Called once from main before any start.
func initTunnelRuntime(base tunnelSupervisorOpts) {
	liveTunnelRuntime.mu.Lock()
	defer liveTunnelRuntime.mu.Unlock()
	liveTunnelRuntime.base = base
}

// start launches a supervisor for opts under a cancellable child of parent,
// replacing (cancelling) any instance already running. Non-secret fields of
// opts are recorded for the status snapshot.
func (rt *tunnelRuntime) start(parent context.Context, opts tunnelSupervisorOpts) {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	if rt.cancel != nil {
		rt.cancel()
		rt.cancel = nil
	}
	// A fresh instance starts from a clean slate: the previous child's
	// hostname and terminal status must not linger as if they were ours.
	setLiveTunnelHostname("")
	setLiveTunnelStatus(tunnelStatusInfo{State: "connecting"})
	broadcastPublicHostnameChange()

	ctx, cancel := context.WithCancel(parent)
	rt.cancel = cancel
	rt.configured = true
	rt.serverURL = opts.ServerURL
	rt.unique = opts.Unique
	// Never carry a previous instance's fatal marker into the new one.
	opts.fatalReason = nil
	go runTunnelSupervisor(ctx, opts)
}

// validateTunnelConfig checks the operator-supplied fields. The identity key
// is optional (empty = the child's own resolution: env or key file).
func validateTunnelConfig(serverURL, unique string) error {
	u, err := url.Parse(strings.TrimSpace(serverURL))
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
		return errors.New("serverUrl must be an http(s) URL")
	}
	unique = strings.TrimSpace(unique)
	if unique == "" || strings.ContainsAny(unique, " \t\n\r/.") {
		return errors.New("unique must be a bare label (no spaces, dots or slashes)")
	}
	return nil
}

// configure validates and (re)starts the supervisor with the given config.
func (rt *tunnelRuntime) configure(serverURL, unique, identityKey string) error {
	if err := validateTunnelConfig(serverURL, unique); err != nil {
		return err
	}
	rt.mu.Lock()
	opts := rt.base
	rt.mu.Unlock()
	if opts.BinPath == "" {
		return errors.New("tunnel client binary is not configured on this server (-tunnel-bin)")
	}
	opts.ServerURL = strings.TrimSpace(serverURL)
	opts.Unique = strings.TrimSpace(unique)
	opts.IdentityKey = strings.TrimSpace(identityKey)
	keySource := "inherited"
	if opts.IdentityKey != "" {
		keySource = "provided"
	}
	log.Printf("[tunnel] runtime configure: server=%s unique=%s identity_key=%s", opts.ServerURL, opts.Unique, keySource)
	rt.start(context.Background(), opts)
	return nil
}

// tunnelRuntimeStatus is the GET /api/server/tunnel shape. Never carries the
// identity key.
type tunnelRuntimeStatus struct {
	Configured   bool   `json:"configured"`
	ServerURL    string `json:"serverUrl,omitempty"`
	Unique       string `json:"unique,omitempty"`
	State        string `json:"state,omitempty"`
	Reason       string `json:"reason,omitempty"`
	RetryAfterMs int64  `json:"retryAfterMs,omitempty"`
	Hostname     string `json:"hostname,omitempty"`
	URL          string `json:"url,omitempty"`
}

func (rt *tunnelRuntime) snapshot() tunnelRuntimeStatus {
	rt.mu.Lock()
	s := tunnelRuntimeStatus{Configured: rt.configured, ServerURL: rt.serverURL, Unique: rt.unique}
	localAddr := rt.base.LocalAddr
	rt.mu.Unlock()
	ts := getLiveTunnelStatus()
	s.State, s.Reason, s.RetryAfterMs = ts.State, ts.Reason, ts.RetryAfterMs
	s.Hostname = getLiveTunnelHostname()
	s.URL = tunnelPublicURL(localAddr, s.Hostname)
	return s
}

// tunnelPublicURL is the operator-facing URL for a registered tunnel: tunneld
// demuxes {port}.{hostname} onto the server's listen port.
func tunnelPublicURL(localAddr, hostname string) string {
	if hostname == "" {
		return ""
	}
	return "https://" + supervisorOpenPort(localAddr) + "." + hostname + "/"
}

// handleServerTunnelAPI serves /api/server/tunnel.
//
//	GET  -> tunnelRuntimeStatus
//	POST {"serverUrl","unique","identityKey"} -> tunnelRuntimeStatus (202)
func handleServerTunnelAPI(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	switch r.Method {
	case http.MethodGet:
		json.NewEncoder(w).Encode(liveTunnelRuntime.snapshot())
	case http.MethodPost:
		var payload struct {
			ServerURL   string `json:"serverUrl"`
			Unique      string `json:"unique"`
			IdentityKey string `json:"identityKey"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&payload); err != nil {
			http.Error(w, `{"error":"invalid JSON body"}`, http.StatusBadRequest)
			return
		}
		if err := liveTunnelRuntime.configure(payload.ServerURL, payload.Unique, payload.IdentityKey); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
			return
		}
		w.WriteHeader(http.StatusAccepted)
		json.NewEncoder(w).Encode(liveTunnelRuntime.snapshot())
	default:
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
	}
}

// tunnelChildEnv builds the tunnel child's environment: base with any existing
// SWE_TUNNEL_IDENTITY_KEY replaced by key. Only the child sees it.
func tunnelChildEnv(base []string, key string) []string {
	return append(filterEnv(base, "SWE_TUNNEL_IDENTITY_KEY"), "SWE_TUNNEL_IDENTITY_KEY="+key)
}
