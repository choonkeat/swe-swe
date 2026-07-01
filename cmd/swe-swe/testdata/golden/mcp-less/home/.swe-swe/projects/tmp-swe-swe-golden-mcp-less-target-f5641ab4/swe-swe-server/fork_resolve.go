package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
)

// Fork-bubble resolver: maps a chat bubble in `.events.jsonl` to the
// per-agent tool_use_id / call_id that forkconvo needs as an anchor.
//
// Two layers:
//
//   1. resolveBubbleAnchor (the entry point used by /api/fork). Reads the
//      bubble from the chat events log. If the bubble (or its userMessage's
//      matching userMessagesConsumed event) carries an AgentToolSeq stamp,
//      we use that ordinal to locate the exact MCP tool_use in the agent's
//      own .jsonl. This is the robust path -- it requires no text content
//      and survives text drift between the sidecar and the agent.
//
//   2. resolveBubbleAnchorByText (fallback). For events written by older
//      agent-chat sidecars (no stamp), we substring-match the bubble's
//      text into the agent's .jsonl. This is the same theory the existing
//      fingerprintClaudeSessionByEvents (fork_legacy.go) uses, but applied
//      per-bubble. Drift in agent-chat's text transformations can break
//      this -- the stamped path doesn't have that weakness.

// Anchor namespaces. Claude renders MCP tools as `mcp__server__tool` in its
// JSONL; codex uses underscores in the namespace and stores name without
// the server prefix.
const (
	chatMCPNamespaceClaudeJSONL = "mcp__swe-swe-agent-chat__"
	chatMCPNamespaceCodexJSONL  = "mcp__swe_swe_agent_chat__"
)

// ResolvedAnchor is what handleSessionForkAPI hands to forkconvo.Fork.
type ResolvedAnchor struct {
	// AnchorID is the per-agent identifier forkconvo expects:
	//   claude  -> tool_use_id (e.g. "toolu_01...")
	//   codex   -> call_id     (e.g. "call_...")
	AnchorID string

	// BubbleKind is "userMessage" or "agentMessage".
	BubbleKind string

	// ToolName is the MCP tool that produced (agent side) or drained
	// (user side) the bubble: "send_message", "check_messages", etc.
	ToolName string

	// Mode is the requested fork semantic: "after", "replay", "before".
	// Forkconvo's existing cut-point logic only implements "after" today;
	// the other modes are reserved for the UI rollout.
	Mode string

	// ResolverUsed identifies which path produced AnchorID: "stamp" for
	// ordinal correlation, "text" for substring fallback.
	ResolverUsed string
}

// bubbleEvent mirrors the subset of agent-chat's Event struct the resolver
// needs. JSON tags match agent-chat exactly so the same .events.jsonl file
// round-trips.
type bubbleEvent struct {
	Type          string   `json:"type"`
	Seq           int64    `json:"seq"`
	ID            string   `json:"id,omitempty"`
	IDs           []string `json:"ids,omitempty"`
	Text          string   `json:"text,omitempty"`
	AgentToolSeq  int64    `json:"agent_tool_seq,omitempty"`
	AgentToolName string   `json:"agent_tool_name,omitempty"`
}

// ErrBubbleNotDrained means the userMessage bubble is in `.events.jsonl` but
// the matching userMessagesConsumed event hasn't been written yet -- the
// agent hasn't called check_messages on it. Typically a race between
// agent-chat publishing and the agent's MCP call landing; retry shortly.
var ErrBubbleNotDrained = errors.New("userMessage bubble not yet consumed by an MCP tool call")

// ErrBubbleNotFound means no event at the requested seq.
var ErrBubbleNotFound = errors.New("bubble seq not found in events log")

// ErrChannelsAgentBubble means the bubble is an agentMessage in
// channels-mode claude where the agent's reply was streamed directly to
// agent-chat without producing an MCP send_message tool_use in the .jsonl.
// Fork-after-agent-bubble has no .jsonl correlate in that case -- callers
// must fall back to fork-after-the-preceding-user-message semantics.
var ErrChannelsAgentBubble = errors.New("channels-mode agent bubble has no MCP tool_use to anchor on")

// resolveBubbleAnchor is the entry point. It returns the agent-side anchor
// for the bubble at bubbleSeq in eventsPath, looking up the corresponding
// tool_use_id / call_id in agentJsonl.
func resolveBubbleAnchor(eventsPath, agentJsonl, agent string, bubbleSeq int64, mode string) (*ResolvedAnchor, error) {
	bubble, consumed, err := loadBubbleAndConsume(eventsPath, bubbleSeq)
	if err != nil {
		return nil, fmt.Errorf("locate bubble seq=%d in %s: %w", bubbleSeq, eventsPath, err)
	}

	var toolName string
	var toolSeq int64
	switch bubble.Type {
	case "agentMessage", "verbalReply":
		toolName, toolSeq = bubble.AgentToolName, bubble.AgentToolSeq
	case "userMessage":
		if consumed == nil {
			return nil, ErrBubbleNotDrained
		}
		toolName, toolSeq = consumed.AgentToolName, consumed.AgentToolSeq
	default:
		return nil, fmt.Errorf("bubble seq=%d has unsupported type %q", bubbleSeq, bubble.Type)
	}

	if toolSeq > 0 && toolName != "" {
		id, err := findNthMCPToolCall(agentJsonl, agent, toolName, toolSeq)
		if err != nil {
			return nil, fmt.Errorf("locate %s call #%d in %s: %w", toolName, toolSeq, agentJsonl, err)
		}
		return &ResolvedAnchor{
			AnchorID:     id,
			BubbleKind:   bubble.Type,
			ToolName:     toolName,
			Mode:         mode,
			ResolverUsed: "stamp",
		}, nil
	}

	// Unstamped (legacy event, or channels-mode agent bubble). Fall through
	// to the text-correlation path.
	return resolveBubbleAnchorByText(eventsPath, agentJsonl, agent, bubble, mode)
}

// findNthMCPToolCall walks the agent .jsonl in order and returns the
// tool_use_id (claude) / call_id (codex) of the n-th call to the named MCP
// tool in the swe-swe-agent-chat namespace.
func findNthMCPToolCall(path, agent, toolName string, n int64) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 16<<20)
	var count int64
	for sc.Scan() {
		id, ok := mcpToolCallID(sc.Bytes(), agent, toolName)
		if !ok {
			continue
		}
		count++
		if count == n {
			return id, nil
		}
	}
	if err := sc.Err(); err != nil {
		return "", err
	}
	return "", fmt.Errorf("only %d of %d expected %s calls present", count, n, toolName)
}

// mcpToolCallID extracts the tool_use_id / call_id from one JSONL line, if
// that line represents an MCP call to the agent-chat namespace's named
// tool. Per-agent JSONL shapes:
//
//	claude:  {"type":"assistant","message":{"content":[{"type":"tool_use","id":"toolu_...","name":"mcp__swe-swe-agent-chat__X"}]}}
//	codex:   {"type":"response_item","payload":{"type":"function_call","call_id":"call_...","namespace":"mcp__swe_swe_agent_chat__","name":"X"}}
//
// Returns ok=false for any other line (including parse errors), so the
// caller's count only advances on legitimate matches.
func mcpToolCallID(line []byte, agent, toolName string) (string, bool) {
	switch agent {
	case "claude":
		var ev struct {
			Type    string `json:"type"`
			Message struct {
				Content []struct {
					Type string `json:"type"`
					ID   string `json:"id"`
					Name string `json:"name"`
				} `json:"content"`
			} `json:"message"`
		}
		if err := json.Unmarshal(line, &ev); err != nil || ev.Type != "assistant" {
			return "", false
		}
		want := chatMCPNamespaceClaudeJSONL + toolName
		for _, c := range ev.Message.Content {
			if c.Type == "tool_use" && c.Name == want {
				return c.ID, true
			}
		}
	case "codex":
		var ev struct {
			Type    string `json:"type"`
			Payload struct {
				Type      string `json:"type"`
				CallID    string `json:"call_id"`
				Namespace string `json:"namespace"`
				Name      string `json:"name"`
			} `json:"payload"`
		}
		if err := json.Unmarshal(line, &ev); err != nil {
			return "", false
		}
		if ev.Type == "response_item" &&
			ev.Payload.Type == "function_call" &&
			ev.Payload.Namespace == chatMCPNamespaceCodexJSONL &&
			ev.Payload.Name == toolName {
			return ev.Payload.CallID, true
		}
	}
	return "", false
}

// loadBubbleAndConsume scans the events JSONL once, returning the bubble at
// seq and (for userMessage bubbles) the matching userMessagesConsumed event
// listing that bubble's ID. Either may be nil if not present; the caller
// distinguishes "not found" from "found but not drained".
func loadBubbleAndConsume(path string, seq int64) (*bubbleEvent, *bubbleEvent, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, nil, err
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 16<<20)

	var bubble *bubbleEvent
	var consumed *bubbleEvent
	for sc.Scan() {
		var ev bubbleEvent
		if err := json.Unmarshal(sc.Bytes(), &ev); err != nil {
			// Tolerate parse errors on the (potentially torn) last line.
			continue
		}
		if ev.Seq == seq && bubble == nil {
			b := ev
			bubble = &b
		}
		if ev.Type == "userMessagesConsumed" && bubble != nil && bubble.ID != "" {
			for _, id := range ev.IDs {
				if id == bubble.ID {
					c := ev
					consumed = &c
					break
				}
			}
		}
	}
	if err := sc.Err(); err != nil {
		return nil, nil, err
	}
	if bubble == nil {
		return nil, nil, ErrBubbleNotFound
	}
	return bubble, consumed, nil
}

// resolveBubbleAnchorByText is the text-correlation fallback for events
// without an AgentToolSeq stamp (legacy sidecars). It substring-matches the
// bubble's text against tool_use args (agentMessage) or tool_result content
// ("User said: ..." prefix for userMessage) in the agent's .jsonl, and
// returns the first occurrence's tool_use_id / call_id.
//
// Caveats documented in design discussion: false positives on short text,
// drift if agent-chat ever rewrites bubble text before forwarding it,
// ordinal disambiguation needed when the same text appears multiple times
// (re-drain, agent retry). This implementation handles the same-text case
// by counting prior bubbles in events.jsonl with the same text and taking
// the corresponding occurrence in the .jsonl.
func resolveBubbleAnchorByText(eventsPath, agentJsonl, agent string, bubble *bubbleEvent, mode string) (*ResolvedAnchor, error) {
	if bubble.Type == "agentMessage" && agent == "claude" {
		// Channels-mode: the agent reply was streamed directly and never
		// produced a tool_use entry. The caller knows to fall back to the
		// preceding userMessage's anchor.
		return nil, ErrChannelsAgentBubble
	}
	if len(strings.TrimSpace(bubble.Text)) < 20 {
		return nil, fmt.Errorf("bubble seq=%d text too short for reliable text correlation (%d chars)", bubble.Seq, len(bubble.Text))
	}

	ordinal, err := countSameTextBubbles(eventsPath, bubble)
	if err != nil {
		return nil, fmt.Errorf("count prior matching bubbles: %w", err)
	}

	id, err := findNthMCPToolCallContainingText(agentJsonl, agent, bubble.Type, bubble.Text, ordinal)
	if err != nil {
		return nil, fmt.Errorf("text-locate bubble seq=%d in %s: %w", bubble.Seq, agentJsonl, err)
	}
	return &ResolvedAnchor{
		AnchorID:     id,
		BubbleKind:   bubble.Type,
		ToolName:     "", // unknown via text correlation
		Mode:         mode,
		ResolverUsed: "text",
	}, nil
}

// countSameTextBubbles returns the 1-based ordinal of bubble among events
// of the same Type with identical Text in eventsPath (up to and including
// bubble's seq).
func countSameTextBubbles(eventsPath string, bubble *bubbleEvent) (int64, error) {
	f, err := os.Open(eventsPath)
	if err != nil {
		return 0, err
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 16<<20)
	var ord int64
	for sc.Scan() {
		var ev bubbleEvent
		if err := json.Unmarshal(sc.Bytes(), &ev); err != nil {
			continue
		}
		if ev.Type == bubble.Type && ev.Text == bubble.Text {
			ord++
			if ev.Seq == bubble.Seq {
				return ord, nil
			}
		}
	}
	if err := sc.Err(); err != nil {
		return 0, err
	}
	return 0, fmt.Errorf("bubble seq=%d not found while counting", bubble.Seq)
}

// findNthMCPToolCallContainingText returns the tool_use_id / call_id of the
// n-th MCP call in agent's .jsonl whose tool args / result text contains
// the given needle. For userMessage bubbles we match tool_result content;
// for agentMessage bubbles we match tool_use args.
func findNthMCPToolCallContainingText(path, agent, bubbleType, needle string, n int64) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 16<<20)
	var count int64
	for sc.Scan() {
		id, ok := mcpCallIDForBubbleText(sc.Bytes(), agent, bubbleType, needle)
		if !ok {
			continue
		}
		count++
		if count == n {
			return id, nil
		}
	}
	if err := sc.Err(); err != nil {
		return "", err
	}
	return "", fmt.Errorf("text needle matched only %d of %d", count, n)
}

// mcpCallIDForBubbleText returns the per-agent call id if the line is an
// MCP call whose args (for agentMessage) or result content (for
// userMessage) contains the needle.
func mcpCallIDForBubbleText(line []byte, agent, bubbleType, needle string) (string, bool) {
	switch agent {
	case "claude":
		var ev struct {
			Type    string `json:"type"`
			Message struct {
				Content []struct {
					Type      string          `json:"type"`
					ID        string          `json:"id"`
					Name      string          `json:"name"`
					Input     json.RawMessage `json:"input,omitempty"`
					ToolUseID string          `json:"tool_use_id,omitempty"`
					Content   json.RawMessage `json:"content,omitempty"`
				} `json:"content"`
			} `json:"message"`
		}
		if err := json.Unmarshal(line, &ev); err != nil {
			return "", false
		}
		if bubbleType == "agentMessage" && ev.Type == "assistant" {
			for _, c := range ev.Message.Content {
				if c.Type != "tool_use" {
					continue
				}
				if !strings.HasPrefix(c.Name, chatMCPNamespaceClaudeJSONL) {
					continue
				}
				if strings.Contains(string(c.Input), needle) {
					return c.ID, true
				}
			}
		}
		if bubbleType == "userMessage" && ev.Type == "user" {
			for _, c := range ev.Message.Content {
				if c.Type != "tool_result" {
					continue
				}
				if strings.Contains(string(c.Content), needle) {
					return c.ToolUseID, true
				}
			}
		}
	case "codex":
		var ev struct {
			Type    string `json:"type"`
			Payload struct {
				Type      string `json:"type"`
				CallID    string `json:"call_id"`
				Namespace string `json:"namespace"`
				Name      string `json:"name"`
				Arguments string `json:"arguments,omitempty"`
				Output    string `json:"output,omitempty"`
			} `json:"payload"`
		}
		if err := json.Unmarshal(line, &ev); err != nil {
			return "", false
		}
		if ev.Type != "response_item" {
			return "", false
		}
		if bubbleType == "agentMessage" &&
			ev.Payload.Type == "function_call" &&
			ev.Payload.Namespace == chatMCPNamespaceCodexJSONL &&
			strings.Contains(ev.Payload.Arguments, needle) {
			return ev.Payload.CallID, true
		}
		if bubbleType == "userMessage" &&
			ev.Payload.Type == "function_call_output" &&
			strings.Contains(ev.Payload.Output, needle) {
			return ev.Payload.CallID, true
		}
	}
	return "", false
}
