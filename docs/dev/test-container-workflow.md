# Test Container Workflow

This workflow enables testing swe-swe container builds without affecting the development container.

**Related**: See [template-editing-guide.md](template-editing-guide.md) for how to modify templates.

## Overview

It uses:
- **Slot-based semaphore**: Supports multiple concurrent test stacks (slots 0-2)
- **Docker label constraints**: Each Traefik only discovers its own project's containers
- **Docker-in-Docker path translation**: Automatically handles container/host path differences

## Quick Start

```bash
# Run all phases
./scripts/test-container/01-init.sh   # Acquire slot, generate project files
./scripts/test-container/02-build.sh  # Build container images
./scripts/test-container/03-run.sh    # Start containers

# MCP browser test at the assigned port (shown in output)
# Default: http://host.docker.internal:19770/

# Teardown
./scripts/test-container/04-down.sh   # Stop containers and release slot
```

## Slot Assignment

| Slot | PORT  | PROJECT_NAME | URL |
|------|-------|--------------|-----|
| 0    | 19770 | swe-test-0   | http://host.docker.internal:19770/ |
| 1    | 19771 | swe-test-1   | http://host.docker.internal:19771/ |
| 2    | 19772 | swe-test-2   | http://host.docker.internal:19772/ |

Scripts automatically acquire the first available slot. If all slots are busy, the script waits.

**Preview ports:** Test containers use `--preview-ports=3100-3119` (external ports 53100-53119) to avoid conflicts with the production stack's default 53000-53019 range.

## Docker-in-Docker Path Translation

The init script automatically handles path translation between container and host:
- Detects host workspace path by inspecting the dev container's mounts
- Stores project files in `/workspace/.test-home-{slot}/` (shared volume)
- Generates `docker-compose.override.yml` with translated host paths for volume mounts

This is transparent - just run the scripts and it works.

## Multi-Project Isolation

Each test stack is isolated via Docker label constraints:
- Traefik uses `--providers.docker.constraints=Label('swe.project','${PROJECT_NAME}')`
- All services have `swe.project=${PROJECT_NAME}` label
- Router/middleware names are prefixed with `${PROJECT_NAME}`

This prevents conflicts when multiple swe-swe instances run simultaneously.

## Prerequisites

- `jq` must be installed in the dev container (for path detection)
- Docker socket must be mounted

## Troubleshooting

### Common Issues

**Slot stuck**: Locks auto-expire after 1 hour, or remove manually:
```bash
rm -rf /tmp/swe-swe-test-slot-*.lock
```

**Path issues**: The init script should handle this automatically. If problems persist, check that `/workspace/` is mounted from the host.

**Container conflicts**: Run teardown script or manually:
```bash
docker compose -f /workspace/.test-home-0/.swe-swe/projects/*/docker-compose.yml down
```

**Claude auth blocked**: If Claude sessions fail due to authentication issues, use OpenCode instead. OpenCode uses `ANTHROPIC_API_KEY` from the environment and doesn't require interactive login. When testing via MCP browser, create sessions under the OpenCode agent dropdown.

### Login / Cookie Issues

**Can't login (cookie not being sent):**
- Test container uses HTTP (not HTTPS) - cookies are set without `Secure` flag
- Clear any existing cookies for the domain before testing
- Access via `http://` URL only (not `https://`)

**Password**: Default is `changeme` (set via `SWE_SWE_PASSWORD` in `.env`). To override:
```bash
SWE_SWE_INIT_FLAGS="--password=mypassword" ./scripts/test-container/01-init.sh
```

**iOS Safari / Direct IP access**: Safari may block cookies on direct IP addresses. Use `host.docker.internal` hostname or a proper domain.

### Template vs Generated Files

**IMPORTANT**: Never copy raw template files directly. Templates contain `{{variables}}` that must be processed by `swe-swe init`.

- **Template files** (what to edit): `/workspace/cmd/swe-swe/templates/host/`
- **Generated files** (what gets run): `~/.swe-swe/projects/<name>/`

Workflow when modifying templates:
```bash
# 1. Edit template file
vim cmd/swe-swe/templates/host/docker-compose.yml

# 2. Rebuild binary (processes templates)
make build

# 3. Regenerate test project
./scripts/test-container/01-init.sh

# 4. Rebuild containers
./scripts/test-container/02-build.sh

# 5. Run
./scripts/test-container/03-run.sh
```

If you copy a template file directly (without `make build` + `swe-swe init`), you'll see broken `{{IF SSL}}`, `{{UID}}`, etc. in the output files.
