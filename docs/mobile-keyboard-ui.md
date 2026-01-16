# Mobile Keyboard UI (Reference)

This document preserves the polling-mode keyboard UI code for potential future use with mobile WebSocket connections.

## Overview

The polling fallback feature included a specialized mobile keyboard UI with:
- Quick-action buttons (Ctrl+C, Ctrl+D, Tab, arrows, Enter)
- Text input bar with Send button
- Brown "Slow connection mode" status bar theme

This UI was designed for mobile devices where:
1. On-screen keyboards don't easily send control characters
2. Touch input benefits from large, tappable buttons

## HTML Structure

```html
<!-- Quick-action buttons for common terminal controls -->
<div class="terminal-ui__polling-actions">
    <button data-send="\x03">Ctrl+C</button>
    <button data-send="\x04">Ctrl+D</button>
    <button data-send="\t">Tab</button>
    <button data-send="\x1b[A">↑</button>
    <button data-send="\x1b[B">↓</button>
    <button data-send="\r">Enter</button>
</div>

<!-- Text input bar for typing commands -->
<div class="terminal-ui__polling-input">
    <input type="text" placeholder="Type command..." class="terminal-ui__polling-command">
    <button class="terminal-ui__polling-send">Send</button>
</div>
```

## CSS Styles

```css
/* Status bar styling for slow connection mode */
.terminal-ui__status-bar.polling-mode {
    background: #795548;
}

/* Text input bar */
.terminal-ui__polling-input {
    display: none;
    padding: 8px;
    background: #2d2d2d;
    border-top: 1px solid #404040;
    gap: 8px;
}
.terminal-ui__polling-input.visible {
    display: flex;
}
.terminal-ui__polling-input input {
    flex: 1;
    padding: 10px 12px;
    font-size: 14px;
    font-family: monospace;
    background: #1e1e1e;
    color: #d4d4d4;
    border: 1px solid #505050;
    border-radius: 4px;
    outline: none;
}
.terminal-ui__polling-input input:focus {
    border-color: #007acc;
}
.terminal-ui__polling-input button {
    padding: 10px 16px;
    font-size: 14px;
    font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif;
    background: #007acc;
    color: #fff;
    border: none;
    border-radius: 4px;
    cursor: pointer;
}
.terminal-ui__polling-input button:hover {
    background: #005a9e;
}

/* Quick-action buttons */
.terminal-ui__polling-actions {
    display: none;
    padding: 8px;
    background: #2d2d2d;
    gap: 4px;
    flex-wrap: wrap;
}
.terminal-ui__polling-actions.visible {
    display: flex;
}
.terminal-ui__polling-actions button {
    flex: 1;
    min-width: 50px;
    padding: 10px 8px;
    font-size: 13px;
    font-family: monospace;
    background: #3c3c3c;
    color: #d4d4d4;
    border: 1px solid #505050;
    border-radius: 4px;
    cursor: pointer;
}
.terminal-ui__polling-actions button:hover {
    background: #505050;
}
.terminal-ui__polling-actions button:active {
    background: #007acc;
}
```

## JavaScript Event Handlers

```javascript
// Toggle mobile keyboard UI visibility
setPollingMode(enabled) {
    this.isPollingMode = enabled;
    const statusBar = this.querySelector('.terminal-ui__status-bar');
    const pollingInput = this.querySelector('.terminal-ui__polling-input');
    const pollingActions = this.querySelector('.terminal-ui__polling-actions');
    const extraKeys = this.querySelector('.terminal-ui__extra-keys');

    if (enabled) {
        statusBar.classList.add('polling-mode');
        pollingInput.classList.add('visible');
        pollingActions.classList.add('visible');
        // Hide extra keys on mobile when in polling mode (we have polling actions instead)
        extraKeys.style.display = 'none';
    } else {
        statusBar.classList.remove('polling-mode');
        pollingInput.classList.remove('visible');
        pollingActions.classList.remove('visible');
        extraKeys.style.display = '';
    }
}

// Send command from text input
sendPollingCommand() {
    const input = this.querySelector('.terminal-ui__polling-command');
    if (!input) return;

    const text = input.value;
    if (!text) return;

    if (this.transport && this.transport.isConnected()) {
        // Send command + carriage return to execute (terminals expect \r for Enter)
        this.transport.send(text + '\r');
        // Clear input
        input.value = '';
        input.focus();
    }
}

// Event listener setup for input bar
const pollingInput = this.querySelector('.terminal-ui__polling-command');
const pollingSendBtn = this.querySelector('.terminal-ui__polling-send');

if (pollingInput) {
    pollingInput.addEventListener('keydown', (e) => {
        if (e.key === 'Enter') {
            e.preventDefault();
            this.sendPollingCommand();
        }
    });
}

if (pollingSendBtn) {
    pollingSendBtn.addEventListener('click', () => {
        this.sendPollingCommand();
    });
}

// Quick-action button handlers
this.querySelectorAll('.terminal-ui__polling-actions button').forEach(btn => {
    btn.addEventListener('click', (e) => {
        e.preventDefault();
        const sendData = btn.dataset.send;
        if (sendData && this.transport && this.transport.isConnected()) {
            // Unescape the data string (e.g., "\x03" -> actual Ctrl+C character)
            const unescaped = sendData
                .replace(/\\x([0-9a-fA-F]{2})/g, (_, hex) => String.fromCharCode(parseInt(hex, 16)))
                .replace(/\\t/g, '\t')
                .replace(/\\n/g, '\n')
                .replace(/\\r/g, '\r');
            this.transport.send(unescaped);
        }
    });
});
```

## Escape Sequence Reference

The `data-send` attribute uses escape sequences:
- `\x03` - Ctrl+C (ETX, interrupt)
- `\x04` - Ctrl+D (EOT, end of transmission)
- `\t` - Tab character
- `\x1b[A` - Arrow Up (ANSI escape)
- `\x1b[B` - Arrow Down (ANSI escape)
- `\r` - Carriage Return (Enter)

## Future Implementation Notes

To re-implement for mobile WebSocket:
1. Detect mobile devices via user agent or touch capability
2. Show the keyboard UI conditionally (not tied to polling mode)
3. Wire up the event handlers to send via WebSocket transport
4. Consider adding more buttons (Ctrl+A, Ctrl+E, Ctrl+L, etc.)
