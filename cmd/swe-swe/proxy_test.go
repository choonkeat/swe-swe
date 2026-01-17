package main

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"
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
