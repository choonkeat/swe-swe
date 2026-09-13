package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Host-side MCP config, one writer per agent.
//
// The container writes every agent's config into a throwaway /home/app, so
// it can clobber freely. On a host those same paths are the user's own
// settings, so nothing here touches a home directory: each agent gets
// either a project-scoped file or (Codex, which has no project-scoped
// config) a launch wrapper on the session PATH.
//
// Goose is deliberately absent: its only config is ~/.config/goose/
// config.yaml, and there is no project-scoped form to write instead.
// agentsWithoutHostMCPConfig reports it so the user is told rather than
// left guessing.

// hostMCPConfigAgents are the agents with a host-side config route.
var hostMCPConfigAgents = map[string]bool{
	"claude":   true,
	"opencode": true,
	"gemini":   true,
	"pi":       true,
	"codex":    true,
	// aider has no MCP support at all, in either runtime, so it is not a gap.
	"aider": true,
}

// agentsWithoutHostMCPConfig lists selected agents swe-swe cannot configure
// host-side, so init can say so instead of leaving a silent gap.
func agentsWithoutHostMCPConfig(agents []string) []string {
	var out []string
	for _, a := range agents {
		if !hostMCPConfigAgents[a] {
			out = append(out, a)
		}
	}
	sort.Strings(out)
	return out
}

// writeDockerlessAgentMCPConfigs writes each selected agent's MCP config and
// returns the paths written, for the caller to report.
func writeDockerlessAgentMCPConfigs(projectDir, binDir string, agents []string) ([]string, error) {
	var written []string
	for _, agent := range agents {
		var path string
		var err error
		switch agent {
		case "claude":
			path, err = writeMCPJSONMerging(filepath.Join(projectDir, ".mcp.json"), "mcpServers", anyMap(mcpStdioSpecs()))
		case "gemini":
			path, err = writeMCPJSONMerging(filepath.Join(projectDir, ".gemini", "settings.json"), "mcpServers", anyMap(mcpStdioSpecs()))
		case "opencode":
			path, err = writeMCPJSONMerging(filepath.Join(projectDir, "opencode.json"), "mcp", anyMap(mcpOpencodeSpecs()))
		case "codex":
			path, err = writeCodexLaunchWrapper(binDir)
		case "pi":
			path, err = writePiMCPBridge(projectDir)
		default:
			continue
		}
		if err != nil {
			return written, fmt.Errorf("%s: %w", agent, err)
		}
		if path != "" {
			written = append(written, path)
		}
	}
	return written, nil
}

// removeDockerlessAgentMCPConfigs retires whatever a previous native-MCP init
// wrote, so an MCP-less re-init leaves no stale server config behind.
func removeDockerlessAgentMCPConfigs(projectDir, binDir string, agents []string) ([]string, error) {
	var removed []string
	drop := func(path, key string) error {
		gone, err := removeMCPEntries(path, key)
		if err != nil {
			return err
		}
		if gone {
			removed = append(removed, path)
		}
		return nil
	}
	for _, agent := range agents {
		var err error
		switch agent {
		case "claude":
			err = drop(filepath.Join(projectDir, ".mcp.json"), "mcpServers")
		case "gemini":
			err = drop(filepath.Join(projectDir, ".gemini", "settings.json"), "mcpServers")
		case "opencode":
			err = drop(filepath.Join(projectDir, "opencode.json"), "mcp")
		case "codex":
			path := filepath.Join(binDir, "codex")
			if rmErr := os.Remove(path); rmErr == nil {
				removed = append(removed, path)
			} else if !os.IsNotExist(rmErr) {
				err = rmErr
			}
		case "pi":
			path := filepath.Join(projectDir, ".pi", "extensions", "mcp-bridge.ts")
			if rmErr := os.Remove(path); rmErr == nil {
				removed = append(removed, path)
			} else if !os.IsNotExist(rmErr) {
				err = rmErr
			}
		}
		if err != nil {
			return removed, fmt.Errorf("%s: %w", agent, err)
		}
	}
	return removed, nil
}

func anyMap[V any](m map[string]V) map[string]any {
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

// writeMCPJSONMerging sets our servers under key in a JSON document,
// preserving every other key and every server the user added themselves.
// A file we cannot parse is left alone rather than overwritten.
func writeMCPJSONMerging(path, key string, servers map[string]any) (string, error) {
	doc := map[string]any{}
	data, err := os.ReadFile(path)
	switch {
	case err == nil:
		if json.Unmarshal(data, &doc) != nil {
			return "", fmt.Errorf("%s is not valid JSON; leaving it alone", path)
		}
	case !os.IsNotExist(err):
		return "", err
	}
	existing, _ := doc[key].(map[string]any)
	if existing == nil {
		existing = map[string]any{}
	}
	for _, name := range mcpRetiredServers {
		delete(existing, name)
	}
	for name, spec := range servers {
		existing[name] = spec
	}
	doc[key] = existing
	out, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return "", err
	}
	if err := os.WriteFile(path, append(out, '\n'), 0644); err != nil {
		return "", err
	}
	return path, nil
}

// removeMCPEntries drops only our servers from a JSON config. A file that
// also carries the user's own servers is rewritten without ours; one holding
// nothing else is deleted. Reports whether anything changed.
func removeMCPEntries(path, key string) (bool, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	var doc map[string]any
	if json.Unmarshal(data, &doc) != nil {
		// Not ours to judge; leave a hand-written or malformed file alone.
		return false, nil
	}
	servers, _ := doc[key].(map[string]any)
	changed := false
	names := append(mcpServerNames(), mcpRetiredServers...)
	for _, name := range names {
		if _, ok := servers[name]; ok {
			delete(servers, name)
			changed = true
		}
	}
	if !changed {
		return false, nil
	}
	if len(servers) == 0 {
		delete(doc, key)
	} else {
		doc[key] = servers
	}
	if len(doc) == 0 {
		return true, os.Remove(path)
	}
	out, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return false, err
	}
	return true, os.WriteFile(path, append(out, '\n'), 0644)
}

func mcpServerNames() []string {
	out := make([]string, 0, len(mcpServers()))
	for _, s := range mcpServers() {
		out = append(out, s.Name)
	}
	return out
}

// writeCodexLaunchWrapper puts a `codex` shim on the session PATH that adds
// the MCP registrations as -c overrides. Codex reads only
// ~/.codex/config.toml, which on a host belongs to the user, so swe-swe
// passes its servers on the command line instead of writing that file.
func writeCodexLaunchWrapper(binDir string) (string, error) {
	var b strings.Builder
	b.WriteString("#!/bin/sh\n")
	b.WriteString("# Generated by swe-swe. Codex has no project-scoped config, so swe-swe's\n")
	b.WriteString("# MCP servers are passed as -c overrides rather than written into the\n")
	b.WriteString("# user's ~/.codex/config.toml. Their own config still applies underneath.\n")
	b.WriteString("\n")
	b.WriteString("# Find the real codex by skipping this wrapper's own directory. Resolving\n")
	b.WriteString("# through PATH alone would find this script again and loop forever.\n")
	fmt.Fprintf(&b, "swe_swe_bin=%s\n", shellSingleQuote(binDir))
	b.WriteString("real_codex=\n")
	b.WriteString("IFS=:\n")
	b.WriteString("for dir in $PATH; do\n")
	b.WriteString("  [ \"$dir\" = \"$swe_swe_bin\" ] && continue\n")
	b.WriteString("  if [ -x \"$dir/codex\" ]; then real_codex=\"$dir/codex\"; break; fi\n")
	b.WriteString("done\n")
	b.WriteString("unset IFS\n")
	b.WriteString("if [ -z \"$real_codex\" ]; then\n")
	b.WriteString("  echo \"swe-swe: codex is not installed on PATH\" >&2\n")
	b.WriteString("  exit 127\n")
	b.WriteString("fi\n")
	b.WriteString("\n")
	b.WriteString("exec \"$real_codex\" \\\n")
	flags := mcpCodexFlags()
	for i := 0; i < len(flags); i += 2 {
		fmt.Fprintf(&b, "  %s %s \\\n", flags[i], shellSingleQuote(flags[i+1]))
	}
	b.WriteString("  \"$@\"\n")
	path := filepath.Join(binDir, "codex")
	if err := os.MkdirAll(binDir, 0755); err != nil {
		return "", err
	}
	if err := os.WriteFile(path, []byte(b.String()), 0755); err != nil {
		return "", err
	}
	return path, nil
}

// shellSingleQuote wraps s for /bin/sh. The -c payloads carry $VAR
// references that Codex itself substitutes from its env_vars whitelist, so
// the shell must NOT expand them on the way through.
func shellSingleQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// writePiMCPBridge installs the Pi extension project-locally. Pi prefers a
// project .pi/extensions/ override, so this reaches sessions in this project
// without touching the user's ~/.pi.
func writePiMCPBridge(projectDir string) (string, error) {
	src, err := assets.ReadFile("templates/host/mcp-bridge.ts")
	if err != nil {
		return "", err
	}
	dir := filepath.Join(projectDir, ".pi", "extensions")
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", err
	}
	path := filepath.Join(dir, "mcp-bridge.ts")
	if err := os.WriteFile(path, src, 0644); err != nil {
		return "", err
	}
	return path, nil
}
