module Topology exposing (Process(..), WebSocketConnection(..), HttpEndpoint(..), fullTopology)

{-| System topology — all processes, connections, and message flows.

Enumerates every WebSocket and HTTP endpoint when 2 terminal-ui
instances + Preview tab are active.

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
    | +------+ | | POST/open <-- 7 HTTP     | |  |
    |          | +---------------------------+ |  |
    |          +-------------------------------+  |
    |                                             |
    |          +--------------+                   |
    |          | swe-swe-open |-- 7 HTTP /open -> |
    |          | (CLI shim)   |                   |
    |          +--------------+         Container |
    +==============================================+

@docs Process, WebSocketConnection, HttpEndpoint, fullTopology

-}

import DebugHub
import DebugProtocol exposing (..)
import Domain exposing (PreviewPort(..), SessionUuid(..), Url(..))
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


{-| Full topology with 2 terminals + preview active.
6 WebSockets + 1 HTTP endpoint.
-}
fullTopology : { websockets : List WebSocketConnection, http : List HttpEndpoint }
fullTopology =
    let
        agentTerminal =
            BrowserTerminalUi { label = "Agent Terminal", sessionUuid = SessionUuid "uuid1" }

        shellTerminal =
            BrowserTerminalUi { label = "Terminal", sessionUuid = SessionUuid "uuid2" }

        sink _ =
            ()
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
        [ -- 7: Open shim HTTP POST
          OpenEndpoint
            { from = ContainerOpenShim
            , to = ContainerAgentReverseProxy
            , method = "POST"
            , path = "/__agent-reverse-proxy-debug__/open"
            }
        ]
    }
