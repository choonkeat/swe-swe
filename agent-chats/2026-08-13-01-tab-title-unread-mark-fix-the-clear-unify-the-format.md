<!-- agent-chat export
title: Tab Title Unread Mark Fix The Clear Unify The Format
date: 2026-08-13
index: 01
slug: tab-title-unread-mark-fix-the-clear-unify-the-format
session: 97bbab153f66e11b
agent: claude
version: 0.10.1 (507c623)
-->

# Tab Title Unread Mark Fix The Clear Unify The Format

_2026-08-13 · 01 · claude · agent-chat 0.10.1 (507c623)_

**USER**

> Checking our latest change

<small>took 11.9s</small><br>
**AGENT**

> Checking the latest change (tab title turn indicator) - reading the commit and its current state.

<small>took 1m 37s</small><br>
**AGENT**

> Look at this browser tab's title right now - it should read "hourglass 0m.. - <session name>" while I'm working. That is the change, and it is already running on this machine.
>
> Status of the tab-title change, 4 points:
>
> 1. Working here. Both halves are installed and running: the session server (rebuilt today 01:05) and the chat program (started today 01:09). I confirmed the new code is inside both running programs.
>
> 2. Not shipped to anyone else. The chat program published online is still the old version without this change. The copy running here is a local hand-built one that was dropped into the install folder under the same version number 0.10.1. Anyone else installing 0.10.1 gets the old behaviour.
>
> 3. Nothing is uploaded yet. The chat project has 1 unsent change (the tab title one). The swe-swe project has 5 unsent changes, this one included. All still only on this machine.
>
> 4. To make it real for others, two things are needed: give the chat project a new version number (0.10.2) and publish it, then upload both projects' pending changes. About 20 minutes, mostly waiting.
>
> Next step, under 2 minutes: glance at the tab title and tell me whether you see the hourglass and a counting timer.

[Quick replies]
- Yes, I see the hourglass and timer
- No, tab title looks unchanged
- Publish 0.10.2 and push everything
- Just push, don't publish yet

**USER**

> Green dot didn’t go away after I focus here

<small>took 6.8s</small><br>
**AGENT**

> Digging into the green-dot clear-on-focus logic in both halves now.

<small>took 1m 17s</small><br>
**AGENT**

> Found the likely cause. Running a browser test now to confirm it before I change anything.

<small>took 56.6s</small><br>
**AGENT**

> Confirmed the cause by reading the code (browser focus can't be simulated in this container). Writing the fix now - two files.

<small>took 59.6s</small><br>
**AGENT**

> Fix written in both halves. Rebuilding and regenerating the golden test files now (about 2 minutes).

**USER**

> 🎤 Six is in this report or agent checked what is the fix

<small>took 1m 15s</small><br>
**AGENT**

> Unit tests + generated files pass. Booting the test environment to run the tab-title tests for real (about 5 minutes).

<small>took 8.1s</small><br>
**AGENT**

> Answering what the fix is and where it lives.

<small>took 7.4s</small><br>
**AGENT**

> The fix is in both projects, and it's already written.
>
> Here's the cause. The green dot lives on the session tab, which is the outer page. It was set to disappear when that outer page gets your attention. But the chat is a separate mini page embedded inside it. When you click into the chat, the attention goes to the chat, not to the outer page, so the outer page never found out you came back. The dot stayed.
>
> The fix is three parts. One, the chat now tells the outer page the moment you click into it. Two, the outer page listens for that and drops the dot. Three, the outer page also drops the dot whenever you simply return to the tab, which covers the case where you land on nothing in particular.
>
> I also fixed a smaller thing: the test suite was failing before I touched anything, because two picture characters had been typed straight into a test file, which your project forbids. That's now passing.
>
> Where things stand: the internal tests and the generated files all pass. The browser test for this specific behaviour hasn't run yet, because it needs a test environment booted, about five minutes.
>
> Want me to boot it and run that browser test now?

[Quick replies]
- Yes, run it
- Skip it, just commit
- Show me the code change first

**USER**

> Sounds like both project ms know exactly what the page title format is? Or is only agent chat in charge of format or is swe-swe in charge?

<small>took 6.2s</small><br>
**AGENT**

> Checking whether the title format is written once or twice.

<small>took 23.4s</small><br>
**AGENT**

> Both projects have their own copy of the format. Neither is in charge.
>
> 1. The chat project decides the format for its own browser tab, when the chat is opened on its own.
> 2. The session project decides the format for the session tab, the one you are looking at.
> 3. The two copies are identical today: same hourglass, same green circle, same way of writing the elapsed time. I compared them line by line just now.
> 4. Nothing forces them to stay identical. If someone changes one, the other keeps the old look and no test catches it.
>
> What actually crosses between them is only raw facts: is the agent busy, when did it start, did it just finish, and now, did the chat just get your attention. The wording and symbols are built separately on each side.
>
> My recommendation: leave it as is. Handing the finished text over from the chat would not remove the duplicate, because the session tab must still know how to draw the title by itself for anyone running an older chat version. You would end up with both copies plus an extra message.
>
> Next step, your call: boot the test environment and run the browser test for the green dot fix, about 5 minutes. Yes or no?

[Quick replies]
- Yes, run the browser test
- Skip it, just commit the fix
- Actually, remove the duplicate anyway
- Show me the code change first

<small>took 1667m 8s</small><br>
**AGENT**

> Still waiting on one yes/no from you: run the browser test for the green dot fix, about 5 minutes?
>
> Where things stand while you were away:
>
> 1. The fix is written and still intact in both projects. Nothing was lost or overwritten.
> 2. Internal tests pass. The generated files were rebuilt and checked.
> 3. Nothing is committed yet. The changes are sitting in the working area of both projects.
> 4. One caution: 49 rebuilt files are staged, meaning queued for the next save. Other sessions share this same folder, so a save from elsewhere could sweep them up. I checked just now and nothing foreign has crept in.
>
> Next step: say run it, or say commit and I will save both projects without the browser test.

[Quick replies]
- Run the browser test
- Commit both without the test
- Show me the code change first
- Leave it, I'll come back to it

**USER**

> Something more subtle than 🟢

<small>took 17.0s</small><br>
**AGENT**

> Swapping the green circle for a plain small dot in both projects, then rebuilding.

**USER**

> I’m also looking for agent-chat to source of truth for the title format. And swe-swe just follow as instructed — then we will not have dup format knowledge

<small>took 1m 8s</small><br>
**AGENT**

> Done. The tab now reads "● session-name" instead of "🟢 session-name".
>
> 1. The mark is now a small filled dot in the tab's own text colour, not a coloured picture character. It sits in the same place and still only appears when a run finished while you were looking elsewhere.
> 2. Changed in both projects, so the chat's own tab and the session tab match.
> 3. Wording in the code and tests updated too: it is now called the unread mark, not the green mark.
> 4. Rebuilt and re-tested. Everything passes.
>
> The hourglass is untouched, since you only flagged the green one.
>
> If the dot is still not right, the usual quieter options are a hollow circle ○, a middle dot ·, or a small triangle ▸. Say the word and it is a one-line swap.
>
> Next step: run the browser test (5 minutes), or commit both projects now. Which?

<small>took 23.9s</small><br>
**AGENT**

> Making agent-chat the single owner of the title format. Removing swe-swe's copy now.

<small>took 7m 0s</small><br>
**AGENT**

> Done. The format now lives in one place: the chat project.
>
> 1. The chat builds the finished text that goes in front of the tab name, and sends it ready-made. The hourglass, the dot, and the way the elapsed time is written are all spelled out there and nowhere else.
> 2. The session project deleted its copy. It now takes that text and puts the session name after it. It has no idea what a dot or an hourglass is.
> 3. The one-second countdown moved too. The chat re-sends the text every second while the agent works; the session project no longer runs its own clock.
> 4. Older chat versions send nothing, and the tab just shows the plain session name. No crash, no leftover symbols.
> 5. Rebuilt and re-tested. All passing.
>
> One thing stayed on the session side, deliberately: deciding whether you actually saw the reply. The chat can only tell whether the chat pane itself had your attention, so a reply you read with the cursor in the terminal looked unseen to it and would have marked the tab you were staring at. That judgment is now made by the page that can see all the panes. It is a yes or no, not a format.
>
> The tests were rewritten to match: they now feed a made-up prefix and check it is used word for word, rather than checking for an hourglass the session project no longer owns.
>
> Next step: run the browser test (5 minutes), or commit both projects now. Which?

[Quick replies]
- Commit both now
- Run the browser test first
- Show me the code change

**USER**

> Commit both, make build, and run the browser test

<small>took 7.7s</small><br>
**AGENT**

> Step 1 of 3: committing both projects.

<small>took 31.9s</small><br>
**AGENT**

> Step 1 of 3 done: both committed. Step 2: make build.

<small>took 33.1s</small><br>
**AGENT**

> Step 2 of 3 done: build clean. Step 3: booting the test environment for the browser test (about 5 minutes).

<small>took 2m 11s</small><br>
**AGENT**

> All three done. 8 browser tests passed.
>
> 1. Committed. Session project: 088a3e5f9. Chat project: a11a0d8.
> 2. Build clean, all platforms.
> 3. Browser tests: 8 of 8 passed in 14 seconds. Test environment booted and shut down again; nothing left running.
>
> What the 8 tests cover: the tab shows the plain name when idle, uses the chat's text word for word, keeps working with an older chat version, leaves the mark when a run ends while you are away, and drops it on all three ways you can come back. Plus the case that started this: a reply read with the cursor in the terminal leaves no mark at all.
>
> Nothing is uploaded. Both projects are committed locally only, and the session project now has 6 changes waiting to go up.
>
> Next step: say push and I will upload both, about 2 minutes.

[Quick replies]
- Push both
- Not yet, leave them local
- Publish a new chat version too

**USER**

> /commit-log-then-end

<small>took 4.4s</small><br>
**AGENT**

> Saving this session's chat log, then ending the session.

