# Worktree Merge Strategies

When a worktree session exits cleanly, swe-swe prompts you to merge the changes back to the target branch. The `--merge-strategy` flag controls how this merge is performed.

## Available Strategies

### merge-commit (default)

Rebase then merge with commit. This strategy:

1. Attempts to rebase the worktree branch onto the target branch
2. Creates a merge commit with `--no-ff` (no fast-forward)
3. If rebase fails due to conflicts, falls back to a regular merge commit

This preserves all individual commits from the worktree while creating a clear merge point in history.

```
*   Merge branch 'fix-bug' (merge commit)
|\
| * Fix the actual bug
| * Add test for bug
|/
* Previous commit on main
```

### merge-ff

Fast-forward when possible. This strategy:

1. Performs a standard `git merge` without forcing a merge commit
2. If the target branch hasn't moved, commits appear directly on the target branch
3. If branches have diverged, creates a merge commit

```
* Fix the actual bug
* Add test for bug
* Previous commit on main
```

### squash

Squash all commits into one. This strategy:

1. Squashes all worktree commits into a single commit
2. Creates one clean commit on the target branch
3. Individual commit history from the worktree is lost

```
* Merge branch 'fix-bug' (single squashed commit)
* Previous commit on main
```

## Configuration

Set the merge strategy when initializing a project:

```bash
# Use default (merge-commit)
swe-swe init

# Use fast-forward
swe-swe init --merge-strategy=merge-ff

# Use squash
swe-swe init --merge-strategy=squash
```

The strategy is saved in `.swe-swe/init.json` and persists across sessions.

## Recommendations

- **merge-commit** (default): Best for preserving agent work history. Each commit shows a step in the agent's reasoning.
- **merge-ff**: Good for simple, linear workflows where you want minimal merge commits.
- **squash**: Good for keeping main branch history clean, but loses individual commit details.
