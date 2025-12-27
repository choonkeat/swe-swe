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

//go:embed templates/* bin/*
var assets embed.FS

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
	case "help":
		printUsage()
	default:
		fmt.Fprintf(os.Stderr, "Unknown command: %s\n", command)
		printUsage()
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Fprintf(os.Stderr, `Usage: swe-swe <command> [options]

Commands:
  init [--path PATH]                     Initialize a new swe-swe project
  up [--path PATH]                       Start the swe-swe environment at PATH (defaults to current directory)
  down [--path PATH]                     Stop the swe-swe environment at PATH (defaults to current directory)
  build [--path PATH]                    Rebuild the swe-swe Docker images (fresh build, no cache)
  update [--path PATH]                   Update swe-swe-server binary in existing project
  help                                   Show this help message

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

	// Create .swe-swe directory
	sweDir := filepath.Join(absPath, ".swe-swe")
	if err := os.MkdirAll(sweDir, 0755); err != nil {
		log.Fatalf("Failed to create .swe-swe directory: %v", err)
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

	// Skip template extraction if --update-binary-only flag is set
	if !*updateBinaryOnly {
		// Extract embedded files
		templateFiles := []string{
			"templates/Dockerfile",
			"templates/docker-compose.yml",
			"templates/traefik-dynamic.yml",
			"templates/entrypoint.sh",
		}

		for _, templateFile := range templateFiles {
			content, err := assets.ReadFile(templateFile)
			if err != nil {
				log.Fatalf("Failed to read embedded file %q: %v", templateFile, err)
			}

			filename := filepath.Base(templateFile)
			destPath := filepath.Join(sweDir, filename)

			// entrypoint.sh should be executable
			fileMode := os.FileMode(0644)
			if filename == "entrypoint.sh" {
				fileMode = os.FileMode(0755)
			}

			if err := os.WriteFile(destPath, content, fileMode); err != nil {
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

	// Write binary to .swe-swe/bin/swe-swe-server
	serverPath := filepath.Join(binDir, "swe-swe-server")
	if err := os.WriteFile(serverPath, binaryData, 0755); err != nil {
		log.Fatalf("Failed to extract swe-swe-server binary: %v", err)
	}
	fmt.Printf("Extracted %s\n", serverPath)

	fmt.Printf("\nInitialized swe-swe project at %s\n", absPath)
	fmt.Printf("Next: cd %s && swe-swe up\n", absPath)
}

func handleUp() {
	fs := flag.NewFlagSet("up", flag.ExitOnError)
	path := fs.String("path", ".", "Path to run from")
	fs.Parse(os.Args[2:])

	if *path == "" {
		*path = "."
	}

	absPath, err := filepath.Abs(*path)
	if err != nil {
		log.Fatalf("Failed to resolve path: %v", err)
	}

	// Check if .swe-swe directory exists
	sweDir := filepath.Join(absPath, ".swe-swe")
	if _, err := os.Stat(sweDir); os.IsNotExist(err) {
		log.Fatalf("Project not initialized at %q. Run: swe-swe init --path %s", absPath, absPath)
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
	fmt.Printf("Starting swe-swe environment at %s\n", absPath)
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

	absPath, err := filepath.Abs(*path)
	if err != nil {
		log.Fatalf("Failed to resolve path: %v", err)
	}

	// Check if .swe-swe directory exists
	sweDir := filepath.Join(absPath, ".swe-swe")
	if _, err := os.Stat(sweDir); os.IsNotExist(err) {
		log.Fatalf("Project not initialized at %q. Run: swe-swe init --path %s", absPath, absPath)
	}

	// Check if docker-compose is available
	if _, err := exec.LookPath("docker-compose"); err != nil {
		if _, err := exec.LookPath("docker"); err != nil {
			log.Fatalf("Docker not found. Please install Docker and Docker Compose.")
		}
		log.Fatalf("docker-compose not found. Please install Docker Compose.")
	}

	composeFile := filepath.Join(sweDir, "docker-compose.yml")
	fmt.Printf("Stopping swe-swe environment at %s\n", absPath)

	// Run docker-compose down
	cmd := exec.Command("docker-compose", "-f", composeFile, "down")
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		log.Fatalf("Failed to stop docker-compose: %v", err)
	}

	fmt.Printf("Stopped swe-swe environment at %s\n", absPath)
}

func handleBuild() {
	fs := flag.NewFlagSet("build", flag.ExitOnError)
	path := fs.String("path", ".", "Path to build from")
	fs.Parse(os.Args[2:])

	if *path == "" {
		*path = "."
	}

	absPath, err := filepath.Abs(*path)
	if err != nil {
		log.Fatalf("Failed to resolve path: %v", err)
	}

	// Check if .swe-swe directory exists
	sweDir := filepath.Join(absPath, ".swe-swe")
	if _, err := os.Stat(sweDir); os.IsNotExist(err) {
		log.Fatalf("Project not initialized at %q. Run: swe-swe init --path %s", absPath, absPath)
	}

	// Check if docker-compose is available
	if _, err := exec.LookPath("docker-compose"); err != nil {
		if _, err := exec.LookPath("docker"); err != nil {
			log.Fatalf("Docker not found. Please install Docker and Docker Compose.")
		}
		log.Fatalf("docker-compose not found. Please install Docker Compose.")
	}

	composeFile := filepath.Join(sweDir, "docker-compose.yml")
	fmt.Printf("Building swe-swe environment at %s (fresh build, no cache)\n", absPath)

	// Run docker-compose build with --no-cache
	cmd := exec.Command("docker-compose", "-f", composeFile, "build", "--no-cache")
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		log.Fatalf("Failed to build docker-compose: %v", err)
	}

	fmt.Printf("Successfully built swe-swe environment at %s\n", absPath)
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

	// Check if .swe-swe directory exists
	sweDir := filepath.Join(absPath, ".swe-swe")
	if _, err := os.Stat(sweDir); os.IsNotExist(err) {
		log.Fatalf("Project not initialized at %q. Run: swe-swe init --path %s", absPath, absPath)
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
