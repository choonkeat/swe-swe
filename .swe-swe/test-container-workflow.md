# Test Container Workflow

## Full workflow

```bash
./scripts/01-test-container-init.sh
./scripts/02-test-container-build.sh
HOST_PORT=9899 HOST_IP=host.docker.internal ./scripts/03-test-container-run.sh
```

mcp browser can test at http://host.docker.internal:9899/

## Home Directory Persistence

By default, `EFFECTIVE_HOME` is set to `/workspace/.home` which persists across container restarts and script re-runs.

What persists in `/home/app` (container) = `/workspace/.home` (host):
- Shell history (`.bash_history`, `.zsh_history`)
- SSH keys and config
- Git config
- Tool configs (`.claude/`, etc.)

To use ephemeral home (cleaned by script 05):
```bash
EFFECTIVE_HOME=/workspace/tmp/home ./scripts/01-test-container-init.sh
EFFECTIVE_HOME=/workspace/tmp/home ./scripts/02-test-container-build.sh
EFFECTIVE_HOME=/workspace/tmp/home HOST_PORT=9899 HOST_IP=host.docker.internal ./scripts/03-test-container-run.sh
```

## Teardown

```bash
./scripts/04-test-container-down.sh      # Stop container only
./scripts/05-test-container-clean.sh     # Remove container, image, and tmp/ (but NOT .home/)
```
