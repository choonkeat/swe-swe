# Procfile runner for multi-service apps (docker-free)

- Date: 2026-07-13
- Status: IN PROGRESS - Phases 1 & 2 complete (model + supervisor runtime; live teardown/leak-fix proven)
- Owner: choonkeat
- Motivation session: agent-chat "Procfile vs docker direction" (2026-07-13)
- Related prior work: `tasks/2026-07-04-preview-hostname-vhost.md`,
  `agent-chats/2026-07-12-02-multi-service-preview-gap-and-default-layout.md`

## 1. Why

Today, swe-swe users building multi-service apps reach for `docker compose`,
which forces two bad outcomes:

1. `swe-swe init --with-docker` bind-mounts the host Docker socket, which is
   host-root-equivalent (ADR-0013). Handing that to an AI agent session is the
   single biggest hole in the trust model.
2. Compose-started containers are not tied to swe-swe's session lifecycle, so
   they **leak**: sessions end, containers keep running, the host accumulates
   remnants nobody cleans up.

The decision (2026-07-13): steer multi-service app devs to a **Procfile** and
ship swe-swe's **own** Procfile runner. Processes started by the runner are
ordinary children in the session's process group, so they die with the session
-- nothing leaks, no socket, no root. The Procfile format (`name: command`, one
line per service) is trivially simple, and building our own runner is what lets
us tie service lifetime to the session (a third-party runner like goreman does
not know what a swe-swe session is).

This reverses the stance currently written on the `preview-hostname-vhost`
branch's `docs/multi-service.md` ("swe-swe does NOT start/stop/supervise your
services ... there is no mini compose runtime"). That line must be updated: we
are now shipping a supervisor, deliberately, scoped to Procfile semantics.

## 2. Goals / Non-goals

Goals:
- A single small runner binary that reads a `Procfile` and runs each service as
  a child process, with output multiplexed into the Agent Terminal.
- Automatic, collision-free **port assignment** derived from the session's base
  `PORT`, exported so services discover each other with zero hardcoded numbers.
- Automatic **service discovery**: host is always `localhost`; each service's
  port is published as an env var named after the service.
- `.env` file loading (foreman parity) plus honoring the existing
  `.swe-swe/env` convention.
- **Clean teardown**: session end kills the whole runner process group; no
  leaked processes.
- Docs that make Procfile the blessed multi-service path and demote
  `--with-docker` to a last resort.

Non-goals (v1):
- No auto-restart of crashed services (see Design 4.7 for exact semantics).
- No declarative service graph / health checks / depends_on ordering.
- No replacement for compose stacks that genuinely need container networking
  (`--with-docker` stays available, just de-emphasized).
- swe-swe-server does not auto-start the runner in v1 (user invokes it). Server
  integration (auto-detect Procfile + one-click "Start services") is a documented
  follow-up, not this task.

## 3. Background facts (verified in code)

- **Per-session ports** (`swe-swe-server/main.go`): each session gets a preview
  base `PORT` in `previewPortStart..previewPortEnd` (3000-3019). Derived ports
  use fixed offsets: agent-chat `+1000`, public `+2000`, CDP `+3000`, VNC
  `+4000`, files `+6000`, preview proxy `+20000`
  (`agentChatPortFromPreview` etc., main.go:4563-4586). Injected into the session
  shell by `buildSessionEnv` (main.go:622-630) as `PORT`, `PUBLIC_PORT`,
  `BROWSER_CDP_PORT`, `BROWSER_VNC_PORT`, `AGENT_CHAT_PORT`.
  => The reserved offset bands are `+1000,+2000,+3000,+4000,+6000,+20000`.
     The `+5000`, `+7000..+? ` and `+8000` bands are FREE across all 20 session
     bases. Concretely `8000-8019` and `10000-19999` never collide with any
     reserved derived port (public=5000-5019, cdp=6000-6019, vnc=7000-7019,
     files=9000-9019, proxy=23000-23019).
- **Teardown precedent**: `startSessionMdServe` / `stopSessionMdServe`
  (main.go:4660-4725) is the exact pattern to mirror if we ever server-manage
  the runner: launch with `SysProcAttr{Setpgid:true}`, `trackPid` +
  `registerSessionPid`, and `syscall.Kill(-pgid, SIGKILL)` on session end. For
  v1 (user-invoked runner inside the PTY), the existing
  `killSessionProcessGroup` already reaps the whole PTY process group on session
  end -- so a user-run `swe-run` is torn down for free. The runner must still
  Setpgid its own children and trap signals so `Ctrl-C` / `SIGTERM` cleans up
  mid-session.
- **Preview demux already solves viewing** (`preview-hostname-vhost` branch,
  unmerged, 35 commits): the per-session preview listener demuxes
  `{name}-{port}.<reach>` and bare `{port}.<reach>` to `127.0.0.1:{port}`,
  regardless of how the service was started. So any port the runner assigns is
  previewable with zero extra work once that branch is merged.

## 4. Design

### 4.1 The runner binary

- Name: `swe-run` (working name; confirm before build). A new small Go program.
- Source location: `cmd/swe-swe/templates/host/swe-run/` (new), built and
  installed into the image at `~/.swe-swe/bin/swe-run` (already on the session
  PATH via `buildSessionEnv`, main.go:632). For dockerless, installed the same
  way other `~/.swe-swe/bin` helpers are.
- Invocation: user types `swe-run` (or `swe-run -f Procfile.dev`) in the Agent
  Terminal. Reads `./Procfile` by default.
- It inherits the session env, so it sees the base `PORT` and can derive service
  ports without any server round-trip.

### 4.2 Procfile format

Foreman-compatible subset:

```
web: node server.js
worker: node worker.js
db: postgres -D ./pgdata -p $PORT_DB -k /tmp
```

- One `name: command` per line. `#` comments and blank lines ignored.
- `command` runs via `sh -c` so `$VAR`, pipes, and `&&` work.
- Names must match `[A-Za-z0-9_-]+`.

### 4.3 Port assignment

- The runner assigns each service a **session-unique** port derived from base
  `PORT`, so two sessions running the same Procfile never collide (this is the
  isolation compose used to give for free).
- **Primary service gets the session base `PORT`** so the default Preview tab
  shows it with zero config. Primary = the service named `web` if present, else
  the first line. (Overridable with `-primary <name>`.)
- Non-primary services get ports from the free band. Recommended deterministic
  formula (executor may refine, MUST keep the invariants below):
  `port(i) = PORT + 5000 + i*20` for the i-th non-primary service
  (0-based over non-primary services). For base 3000 that yields 8000, 8020, ...
  Invariants: (a) session-unique across all 20 bases, (b) avoids reserved
  offsets `+1000/+2000/+3000/+4000/+6000/+20000`, (c) in `1024-65535`, (d)
  within the preview demux's allowed range.
- Alternative considered: have the runner request ports from swe-swe-server via
  a tiny local endpoint (server owns the authoritative port map). More robust
  but adds coupling + an API. **v1 uses the formula**; server allocation is a
  noted follow-up if the formula proves fragile.

### 4.4 Discovery (env vars)

For every service `name` with assigned `port`, the runner exports to ALL
services before starting them:

- `PORT_<NAME>` = its port (`<NAME>` = service name uppercased, non-alnum -> `_`).
  E.g. Procfile line `db:` -> `PORT_DB`.
- Each service additionally sees its own port as plain `PORT` (foreman parity),
  so single-service apps that read `$PORT` keep working.

Discovery contract handed to users:
- **Host is always `localhost` / `127.0.0.1`.** There are no container networks,
  so there is never another hostname to resolve.
- **The port of service `foo` is `$PORT_FOO`.** You control the variable name by
  what you name the service line.
- Your app builds its own URL, e.g. `postgres://localhost:${PORT_DB}/mydb`. We
  deliberately do NOT synthesize `DATABASE_URL` because we do not know your user,
  password, or db name -- but you can set `DATABASE_URL` once in `.env` or
  `.swe-swe/env` referencing `$PORT_DB` and it becomes fully automatic.

### 4.5 Env file loading

Load and inject into every service, in this precedence (later wins):

1. Inherited session environment (PATH, PORT base, etc.).
2. `.swe-swe/env` (existing per-workspace convention).
3. `.env` in the working dir (foreman parity), if present.
4. Runner-assigned `PORT` / `PORT_<NAME>` values (these always win so discovery
   is authoritative).

`.env` / `.swe-swe/env` parsing: `KEY=value` lines, `#` comments, no shell
expansion of the file itself beyond what `sh -c` does per command.

### 4.6 Log multiplexing

- Merge each service's stdout+stderr into the runner's stdout, line-prefixed
  `name | ...` (pad names to align). Optional ANSI color per service (respect
  `NO_COLOR`). This is what appears in the Agent Terminal.

### 4.7 Signals, exit, teardown

- Start every service child with `SysProcAttr{Setpgid:true}` so each service
  (and its own children) is its own killable group.
- The runner traps `SIGINT`/`SIGTERM`: forwards `SIGTERM` to every service
  group, waits a grace period (default 5s), then `SIGKILL` survivors.
- **No auto-restart** in v1.
- **Any service exiting triggers graceful shutdown of the rest** (foreman
  semantics): log `name exited (code N)`, then tear down all remaining services
  and exit with that code. Rationale: a silently half-running stack is worse
  than a clean stop; the user sees the failure immediately. (DECISION POINT: if
  you prefer "keep the others running and just report", flip this -- but foreman
  parity is the recommended default.)
- Because the runner and its groups live inside the session PTY process group,
  session end already `SIGKILL`s everything via `killSessionProcessGroup`. The
  trap handles the interactive `Ctrl-C` and mid-session `swe-run` restart cases.

### 4.8 Common-daemon cheat sheet (docs deliverable)

Off-the-shelf daemons take a port flag; point each at its assigned env var:

| Service  | Procfile line (port from env)                          |
|----------|--------------------------------------------------------|
| Postgres | `db: postgres -D ./pgdata -p $PORT_DB -k /tmp`         |
| Redis    | `cache: redis-server --port $PORT_CACHE`               |
| MySQL    | `db: mysqld --port=$PORT_DB --datadir=./mysql-data`    |
| Mongo    | `db: mongod --port $PORT_DB --dbpath ./mongo-data`     |

App then connects to `localhost:$PORT_DB` etc.

### 4.9 Relationship to Preview vhost + cookies

- Once `preview-hostname-vhost` is merged, every assigned port is reachable in
  the browser as a bare-port subdomain (`8000.<reach>`) or a named vhost
  (`auth-8000.<reach>` with upstream Host rewrite for stacks that route on Host).
- Because sub-apps live on distinct hostnames, cookies are isolated by default;
  set `Domain=.<reach>` to share one cookie across sub-apps (the proxy already
  rewrites cookie domains). This is the "same cookie or different cookie, your
  choice" behavior the user asked for -- it comes from the vhost branch, not this
  task. This task only needs to make the ports exist and be discoverable.

### 4.10 Relationship to `--with-docker`

- Keep `--with-docker` working; do not remove it.
- Docs demote it: the multi-service guide leads with Procfile; `docker.md` and
  the guide add a clear "prefer the Procfile path; `--with-docker` mounts the
  host socket = host root (ADR-0013)" callout.
- Deprecating/soft-warning the flag in `swe-swe init` is a follow-up, not v1.

## 5. Open decisions to confirm before/at execution

1. Binary name `swe-run` (vs `swe-procfile`, `swe-swe run`).
2. One-exits-all vs keep-others-running on a service exit (4.7). Recommended:
   one-exits-all (foreman parity).
3. Port formula (4.3) vs server-allocated ports. Recommended: formula for v1.
4. Whether to write this on `main` and cross-link the vhost branch, or rebase
   onto / after the vhost merge. Recommended: build on `main`; the runner does
   not depend on vhost code (only the docs cross-reference it).

## 6. Execution plan (TDD, worktree)

Each phase: write test first (RED), implement (GREEN), `make test`. For any
template/docs change touching `cmd/swe-swe/templates` run
`make build golden-update` and stage `cmd/swe-swe/testdata/golden`.

### Phase 1 - Procfile parse + port/env model (pure, unit-tested) -- DONE
- [x] 1.1 New package. Layout decision: canonical tested source in `cmd/swe-run/`
      (root module, `_test.go`), mirroring mcp/prctx; bundled stdlib copy into
      `cmd/swe-swe/templates/host/swe-run/` comes in Phase 3.
- [x] 1.2 `parseProcfile(io.Reader) ([]Service, error)` - names, commands,
      comments, blanks, validation, duplicate/empty detection. Table tests.
- [x] 1.3 `assignPorts(base int, services []Service, primary string) (map[string]int, error)`
      - primary=base, others via 4.3 formula `base+5000+i*20`. Invariants tested
      (uniqueness across bases 3000-3019, avoids reserved bands, range bounds,
      overflow error).
- [x] 1.4 `buildServiceEnv(...)` + `parseEnvFile` + `normalizeEnvName` -
      precedence from 4.5, `PORT` + `PORT_<NAME>` injection, runner-ports-win.
      Table tests including `.env`/`.swe-swe/env` merge and PORT-always-wins.

### Phase 2 - Supervisor runtime -- DONE
- [x] 2.1 Launch via `sh -c`, `Setpgid`, captured pipes (supervisor.go). Each
      proc goroutine drains both pipes to EOF before `cmd.Wait` (StdoutPipe
      contract); child exit status always logged (name/pid/code).
- [x] 2.2 Log multiplexer: aligned `name | ` prefixes, per-service ANSI color,
      NO_COLOR honored, mutex-guarded no-torn-lines (concurrency test).
- [x] 2.3 Signal->ctx teardown: SIGTERM to every group, grace, SIGKILL
      survivors; one-exits-all with correct aggregate exit code. Tests:
      OneExitsAll, ContextCancel, SigkillEscalation (TERM-ignoring proc),
      StartFailure. `go test -race` clean.
- [x] 2.4 CLI `swe-run [-f Procfile] [-primary NAME]`: reads base PORT (fallback
      5000), prints port table, wires SIGINT/SIGTERM to ctx cancel.
- [x] LIVE: 3-svc discovery verified; SIGINT teardown leaves ZERO leaked
      descendants (the headline leak-fix).

### Phase 3 - Packaging + install
- 3.1 Build `swe-run` into the image; install to `~/.swe-swe/bin/swe-run`
      (Dockerfile + dockerless install path). Confirm it lands on the session
      PATH.
- 3.2 Golden: if any template/init surface changes, `make build golden-update`.

### Phase 4 - Docs
- 4.1 Rewrite container `.swe-swe/docs/docker.md`: lead users to Procfile;
      keep docker section but add the host-root callout + "prefer Procfile".
- 4.2 New/updated `docs/multi-service.md` (reconcile with the vhost-branch copy
      when merged): Procfile quickstart, discovery contract (localhost +
      `PORT_<NAME>`), `.env`/`.swe-swe/env`, daemon cheat sheet, teardown
      guarantee, and the vhost preview / cookie cross-link.
- 4.3 Container `.swe-swe/docs/app-preview.md`: add "run services with a
      Procfile via `swe-run`" pointer.
- 4.4 CHANGELOG entry.
- 4.5 Reverse the "no mini compose runtime" line wherever it appears.

### Phase 5 - Verification
- 5.1 `make test` green (unit).
- 5.2 Live e2e in a test container (docs/dev/test-container-workflow.md):
      write a 3-line Procfile (`web` static server + a `db`/echo + a `worker`),
      run `swe-run`, assert: primary reachable on session PORT via Preview,
      a second service reachable on its bare-port subdomain, `$PORT_<NAME>`
      visible to siblings, and -- the headline -- end the session and confirm
      NO leftover processes (the leak fix). Tear the container down.

### Phase 6 - Procfile helper slash command (added 2026-07-13)

A bundled slash command that makes the Procfile approachable for non-experts:
list the current services and conversationally CRUD entries, so users do not
hand-edit the file or memorize the port/discovery conventions.

- 6.1 Source: `cmd/swe-swe/slash-commands/swe-swe/procfile.md` + `.toml`
      (single umbrella command recommended over one-command-per-verb; the
      user's working name was `procfile-setup-service`). Bundled + seeded like
      every other `swe-swe` command; `make build golden-update` after.
- 6.2 Behavior (the command is a prompt driving the agent, not code):
  - If no `Procfile` exists, offer to scaffold one -- detect the project
    (package.json scripts, framework, a docker-compose.yml to translate FROM)
    and propose starter `name: command` lines.
  - List existing entries with their resolved ports (primary = `$PORT`, others
    = `$PORT_<NAME>`) so the user sees the discovery wiring.
  - Add / edit / remove a service, validating the name grammar and warning on
    the primary/`web` special case.
  - When adding a known daemon (postgres/redis/mysql/mongo), auto-fill the
    port-flag form from the cheat sheet (4.8) and remind the user to reference
    `localhost:$PORT_<NAME>` from dependents (offer to set `DATABASE_URL` etc.
    in `.env`).
  - Explain, inline, that none of this needs `--with-docker` and that services
    die with the session.
- 6.3 It only edits the `Procfile` / `.env`; it never launches or supervises
      (that is `swe-run`). Keep it read-then-confirm before writing.

## 7. Out of scope / follow-ups

- Server auto-detect of `Procfile` + one-click "Start services" button/MCP tool,
  wired like `startSessionMdServe`.
- `swe-swe init` deprecation warning on `--with-docker`.
- Server-allocated ports API (if the formula proves fragile).
- Auto-restart / health checks / `depends_on` ordering.
- Tunnel-mode named vhost labels (already tracked as vhost Follow-up A).
