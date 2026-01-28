package main

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
)

// dockerComposeCmd represents the detected docker compose command
type dockerComposeCmd struct {
	executable string   // "docker" or "docker-compose"
	args       []string // ["compose"] for v2, [] for v1
}

// getDockerComposeCmd detects available docker compose command
// Prefers "docker compose" (v2 plugin) over "docker-compose" (v1 standalone)
func getDockerComposeCmd() (*dockerComposeCmd, error) {
	// Try "docker compose" first (v2 plugin)
	if dockerPath, err := exec.LookPath("docker"); err == nil {
		cmd := exec.Command(dockerPath, "compose", "version")
		if err := cmd.Run(); err == nil {
			return &dockerComposeCmd{executable: dockerPath, args: []string{"compose"}}, nil
		}
	}

	// Fall back to "docker-compose" (v1 standalone or v2 compatibility wrapper)
	if composePath, err := exec.LookPath("docker-compose"); err == nil {
		return &dockerComposeCmd{executable: composePath, args: []string{}}, nil
	}

	return nil, fmt.Errorf("docker compose not found. Please install Docker Compose")
}

// buildArgs builds the full argument list for docker compose command
func (dc *dockerComposeCmd) buildArgs(composeArgs ...string) []string {
	args := make([]string, 0, len(dc.args)+len(composeArgs))
	args = append(args, dc.args...)
	args = append(args, composeArgs...)
	return args
}

// command creates an exec.Cmd for docker compose
func (dc *dockerComposeCmd) command(composeArgs ...string) *exec.Cmd {
	return exec.Command(dc.executable, dc.buildArgs(composeArgs...)...)
}

// execArgs returns arguments suitable for syscall.Exec (includes executable name as first arg)
func (dc *dockerComposeCmd) execArgs(composeArgs ...string) []string {
	args := []string{filepath.Base(dc.executable)}
	args = append(args, dc.args...)
	args = append(args, composeArgs...)
	return args
}

// handlePassthrough passes commands through to docker compose
func handlePassthrough(command string, args []string) {
	projectDir, remainingArgs := extractProjectDirectory(args)

	absPath, err := filepath.Abs(projectDir)
	if err != nil {
		log.Fatalf("Failed to resolve path: %v", err)
	}

	// Get metadata directory
	sweDir, err := getMetadataDir(absPath)
	if err != nil {
		log.Fatalf("Failed to compute metadata directory: %v", err)
	}

	// Check if metadata directory exists
	if _, err := os.Stat(sweDir); os.IsNotExist(err) {
		log.Fatalf("Project not initialized at %q. Run: swe-swe init --project-directory %s\nView projects: swe-swe list", absPath, absPath)
	}

	// Check if docker compose is available
	dc, err := getDockerComposeCmd()
	if err != nil {
		log.Fatalf("%v", err)
	}

	composeFile := filepath.Join(sweDir, "docker-compose.yml")

	// Prepare environment variables
	// Filter out certificate-related env vars to prevent host paths leaking into containers
	var env []string
	for _, envVar := range os.Environ() {
		if strings.HasPrefix(envVar, "NODE_EXTRA_CA_CERTS=") ||
			strings.HasPrefix(envVar, "SSL_CERT_FILE=") ||
			strings.HasPrefix(envVar, "NODE_EXTRA_CA_CERTS_BUNDLE=") {
			continue
		}
		env = append(env, envVar)
	}

	// Add WORKSPACE_DIR if not already set
	workspaceSet := false
	for _, envVar := range env {
		if strings.HasPrefix(envVar, "WORKSPACE_DIR=") {
			workspaceSet = true
			break
		}
	}
	if !workspaceSet {
		env = append(env, fmt.Sprintf("WORKSPACE_DIR=%s", absPath))
	}

	// Set default for SWE_SWE_PASSWORD if not already set
	if os.Getenv("SWE_SWE_PASSWORD") == "" {
		env = append(env, "SWE_SWE_PASSWORD=changeme")
	}

	// Build arguments for docker compose
	// docker compose -f <file> <command> <args...>
	// Note: We intentionally don't pass --project-directory to docker compose.
	// Our docker-compose.yml uses relative paths (e.g., ./auth) for build contexts,
	// which docker compose resolves relative to the compose file location (metadata dir).
	// Passing --project-directory would cause docker to look for these paths relative
	// to the project dir instead, causing "path not found" errors.
	composeArgs := []string{"-f", composeFile, command}
	composeArgs = append(composeArgs, remainingArgs...)

	// Replace process with docker compose on Unix/Linux/macOS
	if runtime.GOOS != "windows" {
		execArgs := dc.execArgs(composeArgs...)
		if err := syscall.Exec(dc.executable, execArgs, env); err != nil {
			log.Fatalf("Failed to exec docker compose: %v", err)
		}
	} else {
		// Windows fallback: use subprocess with signal forwarding
		cmd := dc.command(composeArgs...)
		cmd.Env = env
		cmd.Stdin = os.Stdin
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr

		sigChan := make(chan os.Signal, 1)
		signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

		go func() {
			sig := <-sigChan
			if cmd.Process != nil {
				cmd.Process.Signal(sig)
			}
		}()

		if err := cmd.Run(); err != nil {
			os.Exit(1)
		}
	}
}
