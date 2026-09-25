# swe-swe runs git one command at a time per project

**Date**: 2026-09-25
**Status**: PLANNED, not started
**Why**: tapping [x] on ~12 branch cards started ~12 folder deletes at once
(plus the re-checks around each), all on one disk. The dialog showed
"checking..." for a long time and the box slowed down. Nothing limited how
many git commands swe-swe-server starts at the same time.
**Estimate**: about 1.5 days.

## Goal

1. Exactly one function in swe-swe-server starts git: `runGit`.
2. Per project, git commands run strictly one at a time, in arrival order.
3. A caller waits with its ctx; if ctx ends while waiting, it leaves the line
   and its command never runs.
4. A test fails the build if git is started anywhere else.
5. Every call logs its wait time and run time when either is slow.

## Decisions already made

- One line per project, not one for the whole server: a slow network command
  in project B must not freeze the New Session dialog for project A.
- Strictly one at a time inside a project: reads do not share the line.
  Accepted cost: opening the dialog on a repo with many branches gets slower
  (~2 git calls per branch, now in single file).
- Only swe-swe-server's own git calls are covered. Git run by agents inside
  sessions is not queued.

## Design

### The key: which line a call joins

A project and all its branch folders (worktrees) share one `.git` store, and
`worktree add/remove`, `branch -D` all write there. So the key is the
repo's **common git dir**, resolved from the filesystem, not by running git:

- `<dir>/.git` is a directory -> key = that directory.
- `<dir>/.git` is a file (`gitdir: X`) -> read `X/commondir` if present,
  else strip `/worktrees/<name>` from X.
- Walk up parents until found. Not found (clone target, `git init` of a new
  folder, global `git config`) -> key = the cleaned dir itself.

### The helper (new file `git_run.go`)

```go
type gitCall struct {
    Dir    string    // required; replaces both "-C dir" and cmd.Dir
    Args   []string
    Env    []string  // nil = inherit
    Stdin  io.Reader
    Combined bool    // CombinedOutput instead of Output
}

func runGit(ctx context.Context, c gitCall) ([]byte, error)
```

- Line per key: `map[key]chan struct{}` (buffer 1) under a mutex. Waiting is
  `select { case line <- struct{}{}: ... case <-ctx.Done(): return ctx.Err() }`.
  Go channel waiters are not strictly FIFO; if order matters in practice,
  switch to a ticket queue. Start with the channel.
- Always `exec.CommandContext` + `killGitGroupOnCancel` (moved here from
  clone_cred.go), so a cancelled or timed-out git is killed and frees the line.
- Every caller must pass a ctx with a deadline. `runGit` refuses a ctx with no
  deadline (returns an error, logs the caller) -- this is what stops a stuck
  `fetch --all` from holding a project's line forever.
- Logging: if wait > 1s or run > 5s, log key, args[0..1], wait, run, exit
  status. Never log Env (credentials).
- Nested calls: a caller must not call runGit while holding the line. The
  line is held only inside runGit, so this is safe by construction.

### Injection points kept for tests

`branchGit` (branch_cards.go) and `branchCheckGit` (branch_check.go) stay as
types; their default implementations call `runGit`. Tests keep injecting
stalls/fakes.

### The linter (new test `git_run_lint_test.go`)

Go test using `go/parser` + `go/ast` over every non-test `.go` file in
swe-swe-server:

- Fail on `exec.Command` / `exec.CommandContext` whose program argument is
  the literal `"git"`, a constant equal to `"git"`, or ends in `/git`.
- Fail on `"sh"`/`"bash"` with `-c` whose script string contains `git `.
- Allowed only inside `git_run.go`.
- Error message names file:line and says "use runGit".
Runs as part of `make test`.

## Phase 1: helper + linter, linter in "report" mode

1. Add `git_run.go` with `runGit`, key resolution, logging.
2. Unit tests: key resolution (plain repo, worktree, nested dir, no repo);
   two calls same key run one after the other; different keys run together;
   ctx cancelled while waiting -> command never starts; ctx with no deadline
   refused.
3. Add the linter test with the current 37 call sites as a known list, so it
   passes today and fails on any NEW call site.
4. `make test` green. Commit.

## Phase 2: move call sites (known list shrinks to zero)

37 call sites in 7 files. Order, one commit per group, `make test` each:

1. branch_cards.go, branch_check.go, branch_delete.go, branch_refresh.go
   (5 sites) -- the ones behind the slowdown.
2. clone_cred.go (2 sites; clone key = destination folder; keep credential
   env handling exactly as is).
3. session_gitconfig.go (3 sites).
4. main.go worktree/branch helpers, lines ~3570-4790 (22 sites).
5. main.go MCP prepare_repo + scaffold, lines ~9515-10640 (5 sites).
   `fetch --all` at ~10579 gets a timeout (it has none today).

For each site, pick a timeout: local commands 10s, network (clone/fetch)
keep existing or use 5 min. Remove the known list at the end; linter is
strict.

## Phase 3: the dialog tells the truth

1. branch-delete returns quickly with a state per card:
   "checking..." -> "waiting, Nth in line" -> "deleting folder..." -> done.
   Needs the helper to expose queue position per key (small addition).
2. Before confirm, a large folder shows its size ("folder is 1.2 GB").
3. node tests for the card state changes.

## Phase 4: live check in a test container

1. Repo with 12 branch folders, each with a large ignored folder
   (e.g. 50k files).
2. Tap [x] on all 12 quickly. Expect: cards move through waiting ->
   deleting one at a time; box stays responsive; server log shows one git
   at a time for that repo.
3. Open New Session on another repo during this: it opens at normal speed.
4. Time opening the dialog on a 30-branch repo before/after; record it here.

## Open questions

- Does strict one-at-a-time make the dialog open noticeably slower on the
  largest real repo? Phase 4 step 4 answers it; if too slow, revisit
  "reads up to 4".
