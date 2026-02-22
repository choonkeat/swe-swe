# Audit: tdspec vs source code fidelity

**Date:** 2026-02-22 (post vestigial endpoint removal)
**Scope:** All 8 tdspec modules against `swe-swe-server/main.go`, `terminal-ui.js`, `entrypoint.sh`, and `agent-reverse-proxy` source (v0.2.7).

## Changes since previous audit

### Vestigial `/__agent-reverse-proxy-debug__/agent` endpoint removed

The agent WS endpoint (`handleDebugAgentWS`) and its supporting code have been removed from agent-reverse-proxy:

- `main.go`: Removed `HandleFunc("/__agent-reverse-proxy-debug__/agent", ...)` and `handleDebugAgentWS` function
- `debughub.go`: Removed `agentConn *websocket.Conn` field, `SetAgent`/`RemoveAgent` methods; renamed `ForwardToAgent` → `BroadcastFromIframe` (now sends to 2 destinations: UI observers + in-process subscribers)
- `Topology.elm`: Removed vestigial endpoint note from module doc comment
- `DebugHub.elm`: Updated comment to say "two destinations" (was three)

### Open shim bug fixed

The open shim now correctly uses `localhost:9898` (was `swe-swe:3000` in the 2026-02-22 morning audit). All entrypoint.sh invocations now target `localhost:9898`.

---

## Discrepancies

### None found

All 8 modules tally with source code. No new discrepancies.

---

## Verified correct

### Topology.elm

- 9 process types match real system ✅
- 6 WebSocket channels (WS 1–6) match real connections ✅
- `OpenEndpointHttp` path: `GET /proxy/{uuid}/preview/__agent-reverse-proxy-debug__/open` ✅
- PreviewProxyChain: `/proxy/{uuid}/preview` → UserApp :3000 ✅
- AgentChatProxyChain: `/proxy/{uuid}/agentchat` → McpSidecar :4000 ✅
- Vestigial agent WS note removed — matches source (endpoint deleted) ✅
- `StdioBridge` doc comment URL: `http://localhost:9898/proxy/$SESSION_UUID/preview/mcp` matches entrypoint.sh ✅

### Domain.elm

- All wrapper types (`Url`, `Path`, `SessionUuid`, `PreviewPort`, `AgentChatPort`, `Bytes`, `Timestamp`, `ServerAddr`) match usage across source ✅
- `Path` doc correctly describes URL bar split (fixed prefix + editable path) ✅
- `ServerAddr` doc correctly lists all 4 configuration sites (main.go, docker-compose, entrypoint bridge, entrypoint open shim) ✅

### PtyProtocol.elm

**ClientMsg** — all 7 variants match source wire types:
- `PtyInput Bytes` — raw binary frames ✅
- `Resize { cols, rows }` — binary `[0x00, rows_hi, rows_lo, cols_hi, cols_lo]` ✅
- `FileUpload { filename, data }` — binary `[0x01, name_len_hi, name_len_lo, ...name, ...data]` ✅
- `Ping` — JSON `{ type: "ping", data?: {...} }` ✅
- `RenameSession { name }` — JSON `{ type: "rename_session", name }` ✅
- `ToggleYolo` — JSON `{ type: "toggle_yolo" }` (no payload) ✅
- `Chat { userName, text }` — JSON `{ type: "chat", userName, text }` ✅

**ServerMsg** — all 6 variants match:
- `PtyOutput Bytes` — binary PTY output ✅
- `Pong` — JSON `{ type: "pong", data?: {...} }` (echoes client's data) ✅
- `Status StatusPayload` — 12 flat fields, tdspec nests into `ports`/`terminal`/`session`/`features` (§3.12 structural grouping, acceptable per §3.14) ✅
- `ChatMsg { userName, text, timestamp }` — JSON `{ type: "chat" }` with RFC3339 timestamp ✅
- `FileUploaded FileUploadResult` — JSON `{ type: "file_upload", success, filename?, error? }`; tdspec uses `Result` (structural mapping, acceptable) ✅
- `Exit ExitPayload` — JSON `{ type: "exit", exitCode, worktree?: { path, branch, targetBranch } }` ✅

### DebugProtocol.elm

**ShellPageDebugMsg** (sent on WS 5 by shell page):
- `Init { url, ts }` — shell page sends on load (also relays inject.js postMessage) ✅
- `UrlChange { url, ts }` — on navigation (pushState/popstate/hashchange) ✅
- `NavState { canGoBack, canGoForward }` — on navigation state change ✅

**ShellPageCommand** (received by shell page on WS 5):
- `ShellNavigate NavigateAction` — `{ t: "navigate", action: "back"|"forward" }` or `{ t: "navigate", url }` ✅
- `ShellReload` — `{ t: "reload" }` ✅

**InjectJsDebugMsg** (sent on WS 6 by inject.js) — all 7 variants match:
- `Console { m, args, ts }` ✅
- `Error { msg, file, line, col, stack, ts }` ✅
- `Rejection { reason, ts }` ✅
- `Fetch FetchResult` — success `{ url, method, status, ok, ms, ts }` or error `{ url, method, error, ms, ts }`; tdspec nests into `request`/`response` with `Result` (§3.12, acceptable) ✅
- `Xhr XhrResult` — always `{ url, method, status, ok, ms, ts }` (no error branch); tdspec nests `request` fields ✅
- `QueryResult { id, found, text, html, visible, rect }` ✅
- `WsUpgrade { from, to, ts }` ✅

**InjectCommand** (received by inject.js on WS 6):
- `DomQuery { id, selector }` ✅

**AllDebugMsg** (received by UI observers on WS 3/4):
- `FromShellPage ShellPageDebugMsg | FromInject InjectJsDebugMsg | Open { url }` ✅

**UiCommand** (sent by terminal-ui on WS 3/4):
- `Navigate NavigateAction | Reload | Query { id, selector }` — Navigate/Reload confirmed in terminal-ui.js; Query routed by DebugHub's `RouteCommand` ✅

### DebugHub.elm

Routing rules verified against debughub.go (post-refactor):
- Shell/inject → `BroadcastFromIframe()` → UI observers + in-process subscribers (2 destinations) ✅
- HTTP `/open` → `SendToUIObservers()` → UI observers only ✅
- UI Navigate/Reload → `ForwardToShellClients()` ✅
- UI Query → `ForwardToInjectClients()` ✅
- Unknown commands → `ForwardToIframes()` (both shell + inject pools) ✅
- Comment "two destinations" matches refactored Go code ✅

### TerminalUi.elm

- Two WS connections per instance (PTY `this.ws` + debug UI `this._debugWs`) ✅
- `onPtyMessage` Status → `ConnectDebugWebSocket` ✅
- `onDebugMessage` handles Init/UrlChange → `UpdateUrlBar (pathFromProxyUrl ...)` ✅
- `onDebugMessage` handles NavState → `EnableBackButton`/`EnableForwardButton` ✅
- `onDebugMessage` handles Open → `OpenIframePane` ✅
- `onDebugMessage` ignores `FromInject` — correct, MCP tools consume via in-process ✅
- Open broadcast bug documented — still present (all UI observers receive Open) ✅
- `pathFromProxyUrl` strips `/proxy/{uuid}/preview` prefix — matches `terminal-ui.js:3480-3492` ✅

### PreviewIframe.elm

- Shell page: `onPageLoad` → `Init`, `onUrlChange` → `UrlChange`, `onNavStateChange` → `NavState` ✅
- Shell page: `onShellCommand` routes Navigate → `NavigateIframe`, Reload → `ReloadIframe` ✅
- inject.js: `onConsole`, `onError`, `onRejection`, `onFetch`, `onXhr`, `onWsUpgrade` all produce correct `InjectSend` effects ✅
- inject.js: `onInjectCommand (DomQuery payload)` → `RunQuery payload` ✅

### HttpProxy.elm

- `agentChatPort (PreviewPort p) = AgentChatPort (p + 1000)` matches source ✅
- ProbeResult: `X-Agent-Reverse-Proxy` header presence (now version `"0.2.7"`) ✅
- PlaceholderDismiss: Preview primary=DebugWebSocket fallback=IframeOnLoad; AgentChat=IframeOnLoad only ✅

---

## Previous audit findings — all resolved

Both discrepancies from the 2026-02-22 morning audit are resolved:

1. **StdioBridge URL** — Doc comment now says `localhost:9898` (was `swe-swe:3000`). Fixed in commit `91da74494`.
2. **Open shim URL** — entrypoint.sh now uses `localhost:9898` (was `swe-swe:3000`). Fixed in commit `0fce8ea88`.

Additionally, the vestigial `/agent` WS endpoint noted in the previous audit's "Verified correct" section has been removed from both source and tdspec.
