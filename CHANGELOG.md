# CHANGELOG

## v2.8.0 - Shell Terminal, Heartbeat Cleanup & Deployment Automation

### Major Features

- **Shell assistant**: Direct terminal access without an AI agent. Launch with `--with-agents=shell` to access a dedicated terminal session with parent workDir inheritance for seamless file navigation
- **Heartbeat-based container cleanup**: Automated detection and graceful shutdown of stale containers via host-side heartbeat watcher with configurable timeout and signal escalation (SIGTERM→SIGKILL)
- **Container-host proxy**: New lightweight proxy bridging container and host communication for lifecycle management and health monitoring
- **DigitalOcean 1-click deployment**: Automated Packer-based image building with optional git repository cloning, hardening, MOTD health checks, and interactive init flags support
- **Bundled slash commands**: Ship swe-swe slash commands in binary, auto-installed to `~/.claude/commands/swe-swe/` with conditional `/workspace/swe-swe/` directory creation
- **Record-tui integration**: Replace custom playback with `record-tui` library for improved terminal recording and playback with speed controls

### Terminal Improvements

- **Link activation hints**: Visual hints for clickable terminal links with required modifier keys (Ctrl/Cmd) to prevent accidental activation
- **URL underline and copy**: Terminal URLs display with underlines; clicking shows copy notification for easy sharing
- **File copy notifications**: Visual feedback when file paths are copied from terminal output

### MCP & Agent Enhancements

- **MCP server rename**: Renamed `playwright` MCP server to `swe-swe-playwright` to avoid config conflicts
- **Generated MCP configs**: Auto-generate MCP configuration for OpenCode, Codex, Gemini, and Goose agents
- **OpenCode support**: Extend `--with-slash-commands` to support OpenCode (`~/.config/opencode/command/`)

### Behavior Changes

- **MOTD suppression**: Suppress MOTD for shell sessions to reduce noise
- **Streaming proxy output**: Real-time stdout/stderr streaming from container-host proxy

### Bug Fixes

- **Go module imports**: Fix missing golang.org/x/text imports for unicode normalization
- **Worktree permissions**: Ensure `/worktrees` directory has proper permissions in container
- **Traefik compatibility**: Downgrade to v2.11 for Docker API compatibility
- **Cloud-init race conditions**: Wait for cloud-final.target instead of cloud-init.target
- **systemd service startup**: Fix dependency issues causing startup race conditions

## v2.7.0 - YOLO Mode, Settings Panel & UI Customization

### Major Features

- **YOLO mode toggle**: Click "Connected" in status bar or use settings panel to toggle agents between normal and auto-approve mode. Supports Claude (`--dangerously-skip-permissions`), Gemini (`--approval-mode=yolo`), Codex (`--yolo`), Goose (`GOOSE_MODE=auto`), Aider (`--yes-always`)
- **Settings panel**: New mobile-responsive settings panel (status bar → click) with runtime customization of username, session name, and status bar color. Includes navigation links to homepage, VSCode, and browser
- **Clickable terminal colors**: CSS colors in terminal output (e.g., `#ff5500`) become clickable links to set status bar color
- **UI customization flags**: New `swe-swe init` flags for theming:
  - `--status-bar-color COLOR` with auto-contrast text and ANSI color swatches (`--status-bar-color=list`)
  - `--terminal-font-size`, `--terminal-font-family`
  - `--status-bar-font-size`, `--status-bar-font-family`

### Mobile Improvements

- **Touch scroll proxy**: Native iOS momentum scrolling with rubber band effect
- **Virtual keyboard handling**: Terminal resizes when keyboard appears, mobile keyboard bar stays visible
- **Touch event fixes**: Fixed z-index for status bar touch interactions

### Behavior Changes

- **Process exit handling**: All process exits now end the session (removed automatic crash-restart). Process replacement only occurs via explicit user action (YOLO toggle)

### Bug Fixes

- **WebSocket panic fix**: Prevent concurrent write panic with SafeConn wrapper
- **PTY cleanup**: Kill process when PTY broken but process still alive
- **Status bar legibility**: Improved text contrast across connection states
- **Worktree symlinks**: Symlink directories instead of copying for faster worktree creation

## v2.6.1 - Simplified Worktree Exit

- **Simplified exit flow**: Remove worktree merge/discard modal - exits now behave like regular sessions (see ADR-0022)

## v2.6.0 - Terminal Recording & Git Worktrees

- **Terminal recording**: Record sessions with playback UI, speed controls, and auto-cleanup (Recent vs Kept model with max 5 per agent, 1h expiry)
- **Git worktrees**: Named sessions create isolated branches with worktree re-entry, exit prompts for merge/discard, and automatic copying of `.env`, `.claude/`, and dotfiles
- **`--copy-home-paths` flag**: Copy host `$HOME` paths into container (e.g., `--copy-home-paths=.gitconfig,.ssh/config`)
- **Bundled slash commands**: Ship swe-swe slash commands in binary, auto-installed to `~/.claude/commands/swe-swe/`
- **OpenCode slash commands**: Extend `--with-slash-commands` to support OpenCode (`~/.config/opencode/command/`)

## v2.5.0 - OpenCode Agent Support

- **OpenCode agent**: Add support for OpenCode (https://github.com/anomalyco/opencode) as the 6th AI assistant
- **npm-based installation**: OpenCode installed via `npm install -g opencode-ai` for reliable Docker builds
- **Session resume**: Support `opencode --continue` for session recovery after crashes

## v2.4.1 - Documentation Fix

- **Fix `--project-directory` documentation**: Correct argument order in help text and README—subcommand must come before the flag (e.g., `swe-swe up --project-directory /path`)

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
- **Agent support**: Claude Code, Aider, Goose, Gemini CLI, Codex CLI, OpenCode
