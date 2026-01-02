# CLI Commands and Build Architecture

This document describes the swe-swe CLI commands and how the swe-swe-server is built and deployed.

## Quick Summary

- `swe-swe init [--agents=...] [--exclude-agents=...] [--apt-get-install=...]` — Initialize a new swe-swe project with customizable agent selection
- `swe-swe up [services...]` — Start the environment (or specific services)
- `swe-swe down [services...]` — Stop the environment (or specific services)
- `swe-swe build [services...]` — Force a fresh Docker image rebuild (no cache)
- `swe-swe list` — List all initialized projects (auto-prunes stale ones)

## Service Targeting

Commands `up`, `down`, and `build` support targeting specific services:

```bash
# Target specific services
swe-swe up chrome                    # Start only chrome (and dependencies)
swe-swe down chrome vscode           # Stop chrome and vscode
swe-swe build chrome                 # Rebuild only chrome image

# No service specified = all services (default behavior)
swe-swe up                           # Start all services
swe-swe down                         # Stop all services
```

### Available Services

Services are defined in docker-compose.yml:

| Service | Description |
|---------|-------------|
| `swe-swe` | Claude Code container (runs swe-swe-server) |
| `vscode` | VSCode/code-server IDE |
| `chrome` | Chrome browser with VNC |
| `traefik` | Reverse proxy (routing) |

### Pass-through Arguments

Use `--` to pass additional arguments directly to docker-compose:

```bash
swe-swe down -- --remove-orphans     # Remove orphaned containers
swe-swe up -- -d                     # Run in detached mode
swe-swe up chrome -- --build         # Build before starting
```

## Project Directory Structure

After `swe-swe init`, metadata is stored in `$HOME/.swe-swe/projects/{sanitized-path}/`:

```
$HOME/.swe-swe/projects/{sanitized-path}/  # All swe-swe metadata and config
├── swe-swe-server/              # Server source code (built at docker-compose time)
│   ├── go.mod, go.sum
│   ├── main.go
│   └── static/
├── auth/                        # ForwardAuth service source code
│   ├── go.mod, go.sum
│   └── main.go
├── home/                        # Persistent home directory for apps
├── certs/                       # Enterprise certificates (if configured)
├── Dockerfile                   # Docker image definition (multi-stage build)
├── docker-compose.yml           # Compose configuration
├── entrypoint.sh                # Container startup script
├── traefik-dynamic.yml          # Traefik routing rules
├── .path                        # Original project path (for discovery)
└── .env                         # Environment variables (if using enterprise certs)
```

**Note**: Metadata is stored outside your project directory for security. Your project directory remains clean (no `.swe-swe/` folder).

## Command Reference

### `swe-swe init [options]`

**Purpose:** Initialize a new swe-swe project at PATH (defaults to current directory).

**What it does:**
1. Creates metadata directory structure in `$HOME/.swe-swe/projects/{sanitized-path}/` (swe-swe-server/, auth/, home/, certs/)
2. Writes `.path` file with original project path
3. Processes Dockerfile template based on selected agents (conditional sections)
4. Extracts Docker templates (Dockerfile, docker-compose.yml, entrypoint.sh, traefik-dynamic.yml)
5. Extracts swe-swe-server and auth service source code from embedded assets
6. Handles enterprise certificates if `NODE_EXTRA_CA_CERTS`, `SSL_CERT_FILE`, or `NODE_EXTRA_CA_CERTS_BUNDLE` environment variables are set

**Options:**
| Flag | Description |
|------|-------------|
| `--path PATH` | Project directory (defaults to current directory) |
| `--agents AGENTS` | Comma-separated list of agents to include (default: all) |
| `--exclude-agents AGENTS` | Comma-separated list of agents to exclude |
| `--apt-get-install PACKAGES` | Additional apt packages to install (comma or space separated) |
| `--list-agents` | List available agents and exit |

**Available Agents:**
| Agent | Description | Dependencies |
|-------|-------------|--------------|
| `claude` | Claude Code CLI | Node.js |
| `gemini` | Gemini CLI | Node.js |
| `codex` | Codex CLI | Node.js |
| `aider` | Aider | Python |
| `goose` | Goose | None (standalone binary) |

**Examples:**
```bash
# Initialize with all agents (default)
swe-swe init --path ~/my-project

# Initialize with Claude only (minimal, fastest build)
swe-swe init --agents=claude

# Initialize with Claude and Gemini
swe-swe init --agents=claude,gemini

# Initialize without Python-based agents (smaller image)
swe-swe init --exclude-agents=aider

# Initialize with additional system packages
swe-swe init --apt-get-install="vim htop tmux"

# Combine options
swe-swe init --agents=claude,codex --apt-get-install="vim" --path ~/my-project

# List available agents
swe-swe init --list-agents
```

**Dockerfile Optimization:**
The Dockerfile template uses conditional sections to minimize image size:
- Python/pip is only installed when `aider` is selected
- Node.js/npm is only installed when `claude`, `gemini`, or `codex` is selected
- Custom apt packages are only added when `--apt-get-install` is specified

This optimization can significantly reduce Docker build time and final image size when using only a subset of agents.

### `swe-swe up [--path PATH] [services...] [-- docker-compose-args...]`

**Purpose:** Start the swe-swe environment (or specific services) and ensure containers are running.

**What it does:**
1. Validates that metadata directory exists in `$HOME/.swe-swe/projects/{sanitized-path}/` (fails if project not initialized)
2. Validates Docker and docker-compose are available
3. Runs `docker-compose up` to start/resume containers (or specific services if specified)
4. On Unix/Linux/macOS: uses `syscall.Exec` to replace the current process with docker-compose (signals go directly to docker-compose)
5. On Windows: runs docker-compose as subprocess with signal forwarding

**Examples:**
```bash
cd ~/my-project
swe-swe up                           # Start all services
swe-swe up chrome                    # Start only chrome (and dependencies)
swe-swe up chrome vscode             # Start chrome and vscode
swe-swe up -- -d                     # Start detached (background)

# Ctrl+C to stop (signals sent directly to docker-compose)
```

### `swe-swe down [--path PATH] [services...] [-- docker-compose-args...]`

**Purpose:** Stop the swe-swe environment (or specific services) without removing data.

**What it does:**
1. Validates metadata directory exists in `$HOME/.swe-swe/projects/{sanitized-path}/`
2. If services specified: runs `docker-compose stop` + `docker-compose rm -f` for those services
3. If no services specified: runs `docker-compose down` which stops and removes all containers
4. Preserves volumes (home directory, workspace persist)
5. Preserves the swe-swe-server binary and metadata

**Examples:**
```bash
swe-swe down                         # Stop all containers
swe-swe down chrome                  # Stop only chrome
swe-swe down chrome vscode           # Stop chrome and vscode
swe-swe down -- --remove-orphans     # Remove orphaned containers

# Running again later with 'swe-swe up' restarts with the same environment
```

### `swe-swe build [--path PATH] [services...] [-- docker-compose-args...]`

**Purpose:** Force a fresh Docker image rebuild with no cache.

**What it does:**
1. Validates metadata directory exists in `$HOME/.swe-swe/projects/{sanitized-path}/`
2. Runs `docker-compose build --no-cache` to rebuild images from scratch (or specific services if specified)
3. Recompiles swe-swe-server and auth service from source (they are built at docker-compose time)

**When to use:**
- When Dockerfile changes
- When base images need to be updated
- When troubleshooting Docker layer issues
- When dependencies in the Docker image need updating

**Examples:**
```bash
swe-swe build                        # Rebuild all images
swe-swe build chrome                 # Rebuild only chrome image
swe-swe build chrome swe-swe         # Rebuild chrome and swe-swe images
swe-swe up                           # Start with fresh images
```

### `swe-swe list`

**Purpose:** List all initialized swe-swe projects and automatically prune stale metadata.

**What it does:**
1. Scans `$HOME/.swe-swe/projects/` directory for metadata directories
2. For each metadata directory, reads `.path` file to recover original project path
3. Checks if original project path still exists on disk
4. **Auto-prunes**: Removes metadata directories for missing project paths
5. Displays remaining active projects with count
6. Shows summary of pruned projects

**Auto-Prune Behavior:**
- **Trigger**: Runs automatically every time `swe-swe list` is executed
- **Stale Detection**: Checks if original project path (stored in `.path` file) still exists
- **Action**: Uses `os.RemoveAll()` to delete the entire metadata directory for missing projects
- **Safety**: Only removes metadata, not the project itself (which is already gone)
- **Transparency**: Warns about stale directories that can't be removed and shows count of successful removals

**When to use:**
- Discover what projects you have initialized
- Clean up metadata for deleted/moved projects
- Verify project paths before any cleanup operations
- Regular maintenance to keep `$HOME/.swe-swe/projects/` clean

**Example:**
```bash
# List all projects and auto-prune stale ones
swe-swe list
# Output:
# Initialized projects (2):
#   /Users/alice/projects/myapp
#   /Users/alice/projects/anotherapp
#
# Removed 1 stale project(s)
```

**What happens if project path no longer exists:**
```bash
# Delete a project directory
rm -rf /Users/alice/projects/oldproject

# Run list - it will detect the stale metadata
swe-swe list
# Output:
# Initialized projects (1):
#   /Users/alice/projects/myapp
#
# Removed 1 stale project(s)
```

**Metadata Directory Structure (what gets pruned):**
When a project is stale and pruned, the entire directory at `$HOME/.swe-swe/projects/{sanitized-path}/` is removed, including:
- Docker templates (Dockerfile, docker-compose.yml, traefik-dynamic.yml)
- Server source code (swe-swe-server/)
- Auth service source code (auth/)
- Persistent home directory (home/)
- Enterprise certificates (certs/)
- Path marker file (.path)
- Environment variables (.env, if present)

**Warning**: If you have running containers from a stale project, they will continue running. Only the metadata directory is removed. Containers must be stopped first:
```bash
# If containers are still running from a deleted project
docker-compose -f $HOME/.swe-swe/projects/{sanitized-path}/docker-compose.yml down
# Then run swe-swe list to prune the metadata
swe-swe list
```

---

## Build Architecture

### Key Design: Source-Built at Docker-Compose Time

The swe-swe-server is **built from source** when the Docker image is built using a multi-stage Dockerfile:

**In Dockerfile:**
```dockerfile
# Stage 1: Build swe-swe-server from source
FROM golang:1.21-alpine AS server-builder
WORKDIR /build
COPY swe-swe-server/go.mod swe-swe-server/go.sum ./
RUN go mod download
COPY swe-swe-server/*.go ./
COPY swe-swe-server/static/ ./static/
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o swe-swe-server .

# Stage 2: Main development environment
FROM golang:1.23-bookworm
# ... rest of Dockerfile ...
COPY --from=server-builder /build/swe-swe-server /usr/local/bin/swe-swe-server
```

This means:
- The server source lives at `$HOME/.swe-swe/projects/{sanitized-path}/swe-swe-server/`
- The server is compiled fresh when you run `swe-swe build` or first `docker-compose up`
- The binary is baked into the Docker image, not volume-mounted
- Same approach is used for the auth service

### Why This Design?

**Benefits:**
1. **Simpler CLI** — No need to embed pre-compiled binaries for multiple architectures
2. **Smaller CLI binary** — Source code is much smaller than compiled binaries
3. **Native Docker caching** — Docker layer caching handles rebuild optimization
4. **Self-contained images** — Everything needed is in the Docker image

**Trade-offs:**
- Slightly longer initial build (Go compilation)
- Requires Go toolchain in the build stage (handled by multi-stage build)

### Architecture Diagram

```
swe-swe CLI
    │
    ├─ Embedded assets: swe-swe-server/*.go, auth/*.go (source code)
    │
    └─ `swe-swe init`
         │
         ├─ Extract source code → .swe-swe/swe-swe-server/
         ├─ Extract source code → .swe-swe/auth/
         └─ Extract templates (Dockerfile, docker-compose.yml)

docker-compose build / up
    │
    └─ Multi-stage Dockerfile
         │
         ├─ Stage 1: Build Go binaries from source
         └─ Stage 2: Copy binaries into final image
```

## Command Comparison: `up` vs `build`

| Aspect | `swe-swe up` | `swe-swe build` |
|--------|--------------|-----------------|
| **Docker rebuild** | Only if image missing | Always rebuilds with `--no-cache` |
| **Server recompile** | Only if image missing | Always recompiles |
| **Container restart** | Starts/resumes containers | Builds only, doesn't start |
| **Service targeting** | Supports `[services...]` | Supports `[services...]` |
| **When to use** | Regular startup | Dockerfile or source changes |
| **Speed** | Fast (reuses cached image) | Slow (rebuilds everything) |

## Build Behavior Timeline

### Initial Setup: `swe-swe init`
```
1. Create .swe-swe/ directories
2. Extract templates (Dockerfile, docker-compose.yml)
3. Extract swe-swe-server source → .swe-swe/swe-swe-server/
4. Extract auth source → .swe-swe/auth/
```

### First Run: `swe-swe up`
```
1. Run docker-compose up
2. Docker builds image (multi-stage):
   a. Stage 1: Compile swe-swe-server from source
   b. Stage 2: Build auth service from source
   c. Final: Copy binaries into runtime image
3. Container starts with compiled binaries
```

### After Source Code Changes
```
swe-swe build
  └─ Run docker-compose build --no-cache
  └─ Recompiles swe-swe-server and auth from source
  └─ Creates new Docker image with updated binaries

swe-swe up
  └─ Run docker-compose up
  └─ Uses newly built image
```

## Environment Variables

### VSCode Password
```bash
export SWE_SWE_PASSWORD=mypassword
swe-swe up
```

### Default: `changeme`

### Workspace Directory
```bash
export WORKSPACE_DIR=/path/to/workspace
swe-swe up
```

**Default:** Current directory (`.`)

### Enterprise Certificates
During `swe-swe init`, the following environment variables are checked:
- `NODE_EXTRA_CA_CERTS` — CA certificate file path
- `SSL_CERT_FILE` — SSL certificate file path
- `NODE_EXTRA_CA_CERTS_BUNDLE` — Bundle of CA certificates

If set, certificates are copied to `.swe-swe/certs/` and mounted in the container.

## Troubleshooting

### Q: I updated the source code but the container is using an old version
**A:** Run `swe-swe build` to recompile, then `swe-swe up` to restart with the new image.

### Q: I modified the Dockerfile but `swe-swe up` didn't rebuild it
**A:** Use `swe-swe build` to force a fresh rebuild, then `swe-swe up`.

### Q: I want to rebuild only the chrome container
**A:** Use `swe-swe build chrome` then `swe-swe up chrome`.

### Q: What if containers don't start?
**A:** Check Docker logs:
```bash
docker-compose -f $HOME/.swe-swe/projects/{sanitized-path}/docker-compose.yml logs
```

### Q: How do I find where my project metadata is stored?
**A:** Run `swe-swe list` to see all projects and their metadata locations. Metadata is stored in `$HOME/.swe-swe/projects/` with a sanitized directory name based on your project path.

### Q: What if I specify an invalid service name?
**A:** docker-compose will return an error like "no such service: foo". Service names must match those defined in docker-compose.yml (swe-swe, vscode, chrome, traefik, auth).

## Related Files

- `cmd/swe-swe/main.go` — CLI command implementations
- `cmd/swe-swe/templates/host/docker-compose.yml` — Docker Compose configuration
- `cmd/swe-swe/templates/host/Dockerfile` — Docker image definition (multi-stage build)
- `cmd/swe-swe/templates/host/swe-swe-server/` — Server source code (embedded)
- `cmd/swe-swe/templates/host/auth/` — Auth service source code (embedded)
