<!-- agent-chat export
title: Bundle Mockup Lo Fi And Screenshot Before After Slash Commands
date: 2026-09-05
index: 03
slug: bundle-mockup-lo-fi-and-screenshot-before-after-slash-commands
session: b23ac3b0d0dd8f32
agent: claude
version: 0.12.0 (4c9dd41)
-->

# Bundle Mockup Lo Fi And Screenshot Before After Slash Commands

_2026-09-05 · 03 · claude · agent-chat 0.12.0 (4c9dd41)_

**USER**

> Looking to add these as swe-swe bundled slash commands. Install locally first so we can try them out refine then make them actually part of swe-swe distro
>
> [attachment: 2 slash-command source files] [REDACTED]

<small>took 5.8s</small><br>
**AGENT**

> Unpacking the zip to see what commands are in it.

<small>took 1m 5s</small><br>
**AGENT**

> Both files read. Writing swe-swe-generic versions now (stripping the other repo's specifics: example file paths and internal system names [REDACTED]).

<small>took 1m 50s</small><br>
**AGENT**

> **Try it now: type `/swe-swe:mockup-lo-fi a login screen` in a new session.** (Commands are loaded at session start, so this session won't see them yet.)
>
> **Step 1 of 2 done: installed locally.** Both files are in `~/.swe-swe/commands/md/swe-swe/`, which is where the swe-swe commands live. Nothing is in the swe-swe source yet, so nothing is committed and a restart of the whole stack would wipe them. That's intentional -- it's the trial phase.
>
> What I installed:
>
> 1. `/swe-swe:mockup-lo-fi` -- unchanged in spirit. I removed the parts that only made sense in the other project: the pointers to two example files that don't exist here, and the claim that this repo's existing mockups are polished. Serving and screenshotting now use swe-swe's own browser and the session's port.
> 2. `/swe-swe:screenshot-before-after` -- renamed from `[REDACTED]` (shorter to type; say the word and I'll rename it back). I cut the paragraphs about that project's own domain workflow and its internal layers [REDACTED], and replaced them with a general instruction: search the repo for whatever else uses the thing you changed, and report each one as affected or untouched, with a file and line number as proof.
>
> One thing I added: the original told the agent to build the picture comparison itself with no starting code. I wrote that code, ran it here on two test images, and pasted the working version into the command. Result attached -- three panels, changed pixels in red, "2440 px changed (3.39%)".
>
> **Next: try `/swe-swe:mockup-lo-fi` in a fresh session and tell me what feels wrong.** Then I'll fold the refined versions into the swe-swe source so every install gets them.
>
> <div style="display:flex;flex-wrap:wrap;gap:8px;">
> <a href="./assets/2026-09-05-03-2-a796a330055e.png" style="flex:0 1 calc(33% - 8px);max-width:calc(33% - 8px);"><img src="./assets/2026-09-05-03-2-a796a330055e.png" alt="out.png" style="width:100%;height:auto;display:block;border-radius:6px;"></a>
> </div>

[Quick replies]
- Will try it in a new session
- Skip the trial -- put them in swe-swe source now
- Rename it back to its original name [REDACTED]
- Show me the full text of both commands

**USER**

> > Serving and screenshotting now use swe-swe's own browser and the session's port.
>
> We don’t actually need that even. _Just mention the relative filepath_ and agent-chat in swe-swe would have a link that user can click and see in Files tab. No serving is needed
>
> I’m also thinking the directory path can nest yyyy-mm/dd-broadcategory-title
>
> 1. Update the prompt with this shorter instruction
> 2. Let’s try it out with you spawning a new session and prompt it to /swe-swe:mockup-lo-fi 3 options for a page

<small>took 2.4s</small><br>
**AGENT**

> Dropping the serve/screenshot steps, switching to yyyy-mm/dd-category-title paths, then spawning a test session.

<small>took 48.7s</small><br>
**AGENT**

> Step 1 of 2 done: prompt updated (no serving, no screenshot, path now mockups/lo-fi/YYYY-MM/DD-category-title.html). Step 2: trial session "Trial mockup-lo-fi command" is running now -- open it in the session list to watch.

<small>took 28.2s</small><br>
**AGENT**

> **Open the session named "Trial mockup-lo-fi command" -- it's asking you a question there and is stuck until you answer.** It's been waiting ~35 seconds. "3 options for a page" didn't tell it which page, so it's asking.
>
> **Step 1 of 2 done: prompt updated.** Three changes:
>
> 1. No web server, no screenshot. It now just tells you the file path relative to the repo, which swe-swe turns into a clickable link to the Files tab.
> 2. Files now land at `mockups/lo-fi/2026-09/06-onboarding-first-run-wizard.html` -- year-month folder, then day plus a one-or-two-word area name plus the title.
> 3. The one-line summary of the command no longer mentions serving or screenshots.
>
> **Step 2 of 2 in progress: the trial.** It read the command and started work in about 10 seconds, so the command itself loads and runs fine. What it does with a vague request is the thing you're about to see.
>
> **Next: answer its question in that session, then tell me what looked wrong.**

[Quick replies]
- Opening that session now
- Kill the trial and re-run it with a specific page
- It shouldn't ask at all -- make it just draw 3 options
- End the trial session

<small>took 2m 43s</small><br>
**AGENT**

> Correction: it wasn't stuck. It finished and wrote `mockups/lo-fi/2026-09/06-homepage-session-list-3-options.html` -- it picked the swe-swe homepage as the subject and drew 3 layouts. Screenshot attached (I took it myself; the command no longer does). Greyscale held, no colour, no real paragraphs, one dark button per option. The dated folder naming worked first try.
>
> <div style="display:flex;flex-wrap:wrap;gap:8px;">
> <a href="./assets/2026-09-05-03-3-11bd610e1d20.png" style="flex:0 1 calc(33% - 8px);max-width:calc(33% - 8px);"><img src="./assets/2026-09-05-03-3-11bd610e1d20.png" alt="lofi.png" style="width:100%;height:auto;display:block;border-radius:6px;"></a>
> </div>

<small>took 18.2s</small><br>
**AGENT**

> **Open `mockups/lo-fi/2026-09/06-homepage-session-list-3-options.html`** -- that's the sketch, click the path to see it in the Files tab.
>
> **Step 2 of 2 done: the trial worked.** From the four words "3 options for a page" it chose the swe-swe homepage, wrote a short plain-text outline first, then drew three layouts on one page: one long list, a list beside a detail panel, and a start form above the sessions. Each has a small flow strip and a one-line trade-off.
>
> What the trial proves:
>
> 1. The file lands where you wanted: `mockups/lo-fi/2026-09/06-homepage-session-list-3-options.html`. Year-month folder, then day, area, title. First try, no correction.
> 2. The roughness rules held. No colour, no invented paragraphs of text, one dark button per layout, no interactive code.
> 3. It never started a server and never took a picture. It just handed over the path, as you asked.
>
> Two things need your call:
>
> 1. The trial session is still open, waiting to ask whether you want changes or a commit. It won't do anything until you answer or I close it.
> 2. `mockups/` is now an untracked folder in the real repo. Nothing is committed.
>
> **Next: say "fold them in" and I'll add both commands to the swe-swe source so every install ships with them (about 20 minutes, includes the regenerated test files).**

[Quick replies]
- Fold them in
- End the trial session and delete mockups/
- Keep the trial session open, I want to iterate
- Show me the final text of both commands first

**USER**

> Session 07b4ce75-b96c-4df3-9c6a-9f036c74890e Worked flawlessly

<small>took 15.4s</small><br>
**AGENT**

> Folding both commands into the swe-swe source now: adding the files, generating the second format other agents use, then regenerating the test snapshots.

<small>took 3m 6s</small><br>
**AGENT**

> **Say "commit" and I'll commit the staged change -- nothing is committed yet.**
>
> **Both commands are now in the swe-swe source and all tests pass.** Anyone who installs or updates swe-swe from here gets `/swe-swe:mockup-lo-fi` and `/swe-swe:screenshot-before-after`. Existing setups pick them up the next time they run the setup step.
>
> What went in:
>
> 1. The two command files, plus a second copy of each in the other format swe-swe keeps for agents that aren't Claude. I checked that format is produced mechanically from the first, so the two can't drift.
> 2. 200 regenerated test snapshot files, about 29,600 added lines. That number is large but boring: swe-swe stores a full expected-output copy for each of its 50 setup variations, so the same two commands appear 100 times over.
> 3. Nothing else changed. I checked the staged list twice -- no other file, and no other session's work rode along.
> 4. Full test run: 10 packages, all pass.
>
> Two loose ends, both waiting on you:
>
> 1. The trial session `07b4ce75` is still open and paused, asking whether to revise or commit its sketch.
> 2. `mockups/2026-09/...` sits in the working folder, uncommitted.
>
> **Next: reply "commit" (I'll commit the source change only, no push, and leave the two loose ends alone).**

[Quick replies]
- Commit
- Commit, then end the trial session and delete mockups/
- Don't commit yet -- show me the diff
- Commit and push

**USER**

> Commit our changes and /swe-swe:commit-session-chat-log

<small>took 2.1s</small><br>
**AGENT**

> Committing the staged source change now, then running the chat-log commit command.

