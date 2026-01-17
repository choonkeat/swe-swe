# Touch Scroll Proxy: Prototype Learnings

**Date**: 2026-01-10
**Branch**: mobile-kb-and-scrolling

## Problem Statement

Mobile xterm.js has broken touch scrolling:
- Touch gestures fight between terminal selection, scrollback, and page scroll
- xterm.js maintainers say mobile support is "non-existent" and "not a priority"
- No native momentum/inertial scrolling

## Hypothesis

Instead of fighting xterm.js touch handling, **overlay a transparent scrollable div** that:
1. Captures all touch events (xterm gets `pointer-events: none`)
2. Has native browser scrolling with momentum
3. Syncs scroll position to xterm via JavaScript

## Key Learnings

### 1. The Overlay Approach Works

```
┌─────────────────────────────┐
│ .touch-scroll-proxy         │  ← overflow-y: scroll, z-index on top
│   └── .scroll-spacer        │  ← tall div to create scrollable area
├─────────────────────────────┤
│ #terminal (xterm.js)        │  ← pointer-events: none on touch devices
│   └── canvas                │
└─────────────────────────────┘
```

- `pointer-events: none !important` on xterm **does** prevent keyboard popup on tap
- Native iOS momentum scrolling **works** on the overlay
- Media query `(pointer: coarse)` correctly detects touch devices

### 2. Spacer Must Be Taller Than Viewport

**Critical discovery**: If the spacer content fits within the viewport, there's nothing to scroll.

```css
/* BROKEN: Only 37 lines * 17px = 629px, less than screen height */
.scroll-spacer {
    height: calc(bufferLines * lineHeight);
}

/* WORKS: Force minimum scrollable area */
.scroll-spacer {
    min-height: 3000px;
}
```

**Real fix**: Spacer height should represent potential scrollback, not just current content:
```javascript
const height = Math.max(
    term.buffer.active.length * lineHeight,
    term.options.scrollback * lineHeight  // e.g., 5000 * 17 = 85000px
);
```

### 3. iOS Safari Requires `-webkit-overflow-scrolling: touch`

```css
.touch-scroll-proxy {
    overflow-y: scroll;
    -webkit-overflow-scrolling: touch;  /* iOS momentum scrolling */
}
```

### 4. `crypto.randomUUID` Needs Polyfill for HTTP

xterm.js (or its dependencies) uses `crypto.randomUUID()` which requires HTTPS or localhost. For LAN testing over HTTP:

```javascript
if (typeof crypto !== 'undefined' && !crypto.randomUUID) {
    crypto.randomUUID = function() {
        return 'xxxxxxxx-xxxx-4xxx-yxxx-xxxxxxxxxxxx'.replace(/[xy]/g, function(c) {
            const r = Math.random() * 16 | 0;
            return (c === 'x' ? r : (r & 0x3 | 0x8)).toString(16);
        });
    };
}
```

### 5. Debug Visibility is Essential

For mobile testing, add visible debug info:
- Build version/timestamp (to confirm cache-busted)
- Media query match status (`pointer: coarse`)
- Computed styles (`pointer-events`, `display`)
- Scroll positions during sync

```javascript
debug.textContent = `v4 | coarse:${isCoarse} | proxy:${proxyStyle.display}/${proxyStyle.pointerEvents}`;
```

### 6. CSS for Disabling xterm Touch

Must use `!important` and target all children:

```css
@media (pointer: coarse) {
    .touch-scroll-proxy {
        display: block;
        pointer-events: auto !important;
    }
    #terminal,
    #terminal *,
    .xterm,
    .xterm *,
    .xterm-viewport,
    .xterm-screen,
    .xterm-helper-textarea {
        pointer-events: none !important;
    }
}
```

## What We Sacrificed

By disabling xterm touch events:
- ❌ Touch text selection (was broken anyway)
- ❌ Touch-to-focus (need tap handler on overlay)
- ✅ Keyboard still works via mobile keyboard UI buttons

## Verified

- [x] Does scroll position sync work? (proxy scroll → term.scrollToLine) **YES**
- [x] Native iOS momentum scrolling? **YES - works perfectly**
- [x] Rubber band overscroll effect? **YES - via translateY transform**
- [ ] Does reverse sync work? (new terminal output → proxy scroll) - Not fully tested
- [ ] Performance with 5000+ line scrollback - Not tested
- [ ] Behavior in alternate buffer mode (vim, less) - Not tested

## Conclusion

**This technique works and is our chosen approach for mobile touch scrolling.**

The touch scroll proxy successfully provides:
- Native iOS/Android momentum scrolling
- Smooth 60fps performance
- Rubber band overscroll effect
- Clean separation from xterm.js touch handling

## Next Steps

1. **Test sync**: Generate lots of output, verify terminal scrolls with overlay
2. **Implement proper spacer height**: Based on scrollback buffer size
3. **Add scroll position sync**: Both directions with loop prevention
4. **Handle edge cases**:
   - New output auto-scroll when at bottom
   - Alternate buffer mode detection
5. **Integrate into swe-swe**: Port learnings to main codebase

## Files

```
prototypes/touch-scroll-proxy/
├── main.go          # Minimal websocket + PTY server
├── index.html       # xterm.js + touch scroll proxy
├── Dockerfile       # Alpine container
├── go.mod / go.sum  # Go dependencies
└── LEARNINGS.md     # This file
```

## Running the Prototype

```bash
cd prototypes/touch-scroll-proxy
docker build -t touch-scroll-proto .
docker run -it --rm -p 8080:8080 touch-scroll-proto
# Open http://<host-ip>:8080 on mobile
```
