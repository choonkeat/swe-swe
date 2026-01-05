# Terminal UI Improvements

## Goal

Make three UI improvements to the terminal interface:

1. **Dimmer unfocused status bar** — More pronounced visual feedback when terminal loses focus
2. **Conditional mobile keyboard** — Show keyboard only on touch+narrow devices OR when `keyboard=show` query param is present
3. **Status messages as chat overlay** — Move temporary status messages from bottom bar to top-right translucent overlay using existing chat infrastructure

## File to Modify

- `/workspace/cmd/swe-swe/templates/host/swe-swe-server/static/terminal-ui.js`

---

## Phase 1: Dimmer Unfocused Status Bar

### What Will Be Achieved

When the terminal loses focus, the status bar will become more visually muted — using both reduced opacity (0.4) and grayscale filter (0.5) to clearly signal "inactive" state.

### Steps

1. **Locate the existing `.blurred` CSS rule** at line ~319-321
2. **Modify the rule** from:
   ```css
   .terminal-ui__status-bar.blurred {
       opacity: 0.6;
   }
   ```
   to:
   ```css
   .terminal-ui__status-bar.blurred {
       opacity: 0.4;
       filter: grayscale(0.5);
   }
   ```

### Verification

1. **Build and run test container**:
   ```bash
   ./scripts/01-test-container-init.sh
   ./scripts/02-test-container-build.sh
   HOST_PORT=9899 HOST_IP=host.docker.internal ./scripts/03-test-container-run.sh
   ```

2. **Use MCP browser to test**:
   - Navigate to `http://host.docker.internal:9899/`
   - Take screenshot of status bar in focused state
   - Click outside terminal area (or use JS to blur the textarea)
   - Take screenshot of status bar in blurred state
   - Verify: opacity is noticeably lower + colors are desaturated

3. **No regression**: Verify status bar still:
   - Shows correct connection state (blue when connected)
   - Displays user/agent info
   - Links are still readable (though dimmed)

---

## Phase 2: Conditional Mobile Keyboard Visibility

### What Will Be Achieved

The mobile keyboard will be hidden by default and only shown when:
- **Hybrid mobile detection**: Device has touch capability AND viewport is narrow (≤768px), OR
- **Query param override**: URL contains `keyboard=show`

This allows desktop users to have a cleaner UI while mobile users still get the keyboard, and anyone can force-show it via query string.

### Steps

1. **Add CSS to hide keyboard by default** — modify existing `.mobile-keyboard` rule:
   ```css
   .mobile-keyboard {
       flex-shrink: 0;
       display: none;  /* Hidden by default */
       flex-direction: column;
       background: #2d2d2d;
       border-top: 1px solid #404040;
   }
   .mobile-keyboard.visible {
       display: flex;
   }
   ```

2. **Add JS detection method**:
   ```javascript
   setupKeyboardVisibility() {
       const keyboard = this.querySelector('.mobile-keyboard');
       if (!keyboard) return;

       const hasTouch = 'ontouchstart' in window || navigator.maxTouchPoints > 0;
       const isNarrow = window.matchMedia('(max-width: 768px)').matches;
       const forceShow = new URLSearchParams(location.search).get('keyboard') === 'show';

       if ((hasTouch && isNarrow) || forceShow) {
           keyboard.classList.add('visible');
       }
   }
   ```

3. **Call `setupKeyboardVisibility()` during initialization** — in `connectedCallback()` after DOM is ready

### Verification

1. **Build and run test container** (reuse from Phase 1)

2. **Test default state (desktop browser)**:
   - Navigate to `http://host.docker.internal:9899/`
   - MCP browser is non-touch + wide viewport
   - Verify: mobile keyboard is NOT visible

3. **Test `keyboard=show` override**:
   - Navigate to `http://host.docker.internal:9899/?keyboard=show`
   - Verify: mobile keyboard IS visible
   - Verify: keyboard buttons work (Esc, Tab, Ctrl combos, text input)

4. **No regression**: When keyboard is visible:
   - Esc/Tab/Ctrl buttons send correct codes
   - Text input + Enter works
   - File attachment button works

---

## Phase 3: Status Notifications via Chat Overlay

### What Will Be Achieved

Temporary status messages (file uploads, chunk received, etc.) will display as translucent toast-style notifications in the top-right corner using the existing chat overlay infrastructure, instead of replacing the status bar text.

### Steps

1. **Add CSS for `.system` message style** (after existing `.terminal-ui__chat-message` styles):
   ```css
   .terminal-ui__chat-message.system {
       background: rgba(60, 60, 60, 0.85);
       font-style: italic;
   }
   ```

2. **Add `showStatusNotification(message)` method** (near `addChatMessage`):
   ```javascript
   showStatusNotification(message, durationMs = 3000) {
       const overlay = this.querySelector('.terminal-ui__chat-overlay');
       if (!overlay) return;

       const msgEl = document.createElement('div');
       msgEl.className = 'terminal-ui__chat-message system';
       msgEl.textContent = message;

       overlay.appendChild(msgEl);

       // Auto-fade after duration
       setTimeout(() => {
           msgEl.classList.add('fading');
           setTimeout(() => msgEl.remove(), 400);
       }, durationMs);
   }
   ```

3. **Find and replace all calls to `showTemporaryStatus()`**:
   - Search for `showTemporaryStatus` usages
   - Replace each call with `showStatusNotification()`

4. **Remove or deprecate `showTemporaryStatus()` method**:
   - If no other usages remain, remove the method entirely
   - Alternatively, have it delegate to `showStatusNotification()` for backwards compatibility

### Verification

1. **Build and run test container** (reuse from previous phases)

2. **Test file upload notification**:
   - Navigate to `http://host.docker.internal:9899/`
   - Trigger a file upload via attach button or drag-drop
   - Verify: notification appears in top-right overlay (not status bar)
   - Verify: notification fades after ~3 seconds
   - Verify: status bar continues showing connection info unchanged

3. **Test multiple notifications**:
   - Trigger several status events in quick succession
   - Verify: they stack in the overlay
   - Verify: each fades independently

4. **No regression**:
   - Chat messages still work (`addChatMessage` via console)
   - Chat messages use `.own`/`.other` styling (blue/gray)
   - Status notifications use `.system` styling (neutral gray, italic)
   - Status bar connection state (connected/error/reconnecting) still works correctly

---

## Teardown

After all testing is complete:
```bash
./scripts/04-test-container-down.sh
```
