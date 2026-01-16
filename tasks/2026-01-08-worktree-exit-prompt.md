# Worktree Exit Prompt

## Goal

Add a worktree cleanup prompt when a session in a worktree exits cleanly, allowing users to merge or discard the worktree directly from the browser UI.

## Prerequisites

- `fix-server-crt-key-golden-files` worktree must be merged before running final `make build golden-update`

## Phases Overview

1. Backend: Extend exit broadcast with worktree info
2. Backend: Add worktree cleanup API endpoints
3. Frontend: Worktree-aware exit prompt
4. Integration testing (via test container + MCP browser)
5. Rebase & golden update (post-prerequisite)

---

## Phase 1: Backend - Extend exit broadcast with worktree info

### What will be achieved

When a session exits, the `BroadcastExit` message will include worktree metadata if the session was running in a worktree.

**Current message:**
```json
{"type": "exit", "exitCode": 0}
```

**New message (worktree session):**
```json
{"type": "exit", "exitCode": 0, "worktree": {"path": "/workspace/.swe-swe/worktrees/fix-bug", "branch": "fix-bug"}}
```

**New message (non-worktree session):**
```json
{"type": "exit", "exitCode": 0}
```
(unchanged - no `worktree` field)

### Steps

1. **Modify `BroadcastExit`** - Add worktree info to the JSON payload by reading from `Session.WorkDir` and `Session.BranchName`

2. **Add conditional inclusion** - Only include `worktree` field when `Session.WorkDir` starts with `worktreeDir` constant (`/workspace/.swe-swe/worktrees`)

3. **Update call sites** - `BroadcastExit` is called in `startPTYReader` - no signature change needed since it reads from Session

### Verification (TDD style)

**Red:** Write test first in `main_test.go`:
```go
func TestBroadcastExitWorktreeInfo(t *testing.T) {
    // Test 1: Session with worktree includes worktree info
    // Test 2: Session without worktree does NOT include worktree info
}
```

**Green:** Implement the change to make tests pass

**Refactor:** Clean up if needed

---

## Phase 2: Backend - Add worktree cleanup API endpoints

### What will be achieved

Two new API endpoints that perform git operations to clean up worktrees:

- `POST /api/worktree/merge` - Merge branch to dev, remove worktree, delete branch
- `POST /api/worktree/discard` - Force-remove worktree, delete branch

### Request/Response format

**Request body:**
```json
{"branch": "fix-bug", "path": "/workspace/.swe-swe/worktrees/fix-bug"}
```

**Success response (200):**
```json
{"success": true}
```

**Error response (400):**
```json
{
  "success": false,
  "error": "git merge failed: <raw git output>",
  "instructions": "To resolve manually:\n  cd /workspace\n  git merge fix-bug\n  # resolve any issues\n  git commit\n  git worktree remove .swe-swe/worktrees/fix-bug\n  git branch -d fix-bug"
}
```

### Steps

1. **Add `handleWorktreeAPI` router** in `main.go` - Route `/api/worktree/*` requests

2. **Implement `handleWorktreeMerge`:**
   - Validate request (branch and path required)
   - Verify path is under `worktreeDir` (security check)
   - Execute: `git -C /workspace merge <branch> --no-edit`
   - If any error: return error with raw output + instructions, run `git merge --abort` to clean up
   - If success: `git worktree remove <path>` then `git branch -d <branch>`
   - Return success

3. **Implement `handleWorktreeDiscard`:**
   - Validate request
   - Verify path is under `worktreeDir` (security check)
   - Execute: `git worktree remove --force <path>`
   - Execute: `git branch -D <branch>`
   - Return success

4. **Add helper `runGitCommand`** - Execute git commands with proper error handling and output capture

### Verification (TDD style)

**Red:** Write tests in `worktree_test.go`:
```go
func TestHandleWorktreeMerge(t *testing.T) {
    // Test 1: Successful merge - creates worktree, makes commit, calls API, verifies cleanup
    // Test 2: Merge error - creates conflicting changes, calls API, verifies error + instructions
    // Test 3: Invalid path (security) - path outside worktreeDir returns 400
}

func TestHandleWorktreeDiscard(t *testing.T) {
    // Test 1: Successful discard - creates worktree, calls API, verifies cleanup
    // Test 2: Invalid path (security) - returns 400
}
```

**Green:** Implement endpoints to make tests pass

**Refactor:** Extract common validation/git logic

Note: Tests will need to set up temporary git repos to avoid affecting the real workspace.

---

## Phase 3: Frontend - Worktree-aware exit prompt

### What will be achieved

The browser UI shows a worktree-specific prompt when a worktree session exits cleanly, with buttons to merge, discard, or defer. Errors display resolution instructions.

### UI behavior

**Non-worktree session exits:**
- Current behavior unchanged (return to homepage prompt)

**Worktree session exits (exitCode 0):**
```
+---------------------------------------------------+
|  Done with this worktree?                         |
|                                                   |
|  Branch: fix-bug                                  |
|                                                   |
|  [Not yet]  [Merge to dev]  [Discard]             |
+---------------------------------------------------+
```

**On Merge/Discard error:**
```
+---------------------------------------------------+
|  ! Merge failed                                   |
|                                                   |
|  git merge failed: CONFLICT (content): ...        |
|                                                   |
|  To resolve manually:                             |
|    cd /workspace                                  |
|    git merge fix-bug                              |
|    # resolve any issues                           |
|    ...                                            |
|                                                   |
|  [Copy instructions]  [Start a new session in /workspace]  |
+---------------------------------------------------+
```

### Steps

1. **Update exit message handler** in `terminal-ui.js` - Check for `worktree` field in exit message

2. **Create worktree prompt modal** - New function `showWorktreeExitPrompt(worktreeInfo)` with three buttons

3. **Implement button handlers:**
   - "Not yet" - dismiss modal, keep current behavior (session stays, can restart)
   - "Merge to dev" - `POST /api/worktree/merge` with body, handle response
   - "Discard" - `POST /api/worktree/discard` with body, handle response

4. **Implement error display** - On API error, show error modal with raw error + instructions + copy button

5. **Redirect behavior:**
   - Success - redirect to homepage
   - Error "Start new session" - redirect to `/session/<new-uuid>?assistant=<same-assistant>` (no name = no worktree)

### Verification

Manual testing via Phase 4 (no automated JS tests in codebase).

---

## Phase 4: Integration testing (via test container + MCP browser)

### What will be achieved

Verify the full end-to-end flow using the test container and MCP browser automation.

### Setup

```bash
./scripts/01-test-container-init.sh
./scripts/02-test-container-build.sh
HOST_PORT=9899 HOST_IP=host.docker.internal ./scripts/03-test-container-run.sh
```

MCP browser tests at `http://host.docker.internal:9899/`

### Test scenarios (using MCP browser)

**Scenario 1: Merge happy path**
1. Start new session with name "test-merge" (creates worktree)
2. Make a small change and commit in the worktree
3. Exit the agent cleanly (exit code 0)
4. See worktree prompt with "Not yet | Merge to dev | Discard"
5. Click "Merge to dev"
6. Verify: redirected to homepage, branch merged, worktree gone, branch deleted

**Scenario 2: Discard happy path**
1. Start new session with name "test-discard" (creates worktree)
2. Make changes (committed or not)
3. Exit cleanly
4. Click "Discard"
5. Verify: redirected to homepage, worktree gone, branch deleted, changes lost

**Scenario 3: "Not yet" behavior**
1. Start worktree session, exit cleanly
2. Click "Not yet"
3. Verify: modal dismisses, can click "Restart" to continue working

**Scenario 4: Merge conflict**
1. Start session with name "test-conflict"
2. Modify a file and commit
3. In another terminal, modify same file in dev and commit
4. Exit session, click "Merge to dev"
5. Verify: error modal with instructions, "Copy instructions" works, "Start new session" opens session in /workspace

**Scenario 5: Non-worktree session unchanged**
1. Start session without a name (runs in /workspace)
2. Exit cleanly
3. Verify: see original prompt (not worktree prompt)

### Teardown

```bash
./scripts/04-test-container-down.sh
```

### Steps

1. Boot test container
2. Use MCP browser to execute each scenario
3. Document any bugs found
4. Fix bugs, re-test
5. Shutdown test container

---

## Phase 5: Rebase & golden update (post-prerequisite)

### What will be achieved

Final verification that changes integrate cleanly with the codebase after prerequisite worktree is merged.

### Steps

1. Wait for `fix-server-crt-key-golden-files` worktree to be merged into `dev`
2. Rebase this worktree: `git rebase dev`
3. Resolve any conflicts
4. Run `make build golden-update`
5. Verify golden file changes are expected
6. Commit golden file changes if any
7. Ready to merge this worktree

---

## Status

- [x] Phase 1: Backend - Extend exit broadcast with worktree info
- [x] Phase 2: Backend - Add worktree cleanup API endpoints
- [ ] Phase 3: Frontend - Worktree-aware exit prompt
- [ ] Phase 4: Integration testing
- [ ] Phase 5: Rebase & golden update
