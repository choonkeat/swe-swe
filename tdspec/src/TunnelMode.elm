module TunnelMode exposing
    ( PublicHostname(..), IdentityFingerprint(..)
    , TunnelEventKind(..), TunnelState(..)
    , SupervisorBehavior(..), supervisorBehavior
    , IdentitySource(..)
    , TunnelStatus, StatusFrameAdditions
    , CookieDomainResolution(..), resolveCookieDomain
    , IframeUrlBuilding(..), iframeUrlBuilding
    , LandingServerMode(..)
    )

{-| Tunnel mode -- the in-process supervisor of `swe-swe-tunnel`, the
runtime behaviors that branch on its presence, and the wire-frame
additions it makes to the rest of the system.

Tunnel mode is enabled at **image build time** via
`swe-swe init --tunnel-server-url=...` (see
[ADR-0043](../docs/adr/0043-tunnel-mode-not-a-sidecar.md) and
[docs/tunnel-explained.md](../docs/tunnel-explained.md)). It is
mutually exclusive with compose-mode-with-Traefik because the cookie
domain, per-port iframe auth, frontend subdomain URLs, and Traefik
routing all key off a public hostname populated **only** by the
in-process supervisor that the build flag bakes into the image.

When tunnel mode is on, `swe-swe-server` exec's the
`swe-swe-tunnel` client as a child process. The child emits JSONL
lifecycle events on stdout. The supervisor parses them, updates a
package-level `liveTunnelHostname` atomic, and broadcasts a
`tunnelStatus` payload on the WebSocket status frame to terminal-ui.

`liveTunnelHostname` is the load-bearing value: cookie `Domain`
resolution, frontend subdomain URL construction, the landing-page
`OPEN AT https://...` line, and the iframe auth-cookie scheme all
read it. Empty string means "tunnel not connected" -- callers fall
back to host-only cookies and path-base URLs (which break for
per-port iframes; see ADR-0043 #1, #2).

This module crosscuts:

  - `Topology` (process-level: supervisor + child + landing server)
  - `PtyProtocol.StatusPayload` (wire frame fields `publicHostname`
    and `tunnelStatus`)
  - `TerminalUi` (status banner, subdomain iframe URL building)
  - `HttpProxy` (per-port `requireAuthCookie` wrap, subdomain routing)
  - `SessionLifecycle` (no direct dependency)

@docs PublicHostname, IdentityFingerprint
@docs TunnelEventKind, TunnelState
@docs SupervisorBehavior, supervisorBehavior
@docs IdentitySource
@docs TunnelStatus, StatusFrameAdditions
@docs CookieDomainResolution, resolveCookieDomain
@docs IframeUrlBuilding, iframeUrlBuilding
@docs LandingServerMode

-}


{-| The public hostname that the tunnel server bound for this client
on `register_ok`. Set in the supervisor's `liveTunnelHostname`
atomic; read by `resolveCookieDomain`, frontend iframe URL builders,
the landing page, and the WS status frame.

`PublicHostname ""` represents "tunnel not connected" -- distinct
states semantically (no tunnel, supervisor failed, child not yet
registered) all collapse to the empty string, since the consumer
behavior is identical: fall back to host-only cookies and path-base
URLs.

Source: `tunnel_supervisor.go:127` (`liveTunnelHostname` atomic).

-}
type PublicHostname
    = PublicHostname String


{-| Short hex identifier for an Ed25519 identity public key.

Computed as `sha256(pubkey)[:6]` hex (12 hex chars, 48 bits) so
operators can compare across deploys to confirm "I deployed the
right key" without leaking the key itself. The same value appears in
the tunnel server's `register ok` / `register denied` log lines, so
quoting it across systems is a fast troubleshooting handle.

Source: `internal/tunnelclient/identity.go:logIdentityFingerprint`
in the swe-swe-tunnel repo.

-}
type IdentityFingerprint
    = IdentityFingerprint String


{-| Event kinds emitted by the `swe-swe-tunnel` child on stdout
(JSONL: one event per line). The supervisor parses each event and
either updates state, restarts the child, or stops permanently.

Order during a healthy session:

    Starting -> Connecting -> RegisterOk -> ... -> DeregisterOk

Re-connect cycle (tunnel server drop, transient network):

    Disconnected -> Reconnecting -> Connecting -> RegisterOk

Permanent denial (e.g. pubkey not authorized, key mismatch):

    Fatal{kind, reason}   -- supervisor stops, no retry

Per-variant payloads:

  - `Starting`, `Connecting`: no payload.
  - `RegisterOk`: carries `hostname` -- the public hostname bound by
    the tunnel server. Sets `liveTunnelHostname`.
  - `Relabel`: same payload shape as `RegisterOk`. Emitted when the
    tunnel server rotates the hostname (e.g. apex DNS migration);
    the atomic is updated and the WS status frame re-broadcast so
    the frontend picks it up live, no reconnect needed.
  - `Reconnecting`: carries `retryAfterMs` and `reason`. Surfaces as
    a UI countdown banner; child will emit a fresh `RegisterOk` on
    reconnect (or another `Fatal` if the deny is permanent).
  - `Disconnected`: no payload.
  - `TransientError`: wire kind is `error` (renamed here to avoid
    clashing with `DebugProtocol.Error`). Logged but not fatal --
    the child will retry per its own backoff.
  - `Fatal`: carries `kind` (e.g. `not_authorized`, `key_mismatch`,
    `signature invalid`) and `reason`. Supervisor stops; the
    container idles without a tunnel. The frontend banner shows a
    permanent error state.
  - `DeregisterOk`: graceful shutdown handshake completed. Emitted
    during orderly container stop, not as part of normal operation.

Source: `tunnel_supervisor.go:359-449` (`onChildEvent`).

-}
type TunnelEventKind
    = Starting
    | Connecting
    | RegisterOk
    | Relabel
    | Reconnecting
    | Disconnected
    | TransientError
    | Fatal
    | DeregisterOk


{-| The tunnel-state value carried on the WS status frame
(`tunnelStatus.state`). The frontend's tunnel-status banner branches
on this to decide what to render (countdown timer, connected
hostname, permanent error message, hidden).

Distinct from `TunnelEventKind` -- multiple event kinds collapse to
the same state (e.g. both `Starting` and `Connecting` show as
`ConnectingState` in the UI).

Wire values:

  - `ConnectingState`: `"connecting"`. Initial + transient state.
  - `Connected`: `"connected"`. Set on `RegisterOk` / `Relabel`.
  - `ReconnectingState`: `"reconnecting"`. Carries `retryAfterMs`
    for the countdown.
  - `DisconnectedState`: `"disconnected"`. Transient -- the
    supervisor is about to attempt re-connect.
  - `ErrorState`: `"error"`. Recoverable error from the child;
    supervisor will retry.
  - `FatalState`: `"fatal"`. Permanent -- supervisor has stopped.
    UI shows the `reason` and does not auto-retry.

-}
type TunnelState
    = ConnectingState
    | Connected
    | ReconnectingState
    | DisconnectedState
    | ErrorState
    | FatalState


{-| What the supervisor does on each event kind.

The reason this lives in the spec at all: the choice of "restart the
child" vs "stop permanently" determines whether the system can
recover from a transient outage vs. a permanent deny -- and getting
this wrong (e.g. restart-looping on a `Fatal{not_authorized}`) would
hammer the tunnel server with denied registrations until rate-limit.

Behaviors:

  - `UpdateAtomicAndBroadcast`: for `RegisterOk` / `Relabel` --
    update `liveTunnelHostname` atomic, re-emit WS status frame to
    all connected clients.
  - `LogAndContinue`: for `Starting`, `Connecting`, `Disconnected`,
    `TransientError`, `DeregisterOk` -- log the event but take no
    other action; child will progress on its own.
  - `UpdateUiState`: for `Reconnecting` -- broadcast
    `tunnelStatus.state = reconnecting` with `retryAfterMs` so the
    UI shows a countdown banner.
  - `StopSupervisor`: for `Fatal` -- stop the supervisor, do not
    restart the child. The container keeps running but is not
    reachable through the tunnel.

-}
type SupervisorBehavior
    = UpdateAtomicAndBroadcast
    | LogAndContinue
    | UpdateUiState
    | StopSupervisor


{-| The supervisor behavior dispatch.

    supervisorBehavior RegisterOk    --> UpdateAtomicAndBroadcast
    supervisorBehavior Relabel       --> UpdateAtomicAndBroadcast
    supervisorBehavior Reconnecting  --> UpdateUiState
    supervisorBehavior Fatal         --> StopSupervisor
    supervisorBehavior _             --> LogAndContinue

Source: `tunnel_supervisor.go:359-449`.

-}
supervisorBehavior : TunnelEventKind -> SupervisorBehavior
supervisorBehavior kind =
    case kind of
        RegisterOk ->
            UpdateAtomicAndBroadcast

        Relabel ->
            UpdateAtomicAndBroadcast

        Reconnecting ->
            UpdateUiState

        Fatal ->
            StopSupervisor

        Starting ->
            LogAndContinue

        Connecting ->
            LogAndContinue

        Disconnected ->
            LogAndContinue

        TransientError ->
            LogAndContinue

        DeregisterOk ->
            LogAndContinue


{-| Where the Ed25519 identity came from at process start.

Logged once on boot as
`[tunnel-client] identity loaded source=env|file fingerprint=ab12cd34ef56`.
`source=env` is what an operator wants to see when they set
`SWE_TUNNEL_IDENTITY_KEY` -- `source=file` means the env var was
missing or unparseable and the client fell back to the on-disk
path, which burns a fresh `unique` on the tunnel server each restart
on ephemeral filesystems.

Variants:

  - `Env`: `SWE_TUNNEL_IDENTITY_KEY` was set and parsed cleanly
    (base64-encoded PKCS8 PEM of an Ed25519 private key).
  - `File`: loaded from `~/.swe-swe-tunnel/identity.key`
    (auto-generated on first run). Persists across restarts only on
    a writable filesystem.

Source: `internal/tunnelclient/identity.go:LoadIdentity` in the
swe-swe-tunnel repo.

-}
type IdentitySource
    = Env
    | File


{-| Subrecord broadcast on the WebSocket status frame as
`tunnelStatus`. Frontend renders the banner from `state` +
`retryAfterMs`.

Source: `tunnel_supervisor.go:131-138` (`tunnelStatus` struct on the
Go side); `main.go:843-847` (assembled into the status payload).

-}
type alias TunnelStatus =
    { state : TunnelState
    , retryAfterMs : Maybe Int
    , reason : Maybe String
    }


{-| Fields the WS status frame carries **only** when tunnel mode is
active (i.e. the supervisor is running). Compose-mode deploys leave
`publicHostname` as an empty string and omit `tunnelStatus`.

These augment `PtyProtocol.StatusPayload`; they are not modelled
there directly so non-tunnel callers can ignore them.

Source: `main.go:838-848`.

-}
type alias StatusFrameAdditions =
    { publicHostname : PublicHostname
    , tunnelStatus : Maybe TunnelStatus
    }


{-| How `auth.go:resolveCookieDomain` decides what to put in the
`Domain` attribute of the `swe_swe_session` auth cookie.

Without tunnel mode (or before the first `RegisterOk`), the cookie
is host-only -- fine for a single-page session, but breaks the
per-port iframe scheme because `{port}.{publicHostname}` subdomains
have no cookie. With tunnel mode and a live hostname, the cookie's
`Domain` is set to the apex so all subdomains share auth state --
**but only when the browser actually reached the server via that
apex**. Tunnel mode and local access coexist: with
`--tunnel-local-ports` the same server answers on `localhost:{port}`
and LAN addresses. A cookie stamped `Domain={apex}` is rejected by
the browser on a localhost login (RFC 6265 requires `Domain` to
domain-match the request host), so the resolver must fall back to
host-only there. That is why the resolver also takes the request
host, not just the public hostname.

This is the single load-bearing piece in ADR-0043 #1 -- the bug
that makes a sidecar tunnel against `swe-swe up` fail with a login
bounce.

Variants:

  - `HostOnly`: `Domain=""`. Cookie is bound to whatever host the
    request came in on. Per-port subdomain iframes won't see it.
    Used in legacy mode, AND in tunnel mode when the request host
    is not the apex or a subdomain of it (localhost, 127.0.0.1, LAN
    IP under `--tunnel-local-ports`).
  - `ApexDomain h`: `Domain=h`. Cookie is shared across all
    subdomains under the apex (e.g.
    `27000.<unique>-tunnel.<server-suffix>` reads the same cookie as
    the main UI). Used only when the request host equals the apex or
    is a subdomain of it.

Source: `auth.go:312-333`.

-}
type CookieDomainResolution
    = HostOnly
    | ApexDomain PublicHostname


{-| `resolveCookieDomain publicHostname requestHost` -- mirrors
`auth.go:resolveCookieDomain`. `requestHost` is the request `Host`
header with any `:port` suffix already stripped.

    resolveCookieDomain (PublicHostname "") "anything"
        --> HostOnly

    resolveCookieDomain (PublicHostname "myproject-tunnel.example") "myproject-tunnel.example"
        --> ApexDomain (PublicHostname "myproject-tunnel.example")

    resolveCookieDomain (PublicHostname "myproject-tunnel.example") "3000.myproject-tunnel.example"
        --> ApexDomain (PublicHostname "myproject-tunnel.example")

    resolveCookieDomain (PublicHostname "myproject-tunnel.example") "localhost"
        --> HostOnly

-}
resolveCookieDomain : PublicHostname -> String -> CookieDomainResolution
resolveCookieDomain (PublicHostname h) requestHost =
    if h == "" then
        HostOnly

    else if requestHost == h || String.endsWith ("." ++ h) requestHost then
        ApexDomain (PublicHostname h)

    else
        HostOnly


{-| How the frontend builds iframe URLs (preview, Agent View, VNC,
agent-chat) -- branches on `tunnelStatus.publicHostname`.

In compose mode (no tunnel), the auth-proxy port can be reached at
the same host the main UI is on (`<host>:<previewProxyPort>`) since
Traefik routes by `Host(<DOMAIN>) && PathPrefix(/)`. In tunnel mode,
the wildcard subdomain pattern routes by leftmost-label port:
`<previewProxyPort>.<publicHostname>` -- which the tunnel server's
`PortDispatchHandler` decodes back into a forward to
`localhost:<previewProxyPort>` inside the container.

Variants:

  - `PortBased`: `<host>:<previewProxyPort>/...` -- compose mode.
    The browser hits a different port on the same host; Traefik /
    OS routes it.
  - `SubdomainBased h`: `<previewProxyPort>.h/...` -- tunnel mode.
    The leftmost label is the port number; the tunnel server
    decodes it and forwards to the in-container listener.

Source: `static/terminal-ui.js:4502-4534`,
`static/terminal-ui.js:1395-1408`.

-}
type IframeUrlBuilding
    = PortBased
    | SubdomainBased PublicHostname


{-| `iframeUrlBuilding publicHostname` -- mirrors the frontend
branch on `tunnelStatus.publicHostname`.

    iframeUrlBuilding (PublicHostname "")
        --> PortBased

    iframeUrlBuilding (PublicHostname "myproject-tunnel.example")
        --> SubdomainBased (PublicHostname "myproject-tunnel.example")

-}
iframeUrlBuilding : PublicHostname -> IframeUrlBuilding
iframeUrlBuilding (PublicHostname h) =
    if h == "" then
        PortBased

    else
        SubdomainBased (PublicHostname h)


{-| What the landing/health server (bound on `$PORT` when set and
distinct from the swe-swe-server bind) renders.

In tunnel mode this is the only port a PaaS exposes to the public
internet. The PaaS healthcheck hits `GET /` and gets a 200 even
when the tunnel is reconnecting -- the status banner reflects the
reconnect separately, so transient drops don't flap the healthcheck.

Variants:

  - `FullLanding`: `GET /` returns a small HTML page with the live
    tunnel URL linked (read from `liveTunnelHostname` -- updates
    per request to pick up label rotations live). `GET /health`
    returns 200.
  - `DisabledLanding`: `SWE_LANDING_DISABLE=1` -- every path
    returns 200 OK with no body. PaaS healthchecks pass, but
    nothing about swe-swe leaks to scanners hitting the landing
    port.
  - `NoLanding`: `$PORT` not set, or equal to the swe-swe-server
    bind. No separate landing server runs; the swe-swe-server
    listener handles `$PORT` requests directly (which the tunnel
    client never reaches in tunnel mode, since it dials loopback).

Source: `listen.go` (`startLandingServer`).

-}
type LandingServerMode
    = FullLanding
    | DisabledLanding
    | NoLanding
