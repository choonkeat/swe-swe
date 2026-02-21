module PreviewIframe exposing
    ( ShellPageEffect(..), onPageLoad, onUrlChange, onNavStateChange
    , ShellPageAction(..), onShellCommand
    , InjectEffect(..), onConsole, onError, onRejection, onFetch, onXhr, onWsUpgrade
    , InjectAction(..), onInjectCommand
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
import Domain exposing (Timestamp(..), Url(..))



-- ── Shell page (WS 5) ──────────────────────────────────────────


{-| Effects produced by the shell page — sends debug messages to the hub.
-}
type ShellPageEffect
    = ShellSend DebugMsg


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
    | IgnoredShellCommand



{- shell page silently drops commands it doesn't handle (e.g. Query) -}


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

        IframeQuery _ ->
            IgnoredShellCommand



-- ── inject.js (WS 6) ───────────────────────────────────────────


{-| Effects produced by inject.js — sends telemetry messages to the hub.
-}
type InjectEffect
    = InjectSend DebugMsg


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
onFetch : FetchResult -> InjectEffect
onFetch result =
    InjectSend (Fetch result)


{-| XMLHttpRequest info.
-}
onXhr : XhrResult -> InjectEffect
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
    | IgnoredCommand


{-| Handle a command from the hub on WS 6.
inject.js handles DOM queries; ignores navigation/reload.
-}
onInjectCommand : IframeCommand -> InjectAction
onInjectCommand cmd =
    case cmd of
        IframeQuery payload ->
            RunQuery payload

        IframeNavigate _ ->
            IgnoredCommand

        IframeReload ->
            IgnoredCommand
