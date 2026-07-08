package main

import (
	_ "embed"
	"fmt"
	"strconv"
	"strings"
)

// Claude hook guard scripts, single-sourced here so the container entrypoint
// (via {{STOP_GUARD_SCRIPT}}/{{ASK_GUARD_SCRIPT}} placeholders) and dockerless
// init (writeDockerlessHooks) can never drift apart.
//
//go:embed hook-scripts/swe-swe-stop-guard.sh
var stopGuardScript string

//go:embed hook-scripts/swe-swe-ask-guard.sh
var askGuardScript string

// processDockerfileTemplate processes the Dockerfile template with conditional sections
// based on selected agents, custom apt packages, custom npm packages, Docker access, enterprise certificates, slash commands, skills, and tunnel mode
func processDockerfileTemplate(content string, agents []string, aptPackages, npmPackages string, withDocker bool, hasCerts bool, slashCommands []SlashCommandsRepo, skills []SkillsRepo, hostUID int, hostGID int, tunnelServerURL string) string {
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

	// Skills are agent-agnostic: the autocomplete surfaces them in every
	// session and we let the agent decide whether to dispatch. So presence is
	// purely "did the user pass --with-skills".
	hasSkills := len(skills) > 0

	// Tunnel mode: when --tunnel-server-url was set, the Dockerfile builds
	// the swe-swe-tunnel client binary alongside swe-swe-server so the
	// supervisor can spawn it without any extra setup. See
	// tasks/2026-04-29-tunnel-subprocess-pivot.md.
	isTunnel := tunnelServerURL != ""

	// Generate slash commands clone lines
	var slashCommandsClone string
	if hasSlashCommands {
		var cloneLines []string
		for _, repo := range slashCommands {
			cloneLines = append(cloneLines, fmt.Sprintf("RUN git clone --depth 1 %s /tmp/slash-commands/%s", repo.URL, repo.Alias))
		}
		slashCommandsClone = strings.Join(cloneLines, "\n")
	}

	// Generate skills clone lines
	var skillsClone string
	if hasSkills {
		var cloneLines []string
		for _, repo := range skills {
			cloneLines = append(cloneLines, fmt.Sprintf("RUN git clone --depth 1 %s /tmp/skills/%s", repo.URL, repo.Alias))
		}
		skillsClone = strings.Join(cloneLines, "\n")
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
			case "SKILLS":
				skip = !hasSkills
			case "TUNNEL":
				skip = !isTunnel
			case "NO_TUNNEL":
				skip = isTunnel
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

		// Handle SKILLS_CLONE placeholder
		if strings.Contains(line, "{{SKILLS_CLONE}}") {
			if skillsClone != "" {
				line = strings.ReplaceAll(line, "{{SKILLS_CLONE}}", skillsClone)
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
func processSimpleTemplate(content string, withDocker bool, ssl string, hostUID int, hostGID int, email string, domain string, reposDir string, previewPorts []int, publicPorts []int, proxyPortOffset int, tunnelServerURL string, tunnelClientCert string, tunnelLocalPorts bool) string {
	lines := strings.Split(content, "\n")
	var result []string

	// SSL mode detection
	isSSL := strings.HasPrefix(ssl, "selfsign") || strings.HasPrefix(ssl, "letsencrypt")
	isLetsEncrypt := strings.HasPrefix(ssl, "letsencrypt")
	isSelfSign := strings.HasPrefix(ssl, "selfsign")
	isLetsEncryptStaging := strings.HasPrefix(ssl, "letsencrypt-staging")
	// Tunnel mode: when SWE_TUNNEL_SERVER_URL was set at init time, the
	// generated compose drops the entire traefik: service plus per-port
	// labels and binds swe-swe-server to 127.0.0.1 only. The tunnel client
	// (a child process of swe-swe-server) handles all inbound traffic via
	// the public tunneld. See tasks/2026-04-29-tunnel-subprocess-pivot.md.
	isTunnel := tunnelServerURL != ""
	// Tunnel mTLS mode: only active when both the tunnel server URL and a
	// host-side client cert are configured. Drives the extra volume mount
	// and SWE_TUNNEL_CLIENT_CERT env block in docker-compose.yml.
	isTunnelClientCert := tunnelClientCert != ""
	// Tunnel local-ports mode: only meaningful in tunnel mode. Widens the
	// swe-swe-server bind to all interfaces and publishes SWE_PORT plus the
	// preview/agent-chat/vnc/public ranges on the host's 127.0.0.1 so the
	// machine running 'swe-swe up' can reach the containers directly. The
	// tunnel client still dials 127.0.0.1:{port} internally (covered by the
	// all-interfaces bind), so the tunnel path is unaffected.
	isTunnelLocalPorts := isTunnel && tunnelLocalPorts
	// {{TUNNEL_BIND}} expands to the swe-swe-server bind address used by both
	// the command's -bind flag and the SWE_BIND env. Default tunnel mode binds
	// 127.0.0.1 only; --tunnel-local-ports widens it to all interfaces so the
	// published host ports can reach it (Docker forwards published ports to the
	// container's eth0, not its loopback).
	tunnelBind := "127.0.0.1:${SWE_PORT:-1977}"
	if isTunnelLocalPorts {
		tunnelBind = ":${SWE_PORT:-1977}"
	}

	// Conditional state is a stack of bools (one entry per open {{IF X}}).
	// A line is emitted only when EVERY frame on the stack is "include"
	// (false for skip). This supports nested conditionals -- the original
	// flat skip-flag design broke when {{IF NO_TUNNEL}} wrapped existing
	// {{IF SSL}}/{{ENDIF}} pairs because the inner ENDIF cleared skip
	// while we were still nested in the outer block.
	var stack []bool
	skipNow := func() bool {
		for _, s := range stack {
			if s {
				return true
			}
		}
		return false
	}

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		// Handle conditional markers (support both # and yaml-style comments)
		if strings.Contains(trimmed, "{{IF DOCKER}}") {
			stack = append(stack, !withDocker)
			continue
		}

		if strings.Contains(trimmed, "{{IF SSL}}") {
			stack = append(stack, !isSSL)
			continue
		}

		if strings.Contains(trimmed, "{{IF NO_SSL}}") {
			stack = append(stack, isSSL)
			continue
		}

		if strings.Contains(trimmed, "{{IF LETSENCRYPT}}") {
			stack = append(stack, !isLetsEncrypt)
			continue
		}

		if strings.Contains(trimmed, "{{IF SELFSIGN}}") {
			stack = append(stack, !isSelfSign)
			continue
		}

		if strings.Contains(trimmed, "{{IF LETSENCRYPT_STAGING}}") {
			stack = append(stack, !isLetsEncryptStaging)
			continue
		}

		if strings.Contains(trimmed, "{{IF LETSENCRYPT_PRODUCTION}}") {
			stack = append(stack, isLetsEncryptStaging || !isLetsEncrypt)
			continue
		}

		if strings.Contains(trimmed, "{{IF TUNNEL}}") {
			stack = append(stack, !isTunnel)
			continue
		}

		if strings.Contains(trimmed, "{{IF NO_TUNNEL}}") {
			stack = append(stack, isTunnel)
			continue
		}

		if strings.Contains(trimmed, "{{IF TUNNEL_CLIENT_CERT}}") {
			stack = append(stack, !isTunnelClientCert)
			continue
		}

		if strings.Contains(trimmed, "{{IF TUNNEL_LOCAL_PORTS}}") {
			stack = append(stack, !isTunnelLocalPorts)
			continue
		}

		if strings.Contains(trimmed, "{{ENDIF}}") {
			if len(stack) > 0 {
				stack = stack[:len(stack)-1]
			}
			continue
		}
		skip := skipNow()

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

			if strings.Contains(line, "{{FILES_ENTRYPOINTS}}") {
				indent := strings.Split(line, "{{FILES_ENTRYPOINTS}}")[0]
				for _, port := range previewPorts {
					fp := filesPort(port)
					entrypoint := fmt.Sprintf("files%d", fp)
					pp := filesProxyPort(fp, proxyPortOffset)
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

			// Tunnel mode + --tunnel-local-ports: publish the in-container
			// listeners on the host's 127.0.0.1. Mirrors the non-tunnel port
			// set (SWE_PORT + preview/agent-chat/vnc/files proxy ranges +
			// public range) but host-scoped to loopback and attached directly
			// to the swe-swe service (no Traefik in tunnel mode).
			if strings.Contains(line, "{{TUNNEL_LOCAL_PORTS}}") {
				indent := strings.Split(line, "{{TUNNEL_LOCAL_PORTS}}")[0]
				// Main swe-swe-server port.
				result = append(result, fmt.Sprintf("%s- \"127.0.0.1:${SWE_PORT:-1977}:${SWE_PORT:-1977}\"", indent))
				// Preview proxy ports.
				for _, port := range previewPorts {
					pp := previewProxyPort(port, proxyPortOffset)
					result = append(result, fmt.Sprintf("%s- \"127.0.0.1:%d:%d\"", indent, pp, pp))
				}
				// Agent chat proxy ports.
				for _, port := range previewPorts {
					acPort := agentChatPort(port)
					pp := agentChatProxyPort(acPort, proxyPortOffset)
					result = append(result, fmt.Sprintf("%s- \"127.0.0.1:%d:%d\"", indent, pp, pp))
				}
				// VNC proxy ports.
				for _, port := range previewPorts {
					vp := vncPort(port)
					pp := vncProxyPort(vp, proxyPortOffset)
					result = append(result, fmt.Sprintf("%s- \"127.0.0.1:%d:%d\"", indent, pp, pp))
				}
				// Files proxy ports.
				for _, port := range previewPorts {
					fp := filesPort(port)
					pp := filesProxyPort(fp, proxyPortOffset)
					result = append(result, fmt.Sprintf("%s- \"127.0.0.1:%d:%d\"", indent, pp, pp))
				}
				// Public ports (no offset; the app binds these directly).
				for _, port := range publicPorts {
					result = append(result, fmt.Sprintf("%s- \"127.0.0.1:%d:%d\"", indent, port, port))
				}
				continue
			}

			if strings.Contains(line, "{{FILES_PORTS}}") {
				indent := strings.Split(line, "{{FILES_PORTS}}")[0]
				for _, port := range previewPorts {
					fp := filesPort(port)
					pp := filesProxyPort(fp, proxyPortOffset)
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

			if strings.Contains(line, "{{FILES_ROUTERS}}") {
				indent := strings.Split(line, "{{FILES_ROUTERS}}")[0]
				for _, port := range previewPorts {
					fp := filesPort(port)
					entrypoint := fmt.Sprintf("files%d", fp)
					routerName := fmt.Sprintf("${PROJECT_NAME}-files-%d", fp)
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
					result = append(result, fmt.Sprintf("%s- \"traefik.http.services.%s.loadbalancer.server.port=%d\"", indent, routerName, fp))
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
			// Handle REPOS_DIR placeholder - default to .swe-swe/repos if not specified
			if strings.Contains(line, "{{REPOS_DIR}}") {
				reposDirValue := reposDir
				if reposDirValue == "" {
					reposDirValue = "${WORKSPACE_DIR:-.}/.swe-swe/repos"
				}
				line = strings.ReplaceAll(line, "{{REPOS_DIR}}", reposDirValue)
			}
			if strings.Contains(line, "{{TUNNEL_SERVER_URL}}") {
				line = strings.ReplaceAll(line, "{{TUNNEL_SERVER_URL}}", tunnelServerURL)
			}
			if strings.Contains(line, "{{TUNNEL_CLIENT_CERT}}") {
				line = strings.ReplaceAll(line, "{{TUNNEL_CLIENT_CERT}}", tunnelClientCert)
			}
			if strings.Contains(line, "{{TUNNEL_BIND}}") {
				line = strings.ReplaceAll(line, "{{TUNNEL_BIND}}", tunnelBind)
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

func filesPort(previewPort int) int {
	return previewPort + 6000
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

func filesProxyPort(port, offset int) int {
	return offset + port
}

// processEntrypointTemplate handles the entrypoint.sh template with DOCKER, SLASH_COMMANDS, and SKILLS conditions
func processEntrypointTemplate(content string, agents []string, withDocker bool, slashCommands []SlashCommandsRepo, skills []SkillsRepo) string {
	// Helper to check if agent is selected
	hasAgent := func(agent string) bool {
		return agentInList(agent, agents)
	}

	// Check if we have slash commands for supported agents (claude, codex, opencode, or pi)
	hasSlashCommands := len(slashCommands) > 0 && (hasAgent("claude") || hasAgent("codex") || hasAgent("opencode") || hasAgent("pi"))

	// Skills are agent-agnostic; presence is just "did the user pass --with-skills"
	hasSkills := len(skills) > 0

	// Generate slash commands copy/link lines. Custom slash command repos are
	// materialized once into a swe-swe-owned canonical store, then projected into
	// each selected agent's native command directory with symlinks.
	var slashCommandsCopy string
	if hasSlashCommands {
		var copyLines []string
		for _, repo := range slashCommands {
			centralDir := "/home/app/.swe-swe/commands/md/" + repo.Alias
			pullCmd := fmt.Sprintf(`(cd %s && git pull) 2>/dev/null`, centralDir)
			if withDocker {
				pullCmd = fmt.Sprintf(`su -s /bin/bash app -c "cd %s && git pull" 2>/dev/null`, centralDir)
			}
			chownLine := ""
			if withDocker {
				chownLine = fmt.Sprintf("\n    chown -R app:app %s", centralDir)
			}
			copyLines = append(copyLines, fmt.Sprintf(`if [ -d "%s/.git" ]; then
    # Try to pull updates (best effort)
    git config --global --add safe.directory %s 2>/dev/null || true
    %s && \
        echo -e "${GREEN}[ok] Updated slash commands: %s (swe-swe store)${NC}" || \
        echo -e "${YELLOW}⚠ Could not update slash commands: %s (swe-swe store)${NC}"
elif [ -d "/tmp/slash-commands/%s" ]; then
    mkdir -p "$(dirname "%s")"
    cp -r /tmp/slash-commands/%s %s%s
    echo -e "${GREEN}[ok] Installed slash commands: %s (swe-swe store)${NC}"
fi`, centralDir, centralDir, pullCmd, repo.Alias, repo.Alias, repo.Alias, centralDir, repo.Alias, centralDir, chownLine, repo.Alias))

			genLinkBlock := func(agentName, configDir string) string {
				linkPath := configDir + "/" + repo.Alias
				return fmt.Sprintf(`if [ -e "%s" ] && [ ! -L "%s" ]; then
    echo -e "${YELLOW}⚠ Slash command target exists and is not a symlink, leaving unchanged: %s (%s)${NC}"
elif [ -d "%s" ]; then
    mkdir -p "$(dirname "%s")"
    ln -sfn %s %s
    echo -e "${GREEN}[ok] Linked slash commands: %s (%s)${NC}"
fi`, linkPath, linkPath, linkPath, agentName, centralDir, linkPath, centralDir, linkPath, repo.Alias, agentName)
			}
			if hasAgent("claude") {
				copyLines = append(copyLines, genLinkBlock("claude", "/home/app/.claude/commands"))
			}
			if hasAgent("codex") {
				copyLines = append(copyLines, genLinkBlock("codex", "/home/app/.codex/prompts"))
			}
			if hasAgent("opencode") {
				copyLines = append(copyLines, genLinkBlock("opencode", "/home/app/.config/opencode/command"))
			}
			if hasAgent("pi") {
				copyLines = append(copyLines, genLinkBlock("pi", "/home/app/.pi/agent/prompts"))
			}
		}
		slashCommandsCopy = strings.Join(copyLines, "\n")
	}

	// Generate skills install lines. The persistent clone lives at
	// ~/.swe-swe/skills-src/<alias>/ (so subsequent entrypoint runs can
	// git-pull updates), and each SKILL.md's parent directory is symlinked
	// flat into ~/.swe-swe/skills/<alias>-<dirname>/ so autocomplete can scan
	// one level. The <alias>- prefix avoids collisions across repos; within a
	// repo, if two skill dirs share a leaf name (in different folders), the
	// second is installed under its repo-relative path (<alias>-<a>-<b>-<name>)
	// with a warning, so no skill is silently dropped by an ln -sfn overwrite.
	// The flat store name is the autocomplete handle, so every installed skill
	// stays distinct and addressable.
	var skillsInstall string
	if hasSkills {
		var installLines []string
		for _, repo := range skills {
			srcDir := "/home/app/.swe-swe/skills-src/" + repo.Alias
			pullCmd := fmt.Sprintf(`(cd %s && git pull) 2>/dev/null`, srcDir)
			if withDocker {
				pullCmd = fmt.Sprintf(`su -s /bin/bash app -c "cd %s && git pull" 2>/dev/null`, srcDir)
			}
			chownLine := ""
			if withDocker {
				chownLine = fmt.Sprintf("\n    chown -R app:app %s", srcDir)
			}
			installLines = append(installLines, fmt.Sprintf(`if [ -d "%s/.git" ]; then
    git config --global --add safe.directory %s 2>/dev/null || true
    %s && \
        echo -e "${GREEN}[ok] Updated skills: %s${NC}" || \
        echo -e "${YELLOW}[warn] Could not update skills: %s${NC}"
elif [ -d "/tmp/skills/%s" ]; then
    mkdir -p "$(dirname "%s")"
    cp -r /tmp/skills/%s %s%s
    echo -e "${GREEN}[ok] Installed skills: %s${NC}"
fi
mkdir -p /home/app/.swe-swe/skills
# Drop this repo's previously-installed symlinks first so renamed or removed
# skills don't linger, and so the clash check below only sees this run's links.
find /home/app/.swe-swe/skills -maxdepth 1 -type l -name '%s-*' -delete 2>/dev/null || true
find %s -name SKILL.md -type f 2>/dev/null | sort | while read -r skill_file; do
    skill_dir=$(dirname "$skill_file")
    skill_link="/home/app/.swe-swe/skills/%s-$(basename "$skill_dir")"
    if [ -e "$skill_link" ]; then
        # Same leaf name in two folders of one repo: disambiguate with the
        # path relative to the repo root so neither skill is silently dropped.
        skill_rel=$(printf '%%s' "${skill_dir#%s/}" | tr '/' '-')
        skill_link="/home/app/.swe-swe/skills/%s-${skill_rel}"
        echo -e "${YELLOW}[warn] skill name clash; installing as $(basename "$skill_link")${NC}"
    fi
    ln -sfn "$skill_dir" "$skill_link"
done`,
				srcDir,
				srcDir,
				pullCmd,
				repo.Alias,
				repo.Alias,
				repo.Alias,
				srcDir,
				repo.Alias,
				srcDir,
				chownLine,
				repo.Alias,
				repo.Alias,
				srcDir,
				repo.Alias,
				srcDir,
				repo.Alias,
			))
		}
		skillsInstall = strings.Join(installLines, "\n")
	}

	// Generate chown lines (only needed when running as root in DOCKER mode)
	chownOpencode := ""
	chownCodex := ""
	chownGemini := ""
	chownGoose := ""
	chownPi := ""
	chownClaude := ""
	if withDocker {
		chownOpencode = "chown -R app: /home/app/.config/opencode"
		chownCodex = "chown -R app: /home/app/.codex"
		chownGemini = "chown -R app: /home/app/.gemini"
		chownGoose = "chown -R app: /home/app/.config/goose"
		chownPi = "chown -R app: /home/app/.pi"
		chownClaude = "chown -R app: /home/app/.claude"
	}

	// Generate Claude MCP setup block (varies by docker mode)
	claudeMCPSetup := `claude_mcp_setup() {
  unset CLAUDECODE
  claude mcp remove --scope user swe-swe-agent-chat 2>/dev/null || true
  claude mcp remove --scope user swe-swe-playwright 2>/dev/null || true
  claude mcp remove --scope user swe-swe-preview 2>/dev/null || true
  claude mcp remove --scope user swe-swe-whiteboard 2>/dev/null || true
  claude mcp remove --scope user swe-swe 2>/dev/null || true
  claude mcp add --scope user --transport stdio swe-swe-agent-chat -- sh -c 'exec npx -y @choonkeat/agent-chat --theme-cookie swe-swe-theme --welcome-replies "What can you help me with?,Give me an overview of this project,What has changed recently?,/swe-swe:recordings-list-orphaned" --autocomplete-triggers /=slash-command --autocomplete-url http://localhost:$SWE_SERVER_PORT/api/autocomplete/$SESSION_UUID?key=$MCP_AUTH_KEY'
  claude mcp add --scope user --transport stdio swe-swe-playwright -- sh -c 'exec mcp-lazy-init --init-method POST --init-url http://localhost:$SWE_SERVER_PORT/api/session/$SESSION_UUID/browser/start?key=$MCP_AUTH_KEY -- npx -y @playwright/mcp@latest --cdp-endpoint http://localhost:$BROWSER_CDP_PORT'
  claude mcp add --scope user --transport stdio swe-swe-preview -- sh -c 'exec npx -y @choonkeat/agent-reverse-proxy --bridge http://localhost:$SWE_SERVER_PORT/proxy/$SESSION_UUID/preview/mcp?key=$MCP_AUTH_KEY'
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
		if strings.Contains(trimmed, "{{IF SKILLS}}") {
			skip = !hasSkills
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
		if strings.Contains(trimmed, "{{IF PI}}") {
			skip = !hasAgent("pi")
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

		// Handle SKILLS_INSTALL placeholder
		if strings.Contains(line, "{{SKILLS_INSTALL}}") {
			if skillsInstall != "" {
				line = strings.ReplaceAll(line, "{{SKILLS_INSTALL}}", skillsInstall)
			}
		}

		// Handle CLAUDE_MCP_SETUP placeholder
		if strings.Contains(line, "{{CLAUDE_MCP_SETUP}}") {
			line = strings.ReplaceAll(line, "{{CLAUDE_MCP_SETUP}}", claudeMCPSetup)
		}

		// Handle hook guard script placeholders (heredoc bodies in entrypoint.sh)
		if strings.Contains(line, "{{STOP_GUARD_SCRIPT}}") {
			line = strings.ReplaceAll(line, "{{STOP_GUARD_SCRIPT}}", strings.TrimRight(stopGuardScript, "\n"))
		}
		if strings.Contains(line, "{{ASK_GUARD_SCRIPT}}") {
			line = strings.ReplaceAll(line, "{{ASK_GUARD_SCRIPT}}", strings.TrimRight(askGuardScript, "\n"))
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
		if strings.Contains(line, "{{CHOWN_PI}}") {
			line = strings.ReplaceAll(line, "{{CHOWN_PI}}", chownPi)
		}
		if strings.Contains(line, "{{CHOWN_CLAUDE}}") {
			line = strings.ReplaceAll(line, "{{CHOWN_CLAUDE}}", chownClaude)
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
