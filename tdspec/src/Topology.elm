module Topology exposing (Process(..), WebSocketConnection(..), HttpEndpoint(..), HttpProxyChain(..), fullTopology)

{-| System topology — all processes, connections, and message flows.

Enumerates every WebSocket and HTTP endpoint when 2 terminal-ui
instances + Preview tab are active.

    WebSocket Topology

    Browser
    +==============================================+
    |                                              |
    | Agent Terminal   Terminal     Preview tab     |
    | terminal-ui #1   terminal-ui  shell page     |
    | ws + _debugWs    #2           inject.js      |
    |  |       |       ws+_debugWs   |       |     |
    | WS1    WS3        |      |    WS5    WS6     |
    |  |       |       WS2   WS4    |       |     |
    +==|=======|========|=====|=====|=======|=====+
       |       |        |     |     |       |
    +==|=======|========|=====|=====|=======|=====+
    |  v       v        v     v     v       v     |
    | +------+ +-------------------------------+  |
    | |swe-  | |    agent-reverse-proxy        |  |
    | |swe-  | | +---------------------------+ |  |
    | |server| | |       DebugHub            | |  |
    | |:3000 | | | UI obs    <-- WS3, WS4   | |  |
    | |      | | | iframe    <-- WS5, WS6   | |  |
    | |      | | | GET/open  <-- 7 HTTP      | |  |
    | +------+ | +---------------------------+ |  |
    |          +-------------------------------+  |
    |                                             |
    |          +--------------+                   |
    |          | swe-swe-open |-- 7 HTTP /open -> |
    |          | (CLI shim)   |                   |
    |          +--------------+         Container |
    +==============================================+

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

@docs Process, WebSocketConnection, HttpEndpoint, HttpProxyChain, fullTopology

-}

import DebugHub
import DebugProtocol exposing (..)
import Domain exposing (AgentChatPort(..), PreviewPort(..), ProxyPort(..), SessionUuid(..), Url(..))
import HttpProxy exposing (PortOffset(..))
import PreviewIframe
import PtyProtocol
import TerminalUi


{-| A process in the system.
-}
type Process
    = BrowserTerminalUi { label : String, sessionUuid : SessionUuid }
    | BrowserShellPage
    | BrowserInjectJs
    | ContainerSweServer
    | ContainerAgentReverseProxy
    | ContainerOpenShim
    | HostTraefik
    | ContainerUserApp
    | ContainerMcpSidecar


{-| A WebSocket connection between two processes.
The `sends` and `receives` fields reference the protocol types,
anchoring each connection to its message schema.
-}
type WebSocketConnection
    = PtyWebSocket
        { from : Process
        , to : Process
        , endpoint : String
        , sends : PtyProtocol.ClientMsg -> ()
        , receives : PtyProtocol.ServerMsg -> ()
        }
    | DebugUiWebSocket
        { from : Process
        , to : Process
        , endpoint : String
        , sends : UiCommand -> ()
        , receives : DebugMsg -> ()
        }
    | DebugIframeWebSocket
        { from : Process
        , to : Process
        , endpoint : String
        , sends : DebugMsg -> ()
        , receives : IframeCommand -> ()
        }


{-| An HTTP endpoint between two processes.
-}
type HttpEndpoint
    = OpenEndpoint
        { from : Process
        , to : Process
        , method : String
        , path : String
        }


{-| An HTTP reverse proxy chain from browser to backend.

Each chain passes through Traefik (forwardauth) and a container-side
reverse proxy before reaching the backend app.

Preview proxy: separate process (`npx @choonkeat/agent-reverse-proxy`).
Injects debug scripts, provides DebugHub, serves shell page.

Agent Chat proxy: built into swe-swe-server (`handleProxyRequest`).
Cookie stripping + CORS headers, no HTML injection.

-}
type HttpProxyChain
    = PreviewProxy
        { from : Process
        , traefik : Process
        , proxy : Process
        , backend : Process
        , proxyPort : ProxyPort
        , appPort : PreviewPort
        }
    | AgentChatProxy
        { from : Process
        , traefik : Process
        , proxy : Process
        , backend : Process
        , proxyPort : ProxyPort
        , appPort : AgentChatPort
        }


{-| Full topology with 2 terminals + preview active.
4 WebSockets + 1 HTTP endpoint (+ 2 more WebSockets when Preview iframe active)

  - 2 HTTP reverse proxy chains through Traefik.

-}
fullTopology :
    { websockets : List WebSocketConnection
    , http : List HttpEndpoint
    , httpProxies : List HttpProxyChain
    }
fullTopology =
    let
        agentTerminal =
            BrowserTerminalUi { label = "Agent Terminal", sessionUuid = SessionUuid "uuid1" }

        shellTerminal =
            BrowserTerminalUi { label = "Terminal", sessionUuid = SessionUuid "uuid2" }

        sink _ =
            ()

        -- Port derivation example with default offset
        offset =
            PortOffset 20000

        previewPort =
            PreviewPort 3000

        acPort =
            HttpProxy.agentChatPort previewPort

        previewProxy =
            HttpProxy.previewProxyPort offset previewPort

        acProxy =
            HttpProxy.agentChatProxyPort offset acPort
    in
    { websockets =
        [ -- WS 1: PTY for Agent Terminal
          PtyWebSocket
            { from = agentTerminal
            , to = ContainerSweServer
            , endpoint = "/ws/{uuid1}"
            , sends = sink
            , receives = sink
            }

        -- WS 2: PTY for Terminal
        , PtyWebSocket
            { from = shellTerminal
            , to = ContainerSweServer
            , endpoint = "/ws/{uuid2}"
            , sends = sink
            , receives = sink
            }

        -- WS 3: Debug UI for Agent Terminal
        , DebugUiWebSocket
            { from = agentTerminal
            , to = ContainerAgentReverseProxy
            , endpoint = "/__agent-reverse-proxy-debug__/ui"
            , sends = sink
            , receives = sink
            }

        -- WS 4: Debug UI for Terminal
        , DebugUiWebSocket
            { from = shellTerminal
            , to = ContainerAgentReverseProxy
            , endpoint = "/__agent-reverse-proxy-debug__/ui"
            , sends = sink
            , receives = sink
            }

        -- WS 5: Shell page iframe client
        , DebugIframeWebSocket
            { from = BrowserShellPage
            , to = ContainerAgentReverseProxy
            , endpoint = "/__agent-reverse-proxy-debug__/ws"
            , sends = sink
            , receives = sink
            }

        -- WS 6: inject.js iframe client
        , DebugIframeWebSocket
            { from = BrowserInjectJs
            , to = ContainerAgentReverseProxy
            , endpoint = "/__agent-reverse-proxy-debug__/ws"
            , sends = sink
            , receives = sink
            }
        ]
    , http =
        [ -- 7: Open shim HTTP GET
          OpenEndpoint
            { from = ContainerOpenShim
            , to = ContainerAgentReverseProxy
            , method = "GET"
            , path = "/__agent-reverse-proxy-debug__/open"
            }
        ]
    , httpProxies =
        [ -- Preview: agent-reverse-proxy (separate npx process, MCP tool)
          PreviewProxy
            { from = agentTerminal
            , traefik = HostTraefik
            , proxy = ContainerAgentReverseProxy
            , backend = ContainerUserApp
            , proxyPort = previewProxy
            , appPort = previewPort
            }

        -- Agent Chat: swe-swe-server built-in proxy (handleProxyRequest)
        , AgentChatProxy
            { from = agentTerminal
            , traefik = HostTraefik
            , proxy = ContainerSweServer
            , backend = ContainerMcpSidecar
            , proxyPort = acProxy
            , appPort = acPort
            }
        ]
    }
