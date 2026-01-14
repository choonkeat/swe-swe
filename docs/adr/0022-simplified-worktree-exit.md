# ADR-022: Simplified worktree exit and agent-based commands

**Status**: Superseded (worktree commands removed)
**Date**: 2026-01-09
**Updated**: 2026-01-14

## Context

The original worktree exit flow showed a modal with "Merge", "Discard", and "Not yet" buttons when a worktree session exited. This was stressful - exiting a process cleanly should feel normal, not require a decision.

Additionally, merge operations can be complex (conflicts, dirty working directory, merge strategy choices) and benefit from conversational handling rather than a rigid modal.

## Decision (Original)

### Simplified exit flow

1. **Exit is just exit**: Worktree and non-worktree sessions behave identically
   - Process exits → confirm dialog → return to homepage
   - No special modal for worktrees
   - WebSocket stops reconnecting after exit (user can review terminal output)

2. **Worktrees persist**: Exiting doesn't delete or merge anything
   - Re-enter from homepage anytime
   - Explicit action required for merge/discard

### Agent-based worktree management (REMOVED)

The `merge-this-worktree` and `discard-this-worktree` commands were originally generated in the worktree's `swe-swe/` directory. These have been removed as of 2026-01-14.

Users can now manage worktree merging and discarding using standard git commands conversationally with their agent:
- `git checkout main && git merge <branch>`
- `git worktree remove /worktrees/<branch>`
- `git branch -d <branch>`

### Current directory convention

```
.swe-swe/                  # Internal (only subdirectories, no loose files)
  docs/                    # Documentation for agents
    AGENTS.md              # Index - explains swe-swe, lists commands, current setup
    browser-automation.md
    docker.md

swe-swe/                   # Commands for file-mention agents (Goose, Aider only)
  setup                    # Configure credentials, testing
```

Note: For agents with slash command support (Claude, Codex, OpenCode, Gemini), the `swe-swe/` directory is not created. These agents use `/swe-swe:setup` instead.

### Removed features

- Worktree exit modal (frontend)
- `/api/worktree/merge` and `/api/worktree/discard` endpoints (backend)
- `--merge-strategy` flag (was only used by removed modal)
- `merge-this-worktree` command (agents can guide merge conversationally)
- `discard-this-worktree` command (agents can guide discard conversationally)
- Worktree-specific `swe-swe/` directory generation

## Consequences

Good:
- Exit flow is stress-free and consistent
- Complex operations get conversational handling
- Slash-command agents get cleaner invocation syntax
- Less cluttered workspace for slash-command agents

Bad:
- Merge/discard requires agent interaction (not one-click)
- Breaking change for users expecting exit modal or worktree commands
