# Troubleshooting

Common problems and fixes for the swe-swe terminal UI.

## File drop overlay stuck

**Symptom**: The blue "Drop file to paste contents" overlay stays visible after a drag is cancelled or the file is dropped outside the target area.

**Why it happens**: Browsers don't always fire matching `dragleave` events — for example, when a drag exits the browser window entirely, or is cancelled from an external app. The internal counter that tracks enter/leave pairs gets out of sync, leaving the overlay visible.

**Fix**: Click anywhere on the overlay to dismiss it, or press **Escape**.

## File upload spinner stuck

**Symptom**: The dark upload overlay with spinner stays visible after uploading a file.

**Why it happens**: The WebSocket connection may have dropped mid-upload, or the server didn't acknowledge the upload in time.

**Fix**: Refresh the page. The terminal session is preserved — you'll reconnect automatically.

## Paste not working in terminal

**Symptom**: Ctrl+V / Cmd+V doesn't paste text into the terminal.

**Why it happens**: The browser's clipboard API requires focus to be inside the terminal component. Clicking on the status bar or other UI elements moves focus away.

**Fix**: Click inside the terminal area first, then paste. On mobile, use the paste button in the on-screen keyboard toolbar.

## Terminal not connecting

**Symptom**: The terminal shows a "Connecting..." or "Disconnected" status and never recovers.

**Why it happens**: The WebSocket connection to the container may have been interrupted (network change, container restart, etc.).

**Fix**: Refresh the page. If the problem persists, check that the container is still running with `docker ps`.

## Blank or white terminal

**Symptom**: The terminal area is completely blank/white after loading.

**Why it happens**: The xterm.js terminal may have failed to initialize, often due to a browser extension interfering with the page.

**Fix**: Try disabling browser extensions, or open the session in an incognito/private window.
