package main

import (
	"os"
	"path/filepath"
)

// hostRuntime is true when this server runs host-native (`swe-swe init
// --runtime=host`, started by `swe-swe up`), which exports SWE_RUNTIME=host.
//
// It changes how agents find swe-swe's MCP servers and hook guards. In the
// container they live in the container user's ~/.claude.json and
// ~/.claude/settings.json, so they cover every project. On a host those are
// the user's real files, which swe-swe never touches, and writing project
// files (.mcp.json, ...) only covered the one folder `swe-swe init` ran in:
// new projects and clones got no swe-swe tools, and a repo's own committed
// .mcp.json showed swe-swe's entries as a change.
//
// Instead, `swe-swe up` writes the config into <swe home>/agent-config/ on
// every boot (see writeHostAgentConfig in cmd/swe-swe), and the server hands
// it to each agent at launch: as flags (hostAgentArgs) or environment
// (hostAgentEnv). Nothing is written into any project, so every workspace
// (init workspace, new project, clone, worktree) is covered the same way,
// and re-running init with different choices takes effect everywhere.
var hostRuntime = os.Getenv("SWE_RUNTIME") == "host"

// Files `swe-swe up` writes into hostAgentConfigDir. Keep in sync with
// writeHostAgentConfig in cmd/swe-swe/host_agent_config.go. A missing file
// means "not wanted" (agent not selected, or MCP-less mode).
const (
	hostClaudeMCPFile      = "claude-mcp.json"
	hostClaudeSettingsFile = "claude-settings.json"
	hostGeminiDefaultsFile = "gemini-system-defaults.json"
	hostOpencodeFile       = "opencode.json"
	hostPiBridgeFile       = "pi-mcp-bridge.ts"
)

func hostAgentConfigDir() string {
	return filepath.Join(sweHomeDir, "agent-config")
}

// hostAgentConfigFile returns the path of name in hostAgentConfigDir, or ""
// when not on a host runtime or the file does not exist.
func hostAgentConfigFile(name string) string {
	if !hostRuntime {
		return ""
	}
	p := filepath.Join(hostAgentConfigDir(), name)
	if _, err := os.Stat(p); err != nil {
		return ""
	}
	return p
}

// hostAgentArgs adds the launch flags that load swe-swe's config into agents
// that take it on the command line. The flags go right after the binary, so
// the assistant's own flags and any user extras follow unchanged:
//
//	claude --mcp-config <f> --settings <f> --continue
//	pi -e <f> --continue
//
// --mcp-config is variadic; --settings (always written: the hook guards)
// follows it, so it never swallows a later positional argument. Servers from
// --mcp-config need no per-project approval, and --settings hooks add to the
// project's own hooks rather than replacing them.
func hostAgentArgs(cmdName string, cmdArgs []string) []string {
	var pre []string
	switch filepath.Base(cmdName) {
	case "claude":
		mcp := hostAgentConfigFile(hostClaudeMCPFile)
		settings := hostAgentConfigFile(hostClaudeSettingsFile)
		if mcp != "" && settings != "" {
			pre = append(pre, "--mcp-config", mcp)
		} else if mcp != "" {
			// Without a following option the variadic flag could swallow a
			// positional arg; the = form takes exactly one value.
			pre = append(pre, "--mcp-config="+mcp)
		}
		if settings != "" {
			pre = append(pre, "--settings", settings)
		}
	case "pi":
		if bridge := hostAgentConfigFile(hostPiBridgeFile); bridge != "" {
			pre = append(pre, "-e", bridge)
		}
	}
	if len(pre) == 0 {
		return cmdArgs
	}
	return append(pre, cmdArgs...)
}

// hostAgentEnv returns the environment that points agents configured by env
// var at swe-swe's config. Both are merged layers, not replacements:
// OPENCODE_CONFIG is merged with the user's and the project's opencode.json;
// Gemini's system-defaults file is its lowest-precedence layer, and its
// mcpServers are merged with the user's and project's.
//
// A variable the user already set is left alone: it names their own file,
// and replacing it would drop their config to add ours.
func hostAgentEnv() []string {
	var env []string
	for _, v := range []struct{ name, file string }{
		{"OPENCODE_CONFIG", hostOpencodeFile},
		{"GEMINI_CLI_SYSTEM_DEFAULTS_PATH", hostGeminiDefaultsFile},
	} {
		if os.Getenv(v.name) != "" {
			continue
		}
		if f := hostAgentConfigFile(v.file); f != "" {
			env = append(env, v.name+"="+f)
		}
	}
	return env
}
