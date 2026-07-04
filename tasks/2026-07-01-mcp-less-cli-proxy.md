# MCP-less mode: `mcp-cli-proxy` daemon + `mcp` CLI

Date: 2026-07-01
Status: PLAN -- not yet implemented
Branch: mcp-less

## Problem

swe-swe must run in environments where **MCP is gated but the CLI agent is not**.
Today the agent process *is* the MCP host: its native MCP client reads
`~/.codex/config.toml` / `~/.gemini/settings.json` (or the Pi `mcp-bridge.ts`
extension), spawns each MCP server over stdio, and owns those pipes for the
session. Critically, the **entire human<->agent conversation runs over the
`swe-swe-agent-chat` MCP server** (`send_message` blocks for the reply,
`check_messages` drains the queue). If MCP is gated, the agent cannot talk to the
user at all -- the web chat UI goes dark. Browser/whiteboard/preview/orchestration
are secondary losses; the chat channel is the existential one.

## Key insight

Our MCP config already normalizes **every** server -- even the HTTP-bridged
(`preview`, `swe-swe`) and lazy-init (`playwright`) ones -- into a uniform stdio
launch spec `{command, args, env}`. That is exactly the standard MCP-host
contract. So we do not rewrite any server; we replace *who hosts them* and *how
the agent reaches them*:

- **Ownership** moves from the agent (spawner) to a long-lived per-server daemon.
- **Transport** from stdio (agent-owned pipe) to a **unix socket** the agent's
  Bash tool can reach via a thin client.
- The agent runs with **no MCP config at all** -- what a gated environment demands.

## Architecture

Two new stdlib-only Go binaries (built like `mcp-lazy-init` in the Dockerfile),
templated under `cmd/swe-swe/templates/host/`:

### `mcp-cli-proxy` (daemon, one instance per MCP server)
A dumb 1:1 adapter. Launched by the entrypoint, one per server, with the **exact
same env the agent gets**:

```
mcp-cli-proxy --name swe-swe-agent-chat \
  --socket .swe-swe/run/mcp/swe-swe-agent-chat.sock \
  -- sh -c 'exec npx -y @choonkeat/agent-chat --autocomplete-url http://localhost:$SWE_SERVER_PORT/api/autocomplete/$SESSION_UUID?key=$MCP_AUTH_KEY ...'
```

Responsibilities:
- Exec the `-- <argv>` **verbatim** (no env expansion of its own -- the
  `sh -c "exec ..."` spec, already proven in the gemini config, lets the shell
  expand `$VAR` from the inherited env; `exec` replaces the shell so we wait on
  and log the *real* child PID/exit per the coding rule).
- Perform the MCP `initialize` handshake with the child once, on boot.
- Listen on the unix socket; accept **concurrent** clients and **multiplex** them
  by JSON-RPC id onto the child's single stdio (mandatory: `send_message` blocks
  for the human while `send_progress`/`check_messages` must still get through).
- On a client connection drop mid-call, emit `notifications/cancelled` for that id.
- Unlink the socket on exit; log child exit status.

### `mcp` (client CLI, on PATH, called by the agent via Bash)
Stateless. The socket directory `.swe-swe/run/mcp/*.sock` **is the registry** --
no config to read, no ports.
- `mcp -h` -> readdir sockets -> connect to each -> `tools/list` -> render the tree.
- `mcp <server> -h` -> that server's tools.
- `mcp <server> <tool> -h` -> flags derived from the tool's `inputSchema`
  (string/bool/number -> typed flags, enum -> choices, required vs optional,
  description -> help).
- `mcp <server> <tool> [flags]` -> open that one socket, `tools/call`, render result:
  text -> stdout, image -> write file + print path, structured -> JSON to stdout.

Naming mirrors the canonical tool id models already know, so any `mcp__X__Y` maps
mechanically to `mcp X Y`:
- `mcp__swe-swe-agent-chat__send_message` -> `mcp swe-swe-agent-chat send_message --text "..."`
- `mcp__swe-swe-playwright__browser_navigate` -> `mcp swe-swe-playwright browser_navigate --url ...`
- `mcp__swe-swe__list_sessions` -> `mcp swe-swe list_sessions`

### Locked decisions
- **Full server names** (not stripped) -- preserves the `mcp__server__tool` symmetry.
- **3-level** `mcp <server> <tool> [flags]` -- tools cannot fold into the server name.
- **No expansion in the proxy** -- reuse `sh -c "exec ..."`; the shell expands from
  inherited env.
- Client = `mcp`, daemon = `mcp-cli-proxy`.

### Works for all five (uniform stdio spec, now 1:1)
| server | command | note |
|---|---|---|
| swe-swe-agent-chat | `npx @choonkeat/agent-chat` | blocking `send_message` -> id-mux |
| swe-swe-playwright | `mcp-lazy-init … -- npx @playwright/mcp` | lazy HTTP init fires on 1st `tools/call` regardless of client |
| swe-swe-preview | `npx agent-reverse-proxy --bridge …/preview/mcp` | stdio->HTTP bridge, just exec |
| swe-swe-whiteboard | `npx @choonkeat/agent-whiteboard` | plain stdio |
| swe-swe | `npx agent-reverse-proxy --bridge …/mcp?key=…` | stdio->HTTP bridge, just exec |

### Bonus property
Because `mcp-cli-proxy` is separate from the agent and env-identical, the servers
**survive `claude --continue` / agent restarts** -- browser context and the chat
queue no longer get torn down on every agent reboot (today they do).

## Phases

### Phase 0 -- Scaffolding + decision on the mode switch
- Decide: is mcp-less a **flag** (`swe-swe init --mcp-less`) or auto-detected?
  Default proposal: an explicit init flag, following the two-commit TDD pattern in
  CLAUDE.md ("Adding new flags"). Baseline commit adds the flag (parsing + golden
  variant, no effect), then implementation.
- Create `cmd/swe-swe/templates/host/mcp-cli-proxy/main.go` and
  `cmd/swe-swe/templates/host/mcp/main.go` skeletons (stdlib only, own go.mod).
- Add Dockerfile build stages mirroring `mcp-lazy-init` (lines ~31-33).
- **Verify:** `make build`; both binaries compile; golden shows only the new flag
  in init.json.

### Phase 1 -- `mcp-cli-proxy` (daemon)
- Arg parsing: `--name`, `--socket`, `-- <argv>`.
- Exec child; MCP `initialize`; keep a single stdio JSON-RPC framing reader/writer.
- Unix socket listener; per-connection goroutine; request<->response routing by id
  across concurrent clients; long-lived (blocking) requests must not head-of-line
  block others.
- Cancellation on client drop; unlink socket on exit; **log child PID + exit
  status** (coding rule -- no silent `Wait`).
- **Verify:** unit test with a fake stdio MCP child (echo server): concurrent
  clients, a deliberately-blocking call, a client-drop -> cancelled notification.
  `make test`.

### Phase 2 -- `mcp` (client CLI)
- Socket discovery (`.swe-swe/run/mcp/*.sock`); ECONNREFUSED / stale socket ->
  "server unavailable", not a crash.
- `tools/list` -> flag synthesis from `inputSchema`; 3-level help.
- `tools/call`; result rendering (text/image-file/structured).
- **Verify:** golden-style test of `mcp -h` / `mcp <server> -h` output against the
  echo server; image result writes a file and prints its path. `make test`.

### Phase 3 -- entrypoint / init wiring
- New canonical `mcpServers` template (single source; the `sh -c "exec ..."` form)
  consumed by the entrypoint launch loop.
- Entrypoint (mcp-less mode): for each server, launch a `mcp-cli-proxy` with the
  agent's env; **do not** write the agent's own MCP config; put `mcp` on PATH;
  inject CLAUDE.md/system-prompt instructions teaching the agent to use
  `mcp <server> <tool>` instead of MCP tools.
- Supervise the proxies (restart policy TBD -- at minimum agent-chat, which hosts
  the chat UI).
- **Verify:** `make build golden-update`; review
  `cmd/swe-swe/testdata/golden` diff -- new mcp-less variant present, no
  unintended churn in existing variants.

### Phase 4 -- live dogfood
- Boot a test container in mcp-less mode (docs/dev/test-container-workflow.md).
- Confirm end-to-end: chat via `mcp swe-swe-agent-chat send_message` round-trips to
  the web UI; `check_messages` drains; a browser + whiteboard + orchestration call
  each succeed; agent restart preserves proxies.
- Shut the container down.

## Resolved decisions (2026-07-01)

1. **Mode switch = explicit `--mcp-less` init flag** (not auto-detect: "gated" is
   an environment policy we cannot reliably probe, and auto-detect breaks golden
   determinism). Trajectory: flag -> dogfood -> make it the default and retire the
   agent-hosted path. Blocker before default: `mcp` is **tools-only**
   (`tools/list`+`tools/call`); native MCP clients also surface
   resources/prompts/sampling. swe-swe's five are tools-only so we lose nothing,
   but that gap must close before mcp-less can be the default.

2. **User-added MCP servers: architect general, scope v1 to swe-swe's five.**
   `mcp-cli-proxy` is already server-agnostic; the missing piece is *ingesting*
   user config (per-agent formats: codex TOML / gemini JSON / claude `.mcp.json`).
   That's an additive fast-follow, not a v1 blocker.

3. **Agent steering = appended system prompt (primary) + CLAUDE.md (backup) +
   self-documenting `-h`.** The sharp contract is that `send_message` **blocks**:
   the agent must run `mcp swe-swe-agent-chat send_message --text "..."`, *wait*
   for it (stdout IS the user's reply), never background it, and end every turn on
   it. Reuse the exact wording from today's agent-chat tool description
   ("blocks until the user responds; the reply is RETURNED by this call"),
   retargeted to the command. Three reinforcing layers: (a) `--append-system-prompt`
   carries the loop + blocking rule + bootstrap ("start with
   `mcp swe-swe-agent-chat check_messages`"); (b) CLAUDE.md durable backup;
   (c) `mcp <server> <tool> -h` inherits the tool's own description.

4. **Restart policy = each `mcp-cli-proxy` self-restarts its own child.** Child
   stdio dies -> respawn same argv -> re-`initialize` -> the **socket never moves**
   (clients retry). Per-child exponential backoff + crash-loop cap; on give-up,
   mark the socket unhealthy so `mcp` returns a clean "server unavailable" instead
   of hanging. Log every exit (coding rule). agent-chat is priority; if it cannot
   stay up, surface loudly in container logs. Entrypoint stays fire-and-forget;
   whether swe-swe-server *also* supervises the proxy processes is a minor
   follow-up (container restart covers the worst case).

## Remaining risk to confirm during build
- **Recording/PTY model:** the agent still runs in a recorded PTY; only the tool
  transport changed, so recording should be unaffected -- confirm in Phase 4.
