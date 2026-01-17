# How to Restart swe-swe Stack

From inside the swe-swe container (with Docker socket access), run these commands to restart the entire stack:

```bash
# 1. Build the latest changes
make build # must not fail

# 2. Make sure our new cli can use
./dist/swe-swe.linux-amd64 init --previous-init-flags=reuse && ./dist/swe-swe.linux-amd64 build # must not fail

# 3. Stop the chrome container first (it doesn't respond to compose down)
docker stop home-app-workspace-swe-swe-6f7a1ba3-chrome-1

# 4. Bring down the rest of the stack
docker compose -p home-app-workspace-swe-swe-6f7a1ba3 down
```

This will cause the host's restart loop (`.swe-swe/restart-loop.sh`) to re-init and bring everything back up with the latest changes.
