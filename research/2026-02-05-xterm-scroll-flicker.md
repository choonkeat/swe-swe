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

---

## Reopened: Cursor-Up Viewport Jump (Mar 15, 2026)

### New Root Cause Identified

The Feb 5 investigation only tested **clear-screen sequences** (`\x1b[2J`, `\x1b[3J`, `\x1b[H`). These are handled correctly by xterm.js 5.5.0. However, the viewport-jump bug persists because of a **different class of sequences**: cursor-up (`\x1b[nA`) used for in-place redraws.

Claude's TUI emits cursor-up sequences to do spinner/status updates:
```
\x1b[3A    ← cursor up 3 lines
\x1b[2K    ← erase current line
...new content...
\x1b[3A    ← cursor up again to re-render
```

When xterm.js processes `\x1b[nA`, it moves the cursor up and **scrolls the viewport to keep the cursor visible**. If the cursor moves above the visible area, the viewport jumps up — and with Fix 3 (no scrollToBottom), nothing brings it back down.

**Symptom:** During MCP tool calls (e.g., `send_message` which blocks), Claude's TUI redraws the "Kneading..." spinner with cursor-up sequences. The viewport gets stuck showing old content at the top of the terminal.

### Why Fix 2's flicker was actually from cursor-up

Re-analyzing Fix 2's "flicker": the `scrollToBottom()` in the write callback was fighting with cursor-up sequences mid-redraw. Each animation frame batch could contain:
1. cursor-up (viewport jumps up) → 2. content rewrite → 3. callback fires → scrollToBottom (viewport jumps down)

The "flicker" was this up-down bounce between frames — but it was actually the correct behavior being applied to both clear-screen (unnecessary) AND cursor-up (necessary). When clear-screen was ruled out, the baby (cursor-up fix) was thrown out with the bathwater.

### Fix 4: Restore write callback scrollToBottom (Mar 15)

**Hypothesis:** Fix 2 was correct for cursor-up sequences. The "flicker" it caused may now be reduced by the rAF write batching (commit `17dde09`, added after Fix 3). With batching, cursor-up + content rewrite are more likely to land in the same batch, so scrollToBottom in the callback fires after the full redraw cycle rather than between cursor-up and content.

**Implementation:** Restore `term.write(combined, callback)` with `scrollToBottom()` when user was near bottom. Also restore for snapshot writes.

**Status:** Testing.

**Expected tradeoff:** May still show occasional single-frame viewport bounce during rapid redraws, but prevents the far worse "stuck at top for seconds/minutes" during MCP waits.
