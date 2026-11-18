package main

import (
	"fmt"
	"strings"
)

// Single-port mode: the operator declares that this box is reachable on ONE
// port and nothing else.
//
// Every pane already has a same-origin path form served by the main listener
// (/proxy/{uuid}/preview, /agentchat, /files, /vnc) and the frontend finds it
// by probing the per-port listener first and falling back. That discovery is
// what an ordinary box relies on and it is not going away -- see
// tasks/2026-09-22-path-based-agent-view-and-single-port-option.md.
//
// What discovery cannot do is stop the per-session proxy listeners from
// EXISTING. startProxyListener binds :port on all interfaces, 20 slots x 4
// bands, so a box that can only ever be reached on one port still carries 80
// externally-bound listeners that nothing can reach: pure attack surface.
// It also cannot make the probe free -- a firewall that DROPs rather than
// REFUSEs leaves each pane waiting out PROBE_TIMEOUT (5s,
// static/modules/proxy-base.js) before it falls back, four times over.
//
// An operator who knows the answer says so once, and swe-swe skips both:
// no per-port listeners, and no proxy ports advertised to the frontend, which
// is what makes every pane resolve straight to its path form with no probe.
// See tasks/2026-09-22-single-port-mode-flag.md.

// singlePortMode is the resolved setting. Written once during main() before
// any listener starts, then only read -- the same discipline as
// configuredPublicHostname.
var singlePortMode bool

// resolveSinglePort folds the -single-port flag and the SWE_SINGLE_PORT env
// var into one value, using the same precedence as resolvePublicHostname: an
// explicitly passed flag wins over the env, and the env wins over the default.
//
// Only 1/true/yes/on (any case, surrounded by whitespace or not) turn the mode
// ON. Anything else -- including 0, false, no and the empty string -- leaves it
// off, because a variable that is merely present and empty is how a compose
// passthrough writes "not set" (SWE_SINGLE_PORT=${SWE_SINGLE_PORT:-}).
func resolveSinglePort(flagVal bool, flagSet bool, lookupEnv func(string) (string, bool)) bool {
	if flagSet {
		return flagVal
	}
	if v, ok := lookupEnv("SWE_SINGLE_PORT"); ok {
		return singlePortEnvTruthy(v)
	}
	return flagVal
}

// singlePortEnvTruthy is the env spelling accepted for the mode.
func singlePortEnvTruthy(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "1", "true", "yes", "on":
		return true
	}
	return false
}

// singlePortConflicts refuses the combinations that cannot work, naming BOTH
// settings so the operator knows which two to choose between.
//
// Wildcard mode (-public-hostname) addresses every pane as {proxyPort}.{apex}
// and its demuxer forwards to 127.0.0.1:{proxyPort}; tunnel mode has tunneld
// dial those same per-port listeners. Single-port mode removes the listeners
// on both ends of that, so either combination would start cleanly and then
// serve nothing -- exactly the half-working state worth dying at boot over.
func singlePortConflicts(singlePort bool, publicHostname, tunnelServerURL string) error {
	if !singlePort {
		return nil
	}
	var both []string
	if strings.TrimSpace(publicHostname) != "" {
		both = append(both, "-public-hostname")
	}
	if strings.TrimSpace(tunnelServerURL) != "" {
		both = append(both, "-tunnel-server-url")
	}
	if len(both) == 0 {
		return nil
	}
	return fmt.Errorf("-single-port cannot be combined with %s: %s reaches every pane through this box's per-session proxy ports, and -single-port is the instruction not to open them. Drop one of the two",
		strings.Join(both, " or "), strings.Join(both, "/"))
}
