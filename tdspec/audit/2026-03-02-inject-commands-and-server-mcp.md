# Audit: inject.js commands, PtyProtocol ports, server MCP tools

Date: 2026-03-02

## Scope

Full audit of all 10 tdspec modules against source code.

## Discrepancies found and fixed

### DebugProtocol.elm — 5 missing InjectCommand + 5 missing UiCommand variants

Source: `.swe-swe/repos/agent-reverse-proxy/workspace/debughub.go:196-203`
Source: `.swe-swe/repos/agent-reverse-proxy/workspace/inject.go:342-431`

The DebugHub routes 6 command types to inject.js clients:
`query, click, type, fillForm, pressKey, evaluate`

Spec only had `DomQuery` / `Query`. Added:

- `InjectCommand`: DomClick, DomType, DomFillForm, DomPressKey, DomEvaluate
- `UiCommand`: Click, Type, FillForm, PressKey, Evaluate

### PreviewIframe.elm — 5 missing InjectAction variants

Downstream of the DebugProtocol changes. Added RunClick, RunType,
RunFillForm, RunPressKey, RunEvaluate with case branches in
`onInjectCommand`.

### DebugHub.elm — 5 missing onUiCommand case branches

Downstream of the UiCommand changes. Added case branches for Click,
Type, FillForm, PressKey, Evaluate — all forward to inject via
`ForwardToInject (Dom* payload)`.

### PtyProtocol.elm — 3 missing ports in StatusPayload

Source: `cmd/swe-swe/templates/host/swe-swe-server/main.go` (status message)

StatusPayload.ports was missing:

- `previewProxy : PreviewProxyPort`
- `agentChatProxy : Maybe AgentChatProxyPort`
- `public : PublicPort`

These were already defined in Domain.elm but not referenced.

### McpTools.elm — ServerTool type + agent chat tool gaps

Source: `cmd/swe-swe/templates/host/swe-swe-server/main.go:5425-5827`

**Server MCP tools (entirely missing):**
Added `ServerMcp` to `McpServer` union, `ServerTool` type with 10 variants
(ListSessions, CreateSession, EndSession, GetSessionOutput, SendSessionInput,
ListWorktrees, ListRecordings, PrepareRepo, SendChatMessage, GetChatHistory),
and `allServerTools` list.

**Agent chat tool gaps:**
- Added `SendVerbalReply` and `SendVerbalProgress` (voice mode tools)
- Added `image_urls` parameter to `SendMessage` and `SendProgress`

## Not fixed (intentional / out of scope)

### Flat-vs-nested record structure

PtyProtocol.elm uses nested records (`ports`, `terminal`, `session`,
`features`) while the wire format is flat JSON. This is intentional
structural grouping per HOW-TO.md section 3.14 and prior audit
`2026-02-21-source-fidelity.md:84`.

### Topology.elm — infrastructure docker services

Audit agent flagged missing auth, chrome, code-server services. These are
infrastructure containers, not message-flow protocol participants. Out of
scope for the topology spec which focuses on WebSocket and HTTP message flows.

## Verification

All 10 modules compile cleanly after fixes: `make build` succeeds.
