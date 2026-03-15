# swe-swe-server Development Workflow

This document describes how to rapidly iterate on `swe-swe-server` changes and test them.

## Source Location

The server source lives at:
```
cmd/swe-swe/templates/host/swe-swe-server/
├── main.go              # Main server code
├── go.mod.txt           # Go module (renamed during build)
├── go.sum.txt           # Go dependencies (renamed during build)
├── static/              # Embedded web assets
└── *_test.go            # Tests
```

## Quick Start (Dev Server)

```bash
# From /workspace root directory
cd /workspace

# Start dev server (background, with logs)
make run > /tmp/server.log 2>&1 &

# View startup logs
cat /tmp/server.log

# Stop dev server
make stop
```

**Note**: First run downloads Go dependencies, which may take a moment.

The dev server runs on `$PORT` (set by the container, typically 3002). Access it via:
- **MCP Browser**: `http://swe-swe:$PORT`
- **App Preview**: The preview tab in the session UI

## Development Cycle

```bash
# 1. Edit server code
vim cmd/swe-swe/templates/host/swe-swe-server/main.go

# 2. Stop previous server
make stop

# 3. Start dev server (background with logs)
make run > /tmp/server.log 2>&1 &

# 4. Verify server started
cat /tmp/server.log

# 5. Test via MCP browser or App Preview

# 6. Repeat from step 1
```

## Testing

### Unit Tests

```bash
make test          # All tests (CLI, server, MCP lazy-init)
make test-server   # Server template tests only
```

### E2E Tests (Real Container)

E2E tests build a real container in dockerfile-only mode and run Playwright tests against it. This tests the full stack: Dockerfile, entrypoint, server binary, MCP configs, auth, and agent interactions.

```bash
# Run all e2e tests
make test-e2e

# Run specific test file
./scripts/e2e.sh tests/login.spec.js

# Run with custom port
E2E_PORT=9790 ./scripts/e2e.sh
```

**What `make test-e2e` does:**
1. Builds the CLI (`make build-cli`)
2. Runs `swe-swe init` in `./tmp/e2e/` (auto-detects dockerfile-only mode)
3. Builds the Docker image via `docker compose build`
4. Starts the container via `docker compose up`
5. Runs Playwright tests against the real container
6. Tears down the container

**Test files:**
- `e2e/tests/login.spec.js` — Auth flow (login page, wrong password, correct password)
- `e2e/tests/agent-browser.spec.js` — Full chain: chat → OpenCode → Playwright MCP → browser start → screenshot

**Why real containers?** The e2e tests have already caught 3 bugs that only manifest in the actual container:
- `SWE_PORT` not passed to container environment
- Host env vars overriding `.env` in compose
- Hardcoded port 9898 in MCP configs (breaks when `SWE_PORT` differs)

## Makefile Targets

### `make run`
- Copies `go.mod.txt` → `go.mod` and `go.sum.txt` → `go.sum`
- Runs `go run main.go -addr :$PORT`
- Uses the `PORT` env var (set by the container); falls back to 3000 if unset

### `make stop`
- Finds and kills the dev server process
- Reports whether server was running or not

### `make test-e2e`
- Runs `./scripts/e2e.sh` — full container build + Playwright tests

## Server Flags

| Flag | Default | Description |
|------|---------|-------------|
| `-addr` | `:9898` | Listen address (overridden by `SWE_PORT` in dockerfile-only mode) |
| `-shell` | `claude` | Command to execute |
| `-working-directory` | current dir | Working directory for shell |

## Architecture

### Dockerfile-Only Mode (Default)

Single container with embedded auth. No Traefik, no separate services.

```
┌─────────────────────────────────────────────┐
│         swe-swe container                   │
│  ┌───────────────────────────────────────┐  │
│  │  swe-swe-server (:SWE_PORT)          │  │
│  │  - Embedded auth (SWE_SWE_PASSWORD)  │  │
│  │  - Session management                │  │
│  │  - MCP server                        │  │
│  └───────────────────────────────────────┘  │
│  ┌───────────────────────────────────────┐  │
│  │  Per-session processes:               │  │
│  │  - Agent (OpenCode/Claude/etc.)       │  │
│  │  - agent-chat sidecar                 │  │
│  │  - Playwright MCP (via mcp-lazy-init) │  │
│  │  - Browser stack (Xvfb/Chrome/VNC)    │  │
│  └───────────────────────────────────────┘  │
└─────────────────────────────────────────────┘
            │
            ▼
   External: port SWE_PORT (default 1977)
```

### Compose Mode (with SSL)

When SSL is enabled, Traefik is added as a reverse proxy. The server listens on port 9898 internally.

## Session Page UI Development

The session page requires WebSocket connection for the terminal, which makes it difficult to iterate on HTML/CSS. Use **preview mode** to render the UI shell without terminal/WebSocket:

```bash
# View session page UI without WebSocket (safe mode)
http://swe-swe:$PORT/session/test123?assistant=claude&preview

# View session page UI in YOLO mode
http://swe-swe:$PORT/session/test123?assistant=claude&preview&yolo
```

The `?preview` query param:
- Renders the full session page HTML/CSS
- Skips terminal initialization and WebSocket connection
- Enables YOLO toggle for UI testing (add `&yolo` to test YOLO mode styling)

## Differences from Production

| Aspect | Dev Server | Production (dockerfile-only) | Production (compose) |
|--------|------------|------------------------------|----------------------|
| Port | `$PORT` (3002) | `SWE_PORT` (default 1977) | 9898 (internal) |
| Auth | Uses `SWE_SWE_PASSWORD` env | Same | Same |
| Build | `go run` (JIT compile) | Pre-compiled binary in container | Same |
| MCP | Not available | Full MCP stack via entrypoint | Same |
| Browser | Not available | Xvfb/Chrome/VNC per session | Same |

## Troubleshooting

### Server won't start
Check if something else is using the port:
```bash
curl -s http://localhost:$PORT/ && echo "Port in use"
```

### Can't stop server
Find and kill manually:
```bash
ps aux | grep 'exe/main.*-addr'
kill <pid>
```

### MCP browser can't reach server
Verify network connectivity:
```bash
curl http://localhost:$PORT/
```

### MCP browser can't click on terminal
Xterm's `touch-scroll-proxy` div intercepts pointer events. Use `browser_type` with `fill` + `submit: true` instead of `browser_click` on the terminal textarea.
