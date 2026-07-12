package main

import (
	"fmt"
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
