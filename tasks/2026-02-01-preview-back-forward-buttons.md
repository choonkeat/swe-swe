# Preview Toolbar: Back/Forward Buttons

**Date**: 2026-02-01
**Status**: Complete
**Source**: research/2026-02-01-preview-tab-enhancements.md (Section 1)

## Goal

Add Back and Forward navigation buttons to the preview tab toolbar.

## Current toolbar layout

```
Home | Refresh | [URL input] | Go | Open External
```

## Proposed toolbar layout

```
Home | Back | Forward | Refresh | [URL input] | Go | Open External
```

## Implementation

Use `iframe.contentWindow.history.back()` and `.forward()` from the parent page. The preview iframe is same-origin (served through the `5{PORT}` proxy), so this is allowed.

### Caveats

- History API does not expose whether there are entries to go back/forward to. `history.length` counts total entries, not position. No `history.canGoBack`.
- Buttons will always be enabled and silently no-op if there is nothing to navigate to.
- SPA `pushState`/`replaceState` navigations work as expected with back/forward.

## Files to change

| File | Change |
|------|--------|
| `cmd/swe-swe/templates/host/swe-swe-server/static/terminal-ui.js` | Add Back/Forward buttons to preview toolbar (~line 349) |

## Verification

1. `make run` per docs/dev/swe-swe-server-workflow.md
2. Open preview tab, navigate to a multi-page app
3. Click links to build history, then verify Back/Forward buttons navigate correctly
