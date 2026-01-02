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

//go:embed all:templates
var assets embed.FS

// splitAtDoubleDash splits args at "--" separator
// Returns (beforeArgs, afterArgs) where afterArgs are passed through to docker compose
func splitAtDoubleDash(args []string) ([]string, []string) {
	for i, arg := range args {
		if arg == "--" {
			return args[:i], args[i+1:]
		}
	}
	return args, nil
}

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
	fmt.Fprintf(os.Stderr, `Usage: swe-swe <command> [options] [services...] [-- docker-args...]

Commands:
  init [options]                         Initialize a new swe-swe project
  up [--path PATH] [services...]         Start the swe-swe environment (or specific services)
  down [--path PATH] [services...]       Stop the swe-swe environment (or specific services)
  build [--path PATH] [services...]      Rebuild Docker images (fresh build, no cache)
  list                                   List all initialized swe-swe projects (auto-prunes missing paths)
  help                                   Show this help message

Init Options:
  --path PATH                            Project directory (defaults to current directory)
  --agents AGENTS                        Comma-separated agents: claude,gemini,codex,aider,goose (default: all)
  --exclude AGENTS                       Comma-separated agents to exclude
  --apt-get-install PACKAGES             Additional apt packages to install (comma or space separated)
  --npm-install PACKAGES                 Additional npm packages to install globally (comma or space separated)
  --list-agents                          List available agents and exit

Available Agents:
  claude   - Claude Code (requires Node.js)
  gemini   - Gemini CLI (requires Node.js)
  codex    - Codex CLI (requires Node.js)
  aider    - Aider (requires Python)
  goose    - Goose (standalone binary)

Services (defined in docker-compose.yml):
  swe-swe, vscode, chrome, traefik

Examples:
  swe-swe init                                   Initialize with all agents
  swe-swe init --agents=claude                   Initialize with Claude only (minimal)
  swe-swe init --agents=claude,gemini            Initialize with Claude and Gemini
  swe-swe init --exclude=aider,goose             Initialize without Aider and Goose
  swe-swe init --apt-get-install="vim htop"      Add custom apt packages
  swe-swe init --npm-install="typescript tsx"    Add custom npm packages
  swe-swe init --list-agents                     Show available agents
  swe-swe up                                     Start all services
  swe-swe up chrome                              Start only chrome (and dependencies)
  swe-swe down chrome                            Stop only chrome
  swe-swe build chrome                           Rebuild only chrome image
  swe-swe down -- --remove-orphans               Pass extra args to docker compose

Environment Variables:
  SWE_SWE_PASSWORD                       VSCode password (defaults to changeme)

Requires: Docker with Compose plugin (docker compose) or standalone docker-compose
`)
}

// allAgents lists all available AI agents that can be installed
var allAgents = []string{"claude", "gemini", "codex", "aider", "goose"}

// SlashCommandsRepo represents a git repository to clone for slash commands
type SlashCommandsRepo struct {
	Alias string // "ck" or derived "choonkeat/slash-commands"
	URL   string // "https://github.com/choonkeat/slash-commands.git"
}

// deriveAliasFromURL extracts owner/repo from a git URL
// e.g., "https://github.com/choonkeat/slash-commands.git" -> "choonkeat/slash-commands"
func deriveAliasFromURL(url string) (string, error) {
	if url == "" {
		return "", fmt.Errorf("empty URL")
	}

	// Remove .git suffix if present
	url = strings.TrimSuffix(url, ".git")

	// Find the last two path segments (owner/repo)
	// Works for: https://github.com/owner/repo, git@github.com:owner/repo, etc.
	parts := strings.Split(url, "/")
	if len(parts) < 2 {
		return "", fmt.Errorf("invalid git URL: cannot extract owner/repo from %q", url)
	}

	owner := parts[len(parts)-2]
	repo := parts[len(parts)-1]

	// Handle git@host:owner/repo format (owner might have : prefix)
	if strings.Contains(owner, ":") {
		ownerParts := strings.Split(owner, ":")
		owner = ownerParts[len(ownerParts)-1]
	}

	if owner == "" || repo == "" {
		return "", fmt.Errorf("invalid git URL: cannot extract owner/repo from %q", url)
	}

	return owner + "/" + repo, nil
}

// parseSlashCommandsEntry parses a single "[alias@]<git-url>" entry
func parseSlashCommandsEntry(entry string) (SlashCommandsRepo, error) {
	entry = strings.TrimSpace(entry)
	if entry == "" {
		return SlashCommandsRepo{}, fmt.Errorf("empty entry")
	}

	// Check for alias@url format
	// Only split on first @ to handle URLs that might contain @
	atIndex := strings.Index(entry, "@")
	if atIndex > 0 && !strings.HasPrefix(entry, "http") && !strings.HasPrefix(entry, "git@") {
		// Has alias prefix (e.g., "ck@https://...")
		alias := entry[:atIndex]
		url := entry[atIndex+1:]
		if url == "" {
			return SlashCommandsRepo{}, fmt.Errorf("empty URL after alias in %q", entry)
		}
		return SlashCommandsRepo{Alias: alias, URL: url}, nil
	}

	// No alias, derive from URL
	alias, err := deriveAliasFromURL(entry)
	if err != nil {
		return SlashCommandsRepo{}, err
	}
	return SlashCommandsRepo{Alias: alias, URL: entry}, nil
}

// parseSlashCommandsFlag parses the full --with-slash-commands flag value
// Format: "[alias@]<git-url> [alias@]<git-url> ..."
func parseSlashCommandsFlag(flag string) ([]SlashCommandsRepo, error) {
	flag = strings.TrimSpace(flag)
	if flag == "" {
		return nil, nil
	}

	// Split on whitespace
	entries := strings.Fields(flag)
	var repos []SlashCommandsRepo

	for _, entry := range entries {
		repo, err := parseSlashCommandsEntry(entry)
		if err != nil {
			return nil, fmt.Errorf("invalid slash commands entry %q: %v", entry, err)
		}
		repos = append(repos, repo)
	}

	return repos, nil
}

// parseAgentList parses a comma-separated agent list and validates agent names
func parseAgentList(input string) ([]string, error) {
	if input == "" {
		return nil, nil
	}
	parts := strings.Split(input, ",")
	var agents []string
	for _, p := range parts {
		agent := strings.TrimSpace(strings.ToLower(p))
		if agent == "" {
			continue
		}
		valid := false
		for _, a := range allAgents {
			if agent == a {
				valid = true
				break
			}
		}
		if !valid {
			return nil, fmt.Errorf("unknown agent %q (available: %s)", agent, strings.Join(allAgents, ", "))
		}
		agents = append(agents, agent)
	}
	return agents, nil
}

// resolveAgents computes the final agent list based on --agents and --exclude flags
func resolveAgents(agentsFlag, excludeFlag string) ([]string, error) {
	// Parse exclude list first
	excludeList, err := parseAgentList(excludeFlag)
	if err != nil {
		return nil, fmt.Errorf("--exclude: %v", err)
	}
	excludeSet := make(map[string]bool)
	for _, e := range excludeList {
		excludeSet[e] = true
	}

	// If --agents is specified, use that list (minus excludes)
	if agentsFlag != "" {
		if agentsFlag == "all" {
			// Start with all agents, apply excludes
			var result []string
			for _, a := range allAgents {
				if !excludeSet[a] {
					result = append(result, a)
				}
			}
			return result, nil
		}
		agentList, err := parseAgentList(agentsFlag)
		if err != nil {
			return nil, fmt.Errorf("--agents: %v", err)
		}
		var result []string
		for _, a := range agentList {
			if !excludeSet[a] {
				result = append(result, a)
			}
		}
		return result, nil
	}

	// Default: all agents minus excludes
	var result []string
	for _, a := range allAgents {
		if !excludeSet[a] {
			result = append(result, a)
		}
	}
	return result, nil
}

// agentInList checks if an agent is in the list
func agentInList(agent string, list []string) bool {
	for _, a := range list {
		if a == agent {
			return true
		}
	}
	return false
}

// processDockerfileTemplate processes the Dockerfile template with conditional sections
// based on selected agents, custom apt packages, custom npm packages, Docker access, and slash commands
func processDockerfileTemplate(content string, agents []string, aptPackages, npmPackages string, withDocker bool, slashCommands []SlashCommandsRepo) string {
	// Helper to check if agent is selected
	hasAgent := func(agent string) bool {
		return agentInList(agent, agents)
	}

	// Check if we need Python (only for aider)
	needsPython := hasAgent("aider")

	// Check if we need Node.js (claude, gemini, codex, or playwright)
	needsNodeJS := hasAgent("claude") || hasAgent("gemini") || hasAgent("codex")

	// Check if we have slash commands for supported agents (claude or codex)
	hasSlashCommands := len(slashCommands) > 0 && (hasAgent("claude") || hasAgent("codex"))

	// Generate slash commands clone lines
	var slashCommandsClone string
	if hasSlashCommands {
		var cloneLines []string
		for _, repo := range slashCommands {
			cloneLines = append(cloneLines, fmt.Sprintf("RUN git clone --depth 1 %s /tmp/slash-commands/%s", repo.URL, repo.Alias))
		}
		slashCommandsClone = strings.Join(cloneLines, "\n")
	}

	// Build the Dockerfile from template
	lines := strings.Split(content, "\n")
	var result []string
	skip := false

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		// Handle conditional markers
		if strings.HasPrefix(trimmed, "# {{IF ") && strings.HasSuffix(trimmed, "}}") {
			condition := strings.TrimSuffix(strings.TrimPrefix(trimmed, "# {{IF "), "}}")
			switch condition {
			case "PYTHON":
				skip = !needsPython
			case "NODEJS":
				skip = !needsNodeJS
			case "CLAUDE":
				skip = !hasAgent("claude")
			case "GEMINI":
				skip = !hasAgent("gemini")
			case "CODEX":
				skip = !hasAgent("codex")
			case "AIDER":
				skip = !hasAgent("aider")
			case "GOOSE":
				skip = !hasAgent("goose")
			case "APT_PACKAGES":
				skip = aptPackages == ""
			case "NPM_PACKAGES":
				skip = npmPackages == ""
			case "DOCKER":
				skip = !withDocker
			case "SLASH_COMMANDS":
				skip = !hasSlashCommands
			}
			continue
		}

		if trimmed == "# {{ENDIF}}" {
			skip = false
			continue
		}

		// Handle APT_PACKAGES placeholder
		if strings.Contains(line, "{{APT_PACKAGES}}") {
			if aptPackages != "" {
				line = strings.ReplaceAll(line, "{{APT_PACKAGES}}", aptPackages)
			}
		}

		// Handle NPM_PACKAGES placeholder
		if strings.Contains(line, "{{NPM_PACKAGES}}") {
			if npmPackages != "" {
				line = strings.ReplaceAll(line, "{{NPM_PACKAGES}}", npmPackages)
			}
		}

		// Handle SLASH_COMMANDS_CLONE placeholder
		if strings.Contains(line, "{{SLASH_COMMANDS_CLONE}}") {
			if slashCommandsClone != "" {
				line = strings.ReplaceAll(line, "{{SLASH_COMMANDS_CLONE}}", slashCommandsClone)
			}
		}

		if !skip {
			result = append(result, line)
		}
	}

	return strings.Join(result, "\n")
}

// processSimpleTemplate handles simple conditional templates with {{IF DOCKER}}...{{ENDIF}} blocks
// This is used for docker-compose.yml and entrypoint.sh which only need the DOCKER condition
func processSimpleTemplate(content string, withDocker bool) string {
	lines := strings.Split(content, "\n")
	var result []string
	skip := false

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		// Handle conditional markers (support both # and yaml-style comments)
		if strings.Contains(trimmed, "{{IF DOCKER}}") {
			skip = !withDocker
			continue
		}

		if strings.Contains(trimmed, "{{ENDIF}}") {
			skip = false
			continue
		}

		if !skip {
			result = append(result, line)
		}
	}

	return strings.Join(result, "\n")
}

func handleInit() {
	fs := flag.NewFlagSet("init", flag.ExitOnError)
	path := fs.String("path", ".", "Path to initialize")
	agentsFlag := fs.String("agents", "", "Comma-separated list of agents to include (claude,gemini,codex,aider,goose) or 'all'")
	excludeFlag := fs.String("exclude", "", "Comma-separated list of agents to exclude")
	aptPackages := fs.String("apt-get-install", "", "Additional packages to install via apt-get (comma-separated)")
	npmPackages := fs.String("npm-install", "", "Additional packages to install via npm (comma-separated)")
	withDocker := fs.Bool("with-docker", false, "Mount Docker socket to allow container to run Docker commands on host")
	slashCommands := fs.String("with-slash-commands", "", "Git repos to clone as slash commands (space-separated, format: [alias@]<git-url>)")
	listAgents := fs.Bool("list-agents", false, "List available agents and exit")
	fs.Parse(os.Args[2:])

	// Handle --list-agents
	if *listAgents {
		fmt.Println("Available agents:")
		fmt.Println("  claude  - Claude Code (Node.js)")
		fmt.Println("  gemini  - Gemini CLI (Node.js)")
		fmt.Println("  codex   - Codex CLI (Node.js)")
		fmt.Println("  aider   - Aider (Python)")
		fmt.Println("  goose   - Goose (Go binary)")
		return
	}

	if *path == "" {
		*path = "."
	}

	// Resolve agent list
	agents, err := resolveAgents(*agentsFlag, *excludeFlag)
	if err != nil {
		log.Fatalf("Failed to resolve agents: %v", err)
	}

	// Normalize apt packages (replace commas with spaces for apt-get)
	aptPkgs := strings.ReplaceAll(*aptPackages, ",", " ")
	aptPkgs = strings.TrimSpace(aptPkgs)

	// Normalize npm packages (replace commas with spaces for npm install)
	npmPkgs := strings.ReplaceAll(*npmPackages, ",", " ")
	npmPkgs = strings.TrimSpace(npmPkgs)

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

	// Set home directory ownership to UID 1000 (code-server user) recursively.
	// This is needed when running as root on Linux, otherwise code-server
	// cannot write to its home directory. Errors are ignored because this
	// only works when running as root on Linux.
	filepath.Walk(homeDir, func(path string, info os.FileInfo, err error) error {
		if err == nil {
			os.Chown(path, 1000, 1000)
		}
		return nil
	})

	// Write .path file to record the project path
	pathFile := filepath.Join(sweDir, ".path")
	if err := os.WriteFile(pathFile, []byte(absPath), 0644); err != nil {
		log.Fatalf("Failed to write path file: %v", err)
	}

	// Extract embedded files
	// Files that go to metadata directory (~/.swe-swe/projects/<path>/)
	hostFiles := []string{
			"templates/host/Dockerfile",
			"templates/host/docker-compose.yml",
			"templates/host/traefik-dynamic.yml",
			"templates/host/entrypoint.sh",
			"templates/host/nginx-vscode.conf",
			"templates/host/chrome/Dockerfile",
			"templates/host/chrome/supervisord.conf",
			"templates/host/chrome/entrypoint.sh",
			"templates/host/chrome/nginx-cdp.conf",
			"templates/host/chrome/novnc-wrapper.html",
			"templates/host/auth/Dockerfile",
			"templates/host/auth/go.mod.txt",
			"templates/host/auth/main.go",
			"templates/host/swe-swe-server/go.mod.txt",
			"templates/host/swe-swe-server/go.sum.txt",
			"templates/host/swe-swe-server/main.go",
			"templates/host/swe-swe-server/static/index.html",
			"templates/host/swe-swe-server/static/selection.html",
			"templates/host/swe-swe-server/static/terminal-ui.js",
			"templates/host/swe-swe-server/static/xterm-addon-fit.js",
			"templates/host/swe-swe-server/static/xterm.css",
			"templates/host/swe-swe-server/static/xterm.js",
		}

		// Files that go to project directory (accessible by Claude in container)
		// Note: .mcp.json must be at project root, not .claude/mcp.json
		containerFiles := []string{
			"templates/container/.mcp.json",
			"templates/container/.swe-swe/browser-automation.md",
		}

		// Print selected agents
		if len(agents) > 0 {
			fmt.Printf("Selected agents: %s\n", strings.Join(agents, ", "))
		} else {
			fmt.Println("No agents selected (minimal environment)")
		}
		if aptPkgs != "" {
			fmt.Printf("Additional apt packages: %s\n", aptPkgs)
		}
		if npmPkgs != "" {
			fmt.Printf("Additional npm packages: %s\n", npmPkgs)
		}
		if *withDocker {
			fmt.Println("Docker access: enabled (container can run Docker commands on host)")
		}

		// Parse slash commands flag
		var slashCmds []SlashCommandsRepo
		if *slashCommands != "" {
			var err error
			slashCmds, err = parseSlashCommandsFlag(*slashCommands)
			if err != nil {
				log.Fatalf("Invalid --with-slash-commands value: %v", err)
			}
			for _, repo := range slashCmds {
				fmt.Printf("Slash commands: %s -> /tmp/slash-commands/%s\n", repo.URL, repo.Alias)
			}
		}

		for _, hostFile := range hostFiles {
			content, err := assets.ReadFile(hostFile)
			if err != nil {
				log.Fatalf("Failed to read embedded file %q: %v", hostFile, err)
			}

			// Process Dockerfile template with conditional sections
			if hostFile == "templates/host/Dockerfile" {
				content = []byte(processDockerfileTemplate(string(content), agents, aptPkgs, npmPkgs, *withDocker, slashCmds))
			}

			// Process docker-compose.yml template with conditional sections
			if hostFile == "templates/host/docker-compose.yml" {
				content = []byte(processSimpleTemplate(string(content), *withDocker))
			}

			// Process entrypoint.sh template with conditional sections
			if hostFile == "templates/host/entrypoint.sh" {
				content = []byte(processSimpleTemplate(string(content), *withDocker))
			}

			// Calculate destination path, preserving subdirectories
			relPath := strings.TrimPrefix(hostFile, "templates/host/")
			// Rename go.mod.txt and go.sum.txt back to go.mod/go.sum
			// (workaround for go:embed excluding directories with go.mod files)
			if strings.HasSuffix(relPath, "go.mod.txt") || strings.HasSuffix(relPath, "go.sum.txt") {
				relPath = strings.TrimSuffix(relPath, ".txt")
			}
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

	// Split at "--" for pass-through args to docker compose
	// Service names are passed directly to docker compose (no validation needed)
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

	// Check if docker compose is available
	dc, err := getDockerComposeCmd()
	if err != nil {
		log.Fatalf("%v", err)
	}

	// Default port for Traefik service
	port := 9899

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

	// Build arguments for docker compose
	composeArgs := []string{"-f", composeFile, "up"}
	composeArgs = append(composeArgs, passThrough...)
	composeArgs = append(composeArgs, services...)

	// Replace process with docker compose on Unix/Linux/macOS
	if runtime.GOOS != "windows" {
		// syscall.Exec replaces the current process with docker compose
		// Signals (Ctrl+C) go directly to docker compose
		// This process never returns
		args := dc.execArgs(composeArgs...)
		if err := syscall.Exec(dc.executable, args, env); err != nil {
			log.Fatalf("Failed to exec docker compose: %v", err)
		}
	} else {
		// Windows fallback: use subprocess with signal forwarding
		// since syscall.Exec is not available on Windows
		runDockerComposeWindows(dc, composeArgs, env)
	}
}

// runDockerComposeWindows runs docker compose as a subprocess with signal forwarding
// This is used on Windows where syscall.Exec is not available
func runDockerComposeWindows(dc *dockerComposeCmd, composeArgs []string, env []string) {
	cmd := dc.command(composeArgs...)
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
		log.Fatalf("Failed to run docker compose: %v", err)
	}
}

func handleDown() {
	fs := flag.NewFlagSet("down", flag.ExitOnError)
	path := fs.String("path", ".", "Path to stop from")
	fs.Parse(os.Args[2:])

	if *path == "" {
		*path = "."
	}

	// Split at "--" for pass-through args to docker compose
	// Service names are passed directly to docker compose (no validation needed)
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

	// Check if docker compose is available
	dc, err := getDockerComposeCmd()
	if err != nil {
		log.Fatalf("%v", err)
	}

	composeFile := filepath.Join(sweDir, "docker-compose.yml")

	// Build command args
	baseArgs := []string{"-f", composeFile}
	if len(services) > 0 {
		// Use "stop" + "rm" for specific services (down doesn't support service targeting)
		fmt.Printf("Stopping services %v at %s\n", services, absPath)
		stopArgs := append(baseArgs, "stop")
		stopArgs = append(stopArgs, passThrough...)
		stopArgs = append(stopArgs, services...)
		cmd := dc.command(stopArgs...)
		cmd.Stdin = os.Stdin
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			log.Fatalf("Failed to stop services: %v", err)
		}

		// Remove stopped containers
		rmArgs := append(baseArgs, "rm", "-f")
		rmArgs = append(rmArgs, services...)
		cmd = dc.command(rmArgs...)
		cmd.Stdin = os.Stdin
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			log.Fatalf("Failed to remove containers: %v", err)
		}
		fmt.Printf("Stopped services %v at %s\n", services, absPath)
	} else {
		fmt.Printf("Stopping swe-swe environment at %s\n", absPath)
		downArgs := append(baseArgs, "down")
		downArgs = append(downArgs, passThrough...)
		cmd := dc.command(downArgs...)
		cmd.Stdin = os.Stdin
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			log.Fatalf("Failed to stop docker compose: %v", err)
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

	// Split at "--" for pass-through args to docker compose
	// Service names are passed directly to docker compose (no validation needed)
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

	// Check if docker compose is available
	dc, err := getDockerComposeCmd()
	if err != nil {
		log.Fatalf("%v", err)
	}

	composeFile := filepath.Join(sweDir, "docker-compose.yml")

	// Build command args (--no-cache by default, can be overridden via passThrough)
	buildArgs := []string{"-f", composeFile, "build", "--no-cache"}
	buildArgs = append(buildArgs, passThrough...)
	if len(services) > 0 {
		fmt.Printf("Building services %v at %s (fresh build, no cache)\n", services, absPath)
		buildArgs = append(buildArgs, services...)
	} else {
		fmt.Printf("Building swe-swe environment at %s (fresh build, no cache)\n", absPath)
	}

	cmd := dc.command(buildArgs...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		log.Fatalf("Failed to build: %v", err)
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
