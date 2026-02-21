module HttpProxy exposing
    ( agentChatPort
    , ProbeResult(..), classifyProbe, PlaceholderDismiss(..)
    )

{-| HTTP Proxy Architecture — port derivation and readiness probing.

Two reverse proxy chains sit between browser and backend apps,
both path-based on the main server port (:9898):

    Preview:     Browser → swe-swe-server :9898 /proxy/{uuid}/preview/    → User app :3000
    Agent Chat:  Browser → swe-swe-server :9898 /proxy/{uuid}/agentchat/  → MCP sidecar :4000

Preview proxy: hosted inside swe-swe-server as an embedded Go library
(`github.com/choonkeat/agent-reverse-proxy`). Each session gets a proxy
instance at `/proxy/{session-uuid}/preview/...`.

Agent Chat proxy: hosted inside swe-swe-server as a path-based handler
at `/proxy/{session-uuid}/agentchat/...`. Simple HTTP forwarding with
cookie Domain/Secure stripping. No HTML injection or debug scripts.

Both proxies are same-origin with the main UI — no CORS needed.

AI agents communicate with the preview proxy via a lightweight stdio bridge
process (`npx @choonkeat/agent-reverse-proxy --bridge`).

Both proxies set `X-Agent-Reverse-Proxy` on every response (including 502),
so browser probes can detect when the proxy handler is active.

Port derivation:

    agentChatPort = previewPort + 1000  (e.g., 4000)

Only 1 port goes through Traefik (the main server port). No per-session
Traefik entrypoints, ports, or routers are needed.

@docs agentChatPort
@docs ProbeResult, classifyProbe, PlaceholderDismiss

-}

import Domain exposing (AgentChatPort(..), PreviewPort(..))



-- ── Port derivation ──────────────────────────────────────────────


{-| Agent chat app port = preview port + 1000.

    agentChatPort (PreviewPort 3000) == AgentChatPort 4000

-}
agentChatPort : PreviewPort -> AgentChatPort
agentChatPort (PreviewPort p) =
    AgentChatPort (p + 1000)



-- ── Probe readiness ──────────────────────────────────────────────


{-| What a probe response tells us about the proxy chain.

The browser calls `probeUntilReady(url, { method: 'GET', ... })` — uses GET
(not the default HEAD) to avoid an iOS Safari CORS preflight bug.
Retries up to 10 times with exponential backoff (2 s → 30 s).
On each response, checks `resp.headers.has('X-Agent-Reverse-Proxy')`.

Since both proxies are now same-origin (path-based on the main server port),
the browser can always read response headers without CORS restrictions.

-}
type ProbeResult
    = ProxyReady
      {- Header present → our proxy handler is active.
         Status may be 200 (app up) or 502 (proxy's waiting page).
      -}
    | NotReady



{- Header absent → the session doesn't exist or proxy hasn't been set up yet.
-}


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
