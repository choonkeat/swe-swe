# Session Naming Feature

## Goal
Allow users to name their sessions. The name appears in the homepage listing and status bar. Clicking the session name in the status bar opens a prompt to rename it.

## Design Decisions
- **Unnamed display**: "Unnamed session {uuid-short}" (e.g., "Unnamed session a3c12")
- **Persistence**: In-memory only (sessions are ephemeral)
- **Who can rename**: Anyone connected
- **Broadcast on rename**: Yes, immediate to all clients
- **Name validation**: Max 32 chars, alphanumeric + spaces + hyphens + underscores
- **Homepage display**: Name + UUID short if named (e.g., "my-feature (a3c12)"), just UUID short if unnamed

---

## Phase 1: Server Data Model ✅

### What will be achieved
The server will store session names and broadcast them to connected clients via the status message.

### Steps

1. ✅ **Add `Name` field to `Session` struct** (`main.go:142`)
   - Add `Name string` field after `UUID`

2. ✅ **Add `Name` field to `SessionInfo` struct** (`main.go:75`)
   - Add `Name string` field for template rendering

3. ✅ **Include `sessionName` and `uuidShort` in status broadcast** (`main.go:317-318`)
   - Add `"sessionName": s.Name` to the status map
   - Add `"uuidShort": s.UUID[:5]` (with length check)

4. ✅ **Populate `Name` in `SessionInfo` when building homepage data** (`main.go:809`)
   - Add `Name: sess.Name` to the SessionInfo initialization

### Verification

| Test | How |
|------|-----|
| ✅ **Build succeeds** | `make build` |
| **Test container runs** | `./scripts/02-test-container-build.sh && HOST_PORT=11977 HOST_IP=host.docker.internal ./scripts/03-test-container-run.sh` |
| **New fields in WebSocket** | MCP browser -> join session -> verify `sessionName` and `uuidShort` in status message |
| **No regression on homepage** | MCP browser -> verify homepage lists sessions with UUID short |
| **No regression on status bar** | MCP browser -> verify status bar still shows "Connected as X with Y" |
| **Container down** | `./scripts/04-test-container-down.sh` |

---

## Phase 2: Server Rename Handler ✅

### What will be achieved
The server will accept `rename_session` WebSocket messages and update the session name, then broadcast the new name to all connected clients.

### Steps

1. ✅ **Add `Name` field to WebSocket message struct** (`main.go:1118`)
   - Add `Name string` to the JSON message struct for parsing

2. ✅ **Add `rename_session` case in message switch** (`main.go:1143-1166`)
   - Handle `"rename_session"` message type
   - Validate name (max 32 chars, alphanumeric + spaces + hyphens + underscores)
   - Update `sess.Name` with lock
   - Call `sess.BroadcastStatus()` to notify all clients

### Verification

| Test | How |
|------|-----|
| **Build succeeds** | `make build` |
| **Rename via WebSocket works** | MCP browser -> join session -> `browser_evaluate` to send `{"type": "rename_session", "name": "test-name"}` via WebSocket -> verify status broadcast contains new name |
| **Invalid names rejected** | Send name with invalid chars or >32 chars -> verify session name unchanged |
| **All clients receive update** | Open 2 browser tabs on same session -> rename in one -> verify both receive updated status |
| **Container down** | `./scripts/04-test-container-down.sh` |

---

## Phase 3: Homepage Display

### What will be achieved
The homepage will display session names alongside UUID short. Format: `my-feature (a3c12)` if named, just `a3c12` if unnamed.

### Steps

1. **Update session item template** (`selection.html:223`)
   - Change from: `<span class="session-item__uuid">{{.UUIDShort}}</span>`
   - Change to: Display `{{.Name}} ({{.UUIDShort}})` if Name exists, else just `{{.UUIDShort}}`

### Verification

| Test | How |
|------|-----|
| **Build succeeds** | `make build` |
| **Unnamed session shows UUID short only** | MCP browser -> homepage -> verify unnamed session shows just `a3c12` format |
| **Named session shows name + UUID** | Rename a session via Phase 2's WebSocket method -> refresh homepage -> verify shows `test-name (a3c12)` format |
| **No layout breakage** | Verify session items still align properly with viewers count and duration |
| **Container down** | `./scripts/04-test-container-down.sh` |

---

## Phase 4: Frontend Status Bar

### What will be achieved
The status bar will display the session name after the assistant name. Format: `Connected as Alice with Claude on my-feature` (if named) or `Connected as Alice with Claude on Unnamed session a3c12` (if unnamed).

### Steps

1. **Store sessionName and uuidShort from status messages** (`terminal-ui.js:1273`)
   - Add `this.sessionName = msg.sessionName || ''`
   - Add `this.uuidShort = msg.uuidShort || ''`

2. **Update status bar HTML generation** (`terminal-ui.js:1338`)
   - After the assistant link, append session name element
   - Format: ` on <span class="terminal-ui__status-link terminal-ui__status-session">{display}</span>`
   - Display logic: if `sessionName` exists use it, else `Unnamed session {uuidShort}`

3. **Add CSS for session name link** (if needed)
   - Style `terminal-ui__status-session` similar to other status links

### Verification

| Test | How |
|------|-----|
| **Build succeeds** | `make build` |
| **Unnamed session shows fallback** | MCP browser -> join new session -> verify status bar shows "Connected as X with Y on Unnamed session a3c12" |
| **Named session shows name** | Rename session via WebSocket -> verify status bar updates to show "Connected as X with Y on my-feature" |
| **Clickable appearance** | Verify session name has same link styling as username/assistant |
| **Container down** | `./scripts/04-test-container-down.sh` |

---

## Phase 5: Frontend Rename Flow

### What will be achieved
Clicking the session name in the status bar opens a prompt to rename it. The name is validated, sent to the server, and the status bar updates.

### Steps

1. **Add `validateSessionName` function** (`terminal-ui.js`, near `validateUsername`)
   - Max 32 chars
   - Allow: letters, numbers, spaces, hyphens, underscores
   - Trim whitespace
   - Empty string is valid (clears the name)

2. **Add `promptRenameSession` function** (`terminal-ui.js`, near `promptRenameUsername`)
   - Show prompt: `"Enter session name (max 32 chars):"` with current name as default
   - If cancelled (null), return
   - Validate input
   - If invalid, alert error and loop
   - If valid, send `{"type": "rename_session", "name": "..."}` via WebSocket

3. **Add click handler for session name** (`terminal-ui.js`, in status bar click handler ~line 1829)
   - Check if click target has class `terminal-ui__status-session`
   - Call `promptRenameSession()`

### Verification

| Test | How |
|------|-----|
| **Build succeeds** | `make build` |
| **Click opens prompt** | MCP browser -> join session -> click session name in status bar -> verify prompt appears with current name |
| **Valid name accepted** | Enter "my-feature" -> verify status bar updates, WebSocket message sent |
| **Empty name clears** | Enter empty string -> verify status bar shows "Unnamed session a3c12" |
| **Invalid name rejected** | Enter "test@#$" -> verify alert shown, prompt loops |
| **Cancel does nothing** | Click cancel in prompt -> verify no change |
| **Homepage reflects change** | After rename -> navigate to homepage -> verify session shows new name |
| **Container down** | `./scripts/04-test-container-down.sh` |

---

## Phase 6: Golden Update & Commit

### What will be achieved
Golden test files are updated to reflect the template changes, and all changes are committed.

### Steps

1. **Run golden update**
   - `make build golden-update`

2. **Review golden diff**
   - `git add -A cmd/swe-swe/testdata/golden`
   - `git diff --cached -- cmd/swe-swe/testdata/golden`
   - Verify diff shows only expected changes (new `Name` field in structs, status broadcast changes, template changes)

3. **Commit all changes**
   - Commit server changes (main.go)
   - Commit frontend changes (terminal-ui.js)
   - Commit template changes (selection.html)
   - Commit golden files

### Verification

| Test | How |
|------|-----|
| **Golden tests pass** | `make build` after golden-update should succeed |
| **Diff is minimal** | Only session naming related changes in golden files |
| **No unintended changes** | Review full `git diff` before commit |

---

## Files to Modify

| File | Changes |
|------|---------|
| `cmd/swe-swe/templates/host/swe-swe-server/main.go` | Session struct, SessionInfo struct, status broadcast, rename handler |
| `cmd/swe-swe/templates/host/swe-swe-server/static/selection.html` | Session name display in listing |
| `cmd/swe-swe/templates/host/swe-swe-server/static/terminal-ui.js` | Status bar display, rename prompt, validation, click handler |
| `cmd/swe-swe/testdata/golden/*` | Updated golden files |
