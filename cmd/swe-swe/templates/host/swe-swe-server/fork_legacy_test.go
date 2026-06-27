package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// withTempRecordingsDir swaps the package-level recordingsDir for the duration
// of t. Restored on cleanup.
func withTempRecordingsDir(t *testing.T) string {
	t.Helper()
	old := recordingsDir
	dir := t.TempDir()
	recordingsDir = dir
	t.Cleanup(func() { recordingsDir = old })
	return dir
}

// writeMetadataFile writes a session-<uuid>.metadata.json into recordingsDir.
func writeMetadataFile(t *testing.T, sourceUUID string, meta RecordingMetadata) {
	t.Helper()
	path := filepath.Join(recordingsDir, "session-"+sourceUUID+".metadata.json")
	data, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		t.Fatalf("marshal metadata: %v", err)
	}
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatalf("write metadata: %v", err)
	}
}

// writeEventsFile writes a session-<parent>-<child>.events.jsonl with the
// given JSON-encoded events (one per line).
func writeEventsFile(t *testing.T, sourceUUID, chatUUID string, events []map[string]any) string {
	t.Helper()
	path := filepath.Join(recordingsDir, "session-"+sourceUUID+"-"+chatUUID+".events.jsonl")
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("create events: %v", err)
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	for _, ev := range events {
		if err := enc.Encode(ev); err != nil {
			t.Fatalf("encode event: %v", err)
		}
	}
	return path
}

func TestLoadEndedForkSource_Success(t *testing.T) {
	withTempRecordingsDir(t)
	sourceUUID := "11111111-1111-1111-1111-111111111111"
	chatUUID := "22222222-2222-2222-2222-222222222222"
	writeMetadataFile(t, sourceUUID, RecordingMetadata{
		UUID:           "rec-" + sourceUUID,
		Name:           "my-session",
		Agent:          "Claude",
		AgentBinary:    "claude",
		SessionMode:    "chat",
		WorkDir:        "/repos/my-project/workspace",
		ExtraArgs:      "--dangerously-skip-permissions",
		StartedAt:      time.Now().Add(-time.Hour),
		AgentSessionID: "abc-def",
	})
	writeEventsFile(t, sourceUUID, chatUUID, []map[string]any{
		{"type": "agentMessage", "text": "hello"},
	})

	src, err := loadEndedForkSource(sourceUUID)
	if err != nil {
		t.Fatalf("loadEndedForkSource: %v", err)
	}
	if src.Assistant != "claude" {
		t.Errorf("Assistant = %q, want claude", src.Assistant)
	}
	if src.SessionMode != "chat" {
		t.Errorf("SessionMode = %q, want chat", src.SessionMode)
	}
	if src.WorkDir != "/repos/my-project/workspace" {
		t.Errorf("WorkDir = %q", src.WorkDir)
	}
	if src.AgentSessionID != "abc-def" {
		t.Errorf("AgentSessionID = %q, want abc-def", src.AgentSessionID)
	}
	if !strings.HasSuffix(src.ChatLogPath, ".events.jsonl") {
		t.Errorf("ChatLogPath = %q", src.ChatLogPath)
	}
}

func TestLoadEndedForkSource_LegacyAgentNameOnly(t *testing.T) {
	withTempRecordingsDir(t)
	sourceUUID := "11111111-1111-1111-1111-111111111112"
	writeMetadataFile(t, sourceUUID, RecordingMetadata{
		// AgentBinary deliberately empty -- the legacy shape.
		Agent:       "Claude",
		SessionMode: "chat",
		WorkDir:     "/workspace",
	})
	writeEventsFile(t, sourceUUID, "child", nil)

	src, err := loadEndedForkSource(sourceUUID)
	if err != nil {
		t.Fatalf("loadEndedForkSource: %v", err)
	}
	if src.Assistant != "claude" {
		t.Errorf("Assistant resolved from name = %q, want claude", src.Assistant)
	}
}

func TestLoadEndedForkSource_MissingMetadataIsError(t *testing.T) {
	withTempRecordingsDir(t)
	if _, err := loadEndedForkSource("nonexistent-uuid"); err == nil {
		t.Fatal("expected error for missing metadata, got nil")
	}
}

func TestLoadEndedForkSource_MissingChatLogIsError(t *testing.T) {
	withTempRecordingsDir(t)
	sourceUUID := "11111111-1111-1111-1111-111111111113"
	writeMetadataFile(t, sourceUUID, RecordingMetadata{
		AgentBinary: "claude",
		SessionMode: "chat",
		WorkDir:     "/workspace",
	})
	// no events file written
	if _, err := loadEndedForkSource(sourceUUID); err == nil {
		t.Fatal("expected error for missing chat log, got nil")
	}
}

func TestFindChatLogPathForSession_PicksCorrectFile(t *testing.T) {
	withTempRecordingsDir(t)
	want := filepath.Join(recordingsDir, "session-aaa-bbb.events.jsonl")
	if err := os.WriteFile(want, []byte("{}\n"), 0644); err != nil {
		t.Fatal(err)
	}
	// Unrelated file with a similar but distinct prefix should not match.
	if err := os.WriteFile(filepath.Join(recordingsDir, "session-aab-ccc.events.jsonl"), []byte("{}\n"), 0644); err != nil {
		t.Fatal(err)
	}
	got, err := findChatLogPathForSession("aaa")
	if err != nil {
		t.Fatalf("findChatLogPathForSession: %v", err)
	}
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestLoadAgentTextNeedles_DedupAndLimit(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "chat.events.jsonl")
	events := []map[string]any{
		{"type": "agentMessage", "text": "first reply with distinctive phrasing"},
		{"type": "userMessage", "text": "user said something"},
		{"type": "agentMessage", "text": "first reply with distinctive phrasing"}, // dup
		{"type": "verbalReply", "text": "second reply"},
		{"type": "agentMessage", "text": "third reply"},
		{"type": "agentMessage", "text": "fourth reply"},
		{"type": "agentMessage", "text": "fifth reply"},
		{"type": "agentMessage", "text": "sixth reply -- should be dropped by max"},
	}
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	enc := json.NewEncoder(f)
	for _, ev := range events {
		if err := enc.Encode(ev); err != nil {
			t.Fatal(err)
		}
	}
	f.Close()

	needles, err := loadAgentTextNeedles(path, 5)
	if err != nil {
		t.Fatalf("loadAgentTextNeedles: %v", err)
	}
	if len(needles) != 5 {
		t.Fatalf("len(needles) = %d, want 5", len(needles))
	}
	if needles[0] != "first reply with distinctive phrasing" {
		t.Errorf("needles[0] = %q", needles[0])
	}
	// Dup must not appear twice.
	seen := map[string]int{}
	for _, n := range needles {
		seen[n]++
	}
	for n, c := range seen {
		if c != 1 {
			t.Errorf("needle %q appears %d times", n, c)
		}
	}
	// userMessage texts must NOT be needles.
	for _, n := range needles {
		if n == "user said something" {
			t.Error("userMessage text leaked into needles")
		}
	}
}

func TestFingerprintClaudeSessionByEvents_MatchesAcrossCandidates(t *testing.T) {
	// Set up a fake claude projects root and two candidate jsonls.
	claudeHome := t.TempDir()
	t.Setenv("CLAUDE_HOME", claudeHome)

	workDir := "/repos/test-project/workspace"
	encoded := encodeClaudeProjectDir(workDir)
	projectDir := filepath.Join(claudeHome, "projects", encoded)
	if err := os.MkdirAll(projectDir, 0755); err != nil {
		t.Fatal(err)
	}

	needleA := "this is a very specific assistant reply with unique wording"
	needleB := "and a second equally distinctive sentence from the agent"
	needleC := "third distinctive turn"
	needleD := "fourth distinctive turn"
	needleE := "fifth distinctive turn"
	allNeedles := []string{needleA, needleB, needleC, needleD, needleE}

	// Build chat events file containing all five.
	withTempRecordingsDir(t)
	sourceUUID := "ffff0001-0000-0000-0000-000000000001"
	chatUUID := "ffff0001-0000-0000-0000-000000000002"
	events := []map[string]any{}
	for _, n := range allNeedles {
		events = append(events, map[string]any{"type": "agentMessage", "text": n})
	}
	chatPath := writeEventsFile(t, sourceUUID, chatUUID, events)

	// Wrong candidate: contains only one needle.
	wrongID := "11111111-aaaa-aaaa-aaaa-111111111111"
	if err := os.WriteFile(filepath.Join(projectDir, wrongID+".jsonl"),
		[]byte("{\"type\":\"user\",\"message\":{\"content\":[{\"type\":\"text\",\"text\":\""+needleA+"\"}]}}\n"),
		0644); err != nil {
		t.Fatal(err)
	}
	// Right candidate: contains all needles.
	rightID := "22222222-bbbb-bbbb-bbbb-222222222222"
	var rightContent strings.Builder
	for _, n := range allNeedles {
		// Each line is a synthetic Claude event whose JSON contains the needle.
		rightContent.WriteString("{\"type\":\"assistant\",\"uuid\":\"x\",\"message\":{\"content\":[{\"type\":\"tool_use\",\"name\":\"mcp__swe-swe-agent-chat__send_message\",\"input\":{\"text\":\"" + n + "\"}}]}}\n")
	}
	if err := os.WriteFile(filepath.Join(projectDir, rightID+".jsonl"), []byte(rightContent.String()), 0644); err != nil {
		t.Fatal(err)
	}

	got, err := fingerprintClaudeSessionByEvents(workDir, chatPath)
	if err != nil {
		t.Fatalf("fingerprintClaudeSessionByEvents: %v", err)
	}
	if got != rightID {
		t.Errorf("matched %q, want %q", got, rightID)
	}
}

func TestFingerprintClaudeSessionByEvents_NoMatchIsError(t *testing.T) {
	claudeHome := t.TempDir()
	t.Setenv("CLAUDE_HOME", claudeHome)
	workDir := "/repos/test-project/workspace"
	encoded := encodeClaudeProjectDir(workDir)
	projectDir := filepath.Join(claudeHome, "projects", encoded)
	if err := os.MkdirAll(projectDir, 0755); err != nil {
		t.Fatal(err)
	}
	// Put an unrelated jsonl that doesn't contain the needles.
	if err := os.WriteFile(filepath.Join(projectDir, "decoy.jsonl"), []byte("{\"unrelated\":true}\n"), 0644); err != nil {
		t.Fatal(err)
	}

	withTempRecordingsDir(t)
	chatPath := writeEventsFile(t, "ffff0002-0000-0000-0000-000000000001", "ffff0002-0000-0000-0000-000000000002", []map[string]any{
		{"type": "agentMessage", "text": "needle text one"},
		{"type": "agentMessage", "text": "needle text two"},
		{"type": "agentMessage", "text": "needle text three"},
		{"type": "agentMessage", "text": "needle text four"},
		{"type": "agentMessage", "text": "needle text five"},
	})
	if _, err := fingerprintClaudeSessionByEvents(workDir, chatPath); err == nil {
		t.Fatal("expected error for no-match, got nil")
	}
}

func TestFingerprintClaudeSessionByEvents_NoAgentTextsIsError(t *testing.T) {
	claudeHome := t.TempDir()
	t.Setenv("CLAUDE_HOME", claudeHome)
	withTempRecordingsDir(t)
	// Events file has only userMessages -- no agentMessage/verbalReply.
	chatPath := writeEventsFile(t, "ffff0003-0000-0000-0000-000000000001", "ffff0003-0000-0000-0000-000000000002", []map[string]any{
		{"type": "userMessage", "text": "just a user message"},
	})
	if _, err := fingerprintClaudeSessionByEvents("/workspace", chatPath); err == nil {
		t.Fatal("expected error for events file with no agent texts, got nil")
	}
}
