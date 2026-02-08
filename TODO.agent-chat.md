# TODO: Agent Chat Integration

## Problem Statement

Agent Chat is currently disjointed from the swe-swe workflow:

1. **Not embedded** — agent-chat binary isn't part of the container template; manually configured
2. **Tab always visible** — no way to hide Agent Chat tab per session while keeping MCP running
3. **Agent unaware** — when user types in agent-chat web UI, agent doesn't know unless user separately types "use agent chat" in TUI
4. **Permissions** — when user operates via Agent Chat (not TUI), they can't approve/deny tool permissions; agent needs yolo mode

---

## 1. Embed agent-chat in swe-swe containers

**Approach**: Publish as npm package (matches whiteboard pattern).

- Publish `@choonkeat/agent-chat` npm package wrapping the Go binary
- Add to `.mcp.json` container template (`cmd/swe-swe/templates/container/.mcp.json`):
  ```json
  "agent-chat": {
    "command": "npx",
    "args": ["-y", "@choonkeat/agent-chat"]
  }
  ```
- Zero Dockerfile changes
- `AGENT_CHAT_PORT` env var is already injected by swe-swe-server into the session env, so agent-chat picks it up automatically

**Files to change**:
- `cmd/swe-swe/templates/container/.mcp.json` — add agent-chat entry
- npm publish workflow for agent-chat repo

---

## 2. Agent Chat tab toggle

**Current**: Agent Chat tab is always visible in the right panel. MCP always runs.

**Approach**: Keep MCP always running. Add UI visibility toggle.

- Toggle icon in right panel header (near tab bar)
- State persisted in localStorage per session
- When "disabled", only Preview tab shows
- MCP server keeps running — toggle is purely visual

**Files to change**:
- `cmd/swe-swe/templates/host/swe-swe-server/static/terminal-ui.js` — add toggle logic
- `cmd/swe-swe/templates/host/swe-swe-server/static/styles/terminal-ui.css` — toggle styling

---

## 3. Agent awareness (message relay)

**Problem**: MCP is request-response. Agent-chat server can't push notifications to the agent. The `check_messages` tool exists but agent doesn't call it reliably.

**Approach**: Hybrid relay — when user sends a chat message, relay it to the agent's PTY stdin.

### Flow

1. User types message in agent-chat web UI
2. agent-chat server receives it via WebSocket, queues it in EventBus
3. agent-chat server notifies swe-swe-server (HTTP callback or shared mechanism)
4. swe-swe-server writes to the active agent's PTY stdin with a wrapper:
   ```
   [from Agent Chat] check the API docs
   ```
5. Agent sees it as normal user input
6. CLAUDE.md instructs: "When you see `[from Agent Chat]`, respond using agent-chat MCP tools (`send_message`/`draw`)"
7. Agent responds in the rich chat UI

### Notification mechanism options

- **HTTP callback**: agent-chat POSTs to `http://localhost:9898/api/chat-relay?session=X` when a user message arrives
- **File watcher**: agent-chat writes to a known path, swe-swe-server watches it
- **Shared EventBus**: if agent-chat is compiled into swe-swe-server (future option)

HTTP callback is simplest for now since both servers run in the same container.

### What swe-swe-server needs

- New HTTP endpoint: `POST /api/chat-relay` accepting `{ "sessionID": "...", "message": "..." }`
- Logic to find the active PTY for the session
- Write the wrapped message to PTY stdin
- Handle edge cases: agent busy (queue it), agent idle (send immediately)

**Files to change**:
- `cmd/swe-swe/templates/host/swe-swe-server/main.go` — add relay endpoint
- agent-chat `tools.go` or `eventbus.go` — add HTTP callback on user message
- agent-chat `main.go` — accept relay target URL (env var or flag)
- Container template CLAUDE.md / system prompt — add `[from Agent Chat]` instructions

---

## 4. Yolo mode for Agent Chat

**Problem**: When user operates via Agent Chat, they can't approve/deny tool permissions in the TUI.

**Approach**: When Agent Chat is the active input channel, agent should run in autonomous (yolo) mode.

### Options

- **A. Start in yolo mode by default** — if agent-chat is enabled, launch `claude --dangerously-skip-permissions`
- **B. UI toggle** — "Enable autonomous mode" button in session UI that switches the agent to yolo mode
- **C. Auto-detect** — when first chat message arrives via relay, switch to yolo mode

Option B gives the user explicit control. Could be a toggle in the Agent Chat tab header.

### Implementation considerations

- Claude Code's `--dangerously-skip-permissions` is set at launch time
- Switching mid-session may require restarting the agent process
- Alternative: use Claude Code's permission allowlist to pre-approve common tools
- Or: agent-chat relay could include a "please approve in terminal" fallback message when permissions are needed

**Files to change**:
- `cmd/swe-swe/templates/host/swe-swe-server/main.go` — session launch flags
- `cmd/swe-swe/templates/host/swe-swe-server/static/terminal-ui.js` — yolo toggle UI
- Agent launch command construction in session management

---

## Implementation Order

1. **Embed agent-chat** (npm publish + .mcp.json template) — standalone, no dependencies
2. **Tab toggle** — pure UI change, can be done independently
3. **Message relay** — requires changes to both agent-chat and swe-swe-server
4. **Yolo mode** — depends on relay working; needs careful UX design

---

## Open Questions

- Should agent-chat replace whiteboard MCP entirely? (agent-chat already has `draw` tool)
- Should the relay be bidirectional? (agent's terminal output → chat UI)
- What happens when agent is mid-task and chat message arrives? Queue vs interrupt?
- Should yolo mode be per-session or global?
