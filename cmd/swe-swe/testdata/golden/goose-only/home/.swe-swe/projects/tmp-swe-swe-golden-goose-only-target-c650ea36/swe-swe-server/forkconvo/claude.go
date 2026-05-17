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

	anchorUUID := opts.Anchor
	if opts.Anchor == AnchorLastChatReply {
		anchorUUID, err = claudeFindLastChatReply(src, opts.Tool)
		if err != nil {
			return nil, fmt.Errorf("resolve %s: %w", AnchorLastChatReply, err)
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
// tool_use whose name matches the requested agent-chat MCP tool, then
// returns the uuid of the *user* event that carries the matching
// tool_result. Cutting at the user event (inclusive) preserves the
// tool_use/tool_result pair so Claude can resume cleanly.
//
// If the requested toolName has no matching tool_use anywhere in the
// session, we fall back to ANY agent-chat MCP tool (e.g. check_messages).
// This handles the "channels" runtime where Claude's text response is
// streamed directly to agent-chat without an explicit send_message
// tool_use entry in the .jsonl -- the most recent check_messages
// tool_result is still a valid fork point because it represents the
// state right before Claude responds.
func claudeFindLastChatReply(path, toolName string) (string, error) {
	fq := chatMCPToolPrefixClaude + toolName
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	scanner := newBigScanner(f)
	var pendingPrimary, pendingFallback string
	var lastMatchPrimary, lastMatchFallback string
	for scanner.Scan() {
		raw := scanner.Bytes()
		var ev claudeEvent
		if err := json.Unmarshal(raw, &ev); err != nil {
			continue
		}
		switch ev.Type {
		case "assistant":
			for _, c := range ev.Message.Content {
				if c.Type != "tool_use" {
					continue
				}
				if c.Name == fq {
					pendingPrimary = c.ID
				}
				if strings.HasPrefix(c.Name, chatMCPToolPrefixClaude) {
					pendingFallback = c.ID
				}
			}
		case "user":
			if pendingPrimary == "" && pendingFallback == "" {
				continue
			}
			for _, c := range ev.Message.Content {
				if c.Type != "tool_result" {
					continue
				}
				if c.ToolUseID == pendingPrimary {
					lastMatchPrimary = ev.UUID
					pendingPrimary = ""
				}
				if c.ToolUseID == pendingFallback {
					lastMatchFallback = ev.UUID
					pendingFallback = ""
				}
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return "", err
	}
	if lastMatchPrimary != "" {
		return lastMatchPrimary, nil
	}
	if lastMatchFallback != "" {
		return lastMatchFallback, nil
	}
	return "", fmt.Errorf("no %s* tool_use/tool_result pair found", chatMCPToolPrefixClaude)
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
	Type      string `json:"type"`
	ID        string `json:"id,omitempty"`
	Name      string `json:"name,omitempty"`
	ToolUseID string `json:"tool_use_id,omitempty"`
}
