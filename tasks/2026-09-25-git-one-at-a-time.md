# Branch cards stop overloading the box; git runs one command at a time

**Date**: 2026-09-25
**Status**: PLANNED, not started
**Estimate**: about 2.5 days (A: 0.5, B: 0.5, C+D: 1.5)

## Why

Live incident on a user box, one large repo (~90 local branches,
~44 branch folders), server logs 2026-09-25:

- Just opening New Session ran `fetch --all` twice at once plus
  branch-check-all (2 git calls per branch, 4 at a time): ~180 git commands.
  Within 10 s: 147 checks failed, fetches failed with
  "Too many open files in system" (ENFILE). Reproduced right after a restart,
  so it is a burst, not a leak (file-nr was 3200 once quiet).
- Tapping [x] on ~12 cards started ~12 folder deletes at once on one disk;
  two failed with ENFILE. Cards sat on "checking..." for minutes.
- Every "Couldn't check what would be lost" came from this: status/rev-list
  killed at the 5 s limit (`signal: killed`) or failing (`exit status 128`).
- Unexplained: file-max reads as unlimited in the container, yet ENFILE hit.
  Some lower limit exists on that host; not located.

## Decisions already made

Dialog:
- Opening New Session lists branches only: the 4 local git calls of
  gatherBranchFacts, no fetch, no per-branch checks.
- No [x] on cards and no count badges in the list.
- Tapping a card picks it (as today) and checks only that branch
  (2 git calls). The card then shows what deleting would lose
  ("Nothing to lose" / "3 saved changes exist only here" / folder edits /
  folder size) and a Delete button.
- Tapping another card cancels the pending check (leaves the git line; if
  already running, git is killed).
- Delete re-checks before deleting; confirm and Undo stay as today.
- Accepted cost: bulk cleanup is 2 taps per branch (pick, Delete).

Online sections:
- Each online section downloads only when expanded, only that remote
  (`git fetch <remote>`), showing "Fetching..." in the section.
  Not "fetch all on first expand": that costs the same, all at once,
  including sections never opened.
- Never two downloads of the same remote at once (share the in-flight one).
- 10 s limit. On failure: "Couldn't reach <remote>. [Retry]".
- A stale remote copy is safe for delete checks: it can only over-warn
  ("only here" when it is also online), never under-warn.

New branch name check:
- Before creating a new branch, ask the remote about that one name
  (`git ls-remote <remote> refs/heads/<name>`), so a name that exists online
  but was never downloaded is used instead of shadowed by a new branch.
- Never hangs: GIT_TERMINAL_PROMPT=0 (fail fast on missing login), 10 s hard
  limit, git killed on timeout (killGitGroupOnCancel).
- While running, Start shows "Checking <remote>..." with Cancel.
- On fail / timeout / Cancel: "Couldn't reach <remote> to check if this name is
  taken. [Start anyway] [Back]". Start anyway = today's behaviour.

Git queue:
- Exactly one function in swe-swe-server starts git: `runGit`. A linter test
  fails the build on any other git start.
- Per project, git commands run strictly one at a time, in arrival order.
  Reads do not share the line.
- Two lines per project: **local** (reads/writes the repo on this box) and
  **network** (fetch, ls-remote, clone, push). A slow remote only holds up
  other network commands, never checks/deletes.
- A caller waits with its ctx; if ctx ends while waiting it leaves the line
  and its command never runs.
- Only swe-swe-server's own git calls are covered. Git run by agents inside
  sessions is not queued.

## Design: the queue

### The key: which line a call joins

A project and all its branch folders (worktrees) share one `.git` store, and
`worktree add/remove`, `branch -D` all write there. So the key is the
repo's **common git dir** (plus local/network), resolved from the filesystem,
not by running git:

- `<dir>/.git` is a directory -> key = that directory.
- `<dir>/.git` is a file (`gitdir: X`) -> read `X/commondir` if present,
  else strip `/worktrees/<name>` from X.
- Walk up parents until found. Not found (clone target, `git init` of a new
  folder, global `git config`) -> key = the cleaned dir itself.

### The helper (new file `git_run.go`)

```go
type gitCall struct {
    Dir      string    // required; replaces both "-C dir" and cmd.Dir
    Args     []string
    Network  bool      // joins the project's network line
    Env      []string  // nil = inherit
    Stdin    io.Reader
    Combined bool      // CombinedOutput instead of Output
}

func runGit(ctx context.Context, c gitCall) ([]byte, error)
```

- Line per key: `map[key]chan struct{}` (buffer 1) under a mutex. Waiting is
  `select { case line <- struct{}{}: ... case <-ctx.Done(): return ctx.Err() }`.
  Go channel waiters are not strictly FIFO; switch to a ticket queue if
  order matters in practice.
- Always `exec.CommandContext` + `killGitGroupOnCancel` (moved here from
  clone_cred.go), so a cancelled or timed-out git is killed and frees the line.
- Network calls always get GIT_TERMINAL_PROMPT=0.
- Every caller must pass a ctx with a deadline; `runGit` refuses one without
  (error + log naming the caller). This stops a stuck `fetch --all` (no
  timeout today, main.go ~10579) from holding a line forever.
- Logging: if wait > 1s or run > 5s, log key, args[0..1], wait, run, exit
  status. Never log Env (credentials).
- Log git's stderr on failure (today `Output()` drops it, so "exit status
  128" hid the real reason).
- The line is held only inside runGit, so nested calls cannot deadlock.

Injection points `branchGit` (branch_cards.go) and `branchCheckGit`
(branch_check.go) stay as types; their defaults call `runGit`.

### The linter (new test `git_run_lint_test.go`)

`go/parser` + `go/ast` over every non-test `.go` file in swe-swe-server:

- Fail on `exec.Command` / `exec.CommandContext` whose program argument is
  the literal `"git"`, a constant equal to `"git"`, or ends in `/git`.
- Fail on `"sh"`/`"bash"` with `-c` whose script string contains `git `.
- Allowed only inside `git_run.go`. Error names file:line, says "use runGit".
Runs in `make test`.

## Phase A: dialog lists only; pick -> check -> Delete (~0.5 day)

1. Dialog open: drop `checkAllBranches` and the background
   `refreshBranchesInBackground` fetch. Keep the endpoint for now.
2. Remove [x] and count badges from cards.
3. Pick a card -> POST `/api/repo/branch-check` for that branch (existing
   endpoint), with AbortController; a new pick aborts the old request.
   Server side the request ctx cancels the git call.
4. Add folder size to the check result (`du`-style walk with the same
   timeout; "can't tell size" is fine and does not block delete).
5. Picked card shows the result + Delete; Delete calls branch-delete as today.
6. node tests for the card states; `make test`.

## Phase B: online sections fetch on expand + new-name check (~0.5 day)

1. Expanding an online section -> POST fetch for that one remote, 10 s,
   in-flight sharing per remote; "Fetching..." / "[Retry]" in the section.
2. Start with a new branch name -> ls-remote that name first (10 s, Cancel,
   "Start anyway" on failure). If it exists online, use the online branch.
3. `make test`.

## Phase C: runGit + linter in "report" mode

1. Add `git_run.go` with `runGit`, key resolution, two lines, logging.
2. Unit tests: key resolution (plain repo, worktree, nested dir, no repo);
   same key runs one after another; local and network lines of one project
   run together; different projects run together; ctx cancelled while
   waiting -> never starts; ctx without deadline refused.
3. Linter with the current 37 call sites as a known list: passes today,
   fails on any NEW site.
4. `make test` green. Commit.

## Phase D: move all call sites (known list shrinks to zero)

37 call sites in 7 files, one commit per group, `make test` each:

1. branch_cards.go, branch_check.go, branch_delete.go, branch_refresh.go (5).
2. clone_cred.go (2; clone key = destination folder; keep credential env).
3. session_gitconfig.go (3).
4. main.go worktree/branch helpers, ~3570-4790 (22).
5. main.go prepare_repo + scaffold, ~9515-10640 (5); `fetch --all` gets a
   timeout.

Timeouts: local 10 s; network 10 s for ls-remote/fetch of one remote, keep
existing (or 5 min) for clone. Remove the known list; linter is strict.

## Phase E: live check in a test container

1. Repo with ~90 branches, ~40 branch folders, each with a large ignored
   folder (e.g. 50k files).
2. Open New Session: record git commands run (expect 4) and time to list.
3. Pick cards quickly through 10 of them: only the last one's check
   completes; server log shows the others cancelled.
4. Delete 5 branches back to back: one at a time, box stays responsive, no
   ENFILE.
5. Expand an online section with the network blocked: "[Retry]" within 10 s.
   Start a new branch with the network blocked: "Start anyway" within 10 s.
6. Open New Session on another repo during step 4: normal speed.
