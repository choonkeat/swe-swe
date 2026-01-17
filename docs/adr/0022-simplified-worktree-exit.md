# ADR-022: Simplified worktree exit and agent-based commands

**Status**: Accepted
**Date**: 2026-01-09

## Context

The original worktree exit flow showed a modal with "Merge", "Discard", and "Not yet" buttons when a worktree session exited. This was stressful - exiting a process cleanly should feel normal, not require a decision.

Additionally, merge operations can be complex (conflicts, dirty working directory, merge strategy choices) and benefit from conversational handling rather than a rigid modal.

## Decision

### Simplified exit flow

1. **Exit is just exit**: Worktree and non-worktree sessions behave identically
   - Process exits → confirm dialog → return to homepage
   - No special modal for worktrees
   - WebSocket stops reconnecting after exit (user can review terminal output)

2. **Worktrees persist**: Exiting doesn't delete or merge anything
   - Re-enter from homepage anytime
   - Explicit action required for merge/discard

### Agent-based worktree management

Move merge/discard operations to agent commands via `@` file mentions.

#### Two-directory convention

```
agent/                     # Commands ONLY (all @-mentionable, clean autocomplete)
  setup                    # Command - configure credentials, testing
  merge-this-worktree      # Command (worktree only, generated with context)
  discard-this-worktree    # Command (worktree only, generated with context)

.swe-swe/                  # Internal (only subdirectories, no loose files)
  docs/                    # Documentation for agents
    AGENTS.md              # Index - lists commands, current setup
    browser-automation.md
    docker.md
```

Key design choices:
- `agent/` = commands only, `@agent/` autocomplete stays clean
- `.swe-swe/` = internal data, only subdirectories (no loose files at root)
- `AGENTS.md` lives in `.swe-swe/docs/` to avoid autocomplete pollution
- `setup` command injects pointer into user's `CLAUDE.md`/`AGENTS.md` pointing to `.swe-swe/docs/AGENTS.md`
- No extension for commands (cleaner, command-like)
- Worktree commands generated at creation with branch/target context baked in
- Agent handles complexity conversationally (conflicts, dirty state, confirmations)

#### What exists where

**Main workspace (`/workspace/`):**
```
agent/
  setup                    # Only command available in main workspace

.swe-swe/
  docs/
    AGENTS.md
    browser-automation.md
    docker.md
```

**Worktree (`/worktrees/<branch>/`):**
```
agent/
  setup                    # Copied from main workspace
  merge-this-worktree      # Generated with branch/target context baked in
  discard-this-worktree    # Generated with branch context baked in

.swe-swe/
  docs/                    # Copied from main workspace
    AGENTS.md
    browser-automation.md
    docker.md
```

### Removed features

- Worktree exit modal (frontend)
- `/api/worktree/merge` and `/api/worktree/discard` endpoints (backend)
- `--merge-strategy` flag (was only used by removed modal)

## Consequences

Good:
- Exit flow is stress-free and consistent
- Complex operations get conversational handling
- Agent commands work across Claude Code, Cursor, and other `@`-supporting tools
- Users control timing of merge/discard decisions

Bad:
- Merge/discard requires agent interaction (not one-click)
- Breaking change for users expecting exit modal
