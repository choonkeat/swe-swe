# Homepage: Join Session in Worktree

## Goal

Enable the homepage to show "Join session in {worktree}" instead of "Start new session in {worktree}" when a worktree already has an active session.

This provides a clearer worktree-centric view, especially when sessions have been renamed and it's not obvious which worktree they belong to.

---

## Phase 1: Backend - Extend `/api/worktrees` response

### What will be achieved

The `/api/worktrees` endpoint will return additional information about active sessions running in each worktree. The response will change from:

```json
{
  "worktrees": [
    {"name": "feat/foo", "path": "/workspace/.swe-swe/worktrees/feat--foo"}
  ]
}
```

To:

```json
{
  "worktrees": [
    {
      "name": "feat/foo",
      "path": "/workspace/.swe-swe/worktrees/feat--foo",
      "activeSession": {
        "uuid": "abc-123-...",
        "name": "My Custom Name",
        "assistant": "claude",
        "clientCount": 2,
        "durationStr": "5m"
      }
    }
  ]
}
```

The `activeSession` field will be omitted when no active session exists for that worktree.

### Small steps

1. Add `WorktreeSessionInfo` struct with fields: `UUID`, `Name`, `Assistant`, `ClientCount`, `DurationStr`
2. Add `ActiveSession *WorktreeSessionInfo` field to existing `WorktreeInfo` struct
3. Modify `handleWorktreesAPI` to:
   - Lock `sessionsMu` and build a map of `branchName → *Session` for active (non-exited) sessions
   - After calling `listWorktrees()`, iterate and populate `ActiveSession` for worktrees with matching branch names
   - Use existing `formatDuration()` helper for `DurationStr`

### Verification (TDD-style)

**Red**: Add a new test `TestHandleWorktreesAPI_WithActiveSession` in `worktree_test.go` that:
- Creates a mock session in the global `sessions` map with a specific `BranchName`
- Creates a matching worktree directory
- Calls `handleWorktreesAPI`
- Asserts the response includes `activeSession` with expected fields

Run `make test-server` - test should fail (no `activeSession` field yet).

**Green**: Implement the backend changes. Run `make test-server` - test should pass.

**Regression guarantee**:
- Existing `TestHandleWorktreesAPI` tests continue to pass (they don't check for `activeSession`)
- The `path` and `name` fields are unchanged
- `activeSession` is `omitempty` so existing clients that don't use it won't break

---

## Phase 2: Frontend - Update worktree link rendering

### What will be achieved

1. Extract session link rendering into a **shared JavaScript function** that generates consistent HTML for session links
2. Use this shared function for both:
   - Session items in the sessions list (existing)
   - "Join session in {worktree}" links (new)
3. "Start new session in {worktree}" remains separate (different behavior)

### Small steps

1. **Extract shared function** `createSessionLink(session, options)`:
   - Takes session info: `uuid`, `name`, `assistant`, `clientCount`, `durationStr`
   - Takes options: `worktreeName` (optional - if provided, shows "Join session in {worktree}" text)
   - Returns consistent HTML structure matching existing `.session-item` styling
   - Handles debug flag consistently

2. **Refactor existing session list** (template around line 461-466):
   - This is server-rendered Go template, not JS
   - Option A: Keep server-rendered, just ensure the JS-generated "Join" links match the same HTML structure/classes
   - Option B: Move to client-side rendering using the shared function
   - **Decision**: Option A - just ensure the JS output matches the template structure

3. **Update worktree link rendering** (JS around line 755):
   - When `wt.activeSession` exists: generate HTML matching `.session-item` structure
   - When no active session: keep existing "Start new session" logic

4. **Ensure CSS classes match** so both render identically

### Verification

**Red**: `make build` to ensure templates compile.

**Green**:
- Session links in list and "Join session in {worktree}" links have identical HTML structure
- Both navigate to the same URL
- Visual appearance is consistent

**Regression guarantee**:
- Existing session list rendering unchanged (still server-rendered)
- JS-generated links match the same structure/classes

---

## Notes

- Redundancy is intentional: sessions appear both in the sessions list AND as "Join session in {worktree}" to provide worktree context when sessions are renamed
- Both links navigate to identical destination (`/session/{UUID}?assistant={assistant}`)
- Code is shared to prevent the two link types from getting out of sync
