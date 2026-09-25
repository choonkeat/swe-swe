# Branch cards in the New Session dialog

**Date**: 2026-09-25
**Status**: DONE on branch feat/branch-cards (phases 1-6), not merged
**Sketch**: `mockups/lo-fi/2026-09/24-new-session-branch-cards.html` (417474675),
decision table at the bottom of the sketch.
**Discussion**: `agent-chats/2026-09-24-07-branch-cards-in-new-session-dialog.md`
**Estimate**: 3 to 3.5 days.

## Goal

Replace the branch dropdown in the New Session dialog with one card per branch,
each with an `[x]` to delete it on the spot, so a long branch list is visible
(the nudge) and cleaning up is one tap. Behaviour follows the decision table.
The dialog must open as fast as today.

Prerequisites already on main: 73b78bec0 (overlapping refreshes share one
fetch), 944287e15 (online-only branch on a non-origin remote is tracked).

## Decisions already made

- No "show more": every branch on this box is a card.
- No "merged" detection (squash merges make it unreliable).
- "Workspace as it is (on: X)" replaces the blank branch field; X never gets
  its own card.
- Online-only branches: one collapsed group per remote, no `[x]`, no
  `<remote>/` prefix shown.
- "odd name" = a local branch whose name starts with any remote's name + "/".
- "+ New branch" only makes something new: typing an existing name picks that
  card; `<remote>/<existing>` picks the online card; `<remote>/<missing>` is
  blocked with a reason.
- Default branch: read from `refs/remotes/origin/HEAD` (saved on this box);
  the background refresh saves it (`git remote set-head origin --auto`). Until
  known: guess main, else master; neither gets `[x]`.
- The default branch never gets `[x]`, even without a folder.
- Every `[x]` tap re-checks on the server before deleting.
- Leftover folder: always confirm, no Undo.
- "N unsaved" delete keeps Undo (the commits stay in git's store).
- Unsaved folder edits: confirm says "can't be undone", no Undo.
- Deletes never touch an online copy.
- Any check that fails or exceeds 5 s = "can't tell" = treat as "would lose
  something" (confirm required).

## Phase 1: server returns the instant facts (no UI change)

Achieves: `/api/repo/branches` also returns `cards` and `leftovers`; the old
`branches` list stays unchanged so today's dialog keeps working.

Steps:
1. Pure function `buildBranchCards(facts) (cards, leftovers)` in a new
   `branch_cards.go`. Facts: local refs, remote refs, remote names (+ host),
   `git worktree list --porcelain` (branch -> folder), folders on disk under the
   repo's worktrees dir, current branch, default branch (+ guessed flag), live
   session work dirs. No git inside.
2. `gatherBranchFacts(repoPath)`: a fixed number of git calls (about 6),
   independent of branch count.
3. Leftovers = folders on disk not in git's worktree list, or listed with a
   branch that no longer exists. (Today `listWorktrees()` infers the branch from
   the folder name, which cannot detect this.)
4. Default branch: `git symbolic-ref refs/remotes/origin/HEAD`; else guess
   main/master with `defaultGuessed: true`.
5. Wire into `handleRepoBranchesAPI` (main.go ~4510).
6. Background refresh (`runBranchFetch`, branch_refresh.go): after the fetch,
   `git remote set-head origin --auto`, inside the same `branchFetchTimeout`.

Verify (red first, then green):
- Table test per decision-table row: plain, has folder, in use, odd name
  (origin/ and upstream/), online only on 2 remotes, workspace card, default
  saved, default guessed (no x on main and master), leftover folder.
- Git-call count test: 3 branches and 30 branches use the same number of calls
  (inject a counting command runner).
- Local bare repo as "online copy": after one refresh, origin/HEAD is saved and
  the next listing reads it without a network call.

No regression: existing `listBranchNames`, `TestBranchesPostRefresh`, refresh
timeout tests unchanged and green; `make test`; `make build golden-update`
diff shows only the new/changed server files.

## Phase 2: server checks a branch before a delete

Achieves: two checks, per branch (for each `[x]` tap) and for all branches (to
fill tags after the dialog opens).

Steps:
1. `unsavedCommitCount(repo, branch, default)`: commits on the branch that are
   in neither the default branch nor any remote-tracking ref
   (`git rev-list --count <branch> --not <default> --remotes`).
2. `folderEditCount(folder)`: `git status --porcelain` lines; untracked counts,
   ignored does not.
3. `POST /api/repo/branch-check {path, branch}` -> `{unsavedCommits,
   folderEdits, inUse, canTell}`, always fresh.
4. `POST /api/repo/branch-check-all {path}`: all branches, folders checked at
   most 4 at a time.
5. Per-check 5 s timeout -> `canTell: false`.

Verify (red first):
- Check 1: local-only work = N; after pushing to the local bare remote = 0;
  after merging into main = 0.
- Check 2: modified file counts, new file counts, ignored file does not, clean
  = 0.
- Branch whose folder is a live session's work dir -> `inUse`.
- Forced timeout -> `canTell: false`.
- 4 folders run concurrently (injected slow runner: total ~ 1x, not 4x).

No regression: phase 1 + existing branch tests; `make test`. New endpoints
only, nothing calls them yet.

## Phase 3: server deletes, undoes, removes leftovers, switches back

Achieves: four endpoints; none ever contact a remote.

Steps:
1. `POST /api/repo/branch-delete {path, branch, confirmed}`:
   re-run phase 2 checks -> refuse if in use -> if something would be lost (or
   can't tell) and not confirmed, reply `needsConfirm` + reason -> remove folder
   (`git worktree remove`, `--force` only when confirmed) -> `git branch -D`.
   Always refuse: default branch, workspace's current branch, online-only.
   Reply carries the undo note `{branch, sha, folder}`; `undoable: false` when
   folder edits were lost.
2. `POST /api/repo/branch-undo {path, branch, sha, folder}`: `git branch
   <branch> <sha>`, then `git worktree add <folder> <branch>` if it had one.
   Refuse if the branch name exists again or sha is unknown.
3. `POST /api/repo/leftover-remove {path, folder, confirmed}`: folder must pass
   `isValidWorktreePath`-style containment for this repo's worktrees dir, not be
   in git's list, not be a live session's work dir; confirmed required;
   remove then `git worktree prune`.
4. `POST /api/repo/switch-default {path}`: refuse if a live session uses the
   workspace, if it has unsaved edits, or if the default branch is open in
   another folder (one-line reason each); else `git checkout <default>`.
5. Validate every branch name (`git check-ref-format --branch`, no leading
   "-").

Verify (red first):
- Delete: plain gone; with folder both gone; unconfirmed + loss -> needsConfirm
  and nothing removed; in use / default / workspace / online-only refused.
- Safe order: `git worktree lock` the folder so removal fails -> branch still
  exists.
- Undo: branch back at same sha; folder back if it had one; local-only commits
  back.
- Leftover: removed when confirmed; `/etc` and `..` paths refused.
- Switch back: clean workspace switches; each of the 3 refusals.
- Never online: test remotes point at an unreachable URL; any network attempt
  fails the test.

No regression: phases 1-2; existing worktree tests (create, re-enter,
`TestHandleWorktreeCheckAPI`); `make test`.

## Phase 4: dialog shows cards (pick only, no [x])

Achieves: the branch combo (`#branch-combo`, `new-session-dialog.js`
`populateBranches`) is replaced by cards: Workspace as it is, + New branch,
On this box, Leftover folders, one collapsed Online-only group per remote.
Instant tags first; "N unsaved" / edit tags fill in from branch-check-all. The
Start request sends exactly what it sends today.

Steps:
1. `static/modules/branch-cards.js` (pure, node-tested): grouping, order, tags,
   typed-name resolution (screen D rules, any remote name).
2. Render cards in `new-session-dialog.js`; markup/CSS in
   `page-templates/selection.html`.
3. Keyboard: arrows move between cards, Enter picks.
4. Recording "+ New" prefill still pre-picks the right card.
5. Fallback: no `cards` in the reply (older server) -> today's combo.

Verify (red first):
- `branch-cards.test.js`: one test per rule and per decision-table row.
- e2e (`e2e/tests/new-session-dialog.spec.js`): groups render; pick + Start
  sends the same branch value as today; the 3 screen-D typing cases.
- Phone width (~390 px): no horizontal scroll.

No regression: the 16 dropdown uses in `new-session-dialog.spec.js` move to
cards and keep their assertions (refresh carries saved token, slow refresh
does not block Start, background refresh does not disturb typing, default
workspace lists branches, recording + New prefill); `make test` + dialog e2e.

## Phase 5: dialog gets [x], checking, confirm, Undo, Switch back

Achieves: sketch screens B, C and E wired to phase 3.

Steps:
1. Card state machine in `branch-cards.js`: ready -> checking -> (confirm |
   deleting) -> deleted(undo) | failed(reason).
2. `[x]` -> "checking..." spinner -> nothing to lose: delete + Undo strip;
   something to lose: confirm in card with reason ("can't be undone" when
   `undoable: false`, then no Undo).
3. Greyed `[x]` tap shows the one-line reason.
4. Undo strip lives until the dialog closes; Undo restores the card.
5. "Switch back to main" on the workspace card; success updates "on: main";
   refusal shows the reason.
6. Double-tap guard while checking/deleting.
7. Deleting the picked card resets the pick to Workspace as it is.

Verify (red first):
- Node tests for every transition, double-tap guard, server refusal.
- e2e: delete plain + Undo; delete with folder; unsaved-work confirm -> Keep
  leaves all; in use can't delete; leftover removed after confirm; switch back
  works and is refused while a session uses the workspace.
- Slow server: "checking..." shows, dialog stays usable.

No regression: phases 1-4 tests; whole dialog e2e file; `make test`; starting
a session right after a delete or undo works.

## Phase 6: live check in a test container

Steps:
1. Boot per `docs/dev/test-container-workflow.md`.
2. Build a test repo with one of each case: plain; local-only work; folder
   with unsaved edits; in use by a live session; odd name `origin/typo`;
   leftover folder; origin-only and upstream-only branches; workspace on a
   non-default branch.
3. Walk every decision-table row in the MCP browser; note result per row.
4. Time dialog open before vs after on the same repo.
5. Before/after screenshots at desktop and phone width
   (`/swe-swe:screenshot-before-after`).
6. Shut the test container down.

Verify: every row matches, or the mismatch is fixed before done; cards appear
within 0.1 s of today's time and tags within 0.5 s.

No regression: `test-full-e2e` passes; starting a session from each card kind
works in the container, including an upstream-only branch.

## Results (2026-09-25)

Commits on feat/branch-cards: c1b85ecc5 (phase 1), 20577f565 (2), 702c19ac0
(3), 97d4b7f49 (4), ab93c9076 (5), b60b0fbe7 (card order), plus the phase 6
commit that adds this section.

Changes made along the way, beyond the plan:
- switch-default ignores untracked files (checkout keeps them); counting them
  refused the switch after any session had run in the workspace.
- Card order: Workspace, branches (no "On this box" heading), Leftover
  folders, Online only groups, then "+ New branch" last.
- "N unsaved" shows only on cards with a working [x]; a repo with no remote
  otherwise tagged its default branch.
- Online-only groups show the remote name only (no host, e.g. github.com).

Phase 6 live walk (e2e simple container, scripted Playwright, no retries):
25/25 rows matched -- every decision-table row, each [x] flow (plain + Undo,
saved work + confirm + Undo restores the sha, folder clean + Undo restores
folder, folder with edits = "can't be undone" and no Undo, odd name,
leftover), Switch back, starting a session from each card kind (workspace,
local, origin-only tracks origin/<b>, upstream-only tracks upstream/<b>, new),
and 390 px width with no horizontal scroll.

Timing, same repo (27 local branches + 4 folders), median of 7 opens, from
picking Where to the branch field being ready:
- before (d4bd24306): 50 ms
- after: 112 ms, all 30+ cards drawn in the same frame (+62 ms; target was
  within 100 ms)
- "N unsaved" tags (branch-check-all, 31 branches): 136 ms after that
  (target 500 ms); one [x] re-check: 31 ms
