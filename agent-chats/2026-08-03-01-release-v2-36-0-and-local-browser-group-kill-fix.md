<!-- agent-chat export
title: Release V2 36 0 And Local Browser Group Kill Fix
date: 2026-08-03
index: 01
slug: release-v2-36-0-and-local-browser-group-kill-fix
session: 855fed00b633aa14
agent: claude
version: 0.9.1 (7abf230)
-->

# Release V2 36 0 And Local Browser Group Kill Fix

_2026-08-03 · 01 · claude · agent-chat 0.9.1 (7abf230)_

**USER**

> What’s changed since last release?

<small>took 10.6s</small><br>
**AGENT**

> Checking the changelog and git history for what's landed since the last release.

<small>took 38.4s</small><br>
**AGENT**

> Nothing is released yet: 10 commits sit on top of v2.35.2, and the CHANGELOG has no entry for them. To ship them, the next step is a version bump to 2.36.0.
>
> Five user-visible changes since v2.35.2:
>
> 1. **The chat input box can no longer slide off the bottom of a tablet screen.** On iPad in landscape with two panes, the page sometimes drew itself taller than the actual screen, pushing the message box and Send button past the bottom edge with no way to scroll to them. Only a reload fixed it. Now the page is capped at the real screen height.
>
> 2. **The Agent View pane stopped going blank.** Agent View is the panel where you watch the browser the agent is driving. When one browser replaced another, the two pieces that send the picture to your screen were started without anyone checking whether they actually survived. They often did not, and the panel stayed blank for the rest of the session with no error anywhere. Those pieces are now checked, retried, and named in the log if they fail. Measured: 8 out of 8 browser replacements now come back working; before, it broke on the second.
>
> 3. **Restarting the browser service no longer strands browsers.** The service could hand out only 20 browsers, and shutting it down abandoned every running one, still holding its slot. After a few restarts nothing could start at all. Shutdown now closes them properly.
>
> 4. **Unused browsers are cleaned up, and a session whose browser disappeared repairs itself.** Browsers idle for 30 minutes are released (configurable, and it will not touch one you are watching or the agent is using). If a browser vanishes mid-session, the agent's next request quietly gets a fresh one instead of failing; if none can be had, the error now says what happened in plain words.
>
> 5. **The login page loads on per-session ports instead of looping forever.** Opening a preview, chat, or files port without being logged in bounced between the port and the login page until the browser gave up. Logged-in users never saw it, which is why it went unnoticed.
>
> Plus three housekeeping commits: two stale automated tests retired (the interface had deliberately changed) and documentation updates.
>
> Next step, under a minute: say the word and I'll draft the v2.36.0 CHANGELOG entries.

[Quick replies]
- Draft the v2.36.0 CHANGELOG
- Not yet -- just wanted the summary
- Bump and release it

**USER**

> /npm-login-and-publish

<small>took 3.7s</small><br>
**AGENT**

> Starting the release: CHANGELOG for v2.36.0, then bump, commit, push, publish.

<small>took 46.0s</small><br>
**AGENT**

> Step 1 of 7 done: v2.36.0 notes drafted in CHANGELOG.md. Read them, then reply OK to continue -- nothing is bumped, built, or published until you do.
>
> Minor bump (2.35.2 -> 2.36.0) because idle reaping adds a new setting, `-browser-backend-idle`.
>
> ---
>
> ## v2.36.0 - Idle Browser Reaping & Per-Session Login
>
> ### Features
>
> - **Idle browsers are reaped, and a session whose browser went away heals itself**: `-browser-backend-idle` (env `SWE_BROWSER_BACKEND_IDLE`, default 30m, `0` disables) frees a browser untouched for that long -- CDP traffic, a live reverse tunnel and a 2-minute Agent View touch all count as use -- and the CDP proxy now re-allocates and replays a request whose browser is gone instead of breaking Agent View for the rest of the session.
>
> ### Fixes
>
> - **Agent View no longer goes blank after a browser is replaced**: `x11vnc` and `websockify` were started unsupervised and quietly lost the race for the port slot, so the pane died while chromium stayed healthy; both are now supervised, started in their own process group so an orphaned `websockify` child cannot squat the port, and the backend reaps its children.
>
> - **The browser backend frees every browser stack on `SIGINT`/`SIGTERM`**: A restart abandoned Xvfb, chromium, `x11vnc` and `websockify` still holding their ports, which stranded one slot per live session until allocation failed outright.
>
> - **The login page is reachable on every per-session port**: In Traefik mode the `/swe-swe-auth` exemption was attached to the `websecure` entrypoint alone, so an unauthenticated request to a preview, chat, VNC or files port redirected to a login page that was itself gated, looping until the browser gave up.
>
> - **The chat input bar can no longer sit below the fold on a tablet**: When mobile Safari dropped the final settling event the app kept painting at the stale -- larger -- viewport height, pushing the input row off an unscrollable page until a reload, so the app is now also capped at `100dvh`.
>
> ---
>
> Next after your OK: `make bump NEW_VERSION=2.36.0`, commit, push, then build the 6 platform binaries. I'll ask for a fresh 6-digit code only when the build is finished.

[Quick replies]
- OK, continue
- Make it 2.35.3 (patch) instead
- Shorten the entries
- Hold off on releasing

**USER**

> Does browser fix work for external browser or docker mode too?

<small>took 4.9s</small><br>
**AGENT**

> Checking which of the three browser modes each fix actually lands in.

<small>took 3m 8s</small><br>
**AGENT**

> Short answer: the blank-pane fix works in both; the other three browser fixes only apply to the separate browser service.
>
> There are exactly two browser setups in the code (there is no "bring your own browser" option -- I checked):
> - **Built-in** -- swe-swe starts the browser inside its own container. This is the default, and what plain docker/compose uses.
> - **Separate service** -- a standalone browser container that swe-swe asks for a browser over the network. Used when you point `SWE_AGENT_VIEW` at its address.
>
> Where each fix lands:
>
> 1. **Blank Agent View pane** -- both setups. The fix is in the shared code that starts the browser (`browser_backend.go`), and both call it.
> 2. **Freeing browsers on shutdown** -- separate service only. The built-in one already closes its browsers when the session ends.
> 3. **Idle cleanup and self-healing** -- separate service only. The built-in one has one browser per session that dies with the session, so there is nothing to reclaim and nothing to reconnect to.
> 4. **Login page loop** -- not a browser fix at all. It only affects docker/compose setups that run Traefik (the piece that routes web traffic to the right port).
> 5. **Tablet input bar** -- every setup. It is web-page code, unrelated to how the browser runs.
>
> One gap I found while checking, and it is not fixed by this release: in the **built-in** setup, ending a session still kills the browser's helper programs one at a time rather than as a group. That is the exact pattern that left a leftover process squatting a port in the separate service. So the same leftover-process problem is still reachable there.
>
> Next step, your call -- one of these:
> - "OK, continue" -- release v2.36.0 exactly as drafted.
> - "Note the gap" -- I add one sentence to the changelog saying the built-in teardown is unchanged, then release.
> - "Fix the gap first" -- about 30 minutes: reuse the group-kill on built-in session teardown, with a test, then release.

[Quick replies]
- OK, continue
- Note the gap, then release
- Fix the gap first
- Hold off on releasing

**USER**

> Fix the gap first

<small>took 5.0s</small><br>
**AGENT**

> Fixing the gap: built-in session teardown will use the group-kill. Reading main.go:4992 and browser_backend.go:271 now.

<small>took 1m 40s</small><br>
**AGENT**

> Gap fix is green (test failed before, passes after). Running the full test suite now -- about 3 minutes.

<small>took 2m 59s</small><br>
**AGENT**

> Gap fixed and committed (853ca22fa). Now bumping to 2.36.0 and pushing; I'll ask for the 6-digit code once the binaries are built.

<small>took 2m 29s</small><br>
**AGENT**

> Open this now -- it expires in 5 minutes:
>
> [REDACTED npm login URL]
>
> Step 5 of 7 done. Finished so far:
> 1. v2.36.0 notes written.
> 2. Gap fixed: local session teardown now kills the whole browser process group (commit 45c46ffa8, test failed before, passes after, `make test` green).
> 3. Version bumped to 2.36.0.
> 4. Both commits pushed to main (c9fe479cf).
> 5. All 6 platform binaries built and stamped 2.36.0.
>
> Left: log in, then publish with a fresh code.
>
> After the login page finishes, send me a fresh 6-digit code from your authenticator. Do not send it early -- codes expire in 30 seconds and I will need to ask again.

[Quick replies]
- Logged in -- code coming next
- Send a new login URL
- Stop here

**USER**

> Was it a tdd fix? Does our tests cover both types of browser?

<small>took 6.4s</small><br>
**AGENT**

> Checking which teardown paths have tests. The login prompt is idle on "Press ENTER" -- harmless, the URL still works.

<small>took 18.5s</small><br>
**AGENT**

> Yes, test-first. And yes, both browser types now have a test for the shutdown fix.
>
> Test-first, with the evidence:
> 1. Wrote the test, ran it against the old code: failed -- `child PID 1548 outlived stopSessionBrowser (only the group leader was killed)`.
> 2. Made the one-line change.
> 3. Re-ran: passed. Then the whole suite: green.
>
> The test starts a program that spawns a second program underneath it, then checks that shutting the session down kills both. That is exactly the leftover process that was squatting the port.
>
> Coverage, both types:
> 1. **Shutdown kills the whole group** -- covered for both. Separate service: `TestStopKillsForkedChildren`. Built-in: `TestStopSessionBrowserKillsProcessGroup`, the one I just added.
> 2. **Starting the browser and checking it came up** -- one test set, and it covers both, because both types call the same startup code.
> 3. **Idle cleanup and self-healing** -- separate service only, tested there (`TestReapIdle*`, `TestCDPProxyRecovers*`). The built-in type does not have these features, so there is nothing to test.
>
> Blank spot worth naming: every one of these runs against stand-in programs, not a real browser. Nothing in the automated suite starts actual Chromium. The real-browser proof for the separate service was done by hand (8 out of 8 replacements came back working); the built-in fix has not had that hands-on check.
>
> Next step: the login page is still waiting. Open it, then send me a fresh 6-digit code.
>
> [REDACTED npm login URL]

[Quick replies]
- Logged in -- code coming next
- Send a new login URL
- Hand-test the built-in browser first
- Stop here

**USER**

> Logged in -- code coming next

<small>took 6.5s</small><br>
**AGENT**

> Send the 6-digit code now -- login confirmed as `choonkeat`, everything else is ready.
>
> Step 6 of 7. The publish runs the moment your code arrives: 7 packages, about 40 seconds. If the code expires mid-run I will ask for one more.

[Quick replies]
- 000000

**USER**

> [REDACTED]

<small>took 1m 24s</small><br>
**AGENT**

> v2.36.0 is live. Upgrade with `npm install -g swe-swe@2.36.0`.
>
> All 7 packages confirmed on npm:
> 1. `swe-swe` 2.36.0
> 2. `@choonkeat/swe-swe-linux-x64`, `-linux-arm64`
> 3. `@choonkeat/swe-swe-darwin-x64`, `-darwin-arm64`
> 4. `@choonkeat/swe-swe-win32-x64`, `-win32-arm64`
>
> Git: two commits pushed to main -- `45c46ffa8` (the gap fix) and `c9fe479cf` (release 2.36.0).
>
> One thing left undone, and it needs a real browser, not a test: the built-in browser's shutdown fix has never been run against actual Chromium. About 15 minutes -- start a test container, open two sessions in a row on the same slot, confirm the second one's Agent View pane is not blank. Want me to do that now?

[Quick replies]
- Yes, hand-test it now
- No, we're done
- Save the session log and end

**USER**

> /commit-log-then-end

<small>took 2.0s</small><br>
**AGENT**

> Saving the chat log, scrubbing it, committing it alone, then ending the session.

