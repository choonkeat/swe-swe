package main

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestParsePreviewLabel(t *testing.T) {
	tests := []struct {
		label    string
		wantName string
		wantPort int
		wantOK   bool
	}{
		// {name}-{port}
		{"app1-5000", "app1", 5000, true},
		{"my-app-5000", "my-app", 5000, true},
		// bare {port}
		{"3001", "", 3001, true},
		// bare {name}
		{"app1", "app1", 0, true},
		{"probe-x", "probe-x", 0, true},
		// invalid
		{"-foo", "", 0, false},
		{"foo-", "", 0, false},
		{"App1", "", 0, false},                     // uppercase
		{strings.Repeat("a", 70), "", 0, false},    // too long (>63)
		{"foo-80", "", 0, false},                   // port < 1024
		{"99999", "", 0, false},                    // bare port > 65535
		{"foo-99999", "", 0, false},                // named port > 65535
		{"", "", 0, false},                         // empty
	}
	for _, tt := range tests {
		t.Run(tt.label, func(t *testing.T) {
			name, port, ok := parsePreviewLabel(tt.label)
			if ok != tt.wantOK {
				t.Fatalf("parsePreviewLabel(%q) ok = %v, want %v", tt.label, ok, tt.wantOK)
			}
			if !tt.wantOK {
				return
			}
			if name != tt.wantName || port != tt.wantPort {
				t.Errorf("parsePreviewLabel(%q) = (%q, %d), want (%q, %d)", tt.label, name, port, tt.wantName, tt.wantPort)
			}
		})
	}
}

func TestResolvePreviewVhost(t *testing.T) {
	// Default suffix lvh.me (SWE_PREVIEW_VHOST_SUFFIX unset).
	base := &Session{PreviewPort: 8080}
	pinned := &Session{PreviewPort: 8080, VhostPin: &vhostPin{Name: "app1", Port: 5000}}
	reachGuarded := &Session{PreviewPort: 8080, PreviewReachLabel: "web"}

	tests := []struct {
		name     string
		label    string
		sess     *Session
		wantPort int
		wantHost string
		wantOK   bool
	}{
		// Rule 1: {name}-{port}
		{"rule1", "app1-5000", base, 5000, "app1.lvh.me:5000", true},
		{"rule1-multidash", "my-app-5000", base, 5000, "my-app.lvh.me:5000", true},
		// Rule 2: bare {port} -> tunnel-style localhost Host
		{"rule2", "3001", base, 3001, "localhost:3001", true},
		// Rule 3: bare {name} -> primary PreviewPort vhost
		{"rule3", "app1", base, 8080, "app1.lvh.me:8080", true},
		// Rule 4: unrecognized (parse fail), no pin -> fall back to clobber
		{"rule4-unrecognized", "foo-80", base, 0, "", false},
		// Rule 4 + pin: pinned target and Host
		{"rule4-pin", "foo-80", pinned, 5000, "app1.lvh.me:5000", true},
		// Pin wins over bare-name rule 3 (pinned-mode bare-hostname safety)
		{"pin-over-rule3", "app2", pinned, 5000, "app1.lvh.me:5000", true},
		// Explicit port wins over pin (user intent)
		{"port-over-pin", "app2-6000", pinned, 6000, "app2.lvh.me:6000", true},
		// "label equals reach's own first label" guard -> rule 4 (no pin -> false)
		{"reach-first-label-guard", "web", reachGuarded, 0, "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			port, host, ok := resolvePreviewVhost(tt.label, tt.sess)
			if ok != tt.wantOK {
				t.Fatalf("resolvePreviewVhost(%q) ok = %v, want %v", tt.label, ok, tt.wantOK)
			}
			if !tt.wantOK {
				return
			}
			if port != tt.wantPort || host != tt.wantHost {
				t.Errorf("resolvePreviewVhost(%q) = (%d, %q), want (%d, %q)", tt.label, port, host, tt.wantPort, tt.wantHost)
			}
		})
	}
}

func TestResolvePreviewVhostSuffixOverride(t *testing.T) {
	t.Setenv("SWE_PREVIEW_VHOST_SUFFIX", "internal.test")
	base := &Session{PreviewPort: 8080}
	port, host, ok := resolvePreviewVhost("app1-5000", base)
	if !ok || port != 5000 || host != "app1.internal.test:5000" {
		t.Errorf("with suffix override = (%d, %q, %v), want (5000, app1.internal.test:5000, true)", port, host, ok)
	}
}

func TestBuildStatusPayloadPreviewVhost(t *testing.T) {
	s := &Session{
		UUID:            "11111111-2222-3333-4444-555555555555",
		WorkDir:         "/workspace",
		AssistantConfig: AssistantConfig{Name: "claude"},
		SessionMode:     "terminal",
		PreviewPort:     23100,
	}

	t.Run("defaults", func(t *testing.T) {
		payload := s.buildStatusPayload(0, 24, 80)
		if got := payload["previewVhostSuffix"]; got != "lvh.me" {
			t.Errorf("previewVhostSuffix = %v, want lvh.me", got)
		}
		cands, ok := payload["previewReachCandidates"].([]string)
		if !ok || len(cands) != 1 || cands[0] != "lvh.me" {
			t.Errorf("previewReachCandidates = %v, want [lvh.me]", payload["previewReachCandidates"])
		}
		// JSON round-trip: fields must reach the wire.
		data, _ := json.Marshal(payload)
		var rt map[string]interface{}
		json.Unmarshal(data, &rt)
		if rt["previewVhostSuffix"] != "lvh.me" {
			t.Errorf("after round-trip previewVhostSuffix = %v", rt["previewVhostSuffix"])
		}
		arr, _ := rt["previewReachCandidates"].([]interface{})
		if len(arr) != 1 || arr[0] != "lvh.me" {
			t.Errorf("after round-trip previewReachCandidates = %v", rt["previewReachCandidates"])
		}
	})

	t.Run("reach-domain-override", func(t *testing.T) {
		t.Setenv("SWE_PREVIEW_REACH_DOMAIN", "preview.example.com")
		payload := s.buildStatusPayload(0, 24, 80)
		cands, _ := payload["previewReachCandidates"].([]string)
		if len(cands) != 1 || cands[0] != "preview.example.com" {
			t.Errorf("previewReachCandidates = %v, want [preview.example.com]", payload["previewReachCandidates"])
		}
	})

	t.Run("vhost-suffix-override", func(t *testing.T) {
		t.Setenv("SWE_PREVIEW_VHOST_SUFFIX", "internal.test")
		payload := s.buildStatusPayload(0, 24, 80)
		if got := payload["previewVhostSuffix"]; got != "internal.test" {
			t.Errorf("previewVhostSuffix = %v, want internal.test", got)
		}
	})
}
