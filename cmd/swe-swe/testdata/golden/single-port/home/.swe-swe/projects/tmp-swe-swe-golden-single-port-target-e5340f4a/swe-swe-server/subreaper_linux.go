//go:build linux

package main

import (
	"log"
	"os"
	"syscall"
)

// startSubreaper marks this process as a subreaper so orphaned descendants are
// reparented to us instead of PID 1, then starts the orphan reaper goroutine
// (see reaper.go) to keep zombies from accumulating. The reaper polls /proc
// instead of using SIGCHLD and skips pids in trackedPids, so it cannot race
// with the per-Session cmd.Wait() callers (PTY reader, killSessionProcessGroup,
// browser supervisors) -- those callers must call trackPid right after
// cmd.Start() and untrackPid right after cmd.Wait() returns.
func startSubreaper() {
	// PR_SET_CHILD_SUBREAPER = 36
	if _, _, errno := syscall.RawSyscall(syscall.SYS_PRCTL, 36, 1, 0); errno != 0 {
		log.Printf("[SUBREAPER] prctl(PR_SET_CHILD_SUBREAPER) failed: %v", errno)
		return
	}
	log.Printf("[SUBREAPER] enabled for pid=%d -- orphaned children will be reparented here", os.Getpid())
	startOrphanReaper()
}
