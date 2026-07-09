package main

import (
	"os"
	"path/filepath"
	"testing"
)

// TestSessionGoneRecordings verifies that the "session has ended" screen can
// discover which playback artifacts exist on disk for an ended session. Both
// viewers are keyed by the session UUID (the same key fork uses), so the
// helper only needs the source UUID to report chat/terminal availability.
func TestSessionGoneRecordings(t *testing.T) {
	h := newTestHelper(t)

	// Real UUIDs (findChatEventsFile / the /recording routes parse them).
	const (
		bothUUID     = "11111111-1111-1111-1111-111111111111"
		terminalUUID = "22222222-2222-2222-2222-222222222222"
		chatUUID     = "33333333-3333-3333-3333-333333333333"
		noneUUID     = "44444444-4444-4444-4444-444444444444"
	)

	writeChat := func(parentUUID string) {
		// Chat events live at session-<parent>-<child>.events.jsonl.
		p := filepath.Join(h.recordingDir, "session-"+parentUUID+"-child.events.jsonl")
		if err := os.WriteFile(p, []byte("{}\n"), 0644); err != nil {
			t.Fatalf("write chat events: %v", err)
		}
	}

	// bothUUID: terminal log + chat events.
	h.createRecordingFiles(bothUUID, recordingOpts{})
	writeChat(bothUUID)
	// terminalUUID: terminal log only.
	h.createRecordingFiles(terminalUUID, recordingOpts{})
	// chatUUID: chat events only.
	writeChat(chatUUID)
	// noneUUID: nothing on disk.

	tests := []struct {
		name             string
		uuid             string
		wantChat         bool
		wantTerminal     bool
	}{
		{"both artifacts", bothUUID, true, true},
		{"terminal only", terminalUUID, false, true},
		{"chat only", chatUUID, true, false},
		{"no artifacts", noneUUID, false, false},
		{"invalid uuid", "not-a-uuid", false, false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			hasChat, hasTerminal := sessionGoneRecordings(tc.uuid)
			if hasChat != tc.wantChat {
				t.Errorf("hasChat = %v, want %v", hasChat, tc.wantChat)
			}
			if hasTerminal != tc.wantTerminal {
				t.Errorf("hasTerminal = %v, want %v", hasTerminal, tc.wantTerminal)
			}
		})
	}
}
