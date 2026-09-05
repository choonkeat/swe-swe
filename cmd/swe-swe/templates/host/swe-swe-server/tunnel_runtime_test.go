package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func resetTunnelRuntime(t *testing.T) {
	t.Helper()
	prevHost := getLiveTunnelHostname()
	prevStatus := getLiveTunnelStatus()
	liveTunnelRuntime.mu.Lock()
	prevBase, prevConfigured := liveTunnelRuntime.base, liveTunnelRuntime.configured
	prevURL, prevUnique := liveTunnelRuntime.serverURL, liveTunnelRuntime.unique
	liveTunnelRuntime.mu.Unlock()
	t.Cleanup(func() {
		liveTunnelRuntime.mu.Lock()
		if liveTunnelRuntime.cancel != nil {
			liveTunnelRuntime.cancel()
		}
		liveTunnelRuntime.base = prevBase
		liveTunnelRuntime.cancel = nil
		liveTunnelRuntime.configured = prevConfigured
		liveTunnelRuntime.serverURL = prevURL
		liveTunnelRuntime.unique = prevUnique
		liveTunnelRuntime.mu.Unlock()
		setLiveTunnelHostname(prevHost)
		setLiveTunnelStatus(prevStatus)
	})
}

func TestValidateTunnelConfig(t *testing.T) {
	cases := []struct {
		url, unique string
		ok          bool
	}{
		{"https://tunnel.example.com", "mybox", true},
		{"http://localhost:8443", "box-1", true},
		{"tunnel.example.com", "mybox", false},
		{"ftp://x", "mybox", false},
		{"https://tunnel.example.com", "", false},
		{"https://tunnel.example.com", "has space", false},
		{"https://tunnel.example.com", "dotted.name", false},
	}
	for _, c := range cases {
		err := validateTunnelConfig(c.url, c.unique)
		if (err == nil) != c.ok {
			t.Errorf("validate(%q,%q): err=%v, want ok=%v", c.url, c.unique, err, c.ok)
		}
	}
}

// A second configure cancels the first supervisor's context (its child dies
// with it) and the new instance carries the new unique and identity key.
func TestTunnelRuntimeConfigure_ReplacesRunningInstance(t *testing.T) {
	resetTunnelRuntime(t)
	prevBroadcast := broadcastPublicHostnameChange
	broadcastPublicHostnameChange = func() {}
	t.Cleanup(func() { broadcastPublicHostnameChange = prevBroadcast })

	type started struct {
		ctx  context.Context
		opts tunnelSupervisorOpts
	}
	starts := make(chan started, 4)
	initTunnelRuntime(tunnelSupervisorOpts{
		BinPath:    "/fake/swe-swe-tunnel",
		LocalAddr:  "127.0.0.1:1977",
		MinBackoff: time.Hour, MaxBackoff: time.Hour,
	})
	// The base opts' startChild is copied into every instance; it records
	// the ctx it was started under and blocks until that ctx is cancelled.
	liveTunnelRuntime.mu.Lock()
	liveTunnelRuntime.base.startChild = nil
	liveTunnelRuntime.mu.Unlock()
	withFake := func(opts tunnelSupervisorOpts) tunnelSupervisorOpts {
		opts.startChild = func(ctx context.Context) (childProcess, error) {
			starts <- started{ctx: ctx, opts: opts}
			c := newFakeChild(nil, nil, 4242)
			go func() { <-ctx.Done(); c.Kill() }()
			return c, nil
		}
		return opts
	}
	// Drive start() directly with fake-equipped opts (configure() would
	// exec a real binary).
	first := withFake(tunnelSupervisorOpts{ServerURL: "https://t.example.com", Unique: "one", BinPath: "/fake", LocalAddr: "127.0.0.1:1977", MinBackoff: time.Hour, MaxBackoff: time.Hour})
	liveTunnelRuntime.start(context.Background(), first)
	var s1 started
	select {
	case s1 = <-starts:
	case <-time.After(3 * time.Second):
		t.Fatal("first instance never started its child")
	}
	if s1.opts.Unique != "one" {
		t.Fatalf("first unique = %q", s1.opts.Unique)
	}

	second := withFake(tunnelSupervisorOpts{ServerURL: "https://t.example.com", Unique: "two", IdentityKey: "k2", BinPath: "/fake", LocalAddr: "127.0.0.1:1977", MinBackoff: time.Hour, MaxBackoff: time.Hour})
	liveTunnelRuntime.start(context.Background(), second)
	select {
	case <-s1.ctx.Done():
	case <-time.After(3 * time.Second):
		t.Fatal("first instance's context was not cancelled by the second start")
	}
	var s2 started
	select {
	case s2 = <-starts:
	case <-time.After(3 * time.Second):
		t.Fatal("second instance never started its child")
	}
	if s2.opts.Unique != "two" || s2.opts.IdentityKey != "k2" {
		t.Errorf("second instance opts = unique %q key %q", s2.opts.Unique, s2.opts.IdentityKey)
	}
	snap := liveTunnelRuntime.snapshot()
	if !snap.Configured || snap.Unique != "two" || snap.ServerURL != "https://t.example.com" {
		t.Errorf("snapshot = %+v", snap)
	}
	if b, _ := json.Marshal(snap); strings.Contains(string(b), "k2") {
		t.Error("snapshot leaked the identity key")
	}
}

// configure() refuses bad input before touching the running instance, and
// refuses to start when the server has no tunnel binary to run.
func TestTunnelRuntimeConfigure_Validation(t *testing.T) {
	resetTunnelRuntime(t)
	initTunnelRuntime(tunnelSupervisorOpts{LocalAddr: "127.0.0.1:1977"})
	if err := liveTunnelRuntime.configure("nope", "x", ""); err == nil {
		t.Error("bad URL accepted")
	}
	if err := liveTunnelRuntime.configure("https://t.example.com", "x", ""); err == nil || !strings.Contains(err.Error(), "tunnel-bin") {
		t.Errorf("missing BinPath: err=%v", err)
	}
	if liveTunnelRuntime.snapshot().Configured {
		t.Error("a rejected configure must not mark the runtime configured")
	}
}

func TestHandleServerTunnelAPI(t *testing.T) {
	resetTunnelRuntime(t)
	initTunnelRuntime(tunnelSupervisorOpts{LocalAddr: "127.0.0.1:1977"})
	setLiveTunnelHostname("")

	rr := httptest.NewRecorder()
	handleServerTunnelAPI(rr, httptest.NewRequest(http.MethodGet, "/api/server/tunnel", nil))
	if rr.Code != http.StatusOK || !strings.Contains(rr.Body.String(), `"configured":false`) {
		t.Fatalf("GET: %d %s", rr.Code, rr.Body.String())
	}

	rr = httptest.NewRecorder()
	handleServerTunnelAPI(rr, httptest.NewRequest(http.MethodPost, "/api/server/tunnel", strings.NewReader(`{"serverUrl":"bogus","unique":"a","identityKey":"secret"}`)))
	if rr.Code != http.StatusBadRequest || strings.Contains(rr.Body.String(), "secret") {
		t.Errorf("POST invalid: %d %s", rr.Code, rr.Body.String())
	}

	rr = httptest.NewRecorder()
	handleServerTunnelAPI(rr, httptest.NewRequest(http.MethodPut, "/api/server/tunnel", nil))
	if rr.Code != http.StatusMethodNotAllowed {
		t.Errorf("PUT: %d", rr.Code)
	}
}

func TestTunnelPublicURL(t *testing.T) {
	if got := tunnelPublicURL("127.0.0.1:1977", "mybox-tunnel.example.com"); got != "https://1977.mybox-tunnel.example.com/" {
		t.Errorf("got %q", got)
	}
	if got := tunnelPublicURL("127.0.0.1:1977", ""); got != "" {
		t.Errorf("no hostname: got %q", got)
	}
}

// The child env carries exactly one identity key: any inherited value is
// replaced, never duplicated.
func TestTunnelChildEnv(t *testing.T) {
	env := tunnelChildEnv([]string{"HOME=/h", "SWE_TUNNEL_IDENTITY_KEY=old"}, "new")
	joined := strings.Join(env, "\n")
	if strings.Contains(joined, "=old") || strings.Count(joined, "SWE_TUNNEL_IDENTITY_KEY=") != 1 || !strings.Contains(joined, "HOME=/h") {
		t.Errorf("env = %v", env)
	}
}
