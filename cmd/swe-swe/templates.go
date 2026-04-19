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

	// Check if we need Node.js (claude, gemini, codex, opencode, pi, or playwright)
	needsNodeJS := hasAgent("claude") || hasAgent("gemini") || hasAgent("codex") || hasAgent("opencode") || hasAgent("pi")

	// Check if we have slash commands for supported agents (claude, codex, opencode, or pi)
	hasSlashCommands := len(slashCommands) > 0 && (hasAgent("claude") || hasAgent("codex") || hasAgent("opencode") || hasAgent("pi"))

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
			case "PI":
				skip = !hasAgent("pi")
			case "APT_PACKAGES":
				skip = aptPackages == ""
			case "NPM_PACKAGES":
				skip = npmPackages == ""
			case "CERTS":
				skip = !hasCerts
			case "DOCKER":
				skip = !withDocker
			case "NO_DOCKER":
				skip = withDocker
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
func processSimpleTemplate(content string, withDocker bool, withVSCode bool, ssl string, hostUID int, hostGID int, email string, domain string, reposDir string, previewPorts []int, publicPorts []int, proxyPortOffset int) string {
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
			if strings.Contains(line, "{{PREVIEW_ENTRYPOINTS}}") {
				indent := strings.Split(line, "{{PREVIEW_ENTRYPOINTS}}")[0]
				for _, port := range previewPorts {
					entrypoint := fmt.Sprintf("preview%d", port)
					pp := previewProxyPort(port, proxyPortOffset)
					result = append(result, fmt.Sprintf("%s- \"--entrypoints.%s.address=:%d\"", indent, entrypoint, pp))
					result = append(result, fmt.Sprintf("%s- \"--entrypoints.%s.transport.respondingTimeouts.readTimeout=60s\"", indent, entrypoint))
				}
				continue
			}

			if strings.Contains(line, "{{AGENT_CHAT_ENTRYPOINTS}}") {
				indent := strings.Split(line, "{{AGENT_CHAT_ENTRYPOINTS}}")[0]
				for _, port := range previewPorts {
					acPort := agentChatPort(port)
					entrypoint := fmt.Sprintf("agentchat%d", acPort)
					pp := agentChatProxyPort(acPort, proxyPortOffset)
					result = append(result, fmt.Sprintf("%s- \"--entrypoints.%s.address=:%d\"", indent, entrypoint, pp))
					result = append(result, fmt.Sprintf("%s- \"--entrypoints.%s.transport.respondingTimeouts.readTimeout=60s\"", indent, entrypoint))
				}
				continue
			}

			if strings.Contains(line, "{{PREVIEW_PORTS}}") {
				indent := strings.Split(line, "{{PREVIEW_PORTS}}")[0]
				for _, port := range previewPorts {
					pp := previewProxyPort(port, proxyPortOffset)
					result = append(result, fmt.Sprintf("%s- \"%d:%d\"", indent, pp, pp))
				}
				continue
			}

			if strings.Contains(line, "{{AGENT_CHAT_PORTS}}") {
				indent := strings.Split(line, "{{AGENT_CHAT_PORTS}}")[0]
				for _, port := range previewPorts {
					acPort := agentChatPort(port)
					pp := agentChatProxyPort(acPort, proxyPortOffset)
					result = append(result, fmt.Sprintf("%s- \"%d:%d\"", indent, pp, pp))
				}
				continue
			}

			if strings.Contains(line, "{{PUBLIC_ENTRYPOINTS}}") {
				indent := strings.Split(line, "{{PUBLIC_ENTRYPOINTS}}")[0]
				for _, port := range publicPorts {
					entrypoint := fmt.Sprintf("public%d", port)
					result = append(result, fmt.Sprintf("%s- \"--entrypoints.%s.address=:%d\"", indent, entrypoint, port))
					result = append(result, fmt.Sprintf("%s- \"--entrypoints.%s.transport.respondingTimeouts.readTimeout=60s\"", indent, entrypoint))
				}
				continue
			}

			if strings.Contains(line, "{{VNC_ENTRYPOINTS}}") {
				indent := strings.Split(line, "{{VNC_ENTRYPOINTS}}")[0]
				for _, port := range previewPorts {
					vp := vncPort(port)
					entrypoint := fmt.Sprintf("vnc%d", vp)
					pp := vncProxyPort(vp, proxyPortOffset)
					result = append(result, fmt.Sprintf("%s- \"--entrypoints.%s.address=:%d\"", indent, entrypoint, pp))
					result = append(result, fmt.Sprintf("%s- \"--entrypoints.%s.transport.respondingTimeouts.readTimeout=60s\"", indent, entrypoint))
				}
				continue
			}

			if strings.Contains(line, "{{PUBLIC_PORTS}}") {
				indent := strings.Split(line, "{{PUBLIC_PORTS}}")[0]
				for _, port := range publicPorts {
					result = append(result, fmt.Sprintf("%s- \"%d:%d\"", indent, port, port))
				}
				continue
			}

			if strings.Contains(line, "{{VNC_PORTS}}") {
				indent := strings.Split(line, "{{VNC_PORTS}}")[0]
				for _, port := range previewPorts {
					vp := vncPort(port)
					pp := vncProxyPort(vp, proxyPortOffset)
					result = append(result, fmt.Sprintf("%s- \"%d:%d\"", indent, pp, pp))
				}
				continue
			}

			if strings.Contains(line, "{{PREVIEW_ROUTERS}}") {
				indent := strings.Split(line, "{{PREVIEW_ROUTERS}}")[0]
				for _, port := range previewPorts {
					entrypoint := fmt.Sprintf("preview%d", port)
					pp := previewProxyPort(port, proxyPortOffset)
					routerName := fmt.Sprintf("${PROJECT_NAME}-preview-%d", port)
					// Probe router: no ForwardAuth, higher priority -- Safari CORS fix
					probeName := routerName + "-probe"
					result = append(result, fmt.Sprintf("%s- \"traefik.http.routers.%s.rule=Path(`/__probe__`)\"", indent, probeName))
					result = append(result, fmt.Sprintf("%s- \"traefik.http.routers.%s.entrypoints=%s\"", indent, probeName, entrypoint))
					result = append(result, fmt.Sprintf("%s- \"traefik.http.routers.%s.priority=200\"", indent, probeName))
					result = append(result, fmt.Sprintf("%s- \"traefik.http.routers.%s.service=%s\"", indent, probeName, routerName))
					// Main router with ForwardAuth
					result = append(result, fmt.Sprintf("%s- \"traefik.http.routers.%s.rule=PathPrefix(`/`)\"", indent, routerName))
					result = append(result, fmt.Sprintf("%s- \"traefik.http.routers.%s.entrypoints=%s\"", indent, routerName, entrypoint))
					result = append(result, fmt.Sprintf("%s- \"traefik.http.routers.%s.middlewares=forwardauth@file\"", indent, routerName))
					result = append(result, fmt.Sprintf("%s- \"traefik.http.routers.%s.service=%s\"", indent, routerName, routerName))
					result = append(result, fmt.Sprintf("%s- \"traefik.http.services.%s.loadbalancer.server.port=%d\"", indent, routerName, pp))
					if isSSL {
						if isLetsEncrypt {
							result = append(result, fmt.Sprintf("%s- \"traefik.http.routers.%s.tls.certresolver=letsencrypt\"", indent, probeName))
							result = append(result, fmt.Sprintf("%s- \"traefik.http.routers.%s.tls.domains[0].main=%s\"", indent, probeName, domain))
							result = append(result, fmt.Sprintf("%s- \"traefik.http.routers.%s.tls.certresolver=letsencrypt\"", indent, routerName))
							result = append(result, fmt.Sprintf("%s- \"traefik.http.routers.%s.tls.domains[0].main=%s\"", indent, routerName, domain))
						} else if isSelfSign {
							result = append(result, fmt.Sprintf("%s- \"traefik.http.routers.%s.tls=true\"", indent, probeName))
							result = append(result, fmt.Sprintf("%s- \"traefik.http.routers.%s.tls=true\"", indent, routerName))
						}
					}
				}
				continue
			}

			if strings.Contains(line, "{{AGENT_CHAT_ROUTERS}}") {
				indent := strings.Split(line, "{{AGENT_CHAT_ROUTERS}}")[0]
				for _, port := range previewPorts {
					acPort := agentChatPort(port)
					entrypoint := fmt.Sprintf("agentchat%d", acPort)
					pp := agentChatProxyPort(acPort, proxyPortOffset)
					routerName := fmt.Sprintf("${PROJECT_NAME}-agentchat-%d", acPort)
					// Probe router: no ForwardAuth, higher priority -- Safari CORS fix
					probeName := routerName + "-probe"
					result = append(result, fmt.Sprintf("%s- \"traefik.http.routers.%s.rule=Path(`/__probe__`)\"", indent, probeName))
					result = append(result, fmt.Sprintf("%s- \"traefik.http.routers.%s.entrypoints=%s\"", indent, probeName, entrypoint))
					result = append(result, fmt.Sprintf("%s- \"traefik.http.routers.%s.priority=200\"", indent, probeName))
					result = append(result, fmt.Sprintf("%s- \"traefik.http.routers.%s.service=%s\"", indent, probeName, routerName))
					// Main router with ForwardAuth
					result = append(result, fmt.Sprintf("%s- \"traefik.http.routers.%s.rule=PathPrefix(`/`)\"", indent, routerName))
					result = append(result, fmt.Sprintf("%s- \"traefik.http.routers.%s.entrypoints=%s\"", indent, routerName, entrypoint))
					result = append(result, fmt.Sprintf("%s- \"traefik.http.routers.%s.middlewares=forwardauth@file\"", indent, routerName))
					result = append(result, fmt.Sprintf("%s- \"traefik.http.routers.%s.service=%s\"", indent, routerName, routerName))
					result = append(result, fmt.Sprintf("%s- \"traefik.http.services.%s.loadbalancer.server.port=%d\"", indent, routerName, pp))
					if isSSL {
						if isLetsEncrypt {
							result = append(result, fmt.Sprintf("%s- \"traefik.http.routers.%s.tls.certresolver=letsencrypt\"", indent, probeName))
							result = append(result, fmt.Sprintf("%s- \"traefik.http.routers.%s.tls.domains[0].main=%s\"", indent, probeName, domain))
							result = append(result, fmt.Sprintf("%s- \"traefik.http.routers.%s.tls.certresolver=letsencrypt\"", indent, routerName))
							result = append(result, fmt.Sprintf("%s- \"traefik.http.routers.%s.tls.domains[0].main=%s\"", indent, routerName, domain))
						} else if isSelfSign {
							result = append(result, fmt.Sprintf("%s- \"traefik.http.routers.%s.tls=true\"", indent, probeName))
							result = append(result, fmt.Sprintf("%s- \"traefik.http.routers.%s.tls=true\"", indent, routerName))
						}
					}
				}
				continue
			}

			if strings.Contains(line, "{{PUBLIC_ROUTERS}}") {
				indent := strings.Split(line, "{{PUBLIC_ROUTERS}}")[0]
				for _, port := range publicPorts {
					entrypoint := fmt.Sprintf("public%d", port)
					routerName := fmt.Sprintf("${PROJECT_NAME}-public-%d", port)
					result = append(result, fmt.Sprintf("%s- \"traefik.http.routers.%s.rule=PathPrefix(`/`)\"", indent, routerName))
					result = append(result, fmt.Sprintf("%s- \"traefik.http.routers.%s.entrypoints=%s\"", indent, routerName, entrypoint))
					result = append(result, fmt.Sprintf("%s- \"traefik.http.routers.%s.service=%s\"", indent, routerName, routerName))
					result = append(result, fmt.Sprintf("%s- \"traefik.http.services.%s.loadbalancer.server.port=%d\"", indent, routerName, port))
					if isSSL {
						if isLetsEncrypt {
							result = append(result, fmt.Sprintf("%s- \"traefik.http.routers.%s.tls.certresolver=letsencrypt\"", indent, routerName))
							result = append(result, fmt.Sprintf("%s- \"traefik.http.routers.%s.tls.domains[0].main=%s\"", indent, routerName, domain))
						} else if isSelfSign {
							result = append(result, fmt.Sprintf("%s- \"traefik.http.routers.%s.tls=true\"", indent, routerName))
						}
					}
				}
				continue
			}

			if strings.Contains(line, "{{VNC_ROUTERS}}") {
				indent := strings.Split(line, "{{VNC_ROUTERS}}")[0]
				for _, port := range previewPorts {
					vp := vncPort(port)
					entrypoint := fmt.Sprintf("vnc%d", vp)
					routerName := fmt.Sprintf("${PROJECT_NAME}-vnc-%d", vp)
					// Probe router: no ForwardAuth, higher priority -- Safari CORS fix
					probeName := routerName + "-probe"
					result = append(result, fmt.Sprintf("%s- \"traefik.http.routers.%s.rule=Path(`/__probe__`)\"", indent, probeName))
					result = append(result, fmt.Sprintf("%s- \"traefik.http.routers.%s.entrypoints=%s\"", indent, probeName, entrypoint))
					result = append(result, fmt.Sprintf("%s- \"traefik.http.routers.%s.priority=200\"", indent, probeName))
					result = append(result, fmt.Sprintf("%s- \"traefik.http.routers.%s.service=%s\"", indent, probeName, routerName))
					// Main router with ForwardAuth
					result = append(result, fmt.Sprintf("%s- \"traefik.http.routers.%s.rule=PathPrefix(`/`)\"", indent, routerName))
					result = append(result, fmt.Sprintf("%s- \"traefik.http.routers.%s.entrypoints=%s\"", indent, routerName, entrypoint))
					result = append(result, fmt.Sprintf("%s- \"traefik.http.routers.%s.middlewares=forwardauth@file\"", indent, routerName))
					result = append(result, fmt.Sprintf("%s- \"traefik.http.routers.%s.service=%s\"", indent, routerName, routerName))
					result = append(result, fmt.Sprintf("%s- \"traefik.http.services.%s.loadbalancer.server.port=%d\"", indent, routerName, vp))
					if isSSL {
						if isLetsEncrypt {
							result = append(result, fmt.Sprintf("%s- \"traefik.http.routers.%s.tls.certresolver=letsencrypt\"", indent, probeName))
							result = append(result, fmt.Sprintf("%s- \"traefik.http.routers.%s.tls.domains[0].main=%s\"", indent, probeName, domain))
							result = append(result, fmt.Sprintf("%s- \"traefik.http.routers.%s.tls.certresolver=letsencrypt\"", indent, routerName))
							result = append(result, fmt.Sprintf("%s- \"traefik.http.routers.%s.tls.domains[0].main=%s\"", indent, routerName, domain))
						} else if isSelfSign {
							result = append(result, fmt.Sprintf("%s- \"traefik.http.routers.%s.tls=true\"", indent, probeName))
							result = append(result, fmt.Sprintf("%s- \"traefik.http.routers.%s.tls=true\"", indent, routerName))
						}
					}
				}
				continue
			}

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
			// Handle VSCODE_SERVICES placeholder -- generates vscode-proxy + code-server services or empty
			if strings.Contains(line, "{{VSCODE_SERVICES}}") {
				if withVSCode {
					var vscodeSslLabels string
					if isSSL {
						vscodeSslLabels += fmt.Sprintf("      - \"traefik.http.routers.${PROJECT_NAME}-vscode.entrypoints=websecure\"\n")
					}
					if isSelfSign {
						vscodeSslLabels += fmt.Sprintf("      - \"traefik.http.routers.${PROJECT_NAME}-vscode.tls=true\"\n")
					}
					if isLetsEncrypt {
						vscodeSslLabels += fmt.Sprintf("      - \"traefik.http.routers.${PROJECT_NAME}-vscode.tls.certresolver=letsencrypt\"\n")
						vscodeSslLabels += fmt.Sprintf("      - \"traefik.http.routers.${PROJECT_NAME}-vscode.tls.domains[0].main=%s\"\n", domain)
					}
					vscodeBlock := fmt.Sprintf(`  vscode-proxy:
    image: nginx:latest
    volumes:
      - ./nginx-vscode.conf:/etc/nginx/conf.d/default.conf:ro
    labels:
      - "swe.project=${PROJECT_NAME}"
      - "traefik.enable=true"
      - "traefik.http.routers.${PROJECT_NAME}-vscode.rule=PathPrefix(`+"`"+`/vscode`+"`"+`)"
      - "traefik.http.routers.${PROJECT_NAME}-vscode.priority=100"
%s      - "traefik.http.routers.${PROJECT_NAME}-vscode.middlewares=forwardauth@file"
      - "traefik.http.services.${PROJECT_NAME}-vscode.loadbalancer.server.port=8081"
    depends_on:
      - code-server
    networks:
      - swe-network
    restart: unless-stopped

  code-server:
    build:
      context: code-server
      args:
        UID: %d
        GID: %d
    command:
      - "--bind-addr=0.0.0.0:8080"
      - "--auth=none"
    labels:
      - "swe.project=${PROJECT_NAME}"
    volumes:
      # Mount project directory (same path as swe-swe container for git worktree compatibility)
      - ${WORKSPACE_DIR:-.}:/workspace
      # Mount worktrees to /worktrees for cleaner agent path separation
      - ${WORKSPACE_DIR:-.}/.swe-swe/worktrees:/worktrees
      # Mount persistent home for VSCode settings/extensions
      - ./home:/home/coder
      # Enterprise certificates (if certs/ directory exists with certificates)
      - ./certs:/swe-swe/certs:ro
    working_dir: /workspace
    networks:
      - swe-network
    environment:
      - TZ=${TZ:-UTC}
      # Enterprise certificates (if configured during swe-swe init)
      - NODE_EXTRA_CA_CERTS=${NODE_EXTRA_CA_CERTS:-}
      - SSL_CERT_FILE=${SSL_CERT_FILE:-}
    restart: unless-stopped`, vscodeSslLabels, hostUID, hostGID)
					for _, vl := range strings.Split(vscodeBlock, "\n") {
						result = append(result, vl)
					}
				}
				continue
			}
			// Handle REPOS_DIR placeholder - default to .swe-swe/repos if not specified
			if strings.Contains(line, "{{REPOS_DIR}}") {
				reposDirValue := reposDir
				if reposDirValue == "" {
					reposDirValue = "${WORKSPACE_DIR:-.}/.swe-swe/repos"
				}
				line = strings.ReplaceAll(line, "{{REPOS_DIR}}", reposDirValue)
			}
			result = append(result, line)
		}
	}

	return strings.Join(result, "\n")
}

func agentChatPort(previewPort int) int {
	return previewPort + 1000
}

func cdpPort(previewPort int) int {
	return previewPort + 3000
}

func vncPort(previewPort int) int {
	return previewPort + 4000
}

func previewProxyPort(port, offset int) int {
	return offset + port
}

func agentChatProxyPort(port, offset int) int {
	return offset + port
}

func cdpProxyPort(port, offset int) int {
	return offset + port
}

func vncProxyPort(port, offset int) int {
	return offset + port
}


// processEntrypointTemplate handles the entrypoint.sh template with DOCKER and SLASH_COMMANDS conditions
func processEntrypointTemplate(content string, agents []string, withDocker bool, slashCommands []SlashCommandsRepo) string {
	// Helper to check if agent is selected
	hasAgent := func(agent string) bool {
		return agentInList(agent, agents)
	}

	// Check if we have slash commands for supported agents (claude, codex, opencode, or pi)
	hasSlashCommands := len(slashCommands) > 0 && (hasAgent("claude") || hasAgent("codex") || hasAgent("opencode") || hasAgent("pi"))

	// Generate slash commands copy lines
	var slashCommandsCopy string
	if hasSlashCommands {
		var copyLines []string
		for _, repo := range slashCommands {
			// Helper: generate slash command copy block for an agent
			genSlashBlock := func(agentName, configDir, subDir string) string {
				pullCmd := fmt.Sprintf(`(cd %s/%s && git pull) 2>/dev/null`, configDir, repo.Alias)
				if withDocker {
					pullCmd = fmt.Sprintf(`su -s /bin/bash app -c "cd %s/%s && git pull" 2>/dev/null`, configDir, repo.Alias)
				}
				chownLine := ""
				if withDocker {
					chownLine = fmt.Sprintf("\n    chown -R app:app %s/%s", configDir, repo.Alias)
				}
				return fmt.Sprintf(`if [ -d "%s/%s/.git" ]; then
    # Try to pull updates (best effort)
    git config --global --add safe.directory %s/%s 2>/dev/null || true
    %s && \
        echo -e "${GREEN}[ok] Updated slash commands: %s (%s)${NC}" || \
        echo -e "${YELLOW}⚠ Could not update slash commands: %s (%s)${NC}"
elif [ -d "/tmp/slash-commands/%s" ]; then
    mkdir -p %s
    cp -r /tmp/slash-commands/%s %s/%s%s
    echo -e "${GREEN}[ok] Installed slash commands: %s (%s)${NC}"
fi`, configDir, repo.Alias, configDir, repo.Alias, pullCmd, repo.Alias, agentName, repo.Alias, agentName, repo.Alias, configDir, repo.Alias, configDir, repo.Alias, chownLine, repo.Alias, agentName)
			}
			// Claude
			if hasAgent("claude") {
				copyLines = append(copyLines, genSlashBlock("claude", "/home/app/.claude/commands", repo.Alias))
			}
			// Codex
			if hasAgent("codex") {
				copyLines = append(copyLines, genSlashBlock("codex", "/home/app/.codex/prompts", repo.Alias))
			}
			// OpenCode
			if hasAgent("opencode") {
				copyLines = append(copyLines, genSlashBlock("opencode", "/home/app/.config/opencode/command", repo.Alias))
			}
			// Pi
			if hasAgent("pi") {
				copyLines = append(copyLines, genSlashBlock("pi", "/home/app/.pi/agent/prompts", repo.Alias))
			}
		}
		slashCommandsCopy = strings.Join(copyLines, "\n")
	}

	// Generate chown lines (only needed when running as root in DOCKER mode)
	chownOpencode := ""
	chownCodex := ""
	chownGemini := ""
	chownGoose := ""
	if withDocker {
		chownOpencode = "chown -R app: /home/app/.config/opencode"
		chownCodex = "chown -R app: /home/app/.codex"
		chownGemini = "chown -R app: /home/app/.gemini"
		chownGoose = "chown -R app: /home/app/.config/goose"
	}

	// Generate Claude MCP setup block (varies by docker mode)
	claudeMCPSetup := `claude_mcp_setup() {
  unset CLAUDECODE
  claude mcp remove --scope user swe-swe-agent-chat 2>/dev/null || true
  claude mcp remove --scope user swe-swe-playwright 2>/dev/null || true
  claude mcp remove --scope user swe-swe-preview 2>/dev/null || true
  claude mcp remove --scope user swe-swe-whiteboard 2>/dev/null || true
  claude mcp remove --scope user swe-swe 2>/dev/null || true
  claude mcp add --scope user --transport stdio swe-swe-agent-chat -- sh -c 'exec npx -y @choonkeat/agent-chat --theme-cookie swe-swe-theme --autocomplete-triggers /=slash-command --autocomplete-url http://localhost:$SWE_SERVER_PORT/api/autocomplete/$SESSION_UUID?key=$MCP_AUTH_KEY'
  claude mcp add --scope user --transport stdio swe-swe-playwright -- sh -c 'exec mcp-lazy-init --init-method POST --init-url http://localhost:$SWE_SERVER_PORT/api/session/$SESSION_UUID/browser/start?key=$MCP_AUTH_KEY -- npx -y @playwright/mcp@latest --cdp-endpoint http://localhost:$BROWSER_CDP_PORT'
  claude mcp add --scope user --transport stdio swe-swe-preview -- sh -c 'exec npx -y @choonkeat/agent-reverse-proxy --bridge http://localhost:$SWE_SERVER_PORT/proxy/$SESSION_UUID/preview/mcp'
  claude mcp add --scope user --transport stdio swe-swe-whiteboard -- npx -y @choonkeat/agent-whiteboard
  claude mcp add --scope user --transport stdio swe-swe -- sh -c 'exec npx -y @choonkeat/agent-reverse-proxy --bridge http://localhost:$SWE_SERVER_PORT/mcp?key=$MCP_AUTH_KEY'
}
`
	if withDocker {
		claudeMCPSetup += `# Run as app user so config goes to /home/app/.claude.json (not /root/)
su -s /bin/bash app -c "$(declare -f claude_mcp_setup); claude_mcp_setup"`
	} else {
		claudeMCPSetup += `claude_mcp_setup`
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
		if strings.Contains(trimmed, "{{IF NO_DOCKER}}") {
			skip = withDocker
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
		if strings.Contains(trimmed, "{{IF CLAUDE}}") {
			skip = !hasAgent("claude")
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

		// Handle CLAUDE_MCP_SETUP placeholder
		if strings.Contains(line, "{{CLAUDE_MCP_SETUP}}") {
			line = strings.ReplaceAll(line, "{{CLAUDE_MCP_SETUP}}", claudeMCPSetup)
		}

		// Handle chown placeholders (empty string when non-DOCKER)
		if strings.Contains(line, "{{CHOWN_OPENCODE}}") {
			line = strings.ReplaceAll(line, "{{CHOWN_OPENCODE}}", chownOpencode)
		}
		if strings.Contains(line, "{{CHOWN_CODEX}}") {
			line = strings.ReplaceAll(line, "{{CHOWN_CODEX}}", chownCodex)
		}
		if strings.Contains(line, "{{CHOWN_GEMINI}}") {
			line = strings.ReplaceAll(line, "{{CHOWN_GEMINI}}", chownGemini)
		}
		if strings.Contains(line, "{{CHOWN_GOOSE}}") {
			line = strings.ReplaceAll(line, "{{CHOWN_GOOSE}}", chownGoose)
		}

		if !skip {
			result = append(result, line)
		}
	}

	return strings.Join(result, "\n")
}

// processTerminalUITemplate processes the terminal-ui.js template with UI customization values
func processTerminalUITemplate(content string, statusBarFontSize int, statusBarFontFamily string, terminalFontSize int, terminalFontFamily string, withVSCode bool) string {
	content = strings.ReplaceAll(content, "{{STATUS_BAR_FONT_SIZE}}", strconv.Itoa(statusBarFontSize))
	content = strings.ReplaceAll(content, "{{STATUS_BAR_FONT_FAMILY}}", statusBarFontFamily)
	content = strings.ReplaceAll(content, "{{TERMINAL_FONT_SIZE}}", strconv.Itoa(terminalFontSize))
	content = strings.ReplaceAll(content, "{{TERMINAL_FONT_FAMILY}}", terminalFontFamily)
	if withVSCode {
		content = strings.ReplaceAll(content, "{{VSCODE_TAB_STYLE}}", "")
		content = strings.ReplaceAll(content, "{{VSCODE_OPTION_ATTR}}", "")
		content = strings.ReplaceAll(content, "{{VSCODE_SERVICE_ENTRY}}", "{ name: 'vscode', label: 'VSCode', url: buildVSCodeUrl(baseUrl, this.workDir) },")
	} else {
		content = strings.ReplaceAll(content, "{{VSCODE_TAB_STYLE}}", "display: none;")
		content = strings.ReplaceAll(content, "{{VSCODE_OPTION_ATTR}}", "hidden disabled")
		content = strings.ReplaceAll(content, "{{VSCODE_SERVICE_ENTRY}}", "")
	}

	return content
}
