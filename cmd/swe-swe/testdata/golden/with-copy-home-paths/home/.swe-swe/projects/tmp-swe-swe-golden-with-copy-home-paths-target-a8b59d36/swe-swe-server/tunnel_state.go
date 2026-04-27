package main

import (
	"encoding/json"
	"fmt"
	"os"
)

// tunnelState matches the JSON written by swe-swe-tunnel after the tunnel
// client establishes a session. Schema documented in the swe-swe-tunnel
// repo (internal/tunnelclient/state.go). Only "hostname" is consumed
// today; "unique" and "registered_at" are kept on the wire for future
// staleness checks but not parsed here.
type tunnelState struct {
	Hostname string `json:"hostname"`
}

// readTunnelStateHostname reads the JSON state file produced by
// swe-swe-tunnel and returns the hostname field. Errors are returned
// verbatim so the caller can distinguish "no tunnel running" (ENOENT)
// from "stale/corrupt state file" (parse error). A missing file is the
// normal case off the tunnel path -- callers should treat ENOENT as
// "fall back to legacy mode" without logging.
func readTunnelStateHostname(path string) (string, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	var st tunnelState
	if err := json.Unmarshal(raw, &st); err != nil {
		return "", fmt.Errorf("parse tunnel state %q: %w", path, err)
	}
	if st.Hostname == "" {
		return "", fmt.Errorf("tunnel state %q has empty hostname field", path)
	}
	return st.Hostname, nil
}
