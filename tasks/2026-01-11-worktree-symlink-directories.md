# Worktree: Symlink Directories, Copy Files

**Date**: 2026-01-11
**Status**: Phase 1 Complete

## Goal

When setting up git worktrees, symlink directories (using absolute paths to `/workspace`) so agent config directories (`.claude/`, `.codex/`, etc.) share permissions and stay in sync. Copy single files (`.env`, etc.) for potential per-worktree isolation.

## Phases

### Phase 1: Modify `copyUntrackedFiles` with TDD ✅

**What will be achieved**: The `copyUntrackedFiles` function will symlink directories and copy files, verified by tests.

**Steps**:

1. ✅ **Red**: Add test in `worktree_test.go` that:
   - Creates source dir with `.claude/` directory and `.env` file
   - Calls `copyUntrackedFiles(srcDir, destDir)`
   - Asserts `.claude` is a symlink to absolute path
   - Asserts `.env` is a regular file (copied)
   - Run `make test` → test fails

2. ✅ **Green**: Modify `copyUntrackedFiles` in `main.go`:
   - Update comment
   - Branch on `entry.IsDir()`: symlink vs copy
   - Use absolute path (srcDir + "/" + name) for symlink target
   - Update log messages
   - Run `make test` → test passes

3. ✅ **Refactor**: Run `make build golden-update`, review diff

**Verification**:
- `make test` passes
- Golden diff shows only the expected function change

**Files**:
- `cmd/swe-swe/templates/host/swe-swe-server/main.go`
- `cmd/swe-swe/templates/host/swe-swe-server/worktree_test.go`

---

### Phase 2: Update documentation

**What will be achieved**: ADR-0020 will accurately reflect the new symlink-for-directories behavior.

**Steps**:

1. Read `docs/adr/0020-git-worktree-integration.md`
2. Update section "2. Untracked file copying" to clarify:
   - Directories (`.claude/`, `.codex/`, etc.) are **symlinked** to `/workspace`
   - Files (`.env`, etc.) are **copied**
3. Add rationale: shared permissions/settings for agent configs, isolation option for env files

**Verification**:
- Read the updated ADR and confirm it matches implemented behavior

**Files**:
- `docs/adr/0020-git-worktree-integration.md`
