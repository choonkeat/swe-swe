package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// testHelper provides utilities for recording API tests
type testHelper struct {
	t            *testing.T
	recordingDir string
	cleanup      func()
}

// newTestHelper creates a new test helper with a temp recordings directory
func newTestHelper(t *testing.T) *testHelper {
	t.Helper()

	// Create temp directory for recordings
	tmpDir := t.TempDir()
	testRecordingDir := filepath.Join(tmpDir, "recordings")
	if err := os.MkdirAll(testRecordingDir, 0755); err != nil {
		t.Fatalf("failed to create temp recordings dir: %v", err)
	}

	// Store original values
	origRecordingsDir := recordingsDir
	origSessions := make(map[string]*Session)
	sessionsMu.Lock()
	for k, v := range sessions {
		origSessions[k] = v
	}
	// Clear sessions for test isolation
	sessions = make(map[string]*Session)
	sessionsMu.Unlock()

	// Set test recordings directory (recordingsDir is now a var, not const)
	recordingsDir = testRecordingDir

	cleanup := func() {
		// Restore original state
		recordingsDir = origRecordingsDir
		sessionsMu.Lock()
		sessions = origSessions
		sessionsMu.Unlock()
	}

	t.Cleanup(cleanup)

	return &testHelper{
		t:            t,
		recordingDir: testRecordingDir,
		cleanup:      cleanup,
	}
}

// createRecordingFiles creates mock recording files in the test directory
func (h *testHelper) createRecordingFiles(uuid string, opts recordingOpts) {
	h.t.Helper()

	// Create .log file (required)
	logPath := filepath.Join(h.recordingDir, "session-"+uuid+".log")
	logContent := opts.logContent
	if logContent == "" {
		logContent = "test terminal output\r\n"
	}
	if err := os.WriteFile(logPath, []byte(logContent), 0644); err != nil {
		h.t.Fatalf("failed to create log file: %v", err)
	}

	// Create .timing file (optional)
	if opts.withTiming {
		timingPath := filepath.Join(h.recordingDir, "session-"+uuid+".timing")
		timingContent := opts.timingContent
		if timingContent == "" {
			timingContent = "0.1 5\n0.2 10\n"
		}
		if err := os.WriteFile(timingPath, []byte(timingContent), 0644); err != nil {
			h.t.Fatalf("failed to create timing file: %v", err)
		}
	}

	// Create .metadata.json file (optional)
	if opts.metadata != nil {
		metadataPath := filepath.Join(h.recordingDir, "session-"+uuid+".metadata.json")
		data, err := json.MarshalIndent(opts.metadata, "", "  ")
		if err != nil {
			h.t.Fatalf("failed to marshal metadata: %v", err)
		}
		if err := os.WriteFile(metadataPath, data, 0644); err != nil {
			h.t.Fatalf("failed to create metadata file: %v", err)
		}
	}
}

// recordingOpts specifies options for creating test recording files
type recordingOpts struct {
	logContent    string
	withTiming    bool
	timingContent string
	metadata      *RecordingMetadata
}

// createMockSession creates a mock session and adds it to the sessions map
func (h *testHelper) createMockSession(sessionUUID, recordingUUID string, processExited bool) *Session {
	h.t.Helper()

	sess := &Session{
		UUID:          sessionUUID,
		RecordingUUID: recordingUUID,
		Cmd:           &exec.Cmd{},
		wsClients:     make(map[*websocket.Conn]bool),
		wsClientSizes: make(map[*websocket.Conn]TermSize),
		CreatedAt:     time.Now(),
		lastActive:    time.Now(),
	}

	if processExited {
		// Simulate exited process by running a quick command and waiting for it
		sess.Cmd = exec.Command("true")
		sess.Cmd.Run() // This sets ProcessState to non-nil
	}

	sessionsMu.Lock()
	sessions[sessionUUID] = sess
	sessionsMu.Unlock()

	return sess
}

// fileExists checks if a file exists
func (h *testHelper) fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// recordingFileExists checks if a recording file exists
func (h *testHelper) recordingFileExists(uuid, suffix string) bool {
	path := filepath.Join(h.recordingDir, "session-"+uuid+suffix)
	return h.fileExists(path)
}

// ============================================================================
// Phase 1: Smoke Test - Verify test infrastructure works
// ============================================================================

func TestRecordingAPI_SmokeTest(t *testing.T) {
	h := newTestHelper(t)

	// Create a mock recording
	testUUID := "test-uuid-1234-5678-9abc-def012345678"
	startTime := time.Now().Add(-time.Hour)
	endTime := time.Now()
	h.createRecordingFiles(testUUID, recordingOpts{
		logContent: "Hello, World!\r\n",
		withTiming: true,
		metadata: &RecordingMetadata{
			UUID:      testUUID,
			Name:      "Test Recording",
			Agent:     "Claude",
			StartedAt: startTime,
			EndedAt:   &endTime,
		},
	})

	// Create test server
	mux := http.NewServeMux()
	mux.HandleFunc("/api/recording/", handleRecordingAPI)

	server := httptest.NewServer(mux)
	defer server.Close()

	// Call list endpoint
	resp, err := http.Get(server.URL + "/api/recording/list")
	if err != nil {
		t.Fatalf("failed to call list endpoint: %v", err)
	}
	defer resp.Body.Close()

	// Verify response
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}

	var result struct {
		Recordings []RecordingListItem `json:"recordings"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	// Verify our recording is in the list
	if len(result.Recordings) != 1 {
		t.Errorf("expected 1 recording, got %d", len(result.Recordings))
	}
	if len(result.Recordings) > 0 {
		rec := result.Recordings[0]
		if rec.UUID != testUUID {
			t.Errorf("expected UUID %s, got %s", testUUID, rec.UUID)
		}
		if rec.Name != "Test Recording" {
			t.Errorf("expected name 'Test Recording', got %s", rec.Name)
		}
		if rec.Agent != "Claude" {
			t.Errorf("expected agent 'Claude', got %s", rec.Agent)
		}
		if !rec.HasTiming {
			t.Error("expected HasTiming to be true")
		}
	}
}
