# Preview Back/Forward Button State: Constraints & Options

## Goal

Enable/disable the ◀ Back and ▶ Forward buttons in the preview toolbar based on whether there's actual history to navigate to. Currently they're always enabled.

## Architecture

```
Parent page (port 1977)          Preview iframe (port 51977)
┌──────────────────────┐         ┌──────────────────────┐
│  terminal-ui.js      │         │  User's app          │
│  ◀ ▶ buttons         │         │  + inject.js         │
│  _debugWs ──────────────WSS──────── ws (debug channel) │
│                      │         │                      │
│  iframe.contentWindow │ ✗ BLOCKED │ history, DOM, etc  │
└──────────────────────┘         └──────────────────────┘
         Different ports = different origins
```

## What We Need

The UI needs two booleans after every navigation event:
- `canGoBack` — is there a history entry before the current one?
- `canGoForward` — is there a history entry after the current one?

## Constraints

### 1. Cross-origin: parent cannot read iframe state

The parent page and iframe are on different ports (e.g., `:1977` vs `:51977`). This means:
- `iframe.contentWindow.history` — **throws SecurityError** (this is why the original back/forward didn't work)
- `iframe.contentWindow.location` — **throws SecurityError**
- `iframe.contentWindow.history.length` — **throws SecurityError**
- `iframe.contentWindow.history.state` — **throws SecurityError**
- `postMessage` — works but requires a listener in the iframe

The parent has **zero direct access** to the iframe's history state.

### 2. Browser `history` API lacks position info

Even inside the iframe, the `history` API doesn't expose the current position:
- `history.length` — total entries (available)
- `history.state` — state object of current entry (available)
- `history.index` or `history.position` — **does not exist**

So even the inject script can't directly ask "am I at position 3 of 7?"

### 3. Inject script reinitializes on full page navigations

When the user clicks a regular `<a>` link (not SPA pushState), the browser does a full page load. The inject script:
- Gets destroyed along with the old page
- Reinitializes on the new page (proxy injects it into every response)
- Loses all in-memory variables (counters, stacks, etc.)

However:
- `history.length` **persists** across page loads (it's the iframe's session history)
- `history.state` **persists** for each entry (set via pushState/replaceState, survives back/forward)

### 4. `history.state` markers survive navigation

If we call `history.replaceState({__sweIdx: 5}, '')` on a page, then navigate away, then come back via history.back(), `history.state.__sweIdx` will be `5`. This is reliable across all browsers.

### 5. The debug WebSocket channel works bidirectionally

The inject script already:
- Receives commands from UI: `navigate` (back/forward), `query` (DOM)
- Sends events to UI: `init`, `urlchange`, `console`, `error`, etc.

We can add new message types in both directions.

### 6. `popstate` doesn't indicate direction

When `history.back()` or `history.forward()` is called, a `popstate` event fires. But the event doesn't say whether it was back or forward. The only info is `event.state` (the state object of the entry we landed on).

### 7. User's app may also call history.back()/forward()

The user's own JavaScript might call `history.back()`, `history.pushState()`, etc. We need to handle these, not just our own button clicks.

## Options

### Option A: Inject-side tracking with `__sweIdx` state markers

**How it works:**
- On `init` (page load): call `replaceState` to stamp current entry with `{__sweIdx: N}`. Read `history.length` to know total entries. But `history.length` alone isn't enough — we need to know our position within it.
- On `pushState` (SPA nav): intercept and inject `__sweIdx: nextIdx` into state. `currentIdx = nextIdx; maxIdx = nextIdx`.
- On `replaceState` (SPA nav): inject `__sweIdx: currentIdx` (same position).
- On `popstate`: read `history.state.__sweIdx` to determine new position.
- After each change, send `{t: 'navstate', canGoBack, canGoForward}` to UI.

**Problem:** On a full page load (new inject script), we can read `history.state.__sweIdx` if we previously stamped it (e.g., user went back to a stamped page). But on a **fresh forward navigation** (clicking a link to a new page), `history.state` is `null` — we never stamped it yet. We know `history.length` but not our position in it.

**Workaround:** On init, if `history.state?.__sweIdx` exists, we know our position. If not, assume we're at the end (`position = history.length - 1`) and stamp it. This works for the common case (forward navigation lands at the end). It breaks if the user's app sets `history.state` for its own purposes and we overwrite it.

**Mitigations for state collision:**
- Merge our `__sweIdx` into existing state rather than replacing: `replaceState({...history.state, __sweIdx: N}, '')`
- Use a namespaced key unlikely to collide: `__sweSweNavIdx`

**Verdict:** Works for both SPA and multi-page sites. Slight risk of state collision with user's app. Position tracking is accurate as long as we stamp every entry.

### Option B: UI-side (parent) tracking from URL events

**How it works:**
- Parent maintains a URL stack and index
- On each `init`/`urlchange` message from iframe, push URL to stack
- Back/forward button clicks: adjust index, set a "pending" flag to suppress the next URL event
- `canGoBack = index > 0; canGoForward = index < stack.length - 1`

**Problem:** The parent is building a shadow copy of iframe history. It can get out of sync:
- If user's app calls `history.back()` directly, the parent sees a `urlchange` and treats it as a new forward navigation (pushes to stack) instead of a back navigation
- Duplicate URL changes (e.g., redirect chains) can add phantom entries
- The `_previewNavPending` flag is fragile — timing issues, dropped messages, or multiple rapid clicks can desync

**Verdict:** Simpler implementation, no state collision risk, but fundamentally fragile because it's reconstructing state from events rather than reading the source of truth.

### Option C: Hybrid — inject tracks, UI renders

**How it works:**
- Inject script does all tracking (Option A) and sends `navstate` messages
- UI just reads `canGoBack`/`canGoForward` from messages and sets `disabled`
- UI doesn't maintain any history state itself

**Benefits:**
- Source of truth is inside the iframe (closest to the real history)
- UI is a dumb renderer — no sync issues
- Inject script has access to `history.length`, `history.state`, `popstate` events

**Verdict:** Clean separation. Inject script owns the truth, UI renders it.

## Recommendation

**Option C (Hybrid)** — inject-side tracking with UI rendering.

The inject script is the only code that can directly access `history.state` and `history.length`. It should own the tracking. The UI should just enable/disable buttons based on messages it receives.

### Key implementation details:
- Use `__sweSweNavIdx` in `history.state` to avoid collision with user's app state
- On init: merge idx into state via `replaceState`, assume end-of-stack if no prior stamp
- On pushState/replaceState: intercept and inject idx
- On popstate: read `history.state.__sweSweNavIdx`
- After every change: send `{t: 'navstate', canGoBack: bool, canGoForward: bool}` over WebSocket
- UI: on `navstate`, set `backBtn.disabled` / `forwardBtn.disabled`
- UI: on WebSocket close/disconnect, disable both buttons
