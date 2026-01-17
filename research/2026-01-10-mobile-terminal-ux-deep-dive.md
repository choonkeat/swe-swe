# Mobile Terminal UX Deep Dive: Touch Scrolling & Keyboard Issues

## Executive Summary

Mobile web terminal UX with xterm.js has two fundamental pain points:

1. **Touch scrolling conflict**: Touch gestures fight between terminal text selection, terminal scrollback scrolling, and page scrolling
2. **Virtual keyboard viewport chaos**: Keyboard show/hide causes layout shifts, covers content, and breaks fixed positioning

The xterm.js maintainers have explicitly stated mobile support is "non-existent" and "not a priority" ([#5377](https://github.com/xtermjs/xterm.js/issues/5377)). Solutions require custom implementation.

---

## Problem 1: Touch Scrolling Conflicts

### Root Cause

xterm.js renders terminal content in a canvas with a scrollable viewport div underneath. Touch events trigger multiple competing behaviors:

| Touch Action | What User Wants | What Happens |
|--------------|-----------------|--------------|
| Swipe up/down | Scroll terminal history | Sometimes scrolls page, sometimes selects text |
| Swipe in app mode (vim, less) | Navigate (arrow keys) | Terminal scrollback scrolls instead |
| Tap and drag | Select text | May scroll instead |
| Two-finger scroll | Scroll terminal | High-frequency wheel events in mouse mode |

### xterm.js Limitations

From [Issue #5377](https://github.com/xtermjs/xterm.js/issues/5377):
- No dedicated touch event handling - relies on browser mouse event translation
- No native touch gesture recognition
- Text selection is "cumbersome" on touch devices
- Maintainers: "feel free to contribute improvements... but this is definitely not a priority"

From [Issue #594](https://github.com/xtermjs/xterm.js/issues/594):
- Ballistic/momentum scrolling not supported "because of how the viewport is actually underneath the row divs"

From [Issue #1007](https://github.com/xtermjs/xterm.js/issues/1007):
- Touch scrolling should send arrow keys in application mode, but doesn't

### Current swe-swe Implementation

```javascript
// terminal-ui.js - No explicit touch handling
// Relies entirely on xterm.js internal behavior
// Only click handlers on mobile keyboard buttons
// touch-action: manipulation on buttons (prevents double-tap zoom)
```

### Potential Solutions

#### Solution A: Swipe Region Detection

From community workaround in [Issue #1007](https://github.com/xtermjs/xterm.js/issues/1007):

```javascript
// Divide terminal into swipe regions
// - Bottom edge: left/right arrows
// - Left edge: up/down arrows
// - Main area: scroll in normal mode, arrows in app mode

terminal.addEventListener('touchmove', (e) => {
    const rect = terminal.getBoundingClientRect();
    const touch = e.touches[0];
    const region = detectRegion(touch, rect);

    if (isAppMode) {
        e.preventDefault();
        sendArrowKey(swipeDirection);
    }
});
```

**Limitation**: Detecting "app mode" requires parsing incoming escape sequences for mouse mode activation.

#### Solution B: Gesture Disambiguation

Implement touch gesture detection with thresholds:

```javascript
// Distinguish between:
// - Tap (< 200ms, < 10px movement) → focus terminal
// - Swipe (> 10px movement in one direction) → scroll or arrows
// - Long press (> 500ms) → text selection mode
// - Two-finger pinch → zoom (or disable)
```

#### Solution C: Dedicated Scroll Buttons

Add explicit scroll controls to mobile keyboard:

```
┌──────┐┌──────┐┌──────┐┌──────┐
│ PgUp ││ PgDn ││ Home ││ End  │
└──────┘└──────┘└──────┘└──────┘
```

This avoids touch gesture conflicts entirely by using explicit controls.

#### Solution D: Lock Scrolling in App Mode

Detect when terminal is in alternate screen buffer (vim, less, etc.) and disable touch scrolling:

```javascript
// xterm.js exposes buffer.active.type
// 'alternate' = vim/less mode, 'normal' = regular shell
const isAltBuffer = term.buffer.active.type === 'alternate';
if (isAltBuffer) {
    // Convert touch to arrow keys
} else {
    // Allow normal scrolling
}
```

---

## Problem 2: Virtual Keyboard Viewport Issues

### Root Cause

When the virtual keyboard appears on mobile:

| Platform | Behavior |
|----------|----------|
| **iOS Safari** | Layout viewport unchanged, visual viewport shrinks. Fixed elements stay behind keyboard. |
| **Android Chrome** | Layout viewport resizes (since Chrome 108, now matches iOS by default) |
| **iOS Safari quirk** | `position: fixed` elements behave like `position: static` with keyboard open |

### The Terminal Problem

For a terminal that fills the screen:
1. User taps input to type
2. Keyboard slides up (300-400px on phones)
3. Terminal content/prompt gets hidden behind keyboard
4. User can't see what they're typing
5. Layout recalculates, terminal resizes, causes flicker

### Current swe-swe Implementation

```html
<!-- index.html -->
<meta name="viewport" content="width=device-width, initial-scale=1.0, user-scalable=no">
```

```javascript
// terminal-ui.js - Keyboard visibility based on touch + narrow screen
const hasTouch = 'ontouchstart' in window || navigator.maxTouchPoints > 0;
const isNarrow = window.matchMedia('(max-width: 768px)').matches;

// Resize terminal after mobile keyboard toggle
requestAnimationFrame(() => {
    this.fitAddon.fit();
    this.sendResize();
    this.term.scrollToBottom();
});
```

No handling for native virtual keyboard appearance/disappearance.

### Solution Options

#### Solution 1: `interactive-widget` Meta Tag

```html
<meta name="viewport" content="width=device-width, initial-scale=1.0,
  interactive-widget=resizes-content">
```

| Value | Effect |
|-------|--------|
| `resizes-visual` | Only visual viewport shrinks (default) |
| `resizes-content` | Both viewports shrink - `dvh` units update |
| `overlays-content` | Keyboard overlays, nothing resizes |

**Browser Support**: Chrome 108+, Firefox 132+, **Safari NOT SUPPORTED**

#### Solution 2: Dynamic Viewport Height (dvh)

```css
.terminal-container {
    height: 100dvh; /* Dynamic - shrinks with keyboard */
}
```

Combined with `interactive-widget=resizes-content`, the terminal automatically resizes when keyboard appears.

**Caveat**: Safari support for `dvh` exists, but without `interactive-widget` it doesn't respond to keyboard.

#### Solution 3: VirtualKeyboard API

```javascript
if ('virtualKeyboard' in navigator) {
    navigator.virtualKeyboard.overlaysContent = true;
}
```

CSS environment variables become available:

```css
.terminal-container {
    height: calc(100vh - env(keyboard-inset-height, 0px));
}

.mobile-keyboard {
    bottom: env(keyboard-inset-height, 0px);
}
```

**Browser Support**: Chrome Android only. **Safari/Firefox NOT SUPPORTED.**

#### Solution 4: visualViewport API (Best Cross-Browser)

```javascript
function updateLayout() {
    const viewport = window.visualViewport;
    const keyboardHeight = window.innerHeight - viewport.height;

    document.documentElement.style.setProperty(
        '--keyboard-height',
        `${keyboardHeight}px`
    );

    // Refit terminal to new size
    fitAddon.fit();
    term.scrollToBottom();
}

visualViewport.addEventListener('resize', updateLayout);
visualViewport.addEventListener('scroll', updateLayout);
```

```css
.terminal-container {
    height: calc(100vh - var(--keyboard-height, 0px));
}
```

**Browser Support**: All modern browsers including Safari iOS.

#### Solution 5: iOS Safari Focus Scroll Prevention

iOS Safari aggressively scrolls/zooms to center focused inputs. Prevent with:

```css
@keyframes prevent-safari-focus-scroll {
    0% { opacity: 0; }
    100% { opacity: 1; }
}

.mobile-keyboard__text:focus {
    animation: prevent-safari-focus-scroll 0.01s;
}
```

This exploits Safari's behavior of not scrolling to invisible elements.

---

## Recommended Implementation Strategy

### Phase 1: Keyboard Viewport Handling

1. **Add visualViewport listener** to track keyboard height
2. **Set CSS variable** `--keyboard-height`
3. **Use `dvh` units** with fallback to calculated height
4. **Add `interactive-widget=resizes-content`** for Chrome/Firefox
5. **Refit terminal** on viewport resize
6. **Prevent iOS focus scroll** with opacity animation trick

### Phase 2: Touch Scroll Improvements

1. **Detect alternate buffer** (vim/less mode) via `term.buffer.active.type`
2. **Add swipe-to-arrow** in alternate buffer mode
3. **Add explicit scroll buttons** (PgUp, PgDn, Home, End) to mobile keyboard
4. **Consider touch-action CSS** to control gesture behavior per-region

### Phase 3: Advanced Touch Gestures (Future)

1. **Gesture disambiguation** with timing/distance thresholds
2. **Long-press for selection mode**
3. **Haptic feedback** for button presses
4. **Swipe regions** for different behaviors

---

## Key References

### xterm.js Issues
- [#5377 - Limited touch support on mobile devices](https://github.com/xtermjs/xterm.js/issues/5377) - Open, maintainers say "not a priority"
- [#1007 - Touch scrolling should send arrow keys](https://github.com/xtermjs/xterm.js/issues/1007) - Closed 2018
- [#594 - Support ballistic scrolling via touch](https://github.com/xtermjs/xterm.js/issues/594) - Acknowledged limitation

### Viewport & Keyboard APIs
- [HTMHell: interactive-widget meta tag](https://www.htmhell.dev/adventcalendar/2024/4/)
- [VirtualKeyboard API (MDN)](https://developer.mozilla.org/en-US/docs/Web/API/VirtualKeyboard_API)
- [VirtualKeyboard API article by Ahmad Shadeed](https://ishadeed.com/article/virtual-keyboard-api/)
- [Fix keyboard overlap with dvh](https://www.franciscomoretti.com/blog/fix-mobile-keyboard-overlap-with-visualviewport)

### iOS Safari Workarounds
- [Preventing iOS Safari scrolling on input focus](https://gist.github.com/kiding/72721a0553fa93198ae2bb6eefaa3299)
- [Safari Mobile Resizing Bug](https://medium.com/@krutilin.sergey.ks/fixing-the-safari-mobile-resizing-bug-a-developers-guide-6568f933cde0)
- [Safari position:fixed and virtual keyboard](https://medium.com/@im_rahul/safari-and-position-fixed-978122be5f29)

### UX Best Practices
- [Touch Gesture Controls for Mobile Interfaces (Smashing Magazine)](https://www.smashingmagazine.com/2017/02/touch-gesture-controls-mobile-interfaces/)
- [Designing for Touch: Mobile UI/UX Best Practices](https://devoq.medium.com/designing-for-touch-mobile-ui-ux-best-practices-c0c71aa615ee)

---

## Browser Support Matrix

| Feature | Chrome Android | Safari iOS | Firefox Android |
|---------|---------------|------------|-----------------|
| `visualViewport` API | Yes | Yes | Yes |
| `VirtualKeyboard` API | Yes | No | No |
| `interactive-widget` | 108+ | No | 132+ |
| `dvh` units | Yes | Yes | Yes |
| `env(keyboard-inset-*)` | Yes | No | No |
| Touch events | Yes | Yes | Yes |

**Conclusion**: `visualViewport` API is the most reliable cross-browser solution for keyboard handling. Touch gesture improvements require custom implementation since xterm.js provides minimal support.

---

## Prototype: visualViewport Keyboard ✅ CHOSEN APPROACH

We built a proof-of-concept that validates the visualViewport approach for keyboard handling. **This is our chosen solution.**

**Location**: `prototypes/visualviewport-keyboard/`

### How It Works

```
┌─────────────────────────────────────┐
│ Debug bars (fixed, top: 0-72px)     │
├─────────────────────────────────────┤
│ .terminal-wrapper                   │  ← position: absolute
│   top: 72px                         │    bottom: dynamically set by JS
│   bottom: keyboardHeight + 60px     │
│   └── #terminal (xterm.js)          │
│   └── .touch-scroll-proxy           │
├─────────────────────────────────────┤
│ .input-bar (fixed)                  │  ← bottom: keyboardHeight
│   └── input + send button           │
├─────────────────────────────────────┤
│ [Virtual Keyboard - not our DOM]    │
└─────────────────────────────────────┘
```

### Verified Results

| Feature | iOS Chrome | iOS Safari |
|---------|------------|------------|
| Keyboard height detection | ✅ Works | ⚠️ Quirky |
| Input bar positioning | ✅ Works | ⚠️ Quirky |
| Terminal resize on keyboard | ✅ Works | ⚠️ Quirky |
| Touch scroll proxy | ✅ Works | ✅ Works |

**Note**: iOS Safari has WebSocket issues that make it unsuitable for our use case anyway (see `research/2025-XX-XX-ios-safari-websocket.md`).

### Critical Implementation Details

1. **Store original window height at page load**:
   ```javascript
   // BEFORE keyboard can appear
   const originalWindowHeight = window.innerHeight;
   ```
   This is critical because `interactive-widget=resizes-content` causes `window.innerHeight` to shrink when keyboard appears.

2. **Calculate keyboard height from visualViewport**:
   ```javascript
   const keyboardHeight = Math.max(0, originalWindowHeight - visualViewport.height);
   const keyboardVisible = keyboardHeight > 50;
   ```

3. **Use absolute positioning, not flex**:
   ```css
   .terminal-wrapper {
       position: absolute;
       top: 72px;
       left: 0;
       right: 0;
       bottom: 60px; /* JS updates this */
   }
   ```

4. **Directly set bottom in JS - no CSS transitions**:
   ```javascript
   const wrapperBottom = keyboardVisible ? keyboardHeight + 60 : 60;
   terminalWrapper.style.bottom = `${wrapperBottom}px`;
   ```

5. **Immediate refit with requestAnimationFrame**:
   ```javascript
   requestAnimationFrame(() => {
       fitAddon.fit();
       sendResize();
       term.scrollToBottom();
   });
   ```

6. **Prevent iOS scroll on focus**:
   ```javascript
   cmdInput.addEventListener('focus', () => {
       setTimeout(() => {
           window.scrollTo(0, 0);
           updateViewport();
       }, 100);
   });
   ```

### What We Learned

- CSS transitions on container height cause black gaps during resize
- `interactive-widget=resizes-content` meta tag helps Chrome/Firefox but not Safari
- Must use `originalWindowHeight` captured at load time, not current `window.innerHeight`
- Absolute positioning with explicit `bottom` value is more reliable than flex layout for keyboard handling
- `requestAnimationFrame` is better than `setTimeout` for refit timing

### Next Steps for Integration

1. Port visualViewport keyboard handling to swe-swe's `terminal-ui.js`
2. Combine with touch scroll proxy (already in this prototype)
3. Test with real terminal workloads

See `prototypes/visualviewport-keyboard/LEARNINGS.md` for full prototype details.

---

## Prototype: Touch Scroll Proxy ✅ CHOSEN APPROACH

We built a proof-of-concept that validates the overlay approach. **This is our chosen solution.**

**Location**: `prototypes/touch-scroll-proxy/`

### How It Works

```
┌─────────────────────────────────────┐
│ .touch-scroll-proxy                 │  ← Transparent overlay, z-index on top
│   - overflow-y: scroll              │     Captures all touch events
│   - pointer-events: auto            │
│   └── .scroll-spacer                │  ← Tall div (scrollback × lineHeight)
│       └── (markers for debugging)   │
├─────────────────────────────────────┤
│ #terminal (xterm.js)                │  ← pointer-events: none on touch devices
│   - Renders terminal content        │     Receives scroll commands via JS
│   - transform: translateY() for     │
│     rubber band effect              │
└─────────────────────────────────────┘
```

### Verified Results

| Feature | Status |
|---------|--------|
| Native iOS momentum scrolling | ✅ Works perfectly |
| Scroll position sync (proxy → xterm) | ✅ Works |
| Rubber band overscroll effect | ✅ Works via translateY |
| Prevents keyboard popup on tap | ✅ Works |
| Performance | ✅ Smooth 60fps |

### Critical Implementation Details

1. **Spacer height must exceed viewport** - Otherwise nothing to scroll. Use:
   ```javascript
   const height = Math.max(
       term.buffer.active.length * lineHeight,
       viewportHeight + 100  // ensure scrollable
   );
   ```

2. **Must disable ALL xterm touch events**:
   ```css
   @media (pointer: coarse) {
       #terminal, #terminal *, .xterm, .xterm *,
       .xterm-viewport, .xterm-helper-textarea {
           pointer-events: none !important;
       }
   }
   ```

3. **iOS momentum requires**:
   ```css
   .touch-scroll-proxy {
       overflow-y: scroll;
       -webkit-overflow-scrolling: touch;
   }
   ```

4. **Rubber band effect**:
   ```javascript
   if (scrollTop < 0) {
       terminal.style.transform = `translateY(${-scrollTop * 0.5}px)`;
   } else if (scrollTop > maxScroll) {
       terminal.style.transform = `translateY(${(maxScroll - scrollTop) * 0.5}px)`;
   }
   ```

### What We Sacrifice

- Touch text selection (was broken in xterm.js anyway)
- Direct touch-to-focus (use tap handler on overlay instead)

### Next Steps for Integration

1. Port touch scroll proxy to swe-swe's `terminal-ui.js`
2. Remove debug markers (cyan numbers) for production
3. Test with full 5000-line scrollback
4. Handle alternate buffer mode (vim/less)
5. Test reverse sync (new output → auto-scroll)

See `prototypes/touch-scroll-proxy/LEARNINGS.md` for full prototype details.
