# swe-swe

**Your agent:** containerized with its own browser for agentic testing.<br>
**Your terminal:** pair live or share recordings with teammates.<br>
**Your sessions:** run multiple in parallel, each on its own git worktree.

Works with Claude, Codex, OpenCode, Gemini, Aider, Goose. Not listed? [Let us know](https://github.com/choonkeat/swe-swe/issues)!

## Quick Start

1. **Install swe-swe**

   Option A: run via npx (requires Node.js)
   ```bash
   alias swe-swe='npx -y swe-swe'
   ```

   Option B: install via curl
   ```bash
   curl -fsSL https://raw.githubusercontent.com/choonkeat/swe-swe/main/install.sh | sh
   ```

2. **Go to your project**
   ```bash
   cd /path/to/your/project
   ```

3. **Initialize and start**
   ```bash
   swe-swe init
   swe-swe up
   ```

4. **Open** http://localhost:1977

### Requirements

- Docker & Docker Compose installed
- Terminal access (works on macOS, Linux, Windows with WSL)

## Commands

For the full command reference — all flags, examples, environment variables, and architecture details — see [docs/cli-commands-and-binary-management.md](docs/cli-commands-and-binary-management.md). For configuration options, see [docs/configuration.md](docs/configuration.md).

**Quick reference:**

```bash
# Native commands
swe-swe init [options]          # Initialize a project
swe-swe list                    # List initialized projects
swe-swe proxy <command>         # Bridge host commands into containers

# Docker Compose pass-through (all other commands)
swe-swe up                      # Start the environment
swe-swe down                    # Stop the environment
swe-swe build                   # Rebuild Docker images
swe-swe ps / logs / exec ...    # Any docker compose command
```

Use `--project-directory` to specify which project (defaults to current directory). The port defaults to `1977` and can be customized via `SWE_PORT`.

## Documentation

- [Configuration Reference](docs/configuration.md) — all init flags, environment variables, and config files
- [CLI Commands and Build Architecture](docs/cli-commands-and-binary-management.md) — full command reference, troubleshooting, build system
- [Browser Automation](docs/browser-automation.md) — Chrome CDP and MCP Playwright
- [WebSocket Protocol](docs/websocket-protocol.md) — terminal communication protocol

## Development

```bash
make build    # Build CLI binaries for all platforms
make test     # Run tests
```

See [docs/cli-commands-and-binary-management.md](docs/cli-commands-and-binary-management.md) for build architecture and troubleshooting.

## License

See LICENSE file for details.
