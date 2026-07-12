package main

import (
	"fmt"
	"net"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
)

// vhostPin is a per-session pinned vhost target used in degraded (pinned)
// preview mode, where the browser cannot reach wildcard subdomains. Label-less
// requests to the session's preview listener route to this target with the
// upstream Host rewritten to the logical vhost. See ADR-0045 / Design in
// tasks/2026-07-04-preview-hostname-vhost.md.
type vhostPin struct {
	Name string // logical vhost name, e.g. "app1"
	Port int    // loopback target port, e.g. 5000
}

// previewLabelRe validates a single DNS label used as a preview vhost prefix:
// lowercase alphanumerics and dashes, must start and end alphanumeric, max 63.
var previewLabelRe = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$`)

// previewVhostSuffix returns the logical vhost suffix (SWE_PREVIEW_VHOST_SUFFIX,
// default "lvh.me"). This is the suffix REWRITTEN onto the upstream Host so the
// user's own Host-based router (traefik/nginx) matches as it would on a laptop.
func previewVhostSuffix() string {
	if s := strings.TrimSpace(os.Getenv("SWE_PREVIEW_VHOST_SUFFIX")); s != "" {
		return s
	}
	return "lvh.me"
}

// parsePreviewLabel parses a preview vhost label per the grammar (see Design):
//
//	{name}-{port}  port 1024-65535 -> (name, port)   e.g. app1-5000, my-app-5000
//	{port}         port 1024-65535 -> ("", port)      e.g. 3001
//	{name}         no trailing port -> (name, 0)      e.g. app1, probe-x
//
// Ports outside 1024-65535 and labels that fail previewLabelRe are rejected
// (ok=false). The {name}-{port} split is on the LAST dash-number segment, so
// "my-app-5000" splits to ("my-app", 5000). Targets are loopback only; this
// function never allocates ports.
func parsePreviewLabel(label string) (name string, port int, ok bool) {
	if !previewLabelRe.MatchString(label) {
		return "", 0, false
	}
	// {name}-{port}: the segment after the final dash is all digits.
	if i := strings.LastIndex(label, "-"); i > 0 && i < len(label)-1 {
		if n, err := strconv.Atoi(label[i+1:]); err == nil {
			if n < 1024 || n > 65535 {
				return "", 0, false
			}
			return label[:i], n, true
		}
	}
	// bare {port}: the whole label is all digits.
	if n, err := strconv.Atoi(label); err == nil {
		if n < 1024 || n > 65535 {
			return "", 0, false
		}
		return "", n, true
	}
	// bare {name}
	return label, 0, true
}

// splitLeftmostLabel splits an inbound "host[:port]" into its leftmost DNS
// label and the remaining reach suffix (port removed). Returns ok=false for a
// single-label host (e.g. "localhost:23000"), which has no vhost prefix and
// must keep today's behavior.
//
//	"app1-5000.x.sslip.io:23000" -> ("app1-5000", "x.sslip.io", true)
//	"localhost:23000"            -> ("", "", false)
//	"127.0.0.1:23000"            -> ("127", "0.0.1", true) [label rejected later]
func splitLeftmostLabel(hostport string) (label, reachSuffix string, ok bool) {
	host := hostport
	if h, _, err := net.SplitHostPort(hostport); err == nil {
		host = h
	}
	i := strings.Index(host, ".")
	if i < 0 {
		return "", "", false
	}
	return host[:i], host[i+1:], true
}

// previewReachLabel returns the first DNS label of SWE_PREVIEW_REACH_DOMAIN, if
// configured, so browsing the bare reach is not mistaken for a vhost prefix.
// Empty when the env is unset (the pin mechanism still guards pinned mode).
func previewReachLabel() string {
	d := strings.TrimSpace(os.Getenv("SWE_PREVIEW_REACH_DOMAIN"))
	if d == "" {
		return ""
	}
	if i := strings.Index(d, "."); i > 0 {
		return d[:i]
	}
	return d
}

// previewResolveTarget is the ResolveTarget hook body: it extracts the leftmost
// label of the inbound Host and resolves it against the session's vhost grammar,
// returning a loopback target and the upstream Host to send. Returns ok=false to
// fall back to the fixed target with today's clobbered Host.
func previewResolveTarget(inboundHost string, s *Session) (*url.URL, string, bool) {
	label, _, ok := splitLeftmostLabel(inboundHost)
	if !ok {
		return nil, "", false
	}
	port, upstreamHost, ok := resolvePreviewVhost(label, s)
	if !ok {
		return nil, "", false
	}
	return &url.URL{Scheme: "http", Host: fmt.Sprintf("127.0.0.1:%d", port)}, upstreamHost, true
}

// previewCookieDomainRewrite is the CookieDomainRewrite hook body: it maps an
// upstream Set-Cookie Domain under the logical suffix (.lvh.me) to the reach
// domain of THIS request (.x.sslip.io), so shared-auth cookies keep working
// across the reach origins. Non-logical domains and single-label hosts are
// stripped (return ""), matching legacy behavior.
func previewCookieDomainRewrite(inboundHost, domain string) string {
	_, reach, ok := splitLeftmostLabel(inboundHost)
	if !ok {
		return ""
	}
	suffix := previewVhostSuffix()
	d := strings.TrimPrefix(domain, ".")
	if d == suffix || strings.HasSuffix(d, "."+suffix) {
		return "." + reach
	}
	return ""
}

// resolvePreviewVhost resolves a leftmost preview label to a loopback target
// port and the upstream Host to send, following the grammar precedence in the
// Design. Returns ok=false to signal "fall back to today's fixed target with
// clobbered Host" (rule 4, no pin) -- the caller wires this into the v0.2.x
// ResolveTarget hook, whose ok=false path preserves legacy behavior.
//
// Precedence:
//  1. {name}-{port} -> ({port}, "{name}.{suffix}:{port}")           [explicit]
//  2. {port}        -> ({port}, "localhost:{port}")                 [explicit, tunnel-style]
//     Explicit-port labels resolve whether or not a pin is set.
//  3. pin set       -> (pin.Port, "{pin.Name}.{suffix}:{pin.Port}") [pinned mode wins over bare name]
//  4. {name}        -> (PreviewPort, "{name}.{suffix}:{PreviewPort}") unless the label equals the
//     reach's own first label (PreviewReachLabel guard).
//  5. otherwise     -> (0, "", false)                              [legacy clobber]
func resolvePreviewVhost(label string, s *Session) (port int, upstreamHost string, ok bool) {
	suffix := previewVhostSuffix()
	name, p, parsed := parsePreviewLabel(label)

	// Rules 1 & 2: explicit port always resolves (user intent beats pin).
	if parsed && p != 0 {
		if name != "" {
			return p, fmt.Sprintf("%s.%s:%d", name, suffix, p), true // rule 1
		}
		return p, fmt.Sprintf("localhost:%d", p), true // rule 2
	}

	// Pinned mode wins for everything that is not an explicit-port label,
	// including bare names and unrecognized labels. This keeps pinned-mode
	// bare-origin browsing (custom hostname whose first label looks like a
	// vhost name) routed to the pin rather than mis-resolved as a vhost.
	if s != nil && s.VhostPin != nil {
		pin := s.VhostPin
		return pin.Port, fmt.Sprintf("%s.%s:%d", pin.Name, suffix, pin.Port), true
	}

	// Rule 3: bare {name} -> primary PreviewPort vhost, unless the label is the
	// reach's own first label (browsing the bare reach, not a vhost prefix).
	if parsed && name != "" && p == 0 {
		if s != nil && s.PreviewReachLabel != "" && label == s.PreviewReachLabel {
			return 0, "", false // reach-first-label guard -> rule 4
		}
		// Phase 6 will consult registered named routes here first.
		if s != nil && s.PreviewPort != 0 {
			return s.PreviewPort, fmt.Sprintf("%s.%s:%d", name, suffix, s.PreviewPort), true
		}
	}

	// Rule 4: no/unrecognized label and no pin -> legacy clobbered behavior.
	return 0, "", false
}
