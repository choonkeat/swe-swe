package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

// TestIntegration_BasicFlow tests the complete request/response cycle.
func TestIntegration_BasicFlow(t *testing.T) {
	helper := newProxyTestHelper(t, "echo")
	defer helper.cleanup()

	helper.startProxy()

	// Submit request
	stdout, stderr, exitCode := helper.runContainerScript("hello", "world")

	if exitCode != 0 {
		t.Errorf("expected exit code 0, got %d", exitCode)
	}
	if got := strings.TrimSpace(stdout); got != "hello world" {
		t.Errorf("expected stdout 'hello world', got %q", got)
	}
	if stderr != "" {
		t.Errorf("expected empty stderr, got %q", stderr)
	}
}

// TestIntegration_ExitCodePropagation tests that non-zero exit codes are passed through.
func TestIntegration_ExitCodePropagation(t *testing.T) {
	helper := newProxyTestHelper(t, "sh")
	defer helper.cleanup()

	helper.startProxy()

	// Run command that exits with code 42
	_, _, exitCode := helper.runContainerScript("-c", "exit 42")

	if exitCode != 42 {
		t.Errorf("expected exit code 42, got %d", exitCode)
	}
}

// TestIntegration_StdoutStderrSeparation tests that stdout and stderr are captured separately.
func TestIntegration_StdoutStderrSeparation(t *testing.T) {
	helper := newProxyTestHelper(t, "sh")
	defer helper.cleanup()

	helper.startProxy()

	// Run command that writes to both stdout and stderr
	stdout, stderr, exitCode := helper.runContainerScript("-c", "echo stdout-message; echo stderr-message >&2")

	if exitCode != 0 {
		t.Errorf("expected exit code 0, got %d", exitCode)
	}
	if got := strings.TrimSpace(stdout); got != "stdout-message" {
		t.Errorf("expected stdout 'stdout-message', got %q", got)
	}
	if got := strings.TrimSpace(stderr); got != "stderr-message" {
		t.Errorf("expected stderr 'stderr-message', got %q", got)
	}
}

// TestIntegration_ConcurrentRequests tests handling multiple simultaneous requests.
func TestIntegration_ConcurrentRequests(t *testing.T) {
	helper := newProxyTestHelper(t, "sh")
	defer helper.cleanup()

	helper.startProxy()

	// Submit 5 concurrent requests
	var wg sync.WaitGroup
	results := make(chan struct {
		stdout   string
		exitCode int
	}, 5)

	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			// Each request echoes a unique number
			stdout, _, exitCode := helper.runContainerScript("-c", "echo "+string('0'+rune(n)))
			results <- struct {
				stdout   string
				exitCode int
			}{strings.TrimSpace(stdout), exitCode}
		}(i)
	}

	wg.Wait()
	close(results)

	// Verify all requests completed successfully
	count := 0
	for result := range results {
		count++
		if result.exitCode != 0 {
			t.Errorf("expected exit code 0, got %d", result.exitCode)
		}
	}
	if count != 5 {
		t.Errorf("expected 5 results, got %d", count)
	}
}

// TestIntegration_SpecialCharacters tests arguments with special characters.
func TestIntegration_SpecialCharacters(t *testing.T) {
	helper := newProxyTestHelper(t, "echo")
	defer helper.cleanup()

	helper.startProxy()

	tests := []struct {
		name string
		args []string
		want string
	}{
		{
			name: "spaces",
			args: []string{"hello world", "foo bar"},
			want: "hello world foo bar",
		},
		{
			name: "equals sign",
			args: []string{"FOO=bar", "BAZ=qux"},
			want: "FOO=bar BAZ=qux",
		},
		{
			name: "quotes",
			args: []string{`"quoted"`, `'single'`},
			want: `"quoted" 'single'`,
		},
		{
			name: "newline in arg",
			args: []string{"line1\nline2"},
			want: "line1\nline2",
		},
		{
			name: "unicode",
			args: []string{"こんにちは", "مرحبا"},
			want: "こんにちは مرحبا",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stdout, _, exitCode := helper.runContainerScript(tt.args...)
			if exitCode != 0 {
				t.Errorf("expected exit code 0, got %d", exitCode)
			}
			if got := strings.TrimSpace(stdout); got != tt.want {
				t.Errorf("expected %q, got %q", tt.want, got)
			}
		})
	}
}

// TestIntegration_NoArguments tests running command with no arguments.
func TestIntegration_NoArguments(t *testing.T) {
	helper := newProxyTestHelper(t, "echo")
	defer helper.cleanup()

	helper.startProxy()

	// Run with no args - echo outputs just a newline
	stdout, _, exitCode := helper.runContainerScript()

	if exitCode != 0 {
		t.Errorf("expected exit code 0, got %d", exitCode)
	}
	// echo with no args outputs just a newline
	if stdout != "\n" {
		t.Errorf("expected newline, got %q", stdout)
	}
}

// TestIntegration_CommandNotFound tests handling of non-existent commands.
func TestIntegration_CommandNotFound(t *testing.T) {
	helper := newProxyTestHelper(t, "nonexistent-command-12345")
	defer helper.cleanup()

	helper.startProxy()

	_, stderr, exitCode := helper.runContainerScript("arg1")

	if exitCode != 127 {
		t.Errorf("expected exit code 127 for command not found, got %d", exitCode)
	}
	if !strings.Contains(stderr, "Failed to execute command") && !strings.Contains(stderr, "not found") {
		t.Errorf("expected error message about command not found, got %q", stderr)
	}
}

// TestIntegration_GracefulShutdown tests that in-flight requests complete on shutdown.
func TestIntegration_GracefulShutdown(t *testing.T) {
	helper := newProxyTestHelper(t, "sh")
	defer helper.cleanup()

	helper.startProxy()

	// Start a long-running request in background
	resultChan := make(chan struct {
		stdout   string
		exitCode int
	}, 1)

	go func() {
		stdout, _, exitCode := helper.runContainerScript("-c", "sleep 0.5; echo done")
		resultChan <- struct {
			stdout   string
			exitCode int
		}{strings.TrimSpace(stdout), exitCode}
	}()

	// Wait briefly for request to start
	time.Sleep(100 * time.Millisecond)

	// Send SIGTERM to proxy
	helper.stopProxy()

	// Wait for result with timeout
	select {
	case result := <-resultChan:
		if result.exitCode != 0 {
			t.Errorf("expected exit code 0, got %d", result.exitCode)
		}
		if result.stdout != "done" {
			t.Errorf("expected stdout 'done', got %q", result.stdout)
		}
	case <-time.After(5 * time.Second):
		t.Error("timeout waiting for request to complete")
	}
}

// proxyTestHelper manages proxy lifecycle for integration tests.
type proxyTestHelper struct {
	t          *testing.T
	command    string
	tmpDir     string
	proxyDir   string
	scriptPath string
	proxyCmd   *exec.Cmd
}

func newProxyTestHelper(t *testing.T, command string) *proxyTestHelper {
	t.Helper()

	tmpDir, err := os.MkdirTemp("", "proxy-integration-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}

	proxyDir := filepath.Join(tmpDir, ".swe-swe", "proxy")
	if err := os.MkdirAll(proxyDir, 0755); err != nil {
		os.RemoveAll(tmpDir)
		t.Fatalf("failed to create proxy dir: %v", err)
	}

	return &proxyTestHelper{
		t:          t,
		command:    command,
		tmpDir:     tmpDir,
		proxyDir:   proxyDir,
		scriptPath: filepath.Join(proxyDir, command),
	}
}

func (h *proxyTestHelper) cleanup() {
	h.stopProxy()
	os.RemoveAll(h.tmpDir)
}

func (h *proxyTestHelper) startProxy() {
	h.t.Helper()

	// Find swe-swe binary - check multiple possible locations
	// Tests run from the package directory, so we need to go up to repo root
	possiblePaths := []string{
		"../../dist/swe-swe.linux-amd64",
		"./dist/swe-swe.linux-amd64",
		"../../../dist/swe-swe.linux-amd64",
	}

	var binaryPath string
	for _, path := range possiblePaths {
		if _, err := os.Stat(path); err == nil {
			absPath, err := filepath.Abs(path)
			if err == nil {
				binaryPath = absPath
				break
			}
		}
	}

	if binaryPath == "" {
		h.t.Skip("swe-swe binary not found, run 'make build' first")
	}

	h.proxyCmd = exec.Command(binaryPath, "proxy", h.command)
	h.proxyCmd.Dir = h.tmpDir
	h.proxyCmd.Stdout = os.Stdout
	h.proxyCmd.Stderr = os.Stderr

	if err := h.proxyCmd.Start(); err != nil {
		h.t.Fatalf("failed to start proxy: %v", err)
	}

	// Wait for script to be generated
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(h.scriptPath); err == nil {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	h.t.Fatalf("timeout waiting for container script to be generated")
}

func (h *proxyTestHelper) stopProxy() {
	if h.proxyCmd != nil && h.proxyCmd.Process != nil {
		h.proxyCmd.Process.Signal(syscall.SIGTERM)
		h.proxyCmd.Wait()
		h.proxyCmd = nil
	}
}

func (h *proxyTestHelper) runContainerScript(args ...string) (stdout, stderr string, exitCode int) {
	h.t.Helper()

	cmd := exec.Command(h.scriptPath, args...)
	cmd.Dir = h.tmpDir
	cmd.Env = append(os.Environ(), "PROXY_DIR="+h.proxyDir)

	var stdoutBuf, stderrBuf bytes.Buffer
	cmd.Stdout = &stdoutBuf
	cmd.Stderr = &stderrBuf

	err := cmd.Run()

	stdout = stdoutBuf.String()
	stderr = stderrBuf.String()

	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			h.t.Fatalf("failed to run container script: %v", err)
		}
	}

	return
}
