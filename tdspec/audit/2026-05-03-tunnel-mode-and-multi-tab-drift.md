# Audit: tunnel mode, multi-tab grid, port quintuple, credential broker

Date: 2026-05-03

## Scope

Full audit of all 10 tdspec modules against source. Last audit was
`2026-03-02-inject-commands-and-server-mcp.md` -- **317 commits** ago.
Major upstream changes since then:

- Auth container removed; auth embedded into swe-swe-server (2026-03-14)
- Tunnel mode: subprocess supervisor, `swe-swe-tunnel` child, JSONL
  lifecycle events, live `publicHostname` atomic, per-port subdomain
  iframes, auth-cookie per-port proxies, fatal/retry-after lifecycle
- Tailscale single-container PaaS support (`tailscaled` subprocess +
  TS_AUTHKEY/TS_HOSTNAME/TS_STATE_DIR/TS_DISABLE)
- Pi agent backend (7th agent alongside claude/codex/gemini/opencode/
  aider/goose)
- Terminal-UI rewrite: 8 layout presets, per-slot multi-tab model,
  drag-resizable gutters, per-preset localStorage, per-tab popout
  gesture (middle/Cmd-click), Agent View pop-out button
- Per-session git credential broker (`@swe-swe-broker` Unix socket,
  `git-credential-swe-swe`, per-session `GIT_CONFIG_GLOBAL`)
- Internal port rename `9898 -> SWE_PORT (default 1977)`
- `PORT` env var + landing/health server with new `resolveListenAddr`
  precedence rule
- VNC reverse proxy + per-port auth-cookie wrap (security fix
  `334034b74`)

Sources cross-referenced:

- `cmd/swe-swe/templates/host/swe-swe-server/{main.go,auth.go,tunnel_supervisor.go,tailscale.go}`
- `cmd/swe-swe/templates/host/{Dockerfile,docker-compose.yml,entrypoint.sh}`
- `cmd/swe-swe/templates/host/swe-swe-server/static/terminal-ui.js`
- `cmd/swe-swe/templates/host/{git-credential-swe-swe,mcp-lazy-init}/`
- `.swe-swe/repos/agent-reverse-proxy/workspace/{main.go,bridge.go,debughub.go,inject.go}`
- `.swe-swe/repos/agent-chat/workspace/tools.go`

Spec compiles cleanly (`make build` -> Compiled 10 modules).

## Discrepancies found

### `Domain.elm`

- **L131-142** documents `localhost :$ SWE_SERVER_PORT (default 9898)`
  with `-addr` flag default `:9898` and entrypoint
  `SWE_SERVER_PORT="${SWE_PORT:-9898}"`.
  - Source: `swe-swe-server/main.go:1696`, `tailscale.go:78-79`,
    `entrypoint.sh:185-188`, `Dockerfile:303-306`. Default is now
    `:1977` everywhere; `-addr` default is `""` (deferred to
    `resolveListenAddr`); Dockerfile has no `ENV SWE_PORT` -- the
    default comes from CMD `${SWE_PORT:-1977}` shell expansion or
    `resolveListenAddr` fallback.
- **L53-55** describes `AgentChatPort` as "the MCP sidecar (agent chat
  backend)".
  - Source: `entrypoint.sh:42`, `main.go:551-553`. The sidecar is
    `npx @choonkeat/agent-chat`, not "MCP sidecar" -- it speaks MCP
    among other protocols but the labelling is misleading.
- **Missing entirely**: `CDPPort` (6000-6019) and `VNCPort` (7000-7019)
  per-session port families (`main.go:454-455, 4228, 4244`), plus
  `vncProxyPort = vncPort + portOffset` (default 27000-27019,
  `main.go:121`).

### `Topology.elm`

- **L31, L118** comments say `swe-swe-server :$SWE_SERVER_PORT (def
  9898)` and path-based proxy "on `:9898`". Default has been `:1977`
  for some time (`tailscale.go:64-85`, `main.go:1696-1729`); literal
  `9898` no longer appears as a default anywhere in the server.
- **L144-172** describes Traefik as host-level reverse proxy with
  per-session preview/agent-chat/public listeners, but **omits tunnel
  mode entirely**.
  - Source: `docker-compose.yml:2,98-103,217-226`. In tunnel mode
    (`{{IF TUNNEL}}`) Traefik is absent, swe-swe binds
    `127.0.0.1:${SWE_PORT}`, and `swe-swe-server` supervises a
    `swe-swe-tunnel` child process (`tunnel_supervisor.go:208-271`).
    The `liveTunnelHostname` atomic, the per-port subdomain scheme
    `{port}.{publicHostname}`, and the JSONL event lifecycle
    (`register_ok` / `reconnecting` / `disconnected` / `fatal`) have
    no representation in Topology.
- **L144-172** Traefik routes mention preview / agent-chat / public
  but **not VNC**.
  - Source: `docker-compose.yml:35-36,67-68,144-145`,
    `main.go:121,4143-4244`. Compose has
    `{{VNC_ENTRYPOINTS}} / {{VNC_PORTS}} / {{VNC_ROUTERS}}` and
    `vncProxyPort = 27000+` per-port listeners; whole route family
    missing from Topology.
- **L210-219** `Process` enum lists `BrowserTerminalUi`,
  `ContainerSweServer`, `ContainerOpenShim`, `ContainerUserApp`,
  `ContainerMcpSidecar`, `ContainerStdioBridge`, `HostTraefik`. Real
  container/host processes missing:
  - `tailscaled` daemon + `tailscale up` (`tailscale.go:108-174`)
  - Landing HTTP server on `$PORT` (`tailscale.go:205-249`)
  - `swe-swe-tunnel` child supervisor (`tunnel_supervisor.go:489-513`)
  - Per-session browser stack: Xvfb + Chromium + x11vnc + noVNC
    (`main.go:4066-4138`)
- **L114-141** "Embedded auth ... Cookie: `swe_swe_session` ... Secure
  flag: explicit via SWE_COOKIE_SECURE env var, or auto-detected from
  X-Forwarded-Proto header".
  - Source: `auth.go:291-299`. Order is reversed: code checks
    `X-Forwarded-Proto` first, falls back to `SWE_COOKIE_SECURE` only
    when the header is absent.
- **Missing**: `resolveCookieDomain` (`auth.go:312-314`) sets cookie
  `Domain` to the live tunnel hostname so it's shared across
  `{port}.{publicHostname}` subdomains. `requireAuthCookie`
  (`auth.go:458-474`) is the per-port-listener cookie gate added for
  tunnel mode.

### `SessionLifecycle.elm`

- **L51-86** `SessionPorts` is a 3-port triple
  `{preview, agentChat, public}`; cites
  `findAvailablePortTriple` in main.go.
  - Source: `main.go:4224-4248`. Function is
    `findAvailablePortQuintuple` and returns five ports
    (`previewPort, agentChatPort, publicPort, cdpPort, vncPort`);
    lifecycle of all five is symmetric (allocated atomically, killed
    in `endSessionByUUID` -- `main.go:6721-6723`). Spec is missing
    `cdp` and `vnc` from the per-session port set.
- **L204-242** port cleanup matches against "session ports
  (preview, agentChat, public)".
  - Source: `main.go:6721-6740`. `endSessionByUUID` passes all five
    session ports into `killProcessesOnPorts`; spec's port set is
    incomplete.
- **L155-191** `DescendantCollection` is a 4-step pipeline
  (`ScanProc | ParsePpid | BuildParentChildMap | BfsFromRoot`).
  - Source: `main.go:6391-6436`. Code builds a full
    `PPID -> [child PIDs]` map for **all** PIDs in `/proc` in one
    combined pass (scan + parse + map building interleaved per PID
    in a single loop), then does BFS from root. The four discrete
    "steps" are a fiction -- two phases.
- **Missing entirely**: per-session **credential broker**.
  - Source: `main.go:528-549`, `Dockerfile:36-38,231`,
    `git-credential-swe-swe/main.go`. Each session has
    `GIT_CONFIG_COUNT=1 / KEY_0=credential.helper / VALUE_0=swe-swe`
    injected and a per-session
    `GIT_CONFIG_GLOBAL=<sid-scoped path>`. The
    `git-credential-swe-swe` helper dials abstract socket
    `@swe-swe-broker`, which uses `SO_PEERCRED` + ancestry walk to
    identify the calling session. None of `ensureSessionGitconfig`,
    `removeSessionGitconfig`, `clearSessionCredentials`, or the
    broker socket are modelled.
- **L316-348** `EndSessionStep` order ends `KillLingeringPortProcesses`
  then `ReleasePortReservation`, with `CloseSessionResources`
  (L267-292) before `killProcessesOnPorts`.
  - Source: `main.go:6726-6748,6583-6584`. `killSessionProcessGroup`
    already calls `clearSessionCredentials` and
    `removeSessionGitconfig` *before* `session.Close()` -- credential
    cleanup is folded into the kill step, not "Close session
    resources".

### `TerminalUi.elm`

- **L36-58** (`State`), **L78-87** (`Effect`), and the module narrative
  refer to a single "preview tab," `state.preview`,
  `OpenIframePane { pane = "preview" ... }`, `setPreviewURL`, etc.
  - Source: `static/terminal-ui.js:41-48` defines **8 layout presets**
    (`classic`, `single`, `two-row`, `l-bigR`, `stacked-R`,
    `stacked-L`, `t-splitBot`, `quadrants`); `:281-294, :3569-3594`
    show `this.preset` + `activeBySlot` (per-slot multi-tab map) +
    `sizesByPreset` (gutter-drag-resizable per-preset persistence in
    `LAYOUT_STATE_KEY`); panes are `agent-terminal`, `agent-chat`,
    `preview`, `vscode`, `shell`, `browser`. Spec's `State` is
    missing `preset`, `activeBySlot`, `sizesByPreset`,
    `publicHostname`, `tunnelStatus`, `vncProxyPort`,
    `previewProxyPort`, `agentChatProxyPort`,
    `_pendingPreviewIframeSrc`, `_proxyMode`,
    `_agentChatPending/Probing/Available`, `mobileActiveView`.
- **L78-87** `Effect` enumeration omits real effects:
  - Tab-popout to a new browser window
    (`static/terminal-ui.js:4546-4565` `panePopoutUrl`, plus the
    middle-click / Cmd-click handler that calls `window.open`)
  - Render the tunnel-status banner (`:3896-3970`
    `_renderTunnelStatusBanner`, driven by `tunnelStatus`
    `{state, retryAfterMs, reason}`)
  - Persist preset/layout to localStorage (`LAYOUT_STATE_KEY`)
  - Set/clear `data-mobile-active`
  - Chat-probe spinner control
- **L87, L91** `AutoSwitchToAgentView`: "When browserStarted transitions
  to True, auto-switch to Agent View tab and show the tab (previously
  hidden)."
  - Source: `static/terminal-ui.js:1412-1419` only auto-adds `browser`
    to its preset's home slot via `autoAddPaneToHome('browser')` and
    calls `setAgentViewTabVisible(true)`; does **not** auto-switch
    focus. The Agent View pane also has its own popout button
    (`04732ea1e` / `78a643068`) that the spec ignores.
- **L132-136** `Open` "BUG: 2x dialogs from broadcast" comment.
  - No code in `agent-reverse-proxy/workspace/inject.go` or
    `debughub.go` ever sends `t: 'open'` (only `urlchange`,
    `navstate`, telemetry). `static/terminal-ui.js:5211-5214` still
    handles `msg.t === 'open'` defensively, but the dup-dialog
    scenario is unreachable through the proxy hub.
- **L84** `OpenIframePane { pane : String, url : Url }` with
  `pane = "preview"`.
  - Source: `static/terminal-ui.js:4744` `openIframePane(tab, url)`
    mutates legacy `this.activeTab` and is now mostly used for the
    `xdg-open` fallback. Real navigation goes through
    `setActiveInSlot(slotId, paneId, {persist})` (`:3664`) and
    slot-aware `panePopoutUrl(paneId)`.
- **L340-341** `DebugChannel` config -- "Sequence: 1s, 2s, 4s, 8s, 10s,
  10s, ...".
  - Source: `static/terminal-ui.js:5238-5247` `_scheduleDebugWsReconnect`
    matches; but there is **no separate PTY backoff machine** in this
    file. PTY uses `modules/reconnect.js` which is not what the spec
    describes for `PtyChannel`'s 60s ceiling. Spec's `wsConfig` /
    `wsTransition` model doesn't correspond to an actual two-arm
    switch.

### `PreviewIframe.elm`

- **L46-51** `Init` is "sent by onPageLoad" producing
  `ShellSend (Init { url, ts })`.
  - Source: `agent-reverse-proxy/workspace/inject.go:214-237`. The
    shell page only ever sends `urlchange` and `navstate` on inner
    iframe load; there is no `init` send. The actual `init` is sent
    by `inject.js` via `window.parent.postMessage` (`inject.go:652`)
    and only relayed to the hub if the shell page's message bridge
    (`:240-244`) catches it. `Init` is part of the
    inject-relayed-via-shell flow, not a shell-page-native effect.
- **L131-135** `onWsUpgrade -> WsUpgrade`.
  - Source: `inject.go:612` actually sends `t: 'ws-upgrade'`
    (kebab-case). Spec's implied JSON tag would be `wsUpgrade` per
    Elm convention; encoder/decoder pair would need explicit kebab
    mapping.
- **L69-83** `ShellPageAction = NavigateIframe | ReloadIframe`.
  - Source: `inject.go:189-207`. Mapping is structurally correct but
    the shell page **also** auto-sends `urlchange` once for every
    inner iframe load including the first -- so `onPageLoad` fires
    only on the very first navigation, while every later URL change
    goes through the same `onUrlChange` codepath. Spec's
    `onPageLoad` vs `onUrlChange` distinction does not exist in the
    source.
- **L131+ documentation of `xdg-open` flowing through HTTP `/open`**.
  - No HTTP `/open` handler in `agent-reverse-proxy/workspace/`;
    `t: 'open'` does not appear as a sender anywhere. Only the
    consumer side at `static/terminal-ui.js:5211` survives -- the
    producer was removed.

### `HttpProxy.elm`

- **L17-18, L149** path-based fallback chain says
  `swe-swe-server :9898`. Default is `:1977` (`main.go:1696,1729`);
  9898 was retired in commit `eb978bc74`.
- **L34-37, L139-146** (`ProxyMode`) covers preview and agent-chat
  per-port proxies only.
  - Source: third per-port listener -- VNC reverse proxy on
    `vncProxyPort = 20000+vncPort` (default 27000-27019),
    `auth.go:444-446,458-474` and `main.go:118-121, 4609-4643`. Whole
    VNC proxy chain is missing.
- **L34-37, L139-146** claims port-based per-port handlers are
  protected only by `corsWrapper`.
  - Source: every per-port handler is wrapped in
    `requireAuthCookie(authPassword, ...)` (`main.go:4574, 4593, 4629`)
    -- the security fix from commit `334034b74` is not represented
    in `ProxyMode` or anywhere else in the module.
- **L25-32, L46-47** port derivation table omits
  `vncProxyPort = vncPort + portOffset`.
- **Missing**: tunnel-mode subdomain note. Source routes tunnel-mode
  iframes through `{previewProxyPort}.{publicHostname}` (commit
  `9d7e03725`, `terminal-ui.js:4513,4987`) so the auth-proxy port --
  not raw target ports -- carries the apex login cookie.
- **Missing**: `resolveCookieSecure` in `auth.go:279-299` honours
  per-request `X-Forwarded-Proto: https` over `SWE_COOKIE_SECURE`.
- **Missing**: `corsWrapper` exposes a no-auth `/__probe__` path
  (`main.go:1510-1514`) and `requireAuthCookie` also exempts it
  (`auth.go:463-466`). The `ProbePhase` machine never mentions this
  dedicated probe endpoint.

### `PtyProtocol.elm`

- **L23-31** (`ClientMsg`): no `set_credentials` variant.
  - Source: `main.go:4990-5028` accepts `set_credentials` over the
    PTY WS (carries `host/username/token/name/email`, write-only).
    This is the per-session credential broker entry point.
- **L37-44** (`ServerMsg`): no `credentials_stored` ack variant.
  - Source: `main.go:5022-5025` emits
    `{type:"credentials_stored", host, hosts}`.
- **L29** (`ToggleYolo` comment) claims wire type is `"toggleYolo"`.
  - Source: `main.go:4953` uses `toggle_yolo` (snake_case); rename is
    also `rename_session`, not `renameSession`.
- **L60-85** (`StatusPayload`) omits `vncPort`, `vncProxyPort`,
  `cdpPort`, `publicHostname`, `tunnelStatus`.
  - Source: all are emitted unconditionally on the wire
    (`main.go:818-848`). Frontend behaviour (Agent View tab
    visibility, tunnel UI, banner) hinges on these.

### `DebugProtocol.elm`

- **L13-17** endpoints pinned to `:9898`. Source serves them on the
  SWE_PORT default `:1977` (see HttpProxy section above).
- **L106-118** (`FetchResult`) models nested
  `{request:{url,method,ms,ts}, response: Result {error} {status, ok}}`.
  - Source: `inject.go:481-502`. Wire shape is **flat**:
    `{t:"fetch", url, method, status, ok, ms, ts}` on success,
    `{t:"fetch", url, method, error, ms, ts}` on failure. No
    `request`/`response` nesting on the wire.
- **L121-135** (`XhrResult`) same nesting mismatch -- wire shape from
  `inject.go:521-531` is flat.
- **L92-100** (`WsUpgrade`) wire `t` discriminator is `ws-upgrade`
  (`inject.go:612`); spec name suggests `wsUpgrade`. JSON tag isn't
  documented in either direction.
- **L57-100** `InjectJsDebugMsg` is missing 5 result variants.
  - Source: `inject.go:357, :385, :411, :420, :427`. Missing:
    `clickResult`, `typeResult`, `fillFormResult`, `pressKeyResult`,
    `evaluateResult`. All observable hub messages with no spec
    representation; any decoder would fail.
- **L62** `Console.args : List String`.
  - Source: `inject.go:446` maps each arg through `serialize()`
    (`:280-303`) which can return objects, arrays, `'[function]'`,
    `{name,message,stack}` for `Error`, etc. -- not always strings.

### `DebugHub.elm`

- Module is structurally accurate against `debughub.go` and `main.go`
  routing. Inherits the `:9898` reference from `DebugProtocol`. No
  other mismatches found.

### `McpTools.elm`

- **L33-37, L96-97** claims `MCP_AUTH_KEY` protects `/mcp`,
  `/api/session/{uuid}/browser/start`, and
  `/api/autocomplete/{uuid}`.
  - Source: `auth.go:411-421` exempts those three paths from the
    cookie middleware; `/mcp` enforces the key
    (`main.go:1929-1934`); `browser/start` is enforced by
    `mcpAuthKey` only at `main.go:6841`; `/api/autocomplete/` has
    no equivalent guard visible. Spec overstates uniformity of
    `MCP_AUTH_KEY` coverage.
- **L298-369, L386-393** (`AgentChatTool`, `allAgentChatTools`)
  missing `export_chat_md` (`agent-chat/workspace/tools.go:484`).
- **L99-193** (`ServerTool`, `allServerTools`) lists
  `SendChatMessage` and `GetChatHistory` as swe-swe-server MCP tools
  -- they exist there as proxies (`main.go:7397-7471`), but the same
  names are also registered on the agent-chat orchestrator MCP
  (`tools.go:541, 562`). Spec doesn't note dual registration.
- **Missing**: `mcp-lazy-init` shim
  (`cmd/swe-swe/templates/host/mcp-lazy-init/main.go`) wraps stdio
  MCP servers and fires an HTTP init request (e.g.
  `POST /api/session/$UUID/browser/start`) before the first
  `tools/call`. This is the lazy-init pathway that turns
  `browser/start` into an implicit dependency of any browser MCP.
- **L46** "preview MCP message flow" claims
  `inject.js -> WS -> DebugHub -> subscriber channel -> MCP tool`.
  - Source: `debughub.go:99-125`. DebugHub additionally fans out to
    UI observers in the same call; in-proc subscribers are
    non-blocking with a 64-buffer drop policy. MCP tools may miss
    messages under load.

## Cross-cutting themes

1. **Internal port default flipped 9898 -> 1977.** Stale across
   `Domain.elm`, `Topology.elm`, `HttpProxy.elm`, `DebugProtocol.elm`.
   Single mechanical pass to fix all references.

2. **Tunnel mode is structurally absent.** Major architectural feature
   shipped over the last 60 days has zero spec representation:
   subprocess supervisor, JSONL event lifecycle, live `publicHostname`
   atomic, per-port subdomain auth-cookie scheme, `requireAuthCookie`
   gate, tunnel-aware landing page, status banner state machine.
   Affects `Topology.elm`, `TerminalUi.elm`, `PtyProtocol.elm`,
   `HttpProxy.elm`. Likely warrants a new `TunnelMode.elm` module
   plus extensions to existing ones, rather than splicing it into
   `Topology` alone.

3. **Per-session ports are now a quintuple, not a triple.** CDP
   (6000s) and VNC (7000s) have full port-family + per-port-listener
   + Traefik routes + lifecycle handling that neither `Domain`,
   `SessionLifecycle`, `Topology`, nor `HttpProxy` reflects.

4. **TerminalUi is conceptually a generation behind.** Still models
   single preview tab + agent terminal; source has shipped 8-preset
   per-slot multi-tab grid with `activeBySlot`, drag-resizable
   gutters, per-preset localStorage persistence,
   dedup-across-slots, per-tab popout gestures, Agent View popout
   button. Probably the biggest single rewrite needed in the spec.

5. **Per-session credential broker is entirely absent.** The
   `@swe-swe-broker` Unix socket, `git-credential-swe-swe` helper,
   per-session `GIT_CONFIG_GLOBAL`, `set_credentials` /
   `credentials_stored` PTY round-trip, and the
   `ensureSessionGitconfig` / `clearSessionCredentials` /
   `removeSessionGitconfig` lifecycle aren't modelled in
   `SessionLifecycle.elm` or `PtyProtocol.elm`.

6. **Topology omits real container/host processes.** `tailscaled`,
   landing HTTP server, `swe-swe-tunnel` child, per-session browser
   stack (Xvfb + Chromium + x11vnc + noVNC). The `Process` enum and
   `Topology` narrative both predate these.

7. **Per-port proxy security model has changed.** `requireAuthCookie`
   wrap on every per-port handler (commit `334034b74`) is the
   security fix that makes tunnel-mode subdomain iframes safe; spec
   still characterises per-port proxies as "CORS only".

8. **DebugProtocol JSON shapes don't match the wire.** `FetchResult`
   and `XhrResult` model a nested `{request, response}` shape; wire
   is flat. 5 missing `*Result` variants from
   `clickResult/typeResult/fillFormResult/pressKeyResult/evaluateResult`.
   `Console.args` typed as `List String` but inject serialises
   objects.

9. **Vestigial spec content.** `Open` HTTP fallback and "2x dialogs
   from broadcast" BUG comment in `TerminalUi` describe a code path
   the producer was removed from. Should either be deleted or moved
   to a "removed" / "historical" section.

## Open questions / decisions deferred

- **Should tunnel mode be a new `TunnelMode.elm` module or spliced
  into existing modules?** Argument for new module: it crosscuts
  Topology, TerminalUi, HttpProxy, PtyProtocol, and SessionLifecycle.
  Argument for splicing: the existing modules are how readers
  navigate; a separate module risks being missed.

- **`PtyChannel`'s reconnect backoff documents 60s ceiling but no
  matching machine exists in `static/terminal-ui.js`.** Either the
  spec should drop the claim or the audit needs to track down where
  PTY reconnect actually lives (likely `modules/reconnect.js` --
  not investigated in this audit).

- **`McpTools` overstatement of `MCP_AUTH_KEY` coverage.** Decide
  whether to narrow the spec's claim to match (only `/mcp` and
  `browser/start`) or whether `/api/autocomplete/` should also be
  guarded (potential security regression worth filing as a separate
  issue).

## Fixes applied

None. This audit is observational. Apply fixes module-by-module in
follow-up commits, ideally bottom-up:

1. Mechanical: `9898 -> 1977` across Domain, Topology, HttpProxy,
   DebugProtocol.
2. Concrete additions: `cdpPort`, `vncPort`, `vncProxyPort` types
   and lifecycle in Domain + SessionLifecycle + HttpProxy + Topology.
3. Concrete corrections: 5 missing `*Result` variants in
   DebugProtocol; `set_credentials` + `credentials_stored` in
   PtyProtocol; `export_chat_md` in McpTools.
4. Structural: TerminalUi rewrite for preset grid + multi-tab.
5. Structural: tunnel mode (decide module shape first).
6. Structural: credential broker module / extensions to
   SessionLifecycle.
