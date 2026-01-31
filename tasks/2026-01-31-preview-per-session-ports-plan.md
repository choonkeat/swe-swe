# Per-Session Preview Ports (Plan)

## Goal

Give each session a unique app PORT (3000-3019 range). The preview proxy should listen on 5{PORT} (e.g., 3007 -> 53007) and proxy to localhost:{PORT}. Parent/child terminal sessions must share the same PORT. Preview placeholder should show `localhost:${PORT}`.

## Decisions (from discussion)

- Allocation: iterate PORT 3000-3019; claim a PORT if both `:{PORT}` and `:5{PORT}` are available.
- No extra mutex beyond `sessionsMu` (getOrCreateSession already serialized).
- Inject `PORT` env into the session process exec (force override).
- Preview proxy listen should change from `1{PORT}` to `5{PORT}`.
- Preview placeholder text should say `localhost:${PORT}`.
- Docker/Traefik should expose the preview proxy range; do not expose directly without Traefik.
- Debug connections should target the per-session preview proxy port (5{PORT}); update CLI debug defaults accordingly.

## Security/TLS guidance

- Avoid exposing preview proxy ports directly from the container because it would require passing TLS private keys into that container (or running without TLS), which is undesirable.
- Prefer routing those ports through Traefik to keep existing TLS + forwardAuth protections.

## Implementation Plan

## Progress

- [x] 1) Session-level port assignment
- [x] 2) Preview proxy server lifecycle
- [x] 3) Inject PORT into session process env
- [x] 4) UI + Preview URL
- [x] 4.5) CLI debug endpoint default
- [ ] 5) Preview error page placeholder
- [ ] 6) Docker/Traefik exposure of preview range
- [ ] 7) Docs and test updates

### 1) Session-level port assignment

Files:
- `cmd/swe-swe/templates/host/swe-swe-server/main.go`

Steps:
- Add `PreviewPort int` to `Session` struct.
- In `getOrCreateSession` (under `sessionsMu`), compute `previewPort`:
  - If `parentUUID` is set and parent exists, reuse parent `PreviewPort`.
  - Else allocate first available PORT in 3000-3019.
- Port allocation logic (new helper):
  - For each PORT in range:
    - Attempt `net.Listen("tcp", fmt.Sprintf(":%d", 50000+PORT))` or string concat `"5"+PORT`.
    - Attempt `net.Listen("tcp", fmt.Sprintf(":%d", PORT))`.
    - If both succeed, close the `:{PORT}` probe listener and keep the `:5{PORT}` listener for the preview proxy to use.
    - If either fails, close any opened listener and try next.
  - If none available, return error.
- Store `PreviewPort` on session struct.

### 2) Preview proxy server lifecycle

Files:
- `cmd/swe-swe/templates/host/swe-swe-server/main.go`

Steps:
- Adjust `startPreviewProxy` to accept a `net.Listener` (already bound to :5{PORT}) + target port string.
- Start the proxy server on that listener instead of fixed `:9899`.
- Update log text to show actual listen port and target port.
- Ensure debug endpoints (`/__swe-swe-debug__/ws`, `/agent`) are served by the per-session proxy goroutine (same mux).
- Ensure parent/child sharing doesn’t start two servers on the same listener:
  - Add a global map: `previewServers map[int]*previewServerRef` with `listener`, `server`, `refCount`.
  - When a session is created, increment refCount and reuse existing server if present.
  - On `Session.Close()`, decrement refCount; if 0, shut down server and close listener.

### 3) Inject PORT into session process env

Files:
- `cmd/swe-swe/templates/host/swe-swe-server/main.go`

Steps:
- When starting `exec.Command`, append `PORT=<PreviewPort>` to `cmd.Env`.
- Do this in both `getOrCreateSession` and `RestartProcess`.
- Keep `TERM=xterm-256color` as-is.

### 4) UI + Preview URL

Files:
- `cmd/swe-swe/templates/host/swe-swe-server/static/terminal-ui.js`

Steps:
- Extend the session status payload to include `previewPort`.
- In `handleJSONMessage` for `status`, store `this.previewPort`.
- Compute `previewBaseUrl` using `5{previewPort}` rather than `1${window.location.port}`.
- Replace other places that build preview URL to use the derived `previewBaseUrl`.

### 4.5) CLI debug endpoint default

Files:
- `cmd/swe-swe/templates/host/swe-swe-server/main.go`

Steps:
- Update `runDebugListen`/`runDebugQuery` default endpoint to use the per-session preview port:
  - If `SWE_PREVIEW_PORT` is set, default to `ws://localhost:5${SWE_PREVIEW_PORT}/__swe-swe-debug__/agent`.
  - Otherwise, keep the existing fallback (9899) for backward compatibility.
- Update `--debug-endpoint` flag description to mention 5{PORT} default when `SWE_PREVIEW_PORT` is set.

### 5) Preview error page placeholder

Files:
- `cmd/swe-swe/templates/host/swe-swe-server/main.go`

Steps:
- Update `previewProxyErrorPage` to show `localhost:${PORT}`.
- Ensure the string is fed `previewPort` value (not the proxy port).

### 6) Docker/Traefik exposure of preview range

Files:
- `cmd/swe-swe/templates/host/docker-compose.yml`
- `cmd/swe-swe/templates/host/traefik-dynamic.yml` (if routing rules exist here)
- CLI code that renders templates (likely `cmd/swe-swe/templates` + init command)

Steps:
- Add new `swe-swe init` flag: `--preview-ports=3000-3019`.
- Interpret the range as the app PORT range, not proxy port range.
- Generate port mappings for preview proxy in docker-compose:
  - For each PORT in range, map `5{PORT}` (host) -> preview proxy entrypoint port (container).
- Configure Traefik entrypoints for each preview port in the range (or a dynamic file provider config that adds them).
- Route each preview entrypoint to the preview proxy service, preserving existing TLS + forwardAuth.

### 7) Docs and test updates

Files:
- `docs/*` (any app preview docs)
- `cmd/swe-swe/testdata/golden/*` for template snapshots

Steps:
- Update docs to reference per-session PORT and preview URL derivation.
- Update golden test fixtures after template changes.

## Open Questions

- Final format for `--preview-ports`: only `start-end`? (e.g., `3000-3019`)
- Default range when flag omitted (still 3000-3019?)
- Should preview port range be validated to avoid privileged ports or >65535?
- Should we export `SWE_PREVIEW_PORT` for CLI debug usage (or reuse existing env)?
