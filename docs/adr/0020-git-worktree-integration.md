# ADR-020: Git worktree integration for sessions

**Status**: Accepted
**Date**: 2026-01-08

## Context

Named sessions benefit from branch isolation to prevent conflicts when working on multiple features. Full repository clones are wasteful; git worktrees provide lightweight branch isolation.

## Decision

Integrate git worktrees with named sessions:

1. **Worktree creation**: Named sessions auto-create worktrees
   - Location: `/worktrees/{sanitized-name}/` (mounted from host's `.swe-swe/worktrees/`)
   - Branch priority: existing worktree > local branch > remote branch > new branch

2. **Untracked file handling**: New worktrees receive untracked dotfiles:
   - **Directories are symlinked** (absolute path to `/workspace`):
     - `.claude/` - Claude Code settings and MCP configs
     - `.codex/`, `.aider/` - other agent config directories
     - Rationale: Shared permissions/settings stay in sync across worktrees
   - **Files are copied**:
     - `.env`, `.env.local`, `.env.*` - environment variables
     - `CLAUDE.md`, `AGENTS.md` - agent instructions (if gitignored)
     - `.aider.conf.yml` - single-file configs
     - Rationale: Allows per-worktree isolation for environment-specific settings

3. **Worktree re-entry**: Homepage shows existing worktrees as quick-start links
   - Clicking enters existing worktree without prompts
   - Remote branch tracking: if `origin/{name}` exists, worktree tracks it

4. **Exit behavior**: Session exit is identical to non-worktree sessions.
   - No modal or prompt - just return to homepage
   - Worktree persists for later re-entry
   - See ADR-0022 for how swe-swe assists with merge/discard

5. **Conflict handling**: When name conflicts with existing branch/worktree:
   - Show warning dialog with conflict type
   - Options: "Enter existing" or "Choose different name"

## Consequences

Good:
- True branch isolation without full clones
- Parallel work on multiple features
- Dotfiles/env preserved in each worktree
- Worktrees persist for re-entry

Bad:
- Disk usage per worktree (but shared .git objects)
- Requires git repository (non-git projects unaffected)
- Worktree names must be valid branch names
