<!-- agent-chat export
title: Browser Idle Reap Plan And Stop Before Commit
date: 2026-08-02
index: 02
slug: browser-idle-reap-plan-and-stop-before-commit
session: c37965695bcac2f8
agent: claude
version: 0.8.22 (8131e36)
-->

# Browser Idle Reap Plan And Stop Before Commit

_2026-08-02 · 02 · claude · agent-chat 0.8.22 (8131e36)_

**USER**

> What has changed recently?

<small>took 15.1s</small><br>
**AGENT**

> Reading the recent commit history to summarize what changed.

<small>took 28.2s</small><br>
**AGENT**

> **Latest released version: 2.35.2 (yesterday, Aug 1). One more fix is finished but not yet released.**
>
> Most of the last two weeks went into making the app work properly on phones and tablets, and into cleaning up what happens when you close down a work session.
>
> **1. Phones and tablets (the biggest chunk)**
>
> - Typing box was hidden on iPad. When the on-screen keyboard opened, the app kept using an old, too-tall measurement of the screen, so the box you type into slid off the bottom. Fixed twice: once by measuring the visible area properly (Jul 31 - Aug 1), and again yesterday with a hard limit so an out-of-date measurement can never push it off-screen. That second fix is the one not yet released.
> - The home page now fits a phone. Long titles wrap instead of running off the edge, the page no longer slides sideways on narrow screens, and the buttons for renaming or deleting a session are always visible on touch screens instead of only appearing when a mouse hovers. Desktop looks the same as before.
> - Tapping a text field on an iPhone no longer zooms the whole page in. The text was just under the size Safari treats as "too small to read", which triggers the zoom.
>
> **2. Closing a session**
>
> - When you end a session, it now asks what to do with that session's chat transcript: save it into the project, leave it lying around, throw it away, or cancel. Saving is the default and is marked as recommended; throwing it away is last.
> - Ending is instant now - it no longer freezes while a large transcript is being saved - and a session that's mid-shutdown refuses new people joining. Its button says "Join Ending Session" in red so it's clear what state it's in.
> - You can plug in your own procedure for the save-and-end step instead of the built-in one.
>
> **3. Smaller things**
>
> - The home page tells you when a newer version of the tool has been published, with the upgrade command on hover.
> - You can now copy a session's ID from its settings, and clicking a file link in the chat opens that file in the file browser panel.
> - The "please wait, restarting" screen used to hang forever in some setups because it misread an error from an in-between server as "still running". It now reloads correctly.
> - Agents can now read text out of screenshots and scanned PDFs without extra setup.
> - Several crash-recovery fixes so the remote browser feature comes back on its own after a restart instead of quietly staying broken.
>
> **Next step:** if you want the iPad typing-box fix live, say the word and I'll cut release 2.35.3 (about 10 minutes).

[Quick replies]
- Cut release 2.35.3
- Not yet
- Tell me more about the session-closing changes

**USER**

> Wrt (2) did we add a slash command mention in the prompt?

<small>took 15.7s</small><br>
**AGENT**

> Checking the end-session prompt text for a slash-command mention.

<small>took 29.6s</small><br>
**AGENT**

> **Yes - two mentions, both live since version 2.35.0.**
>
> When you press "Commit the log, then end", the app types a short instruction into the chat for the agent. That instruction (in `cmd/swe-swe/templates/host/swe-swe-server/main.go:9087`) contains:
>
> 1. **The command it runs**: `/swe-swe:commit-session-chat-log` - a slash command is a saved, named instruction you trigger by typing `/` and its name. This is the built-in one that tidies up and saves the transcript.
> 2. **A tip back to you**: after the work is done, the agent is told to say in one line that you can write your own `/commit-log-then-end` command to decide what that button does - your own wording, your own rules about what to leave out, whether to publish it.
>
> And if you have already written that command, the app skips its own instruction entirely and just types `/commit-log-then-end` (line 9134). The name deliberately sits outside the app's own `swe-swe:` naming space, because every startup wipes and re-writes anything in there - yours would be erased.
>
> The tip is worded into the built-in instruction rather than shipped as its own file on purpose: if it were a file, the app would always find it, treat it as yours, and you would never be told the behaviour is yours to change.
>
> **Next step:** want me to write you a starter `/commit-log-then-end` command so you can see the override working? About 5 minutes.

[Quick replies]
- Write the starter command
- No, that's all I needed
- Cut release 2.35.3 instead

**USER**

> Tell me what you’d write but don’t write it into a file

<small>took 13.6s</small><br>
**AGENT**

> Reading the built-in command to draft the override, no file written.

<small>took 37.8s</small><br>
**AGENT**

> Here is what I would put in the file, plus where it would go. Nothing has been written.
>
> **Where it would go:** `/workspace/.claude/commands/commit-log-then-end.md` — this project only. Put the same file in `/home/app/.claude/commands/` instead and every project on this machine uses it.
>
> **The file, exactly as I would write it:**
>
> ```markdown
> ---
> description: Save this session's chat log my way, then end the session
> ---
>
> # Commit log, then end
>
> 1. Call the `chatlog_close` tool. It stops the log growing, writes the final
>    copy, and returns the exact file paths. If the log has no title yet, pass a
>    short `title` in the same call.
> 2. Read the finished log and replace every sensitive value with `[REDACTED]`:
>    passwords, tokens, one-time codes, private hostnames or URLs, anything
>    personal. Check the images the log links to as well -- if a screenshot shows
>    a secret, do NOT commit it: say so and stop here without ending the session.
> 3. Stage ONLY the paths `chatlog_close` returned. Never `git add -A` and never
>    `git add .`. Confirm with `git diff --cached --name-only` that nothing else
>    is staged, then commit as: `docs(agent-chats): <short title>`.
> 4. Do not push.
> 5. End the session: call the swe-swe MCP tool `end_session` with no arguments.
>
> Do not re-open the log after committing it -- re-opening rebuilds the file from
> the unedited history and undoes step 2.
> ```
>
> **The three lines you would actually change:**
>
> 1. Step 3, the commit message wording — `docs(agent-chats):` is just my habit; use your own.
> 2. Step 4 — change to "Push to the current branch" if you want it going up automatically.
> 3. Step 2 — add anything else you never want stored, for example client names or file paths from a private repository.
>
> **How it takes over:** once that file exists, pressing "Commit the log, then end" sends only `/commit-log-then-end` to the agent, and the built-in wording is skipped entirely. Nothing to restart. Delete the file and the built-in behaviour comes back.
>
> One catch worth knowing: the name has to stay outside the `swe-swe:` naming space. Anything saved under that name is wiped and rewritten each time the tool starts up, so your version would disappear.
>
> **Next step:** say the word and I'll write it to the project path so you can try the button — under a minute.

[Quick replies]
- Write it to the project path
- Write it to the machine-wide path instead
- No, that's all I needed
- Cut release 2.35.3

**USER**

> Write it to the machine-wide path instead

<small>took 12.3s</small><br>
**AGENT**

> Writing the file to /home/app/.claude/commands/commit-log-then-end.md (machine-wide).

<small>took 38.2s</small><br>
**AGENT**

> **Done. The file now exists at `/home/app/.claude/commands/commit-log-then-end.md` and applies to every project on this machine.**
>
> It is already picked up - no restart. My own session listed `commit-log-then-end` as available within seconds of the file being saved.
>
> I checked the app's own code to confirm this is the right folder: when you press "Commit the log, then end", the app looks in exactly two places - this machine-wide folder, and the current project's folder - for a file with that name. It found nothing there before; now it finds yours, so it will send only `/commit-log-then-end` and skip its built-in wording entirely.
>
> **What the file tells the agent to do**, in order:
> 1. Freeze the chat log and get back the exact list of files written.
> 2. Read the log, blank out anything sensitive, and refuse to continue if a screenshot contains a secret.
> 3. Save only those files into the project's history, checking first that nothing else got swept in.
> 4. Do not send it anywhere.
> 5. End the session.
>
> **To try it:** press "Commit the log, then end" on any session. To change what it does, open the file and edit it - it is ordinary text, no special format. To go back to the built-in behaviour, delete the file.
>
> **Next step:** press the button on a throwaway session and confirm it commits and ends as written - about a minute.

[Quick replies]
- Show me the file as saved
- Change step 4 to push automatically
- No, that's all I needed
- Cut release 2.35.3

**USER**

> What’s in tasks/ that are up for discussion or implementation?

<small>took 14.8s</small><br>
**AGENT**

> Going through the tasks/ folder to find the plans that are still open rather than already done.

<small>took 1m 2s</small><br>
**AGENT**

> **Four plans are written up, unblocked, and waiting to be built. Everything else in `tasks/` is either finished or waiting on something.**
>
> The folder holds 349 files, but nearly all are records of work already done. Only these are still live.
>
> ## Ready to build now
>
> All four came out of one review on 19 July of the part of the system that runs a web browser for the agent to look at web pages. None has been started.
>
> 1. **Put a ceiling on how much memory that browser part can use.** Right now there is none. It allows 20 browser sessions at once and each uses roughly 1.5 GB, so a busy machine can be drained until it stops responding. About half a day.
> 2. **Clean up browser sessions nobody closed.** A session that is abandoned keeps its slot and its four background programs running forever, and the slot is never handed back. About a day.
> 3. **Stop two sessions on the same machine fighting over the same connection number.** The second one loses, and its Agent View pane quietly fails to load with no error shown to the person using it. About a day.
> 4. **Replace the shared password on that browser part with proper certificates.** Today all callers share one password, it travels unencrypted, anyone holding it can shut down anyone else's session, and if the password is left blank it lets everyone in. Two to three days.
>
> My pick: number 1. It is the cheapest and it is the one that can take the whole machine down.
>
> ## Waiting on something else
>
> - **Making the no-Docker install work like the normal one** (8 items) - deliberately held until you actually run this machine in that mode, so the real gaps show themselves rather than being guessed at.
> - **Making no-Docker the default** - held behind the item above, plus it would fail outright on Windows and quietly lose two features on Mac.
> - **Named shortcuts for preview addresses** (4 items) - deferred follow-up to work already shipped.
> - **Separating browser sessions from each other for untrusted users** - a stub, not scheduled, only matters if one machine serves people who should not see each other.
> - **The full test suite going red on this box** - understood, not a fault in the product, does not block a release.
>
> ## Also open, needs nothing from a plan
>
> The iPad typing-box report from today is fully built and tested. The only step left is the one on a real device: open a session on your iPad in the two-pane layout, scroll the chat, switch apps, rotate, come back, and confirm the typing box is still on screen without reloading.
>
> **Next step:** do that iPad check - under a minute - and tell me if it holds.

[Quick replies]
- Start item 1, the memory ceiling
- iPad check first
- Tell me more about item 4, the certificates
- Cut release 2.35.3

**USER**

> How do we do (2)?

<small>took 12.0s</small><br>
**AGENT**

> Reading the plan for item 2 -- cleaning up browser sessions nobody closed -- so I can explain how it gets built.

<small>took 42.6s</small><br>
**AGENT**

> **Five changes to one part of the program, about a day's work. Nothing has been started.**
>
> Some background first, in plain terms. When an agent needs to look at a web page, a separate program hands it one of 20 numbered places to work in. Each place runs four small programs and a folder of browser data. Handing a place back only happens one way today: the main app politely asks for it back when a work session ends. If the main app crashes, is force-stopped, or its request fails, that place is held forever and its four programs keep running. Twenty of those and the whole thing is full and refuses new work.
>
> **The five changes**
>
> 1. **Stamp each place with the time it was last touched, and add a check that runs on a timer.** Anything untouched for 30 minutes gets shut down and its place handed back. The 30 minutes is a setting you can change, and setting it to zero turns the whole thing off. This is the main fix.
> 2. **Have that same timed check confirm the browser is actually still running.** If it died on its own, the place is freed immediately. Also fix the "is this ready yet?" answer, which today only checks the part that streams the picture -- it can say yes while the browser behind it is dead.
> 3. **Shut down properly when the browser program itself is told to stop.** Right now it skips two setup steps that normally handle this, so leftover pieces of the browser stay behind and are never cleaned up. Moving those two steps earlier fixes it, plus closing every live place on the way out.
> 4. **Sweep leftover browser data folders on startup.** After a crash these stay on disk forever. Nothing needs to survive a restart here, so deleting all of them at boot is safe.
> 5. **Refuse to start with a nonsensical limit.** The limit on how many places exist can currently be set higher than the range of connection numbers reserved for them. Set it too high and places start quietly stealing each other's numbers, which shows up as an unexplained startup failure. Check it at boot and fail immediately with a message saying what to change.
>
> **How we'd know it works** -- four tests, all done by hand on this machine:
>
> - Open some sessions, force-kill one browser, confirm its place comes back within one timer tick.
> - Open a session, force-kill the main app without letting it ask for the place back, confirm the place comes back after 30 minutes.
> - Stop the browser program with live sessions running, confirm nothing is left running afterwards.
> - Restart it with leftover folders on disk, confirm they are gone.
>
> **One thing I'd leave out of this round:** a sixth idea in the plan, making sessions recover on their own after the browser program restarts. Half of it already works; the other half only matters in a setup you are not currently running. It can wait.
>
> **Next step:** say go and I'll build change 1, the timed check, and show you a place being reclaimed -- about two hours.

[Quick replies]
- Go, build change 1
- Build all five
- Just changes 3 and 4 (the quick ones)
- Not yet
- Cut release 2.35.3 instead

**USER**

> If agent tries to use it after we shut it down due to timer, can we automatically bring it up or somehow let agent know what action to take so it can use again?

<small>took 17.1s</small><br>
**AGENT**

> Checking what actually happens today when an agent uses a browser place that has already been taken back, so I can say whether it can come back on its own.

<small>took 1m 28s</small><br>
**AGENT**

> **Yes, we can bring it back automatically. No, we cannot bring it back the way it was. Neither exists today - both need building.**
>
> **What happens today if the timer shuts it down and the agent then tries to use it**
>
> The agent gets a bare connection failure with no explanation, and it stays broken for the rest of that work session. Two reasons, both fixable:
>
> 1. The agent's browser tool asks for a browser exactly once, the first time it is used, and never asks again. So nothing ever requests a replacement.
> 2. Even if it did ask, the main app still believes a browser is running and would answer "already running" without checking. It has no idea the other side threw it away.
>
> **What I would build - three changes, ranked**
>
> 1. **Do not shut down a browser that is being used.** Today the only thing that would count as "being used" is a health check the app makes on its own. Count the agent's actual page requests too, so the timer only ever reaps browsers nobody has touched for 30 minutes. About an hour, and it removes most of the problem.
> 2. **Restart it on next use, without the agent asking.** Every request the agent makes to its browser already passes through the main app. When one fails because the browser is gone, the app quietly asks for a new one, points the agent at it, and retries the request. The agent sees a pause of roughly two to five seconds, then it works. About half a day.
> 3. **Say what happened, in words the agent can act on.** If the retry cannot be done silently, replace the bare connection failure with a sentence: the browser was shut down for being idle, a fresh one is running, previous pages and logins are gone, start again from the page you need. About an hour or two.
>
> **The part that cannot be fixed**
>
> A restarted browser is blank. Open pages are gone, and so is anything you were signed in to, because shutting it down also deletes its saved data. The agent has to redo its steps.
>
> There is an option that softens this: shut the browser down but keep its saved data folder. Then a restart still comes back with no pages open, but signed-in sessions and site settings survive, so the agent usually just reloads the page it wanted instead of signing in again. Cost is disk space for folders that may never be used again, and a sweep to clear old ones.
>
> **Next step:** say which of the three to build. My pick is 1 and 3 first - about three hours together - because 1 stops it happening to anyone actively working and 3 makes the rare case self-explanatory. Number 2 is the nice one but it is the half-day.

[Quick replies]
- Build 1 and 3 first
- Build all three
- Also keep the saved data folder
- Just explain option 2 more
- Not yet

**USER**

> Build all three

<small>took 17.9s</small><br>
**AGENT**

> Starting on all three. First: reading the browser code and the 19 July plan to confirm what exists today before changing anything.

<small>took 2m 57s</small><br>
**AGENT**

> Code read, design settled. Writing change 1 now (activity tracking + the idle timer that respects it), then 2 (auto-restart on next use), then 3 (the explanatory message). Tests after each.

<small>took 12m 12s</small><br>
**AGENT**

> All three built; new tests pass under the race detector. Running the full server suite plus the golden-file refresh now (the changed files ship inside `swe-swe init`).

<small>took 2m 14s</small><br>
**AGENT**

> **All three are built and tested. Nothing is committed yet, and none of it has been tried on the real machine.**
>
> **1. It will not shut down a browser someone is using.** A browser now counts as "in use" while the agent has its connection open at all, not just when it last sent something. This mattered more than expected: an agent opens one connection and keeps it for the whole work session, so counting only new requests would have shut down browsers that were busy.
>
> **2. It brings the browser back by itself.** If the browser is gone when the agent tries to use it, the app quietly gets a new one and re-sends the request. The agent sees a pause, not a failure. Several requests arriving at once share one replacement instead of each asking for their own.
>
> **3. It says what happened.** If no browser can be had at all, the agent gets a sentence: what went wrong, that it should not keep retrying, and to carry on without the browser. Where a fresh browser was started, the message says the old pages and logins are gone and to start again from the page it needs.
>
> **One thing I added that was not in the three.** A person can sit watching the browser for an hour without the agent doing anything, and that watching happens over a channel the shutdown timer cannot see - so it would have shut the browser down in front of them. The app now checks in every two minutes while the pane is open.
>
> **The setting:** 30 minutes of no use before shutdown. Change it with `-browser-backend-idle`, or set it to zero to switch shutdown off entirely.
>
> **Proof so far:** 11 new automated tests, all passing, including under the checker that catches two-things-at-once bugs. The full existing test suite still passes, and the generated-file snapshots are refreshed and reviewed.
>
> **Next step:** I have not run this against the live browser machine - that needs a restart to pick up the new code. Say the word and I'll commit this, then do the live check (about 15 minutes): start a session, force the browser to be shut down, and confirm the agent's next click just works.

[Quick replies]
- Commit it
- Commit, then do the live check
- Show me the shutdown-timer code
- Leave it uncommitted for now

**USER**

> Commit, then do the live check

<small>took 17.8s</small><br>
**AGENT**

> Committing the three browser changes now, then restarting so the live check can run.

**USER**

> stop

**USER**

> Don’t restart

<small>took 24.1s</small><br>
**AGENT**

> Stopped. Nothing was restarted. Checking exactly what state the work is in.

<small>took 26.2s</small><br>
**AGENT**

> **Stopped. Nothing was restarted and nothing was saved into the project's history.**
>
> The work you approved is still sitting there as edited files on disk, exactly as it was:
>
> 1. Four changed files and two new test files, covering all three changes (don't shut down a browser in use, bring it back by itself, say what happened).
> 2. The copies that get shipped to new installs were already refreshed and are marked ready to save. Nothing else is mixed in with them - I checked, it is 196 files and every one belongs to this change.
> 3. The running system is untouched. It is still on yesterday's code, so none of this is live yet.
>
> **Nothing is at risk.** These edits stay on disk until you say otherwise. The only thing that would lose them is someone undoing changes in this folder.
>
> **Next step:** tell me one of these - "save it" (records the work in the project's history, still nothing live, under a minute), or "leave it" (I stop here and you decide later).

[Quick replies]
- Save it
- Leave it
- Show me what changed
- Throw the changes away

**USER**

> /commit-log-then-end

<small>took 8.1s</small><br>
**AGENT**

> Running /commit-log-then-end: freezing this chat log, checking it for anything sensitive, saving it on its own, then ending the session. Your code changes stay on disk, unsaved, as you left them.

