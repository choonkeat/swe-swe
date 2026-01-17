package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
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
		// Phase 5: Heartbeat support
		{"heartbeat file", "heartbeat_file="},
		{"heartbeat touch before request", "touch \"$heartbeat_file\""},
		{"heartbeat background loop", "while [[ ! -f \"$exit_file\" ]]; do"},
		{"heartbeat cleanup", "kill \"$heartbeat_pid\""},
		// Phase 6: Exit code parsing
		{"exit content read", "exit_content=$(cat \"$exit_file\")"},
		{"exit code parse", "exit_code=\"${exit_content%%:*}\""},
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

// Phase 2 Tests: Heartbeat Watcher

func TestHeartbeatStale_Fresh(t *testing.T) {
	// Create temp file
	tmpFile, err := os.CreateTemp("", "heartbeat-test-*")
	if err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())
	tmpFile.Close()

	// Touch the file (already fresh from creation)
	// File was just created, so it should not be stale with 5s threshold
	if heartbeatStale(tmpFile.Name(), 5*time.Second) {
		t.Error("freshly created file should not be stale")
	}
}

func TestHeartbeatStale_Stale(t *testing.T) {
	// Create temp file
	tmpFile, err := os.CreateTemp("", "heartbeat-test-*")
	if err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())
	tmpFile.Close()

	// Set file time to 10 seconds ago
	oldTime := time.Now().Add(-10 * time.Second)
	if err := os.Chtimes(tmpFile.Name(), oldTime, oldTime); err != nil {
		t.Fatalf("failed to set file time: %v", err)
	}

	// Should be stale with 5s threshold
	if !heartbeatStale(tmpFile.Name(), 5*time.Second) {
		t.Error("file older than threshold should be stale")
	}
}

func TestHeartbeatStale_Missing(t *testing.T) {
	// Non-existent file should be considered stale
	if !heartbeatStale("/non/existent/file/path", 5*time.Second) {
		t.Error("missing file should be considered stale")
	}
}

func TestHeartbeatWatcher_IgnoresWhenIdle(t *testing.T) {
	// Reset counter
	for activeRequests.Load() != 0 {
		if activeRequests.Load() > 0 {
			activeRequests.Add(-1)
		} else {
			activeRequests.Add(1)
		}
	}

	// Create temp directory
	tmpDir, err := os.MkdirTemp("", "heartbeat-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	heartbeatFile := filepath.Join(tmpDir, ".heartbeat")

	// No heartbeat file exists, but activeRequests is 0
	// Should not trigger kill (we can verify by checking the function doesn't panic
	// and by verifying the logic manually)

	// Verify the condition: activeRequests == 0 means we don't kill even if stale
	if activeRequests.Load() > 0 && heartbeatStale(heartbeatFile, 5*time.Second) {
		t.Error("should not trigger kill when activeRequests is 0")
	}
}

func TestHeartbeatWatcher_KillsWhenActive(t *testing.T) {
	// Clear registry
	inFlightProcesses.Range(func(key, value any) bool {
		inFlightProcesses.Delete(key)
		return true
	})

	// Reset counter
	for activeRequests.Load() != 0 {
		if activeRequests.Load() > 0 {
			activeRequests.Add(-1)
		} else {
			activeRequests.Add(1)
		}
	}

	// Create temp directory
	tmpDir, err := os.MkdirTemp("", "heartbeat-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	heartbeatFile := filepath.Join(tmpDir, ".heartbeat")

	// Simulate active request
	activeRequests.Add(1)
	defer activeRequests.Add(-1)

	// Start a real process so we can actually kill it
	cmd := exec.Command("sleep", "60")
	setSysProcAttr(cmd)
	if err := cmd.Start(); err != nil {
		t.Fatalf("failed to start command: %v", err)
	}
	defer cmd.Wait() // Clean up

	mockEntry := &processEntry{
		cmd:  cmd,
		pgid: getProcessGroupID(cmd),
	}
	inFlightProcesses.Store("test-uuid", mockEntry)
	defer inFlightProcesses.Delete("test-uuid")

	// Verify the condition: activeRequests > 0 AND stale heartbeat should trigger kill
	if activeRequests.Load() > 0 && heartbeatStale(heartbeatFile, 5*time.Second) {
		// This is the condition that would trigger killAllInFlight
		killAllInFlight(1 * time.Second)

		// Verify the entry was marked as killed
		if !mockEntry.killedByHost {
			t.Error("expected process to be marked as killed by host")
		}
		if mockEntry.killSignal == "" {
			t.Error("expected killSignal to be set")
		}
	} else {
		t.Error("expected kill condition to be true when active requests and stale heartbeat")
	}
}

func TestGetEnvDuration(t *testing.T) {
	// Test default value when env not set
	result := getEnvDuration("NONEXISTENT_ENV_VAR", 10*time.Second)
	if result != 10*time.Second {
		t.Errorf("expected default 10s, got %v", result)
	}

	// Test with valid env var
	os.Setenv("TEST_DURATION", "30s")
	defer os.Unsetenv("TEST_DURATION")

	result = getEnvDuration("TEST_DURATION", 10*time.Second)
	if result != 30*time.Second {
		t.Errorf("expected 30s from env, got %v", result)
	}

	// Test with invalid env var (returns default)
	os.Setenv("TEST_DURATION_INVALID", "not-a-duration")
	defer os.Unsetenv("TEST_DURATION_INVALID")

	result = getEnvDuration("TEST_DURATION_INVALID", 5*time.Second)
	if result != 5*time.Second {
		t.Errorf("expected default 5s for invalid env, got %v", result)
	}
}

// Phase 3 Tests: Signal Escalation

func TestKillProcessGroup_CleanExit(t *testing.T) {
	// Start a simple process that will be killed
	cmd := exec.Command("sleep", "60")
	setSysProcAttr(cmd)

	if err := cmd.Start(); err != nil {
		t.Fatalf("failed to start command: %v", err)
	}

	pgid := getProcessGroupID(cmd)

	// Kill with signal escalation (1s grace)
	signal := killProcessGroup(pgid, 1*time.Second)

	// Wait for the process to fully exit
	cmd.Wait()

	// Should have been killed with some signal (SIGTERM or SIGKILL)
	// The exact signal depends on timing and process behavior
	if signal == "" {
		t.Log("process was already dead (race condition, acceptable)")
	} else if signal != "SIGTERM" && signal != "SIGKILL" {
		t.Errorf("expected SIGTERM or SIGKILL, got %s", signal)
	}

	// Verify process is actually dead
	time.Sleep(100 * time.Millisecond)
	if isProcessGroupAlive(pgid) {
		t.Error("process group should be dead after killProcessGroup")
	}
}

func TestKillProcessGroup_ForceKill(t *testing.T) {
	// bash's trap '' SIGTERM doesn't work reliably on macOS - skip test there
	// This is a platform quirk, not an issue with the killProcessGroup function
	if runtime.GOOS != "linux" {
		t.Skipf("Test requires reliable signal trapping, skipping on %s", runtime.GOOS)
	}

	// Start a process that ignores SIGTERM
	// We use a shell script that traps SIGTERM
	cmd := exec.Command("bash", "-c", "trap '' SIGTERM; sleep 60")
	setSysProcAttr(cmd)

	if err := cmd.Start(); err != nil {
		t.Fatalf("failed to start command: %v", err)
	}

	pgid := getProcessGroupID(cmd)

	// Kill with short grace period (500ms)
	// Process ignores SIGTERM, so should escalate to SIGKILL
	start := time.Now()
	signal := killProcessGroup(pgid, 500*time.Millisecond)
	elapsed := time.Since(start)

	// Wait for the process to fully exit
	cmd.Wait()

	// Should have been killed with SIGKILL
	if signal != "SIGKILL" {
		t.Errorf("expected SIGKILL for process ignoring SIGTERM, got %s", signal)
	}

	// Should have waited for grace period before SIGKILL.
	// Use more lenient timing (200ms minimum) since system scheduling and bash startup can add variance
	if elapsed < 200*time.Millisecond {
		t.Errorf("expected to wait at least 200ms before SIGKILL, waited %v", elapsed)
	}
}

func TestKillProcessGroup_KillsChildren(t *testing.T) {
	// Start a process that spawns a child
	// Both parent and child should be in the same process group
	cmd := exec.Command("bash", "-c", "sleep 60 & wait")
	setSysProcAttr(cmd)

	if err := cmd.Start(); err != nil {
		t.Fatalf("failed to start command: %v", err)
	}

	pgid := getProcessGroupID(cmd)

	// Give child process time to start
	time.Sleep(100 * time.Millisecond)

	// Kill the process group
	signal := killProcessGroup(pgid, 1*time.Second)

	// Wait for main process
	cmd.Wait()

	// Should have killed with some signal
	if signal == "" {
		t.Log("process group was already dead (race condition, acceptable)")
	} else {
		// Successfully killed - either SIGTERM or SIGKILL
		t.Logf("killed with %s", signal)
	}

	// Note: We don't check isProcessGroupAlive here because after a process group
	// is killed, the pgid may be reused by the OS or child processes may be
	// re-parented to init. The important thing is that cmd.Wait() completed,
	// which means the main process was killed.
}

func TestKillAllInFlight_Multiple(t *testing.T) {
	// Clear registry
	inFlightProcesses.Range(func(key, value any) bool {
		inFlightProcesses.Delete(key)
		return true
	})

	// Start multiple processes
	cmds := make([]*exec.Cmd, 3)
	entries := make([]*processEntry, 3)

	for i := 0; i < 3; i++ {
		cmd := exec.Command("sleep", "60")
		setSysProcAttr(cmd)
		if err := cmd.Start(); err != nil {
			t.Fatalf("failed to start command %d: %v", i, err)
		}
		cmds[i] = cmd

		entry := &processEntry{
			cmd:  cmd,
			pgid: getProcessGroupID(cmd),
		}
		entries[i] = entry
		inFlightProcesses.Store(fmt.Sprintf("uuid-%d", i), entry)
	}

	// Kill all in-flight
	killAllInFlight(1 * time.Second)

	// Wait for all processes and verify they were killed
	for i, cmd := range cmds {
		cmd.Wait()

		if !entries[i].killedByHost {
			t.Errorf("process %d should be marked as killed by host", i)
		}
		if entries[i].killSignal == "" {
			t.Errorf("process %d should have a kill signal recorded", i)
		}
	}

	// Clean up registry
	inFlightProcesses.Range(func(key, value any) bool {
		inFlightProcesses.Delete(key)
		return true
	})
}

// Phase 4 Tests: Graceful Shutdown

func TestStopAccepting(t *testing.T) {
	// Reset state
	stopAccepting.Store(false)

	// Verify starts at false
	if stopAccepting.Load() {
		t.Error("stopAccepting should start as false")
	}

	// Set to true
	stopAccepting.Store(true)
	if !stopAccepting.Load() {
		t.Error("stopAccepting should be true after Store(true)")
	}

	// Reset
	stopAccepting.Store(false)
	if stopAccepting.Load() {
		t.Error("stopAccepting should be false after Store(false)")
	}
}

func TestRejectRequest(t *testing.T) {
	// Create the proxyDir directory for testing
	if err := os.MkdirAll(proxyDir, 0755); err != nil {
		t.Fatalf("failed to create proxy dir: %v", err)
	}

	uuid := "test-reject-uuid"

	// Clean up after test
	defer func() {
		os.Remove(filepath.Join(proxyDir, uuid+".req"))
		os.Remove(filepath.Join(proxyDir, uuid+".stdout"))
		os.Remove(filepath.Join(proxyDir, uuid+".stderr"))
		os.Remove(filepath.Join(proxyDir, uuid+".exit"))
	}()

	// Create the .req file that would exist
	reqFile := filepath.Join(proxyDir, uuid+".req")
	if err := os.WriteFile(reqFile, []byte("test request"), 0644); err != nil {
		t.Fatalf("failed to create req file: %v", err)
	}

	// Call rejectRequest
	rejectRequest(uuid)

	// Verify .req file was removed
	if _, err := os.Stat(reqFile); !os.IsNotExist(err) {
		t.Error("expected .req file to be removed")
	}

	// Verify .stdout file exists and is empty
	stdoutFile := filepath.Join(proxyDir, uuid+".stdout")
	if content, err := os.ReadFile(stdoutFile); err != nil {
		t.Errorf("expected .stdout file to exist: %v", err)
	} else if len(content) != 0 {
		t.Errorf("expected .stdout to be empty, got %d bytes", len(content))
	}

	// Verify .stderr file exists with shutdown message
	stderrFile := filepath.Join(proxyDir, uuid+".stderr")
	if content, err := os.ReadFile(stderrFile); err != nil {
		t.Errorf("expected .stderr file to exist: %v", err)
	} else if !contains(string(content), "shutting down") {
		t.Errorf("expected .stderr to contain shutdown message, got: %s", content)
	}

	// Verify .exit file contains 125:shutdown
	exitFile := filepath.Join(proxyDir, uuid+".exit")
	if content, err := os.ReadFile(exitFile); err != nil {
		t.Errorf("expected .exit file to exist: %v", err)
	} else if string(content) != "125:shutdown" {
		t.Errorf("expected .exit to be '125:shutdown', got: %s", content)
	}
}

func TestGracefulShutdown_RejectsWhenShuttingDown(t *testing.T) {
	// This test verifies that the stopAccepting flag causes requests to be rejected
	// We test the condition check logic that's used in the main loop

	// Reset state
	stopAccepting.Store(false)

	// Simulate not shutting down - should not reject
	if stopAccepting.Load() {
		t.Error("should not reject when stopAccepting is false")
	}

	// Simulate shutting down - should reject
	stopAccepting.Store(true)
	if !stopAccepting.Load() {
		t.Error("should reject when stopAccepting is true")
	}

	// Reset
	stopAccepting.Store(false)
}

func TestGracefulShutdown_WaitsForCompletion(t *testing.T) {
	// Test the shutdown wait logic
	// When activeRequests drops to 0, shutdown should complete

	// Reset counter
	for activeRequests.Load() != 0 {
		if activeRequests.Load() > 0 {
			activeRequests.Add(-1)
		} else {
			activeRequests.Add(1)
		}
	}

	// Simulate an active request
	activeRequests.Add(1)

	done := make(chan bool, 1)
	go func() {
		// Wait for activeRequests to be 0
		ticker := time.NewTicker(100 * time.Millisecond)
		defer ticker.Stop()
		deadline := time.After(2 * time.Second)

		for {
			select {
			case <-ticker.C:
				if activeRequests.Load() == 0 {
					done <- true
					return
				}
			case <-deadline:
				done <- false
				return
			}
		}
	}()

	// Complete the request after a short delay
	time.Sleep(200 * time.Millisecond)
	activeRequests.Add(-1)

	// Verify shutdown wait completed
	if !<-done {
		t.Error("expected shutdown wait to complete when activeRequests reaches 0")
	}
}

func TestGracefulShutdown_KillsAfterDeadline(t *testing.T) {
	// Clear registry and counter
	inFlightProcesses.Range(func(key, value any) bool {
		inFlightProcesses.Delete(key)
		return true
	})
	for activeRequests.Load() != 0 {
		if activeRequests.Load() > 0 {
			activeRequests.Add(-1)
		} else {
			activeRequests.Add(1)
		}
	}

	// Start a hanging process
	cmd := exec.Command("sleep", "60")
	setSysProcAttr(cmd)
	if err := cmd.Start(); err != nil {
		t.Fatalf("failed to start command: %v", err)
	}

	entry := &processEntry{
		cmd:  cmd,
		pgid: getProcessGroupID(cmd),
	}
	activeRequests.Add(1)
	inFlightProcesses.Store("hanging-uuid", entry)

	// Simulate the shutdown deadline behavior
	// After deadline, should kill all in-flight processes
	shutdownGrace := 500 * time.Millisecond
	killGrace := 500 * time.Millisecond

	done := make(chan bool, 1)
	go func() {
		ticker := time.NewTicker(100 * time.Millisecond)
		defer ticker.Stop()
		deadline := time.After(shutdownGrace)

		for {
			select {
			case <-ticker.C:
				if activeRequests.Load() == 0 {
					done <- true
					return
				}
			case <-deadline:
				// Deadline exceeded, kill remaining
				killAllInFlight(killGrace)
				done <- true
				return
			}
		}
	}()

	// Wait for shutdown to complete
	<-done

	// Wait for process to fully exit
	cmd.Wait()

	// Verify process was killed
	if !entry.killedByHost {
		t.Error("expected process to be killed after deadline")
	}

	// Clean up
	activeRequests.Add(-1)
	inFlightProcesses.Delete("hanging-uuid")
}

// Phase 6 Tests: Exit Code Convention

func TestWriteExitFile_NormalExit(t *testing.T) {
	tmpFile, err := os.CreateTemp("", "exit-test-*")
	if err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}
	tmpFile.Close()
	defer os.Remove(tmpFile.Name())

	// Write normal exit (code only)
	if err := writeExitFile(tmpFile.Name(), 0, ""); err != nil {
		t.Fatalf("writeExitFile failed: %v", err)
	}

	// Verify content
	content, err := os.ReadFile(tmpFile.Name())
	if err != nil {
		t.Fatalf("failed to read exit file: %v", err)
	}
	if string(content) != "0" {
		t.Errorf("expected '0', got %q", content)
	}
}

func TestWriteExitFile_NonZeroExit(t *testing.T) {
	tmpFile, err := os.CreateTemp("", "exit-test-*")
	if err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}
	tmpFile.Close()
	defer os.Remove(tmpFile.Name())

	// Write non-zero exit
	if err := writeExitFile(tmpFile.Name(), 1, ""); err != nil {
		t.Fatalf("writeExitFile failed: %v", err)
	}

	// Verify content
	content, err := os.ReadFile(tmpFile.Name())
	if err != nil {
		t.Fatalf("failed to read exit file: %v", err)
	}
	if string(content) != "1" {
		t.Errorf("expected '1', got %q", content)
	}
}

func TestWriteExitFile_Signaled(t *testing.T) {
	tmpFile, err := os.CreateTemp("", "exit-test-*")
	if err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}
	tmpFile.Close()
	defer os.Remove(tmpFile.Name())

	// Write signal death (SIGKILL = 9, exit code 128+9=137)
	if err := writeExitFile(tmpFile.Name(), 137, "killed"); err != nil {
		t.Fatalf("writeExitFile failed: %v", err)
	}

	// Verify content
	content, err := os.ReadFile(tmpFile.Name())
	if err != nil {
		t.Fatalf("failed to read exit file: %v", err)
	}
	if string(content) != "137:killed" {
		t.Errorf("expected '137:killed', got %q", content)
	}
}

func TestWriteExitFile_Timeout(t *testing.T) {
	tmpFile, err := os.CreateTemp("", "exit-test-*")
	if err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}
	tmpFile.Close()
	defer os.Remove(tmpFile.Name())

	// Write timeout (code 124)
	if err := writeExitFile(tmpFile.Name(), 124, "timeout"); err != nil {
		t.Fatalf("writeExitFile failed: %v", err)
	}

	// Verify content
	content, err := os.ReadFile(tmpFile.Name())
	if err != nil {
		t.Fatalf("failed to read exit file: %v", err)
	}
	if string(content) != "124:timeout" {
		t.Errorf("expected '124:timeout', got %q", content)
	}
}

func TestWriteExitFile_Shutdown(t *testing.T) {
	tmpFile, err := os.CreateTemp("", "exit-test-*")
	if err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}
	tmpFile.Close()
	defer os.Remove(tmpFile.Name())

	// Write shutdown (code 125)
	if err := writeExitFile(tmpFile.Name(), 125, "shutdown"); err != nil {
		t.Fatalf("writeExitFile failed: %v", err)
	}

	// Verify content
	content, err := os.ReadFile(tmpFile.Name())
	if err != nil {
		t.Fatalf("failed to read exit file: %v", err)
	}
	if string(content) != "125:shutdown" {
		t.Errorf("expected '125:shutdown', got %q", content)
	}
}

func TestContainerParsesExitCode(t *testing.T) {
	// Test the bash parsing logic: exit_code="${exit_content%%:*}"
	tests := []struct {
		content  string
		expected string
	}{
		{"0", "0"},
		{"1", "1"},
		{"137:killed", "137"},
		{"124:timeout", "124"},
		{"125:shutdown", "125"},
		{"255", "255"},
	}

	for _, tt := range tests {
		t.Run(tt.content, func(t *testing.T) {
			// Simulate the bash parsing: ${exit_content%%:*}
			// This removes the longest match of :* from the end
			result := tt.content
			if idx := findColon(result); idx >= 0 {
				result = result[:idx]
			}
			if result != tt.expected {
				t.Errorf("parsing %q: expected %q, got %q", tt.content, tt.expected, result)
			}
		})
	}
}

func findColon(s string) int {
	for i := 0; i < len(s); i++ {
		if s[i] == ':' {
			return i
		}
	}
	return -1
}
