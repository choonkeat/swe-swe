module PreviewIframe exposing
    ( ShellPageEffect(..)
    , ShellPageAction(..)
    , InjectEffect(..)
    , InjectAction(..)
    , onPageLoad
    , onUrlChange
    , onNavStateChange
    , onShellCommand
    , onConsole
    , onError
    , onRejection
    , onFetch
    , onXhr
    , onWsUpgrade
    , onInjectCommand
    )

{-| Preview iframe — the shell page and inject.js inside the Preview tab.

Two processes connect as "iframe clients" via WS 5,6:

  - **shell page** (`shellPageHTML`) — outer wrapper managing back/forward nav
  - **inject.js** (`debugInjectJS`) — injected into every proxied HTML page

Endpoint: `/__agent-reverse-proxy-debug__/ws` on agent-reverse-proxy.
Both register with DebugHub as iframe clients.

@docs ShellPageEffect, onPageLoad, onUrlChange, onNavStateChange
@docs ShellPageAction, onShellCommand
@docs InjectEffect, onConsole, onError, onRejection, onFetch, onXhr, onWsUpgrade
@docs InjectAction, onInjectCommand

-}

import DebugProtocol exposing (..)
import Domain exposing (Url(..))


-- ── Shell page (WS 5) ──────────────────────────────────────────


{-| Effects produced by the shell page — sends debug messages to the hub.
-}
type ShellPageEffect
    = ShellSend DebugMsg


{-| On page load, the shell page sends an `Init` message with the current URL.
-}
onPageLoad : Url -> ShellPageEffect
onPageLoad url =
    ShellSend (Init { url = url })


{-| On URL change (pushState/popstate/hashchange), sends `UrlChange`.
-}
onUrlChange : Url -> ShellPageEffect
onUrlChange url =
    ShellSend (UrlChange { url = url })


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
Shell page handles navigation and reload; ignores queries.
-}
onShellCommand : IframeCommand -> ShellPageAction
onShellCommand cmd =
    case cmd of
        IframeNavigate action ->
            NavigateIframe action

        IframeReload ->
            ReloadIframe

        Query _ ->
            ReloadIframe


-- ── inject.js (WS 6) ───────────────────────────────────────────


{-| Effects produced by inject.js — sends telemetry messages to the hub.
-}
type InjectEffect
    = InjectSend DebugMsg


{-| Captured console output (log/warn/error/info/debug).
-}
onConsole : ConsolePayload -> InjectEffect
onConsole payload =
    InjectSend (Console payload)


{-| Uncaught error.
-}
onError : { message : String, source : String, lineno : Int } -> InjectEffect
onError payload =
    InjectSend (Error payload)


{-| Unhandled promise rejection.
-}
onRejection : { reason : String } -> InjectEffect
onRejection payload =
    InjectSend (Rejection payload)


{-| Fetch request/response info.
-}
onFetch : { url : Url, method : String, status : Int } -> InjectEffect
onFetch payload =
    InjectSend (Fetch payload)


{-| XMLHttpRequest info.
-}
onXhr : { url : Url, method : String, status : Int } -> InjectEffect
onXhr payload =
    InjectSend (Xhr payload)


{-| Notification that a `ws://` URL was auto-upgraded to `wss://`.
-}
onWsUpgrade : { originalUrl : Url, upgradedUrl : Url } -> InjectEffect
onWsUpgrade payload =
    InjectSend (WsUpgrade payload)


{-| Actions inject.js takes when it receives a command from the hub.
-}
type InjectAction
    = RunQuery { selector : String }
    | IgnoredCommand


{-| Handle a command from the hub on WS 6.
inject.js handles DOM queries; ignores navigation/reload.
-}
onInjectCommand : IframeCommand -> InjectAction
onInjectCommand cmd =
    case cmd of
        Query payload ->
            RunQuery payload

        IframeNavigate _ ->
            IgnoredCommand

        IframeReload ->
            IgnoredCommand
