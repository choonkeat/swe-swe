module Topology exposing
    ( Process(..), TerminalUi(..), IframeClient(..), SweServer(..), AgentReverseProxy(..), OpenShim(..), Traefik(..), UserApp(..), McpSidecar(..)
    , WebSocketChannel(..), OpenEndpointHttp, PreviewProxyChain, AgentChatProxyChain
    , fullTopology
    )

{-| System topology — all processes, connections, and message flows.

Enumerates every WebSocket and HTTP endpoint when 2 terminal-ui
instances + Preview tab are active.

    WebSocket Topology

    Browser
    +==========================================================+
    |                                                          |
    |  Agent Terminal     Terminal          Preview tab        |
    |  terminal-ui #1    terminal-ui #2    shell page          |
    |  ws + _debugWs     ws + _debugWs     inject.js           |
    |   |       |         |       |         |       |          |
    |  WS1    WS3       WS2    WS4       WS5     WS6           |
    |   |       |         |       |         |       |          |
    +===|=======|=========|=======|=========|=======|==========+
        |       |         |       |         |       |
    +===|=======|=========|=======|=========|=======|==========+
    |   v       v         v       v         v       v          |
    |  +--------+   +---------------------------------------+  |
    |  | swe-   |   |       agent-reverse-proxy             |  |
    |  | swe-   |   |  +---------------------------------+  |  |
    |  | server |   |  |           DebugHub              |  |  |
    |  | :3000  |   |  |  UI obs    <-- WS3, WS4         |  |  |
    |  |        |   |  |  iframe    <-- WS5, WS6         |  |  |
    |  |        |   |  |  GET/open  <-- HTTP             |  |  |
    |  +--------+   |  +---------------------------------+  |  |
    |               +---------------------------------------+  |
    |                                                          |
    |               +----------------+                         |
    |               | swe-swe-open   |--- HTTP /open --->      |
    |               | (CLI shim)     |                         |
    |               +----------------+               Container |
    +==========================================================+

    HTTP Proxy Chains (port-based, via Traefik)

    Browser           Traefik              Container proxy            Container backend
                    (forwardauth)
    terminal-ui  →  :23000  ──────→  agent-reverse-proxy :23000  →  User app :3000
    terminal-ui  →  :24000  ──────→  swe-swe-server      :24000  →  MCP sidecar :4000

    Traefik creates per-port entrypoints (20 preview + 20 agent chat = 40 ports).
    Each router applies forwardauth middleware for session cookie validation.

Note: agent-reverse-proxy also exposes a vestigial
`/__agent-reverse-proxy-debug__/agent` WS endpoint.
It is unused — swe-swe-server uses in-process subscribers instead.

@docs Process, TerminalUi, IframeClient, SweServer, AgentReverseProxy, OpenShim, Traefik, UserApp, McpSidecar
@docs WebSocketChannel, OpenEndpointHttp, PreviewProxyChain, AgentChatProxyChain
@docs fullTopology

-}

import DebugProtocol exposing (..)
import Domain exposing (AgentChatPort(..), PreviewPort(..), ProxyPort(..), SessionUuid(..))
import HttpProxy exposing (PortOffset(..))
import PtyProtocol



-- ── Process types ──────────────────────────────────────────────


{-| A terminal-ui web component instance in the browser.
-}
type TerminalUi
    = TerminalUi { label : String, sessionUuid : SessionUuid }


{-| An iframe client inside the Preview tab.
-}
type IframeClient
    = ShellPage
    | InjectJs


{-| The swe-swe-server process (PTY host, port 3000).
-}
type SweServer
    = SweServer


{-| The agent-reverse-proxy process (debug/preview proxy).
-}
type AgentReverseProxy
    = AgentReverseProxy


{-| The swe-swe-open CLI shim.
-}
type OpenShim
    = OpenShim


{-| Traefik reverse proxy on the host (forwardauth).
-}
type Traefik
    = Traefik


{-| The user's application process.
-}
type UserApp
    = UserApp


{-| The MCP sidecar process (agent chat backend).
-}
type McpSidecar
    = McpSidecar


{-| A process in the system — wraps all specific process types.
Used in the ASCII diagram to enumerate every participant.
-}
type Process
    = BrowserTerminalUi TerminalUi
    | BrowserIframeClient IframeClient
    | ContainerSweServer
    | ContainerAgentReverseProxy
    | ContainerOpenShim
    | HostTraefik
    | ContainerUserApp
    | ContainerMcpSidecar



-- ── Connection types ───────────────────────────────────────────


{-| A WebSocket channel between two processes.

The `protocol` type parameter is phantom — it exists only at the
type level to constrain which processes and message types are valid
for each channel. It is never constructed as a value.

    ptyAgentTerminal :
        WebSocketChannel
            { from : TerminalUi
            , to : SweServer
            , receives : PtyProtocol.ServerMsg
            , sends : PtyProtocol.ClientMsg
            }

-}
type WebSocketChannel protocol
    = WebSocketChannel { endpoint : String }


{-| An HTTP endpoint exposed by a process.
-}
type alias OpenEndpointHttp =
    { method : String, path : String }


{-| Preview proxy chain: Browser → Traefik → agent-reverse-proxy → User app.

Separate process (`npx @choonkeat/agent-reverse-proxy`).
Injects debug scripts, provides DebugHub, serves shell page.

-}
type alias PreviewProxyChain =
    { listenPort : ProxyPort, appPort : PreviewPort }


{-| Agent Chat proxy chain: Browser → Traefik → swe-swe-server → MCP sidecar.

Built into swe-swe-server (`handleProxyRequest`).
Cookie stripping + CORS headers, no HTML injection.

-}
type alias AgentChatProxyChain =
    { listenPort : ProxyPort, appPort : AgentChatPort }



-- ── Full topology ──────────────────────────────────────────────


{-| Full topology with 2 terminals + preview active.
6 WebSockets + 1 HTTP endpoint + 2 HTTP reverse proxy chains.
-}
fullTopology :
    { ptyAgentTerminal :
        WebSocketChannel
            { from : TerminalUi
            , to : SweServer
            , receives : PtyProtocol.ServerMsg
            , sends : PtyProtocol.ClientMsg
            }
    , ptyTerminal :
        WebSocketChannel
            { from : TerminalUi
            , to : SweServer
            , receives : PtyProtocol.ServerMsg
            , sends : PtyProtocol.ClientMsg
            }
    , debugUiAgentTerminal :
        WebSocketChannel
            { from : TerminalUi
            , to : AgentReverseProxy
            , receives : DebugMsg
            , sends : UiCommand
            }
    , debugUiTerminal :
        WebSocketChannel
            { from : TerminalUi
            , to : AgentReverseProxy
            , receives : DebugMsg
            , sends : UiCommand
            }
    , debugIframeShellPage :
        WebSocketChannel
            { from : IframeClient
            , to : AgentReverseProxy
            , receives : IframeCommand
            , sends : DebugMsg
            }
    , debugIframeInjectJs :
        WebSocketChannel
            { from : IframeClient
            , to : AgentReverseProxy
            , receives : IframeCommand
            , sends : DebugMsg
            }
    , openEndpoint : OpenEndpointHttp
    , previewProxy : PreviewProxyChain
    , agentChatProxy : AgentChatProxyChain
    }
fullTopology =
    let
        -- Port derivation example with default offset
        offset =
            PortOffset 20000

        previewPort =
            PreviewPort 3000

        acPort =
            HttpProxy.agentChatPort previewPort

        previewProxyPort =
            HttpProxy.previewProxyPort offset previewPort

        acProxyPort =
            HttpProxy.agentChatProxyPort offset acPort
    in
    { ptyAgentTerminal =
        WebSocketChannel { endpoint = "/ws/{uuid1}" }
    , ptyTerminal =
        WebSocketChannel { endpoint = "/ws/{uuid2}" }
    , debugUiAgentTerminal =
        WebSocketChannel { endpoint = "/__agent-reverse-proxy-debug__/ui" }
    , debugUiTerminal =
        WebSocketChannel { endpoint = "/__agent-reverse-proxy-debug__/ui" }
    , debugIframeShellPage =
        WebSocketChannel { endpoint = "/__agent-reverse-proxy-debug__/ws" }
    , debugIframeInjectJs =
        WebSocketChannel { endpoint = "/__agent-reverse-proxy-debug__/ws" }
    , openEndpoint =
        { method = "GET"
        , path = "/__agent-reverse-proxy-debug__/open"
        }
    , previewProxy =
        { listenPort = previewProxyPort
        , appPort = previewPort
        }
    , agentChatProxy =
        { listenPort = acProxyPort
        , appPort = acPort
        }
    }
