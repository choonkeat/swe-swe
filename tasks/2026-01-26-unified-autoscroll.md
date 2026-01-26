# Unified Auto-Scroll Logic for Terminal UI

## Problem

The terminal UI has fragmented scroll logic causing issues where xterm resets to top unexpectedly:

| Location | Threshold | Action |
|----------|-----------|--------|
| `fitAndPreserveScroll()` | `< rows/2` from bottom | scroll to bottom |
| Touch proxy `onWriteParsed` | `>= maxLine - 1` | sync proxy only |
| `onTerminalData()` | none | no scroll |

Key issues:
1. No auto-scroll on new data for desktop users
2. Can't distinguish "user scrolled up" from "terminal reset viewport via ANSI sequences"
3. Inconsistent thresholds across different code paths

## Solution: Hybrid Intent Tracking

### Core Concept

1. **Default**: Always auto-scroll to bottom on new data
2. **User override**: Track when user explicitly scrolls up
3. **Visual feedback**: Show indicator when auto-scroll is paused
4. **Auto-resume**: Clear pause state when user scrolls back to bottom

### Implementation

#### 1. Add State Tracking

```js
// In constructor or connectedCallback
this.userPausedAutoScroll = false;
```

#### 2. Create Unified `shouldAutoScroll()` Method

```js
shouldAutoScroll() {
    if (this.userPausedAutoScroll) {
        return false;
    }
    return true;
}

isNearBottom(threshold = 3) {
    const buffer = this.term.buffer.active;
    const maxLine = buffer.length - this.term.rows;
    const scrolledUp = maxLine - buffer.viewportY;
    return scrolledUp < threshold;
}
```

#### 3. Track User Scroll Intent

```js
setupScrollTracking() {
    // Wheel scroll (desktop)
    this.term.element.addEventListener('wheel', (e) => {
        // Scrolling up = negative deltaY
        if (e.deltaY < 0 && !this.isNearBottom()) {
            this.pauseAutoScroll();
        }
        // Scrolling down near bottom = resume
        if (e.deltaY > 0 && this.isNearBottom()) {
            this.resumeAutoScroll();
        }
    }, { passive: true });

    // Keyboard scroll (Page Up, arrow keys, etc.)
    this.term.onKey(({ domEvent }) => {
        const scrollKeys = ['PageUp', 'ArrowUp'];
        if (scrollKeys.includes(domEvent.key) && !this.isNearBottom()) {
            this.pauseAutoScroll();
        }
        // Page Down / Ctrl+End near bottom = resume
        const resumeKeys = ['PageDown', 'End'];
        if (resumeKeys.includes(domEvent.key) && this.isNearBottom()) {
            this.resumeAutoScroll();
        }
    });

    // Touch scroll handled via scroll proxy (existing code)
    // Add similar logic to syncProxyToTerm()
}
```

#### 4. Pause/Resume Methods with UI Feedback

```js
pauseAutoScroll() {
    if (this.userPausedAutoScroll) return; // already paused
    this.userPausedAutoScroll = true;
    this.showScrollPausedIndicator();
}

resumeAutoScroll() {
    if (!this.userPausedAutoScroll) return; // not paused
    this.userPausedAutoScroll = false;
    this.hideScrollPausedIndicator();
}

showScrollPausedIndicator() {
    // Show a small toast/badge near bottom of terminal
    // "Auto-scroll paused - click to resume" or just an arrow-down icon
    let indicator = this.querySelector('.scroll-paused-indicator');
    if (!indicator) {
        indicator = document.createElement('button');
        indicator.className = 'scroll-paused-indicator';
        indicator.innerHTML = '&#x2193; New output below';
        indicator.addEventListener('click', () => {
            this.term.scrollToBottom();
            this.resumeAutoScroll();
        });
        this.querySelector('.terminal-ui__terminal').appendChild(indicator);
    }
    indicator.classList.add('visible');
}

hideScrollPausedIndicator() {
    const indicator = this.querySelector('.scroll-paused-indicator');
    if (indicator) {
        indicator.classList.remove('visible');
    }
}
```

#### 5. Update `onTerminalData()` to Auto-Scroll

```js
onTerminalData(data) {
    // ... existing batching logic ...

    if (!this.pendingWrites) {
        this.pendingWrites = [];
        requestAnimationFrame(() => {
            // ... existing combine logic ...
            this.term.write(combined);
            this.pendingWrites = null;

            // NEW: Auto-scroll after write
            if (this.shouldAutoScroll()) {
                this.term.scrollToBottom();
            }
        });
    }
    this.pendingWrites.push(data);
}
```

#### 6. Update `fitAndPreserveScroll()` to Use Unified Logic

```js
fitAndPreserveScroll() {
    if (!this.term || !this.fitAddon) return;

    this.fitAddon.fit();
    this.sendResize();

    // Use unified logic instead of ad-hoc threshold
    if (this.shouldAutoScroll()) {
        this.term.scrollToBottom();
    }
}
```

#### 7. Update Touch Scroll Proxy

In `syncProxyToTerm()`, detect user scroll direction and pause/resume accordingly.

### CSS for Indicator

```css
.scroll-paused-indicator {
    position: absolute;
    bottom: 8px;
    left: 50%;
    transform: translateX(-50%);
    background: rgba(59, 130, 246, 0.9);
    color: white;
    border: none;
    border-radius: 16px;
    padding: 6px 16px;
    font-size: 12px;
    cursor: pointer;
    opacity: 0;
    transition: opacity 0.2s;
    pointer-events: none;
    z-index: 10;
}

.scroll-paused-indicator.visible {
    opacity: 1;
    pointer-events: auto;
}

.scroll-paused-indicator:hover {
    background: rgba(59, 130, 246, 1);
}
```

## Testing Plan

1. **Desktop scroll up**: Wheel up, verify indicator appears
2. **Desktop scroll down**: Wheel down to bottom, verify indicator hides
3. **Click indicator**: Verify scrolls to bottom and hides
4. **New data while paused**: Verify stays scrolled up, indicator visible
5. **Terminal reset sequences**: Send `\x1b[2J`, verify auto-scrolls (not paused)
6. **Touch scroll**: Test on iOS, verify same behavior via proxy
7. **Keyboard scroll**: Page Up/Down, verify pause/resume
8. **View switch**: Switch away and back to terminal, verify scroll state preserved

## Files to Modify

1. `cmd/swe-swe/templates/host/swe-swe-server/static/terminal-ui.js`
   - Add state tracking
   - Add `shouldAutoScroll()`, `isNearBottom()`, `pauseAutoScroll()`, `resumeAutoScroll()`
   - Add `showScrollPausedIndicator()`, `hideScrollPausedIndicator()`
   - Update `onTerminalData()` with auto-scroll
   - Update `fitAndPreserveScroll()` to use unified logic
   - Add scroll event listeners in `setupScrollTracking()`

2. `cmd/swe-swe/templates/host/swe-swe-server/static/styles/terminal-ui.css`
   - Add `.scroll-paused-indicator` styles

## Edge Cases

- **Rapid scroll**: Debounce scroll detection to avoid flickering indicator
- **Small buffer**: When buffer is smaller than viewport, always consider "at bottom"
- **Alt buffer**: Programs like vim use alt buffer; may need to reset pause state on buffer switch
