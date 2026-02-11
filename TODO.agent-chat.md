# TODO: Agent Chat Integration

## Problem Statement

Agent Chat is currently disjointed from the swe-swe workflow:

1. **Not embedded** — agent-chat binary isn't part of the container template; manually configured
2. ~~**Tab always visible**~~ — ✅ Done. Agent Chat tab is opt-in with probe-based discovery
3. **Agent unaware** — when user types in agent-chat web UI, agent doesn't know unless user separately types "use agent chat" in TUI
4. **Permissions** — when user operates via Agent Chat (not TUI), they can't approve/deny tool permissions; agent needs yolo mode

---

## 1. Embed agent-chat in swe-swe containers

**Status**: Partially done — agent-chat runs in containers, but npm publishing not yet complete.

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

**Remaining**:
- npm publish workflow for agent-chat repo
- Add agent-chat entry to `.mcp.json` container template

---

## 2. Agent Chat tab & proxy ✅

**Status**: Done.

**What was built**:
- Agent Chat port proxy mirroring Preview architecture (`feat(proxy)`)
- Atomic port allocation — agent chat port allocated alongside preview port (`fix(proxy)`)
- Opt-in tab with probe-based discovery — tab only appears when agent-chat MCP is running (`feat(ui)`)
- Toggle in left pane to show/hide Agent Chat tab (`feat(ui)`)
- Scroll position preserved on tab switch with smart auto-scroll (`fix(ui)`)
- Bootstrap iframe to fix SameSite=Lax cookie issue (`fix(ui)`)
- Classic connected tab style with accent-tinted resizer (`style(ui)`)

**Key commits**:
- `b54fe066f` feat(ui): add Agent Chat tab toggle in left pane
- `44144f90e` feat(proxy): add Agent Chat port proxy mirroring Preview architecture
- `084027467` fix(proxy): make agent chat port allocation atomic with preview port
- `bda2249ad` feat(ui): make Agent Chat tab opt-in with probe-based discovery
- `af67cb013` fix(ui): preserve scroll position on tab switch and smart auto-scroll
- `0246a4701` fix(ui): bootstrap Agent Chat iframe to fix SameSite=Lax cookie issue

---

## 3. Agent awareness (message relay)

**Status**: Not started.

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

## 4. Permission management for Agent Chat

**Status**: Research complete (see `research/2026-02-10-agent-chat-integration.md`).

**Problem**: When user operates via Agent Chat, they can't approve/deny tool permissions in the TUI.

**Research findings**: Claude Code writes session JSONL transcripts. Permission prompts manifest as a gap between `tool_use` (assistant entry) and `tool_result` (user entry). By tailing the JSONL file, we can detect pending prompts and surface them in the Chat UI with Allow/Deny buttons.

### Approach (from research)

1. Tail the active session's JSONL file (`~/.claude/projects/-workspace/{session-id}.jsonl`)
2. Track pending `tool_use` IDs — if no `tool_result` arrives within ~1s, it's a permission prompt
3. Show in Agent Chat UI: "Claude wants to run {tool_name}: {description} — Allow / Deny?"
4. On user response, write keystroke (`y\n` or `n\n`) to PTY stdin via swe-swe-server

### Fallback options

- **A. JSONL-based permission relay** (above) — most seamless
- **B. Start in yolo mode by default** — if agent-chat is enabled, launch `claude --dangerously-skip-permissions`
- **C. UI toggle** — "Enable autonomous mode" button that restarts agent in yolo mode
- **D. Permission allowlist** — pre-approve common tools via Claude Code config

**Files to change**:
- `cmd/swe-swe/templates/host/swe-swe-server/main.go` — JSONL watcher + PTY input endpoint
- `cmd/swe-swe/templates/host/swe-swe-server/static/terminal-ui.js` — permission prompt UI
- Agent Chat frontend — permission card rendering

---

## Implementation Order

1. ~~**Embed agent-chat**~~ (partially done — npm publish remaining)
2. ~~**Tab toggle & proxy**~~ ✅ Done
3. **Message relay** — requires changes to both agent-chat and swe-swe-server
4. **Permission management** — depends on relay working; research complete

---

## Open Questions

- Should agent-chat replace whiteboard MCP entirely? (agent-chat already has `draw` tool)
- Should the relay be bidirectional? (agent's terminal output → chat UI)
- What happens when agent is mid-task and chat message arrives? Queue vs interrupt?
- Should yolo mode be per-session or global?
