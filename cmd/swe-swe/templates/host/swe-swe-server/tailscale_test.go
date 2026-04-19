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
	// Decision rule cases enumerated in www/swe-swe-tailscale.md.
	cases := []struct {
		name           string
		flagAddr       string
		envSwePort     string
		envPort        string
		wantListen     string
		wantLanding    string
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
			name:        "nothing set -> default :9898",
			wantListen:  ":9898",
			wantLanding: "",
		},
		{
			name:        "SWE_PORT == PORT -> no landing",
			envSwePort:  "8080",
			envPort:     "8080",
			wantListen:  ":8080",
			wantLanding: "",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gotListen, gotLanding := resolveListenAddr(tc.flagAddr, tc.envSwePort, tc.envPort)
			if gotListen != tc.wantListen {
				t.Errorf("listen: got %q, want %q", gotListen, tc.wantListen)
			}
			if gotLanding != tc.wantLanding {
				t.Errorf("landing: got %q, want %q", gotLanding, tc.wantLanding)
			}
		})
	}
}

func TestResolveTailscaleConfig(t *testing.T) {
	t.Setenv("TS_AUTHKEY", "")
	t.Setenv("TS_HOSTNAME", "")
	t.Setenv("TS_STATE_DIR", "")
	t.Setenv("TS_DISABLE", "")

	t.Run("defaults when nothing set", func(t *testing.T) {
		cfg := resolveTailscaleConfig("", "", "", false)
		if cfg.AuthKey != "" {
			t.Errorf("AuthKey: got %q, want empty", cfg.AuthKey)
		}
		if cfg.StateDir != "/var/lib/tailscale" {
			t.Errorf("StateDir: got %q, want /var/lib/tailscale", cfg.StateDir)
		}
		if cfg.Disabled {
			t.Errorf("Disabled should be false")
		}
	})

	t.Run("flags win over env", func(t *testing.T) {
		t.Setenv("TS_AUTHKEY", "from-env")
		t.Setenv("TS_HOSTNAME", "env-host")
		cfg := resolveTailscaleConfig("flag-key", "flag-host", "/custom", false)
		if cfg.AuthKey != "flag-key" {
			t.Errorf("AuthKey: got %q, want flag-key", cfg.AuthKey)
		}
		if cfg.Hostname != "flag-host" {
			t.Errorf("Hostname: got %q, want flag-host", cfg.Hostname)
		}
		if cfg.StateDir != "/custom" {
			t.Errorf("StateDir: got %q, want /custom", cfg.StateDir)
		}
	})

	t.Run("env fills when flags empty", func(t *testing.T) {
		t.Setenv("TS_AUTHKEY", "env-key")
		t.Setenv("TS_HOSTNAME", "env-host")
		t.Setenv("TS_STATE_DIR", "/env-state")
		cfg := resolveTailscaleConfig("", "", "", false)
		if cfg.AuthKey != "env-key" {
			t.Errorf("AuthKey: got %q, want env-key", cfg.AuthKey)
		}
		if cfg.Hostname != "env-host" {
			t.Errorf("Hostname: got %q, want env-host", cfg.Hostname)
		}
		if cfg.StateDir != "/env-state" {
			t.Errorf("StateDir: got %q, want /env-state", cfg.StateDir)
		}
	})

	t.Run("TS_DISABLE=1 sets Disabled", func(t *testing.T) {
		t.Setenv("TS_DISABLE", "1")
		cfg := resolveTailscaleConfig("", "", "", false)
		if !cfg.Disabled {
			t.Error("TS_DISABLE=1 should set Disabled=true")
		}
	})

	t.Run("flag disable overrides", func(t *testing.T) {
		cfg := resolveTailscaleConfig("key", "", "", true)
		if !cfg.Disabled {
			t.Error("--tailscale-disable should set Disabled=true")
		}
	})
}

func TestStartTailscaleDormant(t *testing.T) {
	// No auth key -> no-op, no panics, no child process spawned.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	startTailscale(ctx, tailscaleConfig{})
	startTailscale(ctx, tailscaleConfig{Disabled: true, AuthKey: "x"})
}

func TestStartLandingServerNoopOnEmptyAddr(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if got := startLandingServer(ctx, "", ":9898", tailscaleConfig{}); got != nil {
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
	body := renderLandingHTML(":1977", tailscaleConfig{Hostname: "mybox.ts.net"})
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

	// / -> HTML mentioning the tailnet hostname.
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
	if !strings.Contains(s, "mybox.ts.net") {
		t.Errorf("landing HTML should mention hostname; body=\n%s", s)
	}
	if !strings.Contains(s, "1977") {
		t.Errorf("landing HTML should mention SWE port; body=\n%s", s)
	}
	if !strings.Contains(s, "swe-swe") {
		t.Errorf("landing HTML should mention swe-swe; body=\n%s", s)
	}
}

func TestRenderLandingHTMLFallbackHostname(t *testing.T) {
	// No hostname configured -> generic "your tailnet hostname" fallback.
	s := renderLandingHTML(":1977", tailscaleConfig{})
	if !strings.Contains(s, "your tailnet hostname") {
		t.Errorf("no hostname -> should render fallback text; body=\n%s", s)
	}
	if strings.Contains(s, "<code></code>") {
		t.Errorf("fallback should not emit empty <code></code>; body=\n%s", s)
	}
}

func TestRenderLandingHTMLLearnMoreOverride(t *testing.T) {
	t.Setenv("SWE_LANDING_URL", "https://example.org/docs")
	s := renderLandingHTML(":1977", tailscaleConfig{})
	if !strings.Contains(s, "example.org/docs") {
		t.Errorf("SWE_LANDING_URL override should appear; body=\n%s", s)
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
	srv := startLandingServer(ctx, addr, ":1977", tailscaleConfig{})
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
