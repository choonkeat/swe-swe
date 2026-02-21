module HttpProxy exposing
    ( PortOffset(..), previewProxyPort, agentChatPort, agentChatProxyPort
    , ProbeResult(..), classifyProbe, PlaceholderDismiss(..)
    )

{-| HTTP Proxy Architecture — port derivation and readiness probing.

Two reverse proxy chains sit between browser and backend apps:

    Preview:     Browser → swe-swe-server :9898 /proxy/{uuid}/preview/ → User app :3000
    Agent Chat:  Browser → Traefik :24000 → swe-swe-server :24000 → MCP sidecar :4000

Preview proxy: hosted inside swe-swe-server as an embedded Go library
(`github.com/choonkeat/agent-reverse-proxy`). Each session gets a proxy
instance at `/proxy/{session-uuid}/preview/...`. Path-based routing on the
main server port — no separate process or dedicated port needed.

AI agents communicate with the preview proxy via a lightweight stdio bridge
process (`npx @choonkeat/agent-reverse-proxy --bridge`).

Agent Chat proxy: built into swe-swe-server (`handleProxyRequest`).
Simple HTTP forwarding with cookie Domain/Secure stripping and CORS
headers. No HTML injection or debug scripts.

Both proxies set `X-Agent-Reverse-Proxy` on every response (including 502),
so browser probes can distinguish "proxy up" from "Traefik 502."

Port derivation (default offset = 20000):

    previewProxyPort   = offset + previewPort          (e.g., 23000) — retained for port reservation
    agentChatPort      = previewPort + 1000            (e.g., 4000)
    agentChatProxyPort = offset + agentChatPort        (e.g., 24000)

Note: `previewProxyPort` is still used for port reservation
(`findAvailablePortPair`) but preview traffic now goes through the main
server port via path-based routing.

@docs PortOffset, previewProxyPort, agentChatPort, agentChatProxyPort
@docs ProbeResult, classifyProbe, PlaceholderDismiss

-}

import Domain exposing (AgentChatPort(..), AgentChatProxyPort(..), PreviewPort(..), PreviewProxyPort(..))



-- ── Port derivation ──────────────────────────────────────────────


{-| Offset added to app ports to derive proxy ports.
Default: 20000. Configurable via `swe-swe init --proxy-port-offset`.
-}
type PortOffset
    = PortOffset Int


{-| Preview proxy port = offset + preview app port.

    previewProxyPort { offset = PortOffset 20000, appPort = PreviewPort 3000 }
        == PreviewProxyPort 23000

-}
previewProxyPort : { offset : PortOffset, appPort : PreviewPort } -> PreviewProxyPort
previewProxyPort { offset, appPort } =
    let
        (PortOffset o) =
            offset

        (PreviewPort p) =
            appPort
    in
    PreviewProxyPort (o + p)


{-| Agent chat app port = preview port + 1000.

    agentChatPort (PreviewPort 3000) == AgentChatPort 4000

-}
agentChatPort : PreviewPort -> AgentChatPort
agentChatPort (PreviewPort p) =
    AgentChatPort (p + 1000)


{-| Agent chat proxy port = offset + agent chat app port.

    agentChatProxyPort { offset = PortOffset 20000, appPort = AgentChatPort 4000 }
        == AgentChatProxyPort 24000

-}
agentChatProxyPort : { offset : PortOffset, appPort : AgentChatPort } -> AgentChatProxyPort
agentChatProxyPort { offset, appPort } =
    let
        (PortOffset o) =
            offset

        (AgentChatPort p) =
            appPort
    in
    AgentChatProxyPort (o + p)



-- ── Probe readiness ──────────────────────────────────────────────


{-| What a probe response tells us about the proxy chain.

The browser calls `probeUntilReady(url, { method: 'GET', ... })` — uses GET
(not the default HEAD) to avoid an iOS Safari CORS preflight bug.
Retries up to 10 times with exponential backoff (2 s → 30 s).
On each response, checks `resp.headers.has('X-Agent-Reverse-Proxy')`.

-}
type ProbeResult
    = ProxyReady
      {- Header present → our proxy is running.
         Status may be 200 (app up) or 502 (proxy's waiting page).
      -}
    | TraefikDirect



{- Header absent → Traefik itself returned 502
   because the proxy process hasn't bound the port yet.
-}


{-| Classify a probe response by the presence of `X-Agent-Reverse-Proxy`.
-}
classifyProbe : { hasReverseProxyHeader : Bool } -> ProbeResult
classifyProbe { hasReverseProxyHeader } =
    if hasReverseProxyHeader then
        ProxyReady

    else
        TraefikDirect



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
