// Package forkconvo forks coding-agent conversations at an arbitrary message.
//
// Three agents are supported: Claude Code, OpenAI Codex, and pi
// (badlogic/pi-mono). Each agent stores its session as a JSONL file with a
// per-agent format. Forking produces a new JSONL with a fresh session UUID
// whose contents are the source up to (and including) the chosen anchor
// event. The source remains untouched.
//
// The package exposes a single Fork entry point used by both the
// swe-swe-fork-convo CLI and the swe-swe-server /api/fork handler. The
// per-agent details live in claude.go, codex.go, pi.go.
package forkconvo

import (
	"fmt"
	"strings"
)

// Agent identifies the coding agent whose session format we operate on.
type Agent string

const (
	AgentClaude Agent = "claude"
	AgentCodex  Agent = "codex"
	AgentPi     Agent = "pi"
)

// ParseAgent normalises a CLI-style agent name.
func ParseAgent(s string) (Agent, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "claude":
		return AgentClaude, nil
	case "codex":
		return AgentCodex, nil
	case "pi":
		return AgentPi, nil
	}
	return "", fmt.Errorf("unknown agent %q (expected claude|codex|pi)", s)
}

// AnchorLastChatReply is the literal anchor value that means "the most recent
// reply the user gave through the agent-chat MCP tool". Resolved per-agent.
const AnchorLastChatReply = "last-chat-reply"

// DefaultChatTool is the agent-chat MCP tool whose replies we anchor on by
// default. send_message is the only tool that solicits a reply, but exposing
// the override lets callers anchor on send_progress etc. for experimentation.
const DefaultChatTool = "send_message"

// Opts describes a single fork operation.
type Opts struct {
	Agent           Agent  // which CLI's session format
	SourceSessionID string // session id of the source conversation
	Anchor          string // message-uuid/call-id/entry-id, or AnchorLastChatReply
	Tool            string // agent-chat tool when Anchor==AnchorLastChatReply
}

// Result is what Fork returns on success.
type Result struct {
	NewSessionID  string // freshly minted session id for the fork
	NewSourcePath string // absolute path to the forked .jsonl
	AnchorUUID    string // resolved anchor identifier (for diagnostics)
}

// Fork forks an agent session per opts. The source file is left untouched.
func Fork(opts Opts) (*Result, error) {
	if opts.SourceSessionID == "" {
		return nil, fmt.Errorf("forkconvo: SourceSessionID is required")
	}
	if opts.Anchor == "" {
		return nil, fmt.Errorf("forkconvo: Anchor is required")
	}
	if opts.Tool == "" {
		opts.Tool = DefaultChatTool
	}
	switch opts.Agent {
	case AgentClaude:
		return forkClaude(opts)
	case AgentCodex:
		return forkCodex(opts)
	case AgentPi:
		return forkPi(opts)
	}
	return nil, fmt.Errorf("forkconvo: unknown agent %q", opts.Agent)
}
