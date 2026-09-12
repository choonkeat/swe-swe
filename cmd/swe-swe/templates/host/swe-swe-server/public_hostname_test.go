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

func TestResolveConfiguredPublicHostname(t *testing.T) {
	const tunnelURL = "https://tunnel.example.com"
	tests := []struct {
		name          string
		raw           string
		rawExplicit   bool
		tunnel        string
		tunnelExplict bool
		want          string
		wantDropTun   bool
		wantNote      bool
		wantErr       bool
	}{
		{name: "off by default"},
		{name: "off leaves the tunnel alone", tunnel: tunnelURL},
		{name: "accepted", raw: "*.example.com", rawExplicit: true, want: "example.com"},
		{name: "bad hostname rejected", raw: "example.com:1977", rawExplicit: true, wantErr: true},
		{name: "blank tunnel is not set", raw: "example.com", rawExplicit: true, tunnel: "   ", want: "example.com"},

		// Every runtime is allowed: the rules swe-swe generates for Traefik
		// carry no Host() matcher, so a wildcard address reaches this server
		// with its Host intact, and the default compose setup has no Traefik
		// at all.
		{name: "no runtime is refused", raw: "example.com", rawExplicit: true, want: "example.com"},

		// A flag cannot be inherited by accident; a variable can. A box
		// carrying a stale SWE_TUNNEL_SERVER_URL must not defeat an
		// explicit --public-hostname.
		{
			name: "explicit flag beats inherited tunnel env",
			raw:  "example.com", rawExplicit: true,
			tunnel: tunnelURL, tunnelExplict: false,
			want: "example.com", wantDropTun: true, wantNote: true,
		},
		{
			name: "explicit tunnel flag beats inherited hostname env",
			raw:  "example.com", rawExplicit: false,
			tunnel: tunnelURL, tunnelExplict: true,
			want: "", wantNote: true,
		},
		{
			name: "both explicit is a hard error",
			raw:  "example.com", rawExplicit: true,
			tunnel: tunnelURL, tunnelExplict: true,
			wantErr: true,
		},
		{
			name: "both inherited is a hard error",
			raw:  "example.com", rawExplicit: false,
			tunnel: tunnelURL, tunnelExplict: false,
			wantErr: true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := resolveConfiguredPublicHostname(tc.raw, tc.rawExplicit, tc.tunnel, tc.tunnelExplict)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("resolveConfiguredPublicHostname(%q, %v, %q, %v) = %+v, want an error", tc.raw, tc.rawExplicit, tc.tunnel, tc.tunnelExplict, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("resolveConfiguredPublicHostname(%q, %v, %q, %v) errored: %v", tc.raw, tc.rawExplicit, tc.tunnel, tc.tunnelExplict, err)
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

// The whole point: the setting lands in the same place the tunnel's
// hostname lands, so every existing consumer (cookie domain, subdomain URL
// builders, landing page) picks it up unchanged.
func TestPublicHostnameFeedsLiveTunnelHostname(t *testing.T) {
	prev := getLiveTunnelHostname()
	t.Cleanup(func() { setLiveTunnelHostname(prev) })

	decided, err := resolveConfiguredPublicHostname("*.example.com", true, "", false)
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
