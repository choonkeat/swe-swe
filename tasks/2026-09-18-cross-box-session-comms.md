# Cross-box session comms: `<unique>/<uuid>` addresses relayed over swe-swe-tunnel

Status: phase 1 IMPLEMENTED 2026-09-18 (branch feat/cross-box-addresses). Phases 2-4 pending.

## Goal

The 12 orchestration MCP tools (list_sessions, send_chat_message, ...) today
assume one server process on one machine: an in-memory session table,
loopback ports, local disk paths, a locally minted per-session auth key. That
solves same-machine concurrency and has no way to even name a session on a
second box.

Erlang lesson: solve the distributed case and the local case falls out as the
degenerate cluster-of-one. So:

- Every session gets an address `<unique>/<uuid>`. `unique` is the box's
  tunnel name (SWE_TUNNEL_UNIQUE), the same value tunneld registers (tunneld
  stores the label as `<unique>-tunnel`; the address uses the bare unique). A bare `<uuid>` means "this box".
- Every session-addressed tool resolves the address in ONE branch:
  1. unique is me or absent  -> answer from memory (today's code, untouched)
  2. unique is someone else, relay configured -> forward over the tunnel
  3. unique is someone else, no relay -> error `unreachable: <unique>`
- Transport is swe-swe-tunneld, kept STATELESS: forward or say unreachable.
  No queues, no history, no storage in tunneld.
- Every send_chat_message payload is server-stamped with a signoff so the
  receiving agent can reply:

      <text>

      (via send_chat_message from <unique>/<uuid>)

  Stamped by the SENDING server from the caller identity in the per-session
  MCP auth key (unforgeable, works for every agent). When the box has no
  unique, the signoff is `(via send_chat_message from <uuid>)`.

Non-goals (separate question, not in this plan): cross-box create_session,
prepare_repo, list_worktrees. They touch a disk; remote use means "create a
session on box B using a path that exists on box B". Phase 3 forwards them
as-is with no path translation.

## Repos and ownership

| Repo | Path on this box | Who edits |
|---|---|---|
| swe-swe (this repo) | /workspace | this session (phases 1, 3, 4) |
| swe-swe-tunnel | /repos/swe-swe-tunnel/workspace | ITS OWN agent session, from the contract in `tasks/2026-09-18-cross-box-relay-tunneld-contract.md` (phase 2) |

Phase 1 ships alone with zero tunnel changes. Phase 3 depends on phase 2.

## Phase 1 -- addresses + signoff (swe-swe only, no transport)

Files: `cmd/swe-swe/templates/host/swe-swe-server/main.go` (registerOrchestrationTools, ~L9810-10310), new `session_address.go` + test.

1. `session_address.go`: `type sessionAddr struct{ Unique, UUID string }`,
   `parseSessionAddr(s string) (sessionAddr, error)` accepting `<uuid>` and
   `<unique>/<uuid>`; `localUnique()` reading `liveTunnelRuntime` (see
   `tunnel_runtime.go:68,126`); `addr.isLocal()` = Unique=="" ||
   Unique==localUnique(). Unit tests.
2. `list_sessions` snapshot gains `address` (`<unique>/<uuid>`, or `<uuid>`
   when no unique) per entry. Existing fields untouched. (No top-level
   `unique`: the payload is a JSON array and the address already carries it.)
3. The 6 session-addressed tools accept an address in their existing `uuid`
   arg (no new arg; description updated): end_session, set_session_name,
   get_session_output, send_session_input, send_chat_message,
   get_chat_history. Non-local address -> `unreachable: <unique> (no relay
   configured on this box)` until phase 3.
4. Signoff: in the send_chat_message handler (main.go ~L10228) append
   `\n\n(via send_chat_message from <localUnique>/<callerUUID>)` where
   callerUUID = `callerSessionFromContext(ctx)`. Applies to LOCAL sends too.
   Also stamp `sendChatToSession` (main.go ~L9351) callers only if they
   originate from an MCP caller; server-originated nudges stay unstamped.
5. Tool descriptions: mention the address form and the signoff so agents
   know how to reply (`send_chat_message` to the address in the signoff).
6. `make build golden-update`, `make test`, commit.

Verification: two local sessions; A sends to B; B's chat shows the signoff
with A's uuid; `unique/<uuid>` with a foreign unique returns unreachable.

## Phase 2 -- tunneld relay (swe-swe-tunnel repo, its own agent)

Contract file: `tasks/2026-09-18-cross-box-relay-tunneld-contract.md`.
Summary of what swe-swe needs, so this plan reads standalone:

- swe-swe-tunnel client: `--relay-listen 127.0.0.1:0`; every HTTP request it
  receives there is sent to tunneld as a client-initiated yamux stream with
  header `X-Swe-Relay-To: <target unique>`. Bound port reported as a
  `--report-format jsonl` supervisor event `{"event":"relay_listen","addr":...}`.
- tunneld: accept client-initiated streams; policy `--relay off|all|owner`;
  strip client-supplied `X-Swe-Relay-From`, set it to the caller's unique;
  rewrite Host to `<relayPort>.<target>.<apex>` and feed the existing
  `route()` with the port policy bypassed for relayPort; target not
  connected -> 503 body `unreachable: <unique> not connected`.
- Public ingress ALWAYS strips `X-Swe-Relay-From` and the port policy still
  rejects relayPort, so nothing on the internet can reach the relay listener.

## Phase 3 -- forwarding (swe-swe, needs phase 2 binaries)

Files: new `relay.go` + test, `tunnel_supervisor.go` (~L237-333 event
loop), `listen.go` (extra loopback listener), `main.go` tools.

1. Relay listener: server binds `127.0.0.1:<relayPort>` (fixed default,
   e.g. the server port + a documented offset, flag `-relay-port`, NOT in the
   published/`--tunnel-local-ports` set). It serves the EXISTING `orchHandler`
   (main.go ~L2554, the same stateless streamable-HTTP MCP server behind
   `/mcp`) at `/mcp`, wrapped in `relayAuthMiddleware` instead of
   `mcpAuthMiddleware`: require `X-Swe-Relay-From` (only tunneld sets it;
   loopback-only bind + port policy keep everyone else out), inject the
   caller as `relay:<unique>` into the context so tools that need a caller
   identity (create_session, self-end) refuse cleanly. No new tool surface:
   every tool a local agent can call is what a remote box can call.
   send_chat_message appends nothing on the receiving side; the SENDER
   already stamped the signoff.
2. Supervisor: parse the `relay_listen` jsonl event, store the client's relay
   address in `liveTunnelRuntime`.
3. Forwarding: in the ONE resolve branch, non-local address + relay address
   known -> reuse the hand-rolled JSON-RPC `tools/call` POST the server
   already uses against agent-chat (`callAgentChatOrchestrator`, main.go
   ~L10311): generalize it to take a URL + extra headers, then POST to
   `http://<relay-listen>/mcp` with `X-Swe-Relay-To: <unique>`, the same
   tool name, and args with the uuid rewritten to the bare uuid. 10s timeout,
   one shared `http.Client` (never a per-request Transport, see CLAUDE.md).
   The orchestrator handler is Stateless, so no MCP initialize handshake is
   needed per call. 503 from tunneld -> return its body verbatim
   (`unreachable: ...`). Connection refused -> `unreachable: relay not
   running`.
4. `list_sessions` stays LOCAL by default. Add optional arg `unique` to list
   another box's sessions (forwarded). No cluster-wide fan-out in this plan.
5. Tests: unit (address resolve matrix; forward path with an httptest relay
   stub returning 200 and 503). e2e: two swe-swe servers + one tunneld on one
   host (swe-swe-tunnel already has `e2e_test.go` patterns); A sends to B,
   B's chat history shows the stamped message.
6. Live proof: this box <-> the DO droplet `dockerless` (see
   tasks/2026-07-19-* and memory). NOTE relay does not need per-unique DNS
   records, only the tunneld connection, so the droplet's DNS blocker does
   not block this.

## Phase 4 -- docs + release

1. `docs/tunnel-explained.md`: "Box-to-box relay" section (address form,
   trust model, unreachable semantics, relay port not public).
2. MCP tool descriptions final pass; `swe-swe/` command docs if any mention
   session uuids.
3. CHANGELOG: one sentence.

## Trust model (why this is not a new hole)

- Only an authenticated, allowlisted box can ask tunneld to relay; tunneld
  decides same-owner policy.
- The target's relay listener is loopback-only, its port is rejected by the
  public port policy, and the only header it trusts is one tunneld strips
  from all public ingress.
- The signoff is stamped from the MCP auth key identity, never from agent
  text, so a session cannot impersonate another.

## Open decisions

1. Same-owner policy in tunneld: start with `--relay all` (one owner per
   tunneld today) and leave `owner` grouping for when the allowlist grows
   owner metadata. Tunnel agent's call.
2. Relay port default value. Suggest server port + 1000 unless that collides
   with the proxy pools (check `proxy-port-offset` golden variant).
