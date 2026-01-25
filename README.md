# swe-swe

Your agent: containerized with its own browser for manual testing. Your terminal: pair live or share recordings with teammates. Your sessions: run multiple in parallel, each on its own git worktree.

Works with Claude, Aider, Goose, Gemini, Codex, OpenCode. Not listed? [Let us know](https://github.com/choonkeat/swe-swe/issues)!

https://github.com/user-attachments/assets/2a01ed4a-fa5d-4f86-a999-7439611096a0

## Quick Start

1. **Download the swe-swe binary** for your platform:
   ```bash
   # Build or download swe-swe binary
   # Available binaries are in ./dist/swe-swe after building
   ```

2. **Initialize a project**
   ```bash
   swe-swe init --project-directory /path/to/your/project
   ```

3. **Start the environment**
   ```bash
   swe-swe up --project-directory /path/to/your/project
   ```

4. **Access the services** (use `https://` if initialized with `--ssl=selfsign`)
   - **swe-swe terminal**: http://0.0.0.0:1977
   - **VSCode**: http://0.0.0.0:1977/vscode
   - **Chrome VNC**: http://0.0.0.0:1977/chrome (browser automation viewer)
   - **Traefik dashboard**: http://0.0.0.0:1977/dashboard/

5. **View all initialized projects**
   ```bash
   swe-swe list
   ```

6. **Stop the environment**
   ```bash
   swe-swe down --project-directory /path/to/your/project
   ```

### Requirements

- Docker & Docker Compose installed
- Terminal access (works on macOS, Linux, Windows with WSL)

## Commands

swe-swe has three native commands (`init`, `list`, and `proxy`). All other commands are passed directly to `docker compose` with the project's configuration.

### Native Commands

#### `swe-swe init [options]`

Initializes a new swe-swe project at the specified path. Creates metadata directory at `$HOME/.swe-swe/projects/{sanitized-path}/` with:

- **Dockerfile**: Container image definition with Node.js, Go, and optional tools
- **docker-compose.yml**: Multi-container orchestration
- **traefik-dynamic.yml**: Routing configuration
- **swe-swe-server/**: Source code for the AI terminal server (built at docker-compose time)
- **home/**: Persistent storage for VSCode settings and shell history
- **certs/**: Enterprise certificates (if detected)
- **.path**: Records original project path (used for project discovery)

**Options**:
- `--project-directory PATH`: Project directory (defaults to current directory)
- `--previous-init-flags=reuse`: Reapply saved configuration from previous init (cannot be combined with other flags)
- `--previous-init-flags=ignore`: Ignore saved configuration, use provided flags for fresh init
- `--agents AGENTS`: Comma-separated list of agents to include (default: all)
- `--exclude-agents AGENTS`: Comma-separated list of agents to exclude
- `--apt-get-install PACKAGES`: Additional apt packages to install
- `--npm-install PACKAGES`: Additional npm packages to install globally
- `--with-docker`: Mount Docker socket to allow container to run Docker commands on host
- `--with-slash-commands REPOS`: Git repos to clone as slash commands for Claude, Codex, and OpenCode (space-separated, format: `[alias@]<git-url>`)
- `--ssl MODE`: SSL/TLS mode - `no` (default, HTTP) or `selfsign` (HTTPS with auto-generated self-signed certificate)
- `--copy-home-paths PATHS`: Comma-separated paths relative to `$HOME` to copy into the container's home directory (e.g., `.gitconfig,.ssh/config`)
- `--status-bar-color COLOR`: Status bar background color (default: `#007acc`). Use `--status-bar-color=list` to see available preset colors.
- `--terminal-font-size SIZE`: Terminal font size in pixels (default: `14`)
- `--terminal-font-family FONT`: Terminal font family (default: `Menlo, Monaco, "Courier New", monospace`)
- `--status-bar-font-size SIZE`: Status bar font size in pixels (default: `12`)
- `--status-bar-font-family FONT`: Status bar font family (default: system sans-serif)

**Available Agents**: `claude`, `gemini`, `codex`, `aider`, `goose`, `opencode`

**Examples**:
```bash
# Initialize current directory with all agents (default)
swe-swe init

# Initialize current directory with Claude only (minimal, fastest build)
swe-swe init --agents=claude

# Initialize current directory with Claude and Gemini
swe-swe init --agents=claude,gemini

# Initialize current directory without Python-based agents (no aider)
swe-swe init --exclude-agents=aider

# Initialize current directory with additional system packages
swe-swe init --apt-get-install="vim htop tmux"

# Initialize current directory with Docker access
swe-swe init --with-docker

# Initialize current directory with custom slash commands for Claude/Codex/OpenCode
swe-swe init --with-slash-commands=ck@https://github.com/choonkeat/slash-commands.git

# Initialize with HTTPS (self-signed certificate)
swe-swe init --ssl=selfsign

# Initialize with git and SSH config copied from host
swe-swe init --copy-home-paths=.gitconfig,.ssh/config

# Initialize a specific directory
swe-swe init --project-directory ~/my-project

# Reinitialize with same configuration (after updates)
swe-swe init --previous-init-flags=reuse

# Reinitialize with new configuration (overwrite previous)
swe-swe init --previous-init-flags=ignore --agents=claude
```

**Security Note on `--with-docker`**: Mounting the Docker socket grants the container effective root access to the host. The container can mount host filesystems, run privileged containers, and access other containers. Only use this flag when you trust the code running inside the container (e.g., for your own projects, not untrusted third-party code).

**Dependency Optimization**: The Dockerfile is automatically optimized based on selected agents:
- Python/pip is only installed if `aider` is included
- Node.js/npm is only installed if `claude`, `gemini`, `codex`, or `opencode` is included
- This can significantly reduce image size and build time

Services are accessible via path-based routing on `localhost` (or `0.0.0.0`) at the configured port.

**Note on Port**: The port defaults to `1977` and can be customized via the `SWE_PORT` environment variable (e.g., `SWE_PORT=8080 swe-swe up`). Services are routed based on request paths (e.g., `/vscode`, `/chrome`) rather than subdomains, making it compatible with ngrok, cloudflared, and other tunnel services.

**Enterprise Certificate Support**: If you're behind a corporate firewall or VPN, the init command automatically detects and copies certificates from:
- `NODE_EXTRA_CA_CERTS`
- `SSL_CERT_FILE`
- `NODE_EXTRA_CA_CERTS_BUNDLE`

These are copied to `.swe-swe/certs/` and mounted into all containers.

#### `swe-swe list`

Lists all initialized swe-swe projects and automatically prunes stale ones.

**What it does:**
1. Scans `$HOME/.swe-swe/projects/` directory
2. For each project, reads `.path` file to get original path
3. Checks if original path still exists
4. Auto-removes metadata directories for deleted projects (pruning)
5. Displays remaining active projects with count
6. Shows summary of pruned projects

**When to use:**
- Discover what projects you have initialized
- Clean up metadata for deleted projects
- Verify project paths before cleanup

Example:
```bash
swe-swe list
# Output:
# Initialized projects (2):
#   /Users/alice/projects/myapp
#   /Users/alice/projects/anotherapp
#
# Removed 1 stale project(s)
```

#### `swe-swe proxy <command>`

Bridges the container-host boundary by letting containers execute specific host commands with real-time output streaming. This is the **secure alternative to `--with-docker`** when you only need access to specific commands rather than full Docker socket access.

**When to use proxy vs `--with-docker`:**

| Scenario | Solution |
|----------|----------|
| AI agent needs to run `make` with host toolchain | `swe-swe proxy make` |
| Need `docker build` without exposing full Docker socket | `swe-swe proxy docker` |
| Access host's SSH agent for git operations | `swe-swe proxy git` |
| Full Docker/container management needed | `--with-docker` flag |

**How it works:**
1. Host runs `swe-swe proxy <command>` which watches `.swe-swe/proxy/` for requests
2. A container script is generated at `.swe-swe/proxy/<command>`
3. Container runs `.swe-swe/proxy/<command> [args...]` to submit a request
4. Host executes the command and streams stdout/stderr back to container in real-time
5. Container exits with the command's exit code

**Key features:**
- **Real-time streaming**: Output appears immediately, not after command completes
- **Exit code propagation**: Container script exits with the host command's exit code
- **Stdin forwarding**: Piped/redirected input is forwarded to the host command (e.g., `echo "data" | .swe-swe/proxy/cat`)
- **Concurrent requests**: Multiple container processes can use the same proxy simultaneously
- **Efficient file watching**: Uses inotify when available, falls back to polling

**Examples:**
```bash
# Terminal 1: Start proxy for 'make' command
swe-swe proxy make
# [proxy] Listening for 'make' commands... (Ctrl+C to stop)

# Terminal 2 (inside container): Run make with arguments
.swe-swe/proxy/make build TARGET=hello
# Output streams in real-time, exits with make's exit code

# Multiple proxies (run each in separate terminals)
swe-swe proxy make
swe-swe proxy docker
swe-swe proxy npm
```

**Real-world use cases:**
```bash
# Cross-compile with host toolchain
swe-swe proxy make
# In container: .swe-swe/proxy/make build-linux-arm64

# Run Docker commands without --with-docker
swe-swe proxy docker
# In container: .swe-swe/proxy/docker build -t myapp .

# Use host's authenticated npm registry
swe-swe proxy npm
# In container: .swe-swe/proxy/npm publish
```

**Environment Variables:**
- `PROXY_TIMEOUT`: Timeout in seconds for container script (default: 300)
- `PROXY_DIR`: Override proxy directory (default: `.swe-swe/proxy`)

**File protocol:**
```
.swe-swe/proxy/
├── <command>           # Generated container script (executable)
├── <command>.pid       # Host PID file (prevents duplicate proxies)
├── <uuid>.req          # Request file (NUL-delimited args)
├── <uuid>.stdin        # Request stdin (if piped/redirected input)
├── <uuid>.stdout       # Response stdout (streamed)
├── <uuid>.stderr       # Response stderr (streamed)
└── <uuid>.exit         # Exit code (signals completion)
```

**Cleanup responsibilities:**

| File | Cleaned up by | When |
|------|---------------|------|
| `<uuid>.req` | Host | After reading (claims the request) |
| `<uuid>.stdout/stderr/exit` | Container | On script exit (via trap) |
| `<command>`, `<command>.pid` | Host | On proxy shutdown (Ctrl+C) |
| Orphan response files | Host | On startup (files older than 5 min) |

### Docker Compose Pass-through

All commands other than `init`, `list`, and `proxy` are passed directly to `docker compose` using the project's generated `docker-compose.yml`. This means you can use any docker compose command:

```bash
swe-swe up                    # docker compose up
swe-swe down                  # docker compose down
swe-swe build                 # docker compose build
swe-swe ps                    # docker compose ps
swe-swe logs -f swe-swe       # docker compose logs -f swe-swe
swe-swe exec swe-swe bash     # docker compose exec swe-swe bash
swe-swe restart chrome        # docker compose restart chrome
```

Use `--project-directory` to specify which project (defaults to current directory):
```bash
swe-swe up --project-directory ~/my-project
swe-swe logs -f --project-directory ~/my-project
```

#### `swe-swe up [--project-directory PATH]`

Starts the swe-swe environment using `docker compose up`. The environment includes:

1. **swe-swe**: WebSocket-based AI terminal with session management
2. **chrome**: Headless Chromium with VNC for browser automation (used by MCP Playwright)
3. **code-server**: VS Code IDE running in a container
4. **vscode-proxy**: Nginx proxy for VSCode path routing
5. **traefik**: HTTP reverse proxy with path-based routing rules
6. **auth**: ForwardAuth service for unified password protection

The workspace is mounted at `/workspace` inside containers, allowing bidirectional file access.

Example:
```bash
swe-swe up --project-directory ~/my-project
# Press Ctrl+C to stop
```

**Environment Variables** (passed through automatically):
- `ANTHROPIC_API_KEY`: Claude API key (passed by default)
- `SWE_SWE_PASSWORD`: Authentication password for all services (defaults to `changeme`)
- `SWE_PORT`: External port (defaults to 1977)
- `NODE_EXTRA_CA_CERTS`: Enterprise CA certificate path (auto-copied during init)
- `SSL_CERT_FILE`: SSL certificate file path (auto-copied during init)
- `BROWSER_WS_ENDPOINT`: WebSocket endpoint for browser automation (auto-configured to `ws://chrome:9223`)

**Additional API keys** (uncomment in docker-compose.yml if needed):
- `OPENAI_API_KEY`: OpenAI API key (for Codex)
- `GEMINI_API_KEY`: Google Gemini API key

#### `swe-swe down [--project-directory PATH]`

Stops and removes the running Docker containers for the project.

Example:
```bash
swe-swe down --project-directory ~/my-project
```

#### `swe-swe build [--project-directory PATH]`

Rebuilds the Docker image from scratch (clears cache). Useful when:
- Updating the base image
- Installing new dependencies in Dockerfile
- Testing fresh builds

Example:
```bash
swe-swe build --project-directory ~/my-project
```

#### `swe-swe -h` / `swe-swe --help`

Displays the help message with all available commands.

## Architecture

### Directory Structure

Project metadata is stored in `$HOME/.swe-swe/projects/{sanitized-path}/`:

```
$HOME/.swe-swe/projects/{sanitized-path}/
├── Dockerfile              # Container image definition (multi-stage build)
├── docker-compose.yml      # Service orchestration
├── traefik-dynamic.yml     # HTTP routing rules
├── .dockerignore           # Docker build exclusions
├── .path                   # Original project path (for discovery)
├── init.json               # Saved init flags (for --previous-init-flags=reuse)
├── entrypoint.sh           # Container entrypoint script
├── nginx-vscode.conf       # Nginx config for VSCode proxy
├── swe-swe-server/         # Server source code (built at docker-compose time)
│   ├── go.mod, go.sum
│   ├── main.go
│   └── static/
├── auth/                   # ForwardAuth service source code
├── chrome/                 # Chrome/noVNC service (Dockerfile, configs)
├── home/                   # Persistent VSCode/shell home (volume)
├── certs/                  # Enterprise certificates (if detected)
└── tls/                    # Self-signed TLS certificates (if --ssl=selfsign)
```

**Why metadata is stored outside the project:**
- Prevents container access to infrastructure configuration
- Allows metadata cleanup via `swe-swe list` command
- Centralizes all metadata in one location
- Your project directory remains clean (no `.swe-swe/`)

### Services

#### swe-swe-server
- **Port**: 9898 (inside container)
- **Purpose**: WebSocket-based AI coding assistant terminal
- **Features**:
  - Real-time terminal with PTY support
  - Session management (sessions persist until process exits)
  - Multiple AI assistant detection (claude, gemini, codex, goose, aider, opencode)
  - Automatic process restart on error exit (non-zero exit code); clean exit (code 0) ends session
  - File upload via drag-and-drop (saved to `.swe-swe/uploads/`)
  - In-session chat for collaboration
  - YOLO mode toggle for supported agents (auto-approve actions)
  - **Split-pane UI**: Side panel with Preview, Browser, and Shell tabs for app preview and debugging
  - **Session recordings**: Automatic terminal recording with playback (streaming mode for large recordings)
  - **Debug channel**: Agents can receive console logs, errors, and network requests from the App Preview via `swe-swe-server --debug-listen`

#### chrome
- **Ports**: 9223 (CDP), 6080 (VNC/noVNC)
- **Purpose**: Headless Chromium browser for AI-driven browser automation
- **Features**:
  - Chrome DevTools Protocol (CDP) access via nginx proxy
  - VNC server for visual observation at `/chrome` path (e.g., http://0.0.0.0:1977/chrome)
  - Used by MCP Playwright for browser automation tasks
  - Enterprise SSL certificate support via NSS database
- **Documentation**: See `docs/browser-automation.md`

#### code-server
- **Port**: 8080 (inside container)
- **Purpose**: Full VS Code IDE in the browser
- **Features**:
  - Syntax highlighting, extensions support
  - Direct file editing in `/workspace`
  - Terminal integration

#### traefik
- **Port**: 7000 HTTP or 7443 HTTPS (external port 1977)
- **Purpose**: Reverse proxy and routing with path-based request matching
- **Routing Rules**:
  - `/swe-swe-auth/*` path: Auth service for ForwardAuth (priority 200)
  - `/vscode` path: Routes to code-server with path prefix stripped (priority 100)
  - `/chrome` path: Routes to chrome service with path prefix stripped (priority 100)
  - `/dashboard` path: Traefik dashboard (priority 100)
  - `/` path: Routes to swe-swe-server (priority 10, catch-all)
- **Authentication**: All routes (except auth) protected by ForwardAuth middleware

#### auth
- **Port**: 4180 (internal)
- **Purpose**: ForwardAuth service for unified authentication
- **Features**:
  - Cookie-based session management
  - Redirect to original URL after login
  - Mobile-responsive login page

### Network

Each project gets its own isolated Docker network (`{PROJECT_NAME}_swe-network`). This provides:
- Service discovery by container name within each project
- True network isolation between projects (no DNS conflicts)
- Multiple stacks can run simultaneously without interference

## Configuration

### Customizing the Dockerfile

The Dockerfile is located in `$HOME/.swe-swe/projects/{sanitized-path}/Dockerfile`. Edit it to:
- Add Python: `apt-get install -y python3 python3-pip`
- Add Rust: `curl --proto '=https' --tlsv1.2 -sSf https://sh.rustup.rs | sh -s -- -y`
- Add other tools via `apt-get`

**UID:GID Placeholders**: The Dockerfile supports `{{UID}}` and `{{GID}}` placeholders that are replaced with the host user's UID/GID at init time. This ensures files created in the container have matching permissions on the host, eliminating permission conflicts when editing files from both sides.

### API Keys

**Claude** (passed through by default):
```bash
export ANTHROPIC_API_KEY=sk-ant-...
swe-swe up --project-directory ~/my-project
```

**Gemini/Codex** (requires editing docker-compose.yml to uncomment the env vars):
```bash
# First, edit $HOME/.swe-swe/projects/{sanitized-path}/docker-compose.yml
# Uncomment the GEMINI_API_KEY and/or OPENAI_API_KEY lines in the swe-swe service

export GEMINI_API_KEY=...
export OPENAI_API_KEY=sk-...
swe-swe up --project-directory ~/my-project
```

Alternatively, create a `.env` file in `$HOME/.swe-swe/projects/{sanitized-path}/`:
```
ANTHROPIC_API_KEY=sk-ant-...
GEMINI_API_KEY=...
OPENAI_API_KEY=sk-...
```

### Resource Limits

Edit `$HOME/.swe-swe/projects/{sanitized-path}/docker-compose.yml` to adjust resource constraints:

```yaml
code-server:
  deploy:
    resources:
      limits:
        cpus: '2'
        memory: 2G
```

### VSCode Password

The default VSCode password is `changeme`. To use a custom password, set the `SWE_SWE_PASSWORD` environment variable:

```bash
# Default password (changeme)
swe-swe up --project-directory ~/my-project

# Custom password
SWE_SWE_PASSWORD='my-secure-password' swe-swe up --project-directory ~/my-project
```

## Development

> For detailed build architecture and troubleshooting, see [docs/cli-commands-and-binary-management.md](docs/cli-commands-and-binary-management.md).

### Building from Source

```bash
# Build CLI binaries for all platforms
make build

# Run tests
make test
```

The swe-swe-server is built from source at `docker-compose build` time using a multi-stage Dockerfile. This means:
- No pre-compiled server binaries are embedded in the CLI
- The server is always compiled fresh when the Docker image is built
- Changes to server source code in `cmd/swe-swe/templates/host/swe-swe-server/` are reflected after `swe-swe build`

### Project Structure

```
.
├── cmd/
│   └── swe-swe/              # CLI tool
│       ├── main.go
│       └── templates/        # Embedded Docker/server files
│           └── host/
│               ├── Dockerfile, docker-compose.yml, etc.
│               └── swe-swe-server/   # WebSocket server source
├── docs/                     # Technical documentation
├── Makefile                  # Build targets
├── go.mod, go.sum           # Go dependencies
└── README.md                # This file
```

### Key Dependencies

- **gorilla/websocket**: WebSocket support for real-time terminal
- **creack/pty**: PTY (pseudo-terminal) for shell sessions
- **vt10x**: VT100 terminal emulation
- **uuid**: Session ID generation

## Troubleshooting

### Binary Architecture Error

**Error**: `exec format error`

**Solution**: Rebuild the container image which compiles the server from source:
```bash
swe-swe build --project-directory ~/my-project
```

### Port Already in Use

**Error**: `port 1977 already allocated`

**Solution**: Stop other projects or use a custom port via environment variable:
```bash
# Use a different port
SWE_PORT=8080 swe-swe up --project-directory ~/my-project
```

Alternatively, modify `$HOME/.swe-swe/projects/{sanitized-path}/docker-compose.yml` to change the port mapping.

### API Key Not Found

**Error**: `no AI assistants available`

**Solution**: The server didn't detect an installed assistant:
1. Set API keys: `ANTHROPIC_API_KEY` (Claude), `GEMINI_API_KEY` (Gemini), `OPENAI_API_KEY` (Codex)
2. Or install CLI tools: `claude`, `gemini`, `codex`, `goose`, `aider`, `opencode`

### Network Issues

**Error**: Service not accessible at configured port (default 1977)

**Solution**:
1. Verify Docker is running: `docker ps`
2. Check containers are healthy: `swe-swe ps --project-directory ~/my-project`
3. Check Traefik logs: `swe-swe logs traefik --project-directory ~/my-project`
4. Verify you're using correct paths: `http://0.0.0.0:1977/`, `http://0.0.0.0:1977/vscode`, `http://0.0.0.0:1977/chrome`

### Persistent Home Issues

If VSCode settings/extensions don't persist:
1. Verify `$HOME/.swe-swe/projects/{sanitized-path}/home/` exists and has correct permissions
2. Check that the metadata directory wasn't accidentally deleted
3. Reinitialize the project: `swe-swe init --project-directory /path/to/project`

## Advanced Usage

### Running Multiple Projects

Each project gets its own isolated environment. No conflicts:

```bash
# Terminal 1
swe-swe init --project-directory ~/project1
swe-swe up --project-directory ~/project1

# Terminal 2
swe-swe init --project-directory ~/project2
swe-swe up --project-directory ~/project2
# Use different ports if accessing locally
```

### Custom Shell

Use a custom shell by modifying the Dockerfile CMD:

```dockerfile
ENV SHELL=/bin/zsh
CMD ["/usr/local/bin/swe-swe-server", "-shell", "zsh", "-working-directory", "/workspace", "-addr", "0.0.0.0:9898"]
```

### Session Persistence

Sessions persist until the shell process exits. When a shell exits with code 0 (success), the session ends cleanly. When a shell crashes or exits with a non-zero code, it automatically restarts after 500ms.

To customize the shell command, modify the Dockerfile CMD:

```dockerfile
CMD ["/usr/local/bin/swe-swe-server", "-shell", "bash", "-working-directory", "/workspace", "-addr", "0.0.0.0:9898"]
```

### YOLO Mode

YOLO mode allows agents to auto-approve actions without user confirmation. Toggle it via:
- **Status bar**: Click "Connected" text (changes to "YOLO" when active)
- **Settings panel**: Use the YOLO toggle switch

**Supported agents and their YOLO commands:**

| Agent | YOLO Command |
|-------|--------------|
| Claude | `--dangerously-skip-permissions` |
| Gemini | `--approval-mode=yolo` |
| Codex | `--yolo` |
| Goose | `GOOSE_MODE=auto` |
| Aider | `--yes-always` |
| OpenCode | Not supported (toggle hidden) |

When toggled, the agent restarts with the appropriate YOLO flag. The status bar shows "YOLO" with a border indicator when active.

### App Preview Debugging

The App Preview (port 3000 by default) includes a debug channel that forwards browser events to agents. This lets agents debug what users see without needing visual access.

**Listening for debug messages:**
```bash
# Inside container - receive console logs, errors, fetch/XHR requests
swe-swe-server --debug-listen
```

Output is JSON lines:
```json
{"t":"console","m":"log","args":["Hello!"],"ts":...}
{"t":"error","msg":"Uncaught TypeError","stack":"...","ts":...}
{"t":"fetch","url":"/api/users","method":"GET","status":200,"ms":45,"ts":...}
```

**Querying DOM elements:**
```bash
# Query element by CSS selector
swe-swe-server --debug-query ".error-message"
```

Response:
```json
{"t":"queryResult","found":true,"text":"Invalid email","visible":true}
```

**Prerequisites:**
- User must have the Preview tab open in the split-pane UI
- App must be running on port 3000 (or `SWE_PREVIEW_TARGET_PORT`)

For detailed usage, see the `/debug-with-app-preview` slash command.

### Authentication

All services are protected by ForwardAuth by default. The authentication password is set via the `SWE_SWE_PASSWORD` environment variable (defaults to `changeme`).

```bash
# Use default password
swe-swe up --project-directory ~/my-project

# Use custom password
SWE_SWE_PASSWORD='my-secure-password' swe-swe up --project-directory ~/my-project
```

The auth service provides:
- Cookie-based session management (expires when browser closes)
- Redirect to original URL after login
- Mobile-responsive login page

## API Reference

### WebSocket Protocol

The swe-swe-server provides a WebSocket endpoint at `/ws/{sessionId}` for terminal communication.

**Message Format**:
```json
{
  "type": "input" | "resize",
  "data": "shell input" | {"rows": 24, "cols": 80},
  "sessionId": "uuid"
}
```

See `docs/websocket-protocol.md` for detailed specification.

## Contributing

### Code Style

Follow Go conventions:
```bash
make fmt   # Format code
make test  # Run tests
```

### Adding Features

1. Modify the appropriate component (CLI in `cmd/swe-swe`, server in `cmd/swe-swe/templates/host/swe-swe-server`)
2. Test locally: `make build && swe-swe init && swe-swe up`
3. Commit with conventional commits: `fix:`, `feat:`, `docs:`, etc.

## License

See LICENSE file for details.
