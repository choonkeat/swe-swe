module DebugHub exposing (Effect(..), onIframeMessage, onOpenRequest, onUiCommand)

{-| DebugHub — the message router inside agent-reverse-proxy.

Manages three subscriber pools:

  - **UI observers:** terminal-ui instances connected via `/ui` (WS 3,4)
  - **Iframe clients:** shell page + inject.js connected via `/ws` (WS 5,6)
  - **Agent client:** swe-swe-server MCP tools via `/agent` (backward compat)

Routing rules:

    Source               -> Destination
    Iframe sends msg     -> ForwardToAgent + fan-out to UI observers
    HTTP POST /open      -> SendToUIObservers only
    UI observer sends    -> ForwardToIframes

@docs Effect, onIframeMessage, onOpenRequest, onUiCommand

-}

import DebugProtocol exposing (..)
import Domain exposing (Url(..))


{-| Side effects produced by the DebugHub routing logic.
-}
type Effect
    = ForwardToAgent DebugMsg
    | FanOutToUiObservers DebugMsg
    | ForwardToIframes IframeCommand


{-| When an iframe client (WS 5,6) sends a message, the hub
forwards it to the agent AND fans it out to all UI observers.
-}
onIframeMessage : DebugMsg -> List Effect
onIframeMessage msg =
    [ ForwardToAgent msg
    , FanOutToUiObservers msg
    ]


{-| When the swe-swe-open CLI shim hits `HTTP POST /open?url=...`,
the hub broadcasts an `Open` message to all UI observers.
This is the source of the duplicate-prompt bug — all terminal-ui
instances receive and handle it.
-}
onOpenRequest : { url : Url } -> List Effect
onOpenRequest payload =
    [ FanOutToUiObservers (Open { url = payload.url }) ]


{-| When a UI observer (WS 3,4) sends a command, the hub
forwards it to all iframe clients.
-}
onUiCommand : UiCommand -> List Effect
onUiCommand cmd =
    case cmd of
        Navigate action ->
            [ ForwardToIframes (IframeNavigate action) ]

        Reload ->
            [ ForwardToIframes IframeReload ]
