module PreviewIframe exposing
    ( ShellPageEffect(..), onPageLoad, onUrlChange, onNavStateChange
    , ShellPageAction(..), onShellCommand
    , InjectEffect(..), onConsole, onError, onRejection, onFetch, onXhr, onWsUpgrade
    , InjectAction(..), onInjectCommand
    )

{-| Preview iframe — the shell page and inject.js inside the Preview tab.

Two processes connect via WS 5,6:

  - **shell page** (`shellPageHTML`) — outer wrapper managing back/forward nav
  - **inject.js** (`debugInjectJS`) — injected into every proxied HTML page

Endpoint: `/__agent-reverse-proxy-debug__/ws` on agent-reverse-proxy.

Each client sends and receives only its own message types:

  - Shell page sends `ShellPageDebugMsg`, receives `ShellPageCommand`
  - inject.js sends `InjectJsDebugMsg`, receives `InjectCommand`

@docs ShellPageEffect, onPageLoad, onUrlChange, onNavStateChange
@docs ShellPageAction, onShellCommand
@docs InjectEffect, onConsole, onError, onRejection, onFetch, onXhr, onWsUpgrade
@docs InjectAction, onInjectCommand

-}

import DebugProtocol exposing (..)
import Domain exposing (Timestamp(..), Url(..))



-- ── Shell page (WS 5) ──────────────────────────────────────────


{-| Effects produced by the shell page — sends navigation messages to the hub.
-}
type ShellPageEffect
    = ShellSend ShellPageDebugMsg


{-| On page load, the shell page sends an `Init` message with the current URL.
-}
onPageLoad : { url : Url, now : Timestamp } -> ShellPageEffect
onPageLoad payload =
    ShellSend (Init { url = payload.url, ts = payload.now })


{-| On URL change (pushState/popstate/hashchange), sends `UrlChange`.
-}
onUrlChange : { url : Url, now : Timestamp } -> ShellPageEffect
onUrlChange payload =
    ShellSend (UrlChange { url = payload.url, ts = payload.now })


{-| On navigation state change, sends `NavState` with back/forward availability.
-}
onNavStateChange : { canGoBack : Bool, canGoForward : Bool } -> ShellPageEffect
onNavStateChange payload =
    ShellSend (NavState payload)


{-| Actions the shell page takes when it receives a command from the hub.
-}
type ShellPageAction
    = NavigateIframe NavigateAction
    | ReloadIframe


{-| Handle a command from the hub on WS 5.
-}
onShellCommand : ShellPageCommand -> ShellPageAction
onShellCommand cmd =
    case cmd of
        ShellNavigate action ->
            NavigateIframe action

        ShellReload ->
            ReloadIframe



-- ── inject.js (WS 6) ───────────────────────────────────────────


{-| Effects produced by inject.js — sends telemetry messages to the hub.
-}
type InjectEffect
    = InjectSend InjectJsDebugMsg


{-| Captured console output (log/warn/error/info/debug).
-}
onConsole : { m : String, args : List String, ts : Timestamp } -> InjectEffect
onConsole payload =
    InjectSend (Console payload)


{-| Uncaught error.
-}
onError : { msg : String, file : String, line : Int, col : Int, stack : Maybe String, ts : Timestamp } -> InjectEffect
onError payload =
    InjectSend (Error payload)


{-| Unhandled promise rejection.
-}
onRejection : { reason : String, ts : Timestamp } -> InjectEffect
onRejection payload =
    InjectSend (Rejection payload)


{-| Fetch request/response info.
-}
onFetch : HttpResult -> InjectEffect
onFetch result =
    InjectSend (Fetch result)


{-| XMLHttpRequest info.
-}
onXhr : HttpResult -> InjectEffect
onXhr result =
    InjectSend (Xhr result)


{-| Notification that a `ws://` URL was auto-upgraded to `wss://`.
-}
onWsUpgrade : { from : Url, to : Url, ts : Timestamp } -> InjectEffect
onWsUpgrade payload =
    InjectSend (WsUpgrade payload)


{-| Actions inject.js takes when it receives a command from the hub.
-}
type InjectAction
    = RunQuery { id : String, selector : String }


{-| Handle a command from the hub on WS 6.
-}
onInjectCommand : InjectCommand -> InjectAction
onInjectCommand cmd =
    case cmd of
        DomQuery payload ->
            RunQuery payload
