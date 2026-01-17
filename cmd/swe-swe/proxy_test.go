package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"sync"
	"testing"
	"time"
)

func TestParseNulDelimitedArgs(t *testing.T) {
	tests := []struct {
		name     string
		input    []byte
		expected []string
	}{
		{
			name:     "empty input",
			input:    []byte{},
			expected: nil,
		},
		{
			name:     "single arg",
			input:    []byte("hello"),
			expected: []string{"hello"},
		},
		{
			name:     "single arg with trailing NUL",
			input:    []byte("hello\x00"),
			expected: []string{"hello"},
		},
		{
			name:     "two args",
			input:    []byte("hello\x00world"),
			expected: []string{"hello", "world"},
		},
		{
			name:     "two args with trailing NUL",
			input:    []byte("hello\x00world\x00"),
			expected: []string{"hello", "world"},
		},
		{
			name:     "multiple NULs between args",
			input:    []byte("a\x00\x00b"),
			expected: []string{"a", "b"},
		},
		{
			name:     "args with spaces",
			input:    []byte("hello world\x00foo bar"),
			expected: []string{"hello world", "foo bar"},
		},
		{
			name:     "args with equals",
			input:    []byte("FOO=bar\x00BAZ=qux"),
			expected: []string{"FOO=bar", "BAZ=qux"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := parseNulDelimitedArgs(tt.input)
			if len(result) != len(tt.expected) {
				t.Errorf("expected %d args, got %d: %v", len(tt.expected), len(result), result)
				return
			}
			for i, arg := range result {
				if arg != tt.expected[i] {
					t.Errorf("arg %d: expected %q, got %q", i, tt.expected[i], arg)
				}
			}
		})
	}
}

func TestCheckAndClaimPIDFile_NewFile(t *testing.T) {
	// Create temp directory
	tmpDir, err := os.MkdirTemp("", "proxy-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	pidFile := filepath.Join(tmpDir, "test.pid")

	// Should succeed when no PID file exists
	err = checkAndClaimPIDFile(pidFile, "test")
	if err != nil {
		t.Errorf("expected no error, got: %v", err)
	}

	// Verify PID file was created
	data, err := os.ReadFile(pidFile)
	if err != nil {
		t.Errorf("failed to read PID file: %v", err)
	}

	pid, err := strconv.Atoi(string(data))
	if err != nil {
		t.Errorf("PID file content is not a valid integer: %v", err)
	}

	if pid != os.Getpid() {
		t.Errorf("expected PID %d, got %d", os.Getpid(), pid)
	}
}

func TestCheckAndClaimPIDFile_StaleFile(t *testing.T) {
	// Create temp directory
	tmpDir, err := os.MkdirTemp("", "proxy-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	pidFile := filepath.Join(tmpDir, "test.pid")

	// Create a stale PID file with a non-existent PID
	if err := os.WriteFile(pidFile, []byte("999999"), 0644); err != nil {
		t.Fatalf("failed to create stale PID file: %v", err)
	}

	// Should succeed and replace the stale PID file
	err = checkAndClaimPIDFile(pidFile, "test")
	if err != nil {
		t.Errorf("expected no error for stale PID, got: %v", err)
	}

	// Verify PID file now contains our PID
	data, err := os.ReadFile(pidFile)
	if err != nil {
		t.Errorf("failed to read PID file: %v", err)
	}

	pid, err := strconv.Atoi(string(data))
	if err != nil {
		t.Errorf("PID file content is not a valid integer: %v", err)
	}

	if pid != os.Getpid() {
		t.Errorf("expected PID %d, got %d", os.Getpid(), pid)
	}
}

func TestCheckAndClaimPIDFile_InvalidFile(t *testing.T) {
	// Create temp directory
	tmpDir, err := os.MkdirTemp("", "proxy-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	pidFile := filepath.Join(tmpDir, "test.pid")

	// Create an invalid PID file (not a number)
	if err := os.WriteFile(pidFile, []byte("not-a-pid"), 0644); err != nil {
		t.Fatalf("failed to create invalid PID file: %v", err)
	}

	// Should succeed and replace the invalid PID file
	err = checkAndClaimPIDFile(pidFile, "test")
	if err != nil {
		t.Errorf("expected no error for invalid PID file, got: %v", err)
	}
}

func TestCheckAndClaimPIDFile_RunningProcess(t *testing.T) {
	// Create temp directory
	tmpDir, err := os.MkdirTemp("", "proxy-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	pidFile := filepath.Join(tmpDir, "test.pid")

	// Create PID file with our own PID (a running process we own)
	if err := os.WriteFile(pidFile, []byte(strconv.Itoa(os.Getpid())), 0644); err != nil {
		t.Fatalf("failed to create PID file: %v", err)
	}

	// Should fail because the process is running
	err = checkAndClaimPIDFile(pidFile, "test")
	if err == nil {
		t.Error("expected error for running process, got nil")
	}
}

// Phase 2 Tests

func TestGenerateContainerScript(t *testing.T) {
	// Create temp directory
	tmpDir, err := os.MkdirTemp("", "proxy-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	scriptFile := filepath.Join(tmpDir, "make")

	// Generate script
	err = generateContainerScript(scriptFile, "make")
	if err != nil {
		t.Fatalf("generateContainerScript failed: %v", err)
	}

	// Verify file exists
	info, err := os.Stat(scriptFile)
	if err != nil {
		t.Fatalf("script file not found: %v", err)
	}

	// Verify executable permissions
	if info.Mode()&0111 == 0 {
		t.Error("script should be executable")
	}
}

func TestGenerateContainerScript_Content(t *testing.T) {
	// Create temp directory
	tmpDir, err := os.MkdirTemp("", "proxy-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	scriptFile := filepath.Join(tmpDir, "echo")

	// Generate script
	err = generateContainerScript(scriptFile, "echo")
	if err != nil {
		t.Fatalf("generateContainerScript failed: %v", err)
	}

	// Read script content
	content, err := os.ReadFile(scriptFile)
	if err != nil {
		t.Fatalf("failed to read script: %v", err)
	}

	script := string(content)

	// Verify essential components
	checks := []struct {
		name    string
		snippet string
	}{
		{"shebang", "#!/usr/bin/env bash"},
		{"strict mode", "set -euo pipefail"},
		{"command name", "proxying 'echo' commands"},
		{"uuid generation", "/proc/sys/kernel/random/uuid"},
		{"NUL-delimited args", `printf '%s\0' "$@"`},
		{"atomic rename", "mv \"$tmp_file\" \"$req_file\""},
		{"inotifywait", "inotifywait"},
		{"timeout check", "PROXY_TIMEOUT"},
		{"exit code", "exit \"$exit_code\""},
		{"cleanup", "rm -f"},
	}

	for _, check := range checks {
		t.Run(check.name, func(t *testing.T) {
			if !contains(script, check.snippet) {
				t.Errorf("script missing %s snippet: %q", check.name, check.snippet)
			}
		})
	}
}

func TestGenerateContainerScript_Cleanup(t *testing.T) {
	// Create temp directory
	tmpDir, err := os.MkdirTemp("", "proxy-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	scriptFile := filepath.Join(tmpDir, "make")

	// Generate script
	err = generateContainerScript(scriptFile, "make")
	if err != nil {
		t.Fatalf("generateContainerScript failed: %v", err)
	}

	// Verify script exists
	if _, err := os.Stat(scriptFile); err != nil {
		t.Fatalf("script not found: %v", err)
	}

	// Remove script (simulating shutdown cleanup)
	os.Remove(scriptFile)

	// Verify script is gone
	if _, err := os.Stat(scriptFile); !os.IsNotExist(err) {
		t.Error("script should be deleted after cleanup")
	}
}

// contains checks if s contains substr
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 ||
		(len(s) > 0 && len(substr) > 0 && findSubstring(s, substr)))
}

func findSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// Phase 3 Tests

func TestCleanupOrphanFiles_RemovesOldOrphans(t *testing.T) {
	// Create temp directory
	tmpDir, err := os.MkdirTemp("", "proxy-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create old orphan files (older than 5 minutes)
	oldTime := time.Now().Add(-10 * time.Minute)
	orphanFiles := []string{
		"test-uuid-1.stdout",
		"test-uuid-1.stderr",
		"test-uuid-1.exit",
	}

	for _, name := range orphanFiles {
		path := filepath.Join(tmpDir, name)
		if err := os.WriteFile(path, []byte("orphan"), 0644); err != nil {
			t.Fatalf("failed to create orphan file: %v", err)
		}
		if err := os.Chtimes(path, oldTime, oldTime); err != nil {
			t.Fatalf("failed to set file time: %v", err)
		}
	}

	// Run cleanup
	cleanupOrphanFiles(tmpDir)

	// Verify orphans were removed
	for _, name := range orphanFiles {
		path := filepath.Join(tmpDir, name)
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Errorf("orphan file should be deleted: %s", name)
		}
	}
}

func TestCleanupOrphanFiles_KeepsRecentFiles(t *testing.T) {
	// Create temp directory
	tmpDir, err := os.MkdirTemp("", "proxy-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create recent files (within 5 minutes)
	recentFiles := []string{
		"recent-uuid.stdout",
		"recent-uuid.stderr",
		"recent-uuid.exit",
	}

	for _, name := range recentFiles {
		path := filepath.Join(tmpDir, name)
		if err := os.WriteFile(path, []byte("recent"), 0644); err != nil {
			t.Fatalf("failed to create recent file: %v", err)
		}
	}

	// Run cleanup
	cleanupOrphanFiles(tmpDir)

	// Verify recent files were kept
	for _, name := range recentFiles {
		path := filepath.Join(tmpDir, name)
		if _, err := os.Stat(path); os.IsNotExist(err) {
			t.Errorf("recent file should be kept: %s", name)
		}
	}
}

func TestCleanupOrphanFiles_IgnoresNonOrphanFiles(t *testing.T) {
	// Create temp directory
	tmpDir, err := os.MkdirTemp("", "proxy-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create old non-orphan files (.req files, scripts, PID files)
	oldTime := time.Now().Add(-10 * time.Minute)
	nonOrphanFiles := []string{
		"test-uuid.req",
		"make",
		"make.pid",
	}

	for _, name := range nonOrphanFiles {
		path := filepath.Join(tmpDir, name)
		if err := os.WriteFile(path, []byte("non-orphan"), 0644); err != nil {
			t.Fatalf("failed to create non-orphan file: %v", err)
		}
		if err := os.Chtimes(path, oldTime, oldTime); err != nil {
			t.Fatalf("failed to set file time: %v", err)
		}
	}

	// Run cleanup
	cleanupOrphanFiles(tmpDir)

	// Verify non-orphan files were kept
	for _, name := range nonOrphanFiles {
		path := filepath.Join(tmpDir, name)
		if _, err := os.Stat(path); os.IsNotExist(err) {
			t.Errorf("non-orphan file should be kept: %s", name)
		}
	}
}

func TestCleanupOrphanFiles_EmptyDirectory(t *testing.T) {
	// Create temp directory
	tmpDir, err := os.MkdirTemp("", "proxy-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Run cleanup on empty directory - should not panic
	cleanupOrphanFiles(tmpDir)
}

func TestCleanupOrphanFiles_NonExistentDirectory(t *testing.T) {
	// Run cleanup on non-existent directory - should not panic
	cleanupOrphanFiles("/non/existent/directory")
}

// Phase 1 Tests: Host Infrastructure

func TestActiveRequestCounter(t *testing.T) {
	// Reset counter to known state
	for activeRequests.Load() > 0 {
		activeRequests.Add(-1)
	}

	// Verify starts at 0
	if got := activeRequests.Load(); got != 0 {
		t.Errorf("expected counter to start at 0, got %d", got)
	}

	// Increment
	activeRequests.Add(1)
	if got := activeRequests.Load(); got != 1 {
		t.Errorf("expected counter to be 1 after increment, got %d", got)
	}

	// Increment again
	activeRequests.Add(1)
	if got := activeRequests.Load(); got != 2 {
		t.Errorf("expected counter to be 2 after second increment, got %d", got)
	}

	// Decrement
	activeRequests.Add(-1)
	if got := activeRequests.Load(); got != 1 {
		t.Errorf("expected counter to be 1 after decrement, got %d", got)
	}

	// Decrement to 0
	activeRequests.Add(-1)
	if got := activeRequests.Load(); got != 0 {
		t.Errorf("expected counter to be 0 after final decrement, got %d", got)
	}
}

func TestActiveRequestCounter_Concurrent(t *testing.T) {
	// Reset counter
	for activeRequests.Load() != 0 {
		if activeRequests.Load() > 0 {
			activeRequests.Add(-1)
		} else {
			activeRequests.Add(1)
		}
	}

	// Run concurrent increments and decrements
	var wg sync.WaitGroup
	n := 100

	// Increment n times concurrently
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			activeRequests.Add(1)
		}()
	}
	wg.Wait()

	if got := activeRequests.Load(); got != int32(n) {
		t.Errorf("expected counter to be %d after concurrent increments, got %d", n, got)
	}

	// Decrement n times concurrently
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			activeRequests.Add(-1)
		}()
	}
	wg.Wait()

	if got := activeRequests.Load(); got != 0 {
		t.Errorf("expected counter to be 0 after concurrent decrements, got %d", got)
	}
}

func TestProcessRegistry(t *testing.T) {
	// Clear registry
	inFlightProcesses.Range(func(key, value any) bool {
		inFlightProcesses.Delete(key)
		return true
	})

	uuid := "test-uuid-123"
	entry := &processEntry{
		cmd:  nil,
		pgid: 12345,
	}

	// Store entry
	inFlightProcesses.Store(uuid, entry)

	// Verify entry exists
	loaded, ok := inFlightProcesses.Load(uuid)
	if !ok {
		t.Fatal("expected entry to exist in registry")
	}

	loadedEntry := loaded.(*processEntry)
	if loadedEntry.pgid != 12345 {
		t.Errorf("expected pgid 12345, got %d", loadedEntry.pgid)
	}

	// Delete entry
	inFlightProcesses.Delete(uuid)

	// Verify entry is gone
	_, ok = inFlightProcesses.Load(uuid)
	if ok {
		t.Error("expected entry to be deleted from registry")
	}
}

func TestProcessGroupSetup(t *testing.T) {
	// Create a simple command
	cmd := exec.Command("echo", "test")

	// Apply platform-specific setup
	setSysProcAttr(cmd)

	// Start the command
	if err := cmd.Start(); err != nil {
		t.Fatalf("failed to start command: %v", err)
	}

	// Get PGID
	pgid := getProcessGroupID(cmd)

	// PGID should equal PID when Setpgid is true (on Unix)
	// On Windows, this will just be the PID
	if pgid != cmd.Process.Pid {
		t.Errorf("expected pgid %d to equal pid %d", pgid, cmd.Process.Pid)
	}

	// Wait for command to complete
	cmd.Wait()
}
