# Mobile & Touch Device Support

This document describes the mobile-optimized UI features for touch devices (iOS, Android, tablets).

## Overview

The terminal UI automatically detects touch devices and enables:
- Mobile keyboard bar with control keys, navigation, and text input
- Touch scroll proxy for native iOS momentum scrolling
- Visual viewport handling for on-screen keyboard
- iOS Safari-specific workarounds

## Touch Detection

Touch devices are detected via:

```javascript
const hasTouch = 'ontouchstart' in window || navigator.maxTouchPoints > 0;
```

When detected, the mobile keyboard is shown and touch scrolling is enabled.

**Code reference:** `static/terminal-ui.js` (setupMobileKeyboard method)

## Mobile Keyboard Bar

The mobile keyboard provides touch-friendly controls that are difficult to type on virtual keyboards.

### Layout

```
┌────────────────────────────────────────────────────────────┐
│ [Tab] [Esc] [Ctrl] [Nav]                                   │  Main row
├────────────────────────────────────────────────────────────┤
│ [C] [D] [Z] [L] [R]                                        │  Ctrl row (toggleable)
│  ^C  ^D  ^Z  ^L  ^R                                        │
├────────────────────────────────────────────────────────────┤
│ [←] [→] [↑] [↓] [Home] [End]                               │  Nav row (toggleable)
├────────────────────────────────────────────────────────────┤
│ [📎] [Type command...                        ] [Enter]     │  Input bar
└────────────────────────────────────────────────────────────┘
```

### Main Row Buttons

| Button | Action | Description |
|--------|--------|-------------|
| Tab | `\t` | Tab completion |
| Esc | `\x1b` | Escape key |
| Ctrl | Toggle | Shows/hides Ctrl key row |
| Nav | Toggle | Shows/hides navigation row |

### Ctrl Row (Toggle)

Sends Ctrl+key combinations:

| Button | Sequence | Description |
|--------|----------|-------------|
| A | `\x01` | Ctrl+A - Beginning of line |
| C | `\x03` | Ctrl+C - Interrupt |
| D | `\x04` | Ctrl+D - EOF |
| E | `\x05` | Ctrl+E - End of line |
| K | `\x0b` | Ctrl+K - Kill to end of line |
| O | `\x0f` | Ctrl+O - Open |
| W | `\x17` | Ctrl+W - Delete word backward |

### Nav Row (Toggle)

Sends ANSI escape sequences for cursor movement:

| Button | Sequence | Description |
|--------|----------|-------------|
| ← | `\x1b[D` | Cursor left |
| → | `\x1b[C` | Cursor right |
| ↑ | `\x1b[A` | History previous |
| ↓ | `\x1b[B` | History next |
| Home | `\x1b[H` | Line start |
| End | `\x1b[F` | Line end |

### Input Bar

- **Attach button (📎)**: Opens file picker for uploads
- **Text input**: Multi-line textarea for typing commands
- **Enter button**: Sends text + carriage return to terminal

Text input features:
- `autocomplete="off"` - No form autofill suggestions
- Autocapitalize and autocorrect enabled for mobile usability

**Code reference:** `static/terminal-ui.js` (setupMobileKeyboardInput method)

## Touch Scroll Proxy

iOS Safari doesn't support smooth scrolling in xterm.js. A transparent overlay provides native momentum scrolling.

### How It Works

1. A `<div class="touch-scroll-proxy">` overlays the terminal
2. Contains a tall inner div to create scrollable area
3. On touch devices, xterm gets `pointer-events: none`
4. Scroll events on proxy are translated to terminal scroll

```css
.touch-scroll-proxy {
    position: absolute;
    inset: 0;
    overflow-y: scroll;
    -webkit-overflow-scrolling: touch;  /* iOS momentum */
}

@media (pointer: coarse) {
    .touch-scroll-proxy {
        pointer-events: auto;
    }
    .xterm {
        pointer-events: none;
    }
}
```

### Scroll Translation

Scroll position changes are converted to terminal scroll lines:

```javascript
const scrollDelta = newScrollTop - this.lastScrollTop;
const linesToScroll = Math.round(scrollDelta / this.lineHeight);
this.term.scrollLines(linesToScroll);
```

**Code reference:** `static/terminal-ui.js` (setupTouchScrollProxy method)

## Visual Viewport Keyboard Handling

When the on-screen keyboard appears, the mobile keyboard bar moves above it.

### Detection

Uses `visualViewport` API to detect keyboard:

```javascript
window.visualViewport.addEventListener('resize', () => {
    const keyboardHeight = window.innerHeight - window.visualViewport.height;
    if (keyboardHeight > 100) {
        // Keyboard visible
        mobileKeyboard.style.marginBottom = `${keyboardHeight}px`;
    } else {
        // Keyboard hidden
        mobileKeyboard.style.marginBottom = '0';
    }
});
```

**Code reference:** `static/terminal-ui.js` (setupVisualViewportHandler method)

## iOS Safari Specifics

### WebSocket Connection Delay

iOS Safari needs a brief delay before WebSocket connections:

```javascript
// iOS Safari needs a brief delay before WebSocket connection
const isIOS = /iPad|iPhone|iPod/.test(navigator.userAgent);
if (isIOS) {
    setTimeout(() => this.connect(), 100);
} else {
    this.connect();
}
```

### Self-Signed Certificate Issues

iOS Safari silently fails WebSocket connections to self-signed certs. The UI detects this and shows a warning:

```javascript
// WebSocket stuck in CONNECTING state = iOS Safari self-signed cert issue
setTimeout(() => {
    if (this.ws.readyState === WebSocket.CONNECTING) {
        this.updateStatus('error', 'iOS Safari: WebSocket blocked (self-signed cert)');
    }
}, 5000);
```

**Workaround**: Use Let's Encrypt certificates or a different browser on iOS.

### Chunked Snapshots

Screen snapshots are sent in chunks for iOS Safari compatibility (large binary messages can fail):

```go
// Chunk format: [0x02, chunk_index, total_chunks, ...data]
func sendChunked(conn *websocket.Conn, data []byte, chunkSize int) {
    totalChunks := (len(data) + chunkSize - 1) / chunkSize
    for i := 0; i < totalChunks; i++ {
        chunk := []byte{0x02, byte(i), byte(totalChunks)}
        chunk = append(chunk, data[start:end]...)
        conn.WriteMessage(websocket.BinaryMessage, chunk)
    }
}
```

## Status Bar Touch Interaction

The status bar supports touch interactions:

| Element | Tap Action |
|---------|------------|
| "Connected" / "YOLO" text | Toggle YOLO mode (if supported by agent) |
| Session name | Open rename dialog |
| Settings icon | Open settings panel |

**Code reference:** `static/terminal-ui.js` (status bar click handlers)

## CSS Media Queries

Touch-specific styles use `pointer: coarse` media query:

```css
@media (pointer: coarse) {
    /* Touch device styles */
    .touch-scroll-proxy { pointer-events: auto; }
    .xterm { pointer-events: none; }
}

@media (pointer: fine) {
    /* Mouse/trackpad styles */
    .touch-scroll-proxy { pointer-events: none; }
}
```

## File Uploads on Mobile

The attach button (📎) opens the native file picker:

```html
<input type="file" class="mobile-keyboard__file-input" multiple hidden>
```

Files are uploaded via WebSocket binary message (0x01 prefix). See `websocket-protocol.md` for details.

## Known Limitations

1. **iOS Safari + Self-Signed Certs**: WebSocket connections fail silently. Use proper TLS or different browser.
2. **Virtual Keyboard Detection**: Relies on `visualViewport` API which may not be available on older browsers.
3. **Scroll Accuracy**: Touch scroll proxy approximates scroll position; may drift on very long outputs.

## Related Documentation

- [WebSocket Protocol](websocket-protocol.md) - Message format for terminal I/O and file uploads
- [Connection Lifecycle](connection-lifecycle.md) - Connection states and reconnection behavior
