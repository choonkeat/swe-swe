package main

import (
	"fmt"
	"strconv"
	"strings"
)

// processDockerfileTemplate processes the Dockerfile template with conditional sections
// based on selected agents, custom apt packages, custom npm packages, Docker access, enterprise certificates, and slash commands
func processDockerfileTemplate(content string, agents []string, aptPackages, npmPackages string, withDocker bool, hasCerts bool, slashCommands []SlashCommandsRepo, hostUID int, hostGID int) string {
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

		// Handle UID and GID placeholders
		if strings.Contains(line, "{{UID}}") {
			line = strings.ReplaceAll(line, "{{UID}}", fmt.Sprintf("%d", hostUID))
		}
		if strings.Contains(line, "{{GID}}") {
			line = strings.ReplaceAll(line, "{{GID}}", fmt.Sprintf("%d", hostGID))
		}

		if !skip {
			result = append(result, line)
		}
	}

	return strings.Join(result, "\n")
}

// processSimpleTemplate handles simple conditional templates with {{IF DOCKER}}...{{ENDIF}} blocks
// This is used for docker-compose.yml and traefik-dynamic.yml
func processSimpleTemplate(content string, withDocker bool, ssl string, hostUID int, hostGID int, email string, domain string) string {
	lines := strings.Split(content, "\n")
	var result []string
	skip := false

	// SSL mode detection
	isSSL := strings.HasPrefix(ssl, "selfsign") || strings.HasPrefix(ssl, "letsencrypt")
	isLetsEncrypt := strings.HasPrefix(ssl, "letsencrypt")
	isSelfSign := strings.HasPrefix(ssl, "selfsign")
	isLetsEncryptStaging := strings.HasPrefix(ssl, "letsencrypt-staging")

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		// Handle conditional markers (support both # and yaml-style comments)
		if strings.Contains(trimmed, "{{IF DOCKER}}") {
			skip = !withDocker
			continue
		}

		if strings.Contains(trimmed, "{{IF SSL}}") {
			skip = !isSSL
			continue
		}

		if strings.Contains(trimmed, "{{IF NO_SSL}}") {
			skip = isSSL
			continue
		}

		if strings.Contains(trimmed, "{{IF LETSENCRYPT}}") {
			skip = !isLetsEncrypt
			continue
		}

		if strings.Contains(trimmed, "{{IF SELFSIGN}}") {
			skip = !isSelfSign
			continue
		}

		if strings.Contains(trimmed, "{{IF LETSENCRYPT_STAGING}}") {
			skip = !isLetsEncryptStaging
			continue
		}

		if strings.Contains(trimmed, "{{IF LETSENCRYPT_PRODUCTION}}") {
			skip = isLetsEncryptStaging || !isLetsEncrypt
			continue
		}

		if strings.Contains(trimmed, "{{ENDIF}}") {
			skip = false
			continue
		}

		if !skip {
			// Handle UID and GID placeholders
			if strings.Contains(line, "{{UID}}") {
				line = strings.ReplaceAll(line, "{{UID}}", fmt.Sprintf("%d", hostUID))
			}
			if strings.Contains(line, "{{GID}}") {
				line = strings.ReplaceAll(line, "{{GID}}", fmt.Sprintf("%d", hostGID))
			}
			// Handle Let's Encrypt placeholders
			if strings.Contains(line, "{{EMAIL}}") {
				line = strings.ReplaceAll(line, "{{EMAIL}}", email)
			}
			if strings.Contains(line, "{{DOMAIN}}") {
				line = strings.ReplaceAll(line, "{{DOMAIN}}", domain)
			}
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
		if strings.Contains(trimmed, "{{IF OPENCODE}}") {
			skip = !hasAgent("opencode")
			continue
		}
		if strings.Contains(trimmed, "{{IF CODEX}}") {
			skip = !hasAgent("codex")
			continue
		}
		if strings.Contains(trimmed, "{{IF GEMINI}}") {
			skip = !hasAgent("gemini")
			continue
		}
		if strings.Contains(trimmed, "{{IF GOOSE}}") {
			skip = !hasAgent("goose")
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

// processTerminalUITemplate processes the terminal-ui.js template with UI customization values
func processTerminalUITemplate(content string, statusBarFontSize int, statusBarFontFamily string, terminalFontSize int, terminalFontFamily string) string {
	content = strings.ReplaceAll(content, "{{STATUS_BAR_FONT_SIZE}}", strconv.Itoa(statusBarFontSize))
	content = strings.ReplaceAll(content, "{{STATUS_BAR_FONT_FAMILY}}", statusBarFontFamily)
	content = strings.ReplaceAll(content, "{{TERMINAL_FONT_SIZE}}", strconv.Itoa(terminalFontSize))
	content = strings.ReplaceAll(content, "{{TERMINAL_FONT_FAMILY}}", terminalFontFamily)

	return content
}
