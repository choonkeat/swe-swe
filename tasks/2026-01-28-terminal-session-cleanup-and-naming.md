# Terminal Session Cleanup and Naming

**Date**: 2026-01-28
**Status**: Planned

## High Level Goal

Fix the confusing Terminal tab UX where users see old "[Process exited (code 0)]" output when returning to a dead shell session, and improve shell session naming to inherit from parent agent session.

## Background

When a user exits a shell session (Ctrl+D) and later returns to the Terminal tab:
1. The page reloads with fresh JS state (`processExited = false`)
2. Connects to server with same session UUID
3. Server returns existing dead session (process exited but not yet reaped)
4. Server sends old ring buffer including "[Process exited (code 0)]"
5. User sees confusing old output, has to wait up to 1 minute for session reaper

Additionally, shell sessions opened from agent sessions have no name, making recordings hard to identify.

---

## Phase 1: Clean up dead sessions on reconnect

### What will be achieved

When a user navigates back to the Terminal tab after exiting a shell session, they will immediately get a fresh shell instead of seeing the old "[Process exited (code 0)]" output.

### Small steps

1. **Modify `getOrCreateSession()` in `main.go`** - Add a check at the beginning: if the existing session's process has exited (`sess.Cmd.ProcessState != nil && sess.Cmd.ProcessState.Exited()`), clean up the old session and fall through to create a new one.

2. **Clean up the dead session properly** - Call `sess.Close()` and `delete(sessions, sessionUUID)` before creating the new session.

3. **Log the cleanup** - Add a log line indicating a dead session was cleaned up on reconnect.

### Verification

**Using dev server workflow:**

```bash
# 1. Start dev server
make stop && make run > /tmp/server.log 2>&1 &

# 2. Via MCP browser, navigate to:
#    http://swe-swe:3000/session/test-dead-session?assistant=shell

# 3. In the terminal, type: exit

# 4. Navigate away (e.g., to http://swe-swe:3000/)

# 5. Navigate back to same URL:
#    http://swe-swe:3000/session/test-dead-session?assistant=shell

# 6. Verify:
#    - Fresh shell prompt appears immediately
#    - No old "[Process exited (code 0)]" output visible
#    - Check logs: cat /tmp/server.log | grep "cleaned up"
```

**Regression check:**
- Session reaper still works (handles sessions with no clients)
- Agent sessions with exit code 0 still don't auto-restart (per ADR-010)

---

## Phase 2: Shell sessions inherit parent name

### What will be achieved

When a user opens a Terminal tab from an agent session (e.g., Claude session named "fixing bug"), the shell session will automatically be named "fixing bug (Terminal)".

### Small steps

1. **Add `ParentUUID` field to Session struct** - Add `ParentUUID string` to track the parent session relationship.

2. **Store ParentUUID when creating shell session** - In `handleWebSocket()`, when `parentUUID` query param is present, store it in the new session.

3. **Inherit parent's name with suffix** - When creating shell session with a parent, look up parent's name and set shell session's name to `{parentName} (Terminal)`.

4. **Update Metadata accordingly** - Ensure `sess.Metadata.Name` is also set so recordings have the inherited name.

### Verification

**Using dev server workflow:**

```bash
# 1. Start dev server
make stop && make run > /tmp/server.log 2>&1 &

# 2. Via MCP browser, create a named agent session:
#    http://swe-swe:3000/session/parent-session?assistant=claude&name=fixing-bug

# 3. Note the session name shows "fixing-bug" in the UI

# 4. Open shell session (simulating Terminal tab click):
#    http://swe-swe:3000/session/child-shell?assistant=shell&parent=parent-session

# 5. Verify:
#    - Shell session name shows "fixing-bug (Terminal)"
#    - Check logs: cat /tmp/server.log | grep "Terminal"
#    - Check metadata file exists with correct name
```

**Edge cases to test:**
- Parent has no name -> Shell session also has no name
- Parent doesn't exist -> Shell session created without inherited name

**Regression check:**
- Shell sessions without parent param still work normally
- Agent sessions unaffected

---

## Phase 3: Propagate rename to child sessions

### What will be achieved

When a user renames a parent session, all child sessions (Terminal tabs) will automatically update their names.

### Small steps

1. **Modify `rename_session` handler in `handleWebSocket()`** - After updating the parent session's name, iterate through all sessions to find children.

2. **Find child sessions by ParentUUID** - Loop through `sessions` map, check if `childSess.ParentUUID == sess.UUID`.

3. **Update child session names** - For each child, set new name to `{newParentName} (Terminal)`.

4. **Save child metadata and broadcast** - Call `childSess.saveMetadata()` and `childSess.BroadcastStatus()` so the child's UI updates and recordings are correct.

5. **Handle edge case: parent renamed to empty** - If parent name is cleared, child name should also be cleared.

### Verification

**Using dev server workflow:**

```bash
# 1. Start dev server
make stop && make run > /tmp/server.log 2>&1 &

# 2. Create parent session with name:
#    http://swe-swe:3000/session/parent-rename-test?assistant=claude&name=original-name

# 3. Create child shell session:
#    http://swe-swe:3000/session/child-rename-test?assistant=shell&parent=parent-rename-test

# 4. Verify child shows "original-name (Terminal)"

# 5. In parent session, rename to "new-name" via UI (click session name)

# 6. Verify:
#    - Child session now shows "new-name (Terminal)"
#    - Both metadata files updated
#    - Check logs: cat /tmp/server.log | grep "renamed"
```

**Edge cases to test:**
- Rename parent to empty string -> Child name becomes empty
- Multiple children -> All children update
- Child session has no active WebSocket clients -> Metadata still updates

**Regression check:**
- Sessions without children rename normally
- Sessions without parents rename normally
- Rename validation still enforced

---

## Files to Modify

- `cmd/swe-swe/templates/host/swe-swe-server/main.go`
  - `Session` struct: add `ParentUUID` field
  - `getOrCreateSession()`: add dead session cleanup
  - `handleWebSocket()`: store ParentUUID and inherit name
  - `rename_session` handler: propagate to children

## Testing Approach

All phases use the lightweight dev server workflow (`make run` on port 3000) for fast iteration. No container rebuilds needed since all changes are in `main.go`.
