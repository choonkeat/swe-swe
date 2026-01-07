# Task: Auto-create git worktrees for named sessions

> **Date**: 2026-01-07
> **Status**: In Progress (Phase 1 complete)

---

## Goal

When a user provides a session name on the homepage, automatically create a git worktree and run the agent in that isolated branch. Sessions without names run in `/workspace` as before.

---

## Phases

| Phase | Description |
|-------|-------------|
| **1** | Per-session working directory support |
| **2** | Session name prompt on homepage |
| **3** | Git worktree creation for named sessions |
| **4** | Remove TTL-based session expiry |

---

## Phase 1: Per-session working directory support

### What will be achieved
Each session will have its own working directory stored in the `Session` struct. When spawning or restarting a process, `cmd.Dir` will be set to this directory. This is the foundation for worktree support.

### Small steps

**Step 1a: Add `WorkDir` field to Session struct**
- Add `WorkDir string` to `Session` struct (line ~169)
- Initialize to empty string (meaning "use server cwd")

**Step 1b: Set `cmd.Dir` when creating session**
- In `getOrCreateSession()` (line ~1154), after `exec.Command()`:
  ```go
  if sess.WorkDir != "" {
      cmd.Dir = sess.WorkDir
  }
  ```

**Step 1c: Set `cmd.Dir` when restarting process**
- In `RestartProcess()` (line ~657), after `exec.Command()`:
  ```go
  if s.WorkDir != "" {
      cmd.Dir = s.WorkDir
  }
  ```

**Step 1d: Pass `workDir` parameter to `getOrCreateSession()`**
- Change signature: `getOrCreateSession(sessionUUID, assistant, workDir string)`
- Set `sess.WorkDir = workDir` when creating new session
- Update all callers to pass `""` for now (no behavior change yet)

### Verification

**Setup:**
```bash
./scripts/01-test-container-init.sh
./scripts/02-test-container-build.sh
HOST_PORT=9899 HOST_IP=host.docker.internal ./scripts/03-test-container-run.sh
```

**RED - Baseline test:**
1. MCP browser: start a session, run `pwd` in agent → should show `/workspace`
2. Restart process (kill agent, let it auto-restart), run `pwd` → should show `/workspace`

**GREEN - After implementation:**
1. Manually test by hardcoding `workDir = "/tmp"` in one code path
2. Start session → `pwd` should show `/tmp`
3. Restart → `pwd` should still show `/tmp`
4. Revert hardcoding, deploy actual implementation

**REFACTOR:**
- Ensure empty `WorkDir` means "inherit server cwd" (backward compatible)

---

## Phase 2: Session name prompt on homepage

### What will be achieved
When clicking "New Session" on the homepage, a prompt appears asking for an optional session name. The name is sanitized and passed to the server when creating the session.

### Small steps

**Step 2a: Change "New Session" from link to button with JS handler**
- In `selection.html`, change the new session `<a>` tag to trigger JavaScript
- Add click handler that shows `prompt()` dialog

**Step 2b: Prompt UI with default timestamp**
- Default value: `YYYYMMDD-HHMMSS` format (e.g., `20260107-143052`)
- User can accept default, type custom name, or clear to leave empty
- Empty/cancelled = no name (run in `/workspace`)

**Step 2c: Client-side sanitization**
- Trim whitespace
- Max 32 characters
- Replace non-alphanumeric (except spaces, hyphens, underscores) with hyphens
- Show preview: "Branch will be: `fix-login-bug`"

**Step 2d: Pass session name to server**
- Add `?name=` query parameter to session URL
- URL encode the name

**Step 2e: Server receives and stores name**
- In WebSocket handler or session creation endpoint, read `name` param
- Store in `Session.Name` field (already exists)
- For now, `WorkDir` remains empty (Phase 3 will use the name)

### Verification

**Setup:** Test container workflow

**RED - Baseline:**
1. MCP browser: navigate to `http://host.docker.internal:9899/`
2. MCP browser: click "New Session" on Claude → immediately navigates to terminal (current behavior)
3. MCP browser: navigate back to homepage → session shows UUID only (e.g., `a3f2`)

**GREEN - After implementation:**
1. MCP browser: navigate to homepage
2. MCP browser: click "New Session" → prompt dialog appears with timestamp default
3. MCP browser: accept default → navigates to terminal
4. MCP browser: navigate to homepage → session shows timestamp name (e.g., `20260107-143052 (a3f2)`)
5. Repeat: type "Fix Login Bug" → homepage shows `Fix Login Bug (b7c1)`
6. Repeat: clear input, click OK → homepage shows UUID only (backward compatible)

**Teardown:**
```bash
./scripts/04-test-container-down.sh
```

---

## Phase 3: Git worktree creation for named sessions

### What will be achieved
When a session has a name, the server creates a git worktree at `/workspace/.swe-swe/worktrees/{branch-name}` and runs the agent there. Sessions without names continue to run in `/workspace`.

### Small steps

**Step 3a: Add branch name derivation function with unit tests**
- `deriveBranchName(sessionName string) string`
- Lowercase, replace spaces with hyphens, remove special chars
- e.g., "Fix Login Bug" → "fix-login-bug"

**Unit test cases (`main_test.go`):**
```go
func TestDeriveBranchName(t *testing.T) {
    tests := []struct {
        input    string
        expected string
    }{
        {"Fix Login Bug", "fix-login-bug"},
        {"  spaces  around  ", "spaces-around"},
        {"UPPERCASE", "uppercase"},
        {"with_underscores", "with_underscores"},
        {"special!@#chars", "special-chars"},
        {"multiple---hyphens", "multiple-hyphens"},
        {"émojis 🚀 and üñíçödé", "emojis-and-unicode"},
        {"", ""},
        {"a", "a"},
        {"123-numbers-456", "123-numbers-456"},
    }
    // ...
}
```

**Step 3b: Add worktree creation function**
- `createWorktree(branchName string) (workDir string, err error)`
- Check if branch exists: `git rev-parse --verify {branch}`
- If exists, append random suffix: `{branch}-{4-char-hex}`
- Create worktree: `git worktree add /workspace/.swe-swe/worktrees/{branch} -b {branch}`
- Return the worktree path

**Step 3c: Integrate worktree creation into session creation**
- In `getOrCreateSession()`, if `name != ""`:
  1. Derive branch name from session name
  2. Create worktree
  3. Set `sess.WorkDir` to worktree path
- If `name == ""`, `sess.WorkDir` remains empty (use `/workspace`)

**Step 3d: Store branch name in Session struct**
- Add `BranchName string` to `Session` struct
- Useful for display and cleanup later

**Step 3e: Ensure `/workspace/.swe-swe/worktrees/` directory exists**
- Create on first use if missing

### Verification

**Unit tests (run locally):**
```bash
go test ./cmd/swe-swe/... -run TestDeriveBranchName -v
```

**Integration tests (test container + MCP browser):**

**RED - Baseline:**
1. MCP browser: create named session "test worktree"
2. In terminal, agent runs `pwd` → shows `/workspace` (worktree not created yet)

**GREEN - After implementation:**
1. MCP browser: create named session "test worktree"
2. In terminal, agent runs `pwd` → shows `/workspace/.swe-swe/worktrees/test-worktree`
3. In terminal, agent runs `git branch` → shows `* test-worktree`
4. In terminal, agent runs `ls` → shows full repo contents
5. Create another session with same name → gets `test-worktree-a3f2` (no conflict)
6. Create session with empty name → runs in `/workspace` (backward compatible)

**Verify restart preserves cwd:**
1. In named session, kill the agent process
2. Auto-restart triggers
3. Agent runs `pwd` → still shows worktree path

---

## Phase 4: Remove TTL-based session expiry

### What will be achieved
Sessions will persist until manually deleted or server restart, rather than expiring after 1 hour of no viewers. This ensures worktree sessions remain available for later resume.

### Small steps

**Step 4a: Remove TTL condition from sessionReaper**
- Current code (line ~1105):
  ```go
  if sess.ClientCount() == 0 && time.Since(sess.LastActive()) > sessionTTL {
  ```
- Change to only reap sessions where process has exited:
  ```go
  if sess.Cmd.ProcessState != nil && sess.Cmd.ProcessState.Exited() {
  ```

**Step 4b: Remove `--session-ttl` flag entirely**
- Remove flag definition from `main()`
- Remove `sessionTTL` variable
- Remove log line that prints session-ttl value

**Step 4c: Update log message**
- Change "Session expired" to "Session cleaned up (process exited)"

### Verification

**Setup:** Test container workflow

**RED - Baseline (current behavior):**
1. Modify test container to use `--session-ttl=10s`
2. MCP browser: create session, note it appears on homepage
3. MCP browser: navigate away (disconnect)
4. Wait for TTL + reaper interval (~15s for test)
5. MCP browser: refresh homepage → session is gone (reaped)

**GREEN - After implementation:**
1. MCP browser: create session
2. MCP browser: navigate away (disconnect)
3. Wait 30+ seconds
4. MCP browser: refresh homepage → session still exists
5. Verify: session only disappears if process is killed externally or server restarts

---

## Notes

- Agents in worktrees can access `/workspace/.swe-swe/*.md` docs via absolute path
- Worktrees persist after session ends for later resume or manual PR creation
- Branch naming: if conflict, append random 4-char hex suffix
