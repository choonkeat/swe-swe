# Dockerless single-binary distribution

## Status

**In progress.**

> Follow-up 2026-07-18: node/npx demoted from required to
> Agent-View/node-agent-only. The four `@choonkeat/*` npm tools now spawn
> via the bundled `swe-npx` helper (registry-resolving exec, user-level
> `~/.swe-swe/npx-cache`), so 5 of 6 tabs run with NO node installed.
> See tasks/2026-07-18-swe-npx-node-free-helpers.md.

- [x] **Phase 0** -- Remove vscode entirely. Done 2026-06-27. Removed `--with-vscode` flag + `WithVSCode` config/reuse, `VSCODE_SERVICES` compose block + `code-server/Dockerfile` + `nginx-vscode.conf`, `withVSCode` template params, the vscode pane/option/iframe + `buildVSCodeUrl`/`_vscodeEnabled`/`getVSCodeUrl` from the JS, and the `with-vscode` golden variant. `make test` green; 327 node tests green; golden has 0 vscode/code-server refs. See -phase0.log.
- [x] **Phase 1** -- Build static-linux binaries + `go:embed` the payload into the `swe-swe` CLI. Done 2026-06-28 (commit 36d04ee3c). `make dockerless-payload` builds the six host-arch static-linux binaries; CLI `go:embed`s them; `TestDockerlessPayloadEmbedsBinaries` asserts each is a correct-arch ELF (TDD RED->GREEN). Payload 29MB; CLI delta 9.2MB->38.9MB when referenced. NB: linker DCE strips the embed from `make build` until Phase 2 references it (verified via go-test binary). Deferred: multi-arch matrix in build-platforms; script/config staging moves to Phase 2 init. See -phase1.log.
- [x] **Phase 2** -- `swe-swe init --dockerless` dumps the payload + wires it; `swe-swe up`/`--open`/`down` dispatch; `--dockerless` errors on non-Linux CLI. Done 2026-06-28. 2a flag+non-Linux GOOS guard; 2b binary extraction (0755); 2c dump+mode marker; 2d up/down dispatch (loopback bind, --open polls+xdg-open); 2e server path-agnostic (-workspace/-worktrees/-repos/-swe-home + env, defaults reproduce container, golden unchanged); 2c-ii swe-swe-open shim+symlinks + SWE_SERVER_PORT wiring + project-scoped .mcp.json (option ii, 5 servers). E2E smoke: init dumps 6 bins+shim+5 symlinks+.mcp.json+marker; `swe-swe up` boots server, binds loopback, HTTP 302. `make test` green. CAVEAT: project-scope .mcp.json may prompt Claude approval on first use. Tab-level e2e deferred to dockerless-e2e task. See -phase2.log.
- [~] **Phase 3** -- DROPPED 2026-06-29. Only touched *Docker mode* (not dockerless), and its goal (slim base, drop Go toolchain) conflicts with keeping Go in the image for agents to use. Docker-mode Dockerfile left as-is (compiles from source, keeps Go). Not needed for the dockerless vision.
- [x] **Phase 4** -- Tunnel works dockerless. Done 2026-06-29. Embedded the pinned `swe-swe-tunnel` client in the payload (Makefile go-install + dockerlessBinaries=7); `swe-swe up` loads tunnel config from init.json and passes `-tunnel-server-url`/`-tunnel-bin`/`-tunnel-client-cert`/`-tunnel-local-ports` to the dumped server (loopback bind suits tunnel). E2E smoke: `init --dockerless --tunnel-server-url ...` saves config + dumps the 7MB client; `make test` green. (Scoped to dockerless mode; the Docker-mode `DockerfileOnly` auto-detect tweak was NOT done -- Docker-mode concern, same reasoning as the Phase 3 drop. Live tunnel CONNECT not exercised: needs allowlisted pubkey + live tunneld.) See -phase4.log.
- [x] **Phase 5** -- Pluggable browser backend (`local` default / `remote`) so Agent View can run elsewhere. Done 2026-06-29. `-agent-view=local|off|<url>` (+ `SWE_AGENT_VIEW`); capability detection + graceful degradation (lean host -> tab hidden, `{"status":"unavailable"}` not 500, `agentViewAvailable` in WS, terminal-ui hides the tab); extracted `browserProcs` (5b); standalone `swe-swe-server -mode browser-backend` allocation service with bearer auth + max-sessions cap (5c, unit-tested + verified live: real binary serves /health + auth + spawns Xvfb/Chromium on POST /sessions); remote client wires a local CDP reverse-proxy (rewrites /json* hosts back to localhost:CDPPort) + dynamic VNC proxy target (5d); thin `docker/browser-backend` image + `make browser-backend-image` (5e). Follow-ups CLOSED 2026-07-04: remote `vnc-ready` probes the remote websockify; chromium `--host-resolver-rules` maps localhost back to the swe-swe host (auto from allocation source addr, `SWE_AGENT_VIEW_LOCALHOST` override); live remote e2e shipped as `make test-e2e-agent-view-remote[-image]` -- BOTH tiers PASS live, image tier proves the Dockerfile + cross-namespace localhost nav. The e2e surfaced two real bugs, both fixed: `-mode browser-backend` ignored `SWE_CDP_PORTS`/`SWE_VNC_PORTS` (env parsed after mode dispatch), and headful chromium ignores `--remote-debugging-address` (loopback-only CDP) -> per-session CDP reverse-proxy forwarder on the public port. See -phase5.log.
- [~] **Phase 6** -- Mac-native dockerless. Code complete on the Linux-verifiable side (2026-06-30); pending live verification on a Mac. Done: server cross-compiles for darwin/{arm64,amd64} via build-tag split of the SO_PEERCRED + PR_SET_CHILD_SUBREAPER blockers (6a); OS-aware payload (`bin/<goos>-<goarch>`) + Makefile host-GOOS build + cross-compile-safe tunnel + darwin allowed in the guard with an experimental warning (6b, verified: 7 valid Mach-O binaries cross-build); broker disabled off Linux with client fail-open (6c); darwin-aware PTY recording so sessions start (BSD `script`, 6d, unit-tested). Linux `make test` unchanged throughout. **Pending:** live Mac run (build CLI -> init --dockerless -> up -> exercise all 6 tabs); **full macOS broker** via per-session sockets (SO_PEERCRED/`/proc` identity is unportable -- currently disabled, git falls back to normal creds); macOS Agent View (local stack absent -> degrades via Phase 5, or use the remote backend). See -phase6.log.

## Problem

swe-swe ships as a scaffolder, not an app. The user-facing `swe-swe` CLI
(`cmd/swe-swe`) embeds the *source* of `swe-swe-server` and writes it to
disk at `swe-swe init`; the server is then compiled inside `docker build`
(Dockerfile stage 1, `golang:1.24-alpine`) and run from a
`golang:1.24-bookworm` base -- a full Go toolchain image is the runtime
base. Distribution therefore *requires* Docker, even though the server
itself already `go:embed`s all of its web assets, page templates,
agent-chat UI, and container docs (`main.go:51-60`).

We want zero required Docker, all six UI tabs working
(**Agent Terminal, Terminal, Preview, Files, Agent Chat, Agent View**),
and the user only ever typing `swe-swe`.

## Approach: embedded payload (decided 2026-06-27)

Rather than a busybox-style multi-call binary, the `swe-swe` CLI
**`go:embed`s the prebuilt binaries + shell scripts + config it needs**
and dumps them on `swe-swe init --dockerless` -- exactly the pattern the
CLI already uses for its `templates/` tree, extended to compiled outputs.
Helpers stay as separate stdlib binaries (no risky package surgery, still
individually testable); the CLI just carries their compiled outputs.

There are **two audiences** for these binaries, which determines what to
embed:

- **Docker mode** -- binaries run *inside the container* = always Linux.
- **Dockerless mode** -- binaries run *on the host* = the host's OS/arch.

| CLI build    | embeds (dockerless host set) | embeds (docker image set, always Linux) |
|--------------|------------------------------|------------------------------------------|
| linux/amd64  | linux/amd64                  | linux/amd64 (dedup)                      |
| linux/arm64  | linux/arm64                  | linux/arm64 (dedup)                      |
| darwin/*     | *(none yet -- Phase 6)*      | linux/amd64 + linux/arm64                |

All binaries are `CGO_ENABLED=0` static, so one Linux set runs on
alpine/bookworm/slim alike. **First cut targets Linux-host dockerless**
(linux CLI embeds one Linux set serving both audiences). The darwin CLI
embeds only the Linux image set; **`--dockerless` on a non-Linux CLI must
error out** with a clear "Linux host only for now" message until Phase 6.

The embedded payload (one tree, two consumers -- `init --dockerless`
dumps it to the host; the thin Dockerfile `COPY`s it into the image):

- **Binaries**: `swe-swe-server`, `git-credential-swe-swe`,
  `git-sign-swe-swe`, `mcp-lazy-init`, `swe-swe-broker-probe`,
  `swe-swe-fork-convo`.
- **Scripts**: `swe-swe-open` shim (+ `xdg-open`/`open`/... symlink
  names), any setup currently done by `entrypoint.sh`.
- **Config**: MCP server registrations, env/PATH setup.

## Dependency reality (why this is tractable)

| Tab            | Backend                                          | Class            |
|----------------|--------------------------------------------------|------------------|
| Agent Terminal | agent CLI in PTY via `bash` + util-linux `script`| host binary      |
| Terminal       | plain `bash` PTY                                 | host binary      |
| Preview        | Go reverse-proxy -> `localhost:$PORT`            | in-binary (Go)   |
| Files          | `npx @choonkeat/md-serve`                        | host npx/node    |
| Agent Chat     | `npx @choonkeat/agent-chat` (MCP)               | host npx/node    |
| Agent View     | Xvfb + chromium + x11vnc + websockify + playwright MCP | Tier D (heavy) |

Five of six tabs are already "binaries + npx + claude" with no Docker.
Only **Agent View** needs the browser/VNC quartet (system packages), so
that is the only piece we must make relocatable (Phase 5).

## Goals / non-goals

- **Goal:** a Linux host with `node`/`npx`, an agent CLI (`claude`), and
  `git` runs all six tabs via `swe-swe init --dockerless` + `swe-swe up`,
  no Docker daemon involved.
- **Goal:** the user only ever types `swe-swe`; `swe-swe-server` is
  internal.
- **Goal:** the same embedded binaries feed a thin Docker image.
- **Goal:** tunnel mode does not require Docker.
- **Goal:** Agent View works co-located (default) or offloaded.
- **Non-goal (this cut):** Mac-native dockerless -- explicitly errors out
  on non-Linux CLIs; delivered in Phase 6 (needs darwin binaries + fixing
  the abstract-socket `@swe-swe-broker` and `script -T/-I/-O` couplings).
- **Non-goal:** dropping the compose/SSL/Traefik path -- it stays.

---

## Phase 0 -- Remove vscode entirely

(Done -- see Status.)

---

## Phase 1 -- Build static-linux binaries + embed the payload

- Makefile: add a stage that `CGO_ENABLED=0 GOOS=linux` builds the six
  binaries from their sources (the server from
  `templates/host/swe-swe-server` via the existing copy-out flow used by
  `test-server`; the four stdlib helpers from `templates/host/<name>`;
  `swe-swe-fork-convo` from the server module's `cmd/`) and stages them
  into an embed dir, e.g. `cmd/swe-swe/dockerless-payload/bin/linux-<arch>/`.
- Stage the scripts + config into the same payload tree.
- `go:embed` the payload tree into the CLI (new `embed.FS`), alongside the
  existing `templates` embed.
- Build ordering: binaries -> stage payload -> build CLI. Wire into
  `build-cli` / `build-platforms` so every published CLI carries the
  Linux set (per-arch).

**Verify (test-first where it bites):** a Go test asserts the embedded FS
contains each expected binary and that each is an ELF for the right arch
(`debug/elf` parse); `make build` produces a CLI whose embedded payload
lists all six. Record the CLI size delta.

---

## Phase 2 -- `swe-swe init --dockerless` dumps + wires the payload

- `swe-swe init --dockerless`: **error out immediately if the CLI is not
  a Linux build** (`runtime.GOOS != "linux"`) with a clear message; do
  not write anything.
- On Linux: extract the embedded payload into `./.swe-swe/` (binaries to
  `./.swe-swe/bin`, restore `0755`), generate the `swe-swe-open` shim +
  `xdg-open`/`open`/... symlinks, register the 5 MCP servers (agent-chat,
  playwright, preview, whiteboard, orchestration) into `~/.claude.json`
  (command strings `templates.go:866-885`), and set up env/PATH (mirroring
  `entrypoint.sh:182-236`). Write a `mode: dockerless` marker beside
  `init.json`. No Dockerfile, no compose, no `.env` (defaults:
  `SWE_PORT=1977`).
- Configurable paths (today hardcoded): add `-workspace`, `-worktrees`,
  `-repos` flags (env fallbacks) to the server (currently
  `worktreeDir = "/worktrees"` etc.), defaulting to cwd-relative dirs for
  host mode.
- `swe-swe up` dispatch: today `swe-swe` passes `up`/`down` to
  `docker compose` (`main.go:34`). Detect the `mode: dockerless` marker
  and instead exec the dumped `./.swe-swe/bin/swe-swe-server`. The user
  never types `swe-swe-server`.
  - `swe-swe up` -- start server (dockerless) or compose (compose mode).
  - `swe-swe up --open` -- also open the browser at the listen URL.
  - `swe-swe down` -- stop whichever was started.

**Verify:** on a clean Linux box (node+claude+git, no Docker daemon),
`swe-swe init --dockerless && swe-swe up` serves Agent Terminal,
Terminal, Preview, Files, Agent Chat end-to-end (Agent View in Phase 5).
`swe-swe up --open` launches the browser. On macOS, `swe-swe init
--dockerless` errors cleanly and writes nothing.

---

## Phase 3 -- Thin Dockerfile from the same payload

- Rewrite the generated `Dockerfile` to base `node:24-bookworm-slim`,
  `apt-get install` host deps (`git git-lfs bash util-linux curl jq
  poppler-utils` + browser stack `chromium xvfb x11vnc novnc
  websockify`), `npm i -g @anthropic-ai/claude-code` (+ selected agents),
  then `COPY` the **same embedded Linux binaries** (dumped into the build
  context by `swe-swe init`). No Go-builder stage, no toolchain base.
- Stop embedding the server *source* in the CLI once the image consumes
  the prebuilt binary (the Makefile still builds it from
  `templates/host/swe-swe-server` to produce the embedded binary).

**Verify:** image builds and runs all six tabs (browser e2e via
`docs/dev/test-container-workflow.md`); image materially smaller; build
time drops (no in-image Go compile). Record before/after size.

---

## Phase 4 -- Tunnel works dockerless

Today tunnel forces compose: `*tunnelServerURL != ""` excludes
dockerless mode (`init.go:945`), even though the server already
supervises the `swe-swe-tunnel` subprocess in-process
(`tunnel_supervisor.go:522`, `SWE_TUNNEL_BIN`).

- Remove `tunnelServerURL` from the compose-forcing condition.
- Embed/locate the `swe-swe-tunnel` binary (today `go install` pinned in
  Dockerfile:47-58) for host mode; wire `SWE_TUNNEL_BIN` /
  `SWE_TUNNEL_SERVER_URL` / `SWE_TUNNEL_UNIQUE` / `SWE_TUNNEL_CLIENT_CERT`.
- In tunnel mode the server binds loopback and the tunnel client dials
  out (already implemented) -- no Traefik, no Docker.

**Verify:** `swe-swe init --dockerless --tunnel-server-url <url>` +
`swe-swe up` -> reachable through the tunnel, all tabs functional, no
Docker.

---

## Phase 5 -- Pluggable browser backend (Agent View / Tier D)

Exploit the existing seam: Playwright MCP is wired purely by env
(`BROWSER_CDP_PORT` -> `@playwright/mcp --cdp-endpoint
http://localhost:$BROWSER_CDP_PORT`); the VNC view is a reverse-proxy to
`localhost:$VNCPort`; browser start is already on-demand via `POST
/api/session/{uuid}/browser/start` + `mcp-lazy-init`.

- `BrowserBackend` interface, two implementations:
  - `local` (**default = today**): `startSessionBrowser` spawns
    Xvfb/chromium/x11vnc/websockify (`main.go:4231+`).
  - `remote`: on `browser/start`, call a browser service to allocate a
    per-session chromium and return `{cdpURL, vncURL}`; point the CDP env
    + VNC proxy at those. (Service built in
    `tasks/2026-06-27-browser-backend-service.md`.)
- Internal config: `SWE_BROWSER_BACKEND=local|remote`,
  `SWE_BROWSER_SERVICE_URL`. User-facing flag on `swe-swe up` (persisted
  to `.swe-swe`): `--agent-view=local|<url>|off`.
- Graceful degradation: backend unreachable -> Agent View "unavailable",
  other five tabs unaffected.

**Verify:** `local`: Agent View works on a host with the browser stack
(unchanged). `remote`: a lean host with no chromium offloads Agent View
to a sidecar; the pane renders and the agent drives the browser.

---

## Phase 6 (follow-up) -- Mac-native dockerless

- Build darwin/amd64 + darwin/arm64 binaries; darwin CLI embeds the
  darwin host set (in addition to the Linux image set).
- Fix the two Linux-only couplings:
  - credential/signing broker `@swe-swe-broker` abstract unix socket ->
    a filesystem unix socket under the home dir (portable).
  - terminal recording `script -T/-I/-O` (GNU) -> detect BSD `script` (or
    a portable PTY-record path).
- Lift the Phase 2 non-Linux error once darwin dockerless is verified.

**Verify:** `swe-swe init --dockerless && swe-swe up` on macOS serves all
six tabs (Agent View via the browser stack on the Mac or a remote
backend), no Docker.

---

## End state

- `swe-swe init --dockerless` then `swe-swe up`: all six tabs, no Docker
  daemon, on a Linux host with node/claude/git (+ browser stack or a
  remote backend for Agent View). `swe-swe up --open` opens the browser.
  The user only ever types `swe-swe`.
- The same embedded static-Linux binaries feed a thin Docker image.
- Tunnel mode: dockerless. vscode: gone. Compose/SSL/Traefik: untouched.
- macOS: docker mode works; `--dockerless` errors cleanly until Phase 6.
