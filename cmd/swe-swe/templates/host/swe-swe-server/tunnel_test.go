package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// TestResolvePublicHostname covers the precedence rules for the tunnel-mode
// public hostname:
//
//	flag > env > tunnel state file > "" (legacy/off)
//
// Plus the disable knobs: empty stateFile path skips the file step;
// missing/malformed/empty-hostname files all fall through to "".
func TestResolvePublicHostname(t *testing.T) {
	// Helper: write a state file under tmp dir, return its path.
	writeState := func(t *testing.T, body string) string {
		t.Helper()
		dir := t.TempDir()
		p := filepath.Join(dir, "tunnel-state.json")
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatalf("write state file: %v", err)
		}
		return p
	}
	missingPath := func(t *testing.T) string {
		t.Helper()
		return filepath.Join(t.TempDir(), "tunnel-state.json")
	}
	good := `{"hostname":"s.example.com","unique":"s","registered_at":"2026-04-28T00:00:00Z"}`

	cases := []struct {
		name      string
		flag      string
		env       string
		stateFile func(t *testing.T) string
		want      string
	}{
		{
			name:      "all empty + no state file path",
			stateFile: func(*testing.T) string { return "" },
			want:      "",
		},
		{
			name: "flag wins over env+state",
			flag: "f.example.com",
			env:  "e.example.com",
			stateFile: func(t *testing.T) string {
				return writeState(t, good)
			},
			want: "f.example.com",
		},
		{
			name: "env wins over state",
			env:  "e.example.com",
			stateFile: func(t *testing.T) string {
				return writeState(t, good)
			},
			want: "e.example.com",
		},
		{
			name: "state file used when flag+env empty",
			stateFile: func(t *testing.T) string {
				return writeState(t, good)
			},
			want: "s.example.com",
		},
		{
			name:      "empty stateFile path skips file fallback",
			stateFile: func(*testing.T) string { return "" },
			want:      "",
		},
		{
			name: "malformed JSON falls through (no crash)",
			stateFile: func(t *testing.T) string {
				return writeState(t, "not-json")
			},
			want: "",
		},
		{
			name: "empty hostname field falls through",
			stateFile: func(t *testing.T) string {
				return writeState(t, `{"hostname":""}`)
			},
			want: "",
		},
		{
			name: "missing file at given path -> silent fall-through",
			stateFile: func(t *testing.T) string {
				return missingPath(t)
			},
			want: "",
		},
		{
			name:      "flag wins even without state file",
			flag:      "flag.example.com",
			stateFile: func(*testing.T) string { return "" },
			want:      "flag.example.com",
		},
		{
			name: "forwards-compat: extra unknown fields in JSON still parse",
			stateFile: func(t *testing.T) string {
				return writeState(t, `{"hostname":"x.example.com","future_field":42,"another":true}`)
			},
			want: "x.example.com",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sf := tc.stateFile(t)
			got := resolvePublicHostname(tc.flag, tc.env, sf)
			if got != tc.want {
				t.Errorf("resolvePublicHostname(%q, %q, %q) = %q, want %q",
					tc.flag, tc.env, sf, got, tc.want)
			}
		})
	}
}

// TestReadTunnelStateHostname covers the standalone parser: happy path,
// missing file (ENOENT), malformed JSON, empty hostname, and forwards-
// compatibility with extra unknown fields.
func TestReadTunnelStateHostname(t *testing.T) {
	dir := t.TempDir()

	t.Run("happy path", func(t *testing.T) {
		p := filepath.Join(dir, "good.json")
		if err := os.WriteFile(p, []byte(`{"hostname":"abc-tunnel.example.com","unique":"abc","registered_at":"2026-04-28T00:00:00Z"}`), 0o600); err != nil {
			t.Fatal(err)
		}
		got, err := readTunnelStateHostname(p)
		if err != nil {
			t.Fatalf("unexpected err: %v", err)
		}
		if got != "abc-tunnel.example.com" {
			t.Errorf("hostname: got %q, want %q", got, "abc-tunnel.example.com")
		}
	})

	t.Run("missing file -> os.IsNotExist", func(t *testing.T) {
		_, err := readTunnelStateHostname(filepath.Join(dir, "nope.json"))
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if !os.IsNotExist(err) {
			t.Errorf("expected IsNotExist err, got %v", err)
		}
	})

	t.Run("malformed JSON", func(t *testing.T) {
		p := filepath.Join(dir, "bad.json")
		if err := os.WriteFile(p, []byte(`{not json}`), 0o600); err != nil {
			t.Fatal(err)
		}
		_, err := readTunnelStateHostname(p)
		if err == nil {
			t.Fatal("expected parse error, got nil")
		}
		if os.IsNotExist(err) {
			t.Errorf("malformed should NOT look like ENOENT to caller; got %v", err)
		}
	})

	t.Run("empty hostname field", func(t *testing.T) {
		p := filepath.Join(dir, "empty.json")
		if err := os.WriteFile(p, []byte(`{"hostname":""}`), 0o600); err != nil {
			t.Fatal(err)
		}
		_, err := readTunnelStateHostname(p)
		if err == nil {
			t.Fatal("expected error for empty hostname, got nil")
		}
	})

	t.Run("extra unknown fields are ignored", func(t *testing.T) {
		p := filepath.Join(dir, "future.json")
		if err := os.WriteFile(p, []byte(`{"hostname":"x.example.com","unique":"x","registered_at":"2030-01-01T00:00:00Z","new_field":"future"}`), 0o600); err != nil {
			t.Fatal(err)
		}
		got, err := readTunnelStateHostname(p)
		if err != nil {
			t.Fatalf("forwards-compat: unexpected err %v", err)
		}
		if got != "x.example.com" {
			t.Errorf("hostname: got %q, want %q", got, "x.example.com")
		}
	})
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
