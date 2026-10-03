<!-- agent-chat export
title: Fix Blank Agent View Pane Supervise Vnc Procs
date: 2026-08-02
index: 04
slug: fix-blank-agent-view-pane-supervise-vnc-procs
session: 259dfaa3860afdbb
agent: claude
version: 0.8.22 (07df0ae)
-->

# Fix Blank Agent View Pane Supervise Vnc Procs

_2026-08-02 · 04 · claude · agent-chat 0.8.22 (07df0ae)_

**USER**

> Rebooted. What are we testing?

<small>took 12.9s</small><br>
**AGENT**

> Checking the last few sessions and commits to find what was queued for post-reboot testing.

<small>took 25.8s</small><br>
**AGENT**

> The browser service did not come back after the restart. Checking why before I report.

**USER**

> Oh our reboot won’t include browser service. Use docker cli

<small>took 13.7s</small><br>
**AGENT**

> Starting the browser service by hand with the docker command line, then checking it answers.

<small>took 1m 11s</small><br>
**AGENT**

> Found the exact start command in restart-loop2.sh. Starting the container now.

<small>took 29.8s</small><br>
**AGENT**

> Browser service is running. It answers with "0 sessions in use, 20 available".
>
> 1. Started it by hand with the docker command copied from the restart script (same settings the automatic restart would have used, including the shared password it needs).
> 2. Checked it responded: healthy.
> 3. It is set to restart on its own if it ever stops.
>
> Not done yet: the actual thing we queued up to test after the reboot. The recent change makes the system recover on its own when a browser gets shut down for sitting unused. Two checks:
> - kill the browser service on purpose and confirm an open Agent View pane comes back to life instead of staying broken.
> - confirm a browser left idle for 30 minutes gets cleaned up.
>
> Next step (about 5 minutes): open an Agent View pane, then I force-stop the browser service and we watch it heal. Want me to run that?

[Quick replies]
- Yes, run the heal test
- Not now
- Just leave it running

**USER**

> Yes, run the heal test

<small>took 15.4s</small><br>
**AGENT**

> Running the heal test: opening an Agent View pane, then force-stopping the browser service to watch it recover.

<small>took 58.1s</small><br>
**AGENT**

> Step 1 of 4 done: browser started and connected (slot 0). Next: force-stopping the browser service.

<small>took 33.4s</small><br>
**AGENT**

> Step 2 of 4 done: killed the browser service; the system re-allocated a fresh browser on its own 8 seconds later. Next: checking the agent's browser actually works again.

<small>took 23.9s</small><br>
**AGENT**

> Step 3 of 4 done: agent browser works again (one retry needed). Next: the second scenario -- browser removed for sitting idle, while the service stays up.

<small>took 2m 6s</small><br>
**AGENT**

> Found a real defect in the second scenario: the browser comes back but its picture feed does not. Confirming it repeats before I write it up.

<small>took 1m 5s</small><br>
**AGENT**

> Heal test passed, and it turned up one real defect.
>
> Steps 1-4 of 4 done.
>
> 1. Started a browser and loaded a page. Worked.
> 2. Force-stopped the whole browser service. It came back and the system gave itself a fresh browser 8 seconds later, with no action from me.
> 3. Removed just this session's browser three separate times, leaving the service running. Each time a replacement appeared within 2 seconds.
> 4. Loaded a page again after each. Worked.
>
> Defect found: one of those three times, the browser came back but its live picture feed did not. The part that streams the picture to the Agent View pane died one second after starting and nothing ever tries again, so that pane would have stayed blank for the rest of the session while everything else looked fine. Cause: the picture streamer is started and never checked, and the replacement browser reuses the same slot about 2 seconds after the old one was killed, which is not always long enough for the slot to be free. Fix is a short wait-and-retry when starting it, plus recording why it failed. File: cmd/swe-swe/templates/host/swe-swe-server/browser_backend.go, the part that starts the streamer around line 388.
>
> Second, smaller thing: right after a browser is replaced, my first browser command fails once with "browser has been closed", then works on the retry. The system heals itself, but the tool I hold does not notice until it is nudged.
>
> Not tested yet: the 30-minute cleanup of browsers left sitting unused, and the keepalive that must stop it from removing a browser someone is actively watching.
>
> Next step (about 10 minutes): restart the browser service with the unused limit set to 1 minute instead of 30, watch an idle browser get cleaned up, and confirm a watched one survives, then put the setting back. Run it?

[Quick replies]
- Yes, run the idle cleanup test
- Fix the blank-pane defect first
- Stop here

**USER**

> Fix the blank-pane defect first

<small>took 12.6s</small><br>
**AGENT**

> Fixing the blank-pane defect: reading the streamer startup code in browser_backend.go around line 388.

<small>took 2m 6s</small><br>
**AGENT**

> Step 1 of 4: writing the failing tests for the picture-streamer startup check.

<small>took 2m 39s</small><br>
**AGENT**

> Step 2 of 4 done: 6 new tests pass. Next: rebuilding the embedded templates and running the full test suite.

<small>took 3m 19s</small><br>
**AGENT**

> Step 3 of 4: full test suite green. Now rebuilding the browser service image so I can prove the fix live with the same kill-and-replace loop that exposed it.

<small>took 5m 18s</small><br>
**AGENT**

> Root cause found and it is deeper than the first fix: killing the picture-streamer leaves behind a forked copy of itself that keeps holding the port, so the next browser cannot start. Fixing that now.

<small>took 3m 28s</small><br>
**AGENT**

> Fix in place and unit-tested (11 tests). Rebuilding the browser service to re-run the live kill-and-replace loop that failed before.

<small>took 4m 5s</small><br>
**AGENT**

> Found a second leak while verifying (dead helper processes never cleaned up in the browser service). Fixed. Rebuilding once more to prove both live.

<small>took 6m 5s</small><br>
**AGENT**

> Blank-pane defect fixed and proven live: 8 out of 8 replacement browsers came back with a working picture feed. Before the fix it broke on the 2nd try and stopped working completely by the 5th.
>
> Steps 1-4 of 4 done.
>
> 1. Found the real cause. When the picture streamer starts, it makes a copy of itself. Shutting a browser down only stopped the original. The copy stayed alive holding the port (the numbered channel the picture travels on), so the next browser could not start its own streamer, and its pane stayed blank.
> 2. Fixed it: shutdown now stops the copy too. Plus two guards -- we now check the streamer really came up and retry if it did not, and we refuse to start one while the old one still holds the port.
> 3. Found a second, slower leak while testing and fixed that too: the browser service never cleared away finished helper programs, so leftovers piled up one per browser and never went away. Count was 8 and climbing; now 0.
> 4. Proof: 11 new automated tests, the whole suite green, and the live 8-round run above.
>
> Changed files, nothing committed yet: cmd/swe-swe/templates/host/swe-swe-server/browser_backend.go and browser_backend_service.go.
>
> Separately: your own Agent View browser is being refused when it tries to reach pages on this box -- about 270 refusals in 2 minutes, saying the browser is not recognised as belonging to your session. Probably a stale record left after a browser gets replaced. Chase it next?
>
> Next step (1 minute): commit these two fixes with their tests.

[Quick replies]
- Commit the fix
- Run the idle cleanup test
- Look into the refused connections
- Stop here

**USER**

> Commit the fix

<small>took 20.3s</small><br>
**AGENT**

> Committing the two browser fixes with their tests. Checking what is staged first.

<small>took 23.5s</small><br>
**AGENT**

> Step 1 of 3: re-running the build and the full test suite before committing, so the recorded files match the code.

<small>took 4m 12s</small><br>
**AGENT**

> Step 2 of 3 done: build, recorded files, and both test suites all green. Committing now.

<small>took 23.6s</small><br>
**AGENT**

> Committed: 53533f69c "fix(browser-backend): supervise VNC procs and kill their process groups" -- 101 files, nothing left uncommitted.
>
> Steps 1-3 of 3 done.
>
> 1. Rebuilt and regenerated the recorded copies of the changed files. No unexpected differences.
> 2. Ran both test suites. All green, including the 11 new checks (five of them cover the exact start-and-retry logic that was added).
> 3. Committed the two changed files plus the new test file and the regenerated copies. Nothing else from the working folder rode along.
>
> Not pushed. Nothing deployed yet -- the fix reaches the running system at the next rebuild and restart.
>
> Next step (about 10 minutes): the leftover test from earlier -- set the unused-browser limit to 1 minute instead of 30, watch an idle browser get cleaned up, confirm one being watched survives, then put the setting back. Run it?

[Quick replies]
- Run the idle cleanup test
- Look into the refused connections
- Push the commit
- Stop here

**USER**

> /commit-log-then-end

<small>took 2.3s</small><br>
**AGENT**

> Running /commit-log-then-end: freezing this session's chat log, scrubbing it, committing it alone, then ending the session.

