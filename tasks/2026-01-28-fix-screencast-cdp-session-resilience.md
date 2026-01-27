# Fix Screencast CDP Session Resilience

**Date**: 2026-01-28
**Status**: Pending

## Problem

The Chrome screencast server's CDP session becomes stale when MCP Playwright connects separately. The server doesn't detect this or recover - it silently stops receiving `Page.screencastFrame` events, causing the Agent View to show "Connected, waiting for frames..." indefinitely.

### Root Cause

1. Chrome container starts, screencast server connects via `chromium.connectOverCDP()`
2. Screencast server creates a CDP session and calls `Page.startScreencast`
3. MCP Playwright connects separately (also via CDP) and navigates/manipulates the page
4. The screencast server's CDP session stops receiving frame events (reason unclear - possibly session conflict or page target change)
5. Server has no detection or recovery mechanism

### Affected File

`cmd/swe-swe/templates/host/chrome-screencast/server.js`

## Proposed Fix

### Option A: Frame Timeout Detection (Recommended)

Add a watchdog that detects when frames stop arriving and re-establishes the CDP session.

```javascript
// Add to server.js

let lastFrameTime = Date.now();
const FRAME_TIMEOUT_MS = 10000; // 10 seconds without frames = stale

// In Page.screencastFrame handler:
cdpSession.on('Page.screencastFrame', async (params) => {
  lastFrameTime = Date.now();  // Track last frame time
  // ... existing code
});

// Add watchdog interval
setInterval(async () => {
  if (Date.now() - lastFrameTime > FRAME_TIMEOUT_MS && clients.size > 0) {
    console.log('Frame timeout detected, re-establishing CDP session...');
    await reconnectScreencast();
  }
}, 5000);

async function reconnectScreencast() {
  try {
    // Stop existing screencast
    if (cdpSession) {
      try {
        await cdpSession.send('Page.stopScreencast');
        await cdpSession.detach();
      } catch (err) {
        // Ignore - session may already be invalid
      }
    }

    // Get current page (may have changed)
    const contexts = browser.contexts();
    const context = contexts[0];
    const pages = context.pages();
    page = pages[0];

    // Create new CDP session
    cdpSession = await context.newCDPSession(page);
    await startScreencast();

    console.log('CDP session re-established');
  } catch (err) {
    console.error('Failed to reconnect:', err.message);
  }
}
```

### Option B: Listen for Page Events

React to page lifecycle events to re-attach screencast:

```javascript
// Listen for navigation/page changes
page.on('framenavigated', async (frame) => {
  if (frame === page.mainFrame()) {
    console.log('Main frame navigated, restarting screencast...');
    await reconnectScreencast();
  }
});
```

### Option C: Single CDP Connection Architecture

Redesign so MCP Playwright and screencast share the same CDP connection. This is more complex but eliminates the conflict.

## Implementation Steps

1. [ ] Read current `server.js` implementation
2. [ ] Add `lastFrameTime` tracking in frame handler
3. [ ] Add `reconnectScreencast()` function
4. [ ] Add watchdog interval (check every 5s, timeout after 10s)
5. [ ] Add logging for reconnection events
6. [ ] Test by:
   - Start container
   - Connect Agent View (should see frames)
   - Use MCP browser to navigate
   - Verify frames continue (or recover within 10s)
7. [ ] Update all template variants (golden files)
8. [ ] Run `make build golden-update`

## Testing

```bash
# Start test container
docker compose up -d

# Watch screencast logs
docker logs -f <chrome-container>

# In another terminal, use MCP browser to navigate
# Verify "Frame timeout detected" and "CDP session re-established" appear
# Verify Agent View recovers and shows frames
```

## Notes

- The 10-second timeout is conservative; could be reduced to 5s for faster recovery
- Only trigger reconnect when clients are connected (no point if no one is watching)
- Log reconnection events for debugging
