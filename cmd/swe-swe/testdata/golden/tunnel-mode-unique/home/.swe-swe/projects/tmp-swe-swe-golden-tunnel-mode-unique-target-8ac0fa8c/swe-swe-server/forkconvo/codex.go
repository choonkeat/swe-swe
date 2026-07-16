package forkconvo

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
)

// chatMCPNamespaceCodex is the namespace Codex stamps on MCP tool calls in
// its rollout JSONL. Note the underscore form (not dash like Claude).
const chatMCPNamespaceCodex = "mcp__swe_swe_agent_chat__"

func codexSessionsRoot() string {
	if v := os.Getenv("CODEX_HOME"); v != "" {
		return filepath.Join(v, "sessions")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".codex", "sessions")
}

// findCodexSession walks the year/month/day tree for a rollout file ending in
// "-<sessionID>.jsonl".
func findCodexSession(sessionID string) (string, error) {
	root := codexSessionsRoot()
	var match string
	err := filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		name := d.Name()
		if strings.HasPrefix(name, "rollout-") && strings.HasSuffix(name, "-"+sessionID+".jsonl") {
			match = p
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	if match == "" {
		return "", fmt.Errorf("codex session %s not found under %s", sessionID, root)
	}
	return match, nil
}

func forkCodex(opts Opts) (*Result, error) {
	src, err := findCodexSession(opts.SourceSessionID)
	if err != nil {
		return nil, err
	}

	anchorCallID := opts.Anchor
	if opts.Anchor == AnchorLastChatReply {
		anchorCallID, err = codexFindLastChatReply(src, opts.Tool)
		if err != nil {
			return nil, fmt.Errorf("resolve %s: %w", AnchorLastChatReply, err)
		}
	}

	newID := uuid.NewString()
	now := time.Now().UTC()
	dstDir := filepath.Join(codexSessionsRoot(),
		fmt.Sprintf("%04d", now.Year()),
		fmt.Sprintf("%02d", now.Month()),
		fmt.Sprintf("%02d", now.Day()))
	if err := os.MkdirAll(dstDir, 0o755); err != nil {
		return nil, err
	}
	ts := now.Format("2006-01-02T15-04-05")
	dst := filepath.Join(dstDir, fmt.Sprintf("rollout-%s-%s.jsonl", ts, newID))

	if err := codexCopyUntil(src, dst, opts.SourceSessionID, newID, anchorCallID); err != nil {
		_ = os.Remove(dst)
		return nil, err
	}
	return &Result{
		NewSessionID:  newID,
		NewSourcePath: dst,
		AnchorUUID:    anchorCallID,
	}, nil
}

func codexFindLastChatReply(path, toolName string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	scanner := newBigScanner(f)
	var lastCallID string
	for scanner.Scan() {
		var ev codexEvent
		if err := json.Unmarshal(scanner.Bytes(), &ev); err != nil {
			continue
		}
		if ev.Payload.Type == "function_call" &&
			ev.Payload.Namespace == chatMCPNamespaceCodex &&
			ev.Payload.Name == toolName {
			lastCallID = ev.Payload.CallID
		}
	}
	if err := scanner.Err(); err != nil {
		return "", err
	}
	if lastCallID == "" {
		return "", fmt.Errorf("no codex %s function_call found", toolName)
	}
	return lastCallID, nil
}

// codexCopyUntil cuts AFTER the function_call (the agent's tool call)
// whose call_id matches anchorCallID. We deliberately stop BEFORE the
// matching function_call_output so the forked rollout's tail is in a
// WAITING shape -- the assistant has issued the agent-chat call, no
// response folded in -- avoiding the PENDING-ACTION runaway where the
// resumed agent autonomously executes the source's trailing directive.
// See forkconvo/claude.go's claudeFindLastChatReply doc for the full
// reasoning; codex's design problem is symmetric.
func codexCopyUntil(srcPath, dstPath, oldSessionID, newSessionID, anchorCallID string) error {
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
		var ev codexEvent
		if err := json.Unmarshal([]byte(line), &ev); err == nil &&
			ev.Payload.Type == "function_call" &&
			ev.Payload.CallID == anchorCallID {
			found = true
			break
		}
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	if !found {
		return fmt.Errorf("anchor call_id %s not present in %s", anchorCallID, srcPath)
	}
	return nil
}

// CodexIsTailActive reports whether the source codex rollout ends with an
// unresolved non-chat function_call -- the codex analogue of
// ClaudeIsTailActive. Agent-chat namespace calls (send_message,
// check_messages, ...) are skipped: a parked agent-chat call is the
// natural WAITING state and safe to fork at.
func CodexIsTailActive(path string) (bool, error) {
	f, err := os.Open(path)
	if err != nil {
		return false, err
	}
	defer f.Close()

	// Order-independent pairing (see ClaudeIsTailActive): gather all non-chat
	// function_call ids and all function_call_output ids, then subtract. A
	// running add/delete walk would miscount any call whose output line is
	// flushed ahead of its call line; call ids are unique so set-difference
	// is correct regardless of file order.
	scanner := newBigScanner(f)
	calls := make(map[string]bool)
	outputs := make(map[string]bool)
	for scanner.Scan() {
		var ev codexEvent
		if err := json.Unmarshal(scanner.Bytes(), &ev); err != nil {
			continue
		}
		if ev.Type != "response_item" {
			continue
		}
		switch ev.Payload.Type {
		case "function_call":
			if ev.Payload.Namespace == chatMCPNamespaceCodex {
				continue
			}
			calls[ev.Payload.CallID] = true
		case "function_call_output":
			if ev.Payload.CallID != "" {
				outputs[ev.Payload.CallID] = true
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return false, err
	}
	for id := range calls {
		if !outputs[id] {
			return true, nil
		}
	}
	return false, nil
}

type codexEvent struct {
	Type    string       `json:"type"`
	Payload codexPayload `json:"payload"`
}

type codexPayload struct {
	Type      string `json:"type"`
	CallID    string `json:"call_id"`
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
}
