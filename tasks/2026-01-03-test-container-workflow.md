# Test Container Workflow

## Goal

Enable testing new swe-swe-server container builds without killing the current development container.

## Context

- We're developing swe-swe inside a swe-swe container
- Host `/workspace/swe-swe` = Container `/workspace`
- Docker socket is mounted, so we can build/run containers from inside
- Current stack runs on port 1977; test container uses HOST_PORT env var

## Phases

### Phase 1: Generate test project files [DONE]

**Script:** `/workspace/scripts/01-test-container-init.sh`

**What it does:**
- Cleans up any previous `/workspace/tmp/` artifacts
- Creates `/workspace/tmp/home` and `/workspace/tmp/project-directory`
- Runs `swe-swe init` with `HOME=/workspace/tmp/home`
- Prints the generated project path

**Verification:**
- Script is idempotent (can run multiple times)
- Generated directory contains: `Dockerfile`, `docker-compose.yml`, `swe-swe-server/`, `entrypoint.sh`

---

### Phase 2: Build the test image [DONE]

**Script:** `/workspace/scripts/02-test-container-build.sh`

**What it does:**
- Finds the generated project directory
- Runs `docker compose build swe-swe`
- Tags the image as `swe-swe-test:latest`
- Runs smoke test (`swe-swe-server --help`)

**Verification:**
- Script exits non-zero if any step fails
- Image exists and smoke test passes

---

### Phase 3: Run test container [DONE]

**Script:** `/workspace/scripts/03-test-container-run.sh`

**What it does:**
- Stops/removes any existing `swe-swe-test` container (idempotent)
- Runs `swe-swe-test:latest` with:
  - `-p $HOST_PORT:9898`
  - `-v /workspace/swe-swe:/workspace`
  - `--name swe-swe-test`
  - `-d`
- Waits briefly for startup
- Prints container logs

**Verification:**
- Container is running: `docker ps | grep swe-swe-test`
- Server responds: `HOST_IP=<your-ip> ./scripts/03-test-container-run.sh` (optional, localhost doesn't work from inside container)

---

### Phase 4: Stop test container [DONE]

**Script:** `/workspace/scripts/04-test-container-down.sh`

**What it does:**
- Stops and removes the `swe-swe-test` container

---

### Phase 5: Clean up test artifacts [DONE]

**Script:** `/workspace/scripts/05-test-container-clean.sh`

**What it does:**
- Removes `swe-swe-test` container (if exists)
- Removes `swe-swe-test:latest` image
- Removes `/workspace/tmp/` directory

---

## Script Standards

All scripts will:
- Start with `#!/bin/bash` and `set -euox pipefail`
- Be idempotent where possible
- Exit non-zero on failure

## Usage

```bash
# Full workflow
./scripts/01-test-container-init.sh
./scripts/02-test-container-build.sh
HOST_PORT=<port> ./scripts/03-test-container-run.sh

# Test at http://localhost:<port>/

# Teardown
./scripts/04-test-container-down.sh
./scripts/05-test-container-clean.sh
```
