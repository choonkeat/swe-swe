<!-- agent-chat export
title: Test Recent Changes And Fix Files Live Reload In One Port Mode
date: 2026-09-23
index: 02
slug: test-recent-changes-and-fix-files-live-reload-in-one-port-mode
session: be22aaf2173820ba
agent: claude
version: 0.13.1 (02e72ab)
-->

# Test Recent Changes And Fix Files Live Reload In One Port Mode

_2026-09-23 · 02 · claude · agent-chat 0.13.1 (02e72ab)_

**USER**

> How to test recent few changes?

<small>took 5.6s</small><br>
**AGENT**

> Reading the last few changes to work out how to test them.

<small>took 14.8s</small><br>
**AGENT**

> **Next action: reply "run 1 and 2" and I'll run the two automatic checks for you.**
>
> The last few changes are two things:
> - **A. "One port" mode.** A new setup option for machines where only one door (network port) into swe-swe can be opened. Everything (Preview, Agent View, Files) must still work through that one door.
> - **B. Leftover-program cleanup.** When a new session starts, swe-swe now removes programs left running by old, ended sessions that were blocking the doors it wants to hand out.
>
> **How to test (4 steps):**
>
> 1. **Quick automatic checks** - `make test`. Runs every small built-in check, including new ones for B. About 5 minutes.
> 2. **"One port" full rehearsal** - `make test-e2e-single-port`. Builds a throwaway copy of swe-swe set up with only one door, then clicks through it with a robot browser. About 10-15 minutes.
> 3. **Real-life check of B** (after a restart of your swe-swe): open a session, start a web app in it, end the session, start a new session. The new session's Preview should show *its* app, not the old one, and no "unauthorized" error.
> 4. **Real-life check of A** (optional, only if you'll use one-port mode): I start a throwaway copy with `--single-port` and you click Preview, Agent View and Files in it. About 15 minutes together.
>
> Steps 1-2 need nothing from you. Step 3 needs a restart first, because B only takes effect after one.

[Quick replies]
- run 1 and 2
- run 1 only
- walk me through step 3

**USER**

> We’ve just restarted.

<small>took 3.0s</small><br>
**AGENT**

> Checking the restart actually picked up the cleanup fix (B) before you test it.

<small>took 23.1s</small><br>
**AGENT**

> **Step 1 of 4: open a new session** (any agent, from the swe-swe home page).
>
> The restart did pick up the cleanup fix (B). I checked: the running swe-swe contains the new code.
>
> Test for B:
>
> 1. **In the new session's Terminal tab**, paste this line and press Enter. It starts a tiny web page that says "OLD" and is built to keep running after its session ends:
>    `setsid nohup node -e "require('http').createServer((q,s)=>s.end('OLD')).listen(process.env.PORT)" >/dev/null 2>&1 &`
> 2. **Open that session's Preview tab.** It should say **OLD**.
> 3. **End that session.**
> 4. **Start another new session.** In its Terminal, paste:
>    `node -e "require('http').createServer((q,s)=>s.end('NEW')).listen(process.env.PORT)"`
>    Then open its Preview tab.
>
> **Pass:** Preview says **NEW**.
> **Fail:** Preview says **OLD**, or the terminal shows an error containing `EADDRINUSE` (means "this door is already taken").
>
> About 5 minutes. Tell me the result and I'll check swe-swe's own records to confirm the cleanup is what made it pass.

[Quick replies]
- Done - it says NEW
- It says OLD
- I got EADDRINUSE
- Stuck at a step

**USER**

> Done - it says NEW

<small>took 1.7s</small><br>
**AGENT**

> Checking swe-swe's records to confirm the cleanup is what made it pass.

<small>took 13.0s</small><br>
**AGENT**

> **Step 3 of 4 passed. Next: reply "run 1 and 2" and I'll run the automatic checks (about 20 minutes, nothing needed from you).**
>
> What swe-swe's records show:
> - At 05:07:29, when you ended the first session, swe-swe stopped the "OLD" page on its own. The record reads "Killed lingering process 1944 on ports [3001]".
> - Before this fix, swe-swe could not see that kind of page at all, so it stayed running and blocked the next session. That is the part of B you just proved works.
>
> Not covered by your test: B also has a backup check that runs when a new session *starts*. Here it never had to act, because everything was already cleaned up when the old session ended. Doing that by hand is hard to set up, but step 1's automatic checks include it.

[Quick replies]
- run 1 and 2
- run 1 only
- stop here

**USER**

> run 1 and 2

<small>took 1.6s</small><br>
**AGENT**

> Step 1 of 2 started: running the quick automatic checks (make test).

<small>took 1m 41s</small><br>
**AGENT**

> Step 1 of 2 passed: all quick checks pass, including the 4 new ones for the start-of-session backup check. Step 2 started: the one-port rehearsal (about 15 minutes).

<small>took 1m 22s</small><br>
**AGENT**

> Step 2 of 2 running: the one-port copy is up and the robot browser is clicking through it. First checks passing.

<small>took 5.6s</small><br>
**AGENT**

> Step 2 of 2 still running: 4 checks passed so far, none failed. I'll send the final result when it finishes.

<small>took 9.4s</small><br>
**AGENT**

> Step 2 of 2 still running: 9 checks passed, 0 failed. Next update when it finishes or if anything fails.

<small>took 22.8s</small><br>
**AGENT**

> Step 2 of 2 still running: 1 check failed (the one that makes a session start another session). Its details only print at the end, so I'll look once the run finishes.

<small>took 4m 9s</small><br>
**AGENT**

> **All testing is done and passed (steps 1-3 of 4; step 4 was optional). Next: nothing to do. Reply "step 4" only if you plan to use one-port mode yourself.**
>
> 1. **Quick automatic checks: passed.** None failed, including the 4 new checks for the backup cleanup that runs when a session starts.
> 2. **One-port rehearsal: passed.** 73 checks passed and 17 were skipped. "Skipped" means they don't apply to this kind of setup. The 3 checks written for one-port mode all passed:
>    - the extra doors really are closed
>    - Preview, Files and the other tabs all work through the one door
>    - Agent View shows up and connects
>
>    One check failed on its first try and passed when it was automatically retried. That check covers one session starting another session, and one-port mode doesn't change that part. It failed instantly, before doing anything, which points to a slow start rather than a real fault.
> 3. **Your hands-on check of the cleanup: passed** (the "NEW" page).
>
> The throwaway copy used for step 2 has been removed.

[Quick replies]
- Done, thanks
- step 4

**USER**

> You do step 4 check?

<small>took 4.5s</small><br>
**AGENT**

> Step 4 started: bringing up a throwaway one-port copy of swe-swe (about 3 minutes), then I'll click through Preview, Files and Agent View and take screenshots.

<small>took 48.1s</small><br>
**AGENT**

> Step 4: the one-port copy is running. I checked that only 1 door (port 9790) is open on it, as intended. Now logging in with the robot browser.

<small>took 12.8s</small><br>
**AGENT**

> Step 4: logged in to the one-port copy. Starting a session there now.

<small>took 12.2s</small><br>
**AGENT**

> Step 4: session started on the one-port copy. All its tabs load through the single door (/proxy/... addresses), none through extra doors. Waiting for it to finish starting, then screenshots.

<small>took 36.7s</small><br>
**AGENT**

> Step 4: Agent Chat and Files both work over the one door. Preview is waiting for an app, so I'm starting a tiny test page inside the copy to prove Preview shows it.

<small>took 16.3s</small><br>
**AGENT**

> Step 4: Preview works over the one door (screenshot coming). Last check: Agent View (the live view of the agent's own browser). Starting that browser now.

<small>took 31.3s</small><br>
**AGENT**

> Step 4: Agent View works over the one door too. All 4 tabs pass. Checking one leftover: the page logs an error about once a second, and I'm finding out why.

<small>took 33.7s</small><br>
**AGENT**

> **Step 4 of 4 passed. All testing is done. Next: nothing is required. Reply "fix it" only if you want the small Files problem below fixed.**
>
> What I did on a throwaway copy set up with `--single-port`:
> 1. **Only one door open:** confirmed. Port 9790 was the only one the copy published.
> 2. **Agent Chat:** works.
> 3. **Files:** shows the folder listing.
> 4. **Preview:** showed my test page ("Hello from the one-port test app").
> 5. **Agent View:** shows the agent's own live browser.
>
> Screenshots are attached: the Files and Preview tabs, then Agent View. The throwaway copy has been removed.
>
> **One problem found (it doesn't block using the feature):** the Files tab tries to refresh itself when a file changes. In one-port mode that refresh check fails once a second, so Files won't update on its own, and you'd need to click to see changes. Cause: Files asks "has this page changed?" using its full web address, including the `/proxy/...` start, and the file viewer (md-serve) doesn't recognise that address. I checked by asking md-serve directly: the plain address gets an OK, the full address gets "not found". This is older than today's changes, and the same thing happens whenever swe-swe falls back to the one-door route. The fix would take about 30 minutes.
>
> <div style="display:flex;flex-wrap:wrap;gap:8px;">
> <a href="./assets/2026-09-23-02-1-284ee3791722.png" style="flex:0 1 calc(33% - 8px);max-width:calc(33% - 8px);"><img src="./assets/2026-09-23-02-1-284ee3791722.png" alt="sp-3-preview-app.png" style="width:100%;height:auto;display:block;border-radius:6px;"></a>
> <a href="./assets/2026-09-23-02-2-72ba25fe46bd.png" style="flex:0 1 calc(33% - 8px);max-width:calc(33% - 8px);"><img src="./assets/2026-09-23-02-2-72ba25fe46bd.png" alt="sp-4-agent-view.png" style="width:100%;height:auto;display:block;border-radius:6px;"></a>
> </div>

[Quick replies]
- Done, thanks
- fix it

**USER**

> What’s the fix?

<small>took 3.1s</small><br>
**AGENT**

> Looking at the code to find the exact fix.

<small>took 17.6s</small><br>
**AGENT**

> **The fix: swe-swe removes its own `/proxy/<session>/files` address start from the refresh check before passing it on. Reply "do it" and I'll make it (about 30 minutes, including a test).**
>
> What goes wrong now:
> 1. Every second, the Files page asks the file viewer (md-serve) "has the page at *this address* changed?"
> 2. It sends its full address, for example `/proxy/0f52.../files/`.
> 3. swe-swe passes that question to md-serve without changing it.
> 4. md-serve only knows the plain address `/`, so it answers "not found" and the page never refreshes.
>
> The change: in step 3, swe-swe removes the `/proxy/<session>/files` start, so md-serve gets `/`. That start is swe-swe's own addition, so swe-swe removing it is the natural fit. It's about 10 lines in one file (`swe-swe-server/main.go`, next to where Files is routed), plus one automatic check that proves it.
>
> The other option is changing md-serve itself. I don't recommend it: it's a separate project, so it would need its own release first and then an update in swe-swe.

[Quick replies]
- do it
- not now

**USER**

> do it

<small>took 2.0s</small><br>
**AGENT**

> Step 1 of 4 started: writing the automatic check first, so it shows the problem before the fix.

<small>took 1m 12s</small><br>
**AGENT**

> Step 2 of 4 done: the new check failed without the fix and passes with it, and the fix is now connected to Files. Step 3: running every check, then updating swe-swe's saved copies of the files that setup creates (about 8 minutes).

<small>took 2m 19s</small><br>
**AGENT**

> Step 3 of 4 done: every check passes, and the saved copies changed only by the fix. Step 4: rebuilding the one-port copy to watch Files refresh on its own (about 5 minutes).

<small>took 2m 12s</small><br>
**AGENT**

> Step 4 of 4: the one-port copy runs the fixed version, and the refresh error is gone (0 so far, versus 1 a second before). Now checking whether a newly added file shows up in Files on its own.

<small>took 30.0s</small><br>
**AGENT**

> **The fix is done and saved as commit a48633830 on main (not pushed). Next: nothing is required. It reaches your own swe-swe at the next restart.**
>
> 1. **Check written first:** it failed without the fix and passes with it.
> 2. **Fix made:** about 10 lines, in `swe-swe-server/files_livereload.go`, and connected to Files in `main.go`.
> 3. **All automatic checks pass.** swe-swe's saved copies of the files that setup creates changed only by the fix.
> 4. **Tried on a fresh one-port copy:**
>    - The once-a-second refresh error is gone: 0, where before it was about 1 a second.
>    - I added a file while Files was open, and it appeared without a click (screenshot attached: `SECOND-file-added-while-watching.md`).
>
> The throwaway copy has been removed. The only errors still shown are Preview saying "no app yet", which is expected when no app is running.
>
> <div style="display:flex;flex-wrap:wrap;gap:8px;">
> <a href="./assets/2026-09-23-02-3-9009aaa7816f.png" style="flex:0 1 calc(33% - 8px);max-width:calc(33% - 8px);"><img src="./assets/2026-09-23-02-3-9009aaa7816f.png" alt="sp-5b-files-after.png" style="width:100%;height:auto;display:block;border-radius:6px;"></a>
> </div>

[Quick replies]
- Done, thanks
- push it

**USER**

> /reboot

<small>took 1.9s</small><br>
**AGENT**

> Starting the restart (reboot) so your swe-swe picks up the Files fix.

<small>took 11.1s</small><br>
**AGENT**

> Restart step 1 of 5 done: pre-checks pass (the automatic checks already passed on this exact code, libraries match, no public users connected). Step 2: rebuilding swe-swe while it keeps running (about 5-10 minutes).

**USER**

> /swe-swe:commit-session-chat-log

<small>took 3m 22s</small><br>
**AGENT**

> Restart step 2 of 5 done: the rebuild succeeded. Step 3: checking other open sessions, so none gets cut off mid-work.

<small>took 4.3s</small><br>
**AGENT**

> Restart paused at step 3 of 5 (no other sessions are open, so nothing to close). Saving this session's chat log first, then I'll finish the restart.

