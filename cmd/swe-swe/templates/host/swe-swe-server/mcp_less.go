package main

// MCP-less mode: instead of the agent's native MCP client spawning each server
// over stdio, swe-swe-server launches one long-lived `mcp-cli-proxy` per server
// per session, and the agent reaches them through the `mcp` CLI over unix
// sockets. This lets swe-swe run where MCP is gated but the CLI agent is not.
// See tasks/2026-07-03-mcp-less-default-server-launched-fleet.md.

// proxySpec is one mcp-cli-proxy launch: the server name (which also names its
// socket) and the argv passed after `--`. The argv is the canonical
// `sh -c 'exec ...'` form so the shell expands $SWE_SERVER_PORT/$SESSION_UUID/
// $MCP_AUTH_KEY/$BROWSER_CDP_PORT from the inherited session env.
type proxySpec struct {
	Name string
	Argv []string
}

func (p proxySpec) socketName() string { return p.Name + ".sock" }

// shExec wraps a command string in the `sh -c 'exec ...'` form used for every
// proxy child, so a single shell expands env vars and then `exec` replaces it
// (mcp-cli-proxy waits on and logs the real child, per the no-silent-Wait rule).
func shExec(cmd string) []string { return []string{"sh", "-c", "exec " + cmd} }

// mcpLessProxySpecs returns the proxy fleet swe-swe-server launches for a
// session. The agent-chat proxy is included ONLY for chat sessions (mirroring
// "only expose agentChatPort for chat sessions"); every other proxy runs for
// every session.
func mcpLessProxySpecs(sessionMode string) []proxySpec {
	specs := []proxySpec{}
	// agent-chat: the human<->agent channel. Chat sessions only -- terminal
	// sessions use the TUI directly and never bind AGENT_CHAT_PORT.
	if sessionMode == "chat" {
		specs = append(specs, proxySpec{
			Name: "swe-swe-agent-chat",
			Argv: shExec("npx -y @choonkeat/agent-chat --theme-cookie swe-swe-theme --autocomplete-triggers /=slash-command --autocomplete-url http://localhost:$SWE_SERVER_PORT/api/autocomplete/$SESSION_UUID?key=$MCP_AUTH_KEY"),
		})
	}
	specs = append(specs,
		proxySpec{
			Name: "swe-swe-playwright",
			Argv: shExec("mcp-lazy-init --init-method POST --init-url http://localhost:$SWE_SERVER_PORT/api/session/$SESSION_UUID/browser/start?key=$MCP_AUTH_KEY -- npx -y @playwright/mcp@latest --cdp-endpoint http://localhost:$BROWSER_CDP_PORT"),
		},
		proxySpec{
			Name: "swe-swe-preview",
			Argv: shExec("npx -y @choonkeat/agent-reverse-proxy --bridge http://localhost:$SWE_SERVER_PORT/proxy/$SESSION_UUID/preview/mcp"),
		},
		proxySpec{
			Name: "swe-swe-whiteboard",
			Argv: shExec("npx -y @choonkeat/agent-whiteboard"),
		},
		proxySpec{
			Name: "swe-swe",
			Argv: shExec("npx -y @choonkeat/agent-reverse-proxy --bridge http://localhost:$SWE_SERVER_PORT/mcp?key=$MCP_AUTH_KEY"),
		},
	)
	return specs
}
