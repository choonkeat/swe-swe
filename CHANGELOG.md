# CHANGELOG

## v2.36.0 - Idle Browser Reaping & Per-Session Login

### Features

- **Idle browsers are reaped, and a session whose browser went away heals itself**: `-browser-backend-idle` (env `SWE_BROWSER_BACKEND_IDLE`, default 30m, `0` disables) frees a browser untouched for that long -- CDP traffic, a live reverse tunnel and a 2-minute Agent View touch all count as use -- and the CDP proxy now re-allocates and replays a request whose browser is gone instead of breaking Agent View for the rest of the session.

### Fixes

- **Agent View no longer goes blank after a browser is replaced**: `x11vnc` and `websockify` were started unsupervised and quietly lost the race for the port slot, so the pane died while chromium stayed healthy; both are now supervised, started in their own process group so an orphaned `websockify` child cannot squat the port, and both the backend and a local session's own teardown now kill that group rather than the parent alone.

- **The browser backend frees every browser stack on `SIGINT`/`SIGTERM`**: A restart abandoned Xvfb, chromium, `x11vnc` and `websockify` still holding their ports, which stranded one slot per live session until allocation failed outright.

- **The login page is reachable on every per-session port**: In Traefik mode the `/swe-swe-auth` exemption was attached to the `websecure` entrypoint alone, so an unauthenticated request to a preview, chat, VNC or files port redirected to a login page that was itself gated, looping until the browser gave up.

- **The chat input bar can no longer sit below the fold on a tablet**: When mobile Safari dropped the final settling event the app kept painting at the stale -- larger -- viewport height, pushing the input row off an unscrollable page until a reload, so the app is now also capped at `100dvh`.

## v2.35.2 - Reboot Wait Screen & Dockerless Build

### Fixes

- **The reboot wait screen reloads itself behind a proxy or tunnel**: It only reloads once it has seen the server go away, but it inferred that from `fetch` rejecting -- and an edge that stays up answers 502/503/504 instead, which counted as alive, so the screen polled forever and read the real 200 as just another poll; those three statuses now mark it down.

- **`swe-swe build` and `swe-swe pull` are no-ops in dockerless mode, not fatal errors**: A dockerless project has no images to build, but the fatal exit aborted deploy loops that run `build` before `up` under `set -e`, leaving the box down until someone connected by hand.

## v2.35.1 - Phone-Sized Homepage

### Fixes

- **The homepage and its dialogs fit a phone screen**: Titles wrap instead of running off the card, the page no longer scrolls sideways below 340px, dialog inputs stay inside the dialog, and tap targets are enlarged with the hover-only keep/rename/delete row always shown -- the last two gated on `pointer: coarse`, so desktop is unchanged.

## v2.35.0 - Mobile Viewport, Session ID & Your Own End-Session Command

### Features

- **"Commit the log, then end" now runs your command, not ours**: The button sends `/commit-log-then-end` when the session agent's own command directory has one, instead of an instruction baked into the binary; nothing is bundled under `swe-swe:` so the fallback advice still appears, and `end_session`'s `uuid` now defaults to the calling session.

- **Session ID is copyable, and chat file links open in the Files pane**: Session Settings > Profile gains a read-only Session ID with a Copy button, and the chat iframe now carries `files_url` so a click on a workspace-file link reveals it in the Files pane (cmd-click opens a real tab).

### Fixes

- **iPhone: the app no longer collapses to a strip when the keyboard opens, and Session Settings' footer stays reachable**: `visualViewport`'s height and `offsetTop` are published as CSS custom properties and the app is positioned against them, replacing the `vh`/`100%` heights that held a stale value after a keyboard, toolbar, or tab restore.

- **The stop guard missed real silent turns and passed fake sends**: The turn boundary now also anchors on a `tool_result` carrying `User said:`/`User responded:`, and sends are detected from structured `tool_use` records rather than a text scan that counted any line merely naming a send tool.

## v2.34.2 - Ending-Session Label

### Fixes

- **A session being torn down shows `Join Ending Session`, in red**: The card read `Join Session (Ending)`, a parenthetical that reads as an aside rather than the state of the button you are about to press.

## v2.34.1 - Artifact Guard & Interrupt Fix

### Features

- **The built-in `Artifact` tool is now blocked in agent-chat sessions**: It publishes a page to claude.ai, off the surface the user is looking at, so blocked calls get an exit-2 message pointing at the local route (serve the page on the session's `PORT` and link to it); `SWE_ALLOW_ARTIFACTS=1` opts back in and non-chat sessions are untouched.

### Fixes

- **Stop/Cancel from Agent Chat now sends one Esc, not two**: The pair collapsed into a discarded Alt+Esc event, and where it did land it opened Claude Code's rewind picker, which swallowed the nudge typed next.

## v2.34.0 - Update Badge, Browser-Backend Recovery & Mobile Fixes

### Features

- **Homepage tells you when a newer swe-swe is published**: The version stamp grows an `available` badge, with the upgrade command and release notes on hover, from a browser-side fetch of the npm registry that fails silent when offline or blocked.

- **`tesseract-ocr` in the base image**: An agent can read text out of screenshots and scanned PDFs without an ad-hoc install per session.

- **"Commit the log, then end" is now the default, and every disposition lands on the homepage**: The option users almost always want now leads in both the homepage card and the settings panel with a `Recommended` badge, the irreversible Discard sits last, and all four dispositions redirect to `/`.

### Fixes

- **Agent View recovers after a browser-backend restart or outage, in three more places**: The backend clears stale X locks on boot and fails allocation loudly instead of advertising a dead session, `mcp-lazy-init` latches only on success and retries transient failures with backoff, and `/reboot-light` no longer kills the standalone browser backend.

- **Update badge: unreachable tooltip, and prereleases comparing equal**: An `::before` strip spans the 8px gap so the pointer can reach the release-notes link, and version comparison uses real semver precedence instead of stripping `-rc1`, which made a prerelease read as equal to the release that superseded it.

- **Codex sessions: chat-log export and event log were silently dropped**: Codex forwards only the env vars named in `env_vars`, which listed `AGENT_CHAT_PORT` but not `AGENT_CHAT_EVENT_LOG` or `AGENT_CHAT_EXPORT_DIR`.

- **The branch dropdown lists plain branch names, never `origin/*`**: The New Session dialog offered remote-tracking duplicates, which are not branches you can commit to.

- **No more iOS zoom when tapping a field**: Inputs across the New Session dialog, settings panel and terminal UI were below the 16px threshold Mobile Safari uses to decide whether to zoom in on focus.

## v2.33.0 - End-Session Chat-Log Flow & New Session Dialog

### Features

- **Ending a session asks what should happen to its chat log**: The End button, in both the homepage card and the in-session Settings panel, now offers four dispositions -- commit the log and end, keep it uncommitted, discard it, or cancel.

- **Sessions end asynchronously, and joins are refused mid-teardown**: `end_session` returns immediately so committing a large chat log no longer blocks the request, and a session in the ending state refuses new joins.

- **`reboot_server` MCP tool and a self-healing shutdown page**: An agent can restart its own server, and the shutdown page reconnects on its own instead of leaving a dead tab.

- **New Session dialog: fewer required decisions**: Agent Chat is the default channel, a sole available agent is auto-selected, required fields sit above optional ones, and the dev-channels checkbox and prefilled extra-flags box are gone.

- **`prctx --token-env NAME`**: The PR/MR review helper can read its access token from any named environment variable.

### Fixes

- **`swe-npx` hardening: verified cache, offline fallback, https-only registry, size caps, downgrade-proof memo**: Cached binaries are sha256-re-verified before every exec, an unpinned resolve falls back to the newest verified cached version when the download fails, registries and tarballs must be https, metadata/tarball/unpacked sizes and timeouts are capped, and a `latest` memo naming something older than the newest cached version can no longer force a downgrade.

- **Session cards and the in-session end dialog read correctly**: The homepage card reflects the ending state and shows a last-message summary, the Where dropdown works on touch devices, and the in-session chat-log choice is legible.

## v2.32.0 - Slash-Command Bundle Cleanup

### Features

- **`/swe-swe:save-session` and `/swe-swe:resume-session` are bundled**: Promoted from personal commands, with the handoff file always at `.swe-swe/TODO.md` of the session's own checkout, never another's.

- **`/swe-swe:commit-session-chat-log` freezes the log before committing it**: It now calls `chatlog_close` first, scrubs the frozen file and stages exactly the returned paths; the new twin `/swe-swe:discard-session-chat-log` deletes an uncommitted log and stops the stream.

- **Shipped command set culled 18 -> 15, with renames**: `extract-skills` and `fixup-upgrade` deleted, `test-full-e2e` dropped from the bundle, `pr` renamed `pr-discuss`, `update-swe-swe` renamed `update`.

- **`init` prunes swe-swe-owned command stores before re-seeding**: Seeding overwrote bundled commands but never deleted, so a renamed or removed command stayed in every install's autocomplete forever.

- **The Where dropdown shows the workspace's own origin URL**: `/api/repos` returns `workspaceRemoteURL` and the dialog attaches it as the detail line on the default-workspace option, matching the cloned-repo options.

## v2.31.0 - Tap-to-Open Links & Chat-Log Commands

### Features

- **Tap-to-open link banner plus an OSC 52 clipboard bridge**: iOS Safari does not treat a `confirm()` OK tap as user activation, so `window.open` was silently blocked; the banner's Open control is now a real anchor, with a gesture-legal Copy button and an OSC 52 handler for the headless host, and clipboard READ requests are ignored.

- **Tapping a terminal URL on a touch device raises the banner**: Terminal URL links were modifier-gated, which touch devices cannot do, so a tap only copied silently with a desktop-only hint.

- **`/swe-swe:commit-session-chat-log` replaces `/swe-swe:export-chat-logs`**: A short primitive scoped to the current session's log only, dropping the backlog-sweep behaviour; swe-swe's own prompts no longer say anything about whether a repo should commit `agent-chats/`.

### Fixes

- **OAuth links with a localhost callback are copy-only, with an explanation**: The inviting banner link is the one that cannot work from another device, because the dance ends at the agent machine's own localhost callback, so it is now shown copy-only with a pointer to the terminal's cross-device URL.

## v2.30.0 - Chat-Log Archiving, Agent View Tunnel & swe-npx

### Features

- **Automatic chat-log archiving into the repo (`agent-chats/`)**: Sessions default `AGENT_CHAT_EXPORT_DIR` to `{workDir}/agent-chats`, writing the conversation and its attachments as reviewable markdown as it happens; nothing is ever committed automatically, and it can be turned off per workspace, per session, or mid-session.

- **Agent View reverse tunnel (`--agent-view-tunnel`) -- remote browser with ZERO inbound reachability**: swe-swe dials an outbound WebSocket to the browser backend and keeps a declarative port set synced, so the backend binds those ports on its own loopback and the remote chromium needs no resolver rules -- `localhost` and vhost previews work with no inbound route to the swe-swe box at all.

- **`swe-npx` -- node-free spawning of swe-swe's own npm tools**: A stdlib-only helper resolves the platform package from the npm registry, verifies its sha512, caches it under `~/.swe-swe/npx-cache/` and execs it -- ~11ms warm versus npx's ~1.1s, and immune to the cwd collision that made a spawn inside a checkout of the same-named repo die with `not found`.

- **Compose deployments can offload Agent View to a browser-backend container**: The generated `docker-compose.yml` forwards `SWE_AGENT_VIEW`, `SWE_AGENT_VIEW_TUNNEL` and `SWE_BROWSER_BACKEND_TOKEN` from the host environment, which a compose deployment previously had no way to set.

- **Shut down the server from the homepage Settings dialog**: A confirm-gated button takes the same graceful path a SIGTERM does, useful in dockerless mode where stopping the server otherwise means finding the right terminal.

- **Re-tapping the active Files tab returns to the workspace root**: The md-serve iframe is cross-origin, so after browsing into subdirectories there was no way home short of reloading the page.

### Fixes

- **Agent View survives a browser-backend restart (tunnel mode)**: The tunnel client now reads a 404 on the upgrade as "backend up, allocation gone" and re-allocates, retargeting the CDP reverse proxy and VNC atomically, instead of reconnect-looping until the session ended.

## v2.29.1 - Zombie Sessions & Shared-Session Guest Auth

### Fixes

- **Sessions whose agent was signal-killed are now reaped**: The reaper gated on `ProcessState.Exited()`, which is true only for a normal exit, so an OOM-killed or crashed agent lingered in the sessions map forever holding all four proxy listeners.

- **Per-port panes are authorized by the live port owner, not a captured UUID**: A listener that outlived its session kept a gate matching its dead creator, so every scoped guest of the new session reusing that proxy port got a 401.

## v2.29.0 - Tailscale Removal, `--tunnel-unique` & Deterministic E2E

### Features

- **Tailscale bootstrap and its SaaS dependency are gone**: The stack no longer installs or configures Tailscale at all.

- **`--tunnel-unique` names the tunnel label, and tunnel mode fails fast without it**: The flag is wired through to the generated `docker-compose.yml`, and enabling tunnel mode without a unique name is an explicit failure rather than a confusing runtime state.

### Fixes

- **`init` regenerates the `swe-swe-server` build context as a mirror**: Init never reconciled deletions, so a renamed template (`tailscale.go` -> `listen.go`) left a stale `.go` orphan that the Dockerfile's `COPY *.go` glob compiled, breaking the image build.

### Testing

- **The everyday e2e suite no longer waits on a live model**: The one test needing a model reply is split out as an opt-in capstone and the rest converted to a deterministic shell PTY, taking the default suite to 50 tests with no live-model dependency.

- **Sessions are auto-reaped after every e2e test**: Without cleanup the resident session set grew monotonically, sliding host `MemAvailable` from ~1.5 GB to ~287 MB across a single spec file and OOM-crashing the server.

## v2.28.0 - Where Dropdown Readability & Build Reliability

### Features

- **`rsync` in the base image**.

### Fixes

- **Tailscale is installed from the apt mirror, not `curl | sh`**: The pipe takes `sh`'s exit code, so a 5xx from the installer CDN produced a successful layer with tailscale silently absent from the image.

- **Long git URLs render as two-line rows in the Where dropdown**: Options were nowrap + ellipsis against the input width, so a long remote URL was clipped to unreadability; each repo is now a short `org/repo` label over the full URL, both fuzzy-searchable.

- **A plain-checkout recording no longer prefills into a worktree**: A non-empty branch field means "make a worktree", so prefilling the checkout's branch silently converted a shared-checkout session into a worktree-on-main.

- **The popout tooltip is pinned to the UI font**: It is appended to `<body>` to escape overflow clipping, so its `font-family: inherit` fell through to the browser default serif.

- **The test-container harness works against the single-container stack**: Its scripts still assumed the old multi-container layout, so they always reported failure on a healthy stack.

## v2.27.3 - App Preview Host-Demux & Procfile Runner

### Features

- **App Preview host-demux (vhost apps)**: The preview listener demuxes `app1-5000.<reach>:<proxyPort>` to `127.0.0.1:5000` and rewrites the upstream `Host` to the logical `app1.lvh.me:5000`, so compose vhosts that dispatch on `Host` are reachable from a browser on a different machine than swe-swe. See [ADR-0045](docs/adr/0045-preview-host-demux.md).

- **`swe-run` -- a docker-free Procfile runner for multi-service apps**: It runs a foreman-compatible `Procfile` as ordinary children of the session, so the services die with it and nothing leaks onto the host; ports are assigned collision-free from the session `PORT`, with the primary service showing in the Preview tab with zero config. See [docs/multi-service.md](docs/multi-service.md).

## v2.26.0 - MCP-less Mode, Dockerless Single-Binary, prctx PR/MR Review & Remote Agent View

### Features

- **MCP-less mode (every tool reached through a `mcp` CLI, no native MCP client)**: A per-server proxy daemon fronts one stdio MCP server over a unix socket and a short-lived `mcp` client discovers servers by listing that directory, so agents with no MCP client reach every tool as a shell command; native MCP remains the default, opt in with `swe-swe init --without-mcp`.
- **Dockerless single-binary distribution (`swe-swe init --dockerless && swe-swe up`)**: The six static helper binaries are embedded in the CLI (~29MB) and extracted at init, so every tab runs with no Docker; VS Code, code-server and nginx-vscode were removed as part of the slimming.
- **`prctx` -- PR/MR review CLI (bundled in every session)**: Pulls a GitHub PR or GitLab MR into local state outside the worktree, renders it for an agent, and flushes replies, comments and verdicts back upstream idempotently, refusing when local HEAD has moved since the fetch.
- **Remote Agent View backend (off-host browser display stack)**: `-agent-view=local|off|<url>` moves the four-process display stack into a `browser-backend` service and makes the remote browser look local via a CDP reverse proxy, with the tab hiding entirely on a host with no Xvfb or chromium.
- **Per-repo environment variables (Settings > Repo)**: A `KEY=VALUE` blob held in server memory only, injected into newly opened sessions and auto-synced to trusted browsers, with reserved keys dropped and the checked-in `.swe-swe/env` winning collisions.
- **`set_session_name` MCP tool + reboot-safe teardown signals**: Agents can name their own session or a child by uuid, and `list_sessions` gains `busy` and `recordingUUID` so a reboot driver can post resume links before `compose down`.
- **Instant New-Session dialog**: The dialog no longer blocks on a synchronous `git fetch --all` -- it lists local branches immediately and refreshes remote refs in the background.
- **Agent-chat turn guards (Stop + AskUserQuestion)**: Two hooks in the provisioned `~/.claude/settings.json` block a silent turn-end (invisible to a web-UI user, and reads as a crash) and deny the built-in multiple-choice tool, whose menu renders only in the local TUI.
- **Login/auth hardening + per-session MCP keys**: The login rate limiter keys on the transport peer with a global failure ceiling, WebSocket upgrades enforce an origin allow-list, the login open-redirect is fixed, and each session gets its own MCP key so `create_session` inherits the caller's credentials without a shared global key.

### Bug Fixes

- **Chromium pinned to a known-good 147**: An unpinned install shipped chromium 150, whose zygote SIGTRAPs on launch and kills the MCP browser, VNC and preview.
- **Repo env vars delivered before the session spawns**: `inheritSessionEnv` ran after `cmd.Env` was already frozen, so saved env vars arrived one session too late.
- **MCP union-type schema flags**: Nullable arrays are typed as `["null","array"]`, which the `mcp` client failed to unmarshal, forwarding the value uncoerced until server-side validation rejected it.
- **Files tab md-serve readiness**: The Files iframe probes md-serve readiness before loading instead of racing a not-yet-bound port.
- **Files tab follows the swe-swe light/dark theme**: md-serve is launched with `-theme-cookie swe-swe-theme` so the tab honors the swe-swe toggle rather than `prefers-color-scheme`.
- **New-session staging fixes**: Full dialog wiring is staged for new sessions, chat mode survives a staging override, and a recording's "+ New" recovers its checkout branch.
- **MCP-less session resume/fork**: `swe-swe-fork-convo` resume works for MCP-less sessions, and the preview proxy key-exempts `/preview/mcp` so the headless proxy is not 302'd to login.
- **Host-only login cookie + startup/shutdown forensics**: The localhost login cookie is host-only under `--tunnel-local-ports`, and the server logs its parent process and the named signal for exit-0 crash forensics.

## v2.25.0 - Credential Auto-Wire, Worktree Anchoring, Resume/Fork & No-Ghost Sessions

### Features

- **Credential & signing auto-wire (connect-time state, no manual Save)**: The Settings panel reflects true server-side credential state the moment a browser connects and a trusted browser re-establishes its PAT, author identity and signing key without a Save; the Git HTTPS Host autofills from the workdir's `origin`, and the signing principal falls back to the workdir's git email so signatures verify for repos that never hit Save.
- **`swe-swe:merge-worktree` distributed to all deployments**: Previously an untracked loose file in this workspace hardcoding `/workspace`, it now ships in the embedded bundle with paths derived from `git rev-parse --show-toplevel`.
- **`swe-swe:fixup-upgrade` reconcile command**: An interactive reconciliation for projection drift after an upgrade, classifying every projection as IN SYNC / DROP / SALVAGE? / DRIFT and asking before deleting or relinking.
- **execute-in-worktree wires agent chat**: The spawned worktree session passes `--channels server:agent-chat` and is directed to use `send_message`, and the task file is committed before spawning since a fresh worktree only contains committed content.
- **`swe-swe:commit-session-chat-log` command**: An opt-in primitive that titles the current session's log, redacts sensitive values, and commits it with its assets in a standalone commit, staged by explicit path and never pushed.
- **Resume & branch conversations (two-step, side-effect-free fork)**: Ended recordings and live sessions expose Resume/fork affordances; `GET /api/fork/<uuid>` now renders a confirm page with zero side effects (the old GET minted a session on sight, so a prefetch or refresh could fork) and only the confirm POST materializes it.
- **No ghost sessions -- session creation requires a staged intent**: A bare navigation, reconnect, prefetch or stale `/session/<uuid>` tab no longer silently spawns an empty session; creation flows through an explicit staged intent and an unknown UUID renders a "This session has ended" screen with Resume and New session actions.

### Bug Fixes

- **Worktrees anchored off the main repo**: `resolveWorkingDirectory` special-cased only `/workspace`, so launching from a checkout that is itself a worktree doubled the path segment.
- **Forked session no longer auto-runs the source's pending directive**: The fork anchored after the source's `send_message` tool_result, so the resumed agent executed the original next directive autonomously; it now anchors at the assistant `tool_use` with an active-tail guard.
- **Slash commands missing through symlinked namespace dirs**: `discoverSlashCommands` used `entry.IsDir()`, false for a symlink-to-dir, so the canonical swe-swe namespace was silently skipped in every repo reaching it through the system store.
- **Stale swe-swe command dirs migrated to symlinks**: `ensureRelativeSymlink` bailed on any existing real directory, freezing legacy installs so shipped command fixes never reached them.
- **md-serve (Files tab) process-group cleanup**: The captured PID was the npx wrapper, so cleanup left md-serve orphaned and still bound to its port, breaking the next session's bind.
- **execute-in-worktree task delivered as one input**: The directive and the command were sent separately, so the directive started its own turn and the real command sat queued as a draft that never auto-submitted.
- **Fork/resume on workdirs containing a dot**: Claude Code encodes both `/` and `.` as `-` in its rollout folder name but we replaced only `/`, so any workdir with a dot could never be located.

## v2.24.0 - SSH Commit Signing, Skills from Git Repos & Tunnel mTLS

### Features

- **SSH commit signing (per-session; key never touches disk)**: A `sign-ssh` broker op plus the `git-sign-swe-swe` wrapper let `git commit -S` sign with an ed25519 key held only in server memory, with a generated `allowedSignersFile` so signatures verify locally and a browser-side auto-restore bound to an `(origin, init-commit-SHA)` trust tuple.
- **Session Settings redesign (sidebar tabs)**: Five focused panes -- Profile, Appearance, Git HTTPS, SSH Signing, and a confirm-gated End session -- with explicit Save/Revert, a "Saved" badge for stored credentials, and a Test connection button that validates a PAT before saving.
- **Pi MCP bridge**: A globally installed `mcp-bridge.ts` extension registers the four swe-swe MCP servers as Pi tools at session start, giving Pi the same surface as the other agents.
- **Per-tab popout gesture**: Middle-click or Ctrl/Cmd+click a tab whose pane resolves to a shareable URL to open it in a new browser tab, replacing the per-pane popout buttons.
- **`--tunnel-client-cert` (tunnel mTLS)**: Present a client certificate for mutual-TLS tunnel authentication, reusing the agent's existing `identity.key` so no new key file is introduced.
- **`--tunnel-local-ports` (tunnel mode)**: Publish a tunnel-mode container's listeners on the host's `127.0.0.1` so the machine running `swe-swe up` can reach them directly instead of only through the tunnel.
- **`swe-swe init --with-skills <alias>@<url>`**: Bake external skill repos into a container at build time and symlink each `SKILL.md`'s directory into the canonical store, so every skill surfaces to every agent through autocomplete.
- **Skills in autocomplete (every session)**: `/api/autocomplete` discovers skills regardless of assistant, scanning project-level then system-level dirs, so they surface even for a bare shell agent.
- **Collision-free flatten-with-prefix**: The autocomplete handle is the flattened store directory name, so two repos that both ship `grill-me` surface distinctly instead of one silently shadowing the other.

### Bug Fixes

- **Symlinked skills dropped from autocomplete**: `discoverSkills` used `!entry.IsDir()`, true for a symlink-to-directory, so the `--with-skills` store surfaced zero skills in a real deploy.
- **`git worktree add` on git-LFS repos**: The container now installs `git-lfs`, so worktree creation no longer fails on LFS-backed repositories.
- **Settings credentials crash with a local user identity**: The credentials section no longer crashes when the repo has a local `user` identity in `.git/config`.
- **New-session field-edit guards**: The Agent, Extra-flags and Start fields collapse while the branch combo is open, so Start cannot be clicked with uncommitted branch text.
- **Portable copied `.ssh/config`**: Init sanitizes a copied `.ssh/config` for Linux portability.
- **Pi slash-command projections**: Project-level slash commands now project correctly for Pi sessions.

## v2.23.0 - Tunnel Mode, Preset-Grid UI, Tailscale PaaS & Pi Agent

### Features

- **Tunnel mode (`swe-swe init --tunnel-server-url=...`)**: The container dials a `swe-swe-tunnel` server outbound with Ed25519 pubkey auth, so it is reachable from the public internet without owning an IP, opening ports, or provisioning TLS. See `docs/tunnel-explained.md`.
- **Tunnel-mode subprocess supervisor**: `swe-swe-server` execs the tunnel client as a child and reads structured lifecycle events off its stdout, propagating `publicHostname` to the frontend in real time (ADR-0042).
- **Tunnel subdomain iframes**: Preview, Agent View and VNC iframes route via per-session subdomains through a single auth-proxy port, replacing per-port Traefik labels and Let's Encrypt certs.
- **Tunnel-aware landing page**: The landing page shows tunnel state and a click-through to the live URL on `register_ok`.
- **`--bind` / `SWE_BIND` flag**: Restrict the in-container listener to localhost in tunnel mode so nothing on the host network reaches swe-swe directly.
- **`SWE_TUNNEL_IDENTITY_KEY` env var**: Deliver the Ed25519 keypair as a PaaS secret instead of mounting a persistent volume.
- **Tailscale single-container PaaS deploy** (`swe-swe up`): `tailscaled` is baked in and dormant unless `TS_AUTHKEY` is set, in which case the UI binds Tailscale-only and the public `$PORT` exposes just a landing page and `/health`.
- **`pi` agent backend** (`@mariozechner/pi-coding-agent`): Wired alongside the other agents, with bundled slash commands, autocomplete, and `pi --continue` for session resume.
- **Terminal UI preset grid**: 8 layout presets with a per-slot multi-tab model, drag-resizable gutters that snap at 50% and device widths, and per-preset persistence in localStorage.
- **Files tab (per-session `md-serve` read-only repo browser)**: Each session spawns its own md-serve rooted at its working directory, rendering Markdown as HTML and syntax-highlighting source with linkable line numbers, live-reloading as the agent edits.
- **Per-session git credential broker**: `git-credential-swe-swe` plus per-session `GIT_CONFIG_GLOBAL` injection routes git auth through a broker socket; the helper refuses invocation outside git.
- **`/swe-swe:setup` slash-command redesign**: A streamlined flow for git identity, auth, dev-server and env-var setup.
- **`/run-md-serve` slash-command**: Spawns `npx @choonkeat/md-serve` for previewing markdown docs.
- **Theme color resolves before session creation**: The session page resolves sticky repo color via `WorkDir` instead of waiting on a created session.
- **Agent View pop-out button**: An "open in new tab" button on the Agent View pane, mirroring the Preview-pane affordance.

### Bug Fixes

- **Per-port proxy auth bypass (security)**: Per-port proxies in tunnel mode now require the auth cookie before forwarding, and the VNC reverse proxy sits under the same wrap.
- **Cookie secure flag respects `X-Forwarded-Proto`**: The header overrides `SWE_COOKIE_SECURE` per request, so PaaS edge HTTPS works without the SSL compose template.
- **Internal server port rename**: The internal port moved from a hardcoded `9898` to `SWE_PORT` (default `1977`) so it does not clash on hosts using 9898.
- **Agent Chat spinner during probe**: The spinner animates during the non-chat-session probe instead of showing a frozen tab.
- **Stale-state cleanups on terminal-ui boot**: Stale `active:'agent-chat'` and stale inline styles no longer leave a blank Agent Terminal, and pane auto-opens no longer persist across sessions.
- **Tailscale state dir writable**: The compose shim creates a writable state dir and forwards `TS_*` env to the container.
- **`tunnel-down-manual` leaves caller in valid CWD**: The script no longer cd's into the deleted compose dir before exiting.
- **Auto-redirect to home on session end**
- **Auto-upgrade docs**: Documented `SWE_SWE_AUTO_UPGRADE` and `NODE_EXTRA_CA_CERTS_BUNDLE`.

### Refactoring

- **Drop dead per-agent recording fields** in `Session`
- **Parameterize tunnel "OPEN AT URL" port** via supervisor `LocalAddr` instead of hardcoding
- **Drop `--public-hostname` / `--tunnel-state-file`**: Replaced by the subprocess event stream, whose file-based IPC was a one-shot read at boot with order-dependent failure modes (ADR-0042)

### Internal

- Pre-commit hook to keep `.swe-swe/env` values out of commits
- E2E hardcodes all `SWE_*_PORTS` in `override.yml` + widens ranges to 30 to reduce port-collision flakes
- `SWE_SWE_TUNNEL_REF` build-arg pin bumped through `9984c43a6059` -> `751dd1cbdc42` -> `77af59b37ef5`
- Test coverage: per-port proxy auth wrap, VNC reverse proxy, slot dedup, tunnel-mode `getPreviewBaseUrl` + env passthrough, credential broker round-trip, Files-tab e2e

### Documentation

- ADR-0042: Tunnel-mode subprocess supervisor
- `docs/tunnel-explained.md` (concepts + troubleshooting), `docs/tunnel-laptop.md` (laptop runbook), `docs/tunnel-fly.md` (Fly runbook)
- `tasks/2026-04-29-tunnel-subprocess-pivot.md`: Subprocess pivot rationale + 2026-04-30 fatal/retry-after follow-up
- `docs/dev/how-to-restart.md`: End other sessions before compose down

## v2.21.2 - `.swe-swe/env` $VAR Expansion Fix

### Bug Fixes

- **`.swe-swe/env` `PATH=/x:$PATH` broke MCP servers**: `loadEnvFile` expanded `$VAR` against the server's env rather than the session env being built, silently dropping the swe-swe bin prefixes from PATH and leaving Agent Chat stuck in "Loading".

## v2.21.1 - copyDir Skips Sockets

### Bug Fixes

- **`--copy-home-paths` init crash**: `copyDir` aborted with `"operation not supported on socket"` on Unix domain sockets; sockets, FIFOs and device nodes are now skipped with a warning and symlinks preserved.

## v2.21.0 - Global Proxy, Zombie Fix & Workspace Cleanup

### Features

- **Global-tier proxy (`swe-swe proxy --global`)**: Proxy commands can live in `$HOME/.swe-swe/proxy/`, visible across every project's container, with the project tier overriding by PATH order.
- **Extra CLI flags per session**: Sessions accept additional CLI flags via the `extra_args` query parameter or MCP `create_session`.
- **UTF-8 locale in containers**: `LANG` and `LC_ALL` default to `C.UTF-8`, fixing Unicode rendering in agent output.
- **Slash-command autocomplete ranking**: Results are ranked by match run length, with project-level commands ahead of system commands.

### Bug Fixes

- **Zombie process accumulation**: `Session.Close` kills the full process tree instead of just the leader.
- **Recording metadata corruption**: Fixed a race that could corrupt recording metadata and hide recordings from the homepage.
- **Recording summary quality**: Summaries prefer the agent-chat events JSONL over the terminal log tail and are cached in `metadata.json`.
- **Login shell env vars**: `.swe-swe/env` is applied via `/etc/profile.d/zz-swe-swe-env.sh` with `set -a`, so PATH survives Debian's `/etc/profile` reset.
- **Autocomplete matching**: Value-or-hint matching never splits across fields, and ranking uses run length rather than match position.
- **`get_chat_history` fallback**: The MCP tool falls back to ended recordings when the live session has no chat history.
- **Claude extra args**: Fixed the default prefill and the forwarding from page URL to WebSocket URL.
- **Session shutdown**: Shutdown is parallelized and the racy SIGCHLD wildcard reaper replaced with a targeted orphan reaper.
- **Prepared workspaces**: Dropped the empty `container-templates` wrapper directory.

### Refactoring

- **Drop `swe-swe/` scaffolding**: The workspace `swe-swe/` convention is removed, agent docs moved to `.swe-swe/docs/`, and legacy directories removed on next session prepare.
- **Workspace env migration**: The env file moved from `swe-swe/env` to `.swe-swe/env`, auto-renamed on next session prepare.

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
- **Embedded auth**: Auth middleware (cookie-based, HMAC-SHA256, rate limiting) embedded in swe-swe-server, activated by `SWE_SWE_PASSWORD` env var -- no separate auth service needed

### Documentation

- ADR-0037: `--dockerfile-only` single-container mode

## v2.15.0 - Per-Session Browser, On-Demand Startup & VS Code Opt-In

### Features

- **Per-session browser**: Each session gets its own Chrome/Xvfb/VNC stack instead of a shared sidecar -- eliminates cross-session interference (ADR-0034)
- **On-demand browser startup**: Browser processes (~1.5 GB) deferred until first Playwright MCP call via `mcp-lazy-init` proxy -- code-only sessions stay lightweight (ADR-0035)
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
- **VNC routing**: Fix Traefik->websockify port routing, server-sent vncProxyPort

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

- **Rename**: `push_message` -> `send_chat_message` for consistency
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

- **`swe-swe up` merges interactive init**: No separate `swe-swe init` step needed -- `swe-swe up` now runs interactive setup inline
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

- **agent-reverse-proxy**: Update v0.2.4 -> v0.2.7

### Documentation (tdspec)

- **Static tdspec docs**: Host tdspec documentation on Netlify site
- **Package rename**: Rename tdspec package to `choonkeat/swe-swe`
- **MCP tools spec**: Spec all 6 MCP tools and 5 resources
- **Behavioral specs**: Probe state machine, WebSocket reconnect, placeholder lifecycle
- **Proxy mode specs**: Spec both proxy modes -- port-based (preferred) and path-based (fallback)
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

- **Heartbeat-based container cleanup**: Automated detection and graceful shutdown of stale containers via host-side heartbeat watcher with configurable timeout and signal escalation (SIGTERM->SIGKILL)
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
- **Settings panel**: New mobile-responsive settings panel (status bar -> click) with runtime customization of username, session name, and status bar color. Includes navigation links to homepage, VSCode, and browser
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

- **Fix `--project-directory` documentation**: Correct argument order in help text and README--subcommand must come before the flag (e.g., `swe-swe up --project-directory /path`)

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
