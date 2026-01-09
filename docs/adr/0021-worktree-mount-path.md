# ADR-021: Mount worktrees at /worktrees

**Status**: Accepted
**Date**: 2026-01-09

## Context

Git worktrees for named sessions were stored at `/workspace/.swe-swe/worktrees/{branch-name}`. This nested path inside `/workspace` created issues:

1. **Path confusion**: Agents working in `/workspace/.swe-swe/worktrees/fix-bug` could navigate up (`cd ..`) and land in `/workspace/` - the main repository with "same files". This parent-child relationship risked accidental cross-contamination.

2. **Long paths**: `/workspace/.swe-swe/worktrees/feature-xyz` is verbose in prompts and logs.

## Decision

Mount the host's `.swe-swe/worktrees` directory to `/worktrees` in containers:

1. **Docker volume mount**: Add `${WORKSPACE_DIR:-.}/.swe-swe/worktrees:/worktrees` to both `swe-swe` and `code-server` services in docker-compose.yml

2. **Server constant**: Change `worktreeDir` from `/workspace/.swe-swe/worktrees` to `/worktrees`

This makes worktrees siblings to `/workspace` rather than children of it. An agent in `/worktrees/fix-bug` navigating up reaches `/worktrees/` (a directory listing of worktrees), not the main repository.

## Consequences

Good:
- Clear separation between main repo (`/workspace`) and worktrees (`/worktrees`)
- Navigating up from a worktree doesn't reach main repo
- Shorter, cleaner paths in terminal prompts and logs
- Same host storage location (`.swe-swe/worktrees/`) - only container mount point changes

Bad:
- Minor breaking change for users who hardcoded old paths (unlikely)
- Requires docker-compose regeneration on existing projects
