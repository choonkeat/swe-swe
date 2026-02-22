# Audit: tdspec vs source code fidelity

**Date:** 2026-02-22
**Scope:** All 8 tdspec modules against `swe-swe-server/main.go`, `terminal-ui.js`, `entrypoint.sh`, and `agent-reverse-proxy` source (v0.2.5).

## Discrepancies

### 1. Topology.elm StdioBridge URL is stale

```elm
type StdioBridge
    = StdioBridge
-- doc comment says:
-- Spawned as: `npx @choonkeat/agent-reverse-proxy --bridge http://swe-swe:3000/proxy/$SESSION_UUID/preview/mcp`
```

The source (entrypoint.sh:68,95,121,158,189) now uses:

```sh
exec npx -y @choonkeat/agent-reverse-proxy --bridge http://localhost:9898/proxy/$SESSION_UUID/preview/mcp
```

The bridge URL changed from `swe-swe:3000` to `localhost:9898` as part of the path-based routing migration. All 5 bridge invocations (Claude MCP, Gemini, Codex, Goose, OpenCode) use `localhost:9898`.

**Severity:** Doc comment only. The StdioBridge type and Topology structure are correct; only the example command in the doc comment has a stale URL.

---

### 2. Open shim still uses `swe-swe:3000` — source code inconsistency

The open shim (entrypoint.sh:202) still uses the old URL:

```sh
curl -sf "http://swe-swe:3000/proxy/${SESSION_UUID}/preview/__agent-reverse-proxy-debug__/open?url=..."
```

But swe-swe-server binds to `:9898` (main.go `-addr :9898`), and port 3000 is the user app. The `/proxy/{uuid}/preview/__agent-reverse-proxy-debug__/open` endpoint is registered on swe-swe-server's mux, not the user app. The bridge was already migrated to `localhost:9898` but the open shim was not.

The tdspec OpenShim type says "sends HTTP GET /open?url=... to the preview proxy endpoint on swe-swe-server" and specifies the path as `/proxy/{uuid}/preview/__agent-reverse-proxy-debug__/open` — both correct. It does not specify the host:port, so the **tdspec itself is not wrong**. This is a source code bug, not a tdspec inaccuracy.

**Severity:** Source code bug (not tdspec). The curl likely fails silently (`-sf` flags + background `&`).

---

## Previous audit findings — all resolved

The 4 discrepancies from 2026-02-21 have all been fixed:

1. **PtyProtocol.elm doc port** — Now correctly says `:9898` (was `:3000`).
2. **DebugProtocol.elm endpoint context** — Now correctly shows path-based endpoints on `swe-swe-server :9898` with `?role=shell` and `?role=inject`.
3. **agentChatPort wire type** — Now `AgentChatPort` (not `Maybe AgentChatPort`), with doc note that wire value is `0` for terminal sessions.
4. **`/ws` role parameter** — Now documented in DebugProtocol.elm doc comment.

---

## Verified correct

Thorough re-verification of all modules against current source:

- **Topology.elm** — Process list, 6 WebSocket channels, proxy chain paths, port 9898, OpenShim path, vestigial `/agent` WS note. `fullTopology` record structure matches real system. Agent chat proxy at `/proxy/{uuid}/agentchat/`.

- **Domain.elm** — All types accurate. System overview comment correct (4 WS + 2 when Preview active = 6 total).

- **PtyProtocol.elm** — All ClientMsg variants match source wire types: `ping` (data echo), `rename_session` (name field), `toggle_yolo` (no payload), `chat` (userName, text), binary resize (5 bytes: `[0x00, rows_hi, rows_lo, cols_hi, cols_lo]`), binary file upload. All ServerMsg variants match: `pong` (data echo), `status` (12 fields), `chat` (userName, text, timestamp RFC3339), `file_upload` (success/filename/error), `exit` (exitCode, worktree). StatusPayload has all fields accounted for with correct nesting note.

- **DebugProtocol.elm** — All message types and `t` discriminator field. ShellPageDebugMsg (Init, UrlChange, NavState) correctly attributed — inject.js sends nav messages via `postMessage` to shell page, which relays them over the shell WS (`?role=shell`). InjectJsDebugMsg (Console, Error, Rejection, Fetch, Xhr, QueryResult, WsUpgrade) matches inject.js WS (`?role=inject`) output. FetchResult success/failure vs XhrResult flat asymmetry is real.

- **DebugHub.elm** — Routing rules verified against debughub.go:
  - Shell/inject → `ForwardToAgent()` → agent + UI observers + in-proc (3 destinations). ✅
  - HTTP `/open` → `SendToUIObservers()` → UI observers only. ✅
  - UI Navigate/Reload → `ForwardToShellClients()`. ✅
  - UI Query → `ForwardToInjectClients()`. ✅
  - Unknown commands → `ForwardToIframes()` (both pools). ✅
  - `ForwardToAgent` three-destination comment accurately describes Go behavior.

- **TerminalUi.elm** — Two WS connections per instance (PTY + debug UI). `onPtyMessage` Status triggers `ConnectDebugWebSocket`. `onDebugMessage` handles Init/UrlChange/NavState/Open correctly. The Open broadcast bug is real: `SendToUIObservers` sends to all UI observers → both terminal-ui instances call `openIframePane` → both trigger `confirm()` for external URLs → 2x dialogs. `ConfirmExternalUrl` effect exists in the type but not produced by `onDebugMessage` (it's an internal effect of `openIframePane`/`setPreviewURL`).

- **PreviewIframe.elm** — Shell page sends Init/UrlChange/NavState, receives Navigate/Reload. inject.js sends Console/Error/Rejection/Fetch/Xhr/WsUpgrade/QueryResult, receives DomQuery. QueryResult `rect` field models essential DOMRect fields (x, y, width, height); derived fields (top, left, bottom, right) correctly omitted. All match source.

- **HttpProxy.elm** — Port derivation (`agentChatPort = previewPort + 1000`). ProbeResult classification via `X-Agent-Reverse-Proxy` header (currently version `"0.2.5"`). Probe uses GET (not HEAD) for iOS Safari CORS bug workaround. 10 attempts, 2s→30s exponential backoff. PlaceholderDismiss paths verified: Preview primary via Debug WS (init/urlchange), fallback via iframe.onload; Agent Chat via iframe.onload only. CSS classes: `.terminal-ui__iframe-placeholder` (preview), `.terminal-ui__agent-chat-placeholder` (agent chat).

## Notable change since previous audit

The previous audit noted "Both the stdio bridge command and the open shim use `http://swe-swe:3000/proxy/...`". The bridge has since been migrated to `http://localhost:9898/proxy/...` (5 instances updated). The open shim was not updated — only it still uses `swe-swe:3000`.
