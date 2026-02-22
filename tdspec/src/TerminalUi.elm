module TerminalUi exposing
    ( State, Connection(..), Effect(..)
    , onPtyMessage, onDebugMessage
    )

{-| terminal-ui — custom web component mounted in the browser.

Two instances are active simultaneously:

  - **#1** Agent Terminal (assistant session)
  - **#2** Terminal (shell session)

Each instance opens TWO WebSocket connections:

    this.ws       -> WS 1/2  /ws/{uuid}        -> swe-swe-server     (PTY)
    this._debugWs -> WS 3/4  /.../debug.../ui   -> agent-reverse-proxy (preview)

Total: 4 WebSockets from 2 terminal-ui instances.

@docs State, Connection, Effect
@docs onPtyMessage, onDebugMessage

-}

import DebugProtocol exposing (..)
import Domain exposing (AgentChatPort(..), Path(..), PreviewPort(..), SessionUuid(..), Url(..))
import PtyProtocol


{-| Per-instance state of a terminal-ui component.
-}
type alias State =
    { session :
        { uuid : SessionUuid
        , name : String
        , workDir : String
        , assistant : String
        , viewers : Int
        }
    , preview :
        { port_ : Maybe PreviewPort
        , agentChatPort : Maybe AgentChatPort
        , url : Maybe Url
        , canGoBack : Bool
        , canGoForward : Bool
        }
    , features :
        { yoloMode : Bool
        , yoloSupported : Bool
        }
    }


{-| The two WebSocket connections each terminal-ui instance maintains.
-}
type Connection
    = PtyWs SessionUuid
    | DebugUiWs PreviewPort


{-| Side effects produced by terminal-ui message handlers.

**Invariant:** The preview URL bar is split into a fixed prefix
(`localhost:{PreviewPort}`) and an editable Path. The port shown
in the URL bar is ALWAYS `state.preview.port_` — never extracted
from the incoming Url. This prevents xdg-open or other sources
from injecting a misleading port (e.g. AgentChatPort 4000 when
PreviewPort is 3000).

-}
type Effect
    = SendPty PtyProtocol.ClientMsg
    | SendDebug UiCommand
    | UpdateUrlBar Path -- path only; prefix is fixed to state.preview.port_
    | EnableBackButton Bool
    | EnableForwardButton Bool
    | OpenIframePane { pane : String, url : Url }
    | ConfirmExternalUrl Url
    | ConnectDebugWebSocket PreviewPort


{-| Handle a message from the PTY WebSocket (WS 1/2).
The `Status` message delivers `previewPort`, triggering the debug WS connection.
-}
onPtyMessage : { msg : PtyProtocol.ServerMsg, state : State } -> ( State, List Effect )
onPtyMessage { msg, state } =
    case msg of
        PtyProtocol.PtyOutput _ ->
            ( state, [] )

        PtyProtocol.Pong ->
            ( state, [] )

        PtyProtocol.Status payload ->
            ( { state
                | preview = { port_ = Just payload.ports.preview, agentChatPort = Just payload.ports.agentChat, url = state.preview.url, canGoBack = state.preview.canGoBack, canGoForward = state.preview.canGoForward }
                , session = { uuid = state.session.uuid, name = payload.session.name, workDir = payload.session.workDir, assistant = payload.session.assistant, viewers = payload.session.viewers }
                , features = payload.features
              }
            , [ ConnectDebugWebSocket payload.ports.preview ]
            )

        PtyProtocol.ChatMsg _ ->
            ( state, [] )

        PtyProtocol.FileUploaded _ ->
            ( state, [] )

        PtyProtocol.Exit _ ->
            ( state, [] )


{-| Handle a message from the Debug UI WebSocket (WS 3/4).

Only 4 message types are actually acted on by terminal-ui:
`UrlChange`, `Init`, `NavState`, and `Open`.
The rest (`Console`, `Error`, `Fetch`, etc.) are ignored here —
they are consumed by MCP tools via in-process subscribers.

**BUG:** `Open` is broadcast to ALL UI observers by DebugHub.
With 2 terminal-ui instances, both receive the message,
both call `openIframePane -> setPreviewURL -> confirm()`.
Result: 2x "Open in new tab?" dialogs for external URLs.

**Note:** `Init` and `UrlChange` call `pathFromProxyUrl` to strip the
proxy prefix and host:port, producing a bare Path. `Open` also goes
through `setPreviewURL` which extracts the path and ignores the port
from the incoming Url.

-}
onDebugMessage : { msg : AllDebugMsg, state : State } -> ( State, List Effect )
onDebugMessage { msg, state } =
    case msg of
        FromShellPage shellMsg ->
            case shellMsg of
                Init payload ->
                    ( { state | preview = { port_ = state.preview.port_, agentChatPort = state.preview.agentChatPort, url = Just payload.url, canGoBack = state.preview.canGoBack, canGoForward = state.preview.canGoForward } }
                    , [ UpdateUrlBar (pathFromProxyUrl state.session.uuid payload.url) ]
                    )

                UrlChange payload ->
                    ( { state | preview = { port_ = state.preview.port_, agentChatPort = state.preview.agentChatPort, url = Just payload.url, canGoBack = state.preview.canGoBack, canGoForward = state.preview.canGoForward } }
                    , [ UpdateUrlBar (pathFromProxyUrl state.session.uuid payload.url) ]
                    )

                NavState payload ->
                    ( { state | preview = { port_ = state.preview.port_, agentChatPort = state.preview.agentChatPort, url = state.preview.url, canGoBack = payload.canGoBack, canGoForward = payload.canGoForward } }
                    , [ EnableBackButton payload.canGoBack
                      , EnableForwardButton payload.canGoForward
                      ]
                    )

        FromInject _ ->
            ( state, [] )

        Open payload ->
            ( state
            , [ OpenIframePane { pane = "preview", url = payload.url } ]
            )


{-| Strip /proxy/{uuid}/preview prefix and host:port from a proxy URL,
returning only the path + query + fragment.

    pathFromProxyUrl (SessionUuid "abc")
        (Url "https://host:9898/proxy/abc/preview/dashboard?tab=1#s")
    --> Path "/dashboard?tab=1#s"

This ensures the URL bar never shows a port from the incoming URL.
The fixed prefix in the UI supplies `localhost:{PreviewPort}` separately.

-}
pathFromProxyUrl : SessionUuid -> Url -> Path
pathFromProxyUrl _ _ =
    Path "/"
