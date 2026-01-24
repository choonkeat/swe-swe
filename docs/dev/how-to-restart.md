# How to Restart swe-swe Stack

## Prerequisites

**IMPORTANT**: Before restarting, verify that template dependencies are in sync:

```bash
# Check that swe-swe-server template uses the same record-tui version as main go.mod
diff <(grep record-tui go.mod) <(grep record-tui cmd/swe-swe/templates/host/swe-swe-server/go.mod.txt)
```

If they differ, update the template files to match:
- `cmd/swe-swe/templates/host/swe-swe-server/go.mod.txt`
- `cmd/swe-swe/templates/host/swe-swe-server/go.sum.txt`

The swe-swe-server is built inside Docker using its own go.mod, so stale template files will cause the container to run old dependency versions even after restart.

## Restart Commands

From inside the swe-swe container (with Docker socket access), run these commands to restart the entire stack:

```bash
# 1. Re-init
bash .swe-swe/pre-restart.sh

# 2. Stop any test containers first
docker compose -p swe-swe-test down -t 10 2>/dev/null || true

# 3. Stop the chrome container (it doesn't always respond to compose down)
docker stop -t 10 home-app-workspace-swe-swe-6f7a1ba3-chrome-1

# 4. Bring down the rest of the stack (we're all going offline here, including agent)
docker compose -p home-app-workspace-swe-swe-6f7a1ba3 down -t 20
```

This will cause the host's restart loop (`.swe-swe/restart-loop.sh`) to re-init and bring everything back up with the latest changes.
