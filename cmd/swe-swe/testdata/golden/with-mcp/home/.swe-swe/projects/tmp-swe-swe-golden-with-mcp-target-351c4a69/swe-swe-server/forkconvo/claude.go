package forkconvo

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/uuid"
)

// chatMCPToolPrefixClaude is the fully-qualified tool name prefix used in
// Claude's .jsonl when the agent invokes one of the agent-chat MCP tools.
const chatMCPToolPrefixClaude = "mcp__swe-swe-agent-chat__"

// chatCLIServerClaude is the agent-chat server name as invoked through the
// `mcp` CLI in MCP-less sessions: `mcp swe-swe-agent-chat send_message ...`
// runs inside a Bash tool_use, so those sessions contain ZERO native
// mcp__swe-swe-agent-chat__* tool_use entries.
const chatCLIServerClaude = "swe-swe-agent-chat"

// claudeBashAgentChatTool reports whether a Bash tool_use command invokes an
// agent-chat tool through the `mcp` CLI, and which tool. Matching is
// field-based: a bare `mcp` token immediately followed by the server name.
// Quoted mentions (grep patterns, --text payloads) keep their surrounding
// quote characters attached to the field, so they don't match.
func claudeBashAgentChatTool(command string) (tool string, ok bool) {
	fields := strings.Fields(command)
	for i := 0; i+1 < len(fields); i++ {
		if fields[i] != "mcp" || fields[i+1] != chatCLIServerClaude {
			continue
		}
		if i+2 < len(fields) && !strings.HasPrefix(fields[i+2], "-") {
			return fields[i+2], true
		}
		return "", true
	}
	return "", false
}

// claudeContentBashCommand extracts input.command from a Bash tool_use.
// Parsed lazily (Input stays a json.RawMessage) so a non-object or oddly
// shaped input on OTHER tools can never poison the whole line's unmarshal.
func claudeContentBashCommand(c claudeContent) string {
	if c.Name != "Bash" || len(c.Input) == 0 {
		return ""
	}
	var in struct {
		Command string `json:"command"`
	}
	if err := json.Unmarshal(c.Input, &in); err != nil {
		return ""
	}
	return in.Command
}

// claudeProjectsRoot is where Claude Code persists session jsonl files. The
// directory under projectsRoot is the cwd path with slashes replaced by
// dashes.
func claudeProjectsRoot() string {
	if v := os.Getenv("CLAUDE_HOME"); v != "" {
		return filepath.Join(v, "projects")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".claude", "projects")
}

// findClaudeSession locates the source .jsonl for the given session id by
// scanning every project subdirectory under claudeProjectsRoot.
func findClaudeSession(sessionID string) (string, error) {
	root := claudeProjectsRoot()
	entries, err := os.ReadDir(root)
	if err != nil {
		return "", fmt.Errorf("read claude projects: %w", err)
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		candidate := filepath.Join(root, e.Name(), sessionID+".jsonl")
		if _, err := os.Stat(candidate); err == nil {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("claude session %s not found under %s", sessionID, root)
}

func forkClaude(opts Opts) (*Result, error) {
	src, err := findClaudeSession(opts.SourceSessionID)
	if err != nil {
		return nil, err
	}

	// Resolve opts.Anchor to a claude event uuid (the value claudeCopyUntil
	// matches against ev.uuid). Two paths:
	//
	//   - AnchorLastChatReply: scan for the most recent agent-chat tool_use
	//     and return ITS ASSISTANT EVENT'S uuid (not the following user
	//     tool_result's uuid). Cutting at the assistant event leaves the
	//     fork tail in a WAITING state -- agent has delivered its reply,
	//     no unactioned user directive baked in -- which avoids the
	//     "resumed agent autonomously executes the trailing user
	//     instruction" runaway. Verified empirically: claude --resume
	//     accepts a tail whose tool_use has no matching tool_result.
	//
	//   - Otherwise opts.Anchor is a tool_use_id (e.g. from bubble-anchored
	//     forks via fork_resolve.go); translate it to the enclosing
	//     assistant event's uuid before copying.
	anchorUUID := opts.Anchor
	if opts.Anchor == AnchorLastChatReply {
		anchorUUID, err = claudeFindLastChatReply(src, opts.Tool)
		if err != nil {
			return nil, fmt.Errorf("resolve %s: %w", AnchorLastChatReply, err)
		}
	} else {
		anchorUUID, err = claudeFindAssistantEventByToolUseID(src, opts.Anchor)
		if err != nil {
			return nil, fmt.Errorf("translate tool_use_id %s to event uuid: %w", opts.Anchor, err)
		}
	}

	newID := uuid.NewString()
	dst := filepath.Join(filepath.Dir(src), newID+".jsonl")

	if err := claudeCopyUntil(src, dst, opts.SourceSessionID, newID, anchorUUID); err != nil {
		_ = os.Remove(dst)
		return nil, err
	}
	return &Result{
		NewSessionID:  newID,
		NewSourcePath: dst,
		AnchorUUID:    anchorUUID,
	}, nil
}

// claudeFindLastChatReply scans the .jsonl for the most recent assistant
// event containing a tool_use whose name matches the requested agent-chat
// MCP tool, and returns THAT ASSISTANT EVENT'S uuid.
//
// Why the assistant event and not the following user tool_result:
// cutting at the user tool_result leaves the fork tail in a
// PENDING-ACTION shape -- the user's next directive ("User responded:
// ...") is baked in but the agent hasn't acted on it, so the resumed
// agent wakes up and autonomously executes whatever the original next
// directive happened to be. Cutting at the assistant event leaves a
// WAITING tail (agent delivered its reply, awaiting user) with no
// pending directive; the fork's first new user message becomes the
// sole, fresh instruction. Verified empirically: claude --resume
// accepts a tail whose tool_use has no matching tool_result.
//
// If the requested toolName has no matching tool_use anywhere in the
// session, we fall back to ANY agent-chat MCP tool (e.g. check_messages).
// This handles the "channels" runtime where Claude's text response is
// streamed directly to agent-chat without an explicit send_message
// tool_use entry in the .jsonl -- the most recent check_messages call
// is still a valid fork point because it represents the state right
// before Claude responds.
//
// MCP-less sessions reach agent-chat through the `mcp` CLI inside Bash
// tool_uses, so both the primary and fallback scans also match Bash
// commands invoking `mcp swe-swe-agent-chat <tool>` -- otherwise such
// sessions have no anchor at all and every fork fails.
func claudeFindLastChatReply(path, toolName string) (string, error) {
	fq := chatMCPToolPrefixClaude + toolName
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	scanner := newBigScanner(f)
	var lastPrimary, lastFallback string
	for scanner.Scan() {
		raw := scanner.Bytes()
		var ev claudeEvent
		if err := json.Unmarshal(raw, &ev); err != nil {
			continue
		}
		if ev.Type != "assistant" {
			continue
		}
		for _, c := range ev.Message.Content {
			if c.Type != "tool_use" {
				continue
			}
			if c.Name == fq {
				lastPrimary = ev.UUID
			}
			if strings.HasPrefix(c.Name, chatMCPToolPrefixClaude) {
				lastFallback = ev.UUID
			}
			if cliTool, ok := claudeBashAgentChatTool(claudeContentBashCommand(c)); ok {
				if cliTool == toolName {
					lastPrimary = ev.UUID
				}
				lastFallback = ev.UUID
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return "", err
	}
	if lastPrimary != "" {
		return lastPrimary, nil
	}
	if lastFallback != "" {
		return lastFallback, nil
	}
	return "", fmt.Errorf("no %s* tool_use or `mcp %s` Bash call found", chatMCPToolPrefixClaude, chatCLIServerClaude)
}

// claudeFindAssistantEventByToolUseID scans for the assistant event whose
// content contains a tool_use with the given tool_use_id, returning that
// event's uuid. Used to translate bubble-anchored AnchorIDs (which carry
// tool_use_id, per fork_resolve.go) into the event uuid claudeCopyUntil
// matches against.
func claudeFindAssistantEventByToolUseID(path, toolUseID string) (string, error) {
	if toolUseID == "" {
		return "", fmt.Errorf("empty tool_use_id")
	}
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	scanner := newBigScanner(f)
	for scanner.Scan() {
		var ev claudeEvent
		if err := json.Unmarshal(scanner.Bytes(), &ev); err != nil {
			continue
		}
		if ev.Type != "assistant" {
			continue
		}
		for _, c := range ev.Message.Content {
			if c.Type == "tool_use" && c.ID == toolUseID {
				return ev.UUID, nil
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return "", err
	}
	return "", fmt.Errorf("tool_use_id %s not found in %s", toolUseID, path)
}

// ClaudeIsTailActive reports whether the source claude .jsonl ends with an
// unresolved non-chat tool_use -- i.e. the agent is mid-work (running bash,
// editing a file, etc.) with a tool call that has no matching tool_result
// yet. Such a session cannot be cleanly forked: truncating mid-tool-call
// either leaves an invalid resume point or strips in-flight work the user
// hasn't seen the result of. Callers should refuse the fork (or wait for
// the source to settle) when this returns true.
//
// Agent-chat tool calls (send_message, check_messages, ...) are NOT
// counted as active even when their tool_result is absent -- a parked
// send_message is the natural WAITING state and is safe to fork at.
// That exemption covers both shapes: native mcp__swe-swe-agent-chat__*
// tool_uses AND Bash tool_uses invoking `mcp swe-swe-agent-chat ...`
// (MCP-less mode), where the blocking send_message is a Bash call that
// stays result-less for as long as the user takes to reply.
func ClaudeIsTailActive(path string) (bool, error) {
	f, err := os.Open(path)
	if err != nil {
		return false, err
	}
	defer f.Close()

	// Pairing is ORDER-INDEPENDENT: gather every non-chat tool_use id and
	// every tool_result id across the whole file, then subtract. A line-by-
	// line "add-on-use, delete-on-result" walk assumes each tool_use line
	// precedes its tool_result line -- but ToolSearch (the deferred-tool
	// loader) flushes its result event to the .jsonl BEFORE its own tool_use
	// line, so the running walk never clears it and falsely reports ACTIVE,
	// permanently 409-ing /api/fork on otherwise-settled recordings. Tool ids
	// are unique, so set-difference is both correct and immune to flush order.
	scanner := newBigScanner(f)
	uses := make(map[string]bool)
	results := make(map[string]bool)
	for scanner.Scan() {
		var ev claudeEvent
		if err := json.Unmarshal(scanner.Bytes(), &ev); err != nil {
			continue
		}
		switch ev.Type {
		case "assistant":
			for _, c := range ev.Message.Content {
				if c.Type != "tool_use" {
					continue
				}
				if strings.HasPrefix(c.Name, chatMCPToolPrefixClaude) {
					continue
				}
				if _, ok := claudeBashAgentChatTool(claudeContentBashCommand(c)); ok {
					continue
				}
				uses[c.ID] = true
			}
		case "user":
			for _, c := range ev.Message.Content {
				if c.Type == "tool_result" && c.ToolUseID != "" {
					results[c.ToolUseID] = true
				}
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return false, err
	}
	for id := range uses {
		if !results[id] {
			return true, nil
		}
	}
	return false, nil
}

// claudeCopyUntil streams src into dst, rewriting every literal occurrence of
// oldSessionID to newSessionID, and stops after emitting the line whose uuid
// equals anchorUUID. Returns an error if the anchor was never encountered.
func claudeCopyUntil(srcPath, dstPath, oldSessionID, newSessionID, anchorUUID string) error {
	in, err := os.Open(srcPath)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dstPath)
	if err != nil {
		return err
	}
	defer out.Close()

	scanner := newBigScanner(in)
	w := bufio.NewWriter(out)
	defer w.Flush()

	found := false
	for scanner.Scan() {
		line := scanner.Text()
		rewritten := strings.ReplaceAll(line, oldSessionID, newSessionID)
		if _, err := io.WriteString(w, rewritten); err != nil {
			return err
		}
		if _, err := w.WriteString("\n"); err != nil {
			return err
		}
		var ev claudeEvent
		if err := json.Unmarshal([]byte(line), &ev); err == nil && ev.UUID == anchorUUID {
			found = true
			break
		}
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	if !found {
		return fmt.Errorf("anchor uuid %s not present in %s", anchorUUID, srcPath)
	}
	return nil
}

// claudeEvent is just enough of Claude's per-line schema for our purposes.
// Unknown fields are ignored by encoding/json.
type claudeEvent struct {
	Type    string         `json:"type"`
	UUID    string         `json:"uuid"`
	Message claudeMessage  `json:"message"`
}

type claudeMessage struct {
	Content []claudeContent `json:"content"`
}

type claudeContent struct {
	Type      string          `json:"type"`
	ID        string          `json:"id,omitempty"`
	Name      string          `json:"name,omitempty"`
	ToolUseID string          `json:"tool_use_id,omitempty"`
	Input     json.RawMessage `json:"input,omitempty"`
}
