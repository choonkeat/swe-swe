package main

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestResolveListenAddr(t *testing.T) {
	// Decision rule cases, extended with --bind / SWE_BIND for tunnel mode
	// (see auth.go cookie-domain rationale; the symmetric idea here is "only
	// localhost tunnel client should reach swe-swe-server when behind a
	// tunnel").
	cases := []struct {
		name        string
		flagBind    string
		flagAddr    string
		envSweBind  string
		envSwePort  string
		envPort     string
		wantListen  string
		wantLanding string
	}{
		{
			name:        "addr explicit, no env",
			flagAddr:    "0.0.0.0:9898",
			wantListen:  "0.0.0.0:9898",
			wantLanding: "",
		},
		{
			name:        "addr explicit, PORT differs -> landing on PORT",
			flagAddr:    "0.0.0.0:1977",
			envPort:     "8080",
			wantListen:  "0.0.0.0:1977",
			wantLanding: ":8080",
		},
		{
			name:        "addr explicit, PORT same as addr -> no landing",
			flagAddr:    "0.0.0.0:8080",
			envPort:     "8080",
			wantListen:  "0.0.0.0:8080",
			wantLanding: "",
		},
		{
			name:        "addr unset, SWE_PORT wins",
			envSwePort:  "1977",
			envPort:     "8080",
			wantListen:  ":1977",
			wantLanding: ":8080",
		},
		{
			name:        "addr unset, SWE_PORT unset, PORT binds directly",
			envPort:     "8080",
			wantListen:  ":8080",
			wantLanding: "",
		},
		{
			name:        "nothing set -> default :1977",
			wantListen:  ":1977",
			wantLanding: "",
		},
		{
			name:        "SWE_PORT == PORT -> no landing",
			envSwePort:  "8080",
			envPort:     "8080",
			wantListen:  ":8080",
			wantLanding: "",
		},
		// --bind / SWE_BIND: tunnel-mode listen-address restriction.
		{
			name:        "bind flag wins over everything",
			flagBind:    "127.0.0.1:9898",
			flagAddr:    "0.0.0.0:8080",
			envSweBind:  "127.0.0.1:7777",
			envSwePort:  "1977",
			envPort:     "8080",
			wantListen:  "127.0.0.1:9898",
			wantLanding: ":8080",
		},
		{
			name:        "bind flag, PORT same as bind -> no landing",
			flagBind:    "127.0.0.1:8080",
			envPort:     "8080",
			wantListen:  "127.0.0.1:8080",
			wantLanding: "",
		},
		{
			name:        "bind flag empty, addr flag wins over SWE_BIND env",
			flagAddr:    "0.0.0.0:9898",
			envSweBind:  "127.0.0.1:7777",
			wantListen:  "0.0.0.0:9898",
			wantLanding: "",
		},
		{
			name:        "bind/addr flags empty, SWE_BIND wins over SWE_PORT and PORT",
			envSweBind:  "127.0.0.1:9898",
			envSwePort:  "1977",
			envPort:     "8080",
			wantListen:  "127.0.0.1:9898",
			wantLanding: ":8080",
		},
		{
			name:        "SWE_BIND port equals PORT -> no landing",
			envSweBind:  "127.0.0.1:8080",
			envPort:     "8080",
			wantListen:  "127.0.0.1:8080",
			wantLanding: "",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gotListen, gotLanding := resolveListenAddr(tc.flagBind, tc.flagAddr, tc.envSweBind, tc.envSwePort, tc.envPort)
			if gotListen != tc.wantListen {
				t.Errorf("listen: got %q, want %q", gotListen, tc.wantListen)
			}
			if gotLanding != tc.wantLanding {
				t.Errorf("landing: got %q, want %q", gotLanding, tc.wantLanding)
			}
		})
	}
}

func TestStartLandingServerNoopOnEmptyAddr(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if got := startLandingServer(ctx, "", ":9898"); got != nil {
		t.Errorf("empty landingAddr must not start a server, got %v", got)
	}
}

func TestLandingServerHandlers(t *testing.T) {
	// Bind to :0 to grab an ephemeral port -- avoids collisions in CI.
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("OK"))
	})
	body := renderLandingHTML(":1977", "")
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(body))
	})
	ts := httptest.NewServer(mux)
	defer ts.Close()

	// /health -> 200 OK
	resp, err := http.Get(ts.URL + "/health")
	if err != nil {
		t.Fatalf("GET /health: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Errorf("/health status: got %d, want 200", resp.StatusCode)
	}
	b, _ := io.ReadAll(resp.Body)
	if string(b) != "OK" {
		t.Errorf("/health body: got %q, want OK", b)
	}

	// / -> HTML mentioning swe-swe and the SWE port.
	resp, err = http.Get(ts.URL + "/")
	if err != nil {
		t.Fatalf("GET /: %v", err)
	}
	defer resp.Body.Close()
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Errorf("/ Content-Type: got %q, want text/html*", ct)
	}
	b, _ = io.ReadAll(resp.Body)
	s := string(b)
	if !strings.Contains(s, "swe-swe") {
		t.Errorf("landing HTML should mention swe-swe; body=\n%s", s)
	}
}

func TestRenderLandingHTMLNoTunnel(t *testing.T) {
	// No tunnel registered -> generic placeholder copy, no tunnel link.
	s := renderLandingHTML(":1977", "")
	if !strings.Contains(s, "No tunnel is registered yet") {
		t.Errorf("no tunnel -> should render placeholder copy; body=\n%s", s)
	}
	if strings.Contains(s, "https://1977.") {
		t.Errorf("no tunnel -> should not emit a tunnel URL; body=\n%s", s)
	}
}

func TestRenderLandingHTMLLearnMoreOverride(t *testing.T) {
	t.Setenv("SWE_LANDING_URL", "https://example.org/docs")
	s := renderLandingHTML(":1977", "")
	if !strings.Contains(s, "example.org/docs") {
		t.Errorf("SWE_LANDING_URL override should appear; body=\n%s", s)
	}
}

// TestRenderLandingHTMLTunnelMode covers the PaaS-friendly path: when
// the supervisor has registered a tunnel hostname, the landing page
// must surface a click-through https://{port}.{hostname}/ link.
func TestRenderLandingHTMLTunnelMode(t *testing.T) {
	s := renderLandingHTML(":9898", "alpha-tunnel.example.com")
	wantURL := "https://9898.alpha-tunnel.example.com/"
	if !strings.Contains(s, wantURL) {
		t.Errorf("tunnel mode should expose %q; body=\n%s", wantURL, s)
	}
	if strings.Contains(s, "No tunnel is registered yet") {
		t.Errorf("tunnel mode should NOT show the no-tunnel placeholder; body=\n%s", s)
	}
}

func TestLandingServerShutdownOnCtxCancel(t *testing.T) {
	// Regression check: ctx cancellation must trigger srv.Shutdown.  Pick a
	// free port by binding :0 then closing so startLandingServer can rebind.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Skipf("cannot bind local port: %v", err)
	}
	addr := ln.Addr().String()
	_ = ln.Close()

	ctx, cancel := context.WithCancel(context.Background())
	srv := startLandingServer(ctx, addr, ":1977")
	if srv == nil {
		t.Fatal("expected non-nil server")
	}

	// Wait until /health answers before canceling.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := http.Get("http://" + addr + "/health")
		if err == nil {
			resp.Body.Close()
			break
		}
		time.Sleep(25 * time.Millisecond)
	}

	cancel()

	// Shutdown goroutine has up to 5s; poll for the listener to stop.
	deadline = time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := http.Get("http://" + addr + "/health")
		if err != nil {
			return // listener is down, success
		}
		resp.Body.Close()
		time.Sleep(50 * time.Millisecond)
	}
	t.Error("landing server still serving after ctx cancel")
}
