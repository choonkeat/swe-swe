# visualViewport Keyboard: Prototype Learnings

**Date**: 2026-01-10
**Branch**: mobile-kb-and-scrolling

## Problem Statement

Mobile virtual keyboards cause chaos for xterm.js terminal:
- Keyboard slides up and covers terminal content
- User can't see what they're typing
- Layout shifts and flickers during show/hide
- `position: fixed` elements misbehave on iOS Safari

## Hypothesis

Use the `visualViewport` API to:
1. Detect keyboard height by comparing viewport to original window height
2. Dynamically resize terminal container to fit above keyboard
3. Position input bar directly above keyboard

## Key Learnings

### 1. Store Original Window Height at Load Time

```javascript
// CRITICAL: Capture BEFORE keyboard can appear
const originalWindowHeight = window.innerHeight;
```

With `interactive-widget=resizes-content` meta tag, `window.innerHeight` shrinks when keyboard appears. You need the original value as reference.

### 2. Calculate Keyboard Height from visualViewport

```javascript
function updateViewport() {
    const vv = window.visualViewport;
    const keyboardHeight = Math.max(0, originalWindowHeight - vv.height);
    const keyboardVisible = keyboardHeight > 50; // threshold to filter noise
}
```

### 3. Absolute Positioning > Flex Layout

**BROKEN**: Flex layout with CSS variable on container height
```css
.container {
    height: calc(100vh - var(--kb-height, 0px));
    transition: height 0.15s ease-out; /* causes black gaps! */
}
.terminal-wrapper {
    flex: 1;
}
```

**WORKS**: Absolute positioning with explicit bottom
```css
.terminal-wrapper {
    position: absolute;
    top: 72px;
    left: 0;
    right: 0;
    bottom: 60px; /* JS updates this directly */
}
```

### 4. No CSS Transitions on Layout

CSS transitions on height/bottom cause the terminal to show black gaps during resize because xterm.js `fitAddon.fit()` is called while the container is mid-transition.

**Solution**: Remove transitions, use `requestAnimationFrame` for immediate refit.

```javascript
// Don't use setTimeout - use rAF for immediate layout
requestAnimationFrame(() => {
    fitAddon.fit();
    sendResize();
    term.scrollToBottom();
});
```

### 5. Prevent iOS Focus Scroll

iOS Safari aggressively scrolls the page when an input is focused. Counteract with:

```javascript
cmdInput.addEventListener('focus', () => {
    setTimeout(() => {
        window.scrollTo(0, 0);
        updateViewport();
    }, 100);
});
```

### 6. Input Bar Positioning

Position input bar at `bottom: keyboardHeight` so it sits directly above the keyboard:

```javascript
inputBar.style.bottom = `${keyboardHeight}px`;
```

The terminal wrapper's bottom should be `keyboardHeight + inputBarHeight`:

```javascript
const wrapperBottom = keyboardVisible ? keyboardHeight + 60 : 60;
terminalWrapper.style.bottom = `${wrapperBottom}px`;
```

## Iteration History

| Version | Change | Result |
|---------|--------|--------|
| v1-v5 | CSS variable `--kb-height` on container | Black gaps, wrong sizing |
| v6-v8 | Flex layout with transitions | Terminal didn't fill space |
| v9 | Increased buffer (200px) | Too much padding |
| v10 | Reduced buffer (130px), longer setTimeout | Still black gaps |
| v11 | Absolute positioning, no transitions, rAF | ✅ Works on iOS Chrome |

## Verified Results

| Feature | iOS Chrome | iOS Safari |
|---------|------------|------------|
| Keyboard height detection | ✅ Works | ⚠️ Quirky |
| Input bar positioning | ✅ Works | ⚠️ Quirky |
| Terminal resize | ✅ Works | ⚠️ Quirky |
| Keyboard dismiss restore | ✅ Immediate | ⚠️ Delayed |
| Touch scroll proxy | ✅ Works | ✅ Works |
| Momentum scrolling | ✅ Works | ✅ Works |

**Note**: iOS Safari is not our target due to WebSocket issues.

### iOS Safari Keyboard Dismiss Lag

On iOS Safari, when the keyboard is dismissed, there's a noticeable lag before the terminal height and input field position restore. It appears Safari's `visualViewport` resize event fires only after the keyboard dismiss animation completes, not during it.

iOS Chrome fires the resize events during the animation, allowing smooth restoration.

This is another reason iOS Chrome is the preferred mobile browser for this use case.

## What We Sacrificed

Nothing significant - this approach improves on the default behavior.

## Conclusion

**This technique works and is our chosen approach for mobile keyboard handling.**

The visualViewport API combined with:
- Absolute positioning
- No CSS transitions
- `requestAnimationFrame` for refit timing
- `originalWindowHeight` reference

...provides reliable keyboard handling on iOS Chrome.

## Files

```
prototypes/visualviewport-keyboard/
├── main.go          # Websocket + PTY server (same as touch-scroll-proxy)
├── index.html       # xterm.js + visualViewport + touch scroll proxy
├── Dockerfile       # Alpine container
├── go.mod / go.sum  # Go dependencies
└── LEARNINGS.md     # This file
```

## Running the Prototype

```bash
cd prototypes/visualviewport-keyboard
docker build -t visualviewport-proto .
docker run -it --rm -p 8080:8080 visualviewport-proto
# Open http://<host-ip>:8080 on mobile
```
