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
| App Preview | External port 11977 | Via existing preview proxy |

Both work because:
- The dev container and Chrome container share the same Docker network (`swe-network`)
- Chrome can resolve `swe-swe` hostname to reach our container
- App Preview routes through production's preview proxy (9899 → 3000)

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
- Runs `go run main.go -addr :3000 -no-preview-proxy`
- Default port is 3000, override with `DEV_PORT=3001 make run`
- The `-no-preview-proxy` flag disables the preview proxy (production server handles it)

### `make stop`
- Finds and kills the dev server process
- Reports whether server was running or not

## Server Flags

| Flag | Default | Description |
|------|---------|-------------|
| `-addr` | `:9898` | Listen address |
| `-no-preview-proxy` | `false` | Disable preview proxy server |
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
│                        │    :9899     │                    │
│                        │  (→ :3000)   │                    │
│                        └──────────────┘                    │
└─────────────────────────────────────────────────────────────┘
                              │
                              ▼
                    External: port 11977
                       (App Preview)
```

## Differences from Production

| Aspect | Dev Server | Production |
|--------|------------|------------|
| Port | 3000 | 9898 |
| Preview proxy | Disabled via `-no-preview-proxy` | Runs on 9899 |
| Build | `go run` (JIT compile) | Pre-compiled binary |
| Source | Template directory | Embedded in CLI |

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
