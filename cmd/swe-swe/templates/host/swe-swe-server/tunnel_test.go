package main

import (
	"encoding/json"
	"testing"
)

// TestResolvePublicHostname covers the precedence rules for the tunnel-mode
// public hostname: CLI flag wins over env var, both empty means tunnel mode
// is off (legacy behavior unchanged).
func TestResolvePublicHostname(t *testing.T) {
	cases := []struct {
		name string
		flag string
		env  string
		want string
	}{
		{name: "both empty -> off", flag: "", env: "", want: ""},
		{name: "flag only", flag: "abc-tunnel.example.com", env: "", want: "abc-tunnel.example.com"},
		{name: "env only", flag: "", env: "xyz-tunnel.example.com", want: "xyz-tunnel.example.com"},
		{name: "flag wins over env", flag: "flag.example.com", env: "env.example.com", want: "flag.example.com"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := resolvePublicHostname(tc.flag, tc.env)
			if got != tc.want {
				t.Errorf("resolvePublicHostname(%q, %q) = %q, want %q", tc.flag, tc.env, got, tc.want)
			}
		})
	}
}

// TestBuildStatusPayloadIncludesPublicHostname asserts the WS status frame
// carries the publicHostname field, both when set (frontend branches into
// subdomain mode) and when empty (legacy port-based mode).
func TestBuildStatusPayloadIncludesPublicHostname(t *testing.T) {
	cases := []struct {
		name string
		host string
	}{
		{name: "set", host: "abc-tunnel.example.com"},
		{name: "empty", host: ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := &Session{
				UUID:            "11111111-2222-3333-4444-555555555555",
				WorkDir:         "/workspace",
				AssistantConfig: AssistantConfig{Name: "claude"},
				SessionMode:     "terminal",
				PublicHostname:  tc.host,
			}
			payload := s.buildStatusPayload(0, uint16(24), uint16(80))

			got, ok := payload["publicHostname"]
			if !ok {
				t.Fatalf("publicHostname key missing from status payload")
			}
			gotStr, ok := got.(string)
			if !ok {
				t.Fatalf("publicHostname is %T, want string", got)
			}
			if gotStr != tc.host {
				t.Errorf("publicHostname = %q, want %q", gotStr, tc.host)
			}

			// Round-trip through JSON to confirm the field actually
			// makes it onto the wire (json.Marshal can drop fields).
			data, err := json.Marshal(payload)
			if err != nil {
				t.Fatalf("Marshal: %v", err)
			}
			var roundTrip map[string]interface{}
			if err := json.Unmarshal(data, &roundTrip); err != nil {
				t.Fatalf("Unmarshal: %v", err)
			}
			rt, _ := roundTrip["publicHostname"].(string)
			if rt != tc.host {
				t.Errorf("after JSON round-trip publicHostname = %q, want %q", rt, tc.host)
			}
		})
	}
}
