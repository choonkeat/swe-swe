# MCP Debug Channel Tools

**Date**: 2026-02-02
**Status**: In Progress (Phase 2 complete)
**Research**: `research/2026-02-02-agent-debug-channel-discoverability.md`

## Goal

Expose the preview debug channel as MCP tools (`preview_query`, `preview_listen`) so agents discover them in their tool list alongside `browser_snapshot`, solving the discoverability gap where agents default to MCP Playwright instead of the debug channel for preview inspection.

---

## Phase 1: Implement `--mcp` in main.go with tests ✅

### What will be achieved

A new `--mcp` flag on `swe-swe-server` that runs a stdio MCP server exposing two tools. Tests in `mcp_test.go` run as part of `make test`.

### Steps

1. Add `--mcp` boolean flag in `main()` alongside existing flags
2. Add MCP protocol types (structs for JSON-RPC request/response, tool definitions, etc.)
3. Add `runMCP(in io.Reader, out io.Writer, endpoint string)` function:
   - Reads newline-delimited JSON-RPC from `in` via `bufio.Scanner`
   - Writes JSON-RPC responses to `out` (one line per message)
   - Dispatches: `initialize`, `notifications/initialized`, `tools/list`, `tools/call`, `ping`
   - Logs to stderr
4. `initialize` → returns `protocolVersion: "2025-11-25"`, `capabilities: { tools: {} }`, `serverInfo`
5. `tools/list` → returns `preview_query` and `preview_listen` with inputSchema and descriptions
6. `tools/call` for `preview_query` → wraps existing WebSocket logic from `runDebugQuery`
7. `tools/call` for `preview_listen` → time-bounded collect from debug WebSocket
8. Wire `--mcp` in `main()` between existing `--debug-*` flags and server startup: `runMCP(os.Stdin, os.Stdout, *debugEndpoint)`
9. Write `mcp_test.go`:
   - **TestMCPInitialize**: feed initialize JSON-RPC, verify response has correct protocolVersion, tools capability, serverInfo
   - **TestMCPToolsList**: feed tools/list, verify both tools returned with correct names, descriptions, inputSchemas
   - **TestMCPToolsCallUnknown**: feed tools/call with bad name, verify JSON-RPC error -32602
   - **TestMCPToolsCallPreviewQueryNoConnection**: call preview_query with no debug server, verify `isError: true` response
   - **TestMCPToolsCallPreviewListenNoConnection**: call preview_listen with no debug server, verify `isError: true`
   - **TestMCPParseError**: feed malformed JSON, verify JSON-RPC parse error -32700
   - **TestMCPMethodNotFound**: feed unknown method, verify JSON-RPC error -32601
   - **TestMCPPing**: feed ping, verify pong response
   - **TestMCPToolsCallPreviewQueryIntegration**: start mock WebSocket debug server, call preview_query, verify DOM result
   - **TestMCPToolsCallPreviewListenIntegration**: start mock WebSocket debug server that sends messages, call preview_listen, verify messages collected

### Design: testability

`runMCP` takes `io.Reader` and `io.Writer` parameters instead of hardcoding `os.Stdin`/`os.Stdout`. Tests pass `bytes.Buffer` / `strings.Reader`. Integration tests use `httptest.NewServer` with WebSocket upgrader to mock the debug endpoint.

### Tool definitions

```
preview_query:
  description: "Capture a snapshot of the Preview tab content by CSS selector.
               Returns the text, HTML, and visibility of matching elements in the user's Preview tab.
               This is the correct tool for inspecting the Preview tab — browser_snapshot cannot see Preview tab content."
  inputSchema:
    type: object
    properties:
      selector:
        type: string
        description: "CSS selector (e.g. 'h1', '.error-message', '#app')"
    required: [selector]

preview_listen:
  description: "Returns console logs, errors, and network requests from the Preview tab.
               Listens for the specified duration and returns all messages.
               This is the correct tool for debugging the Preview tab — browser_console_messages cannot see Preview tab output."
  inputSchema:
    type: object
    properties:
      duration_seconds:
        type: number
        description: "How long to listen (default: 5, max: 30)"
```

### MCP protocol reference (stdio transport)

- Messages are newline-delimited JSON (no embedded newlines)
- Server reads from stdin, writes to stdout, logs to stderr
- Lifecycle: client sends `initialize` request → server responds → client sends `notifications/initialized` → operation phase
- Protocol version: `2025-11-25`
- JSON-RPC 2.0: `{"jsonrpc":"2.0","id":N,"method":"...","params":{...}}`
- Notifications have no `id` field
- Error codes: -32700 (parse error), -32601 (method not found), -32602 (invalid params)

### Verification

- `make test` runs `test-server` → `go test ./...` → picks up `mcp_test.go`
- All existing tests still pass (new file only, no modifications to existing functions)

---

## Phase 2: Manual CLI verification ✅

### What will be achieved

Confirm the compiled binary works end-to-end as stdio MCP server by piping JSON-RPC through it. Catches flag wiring and stdio buffering issues.

### Steps

1. `make build`
2. Pipe initialize → initialized → tools/list sequence, verify output
3. Pipe tools/call for preview_query (no preview running), verify isError response
4. Pipe malformed JSON, verify parse error
5. Verify process exits cleanly on EOF

### Verification

Manual spot-check. Phase 1's `make test` is the automated safety net.

---

## Phase 3: Config plumbing + golden-update + test container e2e (config+goldens ✅, e2e pending)

### What will be achieved

Wire `swe-swe-server --mcp` as a second MCP server in every agent platform's config, update golden tests, boot test container, verify agent discovers and prefers `preview_query`.

### Steps

**Config plumbing:**

1. Update `.mcp.json` template (Claude default) — add `swe-swe-preview` server:
   ```json
   "swe-swe-preview": {
     "command": "swe-swe-server",
     "args": ["--mcp"]
   }
   ```
2. Update `entrypoint.sh` — add `swe-swe-preview` to each platform block:
   - OpenCode: JSON with `"type": "local"`, `"command": ["swe-swe-server", "--mcp"]`
   - Codex: TOML `[mcp_servers.swe-swe-preview]`
   - Gemini: JSON in `mcpServers`
   - Goose: YAML in `extensions`
3. Sharpen `/debug-with-app-preview` skill description:
   - `.md`: `description: Inspect App Preview page content — use instead of browser tools for preview`
   - `.toml`: matching description

**Golden update:**

4. `make build golden-update`
5. Verify golden diffs: new MCP server entry in configs, updated skill description
6. `make test` — all tests pass

**Test container e2e:**

7. `./scripts/test-container/01-init.sh` → `02-build.sh` → `03-run.sh`
8. Open browser, log in (password: changeme)
9. Create OpenCode session
10. Type: "run a python web server on localhost:$PORT"
11. Type: "what's showing on my preview page?"
12. Observe which tool agent calls — success = `preview_query`, failure = `browser_snapshot`
13. If agent picks wrong tool, iterate on description string → rebuild → retry
14. `./scripts/test-container/04-down.sh`

### Verification

**Automated**: `make test` passes (goldens + mcp_test.go + existing)

**Manual**: Agent picks `preview_query` over `browser_snapshot` when asked about preview content

### Iteration loop if description needs tuning

Edit description in main.go → `make build` → `01-init.sh` → `02-build.sh` → `03-run.sh` → test. No logic changes, just string tuning.

---

## Files to modify

| File | Phase | Change |
|------|-------|--------|
| `cmd/swe-swe/templates/host/swe-swe-server/main.go` | 1 | Add `--mcp` flag, `runMCP()`, MCP types |
| `cmd/swe-swe/templates/host/swe-swe-server/mcp_test.go` | 1 | New file — all MCP tests |
| `cmd/swe-swe/templates/container/.mcp.json` | 3 | Add `swe-swe-preview` server |
| `cmd/swe-swe/templates/host/entrypoint.sh` | 3 | Add `swe-swe-preview` to each platform block |
| `cmd/swe-swe/slash-commands/swe-swe/debug-with-app-preview.md` | 3 | Sharpen description |
| `cmd/swe-swe/slash-commands/swe-swe/debug-with-app-preview.toml` | 3 | Sharpen description |
| `cmd/swe-swe/testdata/golden/**` | 3 | Mechanical update via `make golden-update` |

## No new dependencies

- MCP JSON-RPC is hand-rolled (protocol surface is small for a tool-only server)
- WebSocket client code already exists (`gorilla/websocket`)
- No npm packages, no external MCP SDK
