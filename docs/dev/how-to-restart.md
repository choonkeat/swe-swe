# How to Restart swe-swe Stack

## Prerequisites

**IMPORTANT**: Before restarting, verify that template dependencies are correct
- see docs/dev/template-editing-guide.md
- make sure both go.mod and cmd/swe-swe/templates/host/swe-swe-server/go.mod.txt are using the same version of libraries if used on both sides

The swe-swe-server is built inside Docker using its own go.mod, so stale template files will cause the container to run old dependency versions even after restart.

## Restart Commands

From inside the swe-swe container (with Docker socket access), run these commands to restart the entire stack:

```bash
# 1. Re-init
bash .swe-swe/pre-restart.sh

# 2. Stop any test containers first
docker compose -p swe-swe-test down -t 10 2>/dev/null || true

# 3. Stop the chrome container (it doesn't always respond to compose down)
# Replace <project-name> with your actual compose project name (visible in `docker ps`)
docker stop -t 10 <project-name>-chrome-1

# 4. Bring down the rest of the stack (we're all going offline here, including agent)
docker compose -p <project-name> down -t 20
```

This will cause the host's restart loop (`.swe-swe/restart-loop.sh`) to re-init and bring everything back up with the latest changes.
