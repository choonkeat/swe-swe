package main

import (
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"testing"

	agentproxy "github.com/choonkeat/agent-reverse-proxy"
)

// withSinglePortMode flips the package-level setting for one test and puts it
// back afterwards. The real value is written once in main(); tests are the
// only other writer.
func withSinglePortMode(t *testing.T, on bool) {
	t.Helper()
	prev := singlePortMode
	singlePortMode = on
	t.Cleanup(func() { singlePortMode = prev })
}

// The precedence rule has to be the SAME one -public-hostname uses, or an
// operator who learned it once learns it wrong the second time: an explicitly
// passed flag beats the environment, the environment beats the default.
func TestResolveSinglePort(t *testing.T) {
	env := func(pairs map[string]string) func(string) (string, bool) {
		return func(k string) (string, bool) {
			v, ok := pairs[k]
			return v, ok
		}
	}
	none := env(nil)

	tests := []struct {
		name      string
		flagVal   bool
		flagSet   bool
		lookupEnv func(string) (string, bool)
		want      bool
	}{
		{"default off", false, false, none, false},
		{"flag on", true, true, none, true},
		{"env 1", false, false, env(map[string]string{"SWE_SINGLE_PORT": "1"}), true},
		{"env true", false, false, env(map[string]string{"SWE_SINGLE_PORT": "true"}), true},
		{"env TRUE", false, false, env(map[string]string{"SWE_SINGLE_PORT": "TRUE"}), true},
		{"env yes", false, false, env(map[string]string{"SWE_SINGLE_PORT": "yes"}), true},
		{"env on", false, false, env(map[string]string{"SWE_SINGLE_PORT": " on "}), true},
		{"env 0", false, false, env(map[string]string{"SWE_SINGLE_PORT": "0"}), false},
		{"env false", false, false, env(map[string]string{"SWE_SINGLE_PORT": "false"}), false},
		// The compose passthrough writes SWE_SINGLE_PORT=${SWE_SINGLE_PORT:-},
		// so "present but empty" is how EVERY default deployment looks. It
		// must mean off, not "present therefore on".
		{"env present but empty", false, false, env(map[string]string{"SWE_SINGLE_PORT": ""}), false},
		{"env garbage is off", false, false, env(map[string]string{"SWE_SINGLE_PORT": "maybe"}), false},
		// An explicit flag is never overruled by the environment, in either
		// direction: -single-port=false on a box whose env says 1 must win.
		{"flag off beats env on", false, true, env(map[string]string{"SWE_SINGLE_PORT": "1"}), false},
		{"flag on beats env off", true, true, env(map[string]string{"SWE_SINGLE_PORT": "0"}), true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := resolveSinglePort(tc.flagVal, tc.flagSet, tc.lookupEnv); got != tc.want {
				t.Errorf("resolveSinglePort(%v, %v) = %v, want %v", tc.flagVal, tc.flagSet, got, tc.want)
			}
		})
	}
}

// Both wildcard mode and tunnel mode reach the panes THROUGH the per-session
// proxy ports; single-port mode is the instruction not to open them. Starting
// with both is not a degraded setup, it is a dead one -- so it has to fail at
// boot, naming both settings.
func TestSinglePortConflicts(t *testing.T) {
	t.Run("off: no combination is a conflict", func(t *testing.T) {
		if err := singlePortConflicts(false, "example.com", "https://tunnel.example.com"); err != nil {
			t.Fatalf("singlePortConflicts(false, ...) = %v, want nil", err)
		}
	})

	t.Run("on, alone: fine", func(t *testing.T) {
		if err := singlePortConflicts(true, "", ""); err != nil {
			t.Fatalf("singlePortConflicts(true, \"\", \"\") = %v, want nil", err)
		}
		if err := singlePortConflicts(true, "   ", "  "); err != nil {
			t.Fatalf("whitespace-only settings must count as unset, got %v", err)
		}
	})

	for _, tc := range []struct {
		name           string
		publicHostname string
		tunnelURL      string
		wantMentions   []string
	}{
		{"with public hostname", "example.com", "", []string{"-single-port", "-public-hostname"}},
		{"with tunnel", "", "https://tunnel.example.com", []string{"-single-port", "-tunnel-server-url"}},
		{"with both", "example.com", "https://tunnel.example.com", []string{"-single-port", "-public-hostname", "-tunnel-server-url"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := singlePortConflicts(true, tc.publicHostname, tc.tunnelURL)
			if err == nil {
				t.Fatal("want an error, got nil")
			}
			for _, want := range tc.wantMentions {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("error must name %s so the operator knows what to drop; got: %v", want, err)
				}
			}
		})
	}
}

// The frontend decides "port form or path form" from what the status payload
// advertises: Preview, Agent Chat and Files all build their port candidate
// from the advertised proxy port and skip it when it is missing. So omitting
// the four keys IS the instruction to go straight to the path form, with no
// probe and no 5s wait. The real target ports stay -- those are actual
// servers, and the agent tooling addresses them directly.
func TestSinglePortStatusPayloadOmitsProxyPorts(t *testing.T) {
	newSession := func() *Session {
		return &Session{
			UUID:            "11111111-2222-3333-4444-555555555555",
			WorkDir:         "/workspace",
			AssistantConfig: AssistantConfig{Name: "claude"},
			SessionMode:     "chat",
			PreviewPort:     3000,
			AgentChatPort:   4000,
			PublicPort:      5000,
			CDPPort:         6000,
			VNCPort:         7000,
			FilesPort:       9000,
		}
	}
	proxyKeys := []string{"previewProxyPort", "agentChatProxyPort", "vncProxyPort", "filesProxyPort"}

	t.Run("default mode advertises every proxy port", func(t *testing.T) {
		withSinglePortMode(t, false)
		payload := newSession().buildStatusPayload(0, 24, 80)
		for _, k := range proxyKeys {
			if _, ok := payload[k]; !ok {
				t.Errorf("default mode must still advertise %s", k)
			}
		}
	})

	t.Run("single-port mode advertises none of them", func(t *testing.T) {
		withSinglePortMode(t, true)
		payload := newSession().buildStatusPayload(0, 24, 80)
		for _, k := range proxyKeys {
			if v, ok := payload[k]; ok {
				t.Errorf("single-port mode must omit %s entirely (got %v): a port that is advertised is a port the frontend will probe and wait on", k, v)
			}
		}
	})

	// Agent View's tab is the one that must NOT disappear along with its
	// proxy port: agentViewAvailable is the signal that says the pane exists,
	// and phase 2 makes the frontend use it instead of vncProxyPort.
	t.Run("single-port mode keeps the real target ports and the availability signal", func(t *testing.T) {
		withSinglePortMode(t, true)
		payload := newSession().buildStatusPayload(0, 24, 80)
		for _, k := range []string{"previewPort", "cdpPort", "vncPort", "agentChatPort", "agentViewAvailable"} {
			if _, ok := payload[k]; !ok {
				t.Errorf("single-port mode must still carry %s", k)
			}
		}
	})
}

// The headline assertion, and the one no browser-side test can make: with the
// mode on, nothing binds the per-session proxy ports at all.
// e2e/tests/proxy-fallback.spec.js blocks those ports AT THE BROWSER, which
// proves the frontend finds its way around them -- it cannot see that the
// server went on listening on all 80 of them.
func TestSinglePortStartsNoPerPortListeners(t *testing.T) {
	// Pick proxy ports the OS says are free right now, and derive the session
	// ports from them, so the test never collides with a swe-swe-server
	// actually running on this box in the 23000/24000/27000/29000 bands.
	free := func() int {
		ln, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatalf("find a free port: %v", err)
		}
		defer ln.Close()
		return ln.Addr().(*net.TCPAddr).Port
	}
	// Four DISTINCT session ports: two equal ones would mean the second
	// listener legitimately fails to bind, which would read as "single-port
	// mode worked" in the default-mode case.
	bases := func(n int) []int {
		seen := map[int]bool{}
		out := []int{}
		for len(out) < n {
			p := free()
			if p <= proxyPortOffset {
				t.Skipf("ephemeral port %d is below the proxy offset %d; cannot derive a session port", p, proxyPortOffset)
			}
			if seen[p] {
				continue
			}
			seen[p] = true
			out = append(out, p-proxyPortOffset)
		}
		return out
	}

	run := func(t *testing.T, single bool) []int {
		t.Helper()
		withSinglePortMode(t, single)
		b := bases(4)
		previewPort, acPort, vncPort, filesPort := b[0], b[1], b[2], b[3]
		sess := &Session{
			UUID:            "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee",
			AssistantConfig: AssistantConfig{Name: "claude"},
			PreviewPort:     previewPort,
			AgentChatPort:   acPort,
			VNCPort:         vncPort,
			FilesPort:       filesPort,
		}
		// Close the listeners directly rather than sess.Close(), which also
		// writes metadata and tears down processes this session never had.
		t.Cleanup(func() {
			for _, srv := range []*http.Server{
				sess.PreviewProxyServer, sess.AgentChatProxyServer,
				sess.VNCProxyServer, sess.FilesProxyServer,
			} {
				if srv != nil {
					srv.Close()
				}
			}
		})

		startPerSessionProxyListeners(perSessionProxyDeps{
			Session:       sess,
			PreviewTarget: &url.URL{Scheme: "http", Host: fmt.Sprintf("localhost:%d", previewPort)},
			PreviewPort:   previewPort,
			AgentChatPort: acPort,
			VNCPort:       vncPort,
			VNCProxy:      newVNCReverseProxy(sess, vncPort),
			Hub:           agentproxy.NewDebugHub(),
		})
		return []int{
			previewProxyPort(previewPort),
			agentChatProxyPort(acPort),
			vncProxyPort(vncPort),
			filesProxyPort(filesPort),
		}
	}

	bound := func(port int) bool {
		ln, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
		if err != nil {
			return true
		}
		ln.Close()
		return false
	}

	t.Run("default mode binds all four", func(t *testing.T) {
		for _, port := range run(t, false) {
			if !bound(port) {
				t.Errorf("default mode must still bind proxy port %d", port)
			}
		}
	})

	t.Run("single-port mode binds none", func(t *testing.T) {
		for _, port := range run(t, true) {
			if bound(port) {
				t.Errorf("proxy port %d is listening in single-port mode: unreachable by definition, so it is pure attack surface", port)
			}
		}
	})
}
