# swe-swe

A containerized development environment for AI-assisted coding with integrated VSCode, browser automation, Traefik reverse proxy, and support for multiple AI assistants (Claude, Gemini, Codex, Goose, Aider).

https://github.com/user-attachments/assets/2a01ed4a-fa5d-4f86-a999-7439611096a0

## Quick Start

1. **Download the swe-swe binary** for your platform:
   ```bash
   # Build or download swe-swe binary
   # Available binaries are in ./dist/swe-swe after building
   ```

2. **Initialize a project**
   ```bash
   swe-swe init --path /path/to/your/project
   ```

3. **Start the environment**
   ```bash
   swe-swe up --path /path/to/your/project
   ```

4. **Access the services**
   - **swe-swe terminal**: http://0.0.0.0:9899
   - **VSCode**: http://0.0.0.0:9899/vscode
   - **Chrome VNC**: http://0.0.0.0:9899/chrome (browser automation viewer)
   - **Traefik dashboard**: http://localhost:9900

5. **View all initialized projects**
   ```bash
   swe-swe list
   ```

6. **Stop the environment**
   ```bash
   swe-swe down --path /path/to/your/project
   ```

### Requirements

- Docker & Docker Compose installed
- Terminal access (works on macOS, Linux, Windows with WSL)

## Commands

### `swe-swe init --path PATH`

Initializes a new swe-swe project at the specified path. Creates metadata directory at `$HOME/.swe-swe/projects/{sanitized-path}/` with:

- **Dockerfile**: Container image definition with Node.js, Go, and optional tools
- **docker-compose.yml**: Multi-container orchestration
- **traefik-dynamic.yml**: Routing configuration
- **bin/swe-swe-server**: The AI terminal server (copied for Docker)
- **home/**: Persistent storage for VSCode settings and shell history
- **certs/**: Enterprise certificates (if detected)
- **.path**: Records original project path (used for project discovery)

**Options**:
- `--path PATH`: Project directory (defaults to current directory)
- `--agents AGENTS`: Comma-separated list of agents to include (default: all)
- `--exclude AGENTS`: Comma-separated list of agents to exclude
- `--apt-get-install PACKAGES`: Additional apt packages to install
- `--list-agents`: List available agents and exit
- `--update-binary-only`: Update only the binary, skip template files

**Available Agents**:
| Agent | Description | Dependencies |
|-------|-------------|--------------|
| `claude` | Claude Code CLI | Node.js |
| `gemini` | Gemini CLI | Node.js |
| `codex` | Codex CLI | Node.js |
| `aider` | Aider | Python |
| `goose` | Goose | None (standalone binary) |

**Examples**:
```bash
# Initialize with all agents (default)
swe-swe init --path ~/my-project

# Initialize with Claude only (minimal, fastest build)
swe-swe init --path ~/my-project --agents=claude

# Initialize with Claude and Gemini
swe-swe init --path ~/my-project --agents=claude,gemini

# Initialize without Python-based agents (no aider)
swe-swe init --path ~/my-project --exclude=aider

# Initialize with additional system packages
swe-swe init --path ~/my-project --apt-get-install="vim htop tmux"

# List available agents
swe-swe init --list-agents
```

**Dependency Optimization**: The Dockerfile is automatically optimized based on selected agents:
- Python/pip is only installed if `aider` is included
- Node.js/npm is only installed if `claude`, `gemini`, or `codex` is included
- This can significantly reduce image size and build time

Services are accessible via path-based routing on `localhost` (or `0.0.0.0`) at the configured port.

**Note on Port**: The port defaults to `9899` and can be customized via environment variables for `swe-swe up` (see below). Services are routed based on request paths (e.g., `/vscode`, `/chrome`) rather than subdomains, making it compatible with ngrok, cloudflared, and other tunnel services.

**Enterprise Certificate Support**: If you're behind a corporate firewall or VPN, the init command automatically detects and copies certificates from:
- `NODE_EXTRA_CA_CERTS`
- `SSL_CERT_FILE`
- `NODE_EXTRA_CA_CERTS_BUNDLE`

These are copied to `.swe-swe/certs/` and mounted into all containers.

### `swe-swe up --path PATH`

Starts the swe-swe environment using `docker-compose up`. The environment includes:

1. **swe-swe-server**: WebSocket-based AI terminal with session management
2. **chrome**: Headless Chromium with VNC for browser automation (used by MCP Playwright)
3. **code-server**: VS Code running in a container
4. **traefik**: HTTP reverse proxy with routing rules

The workspace is mounted at `/workspace` inside containers, allowing bidirectional file access.

Example:
```bash
swe-swe up --path ~/my-project
# Press Ctrl+C to stop
```

**Environment Variables**:
- `ANTHROPIC_API_KEY`: Claude API key
- `OPENAI_API_KEY`: OpenAI API key
- `GEMINI_API_KEY`: Google Gemini API key
- `SWE_SWE_PASSWORD`: VSCode password (defaults to `changeme`)
- `SWE_PORT`: External port (defaults to 9899, use environment variable to customize)
- `SWE_DASHBOARD_PORT`: Traefik dashboard port (defaults to 9900)
- `NODE_EXTRA_CA_CERTS`: Enterprise certificate path
- `SSL_CERT_FILE`: Certificate file for HTTPS tools
- `BROWSER_WS_ENDPOINT`: WebSocket endpoint for browser automation (auto-configured to `ws://chrome:9223`)

### `swe-swe down --path PATH`

Stops and removes the running Docker containers for the project.

Example:
```bash
swe-swe down --path ~/my-project
```

### `swe-swe build --path PATH`

Rebuilds the Docker image from scratch (clears cache). Useful when:
- Updating the base image
- Installing new dependencies in Dockerfile
- Testing fresh builds

Example:
```bash
swe-swe build --path ~/my-project
```

### `swe-swe update --path PATH`

Updates the swe-swe-server binary in an existing project to the latest version. Compares the current binary version with the CLI binary and only updates if a newer version is available.

Useful when:
- A new version of swe-swe is released with bug fixes or features
- You want to update without re-initializing the entire project
- You want to preserve custom configuration files

Example:
```bash
swe-swe update --path ~/my-project
# Output: swe-swe-server is already up to date (version dev)
# OR: Updating swe-swe-server from X.Y.Z to A.B.C
# OR: Successfully updated swe-swe-server
```

### `swe-swe init --path PATH --update-binary-only`

Re-initialize a project by updating only the binary, skipping template files. This preserves any custom modifications to:
- `docker-compose.yml`
- `Dockerfile`
- `traefik-dynamic.yml`
- `entrypoint.sh`

Use this flag when you've customized the configuration and want to get the latest binary without overwriting your changes.

Example:
```bash
# First time setup
swe-swe init --path ~/my-project

# Later: update binary while preserving custom docker-compose.yml
swe-swe init --path ~/my-project --update-binary-only
```

### `swe-swe list`

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

### `swe-swe help`

Displays the help message with all available commands.

## Architecture

### Directory Structure

Project metadata is stored in `$HOME/.swe-swe/projects/{sanitized-path}/`:

```
$HOME/.swe-swe/projects/{sanitized-path}/
├── Dockerfile              # Container image definition
├── docker-compose.yml      # Service orchestration
├── traefik-dynamic.yml     # HTTP routing rules
├── .path                   # Original project path (for discovery)
├── bin/
│   └── swe-swe-server     # Linux binary for Docker
├── home/                   # Persistent VSCode/shell home (volume)
└── certs/                  # Enterprise certificates (if detected)
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
  - Session management with configurable TTL
  - Multiple AI assistant detection (claude, gemini, codex, goose, aider)
  - Automatic process restart on failure
  - File upload via drag-and-drop (saved to `.swe-swe/uploads/`)
  - In-session chat for collaboration

#### chrome
- **Ports**: 9223 (CDP), 6080 (VNC/noVNC)
- **Purpose**: Headless Chromium browser for AI-driven browser automation
- **Features**:
  - Chrome DevTools Protocol (CDP) access via nginx proxy
  - VNC server for visual observation at `/chrome` path (e.g., http://0.0.0.0:9899/chrome)
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
- **Ports**: 7000 (web, external port 9899), 8080 (dashboard, external port 9900)
- **Purpose**: Reverse proxy and routing with path-based request matching
- **Routing Rules**:
  - `/vscode` path: Routes to code-server with path prefix stripped (priority 100)
  - `/chrome` path: Routes to chrome service with path prefix stripped (priority 100)
  - `/` path: Routes to swe-swe-server (priority 10, catch-all)

### Network

All containers communicate via the `swe-network` bridge. This allows:
- Service discovery by container name
- Isolated environment per project
- No port conflicts when running multiple projects

## Configuration

### Customizing the Dockerfile

The Dockerfile is located in `$HOME/.swe-swe/projects/{sanitized-path}/Dockerfile`. Edit it to:
- Add Python: `apt-get install -y python3 python3-pip`
- Add Rust: `curl --proto '=https' --tlsv1.2 -sSf https://sh.rustup.rs | sh -s -- -y`
- Add other tools via `apt-get`

### API Keys

Set API keys as environment variables before running:

```bash
export ANTHROPIC_API_KEY=sk-ant-...
export OPENAI_API_KEY=sk-...
swe-swe up --path ~/my-project
```

Or create a `.env` file in `$HOME/.swe-swe/projects/{sanitized-path}/`:
```
ANTHROPIC_API_KEY=sk-ant-...
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
swe-swe up --path ~/my-project

# Custom password
SWE_SWE_PASSWORD='my-secure-password' swe-swe up --path ~/my-project
```

## Development

### Building from Source

```bash
# Build all binaries (CLI + server)
make build

# Run tests
make test

# Format code
make fmt

# Build just the CLI
make build-cli

# Build just the server
make build-server
```

### Cross-Platform Binaries

The build system creates binaries for multiple architectures:
- `swe-swe-server.linux-amd64`: Linux x86-64
- `swe-swe-server.linux-arm64`: Linux ARM64 (Apple Silicon Docker)
- `swe-swe-server.darwin-amd64`: macOS Intel
- `swe-swe-server.darwin-arm64`: macOS Apple Silicon

The `swe-swe init` command automatically selects the correct Linux binary for Docker.

### Project Structure

```
.
├── cmd/
│   ├── swe-swe/              # CLI tool
│   │   ├── main.go
│   │   └── templates/        # Embedded Docker files
│   └── swe-swe-server/       # WebSocket server
│       ├── main.go
│       └── static/           # Web UI assets
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

**Solution**: Run `swe-swe init` again to copy the correct Linux binary for your system.

### Port Already in Use

**Error**: `port 9899 already allocated`

**Solution**: Stop other projects or use a custom port via environment variable:
```bash
# Use a different port
SWE_PORT=9900 swe-swe up --path ~/my-project
```

Alternatively, modify `$HOME/.swe-swe/projects/{sanitized-path}/docker-compose.yml` to change the port mapping.

### API Key Not Found

**Error**: `no AI assistants available`

**Solution**: The server didn't detect an installed assistant:
1. Set API keys: `ANTHROPIC_API_KEY` (Claude), `GEMINI_API_KEY` (Gemini), `OPENAI_API_KEY` (Codex)
2. Or install CLI tools: `claude`, `gemini`, `codex`, `goose`, `aider`

### Network Issues

**Error**: Service not accessible at configured port (default 9899)

**Solution**:
1. Verify Docker is running: `docker ps`
2. Check containers are healthy: `docker-compose -f $HOME/.swe-swe/projects/{sanitized-path}/docker-compose.yml ps`
3. Check Traefik logs: `docker logs <project-name>-traefik-1`
4. Verify you're using correct paths: `http://0.0.0.0:9899/`, `http://0.0.0.0:9899/vscode`, `http://0.0.0.0:9899/chrome`

### Persistent Home Issues

If VSCode settings/extensions don't persist:
1. Verify `$HOME/.swe-swe/projects/{sanitized-path}/home/` exists and has correct permissions
2. Check that the metadata directory wasn't accidentally deleted
3. Reinitialize the project: `swe-swe init --path /path/to/project`

## Advanced Usage

### Running Multiple Projects

Each project gets its own isolated environment. No conflicts:

```bash
# Terminal 1
swe-swe init --path ~/project1
swe-swe up --path ~/project1

# Terminal 2
swe-swe init --path ~/project2
swe-swe up --path ~/project2
# Use different ports if accessing locally
```

### Custom Shell

Use a custom shell with the `-shell` flag by modifying the Dockerfile:

```dockerfile
ENV SHELL=/bin/zsh
CMD ["/usr/local/bin/swe-swe-server", "-shell", "zsh", "-working-directory", "/workspace"]
```

### Session TTL

Control how long idle sessions persist (default 1 hour):

```dockerfile
CMD ["/usr/local/bin/swe-swe-server", "-session-ttl", "30m"]
```

### Basic Auth

Uncomment the auth middleware in `$HOME/.swe-swe/projects/{sanitized-path}/traefik-dynamic.yml` to enable basic authentication for both swe-swe and VSCode.

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

1. Modify the appropriate component (CLI in `cmd/swe-swe`, server in `cmd/swe-swe-server`)
2. Test locally: `make build && swe-swe init && swe-swe up`
3. Commit with conventional commits: `fix:`, `feat:`, `docs:`, etc.

## License

See LICENSE file for details.
