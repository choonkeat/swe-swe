# swe-swe

A containerized development environment for AI-assisted coding with integrated VSCode, Traefik reverse proxy, and support for multiple AI assistants (Claude, Gemini, OpenAI).

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
   - **swe-swe terminal**: http://swe-swe.lvh.me:9899
   - **VSCode**: http://vscode.lvh.me:9899
   - **Traefik dashboard**: http://traefik.lvh.me:9899 (dashboard on port 9900)

5. **Stop the environment**
   ```bash
   swe-swe down --path /path/to/your/project
   ```

### Requirements

- Docker & Docker Compose installed
- Terminal access (works on macOS, Linux, Windows with WSL)

## Commands

### `swe-swe init --path PATH`

Initializes a new swe-swe project at the specified path. Creates the `.swe-swe/` directory structure with:

- **Dockerfile**: Container image definition with Node.js, Go, and optional tools
- **docker-compose.yml**: Multi-container orchestration
- **traefik-dynamic.yml**: Routing configuration
- **bin/swe-swe-server**: The AI terminal server (copied for Docker)
- **home/**: Persistent storage for VSCode settings and shell history
- **certs/**: Enterprise certificates (if detected)

**Options**:
- `--path PATH`: Project directory (defaults to current directory)

Example:
```bash
swe-swe init --path ~/my-project
```

Services are accessible using the `lvh.me` wildcard domain (resolves any subdomain to localhost).

**Note on Domain & Port**: The domain is fixed to `lvh.me` and port defaults to `9899`. Both can be customized via environment variables for `swe-swe up` (see below). The domain cannot be changed via CLI flag; modify `docker-compose.yml` Traefik routing rules for custom domains.

**Enterprise Certificate Support**: If you're behind a corporate firewall or VPN, the init command automatically detects and copies certificates from:
- `NODE_EXTRA_CA_CERTS`
- `SSL_CERT_FILE`
- `NODE_EXTRA_CA_CERTS_BUNDLE`

These are copied to `.swe-swe/certs/` and mounted into all containers.

### `swe-swe up --path PATH`

Starts the swe-swe environment using `docker-compose up`. The environment includes:

1. **swe-swe-server**: WebSocket-based AI terminal with session management
2. **code-server**: VS Code running in a container
3. **traefik**: HTTP reverse proxy with routing rules

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

### `swe-swe help`

Displays the help message with all available commands.

## Architecture

### Directory Structure

```
.swe-swe/
├── Dockerfile              # Container image definition
├── docker-compose.yml      # Service orchestration
├── traefik-dynamic.yml     # HTTP routing rules
├── bin/
│   └── swe-swe-server     # Linux binary for Docker
├── home/                   # Persistent VSCode/shell home (volume)
└── certs/                  # Enterprise certificates (if detected)
```

### Services

#### swe-swe-server
- **Port**: 9898 (inside container)
- **Purpose**: WebSocket-based AI coding assistant terminal
- **Features**:
  - Real-time terminal with PTY support
  - Session management with configurable TTL
  - Multiple AI assistant detection (claude, gemini, openai, etc.)
  - Automatic process restart on failure

#### code-server
- **Port**: 8080 (inside container)
- **Purpose**: Full VS Code IDE in the browser
- **Features**:
  - Syntax highlighting, extensions support
  - Direct file editing in `/workspace`
  - Terminal integration

#### traefik
- **Ports**: 7000 (web, external port 9899), 8080 (dashboard, external port 9900)
- **Purpose**: Reverse proxy and routing
- **Routing Rules** (subdomain-based):
  - `swe-swe.lvh.me`: Routes to swe-swe-server
  - `vscode.lvh.me`: Routes to code-server
  - `traefik.lvh.me`: Dashboard
  - All other subdomains: Routes to swe-swe-server

### Network

All containers communicate via the `swe-network` bridge. This allows:
- Service discovery by container name
- Isolated environment per project
- No port conflicts when running multiple projects

## Configuration

### Customizing the Dockerfile

Edit `.swe-swe/Dockerfile` to:
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

Or create a `.env` file in `.swe-swe/`:
```
ANTHROPIC_API_KEY=sk-ant-...
OPENAI_API_KEY=sk-...
```

### Resource Limits

Edit `.swe-swe/docker-compose.yml` to adjust resource constraints:

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

Alternatively, modify `.swe-swe/docker-compose.yml` to change the port mapping.

### API Key Not Found

**Error**: `no AI assistants available`

**Solution**: The server didn't detect an installed assistant:
1. Set `ANTHROPIC_API_KEY`, `OPENAI_API_KEY`, or similar
2. Or install CLI tools: `claude`, `gemini`, `codex`, `goose`, `aider`

### Network Issues

**Error**: Service not accessible at `*.lvh.me`

**Solution**:
1. Verify Docker is running: `docker ps`
2. Check containers are healthy: `docker-compose -f .swe-swe/docker-compose.yml ps`
3. Check Traefik logs: `docker logs traefik`

### Persistent Home Issues

If VSCode settings/extensions don't persist:
1. Verify `.swe-swe/home/` exists and has correct permissions
2. Reinitialize: `rm -rf .swe-swe && swe-swe init`

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

Uncomment the auth middleware in `.swe-swe/traefik-dynamic.yml` to enable basic authentication for both swe-swe and VSCode.

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
