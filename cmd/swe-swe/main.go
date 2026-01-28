package main

import (
	"embed"
	"encoding/json"
	"flag"
	"fmt"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"strings"
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

// writeBundledSlashCommands extracts bundled slash commands to the destination directory.
// If ext is non-empty, only files with that extension are copied.
func writeBundledSlashCommands(destDir string, ext string) error {
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

		// Skip non-command files (like README.adoc)
		fileExt := filepath.Ext(path)
		if fileExt != ".md" && fileExt != ".toml" {
			return nil
		}

		// Filter by extension if specified
		if ext != "" && fileExt != ext {
			return nil
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
	case "proxy":
		handleProxy()
	case "-h", "--help":
		printUsage()
	default:
		handlePassthrough(command, os.Args[2:])
	}
}


func printUsage() {
	fmt.Fprintf(os.Stderr, "swe-swe %s (%s)\n\n", Version, GitCommit)
	fmt.Fprintf(os.Stderr, `Usage: swe-swe <command> [options]

Native Commands:
  init [options]                         Initialize a new swe-swe project
  list                                   List all initialized swe-swe projects (auto-prunes stale ones)
  proxy <command>                        Proxy host commands to containers with real-time streaming

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
  --copy-home-paths PATHS                Comma-separated paths relative to $HOME to copy into container
                                         (e.g., .gitconfig,.ssh/config)
  --status-bar-color COLOR               Status bar background color (default: #007acc)
                                         Use 'list' to see color swatches: --status-bar-color=list
  --terminal-font-size SIZE              Terminal font size in pixels (default: 14)
  --terminal-font-family FONT            Terminal font family (default: Menlo, Monaco, "Courier New", monospace)
  --status-bar-font-size SIZE            Status bar font size in pixels (default: 12)
  --status-bar-font-family FONT          Status bar font family (default: system sans-serif)

Available Agents:
  claude, gemini, codex, aider, goose, opencode

Services (defined in docker-compose.yml after init):
  swe-swe, chrome, code-server, vscode-proxy, traefik, auth

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
  swe-swe init --copy-home-paths=.gitconfig,.ssh/config
                                                 Copy git and SSH config from host
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
	Agents              []string            `json:"agents"`
	AptPackages         string              `json:"aptPackages,omitempty"`
	NpmPackages         string              `json:"npmPackages,omitempty"`
	WithDocker          bool                `json:"withDocker,omitempty"`
	SlashCommands       []SlashCommandsRepo `json:"slashCommands,omitempty"`
	SSL                 string              `json:"ssl,omitempty"`
	CopyHomePaths       []string            `json:"copyHomePaths,omitempty"`
	StatusBarColor      string              `json:"statusBarColor,omitempty"`
	TerminalFontSize    int                 `json:"terminalFontSize,omitempty"`
	TerminalFontFamily  string              `json:"terminalFontFamily,omitempty"`
	StatusBarFontSize   int                 `json:"statusBarFontSize,omitempty"`
	StatusBarFontFamily string              `json:"statusBarFontFamily,omitempty"`
	HostUID             int                 `json:"hostUID,omitempty"`
	HostGID             int                 `json:"hostGID,omitempty"`
}

// slashCmdAgents are agents that support slash commands (md or toml format)
var slashCmdAgents = map[string]bool{
	"claude":   true,
	"codex":    true,
	"opencode": true,
	"gemini":   true,
}

// HasNonSlashAgents returns true if any enabled agent requires file-based commands
// (Goose, Aider, or any unknown agent)
func (c *InitConfig) HasNonSlashAgents() bool {
	for _, agent := range c.Agents {
		if !slashCmdAgents[agent] {
			return true
		}
	}
	return false
}

// HasSlashAgents returns true if any enabled agent supports slash commands
// (Claude, Codex, OpenCode, or Gemini)
func (c *InitConfig) HasSlashAgents() bool {
	for _, agent := range c.Agents {
		if slashCmdAgents[agent] {
			return true
		}
	}
	return false
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
	statusBarColor := fs.String("status-bar-color", "#007acc", "Status bar background color (CSS color name or hex)")
	terminalFontSize := fs.Int("terminal-font-size", 14, "Terminal font size in pixels")
	terminalFontFamily := fs.String("terminal-font-family", `Menlo, Monaco, "Courier New", monospace`, "Terminal font family")
	statusBarFontSize := fs.Int("status-bar-font-size", 12, "Status bar font size in pixels")
	statusBarFontFamily := fs.String("status-bar-font-family", "-apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif", "Status bar font family")
	previousInitFlags := fs.String("previous-init-flags", "", "How to handle existing init config: 'reuse' or 'ignore'")
	fs.Parse(os.Args[2:])

	// Handle --status-bar-color=list: print color swatches and exit
	if *statusBarColor == "list" {
		PrintColorSwatches()
		os.Exit(0)
	}

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
	// Uses allowlist approach: only --project-directory and --previous-init-flags are allowed with reuse.
	// This is safer than blocklist - forgetting to update allowlist fails safely (rejects new flag),
	// while forgetting to update blocklist would allow invalid combinations.
	if *previousInitFlags == "reuse" {
		hasOtherFlags := false
		fs.Visit(func(f *flag.Flag) {
			if f.Name != "project-directory" && f.Name != "previous-init-flags" {
				hasOtherFlags = true
			}
		})
		if hasOtherFlags {
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
		// UI customization flags
		if savedConfig.StatusBarColor != "" {
			*statusBarColor = savedConfig.StatusBarColor
		}
		if savedConfig.TerminalFontSize != 0 {
			*terminalFontSize = savedConfig.TerminalFontSize
		}
		if savedConfig.TerminalFontFamily != "" {
			*terminalFontFamily = savedConfig.TerminalFontFamily
		}
		if savedConfig.StatusBarFontSize != 0 {
			*statusBarFontSize = savedConfig.StatusBarFontSize
		}
		if savedConfig.StatusBarFontFamily != "" {
			*statusBarFontFamily = savedConfig.StatusBarFontFamily
		}
		fmt.Printf("Reusing saved configuration from %s\n", initConfigPath)
	}

	if err := os.MkdirAll(sweDir, 0755); err != nil {
		log.Fatalf("Failed to create metadata directory: %v", err)
	}

	// Capture host user's UID and GID for container app user creation
	hostUID := os.Getuid()
	hostGID := os.Getgid()
	fmt.Printf("Detected host user: UID=%d GID=%d\n", hostUID, hostGID)

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

	// Copy paths from user's $HOME to project home directory
	if len(copyHomePaths) > 0 {
		userHome, err := os.UserHomeDir()
		if err != nil {
			log.Fatalf("Failed to get user home directory: %v", err)
		}
		for _, relPath := range copyHomePaths {
			srcPath := filepath.Join(userHome, relPath)
			destPath := filepath.Join(homeDir, relPath)

			// Check if source exists
			srcInfo, err := os.Stat(srcPath)
			if os.IsNotExist(err) {
				fmt.Printf("Warning: %s does not exist, skipping\n", srcPath)
				continue
			}
			if err != nil {
				fmt.Printf("Warning: cannot access %s: %v, skipping\n", srcPath, err)
				continue
			}

			// Create parent directories at destination
			if err := os.MkdirAll(filepath.Dir(destPath), 0755); err != nil {
				log.Fatalf("Failed to create parent directories for %s: %v", destPath, err)
			}

			// Copy the file or directory
			if srcInfo.IsDir() {
				if err := copyDir(srcPath, destPath); err != nil {
					log.Fatalf("Failed to copy directory %s to %s: %v", srcPath, destPath, err)
				}
			} else {
				if err := copyFile(srcPath, destPath); err != nil {
					log.Fatalf("Failed to copy file %s to %s: %v", srcPath, destPath, err)
				}
			}
			fmt.Printf("Copied %s → %s\n", srcPath, destPath)

			// Set ownership to UID 1000 for the copied files
			filepath.Walk(destPath, func(path string, info os.FileInfo, err error) error {
				if err == nil {
					os.Chown(path, 1000, 1000)
				}
				return nil
			})
		}
	}

	// Write bundled slash commands to agent-specific directories
	// .md files go to Claude, Codex, and OpenCode directories
	// .toml files go to Gemini directory
	slashCmdAgentDirs := []struct {
		dir string
		ext string
	}{
		{filepath.Join(homeDir, ".claude", "commands"), ".md"},
		{filepath.Join(homeDir, ".codex", "prompts"), ".md"},
		{filepath.Join(homeDir, ".config", "opencode", "command"), ".md"},
		{filepath.Join(homeDir, ".gemini", "commands"), ".toml"},
	}
	for _, agent := range slashCmdAgentDirs {
		if err := writeBundledSlashCommands(agent.dir, agent.ext); err != nil {
			log.Fatalf("Failed to write bundled slash commands to %s: %v", agent.dir, err)
		}
	}

	// Write .path file to record the project path
	pathFile := filepath.Join(sweDir, ".path")
	if err := os.WriteFile(pathFile, []byte(absPath), 0644); err != nil {
		log.Fatalf("Failed to write path file: %v", err)
	}

	// Generate project name from directory name for multi-project isolation
	projectName := sanitizeProjectName(filepath.Base(absPath))

	// Handle enterprise certificates and write .env file (always includes PROJECT_NAME)
	// This allows the certificate detection to inform the Dockerfile template
	hasCerts := handleCertificatesAndEnv(sweDir, certsDir, projectName)

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
			"templates/host/chrome-screencast/Dockerfile",
			"templates/host/chrome-screencast/supervisord.conf",
			"templates/host/chrome-screencast/entrypoint.sh",
			"templates/host/chrome-screencast/nginx-cdp.conf",
			"templates/host/chrome-screencast/package.json",
			"templates/host/chrome-screencast/server.js",
			"templates/host/chrome-screencast/static/index.html",
			"templates/host/code-server/Dockerfile",
			"templates/host/auth/Dockerfile",
			"templates/host/auth/go.mod.txt",
			"templates/host/auth/main.go",
			"templates/host/swe-swe-server/go.mod.txt",
			"templates/host/swe-swe-server/go.sum.txt",
			"templates/host/swe-swe-server/main.go",
			"templates/host/swe-swe-server/static/index.html",
			"templates/host/swe-swe-server/static/selection.html",
			"templates/host/swe-swe-server/static/terminal-ui.js",
			"templates/host/swe-swe-server/static/link-provider.js",
			"templates/host/swe-swe-server/static/xterm-addon-fit.js",
			"templates/host/swe-swe-server/static/xterm.css",
			"templates/host/swe-swe-server/static/xterm.js",
			"templates/host/swe-swe-server/static/styles/terminal-ui.css",
			"templates/host/swe-swe-server/static/modules/util.js",
			"templates/host/swe-swe-server/static/modules/validation.js",
			"templates/host/swe-swe-server/static/modules/uuid.js",
			"templates/host/swe-swe-server/static/modules/url-builder.js",
			"templates/host/swe-swe-server/static/modules/messages.js",
			"templates/host/swe-swe-server/static/modules/reconnect.js",
			"templates/host/swe-swe-server/static/modules/upload-queue.js",
			"templates/host/swe-swe-server/static/modules/chunk-assembler.js",
			"templates/host/swe-swe-server/static/modules/status-renderer.js",
		}

		// Files that go to project directory (accessible by Claude in container)
		// Note: .mcp.json must be at project root, not .claude/mcp.json
		containerFiles := []string{
			"templates/container/.mcp.json",
			"templates/container/.swe-swe/docs/AGENTS.md",
			"templates/container/.swe-swe/docs/browser-automation.md",
		}

		// Only include swe-swe/setup for agents that don't support slash commands
		// (Goose, Aider, or any unknown agent). Slash-command agents get /swe-swe:setup instead.
		tempConfig := InitConfig{Agents: agents}
		if tempConfig.HasNonSlashAgents() {
			containerFiles = append(containerFiles, "templates/container/swe-swe/setup")
		}

		if *withDocker {
			containerFiles = append(containerFiles, "templates/container/.swe-swe/docs/docker.md")
		}

		// Always include app-preview.md since split-pane UI is always available
		containerFiles = append(containerFiles, "templates/container/.swe-swe/docs/app-preview.md")

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
				content = []byte(processDockerfileTemplate(string(content), agents, aptPkgs, npmPkgs, *withDocker, hasCerts, slashCmds, hostUID, hostGID))
			}

			// Process docker-compose.yml template with conditional sections
			if hostFile == "templates/host/docker-compose.yml" {
				content = []byte(processSimpleTemplate(string(content), *withDocker, *sslFlag, hostUID, hostGID))
			}

			// Process traefik-dynamic.yml template with SSL conditional sections
			if hostFile == "templates/host/traefik-dynamic.yml" {
				content = []byte(processSimpleTemplate(string(content), *withDocker, *sslFlag, hostUID, hostGID))
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

			// Process terminal-ui.js template with UI customization values
			if hostFile == "templates/host/swe-swe-server/static/terminal-ui.js" {
				content = []byte(processTerminalUITemplate(string(content), *statusBarColor, *statusBarFontSize, *statusBarFontFamily, *terminalFontSize, *terminalFontFamily))
			}

			// Process terminal-ui.css template with UI customization values
			if hostFile == "templates/host/swe-swe-server/static/styles/terminal-ui.css" {
				content = []byte(processTerminalUITemplate(string(content), *statusBarColor, *statusBarFontSize, *statusBarFontFamily, *terminalFontSize, *terminalFontFamily))
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
		Agents:              agents,
		AptPackages:         aptPkgs,
		NpmPackages:         npmPkgs,
		WithDocker:          *withDocker,
		SlashCommands:       slashCmds,
		SSL:                 *sslFlag,
		CopyHomePaths:       copyHomePaths,
		StatusBarColor:      *statusBarColor,
		TerminalFontSize:    *terminalFontSize,
		TerminalFontFamily:  *terminalFontFamily,
		StatusBarFontSize:   *statusBarFontSize,
		StatusBarFontFamily: *statusBarFontFamily,
		HostUID:             hostUID,
		HostGID:             hostGID,
	}
	if err := saveInitConfig(sweDir, initConfig); err != nil {
		log.Fatalf("Failed to save init config: %v", err)
	}

	fmt.Printf("\nInitialized swe-swe project at %s\n", absPath)
	fmt.Printf("View all projects: swe-swe list\n")
	fmt.Printf("Next: cd %s && swe-swe up\n", absPath)
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
