package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Host-side init must give every selected agent the config form it actually
// reads. Before this, only Claude got one and the rest silently had no tools.
func TestWriteDockerlessAgentMCPConfigs_PerAgentPaths(t *testing.T) {
	cases := []struct {
		agent string
		want  string
	}{
		{"claude", ".mcp.json"},
		{"gemini", filepath.Join(".gemini", "settings.json")},
		{"opencode", "opencode.json"},
		{"pi", filepath.Join(".pi", "extensions", "mcp-bridge.ts")},
	}
	for _, tc := range cases {
		t.Run(tc.agent, func(t *testing.T) {
			dir := t.TempDir()
			written, err := writeDockerlessAgentMCPConfigs(dir, filepath.Join(dir, "bin"), []string{tc.agent})
			if err != nil {
				t.Fatalf("write: %v", err)
			}
			path := filepath.Join(dir, tc.want)
			if len(written) != 1 || written[0] != path {
				t.Fatalf("written = %v, want [%s]", written, path)
			}
			if _, err := os.Stat(path); err != nil {
				t.Fatalf("stat %s: %v", path, err)
			}
		})
	}
	// Codex has no project-scoped config, so it gets a launch wrapper on the
	// session PATH instead of a file in the user's home.
	t.Run("codex", func(t *testing.T) {
		dir := t.TempDir()
		binDir := filepath.Join(dir, "bin")
		written, err := writeDockerlessAgentMCPConfigs(dir, binDir, []string{"codex"})
		if err != nil {
			t.Fatalf("write: %v", err)
		}
		wrapper := filepath.Join(binDir, "codex")
		if len(written) != 1 || written[0] != wrapper {
			t.Fatalf("written = %v, want [%s]", written, wrapper)
		}
		info, err := os.Stat(wrapper)
		if err != nil {
			t.Fatalf("stat: %v", err)
		}
		if info.Mode()&0111 == 0 {
			t.Errorf("codex wrapper not executable: %v", info.Mode())
		}
		body, _ := os.ReadFile(wrapper)
		for _, name := range mcpServerNames() {
			if !strings.Contains(string(body), "mcp_servers."+name+".command") {
				t.Errorf("wrapper missing %s registration:\n%s", name, body)
			}
		}
		// The $VARs must reach Codex unexpanded: it substitutes them itself
		// from the env_vars whitelist inside its sandbox.
		if !strings.Contains(string(body), "$SWE_SERVER_PORT") {
			t.Errorf("wrapper expanded the session env vars:\n%s", body)
		}
		// The wrapper sits on the same PATH as the name it shadows, so it
		// must skip its own directory when resolving the real codex.
		// Without this it execs itself forever.
		if !strings.Contains(string(body), shellSingleQuote(binDir)) {
			t.Errorf("wrapper does not know its own dir, so PATH resolution can loop:\n%s", body)
		}
		if strings.Contains(string(body), `exec "codex"`) || strings.Contains(string(body), "exec codex") {
			t.Errorf("wrapper execs codex by bare name, which re-finds itself:\n%s", body)
		}
	})
}

// Writing must never clobber a config the user already keeps in the project.
func TestWriteDockerlessAgentMCPConfigs_PreservesUserEntries(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "opencode.json")
	prior := `{"model":"anthropic/claude","mcp":{"mine":{"type":"local","command":["my-mcp"]}}}`
	if err := os.WriteFile(path, []byte(prior), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := writeDockerlessAgentMCPConfigs(dir, filepath.Join(dir, "bin"), []string{"opencode"}); err != nil {
		t.Fatalf("write: %v", err)
	}
	var doc map[string]any
	b, _ := os.ReadFile(path)
	if err := json.Unmarshal(b, &doc); err != nil {
		t.Fatalf("unparsable: %v\n%s", err, b)
	}
	if doc["model"] != "anthropic/claude" {
		t.Errorf("unrelated key dropped: %s", b)
	}
	mcp, _ := doc["mcp"].(map[string]any)
	if _, ok := mcp["mine"]; !ok {
		t.Errorf("user's server dropped: %s", b)
	}
	if _, ok := mcp["swe-swe-agent-chat"]; !ok {
		t.Errorf("swe-swe server not added: %s", b)
	}

	// And an MCP-less re-init takes only ours back out.
	if _, err := removeDockerlessAgentMCPConfigs(dir, filepath.Join(dir, "bin"), []string{"opencode"}); err != nil {
		t.Fatalf("remove: %v", err)
	}
	b, _ = os.ReadFile(path)
	if err := json.Unmarshal(b, &doc); err != nil {
		t.Fatalf("unparsable after remove: %v\n%s", err, b)
	}
	mcp, _ = doc["mcp"].(map[string]any)
	if _, ok := mcp["mine"]; !ok {
		t.Errorf("user's server removed too: %s", b)
	}
	if _, ok := mcp["swe-swe-agent-chat"]; ok {
		t.Errorf("swe-swe server survived: %s", b)
	}
	if doc["model"] != "anthropic/claude" {
		t.Errorf("unrelated key dropped on remove: %s", b)
	}
}

// Goose is the one selectable agent with no host-side route; init must say so
// rather than leave the user to discover it when the chat never answers.
func TestAgentsWithoutHostMCPConfig(t *testing.T) {
	got := agentsWithoutHostMCPConfig([]string{"claude", "goose", "codex", "opencode", "gemini", "pi", "aider"})
	if len(got) != 1 || got[0] != "goose" {
		t.Errorf("got %v, want [goose]", got)
	}
	if len(agentsWithoutHostMCPConfig([]string{"claude"})) != 0 {
		t.Error("claude should not be reported as unsupported")
	}
}
