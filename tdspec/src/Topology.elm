module Topology exposing
    ( Process(..), TerminalUi(..), ShellPage(..), InjectJs(..), SweServer(..)
    , Traefik(..), OpenShim(..), UserApp(..), McpSidecar(..), StdioBridge(..)
    , WebSocketChannel(..), OpenEndpointHttp, PreviewProxyChain, AgentChatProxyChain
    , ProxyChains
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
    |  |              swe-swe-server :9898                  |  |
    |  |  PTY host (WS1, WS2)                               |  |
    |  |  +----------------------------------------------+  |  |
    |  |  |  Preview proxy (/proxy/{uuid}/preview/...)   |  |  |
    |  |  |  Agent chat proxy (/proxy/{uuid}/agentchat/) |  |  |
    |  |  |  DebugHub (UI obs WS3,WS4 / iframe WS5,WS6)  |  |  |
    |  |  |  GET/open <-- HTTP                           |  |  |
    |  |  +----------------------------------------------+  |  |
    |  +----------------------------------------------------+  |
    |                                                          |
    |  +------------------+    +----------------------------+  |
    |  | swe-swe-open     |    | stdio bridge               |  |
    |  | (CLI shim)       |    | (agent-reverse-proxy       |  |
    |  | HTTP → serve |    |  --bridge → server /mcp)
    |  +------------------+    +----------------------------+  |
    |                                                Container |
    +==========================================================+

    HTTP Proxy Chains (dual-mode: browser auto-selects)

    Port-based (preferred, per-origin isolation):
      Preview:     Browser → Traefik :23000 → swe-swe-server :23000 → User app :3000
      Agent Chat:  Browser → Traefik :24000 → swe-swe-server :24000 → MCP sidecar :4000

    Path-based (fallback, when per-port listeners are unreachable):
      Preview:     Browser → Traefik :1977 → swe-swe-server :9898 /proxy/{uuid}/preview/    → User app :3000
      Agent Chat:  Browser → Traefik :1977 → swe-swe-server :9898 /proxy/{uuid}/agentchat/  → MCP sidecar :4000

    Both modes reach the same proxy instance — path-based and port-based
    proxies share a single DebugHub per session. Port-based uses empty
    BasePath (no URL rewriting); path-based uses BasePath for rewriting.

    Port-based is cross-origin (browser on :1977, proxy on :23000) so
    per-port handlers are wrapped in corsWrapper. Path-based is same-origin.

    AI agents always use the internal path-based URL (container-internal,
    never through Traefik).

@docs Process, TerminalUi, ShellPage, InjectJs, SweServer
@docs Traefik, OpenShim, UserApp, McpSidecar, StdioBridge
@docs WebSocketChannel, OpenEndpointHttp, PreviewProxyChain, AgentChatProxyChain
@docs ProxyChains
@docs fullTopology

-}

import DebugProtocol exposing (..)
import Domain exposing (AgentChatPort(..), AgentChatProxyPort(..), PreviewPort(..), PreviewProxyPort(..), ProxyPortOffset(..), ServerAddr(..), SessionUuid(..))
import HttpProxy
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


{-| The swe-swe-server process (PTY host + preview/agentchat proxy).

Each session gets two proxy instances that share a DebugHub:

  - Path-based: `/proxy/{uuid}/preview/...`, `/proxy/{uuid}/agentchat/...` on :9898
  - Port-based: per-port listeners (e.g., :23000, :24000) with empty BasePath

Port-based listeners are wrapped in `corsWrapper` for cross-origin access.

-}
type SweServer
    = SweServer
        { addr : ServerAddr
        }


{-| Traefik — host-level reverse proxy providing forwardauth.

Routes the main server port (:9898) plus per-session proxy ports:

  - Main: :1977 → :9898 (path-based proxy, UI, WebSockets)
  - Preview: :23000 → :23000 (port-based proxy, up to 20 sessions)
  - Agent Chat: :24000 → :24000 (port-based proxy, up to 20 sessions)

Each per-port entrypoint gets its own Traefik router with forwardauth.

-}
type Traefik
    = Traefik


{-| swe-swe-open — CLI shim that sends `HTTP GET /open?url=...` to the preview proxy
endpoint on swe-swe-server (`/proxy/{uuid}/preview/__agent-reverse-proxy-debug__/open`).
-}
type OpenShim
    = OpenShim
        { target : ServerAddr
        }


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

Spawned as: `npx @choonkeat/agent-reverse-proxy --bridge http://localhost:9898/proxy/$SESSION_UUID/preview/mcp`

-}
type StdioBridge
    = StdioBridge
        { target : ServerAddr
        }


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
            -- server
            SweServer
            -- serverMsg
            PtyProtocol.ServerMsg
            -- client
            TerminalUi
            -- clientMsg
            PtyProtocol.ClientMsg

-}
type WebSocketChannel server serverMsg client clientMsg
    = WebSocketChannel


{-| An HTTP endpoint exposed by a process.
-}
type alias OpenEndpointHttp =
    { method : String, path : String }


{-| Preview proxy chain: Browser → proxy → User app.

Hosted inside swe-swe-server as an embedded Go library (no separate process).
Injects debug scripts, provides DebugHub, serves shell page.

-}
type alias PreviewProxyChain =
    { basePath : String
    , appPort : PreviewPort
    , proxyPort : PreviewProxyPort
    }


{-| Agent Chat proxy chain: Browser → proxy → MCP sidecar.

Hosted inside swe-swe-server. Cookie stripping, WebSocket relay, no HTML injection.

-}
type alias AgentChatProxyChain =
    { basePath : String
    , appPort : AgentChatPort
    , proxyPort : AgentChatProxyPort
    }


{-| Both proxy chains bundled, with the shared port offset.

Path-based and port-based proxies share a single DebugHub per session.
Path-based uses `basePath` for URL rewriting; port-based uses empty BasePath.

-}
type alias ProxyChains =
    { portOffset : ProxyPortOffset
    , preview : PreviewProxyChain
    , agentChat : AgentChatProxyChain
    }



-- ── Full topology ──────────────────────────────────────────────


{-| Full topology with 2 terminals + preview active.
6 WebSockets + 1 HTTP endpoint + 2 HTTP reverse proxy chains (dual-mode).
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
                -- server
                SweServer
                -- serverMsg
                PtyProtocol.ServerMsg
                -- client
                TerminalUi
                -- clientMsg
                PtyProtocol.ClientMsg
        , ptyTerminal :
            WebSocketChannel
                -- server
                SweServer
                -- serverMsg
                PtyProtocol.ServerMsg
                -- client
                TerminalUi
                -- clientMsg
                PtyProtocol.ClientMsg
        , debugUiAgentTerminal :
            WebSocketChannel
                -- server (preview proxy hosted in swe-swe-server)
                SweServer
                -- serverMsg
                AllDebugMsg
                -- client
                TerminalUi
                -- clientMsg
                UiCommand
        , debugUiTerminal :
            WebSocketChannel
                -- server (preview proxy hosted in swe-swe-server)
                SweServer
                -- serverMsg
                AllDebugMsg
                -- client
                TerminalUi
                -- clientMsg
                UiCommand
        , debugIframeShellPage :
            WebSocketChannel
                -- server (preview proxy hosted in swe-swe-server)
                SweServer
                -- serverMsg
                ShellPageCommand
                -- client
                ShellPage
                -- clientMsg
                ShellPageDebugMsg
        , debugIframeInjectJs :
            WebSocketChannel
                -- server (preview proxy hosted in swe-swe-server)
                SweServer
                -- serverMsg
                InjectCommand
                -- client
                InjectJs
                -- clientMsg
                InjectJsDebugMsg
        }
    , openEndpoint : OpenEndpointHttp
    , proxies : ProxyChains
    }
fullTopology =
    let
        previewPort =
            PreviewPort 3000

        acPort =
            HttpProxy.agentChatPort previewPort

        portOffset =
            ProxyPortOffset 20000

        serverAddr =
            ServerAddr
    in
    { browser =
        { agentTerminal = TerminalUi { label = "Agent Terminal", sessionUuid = SessionUuid "uuid1" }
        , terminal = TerminalUi { label = "Terminal", sessionUuid = SessionUuid "uuid2" }
        , shellPage = ShellPage
        , injectJs = InjectJs
        }
    , container =
        { sweServer = SweServer { addr = serverAddr }
        , openShim = OpenShim { target = serverAddr }
        , userApp = UserApp
        , mcpSidecar = McpSidecar
        , stdioBridge = StdioBridge { target = serverAddr }
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
    , proxies =
        { portOffset = portOffset
        , preview =
            { basePath = "/proxy/{uuid}/preview"
            , appPort = previewPort
            , proxyPort = HttpProxy.previewProxyPort portOffset previewPort
            }
        , agentChat =
            { basePath = "/proxy/{uuid}/agentchat"
            , appPort = acPort
            , proxyPort = HttpProxy.agentChatProxyPort portOffset acPort
            }
        }
    }
