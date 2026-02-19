module DebugProtocol exposing
    ( DebugMsg(..)
    , ConsolePayload
    , ConsoleLevel(..)
    , UiCommand(..)
    , IframeCommand(..)
    , NavigateAction(..)
    )

{-| Debug channel protocol — shared across WS 3,4 (`/ui`) and WS 5,6 (`/ws`).

All messages use JSON with a `t` (type) discriminator field.
The agent-reverse-proxy DebugHub passes messages through without
transforming them — same JSON shape on both endpoints.

Endpoints (on agent-reverse-proxy at `:PROXY_PORT_OFFSET+port`):

    /__agent-reverse-proxy-debug__/ui   -- terminal-ui connects here  (WS 3,4)
    /__agent-reverse-proxy-debug__/ws   -- shell page & inject.js     (WS 5,6)

@docs DebugMsg, ConsolePayload, ConsoleLevel
@docs UiCommand, IframeCommand, NavigateAction

-}

import Domain exposing (Url(..))


{-| Messages flowing through the debug channel.

Iframe clients (WS 5,6) produce all variants EXCEPT `Open`.
UI observers (WS 3,4) receive all variants.
`Open` is injected by the HTTP `/open` endpoint only.

-}
type DebugMsg
    = UrlChange { url : Url }
    | Init { url : Url }
    | NavState { canGoBack : Bool, canGoForward : Bool }
    | Open { url : Url }
    | Console ConsolePayload
    | Error { message : String, source : String, lineno : Int }
    | Rejection { reason : String }
    | Fetch { url : Url, method : String, status : Int }
    | Xhr { url : Url, method : String, status : Int }
    | QueryResult { selector : String, html : String }
    | WsUpgrade { originalUrl : Url, upgradedUrl : Url }


{-| Console message captured by inject.js.
-}
type alias ConsolePayload =
    { level : ConsoleLevel
    , args : List String
    }


{-| Console severity level.
-}
type ConsoleLevel
    = Log
    | Warn
    | ErrorLevel
    | Info
    | Debug


{-| Commands sent by terminal-ui to DebugHub (on WS 3,4).
-}
type UiCommand
    = Navigate NavigateAction
    | Reload


{-| Commands sent by DebugHub to iframe clients (on WS 5,6).
-}
type IframeCommand
    = IframeNavigate NavigateAction
    | IframeReload
    | Query { selector : String }


{-| Navigation sub-action shared between UI commands and iframe commands.
-}
type NavigateAction
    = ToUrl Url
    | Back
    | Forward
