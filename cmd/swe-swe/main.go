package main

import (
	"crypto/md5"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"embed"
	"encoding/json"
	"encoding/pem"
	"flag"
	"fmt"
	"io/fs"
	"log"
	"math/big"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"syscall"
	"time"
)

// Version information set at build time via ldflags
var (
	Version   = "dev"
	GitCommit = "unknown"
	BuildTime = "unknown"
)

//go:embed all:templates
var assets embed.FS

//go:embed all:slash-commands
var slashCommandsFS embed.FS

// writeBundledSlashCommands extracts bundled slash commands to the destination directory
func writeBundledSlashCommands(destDir string) error {
	return fs.WalkDir(slashCommandsFS, "slash-commands", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		// Get relative path from slash-commands/
		relPath := strings.TrimPrefix(path, "slash-commands/")
		if relPath == "" {
			return nil // Skip root
		}

		destPath := filepath.Join(destDir, relPath)

		if d.IsDir() {
			return os.MkdirAll(destPath, 0755)
		}

		// Read embedded file
		content, err := slashCommandsFS.ReadFile(path)
		if err != nil {
			return fmt.Errorf("reading embedded %s: %w", path, err)
		}

		// Create parent directory if needed
		if err := os.MkdirAll(filepath.Dir(destPath), 0755); err != nil {
			return fmt.Errorf("creating directory for %s: %w", destPath, err)
		}

		// Write file
		if err := os.WriteFile(destPath, content, 0644); err != nil {
			return fmt.Errorf("writing %s: %w", destPath, err)
		}

		return nil
	})
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
	case "list":
		handleList()
	case "-h", "--help":
		printUsage()
	default:
		handlePassthrough(command, os.Args[2:])
	}
}

// expandTilde expands ~ to the user's home directory
func expandTilde(path string) string {
	if !strings.HasPrefix(path, "~") {
		return path
	}
	home, err := os.UserHomeDir()
	if err != nil {
		log.Fatalf("Failed to get home directory: %v", err)
	}
	return filepath.Join(home, strings.TrimPrefix(path, "~"))
}

// extractProjectDirectory parses args for --project-directory flag
// Returns (projectDir, remainingArgs)
func extractProjectDirectory(args []string) (string, []string) {
	projectDir := "."
	var remaining []string

	for i := 0; i < len(args); i++ {
		arg := args[i]

		// Handle --project-directory=value
		if strings.HasPrefix(arg, "--project-directory=") {
			projectDir = strings.TrimPrefix(arg, "--project-directory=")
			continue
		}

		// Handle --project-directory value
		if arg == "--project-directory" {
			if i+1 < len(args) {
				projectDir = args[i+1]
				i++ // Skip the value
				continue
			}
		}

		remaining = append(remaining, arg)
	}

	// Expand ~ in projectDir
	projectDir = expandTilde(projectDir)

	return projectDir, remaining
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

func printUsage() {
	fmt.Fprintf(os.Stderr, "swe-swe %s (%s)\n\n", Version, GitCommit)
	fmt.Fprintf(os.Stderr, `Usage: swe-swe <command> [options]

Native Commands:
  init [options]                         Initialize a new swe-swe project
  list                                   List all initialized swe-swe projects (auto-prunes stale ones)

Docker Compose Pass-through:
  All other commands (up, down, build, ps, logs, exec, etc.) are passed directly
  to docker compose with the project's docker-compose.yml. Use --project-directory
  to specify which project, or run from the project directory.

Global Option (for pass-through commands):
  --project-directory PATH               Project directory (defaults to current directory)

Init Options:
  --project-directory PATH               Project directory (defaults to current directory)
  --previous-init-flags=reuse            Reapply saved configuration from previous init
  --previous-init-flags=ignore           Ignore saved configuration, use provided flags
  --agents AGENTS                        Comma-separated agents: claude,gemini,codex,aider,goose,opencode (default: all)
  --exclude-agents AGENTS                Comma-separated agents to exclude
  --apt-get-install PACKAGES             Additional apt packages to install (comma or space separated)
  --npm-install PACKAGES                 Additional npm packages to install globally (comma or space separated)
  --with-docker                          Mount Docker socket to allow container to run Docker commands
  --with-slash-commands REPOS            Git repos to clone as slash commands for Claude/Codex/OpenCode
                                         Format: [alias@]<git-url> (space-separated)
  --ssl MODE                             SSL mode: 'no' (default), 'selfsign', or 'selfsign@<host>'
                                         Use selfsign@<ip-or-hostname> for remote access

Available Agents:
  claude, gemini, codex, aider, goose, opencode

Services (defined in docker-compose.yml after init):
  swe-swe, vscode, chrome, traefik

Examples:
  swe-swe init                                   Initialize current directory with all agents
  swe-swe init --agents=claude                   Initialize current directory with Claude only
  swe-swe init --agents=claude,gemini            Initialize current directory with Claude and Gemini
  swe-swe init --exclude-agents=aider,goose      Initialize current directory without Aider and Goose
  swe-swe init --apt-get-install="vim htop"      Initialize current directory with custom apt packages
  swe-swe init --npm-install="typescript tsx"    Initialize current directory with custom npm packages
  swe-swe init --with-docker                     Initialize current directory with Docker-in-Docker
  swe-swe init --with-slash-commands=ck@https://github.com/choonkeat/slash-commands.git
                                                 Initialize current directory with slash commands
  swe-swe init --ssl=selfsign                    Initialize with self-signed HTTPS certificate
  swe-swe up                                     Start all services
  swe-swe up -d                                  Start all services in background
  swe-swe down                                   Stop all services
  swe-swe down --remove-orphans                  Stop and remove orphan containers
  swe-swe ps                                     List running containers
  swe-swe logs -f swe-swe                        Follow logs for swe-swe service
  swe-swe exec swe-swe bash                      Open shell in swe-swe container
  swe-swe build --no-cache                       Rebuild all images without cache
  swe-swe up --project-directory /path            Run command for project at /path

Environment Variables:
  SWE_SWE_PASSWORD                       Authentication password (defaults to changeme)
  NODE_EXTRA_CA_CERTS                    Enterprise CA certificate path (auto-copied during init)
  SSL_CERT_FILE                          SSL certificate file path (auto-copied during init)

Requires: Docker with Compose plugin (docker compose) or standalone docker-compose
`)
}

// allAgents lists all available AI agents that can be installed
var allAgents = []string{"claude", "gemini", "codex", "aider", "goose", "opencode"}

// SlashCommandsRepo represents a git repository to clone for slash commands
type SlashCommandsRepo struct {
	Alias string `json:"alias"` // "ck" or derived "choonkeat/slash-commands"
	URL   string `json:"url"`   // "https://github.com/choonkeat/slash-commands.git"
}

// InitConfig stores the configuration used to initialize a project.
// This is saved to init.json and used by --previous-init-flags=reuse.
type InitConfig struct {
	Agents        []string            `json:"agents"`
	AptPackages   string              `json:"aptPackages,omitempty"`
	NpmPackages   string              `json:"npmPackages,omitempty"`
	WithDocker    bool                `json:"withDocker,omitempty"`
	SlashCommands []SlashCommandsRepo `json:"slashCommands,omitempty"`
	SSL           string              `json:"ssl,omitempty"`
	CopyHomePaths []string            `json:"copyHomePaths,omitempty"`
}

// saveInitConfig writes the init configuration to init.json
func saveInitConfig(sweDir string, config InitConfig) error {
	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal init config: %w", err)
	}
	configPath := filepath.Join(sweDir, "init.json")
	if err := os.WriteFile(configPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write init config: %w", err)
	}
	return nil
}

// loadInitConfig reads the init configuration from init.json
func loadInitConfig(sweDir string) (InitConfig, error) {
	configPath := filepath.Join(sweDir, "init.json")
	data, err := os.ReadFile(configPath)
	if err != nil {
		return InitConfig{}, fmt.Errorf("failed to read init config: %w", err)
	}
	var config InitConfig
	if err := json.Unmarshal(data, &config); err != nil {
		return InitConfig{}, fmt.Errorf("failed to parse init config: %w", err)
	}
	return config, nil
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
		if alias == "swe-swe" {
			return SlashCommandsRepo{}, fmt.Errorf("alias %q is reserved for bundled slash commands", alias)
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

// resolveAgents computes the final agent list based on --agents and --exclude-agents flags
func resolveAgents(agentsFlag, excludeFlag string) ([]string, error) {
	// Parse exclude list first
	excludeList, err := parseAgentList(excludeFlag)
	if err != nil {
		return nil, fmt.Errorf("--exclude-agents: %v", err)
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
// based on selected agents, custom apt packages, custom npm packages, Docker access, enterprise certificates, and slash commands
func processDockerfileTemplate(content string, agents []string, aptPackages, npmPackages string, withDocker bool, hasCerts bool, slashCommands []SlashCommandsRepo) string {
	// Helper to check if agent is selected
	hasAgent := func(agent string) bool {
		return agentInList(agent, agents)
	}

	// Check if we need Python (only for aider)
	needsPython := hasAgent("aider")

	// Check if we need Node.js (claude, gemini, codex, opencode, or playwright)
	needsNodeJS := hasAgent("claude") || hasAgent("gemini") || hasAgent("codex") || hasAgent("opencode")

	// Check if we have slash commands for supported agents (claude, codex, or opencode)
	hasSlashCommands := len(slashCommands) > 0 && (hasAgent("claude") || hasAgent("codex") || hasAgent("opencode"))

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
			case "OPENCODE":
				skip = !hasAgent("opencode")
			case "APT_PACKAGES":
				skip = aptPackages == ""
			case "NPM_PACKAGES":
				skip = npmPackages == ""
			case "CERTS":
				skip = !hasCerts
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
// This is used for docker-compose.yml which only needs the DOCKER condition
func processSimpleTemplate(content string, withDocker bool, ssl string) string {
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

		if strings.Contains(trimmed, "{{IF SSL}}") {
			skip = !strings.HasPrefix(ssl, "selfsign")
			continue
		}

		if strings.Contains(trimmed, "{{IF NO_SSL}}") {
			skip = strings.HasPrefix(ssl, "selfsign")
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

// processEntrypointTemplate handles the entrypoint.sh template with DOCKER and SLASH_COMMANDS conditions
func processEntrypointTemplate(content string, agents []string, withDocker bool, slashCommands []SlashCommandsRepo) string {
	// Helper to check if agent is selected
	hasAgent := func(agent string) bool {
		return agentInList(agent, agents)
	}

	// Check if we have slash commands for supported agents (claude, codex, or opencode)
	hasSlashCommands := len(slashCommands) > 0 && (hasAgent("claude") || hasAgent("codex") || hasAgent("opencode"))

	// Generate slash commands copy lines
	var slashCommandsCopy string
	if hasSlashCommands {
		var copyLines []string
		for _, repo := range slashCommands {
			// Claude
			if hasAgent("claude") {
				copyLines = append(copyLines, fmt.Sprintf(`if [ -d "/home/app/.claude/commands/%s/.git" ]; then
    # Try to pull updates (best effort)
    git config --global --add safe.directory /home/app/.claude/commands/%s 2>/dev/null || true
    su -s /bin/bash app -c "cd /home/app/.claude/commands/%s && git pull" 2>/dev/null && \
        echo -e "${GREEN}✓ Updated slash commands: %s (claude)${NC}" || \
        echo -e "${YELLOW}⚠ Could not update slash commands: %s (claude)${NC}"
elif [ -d "/tmp/slash-commands/%s" ]; then
    mkdir -p /home/app/.claude/commands
    cp -r /tmp/slash-commands/%s /home/app/.claude/commands/%s
    chown -R app:app /home/app/.claude/commands/%s
    echo -e "${GREEN}✓ Installed slash commands: %s (claude)${NC}"
fi`, repo.Alias, repo.Alias, repo.Alias, repo.Alias, repo.Alias, repo.Alias, repo.Alias, repo.Alias, repo.Alias, repo.Alias))
			}
			// Codex
			if hasAgent("codex") {
				copyLines = append(copyLines, fmt.Sprintf(`if [ -d "/home/app/.codex/prompts/%s/.git" ]; then
    # Try to pull updates (best effort)
    git config --global --add safe.directory /home/app/.codex/prompts/%s 2>/dev/null || true
    su -s /bin/bash app -c "cd /home/app/.codex/prompts/%s && git pull" 2>/dev/null && \
        echo -e "${GREEN}✓ Updated slash commands: %s (codex)${NC}" || \
        echo -e "${YELLOW}⚠ Could not update slash commands: %s (codex)${NC}"
elif [ -d "/tmp/slash-commands/%s" ]; then
    mkdir -p /home/app/.codex/prompts
    cp -r /tmp/slash-commands/%s /home/app/.codex/prompts/%s
    chown -R app:app /home/app/.codex/prompts/%s
    echo -e "${GREEN}✓ Installed slash commands: %s (codex)${NC}"
fi`, repo.Alias, repo.Alias, repo.Alias, repo.Alias, repo.Alias, repo.Alias, repo.Alias, repo.Alias, repo.Alias, repo.Alias))
			}
			// OpenCode
			if hasAgent("opencode") {
				copyLines = append(copyLines, fmt.Sprintf(`if [ -d "/home/app/.config/opencode/command/%s/.git" ]; then
    # Try to pull updates (best effort)
    git config --global --add safe.directory /home/app/.config/opencode/command/%s 2>/dev/null || true
    su -s /bin/bash app -c "cd /home/app/.config/opencode/command/%s && git pull" 2>/dev/null && \
        echo -e "${GREEN}✓ Updated slash commands: %s (opencode)${NC}" || \
        echo -e "${YELLOW}⚠ Could not update slash commands: %s (opencode)${NC}"
elif [ -d "/tmp/slash-commands/%s" ]; then
    mkdir -p /home/app/.config/opencode/command
    cp -r /tmp/slash-commands/%s /home/app/.config/opencode/command/%s
    chown -R app:app /home/app/.config/opencode/command/%s
    echo -e "${GREEN}✓ Installed slash commands: %s (opencode)${NC}"
fi`, repo.Alias, repo.Alias, repo.Alias, repo.Alias, repo.Alias, repo.Alias, repo.Alias, repo.Alias, repo.Alias, repo.Alias))
			}
		}
		slashCommandsCopy = strings.Join(copyLines, "\n")
	}

	lines := strings.Split(content, "\n")
	var result []string
	skip := false

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		// Handle conditional markers
		if strings.Contains(trimmed, "{{IF DOCKER}}") {
			skip = !withDocker
			continue
		}
		if strings.Contains(trimmed, "{{IF SLASH_COMMANDS}}") {
			skip = !hasSlashCommands
			continue
		}
		if strings.Contains(trimmed, "{{ENDIF}}") {
			skip = false
			continue
		}

		// Handle SLASH_COMMANDS_COPY placeholder
		if strings.Contains(line, "{{SLASH_COMMANDS_COPY}}") {
			if slashCommandsCopy != "" {
				line = strings.ReplaceAll(line, "{{SLASH_COMMANDS_COPY}}", slashCommandsCopy)
			}
		}

		if !skip {
			result = append(result, line)
		}
	}

	return strings.Join(result, "\n")
}

func handleInit() {
	fs := flag.NewFlagSet("init", flag.ExitOnError)
	path := fs.String("project-directory", ".", "Project directory to initialize")
	agentsFlag := fs.String("agents", "", "Comma-separated list of agents to include (claude,gemini,codex,aider,goose,opencode) or 'all'")
	excludeFlag := fs.String("exclude-agents", "", "Comma-separated list of agents to exclude")
	aptPackages := fs.String("apt-get-install", "", "Additional packages to install via apt-get (comma-separated)")
	npmPackages := fs.String("npm-install", "", "Additional packages to install via npm (comma-separated)")
	withDocker := fs.Bool("with-docker", false, "Mount Docker socket to allow container to run Docker commands on host")
	slashCommands := fs.String("with-slash-commands", "", "Git repos to clone as slash commands (space-separated, format: [alias@]<git-url>)")
	sslFlag := fs.String("ssl", "no", "SSL mode: 'no' (default) or 'selfsign' for self-signed certificates")
	copyHomePathsFlag := fs.String("copy-home-paths", "", "Comma-separated paths relative to $HOME to copy into container home")
	previousInitFlags := fs.String("previous-init-flags", "", "How to handle existing init config: 'reuse' or 'ignore'")
	fs.Parse(os.Args[2:])

	// Validate --previous-init-flags
	if *previousInitFlags != "" && *previousInitFlags != "reuse" && *previousInitFlags != "ignore" {
		fmt.Fprintf(os.Stderr, "Error: --previous-init-flags must be 'reuse' or 'ignore', got %q\n", *previousInitFlags)
		os.Exit(1)
	}

	// Validate --ssl flag: 'no', 'selfsign', or 'selfsign@hostname/ip'
	sslMode := *sslFlag
	sslHost := ""
	if strings.HasPrefix(*sslFlag, "selfsign@") {
		sslMode = "selfsign"
		sslHost = strings.TrimPrefix(*sslFlag, "selfsign@")
	}
	if sslMode != "no" && sslMode != "selfsign" {
		fmt.Fprintf(os.Stderr, "Error: --ssl must be 'no', 'selfsign', or 'selfsign@<hostname/ip>', got %q\n", *sslFlag)
		os.Exit(1)
	}

	// Parse and validate --copy-home-paths flag
	var copyHomePaths []string
	if *copyHomePathsFlag != "" {
		parts := strings.Split(*copyHomePathsFlag, ",")
		for _, p := range parts {
			p = strings.TrimSpace(p)
			if p == "" {
				continue
			}
			if strings.HasPrefix(p, "/") {
				fmt.Fprintf(os.Stderr, "Error: --copy-home-paths paths must be relative to $HOME, got absolute path %q\n", p)
				os.Exit(1)
			}
			if strings.Contains(p, "..") {
				fmt.Fprintf(os.Stderr, "Error: --copy-home-paths paths cannot contain '..', got %q\n", p)
				os.Exit(1)
			}
			copyHomePaths = append(copyHomePaths, p)
		}
	}

	// Validate that --previous-init-flags=reuse is not combined with other flags
	if *previousInitFlags == "reuse" {
		if *agentsFlag != "" || *excludeFlag != "" || *aptPackages != "" || *npmPackages != "" || *withDocker || *slashCommands != "" || *sslFlag != "no" || *copyHomePathsFlag != "" {
			fmt.Fprintf(os.Stderr, "Error: --previous-init-flags=reuse cannot be combined with other flags\n\n")
			fmt.Fprintf(os.Stderr, "  To reapply saved configuration without changes:\n")
			fmt.Fprintf(os.Stderr, "    swe-swe init --previous-init-flags=reuse\n\n")
			fmt.Fprintf(os.Stderr, "  To apply new configuration:\n")
			fmt.Fprintf(os.Stderr, "    swe-swe init --previous-init-flags=ignore [options]\n")
			os.Exit(1)
		}
	}

	if *path == "" {
		*path = "."
	}

	// Expand ~ in path
	*path = expandTilde(*path)

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

	// Parse slash commands flag early (may be overridden by --previous-init-flags=reuse)
	var slashCmds []SlashCommandsRepo
	if *slashCommands != "" {
		var err error
		slashCmds, err = parseSlashCommandsFlag(*slashCommands)
		if err != nil {
			log.Fatalf("Invalid --with-slash-commands value: %v", err)
		}
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

	// Check if project is already initialized (init.json exists)
	initConfigPath := filepath.Join(sweDir, "init.json")
	initConfigExists := false
	if _, err := os.Stat(initConfigPath); err == nil {
		initConfigExists = true
		// Project already initialized
		if *previousInitFlags == "" {
			fmt.Fprintf(os.Stderr, "Error: Project already initialized at %s\n\n", absPath)
			fmt.Fprintf(os.Stderr, "  To reapply saved configuration:\n")
			fmt.Fprintf(os.Stderr, "    swe-swe init --previous-init-flags=reuse\n\n")
			fmt.Fprintf(os.Stderr, "  To overwrite with new configuration:\n")
			fmt.Fprintf(os.Stderr, "    swe-swe init --previous-init-flags=ignore [options]\n")
			os.Exit(1)
		}
	}

	// Handle --previous-init-flags=reuse
	if *previousInitFlags == "reuse" {
		if !initConfigExists {
			fmt.Fprintf(os.Stderr, "Error: No saved configuration to reuse at %s\n\n", absPath)
			fmt.Fprintf(os.Stderr, "  Run init without --previous-init-flags first to create a configuration.\n")
			os.Exit(1)
		}
		// Load saved config and override the parsed flags
		savedConfig, err := loadInitConfig(sweDir)
		if err != nil {
			log.Fatalf("Failed to load saved config: %v", err)
		}
		agents = savedConfig.Agents
		aptPkgs = savedConfig.AptPackages
		npmPkgs = savedConfig.NpmPackages
		*withDocker = savedConfig.WithDocker
		slashCmds = savedConfig.SlashCommands
		*sslFlag = savedConfig.SSL
		if *sslFlag == "" {
			*sslFlag = "no" // Default for old configs without SSL field
		}
		copyHomePaths = savedConfig.CopyHomePaths
		fmt.Printf("Reusing saved configuration from %s\n", initConfigPath)
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

	// Write bundled slash commands to home/.claude/commands/swe-swe/
	bundledSlashCommandsDir := filepath.Join(homeDir, ".claude", "commands")
	if err := writeBundledSlashCommands(bundledSlashCommandsDir); err != nil {
		log.Fatalf("Failed to write bundled slash commands: %v", err)
	}

	// Write .path file to record the project path
	pathFile := filepath.Join(sweDir, ".path")
	if err := os.WriteFile(pathFile, []byte(absPath), 0644); err != nil {
		log.Fatalf("Failed to write path file: %v", err)
	}

	// Handle enterprise certificates early (before Dockerfile processing)
	// This allows the certificate detection to inform the Dockerfile template
	hasCerts := handleCertificates(sweDir, certsDir)

	// Generate self-signed certificate if SSL mode is selfsign
	// Certs are stored in shared location ~/.swe-swe/tls/ so users only need to trust once
	if sslMode == "selfsign" {
		userHome, err := os.UserHomeDir()
		if err != nil {
			log.Fatalf("Failed to get user home directory: %v", err)
		}
		tlsDir := filepath.Join(userHome, ".swe-swe", "tls")
		if err := os.MkdirAll(tlsDir, 0755); err != nil {
			log.Fatalf("Failed to create TLS directory: %v", err)
		}
		// Check if certificate already exists
		certPath := filepath.Join(tlsDir, "server.crt")
		if _, err := os.Stat(certPath); os.IsNotExist(err) {
			if err := generateSelfSignedCert(tlsDir, sslHost); err != nil {
				log.Fatalf("Failed to generate self-signed certificate: %v", err)
			}
			if sslHost != "" {
				fmt.Printf("Generated self-signed SSL certificate for %s in %s\n", sslHost, tlsDir)
			} else {
				fmt.Printf("Generated self-signed SSL certificate in %s\n", tlsDir)
			}
		} else {
			fmt.Printf("Reusing existing SSL certificate from %s\n", tlsDir)
		}
	}

	// Extract embedded files
	// Files that go to metadata directory (~/.swe-swe/projects/<path>/)
	hostFiles := []string{
			"templates/host/.dockerignore",
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
			"templates/host/swe-swe-server/playback/types.go",
			"templates/host/swe-swe-server/playback/timing.go",
			"templates/host/swe-swe-server/playback/render.go",
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
		if *withDocker {
			containerFiles = append(containerFiles, "templates/container/.swe-swe/docker.md")
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

		// Print slash commands info (already parsed earlier)
		for _, repo := range slashCmds {
			fmt.Printf("Slash commands: %s -> /tmp/slash-commands/%s\n", repo.URL, repo.Alias)
		}

		for _, hostFile := range hostFiles {
			content, err := assets.ReadFile(hostFile)
			if err != nil {
				log.Fatalf("Failed to read embedded file %q: %v", hostFile, err)
			}

			// Process Dockerfile template with conditional sections
			if hostFile == "templates/host/Dockerfile" {
				content = []byte(processDockerfileTemplate(string(content), agents, aptPkgs, npmPkgs, *withDocker, hasCerts, slashCmds))
			}

			// Process docker-compose.yml template with conditional sections
			if hostFile == "templates/host/docker-compose.yml" {
				content = []byte(processSimpleTemplate(string(content), *withDocker, *sslFlag))
			}

			// Process traefik-dynamic.yml template with SSL conditional sections
			if hostFile == "templates/host/traefik-dynamic.yml" {
				content = []byte(processSimpleTemplate(string(content), *withDocker, *sslFlag))
			}

			// Process entrypoint.sh template with conditional sections
			if hostFile == "templates/host/entrypoint.sh" {
				content = []byte(processEntrypointTemplate(string(content), agents, *withDocker, slashCmds))
			}

			// Inject version info into swe-swe-server main.go
			if hostFile == "templates/host/swe-swe-server/main.go" {
				contentStr := string(content)
				contentStr = strings.Replace(contentStr, `Version   = "dev"`, fmt.Sprintf(`Version   = "%s"`, Version), 1)
				contentStr = strings.Replace(contentStr, `GitCommit = "unknown"`, fmt.Sprintf(`GitCommit = "%s"`, GitCommit), 1)
				content = []byte(contentStr)
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

	// Save init configuration for --previous-init-flags=reuse
	initConfig := InitConfig{
		Agents:        agents,
		AptPackages:   aptPkgs,
		NpmPackages:   npmPkgs,
		WithDocker:    *withDocker,
		SlashCommands: slashCmds,
		SSL:           *sslFlag,
		CopyHomePaths: copyHomePaths,
	}
	if err := saveInitConfig(sweDir, initConfig); err != nil {
		log.Fatalf("Failed to save init config: %v", err)
	}

	fmt.Printf("\nInitialized swe-swe project at %s\n", absPath)
	fmt.Printf("View all projects: swe-swe list\n")
	fmt.Printf("Next: cd %s && swe-swe up\n", absPath)
}

// generateSelfSignedCert creates a self-signed TLS certificate and key for HTTPS.
// The certificate is valid for localhost, 127.0.0.1, and optionally an extra host.
// Files are written to certsDir as server.crt and server.key.
func generateSelfSignedCert(certsDir string, extraHost string) error {
	// Generate RSA private key
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return fmt.Errorf("failed to generate private key: %w", err)
	}

	// Create certificate template
	serialNumber, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return fmt.Errorf("failed to generate serial number: %w", err)
	}

	// Base DNS names and IPs
	dnsNames := []string{"localhost", "*.localhost"}
	ipAddresses := []net.IP{net.ParseIP("127.0.0.1"), net.ParseIP("::1")}

	// Add extra host if provided (can be IP or hostname)
	commonName := "localhost"
	if extraHost != "" {
		if ip := net.ParseIP(extraHost); ip != nil {
			// It's an IP address
			ipAddresses = append(ipAddresses, ip)
		} else {
			// It's a hostname
			dnsNames = append(dnsNames, extraHost)
		}
		commonName = extraHost
	}

	template := x509.Certificate{
		SerialNumber: serialNumber,
		Subject: pkix.Name{
			Organization: []string{"swe-swe"},
			CommonName:   commonName,
		},
		NotBefore:             time.Now(),
		NotAfter:              time.Now().AddDate(10, 0, 0), // Valid for 10 years
		KeyUsage:              x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		IsCA:                  true, // Required for iOS to show trust toggle in Certificate Trust Settings
		DNSNames:              dnsNames,
		IPAddresses:           ipAddresses,
	}

	// Create certificate
	certDER, err := x509.CreateCertificate(rand.Reader, &template, &template, &privateKey.PublicKey, privateKey)
	if err != nil {
		return fmt.Errorf("failed to create certificate: %w", err)
	}

	// Write certificate to file
	certPath := filepath.Join(certsDir, "server.crt")
	certFile, err := os.Create(certPath)
	if err != nil {
		return fmt.Errorf("failed to create certificate file: %w", err)
	}
	defer certFile.Close()

	if err := pem.Encode(certFile, &pem.Block{Type: "CERTIFICATE", Bytes: certDER}); err != nil {
		return fmt.Errorf("failed to write certificate: %w", err)
	}

	// Write private key to file
	keyPath := filepath.Join(certsDir, "server.key")
	keyFile, err := os.Create(keyPath)
	if err != nil {
		return fmt.Errorf("failed to create key file: %w", err)
	}
	defer keyFile.Close()

	privBytes, err := x509.MarshalPKCS8PrivateKey(privateKey)
	if err != nil {
		return fmt.Errorf("failed to marshal private key: %w", err)
	}

	if err := pem.Encode(keyFile, &pem.Block{Type: "PRIVATE KEY", Bytes: privBytes}); err != nil {
		return fmt.Errorf("failed to write private key: %w", err)
	}

	return nil
}

// handleCertificates detects and copies enterprise certificates for Docker builds
// Supports NODE_EXTRA_CA_CERTS, SSL_CERT_FILE, and NODE_EXTRA_CA_CERTS_BUNDLE environment variables
// for users behind corporate firewalls or VPNs (Cloudflare Warp, etc)
// Returns true if any certificates were found and copied, false otherwise
func handleCertificates(sweDir, certsDir string) bool {
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
			return false
		}
		fmt.Printf("Created %s with certificate configuration\n", envFilePath)
		return true
	}
	return false
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
	fs := flag.NewFlagSet("list", flag.ExitOnError)
	prune := fs.Bool("prune", false, "Remove orphaned project directories")
	fs.Parse(os.Args[2:])

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

	type projectInfo struct {
		path       string
		config     InitConfig
		hasConfig  bool
	}
	var activeProjects []projectInfo
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
			// If .path file doesn't exist or can't be read, handle based on prune flag
			if os.IsNotExist(err) {
				if *prune {
					if err := os.RemoveAll(metadataDir); err != nil {
						fmt.Printf("Warning: failed to remove orphaned %s: %v\n", entry.Name(), err)
					} else {
						prunedCount++
					}
				} else {
					fmt.Printf("Warning: .path file missing in %s (use --prune to remove)\n", entry.Name())
				}
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
			// Project path exists, try to load init config
			info := projectInfo{path: projectPath}
			if cfg, err := loadInitConfig(metadataDir); err == nil {
				info.config = cfg
				info.hasConfig = true
			}
			activeProjects = append(activeProjects, info)
		}
	}

	// Display active projects
	if len(activeProjects) == 0 {
		fmt.Println("No projects initialized yet")
	} else {
		fmt.Printf("Initialized projects (%d):\n", len(activeProjects))
		for _, info := range activeProjects {
			if info.hasConfig {
				agents := strings.Join(info.config.Agents, ",")
				extras := []string{}
				if info.config.AptPackages != "" {
					extras = append(extras, "apt:"+info.config.AptPackages)
				}
				if info.config.NpmPackages != "" {
					extras = append(extras, "npm:"+info.config.NpmPackages)
				}
				if info.config.WithDocker {
					extras = append(extras, "docker")
				}
				if len(info.config.SlashCommands) > 0 {
					extras = append(extras, fmt.Sprintf("slash-cmds:%d", len(info.config.SlashCommands)))
				}
				if info.config.SSL != "" && info.config.SSL != "no" {
					extras = append(extras, "ssl:"+info.config.SSL)
				}
				if len(extras) > 0 {
					fmt.Printf("  %s [%s] (%s)\n", info.path, agents, strings.Join(extras, ", "))
				} else {
					fmt.Printf("  %s [%s]\n", info.path, agents)
				}
			} else {
				fmt.Printf("  %s\n", info.path)
			}
		}
	}

	// Show pruning summary if any projects were removed
	if prunedCount > 0 {
		fmt.Printf("\nRemoved %d stale project(s)\n", prunedCount)
	}
}
