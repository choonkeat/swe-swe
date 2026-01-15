//go:build unix

package main

import (
	"os/exec"
	"syscall"
)

// setSysProcAttr sets Unix-specific process attributes for process group management.
func setSysProcAttr(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

// getProcessGroupID returns the process group ID for a started process.
// On Unix, when Setpgid is true, the PGID equals the PID.
func getProcessGroupID(cmd *exec.Cmd) int {
	return cmd.Process.Pid
}
