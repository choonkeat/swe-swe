# ADR-028: Preview shell page architecture

**Status**: Accepted
**Date**: 2026-02-02
**Research**: `research/2026-02-02-preview-tab-navigation-spec.md`

## Context

Preview navigation controls (back/forward buttons, URL bar) require the parent page to:
1. Read the current URL from the preview iframe
2. Trigger navigation actions (back, forward, go to URL, reload)

However, the parent page and preview iframe are cross-origin (different ports). The same-origin policy blocks direct iframe manipulation — `iframe.contentWindow.location` and `iframe.contentWindow.history` are inaccessible.

The debug WebSocket channel exists but the injected script inside user content can be destroyed by navigation, losing the connection.

## Decision

Introduce a **shell page** served at `/__swe-swe-shell__` that wraps user content in an inner iframe:

```
Parent page (port 1977)
└── iframe.tab-preview
    └── Shell page at {host}:5{PORT}/__swe-swe-shell__
        ├── Monitoring script (persistent, survives navigation)
        ├── Debug WebSocket connection (persistent)
        └── Inner iframe → user content at {host}:5{PORT}/path
```

### Key Properties

- **Shell ↔ Inner iframe**: Same-origin (both on port 5{PORT}). Shell can read `innerIframe.contentWindow.location.href`, call `history.back()`, etc.
- **Parent ↔ Shell**: Cross-origin but communicate via debug WebSocket
- **Monitoring script**: Never destroyed by user navigation — it lives in the shell, not the user content

### Protocol

**Shell → Parent (via WebSocket):**
- `{ t: 'urlchange', url: '...' }` — on every navigation
- `{ t: 'navstate', canGoBack: bool, canGoForward: bool }` — for button state

**Parent → Shell (via WebSocket):**
- `{ t: 'navigate', action: 'back' }` — trigger history.back()
- `{ t: 'navigate', action: 'forward' }` — trigger history.forward()
- `{ t: 'navigate', url: '/path' }` — set innerIframe.src
- `{ t: 'reload' }` — trigger location.reload()

### URL Bar Invariant

The URL bar always shows `localhost:PORT/path?query#anchor`, never the proxy URL. Translation between proxy URL and display URL is a base-URL swap preserving path, query, and anchor.

## Consequences

**Good:**
- Navigation controls work reliably across all content types
- WebSocket connection survives user navigation
- Clean separation: shell handles navigation, inject script handles console/DOM queries
- Works for non-HTML content (images, JSON) since shell's `onload` fires regardless

**Bad:**
- Extra iframe nesting (shell → inner iframe)
- Slightly more complex architecture
- Shell page must be served for every preview port
