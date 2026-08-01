<!-- agent-chat export
title: Mobile Keyboard Viewport Pinning
date: 2026-07-31
index: 02
slug: mobile-keyboard-viewport-pinning
session: 88c298a9e030d38f
agent: claude
version: 0.8.21 (ad470fa)
-->

# Mobile Keyboard Viewport Pinning

_2026-07-31 · 02 · claude · agent-chat 0.8.21 (ad470fa)_

**USER**

> Rebooted. Forgot what I was testing until I see this screen

<small>took 1m 23s</small><br>
**AGENT**

> **Do this now:** on that phone screen, open Session Settings, scroll to the bottom, and tell me if you can see and tap **End session**. That is the check that matters most.
>
> **What you were testing:** the change you made and rebooted for (saved 12:04 today; the server restarted 12:17, so the new code is running). On the iPhone, the page sometimes believed the screen was taller than it really was, so it drew itself too short. Two things went wrong because of that:
>
> 1. An empty strip appeared between the bottom of the app and the bottom of the phone screen.
> 2. In Session Settings, the bottom row holding **End session** fell outside the drawn area, and no amount of scrolling reached it. Reloading the page was the only way to get it back.
>
> **The three checks:**
>
> 1. Settings: **End session** visible and tappable. (Do this one now.)
> 2. Keyboard open: no empty strip between the message box and the keyboard.
> 3. Switch to another app, come back: the page still fills the screen, no empty strip.
>
> **About your screenshot:** with the keyboard up, the chat sits in a band at the top and there is a large black area below it, down to the address bar. That looks like check 2 still failing. Confirm after check 1 and I will look at it.
>
> **Next step:** open Session Settings and reply with what you see at the bottom.

[Quick replies]
- End session is visible and tappable
- End session is still cut off
- Settings looks fine but the black gap is still there
- Remind me how to open Session Settings

**USER**

> The bug we were fixing seems fixed BUT we have introduced new bug: when I focus into agent chat textarea it goes up so much that it looks like this!

<small>took 1m 25s</small><br>
**AGENT**

> Found the cause. Writing the fix now — about 5 minutes, then you will need one reboot to test it.

<small>took 2m 30s</small><br>
**AGENT**

> Fix written. Rebuilding and regenerating the generated copies now — about 3 minutes.

<small>took 3m 22s</small><br>
**AGENT**

> **Do this now:** reply "reboot" and I will rebuild and restart so you can test on the phone. Takes about 10 minutes. Nothing else works until then — the running server has the old code baked in.
>
> **What was wrong.** When you tap the message box, the keyboard covers the bottom half of the screen. Safari has to move the page up so your typing is not hidden. It cannot scroll the page, because the page is exactly one screen tall, so it slides the *view* down instead — like moving a window frame down over a fixed poster.
>
> This morning's change told the app "be as tall as the part of the screen you can see". It did not tell the app "move down when the view moves down". So the app stayed where it was, the view slid past it, and you saw only the app's bottom edge at the top of the screen with black underneath.
>
> **What I changed.** The app is now pinned to the visible area — it moves with the view instead of staying put. Two small edits, one in the page's style block and one in the screen-measuring code.
>
> **Where we are: step 3 of 5 done.**
> 1. Done: original bug fixed (you confirmed End session is reachable).
> 2. Done: new bug identified.
> 3. Done: fix written, all tests pass, generated copies regenerated.
> 4. Not started: rebuild and restart.
> 5. Not started: you retest on the phone.
>
> Not committed yet — I will commit after your phone confirms it works.
>
> **Next step:** reply "reboot".

[Quick replies]
- reboot
- Wait, explain the fix more
- Commit it first, then reboot
- Not now, I will test later

**USER**

> reboot

<small>took 8.4s</small><br>
**AGENT**

> Step 4 of 5 starting: rebuild and restart. Your session will drop for a few minutes. Reconnect and reload the page when it comes back.

<small>took 1m 0s</small><br>
**AGENT**

> **Save this link before the restart:** [resume this conversation](/api/fork/d7fbe689-7098-4c00-a824-708bcd5ed04e) — it works once the stack is back up.
>
> **Step 4 of 5 in progress.** The rebuild is running now (about 5 more minutes). Nothing has gone offline yet.
>
> Two things I checked before starting:
> 1. No public ports in use, so no outside visitor gets cut off.
> 2. No other sessions running, so nothing else gets interrupted.
>
> When the rebuild finishes I take the stack down, it comes back on its own, and your page will need a reload.
>
> **Reply "go" and I take it down as soon as the rebuild finishes.** That is the only irreversible step, so I am asking once.

[Quick replies]
- go
- Hold off, I am mid-something
- Cancel the reboot

**USER**

> go

<small>took 18.0s</small><br>
**AGENT**

> Got it. Waiting for the rebuild to finish, then taking the stack down. No action needed from you until the page stops responding.

<small>took 10m 39s</small><br>
**AGENT**

> Rebuild done and verified — the fix is in the new build. Taking the stack down now.
>
> Wait about 5 minutes, then reload the page. If the session is gone, use this link: /api/fork/d7fbe689-7098-4c00-a824-708bcd5ed04e
>
> Then test: tap the message box and check that the app fills the screen above the keyboard, with no black strip.

**USER**

> Let’s test what we rebooted for?

<small>took 58.8s</small><br>
**AGENT**

> **Do this now:** on the phone, reload the page, then tap the message box. The chat should fill the screen right down to the top of the keyboard, with no black strip.
>
> Confirmed the new code is live: the program running now was built at 13:44 and contains the fix; the container restarted at 13:47.
>
> **Test, in order:**
> 1. Reload the page.
> 2. Tap the message box. Look for a black strip between the chat and the keyboard. There should be none.
> 3. Type three or four lines. The view should stay put, not jump.
> 4. Open Session Settings, scroll to the bottom, check **End session** is still reachable. (This is the thing we fixed this morning — making sure it did not break.)
>
> If the screen still looks wrong on step 2, close the tab completely and reopen it. Your phone may be holding an old copy of the page.
>
> **Step 4 of 5 done.** Left: your test, then I commit.
>
> **Next step:** reload and tell me what you see when the keyboard opens.

[Quick replies]
- No black strip, looks right
- Still a black strip
- Different problem now
- End session is broken again

**USER**

> That bug is fixed but now when iPhone keyboard comes up because I tap into textarea, the chat content is clipped (bottom of chat is below textarea). Discuss
>
> <div style="display:flex;flex-wrap:wrap;gap:8px;">
> <a href="./assets/2026-07-31-02-4-ab6fb788de6f.png" style="flex:0 1 calc(33% - 8px);max-width:calc(33% - 8px);"><img src="./assets/2026-07-31-02-4-ab6fb788de6f.png" alt="IMG_3164.png" style="width:100%;height:auto;display:block;border-radius:6px;"></a>
> <a href="./assets/2026-07-31-02-5-8ce2425b19c0.png" style="flex:0 1 calc(33% - 8px);max-width:calc(33% - 8px);"><img src="./assets/2026-07-31-02-5-8ce2425b19c0.png" alt="IMG_3163.png" style="width:100%;height:auto;display:block;border-radius:6px;"></a>
> </div>

<small>took 2m 27s</small><br>
**AGENT**

> **Next action: pick A or B at the bottom.**
>
> **What is happening.** The chat page scrolls as one long page, and the typing box is pinned to the bottom edge of whatever part of the page is on screen. When the keyboard opens, the visible area gets shorter — but the page does not scroll itself to compensate. The typing box lands on top of the last few lines of the conversation and hides them. Nothing is lost; dragging the conversation up by hand brings those lines back.
>
> **Why our fix did not cover it.** What we fixed was the outer app — the frame around the chat. That part is now correct: your screenshot shows the app filling the screen down to the keyboard with no black strip. The clipping is inside the chat itself, which is a separate program.
>
> **The fix.** When the visible area changes size, scroll back to the bottom — unless you had deliberately scrolled up to read older messages, in which case leave you where you are. Roughly four lines of code, in `client-dist/app.js` in the agent-chat project.
>
> **The catch.** agent-chat is not part of swe-swe. It is installed from the public package registry, currently version 0.8.21. Either way you will lose this conversation for a minute or two, because the chat program has to restart to pick up the change.
>
> **A.** Build it here and swap it in without publishing. You retest. If good, publish after. About 15 minutes.
> **B.** Make the change and publish 0.8.22 immediately. About 30 minutes, and you need to be present for a one-time login code.
>
> I recommend A — it proves the fix on your actual phone before anything goes public.
>
> **Separately:** the swe-swe fix you just confirmed working is still uncommitted. Want me to commit that first?

[Quick replies]
- A, and commit the swe-swe fix first
- A, skip the commit for now
- B, publish it
- Leave the clipping, it is minor

**USER**

> Wait, so far our changes are in swe-swe (here) or agent-chat repo?

<small>took 14.6s</small><br>
**AGENT**

> **All changes so far are here, in swe-swe. Nothing has been touched in agent-chat.**
>
> Two files, both uncommitted:
> 1. `cmd/swe-swe/templates/host/swe-swe-server/page-templates/index.html` — pins the app to the visible part of the screen.
> 2. `cmd/swe-swe/templates/host/swe-swe-server/static/terminal-ui.js` — measures the visible part and publishes it.
>
> Plus 98 regenerated copies of those two files under `cmd/swe-swe/testdata/golden/` — automatic, already staged.
>
> The agent-chat repo at `/repos/agent-chat/workspace` has no edits from me. The only untracked things there are old chat logs and tool folders from previous sessions, not mine.
>
> **So the split is:** everything you have tested and confirmed working came from swe-swe. The remaining problem — the typing box covering the last lines of the conversation — is the one that would need a change in agent-chat, and I have not started it.
>
> **Next step:** say "commit" and I commit the swe-swe fix, then we decide on agent-chat separately.

[Quick replies]
- commit
- Show me the diff first
- Don't commit yet, do agent-chat first

**USER**

> Write detailed bug report and suggestion into @/repos/agent-chat/workspace/tasks

**USER**

> commit our swe-swe fix and make build

<small>took 50.3s</small><br>
**AGENT**

> **Written:** `/repos/agent-chat/workspace/tasks/2026-07-31-keyboard-hides-last-messages.md` (150 lines, not committed).
>
> **What is in it:**
> 1. The symptom, and how it differs from the swe-swe bug we already fixed.
> 2. Root cause with exact line numbers: `style.css:842` makes the input bar stick to the bottom edge, and `app.js` has no code at all that reacts to the visible area changing size. Confirmed by search: no resize handler exists anywhere in the chat's code.
> 3. A point that would be easy to miss: inside swe-swe the chat is in a frame, so the ordinary resize event fires. In a plain Safari tab the keyboard is an overlay and that event does not fire. Two listeners are needed, not one, or the fix works in one place and silently does nothing in the other.
> 4. The suggested fix, about eight lines, with the code written out.
> 5. Three alternatives considered and why each is worse.
> 6. A five-step manual test, including the case that must NOT change: if you scrolled up to read history, tapping the box must leave you where you are.
>
> Screenshots left out on purpose. They show the dev tunnel address in the Safari bar and that repo is public.
>
> **Where we are:** swe-swe fix done and confirmed by you, still uncommitted. agent-chat fix described but not written.
>
> **Next step:** say "commit" and I commit the swe-swe fix.

<small>took 24.3s</small><br>
**AGENT**

> Committed as b8d18ce02. Running make build now.

<small>took 48.7s</small><br>
**AGENT**

> **Done. Commit `b8d18ce02`, build succeeded (exit 0), working tree clean.**
>
> - 100 files: the 2 real files plus 98 auto-generated copies.
> - Built for all 6 platforms. No warnings.
> - Commit message records what broke, why, the measurement that identified it, and a pointer to the agent-chat bug report for the part still outstanding.
>
> **Where we are:**
> 1. Done: this morning's bug (End session unreachable).
> 2. Done: the strip-at-the-top bug it caused, committed and built.
> 3. Not started: chat content hidden behind the input bar when the keyboard opens. Written up at `/repos/agent-chat/workspace/tasks/2026-07-31-keyboard-hides-last-messages.md`, uncommitted, no code written.
>
> Nothing is pushed. The commit is local only.
>
> **Next step:** pick one -- "push", or "do the agent-chat fix".

[Quick replies]
- do the agent-chat fix
- push
- Stop here for now
- Commit the bug report in agent-chat

**USER**

> Run /swe-swe:commit-session-chat-log to freeze, scrub and commit this session's chat log. When the commit has landed, end this session by calling the swe-swe MCP tool end_session (no uuid needed -- it ends the calling session). If you cannot commit it (for example a screenshot leaks a secret), say so and do NOT end the session. Afterwards, tell the user in one line: they can save their own /commit-log-then-end command to decide what this button actually does -- their own redaction rules, commit message wording, whether to push, what to leave out -- and swe-swe will run that instead of these instructions.

