# Debug Channel: Mini Playwright Command Set

**Date**: 2026-02-01
**Status**: Pending
**Source**: research/2026-02-01-preview-tab-enhancements.md (Section 3)
**Depends on**: 2026-02-01-debug-channel-eval (eval is a subset of this)

## Goal

Extend the debug channel with structured browser-control commands, giving agents Playwright-like capabilities scoped to the preview iframe without CDP multiplexing.

## Command set

| Command | Agent sends | Iframe does |
|---------|------------|-------------|
| `eval` | `{ t: 'eval', id, code }` | `(0, eval)(code)`, return result |
| `click` | `{ t: 'click', id, selector }` | `querySelector(sel).click()` |
| `fill` | `{ t: 'fill', id, selector, value }` | Set `.value`, dispatch `input` + `change` events |
| `getText` | `{ t: 'getText', id, selector }` | Return `textContent` of matched element |
| `getAttribute` | `{ t: 'getAttribute', id, selector, attr }` | Return attribute value |
| `snapshot` | `{ t: 'snapshot', id }` | Serialize DOM into accessibility-like tree |
| `navigate` | `{ t: 'navigate', id, url }` | `window.location.href = url` |
| `back` | `{ t: 'back', id }` | `history.back()` |
| `forward` | `{ t: 'forward', id }` | `history.forward()` |
| `waitForSelector` | `{ t: 'waitFor', id, selector, timeout }` | Poll with MutationObserver, respond when found or timeout |
| `getUrl` | `{ t: 'getUrl', id }` | Return `window.location.href` |
| `getTitle` | `{ t: 'getTitle', id }` | Return `document.title` |

## Architecture

```
Agent CLI / MCP tool
    |  WebSocket: /__swe-swe-debug__/agent
    v
DebugHub (per-session relay, Go) -- dumb pipe, no command parsing
    |  WebSocket: /__swe-swe-debug__/ws
    v
debugInjectJS command handler (injected into every proxied HTML page)
    |
    v
  User's app DOM
```

The DebugHub relay remains a dumb pipe. All command interpretation happens in the injected JS.

## Advantages over CDP multiplexing

- Already per-session isolated (no multiplexing needed)
- No shared Chrome dependency or crash blast radius
- Works through the same proxy/auth boundary
- No new infrastructure -- just extending injected JS

## Limitations vs. real Playwright

- No true screenshots (only DOM snapshots)
- No network interception
- No cookie/storage manipulation beyond eval
- No PDF generation, file download interception, geolocation/permission mocking

## Files to change

| File | Change |
|------|--------|
| `cmd/swe-swe/templates/host/swe-swe-server/main.go` | Extend `debugInjectJS` with command handlers |

## Verification

1. `make run` per docs/dev/swe-swe-server-workflow.md
2. Connect to debug WebSocket, send each command type
3. Verify correct responses for click, fill, getText, snapshot, etc.
4. Test waitForSelector with dynamically added elements
5. Test error cases (selector not found, timeout)
