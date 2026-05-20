package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
)

// Hydrating ended sessions for /api/fork
//
// `sessions[uuid]` only holds live processes; endSessionByUUID removes
// entries on shutdown. For /api/fork to reach back into an ended session,
// we hydrate just enough state from the recording artifacts on disk:
//
//   - session-<uuid>.metadata.json: workdir, assistant, mode, name, extra args,
//     and (post the AgentSessionID capture commit) agent_session_id.
//   - session-<uuid>-*.events.jsonl: the agent-chat sidecar's event log;
//     becomes PrepopulateChatLog for the new session so the browser tab
//     replays the same bubbles.
//
// For sessions that started BEFORE AgentSessionID capture existed, the
// metadata's agent_session_id is empty. fingerprintClaudeSessionByEvents
// recovers it by matching agentMessage texts in the chat events file
// against Claude's tool_use input.text values in each candidate jsonl.

// resolveForkSource returns the fields handleSessionForkAPI needs about a
// source session, sourcing from the in-memory map for live sessions and
// from recording artifacts on disk for ended ones.
func resolveForkSource(sourceUUID string) (*hydratedForkSource, error) {
	sessionsMu.RLock()
	live, ok := sessions[sourceUUID]
	sessionsMu.RUnlock()
	if ok {
		return &hydratedForkSource{
			UUID:           live.UUID,
			Name:           live.Name,
			Assistant:      live.Assistant,
			WorkDir:        live.WorkDir,
			SessionMode:    live.SessionMode,
			ExtraArgs:      live.ExtraArgs,
			Theme:          live.Theme,
			ChatLogPath:    live.ChatLogPath,
			AgentSessionID: live.AgentSessionID,
		}, nil
	}
	return loadEndedForkSource(sourceUUID)
}

// hydratedForkSource is the in-memory shape of an ended session, populated
// from disk artifacts. Mirrors the subset of Session fields handleSessionForkAPI
// actually reads.
type hydratedForkSource struct {
	UUID           string
	Name           string
	Assistant      string
	WorkDir        string
	SessionMode    string
	ExtraArgs      string
	Theme          string
	ChatLogPath    string
	AgentSessionID string
}

// loadEndedForkSource reads recording artifacts for sourceUUID and returns
// the subset of fields needed to fork the session. Returns an error if the
// metadata file is missing, malformed, or if no chat event log can be
// located.
func loadEndedForkSource(sourceUUID string) (*hydratedForkSource, error) {
	metaPath := filepath.Join(recordingsDir, fmt.Sprintf("session-%s.metadata.json", sourceUUID))
	data, err := os.ReadFile(metaPath)
	if err != nil {
		return nil, fmt.Errorf("read metadata %s: %w", metaPath, err)
	}
	var meta RecordingMetadata
	if err := json.Unmarshal(data, &meta); err != nil {
		return nil, fmt.Errorf("parse metadata %s: %w", metaPath, err)
	}
	chatLogPath, err := findChatLogPathForSession(sourceUUID)
	if err != nil {
		return nil, err
	}
	assistant := meta.AgentBinary
	if assistant == "" {
		// Pre-AgentBinary recordings only stored the display name; map it
		// back to the binary by case-insensitive comparison against the
		// configured assistants.
		for _, a := range assistantConfigs {
			if strings.EqualFold(a.Name, meta.Agent) {
				assistant = a.Binary
				break
			}
		}
	}
	return &hydratedForkSource{
		UUID:           sourceUUID,
		Name:           meta.Name,
		Assistant:      assistant,
		WorkDir:        meta.WorkDir,
		SessionMode:    meta.SessionMode,
		ExtraArgs:      meta.ExtraArgs,
		ChatLogPath:    chatLogPath,
		AgentSessionID: meta.AgentSessionID,
	}, nil
}

// findChatLogPathForSession returns the .events.jsonl path for the chat
// sub-recording of sourceUUID. The agent-chat sidecar writes its event log to
// session-<parent>-<child>.events.jsonl; the child uuid is generated at
// session start. We discover it by directory scan rather than persisting it,
// to keep this resilient to older recordings that didn't track it.
func findChatLogPathForSession(sourceUUID string) (string, error) {
	prefix := fmt.Sprintf("session-%s-", sourceUUID)
	entries, err := os.ReadDir(recordingsDir)
	if err != nil {
		return "", fmt.Errorf("scan recordings dir: %w", err)
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if strings.HasPrefix(name, prefix) && strings.HasSuffix(name, ".events.jsonl") {
			return filepath.Join(recordingsDir, name), nil
		}
	}
	return "", fmt.Errorf("no chat events log for session %s in %s", sourceUUID, recordingsDir)
}

// fingerprintClaudeSessionByEvents recovers a Claude session id by matching
// distinctive agentMessage texts from the chat events file against tool_use
// input.text values in each candidate .jsonl under
// ~/.claude/projects/<encoded-workdir>/. Used as a legacy fallback when the
// recording metadata was written before AgentSessionID capture existed.
//
// The fingerprint is the set of `text` fields from the first few
// agentMessage/verbalReply events. Each is also passed verbatim to Claude as
// the `text` argument of `mcp__swe-swe-agent-chat__send_message` /
// `send_verbal_reply` tool_use calls, so they appear inside the source
// .jsonl as substrings.
func fingerprintClaudeSessionByEvents(workDir, chatLogPath string) (string, error) {
	needles, err := loadAgentTextNeedles(chatLogPath, 5)
	if err != nil {
		return "", fmt.Errorf("load fingerprint needles: %w", err)
	}
	if len(needles) == 0 {
		return "", fmt.Errorf("chat events file has no agentMessage/verbalReply texts to fingerprint with")
	}
	encoded := strings.ReplaceAll(workDir, "/", "-")
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(home, ".claude", "projects", encoded)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", fmt.Errorf("read %s: %w", dir, err)
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".jsonl") {
			continue
		}
		path := filepath.Join(dir, e.Name())
		hits, err := countNeedleHitsInClaudeJsonl(path, needles)
		if err != nil {
			log.Printf("WARN: fingerprint scan %s: %v", path, err)
			continue
		}
		// Require ALL needles to be present. Anything less is ambiguous --
		// two sessions in the same workdir could share an early reply by
		// chance, but the odds of sharing five distinct multi-paragraph
		// replies drop to negligible.
		if hits == len(needles) {
			id := strings.TrimSuffix(e.Name(), ".jsonl")
			log.Printf("fingerprint matched legacy chat events -> claude session %s (%d/%d needles)", id, hits, len(needles))
			return id, nil
		}
	}
	return "", fmt.Errorf("no claude .jsonl in %s matched the chat events fingerprint", dir)
}

// loadAgentTextNeedles returns up to max distinctive `text` strings from
// agentMessage/verbalReply events in the chat log. Trimmed to a length that
// is unique enough to fingerprint but small enough to grep efficiently.
func loadAgentTextNeedles(chatLogPath string, max int) ([]string, error) {
	f, err := os.Open(chatLogPath)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	const maxNeedleLen = 120 // long enough to be distinctive, short enough to survive JSON-escape variance
	var needles []string
	seen := map[string]bool{}
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 64*1024), 8*1024*1024)
	for scanner.Scan() {
		var ev struct {
			Type string `json:"type"`
			Text string `json:"text"`
		}
		if err := json.Unmarshal(scanner.Bytes(), &ev); err != nil {
			continue
		}
		if ev.Type != "agentMessage" && ev.Type != "verbalReply" {
			continue
		}
		t := strings.TrimSpace(ev.Text)
		if t == "" {
			continue
		}
		if len(t) > maxNeedleLen {
			t = t[:maxNeedleLen]
		}
		if seen[t] {
			continue
		}
		seen[t] = true
		needles = append(needles, t)
		if len(needles) >= max {
			break
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return needles, nil
}

// countNeedleHitsInClaudeJsonl scans path for substring presence of each
// needle. Returns how many needles were found (anywhere in the file).
// Substring matching tolerates JSON escaping differences -- Claude's .jsonl
// embeds the text as a JSON-quoted string but with the same UTF-8 bytes for
// the common case (no embedded quotes/backslashes), and even with escaping
// the prefix of a needle still matches.
func countNeedleHitsInClaudeJsonl(path string, needles []string) (int, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer f.Close()
	hit := make([]bool, len(needles))
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 64*1024), 16*1024*1024) // Claude jsonl lines can be large
	for scanner.Scan() {
		line := scanner.Bytes()
		for i, n := range needles {
			if hit[i] {
				continue
			}
			if bytesContainsString(line, n) {
				hit[i] = true
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return 0, err
	}
	hits := 0
	for _, h := range hit {
		if h {
			hits++
		}
	}
	return hits, nil
}

// bytesContainsString is strings.Contains without the []byte->string copy.
func bytesContainsString(haystack []byte, needle string) bool {
	return strings.Contains(string(haystack), needle)
}
