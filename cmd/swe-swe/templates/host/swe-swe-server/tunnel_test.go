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

// TestBuildStatusPayloadIncludesTunnelStatus asserts the WS status frame
// carries tunnelStatus when the supervisor has observed at least one
// event. Without this, the frontend can't distinguish "rate-limited;
// retrying in 5m" from a generic indefinite spinner.
func TestBuildStatusPayloadIncludesTunnelStatus(t *testing.T) {
	saved := liveTunnelStatus.Load()
	t.Cleanup(func() {
		if saved == nil {
			liveTunnelStatus.Store(nil)
		} else {
			liveTunnelStatus.Store(saved)
		}
	})

	cases := []struct {
		name           string
		set            *tunnelStatusInfo
		wantPresent    bool
		wantState      string
		wantRetryAfter float64
		wantReason     string
	}{
		{
			name:        "absent when no events observed",
			set:         nil,
			wantPresent: false,
		},
		{
			name:        "connected state passes through",
			set:         &tunnelStatusInfo{State: "connected"},
			wantPresent: true,
			wantState:   "connected",
		},
		{
			name:           "reconnecting carries retryAfterMs and reason",
			set:            &tunnelStatusInfo{State: "reconnecting", RetryAfterMs: 300000, Reason: "rate_limited"},
			wantPresent:    true,
			wantState:      "reconnecting",
			wantRetryAfter: 300000,
			wantReason:     "rate_limited",
		},
		{
			name:        "fatal carries reason",
			set:         &tunnelStatusInfo{State: "fatal", Reason: "key_mismatch"},
			wantPresent: true,
			wantState:   "fatal",
			wantReason:  "key_mismatch",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.set == nil {
				liveTunnelStatus.Store(&tunnelStatusInfo{}) // State="" -> absent
			} else {
				liveTunnelStatus.Store(tc.set)
			}

			s := &Session{
				UUID:            "11111111-2222-3333-4444-555555555555",
				WorkDir:         "/workspace",
				AssistantConfig: AssistantConfig{Name: "claude"},
				SessionMode:     "terminal",
			}
			payload := s.buildStatusPayload(0, uint16(24), uint16(80))

			// Round-trip through JSON since that's what the frontend sees.
			data, err := json.Marshal(payload)
			if err != nil {
				t.Fatalf("Marshal: %v", err)
			}
			var roundTrip map[string]interface{}
			if err := json.Unmarshal(data, &roundTrip); err != nil {
				t.Fatalf("Unmarshal: %v", err)
			}
			got, ok := roundTrip["tunnelStatus"]
			if tc.wantPresent != ok {
				t.Fatalf("tunnelStatus present=%v, want %v (payload=%s)", ok, tc.wantPresent, data)
			}
			if !tc.wantPresent {
				return
			}
			obj, ok := got.(map[string]interface{})
			if !ok {
				t.Fatalf("tunnelStatus is %T, want object", got)
			}
			if state, _ := obj["state"].(string); state != tc.wantState {
				t.Errorf("state = %q, want %q", state, tc.wantState)
			}
			if reason, _ := obj["reason"].(string); reason != tc.wantReason {
				t.Errorf("reason = %q, want %q", reason, tc.wantReason)
			}
			if tc.wantRetryAfter > 0 {
				ra, _ := obj["retryAfterMs"].(float64)
				if ra != tc.wantRetryAfter {
					t.Errorf("retryAfterMs = %v, want %v", ra, tc.wantRetryAfter)
				}
			} else {
				if _, has := obj["retryAfterMs"]; has {
					t.Errorf("retryAfterMs unexpectedly present (omitempty should drop zero)")
				}
			}
		})
	}
}
