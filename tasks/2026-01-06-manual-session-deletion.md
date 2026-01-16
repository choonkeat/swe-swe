# Manual Session Deletion

## Goal
Remove automatic session timeout and add manual session deletion via trash icon on homepage.

**Current behavior:** Sessions auto-expire after 1 hour of no viewers.
**New behavior:** Sessions persist indefinitely; users manually delete via trash icon (with confirmation showing session name + short UUID).

---

## Phase 1: Remove TTL-based session expiry

### What will be achieved
The `sessionReaper` function will no longer delete sessions based on idle time + no viewers. It will only clean up sessions where the underlying process has exited.

### Small steps

**Step 1a: Set test-friendly short durations**
- Change `session-ttl` default: `time.Hour` → `10*time.Second`
- Change reaper ticker: `time.Minute` → `5*time.Second`

**Step 1b: Baseline test (RED - session gets reaped)**
1. Build & run test container
2. MCP browser: navigate to homepage, start a session, note session appears
3. MCP browser: navigate back to homepage (disconnect)
4. Wait ~20 seconds
5. Refresh homepage → session should be gone (confirms current behavior works)

**Step 1c: Remove TTL condition in sessionReaper**
- Change condition from `sess.ClientCount() == 0 && time.Since(sess.LastActive()) > sessionTTL` to just `sess.Cmd.ProcessState != nil`
- Update log message

**Step 1d: Verify (GREEN - session persists)**
1. Rebuild & restart test container
2. Same test flow: start session, go back to homepage, wait ~20 seconds
3. Refresh homepage → session should still be there

**Step 1e: Restore production values**
- Restore `session-ttl` default to `time.Hour`
- Restore reaper ticker to `time.Minute`

---

## Phase 2: Add DELETE /session/:uuid endpoint

### What will be achieved
A new API endpoint that allows deleting a session by UUID. Returns 200 on success, 404 if not found.

### Small steps

**Step 2a: Add DELETE handler**
- In the main `http.HandleFunc("/", ...)` block, before the existing `/session/` GET handling
- Check `r.Method == "DELETE" && strings.HasPrefix(r.URL.Path, "/session/")`
- Extract UUID from path
- Lock `sessionsMu`, find session, call `sess.Close()`, delete from map, unlock
- Return 200 OK (or 404 if not found)

**Step 2b: Test DELETE endpoint (GREEN)**
1. Start test container
2. MCP browser: navigate to homepage, start a session, note the UUID from URL
3. Use `curl -X DELETE http://host.docker.internal:9899/session/{uuid}` via Bash
4. Verify 200 response
5. MCP browser: refresh homepage → session should be gone

**Step 2c: Test 404 case**
1. `curl -X DELETE http://host.docker.internal:9899/session/nonexistent-uuid`
2. Verify 404 response

---

## Phase 3: Add trash icon to homepage with confirmation

### What will be achieved
Each session item on the homepage will have a trash icon button. Clicking it shows a confirmation dialog with session name + short UUID. On confirm, it calls DELETE endpoint and refreshes the page.

### Small steps

**Step 3a: Add CSS for delete button**
- Add `.session-item__delete` styles (positioned on right side, subtle until hover)
- Style to match existing theme (gray, hover shows red/warning color)

**Step 3b: Modify session item HTML structure**
- Change from `<a class="session-item">` wrapping everything
- To: `<div class="session-item">` containing:
  - `<a>` for the clickable session link (uuid, viewers, duration)
  - `<button class="session-item__delete">` with trash icon
- Add `data-uuid` and `data-name` attributes for JS to read

**Step 3c: Add JavaScript for delete functionality**
- `deleteSession(uuid, name, uuidShort)` function
- Shows `confirm("Delete session 'name' (uuidShort)?")` (or just `"Delete session (uuidShort)?"` if no name)
- On confirm: `fetch('/session/' + uuid, {method: 'DELETE'})` then `location.reload()`

**Step 3d: Test (GREEN)**
1. MCP browser: navigate to homepage
2. Start a session, go back to homepage
3. Click trash icon on the session
4. Verify confirmation dialog shows with correct name/UUID
5. Accept confirmation
6. Verify session disappears from homepage

**Step 3e: Test cancel confirmation**
1. Create another session
2. Click trash icon, cancel the confirmation
3. Verify session still exists

---

## Files to modify

- `cmd/swe-swe/templates/host/swe-swe-server/main.go` - sessionReaper, DELETE handler
- `cmd/swe-swe/templates/host/swe-swe-server/static/selection.html` - trash icon UI + JS
