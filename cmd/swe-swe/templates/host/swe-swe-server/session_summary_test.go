package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeRawEventsFile drops a JSONL events file where findChatEventsFile will
// find it. Takes raw lines rather than []map[string]any (as writeEventsFile in
// fork_legacy_test.go does) so a test can feed in a torn, unparseable line.
func writeRawEventsFile(t *testing.T, parentUUID string, lines ...string) {
	t.Helper()
	withTempRecordingsDir(t)

	path := filepath.Join(recordingsDir, "session-"+parentUUID+"-child.events.jsonl")
	body := strings.Join(lines, "\n") + "\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write events file: %v", err)
	}
}

// The homepage summary must survive agent-chat's bookkeeping events. A
// userMessagesConsumed record carries no text and lands immediately after the
// user's message, so trusting the literal last line rendered a bare "Agent:"
// and hid what the user had just said for as long as the agent stayed busy.
func TestSessionSummarySkipsTextlessBookkeepingEvents(t *testing.T) {
	writeRawEventsFile(t, "sess-1",
		`{"type":"agentMessage","text":"Earlier agent reply"}`,
		`{"type":"userMessage","text":"And question for homepage"}`,
		`{"type":"userMessagesConsumed","text":""}`,
	)

	line, status := getSessionSummaryFromChat("sess-1")
	if want := "You: And question for homepage"; line != want {
		t.Errorf("summary line = %q, want %q", line, want)
	}
	if status != "red" {
		t.Errorf("status = %q, want red (agent busy on an unanswered message)", status)
	}
}

// Several bookkeeping events in a row must not exhaust the search.
func TestSessionSummaryWalksBackPastSeveralEmptyEvents(t *testing.T) {
	writeRawEventsFile(t, "sess-2",
		`{"type":"agentMessage","text":"Committed 6c76dd197","quick_replies":["ok"]}`,
		`{"type":"userMessagesConsumed","text":""}`,
		`{"type":"agentProgress","text":""}`,
	)

	line, status := getSessionSummaryFromChat("sess-2")
	if want := "Agent: Committed 6c76dd197"; line != want {
		t.Errorf("summary line = %q, want %q", line, want)
	}
	if status != "green" {
		t.Errorf("status = %q, want green (quick replies offered)", status)
	}
}

// A progress event that DOES carry text is still the newest thing worth
// showing -- the skip is about emptiness, not about the event type.
func TestSessionSummaryKeepsProgressEventsWithText(t *testing.T) {
	writeRawEventsFile(t, "sess-3",
		`{"type":"userMessage","text":"go"}`,
		`{"type":"agentProgress","text":"Rebuilding golden files"}`,
	)

	line, status := getSessionSummaryFromChat("sess-3")
	if want := "Agent: Rebuilding golden files"; line != want {
		t.Errorf("summary line = %q, want %q", line, want)
	}
	if status != "red" {
		t.Errorf("status = %q, want red (still working)", status)
	}
}

// An events file with nothing renderable yields no summary rather than a
// half-built "Agent: " prefix.
func TestSessionSummaryEmptyWhenNothingRenderable(t *testing.T) {
	writeRawEventsFile(t, "sess-4",
		`{"type":"userMessagesConsumed","text":""}`,
		`{"type":"agentProgress","text":""}`,
	)

	line, status := getSessionSummaryFromChat("sess-4")
	if line != "" || status != "" {
		t.Errorf("summary = (%q, %q), want empty", line, status)
	}
}

// Unparseable trailing garbage (a torn write) must not mask the good event
// before it.
func TestSessionSummarySkipsUnparseableTail(t *testing.T) {
	writeRawEventsFile(t, "sess-5",
		`{"type":"userMessage","text":"still here"}`,
		`{"type":"agentMessa`,
	)

	line, _ := getSessionSummaryFromChat("sess-5")
	if want := "You: still here"; line != want {
		t.Errorf("summary line = %q, want %q", line, want)
	}
}
