package main

import "testing"

// An explicitly passed flag beats the env var; the env fills in when the flag
// was not passed. Same precedence as every other setting in main().
func TestResolvePublicHostnamePrecedence(t *testing.T) {
	env := func(m map[string]string) func(string) (string, bool) {
		return func(k string) (string, bool) {
			v, ok := m[k]
			return v, ok
		}
	}
	tests := []struct {
		name    string
		flagVal string
		flagSet bool
		env     map[string]string
		want    string
	}{
		{"nothing set", "", false, nil, ""},
		{"env only", "", false, map[string]string{"SWE_PUBLIC_HOSTNAME": "example.com"}, "example.com"},
		{"flag only", "example.com", true, nil, "example.com"},
		{"flag wins over env", "flag.example", true, map[string]string{"SWE_PUBLIC_HOSTNAME": "env.example"}, "flag.example"},
		{"explicit empty flag disables env", "", true, map[string]string{"SWE_PUBLIC_HOSTNAME": "env.example"}, ""},
		{"blank env is ignored", "", false, map[string]string{"SWE_PUBLIC_HOSTNAME": "   "}, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := resolvePublicHostname(tc.flagVal, tc.flagSet, env(tc.env))
			if got != tc.want {
				t.Errorf("resolvePublicHostname(%q, %v, env) = %q, want %q", tc.flagVal, tc.flagSet, got, tc.want)
			}
		})
	}
}

func TestNormalizePublicHostnameAccepts(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"", ""},
		{"example.com", "example.com"},
		{"  example.com  ", "example.com"},
		{"EXAMPLE.COM", "example.com"},
		{"*.example.com", "example.com"},
		{"example.com.", "example.com"},
		{"https://example.com", "example.com"},
		{"http://example.com/", "example.com"},
		{"https://*.dev.example.com/", "dev.example.com"},
		{"lvh.me", "lvh.me"},
		{"my-box.example.co.uk", "my-box.example.co.uk"},
	}
	for _, tc := range tests {
		t.Run(tc.in, func(t *testing.T) {
			got, err := normalizePublicHostname(tc.in)
			if err != nil {
				t.Fatalf("normalizePublicHostname(%q) errored: %v", tc.in, err)
			}
			if got != tc.want {
				t.Errorf("normalizePublicHostname(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestNormalizePublicHostnameRejects(t *testing.T) {
	tests := []struct {
		name string
		in   string
	}{
		{"port included", "example.com:1977"},
		{"url with port", "https://example.com:1977/"},
		{"ipv4 literal", "203.0.113.5"},
		{"ipv6 literal", "2001:db8::1"},
		{"single label", "localhost"},
		{"single label after wildcard", "*.localhost"},
		{"leading dot", ".example.com"},
		{"double dot", "example..com"},
		{"leading hyphen label", "-bad.example.com"},
		{"trailing hyphen label", "bad-.example.com"},
		{"underscore", "my_box.example.com"},
		{"whitespace inside", "my box.example.com"},
		{"wildcard only", "*."},
		{"scheme only", "https://"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := normalizePublicHostname(tc.in)
			if err == nil {
				t.Fatalf("normalizePublicHostname(%q) = %q, want an error", tc.in, got)
			}
		})
	}
}

// The mode is host-runtime only: ADR-0043 break 5 (Traefik's Host() rules
// cannot match a wildcard, and its certificate is pinned to one domain).
func TestRequirePublicHostnameHostRuntime(t *testing.T) {
	tests := []struct {
		mode    string
		wantErr bool
	}{
		{"host", false},
		{"container", true},
		{"container-with-docker-socket", true},
		{"", true},
	}
	for _, tc := range tests {
		t.Run("runtime="+tc.mode, func(t *testing.T) {
			err := requirePublicHostnameHostRuntime(tc.mode)
			if tc.wantErr && err == nil {
				t.Fatalf("requirePublicHostnameHostRuntime(%q) = nil, want an error", tc.mode)
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("requirePublicHostnameHostRuntime(%q) errored: %v", tc.mode, err)
			}
		})
	}
}

func TestResolveConfiguredPublicHostname(t *testing.T) {
	const tunnelURL = "https://tunnel.example.com"
	tests := []struct {
		name          string
		raw           string
		rawExplicit   bool
		tunnel        string
		tunnelExplict bool
		runtime       string
		want          string
		wantDropTun   bool
		wantNote      bool
		wantErr       bool
	}{
		{name: "off by default", runtime: "host"},
		{name: "off leaves the tunnel alone", tunnel: tunnelURL, runtime: "container"},
		{name: "host runtime accepts", raw: "*.example.com", rawExplicit: true, runtime: "host", want: "example.com"},
		{name: "compose rejects", raw: "example.com", rawExplicit: true, runtime: "container", wantErr: true},
		{name: "compose with docker socket rejects", raw: "example.com", rawExplicit: true, runtime: "container-with-docker-socket", wantErr: true},
		{name: "undeclared runtime rejects", raw: "example.com", rawExplicit: true, runtime: "", wantErr: true},
		{name: "bad hostname rejects before anything else", raw: "example.com:1977", rawExplicit: true, runtime: "host", wantErr: true},
		{name: "blank tunnel is not set", raw: "example.com", rawExplicit: true, tunnel: "   ", runtime: "host", want: "example.com"},

		// A flag cannot be inherited by accident; a variable can. A box
		// carrying a stale SWE_TUNNEL_SERVER_URL must not defeat an
		// explicit --public-hostname.
		{
			name: "explicit flag beats inherited tunnel env",
			raw:  "example.com", rawExplicit: true,
			tunnel: tunnelURL, tunnelExplict: false,
			runtime: "host", want: "example.com", wantDropTun: true, wantNote: true,
		},
		{
			name: "explicit tunnel flag beats inherited hostname env",
			raw:  "example.com", rawExplicit: false,
			tunnel: tunnelURL, tunnelExplict: true,
			runtime: "host", want: "", wantNote: true,
		},
		{
			name: "both explicit is a hard error",
			raw:  "example.com", rawExplicit: true,
			tunnel: tunnelURL, tunnelExplict: true,
			runtime: "host", wantErr: true,
		},
		{
			name: "both inherited is a hard error",
			raw:  "example.com", rawExplicit: false,
			tunnel: tunnelURL, tunnelExplict: false,
			runtime: "host", wantErr: true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := resolveConfiguredPublicHostname(tc.raw, tc.rawExplicit, tc.tunnel, tc.tunnelExplict, tc.runtime)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("resolveConfiguredPublicHostname(%q, %v, %q, %v, %q) = %+v, want an error", tc.raw, tc.rawExplicit, tc.tunnel, tc.tunnelExplict, tc.runtime, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("resolveConfiguredPublicHostname(%q, %v, %q, %v, %q) errored: %v", tc.raw, tc.rawExplicit, tc.tunnel, tc.tunnelExplict, tc.runtime, err)
			}
			if got.Hostname != tc.want {
				t.Errorf("Hostname = %q, want %q", got.Hostname, tc.want)
			}
			if got.DropTunnel != tc.wantDropTun {
				t.Errorf("DropTunnel = %v, want %v", got.DropTunnel, tc.wantDropTun)
			}
			if (got.Note != "") != tc.wantNote {
				t.Errorf("Note = %q, want a note: %v", got.Note, tc.wantNote)
			}
		})
	}
}

// The whole point of phase 1: the setting lands in the same place the tunnel's
// hostname lands, so every existing consumer (cookie domain, subdomain URL
// builders, landing page) picks it up unchanged.
func TestPublicHostnameFeedsLiveTunnelHostname(t *testing.T) {
	prev := getLiveTunnelHostname()
	t.Cleanup(func() { setLiveTunnelHostname(prev) })

	decided, err := resolveConfiguredPublicHostname("*.example.com", true, "", false, "host")
	if err != nil {
		t.Fatalf("resolveConfiguredPublicHostname errored: %v", err)
	}
	setLiveTunnelHostname(decided.Hostname)

	if got := getLiveTunnelHostname(); got != "example.com" {
		t.Errorf("getLiveTunnelHostname() = %q, want %q", got, "example.com")
	}
	if got := resolveCookieDomain(getLiveTunnelHostname(), "23000.example.com"); got != "example.com" {
		t.Errorf("resolveCookieDomain = %q, want %q so one login covers every per-port subdomain", got, "example.com")
	}
}
