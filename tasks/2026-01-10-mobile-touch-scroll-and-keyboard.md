# Mobile Touch Scroll & Keyboard Handling

**Date**: 2026-01-10
**Branch**: mobile-kb-and-scrolling

## Goal

Incorporate validated prototypes (touch scroll proxy + visualViewport keyboard handling) into swe-swe's `terminal-ui.js` to provide reliable mobile terminal experience on iOS Chrome.

## Background

- xterm.js maintainers say mobile support is "non-existent" and "not a priority"
- Touch gestures fight between terminal selection, scrollback, and page scroll
- Virtual keyboard covers terminal content, user can't see what they're typing
- We built and validated two prototypes that solve these problems

## Reference

- Research: `research/2026-01-10-mobile-terminal-ux-deep-dive.md`
- Touch scroll prototype: `prototypes/touch-scroll-proxy/`
- Keyboard prototype: `prototypes/visualviewport-keyboard/`

---

## Phase 1: Touch Scroll Proxy

### What Will Be Achieved

- Native iOS momentum/inertial scrolling on the terminal
- Rubber band overscroll effect at top/bottom
- Touch events no longer fight between xterm selection, scrollback, and page scroll
- Desktop mouse scrolling remains unaffected

### Steps

#### 1.1 Add HTML structure

Insert `.touch-scroll-proxy` div with `.scroll-spacer` inside the terminal container in `terminal-ui.js` template.

```html
<div class="terminal-ui__terminal">
    <!-- xterm mounts here -->
</div>
<div class="touch-scroll-proxy">
    <div class="scroll-spacer"></div>
</div>
```

#### 1.2 Add CSS for proxy

```css
.touch-scroll-proxy {
    position: absolute;
    inset: 0;
    overflow-y: scroll;
    overflow-x: hidden;
    z-index: 10;
    -webkit-overflow-scrolling: touch; /* iOS momentum */
}
.touch-scroll-proxy::-webkit-scrollbar {
    display: none; /* hide scrollbar - terminal has its own */
}
.scroll-spacer {
    width: 100%;
    pointer-events: none;
    /* height set dynamically by JS */
}
```

#### 1.3 Add CSS for terminal transform

```css
.terminal-ui__terminal {
    transition: transform 0.1s ease-out; /* smooth rubber band */
}
```

#### 1.4 Disable xterm touch events

Must use `!important` and target ALL xterm children:

```css
@media (pointer: coarse) {
    .touch-scroll-proxy {
        display: block;
        pointer-events: auto !important;
    }
    .terminal-ui__terminal,
    .terminal-ui__terminal *,
    .xterm,
    .xterm *,
    .xterm-viewport,
    .xterm-screen,
    .xterm-helper-textarea {
        pointer-events: none !important;
    }
}
@media (pointer: fine) {
    .touch-scroll-proxy {
        display: none;
        pointer-events: none;
    }
}
```

#### 1.5 Add spacer height management

Spacer must EXCEED viewport height to be scrollable:

```javascript
updateSpacerHeight() {
    const lineHeight = 17; // approximate, or calculate from xterm
    const bufferLines = this.term.buffer.active.length;
    const height = Math.max(
        bufferLines * lineHeight,
        this.scrollProxy.clientHeight + 100  // ensure scrollable
    );
    this.scrollSpacer.style.height = `${height}px`;
}
```

Call on `term.onWriteParsed()`.

#### 1.6 Add scroll sync with loop prevention

```javascript
// State
this.syncingFromProxy = false;
this.syncingFromTerm = false;

// Proxy scroll -> xterm
syncProxyToTerm() {
    if (this.syncingFromTerm) return;
    this.syncingFromProxy = true;

    const maxScroll = this.scrollProxy.scrollHeight - this.scrollProxy.clientHeight;
    const scrollTop = this.scrollProxy.scrollTop;

    if (maxScroll > 0) {
        const scrollRatio = Math.max(0, Math.min(1, scrollTop / maxScroll));
        const maxLine = this.term.buffer.active.length - this.term.rows;
        this.term.scrollToLine(Math.round(scrollRatio * maxLine));
    }

    requestAnimationFrame(() => { this.syncingFromProxy = false; });
}

// xterm scroll -> proxy
syncTermToProxy() {
    if (this.syncingFromProxy) return;
    this.syncingFromTerm = true;

    const maxLine = this.term.buffer.active.length - this.term.rows;
    if (maxLine > 0) {
        const scrollRatio = this.term.buffer.active.viewportY / maxLine;
        const maxScroll = this.scrollProxy.scrollHeight - this.scrollProxy.clientHeight;
        this.scrollProxy.scrollTop = scrollRatio * maxScroll;
    }

    requestAnimationFrame(() => { this.syncingFromTerm = false; });
}

// Event listeners
this.scrollProxy.addEventListener('scroll', () => this.syncProxyToTerm(), { passive: true });
this.term.onScroll(() => this.syncTermToProxy());
```

#### 1.7 Add rubber band effect

Clamped translateY (max ±100px):

```javascript
syncProxyToTerm() {
    // ... existing code ...

    const maxScroll = this.scrollProxy.scrollHeight - this.scrollProxy.clientHeight;
    const scrollTop = this.scrollProxy.scrollTop;

    // Rubber band effect for overscroll
    if (scrollTop < 0) {
        // Top overscroll - push terminal down
        const rubberBand = Math.min(-scrollTop * 0.5, 100);
        this.terminalEl.style.transform = `translateY(${rubberBand}px)`;
    } else if (scrollTop > maxScroll) {
        // Bottom overscroll - push terminal up
        const rubberBand = Math.max((maxScroll - scrollTop) * 0.5, -100);
        this.terminalEl.style.transform = `translateY(${rubberBand}px)`;
    } else {
        // Normal scroll - reset transform
        this.terminalEl.style.transform = 'translateY(0)';
    }

    // ... rest of sync code ...
}
```

#### 1.8 Add tap-to-focus

Since xterm has `pointer-events: none`, need click handler on proxy:

```javascript
this.scrollProxy.addEventListener('click', () => this.term.focus());
```

### Verification

| What to verify | How |
|----------------|-----|
| Desktop mouse scroll still works | Manual test - proxy hidden via `(pointer: fine)` |
| Desktop text selection still works | Manual test - pointer-events unchanged |
| Mobile touch scroll works | iOS test |
| Scroll position syncs | Generate 150+ lines, scroll to middle, verify match |
| New output auto-scrolls | Run command at bottom, verify stays at bottom |
| Rubber band effect | Overscroll on iOS, verify visual feedback |
| No xterm.js errors | Check browser console |

### Regression Risk

**Low** - Overlay only activates on touch devices. Desktop behavior unchanged.

---

## Phase 2: visualViewport Keyboard Handling

### What Will Be Achieved

- Terminal resizes when iOS virtual keyboard appears/disappears
- Mobile keyboard UI stays visible above the virtual keyboard
- No black gaps or layout jank during keyboard transitions
- User can see what they're typing

### Steps

#### 2.1 Verify viewport meta tag

Ensure `index.html` has:

```html
<meta name="viewport" content="width=device-width, initial-scale=1.0, interactive-widget=resizes-content">
```

#### 2.2 Store original window height

At component init, BEFORE keyboard can appear:

```javascript
connectedCallback() {
    // ... existing code ...
    this.originalWindowHeight = window.innerHeight;
    this.lastKeyboardHeight = 0;
}
```

#### 2.3 Change terminal container to absolute positioning

Flex layout with transitions causes black gaps. Use absolute positioning:

```css
.terminal-ui {
    position: relative;
    height: 100%;
}
.terminal-ui__terminal {
    position: absolute;
    top: 0;
    left: 0;
    right: 0;
    bottom: 0; /* JS updates this */
    /* NO transition on position/size properties */
    transition: transform 0.1s ease-out; /* only for rubber band */
}
.mobile-keyboard {
    position: absolute;
    left: 0;
    right: 0;
    bottom: 0; /* JS updates this */
}
```

#### 2.4 Add visualViewport event listeners

```javascript
setupViewportListeners() {
    if (window.visualViewport) {
        this._viewportHandler = () => this.updateViewport();
        window.visualViewport.addEventListener('resize', this._viewportHandler);
        window.visualViewport.addEventListener('scroll', this._viewportHandler);
    }
}
```

#### 2.5 Implement updateViewport()

```javascript
updateViewport() {
    const vv = window.visualViewport;
    if (!vv) return;

    const keyboardHeight = Math.max(0, this.originalWindowHeight - vv.height);
    const keyboardVisible = keyboardHeight > 50; // threshold to filter noise

    // Only refit if significant change (>20px)
    if (Math.abs(keyboardHeight - this.lastKeyboardHeight) <= 20) {
        return;
    }
    this.lastKeyboardHeight = keyboardHeight;

    // Get mobile keyboard element height
    const mobileKeyboard = this.querySelector('.mobile-keyboard');
    const mobileKeyboardHeight = mobileKeyboard?.offsetHeight || 0;

    // Position mobile keyboard above virtual keyboard
    if (mobileKeyboard) {
        mobileKeyboard.style.bottom = `${keyboardHeight}px`;
    }

    // Resize terminal container
    const terminalBottom = keyboardVisible
        ? keyboardHeight + mobileKeyboardHeight
        : mobileKeyboardHeight;
    this.terminalEl.style.bottom = `${terminalBottom}px`;

    // Refit terminal immediately
    requestAnimationFrame(() => {
        this.fitAddon.fit();
        this.sendResize();
        this.term.scrollToBottom();
        this.updateSpacerHeight(); // for touch scroll proxy
    });
}
```

#### 2.6 Prevent iOS scroll on focus AND blur

```javascript
setupMobileInputHandlers() {
    const mobileInput = this.querySelector('.mobile-keyboard__text');
    if (!mobileInput) return;

    mobileInput.addEventListener('focus', () => {
        setTimeout(() => {
            window.scrollTo(0, 0);
            this.updateViewport();
        }, 100);
    });

    mobileInput.addEventListener('blur', () => {
        setTimeout(() => {
            window.scrollTo(0, 0);
            this.updateViewport();
        }, 100);
    });
}
```

#### 2.7 Cleanup listeners in disconnectedCallback

```javascript
disconnectedCallback() {
    // ... existing cleanup ...

    if (window.visualViewport && this._viewportHandler) {
        window.visualViewport.removeEventListener('resize', this._viewportHandler);
        window.visualViewport.removeEventListener('scroll', this._viewportHandler);
    }
}
```

### Verification

| What to verify | How |
|----------------|-----|
| Desktop unaffected | Manual test - no keyboard height detected |
| Keyboard show resizes terminal | iOS test - tap input, verify shrink |
| Keyboard dismiss restores terminal | iOS test - dismiss, verify expand |
| No black gaps during resize | iOS test - should be immediate |
| Input bar visible above keyboard | iOS test - can see what typing |
| Mobile keyboard buttons accessible | iOS test - Esc, Tab, Ctrl usable |
| Terminal content preserved | Scroll position remains after toggle |

### Regression Risk

**Low-medium** - Modifying layout, but only when visualViewport indicates keyboard present. Desktop unchanged.

---

## Files to Modify

| File | Changes |
|------|---------|
| `cmd/swe-swe/templates/host/swe-swe-server/static/terminal-ui.js` | HTML template, CSS, JS for both phases |
| `cmd/swe-swe/templates/host/swe-swe-server/static/index.html` | Verify viewport meta tag |

---

## Testing

User will handle deployment and iOS testing after each phase is complete.

---

## Success Criteria

- [ ] Touch scrolling has native iOS momentum
- [ ] Rubber band effect on overscroll
- [ ] Terminal resizes when keyboard appears
- [ ] Mobile keyboard UI visible above virtual keyboard
- [ ] No black gaps or layout jank
- [ ] Desktop behavior unchanged
