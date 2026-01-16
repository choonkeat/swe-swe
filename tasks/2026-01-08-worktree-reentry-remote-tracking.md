# Worktree Re-entry and Remote Branch Tracking

## Goal

Enhance worktree session creation so that:

1. **Existing worktrees are discoverable** — show "+ Start new session in {worktree}" links on homepage
2. **Re-entry works seamlessly** — clicking those links enters the existing worktree without prompts
3. **Remote branches are tracked** — if `origin/{branch}` exists, new worktree tracks it
4. **Conflicts are surfaced** — when using generic "+ Start new session", warn if name collides with existing worktree/branch

## Phases Overview

| Phase | Description | Verification |
|-------|-------------|--------------|
| **1** | Backend: List existing worktrees API | `go test` with httptest |
| **2** | Frontend: Display worktree quick-start links | Test container + MCP browser |
| **3** | Backend: Smart worktree creation (re-entry + remote tracking) | `go test` with temp git repo |
| **4** | Frontend: Conflict warning dialog | Test container + MCP browser |
| **5** | Integration testing | Test container + MCP browser |

---

## Phase 1: Backend - List existing worktrees API

### What will be achieved

New API endpoint `GET /api/worktrees` that returns a list of existing worktree directories.

**Response:**
```json
{
  "worktrees": [
    {"name": "feat-hello", "path": "/workspace/.swe-swe/worktrees/feat-hello"},
    {"name": "fix-bug-123", "path": "/workspace/.swe-swe/worktrees/fix-bug-123"}
  ]
}
```

### Small steps

- [x] 1a. Add `listWorktrees()` function — reads directories under `worktreeDir` constant, returns `[]WorktreeInfo`
- [x] 1b. Add `handleWorktreesAPI()` handler — calls `listWorktrees()`, returns JSON response
- [x] 1c. Wire up route — add `if r.URL.Path == "/api/worktrees"` in main HTTP handler

### Verification (TDD style)

**Red:** Write test first in `worktree_test.go`:
```go
func TestListWorktrees(t *testing.T) {
    // Test 1: Empty directory returns empty list
    // Test 2: Directory with subdirs returns them as worktrees
    // Test 3: Non-existent directory returns empty list (not error)
}

func TestHandleWorktreesAPI(t *testing.T) {
    // Test with httptest - verify JSON response format
}
```

**Green:** Implement `listWorktrees()` and handler to make tests pass

**Run:**
```bash
go test ./cmd/swe-swe/templates/host/swe-swe-server/... -run TestListWorktrees -v
go test ./cmd/swe-swe/templates/host/swe-swe-server/... -run TestHandleWorktreesAPI -v
```

---

## Phase 2: Frontend - Display worktree quick-start links

### What will be achieved

Homepage shows existing worktrees as clickable links under each assistant.

**UI:**
```
Claude
  + Start new session                      <- prompts for name
  + Start new session in feat-hello        <- direct entry, no prompt
  + Start new session in fix-bug-123       <- direct entry, no prompt
```

### Small steps

- [x] 2a. Fetch `/api/worktrees` on page load — add `fetch()` call in `selection.html` JS
- [x] 2b. Render worktree links — for each worktree, add a link under each assistant's "Start new session"
- [x] 2c. Direct navigation — clicking worktree link navigates to `/session/{uuid}?assistant={assistant}&name={worktree-name}` (no prompt)
- [x] 2d. Style — indent worktree links or use lighter color to differentiate from generic link

### Verification (test container + MCP browser)

**Setup:**
```bash
./scripts/01-test-container-init.sh
./scripts/02-test-container-build.sh
HOST_PORT=9899 HOST_IP=host.docker.internal ./scripts/03-test-container-run.sh
```

**Test:**
1. Create a named session manually (creates worktree)
2. Navigate back to homepage
3. Verify: worktree link appears under assistant
4. Click worktree link -> enters session in that worktree (check `pwd`)

**Teardown:**
```bash
./scripts/04-test-container-down.sh
```

---

## Phase 3: Backend - Smart worktree creation

### What will be achieved

Refactor `createWorktree()` to handle these scenarios:

| Scenario | Action |
|----------|--------|
| Worktree exists at path | Return existing path (re-entry) |
| Local branch exists, no worktree | `git worktree add <path> <branch>` |
| Remote branch exists, no local | `git worktree add --track -b <branch> <path> origin/<branch>` |
| Neither exists | `git worktree add -b <branch> <path>` (fresh from HEAD) |

### Small steps

- [x] 3a. Add `worktreeExists(branchName) bool` — checks if `worktreeDir + "/" + branchName` directory exists
- [x] 3b. Add `localBranchExists(branchName) bool` — runs `git rev-parse --verify <branch>`
- [x] 3c. Add `remoteBranchExists(branchName) bool` — runs `git rev-parse --verify origin/<branch>`
- [x] 3d. Refactor `createWorktree()` — implement priority logic:
  1. If worktree exists -> return existing path (no git commands)
  2. If local branch exists -> `git worktree add <path> <branch>` (no `-b`)
  3. If remote branch exists -> `git worktree add --track -b <branch> <path> origin/<branch>`
  4. Otherwise -> `git worktree add -b <branch> <path>` (current behavior)
- [x] 3e. Remove random suffix logic — no longer needed since we re-enter existing worktrees

### Verification (TDD style with temp git repo)

**Red:** Write tests in `worktree_test.go`:
```go
func TestWorktreeExists(t *testing.T) {
    // Test 1: Returns false when dir doesn't exist
    // Test 2: Returns true when dir exists
}

func TestLocalBranchExists(t *testing.T) {
    // Test 1: Returns false for non-existent branch
    // Test 2: Returns true for existing branch
}

func TestCreateWorktree_ReentryExisting(t *testing.T) {
    // Setup: Create worktree dir manually
    // Call createWorktree() with same name
    // Verify: Returns existing path, no error, no new git operations
}

func TestCreateWorktree_AttachLocalBranch(t *testing.T) {
    // Setup: Create local branch (no worktree)
    // Call createWorktree() with branch name
    // Verify: Worktree created attached to existing branch
}

func TestCreateWorktree_Fresh(t *testing.T) {
    // Setup: No existing branch or worktree
    // Call createWorktree()
    // Verify: New branch and worktree created
}
```

**(Skip remote branch test for simplicity — verify manually in Phase 5)**

**Green:** Implement to make tests pass

**Run:**
```bash
go test ./cmd/swe-swe/templates/host/swe-swe-server/... -run TestWorktree -v
go test ./cmd/swe-swe/templates/host/swe-swe-server/... -run TestCreateWorktree -v
```

---

## Phase 4: Frontend - Conflict warning dialog

### What will be achieved

When user clicks generic "+ Start new session" and enters a name that conflicts with existing worktree/branch, show a warning dialog with options to rename or use existing.

**Conflict types:**
- Existing worktree
- Existing local branch (no worktree)
- Existing remote branch (no local)

**Dialog:**
```
+--------------------------------------------------+
|  Branch "feat-hello" already exists              |
|                                                  |
|  o Existing worktree found                       |
|    -or-                                          |
|  o Local branch exists (will attach worktree)    |
|    -or-                                          |
|  o Remote branch exists (will track origin)      |
|                                                  |
|  [Rename]  [Use existing]                        |
+--------------------------------------------------+
```

### Small steps

- [ ] 4a. Add `GET /api/worktree/check?name={branch}` endpoint — returns conflict info:
  ```json
  {"exists": false}
  // or
  {"exists": true, "type": "worktree|local|remote"}
  ```
- [ ] 4b. Add handler `handleWorktreeCheckAPI()` — calls helper functions from Phase 3, returns JSON
- [ ] 4c. Wire up route — add to main HTTP handler
- [ ] 4d. Update `startNewSession()` in JS — after user enters name, call check endpoint before navigating
- [ ] 4e. Show warning dialog — if conflict exists, display type-specific message with two buttons
- [ ] 4f. Handle button clicks:
  - "Rename" -> re-prompt for new name (loop back)
  - "Use existing" -> proceed with session creation

### Verification (test container + MCP browser)

**Setup:**
```bash
./scripts/01-test-container-init.sh
./scripts/02-test-container-build.sh
HOST_PORT=9899 HOST_IP=host.docker.internal ./scripts/03-test-container-run.sh
```

**Test scenarios:**

1. **Worktree conflict:**
   - Create named session "test-worktree" (creates worktree)
   - Return to homepage, click "+ Start new session", enter "test-worktree"
   - Verify: Warning shows "Existing worktree found"
   - Click "Use existing" -> enters existing worktree

2. **Rename flow:**
   - Same setup as above
   - Click "Rename" -> prompt reappears
   - Enter different name -> proceeds without warning

3. **No conflict:**
   - Enter unique name
   - Verify: No warning, proceeds directly

**Teardown:**
```bash
./scripts/04-test-container-down.sh
```

---

## Phase 5: Integration testing

### What will be achieved

Full end-to-end verification that all features work together correctly.

### Small steps

- [ ] 5a. Boot test container
- [ ] 5b. Execute test scenarios via MCP browser
- [ ] 5c. Document any bugs found
- [ ] 5d. Fix bugs, re-test
- [ ] 5e. Shutdown test container

### Test scenarios

| # | Scenario | Steps | Expected |
|---|----------|-------|----------|
| 5a | Worktree listing | Create 2 named sessions, return to homepage | Both worktrees appear as quick-start links |
| 5b | Quick-start re-entry | Click worktree link | Enters existing worktree (same `pwd`) |
| 5c | Remote tracking | Push branch to origin, create session with same name | Worktree tracks remote (verify with `git status`) |
| 5d | Conflict warning - Use existing | Enter conflicting name, click "Use existing" | Enters existing worktree |
| 5e | Conflict warning - Rename | Enter conflicting name, click "Rename", enter new name | Creates new worktree |
| 5f | No conflict | Enter unique name | Creates fresh worktree, no warning |

**Setup:**
```bash
./scripts/01-test-container-init.sh
./scripts/02-test-container-build.sh
HOST_PORT=9899 HOST_IP=host.docker.internal ./scripts/03-test-container-run.sh
```

**Teardown:**
```bash
./scripts/04-test-container-down.sh
```

---

## Status

- [x] Phase 1: Backend - List existing worktrees API
- [x] Phase 2: Frontend - Display worktree quick-start links
- [x] Phase 3: Backend - Smart worktree creation
- [ ] Phase 4: Frontend - Conflict warning dialog
- [ ] Phase 5: Integration testing
