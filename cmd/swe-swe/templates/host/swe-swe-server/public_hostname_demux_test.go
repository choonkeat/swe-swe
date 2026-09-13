package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
)

func TestPublicHostnameTargetPort(t *testing.T) {
	const apex = "example.com"
	tests := []struct {
		name string
		host string
		want int
		ok   bool
	}{
		{"plain", "23000.example.com", 23000, true},
		{"with the connection port", "23000.example.com:1977", 23000, true},
		{"uppercase", "23000.EXAMPLE.COM", 23000, true},
		{"trailing root dot", "23000.example.com.", 23000, true},
		{"surrounding space", " 23000.example.com ", 23000, true},
		{"server port label", "1977.example.com:1977", 1977, true},

		// Hostile shapes: every one of these must miss.
		{"wrong apex", "23000.evil.com", 0, false},
		{"apex as a prefix of a longer name", "23000.example.com.evil.com", 0, false},
		{"apex embedded, not the remainder", "23000.notexample.com", 0, false},
		{"extra label before the port", "a.23000.example.com", 0, false},
		{"label not all digits", "23000x.example.com", 0, false},
		{"label with a sign", "+23000.example.com", 0, false},
		{"bare apex", "example.com", 0, false},
		{"apex with the connection port", "example.com:1977", 0, false},
		{"port out of range", "99999.example.com", 0, false},
		{"port zero", "0.example.com", 0, false},
		{"empty label", ".example.com", 0, false},
		{"ipv4 literal", "127.0.0.1:1977", 0, false},
		{"ipv6 literal", "[::1]:1977", 0, false},
		{"empty host", "", 0, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := publicHostnameTargetPort(tc.host, apex)
			if ok != tc.ok || got != tc.want {
				t.Errorf("publicHostnameTargetPort(%q, %q) = (%d, %v), want (%d, %v)", tc.host, apex, got, ok, tc.want, tc.ok)
			}
		})
	}
}

// With the mode off, nothing may match -- a box reached by IP or by the bare
// domain must behave exactly as it does today.
func TestPublicHostnameTargetPortOffMatchesNothing(t *testing.T) {
	for _, host := range []string{"23000.example.com", "example.com", "127.0.0.1:1977", ""} {
		if _, ok := publicHostnameTargetPort(host, ""); ok {
			t.Errorf("publicHostnameTargetPort(%q, \"\") matched; the mode is off", host)
		}
	}
}

// The allowlist is what stops a public hostname from becoming a door into every
// service on the box. Only the per-session proxy bands are routable.
func TestPublicHostnamePortRoutable(t *testing.T) {
	allowed := []struct {
		name string
		port int
	}{
		{"preview low", previewProxyPort(previewPortStart)},
		{"preview high", previewProxyPort(previewPortEnd)},
		{"agent-chat low", agentChatProxyPort(agentChatPortStart)},
		{"public low", proxyPortOffset + publicPortStart},
		{"vnc low", vncProxyPort(vncPortStart)},
		{"vnc internal band", vncProxyPort(vncPortEnd + 1)},
		{"cdp remote-offset band", cdpProxyPort(cdpPortStart + remoteCDPProxyOffset)},
		{"files high", filesProxyPort(filesPortEnd)},
	}
	for _, tc := range allowed {
		t.Run("allow "+tc.name, func(t *testing.T) {
			if _, ok := publicHostnamePortRoutable(tc.port); !ok {
				t.Errorf("port %d should be routable (%s)", tc.port, tc.name)
			}
		})
	}

	refused := []struct {
		name string
		port int
	}{
		{"ssh", 22},
		{"the broker's neighbourhood", 1977},
		{"a raw preview target, not its proxy", previewPortStart},
		{"a raw vnc target, not its proxy", vncPortStart},
		{"just below the preview band", previewProxyPort(previewPortStart) - 1},
		{"just above the files band", filesProxyPort(filesPortEnd) + 1},
		{"between the public and cdp bands", proxyPortOffset + publicPortEnd + 1},
		{"a high random port", 45678},
	}
	for _, tc := range refused {
		t.Run("refuse "+tc.name, func(t *testing.T) {
			if name, ok := publicHostnamePortRoutable(tc.port); ok {
				t.Errorf("port %d should NOT be routable (%s), but matched band %q", tc.port, tc.name, name)
			}
		})
	}
}

// The allowlist must move with the port-range variables rather than repeat
// their values, so an operator's -preview-ports / -proxy-port-offset applies.
func TestPublicHostnameRoutablePortsFollowTheRangeVariables(t *testing.T) {
	prevOffset, prevStart, prevEnd := proxyPortOffset, previewPortStart, previewPortEnd
	t.Cleanup(func() {
		proxyPortOffset, previewPortStart, previewPortEnd = prevOffset, prevStart, prevEnd
	})

	proxyPortOffset = 40000
	previewPortStart, previewPortEnd = 8000, 8009

	if _, ok := publicHostnamePortRoutable(48000); !ok {
		t.Errorf("48000 should be routable after moving the offset and preview range")
	}
	if _, ok := publicHostnamePortRoutable(23000); ok {
		t.Errorf("23000 should NOT be routable after moving the offset off it")
	}
}

// The files band is derived from the preview port, not from the
// filesPortStart/filesPortEnd constants. A box with SWE_PREVIEW_PORTS=3200-3229
// hands out files ports 9200-9229, whose proxies are 29200-29229 -- and the
// constants describe 29000-29019, so the Files pane was refused outright over
// the wildcard route. Caught live by e2e/tests/public-hostname.spec.js.
func TestPublicHostnameFilesBandFollowsThePreviewRange(t *testing.T) {
	prevStart, prevEnd := previewPortStart, previewPortEnd
	t.Cleanup(func() {
		previewPortStart, previewPortEnd = prevStart, prevEnd
	})

	previewPortStart, previewPortEnd = 3200, 3229

	for _, port := range []int{29200, 29207, 29229} {
		band, ok := publicHostnamePortRoutable(port)
		if !ok {
			t.Errorf("%d should be routable as a files proxy port with preview 3200-3229", port)
			continue
		}
		if band != "files" {
			t.Errorf("%d routed as %q, want \"files\"", port, band)
		}
	}
	if _, ok := publicHostnamePortRoutable(29230); ok {
		t.Errorf("29230 is past the end of the files band and must not be routable")
	}
}

// newDemuxProbe starts a fake upstream on an allowlisted port and returns the
// demux handler in front of a fallthrough marker.
func demuxProbe(t *testing.T, apex string, serverPort int) (h http.Handler, fellThrough *bool) {
	t.Helper()
	ft := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ft = true
		w.WriteHeader(http.StatusTeapot)
	})
	return publicHostnameDemux(apex, serverPort, next), &ft
}

func TestPublicHostnameDemuxOffIsAPassthrough(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusTeapot) })
	h := publicHostnameDemux("", 1977, next)
	req := httptest.NewRequest("GET", "http://23000.example.com/", nil)
	req.Host = "23000.example.com"
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusTeapot {
		t.Errorf("mode off: got %d, want the wrapped handler's %d", rr.Code, http.StatusTeapot)
	}
}

func TestPublicHostnameDemuxFallsThrough(t *testing.T) {
	tests := []struct {
		name string
		host string
	}{
		{"bare apex", "example.com"},
		{"a different domain", "23000.evil.com"},
		{"reached by IP", "203.0.113.5:1977"},
		{"the server's own port label", "1977.example.com:1977"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			h, fellThrough := demuxProbe(t, "example.com", 1977)
			req := httptest.NewRequest("GET", "http://"+tc.host+"/", nil)
			req.Host = tc.host
			rr := httptest.NewRecorder()
			h.ServeHTTP(rr, req)
			if !*fellThrough {
				t.Errorf("Host %q should reach the normal handler, got status %d", tc.host, rr.Code)
			}
		})
	}
}

// A port outside the allowlist is a 404 and never a dial.
func TestPublicHostnameDemuxRefusesPortsOutsideTheAllowlist(t *testing.T) {
	for _, port := range []int{22, 5432, previewPortStart, 45678} {
		t.Run(fmt.Sprint(port), func(t *testing.T) {
			h, fellThrough := demuxProbe(t, "example.com", 1977)
			host := fmt.Sprintf("%d.example.com", port)
			req := httptest.NewRequest("GET", "http://"+host+"/", nil)
			req.Host = host
			rr := httptest.NewRecorder()
			h.ServeHTTP(rr, req)
			if rr.Code != http.StatusNotFound {
				t.Errorf("port %d: got %d, want 404", port, rr.Code)
			}
			if *fellThrough {
				t.Errorf("port %d: reached the normal handler; a refused port must not", port)
			}
		})
	}
}

// The happy path, end to end: an allowlisted port is forwarded to loopback with
// the inbound Host passed through unchanged (the upstream must see what the
// tunnel would have delivered).
func TestPublicHostnameDemuxForwardsToLoopbackPreservingHost(t *testing.T) {
	gotHost := ""
	gotPath := ""
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHost = r.Host
		gotPath = r.URL.Path
		fmt.Fprint(w, "upstream-here")
	}))
	defer upstream.Close()

	u, err := url.Parse(upstream.URL)
	if err != nil {
		t.Fatalf("parsing the test upstream address: %v", err)
	}
	portStr, err := strconv.Atoi(u.Port())
	if err != nil {
		t.Fatalf("test upstream has no port in %q: %v", upstream.URL, err)
	}

	// Point the preview band at whatever port httptest picked, so the real
	// allowlist (not a test-only bypass) admits it.
	prevOffset, prevStart, prevEnd := proxyPortOffset, previewPortStart, previewPortEnd
	t.Cleanup(func() {
		proxyPortOffset, previewPortStart, previewPortEnd = prevOffset, prevStart, prevEnd
	})
	proxyPortOffset = 0
	previewPortStart, previewPortEnd = portStr, portStr

	h, fellThrough := demuxProbe(t, "example.com", 1977)
	host := fmt.Sprintf("%d.example.com", portStr)
	req := httptest.NewRequest("GET", "http://"+host+"/some/path", nil)
	req.Host = host
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if *fellThrough {
		t.Fatalf("an allowlisted port reached the normal handler instead of being forwarded")
	}
	if rr.Code != http.StatusOK {
		t.Fatalf("got %d, want 200 (body %q)", rr.Code, rr.Body.String())
	}
	if body := strings.TrimSpace(rr.Body.String()); body != "upstream-here" {
		t.Errorf("body = %q, want the upstream's", body)
	}
	if gotHost != host {
		t.Errorf("upstream saw Host %q, want the inbound %q passed through unchanged", gotHost, host)
	}
	if gotPath != "/some/path" {
		t.Errorf("upstream saw path %q, want %q", gotPath, "/some/path")
	}
}

// An upstream that is not listening is a 502, not a panic or a hang.
func TestPublicHostnameDemuxDeadUpstreamIsABadGateway(t *testing.T) {
	prevOffset, prevStart, prevEnd := proxyPortOffset, previewPortStart, previewPortEnd
	t.Cleanup(func() {
		proxyPortOffset, previewPortStart, previewPortEnd = prevOffset, prevStart, prevEnd
	})
	// A port in the allowlist that nothing is listening on.
	proxyPortOffset = 0
	previewPortStart, previewPortEnd = 1, 1

	h, _ := demuxProbe(t, "example.com", 1977)
	req := httptest.NewRequest("GET", "http://1.example.com/", nil)
	req.Host = "1.example.com"
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusBadGateway {
		t.Errorf("dead upstream: got %d, want 502", rr.Code)
	}
}
