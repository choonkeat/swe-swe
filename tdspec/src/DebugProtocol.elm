module DebugProtocol exposing
    ( ShellPageDebugMsg(..), ShellPageCommand(..)
    , InjectJsDebugMsg(..), InjectCommand(..), FetchResult(..), XhrResult(..)
    , AllDebugMsg(..), UiCommand(..), NavigateAction(..)
    )

{-| Debug channel protocol types, split by client.

Shell page (WS 5) handles navigation: page loads, URL changes, back/forward.
inject.js (WS 6) handles telemetry: console, errors, network, DOM queries.
AllDebugMsg is the aggregate that UI observers (terminal-ui on WS 3,4) receive.

Endpoints (on agent-reverse-proxy at `:PROXY_PORT_OFFSET+port`):

    /__agent-reverse-proxy-debug__/ui   -- terminal-ui connects here  (WS 3,4)
    /__agent-reverse-proxy-debug__/ws   -- shell page & inject.js     (WS 5,6)

All messages use JSON with a `t` (type) discriminator field.

@docs ShellPageDebugMsg, ShellPageCommand
@docs InjectJsDebugMsg, InjectCommand, FetchResult, XhrResult
@docs AllDebugMsg, UiCommand, NavigateAction

-}

import Domain exposing (Timestamp(..), Url(..))



-- ── Shell page protocol (WS 5) ───────────────────────────────


{-| Messages sent by the shell page to the hub.
Navigation-related: page loads, URL changes, back/forward state.
-}
type ShellPageDebugMsg
    = Init { url : Url, ts : Timestamp }
    | UrlChange { url : Url, ts : Timestamp }
    | NavState { canGoBack : Bool, canGoForward : Bool }


{-| Commands sent by the hub to the shell page.
-}
type ShellPageCommand
    = ShellNavigate NavigateAction
    | ShellReload



-- ── inject.js protocol (WS 6) ────────────────────────────────


{-| Messages sent by inject.js to the hub.
Telemetry: console output, errors, network activity, DOM query results.
-}
type InjectJsDebugMsg
    = Console
        { m : String

        {- JSON: "m" is the console method name: "log"|"warn"|"error"|"info"|"debug" -}
        , args : List String
        , ts : Timestamp
        }
    | Error
        { msg : String

        {- JSON: "msg" (error message) -}
        , file : String

        {- JSON: "file" (source filename) -}
        , line : Int

        {- JSON: "line" (line number) -}
        , col : Int

        {- JSON: "col" (column number) -}
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
        { from : Url

        {- JSON: "from" (original ws:// URL) -}
        , to : Url

        {- JSON: "to" (upgraded wss:// URL) -}
        , ts : Timestamp
        }


{-| Result of a Fetch API call.
Success carries status + ok; failure (network error, abort) carries error string.
-}
type FetchResult
    = FetchResult
        { request :
            { url : Url
            , method : String
            , ms : Int
            , ts : Timestamp
            }
        , response :
            Result
                { error : String }
                { status : Int, ok : Bool }
        }


{-| Result of an XMLHttpRequest.
XHR `loadend` always fires — even network failures report status 0.
There is no error branch; ok = status >= 200 && status < 300.
-}
type XhrResult
    = XhrResult
        { request :
            { url : Url
            , method : String
            , ms : Int
            , ts : Timestamp
            }
        , status : Int
        , ok : Bool
        }


{-| Commands sent by the hub to inject.js.
-}
type InjectCommand
    = DomQuery { id : String, selector : String }



-- ── UI observer protocol (WS 3,4) ────────────────────────────


{-| Messages received by UI observers (terminal-ui) from the hub.
Aggregate of all sources: shell page, inject.js, and HTTP /open.
-}
type AllDebugMsg
    = FromShellPage ShellPageDebugMsg
    | FromInject InjectJsDebugMsg
    | Open { url : Url }


{-| Commands sent by terminal-ui to the hub (on WS 3,4).
The hub routes Navigate/Reload to the shell page, Query to inject.js.
-}
type UiCommand
    = Navigate NavigateAction
    | Reload
    | Query { id : String, selector : String }


{-| Navigation direction.
-}
type NavigateAction
    = ToUrl Url
    | Back
    | Forward
