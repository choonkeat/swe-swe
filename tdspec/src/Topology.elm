module Topology exposing
    ( Process(..), TerminalUi(..), ShellPage(..), InjectJs(..), SweServer(..)
    , Traefik(..), OpenShim(..), UserApp(..), McpSidecar(..), StdioBridge(..)
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
    |  +----------------------------------------------------+  |
    |  |              swe-swe-server :9898                   |  |
    |  |  PTY host (WS1, WS2)                               |  |
    |  |  +----------------------------------------------+  |  |
    |  |  |  Preview proxy (/proxy/{uuid}/preview/...)   |  |  |
    |  |  |  DebugHub (UI obs WS3,WS4 / iframe WS5,WS6) |  |  |
    |  |  |  GET/open <-- HTTP                           |  |  |
    |  |  +----------------------------------------------+  |  |
    |  +----------------------------------------------------+  |
    |                                                          |
    |  +------------------+    +----------------------------+  |
    |  | swe-swe-open     |    | stdio bridge               |  |
    |  | (CLI shim)       |    | (agent-reverse-proxy       |  |
    |  | HTTP → server    |    |  --bridge → server /mcp)   |  |
    |  +------------------+    +----------------------------+  |
    |                                                Container |
    +==========================================================+

    HTTP Proxy Chains

    Preview (path-based, single port):
    Browser → swe-swe-server :9898 /proxy/{uuid}/preview/ → User app :3000

    Agent Chat (port-based, via Traefik):
    Browser → Traefik :24000 → swe-swe-server :24000 → MCP sidecar :4000

    Preview proxy is hosted inside swe-swe-server as a Go library
    (github.com/choonkeat/agent-reverse-proxy). Each session gets a
    proxy instance at /proxy/{session-uuid}/preview/...

    AI agents communicate with the preview proxy via a lightweight
    stdio bridge process (npx @choonkeat/agent-reverse-proxy --bridge).

Note: agent-reverse-proxy also exposes a vestigial
`/__agent-reverse-proxy-debug__/agent` WS endpoint.
It is unused — swe-swe-server uses in-process subscribers instead.

@docs Process, TerminalUi, ShellPage, InjectJs, SweServer
@docs Traefik, OpenShim, UserApp, McpSidecar, StdioBridge
@docs WebSocketChannel, OpenEndpointHttp, PreviewProxyChain, AgentChatProxyChain
@docs fullTopology

-}

import DebugProtocol exposing (..)
import Domain exposing (AgentChatPort(..), AgentChatProxyPort(..), PreviewPort(..), SessionUuid(..))
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


{-| The swe-swe-server process (PTY host + preview proxy, port 9898).

Hosts the preview proxy as an embedded Go library — each session gets
a proxy instance at `/proxy/{session-uuid}/preview/...`.
-}
type SweServer
    = SweServer


{-| Traefik — host-level reverse proxy providing port-based routing and forwardauth.
-}
type Traefik
    = Traefik


{-| swe-swe-open — CLI shim that sends `HTTP GET /open?url=...` to the preview proxy
endpoint on swe-swe-server (`/proxy/{uuid}/preview/__agent-reverse-proxy-debug__/open`).
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


{-| Stdio bridge — lightweight relay between AI agent (stdio MCP) and
swe-swe-server's preview proxy HTTP MCP endpoint.

Spawned as: `npx @choonkeat/agent-reverse-proxy --bridge http://swe-swe:3000/proxy/$SESSION_UUID/preview/mcp`
-}
type StdioBridge
    = StdioBridge


{-| A process in the system — wraps all specific process types.
Location prefix (Browser/Container/Host) mirrors fullTopology nesting.
-}
type Process
    = BrowserTerminalUi TerminalUi
    | BrowserShellPage ShellPage
    | BrowserInjectJs InjectJs
    | ContainerSweServer SweServer
    | ContainerOpenShim OpenShim
    | ContainerUserApp UserApp
    | ContainerMcpSidecar McpSidecar
    | ContainerStdioBridge StdioBridge
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


{-| Preview proxy chain: Browser → swe-swe-server /proxy/{uuid}/preview/ → User app.

Hosted inside swe-swe-server as an embedded Go library (no separate process).
Injects debug scripts, provides DebugHub, serves shell page.
Path-based routing on the main server port (:9898).

-}
type alias PreviewProxyChain =
    { basePath : String, appPort : PreviewPort }


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
        , openShim : OpenShim
        , userApp : UserApp
        , mcpSidecar : McpSidecar
        , stdioBridge : StdioBridge
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
                SweServer
                -- server (preview proxy hosted in swe-swe-server)
                AllDebugMsg
                -- serverMsg
                TerminalUi
                -- client
                UiCommand

        -- clientMsg
        , debugUiTerminal :
            WebSocketChannel
                SweServer
                -- server (preview proxy hosted in swe-swe-server)
                AllDebugMsg
                -- serverMsg
                TerminalUi
                -- client
                UiCommand

        -- clientMsg
        , debugIframeShellPage :
            WebSocketChannel
                SweServer
                -- server (preview proxy hosted in swe-swe-server)
                ShellPageCommand
                -- serverMsg
                ShellPage
                -- client
                ShellPageDebugMsg

        -- clientMsg
        , debugIframeInjectJs :
            WebSocketChannel
                SweServer
                -- server (preview proxy hosted in swe-swe-server)
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
        , openShim = OpenShim
        , userApp = UserApp
        , mcpSidecar = McpSidecar
        , stdioBridge = StdioBridge
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
        , path = "/proxy/{uuid}/preview/__agent-reverse-proxy-debug__/open"
        }
    , previewProxy =
        { basePath = "/proxy/{uuid}/preview"
        , appPort = previewPort
        }
    , agentChatProxy =
        { listenPort = acProxyPort
        , appPort = acPort
        }
    }
