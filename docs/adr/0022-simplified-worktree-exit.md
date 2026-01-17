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

Move merge/discard operations to agent commands via `@` file mentions:

```
swe-swe/
  AGENTS.md              # Help, available commands, current setup
  setup                  # Conversational environment setup
  merge-this-worktree    # Generated in worktrees with context baked in
  discard-this-worktree  # Generated in worktrees with context baked in
```

Key design choices:
- `swe-swe/` directory (visible, not `.swe-swe/`) for `@swe` autocomplete
- No extension for commands (cleaner, command-like)
- Worktree commands generated at creation with branch/target/strategy context
- Agent handles complexity conversationally (conflicts, dirty state, confirmations)

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
