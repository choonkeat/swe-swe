package main

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
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
	if opts.logMtime != nil {
		if err := os.Chtimes(logPath, *opts.logMtime, *opts.logMtime); err != nil {
			h.t.Fatalf("failed to set log file mtime: %v", err)
		}
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
	logMtime      *time.Time // if set, the log file's mtime is adjusted after creation
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
		wsClients:     make(map[*SafeConn]bool),
		wsClientSizes: make(map[*SafeConn]TermSize),
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
// Test server helper
// ============================================================================

func (h *testHelper) createTestServer() *httptest.Server {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/recording/", handleRecordingAPI)
	mux.HandleFunc("/recording/", func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/recording/")
		if path == "" {
			http.Redirect(w, r, "/", http.StatusFound)
			return
		}

		// Serve raw session.log for streaming
		if strings.HasSuffix(path, "/session.log") {
			recordingUUID := strings.TrimSuffix(path, "/session.log")
			handleRecordingSessionLog(w, r, recordingUUID)
			return
		}

		// Serve streaming HTML page
		handleRecordingPage(w, r, path)
	})
	return httptest.NewServer(mux)
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

// ============================================================================
// Phase 2: Recording List API Tests (GET /api/recording/list)
// ============================================================================

func TestListRecordings_EmptyDirectory(t *testing.T) {
	h := newTestHelper(t)
	server := h.createTestServer()
	defer server.Close()

	resp, err := http.Get(server.URL + "/api/recording/list")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}

	var result struct {
		Recordings []RecordingListItem `json:"recordings"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if len(result.Recordings) != 0 {
		t.Errorf("expected 0 recordings, got %d", len(result.Recordings))
	}
}

func TestListRecordings_DirectoryMissing(t *testing.T) {
	h := newTestHelper(t)

	// Remove the recordings directory
	os.RemoveAll(h.recordingDir)

	server := h.createTestServer()
	defer server.Close()

	resp, err := http.Get(server.URL + "/api/recording/list")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200 (empty list), got %d", resp.StatusCode)
	}

	var result struct {
		Recordings []RecordingListItem `json:"recordings"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if len(result.Recordings) != 0 {
		t.Errorf("expected 0 recordings, got %d", len(result.Recordings))
	}
}

func TestListRecordings_WithMetadata(t *testing.T) {
	h := newTestHelper(t)

	testUUID := "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"
	startTime := time.Date(2026, 1, 7, 10, 0, 0, 0, time.UTC)
	endTime := time.Date(2026, 1, 7, 11, 0, 0, 0, time.UTC)

	h.createRecordingFiles(testUUID, recordingOpts{
		metadata: &RecordingMetadata{
			UUID:      testUUID,
			Name:      "My Test Session",
			Agent:     "Claude",
			StartedAt: startTime,
			EndedAt:   &endTime,
		},
	})

	server := h.createTestServer()
	defer server.Close()

	resp, err := http.Get(server.URL + "/api/recording/list")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	var result struct {
		Recordings []RecordingListItem `json:"recordings"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if len(result.Recordings) != 1 {
		t.Fatalf("expected 1 recording, got %d", len(result.Recordings))
	}

	rec := result.Recordings[0]
	if rec.UUID != testUUID {
		t.Errorf("expected UUID %s, got %s", testUUID, rec.UUID)
	}
	if rec.Name != "My Test Session" {
		t.Errorf("expected name 'My Test Session', got %s", rec.Name)
	}
	if rec.Agent != "Claude" {
		t.Errorf("expected agent 'Claude', got %s", rec.Agent)
	}
	if rec.StartedAt == nil {
		t.Error("expected StartedAt to be set")
	}
	if rec.EndedAt == nil {
		t.Error("expected EndedAt to be set")
	}
}

func TestListRecordings_WithoutMetadata(t *testing.T) {
	h := newTestHelper(t)

	testUUID := "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"
	h.createRecordingFiles(testUUID, recordingOpts{
		logContent: "test content",
		// No metadata
	})

	server := h.createTestServer()
	defer server.Close()

	resp, err := http.Get(server.URL + "/api/recording/list")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	var result struct {
		Recordings []RecordingListItem `json:"recordings"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if len(result.Recordings) != 1 {
		t.Fatalf("expected 1 recording, got %d", len(result.Recordings))
	}

	rec := result.Recordings[0]
	if rec.UUID != testUUID {
		t.Errorf("expected UUID %s, got %s", testUUID, rec.UUID)
	}
	if rec.Name != "" {
		t.Errorf("expected empty name, got %s", rec.Name)
	}
	if rec.SizeBytes <= 0 {
		t.Error("expected SizeBytes > 0")
	}
}

func TestListRecordings_WithTimingFile(t *testing.T) {
	h := newTestHelper(t)

	testUUID := "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"
	h.createRecordingFiles(testUUID, recordingOpts{
		withTiming: true,
	})

	server := h.createTestServer()
	defer server.Close()

	resp, err := http.Get(server.URL + "/api/recording/list")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	var result struct {
		Recordings []RecordingListItem `json:"recordings"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if len(result.Recordings) != 1 {
		t.Fatalf("expected 1 recording, got %d", len(result.Recordings))
	}

	if !result.Recordings[0].HasTiming {
		t.Error("expected HasTiming to be true")
	}
}

func TestListRecordings_WithoutTimingFile(t *testing.T) {
	h := newTestHelper(t)

	testUUID := "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"
	h.createRecordingFiles(testUUID, recordingOpts{
		withTiming: false,
	})

	server := h.createTestServer()
	defer server.Close()

	resp, err := http.Get(server.URL + "/api/recording/list")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	var result struct {
		Recordings []RecordingListItem `json:"recordings"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if len(result.Recordings) != 1 {
		t.Fatalf("expected 1 recording, got %d", len(result.Recordings))
	}

	if result.Recordings[0].HasTiming {
		t.Error("expected HasTiming to be false")
	}
}

func TestListRecordings_ActiveRecording_ProcessRunning(t *testing.T) {
	h := newTestHelper(t)

	recordingUUID := "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"
	sessionUUID := "session-1234"

	// Create recording files
	h.createRecordingFiles(recordingUUID, recordingOpts{})

	// Create session with RUNNING process (ProcessState = nil)
	h.createMockSession(sessionUUID, recordingUUID, false) // processExited=false

	server := h.createTestServer()
	defer server.Close()

	resp, err := http.Get(server.URL + "/api/recording/list")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	var result struct {
		Recordings []RecordingListItem `json:"recordings"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if len(result.Recordings) != 1 {
		t.Fatalf("expected 1 recording, got %d", len(result.Recordings))
	}

	if !result.Recordings[0].IsActive {
		t.Error("expected IsActive to be true for running process")
	}
}

func TestListRecordings_EndedRecording_ProcessExited(t *testing.T) {
	h := newTestHelper(t)

	recordingUUID := "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"
	sessionUUID := "session-1234"

	// Create recording files
	h.createRecordingFiles(recordingUUID, recordingOpts{})

	// Create session with EXITED process (ProcessState != nil)
	// This is the bug scenario we fixed - session still in map but process exited
	h.createMockSession(sessionUUID, recordingUUID, true) // processExited=true

	server := h.createTestServer()
	defer server.Close()

	resp, err := http.Get(server.URL + "/api/recording/list")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	var result struct {
		Recordings []RecordingListItem `json:"recordings"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if len(result.Recordings) != 1 {
		t.Fatalf("expected 1 recording, got %d", len(result.Recordings))
	}

	if result.Recordings[0].IsActive {
		t.Error("expected IsActive to be false for exited process")
	}
}

func TestListRecordings_MultipleRecordingsSortedByDate(t *testing.T) {
	h := newTestHelper(t)

	// Create recordings with different dates
	oldUUID := "old-uuid-1234-5678-9abc-def012345678"
	newUUID := "new-uuid-1234-5678-9abc-def012345678"

	oldTime := time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)
	newTime := time.Date(2026, 1, 7, 10, 0, 0, 0, time.UTC)

	h.createRecordingFiles(oldUUID, recordingOpts{
		metadata: &RecordingMetadata{
			UUID:      oldUUID,
			Name:      "Old Recording",
			StartedAt: oldTime,
		},
	})

	h.createRecordingFiles(newUUID, recordingOpts{
		metadata: &RecordingMetadata{
			UUID:      newUUID,
			Name:      "New Recording",
			StartedAt: newTime,
		},
	})

	server := h.createTestServer()
	defer server.Close()

	resp, err := http.Get(server.URL + "/api/recording/list")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	var result struct {
		Recordings []RecordingListItem `json:"recordings"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if len(result.Recordings) != 2 {
		t.Fatalf("expected 2 recordings, got %d", len(result.Recordings))
	}

	// Newest first
	if result.Recordings[0].Name != "New Recording" {
		t.Errorf("expected first recording to be 'New Recording', got %s", result.Recordings[0].Name)
	}
	if result.Recordings[1].Name != "Old Recording" {
		t.Errorf("expected second recording to be 'Old Recording', got %s", result.Recordings[1].Name)
	}
}

// ============================================================================
// Phase 3: Recording Delete API Tests (DELETE /api/recording/{uuid})
// ============================================================================

func TestDeleteRecording_NotFound(t *testing.T) {
	h := newTestHelper(t)
	server := h.createTestServer()
	defer server.Close()

	// Use valid UUID format that doesn't exist
	nonExistentUUID := "00000000-0000-0000-0000-000000000000"
	req, _ := http.NewRequest(http.MethodDelete, server.URL+"/api/recording/"+nonExistentUUID, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("expected status 404, got %d", resp.StatusCode)
	}
}

func TestDeleteRecording_InvalidUUID(t *testing.T) {
	h := newTestHelper(t)
	server := h.createTestServer()
	defer server.Close()

	// Invalid UUID format should return 400
	req, _ := http.NewRequest(http.MethodDelete, server.URL+"/api/recording/not-a-valid-uuid", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("expected status 400 for invalid UUID, got %d", resp.StatusCode)
	}
}

func TestDeleteRecording_NoSession(t *testing.T) {
	h := newTestHelper(t)

	testUUID := "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"
	h.createRecordingFiles(testUUID, recordingOpts{
		withTiming: true,
		metadata: &RecordingMetadata{
			UUID: testUUID,
			Name: "Test",
		},
	})

	server := h.createTestServer()
	defer server.Close()

	// Verify files exist before delete
	if !h.recordingFileExists(testUUID, ".log") {
		t.Fatal("log file should exist before delete")
	}
	if !h.recordingFileExists(testUUID, ".timing") {
		t.Fatal("timing file should exist before delete")
	}
	if !h.recordingFileExists(testUUID, ".metadata.json") {
		t.Fatal("metadata file should exist before delete")
	}

	req, _ := http.NewRequest(http.MethodDelete, server.URL+"/api/recording/"+testUUID, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNoContent {
		t.Errorf("expected status 204, got %d", resp.StatusCode)
	}

	// Verify files deleted
	if h.recordingFileExists(testUUID, ".log") {
		t.Error("log file should be deleted")
	}
	if h.recordingFileExists(testUUID, ".timing") {
		t.Error("timing file should be deleted")
	}
	if h.recordingFileExists(testUUID, ".metadata.json") {
		t.Error("metadata file should be deleted")
	}
}

func TestDeleteRecording_ActiveSession_ProcessRunning(t *testing.T) {
	h := newTestHelper(t)

	recordingUUID := "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"
	sessionUUID := "session-1234"

	h.createRecordingFiles(recordingUUID, recordingOpts{})

	// Create session with RUNNING process (ProcessState = nil)
	h.createMockSession(sessionUUID, recordingUUID, false) // processExited=false

	server := h.createTestServer()
	defer server.Close()

	req, _ := http.NewRequest(http.MethodDelete, server.URL+"/api/recording/"+recordingUUID, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusConflict {
		t.Errorf("expected status 409 Conflict, got %d", resp.StatusCode)
	}

	// Verify files NOT deleted
	if !h.recordingFileExists(recordingUUID, ".log") {
		t.Error("log file should NOT be deleted for active session")
	}
}

func TestDeleteRecording_EndedSession_ProcessExited(t *testing.T) {
	h := newTestHelper(t)

	recordingUUID := "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"
	sessionUUID := "session-1234"

	h.createRecordingFiles(recordingUUID, recordingOpts{})

	// Create session with EXITED process (ProcessState != nil)
	// This is the exact bug scenario we fixed
	h.createMockSession(sessionUUID, recordingUUID, true) // processExited=true

	server := h.createTestServer()
	defer server.Close()

	req, _ := http.NewRequest(http.MethodDelete, server.URL+"/api/recording/"+recordingUUID, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNoContent {
		t.Errorf("expected status 204 for exited process, got %d", resp.StatusCode)
	}

	// Verify files deleted
	if h.recordingFileExists(recordingUUID, ".log") {
		t.Error("log file should be deleted for exited process")
	}
}

func TestDeleteRecording_RemovesAllFiles(t *testing.T) {
	h := newTestHelper(t)

	testUUID := "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"
	h.createRecordingFiles(testUUID, recordingOpts{
		withTiming: true,
		metadata: &RecordingMetadata{
			UUID: testUUID,
		},
	})

	server := h.createTestServer()
	defer server.Close()

	req, _ := http.NewRequest(http.MethodDelete, server.URL+"/api/recording/"+testUUID, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNoContent {
		t.Errorf("expected status 204, got %d", resp.StatusCode)
	}

	// Verify ALL files deleted
	if h.recordingFileExists(testUUID, ".log") {
		t.Error(".log file should be deleted")
	}
	if h.recordingFileExists(testUUID, ".timing") {
		t.Error(".timing file should be deleted")
	}
	if h.recordingFileExists(testUUID, ".metadata.json") {
		t.Error(".metadata.json file should be deleted")
	}
}

func TestDeleteRecording_OnlyLogFile(t *testing.T) {
	h := newTestHelper(t)

	testUUID := "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"
	h.createRecordingFiles(testUUID, recordingOpts{
		withTiming: false,
		// No metadata
	})

	server := h.createTestServer()
	defer server.Close()

	req, _ := http.NewRequest(http.MethodDelete, server.URL+"/api/recording/"+testUUID, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	// Should succeed even with only .log file
	if resp.StatusCode != http.StatusNoContent {
		t.Errorf("expected status 204, got %d", resp.StatusCode)
	}

	if h.recordingFileExists(testUUID, ".log") {
		t.Error(".log file should be deleted")
	}
}

func TestDeleteRecording_WrongMethod_GET(t *testing.T) {
	h := newTestHelper(t)

	testUUID := "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"
	h.createRecordingFiles(testUUID, recordingOpts{})

	server := h.createTestServer()
	defer server.Close()

	// GET should not delete
	resp, err := http.Get(server.URL + "/api/recording/" + testUUID)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	// Should be 404 or 405 (method not allowed)
	if resp.StatusCode == http.StatusNoContent {
		t.Error("GET should not delete recording")
	}

	// Verify file still exists
	if !h.recordingFileExists(testUUID, ".log") {
		t.Error("log file should NOT be deleted by GET request")
	}
}

// ============================================================================
// Phase 4: Recording Download API Tests (GET /api/recording/{uuid}/download)
// ============================================================================

func TestDownloadRecording_NotFound(t *testing.T) {
	h := newTestHelper(t)
	server := h.createTestServer()
	defer server.Close()

	// Use valid UUID format that doesn't exist
	nonExistentUUID := "00000000-0000-0000-0000-000000000000"
	resp, err := http.Get(server.URL + "/api/recording/" + nonExistentUUID + "/download")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("expected status 404, got %d", resp.StatusCode)
	}
}

func TestDownloadRecording_InvalidUUID(t *testing.T) {
	h := newTestHelper(t)
	server := h.createTestServer()
	defer server.Close()

	resp, err := http.Get(server.URL + "/api/recording/not-a-valid-uuid/download")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", resp.StatusCode)
	}
}

func TestDownloadRecording_AllFiles(t *testing.T) {
	h := newTestHelper(t)

	testUUID := "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"
	logContent := "terminal output content"
	timingContent := "0.1 5\n0.2 10\n"

	h.createRecordingFiles(testUUID, recordingOpts{
		logContent:    logContent,
		withTiming:    true,
		timingContent: timingContent,
		metadata: &RecordingMetadata{
			UUID: testUUID,
			Name: "Test Recording",
		},
	})

	server := h.createTestServer()
	defer server.Close()

	resp, err := http.Get(server.URL + "/api/recording/" + testUUID + "/download")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}

	// Check Content-Type header
	contentType := resp.Header.Get("Content-Type")
	if contentType != "application/zip" {
		t.Errorf("expected Content-Type 'application/zip', got '%s'", contentType)
	}

	// Check Content-Disposition header
	contentDisp := resp.Header.Get("Content-Disposition")
	if !strings.HasPrefix(contentDisp, "attachment; filename=") {
		t.Errorf("expected Content-Disposition with attachment, got '%s'", contentDisp)
	}
	if !strings.Contains(contentDisp, testUUID[:8]) {
		t.Errorf("expected Content-Disposition to contain UUID prefix, got '%s'", contentDisp)
	}

	// Read zip content
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("failed to read response body: %v", err)
	}

	// Open zip
	zipReader, err := zip.NewReader(bytes.NewReader(body), int64(len(body)))
	if err != nil {
		t.Fatalf("failed to open zip: %v", err)
	}

	// Verify zip contains expected files
	fileNames := make(map[string]bool)
	fileContents := make(map[string]string)
	for _, f := range zipReader.File {
		fileNames[f.Name] = true
		rc, err := f.Open()
		if err != nil {
			t.Fatalf("failed to open file in zip: %v", err)
		}
		content, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			t.Fatalf("failed to read file in zip: %v", err)
		}
		fileContents[f.Name] = string(content)
	}

	expectedFiles := []string{"session.log", "session.timing", "session.metadata.json"}
	for _, name := range expectedFiles {
		if !fileNames[name] {
			t.Errorf("expected zip to contain '%s'", name)
		}
	}

	// Verify log content matches
	if fileContents["session.log"] != logContent {
		t.Errorf("log content mismatch: expected '%s', got '%s'", logContent, fileContents["session.log"])
	}

	// Verify timing content matches
	if fileContents["session.timing"] != timingContent {
		t.Errorf("timing content mismatch: expected '%s', got '%s'", timingContent, fileContents["session.timing"])
	}
}

func TestDownloadRecording_OnlyLogFile(t *testing.T) {
	h := newTestHelper(t)

	testUUID := "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"
	logContent := "only log content"

	h.createRecordingFiles(testUUID, recordingOpts{
		logContent: logContent,
		withTiming: false,
		// No metadata
	})

	server := h.createTestServer()
	defer server.Close()

	resp, err := http.Get(server.URL + "/api/recording/" + testUUID + "/download")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("failed to read response body: %v", err)
	}

	zipReader, err := zip.NewReader(bytes.NewReader(body), int64(len(body)))
	if err != nil {
		t.Fatalf("failed to open zip: %v", err)
	}

	// Verify only log file in zip
	if len(zipReader.File) != 1 {
		t.Errorf("expected 1 file in zip, got %d", len(zipReader.File))
	}

	if len(zipReader.File) > 0 && zipReader.File[0].Name != "session.log" {
		t.Errorf("expected 'session.log', got '%s'", zipReader.File[0].Name)
	}
}

func TestDownloadRecording_LogAndTimingOnly(t *testing.T) {
	h := newTestHelper(t)

	testUUID := "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"

	h.createRecordingFiles(testUUID, recordingOpts{
		withTiming: true,
		// No metadata
	})

	server := h.createTestServer()
	defer server.Close()

	resp, err := http.Get(server.URL + "/api/recording/" + testUUID + "/download")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("failed to read response body: %v", err)
	}

	zipReader, err := zip.NewReader(bytes.NewReader(body), int64(len(body)))
	if err != nil {
		t.Fatalf("failed to open zip: %v", err)
	}

	// Verify 2 files in zip
	if len(zipReader.File) != 2 {
		t.Errorf("expected 2 files in zip, got %d", len(zipReader.File))
	}

	fileNames := make(map[string]bool)
	for _, f := range zipReader.File {
		fileNames[f.Name] = true
	}

	if !fileNames["session.log"] {
		t.Error("expected zip to contain 'session.log'")
	}
	if !fileNames["session.timing"] {
		t.Error("expected zip to contain 'session.timing'")
	}
	if fileNames["session.metadata.json"] {
		t.Error("expected zip NOT to contain 'session.metadata.json'")
	}
}

// ============================================================================
// Phase 5: Recording Playback Page Tests (GET /recording/{uuid})
// ============================================================================

func TestPlaybackPage_NotFound(t *testing.T) {
	h := newTestHelper(t)
	server := h.createTestServer()
	defer server.Close()

	// Use valid UUID format that doesn't exist
	nonExistentUUID := "00000000-0000-0000-0000-000000000000"
	resp, err := http.Get(server.URL + "/recording/" + nonExistentUUID)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("expected status 404, got %d", resp.StatusCode)
	}
}

func TestPlaybackPage_InvalidUUID(t *testing.T) {
	h := newTestHelper(t)
	server := h.createTestServer()
	defer server.Close()

	resp, err := http.Get(server.URL + "/recording/not-a-valid-uuid")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", resp.StatusCode)
	}
}

func TestPlaybackPage_StreamingHTML_Default(t *testing.T) {
	h := newTestHelper(t)

	testUUID := "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"
	h.createRecordingFiles(testUUID, recordingOpts{
		logContent:    "Hello\r\nWorld\r\n",
		withTiming:    true,
		timingContent: "0.1 5\n0.5 6\n",
		metadata: &RecordingMetadata{
			UUID:    testUUID,
			Name:    "Test Recording",
			MaxCols: 80,
			MaxRows: 24,
		},
	})

	server := h.createTestServer()
	defer server.Close()

	// Default (no query param) should use streaming HTML
	resp, err := http.Get(server.URL + "/recording/" + testUUID)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}

	// Check Content-Type header
	contentType := resp.Header.Get("Content-Type")
	if !strings.Contains(contentType, "text/html") {
		t.Errorf("expected Content-Type 'text/html', got '%s'", contentType)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("failed to read response body: %v", err)
	}
	html := string(body)

	// Should use xterm.js for rendering
	if !strings.Contains(html, "xterm") {
		t.Error("expected playback page to use xterm.js")
	}

	// Should have proper HTML structure
	if !strings.Contains(html, "<!DOCTYPE html>") {
		t.Error("expected valid HTML document")
	}

	// Should have record-tui footer
	if !strings.Contains(html, "record-tui") {
		t.Error("expected page to contain record-tui reference")
	}

	// Streaming HTML should fetch data via fetch(), not embed it
	if !strings.Contains(html, "fetch") {
		t.Error("expected streaming HTML to use fetch() for data loading")
	}
}

func TestPlaybackPage_EmbeddedHTML_WithQueryParam(t *testing.T) {
	h := newTestHelper(t)

	testUUID := "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"
	h.createRecordingFiles(testUUID, recordingOpts{
		logContent:    "Hello\r\nWorld\r\n",
		withTiming:    true,
		timingContent: "0.1 5\n0.5 6\n",
		metadata: &RecordingMetadata{
			UUID:    testUUID,
			Name:    "Test Recording",
			MaxCols: 80,
			MaxRows: 24,
		},
	})

	server := h.createTestServer()
	defer server.Close()

	// ?render=embedded should use embedded HTML
	resp, err := http.Get(server.URL + "/recording/" + testUUID + "?render=embedded")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("failed to read response body: %v", err)
	}
	html := string(body)

	// Should use xterm.js for rendering
	if !strings.Contains(html, "xterm") {
		t.Error("expected playback page to use xterm.js")
	}

	// Embedded HTML should have base64-encoded frame data (framesBase64)
	if !strings.Contains(html, "framesBase64") {
		t.Error("expected embedded HTML to have framesBase64 data")
	}
}

func TestPlaybackPage_SessionLogEndpoint(t *testing.T) {
	h := newTestHelper(t)

	testUUID := "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"
	// Content with distinctive first and last parts to verify ENTIRE content is served
	logContent := "FIRST_LINE\r\n...middle content...\r\nLAST_LINE\r\n"
	h.createRecordingFiles(testUUID, recordingOpts{
		logContent: logContent,
		withTiming: false,
		metadata: &RecordingMetadata{
			UUID: testUUID,
			Name: "Static Recording",
		},
	})

	server := h.createTestServer()
	defer server.Close()

	// Test the session.log endpoint that streaming HTML fetches from
	resp, err := http.Get(server.URL + "/recording/" + testUUID + "/session.log")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("failed to read response body: %v", err)
	}
	content := string(body)

	// CRITICAL: Verify both first AND last parts are present
	// This confirms we're serving the entire recording
	if !strings.Contains(content, "FIRST_LINE") {
		t.Error("expected session.log to contain FIRST_LINE")
	}
	if !strings.Contains(content, "LAST_LINE") {
		t.Error("expected session.log to contain LAST_LINE - should serve ALL content")
	}
	if !strings.Contains(content, "middle content") {
		t.Error("expected session.log to contain middle content")
	}
}

func TestPlaybackPage_SessionLogEndpoint_NotFound(t *testing.T) {
	h := newTestHelper(t)
	_ = h // Just set up the test helper

	server := h.createTestServer()
	defer server.Close()

	// Test session.log for non-existent recording (use valid UUID format that doesn't exist)
	resp, err := http.Get(server.URL + "/recording/00000000-0000-0000-0000-000000000000/session.log")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("expected status 404, got %d", resp.StatusCode)
	}
}

func TestPlaybackPage_SessionLogEndpoint_InvalidUUID(t *testing.T) {
	h := newTestHelper(t)
	_ = h // Just set up the test helper

	server := h.createTestServer()
	defer server.Close()

	// Test session.log with invalid UUID (too short)
	resp, err := http.Get(server.URL + "/recording/short/session.log")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", resp.StatusCode)
	}
}

func TestPlaybackPage_WithMetadata_ShowsName(t *testing.T) {
	h := newTestHelper(t)

	testUUID := "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"
	h.createRecordingFiles(testUUID, recordingOpts{
		withTiming: true,
		metadata: &RecordingMetadata{
			UUID: testUUID,
			Name: "My Named Session",
		},
	})

	server := h.createTestServer()
	defer server.Close()

	resp, err := http.Get(server.URL + "/recording/" + testUUID)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("failed to read response body: %v", err)
	}
	html := string(body)

	// Title should contain the session name
	if !strings.Contains(html, "My Named Session") {
		t.Error("expected page title to contain session name")
	}
}

func TestPlaybackPage_WithoutMetadata_ShowsUUIDShort(t *testing.T) {
	h := newTestHelper(t)

	testUUID := "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"
	h.createRecordingFiles(testUUID, recordingOpts{
		withTiming: true,
		// No metadata - should fall back to showing UUID prefix
	})

	server := h.createTestServer()
	defer server.Close()

	resp, err := http.Get(server.URL + "/recording/" + testUUID)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("failed to read response body: %v", err)
	}
	html := string(body)

	// Should contain UUID prefix (first 8 chars) or "session-{uuid8}"
	uuidPrefix := testUUID[:8]
	if !strings.Contains(html, uuidPrefix) && !strings.Contains(html, "session-"+uuidPrefix) {
		t.Errorf("expected page to contain UUID prefix '%s'", uuidPrefix)
	}
}

// TestPlaybackPage_BackLink removed - record-tui's static HTML doesn't include navigation

// ============================================================================
// Phase 6: Homepage Recording Display Tests (loadEndedRecordings, loadEndedRecordingsByAgent)
// ============================================================================

func TestLoadEndedRecordings_EmptyDirectory(t *testing.T) {
	h := newTestHelper(t)
	_ = h // Just initialize the test helper to set up recordings dir

	recordings := loadEndedRecordings()

	if len(recordings) != 0 {
		t.Errorf("expected 0 recordings, got %d", len(recordings))
	}
}

func TestLoadEndedRecordings_DirectoryMissing(t *testing.T) {
	h := newTestHelper(t)

	// Remove the recordings directory
	os.RemoveAll(h.recordingDir)

	recordings := loadEndedRecordings()

	// Should return nil, not panic
	if recordings != nil && len(recordings) != 0 {
		t.Errorf("expected nil or empty slice, got %d recordings", len(recordings))
	}
}

func TestLoadEndedRecordings_ExcludesActiveRecordings(t *testing.T) {
	h := newTestHelper(t)

	recordingUUID := "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"
	sessionUUID := "session-1234"

	h.createRecordingFiles(recordingUUID, recordingOpts{})

	// Create session with RUNNING process (ProcessState = nil)
	h.createMockSession(sessionUUID, recordingUUID, false) // processExited=false

	recordings := loadEndedRecordings()

	// Should NOT include active recording
	for _, rec := range recordings {
		if rec.UUID == recordingUUID {
			t.Error("expected active recording to be excluded from loadEndedRecordings")
		}
	}
}

func TestLoadEndedRecordings_IncludesEndedRecordings(t *testing.T) {
	h := newTestHelper(t)

	recordingUUID := "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"
	sessionUUID := "session-1234"

	h.createRecordingFiles(recordingUUID, recordingOpts{
		metadata: &RecordingMetadata{
			UUID: recordingUUID,
			Name: "Ended Recording",
		},
	})

	// Create session with EXITED process (ProcessState != nil)
	// This is the bug scenario we fixed - session still in map but process exited
	h.createMockSession(sessionUUID, recordingUUID, true) // processExited=true

	recordings := loadEndedRecordings()

	found := false
	for _, rec := range recordings {
		if rec.UUID == recordingUUID {
			found = true
			break
		}
	}

	if !found {
		t.Error("expected ended recording (process exited) to be included in loadEndedRecordings")
	}
}

func TestLoadEndedRecordings_IncludesOrphanRecordings(t *testing.T) {
	h := newTestHelper(t)

	recordingUUID := "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"

	// Create recording files but NO session in map (orphan)
	h.createRecordingFiles(recordingUUID, recordingOpts{
		metadata: &RecordingMetadata{
			UUID: recordingUUID,
			Name: "Orphan Recording",
		},
	})

	recordings := loadEndedRecordings()

	found := false
	for _, rec := range recordings {
		if rec.UUID == recordingUUID {
			found = true
			break
		}
	}

	if !found {
		t.Error("expected orphan recording (no session) to be included in loadEndedRecordings")
	}
}

func TestLoadEndedRecordings_EndedAgoFromMetadata(t *testing.T) {
	h := newTestHelper(t)

	recordingUUID := "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"
	endTime := time.Now().Add(-time.Hour)

	h.createRecordingFiles(recordingUUID, recordingOpts{
		metadata: &RecordingMetadata{
			UUID:    recordingUUID,
			EndedAt: &endTime,
		},
	})

	recordings := loadEndedRecordings()

	if len(recordings) != 1 {
		t.Fatalf("expected 1 recording, got %d", len(recordings))
	}

	// EndedAgo should be populated from metadata.ended_at
	if recordings[0].EndedAgo == "" {
		t.Error("expected EndedAgo to be populated from metadata")
	}
}

func TestLoadEndedRecordings_SortsByEndedAtDesc(t *testing.T) {
	h := newTestHelper(t)

	oldUUID := "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"
	newUUID := "bbbbbbbb-cccc-dddd-eeee-ffffffffffff"

	oldEndedAt := time.Now().Add(-2 * time.Hour)
	newEndedAt := time.Now().Add(-30 * time.Minute)

	h.createRecordingFiles(oldUUID, recordingOpts{
		metadata: &RecordingMetadata{
			UUID:    oldUUID,
			Name:    "Old Session",
			Agent:   "Claude",
			EndedAt: &oldEndedAt,
		},
	})

	h.createRecordingFiles(newUUID, recordingOpts{
		metadata: &RecordingMetadata{
			UUID:    newUUID,
			Name:    "New Session",
			Agent:   "Claude",
			EndedAt: &newEndedAt,
		},
	})

	recordings := loadEndedRecordings()
	if len(recordings) != 2 {
		t.Fatalf("expected 2 recordings, got %d", len(recordings))
	}
	if recordings[0].UUID != newUUID {
		t.Errorf("expected newest recording first, got %s", recordings[0].UUID)
	}
	if recordings[1].UUID != oldUUID {
		t.Errorf("expected oldest recording second, got %s", recordings[1].UUID)
	}
}

func TestLoadEndedRecordingsByAgent_GroupsByAgent(t *testing.T) {
	h := newTestHelper(t)

	// Create recordings for different agents
	claudeUUID := "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"
	geminiUUID := "bbbbbbbb-bbbb-cccc-dddd-eeeeeeeeeeee"

	h.createRecordingFiles(claudeUUID, recordingOpts{
		metadata: &RecordingMetadata{
			UUID:  claudeUUID,
			Name:  "Claude Session",
			Agent: "Claude",
		},
	})

	h.createRecordingFiles(geminiUUID, recordingOpts{
		metadata: &RecordingMetadata{
			UUID:  geminiUUID,
			Name:  "Gemini Session",
			Agent: "Gemini",
		},
	})

	recordings := loadEndedRecordings()
	grouped := loadEndedRecordingsByAgent(recordings)

	// Should have entries for both agents
	claudeRecordings := grouped["claude"]
	geminiRecordings := grouped["gemini"]

	if len(claudeRecordings) != 1 {
		t.Errorf("expected 1 Claude recording, got %d", len(claudeRecordings))
	}
	if len(geminiRecordings) != 1 {
		t.Errorf("expected 1 Gemini recording, got %d", len(geminiRecordings))
	}
}

func TestLoadEndedRecordingsByAgent_MapsDisplayNamesToBinaryNames(t *testing.T) {
	h := newTestHelper(t)

	// Create recording with display name "Claude" (should map to binary name "claude")
	testUUID := "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"

	h.createRecordingFiles(testUUID, recordingOpts{
		metadata: &RecordingMetadata{
			UUID:  testUUID,
			Name:  "Test Session",
			Agent: "Claude", // Display name
		},
	})

	recordings := loadEndedRecordings()
	grouped := loadEndedRecordingsByAgent(recordings)

	// Should be grouped under "claude" (binary name), not "Claude"
	if _, ok := grouped["Claude"]; ok {
		t.Error("expected display name 'Claude' to be mapped to binary name 'claude'")
	}
	if recordings, ok := grouped["claude"]; !ok || len(recordings) != 1 {
		t.Error("expected recording to be grouped under 'claude'")
	}
}

// ============================================================================
// Keep Recording API Tests
// ============================================================================

func TestKeepRecording_Success(t *testing.T) {
	h := newTestHelper(t)

	testUUID := "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"
	startedAt := time.Now().Add(-1 * time.Hour)
	endedAt := time.Now().Add(-30 * time.Minute)

	h.createRecordingFiles(testUUID, recordingOpts{
		metadata: &RecordingMetadata{
			UUID:      testUUID,
			Name:      "Test Session",
			Agent:     "claude",
			StartedAt: startedAt,
			EndedAt:   &endedAt,
		},
	})

	req := httptest.NewRequest(http.MethodPost, "/api/recording/"+testUUID+"/keep", nil)
	w := httptest.NewRecorder()

	handleRecordingAPI(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}

	// Parse response
	var result map[string]interface{}
	body, _ := io.ReadAll(resp.Body)
	if err := json.Unmarshal(body, &result); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}

	if result["already_kept"].(bool) != false {
		t.Error("expected already_kept to be false")
	}
	if result["kept_at"] == nil {
		t.Error("expected kept_at to be set")
	}

	// Verify metadata was updated
	metadataPath := filepath.Join(h.recordingDir, "session-"+testUUID+".metadata.json")
	metaData, err := os.ReadFile(metadataPath)
	if err != nil {
		t.Fatalf("failed to read metadata: %v", err)
	}
	var meta RecordingMetadata
	if err := json.Unmarshal(metaData, &meta); err != nil {
		t.Fatalf("failed to parse metadata: %v", err)
	}
	if meta.KeptAt == nil {
		t.Error("expected KeptAt to be set in metadata")
	}
}

func TestKeepRecording_NotFound(t *testing.T) {
	h := newTestHelper(t)
	_ = h // use helper for test isolation

	testUUID := "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"

	req := httptest.NewRequest(http.MethodPost, "/api/recording/"+testUUID+"/keep", nil)
	w := httptest.NewRecorder()

	handleRecordingAPI(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("expected status 404, got %d", resp.StatusCode)
	}
}

func TestKeepRecording_AlreadyKept(t *testing.T) {
	h := newTestHelper(t)

	testUUID := "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"
	startedAt := time.Now().Add(-1 * time.Hour)
	endedAt := time.Now().Add(-30 * time.Minute)
	keptAt := time.Now().Add(-15 * time.Minute)

	h.createRecordingFiles(testUUID, recordingOpts{
		metadata: &RecordingMetadata{
			UUID:      testUUID,
			Name:      "Test Session",
			Agent:     "claude",
			StartedAt: startedAt,
			EndedAt:   &endedAt,
			KeptAt:    &keptAt,
		},
	})

	req := httptest.NewRequest(http.MethodPost, "/api/recording/"+testUUID+"/keep", nil)
	w := httptest.NewRecorder()

	handleRecordingAPI(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}

	// Parse response
	var result map[string]interface{}
	body, _ := io.ReadAll(resp.Body)
	if err := json.Unmarshal(body, &result); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}

	if result["already_kept"].(bool) != true {
		t.Error("expected already_kept to be true")
	}
}

// ============================================================================
// Cleanup Recent Recordings Tests
// ============================================================================

func TestCleanupRecentRecordings_DeletesOldUnkept(t *testing.T) {
	h := newTestHelper(t)

	// Create a recording with log file mtime older than 48 hours
	testUUID := "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"
	startedAt := time.Now().Add(-50 * time.Hour)
	endedAt := time.Now().Add(-49 * time.Hour)
	logMtime := time.Now().Add(-49 * time.Hour) // 49 hours ago

	h.createRecordingFiles(testUUID, recordingOpts{
		logMtime: &logMtime,
		metadata: &RecordingMetadata{
			UUID:      testUUID,
			Name:      "Old Session",
			Agent:     "claude",
			StartedAt: startedAt,
			EndedAt:   &endedAt,
		},
	})

	// Verify file exists
	if !h.recordingFileExists(testUUID, ".log") {
		t.Fatal("recording file should exist before cleanup")
	}

	cleanupRecentRecordings()

	// Verify file was deleted
	if h.recordingFileExists(testUUID, ".log") {
		t.Error("old unkept recording should have been deleted")
	}
}

func TestCleanupRecentRecordings_KeepsKeptRecordings(t *testing.T) {
	h := newTestHelper(t)

	// Create a kept recording with log mtime older than 48 hours
	testUUID := "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"
	startedAt := time.Now().Add(-50 * time.Hour)
	endedAt := time.Now().Add(-49 * time.Hour)
	keptAt := time.Now().Add(-48 * time.Hour)
	logMtime := time.Now().Add(-49 * time.Hour)

	h.createRecordingFiles(testUUID, recordingOpts{
		logMtime: &logMtime,
		metadata: &RecordingMetadata{
			UUID:      testUUID,
			Name:      "Kept Session",
			Agent:     "claude",
			StartedAt: startedAt,
			EndedAt:   &endedAt,
			KeptAt:    &keptAt,
		},
	})

	cleanupRecentRecordings()

	// Verify file still exists
	if !h.recordingFileExists(testUUID, ".log") {
		t.Error("kept recording should NOT have been deleted")
	}
}

func TestCleanupRecentRecordings_KeepsRecentUnkept(t *testing.T) {
	h := newTestHelper(t)

	// Create a recent recording (log mtime less than 48 hours old)
	testUUID := "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"
	startedAt := time.Now().Add(-2 * time.Hour)
	endedAt := time.Now().Add(-1 * time.Hour)
	logMtime := time.Now().Add(-1 * time.Hour)

	h.createRecordingFiles(testUUID, recordingOpts{
		logMtime: &logMtime,
		metadata: &RecordingMetadata{
			UUID:      testUUID,
			Name:      "Recent Session",
			Agent:     "claude",
			StartedAt: startedAt,
			EndedAt:   &endedAt,
		},
	})

	cleanupRecentRecordings()

	// Verify file still exists
	if !h.recordingFileExists(testUUID, ".log") {
		t.Error("recent unkept recording should NOT have been deleted")
	}
}

func TestCleanupRecentRecordings_DeletesBeyondLimit(t *testing.T) {
	h := newTestHelper(t)

	// Create 7 recent recordings for the same agent
	// All are recent (< 48 hours), but we exceed the limit of 5
	uuids := []string{
		"aaaaaaaa-0001-0000-0000-000000000001",
		"aaaaaaaa-0002-0000-0000-000000000002",
		"aaaaaaaa-0003-0000-0000-000000000003",
		"aaaaaaaa-0004-0000-0000-000000000004",
		"aaaaaaaa-0005-0000-0000-000000000005",
		"aaaaaaaa-0006-0000-0000-000000000006",
		"aaaaaaaa-0007-0000-0000-000000000007",
	}

	for i, uuid := range uuids {
		// Create recordings with different ages (all recent, within 48h)
		endedAt := time.Now().Add(-time.Duration(i+1) * time.Minute)
		logMtime := endedAt // log mtime matches ended time
		h.createRecordingFiles(uuid, recordingOpts{
			logMtime: &logMtime,
			metadata: &RecordingMetadata{
				UUID:      uuid,
				Name:      "Session " + string(rune('A'+i)),
				Agent:     "claude",
				StartedAt: endedAt.Add(-5 * time.Minute),
				EndedAt:   &endedAt,
			},
		})
	}

	cleanupRecentRecordings()

	// Count remaining recordings
	remaining := 0
	for _, uuid := range uuids {
		if h.recordingFileExists(uuid, ".log") {
			remaining++
		}
	}

	if remaining != maxRecentRecordingsPerAgent {
		t.Errorf("expected %d recordings to remain, got %d", maxRecentRecordingsPerAgent, remaining)
	}

	// Verify the newest 5 are kept (oldest 2 deleted)
	for i, uuid := range uuids {
		if i < maxRecentRecordingsPerAgent {
			if !h.recordingFileExists(uuid, ".log") {
				t.Errorf("recording %s (position %d) should have been kept", uuid[:8], i+1)
			}
		} else {
			if h.recordingFileExists(uuid, ".log") {
				t.Errorf("recording %s (position %d) should have been deleted", uuid[:8], i+1)
			}
		}
	}
}

func TestCleanupRecentRecordings_LimitPerAgent(t *testing.T) {
	h := newTestHelper(t)

	// Create 3 recordings for claude and 3 for aider
	// All recent, none should be deleted (within limit per agent)
	claudeUUIDs := []string{
		"cccccccc-0001-0000-0000-000000000001",
		"cccccccc-0002-0000-0000-000000000002",
		"cccccccc-0003-0000-0000-000000000003",
	}
	aiderUUIDs := []string{
		"aaaaaaaa-0001-0000-0000-000000000001",
		"aaaaaaaa-0002-0000-0000-000000000002",
		"aaaaaaaa-0003-0000-0000-000000000003",
	}

	for i, uuid := range claudeUUIDs {
		endedAt := time.Now().Add(-time.Duration(i+1) * time.Minute)
		logMtime := endedAt
		h.createRecordingFiles(uuid, recordingOpts{
			logMtime: &logMtime,
			metadata: &RecordingMetadata{
				UUID:      uuid,
				Name:      "Claude Session",
				Agent:     "claude",
				StartedAt: endedAt.Add(-5 * time.Minute),
				EndedAt:   &endedAt,
			},
		})
	}

	for i, uuid := range aiderUUIDs {
		endedAt := time.Now().Add(-time.Duration(i+1) * time.Minute)
		logMtime := endedAt
		h.createRecordingFiles(uuid, recordingOpts{
			logMtime: &logMtime,
			metadata: &RecordingMetadata{
				UUID:      uuid,
				Name:      "Aider Session",
				Agent:     "aider",
				StartedAt: endedAt.Add(-5 * time.Minute),
				EndedAt:   &endedAt,
			},
		})
	}

	cleanupRecentRecordings()

	// All 6 recordings should still exist (3 per agent, limit is 5)
	for _, uuid := range claudeUUIDs {
		if !h.recordingFileExists(uuid, ".log") {
			t.Errorf("claude recording %s should NOT have been deleted", uuid[:8])
		}
	}
	for _, uuid := range aiderUUIDs {
		if !h.recordingFileExists(uuid, ".log") {
			t.Errorf("aider recording %s should NOT have been deleted", uuid[:8])
		}
	}
}

func TestCalculateTerminalDimensions(t *testing.T) {
	tests := []struct {
		name         string
		content      string
		expectedCols uint16
		expectedRows uint32
	}{
		{
			name: "content with cursor positioning",
			content: "Script started on 2026-01-12 06:41:43+00:00\n" +
				"line1\n" +
				"\x1b[10;5H" + // cursor to row 10
				"positioned content\n" +
				"\x1b[25;1H" + // cursor to row 25
				"more content\n",
			expectedCols: 80,  // min cols (no long lines)
			expectedRows: 25,  // max cursor row
		},
		{
			name: "content with long lines",
			content: "Script started on 2026-01-12 06:41:43+00:00\n" +
				strings.Repeat("x", 150) + "\n" + // 150 char line
				"short line\n",
			expectedCols: 150, // max line length
			expectedRows: 24,  // min rows (line count < 24)
		},
		{
			name: "content with many newlines",
			content: "Script started on 2026-01-12 06:41:43+00:00\n" +
				strings.Repeat("line\n", 100), // 100 lines after header stripped
			expectedCols: 80,  // min cols
			expectedRows: 100, // line count (header stripped)
		},
		{
			name: "empty content after stripping",
			content: "Script started on 2026-01-12 06:41:43+00:00\n" +
				"Script done on 2026-01-12 06:41:43+00:00\n",
			expectedCols: 80, // min cols
			expectedRows: 24, // min rows
		},
		{
			name: "line length exceeds 240 cap",
			content: "Script started on 2026-01-12 06:41:43+00:00\n" +
				strings.Repeat("x", 300) + "\n", // 300 char line
			expectedCols: 240, // capped at 240
			expectedRows: 24,  // min rows
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Create temp file with content
			tmpFile, err := os.CreateTemp("", "session-*.log")
			if err != nil {
				t.Fatalf("Failed to create temp file: %v", err)
			}
			defer os.Remove(tmpFile.Name())

			if _, err := tmpFile.WriteString(tc.content); err != nil {
				t.Fatalf("Failed to write content: %v", err)
			}
			tmpFile.Close()

			result := calculateTerminalDimensions(tmpFile.Name())
			if result.Cols != tc.expectedCols {
				t.Errorf("calculateTerminalDimensions().Cols = %d, want %d", result.Cols, tc.expectedCols)
			}
			if result.Rows != tc.expectedRows {
				t.Errorf("calculateTerminalDimensions().Rows = %d, want %d", result.Rows, tc.expectedRows)
			}
		})
	}

	// Test non-existent file returns defaults
	t.Run("non-existent file returns defaults", func(t *testing.T) {
		result := calculateTerminalDimensions("/non/existent/path/session.log")
		if result.Cols != 240 || result.Rows != 24 {
			t.Errorf("calculateTerminalDimensions() for non-existent file = {%d, %d}, want {240, 24}",
				result.Cols, result.Rows)
		}
	})
}
