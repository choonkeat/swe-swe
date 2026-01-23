# How to Restart swe-swe Stack

From inside the swe-swe container (with Docker socket access), run these commands to restart the entire stack:

```bash
# 1. Re-init
bash .swe-swe/pre-restart.sh

# 2. Stop the chrome container first (it doesn't always respond to compose down)
docker stop -t 10 home-app-workspace-swe-swe-6f7a1ba3-chrome-1

# 3. Bring down the rest of the stack (we're all going offline here, including agent)
docker compose -p home-app-workspace-swe-swe-6f7a1ba3 down
```

This will cause the host's restart loop (`.swe-swe/restart-loop.sh`) to re-init and bring everything back up with the latest changes.
