//go:build windows

package main

import (
	"os/exec"
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
