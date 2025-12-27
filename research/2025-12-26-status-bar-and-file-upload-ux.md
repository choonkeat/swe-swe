# Status Bar Clickability and File Upload Loading UX

## Status Bar Clickability

**Design Decision:** Separate hyperlinks with always-visible dotted underlines

- Split status bar into two separate clickable hyperlinks:
  1. **Username link** (??  or name): Set username if not set, rename if already set
  2. **"N others" link**: Open chat input (requires username to be set first)
- Use dotted underline (not solid) visible without hover - makes them immediately recognizable as interactive
- Apply `cursor: pointer` for visual feedback
- Each link has distinct action based on context
- Follows web conventions for persistent hyperlink visibility

This UX is clearer than a single large hover target - each action is explicitly marked as clickable.

---

## File Upload Loading State

**Design Decision:** Blocking overlay with queue awareness

### Why blocking overlay (not silent background processing):

The core issue with background processing is the async boundary at file handoff. When file upload completes and agent starts processing it, user terminal input gets queued while the agent analyzes. This creates unpredictable behavior - user types expecting terminal control, but input doesn't apply to what they expect.

### Implementation approach:

1. **Show overlay** during file transfer and initial agent processing of that file
2. **Make it dismissible or auto-dismiss** once agent finishes initial analysis
3. **Queue multiple files** - show user "N files queued" in overlay rather than blocking mid-queue
4. **Size threshold:** Quick uploads (<1s) probably don't need overlay; slow uploads definitely do

### Why this works:

- Prevents "what state am I in?" confusion
- Clear mental model: overlay = terminal is busy, can't type
- File queuing handles rapid successive drags naturally
- Aligns with how desktop apps handle async operations
- The slight UX friction of blocking is worth avoiding complexity of concurrent input streams

---

## Alternative considered and rejected:

**Option B (status bar indicator only):** No blocking, just status update
- Problem: When file upload completes and agent starts processing, user terminal input is queued but user may not realize it
- Creates disconnect between perceived and actual state
- Harder to reason about what happens if user drags another file while one is still being processed by agent
