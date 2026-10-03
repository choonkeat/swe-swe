<!-- agent-chat export
title: Branch Cards In New Session Dialog
date: 2026-09-24
index: 07
slug: branch-cards-in-new-session-dialog
session: ed569f3a475e0c40
agent: claude
version: 0.14.0 (3e93905)
-->

# Branch Cards In New Session Dialog

_2026-09-24 · 07 · claude · agent-chat 0.14.0 (3e93905)_

**USER**

> I’d like to create some UX nudge for folks to clean up branches.
>
> Instead of hiding them all behind the combobox, move “existing branches” out as cards to choose (like agents). Each with a `[x]` to allow on the spot deletion (possible? Edge cases?). 
>
> Discuss and /swe-swe:mockup-lo-fi
>
> <div style="display:flex;flex-wrap:wrap;gap:8px;">
> <a href="./assets/2026-09-24-07-1-2ada03d5077b.png" style="flex:0 1 calc(33% - 8px);max-width:calc(33% - 8px);"><img src="./assets/2026-09-24-07-1-2ada03d5077b.png" alt="IMG_3837.png" style="width:100%;height:auto;display:block;border-radius:6px;"></a>
> <a href="./assets/2026-09-24-07-2-e08b8125b182.png" style="flex:0 1 calc(33% - 8px);max-width:calc(33% - 8px);"><img src="./assets/2026-09-24-07-2-e08b8125b182.png" alt="IMG_3838.png" style="width:100%;height:auto;display:block;border-radius:6px;"></a>
> </div>

<small>took 6.1s</small><br>
**AGENT**

> Reading your 2 screenshots and the New Session dialog code, then I'll sketch the branch-cards idea.

<small>took 6.8s</small><br>
**AGENT**

> Found the branch list code. Checking what "delete" would touch (worktrees, running sessions) before sketching.

<small>took 8.6s</small><br>
**AGENT**

> Step 1 of 3 done: plan (breadboard).
>
> Places:
> 1. New Session dialog: Where, then "Start fresh" (new branch name box), then Existing branches as cards, then Agent cards
> 2. Delete confirm, shown inside the card itself
> 3. "Deleted. Undo" strip after delete
>
> Things you can do on a card: tap to pick it, tap [x] to delete. Each card shows a status tag (safe to delete / has unsaved work / session running / only on GitHub).
>
> Next: drawing it (about 5 minutes).

<small>took 39.2s</small><br>
**AGENT**

> Open the sketch: mockups/lo-fi/2026-09/24-new-session-branch-cards.html
>
> Step 2 of 3 done: sketch drawn. 3 screens: (A) dialog with branch cards, (B) tap [x], (C) deleted, with Undo.
>
> **Can we delete on the spot?** Yes. Nothing does it today. The dialog only lists branches, so we'd add one "delete branch" action on the server. About half a day of work.
>
> **Edge cases. Each card gets a tag, and the tag decides what [x] does:**
> 1. **merged** (the work is already in main): deletes at once, and an Undo strip appears.
> 2. **N unsaved** (changes that exist only on this box): asks inside the card first, "3 changes will be lost".
> 3. **in use** (a live session is working in that branch's folder): [x] is greyed out. Deleting would pull the folder out from under the agent.
> 4. **only on GitHub** (no copy on this box): [x] is greyed out. Deleting there would also remove it for everyone else.
> 5. **main / default branch**: never shown as a card.
>
> **Nudges in the sketch:**
> - Show only the 4 most recently used branches, plus "Show 8 more", so the agent cards don't get pushed off screen.
> - A "5 are merged, Clean up 5" strip. It's one tap, and it's safe.
>
> **Your call:** should "only on GitHub" branches appear as cards at all? Hiding them makes the list much shorter. They could still be picked by typing the name into "+ New branch".
>
> Next step (under 2 min): open the file and tell me to change something or commit it.

[Quick replies]
- Commit it
- Hide GitHub-only branches, then iterate
- Iterate: I have changes

**USER**

> “GitHub” is presumptuous? So much info needed, how long will this dialog take to appear?

<small>took 6.2s</small><br>
**AGENT**

> Timing how long each card tag takes to compute on this box's real repo.

<small>took 15.9s</small><br>
**AGENT**

> Speed: the dialog opens as fast as it does now. The branch names come up straight away and each tag fills in a moment later. I timed it on this box: about 0.3 seconds for all 11 branches.
>
> **What the 0.3 s is made of:**
> 1. Which branches are already in main: 0.03 s (one check covers every branch)
> 2. How many saved changes each branch has that main doesn't: 0.07 s (all branches together)
> 3. Edited files not yet saved, in each branch's folder: about 0.07 s per folder (slowest part, and it grows with the number of folders)
> 4. Which branches a live session is using: no extra time (the server already knows)
>
> **To keep it fast:** show the names first and fill the tags in as each finishes. Until a card's tag arrives, its [x] stays greyed out, so nothing can be deleted before its check is done.
>
> **"GitHub": you're right, that assumed too much.** The copy could also be on GitLab, or anywhere else. Fix: the label shows the real site name taken from the repo settings, such as "only on gitlab.com". If there's no site name to show, it says "only online".
>
> Next step (under 2 min): pick one option below.

[Quick replies]
- Update the sketch with both fixes
- Commit as is
- Hide online-only branches entirely

**USER**

> > its [x] stays greyed out, so nothing can be deleted before its check is done.
>
> The [x] can actually be decided immediately? But what happens when you click it before information arrives, need a loading indicator. 
>
> What goes under “merged” group — I’m not sure if that’s immediately upfront or need a per branch processing

<small>took 8.0s</small><br>
**AGENT**

> Working out which checks are instant and which need a per-branch step.

<small>took 7.7s</small><br>
**AGENT**

> Yes: whether [x] shows at all can be decided right away. Only the delete step needs a loading state.
>
> **Known the instant the dialog opens (no waiting):**
> 1. main / default branch: no card
> 2. Online only (no copy on this box): [x] greyed out. We already know this, because the name list itself says which branches have a copy here.
> 3. In use by a live session: [x] greyed out. The server already knows this.
> 4. Every other branch: [x] is active straight away.
>
> **Clicking [x] before the tag arrives:**
> - The card changes to "Checking..." with a small spinner.
> - Then it goes one of two ways: deletes with Undo (nothing would be lost) or asks you to confirm (something would be lost).
> - The check runs again at the moment you click anyway, even if the tag already arrived. An agent might have saved new work since the dialog opened, so an old tag can't be trusted for a delete.
>
> **"Merged" (work already inside main): one check for every branch at once, 0.03 s.**
> Two catches mean "merged" is not quite "safe":
> 1. A merged branch can still have edited files that were never saved, sitting in its folder. That check is per branch (about 0.07 s each), and only for branches that have a folder.
> 2. Some sites squash a branch into main when merging it: they copy the work into main as one new save. The check can't match that, so a branch finished that way shows "N unsaved" even though its work is in main. It won't cause lost work, but it asks you to confirm more often than needed.
>
> Step 2 of 3 done: sketch drawn. Next step (under 2 min): pick an option below. If you choose the first one, I'll add a 4th screen showing "Checking..." and the fixed label for online-only branches.

[Quick replies]
- Update the sketch
- Commit as is
- More questions first

**USER**

> Might be ok to skip merge detection and be simple. 
>
> I’m more concerned about default branch blank vs named branch that is actually default; remote branches vs local branch — I’ve had instances where local branches are accidentally called origin/something and thus became confusing like hell

<small>took 2.5s</small><br>
**AGENT**

> Dropping "merged". Now reading how the code treats blank vs "main", and names like origin/something.

<small>took 8.6s</small><br>
**AGENT**

> Found the cause of the blank vs "main" confusion. Updating the sketch to fix it (about 5 min).

<small>took 21.0s</small><br>
**AGENT**

> Reopen the sketch: mockups/lo-fi/2026-09/24-new-session-branch-cards.html
>
> Step 2 of 3 done: sketch updated. "Merged" is gone. Every [x] tap now does a fresh "checking..." first.
>
> **Blank vs "main": the confusion is real. The code does this today:**
> 1. Blank means "use the workspace folder as it is", on whatever branch that folder happens to be on. That isn't always main.
> 2. Typing "main" while the folder is on main gives the same result as blank.
> 3. Typing "main" while the folder is on another branch makes a second folder, just for main.
>
> So two different inputs can give the same result, and blank can quietly mean "not main".
>
> Fix in the sketch: the blank field becomes a card, "Workspace as it is (on: main)", showing the real branch name. That branch never gets a second card of its own.
>
> **"origin/something" names: the fix in the sketch:**
> 1. Two separate groups: "On this box" and "Online only, on github.com" (collapsed).
> 2. Online-only branches never show the "origin/" part.
> 3. A branch on this box whose name starts with "origin/" gets an "odd name" tag. Its [x] then nudges you to delete it.
>
> Today the dropdown mixes both groups in one list, so a branch on this box called "origin/foo" and an online-only branch "foo" sit side by side.
>
> Next step (under 2 min): open the file, then pick an option below.

[Quick replies]
- Commit the sketch
- Iterate: I have changes
- Plan the build next

**USER**

> > Show 2 more
> Skip that. I want “too many branches” to be visceral
>
> Wrt branch name confusion, what happens when user type main or origin/something into the new branch text field?

<small>took 7.6s</small><br>
**AGENT**

> Removing "Show more" so every branch shows, and adding a screen for what happens when you type into "+ New branch".

<small>took 15.7s</small><br>
**AGENT**

> Reopen the sketch: mockups/lo-fi/2026-09/24-new-session-branch-cards.html
>
> Step 2 of 3 done: "Show more" is gone, so every branch is its own card and a long list really is long. New screen D shows what happens when you type into "+ New branch".
>
> **Today, typing into the branch field does this:**
> 1. "main" while the workspace folder is on main: same as leaving it blank.
> 2. "main" while the folder is on another branch: quietly makes a second folder just for main.
> 3. "origin/foo" when "foo" exists online: the server quietly drops "origin/" and uses "foo". That part is correct.
> 4. "origin/bar" when nothing online is called "bar": it makes a new branch on this box literally named "origin/bar". This is how those confusing names get created.
>
> **In the sketch (screen D), the field only ever makes something new:**
> 1. Type a name that already exists (for example "main"): it picks that card for you and says so.
> 2. Type "origin/foo" where "foo" is online: it picks the online "foo" card.
> 3. Type "origin/bar" with no match: it shows "Names can't start with origin/" and greys out Start.
>
> Rule 3 stops new "origin/..." branches from being created at all. The "odd name" tag deals with the ones you already have.
>
> Next step (under 2 min): open the file and look at screen D, then pick an option below.

[Quick replies]
- Commit the sketch
- Iterate: I have changes
- Plan the build next

**USER**

> it’s not always “origin/“ prefix though — the code gotta know all remotes

<small>took 5.0s</small><br>
**AGENT**

> Checking how the code handles remote names other than "origin" (remote = a named online copy of the repo).

<small>took 16.8s</small><br>
**AGENT**

> Reopen the sketch: mockups/lo-fi/2026-09/24-new-session-branch-cards.html
>
> Step 2 of 3 done: the sketch now covers every remote (a named online copy of the repo, such as "origin" or "upstream"), not just "origin".
> 1. There's one collapsed "Online only" group per remote, for example "origin, github.com" and "upstream, gitlab.com".
> 2. The "odd name" tag and the typing block from screen D apply to any remote's name, not only "origin/".
>
> **The code today is half right:**
> 1. Listing branches and dropping the "upstream/" part of a typed name: both already work for every remote.
> 2. Starting a session on an online-only branch: this only looks in "origin". I found this while checking your question.
>
> **The bug in point 2:** say "fix-x" exists only on "upstream". You pick it from today's dropdown. Instead of copying upstream's work, the server quietly makes a new, empty "fix-x" starting from the workspace's current branch. The agent starts with none of that branch's work. The code that does this is in `cmd/swe-swe/templates/host/swe-swe-server/main.go` around line 4699, and a fix is about 30 minutes.
>
> Next step (under 2 min): pick one option below.

[Quick replies]
- Commit the sketch
- Fix the upstream bug first
- Iterate: I have changes

**USER**

> There are branches, there are worktrees: how is the code going to deal with them listed and delete and undo correctly

<small>took 13.3s</small><br>
**AGENT**

> Checking how branches and their folders relate on this box right now, so the answer uses real examples.

<small>took 16.7s</small><br>
**AGENT**

> Rule: one card per branch. If the branch has a folder, the folder belongs to that card. Deleting the card removes both. Undo brings back both, except for edits that were never saved.
>
> **Terms:**
> - Branch: a named line of saved work.
> - Folder (git calls it a "worktree"): a separate copy of the files where one branch is open for editing. A branch has at most one folder, and many branches have none.
>
> **1. Listing: this box right now has all 3 kinds:**
> - Branch with a folder: browser-backend lives in /worktrees/browser-backend. It shows as one card with a "has folder" tag.
> - Branch with no folder: most of the others. They show as a plain card.
> - Folder with no branch: /worktrees/preview-per-session is a leftover. Its branch is gone and git has forgotten it, but the files are still on disk. These would go in their own "Leftover folders" group, each with [x].
>
> **2. Deleting, in this order (it stops at the first problem):**
> 1. Is a live session using the folder? If so, stop: [x] is greyed out.
> 2. Does the folder have edits that were never saved? If so, ask to confirm.
> 3. Remove the folder.
> 4. Remove the branch name.
> If step 3 fails, the branch is left untouched. You never end up with a branch name that points to a half-deleted folder.
>
> **3. Undo:**
> - Before deleting, the server notes the branch's last saved point and whether it had a folder.
> - Undo recreates the branch at that point, then recreates the folder if there was one.
> - Edits that were never saved cannot come back. So in that case the confirm says "can't be undone" and no Undo strip appears.
>
> Step 2 of 3 done: sketch drawn. It doesn't show the "has folder" tag or the "Leftover folders" group yet.
>
> Next step (under 2 min): pick one option below.

[Quick replies]
- Add both to the sketch
- Commit the sketch as is
- More questions first

**USER**

> What if the workspace directory (usually default branch) is a branch (Claude cli likes to do that)

<small>took 6.7s</small><br>
**AGENT**

> Checking how the server works out which branch is the default one.

<small>took 11.7s</small><br>
**AGENT**

> Next action: decide how the server should know the default branch. My pick is option 1 below.
>
> **The problem:** the server has no fixed idea of the "default branch". It treats whatever the workspace folder is on right now as the default. And this repo doesn't record its default branch on this box either (I checked).
>
> **So when Claude switches the workspace folder to, say, feat/x:**
> 1. "Workspace as it is" shows "on: feat/x". This part already works, because the card uses the real name.
> 2. main has no folder any more, so it shows up as an ordinary card with an [x]. That's dangerous.
> 3. When a side session ends, its "merge into" suggestion (merge = copy that branch's work into another branch) points at feat/x instead of main. This happens today.
> 4. Picking the main card makes a second folder just for main, which is confusing.
>
> **Ways to pin down the default branch:**
> 1. Ask the online copy which branch it treats as the default, and save the answer. One call when the dialog opens, then it's remembered. About 1 hour.
> 2. Guess: main if it exists, otherwise master. Instant, but wrong for repos that use other names.
> 3. A setting you fill in. Always right, but one more thing to set up.
>
> **Once it's known, the sketch changes like this:**
> - The default branch never gets an [x], even when it has no folder.
> - If the workspace isn't on the default branch, its card gets a "not main" tag and a "Switch back to main" button. The button only works when no live session is using the workspace and there are no unsaved edits, so switching can't pull files out from under an agent.
>
> Step 2 of 3 done: sketch drawn. It doesn't show "has folder", "Leftover folders" or "not main" yet.
>
> Next step (under 2 min): pick one option below.

[Quick replies]
- Option 1 + add all 3 to the sketch
- Option 2 + add all 3 to the sketch
- More questions first

**USER**

> Ask online copy means New session dialog is slow

<small>took 7.2s</small><br>
**AGENT**

> It won't slow the dialog down. The dialog already contacts the online copy after it opens, without making you wait. Asking for the default branch rides along on that same background trip.
>
> **How it would work:**
> 1. First time: the dialog opens instantly, as it does now. The background trip that already happens also asks "which branch is the default?" and saves the answer on this box.
> 2. From then on, the answer is read straight from this box. That takes under 0.01 seconds, with no online trip.
> 3. Repos copied from online with the normal method already have the answer saved. This repo just never had it stored.
>
> **Before the answer is known (first open, or when offline):**
> - Guess main, otherwise master.
> - Hide [x] on both main and master for safety, so nothing important can be deleted while the default is still unknown.
>
> Step 2 of 3 done: sketch drawn. It doesn't show "has folder", "Leftover folders" or "not main" yet.
>
> Next step (under 2 min): pick one option below.

[Quick replies]
- OK, add all 3 to the sketch
- More questions first

**USER**

> I get “Unable to fetch latest changes. Using cached branches.” at new session dialog often though

<small>took 3.7s</small><br>
**AGENT**

> Looking into why "Unable to fetch latest changes" shows up so often. Reading the background-trip code and trying it on this box.

<small>took 31.6s</small><br>
**AGENT**

> Couldn't make it fail on this box yet. Checking the server's log for the real reason behind that message.

<small>took 5.0s</small><br>
**AGENT**

> Next time "Unable to fetch latest changes" appears, tell me the time and which repo was picked under "Where". The server writes the real reason to its log, and I'll read it there.
>
> **What I found:**
> 1. Since the last restart (last night, 23:06), the dialog tried to refresh 5 times, and none failed.
> 2. The log from before that restart is gone, so I can't see your earlier failures.
> 3. The message is a catch-all. Timeouts and sign-in problems get their own wording, so this one means "some other error", and the real reason only goes into the log.
> 4. Opening the dialog sometimes starts the refresh twice at the same moment (the log shows pairs). I ran two at once 3 times on this box and nothing failed. It's still a likely suspect when the online copy has new branches to bring in.
>
> **Effect on the default-branch plan:** none. If the refresh fails, the dialog falls back to the guess (main, otherwise master), and [x] stays hidden on both.
>
> Separately: should I make the dialog start only one refresh at a time? About 30 minutes. It's a separate question from the sketch.
>
> Step 2 of 3 done: sketch drawn.
>
> Next step (under 2 min): pick one option below.

[Quick replies]
- Add the 3 items to the sketch
- Fix the double refresh first
- More questions first

**USER**

> /commit-log-then-end

<small>took 2.6s</small><br>
**AGENT**

> Running commit-log-then-end: saving this session's chat log, then ending the session.

