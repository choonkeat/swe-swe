# Simplify Worktree Exit Flow

## Goal

Remove the merge/discard modal that appears when exiting a worktree session. Treat worktree exits the same as non-worktree exits - show the process exit status in the terminal and let the user decide what to do next.

Future worktree merge/discard operations will be handled by a `/swe-swe:merge-this-worktree` skill where the agent handles the complexity conversationally.

---

## Phase 1: Frontend - Remove worktree-specific exit modal

### What will be achieved
When a worktree session exits, it will behave identically to a non-worktree session - no modal appears, just the standard `confirm()` dialog asking if the user wants to return home.

### Steps

1. **Modify `handleProcessExit()`** in `terminal-ui.js` (~line 1458)
   - Remove the `if (worktree && worktree.branch)` branch that calls `showWorktreeExitPrompt()`
   - Use the same `confirm()` flow for both worktree and non-worktree exits

2. **Keep `showWorktreeExitPrompt()` and `showWorktreeError()` for now** - remove in Phase 4 after verification

### Verification

- Worktree session exit → shows browser confirm() (same as non-worktree)
- Non-worktree session exit → shows browser confirm() (unchanged)

---

## Phase 2: Frontend - Prevent WebSocket reconnection after process exit

### What will be achieved
After the process exits, the WebSocket will not attempt to reconnect. Users can stay on the page reviewing terminal output without reconnection spam.

### Steps

1. **Add `processExited` flag** in constructor (~line 44)
   - Initialize `this.processExited = false;`

2. **Set flag in `handleProcessExit()`** (~line 1458)
   - Add `this.processExited = true;` at the start of the function

3. **Check flag in `ws.onclose` handler** (~line 1241)
   - Before calling `scheduleReconnect()`, check `if (this.processExited) return;`

4. **Update status bar for exited state**
   - Show "Session ended" instead of "Disconnected" / "Reconnecting..."

### Verification

- Process exits → WebSocket closes → no reconnect attempts
- Status bar shows "Session ended" (no pulsing countdown)
- User can stay on page indefinitely reviewing output

---

## Phase 3: Browser verification with test container

### What will be achieved
Verify the changes work correctly in a real browser environment.

### Steps

1. **Build the project**
   - `make build`

2. **Boot test container**
   - `./scripts/01-test-container-init.sh`
   - `./scripts/02-test-container-build.sh`
   - `HOST_PORT=9899 HOST_IP=host.docker.internal ./scripts/03-test-container-run.sh`

3. **Test Scenario A: Non-worktree session exit**
   - Start session WITHOUT a name
   - Run `echo "hello" && exit 0`
   - Verify: confirm() appears, no modal
   - Verify: Cancel keeps you on page, no reconnection spam
   - Verify: Status bar shows "Session ended"

4. **Test Scenario B: Worktree session exit (clean)**
   - Start session WITH a name (creates worktree)
   - Run `exit 0`
   - Verify: NO modal with Discard/Merge buttons
   - Verify: Same behavior as Scenario A

5. **Test Scenario C: Worktree session exit (error)**
   - Start named session, run `exit 1`
   - Verify: Same confirm() flow, mentions exit code

6. **Shutdown test container**
   - `./scripts/04-test-container-down.sh`

### Pass criteria
- All scenarios behave identically (worktree vs non-worktree)
- No Discard/Merge/Not yet modal appears
- WebSocket doesn't reconnect after exit
- User can stay on page after dismissing confirm()

---

## Phase 4: Frontend - Clean up unused CSS and code

### What will be achieved
Remove dead code after modal removal.

### Steps

1. **Remove `showWorktreeExitPrompt()` function** (~line 1481-1536)

2. **Remove `showWorktreeError()` function** (~line 1538-1570)

3. **Remove worktree modal CSS styles** (~line 565-680)
   - `.terminal-ui__worktree-modal`
   - `.terminal-ui__worktree-modal-content`
   - `.terminal-ui__worktree-branch`
   - `.terminal-ui__worktree-error`
   - `.terminal-ui__worktree-instructions`
   - `.terminal-ui__worktree-strategy-hint`
   - `.terminal-ui__worktree-buttons`
   - `.terminal-ui__worktree-btn` and variants

4. **Update golden files**
   - `make build golden-update`

### Verification

- `grep -r "showWorktreeExitPrompt"` returns nothing
- `grep -r "terminal-ui__worktree-modal"` returns nothing
- `make build` succeeds
- Quick smoke test with test container

---

## Phase 5: Backend - Remove worktree cleanup API endpoints

### What will be achieved
Remove `/api/worktree/merge` and `/api/worktree/discard` endpoints. Future merge operations will use agent with git commands directly.

### Steps

1. **Locate worktree API handlers**
   - Search for `handleWorktreeMerge`, `handleWorktreeDiscard` in `main.go`

2. **Remove the API route registration**

3. **Remove the handler functions**
   - `handleWorktreeMerge`
   - `handleWorktreeDiscard`
   - Any helper functions only used by these

4. **Remove worktree tests if they exist**

5. **Build and test**
   - `make build`
   - `make test`
   - `make golden-update` if needed

### Verification

- `grep -r "handleWorktreeMerge"` returns nothing
- `grep -r "handleWorktreeDiscard"` returns nothing
- `make test` passes
- Quick smoke test - server starts, sessions work

---

## Status

- [x] Phase 1: Frontend - Remove worktree-specific exit modal
- [x] Phase 2: Frontend - Prevent WebSocket reconnection after process exit
- [x] Phase 3: Browser verification with test container
- [ ] Phase 4: Frontend - Clean up unused CSS and code
- [ ] Phase 5: Backend - Remove worktree cleanup API endpoints
