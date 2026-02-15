# swe-swe-server Development Workflow

This document describes how to rapidly iterate on `swe-swe-server` changes without rebuilding containers.

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

## Quick Start

```bash
# From /workspace root directory
cd /workspace

# Start dev server on port 3000 (background, with logs)
make run > /tmp/server.log 2>&1 &

# View startup logs
cat /tmp/server.log

# Stop dev server
make stop
```

**Note**: First run downloads Go dependencies, which may take a moment.

## Access Points

| Channel | URL | Description |
|---------|-----|-------------|
| MCP Browser | `http://swe-swe:3000` | Direct access from Chrome container |
| App Preview | External port `20000+PORT` | Via per-session preview proxy (e.g., 23001) |

Both work because:
- The dev container and Chrome container share the same Docker network (`swe-network`)
- Chrome can resolve `swe-swe` hostname to reach our container
- App Preview routes through the per-session preview proxy (`20000+PORT` → `PORT`)

## Development Cycle

```bash
# 1. Edit server code
vim cmd/swe-swe/templates/host/swe-swe-server/main.go

# 2. Start dev server (background with logs)
make run > /tmp/server.log 2>&1 &

# 3. Verify server started
cat /tmp/server.log
curl -s http://localhost:3000/ | head -1  # should show <!DOCTYPE html>

# 4. Test via MCP browser
#    Navigate to: http://swe-swe:3000

# 5. Stop server when done or before restarting
make stop

# 6. Repeat from step 1
```

## Makefile Targets

### `make run`
- Copies `go.mod.txt` → `go.mod` and `go.sum.txt` → `go.sum`
- Runs `go run main.go -addr :$PORT -no-preview-proxy`
- Uses the `PORT` env var (set by the container); falls back to 3000 if unset
- The `-no-preview-proxy` flag disables per-session preview proxies (production server handles previews)

### `make stop`
- Finds and kills the dev server process
- Reports whether server was running or not

## Server Flags

| Flag | Default | Description |
|------|---------|-------------|
| `-addr` | `:9898` | Listen address |
| `-no-preview-proxy` | `false` | Disable per-session preview proxies |
| `-shell` | `claude` | Command to execute |
| `-working-directory` | current dir | Working directory for shell |

## Testing Changes

For unit tests without starting the server:
```bash
make test-server
```

This copies the template to `/tmp`, runs tests, and syncs `go.sum` changes back.

## Network Architecture

```
┌─────────────────────────────────────────────────────────────┐
│                     Docker Network (swe-network)            │
│                                                             │
│  ┌──────────────┐     ┌──────────────┐     ┌─────────────┐ │
│  │   Chrome     │     │   swe-swe    │     │  Production │ │
│  │  (MCP browser)│────▶│  container   │     │   server    │ │
│  │              │     │  :3000 (dev) │     │   :9898     │ │
│  └──────────────┘     └──────────────┘     └─────────────┘ │
│         │                                         │        │
│         │              ┌──────────────┐          │        │
│         └─────────────▶│ Preview Proxy│◀─────────┘        │
│                        │  :5{PORT}    │                    │
│                        │  (→ :3000)   │                    │
│                        └──────────────┘                    │
└─────────────────────────────────────────────────────────────┘
                              │
                              ▼
                    External: port 5{PORT}
                       (App Preview)
```

## Differences from Production

| Aspect | Dev Server | Production |
|--------|------------|------------|
| Port | 3000 | 9898 |
| Preview proxy | Disabled via `-no-preview-proxy` | Per-session ports (5{PORT}) |
| Build | `go run` (JIT compile) | Pre-compiled binary |
| Source | Template directory | Embedded in CLI |

## Session Page UI Development

The session page requires WebSocket connection for the terminal, which makes it difficult to iterate on HTML/CSS. Use **preview mode** to render the UI shell without terminal/WebSocket:

```bash
# View session page UI without WebSocket (safe mode)
http://swe-swe:3000/session/test123?assistant=claude&preview

# View session page UI in YOLO mode
http://swe-swe:3000/session/test123?assistant=claude&preview&yolo
```

The `?preview` query param:
- Renders the full session page HTML/CSS
- Skips terminal initialization and WebSocket connection
- Enables YOLO toggle for UI testing (add `&yolo` to test YOLO mode styling)
- Allows visual iteration on navigation, panels, styling, etc.

This is useful for fixing:
- Navigation bar layout and colors
- Panel tabs styling
- Header/status bar appearance
- YOLO toggle styling (use `&yolo` to test both states)
- Any CSS that doesn't depend on terminal content

## Troubleshooting

### Server won't start
Check if something else is using port 3000:
```bash
curl -s http://localhost:3000/ && echo "Port 3000 in use"
```

### Can't stop server
Find and kill manually:
```bash
ps aux | grep 'exe/main.*-addr :3000'
kill <pid>
```

### MCP browser can't reach server
Verify network connectivity:
```bash
# From inside the container
curl http://localhost:3000/
```

If localhost works but `swe-swe:3000` doesn't from Chrome, check Docker networking.

### MCP browser can't click on terminal
Xterm's `touch-scroll-proxy` div intercepts pointer events. Use `browser_type` with `fill` + `submit: true` instead of `browser_click` on the terminal textarea.
