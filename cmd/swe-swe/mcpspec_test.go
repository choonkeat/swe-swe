package main

import (
	"encoding/json"
	"strings"
	"testing"
)

// The expected strings below are copied verbatim from what the container
// entrypoint wrote before the spec table existed. They are the contract:
// if the table stops reproducing them, every agent's config changed at once
// and that had better be deliberate.
func TestMCPServerScriptsMatchContainerForm(t *testing.T) {
	want := map[string]string{
		"swe-swe-agent-chat": `exec swe-npx -y @choonkeat/agent-chat --theme-cookie swe-swe-theme --welcome-replies "What can you help me with?,Give me an overview of this project,What has changed recently?,Discuss: which worktrees & branches can we clean up?,/swe-swe:recordings-list-orphaned" --autocomplete-triggers /=slash-command --autocomplete-url http://localhost:$SWE_SERVER_PORT/api/autocomplete/$SESSION_UUID?key=$MCP_AUTH_KEY`,
		"swe-swe-playwright": `exec mcp-lazy-init --init-method POST --init-url http://localhost:$SWE_SERVER_PORT/api/session/$SESSION_UUID/browser/start?key=$MCP_AUTH_KEY -- npx -y @playwright/mcp@latest --cdp-endpoint http://localhost:$BROWSER_CDP_PORT`,
		"swe-swe-preview":    `exec swe-npx -y @choonkeat/agent-reverse-proxy --bridge http://localhost:$SWE_SERVER_PORT/proxy/$SESSION_UUID/preview/mcp?key=$MCP_AUTH_KEY`,
		"swe-swe":            `exec swe-npx -y @choonkeat/agent-reverse-proxy --bridge http://localhost:$SWE_SERVER_PORT/mcp?key=$MCP_AUTH_KEY`,
	}
	servers := mcpServers()
	if len(servers) != len(want) {
		t.Fatalf("server count: got %d, want %d", len(servers), len(want))
	}
	for _, s := range servers {
		exp, ok := want[s.Name]
		if !ok {
			t.Errorf("unexpected server %q", s.Name)
			continue
		}
		if got := s.Script(); got != exp {
			t.Errorf("%s script:\n got %s\nwant %s", s.Name, got, exp)
		}
	}
}

// Codex forwards only the env vars a server whitelists, so every $VAR the
// command references must be declared or it arrives empty inside the sandbox.
func TestMCPServerEnvVarsCoverReferencedVars(t *testing.T) {
	for _, s := range mcpServers() {
		declared := map[string]bool{}
		for _, v := range s.EnvVars {
			declared[v] = true
		}
		for _, a := range s.Args {
			for _, ref := range referencedEnvVars(a) {
				if !declared[ref] {
					t.Errorf("%s references $%s but does not declare it in EnvVars", s.Name, ref)
				}
			}
		}
	}
}

func referencedEnvVars(s string) []string {
	var out []string
	for i := 0; i < len(s); i++ {
		if s[i] != '$' {
			continue
		}
		j := i + 1
		for j < len(s) && (s[j] == '_' || (s[j] >= 'A' && s[j] <= 'Z') || (s[j] >= 'a' && s[j] <= 'z') || (s[j] >= '0' && s[j] <= '9')) {
			j++
		}
		if j > i+1 {
			out = append(out, s[i+1:j])
		}
		i = j - 1
	}
	return out
}

// Codex gives up on a tool call after its default timeout (300s seen in
// 0.157.1), so a send_message waiting for the user's reply fails and Codex
// ends its turn. Only agent-chat blocks on a human; the others keep the default.
func TestMCPCodexAgentChatWaitsForTheUser(t *testing.T) {
	const want = "tool_timeout_sec = 86400"
	for _, block := range strings.Split(mcpCodexTOML(), "\n\n") {
		has := strings.Contains(block, want)
		isChat := strings.HasPrefix(block, "[mcp_servers.swe-swe-agent-chat]")
		if has != isChat {
			t.Errorf("%q present=%v in block:\n%s", want, has, block)
		}
	}

	flags := strings.Join(mcpCodexFlags(), " ")
	if !strings.Contains(flags, "-c mcp_servers.swe-swe-agent-chat.tool_timeout_sec=86400") {
		t.Errorf("flags missing agent-chat tool_timeout_sec: %s", flags)
	}
	if n := strings.Count(flags, "tool_timeout_sec"); n != 1 {
		t.Errorf("tool_timeout_sec set on %d servers, want 1: %s", n, flags)
	}
}

// OpenCode's MCP client gives up on a tool call after 60s unless the server
// sets its own timeout (in ms), so a send_message waiting for the user fails
// and OpenCode repeats itself. Only agent-chat gets the long wait.
func TestMCPOpencodeAgentChatWaitsForTheUser(t *testing.T) {
	for name, spec := range mcpOpencodeSpecs() {
		want := 0
		if name == "swe-swe-agent-chat" {
			want = 86400 * 1000
		}
		if spec.Timeout != want {
			t.Errorf("%s timeout = %d, want %d", name, spec.Timeout, want)
		}
	}
}

// OpenCode's "Access external directory" prompt only shows in its TUI, so
// reading one of swe-swe's slash-command files from a chat session hung.
func TestMCPOpencodeAllowsSlashCommandDirs(t *testing.T) {
	var doc struct {
		Permission struct {
			ExternalDirectory map[string]string `json:"external_directory"`
		} `json:"permission"`
	}
	if err := json.Unmarshal([]byte(mcpOpencodeJSON()), &doc); err != nil {
		t.Fatal(err)
	}
	for _, dir := range []string{"/home/app/.config/opencode/command/*", "/home/app/.swe-swe/commands/*"} {
		if got := doc.Permission.ExternalDirectory[dir]; got != "allow" {
			t.Errorf("external_directory[%q] = %q, want allow", dir, got)
		}
	}
}
