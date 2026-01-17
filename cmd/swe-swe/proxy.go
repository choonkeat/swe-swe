package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"github.com/fsnotify/fsnotify"
)

const proxyDir = ".swe-swe/proxy"

// handleProxy implements the `swe-swe proxy <command>` subcommand.
// It creates a file-based proxy that allows containers to execute
// the specified command on the host and receive stdout/stderr/exit code.
func handleProxy() {
	if len(os.Args) < 3 {
		fmt.Fprintf(os.Stderr, "Usage: swe-swe proxy <command>\n\n")
		fmt.Fprintf(os.Stderr, "Starts a proxy that allows containers to execute <command> on the host.\n")
		fmt.Fprintf(os.Stderr, "The container can then run .swe-swe/proxy/<command> with arguments.\n\n")
		fmt.Fprintf(os.Stderr, "Example:\n")
		fmt.Fprintf(os.Stderr, "  Host:      swe-swe proxy make\n")
		fmt.Fprintf(os.Stderr, "  Container: .swe-swe/proxy/make build TARGET=hello\n")
		os.Exit(1)
	}

	command := os.Args[2]

	// Validate command is not empty
	if command == "" {
		fmt.Fprintf(os.Stderr, "Error: command cannot be empty\n")
		os.Exit(1)
	}

	// Create proxy directory if needed
	if err := os.MkdirAll(proxyDir, 0755); err != nil {
		fmt.Fprintf(os.Stderr, "Error: failed to create proxy directory: %v\n", err)
		os.Exit(1)
	}

	pidFile := filepath.Join(proxyDir, command+".pid")

	// Check for existing PID file
	if err := checkAndClaimPIDFile(pidFile, command); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	// Ensure PID file is cleaned up on exit
	defer os.Remove(pidFile)

	fmt.Printf("[proxy] Starting proxy for command: %s\n", command)
	fmt.Printf("[proxy] PID file: %s\n", pidFile)
	fmt.Printf("[proxy] Watching for requests in: %s\n", proxyDir)

	// Set up file watcher
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: failed to create file watcher: %v\n", err)
		os.Exit(1)
	}
	defer watcher.Close()

	// Watch the proxy directory
	if err := watcher.Add(proxyDir); err != nil {
		fmt.Fprintf(os.Stderr, "Error: failed to watch directory: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("[proxy] Listening for '%s' commands...\n", command)

	// Main event loop
	for {
		select {
		case event, ok := <-watcher.Events:
			if !ok {
				return
			}

			// We're interested in Create and Rename (moved_to) events for .req files
			if event.Op&(fsnotify.Create|fsnotify.Rename) != 0 {
				if strings.HasSuffix(event.Name, ".req") {
					uuid := strings.TrimSuffix(filepath.Base(event.Name), ".req")
					fmt.Printf("[proxy] Received request: %s\n", uuid)
					processRequest(command, uuid)
				}
			}

		case err, ok := <-watcher.Errors:
			if !ok {
				return
			}
			fmt.Fprintf(os.Stderr, "[proxy] Watcher error: %v\n", err)
		}
	}
}

// processRequest handles a single proxy request:
// 1. Read NUL-delimited args from <uuid>.req
// 2. Delete .req file (claim it)
// 3. Execute command with args
// 4. Write stdout, stderr, and exit code to response files
func processRequest(command, uuid string) {
	reqFile := filepath.Join(proxyDir, uuid+".req")
	stdoutFile := filepath.Join(proxyDir, uuid+".stdout")
	stderrFile := filepath.Join(proxyDir, uuid+".stderr")
	exitFile := filepath.Join(proxyDir, uuid+".exit")

	// Read the request file
	data, err := os.ReadFile(reqFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[proxy] Failed to read request %s: %v\n", uuid, err)
		// Write error to response files
		os.WriteFile(stderrFile, []byte(fmt.Sprintf("Failed to read request: %v\n", err)), 0644)
		os.WriteFile(stdoutFile, []byte{}, 0644)
		os.WriteFile(exitFile, []byte("1"), 0644)
		return
	}

	// Delete request file (claim it)
	os.Remove(reqFile)

	// Parse NUL-delimited arguments
	args := parseNulDelimitedArgs(data)
	fmt.Printf("[proxy] Executing: %s %v\n", command, args)

	// Execute the command
	cmd := exec.Command(command, args...)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err = cmd.Run()

	// Determine exit code
	exitCode := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			// Command failed to start (e.g., not found)
			exitCode = 127
			stderr.WriteString(fmt.Sprintf("Failed to execute command: %v\n", err))
		}
	}

	fmt.Printf("[proxy] Command exited with code: %d\n", exitCode)

	// Write response files (exit file last to signal completion)
	if err := os.WriteFile(stdoutFile, stdout.Bytes(), 0644); err != nil {
		fmt.Fprintf(os.Stderr, "[proxy] Failed to write stdout: %v\n", err)
	}
	if err := os.WriteFile(stderrFile, stderr.Bytes(), 0644); err != nil {
		fmt.Fprintf(os.Stderr, "[proxy] Failed to write stderr: %v\n", err)
	}
	if err := os.WriteFile(exitFile, []byte(strconv.Itoa(exitCode)), 0644); err != nil {
		fmt.Fprintf(os.Stderr, "[proxy] Failed to write exit code: %v\n", err)
	}
}

// parseNulDelimitedArgs splits a NUL-delimited byte slice into arguments.
// Empty input returns nil (no arguments).
// Trailing NUL bytes are handled gracefully.
func parseNulDelimitedArgs(data []byte) []string {
	if len(data) == 0 {
		return nil
	}

	// Split on NUL bytes
	parts := bytes.Split(data, []byte{0})

	// Filter out empty strings (handles trailing NUL)
	var args []string
	for _, part := range parts {
		if len(part) > 0 {
			args = append(args, string(part))
		}
	}

	return args
}

// checkAndClaimPIDFile checks if a proxy is already running for this command.
// If a stale PID file exists (process dead), it removes it.
// If the process is still running, it returns an error.
// Otherwise, it writes the current PID to the file.
func checkAndClaimPIDFile(pidFile, command string) error {
	// Check if PID file exists
	data, err := os.ReadFile(pidFile)
	if err == nil {
		// PID file exists, check if process is running
		pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
		if err != nil {
			// Invalid PID file content, remove it
			fmt.Printf("[proxy] Removing invalid PID file: %s\n", pidFile)
			os.Remove(pidFile)
		} else {
			// Check if process is alive using signal 0
			process, err := os.FindProcess(pid)
			if err == nil {
				// On Unix, FindProcess always succeeds, so we need to send signal 0
				err = process.Signal(syscall.Signal(0))
				if err == nil {
					// Process is still running
					return fmt.Errorf("proxy for '%s' already running (PID %d)", command, pid)
				}
			}
			// Process is dead, remove stale PID file
			fmt.Printf("[proxy] Removing stale PID file (PID %d no longer running)\n", pid)
			os.Remove(pidFile)
		}
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("failed to read PID file: %v", err)
	}

	// Write our PID to the file
	currentPID := os.Getpid()
	if err := os.WriteFile(pidFile, []byte(strconv.Itoa(currentPID)), 0644); err != nil {
		return fmt.Errorf("failed to write PID file: %v", err)
	}

	return nil
}
