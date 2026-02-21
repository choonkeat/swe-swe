# Audit: tdspec vs source code fidelity

**Date:** 2026-02-21
**Scope:** All 8 tdspec modules against `swe-swe-server/main.go`, `terminal-ui.js`, `entrypoint.sh`, and `agent-reverse-proxy` source.

## Discrepancies

### 1. PtyProtocol.elm doc comment says server port :3000

```elm
{-| WS 1,2 — PTY WebSocket protocol.

Endpoint: `/ws/{uuid}` on swe-swe-server (:3000).
```

swe-swe-server listens on **:9898** (main.go:1453, Dockerfile CMD). Port 3000 is the user's app.

The Topology.elm module correctly documents `port 9898` for SweServer — this is only wrong in PtyProtocol's doc comment.

**Severity:** Doc comment only. The Topology module has it right.

---

### 2. DebugProtocol.elm doc references old port-based architecture

```elm
Endpoints (on agent-reverse-proxy at `:PROXY_PORT_OFFSET+port`):

    /__agent-reverse-proxy-debug__/ui   -- terminal-ui connects here  (WS 3,4)
    /__agent-reverse-proxy-debug__/ws   -- shell page & inject.js     (WS 5,6)
```

There is no separate agent-reverse-proxy port. The debug endpoints are path-based through swe-swe-server:

```
/proxy/{uuid}/preview/__agent-reverse-proxy-debug__/ui    (WS 3,4)
/proxy/{uuid}/preview/__agent-reverse-proxy-debug__/ws    (WS 5,6)
```

The endpoint paths after the prefix are correct, but the "on agent-reverse-proxy at `:PROXY_PORT_OFFSET+port`" phrasing is stale — it's from before the library-embedding change.

**Severity:** Doc comment. The endpoint sub-paths are correct; only the host:port context is wrong.

---

### 3. agentChatPort wire type: `int` not `Maybe`

The tdspec defines:

```elm
ports : { preview : PreviewPort, agentChat : Maybe AgentChatPort }
```

The source always sends the field as an `int` — it's `0` for terminal sessions (main.go:648-650), never `null`. The JS client treats `0` as falsy (`msg.agentChatPort || null`), so the behavior is equivalent, but the wire format is `0` not absent/null.

**Severity:** Minor. Behavioral equivalence via JS falsy coercion, but the wire type is technically `int`.

---

### 4. `/ws` role parameter not documented

The debug iframe endpoint `/__agent-reverse-proxy-debug__/ws` uses a `?role=` query parameter to distinguish connections:

- Shell page connects with `?role=shell`
- inject.js connects with `?role=inject`

The tdspec (DebugProtocol, PreviewIframe, Topology) describes them as separate logical channels (WS 5 vs WS 6) but doesn't document the `?role=` query parameter that implements the distinction. The agent-reverse-proxy `handleDebugIframeWS` handler routes to different pools based on this parameter (shell vs inject).

**Severity:** Implementation detail. The logical separation is correctly documented; the mechanism is not.

---

## Verified correct

These were checked and found accurate:

- **Topology.elm** — Process list, WebSocket channel count (6), proxy chain paths, swe-swe-server port 9898, OpenShim path, StdioBridge command, vestigial `/agent` WS note.
- **DebugHub.elm** — Routing rules (shell/inject -> BroadcastToUiObservers, /open -> SendToUiObserversOnly, UI Navigate/Reload -> ForwardToShellPage, UI Query -> ForwardToInject). The `ForwardToAgent` three-destination comment is accurate.
- **TerminalUi.elm** — Two WS connections per instance (PTY + debug UI). `onPtyMessage` Status triggers `ConnectDebugWebSocket`. `onDebugMessage` handles Init/UrlChange/NavState/Open correctly. The Open broadcast bug is documented and real.
- **PreviewIframe.elm** — Shell page sends Init/UrlChange/NavState, receives Navigate/Reload. inject.js sends Console/Error/Rejection/Fetch/Xhr/WsUpgrade/QueryResult, receives DomQuery. All match source.
- **DebugProtocol.elm** — Message types and JSON `t` discriminator field. FetchResult success/failure branches vs XhrResult flat status/ok (no error branch) — this asymmetry is real and matches inject.js.
- **HttpProxy.elm** — Port derivation (`agentChatPort = previewPort + 1000`), ProbeResult classification via `X-Agent-Reverse-Proxy` header, PlaceholderDismiss paths (DebugWebSocket primary for preview, IframeOnLoad for both).
- **Domain.elm** — All types accurate. System overview comment correct.
- **PtyProtocol.elm StatusPayload** — The wire format is flat (`previewPort`, `sessionName`, `cols`) while the tdspec groups fields into nested records (`ports.preview`, `session.name`, `terminal.cols`). Per HOW-TO.md section 3.14: record nesting differences are fine — the spec is clearer about what belongs together. All fields are present and correctly typed; only the grouping differs.

## Notable observations

### Bridge and open-shim both use `swe-swe:3000`

Both the stdio bridge command and the open shim use `http://swe-swe:3000/proxy/...`:

```sh
# Bridge (entrypoint.sh:68,95,121,158,188)
exec npx -y @choonkeat/agent-reverse-proxy --bridge http://swe-swe:3000/proxy/$SESSION_UUID/preview/mcp

# Open shim (entrypoint.sh:201)
curl -sf "http://swe-swe:3000/proxy/${SESSION_UUID}/preview/__agent-reverse-proxy-debug__/open?url=..."
```

But swe-swe-server binds to `:9898`. The `/proxy/{uuid}/preview/...` routes are registered on swe-swe-server's mux, not on the user's app port. The tdspec Topology.elm documents the same port 3000 URL, faithfully reflecting the source code.
