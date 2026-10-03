<!-- agent-chat export
title: Mcp Less Rebase And Doc
date: 2026-07-03
index: 03
slug: mcp-less-rebase-and-doc
agent: claude
version: dev (unknown)
-->

# Mcp Less Rebase And Doc

_2026-07-03 · 03 · claude · agent-chat dev (unknown)_

**USER**

> swe-swe must work in an environment where “mcp” is gated (but cli is not). So let’s have a strategy and tooling to support such a setup. Discuss

<small>took 17.6s</small><br>
**AGENT**

> Digging into how swe-swe currently depends on MCP so I can propose a concrete "mcp-gated" strategy...

<small>took 2m 5s</small><br>
**AGENT**

> Here's the lay of the land and a proposed strategy for an **MCP-gated environment**.
>
> ## The real problem
> It's easy to think "MCP gated" just means losing playwright/whiteboard. It doesn't. In swe-swe today, **the entire human↔agent conversation runs over the `swe-swe-agent-chat` MCP server**. The agent runs `claude` in a PTY (`main.go:367` `ShellCmd:"claude"`), the TUI is invisible, and the web chat bubbles are produced by the agent calling MCP tools: `send_message` (blocks for the reply), `check_messages`, `send_progress`, `draw`. Kill MCP and **the user literally cannot talk to the agent at all.** So Tier‑0 of any strategy is: restore the chat channel without MCP.
>
> ## Define "gated" first (this picks the design)
> Two very different threat models:
> - **(A) Gated at the model/harness layer** — the CLI agent still runs, but its MCP feature is disabled by policy (`--mcp-config` ignored / tools not surfaced). Local HTTP + subprocesses are fine.
> - **(B) Gated at the transport/network layer** — can't spawn the `npx` stdio bridges or reach any MCP endpoint (e.g. no npm egress), but local loopback HTTP is fine.
>
> Which one are you targeting? It changes the answer. My guess is (A).
>
> ## The key asset we can exploit
> Our MCP servers are **thin bridges to HTTP that already exists**: `swe-swe`/`preview` are just `agent-reverse-proxy --bridge http://localhost:$SWE_SERVER_PORT/mcp`, and swe-swe-server already exposes the orchestration tools over HTTP (`main.go:8214+`: list_sessions, create_session, send_chat_message, get_chat_history, …). So "mcp-less" isn't a rewrite — it's **swapping the MCP front-end for a CLI front-end over the same backend.**
>
> ## Proposed strategy — two tiers
>
> **Tier 0 — the chat loop (mandatory). Two options:**
> - **Option 1: CLI‑native stdio driver.** Drive the agent in headless `--input-format stream-json --output-format stream-json` instead of a PTY. swe-swe-server becomes the bridge: web message → agent stdin; assistant events → chat bubbles. Highest fidelity, but per‑agent plumbing (claude/codex/gemini differ) and loses the current recording/PTY model.
> - **Option 2: Bash‑tool shims (my recommendation).** Ship a small `swe` binary on PATH; the agent calls `swe send-message "..."` / `swe check-messages` through its normal Bash tool. `send-message` **long-polls** the existing HTTP endpoint so it blocks for the user's reply — preserving today's turn semantics and quick-replies. Agent‑agnostic, minimal backend change, works for any CLI with shell access, and degrades gracefully.
>
> **Tier 1 — capability tools as the same shims:** `swe draw`, `swe browser ...`, `swe whiteboard`, `swe preview`, plus the orchestration verbs — all hitting the HTTP endpoints the MCP bridges already call. Reuse the `MCP_AUTH_KEY` for auth.
>
> ## Tooling to build
> 1. A single `swe` CLI (Go, embedded in the image) mirroring the MCP tool surface over HTTP.
> 2. An `--mcp-less` init flag (or auto-detect) that: skips writing the MCP configs, and injects CLAUDE.md/system-prompt instructions teaching the agent to use `swe ...` instead of MCP tools.
> 3. Golden-test variants for the mcp-less init output.
>
> **My recommendation: target scenario (A), go with Option 2 (Bash shims + long-poll) for Tier 0.** It's the smallest change that keeps the product usable and reuses all existing HTTP. Which "gated" scenario are we actually designing for, and do you want me to turn this into a phased task plan?

[Quick replies]
- Scenario A — write the phased task plan
- Actually it's scenario B
- Go with Option 1 (stream-json) instead
- Just discuss more, don't plan yet

**USER**

> Our mcp servers boot on agent start. Who runs them — whoever runs them has to maintain stdio communicate with these mcps (agent-chat, preview, …) and `swe` cli talks to it over what channel?

<small>took 1m 2s</small><br>
**AGENT**

> You've put your finger on the load-bearing question. Here's the honest answer, and it reshapes the design.
>
> ## Who runs the MCP servers today
> The **agent process itself** is the MCP client, and it's the spawner:
> - **claude/codex/gemini**: the agent's native MCP runtime reads `~/.codex/config.toml` / `~/.gemini/settings.json`, spawns each server as a **stdio** subprocess, and owns those stdin/stdout pipes for the whole session.
> - **Pi**: `mcp-bridge.ts` does it — but notice it already boots them differently: `new SpawnedHttpService({... "--no-stdio-mcp" ...})` and then bridges to `{url}/mcp` **over HTTP**, not stdio.
>
> So "whoever runs them maintains the stdio" = the agent, and only for stdio agents. Pi already sidesteps stdio entirely.
>
> ## Why `swe` can't use stdio
> Stdio MCP is **1:1 and spawner-owned**: the process that forked the server owns its pipes for the server's lifetime. A `swe` CLI invoked fresh from the agent's Bash tool is short-lived and did **not** spawn anything — it has no pipe to attach to. You can't have N independent `swe check-messages` calls sharing one persistent stdio session with a server they didn't fork. Stdio is simply the wrong channel for a stateless per-invocation client.
>
> ## The fix: flip ownership, and the channel is loopback HTTP
> 1. **A long-lived supervisor boots the services — not the agent.** That supervisor is **swe-swe-server** (it already runs for the session's whole life and already owns port allocation). It launches agent-chat / whiteboard / preview in their existing `--no-stdio-mcp` **HTTP** mode, and playwright in `--port` (SSE/HTTP) mode. This isn't new code to invent — `mcp-bridge.ts` already proves the HTTP-service boot pattern; we're just moving that responsibility from the Pi-only bridge into the always-on server (with proper `Wait()` exit logging per our coding rule).
> 2. **`swe` talks loopback HTTP JSON-RPC** to each service's `/mcp` endpoint, authed by `MCP_AUTH_KEY` from env. Crucially, **the endpoints already exist**:
>    - orchestration → `http://localhost:$SWE_SERVER_PORT/mcp?key=$MCP_AUTH_KEY` (main.go:2045, the `agent-reverse-proxy` bridge was *only* there to translate the agent's stdio into this HTTP — remove the agent's stdio need and `swe` calls it directly)
>    - agent-chat → its `--no-stdio-mcp` HTTP `/mcp` (what mcp-bridge already talks to)
>    - preview → `/proxy/$UUID/preview/mcp` (already HTTP)
> 3. **Blocking is preserved by long-poll**: `swe send-message` holds the HTTP request open until the user replies — same semantics as the MCP tool blocking today.
>
> ## The one subtlety: session state
> Stdio gave each server an implicit per-agent session. Over HTTP with a stateless CLI you need session affinity — and we already have it: state is keyed server-side by `SESSION_UUID` / `MCP_AUTH_KEY` (that's `mcp_caller_identity`, and it's how the web UI + forks already share one agent-chat queue). So playwright's browser context, agent-chat's message queue, etc. stay coherent across separate `swe` calls.
>
> ## Net architecture shift
> - **Ownership**: agent (spawner) → swe-swe-server (supervisor).
> - **Transport**: stdio → loopback HTTP JSON-RPC.
> - **`swe` CLI**: a stateless HTTP client the agent calls via Bash; per-call auth = `MCP_AUTH_KEY`.
>
> The agent stops being an MCP host entirely — which is exactly what surviving a gated-MCP environment requires. Want me to turn this into a phased task plan (supervisor boot changes + `swe` client + init `--mcp-less` wiring + golden tests)?

[Quick replies]
- Yes — write the phased task plan
- What about non-stdio agents like Pi?
- Does agent-chat's HTTP /mcp support long-poll today?
- Keep discussing — one concern first

**USER**

> No. I want a `swe-swe-mcp` cli that given mcp config json will behave like agent to boot them, maintain stdio, etc
>
> swe-swe used to just start agent cli, now it also starts `swe-swe-mcp` with the same exact env
>
> When agent executes `swe -h` it talks to `swe-swe-mcp` and converts each mcp and its options into cli flags. When agent executes `swe [mcp name] [flags]` it talks to `swe-swe-mcp` which will multiplex to talk to the connected mcp. 
>
> Check our mcp config for how this can work for all

<small>took 1m 32s</small><br>
**AGENT**

> Checked all five against the actual config. **The design works uniformly — because our config already normalizes every server (even the HTTP-bridged and lazy-init ones) into a plain stdio launch spec `{command, args, env_vars}`.** That's precisely the standard MCP-host contract, so `swe-swe-mcp` just has to be a generic MCP client. Per server:
>
> | MCP | command | shape | works because |
> |---|---|---|---|
> | `swe-swe-agent-chat` | `npx @choonkeat/agent-chat …` | plain stdio, **blocking** `send_message` | id-multiplexed request; socket stays open till reply |
> | `swe-swe-playwright` | `mcp-lazy-init … -- npx @playwright/mcp` | stdio + lazy HTTP init on 1st `tools/call` | lazy-init fires regardless of *who* the client is |
> | `swe-swe-preview` | `npx agent-reverse-proxy --bridge …/preview/mcp` | stdio→HTTP bridge | just a command to exec |
> | `swe-swe-whiteboard` | `npx @choonkeat/agent-whiteboard` | plain stdio | trivial |
> | `swe-swe` (orchestration) | `npx agent-reverse-proxy --bridge …/mcp?key=…` | stdio→HTTP bridge | just a command to exec |
>
> So `swe-swe-mcp` reads `mcpServers`, execs each `command+args`, does the `initialize` handshake, and holds the stdio pipes. `swe` is its thin CLI face over a unix socket in the session dir. Feasible for all — but four concrete decisions decide the shape:
>
> **1. Config source + env expansion.** The codex/gemini/claude configs differ (TOML vs JSON, `env_vars` whitelist vs `sh -c "exec … $VAR"`). Those wrappers exist only to work around *each agent's* env quirks. `swe-swe-mcp` has none — it gets the exact same env, so we feed it **one canonical JSON** (the clean `mcpServers` shape) and it does its **own `$VAR` expansion** in args. New template file, not a reused agent config.
>
> **2. Schema→flags (`-h`).** `swe-swe-mcp` calls `tools/list`, converts each tool's `inputSchema` to flags: string/bool/number → typed flags, enum → choices, required vs optional, description → help. Levels: `swe -h` lists MCPs → `swe <mcp> -h` lists tools → `swe <mcp> <tool> -h` lists flags. **Confirm you want the 3-level `swe <mcp> <tool> [flags]`** (some MCPs have ~10 tools, so tool can't collapse into the mcp name).
>
> **3. Blocking + cancellation.** `send_message` blocks for the human reply — fine: `swe-swe-mcp` multiplexes by JSON-RPC id over one pipe, so a slow `send_message` doesn't block an orchestration call. If the agent kills a `swe` process, `swe-swe-mcp` must emit `notifications/cancelled` for that id.
>
> **4. Result rendering.** MCP results are content blocks: text → stdout; image (draw/screenshot) → write file, print path (agent reads images by path); structured → JSON to stdout.
>
> **One bonus this design buys us:** because `swe-swe-mcp` is separate from the agent and env-identical, the servers survive `claude --continue` / agent restarts — browser context and the chat queue no longer get torn down on every agent reboot (today they do). And the agent runs with **no MCP config at all**, which is exactly what a gated environment demands.
>
> Net: the only genuinely new code is (a) `swe-swe-mcp` = generic stdio MCP host + unix-socket broker, (b) `swe` = schema→flags CLI client, (c) init wiring: launch `swe-swe-mcp` with the agent's env + drop the agent's own MCP config + put `swe` on PATH + CLAUDE.md instructions, (d) golden tests. Shall I write that up as the phased plan, or do you want to lock decision #2 (command levels) and #4 (image handling) first?

[Quick replies]
- Write the phased plan (3-level, image=file+path)
- Lock decisions first, let's discuss #2/#4
- How does `swe` find the unix socket?
- What about the agent's OWN mcp servers (user-added)?

**USER**

> `swe-swe-mcp` could also be simplest if it works with 1 mcp only. Then `swe` cli itself multiplex. 
>
> Maybe when each `swe-swe-mcp` launch, they have a filesocket in a standard .swe-swe/tmp or known subdirectory. Then `swe` just list and finds them and talk to them thru the socket file

<small>took 55.3s</small><br>
**AGENT**

> Agreed — one `swe-swe-mcp` per MCP is the right cut. It collapses the hard part (a routing broker) into a trivial 1:1 adapter, and the **socket directory becomes the registry** — no config for `swe` to read, no ports, no central process.
>
> **Shape:**
> - Entrypoint loops the canonical `mcpServers` config and launches **N instances**, each: `swe-swe-mcp --name swe-swe-agent-chat --socket .swe-swe/run/mcp/agent-chat.sock -- npx -y @choonkeat/agent-chat …`, all with the agent's exact env. `swe-swe-mcp` itself parses no config — it execs the one command after `--` and fronts it with one unix socket. (Sockets live under a `.swe-swe/` **subdir**, which the directory convention allows — not a loose file.)
> - `swe` is pure client + multiplexer: `swe -h` = `readdir` the socket dir → connect to each → `tools/list` → render the tree. `swe <mcp> <tool> [flags]` = open that one socket, `tools/call`, print result. The filename *is* the mcp name (strip `swe-swe-` → `swe agent-chat send-message`, `swe playwright browser-navigate`, …).
>
> **Works for all five** — same uniform stdio spec, now 1:1 instead of multiplexed. Each server independently supervised/restartable; one crashing doesn't blind the others.
>
> **What genuinely stays in `swe-swe-mcp` (doesn't vanish with the split):**
> 1. **Per-instance concurrency.** Even one server needs id-muxing: `send_message` blocks for the human while a `send_progress` or barge-in check must still get through. So `swe-swe-mcp` accepts multiple concurrent socket clients and muxes them by JSON-RPC id onto the child's single stdio. Smaller than a global broker, but still real — and mandatory for agent-chat specifically.
> 2. **Cancellation.** `swe` process killed → its socket conn drops → `swe-swe-mcp` emits `notifications/cancelled` for that id to the child.
> 3. **Lifecycle.** `initialize` handshake on boot; unlink the socket on exit; log child PID + exit status (coding rule). `swe` treats ECONNREFUSED / stale socket as "mcp unavailable."
>
> **Three small decisions left, then I'll write the plan:**
> - **A. Env-var expansion** of `$SWE_SERVER_PORT` etc. in the args — do it in the entrypoint (pre-expanded before exec) or inside `swe-swe-mcp`? Entrypoint is simpler and keeps `swe-swe-mcp` dumb.
> - **B. Name collisions/awkwardness** — orchestration is literally named `swe-swe`, so strip-prefix gives `swe swe list-sessions`. Keep full name (`swe swe-swe list-sessions`) or special-case it?
> - **C. Command depth** — confirm 3-level `swe <mcp> <tool> [flags]` (tools can't collapse into mcp name; agent-chat/playwright have many).
>
> Lock A/B/C and I'll turn it into the phased task plan (swe-swe-mcp adapter → swe client → entrypoint/init wiring → golden tests).

[Quick replies]
- A: entrypoint expands · B: keep full name · C: 3-level — write the plan
- Let me answer A/B/C differently
- Why not have swe-swe-mcp do env expansion?
- Show me the .swe-swe/run/mcp layout first

**USER**

> for B, prefer keeping fullname. Maybe also cli name is wrong.. how about `mcp`?

<small>took 28.3s</small><br>
**AGENT**

> `mcp` as the CLI name is great — and with full server names it lines up 1:1 with the tool ids models already recognize:
>
> - `mcp__swe-swe-agent-chat__send_message` → `mcp swe-swe-agent-chat send_message --text "..."`
> - `mcp__swe-swe-playwright__browser_navigate` → `mcp swe-swe-playwright browser_navigate --url ...`
> - `mcp__swe-swe__list_sessions` → `mcp swe-swe list_sessions`
>
> So `mcp -h` lists servers, `mcp <server> -h` lists that server's tools, `mcp <server> <tool> -h` lists flags. That symmetry basically documents itself — the agent can guess the command from any tool id it's seen.
>
> **Naming locked:**
> - **`mcp`** = the agent-facing client CLI (on PATH, called via Bash).
> - **`swe-swe-mcp`** = the internal per-server adapter daemon (one per MCP, launched by entrypoint, drops a socket). Matches the other `swe-swe-*` host binaries.
> - **B = full server names** ✓
>
> **Still open (I'll default these unless you object):**
> - **A. Env expansion → entrypoint** (pre-expand `$SWE_SERVER_PORT` etc. before exec; keeps `swe-swe-mcp` a dumb 1:1 adapter).
> - **C. 3-level `mcp <server> <tool> [flags]`** ✓ (now clearly required — tools can't fold into the server name).
>
> If A/C are good, I'll write the phased plan:
> 1. **`swe-swe-mcp`** — single-child stdio MCP adapter: exec `-- cmd args`, `initialize`, accept concurrent socket clients, id-mux onto child stdio, `notifications/cancelled` on client drop, unlink-on-exit, log child exit.
> 2. **`mcp`** — socket-dir discovery + schema→flags + result rendering (text→stdout, image→file+path, structured→JSON).
> 3. **init/entrypoint wiring** — new canonical `mcpServers` JSON, launch N `swe-swe-mcp`, drop the agent's own MCP config in mcp-less mode, put `mcp` on PATH + CLAUDE.md instructions.
> 4. **golden tests** for the mcp-less init output.
>
> Green light?

[Quick replies]
- Green light — write the plan
- Change A (expand in swe-swe-mcp instead)
- Rethink C
- One more concern before planning

**USER**

> Sorry `swe-swe-mcp` rename to `mcp-cli-proxy`
>
> > Env expansion
> Not expanding seem most compatible move?

<small>took 38.1s</small><br>
**AGENT**

> Renamed: daemon = **`mcp-cli-proxy`**, client = **`mcp`**. 
>
> On env expansion — you're right, and here's *why* not-expanding is the most compatible: reuse the **`sh -c "exec …"` spec that gemini/claude already use today** and have `mcp-cli-proxy` exec it **literally**. The shell does the expansion from the inherited env; `mcp-cli-proxy` implements zero expansion logic. Concretely:
>
> ```
> mcp-cli-proxy --name swe-swe-agent-chat --socket .swe-swe/run/mcp/swe-swe-agent-chat.sock \
>   -- sh -c 'exec npx -y @choonkeat/agent-chat --autocomplete-url http://localhost:$SWE_SERVER_PORT/api/autocomplete/$SESSION_UUID?key=$MCP_AUTH_KEY ...'
> ```
>
> Why this wins over the alternatives:
> - **Proven** — gemini config already launches all five servers via `sh -c "exec …"`, so we know the exact strings work for every one.
> - **No reinvented shell** — quoting/`$VAR`/globbing semantics stay in `sh`, not in buggy hand-rolled Go expansion.
> - **Runtime env, not baked** — ports/keys resolve at launch from the live env (same env the agent gets), not frozen into a static file.
> - **Clean process tree** — `exec` replaces the shell, so `mcp-cli-proxy` waits on the *real* child and logs its true PID/exit (coding rule holds).
>
> So `mcp-cli-proxy` stays a dumb 1:1 adapter: exec `-- <argv>` verbatim, `initialize`, front it with one unix socket, id-mux concurrent clients, cancel-on-drop, unlink-on-exit, log child exit.
>
> **Final locked design:**
> - **`mcp`** (client) — discover sockets in `.swe-swe/run/mcp/*.sock`; `mcp -h` lists servers, `mcp <server> -h` lists tools, `mcp <server> <tool> [flags]` calls; results: text→stdout, image→file+path, structured→JSON. Mirrors `mcp__<server>__<tool>`.
> - **`mcp-cli-proxy`** (daemon, 1 per server) — `sh -c "exec …"` spec passed verbatim, no expansion.
> - **A** = no expansion via `sh -c` ✓ · **B** = full names ✓ · **C** = 3-level ✓.
>
> Green light to write the phased task plan (mcp-cli-proxy → mcp → entrypoint/init wiring + drop agent's own MCP config in mcp-less mode → golden tests)?

[Quick replies]
- Green light — write the plan
- Wait — one concern about sh -c
- Should mcp-less be a flag or the default?
- How do agents learn to use `mcp`?

**USER**

> Green light — write the plan

<small>took 1m 17s</small><br>
**AGENT**

> Plan written to **`tasks/2026-07-01-mcp-less-cli-proxy.md`**. Structure:
>
> **Architecture** — two stdlib-only Go binaries (built like `mcp-lazy-init`):
> - **`mcp-cli-proxy`** — one daemon per MCP server; execs the `sh -c "exec …"` spec verbatim (no expansion), does `initialize`, fronts one unix socket, id-muxes concurrent clients onto the child stdio, cancels on client drop, logs child exit.
> - **`mcp`** — stateless client; `.swe-swe/run/mcp/*.sock` **is the registry**; `-h` synthesizes flags from each tool's `inputSchema`; results render text→stdout / image→file+path / structured→JSON. `mcp__X__Y` ↔ `mcp X Y`.
>
> **Phases:**
> - **0** — scaffolding + the one decision I left open: is mcp-less a `--mcp-less` init flag or auto-detected? (I default to an explicit flag, using the two-commit TDD flag pattern from CLAUDE.md.)
> - **1** — `mcp-cli-proxy` daemon (+ unit test w/ fake stdio child: concurrency, blocking call, client-drop→cancel).
> - **2** — `mcp` client (discovery, schema→flags, rendering).
> - **3** — entrypoint/init wiring: canonical `mcpServers` template, launch N proxies with agent env, **drop the agent's own MCP config**, `mcp` on PATH + CLAUDE.md instructions, golden tests.
> - **4** — live dogfood in a test container.
>
> **Open questions I flagged (worth your call before I start Phase 0):**
> 1. **Mode switch** — flag vs auto-detect?
> 2. **User-added MCP servers** — in mcp-less mode a user's own agent MCP config would be bypassed; do we also feed those into `mcp-cli-proxy` instances, or scope this to swe-swe's five?
> 3. **Agent steering** — how hard to push the model toward `mcp …`, and spelling out that `send_message` *blocks* (the agent must wait on that command).
> 4. **Proxy restart policy** (esp. agent-chat, which hosts the chat UI).
>
> Want me to start implementing Phase 0, or resolve those four first?

[Quick replies]
- Start Phase 0 (explicit --mcp-less flag)
- Resolve the 4 open questions first
- Auto-detect, not a flag
- Also handle user-added MCP servers

**USER**

> Resolve the 4 open questions first

<small>took 1m 3s</small><br>
**AGENT**

> ## 1. Mode switch → **explicit `--mcp-less` flag now, default later**
> Not auto-detect: "MCP is gated" is an environment *policy*, not something we can reliably probe, and auto-detection would make golden tests nondeterministic. A flag is testable and follows the two-commit TDD flag pattern in CLAUDE.md.
> But note the bigger truth: **the proxy path is strictly more portable than agent-hosted MCP** — it works gated or not. So the real trajectory is flag → dogfood → make it the *default* and retire the agent-hosted path. The one caveat holding it back from day-one default: our `mcp` CLI is **tools-only** (`tools/list`+`tools/call`); native MCP clients also surface resources/prompts/sampling. swe-swe's five are tools-only so we lose nothing, but that's the gap to close before default.
>
> ## 2. User-added MCP servers → **architect general, scope v1 to swe-swe's five**
> `mcp-cli-proxy` is already server-agnostic, so supporting user servers is "free" mechanically. The actual work is *ingesting* their config — which lives in per-agent formats (codex TOML vs gemini JSON vs claude `.mcp.json`) — so it means writing per-agent config parsers, orthogonal to the core. Ship the five first; add user-server ingestion as an additive fast-follow. (Worth noting: if MCP is gated at the agent layer, the user's own servers are dead too — so long-term this proxy is the *only* way their custom servers work, which makes generalizing genuinely valuable.)
>
> ## 3. Agent steering → **appended system prompt (primary) + CLAUDE.md (backup) + self-documenting `-h`**
> The sharp risk is the **blocking** contract: the agent must run `mcp swe-swe-agent-chat send_message --text "..."` and *wait* — its stdout IS the user's reply — never background it, and end every turn on it. I'll lift the exact wording from today's agent-chat tool description ("blocks until the user responds; the reply is RETURNED by this call") and retarget it to the command. Three reinforcing layers: (a) `--append-system-prompt` carries the loop + blocking rule + bootstrap ("start with `mcp swe-swe-agent-chat check_messages`"); (b) CLAUDE.md as durable backup; (c) `mcp <server> <tool> -h` inherits the tool's own description, so the blocking note shows up there too.
>
> ## 4. Restart policy → **each `mcp-cli-proxy` self-restarts its own child; socket stays stable**
> Since each proxy fronts exactly one child, restart lives *inside* it: child stdio dies → respawn same argv → re-`initialize` → socket never moves (clients just retry). Per-child **exponential backoff + crash-loop cap**; on give-up, mark the socket unhealthy so `mcp` returns a clean "server unavailable" instead of hanging. Every exit logged (coding rule). agent-chat is priority — if it can't stay up, surface that loudly in container logs. This keeps the entrypoint dumb (fire-and-forget N proxies); whether swe-swe-server *also* supervises the proxy processes themselves is a minor follow-up (container restart covers the worst case).
>
> ---
> If you're good with all four, I'll fold them into the plan and start Phase 0 (baseline `--mcp-less` flag + binary scaffolding). Any you want to change?

[Quick replies]
- All four good — fold in and start Phase 0
- Make mcp-less the default, no flag
- Change #3 (steering approach)
- Change #4 (supervise in swe-swe-server)

**USER**

> All four good — fold in and start Phase 0

<small>took 12m 42s</small><br>
**AGENT**

> Decisions folded into the plan, and **Phase 0 (flag baseline) is done and green.**
>
> What landed (no functional effect yet, per the two-commit TDD pattern):
> - `InitConfig.MCPLess` field + `--mcp-less` flag parsing + reuse-from-saved wiring (`init.go`).
> - Registered `MCPLess` in `TestInitConfigReuseCoverage` (the guard that every persisted field is classified).
> - New golden variant `mcp-less` (Makefile `_golden-variant` line + `main_test.go` variant).
>
> Verified:
> - `mcp-less` golden `entrypoint.sh` and `Dockerfile` are **byte-identical** to the `default` variant — the flag is a true no-op so far.
> - Only functional diff: `"mcpLess": true` in `init.json`.
> - `make test` → all packages `ok` (TestGoldenFiles/mcp-less PASS, TestInitConfigReuseCoverage PASS).
>
> I have **not committed** — I don't commit unless you ask. Two ways to proceed:
> 1. Commit this as the baseline (`feat(swe-swe): add --mcp-less init flag (baseline, no effect)`), then start the real implementation: `mcp-cli-proxy` daemon skeleton + `mcp` client skeleton + Dockerfile build stages (rest of Phase 0/Phase 1).
> 2. Skip committing for now and push straight into `mcp-cli-proxy`.
>
> Which do you want?

[Quick replies]
- Commit baseline, then start mcp-cli-proxy
- Don't commit yet — start mcp-cli-proxy
- Commit baseline and stop here for now
- Show me the init.go diff first

**USER**

> Commit baseline, then start mcp-cli-proxy

<small>took 2m 28s</small><br>
**AGENT**

> Baseline committed (3b1193f). Now writing the mcp-cli-proxy daemon: single-child stdio MCP adapter with id-multiplexed unix socket, initialize handshake, restart/backoff, and cancel-on-disconnect. This is a chunk of Go — will report when it builds + tests green.

<small>took 6m 6s</small><br>
**AGENT**

> **Phase 1 done — `mcp-cli-proxy` daemon landed and green** (commit 47a7183).
>
> What it does (the host half of MCP-less mode):
> - One instance fronts exactly one stdio MCP server, exposed over a unix socket.
> - Execs `-- <argv>` **verbatim** (the `sh -c "exec …"` spec lets the shell expand `$VAR`; the proxy expands nothing).
> - MCP `initialize` handshake once; then id-**multiplexes concurrent socket clients** onto the child's single newline-delimited stdio.
> - `notifications/cancelled` on client disconnect; child **restart w/ exponential backoff + crash-loop cap**; every child exit logged (coding rule).
>
> Tests (`-race`, all pass):
> - `TestToolsListRoundTrip` — id remapped out and restored back.
> - `TestConcurrentClientsNoHeadOfLineBlock` — a slow blocking call does **not** stall a concurrent fast call (proves the multiplex, the key correctness property for `send_message`).
> - `TestParseArgs`.
>
> Shipping wiring:
> - Canonical source `cmd/mcp-cli-proxy/` (tested); bundled copy `templates/host/mcp-cli-proxy/main.go` ships via a Dockerfile build stage → `/usr/local/bin/mcp-cli-proxy`.
> - `check-mcp-cli-proxy-sync` Makefile guard prevents the two copies drifting (mirrors the prctx pattern). `make test` fully green, golden regenerated.
>
> **Next: Phase 2 — the `mcp` client CLI.** It'll: discover `.swe-swe/run/mcp/*.sock`, `tools/list` each, synthesize flags from `inputSchema` (`mcp -h` → servers, `mcp <server> -h` → tools, `mcp <server> <tool> -h` → flags), then `tools/call` and render results (text→stdout, image→file+path, structured→JSON).
>
> Want me to build `mcp` now, or pause here to review the daemon first?

[Quick replies]
- Build the `mcp` client now
- Pause — let me review the daemon first
- Show me the daemon's socket protocol summary
- Push these commits first

**USER**

> Build the `mcp` client now

<small>took 5m 56s</small><br>
**AGENT**

> **Phase 2 done — the `mcp` client CLI is landed and green** (commit 6e68f7c). Both halves of the mechanism now exist and are proven to interoperate.
>
> The client:
> - Discovers servers by listing the socket dir (`SWE_MCP_DIR`, default `/workspace/.swe-swe/run/mcp`) — the filesystem is the registry.
> - `mcp` → servers, `mcp <server>` → tools, `mcp <server> <tool> -h` → flags synthesized from the tool's `inputSchema`, `mcp <server> <tool> [flags]` → call.
> - Flags typed from JSON Schema (string/bool/int/number/array/object, enum, required); `--flag=v` and bare `--bool` supported.
> - Results: text→stdout, image/audio→written to a file whose path is printed, `structuredContent`→JSON. **No client-side read timeout**, so blocking tools (`send_message`) work.
>
> **Real end-to-end smoke test** (actual binaries, not fakes): launched `mcp-cli-proxy --name demo-server -- sh -c "exec <stdio-mcp>"`, then:
> ```
> $ mcp                              # → lists demo-server
> $ mcp demo-server                  # → shout  Uppercase-ish echo.
> $ mcp demo-server shout -h         # → --text string (required) / what to shout
> $ mcp demo-server shout --text 'hi there'
> ECHO:hi there
> $ mcp demo-server shout            # → mcp: missing required flag --text  (exit 1)
> ```
> The full path works through the real `sh -c "exec …"` spec shape. Unit tests (`-race`) cover schema→flags coercion, enum/required validation, and text/image/structured rendering. `make test` fully green; both bundle-sync guards pass.
>
> **Status:** mechanism complete. What's left is **Phase 3 — entrypoint/init wiring**: emit a canonical `mcpServers` JSON in `--mcp-less` mode, launch one `mcp-cli-proxy` per server with the agent's env, **stop writing the agent's native MCP config**, and inject the CLAUDE.md/system-prompt instructions teaching the agent to use `mcp …` (incl. the blocking `send_message` contract). That's the wiring that actually flips a session into MCP-less mode.
>
> Want me to start Phase 3, or pause to review the two binaries first?

[Quick replies]
- Start Phase 3 (entrypoint wiring)
- Pause — let me review the binaries
- Push all three commits first
- Show me how a real session's config maps to proxies

**USER**

> Start Phase 3 (entrypoint wiring)

<small>took 2m 47s</small><br>
**AGENT**

> Mapping the Phase 3 wiring. Confirmed the entrypoint writes per-agent native MCP config (codex TOML / gemini JSON / goose YAML / opencode JSON / `claude mcp add` / pi bridge) — all top-level {{IF AGENT}} blocks over the same canonical server set. Since MCP servers are per-session, the proxy launch has to live in swe-swe-server's session path, not the once-run entrypoint. Waiting on a map of that launch/teardown path, then I'll bring you a concrete Phase 3 design to confirm before I touch the session-creation critical path.

<small>took 4m 12s</small><br>
**AGENT**

> Phase 3 mapped. One finding changes the shape, and one simplification removes most of the risk.
>
> ## The finding: swe-swe-server can't see `MCPLess`
> swe-swe-server does **not** read `init.json` — it has no runtime knowledge of the flag. But the proxies must be launched per-session (they need each session's `SESSION_UUID`/`MCP_AUTH_KEY`/ports), which is swe-swe-server's job. So mcp-less has to *reach* swe-swe-server at runtime. Cleanest: the entrypoint (which starts swe-swe-server) **exports `SWE_MCP_LESS=1`** and writes a canonical **`mcp-servers.json`** spec file (the 5 `sh -c "exec …"` commands). swe-swe-server reads both at startup.
>
> ## The simplification: proxies as process-group children (free teardown)
> Instead of swe-swe-server explicitly spawning + tracking + killing N proxies (new code in the session-creation *and* teardown critical paths), launch them as **children of the agent's own process group**. The agent already launches via a `bash -c` wrapper (the recording wrapper), and teardown already does `killSessionProcessGroup(-pid)` which reaps the **whole** group. So if the wrapper does `start proxies in background; exec <agent>`, the proxies:
> - inherit the exact per-session env automatically (same process),
> - write sockets into a per-session `SWE_MCP_DIR`,
> - and get killed on session end with **zero new teardown code**.
>
> That confines the runtime change to one spot (the launch wrapper) and keeps the dangerous teardown path untouched.
>
> ## Proposed split (each independently landable + testable)
> - **3a — init/template plumbing (no runtime risk, golden-testable):** thread `config.MCPLess` into `processEntrypointTemplate`; add `{{IF MCP_LESS}}`/`{{IF NO_MCP_LESS}}`; gate the per-agent native-MCP config blocks off in mcp-less mode; write `mcp-servers.json`; export `SWE_MCP_LESS=1`. *(Note: the entrypoint's skip flag is flat/non-nesting, and the agent MCP blocks are already inside `{{IF AGENT}}`. Rather than refactor to nested skip, I'll gate at the top level — a single `{{IF NO_MCP_LESS}}` region around the whole native-MCP-config section, since those blocks do nothing but write MCP config.)*
> - **3b — the launch wrapper (the one critical-path change):** when `SWE_MCP_LESS`, wrap the agent command so it boots one `mcp-cli-proxy` per spec (process-group children) + sets `SWE_MCP_DIR` before `exec`ing the agent. Behind the env gate, so non-mcp-less sessions are byte-identical.
> - **3c — agent instructions:** teach the agent to use `mcp …` incl. the blocking `send_message` contract. This is the only genuinely per-agent-messy bit (claude has `--append-system-prompt`; codex/gemini/pi differ). Proposal: land a workspace instructions doc + lean on `mcp -h` self-docs first, add `--append-system-prompt` for claude as the reliable bootstrap, and treat the other agents as follow-up.
>
> Two decisions for you:
> 1. **Proxies as process-group children via the launch wrapper** — good? (vs explicit spawn/track/kill in Go.)
> 2. **3c scope** — claude-first (`--append-system-prompt`) + docs now, other agents later? Or hold 3c until I can do all agents uniformly?
>
> Once you confirm, I'll start with 3a (safe, golden-tested) and bring it back before 3b touches the launch path.

[Quick replies]
- Both good — start 3a
- Explicit Go spawn/kill instead of process-group children
- Do 3c for all agents uniformly (hold until ready)
- Rethink: launch proxies from entrypoint, not per-session

**USER**

> local main branch have moved. can we look to rebase to local main first then see if questions still apply

<small>took 1m 1s</small><br>
**AGENT**

> Rebased mcp-less onto local main — clean, now 2 ahead / 0 behind (linear). Only conflict was agent-chats/index.html (both sides prepended a same-date manifest entry); kept both, newest-first. Main's incoming commit was just a docs-export, no code change.
>
> Which questions did you want to re-check against the new base? I don't have the pre-rebase question thread in context after the compaction — remind me what was open and I'll re-evaluate.

[Quick replies]
- It was Phase 3 wiring

**USER**

> It was Phase 3 wiring

<small>took 2m 28s</small><br>
**AGENT**

> Post-rebase state of the Phase 3 fleet-wiring: nothing moved. Main only pulled a docs-export commit (zero code), so the wiring code is byte-identical to before the rebase — every open question stands exactly where it was. Tests green, golden regenerates clean, tree committed.
>
> Where Phase 3 actually stands:
> • Phases A/B/C DONE + committed (spec-selection, per-session launch+reap, default-on + native-config gating).
> • Phase D (dogfood) is ~done and this very chat proves it — we're talking THROUGH the proxy fleet right now. Last dogfood run verified 4/5 proxies live (agent-chat, swe-swe, playwright, whiteboard).
>
> The one genuinely-open thread:
> 1. preview proxy — it hung on MCP initialize because the bridge 302'd. The fix (71e590070, key-exempt /preview/mcp) LANDED but was never re-dogfooded. So the open question is just: does 'mcp swe-swe-preview' now complete initialize instead of hanging? Needs one live re-check.
> 2. Deferred by design (v1 = claude-only): codex/gemini/goose/opencode still need their own native-config-skip + steering.
>
> So — no question was invalidated or answered by the rebase. Want me to re-dogfood the preview proxy now to close thread #1?

[Quick replies]
- Re-check the preview proxy

**USER**

> 1. Is current swe-swe-server version carrying the fix or we need reboot?
>
> 2. leave a mcp-less doc that I can point other agents at to introduce the concept to agents and have them setup their own prompt/steering

<small>took 1m 55s</small><br>
**AGENT**

> Both answered:
>
> 1) NO — the running swe-swe-server does NOT carry the preview fix. The live binary at /usr/local/bin/swe-swe-server was built Jul 3 17:26; the preview fix (71e590070) was committed 21:48 UTC, ~4h later. The 17:26 build corresponds to the default-on commit (38991b6d9, 16:42), which is exactly what the last reboot rebuilt on. So a reboot/rebuild is required before the preview proxy will complete initialize — re-dogfooding it right now would still show the hang. Nothing to test until we rebuild.
>
> 2) DONE — wrote docs/mcp-less.md: a self-contained primer any agent can be pointed at. It covers the concept (MCP gated, CLI not), the mcp CLI surface (mcp / mcp <server> / mcp <server> <tool> -h / call), the blocking agent-chat contract, 'discover don't memorize', env vars, and a copy-paste steering block for non-claude agents to drop into their own system prompt / AGENTS.md / rules file. Passes make ascii-check. Not committed yet (I commit only when you ask).
>
> Given #1, the natural next step is: reboot to rebuild on the current branch (which now includes the preview fix + rebase), then re-dogfood the preview proxy to close that last thread. Want me to commit the doc first, or fold it into that reboot cycle?

[Quick replies]
- Commit the doc, then reboot

**USER**

> /ck:export-chat-logs and commit the doc along too

<small>took 12.7s</small><br>
**AGENT**

> Exporting chat log + will commit docs/mcp-less.md alongside, then run scrub-git-history.

