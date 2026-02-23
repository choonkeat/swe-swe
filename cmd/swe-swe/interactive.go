package main

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// detectInstalledAgents checks which agent CLIs are available on $PATH.
func detectInstalledAgents() []string {
	var found []string
	for _, agent := range allAgents {
		cli := agentCLIName(agent)
		if _, err := exec.LookPath(cli); err == nil {
			found = append(found, agent)
		}
	}
	return found
}

// agentCLIName returns the CLI binary name for a given agent.
func agentCLIName(agent string) string {
	switch agent {
	case "claude":
		return "claude"
	case "gemini":
		return "gemini"
	case "codex":
		return "codex"
	case "aider":
		return "aider"
	case "goose":
		return "goose"
	case "opencode":
		return "opencode"
	default:
		return agent
	}
}

// promptAgents asks the user which agents to enable.
// detected is the list of agents found on $PATH (used as default).
func promptAgents(scanner *bufio.Scanner, w io.Writer, detected []string) ([]string, error) {
	defaultStr := "all"
	if len(detected) > 0 {
		defaultStr = strings.Join(detected, ", ")
	}

	fmt.Fprintf(w, "Available agents: %s\n", strings.Join(allAgents, ", "))
	fmt.Fprintf(w, "Which agents? [%s] ", defaultStr)

	if !scanner.Scan() {
		return nil, fmt.Errorf("unexpected end of input")
	}
	line := strings.TrimSpace(scanner.Text())
	if line == "" {
		if len(detected) > 0 {
			return detected, nil
		}
		return allAgents, nil
	}

	agents, err := parseAgentList(line)
	if err != nil {
		return nil, err
	}
	if len(agents) == 0 {
		return allAgents, nil
	}
	return agents, nil
}

// promptDocker asks whether to enable Docker socket access.
func promptDocker(scanner *bufio.Scanner, w io.Writer) bool {
	fmt.Fprintf(w, "Enable Docker socket access? (lets agents run Docker commands on host) [y/N] ")

	if !scanner.Scan() {
		return false
	}
	line := strings.TrimSpace(strings.ToLower(scanner.Text()))
	return line == "y" || line == "yes"
}

// promptAccess asks about remote access / SSL configuration.
// Returns the ssl flag value and email (email only set for letsencrypt modes).
func promptAccess(scanner *bufio.Scanner, w io.Writer) (sslFlag, email string) {
	fmt.Fprintf(w, "Remote access? [Enter=local only, s=self-signed HTTPS, l=letsencrypt HTTPS] ")

	if !scanner.Scan() {
		return "no", ""
	}
	line := strings.TrimSpace(strings.ToLower(scanner.Text()))

	switch line {
	case "":
		return "no", ""
	case "s":
		// Self-signed: optionally bind to a hostname or IP
		fmt.Fprintf(w, "Hostname or IP (Enter=localhost only): ")
		if !scanner.Scan() {
			return "selfsign", ""
		}
		host := strings.TrimSpace(scanner.Text())
		if host == "" {
			return "selfsign", ""
		}
		return "selfsign@" + host, ""
	case "l":
		// Let's Encrypt: requires a hostname (not IP) and email
		fmt.Fprintf(w, "Hostname (must have DNS pointing to this machine): ")
		if !scanner.Scan() {
			return "no", ""
		}
		host := strings.TrimSpace(scanner.Text())
		if host == "" {
			return "no", ""
		}

		fmt.Fprintf(w, "Email (for Let's Encrypt certificate notifications): ")
		if !scanner.Scan() {
			return "no", ""
		}
		emailStr := strings.TrimSpace(scanner.Text())
		if emailStr == "" {
			return "no", ""
		}

		fmt.Fprintf(w, "Using Let's Encrypt staging (test certificates).\n")
		fmt.Fprintf(w, "After verifying, upgrade to production with:\n")
		fmt.Fprintf(w, "  swe-swe init --previous-init-flags=reuse --ssl=letsencrypt@%s --email=%s\n", host, emailStr)
		return "letsencrypt-staging@" + host, emailStr
	default:
		return "no", ""
	}
}

// runInteractiveInit runs the interactive Q&A flow, builds an InitConfig,
// and calls executeInit. If metadataDir is empty, it's derived from absPath.
func runInteractiveInit(absPath string, metadataDir string, stdin io.Reader, stdout io.Writer) error {
	scanner := bufio.NewScanner(stdin)

	fmt.Fprintf(stdout, "\nStarting express setup — for advanced options, use `swe-swe init -h`\n\n")

	// 1. Agents
	detected := detectInstalledAgents()
	agents, err := promptAgents(scanner, stdout, detected)
	if err != nil {
		return fmt.Errorf("agent selection: %w", err)
	}

	// 2. Docker
	withDocker := promptDocker(scanner, stdout)

	// 3. Access / SSL
	sslFlag, email := promptAccess(scanner, stdout)

	// Build config with sensible defaults
	config := InitConfig{
		Agents:              agents,
		WithDocker:          withDocker,
		SSL:                 sslFlag,
		Email:               email,
		PreviewPorts:        "3000-3019",
		PublicPorts:         "5000-5019",
		TerminalFontSize:    14,
		TerminalFontFamily:  `Menlo, Monaco, "Courier New", monospace`,
		StatusBarFontSize:   12,
		StatusBarFontFamily: "-apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif",
		ProxyPortOffset:     20000,
	}

	// Parse SSL
	sslMode, sslHost, sslDomain, sslErr := parseSSLFlagValue(config.SSL)
	if sslErr != nil {
		return fmt.Errorf("invalid SSL configuration: %w", sslErr)
	}

	// Derive metadata directory if not specified
	if metadataDir == "" {
		var err error
		metadataDir, err = getMetadataDir(absPath)
		if err != nil {
			return fmt.Errorf("failed to compute metadata directory: %w", err)
		}
	}

	fmt.Fprintf(stdout, "\n")
	executeInit(absPath, metadataDir, config, sslMode, sslHost, sslDomain)
	return nil
}

// handleInteractiveInit is called from handleInit when --ask flag is present.
// askValue is the value of --ask (empty string means derive metadata dir).
func handleInteractiveInit(absPath string, askValue string) {
	metadataDir := askValue
	if metadataDir != "" {
		metadataDir = expandTilde(metadataDir)
		if !filepath.IsAbs(metadataDir) {
			var err error
			metadataDir, err = filepath.Abs(metadataDir)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error resolving metadata path: %v\n", err)
				os.Exit(1)
			}
		}
	}

	if err := runInteractiveInit(absPath, metadataDir, os.Stdin, os.Stdout); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
