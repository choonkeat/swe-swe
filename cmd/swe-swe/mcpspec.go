package main

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

// The four MCP servers every swe-swe session exposes, described ONCE.
//
// Before this table there were six hand-maintained copies of the same four
// command lines -- one per agent config format in entrypoint.sh, plus
// dockerlessMCPServers() -- and they had already drifted (the dockerless
// preview entry lost its ?key= while the container kept it). Every emitter
// below derives from this table, so a change to a command reaches all of
// them or none.
//
// The $VAR references are deliberately NOT expanded here. They are session
// env vars (SWE_SERVER_PORT, SESSION_UUID, MCP_AUTH_KEY, BROWSER_CDP_PORT)
// that the server sets per session, so they must survive into the config
// file verbatim and expand when the agent spawns the server.
type mcpServer struct {
	Name string
	// Prog and Args are the direct invocation. Codex needs this form: it
	// sandboxes MCP children and forwards only the env vars named in
	// EnvVars, so the `sh -c "... $VAR ..."` wrapper the other agents use
	// would expand to empty inside its sandbox.
	Prog string
	Args []string
	// EnvVars is Codex's forward-whitelist. Every $VAR the command
	// references must appear here or it arrives empty.
	EnvVars []string
	// Note is carried into the generated Codex TOML as a comment, so the
	// reasoning behind a whitelist entry survives the generation step.
	Note string
}

func mcpServers() []mcpServer {
	const welcomeReplies = "What can you help me with?,Give me an overview of this project,What has changed recently?,/swe-swe:recordings-list-orphaned"
	return []mcpServer{
		{
			Name: "swe-swe-agent-chat",
			Prog: "swe-npx",
			Args: []string{
				"-y", "@choonkeat/agent-chat",
				"--theme-cookie", "swe-swe-theme",
				"--welcome-replies", welcomeReplies,
				"--autocomplete-triggers", "/=slash-command",
				"--autocomplete-url", "http://localhost:$SWE_SERVER_PORT/api/autocomplete/$SESSION_UUID?key=$MCP_AUTH_KEY",
			},
			EnvVars: []string{"AGENT_CHAT_PORT", "AGENT_CHAT_EVENT_LOG", "AGENT_CHAT_EXPORT_DIR", "SWE_SERVER_PORT", "SESSION_UUID", "MCP_AUTH_KEY"},
			Note:    "AGENT_CHAT_EVENT_LOG (chat history / recordings) and AGENT_CHAT_EXPORT_DIR\n(streaming chat-log export, which chatlog_close needs) are read by\nagent-chat itself, so they have to be on the whitelist or Codex sessions\nsilently lose both.",
		},
		{
			Name: "swe-swe-playwright",
			Prog: "mcp-lazy-init",
			Args: []string{
				"--init-method", "POST",
				"--init-url", "http://localhost:$SWE_SERVER_PORT/api/session/$SESSION_UUID/browser/start?key=$MCP_AUTH_KEY",
				"--", "npx", "-y", "@playwright/mcp@latest",
				"--cdp-endpoint", "http://localhost:$BROWSER_CDP_PORT",
			},
			EnvVars: []string{"SWE_SERVER_PORT", "SESSION_UUID", "MCP_AUTH_KEY", "BROWSER_CDP_PORT"},
		},
		{
			Name: "swe-swe-preview",
			Prog: "swe-npx",
			Args: []string{
				"-y", "@choonkeat/agent-reverse-proxy",
				"--bridge", "http://localhost:$SWE_SERVER_PORT/proxy/$SESSION_UUID/preview/mcp?key=$MCP_AUTH_KEY",
			},
			EnvVars: []string{"SWE_SERVER_PORT", "SESSION_UUID", "MCP_AUTH_KEY"},
		},
		{
			Name: "swe-swe",
			Prog: "swe-npx",
			Args: []string{
				"-y", "@choonkeat/agent-reverse-proxy",
				"--bridge", "http://localhost:$SWE_SERVER_PORT/mcp?key=$MCP_AUTH_KEY",
			},
			EnvVars: []string{"SWE_SERVER_PORT", "MCP_AUTH_KEY"},
		},
	}
}

// mcpRetiredServers are names earlier versions registered that must still be
// removed on re-init, so an upgraded config does not carry a dead server.
var mcpRetiredServers = []string{"swe-swe-whiteboard"}

// shSafeArg matches an argument that needs no quoting in a double-quoted
// `sh -c` script. $ is deliberately allowed through unquoted so session env
// vars still expand at agent-launch time.
var shSafeArg = regexp.MustCompile(`^[A-Za-z0-9_@./:=+?&$%,-]+$`)

// shArg renders one argument for embedding in a `sh -c` script. Arguments
// carrying spaces get double quotes (never single, which would freeze the
// $VAR references the servers rely on).
func shArg(s string) string {
	if s == "" {
		return `""`
	}
	if shSafeArg.MatchString(s) {
		return s
	}
	if strings.ContainsAny(s, "\"\\`") {
		// No current argument needs this, and quoting it correctly across
		// five config syntaxes is not worth guessing at.
		panic(fmt.Sprintf("mcpspec: argument needs escaping, which no emitter supports: %q", s))
	}
	return `"` + s + `"`
}

// Script is the `sh -c` form every agent except Codex uses.
func (m mcpServer) Script() string {
	parts := make([]string, 0, len(m.Args)+2)
	parts = append(parts, "exec", m.Prog)
	for _, a := range m.Args {
		parts = append(parts, shArg(a))
	}
	return strings.Join(parts, " ")
}

// ---------- Emitters: one per agent config syntax ----------
//
// Every one of these was previously a hand-maintained literal. They now all
// read the same table, which is the whole point: adding a server, or fixing
// a command, is a one-line change here instead of six edits in five
// syntaxes that nothing checks for agreement.

// mcpStdioSpec is the {command, args} shape Claude's .mcp.json and Gemini's
// settings.json both use.
type mcpStdioSpec struct {
	Command string   `json:"command"`
	Args    []string `json:"args"`
}

func mcpStdioSpecs() map[string]mcpStdioSpec {
	out := make(map[string]mcpStdioSpec, len(mcpServers()))
	for _, s := range mcpServers() {
		out[s.Name] = mcpStdioSpec{Command: "sh", Args: []string{"-c", s.Script()}}
	}
	return out
}

// mcpOpencodeSpec is OpenCode's shape: a type tag and one flat command array.
type mcpOpencodeSpec struct {
	Type    string   `json:"type"`
	Command []string `json:"command"`
}

func mcpOpencodeSpecs() map[string]mcpOpencodeSpec {
	out := make(map[string]mcpOpencodeSpec, len(mcpServers()))
	for _, s := range mcpServers() {
		out[s.Name] = mcpOpencodeSpec{Type: "local", Command: []string{"sh", "-c", s.Script()}}
	}
	return out
}

// mcpClaudeAddLines renders the `claude mcp add` registration block: the
// removes first (idempotent re-registration, plus retired names), then one
// add per server.
func mcpClaudeAddLines(indent string) string {
	var b strings.Builder
	names := make([]string, 0, len(mcpServers()))
	for _, s := range mcpServers() {
		names = append(names, s.Name)
	}
	for _, n := range names {
		fmt.Fprintf(&b, "%sclaude mcp remove --scope user %s 2>/dev/null || true\n", indent, n)
	}
	for _, n := range mcpRetiredServers {
		fmt.Fprintf(&b, "%s# %s was retired; keep removing it so an upgraded user\n", indent, n)
		fmt.Fprintf(&b, "%s# scope does not carry a dead registration forward.\n", indent)
		fmt.Fprintf(&b, "%sclaude mcp remove --scope user %s 2>/dev/null || true\n", indent, n)
	}
	for _, s := range mcpServers() {
		fmt.Fprintf(&b, "%sclaude mcp add --scope user --transport stdio %s -- sh -c '%s'\n", indent, s.Name, s.Script())
	}
	return b.String()
}

// mcpCodexTOML renders Codex's config.toml. Codex is the one agent that
// cannot take the `sh -c` wrapper, so it gets Prog/Args directly plus the
// env_vars whitelist.
func mcpCodexTOML() string {
	var b strings.Builder
	for i, s := range mcpServers() {
		if i > 0 {
			b.WriteString("\n")
		}
		fmt.Fprintf(&b, "[mcp_servers.%s]\n", s.Name)
		fmt.Fprintf(&b, "command = %q\n", s.Prog)
		fmt.Fprintf(&b, "args = [%s]\n", tomlStrings(s.Args))
		for _, line := range strings.Split(s.Note, "\n") {
			if s.Note != "" {
				fmt.Fprintf(&b, "# %s\n", line)
			}
		}
		fmt.Fprintf(&b, "env_vars = [%s]\n", tomlStrings(s.EnvVars))
	}
	return b.String()
}

// mcpCodexFlags renders the same registration as `-c key=value` overrides,
// for hosts where Codex has no project-scoped config file to write into and
// clobbering the user's own ~/.codex/config.toml is not acceptable.
func mcpCodexFlags() []string {
	var out []string
	for _, s := range mcpServers() {
		out = append(out,
			"-c", fmt.Sprintf("mcp_servers.%s.command=%q", s.Name, s.Prog),
			"-c", fmt.Sprintf("mcp_servers.%s.args=[%s]", s.Name, tomlStrings(s.Args)),
			"-c", fmt.Sprintf("mcp_servers.%s.env_vars=[%s]", s.Name, tomlStrings(s.EnvVars)),
		)
	}
	return out
}

func tomlStrings(ss []string) string {
	parts := make([]string, 0, len(ss))
	for _, s := range ss {
		parts = append(parts, fmt.Sprintf("%q", s))
	}
	return strings.Join(parts, ", ")
}

// mcpGooseYAML renders Goose's extensions block.
func mcpGooseYAML() string {
	var b strings.Builder
	b.WriteString("extensions:\n")
	for _, s := range mcpServers() {
		fmt.Fprintf(&b, "  %s:\n", s.Name)
		b.WriteString("    type: stdio\n")
		b.WriteString("    cmd: sh\n")
		b.WriteString("    args:\n")
		b.WriteString("      - \"-c\"\n")
		fmt.Fprintf(&b, "      - %s\n", yamlQuote(s.Script()))
	}
	return b.String()
}

// yamlQuote double-quotes a scalar, escaping the inner double quotes YAML
// would otherwise end the scalar on.
func yamlQuote(s string) string {
	return `"` + strings.ReplaceAll(s, `"`, `\"`) + `"`
}

// mcpStdioJSON renders the {"mcpServers": ...} document Claude's .mcp.json
// and Gemini's settings.json share.
func mcpStdioJSON() string {
	return mustMarshalIndent(struct {
		MCPServers map[string]mcpStdioSpec `json:"mcpServers"`
	}{MCPServers: mcpStdioSpecs()})
}

// mcpOpencodeJSON renders OpenCode's {"mcp": ...} document.
func mcpOpencodeJSON() string {
	return mustMarshalIndent(struct {
		MCP map[string]mcpOpencodeSpec `json:"mcp"`
	}{MCP: mcpOpencodeSpecs()})
}

func mustMarshalIndent(v any) string {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		// Only fixed, literal data reaches this; a failure is a programming
		// error, not a runtime condition.
		panic(fmt.Sprintf("mcpspec: marshal: %v", err))
	}
	return string(data)
}
