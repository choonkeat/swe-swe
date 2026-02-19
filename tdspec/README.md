# WebSocket Architecture

Elm type-checked specification of the swe-swe WebSocket architecture.

**6 WebSockets + 1 HTTP endpoint** connecting browser and container processes.

See `HOW-TO.md` for the methodology behind this approach.

## Quick start

Type-check:

    make build

Browse docs with clickable type navigation:

    PORT=3000 make preview

## Spec modules

  - **Domain** -- shared primitives: `Url`, `SessionUuid`, `PreviewPort`, `Bytes`
  - **PtyProtocol** -- WS 1,2: `/ws/{uuid}` terminal-ui to swe-swe-server (PTY I/O)
  - **DebugProtocol** -- WS 3,4,5,6: debug channel message types
  - **DebugHub** -- routing logic inside agent-reverse-proxy
  - **TerminalUi** -- terminal-ui web component behavior (2 instances)
  - **PreviewIframe** -- shell page + inject.js inside the Preview tab
  - **Topology** -- full system wiring, all connections enumerated
