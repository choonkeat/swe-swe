# CLI Commands and Binary Management

This document describes the swe-swe CLI commands and how the swe-swe-server binary is managed across the project lifecycle.

## Quick Summary

- `swe-swe init [--agents=...] [--exclude=...] [--apt-get-install=...]` — Initialize a new swe-swe project with customizable agent selection
- `swe-swe up [services...]` — Start the environment (or specific services) AND **always update the server binary**
- `swe-swe down [services...]` — Stop the environment (or specific services)
- `swe-swe build [services...]` — Force a fresh Docker image rebuild (no cache)
- `swe-swe update` — Update the server binary without starting containers
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
├── bin/
│   └── swe-swe-server          # Server binary (volume-mounted into container)
├── home/                        # Persistent home directory for apps
├── certs/                       # Enterprise certificates (if configured)
├── Dockerfile                   # Docker image definition
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
1. Creates metadata directory structure in `$HOME/.swe-swe/projects/{sanitized-path}/` (bin/, home/, certs/)
2. Writes `.path` file with original project path
3. Processes Dockerfile template based on selected agents (conditional sections)
4. Extracts Docker templates (Dockerfile, docker-compose.yml, entrypoint.sh, traefik-dynamic.yml)
5. Extracts swe-swe-server binary from embedded assets to metadata directory
6. Handles enterprise certificates if `NODE_EXTRA_CA_CERTS`, `SSL_CERT_FILE`, or `NODE_EXTRA_CA_CERTS_BUNDLE` environment variables are set

**Options:**
| Flag | Description |
|------|-------------|
| `--path PATH` | Project directory (defaults to current directory) |
| `--agents AGENTS` | Comma-separated list of agents to include (default: all) |
| `--exclude AGENTS` | Comma-separated list of agents to exclude |
| `--apt-get-install PACKAGES` | Additional apt packages to install (comma or space separated) |
| `--list-agents` | List available agents and exit |
| `--update-binary-only` | Update only the binary, skip template files |

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
swe-swe init --exclude=aider

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
2. **Always extracts the current swe-swe-server binary** from the CLI's embedded assets to metadata directory
3. Validates Docker and docker-compose are available
4. Runs `docker-compose up` to start/resume containers (or specific services if specified)
5. On Unix/Linux/macOS: uses `syscall.Exec` to replace the current process with docker-compose (signals go directly to docker-compose)
6. On Windows: runs docker-compose as subprocess with signal forwarding

**Critical behavior:** The binary is extracted **every time** `swe-swe up` runs, regardless of whether containers already exist.

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
3. Does NOT update the swe-swe-server binary (binary is volume-mounted, not baked into image)

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

### `swe-swe update [--path PATH]`

**Purpose:** Update the swe-swe-server binary without starting containers.

**What it does:**
1. Validates metadata directory exists in `$HOME/.swe-swe/projects/{sanitized-path}/`
2. Extracts swe-swe-server binary from the CLI's embedded assets (matches CLI architecture: linux-amd64 or linux-arm64)
3. Compares versions using `--version` flag on both binaries
4. If versions differ: copies new binary to metadata directory and makes it executable
5. If already up-to-date: no action needed

**When to use:**
- Updating an existing project when the CLI binary is updated
- Preparing for a container restart without starting it immediately
- Checking if an update is needed

**Example:**
```bash
swe-swe update                       # Updates binary if needed
# Later, restart containers with 'swe-swe up' to use new binary
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
- Server binary (bin/swe-swe-server)
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

## Binary Management Architecture

### Key Design: Volume-Mounted Binary

The swe-swe-server binary is **not baked into the Docker image**. Instead, it's **volume-mounted from the host**:

**In docker-compose.yml:**
```yaml
volumes:
  - ./bin/swe-swe-server:/usr/local/bin/swe-swe-server:ro
```

This means:
- The binary lives on the host at `$HOME/.swe-swe/projects/{sanitized-path}/bin/swe-swe-server`
- The container sees it at `/usr/local/bin/swe-swe-server` (read-only)
- Any update to the binary on the host is immediately available to the container on restart

### Why This Design?

**Benefits:**
1. **Fast updates without image rebuilds** — Updating the binary doesn't require rebuilding the Docker image
2. **Consistent binary across environments** — Uses the same binary (from the CLI) regardless of Docker image state
3. **Minimal Docker layer complexity** — Keeps the Dockerfile simple
4. **Quick deployments** — New `swe-swe up` just extracts binary and starts containers

**Trade-offs:**
- Binary isn't cached inside the image (must always be present on host)
- Requires host filesystem to persist the binary
- Not suitable if you need the binary frozen at image-build time

### Architecture Diagram

```
swe-swe CLI (binary)
    │
    ├─ Embedded assets: swe-swe-server.linux-amd64, swe-swe-server.linux-arm64
    │
    └─ `swe-swe up` / `swe-swe init` / `swe-swe update`
         │
         ├─ Extract binary from assets
         ├─ Write to .swe-swe/bin/swe-swe-server
         └─ Volume mount into container
              │
              └─ Container sees at /usr/local/bin/swe-swe-server
```

### Host Architecture Detection

The CLI detects the host's CPU architecture (arm64 or amd64) and extracts the matching Linux binary:

**In main.go:**
```go
if runtime.GOARCH == "arm64" {
    embeddedPath = "bin/swe-swe-server.linux-arm64"
} else {
    embeddedPath = "bin/swe-swe-server.linux-amd64"
}
```

If the matching architecture binary isn't available, it falls back to amd64.

## Command Comparison: `up` vs `build`

| Aspect | `swe-swe up` | `swe-swe build` |
|--------|--------------|-----------------|
| **Binary update** | Always updates | No binary update |
| **Docker rebuild** | Only if image missing | Always rebuilds with `--no-cache` |
| **Container restart** | Starts/resumes containers | Builds only, doesn't start |
| **Service targeting** | Supports `[services...]` | Supports `[services...]` |
| **When to use** | Regular startup | Dockerfile changes |
| **Speed** | Fast (reuses image layers) | Slow (rebuilds everything) |

## Binary Update Behavior Timeline

### Initial Setup: `swe-swe init`
```
1. Create .swe-swe/ directories
2. Extract templates (Dockerfile, docker-compose.yml)
3. Extract swe-swe-server binary → .swe-swe/bin/
```

### First Run: `swe-swe up`
```
1. Extract swe-swe-server binary → .swe-swe/bin/ (overwrites)
2. Run docker-compose up
3. Docker builds image if it doesn't exist
4. Container starts, uses binary from volume mount
```

### Containers Already Running
```
swe-swe up
  └─ Extract swe-swe-server binary → .swe-swe/bin/ (overwrites)
  └─ Run docker-compose up
  └─ Containers restart
  └─ They see the updated binary via volume mount
```

### No Container Restart Needed
```
swe-swe update
  └─ Extract swe-swe-server binary → .swe-swe/bin/ (overwrites)
  └─ Check version
  └─ Done (no container restart)
  └─ Run 'swe-swe up' later to use the new binary
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

### Q: I updated the CLI but the container is using an old binary
**A:** Run `swe-swe up`. This always extracts the latest binary from the CLI.

### Q: I modified the Dockerfile but `swe-swe up` didn't rebuild it
**A:** Use `swe-swe build` to force a fresh rebuild, then `swe-swe up`.

### Q: I want to rebuild only the chrome container
**A:** Use `swe-swe build chrome` then `swe-swe up chrome`.

### Q: The swe-swe-server binary is not executable
**A:** The CLI sets the binary to 0755 (executable) automatically. If this fails, manually run:
```bash
chmod +x $HOME/.swe-swe/projects/{sanitized-path}/bin/swe-swe-server
```

### Q: Do I need to run `swe-swe update` separately?
**A:** No, `swe-swe up` already updates the binary. `swe-swe update` is useful if you want to update without restarting containers.

### Q: What if containers don't start?
**A:** Check Docker logs:
```bash
docker-compose -f $HOME/.swe-swe/projects/{sanitized-path}/docker-compose.yml logs
```

### Q: How do I find where my project metadata is stored?
**A:** Run `swe-swe list` to see all projects and their metadata locations. Metadata is stored in `$HOME/.swe-swe/projects/` with a sanitized directory name based on your project path.

### Q: What if I specify an invalid service name?
**A:** docker-compose will return an error like "no such service: foo". Service names must match those defined in docker-compose.yml (swe-swe, vscode, chrome, traefik).

## Related Files

- `cmd/swe-swe/main.go` — CLI command implementations
- `cmd/swe-swe/templates/host/docker-compose.yml` — Docker Compose configuration
- `cmd/swe-swe/templates/host/Dockerfile` — Docker image definition
- `cmd/swe-swe/bin/` — Embedded swe-swe-server binaries (linux-amd64, linux-arm64)
