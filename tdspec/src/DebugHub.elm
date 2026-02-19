module DebugHub exposing (Effect(..), onIframeMessage, onOpenRequest, onUiCommand)

{-| DebugHub — the message router inside agent-reverse-proxy.

Manages three subscriber pools:

  - **UI observers:** terminal-ui instances connected via `/ui` (WS 3,4)
  - **Iframe clients:** shell page + inject.js connected via `/ws` (WS 5,6)
  - **Agent client:** vestigial `/agent` WS endpoint (usually nil, unused by swe-swe-server)

Routing rules:

    Source               -> Destination
    Iframe sends msg     -> BroadcastFromIframe (agent + UI observers + in-proc subscribers)
    HTTP GET /open       -> SendToUiObserversOnly
    UI observer sends    -> ForwardToIframes

@docs Effect, onIframeMessage, onOpenRequest, onUiCommand

-}

import DebugProtocol exposing (..)
import Domain exposing (Url(..))


{-| Side effects produced by the DebugHub routing logic.
-}
type Effect
    = BroadcastFromIframe DebugMsg
      {- Go's ForwardToAgent sends to three destinations:
         1. The agent WS conn (vestigial, usually nil)
         2. All UI observers
         3. All in-process subscribers (MCP tools in swe-swe-server)
      -}
    | SendToUiObserversOnly DebugMsg
    | ForwardToIframes IframeCommand


{-| When an iframe client (WS 5,6) sends a message, the hub
broadcasts it to agent + UI observers + in-process subscribers
via a single Go function call (ForwardToAgent).
-}
onIframeMessage : DebugMsg -> List Effect
onIframeMessage msg =
    [ BroadcastFromIframe msg ]


{-| When the swe-swe-open CLI shim hits `HTTP GET /open?url=...`,
the hub broadcasts an `Open` message to all UI observers.
This is the source of the duplicate-prompt bug — all terminal-ui
instances receive and handle it.
-}
onOpenRequest : { url : Url } -> List Effect
onOpenRequest payload =
    [ SendToUiObserversOnly (Open { url = payload.url }) ]


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

        Query payload ->
            [ ForwardToIframes (IframeQuery payload) ]
