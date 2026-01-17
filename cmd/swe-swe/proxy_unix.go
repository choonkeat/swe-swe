//go:build unix

package main

import (
	"fmt"
	"os/exec"
	"syscall"
	"time"
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

// sendSignalToProcessGroup sends a signal to a process group on Unix.
// Negative pgid means send to the entire process group.
func sendSignalToProcessGroup(pgid int, sig syscall.Signal) error {
	return syscall.Kill(-pgid, sig)
}

// isProcessGroupAlive checks if a process group is still alive using signal 0.
func isProcessGroupAlive(pgid int) bool {
	err := syscall.Kill(-pgid, 0)
	return err == nil
}

// killProcessGroupPlatform sends SIGTERM to a process group, waits for grace period,
// then sends SIGKILL if the process is still alive.
// Returns the signal that finally killed the process (or "" if already dead).
func killProcessGroupPlatform(pgid int, grace time.Duration) string {
	// Check if already dead
	if !isProcessGroupAlive(pgid) {
		return ""
	}

	// Send SIGTERM
	fmt.Printf("[proxy] Sending SIGTERM to process group %d\n", pgid)
	if err := sendSignalToProcessGroup(pgid, syscall.SIGTERM); err != nil {
		// Process may have exited between check and kill
		return ""
	}

	// Wait for process to exit or grace period to elapse
	deadline := time.Now().Add(grace)
	for time.Now().Before(deadline) {
		if !isProcessGroupAlive(pgid) {
			return "SIGTERM"
		}
		time.Sleep(100 * time.Millisecond)
	}

	// Process still alive, send SIGKILL
	fmt.Printf("[proxy] Process group %d did not exit after SIGTERM, sending SIGKILL\n", pgid)
	if err := sendSignalToProcessGroup(pgid, syscall.SIGKILL); err != nil {
		return "SIGTERM" // Process died after SIGTERM but before SIGKILL
	}

	return "SIGKILL"
}
