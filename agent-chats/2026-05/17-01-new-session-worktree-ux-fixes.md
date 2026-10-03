<!-- agent-chat export
title: New Session Worktree Ux Fixes
date: 2026-05-17
index: 01
slug: new-session-worktree-ux-fixes
agent: claude
version: 0.6.0 (4e2b560)
-->

# New Session Worktree Ux Fixes

_2026-05-17 · 01 · claude · agent-chat 0.6.0 (4e2b560)_

**USER**

> i think our New Session isn't creating a worktree anymore. verify?

<small>took 12.4s</small><br>
**AGENT**

> Looking into the New Session flow to check whether it still creates a worktree.

<small>took 1m 22s</small><br>
**AGENT**

> Let me boot the e2e test stack and walk the New Session dialog through the browser to see what URL params it actually emits.

<small>took 4m 56s</small><br>
**AGENT**

> Verified in a fresh e2e container. Short answer: **the worktree code path is intact, but it silently falls back to `/workspace` when you pick a branch that's already checked out (typically `main`).**
>
> What I did:
> 1. Booted `make e2e-up-simple`, opened the New Session dialog.
> 2. Picked `Default workspace`, branch combo offered only `main`, selected it, clicked Start Agent Terminal.
> 3. URL was `…/session/<uuid>?assistant=opencode&branch=main` — so the dialog *is* sending `branch=main`.
> 4. `/worktrees/` stayed empty. Container log:
>    ```
>    Attaching worktree to existing local branch main in /workspace
>    Warning: failed to create worktree for branch main in /workspace:
>      exit status 128 (output: fatal: 'main' is already checked out at '/workspace')
>    ```
> 5. To rule out the rest of the pipeline, I hit `/session/<uuid>?assistant=opencode&branch=feature-test` directly. Worktree appeared at `/worktrees/feature-test` and log says `Created worktree at /worktrees/feature-test (branch: feature-test)`. So `createWorktreeInRepo` still works for new branches.
>
> Why it looks like "not creating a worktree anymore":
> - `main.go:4324` swallows the worktree error and falls back to `workDir = baseRepo`. No banner, no toast — the session just runs in `/workspace`.
> - The branch combo only lists existing branches and the user-friendliest pick (`main`) is exactly the case git refuses.
>
> Two ways to fix, depending on what behavior you want:
> - **(A) Don't silently drop to `/workspace`.** Surface the error so the user sees "main is already checked out" instead of getting a session in the base repo unknowingly.
> - **(B) Default-suggest a fresh branch name** in the dialog (e.g. `session-<timestamp>` or `<agent>-<short-uuid>`) so the common click-through path *does* create a worktree, like it did when the field was free-text.
>
> Want me to draft one of these?

[Quick replies]
- Draft fix (B) — auto-suggest a fresh branch name
- Draft fix (A) — surface the error
- Both
- Hold off — just wanted the diagnosis

<small>took 1666m 52s</small><br>
**AGENT**

> Verified in a fresh e2e container. Short answer: **the worktree code path is intact, but it silently falls back to `/workspace` when you pick a branch that's already checked out (typically `main`).**
>
> What I did:
> 1. Booted `make e2e-up-simple`, opened the New Session dialog.
> 2. Picked `Default workspace`, branch combo offered only `main`, selected it, clicked Start Agent Terminal.
> 3. URL was `…/session/<uuid>?assistant=opencode&branch=main` — so the dialog *is* sending `branch=main`.
> 4. `/worktrees/` stayed empty. Container log:
>    ```
>    Attaching worktree to existing local branch main in /workspace
>    Warning: failed to create worktree for branch main in /workspace:
>      exit status 128 (output: fatal: 'main' is already checked out at '/workspace')
>    ```
> 5. To rule out the rest of the pipeline, I hit `/session/<uuid>?assistant=opencode&branch=feature-test` directly. Worktree appeared at `/worktrees/feature-test` and log says `Created worktree at /worktrees/feature-test (branch: feature-test)`. So `createWorktreeInRepo` still works for new branches.
>
> Why it looks like "not creating a worktree anymore":
> - `main.go:4324` swallows the worktree error and falls back to `workDir = baseRepo`. No banner, no toast — the session just runs in `/workspace`.
> - The branch combo only lists existing branches and the user-friendliest pick (`main`) is exactly the case git refuses.
>
> Two ways to fix, depending on what behavior you want:
> - **(A) Don't silently drop to `/workspace`.** Surface the error so the user sees "main is already checked out" instead of getting a session in the base repo unknowingly.
> - **(B) Default-suggest a fresh branch name** in the dialog (e.g. `session-<timestamp>` or `<agent>-<short-uuid>`) so the common click-through path *does* create a worktree, like it did when the field was free-text.
>
> Want me to draft one of these?

[Quick replies]
- Draft fix (B) — auto-suggest a fresh branch name
- Draft fix (A) — surface the error
- Both
- Hold off — just wanted the diagnosis

<small>took 1666m 53s</small><br>
**AGENT**

> Verified in a fresh e2e container. **Worktree code path is intact — but it silently falls back to `/workspace` when you pick a branch that's already checked out (typically `main`).**
>
> What I saw:
> 1. New Session → Default workspace → branch combo offered only `main` → Start Agent Terminal.
> 2. URL was `…/session/<uuid>?assistant=opencode&branch=main` (dialog *is* sending `branch=`).
> 3. `/worktrees/` stayed empty. Server log:
>    ```
>    Attaching worktree to existing local branch main in /workspace
>    Warning: failed to create worktree for branch main in /workspace:
>      exit status 128 (fatal: 'main' is already checked out at '/workspace')
>    ```
> 4. Hitting `/session/<uuid>?assistant=opencode&branch=feature-test` directly *did* create `/worktrees/feature-test` cleanly — so `createWorktreeInRepo` itself still works.
>
> Why it looks broken:
> - `main.go:4324` swallows the worktree error and sets `workDir = baseRepo`. No banner/toast — the session just runs in `/workspace`.
> - The branch combo only lists existing branches, so the friendliest pick (`main`) is exactly the case git refuses.
>
> Two repair options:
> - **(A) Surface the error** instead of silently dropping to `/workspace`.
> - **(B) Default-suggest a fresh branch name** in the dialog (e.g. `session-<timestamp>`) so the click-through path creates a worktree, like when the field was free-text.
>
> Which do you want me to draft?

[Quick replies]
- Draft (B) — auto-suggest fresh branch name
- Draft (A) — surface the error
- Both
- Just the diagnosis, no fix yet

**USER**

> Whether I press "Enter" or something seem to make a difference? When I'm creating a new branch, it also feels unexpected

<small>took 8.5s</small><br>
**AGENT**

> Let me trace Enter handling in the combo-box and dialog so I can describe what's actually different.

<small>took 5m 25s</small><br>
**AGENT**

> Reproduced the Enter-vs-click difference. Here's what's going on:
>
> **The combo-box only commits its typed text on close — not while you're typing.**
>
> `combo-box.js` keeps your keystrokes in its internal input but the upgraded `<input id="new-session-branch">` (which is what the dialog actually reads when building the URL) stays empty until the combo "closes". Close fires on:
> - pressing Enter in the combo,
> - pressing Escape / Tab,
> - clicking outside the combo (which is what happens when you click Start — the document-click listener fires first, commits, then the Start handler runs).
>
> So the user-visible behavior:
>
> | Action | What happens |
> |---|---|
> | Type new branch → click Start | Mousedown on Start closes the combo → commit → dialogState gets branch → Start builds URL with `&branch=...` → worktree created |
> | Type new branch → press Enter | Combo commits the text, dropdown closes. **No navigation.** You still have to click Start. |
> | Type new branch → blur (Tab away) → click Start | Same as path 1, works |
> | Type new branch → never close combo before clicking Start | Path 1 saves you because the doc-click listener runs first |
>
> Why Enter "feels weird":
> - In the *clone URL* and *new project name* fields, Enter triggers `Next` (calls `prepareRepo`). That's wired with explicit `keydown` handlers on those inputs.
> - In the branch combo, Enter is captured by `combo-box.js`, `e.preventDefault()`ed, and only commits the value — it does **not** advance to Start.
> - So the user gets two different Enter behaviors between fields in the same dialog. The branch field also shows "No suggestions — press Enter to use as-is" when you type a new name, which strongly implies Enter does something more than it does.
>
> I also confirmed: typing a new branch + clicking Start *does* create the worktree on the server (`/worktrees/click-flow-branch` after the click flow). So the new-branch path itself works — it just feels off because Enter doesn't behave like a primary submit and there's no visible confirmation of the commit.
>
> Three small tweaks that would cover both complaints, ordered by impact:
> - **Enter in the branch combo submits** when the combo is closed (or after auto-closing): trigger Start (default) or move focus to Start.
> - **Commit-on-input**, not just on close, so dialogState always reflects what the user sees.
> - **Replace the "press Enter to use as-is" hint** with something clearer, or drop it once Enter actually submits.
>
> Want me to draft the change?

[Quick replies]
- Yes — draft all three tweaks
- Just the Enter-submits tweak
- Just the commit-on-input tweak
- Hold off

**USER**

> What if focus into branch field means we hide the fields below (like when we first New Session) what becomes of the problem?
>
> <div style="display:flex;flex-wrap:wrap;gap:8px;">
> <a href="./assets/2026-05-17-01-1.png" style="flex:0 1 calc(33% - 8px);max-width:calc(33% - 8px);"><img src="./assets/2026-05-17-01-1.png" alt="Screenshot 2026-05-17 at 7.18.23 AM.png" style="width:100%;height:auto;display:block;border-radius:6px;"></a>
> </div>

<small>took 51.6s</small><br>
**AGENT**

> That mostly fixes it, with one wrinkle.
>
> **What goes away:**
> - Click-Start-while-combo-has-uncommitted-text becomes impossible — Start isn't on screen.
> - Enter gains a visible effect: it closes the combo → commits the value → reveals Agent / Extra-args / Start. So Enter now matches the Where-field "you completed this step, here's the next" pattern — consistent with the rest of the wizard.
> - New-branch flow stops feeling weird because the user's mental model becomes "type → Enter → next step appears," which matches how they got past the Where step.
>
> **What stays:**
> - The actual *commit-on-close-only* behavior in `combo-box.js` is unchanged. We're just preventing the user from observing it. Fine in this dialog; might bite us in future use sites.
>
> **One small downside to flag:**
> - If the user has already filled everything in and just wants to fix a typo in the branch, focusing the branch combo would collapse Agent / Extra-args / Start. They'd have to commit again to get them back. Two ways to soften:
>   - Only collapse if the combo has no committed value yet (first-time-through). Re-focusing a previously-committed branch leaves Start visible.
>   - Or collapse but keep the Agent selection / extra-args values intact (they're still in state), so re-revealing is instant — just visual flicker, no lost work.
>
> I'd lean **collapse-only-when-uncommitted**, since it gives the wizard feel on the first pass without punishing edits. Want me to draft it?

[Quick replies]
- Draft collapse-only-when-uncommitted
- Draft always-collapse-on-focus
- Draft both, side-by-side
- Hold off

**USER**

> Draft always-collapse-on-focus

<small>took 16m 11s</small><br>
**AGENT**

> Done and verified end-to-end. Three small additions, no churn elsewhere.
>
> **Changes (26 net lines)**
> - `static/new-session-dialog.js` — MutationObserver on `#branch-combo`'s `open` attribute toggles a class on the dialog overlay. Cleared in `closeDialog`.
> - `page-templates/selection.html` — CSS rule hides Agent / Extra CLI flags / env hint / footer (Start buttons) when that class is on. `!important` needed because `prepareRepo` sets `envHint.style.display` inline.
> - Plus the auto-generated golden-file mirrors of those two edits (via `make build golden-update`).
>
> **Behavior verified in the e2e container**
> 1. Open dialog → pick Default workspace → full form visible (Where, Theme, Branch, Agent, Extra args, env hint, Start buttons). [screenshot earlier: `branch-before-focus.png`]
> 2. Focus branch combo → Agent / Extra args / env hint / Start collapse; only Where, Theme, Branch remain. [`branch-collapsed.png`]
> 3. Type `collapse-flow-branch` → press Enter → combo commits, dropdown closes, lower steps re-appear with the typed value bound. [`branch-after-commit.png`]
> 4. Click Start Agent Terminal → URL is `…&branch=collapse-flow-branch` → server log: `Created worktree at /worktrees/collapse-flow-branch` and `/worktrees/` contains the new dir.
>
> **Side checks**
> - `make test` — all packages PASS.
> - `make build golden-update` — diff is exactly the two source edits mirrored into each golden variant, no surprise churn.
>
> **Original underlying bug (silent fallback for already-checked-out branches) is NOT fixed by this**
> Picking `main` from the suggestion list when `/workspace` already has `main` checked out will still fail with `fatal: 'main' is already checked out at '/workspace'`, and `main.go:4324` will still swallow the error and run the session in `/workspace` without a worktree. The collapse UI nudges users toward typing a new branch name (which works), but doesn't surface the failure when they pick an existing one. Want me to follow up with the surface-the-error fix (your option (A) from earlier)?

[Quick replies]
- Yes, do the surface-the-error fix
- No, this is enough for now
- Show me the diff first

**USER**

> Yes, do the surface-the-error fix

<small>took 6m 40s</small><br>
**AGENT**

> Surface-the-error fix shipped on top of the collapse change. End-to-end verified.
>
> **Three template edits (50 net lines)**
>
> 1. `main.go:4334` — `getOrCreateSession` now returns the error from `createWorktreeInRepo` instead of swallowing it and silently running in `/workspace`.
>
> 2. `main.go:4802` — `handleWebSocket` was sending the error as a text message before `defer rawConn.Close()`, which the frontend parsed as invalid JSON and then auto-reconnected against. Now it sends a proper WS close frame with code 4002 + the error string as the reason (trimmed to 120 bytes — the WS close-reason cap is 123).
>
> 3. `terminal-ui.js:1280` — added a 4002 handler next to the existing 4001 case: marks `processExited = true` (suppresses the reconnect loop) and displays the close reason in the status bar.
>
> **Verified in container**
> - `GET /session/<uuid>?assistant=opencode&branch=main` (the existing-checked-out case): server log shows `Session creation error: worktree for branch "main" in /workspace: failed to create worktree...`, the page status bar shows that same string at the top, terminal stays blank, no reconnect loop. Screenshot above [`surface-error.png`].
> - `GET /session/<uuid>?assistant=opencode&branch=verify-newbranch`: still creates `/worktrees/verify-newbranch` and starts the session normally.
> - `make test` — all PASS.
> - `make build golden-update` — diff is just the source edits mirrored into each golden variant, no unexpected churn.
>
> **Caveat to flag for follow-up (not done)**
> The 120-byte truncation cuts off the most useful part of the git error: `fatal: 'main' is already checked out at '/workspace'`. The reason that survives reads `worktree for branch "main" in /workspace: failed to create worktree: exit status 128 (output: Preparing worktree (che…`. To get the "already checked out" phrase to the user, we could either (a) detect this specific case in `createWorktreeInRepo` and return a short purpose-built message, or (b) skip the WS-reason path and send a JSON `session_error` message before close so the frontend can render the full text. Want me to do (a)? It's the smallest follow-up.

[Quick replies]
- Yes, do (a) — short purpose-built message
- Do (b) — JSON message + full text
- Hold off

**USER**

> Do (b) — JSON message + full text

<small>took 4m 50s</small><br>
**AGENT**

> Done — switched from the 120-byte truncated close-reason to a JSON `session_error` message + close. Full error reaches the user.
>
> **What changed (62 net lines across 4 templates)**
> - `main.go:4802` — on session-creation failure, write a `{"type":"session_error","message":...}` text frame first, *then* close with code 4002 + short generic reason. Drops the previous 120-byte trim.
> - `terminal-ui.js:1450` — added a `case 'session_error'` in `handleJSONMessage` that stashes `msg.message` on `this._sessionErrorMsg`.
> - `terminal-ui.js:1280` — onclose 4002 handler now prefers `this._sessionErrorMsg` over the close `reason` field.
> - (Plus the earlier `main.go` change that propagates the worktree error instead of silently falling back, and the dialog collapse-on-focus change.)
>
> **End-to-end verified**
> - `GET /session/<uuid>?assistant=opencode&branch=main` with `main` checked out at `/workspace`: page status bar shows the full message including `fatal: 'main' is already checked out at '/workspace'` — the previously-truncated tail is now visible. Terminal stays blank, no reconnect loop. [`surface-error-full.png` confirmed]
> - Successful branch (`verify-newbranch`) still creates the worktree and starts normally — regression-tested earlier.
> - `make test` — all PASS.
> - `make build golden-update` — diff is just the four source edits mirrored into goldens.
>
> All three behaviors land together:
> 1. Branch combo focus collapses lower steps; commit re-expands.
> 2. Worktree errors propagate instead of silent `/workspace` fallback.
> 3. Full git error text now reaches the user instead of being WS-close-reason-truncated.

[Quick replies]
- Looks good — anything else?
- Commit these changes
- Show me the full diff

**USER**

> screenshots?

<small>took 7.8s</small><br>
**AGENT**

> Re-spinning the e2e container to recapture the screenshots — I deleted them when tearing down earlier.

<small>took 2m 21s</small><br>
**AGENT**

> Four screenshots — full New Session flow plus the fatal-error landing.

[Quick replies]
- Looks good — commit these
- Show me the full diff first
- More changes

**USER**

> export chat logs and git commit with our changes

