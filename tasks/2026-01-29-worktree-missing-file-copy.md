# Bug: Worktree Creation Missing File Copy for All Repos

## Summary

When creating worktrees (for both `/workspace` and external repos), the critical files needed for agent functionality are not being copied. This breaks MCP browser and other agent features because `.swe-swe/docs/` is missing.

## Root Cause

**File**: `cmd/swe-swe/templates/host/swe-swe-server/main.go`

There are two worktree creation functions:

1. **`createWorktree()`** (lines 2634-2701) - HAS file copying logic but is **DEAD CODE** (never called)
2. **`createWorktreeInRepo()`** (lines 3296-3344) - **MISSING** file copying logic but is the ONLY function used

At line 3574, all worktree creation goes through `createWorktreeInRepo()`:
```go
// Use createWorktreeInRepo which supports both /workspace and external repos
workDir, err = createWorktreeInRepo(baseRepo, branchName)
```

## Files That Should Be Copied

### From `copyUntrackedFiles()` (symlink dirs, copy files):

| Type | Pattern | Examples |
|------|---------|----------|
| Dotfiles (untracked) | `.*` | `.env`, `.envrc`, `.tool-versions` |
| Agent instructions | `CLAUDE.md`, `AGENTS.md` | Project-specific agent config |
| Excluded | `.git`, `.swe-swe` | Never copied |

**Behavior**: Directories are symlinked (absolute path), files are copied.

### From `copySweSweDocsDir()` (always copied):

| File | Purpose |
|------|---------|
| `.swe-swe/docs/AGENTS.md` | Agent documentation |
| `.swe-swe/docs/app-preview.md` | App preview instructions |
| `.swe-swe/docs/browser-automation.md` | **MCP browser instructions** |
| `.swe-swe/docs/docker.md` | Docker usage docs |

## Impact

- **MCP Browser broken**: Agent can't find `browser-automation.md` in worktrees
- **Missing agent context**: `CLAUDE.md`, `AGENTS.md` not available
- **Missing environment**: `.env` files not copied
- **Affects ALL worktrees**: Both `/workspace` and external repos

## Proposed Fix

Add file copying to `createWorktreeInRepo()` after worktree creation:

```go
func createWorktreeInRepo(repoPath, branchName string) (string, error) {
    // ... existing worktree creation code ...

    output, err = cmd.CombinedOutput()
    if err != nil {
        return "", fmt.Errorf("failed to create worktree: %w (output: %s)", err, string(output))
    }

    // NEW: Copy files to worktree (graceful degradation on failure)
    if err := copyUntrackedFiles(repoPath, worktreePath); err != nil {
        log.Printf("Warning: failed to copy untracked files to worktree: %v", err)
    }
    if err := copySweSweDocsDir(repoPath, worktreePath); err != nil {
        log.Printf("Warning: failed to copy .swe-swe/docs/ to worktree: %v", err)
    }

    return worktreePath, nil
}
```

## Cleanup

After fixing `createWorktreeInRepo()`, consider removing the dead `createWorktree()` function (lines 2634-2701) to avoid confusion.

## Test Cases

1. Create worktree for `/workspace` with branch → verify `.swe-swe/docs/` exists
2. Clone external repo → create worktree with branch → verify `.swe-swe/docs/` exists
3. Run MCP browser in worktree → should work

## Related Files

- `cmd/swe-swe/templates/host/swe-swe-server/main.go:2634-2701` - Dead code to remove
- `cmd/swe-swe/templates/host/swe-swe-server/main.go:3296-3344` - Function to fix
- `cmd/swe-swe/templates/host/swe-swe-server/main.go:2480-2587` - Copy helper functions

## Priority

**High** - Breaks core agent functionality (MCP browser) in all worktree sessions.
