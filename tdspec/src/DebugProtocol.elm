module DebugProtocol exposing
    ( DebugMsg(..)
    , FetchResult(..)
    , XhrResult(..)
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

@docs DebugMsg, FetchResult, XhrResult
@docs UiCommand, IframeCommand, NavigateAction

-}

import Domain exposing (Timestamp(..), Url(..))


{-| Messages flowing through the debug channel.

Iframe clients (WS 5,6) produce all variants EXCEPT `Open`.
UI observers (WS 3,4) receive all variants.
`Open` is injected by the HTTP `/open` endpoint only.

-}
type DebugMsg
    = UrlChange { url : Url, ts : Timestamp }
    | Init { url : Url, ts : Timestamp }
    | NavState { canGoBack : Bool, canGoForward : Bool }
    | Open { url : Url }
    | Console
        { m : String {- JSON: "m" is the console method name: "log"|"warn"|"error"|"info"|"debug" -}
        , args : List String
        , ts : Timestamp
        }
    | Error
        { msg : String {- JSON: "msg" (error message) -}
        , file : String {- JSON: "file" (source filename) -}
        , line : Int {- JSON: "line" (line number) -}
        , col : Int {- JSON: "col" (column number) -}
        , stack : Maybe String
        , ts : Timestamp
        }
    | Rejection { reason : String, ts : Timestamp }
    | Fetch FetchResult
    | Xhr XhrResult
    | QueryResult
        { id : String
        , found : Bool
        , text : Maybe String
        , html : Maybe String
        , visible : Bool
        , rect : Maybe { x : Float, y : Float, width : Float, height : Float }
        }
    | WsUpgrade
        { from : Url {- JSON: "from" (original ws:// URL) -}
        , to : Url {- JSON: "to" (upgraded wss:// URL) -}
        , ts : Timestamp
        }


{-| Fetch API result — success with status or failure with error.
-}
type FetchResult
    = FetchOk { url : Url, method : String, status : Int, ok : Bool, ms : Int, ts : Timestamp }
    | FetchFailed { url : Url, method : String, error : String, ms : Int, ts : Timestamp }


{-| XMLHttpRequest result — success with status or failure with error.
-}
type XhrResult
    = XhrOk { url : Url, method : String, status : Int, ok : Bool, ms : Int, ts : Timestamp }
    | XhrFailed { url : Url, method : String, error : String, ms : Int, ts : Timestamp }


{-| Commands sent by terminal-ui to DebugHub (on WS 3,4).
-}
type UiCommand
    = Navigate NavigateAction
    | Reload
    | Query { id : String, selector : String }


{-| Commands sent by DebugHub to iframe clients (on WS 5,6).
-}
type IframeCommand
    = IframeNavigate NavigateAction
    | IframeReload
    | IframeQuery { id : String, selector : String }


{-| Navigation sub-action shared between UI commands and iframe commands.
-}
type NavigateAction
    = ToUrl Url
    | Back
    | Forward
