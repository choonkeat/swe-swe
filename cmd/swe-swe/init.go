package main

import (
	"embed"
	"encoding/json"
	"flag"
	"fmt"
	"io/fs"
	"log"
	"net"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
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
	Email               string              `json:"email,omitempty"`
	PreviewPorts        string              `json:"previewPorts,omitempty"`
	CopyHomePaths       []string            `json:"copyHomePaths,omitempty"`
	ReposDir            string              `json:"reposDir,omitempty"`
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

func parsePreviewPortsRange(rangeStr string) ([]int, error) {
	trimmed := strings.TrimSpace(rangeStr)
	if trimmed == "" {
		return nil, fmt.Errorf("range is empty")
	}

	parts := strings.Split(trimmed, "-")
	if len(parts) < 1 || len(parts) > 2 {
		return nil, fmt.Errorf("invalid range format: %q", rangeStr)
	}

	startStr := strings.TrimSpace(parts[0])
	endStr := startStr
	if len(parts) == 2 {
		endStr = strings.TrimSpace(parts[1])
	}

	start, err := strconv.Atoi(startStr)
	if err != nil {
		return nil, fmt.Errorf("invalid start port %q", startStr)
	}
	end, err := strconv.Atoi(endStr)
	if err != nil {
		return nil, fmt.Errorf("invalid end port %q", endStr)
	}
	if start > end {
		return nil, fmt.Errorf("range start %d is greater than end %d", start, end)
	}
	if start < 3000 || end > 3999 {
		return nil, fmt.Errorf("range must be within 3000-3999 (got %d-%d)", start, end)
	}

	ports := make([]int, 0, end-start+1)
	for port := start; port <= end; port++ {
		ports = append(ports, port)
	}

	return ports, nil
}

// upsertMcpServers merges swe-swe MCP server definitions into an existing
// .mcp.json file. swe-swe servers are always upserted; user-defined servers
// are preserved. Returns the merged JSON.
//
// SYNC: This logic is duplicated in swe-swe-server's setupSweSweFiles().
// If you change this function, update the copy in
// cmd/swe-swe/templates/host/swe-swe-server/main.go as well.
func upsertMcpServers(existing, template []byte) ([]byte, error) {
	var tmplDoc map[string]any
	if err := json.Unmarshal(template, &tmplDoc); err != nil {
		return nil, fmt.Errorf("failed to unmarshal template .mcp.json: %w", err)
	}

	var existingDoc map[string]any
	if len(existing) == 0 || json.Unmarshal(existing, &existingDoc) != nil {
		// No existing file or invalid JSON — use template as-is
		merged, err := json.MarshalIndent(tmplDoc, "", "  ")
		if err != nil {
			return nil, err
		}
		return append(merged, '\n'), nil
	}

	// Snapshot before merge for comparison
	var beforeDoc map[string]any
	json.Unmarshal(existing, &beforeDoc) // already validated above

	// Ensure existingDoc has mcpServers map
	existingServers, _ := existingDoc["mcpServers"].(map[string]any)
	if existingServers == nil {
		existingServers = make(map[string]any)
		existingDoc["mcpServers"] = existingServers
	}

	// Upsert template servers into existing
	if tmplServers, ok := tmplDoc["mcpServers"].(map[string]any); ok {
		for name, cfg := range tmplServers {
			existingServers[name] = cfg
		}
	}

	// If nothing changed, return original bytes to preserve formatting
	if reflect.DeepEqual(existingDoc, beforeDoc) {
		return existing, nil
	}

	merged, err := json.MarshalIndent(existingDoc, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(merged, '\n'), nil
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
	sslFlag := fs.String("ssl", "no", "SSL mode: 'no', 'selfsign', 'selfsign@hostname', 'letsencrypt@domain', 'letsencrypt-staging@domain'")
	emailFlag := fs.String("email", "", "Email for Let's Encrypt certificate expiry notifications (required for letsencrypt)")
	copyHomePathsFlag := fs.String("copy-home-paths", "", "Comma-separated paths relative to $HOME to copy into container home")
	reposDir := fs.String("repos-dir", "", "Host directory to mount at /repos for external repo clones (default: .swe-swe/repos in project)")
	terminalFontSize := fs.Int("terminal-font-size", 14, "Terminal font size in pixels")
	terminalFontFamily := fs.String("terminal-font-family", `Menlo, Monaco, "Courier New", monospace`, "Terminal font family")
	statusBarFontSize := fs.Int("status-bar-font-size", 12, "Status bar font size in pixels")
	statusBarFontFamily := fs.String("status-bar-font-family", "-apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif", "Status bar font family")
	previewPorts := fs.String("preview-ports", "3000-3019", "App preview port range (e.g., 3000-3019)")
	previousInitFlags := fs.String("previous-init-flags", "", "How to handle existing init config: 'reuse' or 'ignore'")
	fs.Parse(os.Args[2:])

	// Validate --previous-init-flags
	if *previousInitFlags != "" && *previousInitFlags != "reuse" && *previousInitFlags != "ignore" {
		fmt.Fprintf(os.Stderr, "Error: --previous-init-flags must be 'reuse' or 'ignore', got %q\n", *previousInitFlags)
		os.Exit(1)
	}

	// Validate --ssl flag: 'no', 'selfsign', 'selfsign@hostname', 'letsencrypt@domain', 'letsencrypt-staging@domain'
	sslMode := *sslFlag
	sslHost := ""
	sslDomain := ""
	if strings.HasPrefix(*sslFlag, "selfsign@") {
		sslMode = "selfsign"
		sslHost = strings.TrimPrefix(*sslFlag, "selfsign@")
	} else if strings.HasPrefix(*sslFlag, "letsencrypt-staging@") {
		sslMode = "letsencrypt-staging"
		sslDomain = strings.TrimPrefix(*sslFlag, "letsencrypt-staging@")
	} else if strings.HasPrefix(*sslFlag, "letsencrypt@") {
		sslMode = "letsencrypt"
		sslDomain = strings.TrimPrefix(*sslFlag, "letsencrypt@")
	}
	if sslMode != "no" && sslMode != "selfsign" && sslMode != "letsencrypt" && sslMode != "letsencrypt-staging" {
		fmt.Fprintf(os.Stderr, "Error: --ssl must be 'no', 'selfsign', 'selfsign@<hostname>', 'letsencrypt@<domain>', or 'letsencrypt-staging@<domain>', got %q\n", *sslFlag)
		os.Exit(1)
	}

	// Validate letsencrypt requirements
	if sslMode == "letsencrypt" || sslMode == "letsencrypt-staging" {
		if sslDomain == "" {
			fmt.Fprintf(os.Stderr, "Error: --ssl=%s requires a domain (e.g., --ssl=%s@example.com)\n", sslMode, sslMode)
			os.Exit(1)
		}
		if *emailFlag == "" {
			fmt.Fprintf(os.Stderr, "Error: --email is required when using Let's Encrypt (for certificate expiry notifications)\n")
			os.Exit(1)
		}
		// Validate domain resolves
		if _, err := net.LookupHost(sslDomain); err != nil {
			fmt.Fprintf(os.Stderr, "Error: domain %q does not resolve: %v\n", sslDomain, err)
			fmt.Fprintf(os.Stderr, "  Make sure your domain's DNS is configured before using Let's Encrypt.\n")
			os.Exit(1)
		}
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
		*emailFlag = savedConfig.Email
		if savedConfig.PreviewPorts != "" {
			*previewPorts = savedConfig.PreviewPorts
		}
		copyHomePaths = savedConfig.CopyHomePaths
		*reposDir = savedConfig.ReposDir
		// UI customization flags
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

	previewPortsRange, err := parsePreviewPortsRange(*previewPorts)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: --preview-ports must be in the form start-end within 3000-3999 (e.g., 3000-3019). %v\n", err)
		os.Exit(1)
	}

	// Derive agent chat port range from preview ports (+1000)
	agentChatPortsRange := make([]int, len(previewPortsRange))
	for i, p := range previewPortsRange {
		agentChatPortsRange[i] = agentChatPort(p)
	}

	if err := os.MkdirAll(sweDir, 0755); err != nil {
		log.Fatalf("Failed to create metadata directory: %v", err)
	}

	// Capture host user's UID and GID for container app user creation
	hostUID := os.Getuid()
	hostGID := os.Getgid()
	fmt.Printf("Detected host user: UID=%d GID=%d\n", hostUID, hostGID)

	// Create bin, home, and certs subdirectories in sweDir (project config directory)
	binDir := filepath.Join(sweDir, "bin")
	homeDir := filepath.Join(sweDir, "home")
	certsDir := filepath.Join(sweDir, "certs")
	for _, dir := range []string{binDir, homeDir, certsDir} {
		if err := os.MkdirAll(dir, 0755); err != nil {
			log.Fatalf("Failed to create directory %q: %v", dir, err)
		}
	}

	// Create worktrees directory in workspace (absPath/.swe-swe/worktrees)
	// This is where docker-compose.yml mounts from: ${WORKSPACE_DIR:-.}/.swe-swe/worktrees
	workspaceSweDir := filepath.Join(absPath, ".swe-swe")
	worktreesDir := filepath.Join(workspaceSweDir, "worktrees")
	if err := os.MkdirAll(worktreesDir, 0755); err != nil {
		log.Fatalf("Failed to create directory %q: %v", worktreesDir, err)
	}

	// Create repos directory only when --repos-dir is NOT specified
	// (when user specifies --repos-dir, they are responsible for the directory)
	// This is in workspace: ${WORKSPACE_DIR:-.}/.swe-swe/repos
	if *reposDir == "" {
		reposDirPath := filepath.Join(workspaceSweDir, "repos")
		if err := os.MkdirAll(reposDirPath, 0755); err != nil {
			log.Fatalf("Failed to create directory %q: %v", reposDirPath, err)
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

	// Append preview and agent chat port ranges to .env so swe-swe-server uses the correct ranges
	if len(previewPortsRange) > 0 {
		envFilePath := filepath.Join(sweDir, ".env")
		f, err := os.OpenFile(envFilePath, os.O_APPEND|os.O_WRONLY, 0644)
		if err == nil {
			fmt.Fprintf(f, "SWE_PREVIEW_PORTS=%d-%d\n", previewPortsRange[0], previewPortsRange[len(previewPortsRange)-1])
			fmt.Fprintf(f, "SWE_AGENT_CHAT_PORTS=%d-%d\n", agentChatPortsRange[0], agentChatPortsRange[len(agentChatPortsRange)-1])
			f.Close()
		}
	}

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

	// Create ACME directory for Let's Encrypt certificate storage
	if sslMode == "letsencrypt" || sslMode == "letsencrypt-staging" {
		userHome, err := os.UserHomeDir()
		if err != nil {
			log.Fatalf("Failed to get user home directory: %v", err)
		}
		acmeDir := filepath.Join(userHome, ".swe-swe", "acme")
		if err := os.MkdirAll(acmeDir, 0755); err != nil {
			log.Fatalf("Failed to create ACME directory: %v", err)
		}
		// Create empty acme.json if it doesn't exist (Traefik requires proper permissions)
		acmeFile := filepath.Join(acmeDir, "acme.json")
		if _, err := os.Stat(acmeFile); os.IsNotExist(err) {
			if err := os.WriteFile(acmeFile, []byte("{}"), 0600); err != nil {
				log.Fatalf("Failed to create ACME storage file: %v", err)
			}
		}
		modeStr := "production"
		if sslMode == "letsencrypt-staging" {
			modeStr = "staging (internal testing)"
		}
		fmt.Printf("Let's Encrypt SSL enabled for %s (%s)\n", sslDomain, modeStr)
		fmt.Printf("ACME storage: %s\n", acmeDir)
	}

	// Extract embedded files
	// Files that go to metadata directory (~/.swe-swe/projects/<path>/)
	//
	// IMPORTANT: When adding new files to templates/host/, you MUST add them to this list.
	// Otherwise they won't be copied during `swe-swe init` and will 404 at runtime.
	// After adding, run: make build golden-update
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
			"templates/host/swe-swe-server/static/styles/theme.css",
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
			"templates/host/swe-swe-server/static/color-utils.js",
			"templates/host/swe-swe-server/static/homepage-main.js",
			"templates/host/swe-swe-server/static/new-session-dialog.js",
			"templates/host/swe-swe-server/static/theme-mode.js",
			"templates/host/swe-swe-server/static/homepage-theme.js",
			"templates/host/swe-swe-server/static/session-theme.js",
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
				content = []byte(processSimpleTemplate(string(content), *withDocker, *sslFlag, hostUID, hostGID, *emailFlag, sslDomain, *reposDir, previewPortsRange))
			}

			// Process traefik-dynamic.yml template with SSL conditional sections
			if hostFile == "templates/host/traefik-dynamic.yml" {
				content = []byte(processSimpleTemplate(string(content), *withDocker, *sslFlag, hostUID, hostGID, *emailFlag, sslDomain, *reposDir, previewPortsRange))
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
				content = []byte(processTerminalUITemplate(string(content), *statusBarFontSize, *statusBarFontFamily, *terminalFontSize, *terminalFontFamily))
			}

			// Process terminal-ui.css template with UI customization values
			if hostFile == "templates/host/swe-swe-server/static/styles/terminal-ui.css" {
				content = []byte(processTerminalUITemplate(string(content), *statusBarFontSize, *statusBarFontFamily, *terminalFontSize, *terminalFontFamily))
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

			// For .mcp.json, upsert swe-swe servers into any existing config
			writeContent := content
			if relPath == ".mcp.json" {
				existingContent, _ := os.ReadFile(destPath) // ignore error (file may not exist)
				merged, err := upsertMcpServers(existingContent, content)
				if err != nil {
					log.Fatalf("Failed to merge .mcp.json: %v", err)
				}
				writeContent = merged
			}

			if err := os.WriteFile(destPath, writeContent, 0644); err != nil {
				log.Fatalf("Failed to write %q: %v", destPath, err)
			}
			fmt.Printf("Created %s\n", destPath)

			// Also write baseline snapshot for three-way merge during updates
			// Baseline always uses the template content (not merged)
			baselinePath := filepath.Join(absPath, ".swe-swe", "baseline", relPath)
			baselineDir := filepath.Dir(baselinePath)
			if err := os.MkdirAll(baselineDir, os.FileMode(0755)); err != nil {
				log.Fatalf("Failed to create baseline directory %q: %v", baselineDir, err)
			}
			if err := os.WriteFile(baselinePath, content, 0644); err != nil {
				log.Fatalf("Failed to write baseline %q: %v", baselinePath, err)
			}
		}

		// Copy ALL container templates into server source for embedding in server binary.
		// This allows the server to write swe-swe files into cloned repos and new projects.
		// All files are included unconditionally (extra docs cause no harm).
		allContainerTemplates := []string{
			"templates/container/.mcp.json",
			"templates/container/.swe-swe/docs/AGENTS.md",
			"templates/container/.swe-swe/docs/browser-automation.md",
			"templates/container/.swe-swe/docs/app-preview.md",
			"templates/container/.swe-swe/docs/docker.md",
			"templates/container/swe-swe/setup",
		}
		for _, tmplFile := range allContainerTemplates {
			content, err := assets.ReadFile(tmplFile)
			if err != nil {
				log.Fatalf("Failed to read embedded file %q: %v", tmplFile, err)
			}
			relPath := strings.TrimPrefix(tmplFile, "templates/container/")
			destPath := filepath.Join(sweDir, "swe-swe-server", "container-templates", relPath)
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
		Email:               *emailFlag,
		PreviewPorts:        *previewPorts,
		CopyHomePaths:       copyHomePaths,
		ReposDir:            *reposDir,
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
