# Readonly Viewer Mode for swe-swe

## Goal

Implement a readonly viewer mode for swe-swe that allows users to observe terminal sessions without being able to interact with them. **All users** (editors and viewers) must provide their name at login. The name and role are encoded in the auth cookie. Viewers authenticate with `READONLY_PASSWORD` and can only chat. Editors authenticate with `SWE_PASSWORD` and have full access.

## Security Architecture

```
┌─────────────────────────────────────────────────────────────┐
│ Auth Service (auth/main.go)                                 │
│  - Login with SWE_PASSWORD → sets X-SWE-Role: editor        │
│  - Login with READONLY_PASSWORD → sets X-SWE-Role: viewer   │
│  - Role + name encoded in cookie: timestamp|role|name|hmac  │
└─────────────────────────────────────────────────────────────┘
                              ↓
┌─────────────────────────────────────────────────────────────┐
│ Traefik (ForwardAuth response headers)                      │
│  - Forwards X-SWE-Role and X-SWE-Name headers to backend    │
└─────────────────────────────────────────────────────────────┘
                              ↓
┌─────────────────────────────────────────────────────────────┐
│ swe-swe-server                                              │
│  - On WS upgrade: reads X-SWE-Role and X-SWE-Name           │
│  - Stores role per-connection in wsClients map              │
│  - DROPS all PTY input from viewer connections              │
│  - Enforces server-known name for chat (ignores client)     │
└─────────────────────────────────────────────────────────────┘
```

## Message Types Reference

### Binary Messages (client → server)
| Prefix | Type | Editor | Viewer |
|--------|------|--------|--------|
| (none) | Terminal input | ✓ Allow | ✗ Block |
| `0x00` | Resize | ✓ Allow | ✗ Block |
| `0x01` | File upload | ✓ Allow | ✗ Block |

### Text Messages (client → server)
| Type | Editor | Viewer |
|------|--------|--------|
| `ping` | ✓ Allow | ✓ Allow |
| `chat` | ✓ Allow | ✓ Allow (name enforced by server) |
| `rename_session` | ✓ Allow | ✗ Block |

---

## Phase 1: Auth Service - Name + Role in Cookie

### What Will Be Achieved
The auth service will require all users to provide their name and password at login. The login page will have Editor/Viewer tabs (Viewer tab always visible for feature awareness). The cookie will encode `timestamp|role|name|hmac`, and successful verification will set `X-SWE-Role` and `X-SWE-Name` headers for downstream services.

### Steps

1. **Update cookie format**
   - Change from `timestamp|hmac` to `timestamp|role|name|hmac`
   - Update `signCookie()` to accept role and name parameters
   - Update `verifyCookie()` to parse and return role and name
   - HMAC covers all fields (timestamp, role, name) for integrity

2. **Update login handler**
   - Accept `name` field (required, non-empty)
   - Accept `password` field
   - Accept `mode` field (`editor` or `viewer`)
   - Validate password against `SWE_PASSWORD` or `READONLY_PASSWORD` based on mode
   - If mode=viewer and `READONLY_PASSWORD` is not set/blank → reject login
   - Set cookie with appropriate role and provided name

3. **Update login UI (login.html)**
   - Remove readonly "admin" username field
   - Add "Name" input field (required)
   - On page load, autofill Name field from `localStorage.getItem('swe_swe_username')`
   - Add Editor/Viewer tab toggle (both always visible/enabled for feature awareness)
   - Form submits: name, password, mode

4. **Update verify handler**
   - On valid cookie: set `X-SWE-Role` and `X-SWE-Name` headers, return 200
   - On invalid/missing cookie: return 302 redirect to `/swe-swe-auth/login`

5. **Environment variable handling**
   - `SWE_PASSWORD` - required (fail startup if not set)
   - `READONLY_PASSWORD` - optional (viewer login rejected if not set, but tab still shown)

### Verification

**Test setup:**
```bash
cd /workspace/cmd/swe-swe/templates/host/auth
SWE_SWE_PASSWORD=testpass READONLY_PASSWORD=viewpass go run main.go
# Listens on http://localhost:4180
```

**MCP browser tests:**

1. **Login page renders correctly**
   - Navigate to `http://localhost:4180/swe-swe-auth/login`
   - Verify: Name input field exists (not readonly)
   - Verify: Password input field exists
   - Verify: Editor/Viewer tabs both visible and enabled

2. **Editor login with valid credentials**
   - Fill name: "Alice", password: "testpass", select Editor tab
   - Submit → Verify: Redirected, cookie contains `editor` and `Alice`

3. **Viewer login with valid credentials**
   - Clear cookies, fill name: "Bob", password: "viewpass", select Viewer tab
   - Submit → Verify: Cookie contains `viewer` and `Bob`

4. **Login rejected with empty name**
   - Fill name: "", password: "testpass" → Verify: Error, no redirect

5. **Login rejected with wrong password**
   - Fill name: "Alice", password: "wrongpass" → Verify: Error, no redirect

6. **Verify endpoint returns headers**
   - With valid editor cookie, call `/swe-swe-auth/verify`
   - Verify: 200, `X-SWE-Role: editor`, `X-SWE-Name: Alice`

7. **Invalid cookie redirects to login**
   - Set garbage cookie, navigate to `/swe-swe-auth/verify`
   - Verify: 302 redirect to `/swe-swe-auth/login`

8. **Viewer login rejected when READONLY_PASSWORD not set**
   - Restart auth server: `SWE_SWE_PASSWORD=testpass go run main.go`
   - Navigate to login, verify Viewer tab is still visible/clickable
   - Fill name: "Bob", password: "anything", select Viewer tab
   - Submit → Verify: Error message (e.g., "Viewer mode not configured"), no redirect

---

## Phase 2: Traefik - Forward Role & Name Headers

### What Will Be Achieved
Traefik's ForwardAuth middleware will be configured to pass the `X-SWE-Role` and `X-SWE-Name` headers (set by the auth service) through to backend services.

### Steps

1. **Update Traefik dynamic configuration**
   - In the ForwardAuth middleware config, add `authResponseHeaders` to forward `X-SWE-Role` and `X-SWE-Name` from auth response to upstream requests

### Files to Modify

- `/workspace/cmd/swe-swe/templates/host/traefik/dynamic.yml` (or equivalent)

### Verification

**Manual testing on your instance:**

1. Deploy updated auth service + Traefik config
2. Login as editor with name "Alice"
3. Add temporary debug logging to swe-swe-server's WebSocket handler to log incoming request headers:
   ```go
   log.Printf("Headers: Role=%s, Name=%s", r.Header.Get("X-SWE-Role"), r.Header.Get("X-SWE-Name"))
   ```
4. Connect to a terminal session, check container logs for the headers
5. Remove debug logging after verification

---

## Phase 3: swe-swe-server - Role-Based Message Filtering

### What Will Be Achieved
The swe-swe-server will read the user's role and name from request headers on WebSocket upgrade, track them per-connection, filter messages based on role (blocking terminal input, resize, file upload, and rename_session for viewers), and use the server-provided name for chat broadcasts.

### Steps

0. **Audit all HTTP endpoints and WebSocket handlers**
   - Review `main.go` for all `http.HandleFunc` registrations
   - Review WebSocket message handling for all message types (binary prefixes + JSON types)
   - Document any new endpoints/handlers added since this plan was written
   - For each, determine: should viewers have access? If not, add to block list

1. **Update client tracking structure**
   ```go
   type ClientInfo struct {
       Role string    // "editor" or "viewer"
       Name string    // From X-SWE-Name header
       Size TermSize  // Terminal dimensions
   }

   wsClients     map[*websocket.Conn]*ClientInfo  // was map[*websocket.Conn]bool
   ```

2. **Read headers on WebSocket upgrade**
   - In `handleWebSocket()`, extract `X-SWE-Role` and `X-SWE-Name` from `r.Header`
   - Default to `viewer` if role header missing (fail-secure)
   - Store in `ClientInfo` when adding client to session

3. **Filter binary messages by role**
   - Terminal input (no prefix): block for viewers
   - Resize `0x00`: block for viewers
   - File upload `0x01`: block for viewers

4. **Filter text messages by role**
   - `ping`: allow all
   - `chat`: allow all, but **override `userName` with server-known name**
   - `rename_session`: block for viewers

5. **Update status broadcast**
   - Include viewer count vs editor count, or list of names with roles

6. **Update `AddClient()` / `RemoveClient()` signatures**
   - Pass `ClientInfo` instead of just `TermSize`
   - PTY resize logic only considers editor sizes (viewers don't affect PTY)

### Verification

**Manual testing on your instance:**

1. Deploy updated swe-swe-server + auth + Traefik
2. Open two browser windows:
   - Window A: Login as editor "Alice"
   - Window B: Login as viewer "Bob"
3. Both connect to same terminal session
4. Test matrix:

| Action | Editor Alice | Viewer Bob |
|--------|--------------|------------|
| Type in terminal | ✓ Works | ✗ Dropped |
| See terminal output | ✓ | ✓ |
| Resize window affects PTY | ✓ | ✗ |
| Upload file | ✓ | ✗ |
| Rename session | ✓ | ✗ |
| Send chat | ✓ | ✓ |
| Chat shows correct name | ✓ | ✓ (server-enforced) |

5. Verify chat name enforcement:
   - Use browser devtools to send raw WebSocket message with fake `userName`
   - Confirm broadcast uses server-known name, not fake one

---

## Phase 4: Client UI - Viewer Mode & Name Sync

### What Will Be Achieved
The terminal UI will show visual indicators when connected as a viewer, disable input-related UI elements for viewers, sync the server-provided name to localStorage, and remove the redundant name prompt from chat (since name is now known from login).

### Steps

0. **Audit all client-side handlers and UI actions**
   - Review `terminal-ui.js` for all keyboard shortcuts (e.g., 'c' for chat, etc.)
   - Review all event handlers (click, drag, keypress, etc.)
   - Review all UI elements that trigger actions (buttons, inputs, dropdowns)
   - Document any new handlers/actions added since this plan was written
   - For each, determine: should viewers have access? If not, add to disable list

1. **Receive role and name from server via status message**
   - Update server's `BroadcastStatus()` to include `role` and `name` for the receiving client
   - Or send a one-time `welcome` message on connect with client's own role/name

2. **Store role and sync name on client**
   - On receiving status/welcome, store `this.role` and `this.currentUserName`
   - Sync name to `localStorage.setItem('swe_swe_username', name)` so it persists and autofills login next time

3. **Visual indicator for viewer mode**
   - Show "Viewing" badge or "(read-only)" text in status bar when `role === 'viewer'`
   - Different color/style to make it obvious

4. **Disable input UI for viewers** *(informed by audit)*
   - Terminal input: already blocked server-side, but optionally show visual feedback
   - File upload drop zone: hide or show "Viewers cannot upload" on drag
   - Session rename: hide or disable rename UI for viewers

5. **Remove name prompt from chat**
   - Currently `getUserName()` prompts if name not set
   - Now name is always known from login, so remove the prompt
   - Use `this.currentUserName` directly

6. **Update status display**
   - Show connected users with their roles (e.g., "Alice (editor), Bob (viewer)")
   - Or show counts: "1 editor, 2 viewers"

### Verification

**Manual testing on your instance:**

1. **Viewer sees visual indicator**
   - Login as viewer "Bob"
   - Connect to terminal
   - Verify: Status bar shows "Viewing" or "(read-only)" badge

2. **Viewer sees disabled state feedback**
   - As viewer, try typing in terminal
   - Verify: Optional feedback (e.g., subtle flash, or just nothing happens)
   - Drag file over terminal
   - Verify: Drop zone shows "Viewers cannot upload" or is hidden

3. **Name synced to localStorage**
   - Login as editor "Alice"
   - Open devtools → Application → localStorage
   - Verify: `swe_swe_username` is set to "Alice"
   - Logout, go to login page
   - Verify: Name field autofilled with "Alice"

4. **No name prompt on chat**
   - As editor "Alice", open chat input (press 'c' or click)
   - Verify: No prompt asking for name
   - Send message
   - Verify: Message shows "Alice" as sender

5. **Status shows connected users**
   - Editor Alice and Viewer Bob both connected
   - Verify: Status area shows both names (with roles or as counts)

---

## Phase 5: Server-Side Hardening

### What Will Be Achieved
Add defense-in-depth validation for user-provided strings (name at auth, chat messages at server) to prevent abuse and ensure clean data.

### Steps

1. **Name validation at auth service (auth/main.go)**
   - Max length: 50 characters
   - Min length: 1 character (non-empty, already covered)
   - Strip leading/trailing whitespace
   - Reject control characters (0x00-0x1F)
   - Reject names that are only whitespace

2. **Chat message validation at swe-swe-server (main.go)**
   - Max length: 500 characters
   - Reject empty messages
   - Reject control characters (except maybe newline for multi-line?)
   - Truncate or reject if too long (prefer reject with error)

3. **Name header validation at swe-swe-server**
   - Validate `X-SWE-Name` header on WebSocket upgrade
   - If missing/invalid, use fallback like "Anonymous" or reject connection
   - Should match same rules as auth validation (defense-in-depth)

### Verification

**Test auth name validation (standalone auth server):**
```bash
cd /workspace/cmd/swe-swe/templates/host/auth
SWE_SWE_PASSWORD=testpass READONLY_PASSWORD=viewpass go run main.go
```

1. **Name too long rejected** - 100+ chars → Error
2. **Name with control characters rejected** → Error
3. **Whitespace-only name rejected** → Error
4. **Name trimmed** - "  Alice  " → Cookie contains "Alice"

**Full E2E testing via MCP browser on your instance:**

Target: `https://host.docker.internal:1977`

*(Note: If MCP browser can't bypass self-signed cert warning, may need to click through manually first or configure browser to trust the cert)*

5. **Editor login flow**
   - Navigate to login page
   - Fill name: "Alice", password, select Editor
   - Submit → Verify redirect to home/terminal

6. **Viewer login flow**
   - Clear cookies, navigate to login
   - Fill name: "Bob", password, select Viewer
   - Submit → Verify redirect, "Viewing" badge visible

7. **Viewer cannot type**
   - As viewer Bob, connect to terminal
   - Attempt to type → Nothing happens

8. **Viewer can see output**
   - Open second browser/tab as editor Alice
   - Type `echo hello` → Verify Bob sees output

9. **Viewer can chat**
   - As Bob, press 'c', type message, send
   - Verify Alice sees "Bob: message"

10. **Chat message length limit**
    - Send 1000+ char message via devtools
    - Verify rejected or truncated

11. **Name autofill on re-login**
    - Logout, go to login
    - Verify name field autofilled from localStorage

---

## Files to Modify

### Auth Service
- `cmd/swe-swe/templates/host/auth/main.go` - Cookie format, login handler, verify handler, validation

### Traefik
- `cmd/swe-swe/templates/host/traefik/dynamic.yml` - Forward auth response headers

### swe-swe-server
- `cmd/swe-swe/templates/host/swe-swe-server/main.go` - Client tracking, role filtering, chat validation

### Client
- `cmd/swe-swe/templates/host/swe-swe-server/static/terminal-ui.js` - Viewer mode UI, name sync
- `cmd/swe-swe/templates/host/auth/login.html` (or embedded) - Login form with name + tabs

---

## Security Considerations

| Attack | Mitigation |
|--------|------------|
| Client sends terminal input as viewer | Server drops it (checks role) |
| Client forges role in cookie | HMAC verification fails |
| Client claims editor role in WS message | Role derived from auth header, not WS |
| Client spoofs chat username | Server overrides with auth-provided name |
| XSS in chat message/name | Client uses `escapeHtml()` (textContent→innerHTML pattern) |
| Excessively long name/message | Length limits at auth and server |
| Control characters in name/message | Rejected at validation |
