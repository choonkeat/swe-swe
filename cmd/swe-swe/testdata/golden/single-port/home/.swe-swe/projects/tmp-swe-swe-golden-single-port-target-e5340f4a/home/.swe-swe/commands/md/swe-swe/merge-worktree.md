---
description: Merge a git worktree branch into local main
---

Merge a git worktree branch into local main.

## Steps

### 0. Determine the main worktree, then verify it is on main with a clean working tree

First find the repository's main worktree root: run `git rev-parse --show-toplevel` (or take the `[main]` entry from `git worktree list`). Use that path as `<main-worktree>` for the rest of these steps -- do NOT assume `/workspace`; this command runs in whatever repo the user is currently in.

Run `git branch --show-current` in `<main-worktree>`. If not on `main`, stop and tell the user they must switch to main first.

Run `git status --porcelain` in `<main-worktree>` (excluding untracked files: `git status --porcelain -uno`). If there are staged or unstaged changes, stop and tell the user to commit or stash them first -- the merge requires a clean working tree.

### 1. List all worktrees with status

Run `git worktree list` to get all worktrees. For each worktree that is NOT the main worktree:

- Run `git -C <worktree-path> status --porcelain` to check if it's clean or dirty
- Run `git rev-list --left-right --count main...<branch>` to get ahead/behind counts relative to local main

Present a compact list like:

```
1) feat-x -- clean, +3/-0
   /path/to/wt

2) fix-y -- dirty (2 files), +1/-5
   /path/to/wt
```

Where `+N` is commits ahead of main and `-N` is commits behind main.

If there are no worktrees (other than main), tell the user and stop.

### 2. Ask the user which worktree to merge

Present each worktree branch as a numbered option and wait for the user's choice. If any selected worktree is dirty, warn the user and ask them to confirm or abort before continuing.

### 3. Reconcile the branch, then merge with --no-ff

We prefer a no-fast-forward merge: every integration leaves an explicit merge commit, and main's existing commit hashes are never rewritten (a plain `git rebase` of main onto the branch would rewrite them).

First, if the branch is behind main (the `-N` count in step 1 is non-zero), rebase the **branch** onto main from inside its **own worktree**, so any conflicts -- especially auto-generated golden files -- are resolved off to the side without touching main:

```bash
git -C <worktree-path> rebase main
```

Resolve golden-file conflicts by regenerating, not hand-editing: accept either side (`git checkout --theirs cmd/swe-swe/testdata/golden/` or `--ours`), then `make build golden-update && git add cmd/swe-swe/testdata/golden/`, then `git rebase --continue`. When the rebase is done, run `make test` in the worktree and confirm it is green before merging.

Then, from the main worktree (`<main-worktree>` from step 0), merge with an explicit merge commit:

```bash
git merge --no-ff <branch>
```

Use `--no-ff` even when a fast-forward would be possible. If the merge reports conflicts, resolve them (golden files: regenerate as above) or run `git merge --abort`, inform the user, and stop.

### 4. Clean up

After successful rebase:

```bash
git worktree remove --force <worktree-path>
git branch -d <branch>
```

Use `--force` on worktree removal because worktrees commonly have untracked generated files (`.cache`, `.swe-swe`, etc.) that are not real work.

Report success: which branch was merged, how many commits were added, and confirm the worktree and branch were removed.
