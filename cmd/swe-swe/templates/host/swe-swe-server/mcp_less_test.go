package main

import "testing"

func specNames(specs []proxySpec) map[string]bool {
	m := map[string]bool{}
	for _, s := range specs {
		m[s.Name] = true
	}
	return m
}

// TestMcpLessProxySpecs pins the gating rule: swe-swe-server launches the whole
// fleet per session, but the agent-chat proxy only for chat sessions.
func TestMcpLessProxySpecs(t *testing.T) {
	t.Run("chat session includes agent-chat and the full fleet", func(t *testing.T) {
		specs := mcpLessProxySpecs("chat")
		if len(specs) != 5 {
			t.Fatalf("chat: want 5 specs, got %d (%v)", len(specs), specNames(specs))
		}
		for _, want := range []string{"swe-swe-agent-chat", "swe-swe-playwright", "swe-swe-preview", "swe-swe-whiteboard", "swe-swe"} {
			if !specNames(specs)[want] {
				t.Errorf("chat fleet missing %q", want)
			}
		}
	})

	t.Run("terminal session omits agent-chat", func(t *testing.T) {
		specs := mcpLessProxySpecs("terminal")
		if specNames(specs)["swe-swe-agent-chat"] {
			t.Error("terminal session must NOT launch the agent-chat proxy")
		}
		if len(specs) != 4 {
			t.Fatalf("terminal: want 4 specs, got %d (%v)", len(specs), specNames(specs))
		}
	})

	t.Run("empty/default mode is treated as non-chat", func(t *testing.T) {
		if specNames(mcpLessProxySpecs(""))["swe-swe-agent-chat"] {
			t.Error("default (terminal) mode must NOT launch the agent-chat proxy")
		}
	})

	t.Run("every spec is well-formed", func(t *testing.T) {
		for _, s := range mcpLessProxySpecs("chat") {
			if s.Name == "" || len(s.Argv) == 0 {
				t.Errorf("malformed spec: %+v", s)
			}
			if s.socketName() != s.Name+".sock" {
				t.Errorf("socket name for %q = %q", s.Name, s.socketName())
			}
		}
	})
}
