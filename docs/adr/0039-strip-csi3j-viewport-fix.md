# ADR-0039: Strip CSI 3J to prevent xterm.js viewport jump

**Status**: Accepted
**Date**: 2026-03-22
**Research**: `research/2026-02-05-xterm-scroll-flicker.md`

## Context

Claude's Ink-based TUI does not use the alternate screen buffer (unlike OpenCode, vim, htop). During full-screen redraws, it emits `CSI 2J` (clear screen) + `CSI 3J` (clear scrollback) + `CSI H` (cursor home) at ~1/sec. The `CSI 3J` clears xterm.js's scrollback buffer, causing the viewport to jump to the top. Users must manually scroll back to the bottom.

Five fixes were attempted over Feb-Mar 2026 (see research doc for full history):

| Fix | Approach | Result |
|-----|----------|--------|
| 1 | Synchronous `scrollToBottom()` after write | Failed -- `term.write()` is async |
| 2 | `scrollToBottom()` in write callback | Works but flickers (up-down bounce) |
| 3 | Remove all scroll corrections | Viewport stuck at top during redraws |
| 4 | Restore write callback (Mar 15) | Flicker returned; wrong root cause blamed |
| 5 | Strip `CSI 3J` from PTY output | **Accepted** -- eliminates root cause |

Key findings:
- `CSI 2J` alone does NOT cause viewport jump on xterm.js 5.5.0
- `CSI 3J` always appears paired with `CSI 2J` (never standalone)
- OpenCode avoids the issue entirely by using alternate screen buffer

## Decision

Strip `CSI 3J` (`\x1b[3J`, 4 bytes: `0x1b 0x5b 0x33 0x4a`) from PTY output in `terminal-ui.js` before writing to xterm.js. Remove Fix 4's `scrollToBottom()` write callback workaround.

Implementation: `stripCSI3J()` function scans the byte buffer and removes matching 4-byte sequences.

## Consequences

Good:
- Eliminates viewport jump at the root cause level (no workaround needed)
- No flicker -- there is no jump to correct
- Simple, targeted fix (strip one escape sequence)

Bad:
- Scrollback grows with stale TUI frames (bounded by xterm.js scrollback cap of 5000 lines)
- Explicit `/clear` in the terminal won't clear scrollback (acceptable in web terminal context)

## Related

- `tasks/2026-01-26-unified-autoscroll.md` -- proposed scroll-to-bottom overlay button (separate UX improvement)
- `tasks/2026-01-10-mobile-touch-scroll-and-keyboard.md` -- mobile touch scroll proxy
- `tasks/2026-01-06-ring-buffer-scrollback.md` -- server-side scrollback buffer for session joiners
