# Test Container Workflow

## Overview

This workflow enables testing swe-swe container builds without affecting the development container. It uses:
- **Slot-based semaphore**: Supports multiple concurrent test stacks (slots 0-2)
- **Docker label constraints**: Each Traefik only discovers its own project's containers
- **Docker-in-Docker path translation**: Automatically handles container/host path differences

## Quick Start

```bash
# Run all phases
./scripts/01-test-container-init.sh   # Acquire slot, generate project files
./scripts/02-test-container-build.sh  # Build container images
./scripts/03-test-container-run.sh    # Start containers

# MCP browser test at the assigned port (shown in output)
# Default: http://host.docker.internal:19770/

# Teardown
./scripts/04-test-container-down.sh   # Stop containers and release slot
```

## Slot Assignment

| Slot | PORT  | PROJECT_NAME | URL |
|------|-------|--------------|-----|
| 0    | 19770 | swe-test-0   | http://host.docker.internal:19770/ |
| 1    | 19771 | swe-test-1   | http://host.docker.internal:19771/ |
| 2    | 19772 | swe-test-2   | http://host.docker.internal:19772/ |

Scripts automatically acquire the first available slot. If all slots are busy, the script waits.

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

**Slot stuck**: Locks auto-expire after 1 hour, or remove manually:
```bash
rm -rf /tmp/swe-swe-test-slot-*.lock
```

**Path issues**: The init script should handle this automatically. If problems persist, check that `/workspace/` is mounted from the host.

**Container conflicts**: Run teardown script or manually:
```bash
docker compose -f /workspace/.test-home-0/.swe-swe/projects/*/docker-compose.yml down
```
