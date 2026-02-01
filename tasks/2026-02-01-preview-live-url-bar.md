# Preview Tab: Live URL Bar Updates

**Date**: 2026-02-01
**Status**: Pending
**Source**: research/2026-02-01-preview-tab-enhancements.md (Section 4)

## Goal

Keep the preview toolbar URL input in sync with in-iframe navigation, including SPA routing (pushState/replaceState) and hash changes.

## Current problems

1. `updateIframeUrlDisplay()` is disabled for preview tab (line ~3155: `if (this.activeTab === 'preview') return;`)
2. Only fires on full page `load` events -- misses SPA navigations
3. Shows proxy URL (`https://host:53007/about`) instead of logical URL (`http://localhost:3007/about`)

## Implementation

### 1. URL change reporting in debugInjectJS (main.go)

Add ~12 lines to the injected JS:

```js
var lastUrl = location.href;
function checkUrl() {
  if (location.href !== lastUrl) {
    lastUrl = location.href;
    send({ t: 'urlchange', url: location.href });
  }
}
window.addEventListener('popstate', function() { checkUrl(); });
window.addEventListener('hashchange', function() { checkUrl(); });
var origPush = history.pushState;
var origReplace = history.replaceState;
history.pushState = function() { origPush.apply(this, arguments); checkUrl(); };
history.replaceState = function() { origReplace.apply(this, arguments); checkUrl(); };
```

### 2. URL display handler in terminal-ui.js

- Listen for `urlchange` messages on the debug WebSocket
- Reverse-map proxy URL to logical `localhost:{PORT}` URL (simple base-URL swap using pathname + search + hash)
- Update the URL input field
- Remove or fix the early return at line ~3155

## URL reverse-mapping

Proxy URL: `https://host:53007/dashboard?tab=settings#section`
Display URL: `http://localhost:3007/dashboard?tab=settings#section`

Swap the base URL, keep path + query + hash.

## Files to change

| File | Change |
|------|--------|
| `cmd/swe-swe/templates/host/swe-swe-server/main.go` | Add URL change detection to `debugInjectJS` |
| `cmd/swe-swe/templates/host/swe-swe-server/static/terminal-ui.js` | Handle `urlchange` messages, reverse-map URL, remove early return |

## Verification

1. `make run` per docs/dev/swe-swe-server-workflow.md
2. Open preview tab with a multi-page or SPA app
3. Click links -- URL bar should update in real time
4. Use browser back/forward -- URL bar should update
5. Verify displayed URL shows `localhost:{PORT}` not proxy URL
6. Test hash-only changes (`#section`)
