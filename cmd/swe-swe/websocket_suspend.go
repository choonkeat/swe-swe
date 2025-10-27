package main

import (
	"log"
	"os/exec"
	"syscall"
	"time"
)

// suspendProcess sends SIGTSTP to suspend (pause) a process without killing it
// This is equivalent to Ctrl+Z in terminal - the process can be resumed with SIGCONT
func suspendProcess(cmd *exec.Cmd) error {
	if cmd == nil || cmd.Process == nil {
		return nil
	}
	
	log.Printf("[PROCESS] Suspending process PID: %d with SIGTSTP", cmd.Process.Pid)
	
	// Send SIGTSTP to suspend the process (like Ctrl+Z)
	err := cmd.Process.Signal(syscall.SIGTSTP)
	if err != nil {
		log.Printf("[PROCESS] Failed to suspend process: %v", err)
		return err
	}
	
	log.Printf("[PROCESS] Process suspended successfully")
	return nil
}

// resumeProcess sends SIGCONT to resume a suspended process
func resumeProcess(cmd *exec.Cmd) error {
	if cmd == nil || cmd.Process == nil {
		return nil
	}
	
	log.Printf("[PROCESS] Resuming process PID: %d with SIGCONT", cmd.Process.Pid)
	
	// Send SIGCONT to resume the suspended process
	err := cmd.Process.Signal(syscall.SIGCONT)
	if err != nil {
		log.Printf("[PROCESS] Failed to resume process: %v", err)
		return err
	}
	
	log.Printf("[PROCESS] Process resumed successfully")
	return nil
}

// interruptProcess sends SIGINT to interrupt a process (like Escape or Ctrl+C in TUI)
// This should pause Claude's execution but keep the session alive
func interruptProcess(cmd *exec.Cmd) error {
	if cmd == nil || cmd.Process == nil {
		return nil
	}
	
	log.Printf("[PROCESS] Interrupting process PID: %d with SIGINT", cmd.Process.Pid)
	
	// Send SIGINT to interrupt the process (like Ctrl+C)
	err := cmd.Process.Signal(syscall.SIGINT)
	if err != nil {
		log.Printf("[PROCESS] Failed to interrupt process: %v", err)
		return err
	}
	
	// Give it a moment to handle the interrupt
	time.Sleep(100 * time.Millisecond)
	
	log.Printf("[PROCESS] Process interrupted successfully")
	return nil
}