//go:build windows

package main

import (
	"os/exec"
	"time"
)

// setSysProcAttr sets Windows-specific process attributes.
// Windows doesn't have Unix-style process groups, so this is a no-op.
func setSysProcAttr(cmd *exec.Cmd) {
	// No-op on Windows
}

// getProcessGroupID returns the process ID on Windows.
// Windows doesn't have process groups, so we use the PID directly.
func getProcessGroupID(cmd *exec.Cmd) int {
	return cmd.Process.Pid
}

// killProcessGroupPlatform terminates a process on Windows.
// Windows doesn't have process groups, so we just kill the main process.
// Returns the signal that killed the process (or "" if already dead).
func killProcessGroupPlatform(pgid int, grace time.Duration) string {
	// On Windows, we use os.Process.Kill() which sends a terminate signal
	// This is handled at a higher level; this stub is for API compatibility
	_ = grace
	return "SIGKILL" // Windows doesn't have signals, but we return SIGKILL for consistency
}
