package main

import (
	"fmt"
	"log"
	"net"
	"net/http"
	"net/http/httputil"
	"strconv"
	"strings"
	"sync"
)

// Wildcard host-demux, phase 2: receiving {port}.{apex} on the main listener
// and forwarding it to that port on loopback.
//
// Phase 1 (public_hostname.go) set the hostname every subdomain URL is built
// from. This is the other half: something has to ANSWER those addresses. In
// tunnel mode the tunnel server does it; here swe-swe-server does it on its own
// listener, so a box whose *.example.com DNS points at it needs no tunnel.
//
// It sits in front of the main handler, BEFORE auth, which is deliberate and
// safe: every per-port listener wraps itself in requireAuthCookie (see the
// preview/agent-chat/vnc/files handlers in main.go), so the destination
// authenticates. That is exactly what happens in tunnel mode, where tunneld
// forwards raw TCP to the same ports with no auth of its own.
//
// The inbound Host is passed through unchanged for the same reason: the
// upstream must see what it would see behind the tunnel, or per-port behaviour
// (the preview vhost grammar in particular) would differ between the two modes.

// publicHostnamePortRange is one contiguous band of loopback ports the demuxer
// may forward to, with the name used in the refusal log.
type publicHostnamePortRange struct {
	Name string
	Lo   int
	Hi   int
}

// publicHostnameRoutablePorts is the allowlist, derived at call time from the
// live range variables so it moves with -preview-ports / -proxy-port-offset and
// their env equivalents rather than drifting from them.
//
// Do not skip this list. A public hostname that forwards to any 127.0.0.1:<n>
// is an open door into every service on the box -- swe-swe's own broker
// included -- for anyone who can resolve the domain.
//
// Only the per-session proxy bands are here (proxyPortOffset + each range).
// The raw target ports behind them are NOT routable: reaching an app means
// reaching its proxy, which is the thing that checks the login cookie.
//
// The CDP and VNC bands run past their nominal end because both have internal
// ranges stacked above them -- remoteCDPProxyOffset for a remote Agent View
// backend, and websockify's own band for VNC. defaultTunnelExcludePorts
// (agentview_tunnel_client.go) derives its bands the same way.
func publicHostnameRoutablePorts() []publicHostnamePortRange {
	cdpSize := cdpPortEnd - cdpPortStart + 1
	vncSize := vncPortEnd - vncPortStart + 1
	return []publicHostnamePortRange{
		{"preview", previewProxyPort(previewPortStart), previewProxyPort(previewPortEnd)},
		{"agent-chat", agentChatProxyPort(agentChatPortStart), agentChatProxyPort(agentChatPortEnd)},
		{"public", proxyPortOffset + publicPortStart, proxyPortOffset + publicPortEnd},
		{"cdp", cdpProxyPort(cdpPortStart), cdpProxyPort(cdpPortEnd + 2*cdpSize)},
		{"vnc", vncProxyPort(vncPortStart), vncProxyPort(vncPortEnd + vncSize)},
		{"files", filesProxyPort(filesPortStart), filesProxyPort(filesPortEnd)},
	}
}

// publicHostnamePortRoutable reports whether port is in the allowlist, and
// which band it fell in.
func publicHostnamePortRoutable(port int) (string, bool) {
	for _, r := range publicHostnameRoutablePorts() {
		if port >= r.Lo && port <= r.Hi {
			return r.Name, true
		}
	}
	return "", false
}

// publicHostnameTargetPort extracts the loopback port an inbound Host is
// asking for. It matches ONLY "<all digits>.<apex>" exactly -- the leftmost
// label names the port and everything after it must be the configured apex.
//
// The exactness is the security boundary, so the hostile shapes all miss:
//
//	23000.example.com          -> 23000        (apex example.com)
//	23000.example.com:1977     -> 23000        (the connection port is not part of the name)
//	23000.EXAMPLE.COM.         -> 23000        (case and the root dot do not change a name)
//	23000.evil.com             -> no match     (wrong apex)
//	23000.example.com.evil.com -> no match     (apex is not the suffix, it is the whole remainder)
//	a.23000.example.com        -> no match     (leftmost label is not the port)
//	23000x.example.com         -> no match     (not all digits)
//	example.com                -> no match     (no port label at all)
func publicHostnameTargetPort(inboundHost, apex string) (int, bool) {
	if apex == "" || inboundHost == "" {
		return 0, false
	}
	host := inboundHost
	if h, _, err := net.SplitHostPort(inboundHost); err == nil {
		host = h
	}
	host = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(host), "."))
	// An IPv6 literal arrives bracketed and has no labels to speak of.
	host = strings.Trim(host, "[]")

	suffix := "." + strings.ToLower(apex)
	if !strings.HasSuffix(host, suffix) {
		return 0, false
	}
	label := host[:len(host)-len(suffix)]
	if label == "" || strings.Contains(label, ".") {
		return 0, false
	}
	// strconv.Atoi accepts a leading sign and Go's parser accepts underscores
	// in some literals; neither is a DNS label, so check the digits directly.
	for _, r := range label {
		if r < '0' || r > '9' {
			return 0, false
		}
	}
	port, err := strconv.Atoi(label)
	if err != nil || port < 1 || port > 65535 {
		return 0, false
	}
	return port, true
}

// publicHostnameRefusalLog remembers which ports have already been refused so a
// misconfiguration is visible once rather than once per request. A scan or a
// stuck client would otherwise flood the log.
var publicHostnameRefusalLog sync.Map

func logPublicHostnameRefusal(port int, inboundHost string) {
	if _, seen := publicHostnameRefusalLog.LoadOrStore(port, true); seen {
		return
	}
	log.Printf("public-hostname demux: refusing %s -> 127.0.0.1:%d (port is not one swe-swe serves; allowed: %s)",
		inboundHost, port, describePublicHostnameRoutablePorts())
}

// describePublicHostnameRoutablePorts renders the allowlist for a log line.
func describePublicHostnameRoutablePorts() string {
	parts := make([]string, 0, 6)
	for _, r := range publicHostnameRoutablePorts() {
		parts = append(parts, fmt.Sprintf("%s %d-%d", r.Name, r.Lo, r.Hi))
	}
	return strings.Join(parts, ", ")
}

// publicHostnameDemux wraps the main handler with the subdomain demuxer.
//
// apex == "" (the mode is off) returns next untouched, so a box reached by IP,
// by the bare domain, or with the mode disabled behaves exactly as it does
// today. Requests whose Host is not "<port>.<apex>" also fall straight through.
//
// A request for the server's OWN port falls through rather than being proxied.
// It has to: the landing page advertises "{server port}.{apex}", and proxying
// that to ourselves would arrive with the same Host and match again, forever.
func publicHostnameDemux(apex string, serverPort int, next http.Handler) http.Handler {
	if apex == "" {
		return next
	}
	if next == nil {
		next = http.DefaultServeMux
	}

	proxy := &httputil.ReverseProxy{
		Director: func(req *http.Request) {
			port, ok := publicHostnameTargetPort(req.Host, apex)
			if !ok {
				// Unreachable: ServeHTTP below only calls the proxy after the
				// same parse succeeded. Fail closed rather than dial port 0.
				req.URL.Scheme = ""
				req.URL.Host = ""
				return
			}
			req.URL.Scheme = "http"
			req.URL.Host = fmt.Sprintf("127.0.0.1:%d", port)
			// req.Host is deliberately NOT rewritten: the upstream must see
			// the same Host the tunnel would have delivered.
		},
		// Stream immediately. Preview apps serve server-sent events and the
		// terminal/VNC panes are long-lived; buffering would stall them.
		FlushInterval: -1,
		ErrorHandler: func(w http.ResponseWriter, req *http.Request, err error) {
			log.Printf("public-hostname demux: %s -> upstream error: %v", req.Host, err)
			http.Error(w, "Bad Gateway", http.StatusBadGateway)
		},
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		port, ok := publicHostnameTargetPort(r.Host, apex)
		if !ok || port == serverPort {
			next.ServeHTTP(w, r)
			return
		}
		if _, allowed := publicHostnamePortRoutable(port); !allowed {
			logPublicHostnameRefusal(port, r.Host)
			http.NotFound(w, r)
			return
		}
		proxy.ServeHTTP(w, r)
	})
}
