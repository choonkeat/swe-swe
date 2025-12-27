package main

import (
	"crypto/md5"
	"embed"
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"syscall"
)

//go:embed all:templates bin/*
var assets embed.FS

// splitAtDoubleDash splits args at "--" separator
// Returns (beforeArgs, afterArgs) where afterArgs are passed through to docker-compose
func splitAtDoubleDash(args []string) ([]string, []string) {
	for i, arg := range args {
		if arg == "--" {
			return args[:i], args[i+1:]
		}
	}
	return args, nil
}

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	command := os.Args[1]

	switch command {
	case "init":
		handleInit()
	case "up":
		handleUp()
	case "down":
		handleDown()
	case "build":
		handleBuild()
	case "update":
		handleUpdate()
	case "list":
		handleList()
	case "help":
		printUsage()
	default:
		fmt.Fprintf(os.Stderr, "Unknown command: %s\n", command)
		printUsage()
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Fprintf(os.Stderr, `Usage: swe-swe <command> [options] [services...] [-- docker-compose-args...]

Commands:
  init [--path PATH]                     Initialize a new swe-swe project
  up [--path PATH] [services...]         Start the swe-swe environment (or specific services)
  down [--path PATH] [services...]       Stop the swe-swe environment (or specific services)
  build [--path PATH] [services...]      Rebuild Docker images (fresh build, no cache)
  update [--path PATH]                   Update swe-swe-server binary in existing project
  list                                   List all initialized swe-swe projects (auto-prunes missing paths)
  help                                   Show this help message

Services (defined in docker-compose.yml):
  swe-swe, vscode, chrome, traefik

Examples:
  swe-swe up                             Start all services
  swe-swe up chrome                      Start only chrome (and dependencies)
  swe-swe down chrome                    Stop only chrome
  swe-swe build chrome                   Rebuild only chrome image
  swe-swe down -- --remove-orphans       Pass args to docker-compose

Environment Variables:
  SWE_SWE_PASSWORD                       VSCode password (defaults to changeme)
`)
}

func handleInit() {
	fs := flag.NewFlagSet("init", flag.ExitOnError)
	path := fs.String("path", ".", "Path to initialize")
	updateBinaryOnly := fs.Bool("update-binary-only", false, "Update only the binary, skip templates (for existing projects)")
	fs.Parse(os.Args[2:])

	if *path == "" {
		*path = "."
	}

	// Create directory if it doesn't exist
	absPath, err := filepath.Abs(*path)
	if err != nil {
		log.Fatalf("Failed to resolve path: %v", err)
	}

	if err := os.MkdirAll(absPath, 0755); err != nil {
		log.Fatalf("Failed to create directory %q: %v", absPath, err)
	}

	// Get metadata directory in $HOME/.swe-swe/projects/
	sweDir, err := getMetadataDir(absPath)
	if err != nil {
		log.Fatalf("Failed to compute metadata directory: %v", err)
	}

	if err := os.MkdirAll(sweDir, 0755); err != nil {
		log.Fatalf("Failed to create metadata directory: %v", err)
	}

	// Create bin, home, and certs subdirectories
	binDir := filepath.Join(sweDir, "bin")
	homeDir := filepath.Join(sweDir, "home")
	certsDir := filepath.Join(sweDir, "certs")
	for _, dir := range []string{binDir, homeDir, certsDir} {
		if err := os.MkdirAll(dir, 0755); err != nil {
			log.Fatalf("Failed to create directory %q: %v", dir, err)
		}
	}

	// Write .path file to record the project path
	pathFile := filepath.Join(sweDir, ".path")
	if err := os.WriteFile(pathFile, []byte(absPath), 0644); err != nil {
		log.Fatalf("Failed to write path file: %v", err)
	}

	// Skip template extraction if --update-binary-only flag is set
	if !*updateBinaryOnly {
		// Extract embedded files
		// Files that go to metadata directory (~/.swe-swe/projects/<path>/)
		hostFiles := []string{
			"templates/host/Dockerfile",
			"templates/host/docker-compose.yml",
			"templates/host/traefik-dynamic.yml",
			"templates/host/entrypoint.sh",
			"templates/host/chrome/Dockerfile",
			"templates/host/chrome/supervisord.conf",
			"templates/host/chrome/entrypoint.sh",
			"templates/host/chrome/nginx-cdp.conf",
		}

		// Files that go to project directory (accessible by Claude in container)
		// Note: .mcp.json must be at project root, not .claude/mcp.json
		containerFiles := []string{
			"templates/container/.mcp.json",
			"templates/container/.swe-swe/browser-automation.md",
		}

		for _, hostFile := range hostFiles {
			content, err := assets.ReadFile(hostFile)
			if err != nil {
				log.Fatalf("Failed to read embedded file %q: %v", hostFile, err)
			}

			// Calculate destination path, preserving subdirectories
			relPath := strings.TrimPrefix(hostFile, "templates/host/")
			destPath := filepath.Join(sweDir, relPath)

			// Create parent directories if needed
			destDir := filepath.Dir(destPath)
			if err := os.MkdirAll(destDir, os.FileMode(0755)); err != nil {
				log.Fatalf("Failed to create directory %q: %v", destDir, err)
			}

			// entrypoint.sh should be executable
			fileMode := os.FileMode(0644)
			if filepath.Base(hostFile) == "entrypoint.sh" {
				fileMode = os.FileMode(0755)
			}

			if err := os.WriteFile(destPath, content, fileMode); err != nil {
				log.Fatalf("Failed to write %q: %v", destPath, err)
			}
			fmt.Printf("Created %s\n", destPath)
		}

		// Extract container files (go to project directory, accessible by Claude)
		for _, containerFile := range containerFiles {
			content, err := assets.ReadFile(containerFile)
			if err != nil {
				log.Fatalf("Failed to read embedded file %q: %v", containerFile, err)
			}

			// Calculate destination path in project directory
			relPath := strings.TrimPrefix(containerFile, "templates/container/")
			destPath := filepath.Join(absPath, relPath)

			// Create parent directories if needed
			destDir := filepath.Dir(destPath)
			if err := os.MkdirAll(destDir, os.FileMode(0755)); err != nil {
				log.Fatalf("Failed to create directory %q: %v", destDir, err)
			}

			if err := os.WriteFile(destPath, content, 0644); err != nil {
				log.Fatalf("Failed to write %q: %v", destPath, err)
			}
			fmt.Printf("Created %s\n", destPath)
		}

		// Handle enterprise certificates
		handleCertificates(sweDir, certsDir)
	} else {
		fmt.Printf("Skipping templates (--update-binary-only mode)\n")
	}

	// Extract swe-swe-server binary from embedded assets
	// For Docker container (always Linux)
	arch := runtime.GOARCH
	if arch == "amd64" {
		arch = "amd64"
	} else if arch == "arm64" {
		arch = "arm64"
	} else {
		arch = "amd64" // fallback to amd64
	}

	// Try to extract the binary for this architecture, fallback to amd64
	embeddedBinaryPath := fmt.Sprintf("bin/swe-swe-server.linux-%s", arch)
	binaryData, err := assets.ReadFile(embeddedBinaryPath)
	if err != nil {
		if arch != "amd64" {
			// Try amd64 fallback
			embeddedBinaryPath = "bin/swe-swe-server.linux-amd64"
			binaryData, err = assets.ReadFile(embeddedBinaryPath)
		}
		if err != nil {
			log.Fatalf("Failed to find swe-swe-server binary in embedded assets: %v", err)
		}
	}

	// Write binary to metadata/bin/swe-swe-server
	serverPath := filepath.Join(binDir, "swe-swe-server")
	if err := os.WriteFile(serverPath, binaryData, 0755); err != nil {
		log.Fatalf("Failed to extract swe-swe-server binary: %v", err)
	}
	fmt.Printf("Extracted %s\n", serverPath)

	fmt.Printf("\nInitialized swe-swe project at %s\n", absPath)
	fmt.Printf("View all projects: swe-swe list\n")
	fmt.Printf("Next: cd %s && swe-swe up\n", absPath)
}

func handleUp() {
	fs := flag.NewFlagSet("up", flag.ExitOnError)
	path := fs.String("path", ".", "Path to run from")
	fs.Parse(os.Args[2:])

	if *path == "" {
		*path = "."
	}

	// Split at "--" for pass-through args to docker-compose
	// Service names are passed directly to docker-compose (no validation needed)
	services, passThrough := splitAtDoubleDash(fs.Args())

	absPath, err := filepath.Abs(*path)
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
		log.Fatalf("Project not initialized at %q. Run: swe-swe init --path %s\nView projects: swe-swe list", absPath, absPath)
	}

	// Check if docker-compose is available
	if _, err := exec.LookPath("docker-compose"); err != nil {
		if _, err := exec.LookPath("docker"); err != nil {
			log.Fatalf("Docker not found. Please install Docker and Docker Compose.")
		}
		log.Fatalf("docker-compose not found. Please install Docker Compose.")
	}

	// Default port for Traefik service
	port := 9899

	// Extract swe-swe-server binary from embedded assets to .swe-swe/bin/
	binDir := filepath.Join(sweDir, "bin")
	if err := os.MkdirAll(binDir, 0755); err != nil {
		log.Fatalf("Failed to create bin directory: %v", err)
	}

	// Always use Linux binary for Docker container
	// Match architecture of the host machine (for cross-compile compatibility)
	var embeddedPath string
	if runtime.GOARCH == "arm64" {
		embeddedPath = "bin/swe-swe-server.linux-arm64"
	} else {
		embeddedPath = "bin/swe-swe-server.linux-amd64"
	}

	// Extract binary from embedded assets
	binaryData, err := assets.ReadFile(embeddedPath)
	if err != nil {
		log.Fatalf("Failed to extract swe-swe-server binary from assets: %v", err)
	}

	serverDest := filepath.Join(binDir, "swe-swe-server")
	if err := os.WriteFile(serverDest, binaryData, 0755); err != nil {
		log.Fatalf("Failed to write swe-swe-server binary: %v", err)
	}

	composeFile := filepath.Join(sweDir, "docker-compose.yml")
	if len(services) > 0 {
		fmt.Printf("Starting services %v at %s\n", services, absPath)
	} else {
		fmt.Printf("Starting swe-swe environment at %s\n", absPath)
	}
	fmt.Printf("Access at: http://0.0.0.0:%d\n", port)

	// Prepare environment variables
	// Filter out certificate-related env vars to prevent host paths leaking into containers
	// The .env file in .swe-swe/ will set the correct container paths
	var env []string
	for _, envVar := range os.Environ() {
		// Skip certificate env vars that may have host paths
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
		if len(envVar) > 13 && envVar[:13] == "WORKSPACE_DIR" {
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

	// Locate docker-compose executable
	executable, err := exec.LookPath("docker-compose")
	if err != nil {
		log.Fatalf("docker-compose not found: %v", err)
	}

	// Build arguments for docker-compose
	args := []string{"docker-compose", "-f", composeFile, "up"}
	args = append(args, passThrough...)
	args = append(args, services...)

	// Replace process with docker-compose on Unix/Linux/macOS
	if runtime.GOOS != "windows" {
		// syscall.Exec replaces the current process with docker-compose
		// Signals (Ctrl+C) go directly to docker-compose
		// This process never returns
		if err := syscall.Exec(executable, args, env); err != nil {
			log.Fatalf("Failed to exec docker-compose: %v", err)
		}
	} else {
		// Windows fallback: use subprocess with signal forwarding
		// since syscall.Exec is not available on Windows
		runDockerComposeWindows(executable, args, env)
	}
}

// runDockerComposeWindows runs docker-compose as a subprocess with signal forwarding
// This is used on Windows where syscall.Exec is not available
func runDockerComposeWindows(executable string, args []string, env []string) {
	cmd := exec.Command(executable, args[1:]...)
	cmd.Env = env
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	// Forward signals to subprocess
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		sig := <-sigChan
		if cmd.Process != nil {
			cmd.Process.Signal(sig)
		}
	}()

	if err := cmd.Run(); err != nil {
		// Ignore interrupt errors from signal forwarding (expected on Ctrl+C)
		log.Fatalf("Failed to run docker-compose: %v", err)
	}
}

func handleDown() {
	fs := flag.NewFlagSet("down", flag.ExitOnError)
	path := fs.String("path", ".", "Path to stop from")
	fs.Parse(os.Args[2:])

	if *path == "" {
		*path = "."
	}

	// Split at "--" for pass-through args to docker-compose
	// Service names are passed directly to docker-compose (no validation needed)
	services, passThrough := splitAtDoubleDash(fs.Args())

	absPath, err := filepath.Abs(*path)
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
		log.Fatalf("Project not initialized at %q. Run: swe-swe init --path %s\nView projects: swe-swe list", absPath, absPath)
	}

	// Check if docker-compose is available
	if _, err := exec.LookPath("docker-compose"); err != nil {
		if _, err := exec.LookPath("docker"); err != nil {
			log.Fatalf("Docker not found. Please install Docker and Docker Compose.")
		}
		log.Fatalf("docker-compose not found. Please install Docker Compose.")
	}

	composeFile := filepath.Join(sweDir, "docker-compose.yml")

	// Build command args
	args := []string{"-f", composeFile}
	if len(services) > 0 {
		// Use "stop" + "rm" for specific services (down doesn't support service targeting)
		fmt.Printf("Stopping services %v at %s\n", services, absPath)
		stopArgs := append(args, "stop")
		stopArgs = append(stopArgs, passThrough...)
		stopArgs = append(stopArgs, services...)
		cmd := exec.Command("docker-compose", stopArgs...)
		cmd.Stdin = os.Stdin
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			log.Fatalf("Failed to stop services: %v", err)
		}

		// Remove stopped containers
		rmArgs := append(args, "rm", "-f")
		rmArgs = append(rmArgs, services...)
		cmd = exec.Command("docker-compose", rmArgs...)
		cmd.Stdin = os.Stdin
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			log.Fatalf("Failed to remove containers: %v", err)
		}
		fmt.Printf("Stopped services %v at %s\n", services, absPath)
	} else {
		fmt.Printf("Stopping swe-swe environment at %s\n", absPath)
		args = append(args, "down")
		args = append(args, passThrough...)
		cmd := exec.Command("docker-compose", args...)
		cmd.Stdin = os.Stdin
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			log.Fatalf("Failed to stop docker-compose: %v", err)
		}
		fmt.Printf("Stopped swe-swe environment at %s\n", absPath)
	}
}

func handleBuild() {
	fs := flag.NewFlagSet("build", flag.ExitOnError)
	path := fs.String("path", ".", "Path to build from")
	fs.Parse(os.Args[2:])

	if *path == "" {
		*path = "."
	}

	// Split at "--" for pass-through args to docker-compose
	// Service names are passed directly to docker-compose (no validation needed)
	services, passThrough := splitAtDoubleDash(fs.Args())

	absPath, err := filepath.Abs(*path)
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
		log.Fatalf("Project not initialized at %q. Run: swe-swe init --path %s\nView projects: swe-swe list", absPath, absPath)
	}

	// Check if docker-compose is available
	if _, err := exec.LookPath("docker-compose"); err != nil {
		if _, err := exec.LookPath("docker"); err != nil {
			log.Fatalf("Docker not found. Please install Docker and Docker Compose.")
		}
		log.Fatalf("docker-compose not found. Please install Docker Compose.")
	}

	composeFile := filepath.Join(sweDir, "docker-compose.yml")

	// Build command args (--no-cache by default, can be overridden via passThrough)
	args := []string{"-f", composeFile, "build", "--no-cache"}
	args = append(args, passThrough...)
	if len(services) > 0 {
		fmt.Printf("Building services %v at %s (fresh build, no cache)\n", services, absPath)
		args = append(args, services...)
	} else {
		fmt.Printf("Building swe-swe environment at %s (fresh build, no cache)\n", absPath)
	}

	cmd := exec.Command("docker-compose", args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		log.Fatalf("Failed to build docker-compose: %v", err)
	}

	if len(services) > 0 {
		fmt.Printf("Successfully built services %v at %s\n", services, absPath)
	} else {
		fmt.Printf("Successfully built swe-swe environment at %s\n", absPath)
	}
}

// handleCertificates detects and copies enterprise certificates for Docker builds
// Supports NODE_EXTRA_CA_CERTS and SSL_CERT_FILE environment variables
// for users behind corporate firewalls or VPNs (Cloudflare Warp, etc)
func handleCertificates(sweDir, certsDir string) {
	// Check for certificate environment variables
	certEnvVars := []string{
		"NODE_EXTRA_CA_CERTS",
		"SSL_CERT_FILE",
		"NODE_EXTRA_CA_CERTS_BUNDLE",
	}

	var certPaths []string
	envFileContent := ""

	for _, envVar := range certEnvVars {
		certPath := os.Getenv(envVar)
		if certPath == "" {
			continue
		}

		// Verify certificate file exists
		_, err := os.Stat(certPath)
		if err != nil {
			if os.IsNotExist(err) {
				fmt.Printf("Warning: %s points to non-existent file: %s (ignoring)\n", envVar, certPath)
			} else {
				fmt.Printf("Warning: Could not access %s=%s: %v (ignoring)\n", envVar, certPath, err)
			}
			continue
		}

		// Copy certificate file to .swe-swe/certs/
		certFilename := filepath.Base(certPath)
		destCertPath := filepath.Join(certsDir, certFilename)

		if err := copyFile(certPath, destCertPath); err != nil {
			fmt.Printf("Warning: Failed to copy certificate %s: %v (ignoring)\n", certPath, err)
			continue
		}

		fmt.Printf("Copied enterprise certificate: %s → %s\n", certPath, destCertPath)

		// Track certificate for .env file
		certPaths = append(certPaths, certFilename)
		envFileContent += fmt.Sprintf("%s=/swe-swe/certs/%s\n", envVar, certFilename)
	}

	// Create .env file if certificates were found
	if len(certPaths) > 0 {
		envFilePath := filepath.Join(sweDir, ".env")
		if err := os.WriteFile(envFilePath, []byte(envFileContent), 0644); err != nil {
			fmt.Printf("Warning: Failed to create .env file: %v\n", err)
			return
		}
		fmt.Printf("Created %s with certificate configuration\n", envFilePath)
	}
}

// copyFile copies source to destination
func copyFile(src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, data, 0644)
}

func handleUpdate() {
	fs := flag.NewFlagSet("update", flag.ExitOnError)
	path := fs.String("path", ".", "Path to update")
	fs.Parse(os.Args[2:])

	if *path == "" {
		*path = "."
	}

	absPath, err := filepath.Abs(*path)
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
		log.Fatalf("Project not initialized at %q. Run: swe-swe init --path %s\nView projects: swe-swe list", absPath, absPath)
	}

	binDir := filepath.Join(sweDir, "bin")
	projectBinaryPath := filepath.Join(binDir, "swe-swe-server")

	// Get CLI binary path
	cliExePath, err := os.Executable()
	if err != nil {
		log.Fatalf("Failed to get CLI executable path: %v", err)
	}

	// Determine which architecture to use (same logic as init)
	arch := runtime.GOARCH
	if arch == "amd64" {
		arch = "amd64"
	} else if arch == "arm64" {
		arch = "arm64"
	} else {
		arch = "amd64" // fallback to amd64
	}

	cliDir := filepath.Dir(cliExePath)
	cliBinaryPath := filepath.Join(cliDir, fmt.Sprintf("swe-swe-server.linux-%s", arch))

	// Check if the binary exists, fallback to amd64 if arm64 not found
	if _, err := os.Stat(cliBinaryPath); os.IsNotExist(err) {
		if arch != "amd64" {
			cliBinaryPath = filepath.Join(cliDir, "swe-swe-server.linux-amd64")
		}
		if _, err := os.Stat(cliBinaryPath); os.IsNotExist(err) {
			log.Fatalf("Failed to find swe-swe-server binary in %s", cliDir)
		}
	}

	// Compare versions
	needsUpdate, oldVersion, newVersion, err := compareBinaryVersions(cliBinaryPath, projectBinaryPath)
	if err != nil {
		log.Fatalf("Failed to compare versions: %v", err)
	}

	if !needsUpdate {
		fmt.Printf("swe-swe-server is already up to date (%s)\n", oldVersion)
		return
	}

	fmt.Printf("Updating swe-swe-server from %s to %s\n", oldVersion, newVersion)

	// Copy new binary to project
	if err := copyFile(cliBinaryPath, projectBinaryPath); err != nil {
		log.Fatalf("Failed to copy swe-swe-server binary: %v", err)
	}

	// Make it executable
	if err := os.Chmod(projectBinaryPath, 0755); err != nil {
		log.Fatalf("Failed to make binary executable: %v", err)
	}

	fmt.Printf("Successfully updated swe-swe-server\n")
	fmt.Printf("Run: swe-swe up --path %s\n", absPath)
}

// getBinaryVersion executes a binary with --version flag and returns the version string
// Returns "unknown" if version cannot be determined
func getBinaryVersion(binaryPath string) string {
	cmd := exec.Command(binaryPath, "--version")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "unknown"
	}
	version := strings.TrimSpace(string(output))
	if version == "" {
		return "unknown"
	}
	return version
}

// compareBinaryVersions compares versions of CLI binary and project binary
// Returns (needsUpdate, oldVersion, newVersion, error)
func compareBinaryVersions(cliBinaryPath, projectBinaryPath string) (bool, string, string, error) {
	// Get new version from CLI binary
	newVersion := getBinaryVersion(cliBinaryPath)

	// Get old version from project binary (only if it exists)
	oldVersion := "unknown"
	if _, err := os.Stat(projectBinaryPath); err == nil {
		oldVersion = getBinaryVersion(projectBinaryPath)
	}

	// For now, simple string comparison
	// If versions differ, we need an update
	needsUpdate := newVersion != oldVersion && newVersion != "unknown"

	return needsUpdate, oldVersion, newVersion, nil
}

// sanitizePath converts an absolute path into a sanitized directory name suitable
// for use under $HOME/.swe-swe/projects/. It replaces non-alphanumeric characters
// (except separators) with hyphens and appends an MD5 hash of the full absolute path.
// Example: /Users/alice/projects/my-app -> users-alice-projects-my-app-{md5-first-8-chars}
func sanitizePath(absPath string) string {
	// Replace path separators and non-alphanumeric chars with hyphens
	re := regexp.MustCompile(`[^a-zA-Z0-9]+`)
	sanitized := re.ReplaceAllString(absPath, "-")
	// Remove leading/trailing hyphens
	sanitized = strings.Trim(sanitized, "-")

	// Compute MD5 hash of absolute path
	hash := md5.Sum([]byte(absPath))
	hashStr := fmt.Sprintf("%x", hash)[:8] // First 8 chars of hex hash

	return fmt.Sprintf("%s-%s", sanitized, hashStr)
}

// getMetadataDir returns the metadata directory path for a given project absolute path.
// Metadata is stored in $HOME/.swe-swe/projects/{sanitized-path}/
func getMetadataDir(absPath string) (string, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to get home directory: %v", err)
	}

	sanitized := sanitizePath(absPath)
	return filepath.Join(homeDir, ".swe-swe", "projects", sanitized), nil
}

// handleList lists all initialized swe-swe projects and auto-prunes missing ones
func handleList() {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		log.Fatalf("Failed to get home directory: %v", err)
	}

	projectsDir := filepath.Join(homeDir, ".swe-swe", "projects")

	// Check if projects directory exists
	entries, err := os.ReadDir(projectsDir)
	if err != nil {
		if os.IsNotExist(err) {
			fmt.Println("No projects initialized yet")
			return
		}
		log.Fatalf("Failed to read projects directory: %v", err)
	}

	var activeProjects []string
	var prunedCount int

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		metadataDir := filepath.Join(projectsDir, entry.Name())
		pathFile := filepath.Join(metadataDir, ".path")

		// Read the .path file
		pathData, err := os.ReadFile(pathFile)
		if err != nil {
			// If .path file doesn't exist or can't be read, skip this entry
			if os.IsNotExist(err) {
				fmt.Printf("Warning: .path file missing in %s (skipping)\n", entry.Name())
			}
			continue
		}

		projectPath := string(pathData)

		// Check if the original path still exists
		if _, err := os.Stat(projectPath); os.IsNotExist(err) {
			// Project path no longer exists, remove metadata directory
			if err := os.RemoveAll(metadataDir); err != nil {
				fmt.Printf("Warning: Failed to remove stale metadata directory %s: %v\n", metadataDir, err)
			}
			prunedCount++
		} else {
			// Project path exists, add to active list
			activeProjects = append(activeProjects, projectPath)
		}
	}

	// Display active projects
	if len(activeProjects) == 0 {
		fmt.Println("No projects initialized yet")
	} else {
		fmt.Printf("Initialized projects (%d):\n", len(activeProjects))
		for _, projectPath := range activeProjects {
			fmt.Printf("  %s\n", projectPath)
		}
	}

	// Show pruning summary if any projects were removed
	if prunedCount > 0 {
		fmt.Printf("\nRemoved %d stale project(s)\n", prunedCount)
	}
}
