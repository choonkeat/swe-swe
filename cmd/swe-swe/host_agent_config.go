package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Host runtime agent config lives in <sweDir>/agent-config/, never in a
// project. The server hands each file to its agent at launch (see
// templates/host/swe-swe-server/host_agent_config.go, whose file names must
// match these):
//
//	claude-mcp.json             claude --mcp-config
//	claude-settings.json        claude --settings  (hook guards)
//	gemini-system-defaults.json GEMINI_CLI_SYSTEM_DEFAULTS_PATH
//	opencode.json               OPENCODE_CONFIG
//	pi-mcp-bridge.ts            pi -e
//
// Codex keeps its launch wrapper in bin/ (it had no project file either).
//
// Writing project files (.mcp.json, ...) covered only the folder `swe-swe
// init` ran in: new projects and clones had no swe-swe tools, and a repo's
// own committed .mcp.json showed swe-swe's entries as a change. Launch-time
// config covers every workspace the same way and touches none of them.
const (
	hostClaudeMCPFile      = "claude-mcp.json"
	hostClaudeSettingsFile = "claude-settings.json"
	hostGeminiDefaultsFile = "gemini-system-defaults.json"
	hostOpencodeFile       = "opencode.json"
	hostPiBridgeFile       = "pi-mcp-bridge.ts"
)

func hostAgentConfigDir(sweDir string) string {
	return filepath.Join(sweDir, "agent-config")
}

// writeHostAgentConfig rebuilds <sweDir>/agent-config from scratch for the
// selected agents, so a change of agents or of --without-mcp leaves nothing
// stale behind. `swe-swe init` and every `swe-swe up` call it. Returns the
// paths written. MCP-less mode writes no MCP config (the server runs the
// proxy fleet and the agent uses the `mcp` CLI) but still writes the hook
// guards.
func writeHostAgentConfig(sweDir string, agents []string, withoutMCP bool) ([]string, error) {
	dir := hostAgentConfigDir(sweDir)
	if err := os.RemoveAll(dir); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	binDir := filepath.Join(sweDir, "bin")
	var written []string
	write := func(name string, doc any) error {
		data, err := json.MarshalIndent(doc, "", "  ")
		if err != nil {
			return err
		}
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, append(data, '\n'), 0o644); err != nil {
			return err
		}
		written = append(written, p)
		return nil
	}

	for _, agent := range agents {
		var err error
		switch agent {
		case "claude":
			if !withoutMCP {
				err = write(hostClaudeMCPFile, map[string]any{"mcpServers": mcpStdioSpecs()})
			}
		case "gemini":
			if !withoutMCP {
				err = write(hostGeminiDefaultsFile, map[string]any{"mcpServers": mcpStdioSpecs()})
			}
		case "opencode":
			if !withoutMCP {
				err = write(hostOpencodeFile, map[string]any{"mcp": mcpOpencodeSpecs()})
			}
		case "pi":
			if !withoutMCP {
				var src []byte
				src, err = assets.ReadFile("templates/host/mcp-bridge.ts")
				if err == nil {
					p := filepath.Join(dir, hostPiBridgeFile)
					if err = os.WriteFile(p, src, 0o644); err == nil {
						written = append(written, p)
					}
				}
			}
		case "codex":
			if withoutMCP {
				if rmErr := os.Remove(filepath.Join(binDir, "codex")); rmErr != nil && !os.IsNotExist(rmErr) {
					err = rmErr
				}
			} else {
				var p string
				if p, err = writeCodexLaunchWrapper(binDir); err == nil && p != "" {
					written = append(written, p)
				}
			}
		}
		if err != nil {
			return written, fmt.Errorf("%s: %w", agent, err)
		}
	}

	// Claude hook guards (AskUserQuestion, Artifact, silent-stop). Written
	// whatever the agent selection: the guards self-exempt sessions with no
	// agent-chat channel, and a stray file costs nothing.
	settings := filepath.Join(dir, hostClaudeSettingsFile)
	if err := writeDockerlessHooksTo(settings, sweDir); err != nil {
		return written, fmt.Errorf("claude hook guards: %w", err)
	}
	written = append(written, settings)
	return written, nil
}

// removeLegacyProjectAgentConfig takes out what earlier versions of `swe-swe
// init --runtime=host` wrote into the project folder: swe-swe's servers in
// .mcp.json / .gemini/settings.json / opencode.json, the Pi bridge, and
// swe-swe's hook guards + enabledMcpjsonServers names in
// .claude/settings.local.json. The user's own entries stay; a file left
// holding nothing is deleted. Returns the paths changed.
func removeLegacyProjectAgentConfig(projectDir string) ([]string, error) {
	// Codex is not listed: its wrapper is in bin/ and is still current.
	removed, err := removeDockerlessAgentMCPConfigs(projectDir, "", []string{"claude", "gemini", "opencode", "pi"})
	if err != nil {
		return removed, err
	}
	settings := filepath.Join(projectDir, ".claude", "settings.local.json")
	changed, err := removeSweSweClaudeSettings(settings)
	if err != nil {
		return removed, err
	}
	if changed {
		removed = append(removed, settings)
	}
	return removed, nil
}

// isSweSweHookEntry reports whether a Claude hook entry runs one of swe-swe's
// guard scripts.
func isSweSweHookEntry(entry any) bool {
	b, _ := json.Marshal(entry)
	s := string(b)
	return strings.Contains(s, "swe-swe-stop-guard") ||
		strings.Contains(s, "swe-swe-ask-guard") ||
		strings.Contains(s, "swe-swe-artifact-guard")
}

func isSweSweServerName(name string) bool {
	for _, n := range append(mcpServerNames(), mcpRetiredServers...) {
		if n == name {
			return true
		}
	}
	return false
}

// removeSweSweClaudeSettings drops swe-swe's hook guards and swe-swe server
// names in enabledMcpjsonServers from a Claude settings file, keeping
// everything else. A file left empty is deleted; one that is not valid JSON
// is left alone. Reports whether anything changed.
func removeSweSweClaudeSettings(path string) (bool, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	var doc map[string]any
	if json.Unmarshal(data, &doc) != nil {
		return false, nil
	}
	changed := false
	if hooks, ok := doc["hooks"].(map[string]any); ok {
		for event, list := range hooks {
			entries, _ := list.([]any)
			var kept []any
			for _, e := range entries {
				if isSweSweHookEntry(e) {
					changed = true
					continue
				}
				kept = append(kept, e)
			}
			if len(kept) == 0 {
				delete(hooks, event)
			} else {
				hooks[event] = kept
			}
		}
		if len(hooks) == 0 {
			delete(doc, "hooks")
		}
	}
	if names, ok := doc["enabledMcpjsonServers"].([]any); ok {
		var kept []any
		for _, n := range names {
			if s, ok := n.(string); ok && isSweSweServerName(s) {
				changed = true
				continue
			}
			kept = append(kept, n)
		}
		if len(kept) == 0 {
			delete(doc, "enabledMcpjsonServers")
		} else {
			doc["enabledMcpjsonServers"] = kept
		}
	}
	if !changed {
		return false, nil
	}
	if len(doc) == 0 {
		return true, os.Remove(path)
	}
	out, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return false, err
	}
	return true, os.WriteFile(path, append(out, '\n'), 0o644)
}

// setUpHostAgentConfig is what init and up both run: retire the legacy
// project files, then rebuild <sweDir>/agent-config. Failures are returned
// for the caller to report; the paths changed are printed.
func setUpHostAgentConfig(sweDir, projectDir string, agents []string, withoutMCP bool) error {
	removed, err := removeLegacyProjectAgentConfig(projectDir)
	for _, p := range removed {
		fmt.Printf("Removed swe-swe entries from %s (agent config now passed at launch)\n", p)
	}
	if err != nil {
		return fmt.Errorf("remove legacy project agent config: %w", err)
	}
	if _, err := writeHostAgentConfig(sweDir, agents, withoutMCP); err != nil {
		return fmt.Errorf("write agent config: %w", err)
	}
	return nil
}
