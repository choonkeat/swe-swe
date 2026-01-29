# Bug: Inconsistent Worktree Directory Naming (worktree vs worktrees)

## Summary

The worktree directory naming is inconsistent between default workspace and external repos:

| Repo Type | Worktree Path | Directory Name |
|-----------|---------------|----------------|
| Default workspace | `/worktrees/{branch}` | **worktrees** (plural) |
| External repo | `/repos/.../worktree/{branch}` | **worktree** (singular) |

## Root Cause

**File**: `cmd/swe-swe/templates/host/swe-swe-server/main.go`

```go
// Line 2330 - global variable uses plural
var worktreeDir = "/worktrees"

// Line 3215 - default workspace uses the variable (plural)
if repoPath == "/workspace" {
    return filepath.Join(worktreeDir, worktreeDirName(branchName))
}

// Line 3220 - external repo hardcodes singular
return filepath.Join(filepath.Dir(repoPath), "worktree", worktreeDirName(branchName))
```

## Proposed Fix

Change line 3220 to use "worktrees" (plural) for consistency:

```go
return filepath.Join(filepath.Dir(repoPath), "worktrees", worktreeDirName(branchName))
```

This would make the structure consistent:
- `/worktrees/{branch}`
- `/repos/{sanitized-url}/worktrees/{branch}`

## Related Code

The `isValidWorktreePath` function (line 3287) only validates paths under `/worktrees/`. If used for external repo worktrees in the future, it would need updating to also handle `/repos/.../worktrees/` paths.

## Priority

Low - Cosmetic inconsistency, does not affect functionality.

## Implementation Steps

- [x] **Step 1**: Edit `cmd/swe-swe/templates/host/swe-swe-server/main.go` line 3220
  - Change `"worktree"` to `"worktrees"` in `resolveWorkingDirectory` function
- [x] **Step 2**: Run `make test` to verify no tests break
  - Also updated test expectations in `worktree_test.go` (3 occurrences)
- [x] **Step 3**: Run `make build golden-update` to update golden files
- [ ] **Step 4**: Verify golden file diff shows only the expected `worktree` → `worktrees` change
  - `git diff --cached -- cmd/swe-swe/testdata/golden`
