module DebugHub exposing (Effect(..), onShellPageMessage, onInjectMessage, onOpenRequest, onUiCommand)

{-| DebugHub — the message router inside agent-reverse-proxy.

Manages three subscriber pools:

  - **UI observers:** terminal-ui instances connected via `/ui` (WS 3,4)
  - **Shell page:** connected via `/ws` (WS 5)
  - **inject.js:** connected via `/ws` (WS 6)

Routing rules:

    Source               -> Destination
    Shell page sends     -> BroadcastToUiObservers (FromShellPage msg)
    inject.js sends      -> BroadcastToUiObservers (FromInject msg)
    HTTP GET /open       -> SendToUiObserversOnly (Open)
    UI Navigate/Reload   -> ForwardToShellPage
    UI Query             -> ForwardToInject

@docs Effect, onShellPageMessage, onInjectMessage, onOpenRequest, onUiCommand

-}

import DebugProtocol exposing (..)
import Domain exposing (Url(..))


{-| Side effects produced by the DebugHub routing logic.
-}
type Effect
    = BroadcastToUiObservers AllDebugMsg
      {- Go's BroadcastFromIframe sends to two destinations:
         1. All UI observers
         2. All in-process subscribers (MCP tools in swe-swe-server)
      -}
    | SendToUiObserversOnly AllDebugMsg
    | ForwardToShellPage ShellPageCommand
    | ForwardToInject InjectCommand


{-| When the shell page (WS 5) sends a message, the hub
broadcasts it to all UI observers as a AllDebugMsg.
-}
onShellPageMessage : ShellPageDebugMsg -> List Effect
onShellPageMessage msg =
    [ BroadcastToUiObservers (FromShellPage msg) ]


{-| When inject.js (WS 6) sends a message, the hub
broadcasts it to all UI observers as a AllDebugMsg.
-}
onInjectMessage : InjectJsDebugMsg -> List Effect
onInjectMessage msg =
    [ BroadcastToUiObservers (FromInject msg) ]


{-| When the swe-swe-open CLI shim hits `HTTP GET /open?url=...`,
the hub broadcasts an `Open` message to all UI observers.
This is the source of the duplicate-prompt bug — all terminal-ui
instances receive and handle it.
-}
onOpenRequest : { url : Url } -> List Effect
onOpenRequest payload =
    [ SendToUiObserversOnly (Open { url = payload.url }) ]


{-| When a UI observer (WS 3,4) sends a command, the hub
routes it to the appropriate iframe client:
Navigate/Reload → shell page, Query → inject.js.
-}
onUiCommand : UiCommand -> List Effect
onUiCommand cmd =
    case cmd of
        Navigate action ->
            [ ForwardToShellPage (ShellNavigate action) ]

        Reload ->
            [ ForwardToShellPage ShellReload ]

        Query payload ->
            [ ForwardToInject (DomQuery payload) ]
