package main

import (
	"fmt"
	"os"
)

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

	fmt.Printf("[proxy] Starting proxy for command: %s\n", command)
	fmt.Printf("[proxy] TODO: Implement file watching and command execution\n")

	// TODO: Phase 1 Steps 2-5 will implement:
	// - PID file management
	// - fsnotify watcher setup
	// - Request processing
	// - Graceful shutdown
}
