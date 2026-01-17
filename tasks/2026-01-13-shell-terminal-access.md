# Shell Terminal Access for swe-swe Sessions

## Goal

Add shell terminal access to swe-swe sessions, allowing users to open a dedicated shell terminal that inherits the working directory from a parent AI session. The shell appears as a peer to VSCode and Browser in the status bar, positioned as a supporting tool to augment AI conversations.

## URL Structure

```
/session/{shell-uuid}?assistant=shell&parent={parent-uuid}&debug
```

- `shell-uuid`: Deterministic UUID derived from parent UUID (clicking Shell multiple times reopens same session)
- `parent`: Parent AI session UUID, used to resolve working directory
- `debug`: Propagated from parent session if present

---

## Phase 1: Backend - Add shell assistant type ✅ COMPLETE

### What will be achieved
The backend will recognize `shell` as a valid assistant type, filter it from the homepage, and support the `parent` query parameter to inherit the working directory from a parent session.

### Steps

1. **Add `Homepage` field to `AssistantConfig` struct**
   - Location: `cmd/swe-swe/templates/host/swe-swe-server/main.go` line ~133
   - Add `Homepage bool` field
   - Update all existing assistant configs to have `Homepage: true`

2. **Add `shell` to `assistantConfigs`**
   - Add entry with `Homepage: false`
   - `Binary: "shell"` (for recording grouping)
   - `ShellCmd`, `ShellRestartCmd` empty (resolved at runtime)

3. **Update `detectAvailableAssistants()` to filter by `Homepage`**
   - Location: `main.go` line ~980
   - Only add to `availableAssistants` if `cfg.Homepage == true`

4. **Add runtime shell command resolution**
   - In session creation, if assistant is `shell`:
     - Get `$SHELL` env var, fallback to `bash`
     - Set command to `{shell} -l`

5. **Parse `parent` query parameter in WebSocket handler**
   - Extract `parent` from query string
   - Look up parent session by UUID
   - If found, use parent's `WorkDir` for the shell session
   - If not found, use default `/workspace`

### Verification

1. `make test` - ensure existing tests pass
2. `make build golden-update` - verify no unexpected golden changes
3. Boot test container (docs/dev/test-container-workflow.md)
4. MCP browser: Navigate to homepage, verify no "Shell" option visible
5. MCP browser: Create AI session with named worktree
6. MCP browser: Navigate to `/session/{new-uuid}?assistant=shell&parent={ai-uuid}`
7. MCP browser: Verify shell starts, run `pwd`, confirm correct worktree directory
   - **Critical**: Worktree sessions have unique pwd (e.g., `/workspace/.worktrees/session-name`), not `/workspace` - this is the key case to verify
8. MCP browser: Test `/session/{uuid}?assistant=shell` (no parent) starts in `/workspace`
9. Shutdown test container

---

## Phase 2: Frontend - Add Shell link to status bar ✅ COMPLETE

### What will be achieved
The status bar in AI sessions will show a "Shell" link alongside VSCode and Browser. Clicking it opens a shell session that inherits the parent session's working directory. Shell sessions will not show the Shell link.

### Steps

1. **Add deterministic UUID derivation function**
   - Location: `cmd/swe-swe/templates/host/swe-swe-server/static/terminal-ui.js`
   - Use Web Crypto API to derive shell UUID from parent UUID
   - Function: `async deriveShellUUID(parentUUID)` using SHA-256 hash

2. **Add Shell link to status bar**
   - Location: `static/terminal-ui.js` (status bar rendering)
   - Add "Shell" link before "VSCode" in the status bar
   - URL format: `/session/{derived-uuid}?assistant=shell&parent={current-uuid}`
   - Propagate `?debug` flag if present in current URL

3. **Hide Shell link when viewing a shell session**
   - Check if current `assistant` param is `shell`
   - If so, don't render the Shell link in status bar
   - VSCode and Browser links remain visible

4. **Handle link click behavior**
   - Opens in new tab (same as VSCode/Browser)
   - Deterministic UUID means clicking multiple times reopens same session

### Verification

1. `make test` - ensure no regressions
2. Boot test container
3. MCP browser: Navigate to AI session
4. MCP browser: Verify status bar shows `[Shell] [VSCode] [Browser]`
5. MCP browser: Click Shell link - verify new tab opens with shell session
6. MCP browser: Verify shell session's status bar shows `[VSCode] [Browser]` (no Shell)
7. MCP browser: Click Shell link again from AI session - verify same shell (same UUID)
8. MCP browser: Test with `?debug` flag - verify propagation to shell URL
9. Shutdown test container

---

## Phase 3: Integration testing & polish ✅ COMPLETE

### What will be achieved
End-to-end verification that all pieces work together, edge cases are handled gracefully, and the feature is production-ready.

### Steps

1. **Test parent session edge cases**
   - Parent not found: Shell starts in `/workspace` (graceful fallback)
   - Parent session ended: Still use its `WorkDir` if available in sessions map
   - Invalid parent UUID format: Treat as not found, use default

2. **Test session lifecycle**
   - Shell session persists after parent ends (own UUID, own recording)
   - Shell can be reconnected after disconnect (scrollback preserved)
   - Shell recording appears in recordings list grouped under "shell"

3. **Test across browsers (if applicable)**
   - Verify deterministic UUID derivation works consistently
   - Verify WebSocket connection works for shell sessions

4. **Polish**
   - Ensure shell sessions have sensible display name in recordings
   - Verify `$SHELL` resolution works (test with different shells if possible)

### Verification

1. `make test` passes
2. `make build golden-update` shows no unexpected changes
3. Boot test container
4. MCP browser: Create named AI session (triggers worktree creation)
5. MCP browser: Open shell from status bar
6. MCP browser: Verify shell is in worktree directory (e.g., `/workspace/.worktrees/session-name`, not `/workspace`)
7. MCP browser: Run some commands in shell
8. MCP browser: Close shell tab, reopen from AI session - verify same session (scrollback preserved)
9. MCP browser: End AI session, verify shell session still works independently
10. MCP browser: Check recordings - verify shell session is recorded
11. MCP browser: Navigate to `/session/{random-uuid}?assistant=shell&parent=nonexistent` - verify starts in `/workspace`
12. Shutdown test container

---

## Files to Modify

| File | Changes |
|------|---------|
| `cmd/swe-swe/templates/host/swe-swe-server/main.go` | Add `Homepage` field, add `shell` config, filter homepage, parse `parent` param, resolve shell command |
| `cmd/swe-swe/templates/host/swe-swe-server/static/terminal-ui.js` | Add `deriveShellUUID()`, add Shell link to status bar, hide for shell sessions, propagate debug flag |

## Design Decisions

1. **Shell as 2nd class citizen** - Not shown on homepage, only accessible via status bar link from AI sessions
2. **Deterministic UUID** - Clicking Shell multiple times reopens same session (like page reload)
3. **Parent inheritance** - Shell inherits `WorkDir` from parent session (worktree aware). This is especially important for worktree sessions which have unique pwd like `/workspace/.worktrees/session-name`
4. **`$SHELL` respect** - Uses user's preferred shell, not hardcoded bash
5. **Debug propagation** - `?debug` flag passed through to shell sessions
6. **No Shell link in shell** - Avoid confusion, shell status bar only shows VSCode/Browser
