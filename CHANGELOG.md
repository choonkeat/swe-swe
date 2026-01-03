# CHANGELOG

## v2.4.0 - CLI Improvements & Docker Integration

- **`--with-docker` flag**: Enable Docker-in-Docker with socket mounting for agents to run Docker commands
- **`--with-slash-commands` flag**: Clone custom slash command repositories into container
- **`--previous-init-flags` flag**: Reuse init flags from previous initialization
- **CLI passthrough refactor**: Simplify CLI to pass commands directly to docker compose
- **Homepage redesign**: Unified layout showing active sessions with creation timestamps
- **Password manager fix**: Add username field for 1Password/browser autofill compatibility

## v2.3.0 - Authentication & Mobile Terminal

- **ForwardAuth authentication**: Unified password protection for all services (vscode, terminal, chrome, traefik dashboard)
- **Mobile terminal toolbar**: Add Paste button and Ctrl modifier for mobile keyboards
- **Docker Compose v2 support**: Support both `docker compose` and `docker-compose`
- **Build refactor**: Build swe-swe-server at compose time instead of embedding binary

## v2.2.0 - Path-Based Routing

- **Migrate to path-based routing**: Replace subdomain routing (`vscode.domain`, `chrome.domain`) with path-based (`/vscode`, `/chrome`) to support ngrok/cloudflared tunnels
- **Status bar links**: Add clickable links to vscode, browser, agent in terminal UI
- **Chrome/noVNC fixes**: Fix WebSocket paths, SSL certificates in NSS database

## v2.1.0 - Browser Automation & Project Management

- **Browser automation**: Chrome sidecar with MCP Playwright for AI-controlled web browsing via noVNC
- **`swe-swe list` command**: List projects with auto-prune for missing paths
- **Metadata relocation**: Move project metadata from `.swe-swe/` to `~/.swe-swe/projects/` (security: outside container reach)
- **Multi-agent support**: Add `--agents`, `--exclude-agents`, `--apt-get-install`, `--npm-install` flags
- **Enterprise SSL certs**: Install certificates into container for corporate proxies
- **Various Docker fixes**: Node.js 24 LTS upgrade, permission fixes, resource limit adjustments

## v2.0.0 - Terminal UI Rewrite

**Breaking change:** Complete architecture rewrite from web-chat to terminal-based UI.

- **xterm.js terminal**: Full terminal experience replacing chat interface
- **WebSocket multiplexing**: Multi-viewer session support with reconnection
- **Docker Compose orchestration**: Traefik reverse proxy, code-server integration
- **`swe-swe` CLI**: New CLI for `init`, `up`, `down`, `build` commands
- **Agent support**: Claude Code, Aider, Goose, Gemini CLI, Codex CLI
