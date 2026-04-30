package main

import (
	"encoding/json"
	"testing"
)

// TestBuildStatusPayloadIncludesPublicHostname asserts the WS status frame
// carries the publicHostname field, both when set (frontend branches into
// subdomain mode) and when empty (legacy port-based mode). Source of
// truth is the supervisor's live atomic value: legacy --public-hostname
// flag and state-file fallback were retired in the subprocess pivot
// (tasks/2026-04-29-tunnel-subprocess-pivot.md).
func TestBuildStatusPayloadIncludesPublicHostname(t *testing.T) {
	saved := getLiveTunnelHostname()
	t.Cleanup(func() { setLiveTunnelHostname(saved) })

	cases := []struct {
		name string
		host string
	}{
		{name: "set", host: "abc-tunnel.example.com"},
		{name: "empty", host: ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			setLiveTunnelHostname(tc.host)

			s := &Session{
				UUID:            "11111111-2222-3333-4444-555555555555",
				WorkDir:         "/workspace",
				AssistantConfig: AssistantConfig{Name: "claude"},
				SessionMode:     "terminal",
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
