package main

import (
	"reflect"
	"testing"
)

func TestBuildAgentArgv(t *testing.T) {
	tests := []struct {
		name      string
		shellCmd  string
		extraArgs string
		wantName  string
		wantArgs  []string
	}{
		{
			name:      "no extra args",
			shellCmd:  "claude",
			extraArgs: "",
			wantName:  "claude",
			wantArgs:  []string{},
		},
		{
			name:      "single flag with value",
			shellCmd:  "claude",
			extraArgs: "--channels server:agent-chat",
			wantName:  "claude",
			wantArgs:  []string{"--channels", "server:agent-chat"},
		},
		{
			name:      "preserves shell cmd args before extra",
			shellCmd:  "bash -l",
			extraArgs: "--foo",
			wantName:  "bash",
			wantArgs:  []string{"-l", "--foo"},
		},
		{
			name:      "whitespace-only extra is ignored",
			shellCmd:  "claude",
			extraArgs: "   ",
			wantName:  "claude",
			wantArgs:  []string{},
		},
		{
			name:      "multiple extra flags",
			shellCmd:  "claude",
			extraArgs: "--a 1 --b 2",
			wantName:  "claude",
			wantArgs:  []string{"--a", "1", "--b", "2"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			gotName, gotArgs := buildAgentArgv(tc.shellCmd, tc.extraArgs)
			if gotName != tc.wantName {
				t.Errorf("name: got %q, want %q", gotName, tc.wantName)
			}
			if !reflect.DeepEqual(gotArgs, tc.wantArgs) {
				t.Errorf("args: got %#v, want %#v", gotArgs, tc.wantArgs)
			}
		})
	}
}

func TestSessionPageQueryEncodeExtraArgs(t *testing.T) {
	q := SessionPageQuery{
		Assistant: "claude",
		ExtraArgs: "--channels server:agent-chat",
	}
	encoded := string(q.Encode())
	// Order is determined by url.Values.Encode (alphabetical), so just check
	// that both required pairs are present.
	want1 := "assistant=claude"
	want2 := "extra_args=--channels+server%3Aagent-chat"
	if !contains(encoded, want1) {
		t.Errorf("encoded %q missing %q", encoded, want1)
	}
	if !contains(encoded, want2) {
		t.Errorf("encoded %q missing %q", encoded, want2)
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
