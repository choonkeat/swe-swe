# CHANGELOG

## Unreleased

### Features

- **The built-in `Artifact` tool is now blocked in agent-chat sessions**: swe-swe already installs a `PreToolUse` guard on `AskUserQuestion` (its menu renders only in the local TUI, which a web-chat user never sees). A second guard, `swe-swe-artifact-guard.sh`, now does the same for `Artifact`: that tool publishes a page to claude.ai, which is not the surface a swe-swe user is looking at and ships workspace content off-box, when the session already has a viewer of its own. Blocked calls get an exit-2 message spelling out the local route instead: write the page to `mockups/<name>.html`, serve that directory on the session's `PORT`, and put a `http://localhost:<PORT>/<name>.html` link in the chat reply -- the chat UI already intercepts localhost links and loads them in the App Preview pane rather than a new tab, so the user gets one click to the same result. Gating matches the existing guard exactly -- enforced only where the session has an agent-chat channel, so terminal TUI and plain `claude` runs are untouched -- and `SWE_ALLOW_ARTIFACTS=1` (or `AGENT_CHAT_DISABLE=1`) opts back in. Installed by both the container entrypoint and dockerless init from a single source script, with the settings merge dropping any prior `Artifact` matcher so re-init never duplicates.

- **Homepage tells you when a newer swe-swe is published**: The header's version stamp now grows a small `2.34.1 available` badge when the npm registry has a release newer than the one this server was built from; hovering it shows the upgrade command (`npx swe-swe@latest up`) and a link to the release notes, and clicking through opens the CHANGELOG. The check is a browser-side `fetch` of `https://registry.npmjs.org/swe-swe/latest` -- the registry sends `access-control-allow-origin: *` and `cache-control: max-age=300`, so the browser does the request and the caching, and the server makes no outbound call of its own. It fails silent: offline, blocked, or an unparseable response simply leaves the badge absent, as does a `dev` build (not on npm) or a server already on the newest version.

- **`tesseract-ocr` in the base image**: OCR (`tesseract`, English + orientation/script data) is now installed alongside the existing `poppler-utils`, so an agent can read text out of screenshots and scanned PDFs without an ad-hoc install per session.

- **Shut down the server from the homepage Settings dialog**: A new Server section in the homepage Settings dialog carries a "Shut down server" button (confirm-gated) that POSTs `/api/server/shutdown` and takes the exact graceful path a SIGTERM does -- every session closed in parallel, HTTP drained, exit 0 -- with the trigger named in the shutdown log (`shutdown requested via web UI from <addr>`). Especially useful in dockerless mode, where `swe-swe up` foregrounds the server and stopping it otherwise means finding the right terminal. The endpoint sits behind the auth cookie and is denied to shared-session guests; under a container restart policy (compose uses `unless-stopped`) the exit comes back as a fresh restart instead of a stop, and the dialog says so.

### Fixes

- **`swe-npx` hardening: verified cache, offline fallback, https-only registry, size caps, downgrade-proof memo**: Five gaps closed in the npm-package resolver that spawns swe-swe's own tools. (1) *Offline fallback now covers the download, not just dist-tags*: a resolve whose tarball or version doc cannot be fetched -- registry reachable but the CDN down, or a memoized `latest` that was never cached -- previously died instead of using the copy already on disk; unpinned requests now fall back to the newest verified cached version with a stderr note. An explicit `@1.2.3` still fails rather than silently running a different version, and an integrity mismatch is always fatal (never a fallback trigger). (2) *Cache entries are re-verified before every exec*: the sha256 of the extracted binary is recorded next to it at download time (`.swe-npx-digest`) and re-checked on each cache hit, along with a group/world-writable mode check -- a cache entry tampered with after download is discarded and re-fetched instead of exec'd. Entries written before this change carry no digest and are re-downloaded once. (3) *`SWE_NPX_REGISTRY` must be https* (plain http allowed for loopback only), tarball URLs are held to the same rule, and a tarball host differing from the registry host is noted on stderr; a version doc advertising neither `integrity` nor `shasum` is now rejected rather than trusted. (4) *Resource caps and split timeouts*: metadata 8 MiB, compressed tarball 128 MiB, total unpacked 512 MiB (decompression-bomb guard), with metadata on a 15s budget and tarball downloads on a separate 5m one -- the previous single 5s client timeout covered the whole body read and would abort a large binary on a slow link. Extracted files are also masked to never be group/world-writable. (5) *The `latest` memo can no longer force a downgrade*: rewriting `<pkg>.latest` to an old release was the cheapest way to make an already-patched box run a known-vulnerable build for up to a TTL. The cache is now the floor -- a memo naming something older than the newest cached version is ignored and the registry re-checked (the floor is a directory listing, not a digest pass, so the warm path stays ~11ms) -- and the memo records its own write time, so a restore or `touch` that resets mtimes cannot extend the TTL and a future-dated memo counts as expired rather than valid forever. A downgrade the registry itself declares (a rolled-back dist-tag) is still honoured, but announced on stderr. The legacy bare-version memo format is still read.

- **Agent View survives a browser-backend restart (tunnel mode)**: A `swe-swe-browser-backend` restart (chromium bump, config change, crash) used to orphan every live session's Agent View forever -- the backend's allocation table is in-memory, so the session's tunnel client reconnect-looped on `bad handshake` and the Playwright MCP got `Target page, context or browser has been closed` until the session ended. The tunnel client now classifies the failed WebSocket upgrade: a 404 from `/sessions/<id>/tunnel` means "backend up, allocation gone" (401/403/409 and network errors keep the plain retry loop) and hands off to re-allocation, which re-POSTs `/sessions` with capped backoff, retargets the running CDP reverse proxy atomically (the new allocation may land on a different slot; no listener churn, the agent's `--cdp-endpoint` keeps working), updates the VNC target, starts a fresh tunnel client, and broadcasts session status so the Browser tab recovers. Session teardown racing a re-allocation is guarded: the fresh allocation is freed if the session closed mid-flight, and `Stop()` cancels a re-allocation stuck waiting out a still-restarting backend. Direct (non-tunnel) remote mode has no reconnect loop to hook and is unchanged (see `tasks/2026-07-18-agent-view-remote-reallocation.md`).

### Features

- **Re-tapping the active Files tab returns to the workspace root**: Clicking the Files tab while it is already the active tab in its slot now navigates the pane back to the base directory (the session cwd). The md-serve iframe is cross-origin, so after browsing into subdirectories there was previously no way home short of reloading the whole page.

- **Compose deployments can offload Agent View to a browser-backend container**: The generated `docker-compose.yml` now forwards `SWE_AGENT_VIEW`, `SWE_AGENT_VIEW_TUNNEL`, and `SWE_BROWSER_BACKEND_TOKEN` from the host environment into the swe-swe container (same pattern as `SWE_TUNNEL_UNIQUE`; defaults to `local`, so existing setups are unchanged). The server resolves `-agent-view` from its process env at startup, so without the passthrough a compose deployment could not point Agent View at a `swe-swe/browser-backend` container. Typical same-host setup: run the backend standalone with `--restart=always` and ports published on the docker host-gateway IP (e.g. `172.17.0.1`) only -- containers reach it via `host.docker.internal`, the internet cannot -- then export `SWE_AGENT_VIEW=http://host.docker.internal:9333`, `SWE_AGENT_VIEW_TUNNEL=1`, and the shared token before `swe-swe up`.

- **Automatic chat-log archiving into the repo (`agent-chats/`)**: Chat sessions now default `AGENT_CHAT_EXPORT_DIR` to `{workDir}/agent-chats`, activating agent-chat 0.8.14's streaming export -- the conversation is written as reviewable markdown *as it happens* (screenshots and other attachments copied into `agent-chats/assets/` at event time, so they survive the transient upload dir), with `index.html` regenerated as a browsable archive landing page. Worktree sessions archive into their worktree, so merging a branch carries its conversation along with its code. Nothing is ever committed automatically. The default is presence-checked so user overrides win: opt out per workspace (`AGENT_CHAT_EXPORT_DIR=` empty line in `.swe-swe/env`), per session (new "Archive chat log into repo" checkbox in the new-session dialog, or the Settings env textarea), or mid-session (agent-chat's `chatlog_optout` tool); a custom path relocates the archive (must stay inside the session workDir). See docs/configuration.md.

- **Agent View reverse tunnel (`--agent-view-tunnel`) -- remote browser with ZERO inbound reachability**: Direct remote Agent View needs the browser box to reach the swe-swe host's ports (chromium's `--host-resolver-rules` redirects DNS, it does not tunnel). The new opt-in tunnel mode (`--agent-view-tunnel` / `SWE_AGENT_VIEW_TUNNEL=1`, only with `-agent-view=<url>`) reverses the trust direction to match swe-swe-tunnel: per session, swe-swe dials an outbound WebSocket to `<backend>/sessions/<id>/tunnel` (bearer-authed, 409 on a duplicate) and keeps a declarative port set synced (`{"op":"sync","ports":[...]}` -- full set, idempotent, resent on reconnect); the backend binds real listeners for those ports on ITS OWN loopback, so chromium there runs with NO resolver rules -- `localhost`/`*.lvh.me` resolve natively, the Host header arrives intact (vhost preview demux routes normally) -- and each accepted connection becomes one stream over the WS (4-byte stream-id framing, bounded 4MiB per-stream buffers so a slow stream is killed instead of head-of-line-blocking the tunnel), replayed against the swe-swe box's `127.0.0.1:<same port>`. Ports follow the session automatically: static (server port + session preview port), Procfile services (pre-bound via swe-run's deterministic assignment -- no discovery race), and a `/proc/net/tcp{,6}` mirror (~2s) that picks up ad-hoc `npm run dev`-style listeners and drops vanished ones (`SWE_AGENT_VIEW_TUNNEL_EXCLUDE_PORTS` overrides the default exclusion of swe-swe's internal pools). Backend bind rules: 127.0.0.1 only, own service + CDP/VNC ranges refused, first-bind-wins across sessions -- refusals are reported in `sync-result` and warned once per port, never silent. Accepted connections are peer-guarded on Linux (TCP has no SO_PEERCRED, so: `/proc/net/tcp` inode -> owning pid -> PPid ancestry against the session's browser process tree; fail-closed). Tunnel death never kills the session -- reconnect with capped backoff re-syncs everything; session teardown closes WS + listeners + streams. Direct mode is untouched (resolver-rule envs are ignored with a note in tunnel mode). Proven by new e2e tiers (`make test-e2e-agent-view-remote-tunnel[-image]`, tunnel tier added to `test-full-e2e`): the image tier renders a page served ONLY on the swe-swe side's network-namespace loopback while `SWE_AGENT_VIEW_LOCALHOST` points at a blackhole IP -- no inbound route exists, and it works anyway. Multi-tenant netns isolation is sketched in `tasks/TODO-agent-view-netns-multitenancy.md`.

- **`swe-npx` -- node-free spawning of swe-swe's own npm tools**: The four `@choonkeat/*` tools swe-swe spawns (`md-serve`, `agent-chat`, `agent-whiteboard`, `agent-reverse-proxy`) are static Go binaries published npm-style; a new stdlib-only `swe-npx` helper (bundled in the image and the dockerless payload, 9th embedded binary) resolves the platform package (`<pkg>-<os>-<arch>`) straight from the npm registry, verifies `dist.integrity` (sha512), caches the binary under the user-level `~/.swe-swe/npx-cache/` (shared across sessions/projects), and **execs** it -- no wrapper process, stdio/signals pass straight through, and the captured PID is the tool itself. `latest` resolution is memoized per package (`SWE_NPX_LATEST_TTL`, default 15m; registry-down falls back to the newest cached version with a stderr note), so a warm MCP spawn is ~11ms vs npx's ~1.1s. All `npx -y @choonkeat/...` call sites now use `swe-npx` (Files-tab md-serve, mcp-less proxy fleet, dockerless `.mcp.json`, per-agent MCP configs, pi mcp-bridge); `@playwright/mcp` stays on real npx. This makes 5 of 6 tabs genuinely node-free in dockerless mode (node remains only for Agent View and node-based agent CLIs) and structurally kills the npx cwd-collision bug: resolution is keyed by package name against the registry and never consults the project cwd, so a session working inside a checkout of e.g. the agent-chat repo can no longer shadow the published tool (previously that spawn died with `agent-chat: not found`). Overrides: `SWE_NPX_REGISTRY`, `SWE_NPX_CACHE_DIR`. Verified by a poisoned-PATH dockerless e2e (`E2E_POISON_NODE=1` in `scripts/e2e-dockerless.sh`) where all tabs come up with node/npx masked to `exit 127`.

- **App Preview host-demux (vhost apps)**: The App Preview tab can now reach compose vhosts like `app1.lvh.me:5000` that dispatch on the `Host` header, from a browser on a different machine than swe-swe. The per-session preview listener demuxes a browser-facing label `app1-5000.<reach>:<proxyPort>` to `127.0.0.1:5000` and rewrites the upstream `Host` to the logical `app1.lvh.me:5000` (so your traefik/nginx matches as on a laptop), rewriting `Set-Cookie Domain` logical->reach so shared auth still works. The reach is browser-probed (`probe-<rand>.<candidate>/__probe__` for the `X-Agent-Reverse-Proxy` header): wildcard mode when a wildcard domain resolves to swe-swe, else a visibly-degraded pinned mode (one vhost at a time via a per-session `vhost-pin` endpoint). `localhost`/`127.0.0.1` previews and every existing flow are unchanged. New envs `SWE_PREVIEW_VHOST_SUFFIX` (default `lvh.me`) and `SWE_PREVIEW_REACH_DOMAIN`. Built on `agent-reverse-proxy` v0.2.11's `ResolveTarget` + `CookieDomainRewrite` per-request hooks. See [ADR-0045](docs/adr/0045-preview-host-demux.md) and [docs/multi-service.md](docs/multi-service.md). Under a password, wildcard mode shares the login cookie across sub-app origins by pinning `Domain` to a *configured* reach (`SWE_PREVIEW_VHOST_SUFFIX` / `SWE_PREVIEW_REACH_DOMAIN`, default `lvh.me`); a browser-probed reach the server is not configured for (e.g. an auto-derived `<ip>.sslip.io`) stays host-only (set `SWE_PREVIEW_REACH_DOMAIN` to it, or use pinned mode).
- **`swe-run` -- a docker-free Procfile runner for multi-service apps**: The blessed path for apps that need more than one process (web + worker + database) is now a `Procfile` run by swe-swe's own supervisor, `swe-run`, installed on every session's PATH (built into the image and shipped in the dockerless payload). It reads a foreman-compatible `Procfile` (`name: command` per line, `#` comments, `sh -c` execution) and starts each service as an ordinary child in the session's process group -- so, unlike `docker compose`, **the services die with the session and nothing leaks onto the host** (no Docker socket, no root). Ports are assigned automatically and collision-free from the session base `PORT`: the primary service (`web`, or the first line, or `-primary <name>`) gets `PORT` so it shows in the Preview tab with zero config, and every other service gets a session-unique port from a free band, published to all services as `PORT_<NAME>` (host is always `localhost`). `.env` and `.swe-swe/env` are loaded with a defined precedence (runner-assigned ports always win). Output is multiplexed into the terminal with aligned, per-service color `name |` prefixes (NO_COLOR honored). Following foreman semantics, any service exiting triggers a graceful shutdown of the rest (SIGTERM -> grace -> SIGKILL to every process group) with the correct aggregate exit code, and `Ctrl-C` tears the whole stack down. Documented in `docs/multi-service.md` and the in-session `.swe-swe/docs/multi-service.md`; `docker.md` now leads with the Procfile path and flags that `--with-docker` mounts the host-root-equivalent Docker socket (ADR-0013). No auto-restart, health checks, or `depends_on` ordering in v1; `--with-docker` stays available for stacks that genuinely need container networking.

## v2.26.0 - MCP-less Mode, Dockerless Single-Binary, prctx PR/MR Review & Remote Agent View

### Features

- **MCP-less mode (every tool reached through a `mcp` CLI, no native MCP client)**: A complete alternative MCP transport for agents whose harness has no MCP client. A per-server `mcp-cli-proxy` daemon fronts exactly one stdio MCP server over a unix socket -- performing the `initialize` handshake once, id-multiplexing concurrent socket clients onto the child's single newline-delimited stdio (so a blocking `send_message` never head-of-line-blocks other calls), emitting `notifications/cancelled` on client disconnect, and restarting the child on exit (exponential backoff + crash-loop cap, socket never moves) with every child exit logged. `swe-swe-server` launches and reaps a per-session proxy fleet (agent-chat gated on chat mode) in the session's workDir, and each agent's native MCP-config write is runtime-guarded behind `SWE_MCP_LESS` so the fleet is the single agent-agnostic MCP surface (no double-bound `AGENT_CHAT_PORT`). The short-lived `mcp` client discovers servers by listing the socket directory (the registry IS the filesystem), synthesizes typed flags from each tool's `inputSchema`, and renders results (text -> stdout, image/audio -> file path, `structuredContent` -> JSON) with no client-side read timeout so blocking tools work. `mcp -h` / `mcp <server>` dump every tool's full multi-line docs, defaults, annotations (`[read-only]`/`[destructive]`), and any unmodeled schema keywords as raw JSON -- everything a native client would inject -- printed docs-first so an agent reading top-down lands on the orientation last; a throttled post-call `<mcp>tip: ... refresh: mcp <server> <tool> -h</mcp>` reminder (per `(server,tool)`, default 30m, tunable via `--remind-help-text-throttle`) survives context compaction. A generic client-side blocking-call notice writes an immediate `notifications/message` frame on `tools/call` for declared `--blocking-tools` (agent-chat's `send_message` / `send_verbal_reply`) so an early read of a still-pending blocking call is never mistaken for "the user did not reply". **Native MCP remains the default; `swe-swe init --without-mcp` opts into MCP-less mode.**
- **Dockerless single-binary distribution (`swe-swe init --dockerless && swe-swe up`)**: Run every swe-swe tab with no Docker. The six static-linux helper binaries (`swe-swe-server`, `git-credential-swe-swe`, `git-sign-swe-swe`, `mcp-lazy-init`, `swe-swe-broker-probe`, `swe-swe-fork-convo`) are `go:embed`ded into the CLI (~29MB payload) via `make dockerless-payload`; `init --dockerless` extracts them to the metadata dir with a mode marker and a project-scoped `<project>/.mcp.json` (the five swe-swe servers, no global `~/.claude.json` pollution) and skips all Dockerfile/compose generation. `swe-swe up`/`down` detect a dockerless project and foreground-exec the dumped host server (project workdir, loopback bind, `bin/` on PATH, `--open` polls readiness then opens a browser). `swe-swe-server` is made path-agnostic for host runs, and cross-compiles for `darwin/{arm64,amd64}` via build-tag splits (`peercred_*`, `subreaper_*`) that fail-open off Linux -- keeping the Linux build byte-identical. Claude hook guards install for dockerless too via a project-scoped `.claude/settings.local.json`. As part of the slimming, VS Code / code-server / nginx-vscode were removed from the templates, Go layer, and frontend.
- **`prctx` -- PR/MR review CLI (bundled in every session)**: A provider-agnostic helper to pull a GitHub PR or GitLab MR into local state, render it for an agent, and flush replies/comments/verdicts back upstream. Read path fetches via GitHub GraphQL `reviewThreads` (accurate `isResolved` + thread node ids) or GitLab discussions, storing state under `$XDG_STATE_HOME/prctx` (outside the worktree, with idempotency stamps); the diff stays in the worktree via git. Write path stages `reply`/`comment`/`resolve`/`drop` locally then `flush` posts them atomically per-action (idempotent via `posted_id` stamps), refusing (unless `--force`) when local HEAD differs from the fetched PR head since the CLI never pushes; `approve`/`reject` are separate atomic submissions. GitLab MRs map onto the same model (inline discussions carry a position; `reject` is unapprove since there is no portable REQUEST_CHANGES). `prctx` is built into the image and installed on PATH, driven conversationally by the new `/swe-swe:pr` slash command; it authenticates via `GH_TOKEN`/`GITLAB_TOKEN`, which `swe-swe-server` now derives from the stored per-host Git HTTPS credential and exports to every new session (`github.com` -> `GH_TOKEN`, `gitlab.*` -> `GITLAB_TOKEN`, nothing written to disk).
- **Remote Agent View backend (off-host browser display stack)**: `-agent-view=local|off|<url>` (`SWE_AGENT_VIEW`) with host capability detection -- a lean host with no Xvfb/chromium/x11vnc/websockify reports Agent View unavailable (`browser/start` returns `{"status":"unavailable"}`, WS status carries `agentViewAvailable`) so its tab hides while the other five tabs work. The four-process orchestration is extracted into a `browser-backend` service (`swe-swe-server -mode browser-backend`) exposing a bearer-authed session-allocation API with a max-sessions cap; when `-agent-view` is a backend URL, the client allocates a remote browser and makes it look local -- a CDP reverse proxy on the session's `CDPPort` rewrites `/json*` debugger hosts back to `localhost` (so the Playwright MCP follows them) and the VNC proxy targets the remote websockify. The remote chromium's `localhost` (and wildcard loopback dev domains -- `lvh.me`, `localtest.me`, `*.localhost`, tunable via `SWE_AGENT_VIEW_LOOPBACK_DOMAINS` / `loopbackDomains`) resolves back to the swe-swe host via `--host-resolver-rules`, so watching the agent browse an off-host dev server works. A thin `swe-swe/browser-backend` image + build target ships the service.
- **Per-repo environment variables (Settings > Repo)**: A `KEY=VALUE` blob stored in server memory (never on disk, never committed), keyed per session and injected into newly opened sessions via `buildSessionEnv`. Values auto-sync to trusted browsers over TLS using the existing `(origin, init_sha)` trust gate matched by repo -- one decision now covers PAT, signing key, and env vars. Reserved keys (`PATH`, `GH_TOKEN`, `GIT_CONFIG_*`, ports, ...) are dropped and reported to the UI, and the checked-in `.swe-swe/env` file wins collisions. Env is materialized at spawn (a save applies to the next new session), and both `create_session` children and forks inherit the repo's env -- forks deliver the blob from the browser (locating it by `(origin, init_sha)` in localStorage) since an ended source session's store is often already wiped.
- **`set_session_name` MCP tool + reboot-safe teardown signals**: Agents can now name their own session (or, for an orchestrating parent, a child by uuid) via the `set_session_name` orchestration tool -- identified securely by its per-session MCP key -- ending the wall of identical `owner/repo@main` default names; the bundled `/swe-swe:session-title-set` command composes `{short title} {owner}/{repo}@{branch}`. `list_sessions` gains `busy` (tri-state, a best-effort reuse of the `/api/fork` ACTIVE-tail guard; a session parked on a blocking `send_message` reports `busy=false`, the designed safe point) and `recordingUUID` so a reboot driver can post `/api/fork/<uuid>` resume links to chat before `compose down`.
- **Instant New-Session dialog**: The dialog no longer blocks on a synchronous `git fetch --all` -- `/api/repo/prepare` returns in milliseconds (`hasRemote` only), the dialog lists local branches instantly and refreshes remote refs in the background (`/api/repo/branches?fetch=1`, soft-failing to cached branches), and a recording's "+ New" opens a prefilled dialog carrying pwd/name/branch/extra-args through the staged-intent path.
- **Agent-chat turn guards (Stop + AskUserQuestion)**: Two PreToolUse/Stop hooks baked into the swe-swe-provisioned `~/.claude/settings.json` (idempotent `jq` merge) protect web-UI sessions. The Stop guard blocks the first silent turn-end (a turn with only plain response text is invisible to the user and reads as a crash), exempting sessions that already sent `send_message`/`send_progress`/`send_verbal_*`/`draw` or whose `check_messages` found an empty queue, with `stop_hook_active` preventing any loop; the AskUserQuestion guard denies the built-in multiple-choice tool (whose menu renders only in the local TUI) and steers the agent to `send_message`. Both exempt non-chat sessions via `AGENT_CHAT_DISABLE=1`.
- **Login/auth hardening + per-session MCP keys**: The login rate limiter keys on the transport peer by default (honoring `X-Forwarded-For` only under `SWE_TRUST_FORWARDED_FOR=true`) plus a global failed-attempt ceiling no per-key trick can dodge; WebSocket upgrades enforce a `CheckOrigin` allow-list (same-host, tunnel apex, `{port}.{apex}` subdomains) closing Cross-Site WebSocket Hijacking; and the open-redirect on login is fixed. Each session is issued its own random MCP auth key (injected as `MCP_AUTH_KEY`), and `/mcp` resolves the caller session from it -- so `create_session` inherits the *calling* session's git credentials, author identity, and SSH signing key (a spawned session can sign/push immediately) without the old shared-global-key harvesting risk. The preview-proxy open endpoint and other non-browser routes authenticate via that per-session key rather than the browser cookie.

### Bug Fixes

- **Chromium pinned to a known-good 147**: An unpinned `apt-get install chromium` tracked bookworm-security and shipped chromium 150, whose zygote SIGTRAPs on launch and kills every browser feature (MCP browser, VNC, preview). Both image Dockerfiles now pin `147.0.7727.137-1~deb12u1` from the permanently-available base-distro pocket; the e2e harness also auto-selects a launchable chromium and documents the system-chromium SIGTRAP.
- **Repo env vars delivered before the session spawns**: `inheritSessionEnv` ran after `buildSessionEnv` had already frozen `cmd.Env`, and the browser new-session flow relied on a `set_env` WS frame the PTY spawned before reading -- so saved env vars arrived one session too late. The child's env store is now populated pre-spawn and the blob rides the new-session request.
- **MCP union-type schema flags**: Real MCP tool schemas type nullable arrays as a union (`["null","array"]`), which the `mcp` client silently failed to unmarshal, defaulting the flag to string and forwarding the value uncoerced (server-side schema validation then rejected e.g. `--more_quick_replies` / `--image_urls`). A union-aware type resolver skips `"null"` to find the real type across coercion, bare-boolean detection, and help.
- **Files tab md-serve readiness**: the Files iframe now probes md-serve readiness (retrying its npx cold-start) before loading, instead of racing a not-yet-bound port.
- **Files tab follows the swe-swe light/dark theme**: the per-session md-serve is launched with `-theme-cookie swe-swe-theme`, so the Files tab honors swe-swe's own theme toggle (via the `swe-swe-theme` cookie the reverse proxy forwards) instead of following the browser's `prefers-color-scheme`. Requires md-serve >= 0.6.0, pulled automatically via the `@latest` launch pin. In tunnel mode the theme cookie is scoped to the parent domain (`Domain=publicHostname`, matching the auth cookie) so it reaches the `{port}.{publicHostname}` Files subdomain; host-only in local mode.
- **New-session staging fixes**: full dialog wiring (pwd/name/branch/extra-args) is staged for new sessions, chat mode is preserved through a new-session staging override, and a recording's "+ New" recovers the recording's checkout branch instead of losing it.
- **MCP-less session resume/fork**: `swe-swe-fork-convo` resume works for MCP-less sessions (agent-chat reached via the Bash `mcp` CLI), and the preview proxy key-exempts `/preview/mcp` so the headless proxy can list/call instantly instead of being 302'd to login.
- **Host-only login cookie + startup/shutdown forensics**: the localhost login cookie is host-only under `--tunnel-local-ports`, and the server now logs its parent process and the named signal at startup/shutdown for exit-0 crash forensics. `poppler-utils` was added to the base image.

## v2.25.0 - Credential Auto-Wire, Worktree Anchoring, Resume/Fork & No-Ghost Sessions

### Features

- **Credential & signing auto-wire (connect-time state, no manual Save)**: The Settings panel now reflects true server-side credential/signing state the moment a browser connects, and a trusted browser re-establishes its HTTPS PAT, author identity, and signing key without a manual Save. On connect, `swe-swe-server` snapshots the three credential stores under a single mutex and pushes a secret-free `session_cred_state` (hosts, fingerprint, override text, verdict) to the connecting client, re-broadcasting after every `set_credentials` / `set_signing_key` so co-viewers never go stale. The Git HTTPS **Host** field autofills from the workdir's `origin` remote (URL or scp-style) instead of hardcoding `github.com`, so a GitLab user's stored creds apply without switching Host; on a trusted `(origin, init-commit-SHA)` browser the PAT + bound signing key are auto-sent as one combined `set_credentials`. The `allowedSignersFile` principal now falls back to the workdir's effective git email (`git config user.email`, local-then-global) when no session author email is set, keeping the signature principal and committer in lockstep so `git log --show-signature` / `git verify-commit` pass for repos that never hit Save. The session gitconfig + `<sid>.allowed_signers` pair is written atomically under a per-sid lock (tmp + `os.Rename`, signers before the referencing gitconfig) so a concurrent agent `git` can never read a torn pair. **Test connection** is GitLab-aware (probes `{host}/api/v4/user` with a `PRIVATE-TOKEN` header for non-GitHub hosts, falling back to the generic GET) and a new `verify_stored_signing_key` op signs a test payload with the in-memory key so an auto-restored (empty-form) key verifies instead of reporting "Paste a private key first"
- **`swe-swe:merge-worktree` distributed to all deployments**: Previously an untracked, gitignored loose file in this workspace only (and hardcoding `/workspace` in three places), it now ships in the embedded slash-command bundle (md + toml) with paths derived via `git rev-parse --show-toplevel`, so it merges the correct repo from any checkout. `AskUserQuestion` is replaced with a harness-agnostic numbered-options prompt so it works in the chat TUI
- **`swe-swe:fixup-upgrade` reconcile command**: An interactive, agent-driven reconciliation for projection drift after an upgrade. It treats the canonical store (`~/.swe-swe/commands/md/swe-swe`) as the expected set, classifies every projection and project-level command as IN SYNC / DROP / SALVAGE? / DRIFT, and asks before deleting or relinking -- surfacing the cleanup that `init` previously did silently, and offering to salvage files that look intentional
- **execute-in-worktree wires agent chat**: The spawned worktree session now always passes `--channels server:agent-chat` (so chat is wired to the UI, not merely tool-present) and is directed to use `send_message`/`send_progress` rather than the unwatched TUI. The command also commits the task file before spawning, since a fresh worktree only contains committed content. (No chat-export wrap-up: the streaming `agent-chats/` archive makes it redundant, and swe-swe prompts stay neutral on whether chat logs are committed)
- **`swe-swe:commit-session-chat-log` command**: A small opt-in primitive for repos that keep chat logs in git: it titles the current session's `agent-chats/` log (`set_chat_title`), redacts sensitive values (checking referenced screenshots too), and commits the log together with its assets in a standalone commit -- staged by explicit path, never pushed. swe-swe itself never instructs agents to commit (or not commit) `agent-chats/` -- whether a repo tracks chat logs is the repo owner's policy, so existing installs see no new behavior unless they invoke the command
- **Resume & branch conversations (two-step, side-effect-free fork)**: Ended chat recordings (claude/codex with a chat event log) and live sessions now expose **Resume**/**fork** affordances that branch a conversation into a fresh session with its chat history restored and the agent reattached. Forking is a deliberate two-step: `GET /api/fork/<uuid>` renders a skeleton confirm page with **zero side effects** -- so a browser prefetch, link unfurl, refresh, or back-button can no longer fork (the old GET minted a session and rollout file on sight) -- and only the modal's confirm-button **POST** materializes the fork and redirects. agent-chat gains a **per-bubble fork** button (passed `fork_session`; opens `/api/fork/<id>?bubble=<seq>&mode=after`) to branch at any point in the transcript, and an embedded agent-chat iframe now receives the top-level `parent_url` so relative markdown links/images -- and the fork links themselves -- resolve against the page the user sees rather than the iframe's own origin. Two bundled slash commands ship in the embedded source so they survive `init` re-seeding: `swe-swe:recordings-list-orphaned` (ended resumable recordings plus live sessions, each with a fork link) and `swe-swe:recordings-resume` (reply with a click-to-resume link, `create_session` fallback for non-claude/codex agents)
- **No ghost sessions -- session creation requires a staged intent**: A bare navigation, WebSocket reconnect, prefetch, or stale/bookmarked `/session/<uuid>` tab no longer silently spawns an empty "ghost" session. Creation now flows through an explicit staged intent -- the New-session dialog and a recording's "+ New" both **POST** to `/api/session/new`, and fork POSTs after its confirm page; each stages an intent that the WS handler consumes to materialize the session. The gate (`getOrCreateSession(..., allowCreate)`) materializes a UUID only when it is already live, has a staged intent, or is a child shell of a live parent; otherwise it returns `session_gone` and the page renders a **"This session has ended"** screen with **Resume** (when the conversation is forkable) and **New session** actions, instead of a blank or ghost terminal. The live-check and create happen under a single lock (no create-race when two clients hit the same fresh UUID), staged intents carry a 10-minute TTL swept by a background reaper that also removes an abandoned fork's orphaned rollout file, and MCP `create_session` still creates eagerly (`allowCreate=true`). Closes both the side-effecting-GET fork footgun and the stale-tab ghost-session class of bugs

### Bug Fixes

- **Worktrees anchored off the main repo**: `resolveWorkingDirectory` special-cased only `/workspace`, so launching a session from a checkout that is itself a worktree doubled the path segment (`/worktrees/worktrees/<branch>`). Worktrees now anchor off the MAIN repo with per-repo namespacing (`/workspace` and any `/worktrees/<x>` -> `/worktrees/<branch>`; `/repos/{name}/...` -> `/repos/{name}/worktrees/<branch>`). `buildExitMessage` and `listWorktrees` are made external-repo aware so exit payloads and the `/api/worktrees` endpoint / `list_worktrees` MCP tool no longer drop worktrees living under `/repos/{name}/worktrees`
- **Forked session no longer auto-runs the source's pending directive**: Forking a chat-mode session at the last reply left the resumed agent in a PENDING-ACTION state -- the original next directive was baked into the source's `send_message` tool_result and the resumed agent would execute it autonomously (observed: a fork re-running its own fork test, recursive `/api/fork` and all). The fork now anchors at the assistant `tool_use` (B1) with an ACTIVE-tail guard; `claude --resume` accepts a tail whose `tool_use` has no matching `tool_result` and processes a fresh prompt cleanly
- **Slash commands missing through symlinked namespace dirs**: `discoverSlashCommands` used `entry.IsDir()`, false for a symlink-to-dir, so the canonical swe-swe namespace (shipped as a symlink `~/.claude/commands/swe-swe -> ~/.swe-swe/commands/md/swe-swe`) was silently skipped -- `swe-swe:*` commands returned "No results" in autocomplete for every repo reaching them through the system store. Resolved with `os.Stat` (follows the link), matching `discoverSkills`
- **Stale swe-swe command dirs migrated to symlinks**: `ensureRelativeSymlink` bailed on any existing real directory, freezing legacy pre-symlink installs so shipped slash-command fixes became shelf-ware in every existing home. A real dir is now migrated to a symlink when it is recognizably swe-swe-owned (flat + a README.adoc marker or a current bundled-command filename); stale leftovers are dropped (logged), user-owned dirs are preserved, and both branches log
- **md-serve (Files tab) process-group cleanup**: launching md-serve via `npx -y @latest` meant the captured PID was the npx wrapper, so `stopSessionMdServe` SIGKILLed only the wrapper and left md-serve orphaned and still bound to its `FilesPort` (breaking the next session's bind). md-serve now shares a process group (`Setpgid`) with npx and is killed by negative PID, matching the agent-process pattern
- **execute-in-worktree task delivered as one input**: the directive and the `/swe-swe:execute-step-by-step` command were sent as two separate `send_session_input` messages -- the directive started its own turn and the agent finished with "no pending instruction" while the real command sat queued as a draft that never auto-submitted. They are now sent as a single input with the slash command first (so the TUI expands it) and the directive riding along as trailing `$ARGUMENTS`
- **Fork/resume on workdirs containing a dot**: Claude Code encodes its `~/.claude/projects/<dir>` rollout folder by replacing both `/` and `.` with `-`, but we only replaced `/`, so any workdir containing a dot (notably `github.com-...` repo paths) produced a folder name that never matched the real one -- `/api/fork` could not locate the rollout and fork/resume failed for those sessions. Encoding is centralized in `encodeClaudeProjectDir()` and used at all three lookup sites (`agentSessionDir`, `agentSessionFilePath`, `findLatestClaudeSessionInWorkDir`); regression test added

## v2.24.0 - SSH Commit Signing, Skills from Git Repos & Tunnel mTLS

### Features

- **SSH commit signing (per-session; key never touches disk)**: Sign commits made in a session with an ed25519 SSH key held only in `swe-swe-server` memory. A `sign-ssh` credential-broker op produces SSHSIG armored signatures, and the `git-sign-swe-swe` wrapper (slotted into git's `gpg.ssh.program`) dials the broker so `git commit -S` signs without the key ever hitting disk; `-Y verify` forwards to real `ssh-keygen` so `git log --show-signature` / `git verify-commit` keep working. The per-session gitconfig emits the full `gpg.format=ssh` signing block plus a generated `allowedSignersFile` (when an author email is set) so signatures verify locally, not just on the forge. Browser auto-restore binds a stored key to an `(origin, init-commit-SHA)` trust tuple with a 90-day TTL, gated to TLS/loopback, with a "Forget on this device" control -- the PEM lives only in the browser, never on the server, and the passphrase is never persisted. A warning banner flags when a workdir's local `.git/config` (`gpg.format`, `commit.gpgsign`, ...) would silently override the session signing setup
- **Session Settings redesign (sidebar tabs)**: The Settings modal becomes a wider sidebar-nav layout with five focused panes -- Profile, Appearance, Git HTTPS, SSH Signing, and a footer End-session link behind a confirm popover. Profile/Appearance gain explicit Save/Revert (Appearance still previews live); a "Saved" badge surfaces stored Git HTTPS credentials at a glance. A new **Test connection** button validates a PAT before saving (GitHub via `api.github.com/user`, generic `GET https://{host}/` otherwise) so a mistyped or revoked token is caught without committing it to the broker. Separate `set_signing_key` / `verify_signing_key` WS handlers keep the SSH flow from clobbering the HTTPS PAT bag. Mobile collapses the sidebar to a horizontal pill scroller
- **Pi MCP bridge**: A `mcp-bridge.ts` Pi extension, installed globally by the entrypoint (project-local `.pi/extensions/` still overrides), registers the `swe-swe`, `swe-swe-agent-chat`, `swe-swe-whiteboard`, and `swe-swe-playwright` MCP servers as Pi tools at session start -- so Pi sessions get the same MCP surface as the other agents
- **Per-tab popout gesture**: Middle-click or Ctrl/Cmd+click a tab whose pane resolves to a shareable URL (preview, browser, VS Code, agent-chat) to open it in a new browser tab, with a dotted-underline hover affordance and a platform-aware tooltip. Replaces the per-pane popout buttons; on mobile, an adjacent button pops out the currently-selected pane
- **`--tunnel-client-cert` (tunnel mTLS)**: Present a client certificate for mutual-TLS tunnel authentication. `--tunnel-client-cert <host-path>` at init read-only bind-mounts the cert to `/home/app/.swe-swe-tunnel/client.crt` and sets `SWE_TUNNEL_CLIENT_CERT`, which `swe-swe-server` forwards to the `swe-swe-tunnel` child as `--client-cert`. Needed when the tunnel daemon runs with `--mtls-ca`: the agent presents the cert and the daemon verifies its embedded pubkey matches the Register pubkey (defence-in-depth). The matching private key reuses the agent's existing `identity.key`, so no new key file is introduced. Empty by default, leaving non-mTLS tunnels unaffected. Documented in `tunnel-explained.md`
- **`--tunnel-local-ports` (tunnel mode)**: Optionally publish a tunnel-mode container's in-container listeners on the host's `127.0.0.1`, so the machine running `swe-swe up` can reach the containers directly instead of only through the tunnel. Widens the `swe-swe-server` bind from `127.0.0.1:${SWE_PORT}` to all interfaces (via a new `{{TUNNEL_BIND}}` placeholder driving both the `-bind` flag and `SWE_BIND`) -- required because Docker publishes ports to the container's `eth0`, not its loopback -- and adds a compose `ports:` block publishing `SWE_PORT` plus the preview / agent-chat / VNC-proxy / public ranges on host loopback only. No exposure beyond the tunnel (the per-session proxy listeners already self-authenticate); a local-only convenience with no benefit on PaaS/Fly. Documented in `tunnel-explained.md` and the tunnel-laptop runbook

- **`swe-swe init --with-skills <alias>@<url>`**: Bake external skill repos into a container so every `SKILL.md` is exposed to the agent through autocomplete. The Dockerfile runs `git clone --depth 1 <url> /tmp/skills/<alias>` at build time; on first boot the entrypoint copies the clone into `~/.swe-swe/skills-src/<alias>/` (and `git pull`s it on later boots), then symlinks each `SKILL.md`'s parent directory into the canonical store `~/.swe-swe/skills/<alias>-<skill>`. Skills are agent-agnostic -- they live only under `~/.swe-swe/skills/` and surface to Claude, Codex, Gemini, OpenCode, Aider, Goose, and Pi alike. Handles arbitrarily nested repo layouts (e.g. mattpocock's `skills/engineering/<name>/`)
- **Skills in autocomplete (every session)**: `/api/autocomplete` now discovers skills regardless of assistant, scanning project-level dirs (`<workDir>/.swe-swe/skills`, then `.claude`/`.codex`/`.gemini`/`.opencode`/`.pi` skills dirs) then system-level dirs (`~/.swe-swe/skills` canonical store, then the same per-agent dirs under `$HOME`). Hints are prefixed `[skill]` and truncated to the first sentence to fit a single-line pill; the early-exit for assistants without a slash-command convention is dropped so skills surface even for a bare shell agent
- **Collision-free flatten-with-prefix**: the autocomplete handle is the flattened store directory name (`<alias>-<skill>`) rather than the SKILL.md frontmatter `name:`. Because the store is one flat directory of uniquely-named entries, every installed skill is distinct by construction -- two repos that both ship `grill-me` surface as `/<alias-a>-grill-me` and `/<alias-b>-grill-me` instead of one silently shadowing the other. Project-level skills still override system skills of the same name (project-wins). Within one repo, a leaf-name clash across folders installs the second under its repo-relative path with a `[warn]` instead of an `ln -sfn` silent overwrite; the entrypoint clears stale `<alias>-*` links before re-linking and sorts `find` output so the short-name winner is deterministic

### Bug Fixes

- **Symlinked skills dropped from autocomplete**: `discoverSkills` used `!entry.IsDir()`, which is true for a symlink-to-directory (`os.ReadDir` reports `IsDir()==false` for symlinks), so the `--with-skills` store -- which installs every skill as an `ln -sfn` symlink -- surfaced **zero** skills in a real deploy. Fixed by using `os.Stat` (follows the link); verified against a real clone of `github.com/mattpocock/skills` where 28 nested skills went from 0 discovered to 28. Golden + unit fixtures had only ever built real dirs (`MkdirAll`), never the symlink shape production uses, which is why it shipped
- **`git worktree add` on git-LFS repos**: the container now installs `git-lfs`, so worktree creation no longer fails on LFS-backed repositories
- **Settings credentials crash with a local user identity**: the credentials section no longer crashes when the repo has a local `user` identity in `.git/config`
- **New-session field-edit guards**: collapse the Agent / Extra-flags / Start fields while the branch combo is open and on focus, so Start can't be clicked with uncommitted branch text; worktree-creation errors now surface instead of silently falling back
- **Portable copied `.ssh/config`**: init sanitizes a copied `.ssh/config` for Linux portability
- **Pi slash-command projections**: project-level slash commands now project correctly for Pi sessions

## v2.23.0 - Tunnel Mode, Preset-Grid UI, Tailscale PaaS & Pi Agent

### Features

- **Tunnel mode (`swe-swe init --tunnel-server-url=...`)**: New deploy path that lets a swe-swe container be reached from the public internet without owning an IP, opening ports, or provisioning TLS. The container dials a `swe-swe-tunnel` server outbound (Ed25519 pubkey auth); the tunnel server fronts subdomain-routed traffic over the same connection. Works on residential networks, Fly.io, Railway, Render, Cloud Run -- anywhere with outbound HTTPS. See `docs/tunnel-explained.md` (concepts), `docs/tunnel-laptop.md` (laptop runbook), `docs/tunnel-fly.md` (Fly runbook)
- **Tunnel-mode subprocess supervisor**: `swe-swe-server` exec's the `swe-swe-tunnel` client as a child process and reads structured lifecycle events (`register_ok`, `disconnect`, `fatal`, `retry-after`) off its stdout. Live `publicHostname` propagates to the WebSocket broadcast and frontend in real time, including `retryAfterMs` for backoff and `kind=fatal` to halt the supervisor instead of restart-looping (ADR-0042)
- **Tunnel subdomain iframes**: In tunnel mode, preview / Agent View / VNC iframes route via per-session subdomains through a single auth-proxy port — replacing per-port Traefik labels and Let's Encrypt certs (which the tunnel server fronts instead). Path-based probes still resolve before iframe mount
- **Tunnel-aware landing page**: Landing page shows tunnel state and a click-through to the live URL on `register_ok`
- **`--bind` / `SWE_BIND` flag**: Restrict the in-container listener to localhost in tunnel mode so nothing on the host network can reach swe-swe directly
- **`SWE_TUNNEL_IDENTITY_KEY` env var**: Deliver the Ed25519 keypair as a PaaS secret instead of mounting a persistent volume
- **Tailscale single-container PaaS deploy** (`swe-swe up`): `tailscaled` is baked into the image and dormant unless `TS_AUTHKEY` is set. When present, swe-swe-server spawns `tailscaled --tun=userspace-networking`, joins the tailnet, and binds the swe-swe UI Tailscale-only. The PaaS public `$PORT` exposes only a placeholder landing page + `/health`. New `--tailscale-*` flags (`TS_AUTHKEY` / `TS_HOSTNAME` / `TS_STATE_DIR` / `TS_DISABLE`)
- **`pi` agent backend** (`@mariozechner/pi-coding-agent`): Wired alongside Claude, Codex, Gemini, OpenCode, Aider, and Goose. Bundled swe-swe slash commands install into `~/.pi/agent/prompts/`, autocomplete plumbed for system + project (`.pi/prompts/`), `pi --continue` for session resume
- **Terminal UI preset grid**: 8 layout presets with per-slot multi-tab model. Each slot owns its own tab bar, with `+` replace-menu + loading/unavailable states, drag-resizable gutters that snap at 50% and device widths, per-preset persistence in localStorage. Auto-homes Agent Chat + Agent View into preset slots; mobile falls back to Agent Terminal during chat probe
- **Files tab (per-session `md-serve` read-only repo browser)**: Each session spawns its own `md-serve` (`@choonkeat/md-serve`) rooted at the session's working directory, surfaced as a new "Files" pane reachable from a slot's `+` menu. md-serve renders Markdown as GitHub-styled HTML, syntax-highlights source with linkable line numbers, lists directories, and live-reloads on mtime change -- a read-only view that stays current as the agent edits files, distinct from the Code (code-server) tab. It is exposed through a new per-session proxy-port band: the files port is `previewPort+6000` (range 9000-9019), wrapped by an auth-checked reverse proxy at the existing `proxyPortOffset` (default band 29000-29019). Cross-origin only in both modes (local `localhost:{filesProxyPort}`, tunnel `{filesProxyPort}.{publicHostname}`) because md-serve emits root-relative links. Read-only by design and behind the same login cookie as every other tab
- **Per-session git credential broker**: `git-credential-swe-swe` helper + per-session `GIT_CONFIG_GLOBAL` injection routes git auth through a per-session credential broker socket. Author Name/Email wire through the credentials UI, with readonly fields when local `.git/config` overrides. Helper refuses invocation outside git
- **`/swe-swe:setup` slash-command redesign**: Streamlined flow for git identity + auth + dev-server + env-var setup
- **`/run-md-serve` slash-command**: Spawns `npx @choonkeat/md-serve` for previewing markdown docs
- **Theme color resolves before session creation**: Session page now resolves sticky repo color via `WorkDir` instead of waiting on a created session
- **Agent View pop-out button**: New "open in new tab" button on the Agent View pane (top-right of the noVNC toolbar, replacing the removed Send CtrlAltDel button). Mirrors the existing Preview-pane affordance; pop-out URL is wired per pane via a `popoutUrlForPane` map so other iframe panes can opt in

### Bug Fixes

- **Per-port proxy auth bypass (security)**: Per-port proxies in tunnel mode now require the auth cookie before forwarding; added VNC reverse-proxy under the same auth wrap. Slot dedup prevents duplicate panes across slots on layout load
- **Cookie secure flag respects `X-Forwarded-Proto`**: Auth middleware lets `X-Forwarded-Proto: https` override `SWE_COOKIE_SECURE` per request, so PaaS edge HTTPS works without forcing the SSL compose template. Dropped `SWE_COOKIE_SECURE=true` from the SSL template
- **Internal server port rename**: `swe-swe-server` internal port renamed from hardcoded `9898` to `SWE_PORT` (default `1977`) so it doesn't clash on hosts where 9898 is in use
- **Agent Chat spinner during probe**: Spinner animates during non-chat-session probe so users see activity instead of a frozen tab
- **Stale-state cleanups on terminal-ui boot**: Override stale `active:'agent-chat'` to agent-terminal; clear stale inline styles that left Agent Terminal blank; mobile viewport no longer blanks after preset-grid rewrite; session-driven pane auto-opens and probe-success flips no longer persist across sessions
- **Tailscale state dir writable**: Compose shim creates a writable tailscale state dir and forwards `TS_*` env to the container
- **`tunnel-down-manual` leaves caller in valid CWD**: Script no longer cd's into the deleted compose dir before exiting
- **Auto-redirect to home on session end**
- **Auto-upgrade docs**: Documented `SWE_SWE_AUTO_UPGRADE` and `NODE_EXTRA_CA_CERTS_BUNDLE` env vars

### Refactoring

- **Drop dead per-agent recording fields** in `Session`
- **Parameterize tunnel "OPEN AT URL" port** via supervisor `LocalAddr` instead of hardcoding
- **Drop `--public-hostname` / `--tunnel-state-file`**: Replaced by the subprocess event stream — the file-based IPC was a one-shot read at boot with order-dependent failure modes (see ADR-0042)

### Internal

- Pre-commit hook to keep `.swe-swe/env` values out of commits
- E2E hardcodes all `SWE_*_PORTS` in `override.yml` + widens ranges to 30 to reduce port-collision flakes
- `SWE_SWE_TUNNEL_REF` build-arg pin bumped through `9984c43a6059` → `751dd1cbdc42` → `77af59b37ef5`
- Test coverage: per-port proxy auth wrap, VNC reverse proxy, slot dedup, tunnel-mode `getPreviewBaseUrl` + env passthrough, credential broker round-trip, Files-tab e2e (slot `+` menu adds the pane, iframe loads md-serve) + files proxy-port band assertion

### Documentation

- ADR-0042: Tunnel-mode subprocess supervisor
- `docs/tunnel-explained.md` (concepts + troubleshooting), `docs/tunnel-laptop.md` (laptop runbook), `docs/tunnel-fly.md` (Fly runbook)
- `tasks/2026-04-29-tunnel-subprocess-pivot.md`: Subprocess pivot rationale + 2026-04-30 fatal/retry-after follow-up
- `docs/dev/how-to-restart.md`: End other sessions before compose down

## v2.21.2 - `.swe-swe/env` $VAR Expansion Fix

### Bug Fixes

- **`.swe-swe/env` `PATH=/x:$PATH` broke MCP servers**: `loadEnvFile` now expands `$VAR` references against the session env being built, not against the server's env. Previously, a line like `PATH=/usr/local/go/bin:$PATH` in `.swe-swe/env` would expand `$PATH` against the server's PATH (which lacks the swe-swe bin prefixes), silently dropping `/home/app/.swe-swe/bin` from the session PATH. `agent-chat`, `agent-whiteboard`, and related MCP servers then resolved to wrong binaries or failed to start, leaving Agent Chat stuck in "Loading" state

## v2.21.1 - copyDir Skips Sockets

### Bug Fixes

- **`--copy-home-paths` init crash**: `copyDir` no longer aborts with `"operation not supported on socket"` when it encounters Unix domain sockets (e.g. `~/.ssh/agent/*.agent.*` from macOS SSH agents). Sockets, FIFOs, and device nodes are now skipped with a warning; symlinks are preserved as symlinks instead of dereferenced

## v2.21.0 - Global Proxy, Zombie Fix & Workspace Cleanup

### Features

- **Global-tier proxy (`swe-swe proxy --global`)**: Proxy commands are now available in a global tier (`$HOME/.swe-swe/proxy/`) visible across every project's container, in addition to the existing per-project tier. Project-tier overrides global-tier by PATH order
- **Extra CLI flags per session**: Sessions can now receive additional CLI flags (e.g. `--channels server:agent-chat`) via `extra_args` query parameter or MCP `create_session` tool
- **UTF-8 locale in containers**: Containers now set `LANG=C.UTF-8` and `LC_ALL=C.UTF-8` by default, fixing Unicode rendering in agent output
- **Slash-command autocomplete ranking**: Autocomplete results are ranked by match quality (run length), with project-level commands ranked ahead of system commands

### Bug Fixes

- **Zombie process accumulation**: `Session.Close` now kills the full process tree instead of just the leader, preventing zombie buildup across session restarts
- **Recording metadata corruption**: Fix race that could corrupt recording metadata, hiding recordings from the homepage
- **Recording summary quality**: Summary generation now prefers agent-chat events JSONL over terminal log tail, and caches the result in `metadata.json` to avoid per-request gzip decompression
- **Login shell env vars**: `.swe-swe/env` is now applied in login shells via `/etc/profile.d/zz-swe-swe-env.sh` with `set -a`, so PATH and other vars survive Debian's `/etc/profile` PATH reset
- **Autocomplete matching**: Fix value-or-hint matching to never split across fields; rank by run length instead of earlier match position
- **`get_chat_history` fallback**: MCP tool now falls back to ended recordings when the live session has no chat history
- **Claude extra args**: Fix default Claude extra args prefill and forwarding from page URL to WebSocket URL
- **Session shutdown**: Parallelize session shutdown and replace racy SIGCHLD wildcard reaper with a targeted orphan reaper
- **Prepared workspaces**: Drop empty `container-templates` wrapper directory from prepared workspaces

### Refactoring

- **Drop `swe-swe/` scaffolding**: Remove the workspace `swe-swe/` directory convention (setup script, agent MOTD). Agent docs moved to `.swe-swe/docs/`. Legacy `swe-swe/` directories are automatically removed on next session prepare
- **Workspace env migration**: Env file moved from `swe-swe/env` to `.swe-swe/env`. Server auto-renames the old path on next session prepare for backward compatibility

### Internal

- Bump Go base images to 1.24
- Agent-chat: playback UI parity, markdown rendering fixes, export script-tag safety
- ASCII-fix non-ASCII characters in autocomplete comments

## v2.20.0 - Recording Compression, Memory Safety & Streaming Playback

### Features

- **Gzip-compressed terminal recordings**: Recordings are compressed after session end by the cleanup scheduler, achieving ~100x size reduction for large sessions (ADR-0041)
- **Channel-based prompt compression**: Session-end prompt compression now uses a channel-based approach instead of inline processing
- **Memory guard on session creation**: Reject new sessions when server RSS is too high; per-session RSS shown on homepage
- **Recording file size on homepage**: Homepage now displays recording file sizes for each session
- **pprof endpoint**: Added `/debug/pprof` endpoint for memory leak diagnosis

### Bug Fixes

- **OOM on large recordings**: `calculateTerminalDimensions` now streams the recording log instead of loading it entirely into memory
- **Embedded recording mode removed**: Removed embedded recording mode and capped TOC entries for large logs to prevent memory issues
- **Streaming TOC**: Switched to `BuildTOCFromReader` streaming API for table-of-contents generation
- **Interactive TUI stdin**: Run script in foreground so stdin reaches interactive TUI apps (e.g. Claude Code)
- **Gzip flush on session end**: Fix gzip recording flush ensuring data is written before process cleanup
- **Deferred compression**: Moved log compression from real-time FIFO pipeline to cleanup scheduler to avoid 0-byte files caused by gzip buffering + SIGKILL race (ADR-0041)
- **create_session default repo_path**: `repo_path` is now required in the MCP `create_session` tool -- previously defaulted silently to `/workspace`, causing sessions to use the wrong repository

### Internal

- Bump `record-tui` dependency to streaming-only `BuildTOC` API
- Remove broken `make run` target

### Documentation

- ADR-0041: Deferred log compression

## v2.19.0 - Non-Root Containers, Recording TTL & E2E Testing

### Features

- **Non-root containers**: Non-DOCKER container variants now run as `USER app` instead of root, with build-time PATH shim and template conditionals for chown/su/exec. DOCKER variants still boot as root for socket permissions then drop to app user
- **Chat progress reporting**: Worktree and step-by-step skills now report progress to the chat UI
- **Composable e2e scripts**: New `e2e-up.sh`, `e2e-test.sh`, `e2e-down.sh` scripts with port connectivity tests and docker e2e mode
- **Let's Encrypt MOTD**: DigitalOcean SSH MOTD now shows Let's Encrypt upgrade steps

### Bug Fixes

- **Recording expiry**: Base recording expiry on `EndedAt` instead of creation time, bump TTL to 14 days, remove per-agent cap
- **Template nesting**: Fix template nesting bug and shell expansion in non-root CMD
- **SSE response handling**: Handle SSE-formatted responses in `callAgentChatOrchestrator`
- **Agent View VNC**: Fix broken VNC in no-SSL mode due to wrong port mapping in docker-compose
- **Auto-upgrade trigger**: Trigger auto-upgrade for configs with empty `cliVersion`

### Refactoring

- Use `SessionEnvParams` struct for `buildSessionEnv`
- Use `SessionParams` struct for `getOrCreateSession`

### Documentation

- Consolidate e2e testing documentation into composable scripts with port connectivity tests
- Update DigitalOcean deploy MOTD and docs

## v2.18.0 - VNC Readiness Probe, Chat Recording Fix & Version Display

### Features

- **Version in Session Manager header**: Homepage now shows version + commit hash (e.g. "Session Manager 2.18.0 (abc1234)") for quick identification of running version
- **Minimal shell prompt**: Container shells use `\W\$ ` prompt (just directory basename) instead of verbose Debian default

### Bug Fixes

- **Agent View "Bad Gateway"**: VNC readiness probe now uses a same-origin `/api/session/{uuid}/vnc-ready` endpoint instead of cross-origin `no-cors` requests that couldn't distinguish 502 from 200 (ADR-0040)
- **Chat recording playback**: Fix raw JS source showing instead of rendered chat UI -- a literal `</script>` in app.js prematurely closed the inlined script tag
- **Auto-upgrade on DigitalOcean**: Fix `/var/cache/swe-swe` ownership so the `swe-swe` user can auto-upgrade the cached binary (was root-owned from Packer build)

### Documentation

- ADR-0040: Same-origin VNC readiness probe

## v2.17.0 - Agent Chat Loading, Stale Config Detection & ASCII Lint

### Features

- **Agent Chat loading indicator**: Agent Chat tab appears immediately with a loading spinner while waiting for the MCP server to become available
- **Stale container config detection**: `swe-swe up` auto-detects when the container configuration is outdated and prompts to re-initialize
- **CLI version tracking**: `cliVersion` field added to InitConfig for version compatibility checks
- **ASCII-only source lint**: `make ascii-check` enforces ASCII-only source files with per-file character allowlist; `make ascii-fix` auto-replaces common accidental non-ASCII characters

### Bug Fixes

- **Agent Chat probe**: Require HTTP 200 from MCP health probe before activating Agent Chat tab (prevents premature tab activation on non-200 responses)
- **Dockerfile-only compose shim**: Cert volume mounts, env vars, proxy port ranges, and full parity with compose template (v2.16.1-v2.16.3)

### Documentation

- ADR-0038: Hybrid cookie Secure flag (X-Forwarded-Proto auto-detection with explicit override)

## v2.16.0 - Dockerfile-Only Single-Container Mode

### Features

- **`--dockerfile-only` mode**: New `swe-swe init --dockerfile-only` flag generates a single Dockerfile for deployment on platforms like Fly.io, Railway, and Render that only support single containers (ADR-0037)
- **Embedded auth**: Auth middleware (cookie-based, HMAC-SHA256, rate limiting) embedded in swe-swe-server, activated by `SWE_SWE_PASSWORD` env var — no separate auth service needed

### Documentation

- ADR-0037: `--dockerfile-only` single-container mode

## v2.15.0 - Per-Session Browser, On-Demand Startup & VS Code Opt-In

### Features

- **Per-session browser**: Each session gets its own Chrome/Xvfb/VNC stack instead of a shared sidecar — eliminates cross-session interference (ADR-0034)
- **On-demand browser startup**: Browser processes (~1.5 GB) deferred until first Playwright MCP call via `mcp-lazy-init` proxy — code-only sessions stay lightweight (ADR-0035)
- **Agent View auto-show**: Agent View tab hidden until browser starts, then auto-switches to it
- **View only / Interactive toggle**: Agent View gains a mode toggle between view-only VNC and interactive control
- **VS Code opt-in**: New `--with-vscode` flag makes code-server installation opt-in (ADR-0036)
- **Slash command autocomplete**: `/api/autocomplete` endpoint, project-level + flat command discovery, duplicate disambiguation
- **Slash command skills**: Built-in `plan-carefully`, `execute-step-by-step`, `execute-in-worktree` commands

### Bug Fixes

- **Preview proxy OOM**: Update agent-reverse-proxy to v0.2.9
- **Memory leak**: Shared `http.Client` prevents per-request Transport OOM
- **Chrome singleton lock**: Per-session user-data-dir prevents conflict
- **Stale template cleanup**: `swe-swe init` cleans container-templates dir
- **iPad tab tapping**: Raise tab bar z-index above touch-scroll-proxy
- **Cache busting**: Use GitCommit for static assets + vnc_lite.html
- **VNC routing**: Fix Traefik→websockify port routing, server-sent vncProxyPort

### Refactoring

- Replace CDP screencast with VNC for browser viewing

### Documentation

- ADR-0034: Per-session Chrome/VNC architecture
- ADR-0035: On-demand browser startup via mcp-lazy-init
- ADR-0036: VS Code (code-server) as opt-in flag
- Extract crash forensics and host security runbooks from CLAUDE.md

## v2.14.0 - Autocomplete, Session Summaries & VNC Browser

### Features

- **Slash command autocomplete**: New `/api/autocomplete` endpoint with structured responses and `has_more` field for agent-chat slash command completion
- **Session summaries**: Summary lines on session selection page and recording cards, with fallback to agent terminal log
- **Interactive browser via VNC**: Replace CDP screencast with VNC for interactive browser viewing

### Bug Fixes

- **Memory leak fix**: Use shared `http.Client` in `agentChatProxyHandler` to prevent OOM from per-request Transport allocation
- **MCP config reliability**: Always re-create Claude MCP config on container start
- **Recording cleanup**: Clean up orphaned recording files without corresponding `.log`
- **Autocomplete trigger**: Remove `@=filepath` autocomplete trigger from agent-chat config

### Documentation

- Add session and recording summaries documentation
- tdspec: add inject commands, server MCP tools, fix port gaps

## v2.13.0 - MCP Orchestration, Agent Chat Tools & Session Management

### Features

- **MCP orchestration server**: Agent-to-agent coordination via MCP orchestration server
- **Chat MCP tools**: `send_chat_message` and `get_chat_history` MCP tools for programmatic chat interaction
- **Agent-chat interrupt**: Handle `agent-chat-interrupt` to send Esc Esc + `check_messages`
- **End session confirmation**: `confirm()` dialog before ending sessions
- **Session page query params**: Unify session page query params into `SessionPageQuery` type

### Bug Fixes

- **StreamableHTTP Accept header**: Set Accept header in `callAgentChatOrchestrator` for MCP StreamableHTTP
- **Theme cookie**: Add `--theme-cookie swe-swe-theme` to agent-chat MCP config
- **Session input timing**: `send_session_input` delays CR after text, matching mobile keyboard pattern
- **JSON schema tags**: Use plain description in jsonschema struct tags for go-sdk v1.2.0
- **Template escaping**: Prevent `html/template` from double-escaping session URL query params
- **Child session handling**: Reject child session when parent not found, preserve query params on redirect
- **Process cleanup**: Kill escaped descendant processes and defer port reuse on session end
- **PTY Setpgid conflict**: Remove Setpgid that conflicts with pty.Start's Setsid
- **Public port routing**: Server-side public port probe and port-based process cleanup on session end

### Refactoring

- **Rename**: `push_message` → `send_chat_message` for consistency
- **Public port routing**: Route `PUBLIC_PORT` directly through Traefik, remove proxy hop

### Documentation

- Add `PUBLIC_PORT` direct route to tdspec topology
- Add `SessionLifecycle` tdspec for process management and end-session flow

## v2.12.1 - Public Ports, End Session & Chat Fixes

### Features

- **`PUBLIC_PORT` per session**: Each session gets a public port with a no-auth Traefik route, enabling shareable preview URLs
- **End Session button**: New button in Session Settings dialog for explicit session termination
- **Agent-chat iframe permissions**: Allow microphone and autoplay on agent-chat iframe

### Bug Fixes

- **Empty chat event files**: Skip empty JSONL files during chat event loading; conditionally show iOS Safari warning
- **Entrypoint error reporting**: Improve error messages and fix `claude mcp add` crash in entrypoint
- **Bump target ordering**: Ensure `make bump` runs docs and golden-update in correct order

## v2.12.0 - Interactive `swe-swe up`, Agent-Chat Persistence & Recording UI

### Major Features

- **`swe-swe up` merges interactive init**: No separate `swe-swe init` step needed — `swe-swe up` now runs interactive setup inline
- **Agent-chat persistence**: JSONL event logs with grouped session playback in browser
- **Recording button UI**: Distinct [Terminal] [Chat] [Agent] buttons replacing single [View] button
- **agent-chat-dist in Docker**: Embedded chat viewer in production image

### Security

- **Auth hardening**: Constant-time comparison, 7-day cookie expiry, per-IP rate limiting

### Bug Fixes

- **WebSocket relay**: gorilla/websocket frame relay (fixes path-based proxy/Cloudflare)
- **Template crash**: Fix $rec variable in TerminalUUIDs range
- **Recording card**: Move "Expires in" status into recording card meta line

### Documentation

- Clarify restart order and separate apt upgrade
- Extensive tdspec additions (MCP tools, behavioral specs, proxy specs, audit)

## v2.11.0 - Port-Based Proxy, Path Fallback & Env Var Expansion

### Major Features

- **`--proxy-port-offset` flag**: Port-based preview proxy (preferred) with automatic path-based fallback when port unavailable. Eliminates all Traefik proxy ports
- **Preview proxy in swe-swe-server**: Host the preview proxy directly in swe-swe-server with stdio bridge wired in the entrypoint, simplifying the architecture
- **Env var expansion in `swe-swe/env`**: Support `$VAR` and `${VAR}` expansion in environment files

### Bug Fixes

- **escapeHtml crash on session join**: Fix null URL crash caused by path-based routing
- **Preview URL bar**: Display logical `localhost:PORT` prefix instead of proxy paths; strip proxy prefix from URL bar
- **Self-heal stale MCP config**: Detect and fix Claude MCP config missing `--bridge` flag for preview
- **Open shim URL**: Fix open shim URL, port allocation, and resize tooltip styling
- **MCP bridge URL**: Use `localhost:9898` for preview MCP bridge URL
- **iframe embedding**: Strip `X-Frame-Options` header via agent-reverse-proxy for proper iframe embedding
- **MCP SDK dependency**: Add missing MCP SDK dependency to swe-swe-server `go.mod` template
- **Port-based proxy BasePath**: Use empty BasePath to prevent double-prefixing
- **Vestigial agent WS endpoint**: Remove unused agent WebSocket endpoint

### Dependencies

- **agent-reverse-proxy**: Update v0.2.4 → v0.2.7

### Documentation (tdspec)

- **Static tdspec docs**: Host tdspec documentation on Netlify site
- **Package rename**: Rename tdspec package to `choonkeat/swe-swe`
- **MCP tools spec**: Spec all 6 MCP tools and 5 resources
- **Behavioral specs**: Probe state machine, WebSocket reconnect, placeholder lifecycle
- **Proxy mode specs**: Spec both proxy modes — port-based (preferred) and path-based (fallback)
- **ServerAddr type**: Add `ServerAddr` type to prevent host:port confusion
- **Accuracy fixes**: Fix 6+ tdspec audit inaccuracies (stale ports, Maybe wire type, Chat userName, Fetch/XHR divergence)
- **Refactors**: Simplify types, split debug protocol types per client, unify HttpResult/ExitPayload, nest StatusPayload/State records
- **HOW-TO**: Add sum type vs record structure distinction, meta-principles

## v2.10.0 - MCP Debug Tools, Per-Session Ports & WebSocket Proxy

### Major Features

- **MCP debug channel server**: New `--mcp` stdio server with `browser_debug_preview` and `browser_debug_preview_listen` tools enabling agents to query DOM and capture console output from the Preview tab without Playwright overhead
- **Agent Whiteboard MCP server**: Add visual whiteboard capability for agent deployments to explain concepts with diagrams
- **Per-session preview ports**: Each terminal session now gets its own preview port (default range 3000-3019) with individual proxy servers, eliminating cross-session conflicts. New `--preview-ports` flag for customization
- **WebSocket proxy relay**: Preview proxy now supports WebSocket connections, enabling real-time apps (e.g., chat, live updates) in the preview tab
- **Preview navigation controls**: Back/forward buttons and read-only URL bar in preview tab toolbar, routed through debug WebSocket channel

### Slash Commands

- **`/swe-swe:update-swe-swe`**: Three-way merge support for updating workspace swe-swe files after version upgrades
- **`/swe-swe:extract-skills`**: Extract skills from task runners (Makefile, package.json, etc.)

### Terminal Recording

- **Input capture & TOC**: Record user input events for table-of-contents navigation during playback
- **Recording pagination**: Homepage now paginates recordings with agent badges
- **Extended TTL**: Auto-delete TTL extended to 48h, based on log file mtime

### Bug Fixes

- **Scroll preservation**: Fix viewport reset on clear-screen sequences using xterm.js write callback
- **iframe sandbox**: Add `allow-downloads` to app preview iframe sandbox policy
- **Mutex deadlock**: Add missing `defer mu.Unlock()` in `AddClient`, `RemoveClient`, `UpdateClientSize`
- **Codex CLI compatibility**: Intercept DSR queries and use correct TOML config format
- **Session naming**: Extract only last 2 segments from SSH URLs with nested paths
- **Worktree consistency**: Use consistent 'worktrees' plural for external repos
- **Enterprise certs**: Install certificates in builder stage for `go mod download`

### Infrastructure

- **Base dependencies**: Added `lsof` and `less` to container image
- **Dependency sync**: New `check-gomod-sync` Makefile target to detect template/main go.mod drift

## v2.9.0 - Split-Pane UI, UID:GID Mapping & Streaming Playback

### Major Features

- **Host UID:GID mapping**: Container now runs with matching host user permissions, eliminating file permission conflicts between host and container editing. Uses `{{UID}}`/`{{GID}}` Dockerfile placeholders with automatic capture at `swe-swe init`
- **Split-pane UI**: Always-available side panel with Preview, Browser, and Shell tabs. Click tab to toggle panel; desktop supports Ctrl+click for quick access. Preview proxy includes home/refresh navigation buttons
- **Debug injection proxy**: Agents can debug web apps via injected script providing WebSocket channel for console logs, DOM inspection, and network requests. New `--debug-browser` and `--debug-localhost` flags for agent integration
- **Streaming recording playback**: Now the default mode for session recordings. Streams session.log directly to xterm.js instead of embedding in HTML, improving performance for large recordings with exact terminal dimensions from metadata

### Terminal UI

- **JS module extraction**: Refactored monolithic HTML into 10 independent modules (util, validation, uuid, url-builder, messages, reconnect, upload-queue, chunk-assembler, status-renderer, CSS stylesheet)
- **Multi-line URL detection**: Detect and activate URLs wrapped across multiple terminal lines
- **Session page UX**: Improved default behavior with smart auto-open logic for preview panel

### Infrastructure

- **Network isolation**: Docker networks isolated per project to prevent cross-project conflicts
- **CDP screencast**: Replaced VNC with Chrome DevTools Protocol for browser preview
- **Auto-detect version**: Build version automatically derived from git tags
- **Base dependencies**: Added jq, vim, unzip to container image
- **Golang 1.23**: Upgraded server-builder to golang:1.23-alpine

### Bug Fixes

- **Traefik routing**: Fix 404 errors with SSL preview proxy configuration
- **Iframe embedding**: Hide status bar when terminal embedded in iframe; prevent iframe nesting
- **Container users**: Handle existing GID when creating users with matching host permissions
- **Mobile keyboard**: Apply proper margin to status bar when virtual keyboard appears
- **Cross-platform**: Use /dev/urandom for UUID generation (macOS/Linux compatibility)
- **DigitalOcean**: Run init as swe-swe user with proper home directory ownership

### Slash Commands

- **`/debug-preview-page`**: New slash command teaching agents how to use the debug channel for real-time console logs, errors, network requests, and DOM queries from the user's browser

### Documentation

- ADR-0024: Debug injection proxy security model (`docs/adr/0024-debug-injection-proxy-security.md`)
- Template editing guide (`docs/dev/template-editing-guide.md`)
- Record-tui workflow (`docs/dev/record-tui-workflow.md`)
- Streaming vs embedded rendering research (`research/2026-01-24-streaming-vs-embedded-rendering.md`)

## v2.8.0 - Shell Terminal, Heartbeat Cleanup & Deployment Automation

### Major Features

- **Heartbeat-based container cleanup**: Automated detection and graceful shutdown of stale containers via host-side heartbeat watcher with configurable timeout and signal escalation (SIGTERM→SIGKILL)
- **Container-host proxy**: New lightweight proxy bridging container and host communication for lifecycle management and health monitoring
- **DigitalOcean 1-click deployment**: Automated Packer-based image building with optional git repository cloning, hardening, MOTD health checks, and interactive init flags support
- **Bundled slash commands**: Ship swe-swe slash commands in binary, auto-installed to `~/.claude/commands/swe-swe/` with conditional `/workspace/swe-swe/` directory creation
- **Record-tui integration**: Replace custom playback with `record-tui` library for improved terminal recording and playback with speed controls

### Terminal Improvements

- **Link activation hints**: Visual hints for clickable terminal links with required modifier keys (Ctrl/Cmd) to prevent accidental activation
- **URL underline and copy**: Terminal URLs display with underlines; clicking shows copy notification for easy sharing
- **File copy notifications**: Visual feedback when file paths are copied from terminal output

### MCP & Agent Enhancements

- **MCP server rename**: Renamed `playwright` MCP server to `swe-swe-playwright` to avoid config conflicts
- **Generated MCP configs**: Auto-generate MCP configuration for OpenCode, Codex, Gemini, and Goose agents
- **OpenCode support**: Extend `--with-slash-commands` to support OpenCode (`~/.config/opencode/command/`)

### Behavior Changes

- **MOTD suppression**: Suppress MOTD for shell sessions to reduce noise
- **Streaming proxy output**: Real-time stdout/stderr streaming from container-host proxy

### Bug Fixes

- **Go module imports**: Fix missing golang.org/x/text imports for unicode normalization
- **Worktree permissions**: Ensure `/worktrees` directory has proper permissions in container
- **Traefik compatibility**: Downgrade to v2.11 for Docker API compatibility
- **Cloud-init race conditions**: Wait for cloud-final.target instead of cloud-init.target
- **systemd service startup**: Fix dependency issues causing startup race conditions

## v2.7.0 - YOLO Mode, Settings Panel & UI Customization

### Major Features

- **YOLO mode toggle**: Click "Connected" in status bar or use settings panel to toggle agents between normal and auto-approve mode. Supports Claude (`--dangerously-skip-permissions`), Gemini (`--approval-mode=yolo`), Codex (`--yolo`), Goose (`GOOSE_MODE=auto`), Aider (`--yes-always`)
- **Settings panel**: New mobile-responsive settings panel (status bar → click) with runtime customization of username, session name, and status bar color. Includes navigation links to homepage, VSCode, and browser
- **Clickable terminal colors**: CSS colors in terminal output (e.g., `#ff5500`) become clickable links to set status bar color
- **UI customization flags**: New `swe-swe init` flags for theming:
  - `--status-bar-color COLOR` with auto-contrast text and ANSI color swatches (`--status-bar-color=list`)
  - `--terminal-font-size`, `--terminal-font-family`
  - `--status-bar-font-size`, `--status-bar-font-family`

### Mobile Improvements

- **Touch scroll proxy**: Native iOS momentum scrolling with rubber band effect
- **Virtual keyboard handling**: Terminal resizes when keyboard appears, mobile keyboard bar stays visible
- **Touch event fixes**: Fixed z-index for status bar touch interactions

### Behavior Changes

- **Process exit handling**: All process exits now end the session (removed automatic crash-restart). Process replacement only occurs via explicit user action (YOLO toggle)

### Bug Fixes

- **WebSocket panic fix**: Prevent concurrent write panic with SafeConn wrapper
- **PTY cleanup**: Kill process when PTY broken but process still alive
- **Status bar legibility**: Improved text contrast across connection states
- **Worktree symlinks**: Symlink directories instead of copying for faster worktree creation

## v2.6.1 - Simplified Worktree Exit

- **Simplified exit flow**: Remove worktree merge/discard modal - exits now behave like regular sessions (see ADR-0022)

## v2.6.0 - Terminal Recording & Git Worktrees

- **Terminal recording**: Record sessions with playback UI, speed controls, and auto-cleanup (Recent vs Kept model with max 5 per agent, 1h expiry)
- **Git worktrees**: Named sessions create isolated branches with worktree re-entry, exit prompts for merge/discard, and automatic copying of `.env`, `.claude/`, and dotfiles
- **`--copy-home-paths` flag**: Copy host `$HOME` paths into container (e.g., `--copy-home-paths=.gitconfig,.ssh/config`)
- **Bundled slash commands**: Ship swe-swe slash commands in binary, auto-installed to `~/.claude/commands/swe-swe/`
- **OpenCode slash commands**: Extend `--with-slash-commands` to support OpenCode (`~/.config/opencode/command/`)

## v2.5.0 - OpenCode Agent Support

- **OpenCode agent**: Add support for OpenCode (https://github.com/anomalyco/opencode) as the 6th AI assistant
- **npm-based installation**: OpenCode installed via `npm install -g opencode-ai` for reliable Docker builds
- **Session resume**: Support `opencode --continue` for session recovery after crashes

## v2.4.1 - Documentation Fix

- **Fix `--project-directory` documentation**: Correct argument order in help text and README—subcommand must come before the flag (e.g., `swe-swe up --project-directory /path`)

## v2.4.0 - CLI Improvements & Docker Integration

- **`--with-docker` flag**: Enable Docker-in-Docker with socket mounting for agents to run Docker commands
- **`--with-slash-commands` flag**: Clone custom slash command repositories into container
- **`--previous-init-flags` flag**: Reuse init flags from previous initialization
- **CLI passthrough refactor**: Simplify CLI to pass commands directly to docker compose
- **Homepage redesign**: Unified layout showing active sessions with creation timestamps
- **Password manager fix**: Add username field for 1Password/browser autofill compatibility

## v2.3.0 - Authentication & Mobile Terminal

- **ForwardAuth authentication**: Unified password protection for all services (vscode, terminal, chrome, traefik dashboard)
- **Mobile terminal toolbar**: Add Paste button and Ctrl modifier for mobile keyboards
- **Docker Compose v2 support**: Support both `docker compose` and `docker-compose`
- **Build refactor**: Build swe-swe-server at compose time instead of embedding binary

## v2.2.0 - Path-Based Routing

- **Migrate to path-based routing**: Replace subdomain routing (`vscode.domain`, `chrome.domain`) with path-based (`/vscode`, `/chrome`) to support ngrok/cloudflared tunnels
- **Status bar links**: Add clickable links to vscode, browser, agent in terminal UI
- **Chrome/noVNC fixes**: Fix WebSocket paths, SSL certificates in NSS database

## v2.1.0 - Browser Automation & Project Management

- **Browser automation**: Chrome sidecar with MCP Playwright for AI-controlled web browsing via noVNC
- **`swe-swe list` command**: List projects with auto-prune for missing paths
- **Metadata relocation**: Move project metadata from `.swe-swe/` to `~/.swe-swe/projects/` (security: outside container reach)
- **Multi-agent support**: Add `--agents`, `--exclude-agents`, `--apt-get-install`, `--npm-install` flags
- **Enterprise SSL certs**: Install certificates into container for corporate proxies
- **Various Docker fixes**: Node.js 24 LTS upgrade, permission fixes, resource limit adjustments

## v2.0.0 - Terminal UI Rewrite

**Breaking change:** Complete architecture rewrite from web-chat to terminal-based UI.

- **xterm.js terminal**: Full terminal experience replacing chat interface
- **WebSocket multiplexing**: Multi-viewer session support with reconnection
- **Docker Compose orchestration**: Traefik reverse proxy, code-server integration
- **`swe-swe` CLI**: New CLI for `init`, `up`, `down`, `build` commands
- **Agent support**: Claude Code, Aider, Goose, Gemini CLI, Codex CLI, OpenCode
