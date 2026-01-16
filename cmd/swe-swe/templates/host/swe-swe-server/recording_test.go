package main

import (
	"archive/zip"
	"bytes"
	"encoding/base64"
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
// Test server helper
// ============================================================================

func (h *testHelper) createTestServer() *httptest.Server {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/recording/", handleRecordingAPI)
	mux.HandleFunc("/recording/", func(w http.ResponseWriter, r *http.Request) {
		recordingUUID := r.URL.Path[len("/recording/"):]
		handleRecordingPage(w, r, recordingUUID)
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

func TestPlaybackPage_WithTiming_AnimatedMode(t *testing.T) {
	h := newTestHelper(t)

	testUUID := "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"
	h.createRecordingFiles(testUUID, recordingOpts{
		logContent:    "Hello\r\nWorld\r\n",
		withTiming:    true,
		timingContent: "0.1 5\n0.5 6\n",
		metadata: &RecordingMetadata{
			UUID:    testUUID,
			Name:    "Animated Recording",
			MaxCols: 80,
			MaxRows: 24,
		},
	})

	server := h.createTestServer()
	defer server.Close()

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

	// Should have play/pause controls
	if !strings.Contains(html, "play") && !strings.Contains(html, "Play") {
		t.Error("expected animated playback to contain play controls")
	}

	// Should have timeline/progress
	if !strings.Contains(html, "timeline") && !strings.Contains(html, "progress") && !strings.Contains(html, "slider") {
		t.Error("expected animated playback to contain timeline/progress")
	}

	// Should contain recording name in title or heading
	if !strings.Contains(html, "Animated Recording") {
		t.Error("expected page to contain recording name")
	}

	// Should have back link to homepage
	if !strings.Contains(html, `href="/"`) && !strings.Contains(html, `href='/'`) {
		t.Error("expected page to contain back link to homepage")
	}
}

func TestPlaybackPage_WithoutTiming_StaticMode(t *testing.T) {
	h := newTestHelper(t)

	testUUID := "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"
	// Content with distinctive first and last parts to verify ENTIRE content is embedded
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

	resp, err := http.Get(server.URL + "/recording/" + testUUID)
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

	// Static mode embeds content as base64 in JavaScript
	// Check that the page has the static mode notice
	if !strings.Contains(html, "Timing data not available") {
		t.Error("expected static mode to show 'Timing data not available' notice")
	}

	// Should have the recording name
	if !strings.Contains(html, "Static Recording") {
		t.Error("expected static mode to contain recording name")
	}

	// Should have xterm.js for rendering
	if !strings.Contains(html, "xterm") {
		t.Error("expected static mode to use xterm.js")
	}

	// Should NOT have play button (static mode shows final state only)
	if strings.Contains(html, "playBtn") || strings.Contains(html, "play-btn") {
		t.Error("expected static mode NOT to have play button")
	}

	// Should NOT have timeline/progress controls
	if strings.Contains(html, "progressBar") || strings.Contains(html, "progress-bar") {
		t.Error("expected static mode NOT to have progress bar")
	}

	// Verify the ENTIRE content is embedded (not just first frame)
	// Extract the base64 content from the HTML and decode it
	// Look for: const contentBase64 = '...';
	contentMatch := strings.Index(html, "const contentBase64 = '")
	if contentMatch == -1 {
		t.Fatal("could not find contentBase64 in static HTML")
	}
	start := contentMatch + len("const contentBase64 = '")
	end := strings.Index(html[start:], "'")
	if end == -1 {
		t.Fatal("could not find end of contentBase64 string")
	}
	base64Content := html[start : start+end]

	// Decode base64
	decoded, err := base64.StdEncoding.DecodeString(base64Content)
	if err != nil {
		t.Fatalf("failed to decode base64 content: %v", err)
	}
	decodedStr := string(decoded)

	// CRITICAL: Verify both first AND last parts are present
	// This confirms we're showing the entire recording, not just first frame
	if !strings.Contains(decodedStr, "FIRST_LINE") {
		t.Error("expected decoded content to contain FIRST_LINE")
	}
	if !strings.Contains(decodedStr, "LAST_LINE") {
		t.Error("expected decoded content to contain LAST_LINE - static mode should show ALL content, not just first frame")
	}
	if !strings.Contains(decodedStr, "middle content") {
		t.Error("expected decoded content to contain middle content")
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

func TestPlaybackPage_BackLink(t *testing.T) {
	h := newTestHelper(t)

	testUUID := "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"
	h.createRecordingFiles(testUUID, recordingOpts{
		withTiming: true,
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

	// Should have a link back to homepage
	if !strings.Contains(html, `href="/"`) && !strings.Contains(html, `href='/'`) {
		t.Error("expected page to contain back link to homepage '/'")
	}
}

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

	grouped := loadEndedRecordingsByAgent()

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

	grouped := loadEndedRecordingsByAgent()

	// Should be grouped under "claude" (binary name), not "Claude"
	if _, ok := grouped["Claude"]; ok {
		t.Error("expected display name 'Claude' to be mapped to binary name 'claude'")
	}
	if recordings, ok := grouped["claude"]; !ok || len(recordings) != 1 {
		t.Error("expected recording to be grouped under 'claude'")
	}
}
