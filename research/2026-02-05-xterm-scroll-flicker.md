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

---

## Reopened: Full-Screen Redraw Viewport Jump (Mar 21, 2026)

### Mar 15 Root Cause Was Wrong

The Mar 15 investigation blamed cursor-up (`\x1b[nA`) sequences for the viewport jump. Session recording analysis (session `06fd6d10`) proves this is incorrect:

1. **xterm.js does NOT scroll viewport on cursor-up.** Per xterm.js source (`BufferService`, `Viewport.ts`), `CSI A` only moves the cursor position in the buffer. No viewport scroll is triggered. Confirmed by xterm.js maintainers ([issue #216](https://github.com/xtermjs/xterm.js/issues/216), [PR #336](https://github.com/xtermjs/xterm.js/pull/336)).

2. **The spinner pattern is balanced.** Claude's spinner cycle is: `CSI 7A` (cursor up 7) + content + `\r` + 7x `\r\n` (back down 7). Net cursor movement: zero. No scrollback growth.

3. **Identical spinner in jumping vs non-jumping periods.** Comparing two windows from the same session, the spinner escape sequences are byte-for-byte identical. The spinner alone does not cause jumps.

### Actual Root Cause: CSI 2J + CSI 3J + CSI H (Full-Screen Redraw)

Claude's TUI periodically does full-screen redraws, emitting:
```
\x1b[2J    <- clear visible screen
\x1b[3J    <- clear scrollback buffer
\x1b[H     <- cursor home (row 1, col 1)
...        <- re-render entire TUI top-to-bottom
```

Session recording comparison:

| | Jump window (6MB, ~5min) | Non-jump window (185KB, ~4min) |
|---|---|---|
| `CSI 2J` (clear screen) | **322** | **0** |
| `CSI 3J` (clear scrollback) | **322** | **0** |
| `CSI H` (cursor home) | **322** | **0** |
| Cursor-up (spinner) | 2525 | 3701 |

The jumping window has **322 full-screen redraws** (~once per second). The non-jumping window has **zero**. The spinner runs continuously in both.

### Why It Causes the Jump

When Claude's TUI does a full redraw:
1. `CSI 3J` clears the scrollback buffer
2. `CSI H` moves cursor to top-left
3. Content is re-drawn line by line with `\r\n` sequences

The jump occurs when this redraw cycle is split across rendering frames. Possible splitting points:
- **rAF write batching**: our `requestAnimationFrame` batching in `terminal-ui.js` combines WebSocket messages. If the clear-screen lands in one rAF frame and the content redraw in the next, the user briefly sees a blank/partial screen.
- **DECSET 2026 (synchronized output)**: Claude wraps redraws in `\x1b[?2026h` / `\x1b[?2026l` markers. If xterm.js supports this, the redraw should be atomic. If not, or if our rAF batching splits the sync block, the atomicity guarantee is broken.

### Why It's Intermittent

Claude's TUI only does full-screen redraws when its layout changes (e.g., streaming response text, tool call status updates, new spinner state). During a pure spinner wait with no layout change, only the cursor-up spinner animation runs -- no full redraws, no jumps.

This explains the user observation: "the current MCP wait isn't jumping, but earlier it was." The earlier wait had layout changes triggering full redraws; the current one is a pure spinner.

### Implications for Fix 4

Fix 4 (`scrollToBottom()` in write callback) is a **symptom fix**, not a root cause fix. It corrects the viewport after a jump, but the jump still happens for one frame. The actual solutions to explore:

1. **Strip `CSI 3J`** from PTY output before writing to xterm.js. This prevents scrollback clearing, so the viewport has no reason to jump. Risk: breaks explicit `/clear` behavior in the terminal.

2. **Ensure DECSET 2026 works end-to-end.** If xterm.js properly buffers synchronized output, the full redraw should be atomic (no mid-redraw frame visible). Need to verify: (a) our xterm.js version supports DECSET 2026, (b) our rAF batching doesn't split sync blocks.

3. **Don't split sync blocks in rAF batching.** Modify our write batching to detect `\x1b[?2026h` / `\x1b[?2026l` boundaries and ensure a sync block is never split across frames.

### All Scroll Interference Points (terminal-ui.js audit)

| Line | Context | What it does |
|------|---------|-------------|
| 735 | `fit()` during resize | If near bottom before resize, scroll back to bottom |
| 908-916 | **Fix 4 -- main write path (rAF batch)** | `wasNearBottom` check + `scrollToBottom()` in write callback |
| 966 | Snapshot reassembly | Always `scrollToBottom()` after writing decompressed snapshot |
| 2163 | Mobile ctrl toggle | After keyboard row shows/hides, scroll to bottom if near bottom |
| 2178 | Mobile nav toggle | Same pattern |
| 2374 | Mobile viewport resize (iOS keyboard) | Same pattern |

Only line 908-916 affects normal desktop PTY output. Lines 2163/2178/2374 are mobile-only. Line 966 is snapshot replay only.

### External Research

**xterm.js issues:**
- [#216](https://github.com/xtermjs/xterm.js/issues/216): PTY writes should not auto-scroll when user scrolled up. Fixed via `isUserScrolling` flag.
- [#1579](https://github.com/xtermjs/xterm.js/issues/1579): Device status reports (`CSI 6n`) routed through input handler caused scroll-to-bottom. Fixed in v3.6.0.
- [#1824](https://github.com/xtermjs/xterm.js/issues/1824): `scrollOnUserInput` option added in v5.1.0.
- [Discussion #4869](https://github.com/xtermjs/xterm.js/discussions/4869): Replaying history with control characters -- recommends replay before connecting PTY.

**asciinema:** Uses its own virtual terminal ([avt](https://github.com/asciinema/avt), Rust/WASM) with no scrollback -- cursor-up is just a pointer move in a 2D grid. Not applicable to interactive terminals.

**Other web terminals (ttyd, gotty, wetty):** All use xterm.js, none have custom scroll fixes. Rely on xterm.js's built-in `isUserScrolling` tracking.

### Solution Analysis

**Option 1: Strip CSI 3J from PTY output (RECOMMENDED)**

`CSI 3J` clears the scrollback buffer. For full-screen TUI apps, scrollback is irrelevant -- the TUI redraws every frame. Stripping `CSI 3J` preserves scrollback, so the viewport has no reason to jump.

Session recording evidence: `CSI 3J` always appears paired with `CSI 2J` (100% of the time). Never standalone. Feb 5 testing confirmed `CSI 2J` alone does NOT cause viewport jump on xterm.js 5.5.0.

Collateral:
- Scrollback grows with stale TUI frames (capped at 5000 lines, bounded)
- Explicit `/clear` won't clear scrollback (acceptable in web terminal context)
- No impact on OpenCode (never emits `CSI 3J` -- uses alternate screen buffer)

**Option 2: scrollOnEraseInDisplay -- NOT relevant**

Only affects `CSI 2J` behavior, not `CSI 3J`. Setting it to `true` would push stale frames into scrollback on every redraw, making things worse.

**Option 3: Ensure DECSET 2026 sync blocks aren't split**

Claude wraps redraws in sync markers. If xterm.js processes them atomically, the clear+redraw is invisible. But rAF batching may split sync blocks across frames. More complex to implement.

### Agent Comparison: Claude vs OpenCode

| Sequence | Claude | OpenCode |
|---|---|---|
| `CSI 2J` (clear screen) | ~1/sec during active work | 0 |
| `CSI 3J` (clear scrollback) | ~1/sec during active work | 0 |
| `CSI ?1049h` (alternate screen) | no | yes |
| `CSI ?2026h/l` (sync output) | yes | yes |
| Cursor-up for spinner | yes (balanced up/down) | no |

OpenCode uses the alternate screen buffer (standard for TUI apps like vim, htop, tmux). Alternate screen has no scrollback, so `CSI 3J` is never needed. Claude's Ink-based TUI does not use the alternate screen.

### Fix 5: Strip CSI 3J + Remove Fix 4 (Mar 21, 2026)

**Implementation:**
1. Strip `\x1b[3J` from combined write buffer in `onTerminalData()` before passing to `term.write()`
2. Remove Fix 4's `wasNearBottom` + `scrollToBottom()` write callback -- no longer needed if viewport doesn't jump
3. Keep `scrollToBottom()` for snapshot reassembly (line 966) -- still needed for initial load

**Status:** Implemented and accepted. See [ADR-0039](../docs/adr/0039-strip-csi3j-viewport-fix.md).
**Result:** Stable -- viewport no longer jumps during TUI redraws (tested Mar 22).
