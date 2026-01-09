# Merge Strategy Flag

## Goal

Add `--merge-strategy={merge-commit,merge-ff,squash}` flag to `swe-swe init` with `merge-commit` as the default. This controls how worktree branches are merged back to the target branch when a session exits cleanly.

## Phases Overview

1. **Baseline**: Add flag parsing (no effect yet) + golden test variants
2. **Backend Implementation**: Update merge endpoint to use the configured strategy
3. **Frontend Update**: Display merge strategy hint in UI + golden-update verification
4. **Integration Testing**: Verify end-to-end via test container

---

## Phase 1: Baseline - Add flag parsing (no effect yet) + golden test variants

### What will be achieved

The `--merge-strategy` flag will be parsed and stored in `InitConfig` and `init.json`, but won't have any functional effect yet. Golden tests will capture the new field. Help text will explain the options.

### Steps

1. Add `MergeStrategy` field to `InitConfig` struct in `cmd/swe-swe/main.go`
2. Add flag definition: `--merge-strategy` with allowed values `merge-commit`, `merge-ff`, `squash` and default `merge-commit`
3. Add help text explaining each option (default first):
   - `merge-commit`: Rebase then merge with commit (default)
   - `merge-ff`: Fast-forward when possible
   - `squash`: Squash all commits into one
4. Add validation to reject invalid values
5. Ensure the field is serialized to `init.json` and read back on subsequent runs
6. Add golden test variant `with-merge-strategy-squash` in `cmd/swe-swe/main_test.go`
7. Create `docs/worktree-merge-strategies.md` documenting the feature
8. Run `make build golden-update`
9. Verify golden diff shows only `mergeStrategy` field in init.json files

### Verification (TDD style)

**Red:**
- Write test variant `with-merge-strategy-squash` that passes `--merge-strategy=squash`
- Test will fail because flag doesn't exist

**Green:**
- Add flag parsing and `MergeStrategy` field
- Run `make build golden-update`
- Test passes, golden files show `"mergeStrategy": "squash"` in init.json

**Refactor:**
- Clean up if needed

**Regression check:**
- All existing golden tests pass unchanged (except init.json gaining new field with default value)

---

## Phase 2: Backend Implementation - Update merge endpoint to use the configured strategy

### What will be achieved

The `/api/worktree/merge` endpoint will read `mergeStrategy` from the project's `init.json` and execute the appropriate git commands.

### Steps

1. Add function `readMergeStrategy()` to read from `/workspace/.swe-swe/init.json`, defaulting to `merge-commit`
2. Update `handleWorktreeMerge` to dispatch based on strategy:
   - `merge-commit`: `executeMergeCommitStrategy(branch, path)`
   - `merge-ff`: `executeMergeFFStrategy(branch, path)` (current behavior)
   - `squash`: `executeSquashStrategy(branch, path)`
3. Implement `executeMergeCommitStrategy`:
   - `git rebase main <branch>`
   - If success: `git checkout main && git merge --no-ff <branch>`
   - If fail: `git rebase --abort && git checkout main && git merge --no-ff <branch>`
4. Implement `executeSquashStrategy`:
   - `git merge --squash <branch>`
   - `git commit -m "Merge branch '<branch>'"`
5. Update `buildMergeInstructions` to include strategy-specific manual instructions

### Verification (TDD style)

**Test setup helper:**
```go
// setupMergeTestRepo creates a repo with main branch and a worktree branch with commits
func setupMergeTestRepo(t *testing.T, mainCommits int, worktreeCommits int, diverge bool) (repoDir, worktreePath, branch string)
```

**Red - Write tests first:**

```go
func TestMergeStrategy_MergeFF(t *testing.T) {
    // Setup: main with 1 commit, worktree with 2 commits (no divergence)
    // Execute: merge with merge-ff strategy
    // Verify:
    //   - git rev-list --count main == 3 (1 + 2)
    //   - git cat-file -p HEAD shows 1 parent (fast-forward, no merge commit)
    //   - worktree directory removed
    //   - branch deleted
}

func TestMergeStrategy_MergeCommit_RebaseSuccess(t *testing.T) {
    // Setup: main with 1 commit, worktree with 2 commits (no divergence)
    // Execute: merge with merge-commit strategy
    // Verify:
    //   - git cat-file -p HEAD shows 2 parents (merge commit)
    //   - all worktree commits preserved in history
    //   - worktree directory removed
    //   - branch deleted
}

func TestMergeStrategy_MergeCommit_RebaseFailFallback(t *testing.T) {
    // Setup: main with 2 commits, worktree with 2 commits, SAME FILE modified (conflict)
    // Execute: merge with merge-commit strategy
    // Verify:
    //   - rebase failed, fell back to regular merge --no-ff
    //   - git cat-file -p HEAD shows 2 parents (merge commit)
    //   - no partial rebase state left (.git/rebase-merge shouldn't exist)
    //   - OR: if merge also fails, proper error + instructions returned
}

func TestMergeStrategy_Squash(t *testing.T) {
    // Setup: main with 1 commit, worktree with 3 commits
    // Execute: merge with squash strategy
    // Verify:
    //   - git rev-list --count main == 2 (1 original + 1 squashed)
    //   - git cat-file -p HEAD shows 1 parent (not a merge commit)
    //   - all changes from worktree present in working tree
    //   - worktree directory removed
    //   - branch deleted
}

func TestMergeStrategy_ReadsFromInitJson(t *testing.T) {
    // Setup: create init.json with mergeStrategy: "squash"
    // Execute: call handleWorktreeMerge via HTTP
    // Verify: squash behavior was used
}
```

**Green:** Implement each strategy to make tests pass

**Refactor:** Extract common git command helpers (`runGit`, `runGitInDir`)

**Regression check:**
- All existing worktree tests pass
- Default behavior is now `merge-commit` (not `merge-ff`)

---

## Phase 3: Frontend Update - UI improvements + merge strategy hint + golden-update verification

### What will be achieved

The worktree exit prompt will be improved with better button order, proper "Not yet" behavior, and a human-readable hint describing the configured merge strategy.

### Steps

1. **Reorder buttons** in `terminal-ui.js`: `[Discard] [Merge to {branch}] [Not yet]`
   - Safe action (Not yet) on the right where users expect default
   - Destructive action (Discard) on the left, less prominent
2. **Fix "Not yet" behavior**: redirect to homepage instead of staying on dead terminal page
   ```js
   if (action === 'not-yet') {
       window.location.href = '/' + this.getDebugQueryString();
       return;
   }
   ```
3. Update `buildExitMessage` to include `mergeStrategy` and `mergeStrategyDescription` in the worktree info
4. Add strategy descriptions (matching `-h` help text):
   - `merge-commit`: "Rebase then merge with commit"
   - `merge-ff`: "Fast-forward when possible"
   - `squash`: "Squash all commits into one"
5. Update `showWorktreeExitPrompt` in `terminal-ui.js` to display the description as footer hint
6. Run `make build golden-update`
7. Verify golden diff shows only expected changes

### UI Changes

```
+---------------------------------------------------+
|  Done with this worktree?                         |
|                                                   |
|  Branch: fix-bug                                  |
|                                                   |
|  [Discard]  [Merge to main]  [Not yet]            |
|                                                   |
|  Merge strategy: rebase then merge with commit    |
+---------------------------------------------------+
```

Button behavior:
- **Discard**: calls API, redirects to homepage on success
- **Merge to {branch}**: calls API, redirects to homepage on success
- **Not yet**: redirects to homepage immediately (worktree preserved for later)

### Verification

**Golden file verification:**
```bash
make build golden-update
git add -A cmd/swe-swe/testdata/golden
git diff --cached -- cmd/swe-swe/testdata/golden
```

Expected changes:
- `init.json` files: new `mergeStrategy` field with default value
- `terminal-ui.js`: button reorder, "Not yet" redirect, footer hint
- `main.go`: exit message includes strategy + description

**Manual verification (deferred to Phase 4):**
- Button order is correct
- "Not yet" redirects to homepage
- Hint text shows correct description
- Clicking merge executes correct strategy

---

## Phase 4: Integration Testing - Verify end-to-end via test container

### What will be achieved

Verify the full merge strategy flow works end-to-end using the test container and MCP browser automation.

### Steps

1. Boot test container following `/workspace/.swe-swe/test-container-workflow.md`
2. Run test scenarios via MCP browser
3. Document any bugs found, fix, and re-test
4. Shutdown test container

### Test Scenarios

**Scenario 1: merge-commit (default) - happy path**
1. Run `swe-swe init` (uses default merge-commit)
2. Start session with name "test-merge-commit"
3. Make 2-3 commits in the worktree
4. Exit cleanly
5. Verify: hint shows "Rebase then merge with commit"
6. Click "Merge to main"
7. Verify: redirected to homepage, `git log --oneline --graph` shows merge commit with preserved commits

**Scenario 2: merge-ff - happy path**
1. Run `swe-swe init --merge-strategy=merge-ff`
2. Start session with name "test-merge-ff"
3. Make 2 commits in the worktree
4. Exit cleanly
5. Verify: hint shows "Fast-forward when possible"
6. Click "Merge to main"
7. Verify: `git log --oneline` shows commits directly on main (no merge commit)

**Scenario 3: squash - happy path**
1. Run `swe-swe init --merge-strategy=squash`
2. Start session with name "test-squash"
3. Make 3 commits in the worktree
4. Exit cleanly
5. Verify: hint shows "Squash all commits into one"
6. Click "Merge to main"
7. Verify: `git log --oneline` shows single new commit on main

**Scenario 4: merge-commit with conflict (fallback)**
1. Use default merge-commit strategy
2. Start worktree session, modify file A, commit
3. In main workspace, modify same file A, commit
4. Exit worktree session, click merge
5. Verify: either succeeds with merge commit, or shows error with manual instructions

**Scenario 5: Discard still works**
1. Start worktree session, make commits
2. Exit cleanly, click "Discard"
3. Verify: worktree gone, branch deleted, no merge happened

**Scenario 6: "Not yet" redirects to homepage**
1. Start worktree session, make commits
2. Exit cleanly, click "Not yet"
3. Verify: redirected to homepage, worktree still listed, branch still exists
4. Re-enter the same worktree from homepage
5. Verify: previous commits are still there

**Scenario 7: Button order is correct**
1. Start worktree session, exit cleanly
2. Verify: buttons appear in order `[Discard] [Merge to {branch}] [Not yet]`

### Verification

- All scenarios pass
- No regressions in existing worktree functionality
- Error messages include strategy-appropriate manual instructions
- Button order puts safe action (Not yet) on the right

---

## Status

- [ ] Phase 1: Baseline - Add flag parsing + golden test variants
- [ ] Phase 2: Backend Implementation - Update merge endpoint
- [ ] Phase 3: Frontend Update - Display merge strategy hint + golden-update
- [ ] Phase 4: Integration Testing - Verify end-to-end
