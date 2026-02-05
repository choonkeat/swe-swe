# xterm.js Scroll-Up / Reset Flicker

## Problem

When CLI tools (like Claude Code) emit clear-screen escape sequences (`\x1b[2J`, `\x1b[3J`, `\x1b[H`), xterm.js resets the viewport to the top of the scrollback buffer. The user sees old output at the top instead of the current prompt — the terminal appears to "scroll up" unexpectedly.

## Fix 1: Synchronous scrollToBottom after write (Feb 4)

**"fix: preserve scroll position when clear-screen sequences reset viewport"**

**Hypothesis:** Clear-screen escape sequences jump the viewport to the top. If we detect the user was near the bottom before the write, we can scroll back immediately after.

**Implementation:** Before calling `term.write()`, capture the current scroll position. If the user was near the bottom (within half a screen), call `scrollToBottom()` synchronously right after the write.

```js
const buffer = this.term.buffer.active;
const maxLine = buffer.length - this.term.rows;
const scrolledUp = maxLine - buffer.viewportY;
const wasNearBottom = scrolledUp < this.term.rows / 2;
this.term.write(combined);
if (wasNearBottom) {
    this.term.scrollToBottom();
}
```

**Result:** Did not work. `term.write()` is async — escape sequences are parsed *after* the call returns. `scrollToBottom()` ran *before* xterm processed the clear-screen sequences, so the viewport still reset afterward.

## Fix 2: Write callback for scroll preservation (Feb 5)

**"fix: use xterm.js write callback for scroll preservation"**

**Hypothesis:** The synchronous fix fails because `term.write()` is async. Using `term.write(data, callback)` ensures `scrollToBottom()` runs after xterm has fully parsed the escape sequences.

**Implementation:** Move `scrollToBottom()` into the write callback. Also fixed the snapshot restore path which used `requestAnimationFrame` — same timing issue.

```js
const buffer = this.term.buffer.active;
const maxLine = buffer.length - this.term.rows;
const scrolledUp = maxLine - buffer.viewportY;
const wasNearBottom = scrolledUp < this.term.rows / 2;
this.term.write(combined, () => {
    if (wasNearBottom) {
        this.term.scrollToBottom();
    }
});
```

**Result:** Partially works — the viewport does end up at the bottom. But introduces a visible flicker: the viewport briefly shows the top-of-scrollback state before snapping back to the bottom.

**TL;DR:** Clear-screen escape sequences caused xterm.js to jump to top of scrollback. Fix 1 tried to scroll back synchronously after write — didn't work because `term.write()` is async. Fix 2 moved the scroll correction into the `term.write()` callback so it executes after escape sequence parsing — works but flickers.

## Live Testing (Feb 5): Premise Was Wrong

Ran a bash script directly in the swe-swe Terminal tab (same xterm.js 5.5.0, scrollback: 5000) to test each sequence in isolation after filling scrollback with 100+ lines:

- **Test 1: `\x1b[2J` alone** — viewport did NOT jump
- **Test 2: `\x1b[3J` alone** — viewport did NOT jump
- **Test 3: `\x1b[2J\x1b[3J\x1b[H` combo** — viewport did NOT jump

**Conclusion:** xterm.js 5.5.0 handles all three sequences without resetting the viewport. The original premise (escape sequences cause viewport jump) was wrong for this version.

### The fix was causing the flicker

The `scrollToBottom()` calls in Fix 2's write callbacks were likely **creating** the flicker, not preventing it. The forced scroll to absolute bottom could fight with xterm.js's natural viewport position, especially when:

- The buffer grows between the position check and the callback
- Multiple batched writes are in flight
- xterm.js already auto-scrolled correctly on its own

### Fix 3: Remove scrollToBottom workaround (Feb 5)

Removed all `scrollToBottom()` calls from both the write path and snapshot path. Replaced with `console.log` debug logging (`[scroll-debug]`) to monitor what would have been scrolled in production.

**Status:** Deployed for observation. Debug logs will confirm whether the scroll correction was ever actually needed.

## Discarded Solutions

The following solutions were analyzed but are no longer needed since the root cause was the fix itself, not the escape sequences:

### 1. Preemptive: scan and neutralize clear-screen sequences

Would have scanned the byte stream for `\x1b[2J` / `\x1b[3J` / `\x1b[H` and stripped or replaced them. Rejected because:

- `\x1b[H` cannot be touched — every TUI frame depends on cursor positioning
- `\x1b[2J` cannot be stripped — TUIs need screen clearing; server's `GenerateSnapshot()` emits it
- `\x1b[3J` is the safest to strip but breaks explicit `/clear` behavior
- Even stripping `\x1b[3J` alone wouldn't help if `\x1b[2J` also caused jumps (it doesn't)
- Moot: xterm.js 5.5.0 doesn't jump on any of these

### 2. Visual masking: hide during write

Would have set `visibility: hidden` during write, restored in callback. Rejected: trades content-flicker for blank-flicker — same problem, different costume.

### 3. Lock scroll position at the DOM level

Would have set `overflow: hidden` on xterm's viewport div during writes. Rejected: amounts to DIY scrolling that fights xterm.js internals.
