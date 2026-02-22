module HttpProxy exposing
    ( agentChatPort, previewProxyPort, agentChatProxyPort
    , ProxyMode(..), ProbeResult(..), classifyProbe, PlaceholderDismiss(..)
    )

{-| HTTP Proxy Architecture — port derivation, dual-mode routing, and readiness probing.

Two reverse proxy chains sit between browser and backend apps. Each is
reachable via **two modes** — the browser auto-selects at runtime:

    Port-based (preferred, per-origin isolation):
      Preview:     Browser → Traefik :23000 → swe-swe-server :23000 → User app :3000
      Agent Chat:  Browser → Traefik :24000 → swe-swe-server :24000 → MCP sidecar :4000

    Path-based (fallback, when per-port listeners are unreachable):
      Preview:     Browser → Traefik :1977 → swe-swe-server :9898 /proxy/{uuid}/preview/    → User app :3000
      Agent Chat:  Browser → Traefik :1977 → swe-swe-server :9898 /proxy/{uuid}/agentchat/  → MCP sidecar :4000

Both modes reach the **same proxy instance** — a path-based proxy and a
port-based proxy share a single DebugHub per session. The port-based
proxy uses an empty BasePath (no URL rewriting needed); the path-based
proxy uses `/proxy/{uuid}/preview` as BasePath for URL rewriting.

Preview proxy: hosted inside swe-swe-server as an embedded Go library
(`github.com/choonkeat/agent-reverse-proxy`). Each session gets two
instances that share a DebugHub.

Agent Chat proxy: hosted inside swe-swe-server as a handler that
forwards HTTP and relays WebSocket upgrades. Cookie Domain/Secure
stripping, no HTML injection. Port-based and path-based reach the
same `agentChatProxyHandler`.

CORS: Port-based is cross-origin (browser on :1977, proxy on :23000),
so per-port handlers are wrapped in `corsWrapper` that sets
`Access-Control-Allow-Origin`, `-Credentials`, `-Methods`, `-Headers`.
Path-based is same-origin — no CORS needed.

Both proxies set `X-Agent-Reverse-Proxy` on every response (including 502),
so browser probes can detect when the proxy handler is active.

Port derivation:

    agentChatPort     = previewPort + 1000          (e.g., 4000)
    previewProxyPort  = previewPort + portOffset     (e.g., 23000)
    agentChatProxyPort = agentChatPort + portOffset  (e.g., 24000)

AI agents always use the internal path-based URL (container-internal,
never through Traefik), so they work regardless of browser mode.

@docs agentChatPort, previewProxyPort, agentChatProxyPort
@docs ProxyMode, ProbeResult, classifyProbe, PlaceholderDismiss

-}

import Domain exposing (AgentChatPort(..), AgentChatProxyPort(..), PreviewPort(..), PreviewProxyPort(..), ProxyPortOffset(..))



-- ── Port derivation ──────────────────────────────────────────────


{-| Agent chat app port = preview port + 1000.

    agentChatPort (PreviewPort 3000) == AgentChatPort 4000

-}
agentChatPort : PreviewPort -> AgentChatPort
agentChatPort (PreviewPort p) =
    AgentChatPort (p + 1000)


{-| Per-port proxy listener port for preview.

    previewProxyPort (ProxyPortOffset 20000) (PreviewPort 3000) == PreviewProxyPort 23000

-}
previewProxyPort : ProxyPortOffset -> PreviewPort -> PreviewProxyPort
previewProxyPort (ProxyPortOffset offset) (PreviewPort p) =
    PreviewProxyPort (offset + p)


{-| Per-port proxy listener port for agent chat.

    agentChatProxyPort (ProxyPortOffset 20000) (AgentChatPort 4000) == AgentChatProxyPort 24000

-}
agentChatProxyPort : ProxyPortOffset -> AgentChatPort -> AgentChatProxyPort
agentChatProxyPort (ProxyPortOffset offset) (AgentChatPort p) =
    AgentChatProxyPort (offset + p)



-- ── Proxy mode ─────────────────────────────────────────────────


{-| Which proxy mode the browser is using for a session.

The browser discovers this via a two-phase probe:

1.  **Path probe** — `probeUntilReady(pathBasedUrl)`. Checks if the proxy
    handler is active (target app may or may not be up). Retries with
    exponential backoff. If this fails, neither mode works yet.

2.  **Port probe** — once path probe succeeds, single fetch to port-based
    URL (e.g., `https://hostname:23000/`). If `X-Agent-Reverse-Proxy`
    header present → PortBased. Otherwise → PathBased.

The decided mode is stored for the session. All subsequent URL
construction (iframe src, debug WebSocket, agent chat) follows the
chosen mode consistently.

Why path-first: the path-based URL is always reachable (main server port).
By probing it first, we know the proxy handler exists. The port probe then
just checks reachability of the dedicated port.

-}
type ProxyMode
    = PortBased
      {- Per-port listener (e.g., :23000). Cross-origin with main UI
         (browser on :1977, proxy on :23000), so CORS headers are set
         by corsWrapper. Preferred for per-origin isolation.
      -}
    | PathBased



{- Path on main server port (e.g., /proxy/{uuid}/preview/ on :9898).
   Same-origin with main UI — no CORS needed.
   Fallback when per-port listeners are unreachable (firewall, etc.).
-}
-- ── Probe readiness ──────────────────────────────────────────────


{-| What a probe response tells us about the proxy chain.

The browser calls `probeUntilReady(url, { method: 'GET', ... })` — uses GET
(not the default HEAD) to avoid an iOS Safari CORS preflight bug.
Retries up to 10 times with exponential backoff (2 s → 30 s).
On each response, checks `resp.headers.has('X-Agent-Reverse-Proxy')`.

Path-based probes are same-origin — headers always readable. Port-based
probes are cross-origin, but `corsWrapper` sets `Access-Control-Expose-Headers`
so the `X-Agent-Reverse-Proxy` header is visible.

-}
type ProbeResult
    = ProxyReady
      {- Header present → our proxy handler is active.
         Status may be 200 (app up) or 502 (proxy's waiting page).
      -}
    | NotReady



{- Header absent → the session doesn't exist or proxy hasn't been set up yet. -}


{-| Classify a probe response by the presence of `X-Agent-Reverse-Proxy`.
-}
classifyProbe : { hasReverseProxyHeader : Bool } -> ProbeResult
classifyProbe { hasReverseProxyHeader } =
    if hasReverseProxyHeader then
        ProxyReady

    else
        NotReady



-- ── Placeholder dismissal ────────────────────────────────────────


{-| How a placeholder overlay gets dismissed after probe succeeds.

Both panels show a placeholder ("Connecting to preview/chat...") over the
iframe while probing. After probe success, `iframe.src` is set and the
placeholder waits for a dismissal event.

Preview has two paths; Agent Chat has one:

    Preview:      DebugWebSocket (urlchange | init)  — primary
                  IframeOnLoad                       — fallback

    Agent Chat:   IframeOnLoad                       — only path

Placeholder CSS: both share `.terminal-ui__iframe-placeholder`.
Preview scopes to `.terminal-ui__iframe-container .terminal-ui__iframe-placeholder`.
Agent Chat uses `.terminal-ui__agent-chat-placeholder`.

-}
type PlaceholderDismiss
    = DebugWebSocket
      {- Debug WS (WS 3/4) receives `urlchange` or `init` from
         agent-reverse-proxy. Only available for Preview — agent chat
         has no debug WS. Primary path: fires when proxied page loads.
      -}
    | IframeOnLoad



{- iframe.onload event. Used by both panels.
   For Preview: fallback if debug WS hasn't connected yet.
   For Agent Chat: the only dismissal path.
-}
