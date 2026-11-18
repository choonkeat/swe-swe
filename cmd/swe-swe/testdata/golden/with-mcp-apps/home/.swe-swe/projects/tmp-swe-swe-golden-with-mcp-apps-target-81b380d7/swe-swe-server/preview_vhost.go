package main

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
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

// setVhostPin/getVhostPin/clearVhostPin guard Session.VhostPin with s.mu, since
// the pin endpoint mutates it while concurrent proxy requests read it.
func (s *Session) setVhostPin(name string, port int) {
	s.mu.Lock()
	s.VhostPin = &vhostPin{Name: name, Port: port}
	s.mu.Unlock()
}

func (s *Session) getVhostPin() *vhostPin {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.VhostPin
}

func (s *Session) clearVhostPin() {
	s.mu.Lock()
	s.VhostPin = nil
	s.mu.Unlock()
}

// previewVhostPinHandler intercepts the per-session pin endpoint on the preview
// listener and otherwise delegates to next. Pinned mode uses this to route
// label-less requests to a single vhost target (see ADR-0045). The caller wraps
// it in requireAuthCookie, exactly like the other per-port proxy debug routes.
func previewVhostPinHandler(sess *Session, next http.Handler) http.Handler {
	const pinPath = "/__agent-reverse-proxy-debug__/vhost-pin"
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != pinPath {
			next.ServeHTTP(w, r)
			return
		}
		switch r.Method {
		case http.MethodPost:
			var body struct {
				Name string `json:"name"`
				Port int    `json:"port"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				http.Error(w, "invalid JSON body", http.StatusBadRequest)
				return
			}
			if !validPinName(body.Name) || body.Port < 1024 || body.Port > 65535 {
				http.Error(w, "invalid name or port", http.StatusBadRequest)
				return
			}
			sess.setVhostPin(body.Name, body.Port)
			writePinJSON(w, http.StatusOK, sess.getVhostPin())
		case http.MethodGet:
			writePinJSON(w, http.StatusOK, sess.getVhostPin())
		case http.MethodDelete:
			sess.clearVhostPin()
			w.WriteHeader(http.StatusNoContent)
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	})
}

// validPinName accepts a DNS label that is not purely numeric (a numeric label
// is a port, not a vhost name).
func validPinName(name string) bool {
	if !previewLabelRe.MatchString(name) {
		return false
	}
	if _, err := strconv.Atoi(name); err == nil {
		return false
	}
	return true
}

// writePinJSON renders the current pin state. A nil pin reports pinned=false.
func writePinJSON(w http.ResponseWriter, status int, pin *vhostPin) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if pin == nil {
		json.NewEncoder(w).Encode(map[string]any{"pinned": false})
		return
	}
	json.NewEncoder(w).Encode(map[string]any{"pinned": true, "name": pin.Name, "port": pin.Port})
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

// previewReachCandidates returns the server's ordered reach-domain candidates
// for the status payload. If SWE_PREVIEW_REACH_DOMAIN is set it wins outright;
// otherwise the same-machine wildcard domain lvh.me is offered. The frontend
// appends its own window.location.hostname (and any IP-derived sslip.io
// variant) and probes each in order (see ADR-0045 / Design).
func previewReachCandidates() []string {
	if d := strings.TrimSpace(os.Getenv("SWE_PREVIEW_REACH_DOMAIN")); d != "" {
		return []string{d}
	}
	return []string{"lvh.me"}
}

// allowedPreviewReaches returns the reach suffixes the server is configured to
// trust for wildcard preview: the logical vhost suffix (SWE_PREVIEW_VHOST_SUFFIX,
// default "lvh.me") plus the reach candidates (SWE_PREVIEW_REACH_DOMAIN, else
// "lvh.me"), de-duplicated in order. Only these are ever used to widen a cookie
// Domain, so an arbitrary inbound Host cannot scope a cookie to a domain the
// server does not expect.
func allowedPreviewReaches() []string {
	seen := map[string]bool{}
	var out []string
	add := func(s string) {
		s = strings.TrimSpace(s)
		if s != "" && !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	add(previewVhostSuffix())
	for _, c := range previewReachCandidates() {
		add(c)
	}
	return out
}

// previewCookieReach returns the reach suffix the session auth cookie should be
// pinned to when the request lands on a wildcard-preview origin, or "" when it
// does not. It is the non-tunnel analogue of resolveCookieDomain's apex pinning:
// in wildcard mode the browser reaches sub-apps at "{name}-{port}.{reach}", so a
// host-only auth cookie set on one origin is never sent to the siblings. Pinning
// Domain={reach} lets the single login cover them all.
func previewCookieReach(requestHost string) string {
	return previewCookieReachFrom(requestHost, allowedPreviewReaches())
}

// previewCookieReachFrom is the pure core of previewCookieReach: it returns the
// first reach in `reaches` that `requestHost` equals or is a subdomain of (port
// stripped), else "". Matching only a configured reach (never an arbitrary Host
// suffix) is what keeps this from scoping the cookie too broadly.
func previewCookieReachFrom(requestHost string, reaches []string) string {
	host := requestHost
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	for _, reach := range reaches {
		if reach == "" {
			continue
		}
		if host == reach || strings.HasSuffix(host, "."+reach) {
			return reach
		}
	}
	return ""
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
	// A single-label host (e.g. the pinned-mode bare origin "myhost:23000") has
	// no vhost prefix; pass an empty label so resolvePreviewVhost still consults
	// the session pin before falling back to legacy behavior.
	label, _, _ := splitLeftmostLabel(inboundHost)
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
	if s != nil {
		if pin := s.getVhostPin(); pin != nil {
			return pin.Port, fmt.Sprintf("%s.%s:%d", pin.Name, suffix, pin.Port), true
		}
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
