# Dockerless boot parity with entrypoint.sh (single-source + parity test)

Status: PLANNED -- **GATED: do not start until the dev box boots and runs
in dockerless mode with all its existing configs** (tunnel, slash commands,
Agent View offload, chat-log export; see checklist below). The live run will
surface which gaps actually matter and in what order.
Decided: 2026-07-19 (chat session "Fix dockerless mode marker lockout")

## Problem

Container mode does its per-boot setup in `templates/host/entrypoint.sh`
(runs EVERY container boot). Dockerless mode does its setup in Go
(`cmd/swe-swe/dockerless.go`, `executeDockerlessInit`) and only at INIT
time. The duty lists have already drifted, and two payloads are duplicated
verbatim between the bash and Go implementations.

Decision from the design discussion: do NOT add a second maintained
entrypoint-like script for dockerless (a second script gives drift a nicer
file to live in, it does not prevent drift). Instead: single-source the
shared payloads, add a parity test over an explicit duty checklist, and
longer-term move boot duties into the server so both modes run the same
code every boot.

## Current duty inventory (audited 2026-07-19)

| Duty (entrypoint.sh)                   | Container            | Dockerless (Go)                        |
|----------------------------------------|----------------------|----------------------------------------|
| docker socket group perms              | yes ({{IF DOCKER}})  | n/a by design                           |
| slash-commands copy                    | yes                  | **MISSING** (flag saved to init.json, never installed) |
| skills install ({{SKILLS_INSTALL}})    | yes                  | **MISSING**                             |
| Claude MCP config                      | yes (claude mcp add, user scope) | yes (project .mcp.json)     |
| OpenCode MCP config                    | yes                  | **MISSING**                             |
| Codex MCP config (TOML, env_vars gotcha)| yes                 | **MISSING**                             |
| Gemini MCP config                      | yes                  | **MISSING**                             |
| Goose MCP config + configure-wrapper   | yes                  | **MISSING**                             |
| Pi mcp-bridge extension                | yes                  | **MISSING**                             |
| Claude hook guards (ask + stop)        | yes (global ~/.claude, jq merge) | yes (project settings.local.json, Go merge) -- single-sourced scripts, GOOD pattern |
| SWE_SERVER_PORT resolution             | yes                  | yes (`swe-swe up` env)                  |
| swe-swe-open shim + xdg-open symlinks  | yes (heredoc)        | yes (Go const) -- **DUPLICATED CONTENT**|
| drop privileges + exec server          | yes                  | n/a (already the invoking user)         |

Duplicated-content inventory (bash blob vs Go value, no shared source):

1. `swe-swe-open` shim body: entrypoint.sh heredoc `SHIM` vs
   `dockerlessOpenShim` const in dockerless.go ("identical in spirit").
2. The 5 MCP server command lines: `{{CLAUDE_MCP_SETUP}}` expansion in
   templates.go vs `dockerlessMCPServers()` in dockerless.go. (Plus 4 more
   hand-maintained copies inside entrypoint.sh for opencode/codex/gemini/
   goose -- same commands re-expressed in each agent's config syntax.)
3. Hook guard scripts: ALREADY single-sourced in `cmd/swe-swe/hook-scripts/`
   -- this is the pattern to replicate.

Init-time vs boot-time semantics: entrypoint duties re-run every boot;
dockerless duties only on re-init. `.swe-swe/restart-dockerless.sh`
compensates by re-initing every watchdog cycle, but plain `swe-swe up`
users do not get that. Long-term fix (phase C) erases the difference.

## Plan

### Phase A -- single-source the duplicated payloads
- Move the open-shim body to one embedded asset consumed by both
  entrypoint.sh templating and `writeDockerlessOpenShim`.
- Define the 5 MCP server specs ONCE (command + args + env deps as data),
  generate: claude `mcp add` lines, project .mcp.json, opencode json,
  codex toml (respect its env_vars whitelist gotcha), gemini json, goose
  yaml from that single table.
- Mirror the hook-scripts/ precedent for anything else that graduates.

### Phase B -- parity checklist test
- A Go test owns the duty table above as data: each duty declares
  {container: yes/no, dockerless: yes/exempt(reason)}.
- Test greps/asserts each duty's marker in entrypoint.sh template output
  AND in the dockerless init outputs (golden dirs), fails when a duty
  appears in one path but has no entry/exemption for the other.
- Effect: adding a block to entrypoint.sh without updating the table (or
  the dockerless path) breaks `make test`. That is the "track if anything
  is amiss" guarantee.

### Phase C (long-term, separate decision) -- move boot duties into the
server (or a bootstrap step it runs at startup): entrypoint.sh shrinks to
socket-perms + exec; both modes execute the same bootstrap code every boot.
Aligns with dockerless single-binary + Dockerfile-only roadmaps.

## Gate checklist: the dev box on dockerless (prerequisite for starting)

Boot via `.swe-swe/restart-dockerless.sh` and verify against what the
compose deployment provides today:

- [ ] tunnel: registers under this box's SWE_TUNNEL_UNIQUE (see
      .swe-swe/env) with the existing identity.key; public hostname
      reachable (env export + local ports)
- [ ] slash commands: /swe-swe:* and /ck:* available in sessions
      (currently expected to FAIL -- the Phase A/B work fixes the swe-swe
      bundle seeding; ck repo clone is the --with-slash-commands gap)
- [ ] skills: n/a on this box today (no --with-skills in restart flags)
- [ ] agents: claude works; opencode/codex/pi = expected missing MCP
      configs (gap above) -- decide which we actually need host-side
- [ ] Agent View offload via 172.17.0.1:9333 backend
- [ ] chat-log auto-export default-on (env passthrough without compose?)
- [ ] recordings, worktree sessions, preview vhost behavior host-side
- [ ] whatever else the live run surfaces -> append here

Surfaced by the first live run (DO droplet, 2026-07-19):
- FIXED b1225d44b: dockerless `up` passed `-tunnel-local-ports` to the
  server, which has no such flag -> usage dump + exit 2 crash-loop. This
  was the original "dockerless failed here" incident.
- DNS is per-unique: no blanket wildcard on the tunnel domain; every new
  unique needs `<unique>-tunnel` + `*.<unique>-tunnel` A records at the
  DNS host before tunneld can complete LE issuance (client hangs in
  "awaiting RegisterOK" until then). Candidate doc/UX fix: tunnel client
  should surface "cert issuance pending -- does DNS exist?" instead of
  waiting silently.
- systemd unit with Restart=always works as the dockerless watchdog (no
  screen/restart-script needed): see the droplet's /etc/systemd/system/
  swe-swe.service.

## Related

- tasks/2026-07-19-runtime-flag.md (--runtime enum; host default only
  becomes a GOOD default once this parity work lands)
- tasks/2026-06-27-dockerless-single-binary.md (Phase 6 darwin etc.)
- ac5b9b03d (mode marker cleared by docker-mode init)
