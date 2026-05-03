module DebugProtocol exposing
    ( ShellPageDebugMsg(..), ShellPageCommand(..)
    , InjectJsDebugMsg(..), InjectCommand(..), FetchResult(..), XhrResult(..)
    , AllDebugMsg(..), UiCommand(..), NavigateAction(..)
    )

{-| Debug channel protocol types, split by client.

Shell page (WS 5) handles navigation: page loads, URL changes, back/forward.
inject.js (WS 6) handles telemetry: console, errors, network, DOM queries.
AllDebugMsg is the aggregate that UI observers (terminal-ui on WS 3,4) receive.

Endpoints (path-based on swe-swe-server :1977, via embedded agent-reverse-proxy):

    /proxy/{uuid}/preview/__agent-reverse-proxy-debug__/ui        (WS 3,4)
    /proxy/{uuid}/preview/__agent-reverse-proxy-debug__/ws?role=shell   (WS 5)
    /proxy/{uuid}/preview/__agent-reverse-proxy-debug__/ws?role=inject  (WS 6)

All messages use JSON with a `t` (type) discriminator field.

@docs ShellPageDebugMsg, ShellPageCommand
@docs InjectJsDebugMsg, InjectCommand, FetchResult, XhrResult
@docs AllDebugMsg, UiCommand, NavigateAction

-}

import Domain exposing (Timestamp(..), Url(..))



-- -- Shell page protocol (WS 5) -------------------------------


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



-- -- inject.js protocol (WS 6) --------------------------------


{-| Messages sent by inject.js to the hub.

Telemetry: console output, errors, network activity, DOM query results,
and DOM action results (click / type / fillForm / pressKey / evaluate).

Action results echo back command `id` so callers can correlate request
and response. Wire `t` discriminator is camelCase for results
(`clickResult`, `typeResult`, etc.) and lowercase for telemetry
(`console`, `error`, `fetch`, `xhr`). `WsUpgrade` is the exception:
wire `t` is kebab-case `ws-upgrade`.

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

        {- JSON: "to" (upgraded wss:// URL); wire t = "ws-upgrade" (kebab-case). -}
        , ts : Timestamp
        }
    | ClickResult
        {- inject.go:357. Wire: { t: "clickResult", id, success, tag?, text?, error? }.
           success=true -> { tag, text }; success=false -> { error }.
        -}
        { id : String
        , result : Result { error : String } { tag : String, text : String }
        }
    | TypeResult
        {- inject.go:385. Wire: { t: "typeResult", id, success, value?, error? }.
           success=true -> { value }; success=false -> { error }.
           `value` is truncated to 500 chars by inject.js.
        -}
        { id : String
        , result : Result { error : String } { value : String }
        }
    | FillFormResult
        {- inject.go:411. Wire: { t: "fillFormResult", id,
           results: [{ selector, success, error? }] }.
           Each entry's result is independent -- partial failures possible.
           Outer envelope has no top-level success flag.
        -}
        { id : String
        , results :
            List
                { selector : String
                , result : Result { error : String } {}
                }
        }
    | PressKeyResult
        {- inject.go:420. Wire: { t: "pressKeyResult", id, success, key?, error? }.
           success=true -> { key }; success=false -> { error }.
        -}
        { id : String
        , result : Result { error : String } { key : String }
        }
    | EvaluateResult
        {- inject.go:427. Wire: { t: "evaluateResult", id, success, result?, error? }.
           success=true -> { result : <serialized JSON value> };
           success=false -> { error : String }.
           Wire `result` carries the JS `serialize(evalResult)` output
           (inject.go:280-303) -- can be objects, arrays, '[function]',
           or {name,message,stack} for Error -- modelled here as an
           opaque serialized form to match the wire's polymorphism.
        -}
        { id : String
        , result : Result { error : String } { serialized : String }
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
XHR `loadend` always fires -- even network failures report status 0.
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

The hub routes commands from terminal-ui (WS 3,4) to inject.js (WS 6).
See debughub.go RouteCommand: query, click, type, fillForm, pressKey, evaluate.

-}
type InjectCommand
    = DomQuery { id : String, selector : String }
    | DomClick { id : String, selector : String }
    | DomType { id : String, selector : Maybe String, text : String, clear : Bool, submit : Bool }
    | DomFillForm { id : String, fields : List { selector : String, value : String } }
    | DomPressKey { id : String, key : String, selector : Maybe String }
    | DomEvaluate { id : String, expression : String }



-- -- UI observer protocol (WS 3,4) ----------------------------


{-| Messages received by UI observers (terminal-ui) from the hub.
Aggregate of all sources: shell page, inject.js, and HTTP /open.
-}
type AllDebugMsg
    = FromShellPage ShellPageDebugMsg
    | FromInject InjectJsDebugMsg
    | Open { url : Url }


{-| Commands sent by terminal-ui to the hub (on WS 3,4).
The hub routes Navigate/Reload to the shell page (WS 5).
Query, Click, Type, FillForm, PressKey, Evaluate to inject.js (WS 6).
-}
type UiCommand
    = Navigate NavigateAction
    | Reload
    | Query { id : String, selector : String }
    | Click { id : String, selector : String }
    | Type { id : String, selector : Maybe String, text : String, clear : Bool, submit : Bool }
    | FillForm { id : String, fields : List { selector : String, value : String } }
    | PressKey { id : String, key : String, selector : Maybe String }
    | Evaluate { id : String, expression : String }


{-| Navigation direction.
-}
type NavigateAction
    = ToUrl Url
    | Back
    | Forward
