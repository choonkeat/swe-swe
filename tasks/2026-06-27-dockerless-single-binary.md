# Dockerless single-binary distribution

## Status

**Planned.** Not started.

- [ ] **Phase 0** -- Remove vscode entirely
- [ ] **Phase 1** -- Multi-call `swe-swe-server` (fold helper binaries as subcommands + `install`)
- [ ] **Phase 2** -- Prebuilt server binary + thin Dockerfile (drop the Go-toolchain build/runtime stages)
- [ ] **Phase 3** -- `swe-swe init --dockerless` host installer (replay entrypoint.sh on the host) + configurable paths
- [ ] **Phase 4** -- Tunnel works dockerless (decouple tunnel from compose mode)
- [ ] **Phase 5** -- Pluggable browser backend (`local` default / `remote`) so Agent View (Tier D) can run elsewhere

## Problem

swe-swe ships as a scaffolder, not an app. The user-facing `swe-swe` CLI
(`cmd/swe-swe`) embeds the *source* of `swe-swe-server` and writes it to
disk at `swe-swe init`; the server is then compiled inside `docker build`
(Dockerfile stage 1, `golang:1.24-alpine`) and run from a
`golang:1.24-bookworm` base -- a full Go toolchain image is the runtime
base. Distribution therefore *requires* Docker, even though the server
itself already `go:embed`s all of its web assets, page templates,
agent-chat UI, and container docs (`main.go:51-60`).

We want two prebuilt binaries and zero required Docker:

1. `swe-swe` -- the existing standalone host CLI, gaining a flag to
   initialise a dockerless host-native `swe-swe-server` setup.
2. `swe-swe-server` -- a standalone host binary whose heavy Tier-D
   browser dependency (Agent View) can be pointed at a backend running
   somewhere else; default config reproduces today's behavior.

All six UI tabs must remain functional in the dockerless setup:
**Agent Terminal, Terminal, Preview, Files, Agent Chat, Agent View.**

## Dependency reality (why this is tractable)

Per-tab backend and dependency class:

| Tab            | Backend                                          | Class            |
|----------------|--------------------------------------------------|------------------|
| Agent Terminal | agent CLI in PTY via `bash` + util-linux `script`| host binary      |
| Terminal       | plain `bash` PTY                                 | host binary      |
| Preview        | Go reverse-proxy -> `localhost:$PORT`            | in-binary (Go)   |
| Files          | `npx @choonkeat/md-serve`                        | host npx/node    |
| Agent Chat     | `npx @choonkeat/agent-chat` (MCP)               | host npx/node    |
| Agent View     | Xvfb + chromium + x11vnc + websockify + playwright MCP | Tier D (heavy) |

Five of six tabs are already "single binary + npx + claude" with no
Docker. Only **Agent View** needs the browser/VNC quartet, which cannot
be embedded in a Go binary (~system packages). That is the only piece we
must make relocatable.

Helper binaries currently built separately (all stdlib-only `package
main`, see Dockerfile:31-45): `git-credential-swe-swe`,
`git-sign-swe-swe`, `swe-swe-open`, `mcp-lazy-init`,
`swe-swe-broker-probe`, `swe-swe-fork-convo`. Folding these into one
multi-call binary is what makes "single file" literally true.

## Goals / non-goals

- **Goal:** a Linux host with `node`/`npx`, an agent CLI (`claude`), and
  `git` can run all six tabs with `swe-swe init --dockerless` +
  `swe-swe-server`, no Docker daemon involved.
- **Goal:** tunnel mode does not require Docker.
- **Goal:** Agent View works either co-located (default) or offloaded to
  a remote browser backend.
- **Non-goal (defer):** macOS-native host mode. Two Linux-only couplings
  remain -- the abstract unix socket `@swe-swe-broker` (creds/signing)
  and `script -T/-I/-O` recording syntax. Dockerless targets a Linux
  host or single container for now.
- **Non-goal:** dropping the existing compose/SSL/Traefik path. It stays
  for users who want it; dockerless is an additional, simpler path.

---

## Phase 0 -- Remove vscode entirely

vscode is dead weight for the target DX and is one of the three triggers
that force compose mode (`init.go:945`:
`*sslFlag == "no" && !*withVSCode && *tunnelServerURL == ""`).

**Remove:**
- `--with-vscode` flag + `WithVSCode` config (`init.go`, `init.json`).
- `cmd/swe-swe/templates/host/code-server/Dockerfile`,
  `cmd/swe-swe/templates/host/nginx-vscode.conf`.
- `vscode-proxy` + `code-server` compose service generation
  (`templates.go:591-655`, the `{{VSCODE_SERVICES}}` placeholder).
- The `vscode` pane from `PANES_IN_ORDER` (`terminal-ui.js:60`) + its
  label/layout entries; any vscode references in `link-provider.js`.
- vscode test variants in `main_test.go`; `.dockerignore` code-server
  lines.

**Verify:** `make build golden-update`; diff shows only removals.
`make test`. Grep `-ri vscode cmd/` returns nothing meaningful.

---

## Phase 1 -- Multi-call `swe-swe-server`

Make `swe-swe-server` a busybox-style multi-call binary. When invoked
under an alias name (argv[0]) or via an explicit subcommand, it runs the
corresponding helper instead of the server.

- Fold these into the server module as internal packages dispatched up
  front in `main()`: `git-credential-swe-swe`, `git-sign-swe-swe`,
  `swe-swe-open`, `mcp-lazy-init`, `swe-swe-broker-probe`,
  `swe-swe-fork-convo`.
- Dispatch rule: if `filepath.Base(os.Args[0])` matches a known helper
  name -> run it; else if `os.Args[1]` is a known subcommand -> run it;
  else run the server.
- Add `swe-swe-server install --prefix <dir>` (default `~/.swe-swe`):
  creates `<prefix>/bin`, symlinks each helper + xdg-open shim names
  (`xdg-open open x-www-browser www-browser sensible-browser`) back to
  the server binary, and writes the per-session env/PATH config that
  `entrypoint.sh:182-236` produces today.

**Verify:** `swe-swe-server git-credential-swe-swe` behaves identically
to the old standalone (broker round-trip test). `make test`. Dockerfile
stage 1 stops building the 6 separate binaries (Phase 2 consumes this).

---

## Phase 2 -- Prebuilt server binary + thin Dockerfile

- Add a real build target + release pipeline for `swe-swe-server`
  (per-platform, like `build-cli` in `Makefile:187-195`). Source already
  compiles standalone (own `go.mod.txt`/`go.sum.txt`, exercised by
  `make` server test target).
- Rewrite the generated `Dockerfile` to: base `node:24-bookworm-slim`,
  `apt-get install` the host deps (`git git-lfs bash util-linux curl jq
  poppler-utils` + browser stack `chromium xvfb x11vnc novnc
  websockify`), `npm i -g @anthropic-ai/claude-code` (+ other selected
  agents), then `COPY` the prebuilt `swe-swe-server`. No Go builder
  stage, no toolchain runtime base.
- Keep the embedded-source path available behind a flag during
  transition if needed, but default to prebuilt.

**Verify:** image builds and runs all six tabs (browser e2e via
`docs/dev/test-container-workflow.md`). Image size materially smaller;
record before/after. Build time drops (no Go compile).

---

## Phase 3 -- `swe-swe init --dockerless` + configurable paths

The host installer must replay what `entrypoint.sh` does in the
container, on the host:

- `claude mcp add` registration of the 5 MCP servers (agent-chat,
  playwright, preview, whiteboard, orchestration) into `~/.claude.json`
  (`entrypoint.sh:194`, command strings `templates.go:866-885`).
- PATH-prepend of `<prefix>/proxy` + `<prefix>/bin`; `BROWSER` ->
  `swe-swe-open` shim (`entrypoint.sh:218-231`). Reuses Phase 1
  `install`.
- Slash-commands / skills seeding equivalent (today
  `writeBundledSlashCommands`, `entrypoint.sh` SLASH_COMMANDS_COPY).

Configurable paths (today hardcoded, must become flags/env for host
mode): `/workspace` (workdir), `/worktrees`, `/repos`,
`/var/lib/tailscale`, `TLS_CERT_PATH`. Add `-workspace`, `-worktrees`,
`-repos` flags (env fallbacks) read in `main.go` (currently
`worktreeDir = "/worktrees"` etc.).

`swe-swe init --dockerless` writes generated artifacts **into
`./.swe-swe/`** only (env file, MCP config, a `mode: dockerless` marker
alongside `init.json`). No `./run` script, no Dockerfile, no compose, no
`.env` required (sane defaults: `SWE_PORT=1977`).

The user-facing command stays `swe-swe up`. Today `swe-swe` passes
`up`/`down`/etc. straight to `docker compose` (`main.go:34`). Make the
CLI **detect dockerless mode** (read the `.swe-swe` marker / `init.json`)
and, in that mode, exec the bundled server directly instead of docker
compose. `swe-swe-server` is internal -- the user never types it.
- `swe-swe up` -- starts the server (dockerless) or compose stack
  (compose mode), transparently.
- `swe-swe up --open` -- also opens the browser at the listen URL
  (optional convenience).
- `swe-swe down` -- stops whichever was started.

Packaging: the npm/brew artifact ships both `swe-swe` and the
`swe-swe-server` binary (or `swe-swe` is itself the multi-call binary);
`swe-swe up` locates and execs it. No separate install step for the user.

**Verify:** on a clean Linux box with node+claude+git, `swe-swe init
--dockerless && swe-swe up` serves all tabs except Agent View (Tier D
covered in Phase 5). `swe-swe up --open` launches the browser. Agent
Terminal, Terminal, Preview, Files, Agent Chat all functional
end-to-end.

---

## Phase 4 -- Tunnel works dockerless

Today tunnel forces compose: `*tunnelServerURL != ""` excludes
dockerless mode (`init.go:945`), even though the server already
supervises the `swe-swe-tunnel` subprocess in-process
(`tunnel_supervisor.go:522`, `SWE_TUNNEL_BIN`).

- Remove `tunnelServerURL` from the compose-forcing condition; tunnel
  becomes a config of the dockerless server too.
- Host installer fetches/locates the `swe-swe-tunnel` binary (today
  `go install` pinned to a commit in Dockerfile:47-58) -- either ship it
  as an optional download or document `go install`. Wire `SWE_TUNNEL_BIN`
  / `SWE_TUNNEL_SERVER_URL` / `SWE_TUNNEL_UNIQUE` /
  `SWE_TUNNEL_CLIENT_CERT` for host mode.
- In tunnel mode the server binds loopback and the tunnel client dials
  out (already implemented) -- no Traefik, no Docker needed.

**Verify:** `swe-swe init --dockerless --tunnel-server-url <url>` +
run -> reachable through the tunnel, all tabs functional, no Docker.

---

## Phase 5 -- Pluggable browser backend (Agent View / Tier D)

Exploit the existing seam: Playwright MCP is wired purely by env
(`BROWSER_CDP_PORT` -> `@playwright/mcp --cdp-endpoint
http://localhost:$BROWSER_CDP_PORT`); the VNC view is a reverse-proxy to
`localhost:$VNCPort`; browser start is already on-demand via `POST
/api/session/{uuid}/browser/start` + `mcp-lazy-init`.

- Introduce a `BrowserBackend` interface with two implementations:
  - `local` (**default = today**): `startSessionBrowser` spawns
    Xvfb/chromium/x11vnc/websockify (`main.go:4231+`).
  - `remote`: on `browser/start`, call a browser service to allocate a
    per-session chromium (own `user-data-dir` + display) and return
    `{cdpURL, vncURL}`; point the CDP env + VNC proxy target at those.
- Internal config: `SWE_BROWSER_BACKEND=local|remote`,
  `SWE_BROWSER_SERVICE_URL`. Remote backend needs: per-session
  allocate/release protocol, auth, cleanup, and two routable network
  paths (CDP for the agent + VNC websocket for the human).
- User-facing surface is a flag on `swe-swe up` (persisted into
  `.swe-swe` so subsequent `swe-swe up` reuses it):
  - `--agent-view=local` (default) -- use the host browser stack.
  - `--agent-view=<url>` -- remote backend, e.g.
    `--agent-view=https://browsers.example.internal`. Maps to
    `SWE_BROWSER_BACKEND=remote` + `SWE_BROWSER_SERVICE_URL=<url>`.
  - `--agent-view=off` -- disable the pane.
- The "somewhere else" is the existing Docker image reused as a
  browser-only sidecar (`docker run ... swe-swe/browser-backend`), run on
  any box. Document this. Graceful degradation: backend unreachable ->
  Agent View shows "unavailable", other five tabs unaffected.

**Verify:** `local` backend: Agent View works on a host that has the
browser stack (unchanged behavior). `remote` backend: lean host binary
with no chromium offloads Agent View to a sidecar and the pane renders +
the agent can drive the browser via playwright MCP.

---

## End state

- `swe-swe init --dockerless` then `swe-swe up`: all six tabs, no Docker
  daemon, on a Linux host with node/claude/git (+ browser stack or a
  remote browser backend for Agent View). `swe-swe up --open` opens the
  browser. The user only ever types `swe-swe`; `swe-swe-server` stays
  internal.
- Tunnel mode: dockerless.
- vscode: gone.
- Existing compose/SSL/Traefik path: untouched, still available; `swe-swe
  up` dispatches to the right backend automatically.
- The server is a single multi-call binary that self-installs its helpers
  (driven by `swe-swe up`, not by the user).
