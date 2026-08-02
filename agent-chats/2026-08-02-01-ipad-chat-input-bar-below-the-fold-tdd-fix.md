<!-- agent-chat export
title: Ipad Chat Input Bar Below The Fold Tdd Fix
date: 2026-08-02
index: 01
slug: ipad-chat-input-bar-below-the-fold-tdd-fix
session: 5492173208ff34f7
agent: claude
version: 0.8.22 (af9db02)
-->

# Ipad Chat Input Bar Below The Fold Tdd Fix

_2026-08-02 · 01 · claude · agent-chat 0.8.22 (af9db02)_

**USER**

> Discuss @tasks/2026-08-02-ipad-input-bar-below-fold.md

<small>took 8.0s</small><br>
**AGENT**

> Reading tasks/2026-08-02-ipad-input-bar-below-fold.md now.

<small>took 32.6s</small><br>
**AGENT**

> Nothing in the plan is built yet — I checked the two files it names and both are untouched. Reply "implement" and I'll do it (about 20 minutes including the golden-file regeneration).
>
> **What goes wrong on the iPad**
>
> The box you type into, and the Send button, end up drawn past the bottom edge of the screen. Dragging up to reach them just bounces. Reloading always fixes it.
>
> The app is not sliding down — its top is in the right place in both of your screenshots. It is being drawn *taller than the screen*, so the bottom strip hangs off into nowhere.
>
> **Why**
>
> 1. The app asks the browser "how tall is the visible screen right now?" and stores the answer as a number.
> 2. It only asks again when Safari announces something changed — you rotated, dismissed the keyboard, came back from another app.
> 3. On an iPad, Safari sometimes never sends that final announcement. The app keeps using the old, too-large number.
> 4. The page is deliberately locked against scrolling, so the overhanging bottom cannot be dragged back into view. It stays broken until a reload makes the app measure from scratch.
>
> **The proposed fix**
>
> Add an upper limit written in the stylesheet, using a screen-height value the browser recalculates itself on every repaint. No stored number, so it cannot go stale. The old measurement is kept — it can still make the app *shorter* when that is correct (which is why it was added, for a different iPhone bug) — but it can never make the app *taller* than the actual screen. Plus a small second change: take one extra measurement a third of a second after each change, because Safari sometimes reports the size before it has finished settling.
>
> **One thing I'd flag before you say go**
>
> Nobody has reproduced this on demand. The cause was worked out by reading the code, not by catching it happening. The console test in the plan proves the new limit works; it does not prove that stale-too-tall was really what your iPad did. Your device re-test is what settles it.
>
> That said, the change is very low risk: when the measurement is correct, the limit does nothing at all.
>
> Next step: reply "implement" and I'll make both changes and run the tests.

[Quick replies]
- implement
- Just fix 1, skip the extra measurement
- Explain the iPhone bug it was added for first
- Hold off

**USER**

> Possible to TDD implement?

<small>took 9.1s</small><br>
**AGENT**

> Checking what test hooks exist for the template CSS and terminal-ui.js before answering.

<small>took 55.1s</small><br>
**AGENT**

> Yes for the main fix, only partly for the second one. Reply "TDD it" and I'll start with the failing test (about 40 minutes total, most of it waiting on the test container).
>
> **Fix 1 (the height limit) — real test-first is possible**
>
> The test would be a browser test, run in a real Chromium against a running copy of the app:
>
> 1. Open a session page.
> 2. Force the bad state: set the stored screen-height number to 200 pixels taller than the real window.
> 3. Measure where the bottom of the app actually lands.
> 4. Assert it is still inside the window.
>
> That test fails today and passes after the change. The project already has this kind of test (`e2e/tests/terminal-ui-tabs.spec.js`) with helpers for opening a session and setting a small screen size, so there is nothing new to build.
>
> **Fix 2 (the extra late measurement) — only a shallow test**
>
> The point of that change is to survive Safari reporting a size before it has finished settling. Chromium does not do that, so no test can reproduce the real condition. The most I can honestly write is: fire a size-change event, wait, and assert the app measured a second time. That checks the wiring exists, not that it helps. I'd write it, and label it as exactly that.
>
> **Two things worth knowing**
>
> 1. Neither test proves your iPad is fixed. Only your device re-test does.
> 2. The project's JavaScript unit tests are not run by `make test` at all — they only run if someone types `node --test` by hand. Separate issue; say the word and I'll look at wiring them in afterwards.
>
> Next step: reply "TDD it" and I'll write the failing browser test first, show you it failing, then apply the fix.

[Quick replies]
- TDD it
- Skip the test, just implement
- Only fix 1, test-first
- Wire up the JS unit tests first

**USER**

> TDD it

<small>took 5.4s</small><br>
**AGENT**

> Step 1 of 6: writing the failing browser test. Checking how the e2e environment picks up template edits first.

<small>took 40.7s</small><br>
**AGENT**

> Step 1 of 6: writing the failing test now (e2e/tests/viewport-height.spec.js).

<small>took 25.2s</small><br>
**AGENT**

> Step 1 of 6 done: test written. Step 2 of 6: bringing up the test container (docker build, ~5-10 min).

<small>took 2m 35s</small><br>
**AGENT**

> Step 2 of 6 done: container up on port 9780. Step 3 of 6: running the test to confirm it fails.

<small>took 2m 20s</small><br>
**AGENT**

> Step 3 of 6 done: both tests failed as predicted. Step 4 of 6: fix applied, rebuilding the container to re-run them.

<small>took 3m 48s</small><br>
**AGENT**

> Step 4 of 6 done: both tests now pass. Step 5 of 6: regenerating golden files and running the full unit suite.

<small>took 3m 3s</small><br>
**AGENT**

> Step 5 of 6 done: golden files regenerated (98 files, insert-only) and `make test` passes. Step 6 of 6: running the existing layout tests to check for regressions.

<small>took 3m 20s</small><br>
**AGENT**

> Done, test-first, all 6 steps. Nothing is committed — say "commit it" and I'll do that next.
>
> **The proof, in order it happened**
>
> 1. Wrote `e2e/tests/viewport-height.spec.js` before touching any app code.
> 2. Ran it against the unmodified app: **both tests failed.**
> 3. Applied the two changes.
> 4. Rebuilt and re-ran: **both tests passed** (42.5s).
> 5. `make test` — the full unit suite — passes, and the 98 auto-generated copies of the two files regenerated cleanly (additions only, no deletions).
> 6. Re-ran the 14 existing layout tests as a regression check: **all 14 passed.**
>
> **What changed (3 files)**
>
> 1. `page-templates/index.html` — the height limit. The app can still be made shorter by the stored measurement; it can no longer be made taller than the screen.
> 2. `static/terminal-ui.js` — one extra measurement a third of a second after each size change, and a matching cleanup so that pending measurement is cancelled when the page shuts down.
> 3. `e2e/tests/viewport-height.spec.js` — new, the two tests above.
>
> **Still unproven**
>
> Your iPad. Chromium cannot be made to fail the way Safari does, so test 2 (the late re-measure) only checks the wiring exists, not that it helps. That is written into the test file as a comment so nobody later mistakes it for stronger evidence than it is.
>
> Next step: reply "commit it" and I'll commit the three source files plus the regenerated copies as one change.

[Quick replies]
- commit it
- Show me the diff first
- I'll test on the iPad first
- Wire up the JS unit tests next

**USER**

> commit it

<small>took 3.6s</small><br>
**AGENT**

> Committing now. Checking the staged index first (shared checkout can carry another session's files in).

<small>took 37.9s</small><br>
**AGENT**

> Committed as `e66165c20` on main, not pushed.
>
> 101 files: the 3 real ones plus their 98 auto-generated copies. Working tree is clean apart from unrelated untracked files.
>
> To see the fix on your iPad you need the running app rebuilt from this commit — the test container it was verified in has been torn down.
>
> Next step: reply "reboot" and I'll restart the stack so you can re-test on the iPad.

[Quick replies]
- reboot
- Push it first
- Not now
- Wire up the JS unit tests next

**USER**

> Run /swe-swe:commit-session-chat-log to freeze, scrub and commit this session's chat log. When the commit has landed, end this session by calling the swe-swe MCP tool end_session (no uuid needed -- it ends the calling session). If you cannot commit it (for example a screenshot leaks a secret), say so and do NOT end the session. Afterwards, tell the user in one line: they can save their own /commit-log-then-end command to decide what this button actually does -- their own redaction rules, commit message wording, whether to push, what to leave out -- and swe-swe will run that instead of these instructions.

<small>took 2.3s</small><br>
**AGENT**

> Running /swe-swe:commit-session-chat-log now, then ending this session.

