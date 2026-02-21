module Topology exposing
    ( Process(..), TerminalUi(..), ShellPage(..), InjectJs(..), SweServer(..), AgentReverseProxy(..)
    , Traefik(..), OpenShim(..), UserApp(..), McpSidecar(..)
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

@docs Process, TerminalUi, ShellPage, InjectJs, SweServer, AgentReverseProxy
@docs Traefik, OpenShim, UserApp, McpSidecar
@docs WebSocketChannel, OpenEndpointHttp, PreviewProxyChain, AgentChatProxyChain
@docs fullTopology

-}

import DebugProtocol exposing (..)
import Domain exposing (AgentChatPort(..), AgentChatProxyPort(..), PreviewPort(..), PreviewProxyPort(..), SessionUuid(..))
import HttpProxy exposing (PortOffset(..))
import PtyProtocol



-- ── Process types ──────────────────────────────────────────────


{-| A terminal-ui web component instance in the browser.
-}
type TerminalUi
    = TerminalUi { label : String, sessionUuid : SessionUuid }


{-| Shell page — outer wrapper in Preview iframe, manages navigation (WS 5).
-}
type ShellPage
    = ShellPage


{-| inject.js — injected into every proxied HTML page, captures telemetry (WS 6).
-}
type InjectJs
    = InjectJs


{-| The swe-swe-server process (PTY host, port 3000).
-}
type SweServer
    = SweServer


{-| The agent-reverse-proxy process (debug/preview proxy).
-}
type AgentReverseProxy
    = AgentReverseProxy


{-| Traefik — host-level reverse proxy providing port-based routing and forwardauth.
-}
type Traefik
    = Traefik


{-| swe-swe-open — CLI shim that sends `HTTP GET /open?url=...` to agent-reverse-proxy.
-}
type OpenShim
    = OpenShim


{-| The user's application process (e.g., a dev server on port 3000).
-}
type UserApp
    = UserApp


{-| MCP sidecar process — agent chat backend on port `previewPort + 1000`.
-}
type McpSidecar
    = McpSidecar


{-| A process in the system — wraps all specific process types.
Location prefix (Browser/Container/Host) mirrors fullTopology nesting.
-}
type Process
    = BrowserTerminalUi TerminalUi
    | BrowserShellPage ShellPage
    | BrowserInjectJs InjectJs
    | ContainerSweServer SweServer
    | ContainerAgentReverseProxy AgentReverseProxy
    | ContainerOpenShim OpenShim
    | ContainerUserApp UserApp
    | ContainerMcpSidecar McpSidecar
    | HostTraefik Traefik



-- ── Connection types ───────────────────────────────────────────


{-| A WebSocket channel between two processes.

All type parameters are phantom — they exist only at the type level
to document which processes and message types are valid for each channel.


    ptyAgentTerminal :
        WebSocketChannel
            SweServer
            -- server
            PtyProtocol.ServerMsg
            -- serverMsg
            TerminalUi
            -- client
            PtyProtocol.ClientMsg

    -- clientMsg

-}
type WebSocketChannel server serverMsg client clientMsg
    = WebSocketChannel


{-| An HTTP endpoint exposed by a process.
-}
type alias OpenEndpointHttp =
    { method : String, path : String }


{-| Preview proxy chain: Browser → Traefik → agent-reverse-proxy → User app.

Separate process (`npx @choonkeat/agent-reverse-proxy`).
Injects debug scripts, provides DebugHub, serves shell page.

-}
type alias PreviewProxyChain =
    { listenPort : PreviewProxyPort, appPort : PreviewPort }


{-| Agent Chat proxy chain: Browser → Traefik → swe-swe-server → MCP sidecar.

Built into swe-swe-server (`handleProxyRequest`).
Cookie stripping + CORS headers, no HTML injection.

-}
type alias AgentChatProxyChain =
    { listenPort : AgentChatProxyPort, appPort : AgentChatPort }



-- ── Full topology ──────────────────────────────────────────────


{-| Full topology with 2 terminals + preview active.
6 WebSockets + 1 HTTP endpoint + 2 HTTP reverse proxy chains.
-}
fullTopology :
    { browser :
        { agentTerminal : TerminalUi
        , terminal : TerminalUi
        , shellPage : ShellPage
        , injectJs : InjectJs
        }
    , container :
        { sweServer : SweServer
        , agentReverseProxy : AgentReverseProxy
        , openShim : OpenShim
        , userApp : UserApp
        , mcpSidecar : McpSidecar
        }
    , host :
        { traefik : Traefik
        }
    , channels :
        { ptyAgentTerminal :
            WebSocketChannel
                SweServer
                -- server
                PtyProtocol.ServerMsg
                -- serverMsg
                TerminalUi
                -- client
                PtyProtocol.ClientMsg

        -- clientMsg
        , ptyTerminal :
            WebSocketChannel
                SweServer
                -- server
                PtyProtocol.ServerMsg
                -- serverMsg
                TerminalUi
                -- client
                PtyProtocol.ClientMsg

        -- clientMsg
        , debugUiAgentTerminal :
            WebSocketChannel
                AgentReverseProxy
                -- server
                AllDebugMsg
                -- serverMsg
                TerminalUi
                -- client
                UiCommand

        -- clientMsg
        , debugUiTerminal :
            WebSocketChannel
                AgentReverseProxy
                -- server
                AllDebugMsg
                -- serverMsg
                TerminalUi
                -- client
                UiCommand

        -- clientMsg
        , debugIframeShellPage :
            WebSocketChannel
                AgentReverseProxy
                -- server
                ShellPageCommand
                -- serverMsg
                ShellPage
                -- client
                ShellPageDebugMsg

        -- clientMsg
        , debugIframeInjectJs :
            WebSocketChannel
                AgentReverseProxy
                -- server
                InjectCommand
                -- serverMsg
                InjectJs
                -- client
                InjectJsDebugMsg

        -- clientMsg
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
            HttpProxy.previewProxyPort { offset = offset, appPort = previewPort }

        acProxyPort =
            HttpProxy.agentChatProxyPort { offset = offset, appPort = acPort }
    in
    { browser =
        { agentTerminal = TerminalUi { label = "Agent Terminal", sessionUuid = SessionUuid "uuid1" }
        , terminal = TerminalUi { label = "Terminal", sessionUuid = SessionUuid "uuid2" }
        , shellPage = ShellPage
        , injectJs = InjectJs
        }
    , container =
        { sweServer = SweServer
        , agentReverseProxy = AgentReverseProxy
        , openShim = OpenShim
        , userApp = UserApp
        , mcpSidecar = McpSidecar
        }
    , host =
        { traefik = Traefik
        }
    , channels =
        { ptyAgentTerminal = WebSocketChannel
        , ptyTerminal = WebSocketChannel
        , debugUiAgentTerminal = WebSocketChannel
        , debugUiTerminal = WebSocketChannel
        , debugIframeShellPage = WebSocketChannel
        , debugIframeInjectJs = WebSocketChannel
        }
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
