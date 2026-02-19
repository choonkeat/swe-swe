module TerminalUi exposing (State, Connection(..), Effect(..), onPtyMessage, onDebugMessage)

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
import Domain exposing (PreviewPort(..), SessionUuid(..), Url(..))
import PtyProtocol


{-| Per-instance state of a terminal-ui component.
-}
type alias State =
    { sessionUuid : SessionUuid
    , previewPort : Maybe PreviewPort
    , previewUrl : Maybe Url
    , canGoBack : Bool
    , canGoForward : Bool
    }


{-| The two WebSocket connections each terminal-ui instance maintains.
-}
type Connection
    = PtyWs SessionUuid
    | DebugUiWs PreviewPort


{-| Side effects produced by terminal-ui message handlers.
-}
type Effect
    = SendPty PtyProtocol.ClientMsg
    | SendDebug UiCommand
    | UpdateUrlBar Url
    | EnableBackButton Bool
    | EnableForwardButton Bool
    | OpenIframePane { pane : String, url : Url }
    | ConfirmExternalUrl Url
    | ConnectDebugWebSocket PreviewPort


{-| Handle a message from the PTY WebSocket (WS 1/2).
The `Status` message delivers `previewPort`, triggering the debug WS connection.
-}
onPtyMessage : PtyProtocol.ServerMsg -> State -> ( State, List Effect )
onPtyMessage msg state =
    case msg of
        PtyProtocol.PtyOutput _ ->
            ( state, [] )

        PtyProtocol.FileDownloadChunk _ ->
            ( state, [] )

        PtyProtocol.Pong ->
            ( state, [] )

        PtyProtocol.Status payload ->
            ( { state | previewPort = Just payload.previewPort }
            , [ ConnectDebugWebSocket payload.previewPort ]
            )

        PtyProtocol.ChatResponse _ ->
            ( state, [] )

        PtyProtocol.FileUploadAck _ ->
            ( state, [] )

        PtyProtocol.Exit ->
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

-}
onDebugMessage : DebugMsg -> State -> ( State, List Effect )
onDebugMessage msg state =
    case msg of
        UrlChange payload ->
            ( { state | previewUrl = Just payload.url }
            , [ UpdateUrlBar payload.url ]
            )

        Init payload ->
            ( { state | previewUrl = Just payload.url }
            , [ UpdateUrlBar payload.url ]
            )

        NavState payload ->
            ( { state | canGoBack = payload.canGoBack, canGoForward = payload.canGoForward }
            , [ EnableBackButton payload.canGoBack
              , EnableForwardButton payload.canGoForward
              ]
            )

        Open payload ->
            ( state
            , [ OpenIframePane { pane = "preview", url = payload.url } ]
            )

        Console _ ->
            ( state, [] )

        Error _ ->
            ( state, [] )

        Rejection _ ->
            ( state, [] )

        Fetch _ ->
            ( state, [] )

        Xhr _ ->
            ( state, [] )

        QueryResult _ ->
            ( state, [] )

        WsUpgrade _ ->
            ( state, [] )
