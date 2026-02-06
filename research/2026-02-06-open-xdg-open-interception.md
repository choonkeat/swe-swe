# Research: Intercepting `open` / `xdg-open` in swe-swe Containers

**Date**: 2026-02-06
**Status**: Research complete, ready for implementation

## Problem

When agents or user code inside the container call `open <url>` or `xdg-open <url>`, nothing happens — there's no display, no browser. We want to intercept these calls and route the URL to the swe-swe Preview pane instead.

## How Programs Open URLs

Programs use a cascade of strategies to open URLs. The order varies by tool:

### The `BROWSER` Environment Variable

Proposed by Eric S. Raymond (2001), modeled after `PAGER`/`EDITOR`. The spec says:

- **Colon-separated list** of browser commands, tried left-to-right
- If an entry contains `%s`, it's replaced with the URL
- If no `%s`, the URL is appended as the first argument

In practice, most tools treat it as a **single command name** (not colon-separated).

### What Each Tool Checks

| Tool | Chain |
|---|---|
| **Go** (`cmd/internal/browser`) | `$BROWSER` → (linux+DISPLAY: `xdg-open`) → `chrome` / `google-chrome` / `chromium` / `firefox` |
| **Go** (`pkg/browser`) | `xdg-open` → `x-www-browser` → `www-browser` (does NOT check `$BROWSER`) |
| **Python** (`webbrowser`) | `$BROWSER` (colon-separated, `%s` support) → `xdg-open` → known browsers → text browsers |
| **Node.js** (`open` / `sindresorhus/open`) | macOS: `open`; Linux: bundled `xdg-open` script (does NOT check `$BROWSER`) |
| **Cargo** (`cargo doc --open`) | `$BROWSER` → `xdg-open` → `gnome-open` → `kde-open` |
| **GitHub CLI** (`gh`) | `$GH_BROWSER` → config `browser` → `$BROWSER` |
| **Create React App** | `$BROWSER` env var (`BROWSER=none` to disable) |
| **Debian** (`sensible-browser`) | `$BROWSER` → `x-www-browser` → `www-browser` |

### `xdg-open` Internals

`xdg-open` is a shell script (~900 lines) that:

1. Detects desktop environment via `$XDG_CURRENT_DESKTOP`, `$KDE_FULL_SESSION`, etc.
2. Delegates to DE-specific opener (`gio open`, `kde-open`, `exo-open`, etc.)
3. Falls back to generic handler: checks MIME associations, then `$BROWSER`
4. If `$BROWSER` is unset and no `$DISPLAY`, tries text-mode browsers only

**Key insight**: In our headless container, `xdg-open` would fail at every step. We don't need the real `xdg-open` at all — a shim is strictly better.

## Interception Strategy

### Three-Layer Coverage

For maximum coverage, install three shims that all point to the same handler:

| Layer | What it catches |
|---|---|
| `BROWSER=/usr/local/bin/swe-swe-open` | Python, Go `cmd/internal/browser`, Cargo, `sensible-browser`, `gh`, CRA, `xdg-open` generic fallback |
| `/usr/local/bin/xdg-open` shim | Node.js `open` package, any tool calling `xdg-open` directly |
| `/usr/local/bin/open` shim | macOS-style calls (rare in containers, but covers edge cases) |

Optional fourth layer for Debian tools:
```
/usr/local/bin/x-www-browser  → symlink to swe-swe-open
/usr/local/bin/www-browser     → symlink to swe-swe-open
/usr/local/bin/sensible-browser → symlink to swe-swe-open
```

### Environment Setup

`buildSessionEnv()` in main.go (line 351) already controls the env for every session:

```go
func buildSessionEnv(previewPort int) []string {
    env := filterEnv(os.Environ(), "TERM", "PORT")
    env = append(env, "TERM=xterm-256color", fmt.Sprintf("PORT=%d", previewPort))
    return env
}
```

This is the injection point. We add `BROWSER` and prepend `PATH` here:

```go
func buildSessionEnv(previewPort int) []string {
    env := filterEnv(os.Environ(), "TERM", "PORT", "BROWSER", "PATH")
    env = append(env,
        "TERM=xterm-256color",
        fmt.Sprintf("PORT=%d", previewPort),
        "BROWSER=/home/app/.swe-swe/bin/swe-swe-open",
        fmt.Sprintf("PATH=/home/app/.swe-swe/bin:%s", os.Getenv("PATH")),
    )
    return env
}
```

No need to unset `DISPLAY` — if Go sees `DISPLAY` and tries `xdg-open`, our PATH-prepended shim catches it anyway.

## The Handler Script: `swe-swe-open`

The handler needs to take the URL and route it to the Preview pane. The Preview pane is already controllable via the debug hub WebSocket.

### Architecture: How URLs Reach the Preview Pane Today

```
Terminal UI  →  ws://localhost:5${PORT}/__swe-swe-debug__/ui
                    ↓ (DebugHub forwards to shell page observers)
Shell Page   ←  receives { t: 'navigate', url: '...' }
                    ↓
Inner iframe  ←  inner.src = cmd.url
```

The shell page (`/__swe-swe-shell__`) already handles `{ t: 'navigate', url: '...' }` commands over its WebSocket connection (main.go:1245-1259). The UI observer WebSocket at `/__swe-swe-debug__/ui` already forwards messages to iframe clients.

### Option A: WebSocket Client in the Script

The handler script connects to the debug hub and sends a navigate command:

```bash
#!/bin/sh
# /home/app/.swe-swe/bin/swe-swe-open
URL="${1:-}"
[ -z "$URL" ] && exit 0

PREVIEW_PORT="5${PORT:-3000}"

echo "{\"t\":\"navigate\",\"url\":\"$URL\"}" | \
  websocat "ws://localhost:${PREVIEW_PORT}/__swe-swe-debug__/ui" --one-message

echo "→ Preview: $URL" >&2
```

**Dependency**: Needs a WebSocket client CLI (`websocat`, `wscat`, etc.)

### Option B: HTTP Endpoint on swe-swe-server (Simplest)

Add a simple HTTP endpoint to `swe-swe-server` that accepts a URL and forwards it as a navigate command through the debug hub:

```bash
#!/bin/sh
# /home/app/.swe-swe/bin/swe-swe-open
URL="${1:-}"
[ -z "$URL" ] && exit 0

PREVIEW_PORT="5${PORT:-3000}"
curl -sf "http://localhost:${PREVIEW_PORT}/__swe-swe-debug__/open?url=$(printf '%s' "$URL" | jq -sRr @uri)" >/dev/null 2>&1 &

echo "→ Preview: $URL" >&2
```

This requires adding a small HTTP handler to the preview proxy in main.go:

```go
if r.URL.Path == "/__swe-swe-debug__/open" {
    url := r.URL.Query().Get("url")
    if url != "" {
        hub.BroadcastToShell(json.Marshal(map[string]string{"t": "navigate", "url": url}))
    }
    w.WriteHeader(http.StatusOK)
    return
}
```

**Advantages**: No extra dependencies; `curl` is already in the container; trivially simple script.

### Option C: `swe-swe-server --open <url>` Subcommand

Add a CLI subcommand to `swe-swe-server` itself:

```bash
#!/bin/sh
exec swe-swe-server --open "${1:-}"
```

The binary is already in the container. This could connect to the debug hub internally.

## Recommendation: Option B (HTTP Endpoint)

**Option B** is the best fit because:

1. **Zero new dependencies** — `curl` is already in the container (Dockerfile line 31)
2. **Trivial shell script** — no WebSocket complexity, no binary to install
3. **Works synchronously** — curl returns after the server processes the request
4. **Easy to test** — `curl http://localhost:53000/__swe-swe-debug__/open?url=https://example.com`
5. **Existing pattern** — the debug endpoints already live under `/__swe-swe-debug__/`

## Where Things Live

### `.swe-swe/bin/` — created by `entrypoint.sh`

The entrypoint already does per-agent config setup (MCP configs for Codex, Gemini, Goose, OpenCode) as the app user. Same pattern — create the bin dir, write the script, make symlinks. Real symlinks, no embed issues.

Added to `entrypoint.sh` (before the final `exec su`):
```bash
# URL open interception — route to Preview pane
mkdir -p /home/app/.swe-swe/bin
cat > /home/app/.swe-swe/bin/swe-swe-open << 'SCRIPT'
#!/bin/sh
URL="${1:-}"
[ -z "$URL" ] && exit 0
PREVIEW_PORT="5${PORT:-3000}"
curl -sf "http://localhost:${PREVIEW_PORT}/__swe-swe-debug__/open?url=$(printf '%s' "$URL" | jq -sRr @uri)" >/dev/null 2>&1 &
echo "→ Preview: $URL" >&2
SCRIPT
chmod +x /home/app/.swe-swe/bin/swe-swe-open
for cmd in xdg-open open x-www-browser www-browser sensible-browser; do
  ln -sf swe-swe-open "/home/app/.swe-swe/bin/$cmd"
done
chown -R app: /home/app/.swe-swe/bin
```

Runs as root (entrypoint runs as root before `exec su`), sets ownership to app. Idempotent via `ln -sf` and `cat >` overwrite.

### `buildSessionEnv()` — injects PATH and BROWSER

Every session process already gets its env from `buildSessionEnv(previewPort)` (main.go:351). This is where we prepend `.swe-swe/bin/` to PATH and set `BROWSER`:

```go
func buildSessionEnv(previewPort int) []string {
    env := filterEnv(os.Environ(), "TERM", "PORT", "BROWSER", "PATH")
    env = append(env,
        "TERM=xterm-256color",
        fmt.Sprintf("PORT=%d", previewPort),
        "BROWSER=/home/app/.swe-swe/bin/swe-swe-open",
        fmt.Sprintf("PATH=/home/app/.swe-swe/bin:%s", os.Getenv("PATH")),
    )
    return env
}
```

This means every agent session (Claude, Gemini, Codex, etc.) and every shell session automatically gets the interception. The `PORT` env var is already injected — the handler script just uses `$PORT` to derive the preview proxy port (`5${PORT}`).

### Preview proxy — add `/__swe-swe-debug__/open` HTTP endpoint

In the preview proxy handler (main.go ~line 1942), add:

```go
if r.URL.Path == "/__swe-swe-debug__/open" {
    url := r.URL.Query().Get("url")
    if url != "" {
        // Broadcast navigate command to shell page
        msg, _ := json.Marshal(map[string]string{"t": "navigate", "url": url})
        hub.BroadcastToShell(msg)
    }
    w.WriteHeader(http.StatusOK)
    return
}
```

### Shell page — already handles it

The shell page already processes `{ t: 'navigate', url: '...' }` (main.go:1245-1259):
```javascript
if (cmd.url) {
    inner.src = cmd.url;
}
```

For **external URLs** (not localhost), the iframe navigates directly. This is fine for most cases. If we need to proxy external URLs too, that's a separate concern.

## Implementation Plan

### 1. `entrypoint.sh` — create `.swe-swe/bin/` with script + symlinks

Write `swe-swe-open` script and symlinks for `xdg-open`, `open`, `x-www-browser`, `www-browser`, `sensible-browser`. Runs as root, `chown` to app.

### 2. `buildSessionEnv()` in main.go — inject PATH and BROWSER

Add to the existing function (main.go:351):
- `BROWSER=/home/app/.swe-swe/bin/swe-swe-open`
- Prepend `/home/app/.swe-swe/bin` to `PATH`

### 3. Preview proxy in main.go — add `/__swe-swe-debug__/open` HTTP endpoint

Accept `?url=` and broadcast `{ t: 'navigate', url }` to the shell page.

## Prior Art

- **VS Code Devcontainers**: Sets `BROWSER` to a helper script at `/vscode/vscode-server/bin/.../helpers/browser.sh` that communicates back to the VS Code client via IPC
- **GitHub Codespaces**: Same approach + automatic port forwarding detection
- **Distrobox**: Uses D-Bus portal (`org.freedesktop.portal.OpenURI`) or `host-spawn` to execute on host

## Edge Cases

| Case | Handling |
|---|---|
| URL is `localhost:PORT` | Preview proxy already handles this — navigate to the proxy URL `localhost:5PORT` |
| URL is external (https://...) | Shell page navigates iframe directly — works if not blocked by CSP |
| URL is a file path | Could `open` a file — ignore non-URL arguments or handle separately |
| Multiple rapid opens | Each curl fires async (`&`), last one wins in the Preview |
| `BROWSER=none` convention | CRA uses this to suppress — our handler should check and respect it |
| No preview port running | `curl` fails silently, no harm done |

## Environment Variables Summary

| Variable | Value | Purpose |
|---|---|---|
| `BROWSER` | `/home/app/.swe-swe/bin/swe-swe-open` | Primary interception for most tools |
| `PATH` | `/home/app/.swe-swe/bin:$PATH` | Shims shadow system `xdg-open`, `open`, etc. |
| `PORT` | (already injected per-session) | Handler derives preview port as `5${PORT}` |
