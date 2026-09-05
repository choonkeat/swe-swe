package main

import (
	"bytes"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// mcpCliProxyBin is the proxy daemon binary; a package var so tests can point it
// at a stub.
var mcpCliProxyBin = "mcp-cli-proxy"

// mcpLessSocketRoot is the base dir under which each session gets its own
// <root>/<uuid>/ socket dir. The agent's `mcp` client is pointed here via
// SWE_MCP_DIR, so concurrent sessions never collide on socket names.
//
// It is deliberately SHORT and per-user under the temp dir, not under the
// workspace or the .swe-swe home: unix socket paths are capped at 108 bytes
// (sun_path), and a dockerless project's metadata dir already runs to
// ~/.swe-swe/projects/<path-derived-name>/, which with the uuid and
// "swe-swe-agent-chat.sock" blew past the cap -- every proxy then died on
// bind with "invalid argument". Sockets are ephemeral, so tmp is the right
// home anyway; the uid keeps users on a shared box apart.
func mcpLessSocketRoot() string {
	return filepath.Join(os.TempDir(), fmt.Sprintf("swe-swe-%d", os.Getuid()), "mcp")
}

// unixSocketPathMax is the sun_path limit (108 on Linux, 104 on macOS); the
// longest per-session socket path must stay under it.
const unixSocketPathMax = 104

// mcpLessEnabled reports whether this server runs in MCP-less mode. Native
// MCP is the default; `swe-swe up` exports SWE_MCP_LESS=1 only when the
// project was created with `swe-swe init --runtime=host --without-mcp`.
func mcpLessEnabled() bool { return os.Getenv("SWE_MCP_LESS") != "" }

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
	// BlockingTools names this server's tools whose call does not return until
	// a human acts. Passed to mcp-cli-proxy as --blocking-tools so it emits an
	// immediate "still waiting" notification the `mcp` client relays to stderr
	// -- an early read of a blocking call's output is then never mistaken for
	// "no reply". Empty for servers with no such tools.
	BlockingTools []string
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
			Argv: shExec("swe-npx -y @choonkeat/agent-chat --theme-cookie swe-swe-theme --welcome-replies \"What can you help me with?,Give me an overview of this project,What has changed recently?,/swe-swe:recordings-list-orphaned\" --autocomplete-triggers /=slash-command --autocomplete-url http://localhost:$SWE_SERVER_PORT/api/autocomplete/$SESSION_UUID?key=$MCP_AUTH_KEY"),
			// send_message / send_verbal_reply block until the user replies.
			BlockingTools: []string{"send_message", "send_verbal_reply"},
		})
	}
	specs = append(specs,
		proxySpec{
			Name: "swe-swe-playwright",
			Argv: shExec("mcp-lazy-init --init-method POST --init-url http://localhost:$SWE_SERVER_PORT/api/session/$SESSION_UUID/browser/start?key=$MCP_AUTH_KEY -- npx -y @playwright/mcp@latest --cdp-endpoint http://localhost:$BROWSER_CDP_PORT"),
		},
		proxySpec{
			Name: "swe-swe-preview",
			Argv: shExec("swe-npx -y @choonkeat/agent-reverse-proxy --bridge http://localhost:$SWE_SERVER_PORT/proxy/$SESSION_UUID/preview/mcp?key=$MCP_AUTH_KEY"),
		},
		proxySpec{
			Name: "swe-swe",
			Argv: shExec("swe-npx -y @choonkeat/agent-reverse-proxy --bridge http://localhost:$SWE_SERVER_PORT/mcp?key=$MCP_AUTH_KEY"),
		},
	)
	return specs
}

// launchMcpLessFleet starts one mcp-cli-proxy per spec for a session, dropping
// each server's socket into socketDir (created if missing) and giving every
// child the session env so the `sh -c exec` shells expand the per-session vars.
// Every proxy runs with the session's workDir as cwd -- in native-MCP mode the
// agent spawns these servers from its own cwd, and cwd-dependent tools
// (agent-chat filepath autocomplete, export_chat_md's ./agent-chats) rely on
// that. It returns the started commands for teardown. Best-effort: a proxy that
// fails to start is logged and skipped (agent-chat health is surfaced
// separately), so one bad server never blocks the rest.
func launchMcpLessFleet(sessionMode, socketDir string, env []string, workDir string) ([]*exec.Cmd, error) {
	if err := os.MkdirAll(socketDir, 0o755); err != nil {
		return nil, fmt.Errorf("mcp-less socket dir %s: %w", socketDir, err)
	}
	var cmds []*exec.Cmd
	for _, spec := range mcpLessProxySpecs(sessionMode) {
		sock := filepath.Join(socketDir, spec.socketName())
		args := []string{"--name", spec.Name, "--socket", sock}
		if len(spec.BlockingTools) > 0 {
			args = append(args, "--blocking-tools", strings.Join(spec.BlockingTools, ","))
		}
		args = append(args, "--")
		args = append(args, spec.Argv...)
		cmd := exec.Command(mcpCliProxyBin, args...)
		cmd.Env = env
		cmd.Dir = workDir
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Start(); err != nil {
			log.Printf("mcp-less: failed to start proxy %s: %v", spec.Name, err)
			continue
		}
		pid := cmd.Process.Pid
		log.Printf("mcp-less: started proxy %s (pid %d) socket %s", spec.Name, pid, sock)
		trackPid(pid)
		cmds = append(cmds, cmd)
		// No silent Wait (coding rule): reap and log name+pid+exit. The proxy
		// self-restarts its own child; we do not restart the proxy process here
		// (container restart is the backstop) -- but we always record its exit.
		go func(name string, c *exec.Cmd, pid int) {
			err := c.Wait()
			log.Printf("mcp-less: proxy %s (pid %d) exited: %v", name, pid, err)
			untrackPid(pid)
		}(spec.Name, cmd, pid)
	}
	return cmds, nil
}

// stopMcpLessFleet kills every proxy in the fleet. The per-proxy reaper
// goroutine started in launchMcpLessFleet logs each exit and untracks its pid.
func stopMcpLessFleet(cmds []*exec.Cmd) {
	for _, c := range cmds {
		if c != nil && c.Process != nil {
			c.Process.Kill()
		}
	}
}

// MCP-less agent steering. With no native MCP client the agent has no tool
// docs in context and no send_message tool: it must reach every tool through
// the `mcp` CLI, and the blocking send_message contract is the load-bearing
// rule. The container entrypoint used to write this into ~/.claude/CLAUDE.md;
// a dockerless host must not touch the user's global ~/.claude, so the server
// maintains it per session workDir in CLAUDE.local.md -- Claude Code's
// machine-local project memory, which is auto-loaded and by convention never
// committed. Fenced by markers so re-runs replace (never duplicate) the block
// and a switch back to native MCP removes it. Claude only for now: AGENTS.md
// has no local variant, and the other agents' steering was never wired.
const (
	mcpLessSteeringBegin = "<!-- swe-swe:mcp-less begin -->"
	mcpLessSteeringEnd   = "<!-- swe-swe:mcp-less end -->"
	mcpLessSteeringBody  = `# MCP-less mode

This environment has NO MCP client. Reach every tool through the ` + "`mcp`" + ` CLI.
Run ` + "`mcp -h`" + ` FIRST -- and again after any context compaction. It prints the
full documentation for every server and tool (what a native MCP client would
inject into your context automatically). Never guess flags.

Talk to the user through agent-chat -- it is the ONLY channel the user sees:

- Start each turn with ` + "`mcp swe-swe-agent-chat check_messages`" + `.
- EVERY user-visible message MUST go through send_message, following its
  documentation from ` + "`mcp -h`" + ` exactly.
- ` + "`send_message`" + ` BLOCKS until the user replies; the reply is RETURNED as the
  command's stdout. Never background it; end every turn on it.
- Non-blocking status: ` + "`mcp swe-swe-agent-chat send_progress --text \"...\"`" + `.

Once the task at hand is clear (and when it changes), name this session so the
user can tell sessions apart: see ` + "`mcp swe-swe set_session_name -h`" + `.
`
)

// mcpLessSteeringFile is the machine-local memory file the steering lives in,
// relative to the session workDir. Only claude reads it.
func mcpLessSteeringFile(assistant string) string {
	if assistant == "claude" {
		return "CLAUDE.local.md"
	}
	return ""
}

// stripMcpLessSteering returns content with the fenced steering block (and
// its trailing newline) removed, and whether a block was present.
func stripMcpLessSteering(content []byte) ([]byte, bool) {
	start := bytes.Index(content, []byte(mcpLessSteeringBegin))
	if start < 0 {
		return content, false
	}
	end := bytes.Index(content[start:], []byte(mcpLessSteeringEnd))
	if end < 0 {
		return content, false
	}
	end += start + len(mcpLessSteeringEnd)
	if end < len(content) && content[end] == '\n' {
		end++
	}
	return append(append([]byte{}, content[:start]...), content[end:]...), true
}

// syncMcpLessSteering upserts the steering block into the workDir's
// CLAUDE.local.md when MCP-less mode is on, and removes it (deleting the file
// if nothing else is in it) when off. Idempotent; a no-op for an empty workDir
// or an assistant with no local memory file.
func syncMcpLessSteering(workDir, assistant string, enabled bool) error {
	name := mcpLessSteeringFile(assistant)
	if workDir == "" || name == "" {
		return nil
	}
	path := filepath.Join(workDir, name)
	existing, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("read %s: %w", path, err)
	}
	rest, had := stripMcpLessSteering(existing)
	if !enabled {
		if !had {
			return nil
		}
		if len(bytes.TrimSpace(rest)) == 0 {
			return os.Remove(path)
		}
		return os.WriteFile(path, rest, 0644)
	}
	var b bytes.Buffer
	b.Write(rest)
	if b.Len() > 0 && !bytes.HasSuffix(b.Bytes(), []byte("\n")) {
		b.WriteByte('\n')
	}
	b.WriteString(mcpLessSteeringBegin + "\n" + mcpLessSteeringBody + mcpLessSteeringEnd + "\n")
	if had && bytes.Equal(b.Bytes(), existing) {
		return nil
	}
	return os.WriteFile(path, b.Bytes(), 0644)
}
