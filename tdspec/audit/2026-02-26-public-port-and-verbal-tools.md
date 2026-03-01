# Audit: tdspec vs source code fidelity

**Date:** 2026-02-26
**Scope:** All 9 tdspec modules against `swe-swe-server/main.go`, `terminal-ui.js`, `end-session.js`, `reconnect.js`, and `agent-chat/workspace/tools.go`.

## Discrepancies

### 1. PUBLIC_PORT not represented in tdspec (multi-module)

Source code now allocates a **third port per session** — a public (no-auth) port:

```
publicPort = previewPort + 2000   (e.g., 3000 → 5000)
publicProxyPort = publicPort + offset   (e.g., 5000 + 20000 = 25000)
```

**Affected modules:**

- **Domain.elm** — Missing `PublicPort` and `PublicProxyPort` types. The `ProxyPortOffset` constraint doc says `offset + 5019 < 65536`, which numerically accounts for public ports (5019 is the max public port), but no `PublicPort` type is defined to justify the 5019 figure.

- **PtyProtocol.elm** — `StatusPayload` missing 4 wire fields:
  - `publicPort : PublicPort` — container-internal port (e.g., 5000). Sent as `0` for sessions without a public port.
  - `publicProxyPort : PublicProxyPort` — Traefik-facing port (e.g., 25000).
  - `previewProxyPort : PreviewProxyPort` — wire includes this (was always sent, not in tdspec).
  - `agentChatProxyPort : AgentChatProxyPort` — conditionally sent when `agentChatPort != 0`.

  The previous audit said "12 flat fields". Wire now has 16–17 flat fields.

- **HttpProxy.elm** — Missing derivation function:
  ```
  publicPort (PreviewPort p) = PublicPort (p + 2000)
  publicProxyPort (ProxyPortOffset o) (PublicPort p) = PublicProxyPort (o + p)
  ```
  Source: `main.go:3389-3391`, `main.go:96`.

- **Topology.elm** — Missing public proxy chain. The `Traefik` doc lists only preview (:23000) and agent chat (:24000) per-port routes. Source adds:
  - Public: `:25000 → :25000` (port-based only, **no forwardauth**, no path-based fallback)

  The statement "Each per-port entrypoint gets its own Traefik router with forwardauth" is now inaccurate — public ports have **no forwardauth middleware**.

  `ProxyChains` type alias missing `public : PublicProxyChain`.

  The public proxy is **port-based only** — no path-based route on :9898. It reuses `agentChatProxyHandler` (HTTP forwarding + WebSocket relay, cookie stripping, no HTML injection, no DebugHub).

- **TerminalUi.elm** — `State` missing `publicPort` and `publicProxyPort` fields. These are stored by terminal-ui.js (`this.publicPort`, `this.publicProxyPort`) and used by end-session safety check.

### 2. Agent Chat verbal MCP tools not in McpTools.elm

Source (`agent-chat/workspace/tools.go`) implements 6 tools. The tdspec lists only 4.

**Missing tools:**

- **`SendVerbalReply`** — Send a spoken reply in voice mode (blocking, waits for user response). Parameters: `{ text, quickReply, moreQuickReplies?, imageUrls? }`. Output: user's spoken response. Publishes event type `"verbalReply"`.

- **`SendVerbalProgress`** — Send a spoken progress update in voice mode (non-blocking). Parameters: `{ text, imageUrls? }`. Output: `"Progress sent."`. Publishes event type `"verbalReply"`.

These tools are used when the user interacts via voice (message prefixed with microphone emoji). Calling `SendMessage` in voice mode returns an error directing use of `SendVerbalReply`.

`allAgentChatTools` should list 6 tools, not 4.

### 3. End Session feature not documented

New server endpoint and client-side module:

- **Server:** `POST /api/session/{uuid}/end` — terminates a session. Source: `main.go:4895-4930`.
- **Client:** `end-session.js` — shared utility with PUBLIC_PORT safety check. If something is listening on the public proxy port, prompts user to type the port number to confirm. Used by both homepage and terminal-ui.

This is a UI/HTTP concern rather than a WebSocket/proxy architecture concern, so it may be intentionally out of tdspec scope. Noting for completeness.

---

## Verified correct

### Domain.elm

- All existing types (`Url`, `Path`, `SessionUuid`, `PreviewPort`, `AgentChatPort`, `ProxyPortOffset`, `PreviewProxyPort`, `AgentChatProxyPort`, `Bytes`, `Timestamp`, `ServerAddr`) remain accurate.
- `ServerAddr` doc correctly lists `localhost:9898` across all 4 sites.
- `Path` doc correctly describes the URL bar split behavior.

### PtyProtocol.elm

**ClientMsg** — all 7 variants still match source wire types:
- `PtyInput`, `Resize`, `FileUpload`, `Ping`, `RenameSession`, `ToggleYolo`, `Chat` all verified.

**ServerMsg** — all 6 variants still match:
- `PtyOutput`, `Pong`, `Status`, `ChatMsg`, `FileUploaded`, `Exit` verified.
- `StatusPayload` structural grouping remains correct for the fields it covers.
- `ExitPayload` `{ exitCode, worktree? { path, branch, targetBranch } }` matches.

### DebugProtocol.elm

- All types (`ShellPageDebugMsg`, `ShellPageCommand`, `InjectJsDebugMsg`, `InjectCommand`, `FetchResult`, `XhrResult`, `AllDebugMsg`, `UiCommand`, `NavigateAction`) match source.
- WS endpoint paths (`/ui`, `/ws?role=shell`, `/ws?role=inject`) confirmed.

### DebugHub.elm

- Routing rules verified: shell/inject → `BroadcastToUiObservers`, HTTP /open → `SendToUiObserversOnly`, Navigate/Reload → `ForwardToShellPage`, Query → `ForwardToInject`.
- Open broadcast bug still present (all UI observers receive Open message).

### TerminalUi.elm

- Two WS connections per instance (PTY `this.ws` + debug `this._debugWs`).
- `onPtyMessage` / `onDebugMessage` handlers match source behavior.
- `pathFromProxyUrl` correctly strips proxy prefix.
- WS reconnect state machine:
  - PTY: `createReconnectState()` defaults — baseDelay=1000, maxDelay=60000 ✅
  - Debug: inline backoff `Math.min(1000 * 2^(attempts-1), 10000)` — baseDelay=1000, maxDelay=10000 ✅
- Placeholder state machine transitions match source.
- PendingNav (`_pendingPreviewIframeSrc`) pattern confirmed.

### PreviewIframe.elm

- Shell page and inject.js effects all match source.
- `onShellCommand` routing correct.
- `onInjectCommand (DomQuery)` → `RunQuery` confirmed.

### HttpProxy.elm

- `agentChatPort = previewPort + 1000` matches `main.go:3385-3386`.
- `previewProxyPort`, `agentChatProxyPort` derivation functions match source.
- Probe config (maxAttempts=10, baseDelay=2000, maxDelay=30000) matches `reconnect.js:88-92` and `terminal-ui.js:1029,3474`.
- Probe uses `method: 'GET'` (not default HEAD) — confirmed in `terminal-ui.js:1028,3473`.
- `X-Agent-Reverse-Proxy` header check for `classifyProbe` confirmed.
- PlaceholderDismiss: Preview primary=DebugWebSocket, fallback=IframeOnLoad; AgentChat=IframeOnLoad only — still correct.
- Two-phase probe state machine (PathProbing → PortChecking → Decided) matches `terminal-ui.js:3467-3497,1026-1057`.

### McpTools.elm

- Preview MCP tools (`BrowserSnapshot`, `BrowserConsoleMessages`) still correct.
- Preview MCP resources (`PreviewReference`, `PreviewHelp`) confirmed.
- Agent Chat MCP resources (`WhiteboardInstructions`, `WhiteboardDiagrammingGuide`, `WhiteboardQuickReference`) confirmed.
- `ConsoleEntry` variants all match source wire format.
- `DrawReply` (`Acknowledged` / `ViewerFeedback`) matches agent-chat implementation.

### Topology.elm

- 9 process types still match (with note that public proxy chain is missing from topology).
- 6 WebSocket channels (WS 1–6) still accurate.
- `OpenEndpointHttp` path correct.
- `PreviewProxyChain` and `AgentChatProxyChain` match source for existing proxy chains.
- `StdioBridge` target URL `localhost:9898` confirmed.

---

## Summary

| # | Discrepancy | Severity | Modules |
|---|---|---|---|
| 1 | PUBLIC_PORT (third port type, no-auth proxy chain) missing | High | Domain, PtyProtocol, HttpProxy, Topology, TerminalUi |
| 2 | Verbal MCP tools (SendVerbalReply, SendVerbalProgress) missing | Medium | McpTools |
| 3 | End Session feature not documented | Low | (out of scope?) |

The core WebSocket protocol, debug channel routing, and proxy mode detection remain accurate. The discrepancies are additive (new features not yet reflected) rather than contradictory (existing spec saying wrong things), with one exception: the Topology.elm Traefik doc's claim that "each per-port entrypoint gets its own Traefik router with forwardauth" is now incorrect for public ports.
