# MCP-less by default: swe-swe-server launches the mcp-cli-proxy fleet per session

Date: 2026-07-03
Status: IN PROGRESS
Branch: mcp-less
Supersedes the Phase 3 wiring in tasks/2026-07-01-mcp-less-cli-proxy.md
(which assumed the *entrypoint* launches the fleet).

## Decision (2026-07-03, with user)

Make MCP-less the **default** mode and move fleet ownership to **swe-swe-server**:

1. **`--mcp-less` default flips to `true`.** `--mcp-less=false` restores the
   legacy agent-hosted native-MCP path. (`init.go:628`)
2. **swe-swe-server launches the `mcp-cli-proxy` fleet per session**, at session
   creation -- NOT the entrypoint. The server already owns per-session ports,
   env, UUID, SessionMode, and teardown, so it is the natural owner.
3. **agent-chat proxy is gated on chat mode.** Launch all other proxies for every
   session; launch `swe-swe-agent-chat` only when `SessionMode == "chat"`
   (mirrors the existing "only expose agentChatPort for chat sessions",
   main.go:885-889). Terminal sessions get the fleet minus agent-chat.
4. **In mcp-less mode the entrypoint does NOT write the agent's MCP json.**
   Gate `{{CLAUDE_MCP_SETUP}}` behind `{{IF NOT_MCP_LESS}}`.
5. **Signal:** entrypoint exports `SWE_MCP_LESS=1` (from `InitConfig.MCPLess`);
   swe-swe-server reads `os.Getenv("SWE_MCP_LESS")`.
6. **Socket dir is per-session** to avoid cross-session socket collisions:
   `/workspace/.swe-swe/run/mcp/<uuid>/`. swe-swe-server sets `SWE_MCP_DIR` to
   that path in the agent env so the `mcp` client discovers exactly this
   session's proxies.
7. **Agent steering** (so the agent talks via `mcp` CLI): `--append-system-prompt`
   (+ CLAUDE.md backup) carrying the loop + the blocking-`send_message` rule +
   the bootstrap `mcp swe-swe-agent-chat check_messages`. Per-agent; scope v1 to
   `claude`.

## Canonical proxy specs (single source)

The five uniform stdio specs (already in `.claude.json` / templates.go), each
run as `mcp-cli-proxy --name N --socket $DIR/N.sock -- sh -c 'exec ...'`:

| name | argv (sh -c 'exec ...') | gate |
|---|---|---|
| swe-swe-agent-chat | `npx -y @choonkeat/agent-chat --theme-cookie swe-swe-theme --autocomplete-triggers /=slash-command --autocomplete-url http://localhost:$SWE_SERVER_PORT/api/autocomplete/$SESSION_UUID?key=$MCP_AUTH_KEY` | chat only |
| swe-swe-playwright | `mcp-lazy-init --init-method POST --init-url http://localhost:$SWE_SERVER_PORT/api/session/$SESSION_UUID/browser/start?key=$MCP_AUTH_KEY -- npx -y @playwright/mcp@latest --cdp-endpoint http://localhost:$BROWSER_CDP_PORT` | always |
| swe-swe-preview | `npx -y @choonkeat/agent-reverse-proxy --bridge http://localhost:$SWE_SERVER_PORT/proxy/$SESSION_UUID/preview/mcp` | always |
| swe-swe-whiteboard | `npx -y @choonkeat/agent-whiteboard` | always |
| swe-swe | `npx -y @choonkeat/agent-reverse-proxy --bridge http://localhost:$SWE_SERVER_PORT/mcp?key=$MCP_AUTH_KEY` | always |

## Phases (each ends GREEN via `make test`; TDD)

### Phase A -- pure spec selection (START HERE)
- `mcpLessProxySpecs(sessionMode string) []proxySpec` in swe-swe-server: returns
  the fleet, including agent-chat iff `sessionMode == "chat"`.
- **RED tests:** chat -> 5 specs incl agent-chat; terminal -> 4 specs, no
  agent-chat; every spec has name+argv; socket name == `<name>.sock`.

### Phase B -- launcher + lifecycle
- At session create, if `SWE_MCP_LESS` set: for each spec, `exec mcp-cli-proxy`
  with the session env + per-session `SWE_MCP_DIR`; `trackPid`; store cmds on
  `Session` for teardown; ensure `killSessionProcessGroup`/close reaps them.
- Set `SWE_MCP_DIR` in the agent env.
- **Verify:** unit test with a stub `mcp-cli-proxy` on PATH (temp script) that
  the launcher spawns exactly the gated set and cleanup kills them.

### Phase C -- entrypoint + flag default + steering
- Flip flag default true; fix save-config restore (`init.go:877`) so a stale
  saved `false` doesn't override the new default.
- Entrypoint: `{{IF NOT_MCP_LESS}}` around `{{CLAUDE_MCP_SETUP}}`; export
  `SWE_MCP_LESS=1` when mcp-less; add `NOT_MCP_LESS` to the template switch.
- Append-system-prompt + CLAUDE.md steering (claude only for v1).
- `make build golden-update`; review golden churn (broad but deterministic).

### Phase D -- e2e
- Boot an e2e container in the default (mcp-less) mode; assert **no agent MCP
  json written**; assert chat round-trips through the proxy fleet; browser +
  whiteboard succeed; terminal session has no agent-chat proxy.

## Open risks
- Teardown: proxies launched by the server are not under the agent PTY's process
  group; must be tracked + killed explicitly on session end.
- npx cold-start latency for agent-chat -> chat UI blank until the proxy child
  binds AGENT_CHAT_PORT; acceptable (same as today) but note in the loading page
  work (separate proposal).
- Other agents (codex/gemini/goose/opencode) still need their own
  config-skip + steering; v1 is claude-only.
