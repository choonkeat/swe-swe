package main

import (
	"fmt"
	"net"
	"strings"
)

// Wildcard host-demux, phase 1: the setting and its plumbing.
//
// On a box whose *.example.com DNS points at it, swe-swe can serve every pane
// through its ONE listening port by naming the internal port in the leftmost
// label -- exactly what tunnel mode already does, minus the tunnel:
//
//	http://1977.example.com:1977/    -> the swe-swe UI
//	http://23000.example.com:1977/   -> that session's preview proxy
//
// All of that addressing already exists and hangs off ONE value,
// liveTunnelHostname (tunnel_supervisor.go), which today only the in-process
// tunnel supervisor ever sets. This file lets an operator set the same value
// directly with -public-hostname / SWE_PUBLIC_HOSTNAME, so the cookie domain
// (resolveCookieDomain), the frontend's subdomain URL builders and the landing
// page all light up unchanged.
//
// Phase 1 stops there deliberately. The demuxer that RECEIVES
// 23000.example.com and forwards it to 127.0.0.1:23000 -- plus the port
// allowlist that keeps it from becoming an open relay into every local service
// -- is phase 2. See tasks/2026-09-09-wildcard-host-demux.md.

// configuredPublicHostname is the wildcard apex resolved at boot, or "" when
// the mode is off. Phase 2's demuxer matches Host headers against it. It is
// written once during main() before any listener starts, then only read.
var configuredPublicHostname string

// publicHostnameRuntimeEnv is the env var `swe-swe up` exports for a
// --runtime=host project. docker-compose never sets it, which is exactly the
// discrimination this mode needs (see requirePublicHostnameHostRuntime).
const publicHostnameRuntimeEnv = "SWE_RUNTIME"

// resolvePublicHostname folds the -public-hostname flag and the
// SWE_PUBLIC_HOSTNAME env var into one value, using the same precedence as
// every other setting here: an explicitly passed flag wins over the env, and
// the env wins over the (empty) default.
func resolvePublicHostname(flagVal string, flagSet bool, lookupEnv func(string) (string, bool)) string {
	if flagSet {
		return flagVal
	}
	if v, ok := lookupEnv("SWE_PUBLIC_HOSTNAME"); ok && strings.TrimSpace(v) != "" {
		return v
	}
	return flagVal
}

// normalizePublicHostname turns what an operator is likely to type into the
// bare apex the subdomain builders need. It accepts "*.example.com",
// "https://example.com/" and "Example.COM." because all three are the same
// intent, and rejects anything that could not be a wildcard apex.
//
// An empty input is not an error: it means the mode is off.
func normalizePublicHostname(raw string) (string, error) {
	h := strings.TrimSpace(raw)
	if h == "" {
		return "", nil
	}
	h = strings.ToLower(h)

	// A pasted URL: drop the scheme and everything from the first slash.
	for _, scheme := range []string{"http://", "https://"} {
		h = strings.TrimPrefix(h, scheme)
	}
	if i := strings.IndexByte(h, '/'); i >= 0 {
		h = h[:i]
	}

	// "*.example.com" is how a wildcard DNS record is written, so accept it
	// and keep the apex.
	h = strings.TrimPrefix(h, "*.")
	// A fully-qualified name may carry a trailing root dot.
	h = strings.TrimSuffix(h, ".")

	if h == "" {
		return "", fmt.Errorf("-public-hostname %q has no hostname in it", raw)
	}
	if strings.ContainsAny(h, " \t") {
		return "", fmt.Errorf("-public-hostname %q contains whitespace", raw)
	}
	// An IP address has no subdomains, so a wildcard cannot exist for it.
	// Checked before the port rule so an IPv6 literal, which is all colons,
	// gets the accurate message.
	if net.ParseIP(h) != nil || net.ParseIP(strings.Trim(h, "[]")) != nil {
		return "", fmt.Errorf("-public-hostname %q is an IP address: this mode needs a domain whose *.<domain> DNS points here", raw)
	}
	// A port here would be silently ignored: the port people reach is the
	// server's own listener, never part of the wildcard apex.
	if strings.ContainsRune(h, ':') {
		return "", fmt.Errorf("-public-hostname %q must not include a port: pass just the domain (the port is whatever this server listens on)", raw)
	}

	labels := strings.Split(h, ".")
	if len(labels) < 2 {
		return "", fmt.Errorf("-public-hostname %q is a single label: this mode needs a domain with a dot in it, e.g. example.com or lvh.me", raw)
	}
	for _, l := range labels {
		if err := validateHostLabel(l, raw); err != nil {
			return "", err
		}
	}
	return h, nil
}

// validateHostLabel checks one dot-separated piece of a hostname against the
// DNS rules the demuxer will rely on: 1-63 characters of a-z, 0-9 and hyphen,
// with no hyphen at either end.
func validateHostLabel(l, raw string) error {
	if l == "" {
		return fmt.Errorf("-public-hostname %q has an empty label (two dots in a row, or a leading dot)", raw)
	}
	if len(l) > 63 {
		return fmt.Errorf("-public-hostname %q has a label longer than 63 characters", raw)
	}
	if strings.HasPrefix(l, "-") || strings.HasSuffix(l, "-") {
		return fmt.Errorf("-public-hostname %q has a label starting or ending with a hyphen: %q", raw, l)
	}
	for _, r := range l {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= '0' && r <= '9':
		case r == '-':
		default:
			return fmt.Errorf("-public-hostname %q contains %q, which is not allowed in a hostname", raw, string(r))
		}
	}
	return nil
}

// requirePublicHostnameHostRuntime rejects the mode anywhere but a
// --runtime=host project.
//
// This is ADR-0043's break 5, and it is real: under docker-compose every
// request arrives through Traefik, whose Host() router rules cannot match a
// wildcard and whose TLS certificate is pinned to one domain. A wildcard
// hostname there would produce addresses that never reach this server, which
// is worse than refusing to start. `swe-swe up` on a host-runtime project
// exports SWE_RUNTIME=host; compose never does.
func requirePublicHostnameHostRuntime(runtimeMode string) error {
	if runtimeMode == "host" {
		return nil
	}
	if runtimeMode == "" {
		return fmt.Errorf("-public-hostname needs a host-native run: start the server with `swe-swe up` on a project initialized with --runtime=host (or export %s=host if you are launching swe-swe-server yourself)", publicHostnameRuntimeEnv)
	}
	return fmt.Errorf("-public-hostname is not supported with %s=%s: under docker-compose, Traefik routes by exact hostname and cannot match *.<domain>. Use --runtime=host, or reach this box through a tunnel instead", publicHostnameRuntimeEnv, runtimeMode)
}

// publicHostnameDecision is the outcome of the boot-time resolution.
// Hostname is the apex to publish ("" = mode off). DropTunnel says the
// wildcard hostname beat an inherited tunnel setting, so the caller must not
// start the tunnel supervisor. Note carries the one line to log when something
// non-obvious was decided.
type publicHostnameDecision struct {
	Hostname   string
	DropTunnel bool
	Note       string
}

// resolveConfiguredPublicHostname is the whole boot-time decision in one pure
// function: normalize what was asked for, settle it against the tunnel, refuse
// what cannot work.
//
// The tunnel cannot run alongside it: both own liveTunnelHostname, and with
// both set the last writer would silently win, so the addresses in the UI would
// depend on timing. Which one gives way follows one rule -- an explicitly
// passed flag beats a value merely inherited from the environment. That rule
// exists because boxes really do carry a stale SWE_TUNNEL_SERVER_URL in their
// environment, and `swe-swe up --public-hostname=...` there must mean what it
// says rather than dying on a variable nobody typed. A flag cannot be inherited
// by accident; a variable can.
//
// Ambiguous cases -- both explicit, or both inherited -- are a hard error, not
// a silent winner.
func resolveConfiguredPublicHostname(raw string, rawExplicit bool, tunnelServerURL string, tunnelExplicit bool, runtimeMode string) (publicHostnameDecision, error) {
	host, err := normalizePublicHostname(raw)
	if err != nil {
		return publicHostnameDecision{}, err
	}
	if host == "" {
		return publicHostnameDecision{}, nil
	}
	if err := requirePublicHostnameHostRuntime(runtimeMode); err != nil {
		return publicHostnameDecision{}, err
	}

	if strings.TrimSpace(tunnelServerURL) == "" {
		return publicHostnameDecision{Hostname: host}, nil
	}
	switch {
	case rawExplicit && !tunnelExplicit:
		return publicHostnameDecision{
			Hostname:   host,
			DropTunnel: true,
			Note: fmt.Sprintf("-public-hostname=%s given on the command line, so tunnel mode is OFF for this run (SWE_TUNNEL_SERVER_URL=%s came from the environment, not from you)",
				host, strings.TrimSpace(tunnelServerURL)),
		}, nil
	case tunnelExplicit && !rawExplicit:
		return publicHostnameDecision{
			Note: fmt.Sprintf("SWE_PUBLIC_HOSTNAME=%s ignored: -tunnel-server-url was given on the command line and the tunnel supplies the public hostname itself", host),
		}, nil
	default:
		return publicHostnameDecision{}, fmt.Errorf("-public-hostname and -tunnel-server-url are mutually exclusive: both decide the public hostname this server advertises. A wildcard domain pointing here needs no tunnel; drop one of the two")
	}
}
