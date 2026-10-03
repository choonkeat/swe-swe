<!-- agent-chat export
title: Branch Refresh Cannot Fetch Over Https
date: 2026-08-17
index: 01
slug: branch-refresh-cannot-fetch-over-https
session: 21ddc67b7e25dfc7
agent: claude
version: 0.11.0 (01f4c81)
-->

# Branch Refresh Cannot Fetch Over Https

_2026-08-17 · 01 · claude · agent-chat 0.11.0 (01f4c81)_

**USER**

> On a different install of swe-swe I seem to get “Unable to fetch latest changes. Using cached branches”. Those instances uses git https config in swe-swe session settings. Inside those sessions I can fetch and push. Research and discuss

<small>took 25.8s</small><br>
**AGENT**

> Tracing "Unable to fetch latest changes. Using cached branches" in the source now.

<small>took 1m 42s</small><br>
**AGENT**

> Workaround right now: in any open session on that install, run `git fetch --all`, then reopen the New Session dialog. The branch list will be current and the warning goes away for that repo.
>
> What is actually happening
>
> 1. The New Session dialog asks the server for the repo's branch list, and asks it to refresh from the remote first.
> 2. The server does that refresh itself, as the background program that runs the whole app -- not inside any session.
> 3. Your HTTPS sign-in details (the username and access token you typed into session settings) are stored per session, in the server's memory, and handed out only to programs running inside that session.
> 4. So the server's own refresh has no sign-in details at all. A private repo over HTTPS refuses it, and you get "Unable to fetch latest changes. Using cached branches."
> 5. Inside a session, the same fetch works, because there the sign-in details are wired in. That is why you see the split.
>
> Why this box does not show it: this checkout talks to GitHub over SSH, using a key file the server can read on its own. No typed-in token needed, so the server's refresh succeeds.
>
> What the message costs you: only freshness. "Cached branches" means the list of branches this machine already knew about. A branch someone else pushed in the last few minutes will be missing from the dropdown until something inside a session fetches. Everything else in the dialog works normally.
>
> The exact spot: `cmd/swe-swe/templates/host/swe-swe-server/main.go:4366` runs the refresh as a plain command with no credentials attached. The clone-a-repo feature on the homepage already solved this exact problem -- it borrows a token for one command and throws it away after (`clone_cred.go`). The branch refresh never got that treatment.
>
> Next step, pick one: want me to write up the fix (make the branch refresh borrow a stored token the same way clone does, about an hour including tests), or leave it and just document the workaround?

[Quick replies]
- Write up the fix
- Just document the workaround
- Show me the clone_cred approach first

**USER**

> Write up the fix

<small>took 2.2s</small><br>
**AGENT**

> Writing the fix plan to a task file now (no code changes yet).

<small>took 43.4s</small><br>
**AGENT**

> Design settled (browser supplies the saved token, server borrows it for one fetch). Writing the plan file.

<small>took 41.3s</small><br>
**AGENT**

> Plan written: `tasks/2026-08-17-branch-refresh-https-credentials.md`. No code changed yet.
>
> The fix in one line: the browser already has your saved token in its own storage, so it hands it to the server for that one refresh command, and the server throws it away straight after -- exactly what the clone-a-repo feature already does.
>
> Five steps in the file:
>
> 1. Server reports which website a repo talks to (github.com, your GitLab, and so on), so the browser knows which saved token to send.
> 2. Server accepts that token on the refresh request and uses the existing borrow-then-discard path. Sent in the request body, never in the address, so it never lands in a log.
> 3. Browser attaches the saved token when it asks for a refresh. If the token is rejected, the dialog shows the same paste-a-new-token fields the clone flow already uses, instead of a dead-end message.
> 4. Tests: bad token gives cached branches plus a clearer message; no token behaves exactly as today.
> 5. Regenerate the built-in copies of the changed files (required in this repo after touching those templates).
>
> Two things worth knowing:
>
> - The message is only about freshness. Branches already known to that machine still show; one pushed elsewhere minutes ago will be missing until a refresh succeeds.
> - The current code can also stall waiting for a typed-in username in some setups. Step 2 removes that as a side effect.
>
> Next step: say "implement it" and I will do steps 1-5 in the working tree, about an hour.

[Quick replies]
- Implement it
- Leave it as a plan for now
- Do steps 1-2 only (server side)

**USER**

> Slow branch fetch must not block user from proceeding with New Session. Refreshed list of branches must not corrupt any value that user might have entered in the branch combobox

<small>took 4.7s</small><br>
**AGENT**

> Checking whether the background refresh can clobber the branch box or block Create, then folding both into the plan.

<small>took 1m 25s</small><br>
**AGENT**

> Both requirements are now steps 4 and 5 in `tasks/2026-08-17-branch-refresh-https-credentials.md`. One of them is already a real bug in the current code.
>
> Never blocks Create (step 4)
>
> 1. Already true in the browser: the refresh is fire-and-forget, and Create does not wait for it.
> 2. Not true on the server: the refresh has no time limit, so a stalled connection keeps a git process alive and can hold a lock that makes creating the session's branch fail. Fix: cut it off at 20 seconds and show "Refresh timed out. Using cached branches."
> 3. Also cancel the refresh when you close the dialog, switch repo, or press Create. Worst case becomes a slightly stale list, never a wait.
>
> Never disturbs the branch box (step 5)
>
> 1. Your typed text is already safe -- the refresh replaces the suggestion list only, never the text.
> 2. Real bug found: if the suggestion list is open and narrowed by what you typed, an arriving refresh re-shows every branch and loses your keyboard position, so the next Enter or arrow key can land on a different branch. `combo-box.js:285-292` re-draws with no filter and clears the highlight at line 517.
> 3. Fix: re-apply the filter you already typed and put the highlight back on the same branch if it is still listed. The exact filter expression already exists three lines away in the same file.
> 4. Bonus in the same spot: a successful refresh currently never clears an earlier error message. Fix that too.
>
> Plan is now 7 steps, about 90 minutes.
>
> Next step: say "implement it" and I will do all 7 in the working tree.

[Quick replies]
- Implement it
- Do steps 4 and 5 only (the two bugs)
- Leave it as a plan for now

**USER**

> TDD implement

<small>took 5.2s</small><br>
**AGENT**

> Starting TDD. Step 1 of 7: writing failing Go tests for the remote-host parser.

<small>took 3m 52s</small><br>
**AGENT**

> Server side green (credentials + 20s timeout + remoteHost). Now the combo-box filter/highlight bug, test first.

<small>took 3m 12s</small><br>
**AGENT**

> Combo-box fix green (proved with a real browser: before the fix, Enter after a refresh picked the wrong branch). Now adding the e2e regression test.

<small>took 1m 37s</small><br>
**AGENT**

> Server suite green. Running the full `make test` plus golden regeneration (a few minutes).

<small>took 3m 2s</small><br>
**AGENT**

> All 7 steps written and `make test` + golden update are green. Booting the e2e container to run the New Session dialog tests for real (about 5-10 minutes).

<small>took 11m 43s</small><br>
**AGENT**

> Done, all 7 steps, everything green. Nothing committed yet.
>
> Proof it works, in order:
>
> 1. `make test` passes (whole repo).
> 2. 9 of 9 New Session dialog browser tests pass against a real running container, including the 3 new ones.
> 3. The branch box bug was reproduced first, in a real browser, before fixing: after a refresh landed, pressing Enter picked the wrong thing. Now it picks the branch you were on.
>
> What changed, in plain terms:
>
> 1. The browser now hands its saved sign-in details to the refresh, and the server borrows them for that one command and discards them. Your other install should stop showing the message, as long as the token is saved in that browser (Settings > Git saves it there).
> 2. The refresh gives up after 20 seconds instead of hanging forever, and is cancelled outright when you close the dialog, switch repo, or press Start.
> 3. Killing a stuck refresh needed extra care: git leaves a helper process behind that keeps the connection open, so the whole group is now stopped, not just git itself.
> 4. The suggestion list keeps your typed filter and your keyboard position when a refresh lands.
> 5. A refresh that succeeds now clears an earlier error message instead of leaving it up.
>
> Files: new `branch_refresh.go` plus edits to `clone_cred.go`, `main.go`, `combo-box.js`, `new-session-dialog.js`, and the browser test file. Built-in copies (golden files) regenerated. Details in `tasks/2026-08-17-branch-refresh-https-credentials.md`.
>
> One thing to know: this only helps a browser that has the token saved. On an install where you typed the sign-in details into a session but never saved them in that browser, the message will still appear.
>
> Next step: say "commit it" and I will commit the change with the golden files in one commit.

[Quick replies]
- Commit it
- Show me the diff first
- Leave it uncommitted

**USER**

> Commit it

<small>took 4.6s</small><br>
**AGENT**

> Committing the change (explicit paths only, so nothing from other sessions rides along).

<small>took 30.2s</small><br>
**AGENT**

> Committed on main as `e0f8bfbfe`, not pushed.
>
> 1. 254 files in the commit: 9 real ones plus 245 built-in copies that are generated from them.
> 2. Nothing from other work rode along -- the commit lists only the files for this fix.
> 3. Working tree is clean of these changes now.
>
> To see it: `git show --stat HEAD`.
>
> Next step, pick one: push to GitHub, or add a one-line CHANGELOG entry first.

[Quick replies]
- Push it
- Add the CHANGELOG entry first
- Stop here

<small>took 1666m 57s</small><br>
**AGENT**

> Still parked at the same point: `e0f8bfbfe` committed on main, 1 commit ahead of GitHub, working tree clean.
>
> Say the word and I push (it takes two pushes -- the first rewrites the commit and stops itself by design).

[Quick replies]
- Push it
- Add the CHANGELOG entry first
- Stop here

**USER**

> /commit-log-then-end

<small>took 3.9s</small><br>
**AGENT**

> Running /commit-log-then-end: freezing this session's chat log, scrubbing, committing it, then ending the session.

