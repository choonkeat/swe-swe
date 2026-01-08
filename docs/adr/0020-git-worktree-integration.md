# ADR-020: Git worktree integration for sessions

**Status**: Accepted
**Date**: 2026-01-08

## Context

Named sessions benefit from branch isolation to prevent conflicts when working on multiple features. Full repository clones are wasteful; git worktrees provide lightweight branch isolation.

## Decision

Integrate git worktrees with named sessions:

1. **Worktree creation**: Named sessions auto-create worktrees
   - Location: `/workspace/.swe-swe-worktrees/{sanitized-name}/`
   - Branch priority: existing worktree > local branch > remote branch > new branch

2. **Untracked file copying**: New worktrees receive copies of:
   - `.env`, `.env.local`, `.env.*` - environment variables
   - `.claude/` - Claude Code settings and MCP configs
   - `CLAUDE.md`, `AGENTS.md` - agent instructions (if gitignored)
   - Other agent configs (`.aider.conf.yml`, `.codex/`, etc.)

3. **Worktree re-entry**: Homepage shows existing worktrees as quick-start links
   - Clicking enters existing worktree without prompts
   - Remote branch tracking: if `origin/{name}` exists, worktree tracks it

4. **Exit prompt**: When session in worktree exits cleanly (exit code 0):
   - Modal offers: "Not yet", "Merge to {target}", "Discard"
   - Merge: squash-merge to target branch, delete worktree
   - Discard: delete worktree and branch

5. **Conflict handling**: When name conflicts with existing branch/worktree:
   - Show warning dialog with conflict type
   - Options: "Enter existing" or "Choose different name"

## Consequences

Good:
- True branch isolation without full clones
- Parallel work on multiple features
- Dotfiles/env preserved in each worktree
- Clean exit flow with merge/discard options

Bad:
- Disk usage per worktree (but shared .git objects)
- Requires git repository (non-git projects unaffected)
- Worktree names must be valid branch names
