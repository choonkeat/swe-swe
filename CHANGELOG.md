# CHANGELOG

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
