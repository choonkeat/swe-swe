# How to Restart swe-swe Stack

## Prerequisites

**IMPORTANT**: Before restarting, verify that template dependencies are correct
- see docs/dev/template-editing-guide.md
- make sure both go.mod and cmd/swe-swe/templates/host/swe-swe-server/go.mod.txt are using the same version of libraries if used on both sides

The swe-swe-server is built inside Docker using its own go.mod, so stale template files will cause the container to run old dependency versions even after restart.

**Check public ports**: If any sessions have PUBLIC_PORT active, check whether anything is listening on the public port range (default 5000-5019). Restarting will disrupt any public users accessing those ports. You can check with:
```bash
for port in $(seq 5000 5019); do (echo >/dev/tcp/localhost/$port) 2>/dev/null && echo "Port $port: LISTENING"; done
```

## Restart Commands

From inside the swe-swe container (with Docker socket access), run these commands **in this order**:

```bash
# 1. Re-init (builds binary + rebuilds Docker images with latest templates)
bash .swe-swe/pre-restart.sh

# 2. Stop any test containers
docker compose -p swe-swe-test down -t 10 2>/dev/null || true

# 3. Stop the chrome container (it doesn't always respond to compose down)
# Replace <project-name> with your actual compose project name (visible in `docker ps`)
docker stop -t 10 <project-name>-chrome-1

# 4. Bring down the rest of the stack (we're all going offline here, including agent)
docker compose -p <project-name> down -t 20
```

**Why this order**: Re-init (step 1) rebuilds images while the stack is still running, so build failures are caught before we go offline. The final compose down is the point of no return — the host restart loop brings everything back up.

## Host Apt Upgrade (separate)

Host OS security patches are independent of code changes. Run this separately, not as part of every reboot:

```bash
docker run --rm --privileged --pid=host --network=host alpine sh -c \
  "nsenter -t 1 -m -u -i -n -p -- /bin/bash -c 'apt-get update -qq && DEBIAN_FRONTEND=noninteractive apt-get upgrade -y -qq'"
```

If apt upgrade installed a new kernel, a host reboot is needed for it to take effect. Reboot via DigitalOcean Console or:
```bash
docker run --rm --privileged --pid=host alpine nsenter -t 1 -m -u -i -n -p -- /sbin/reboot
```
