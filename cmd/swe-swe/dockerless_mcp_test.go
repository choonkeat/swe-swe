package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Host-side init/up must give every selected agent the config form it can be
// handed at launch, in <sweDir>/agent-config -- never in the project.
func TestWriteHostAgentConfig_PerAgentPaths(t *testing.T) {
	cases := []struct {
		agent string
		want  string
	}{
		{"claude", hostClaudeMCPFile},
		{"gemini", hostGeminiDefaultsFile},
		{"opencode", hostOpencodeFile},
		{"pi", hostPiBridgeFile},
	}
	for _, tc := range cases {
		t.Run(tc.agent, func(t *testing.T) {
			sweDir := t.TempDir()
			written, err := writeHostAgentConfig(sweDir, []string{tc.agent}, false)
			if err != nil {
				t.Fatalf("write: %v", err)
			}
			path := filepath.Join(hostAgentConfigDir(sweDir), tc.want)
			settings := filepath.Join(hostAgentConfigDir(sweDir), hostClaudeSettingsFile)
			if len(written) != 2 || written[0] != path || written[1] != settings {
				t.Fatalf("written = %v, want [%s %s]", written, path, settings)
			}
			if _, err := os.Stat(path); err != nil {
				t.Fatalf("stat %s: %v", path, err)
			}
		})
	}
	// Codex has no per-launch config file, so it gets a launch wrapper on the
	// session PATH instead of a file in the user's home.
	t.Run("codex", func(t *testing.T) {
		sweDir := t.TempDir()
		binDir := filepath.Join(sweDir, "bin")
		written, err := writeHostAgentConfig(sweDir, []string{"codex"}, false)
		if err != nil {
			t.Fatalf("write: %v", err)
		}
		wrapper := filepath.Join(binDir, "codex")
		if len(written) != 2 || written[0] != wrapper {
			t.Fatalf("written = %v, want [%s, claude settings]", written, wrapper)
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

// The directory is rebuilt from scratch, so switching to MCP-less (or
// dropping an agent) leaves no stale MCP config behind -- gap 1 of the
// "new project has no swe-swe tools" fix. Hook guards stay.
func TestWriteHostAgentConfig_RebuildDropsStale(t *testing.T) {
	sweDir := t.TempDir()
	all := []string{"claude", "gemini", "opencode", "pi", "codex"}
	if _, err := writeHostAgentConfig(sweDir, all, false); err != nil {
		t.Fatal(err)
	}
	if _, err := writeHostAgentConfig(sweDir, all, true); err != nil {
		t.Fatal(err)
	}
	entries, _ := os.ReadDir(hostAgentConfigDir(sweDir))
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	if len(names) != 1 || names[0] != hostClaudeSettingsFile {
		t.Errorf("after MCP-less rebuild, agent-config = %v, want only %s", names, hostClaudeSettingsFile)
	}
	if _, err := os.Stat(filepath.Join(sweDir, "bin", "codex")); !os.IsNotExist(err) {
		t.Errorf("codex wrapper survived MCP-less rebuild (stat err %v)", err)
	}
	// Hook guards wired to the scripts in sweDir/hooks.
	b, _ := os.ReadFile(filepath.Join(hostAgentConfigDir(sweDir), hostClaudeSettingsFile))
	for _, want := range []string{"swe-swe-stop-guard.sh", "swe-swe-ask-guard.sh", "swe-swe-artifact-guard.sh"} {
		if !strings.Contains(string(b), filepath.Join(sweDir, "hooks", want)) {
			t.Errorf("claude settings missing %s:\n%s", want, b)
		}
	}
}

// Older host inits wrote swe-swe's config into the project. Upgrading takes
// only swe-swe's entries back out; the user's own stay (gap 2: a repo's
// committed .mcp.json must not carry swe-swe's entries).
func TestRemoveLegacyProjectAgentConfig(t *testing.T) {
	dir := t.TempDir()
	write := func(rel, body string) {
		t.Helper()
		p := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(".mcp.json", mcpStdioJSON())
	write("opencode.json", `{"model":"anthropic/claude","mcp":{"mine":{"type":"local","command":["my-mcp"]},"swe-swe-agent-chat":{"type":"local","command":["x"]}}}`)
	write(filepath.Join(".pi", "extensions", "mcp-bridge.ts"), "// bridge")
	write(filepath.Join(".claude", "settings.local.json"), `{
  "permissions": {"allow": ["Bash(make:*)"]},
  "enabledMcpjsonServers": ["swe-swe", "swe-swe-agent-chat", "mine"],
  "hooks": {
    "PreToolUse": [
      {"matcher": "AskUserQuestion", "hooks": [{"type": "command", "command": "/h/hooks/swe-swe-ask-guard.sh"}]},
      {"matcher": "Bash", "hooks": [{"type": "command", "command": "my-hook"}]}
    ],
    "Stop": [{"hooks": [{"type": "command", "command": "/h/hooks/swe-swe-stop-guard.sh"}]}]
  }
}`)

	removed, err := removeLegacyProjectAgentConfig(dir)
	if err != nil {
		t.Fatalf("remove: %v", err)
	}
	if len(removed) != 4 {
		t.Errorf("removed = %v, want 4 paths", removed)
	}
	if _, err := os.Stat(filepath.Join(dir, ".mcp.json")); !os.IsNotExist(err) {
		t.Errorf("swe-swe-only .mcp.json should be deleted (stat err %v)", err)
	}
	if _, err := os.Stat(filepath.Join(dir, ".pi", "extensions", "mcp-bridge.ts")); !os.IsNotExist(err) {
		t.Errorf("pi bridge should be removed (stat err %v)", err)
	}

	var oc map[string]any
	b, _ := os.ReadFile(filepath.Join(dir, "opencode.json"))
	if err := json.Unmarshal(b, &oc); err != nil {
		t.Fatalf("opencode.json unparsable: %v\n%s", err, b)
	}
	mcp, _ := oc["mcp"].(map[string]any)
	if _, ok := mcp["mine"]; !ok || oc["model"] != "anthropic/claude" {
		t.Errorf("user's opencode config lost: %s", b)
	}
	if _, ok := mcp["swe-swe-agent-chat"]; ok {
		t.Errorf("swe-swe server survived: %s", b)
	}

	var st map[string]any
	b, _ = os.ReadFile(filepath.Join(dir, ".claude", "settings.local.json"))
	if err := json.Unmarshal(b, &st); err != nil {
		t.Fatalf("settings unparsable: %v\n%s", err, b)
	}
	if _, ok := st["permissions"]; !ok {
		t.Errorf("user's permissions lost: %s", b)
	}
	if en, _ := st["enabledMcpjsonServers"].([]any); len(en) != 1 || en[0] != "mine" {
		t.Errorf("enabledMcpjsonServers = %v, want [mine]", st["enabledMcpjsonServers"])
	}
	hooks, _ := st["hooks"].(map[string]any)
	if _, ok := hooks["Stop"]; ok {
		t.Errorf("swe-swe Stop guard survived: %s", b)
	}
	if pre, _ := hooks["PreToolUse"].([]any); len(pre) != 1 {
		t.Errorf("PreToolUse = %v, want only the user's Bash hook", pre)
	}

	// Running again changes nothing.
	again, err := removeLegacyProjectAgentConfig(dir)
	if err != nil || len(again) != 0 {
		t.Errorf("second run: removed=%v err=%v, want none", again, err)
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
