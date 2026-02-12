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

## 3. Agent awareness (first-message bootstrap)

**Status**: In progress — swe-swe listener done, agent-chat postMessage pending.

**Approach**: Browser-side bootstrap. When user sends first message in Agent Chat, the chat iframe sends a `postMessage` to the parent (terminal-ui.js), which injects `check_messages; i sent u a chat message\n` into the PTY. The agent then calls `check_messages`, gets the message, and starts using chat MCP tools.

### Flow

1. User types message in Agent Chat web UI → `handleSend()`
2. Agent Chat frontend sends `postMessage({ type: 'agent-chat-first-user-message' })` to parent
3. terminal-ui.js receives it, writes bootstrap text to PTY via WebSocket (first time only)
4. Agent sees it as TUI input, calls `check_messages`, gets the actual message
5. Agent responds via `send_message` / `draw` — subsequent messages flow through MCP

### Done

- **terminal-ui.js**: postMessage listener for `agent-chat-first-user-message` with `_chatBootstrapped` dedup flag

### Remaining (in agent-chat repo)

- **`client-dist/app.js`**: Add `window.parent.postMessage({ type: 'agent-chat-first-user-message' }, '*')` in `handleSend()`
- See `.swe-swe/repos/agent-chat/workspace/TODO.md` for details

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
