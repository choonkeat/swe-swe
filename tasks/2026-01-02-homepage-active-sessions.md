# Homepage Active Sessions Feature

## Goal

Enhance the homepage (`selection.html`) to display a unified list of agents with their active sessions, replacing the current agent-only grid.

**Display for each agent:**
- Active sessions (UUID, connected users, duration)
- "Start new session" action

**Example layout:**
```
🧠 Claude (2 sessions)
   abc12 │ 2 viewers │ 5m ago    [Join]
   def34 │ 1 viewer  │ 1h ago    [Join]
   + Start new session

✨ Gemini
   + Start new session

🛠 Aider (1 session)
   ghi56 │ 0 viewers │ 30m ago   [Join]
   + Start new session
```

---

## Phase 1: Backend Data Model ✅

### What will be achieved
Add a `CreatedAt` field to the Session struct so we can display how long each session has been running.

### Steps
- [x] Add `CreatedAt time.Time` field to the `Session` struct (line 92-105 in main.go)
- [x] Set `CreatedAt: time.Now()` when creating the session in `getOrCreateSession()` (around line 795)

### Verification
- [x] **Build check**: Run `go build` to ensure no compile errors
- [x] **Existing tests**: Run `go test ./...` to verify no regressions
- [x] **Manual check**: The field is only used for display - existing functionality unchanged

---

## Phase 2: Template Data ✅

### What will be achieved
The `/` handler will pass active session information to the template, **filtering out sessions where the process has exited**.

### Steps
- [x] Create structs for template data:
   ```go
   type SessionInfo struct {
       UUID          string
       UUIDShort     string
       Assistant     string        // binary name for URL
       AssistantName string
       ClientCount   int
       CreatedAt     time.Time
   }

   type AgentWithSessions struct {
       Assistant AssistantConfig
       Sessions  []SessionInfo  // sorted by CreatedAt desc
   }
   ```

- [x] In the `/` handler:
   - Lock `sessionsMu.RLock()`
   - Iterate over `sessions` map
   - **Skip sessions where `sess.Cmd.ProcessState != nil`** (process exited)
   - Group sessions by assistant
   - Sort sessions within each group by `CreatedAt` desc (most recent first)
   - Unlock

- [x] Build `[]AgentWithSessions` for all available assistants (including those with no sessions)

- [x] Update template data struct:
   ```go
   data := struct {
       Agents  []AgentWithSessions
       NewUUID string
   }{...}
   ```

### Verification
- [x] **Build check**: `go build`
- [x] **Existing tests**: `go test ./...`
- [ ] **Manual check**:
   - Start a session, refresh homepage → session appears under agent
   - Exit the process cleanly → refresh homepage → session gone

---

## Phase 3: Frontend Template - Unified Layout ✅

### What will be achieved
Replace the current agent grid with a unified list grouped by agent, showing active sessions and a "Start new session" action for each agent.

### Steps
- [x] Replace the grid layout in `selection.html` with grouped list:
   - For each agent: show icon + name + session count (only if > 0)
   - For each session under agent: UUID short, viewer count, duration, join link
   - Always show "+ Start new session" link at bottom of each agent group

- [x] Update CSS:
   - Remove grid styles (or repurpose)
   - Add styles for grouped list layout (indented sessions, hover states)
   - Keep dark theme consistency

- [x] Duration formatting via `formatDuration()` helper in Go (e.g., "5m", "1h 23m")

### Verification
- [x] **Build check**: `go build`
- [x] **Existing tests**: `go test ./...`
- [ ] **Manual check**:
  - No sessions anywhere: Each agent shows just "+ Start new session"
  - Sessions exist: Agent groups show session count, sessions listed with details
  - Join existing: Clicking session joins it (viewer count increases)
  - Start new: Clicking "Start new session" creates new UUID
  - Order: Sessions within agent sorted by most recent first
  - Visual: Clean, readable, consistent with dark theme

---

## Design Decisions

1. **No API endpoint**: Data rendered server-side directly in template
2. **No JavaScript polling**: Users refresh page manually for updates
3. **Filter dead sessions**: Sessions with exited processes (`Cmd.ProcessState != nil`) are not shown
4. **Unified layout**: Single list grouped by agent instead of separate "agents" and "sessions" sections
5. **Always expanded**: No collapse/expand functionality (no JS required)
