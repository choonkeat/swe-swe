module TerminalUi exposing
    ( State, Connection(..), Effect(..)
    , onPtyMessage, onDebugMessage
    , WsChannel(..), WsPhase(..), WsEvent(..), wsTransition, wsConfig
    , PlaceholderPhase(..), PlaceholderEvent(..), placeholderTransition
    , PendingNav(..)
    )

{-| terminal-ui — custom web component mounted in the browser.

Two instances are active simultaneously:

  - **#1** Agent Terminal (assistant session)
  - **#2** Terminal (shell session)

Each instance opens TWO WebSocket connections:

    this.ws       -> WS 1/2  /ws/{uuid}        -> swe-swe-server     (PTY)
    this._debugWs -> WS 3/4  /.../debug.../ui   -> agent-reverse-proxy (preview)

Total: 4 WebSockets from 2 terminal-ui instances.

@docs State, Connection, Effect
@docs onPtyMessage, onDebugMessage
@docs WsChannel, WsPhase, WsEvent, wsTransition, wsConfig
@docs PlaceholderPhase, PlaceholderEvent, placeholderTransition
@docs PendingNav

-}

import DebugProtocol exposing (..)
import Domain exposing (AgentChatPort(..), Path(..), PreviewPort(..), SessionUuid(..), Url(..))
import PtyProtocol


{-| Per-instance state of a terminal-ui component.
-}
type alias State =
    { session :
        { uuid : SessionUuid
        , name : String
        , workDir : String
        , assistant : String
        , viewers : Int
        }
    , preview :
        { port_ : Maybe PreviewPort
        , agentChatPort : Maybe AgentChatPort
        , url : Maybe Url
        , canGoBack : Bool
        , canGoForward : Bool
        }
    , features :
        { yoloMode : Bool
        , yoloSupported : Bool
        }
    }


{-| The two WebSocket connections each terminal-ui instance maintains.
-}
type Connection
    = PtyWs SessionUuid
    | DebugUiWs PreviewPort


{-| Side effects produced by terminal-ui message handlers.

**Invariant:** The preview URL bar is split into a fixed prefix
(`localhost:{PreviewPort}`) and an editable Path. The port shown
in the URL bar is ALWAYS `state.preview.port_` — never extracted
from the incoming Url. This prevents xdg-open or other sources
from injecting a misleading port (e.g. AgentChatPort 4000 when
PreviewPort is 3000).

-}
type Effect
    = SendPty PtyProtocol.ClientMsg
    | SendDebug UiCommand
    | UpdateUrlBar Path -- path only; prefix is fixed to state.preview.port_
    | EnableBackButton Bool
    | EnableForwardButton Bool
    | OpenIframePane { pane : String, url : Url }
    | ConfirmExternalUrl Url
    | ConnectDebugWebSocket PreviewPort


{-| Handle a message from the PTY WebSocket (WS 1/2).
The `Status` message delivers `previewPort`, triggering the debug WS connection.
-}
onPtyMessage : { msg : PtyProtocol.ServerMsg, state : State } -> ( State, List Effect )
onPtyMessage { msg, state } =
    case msg of
        PtyProtocol.PtyOutput _ ->
            ( state, [] )

        PtyProtocol.Pong ->
            ( state, [] )

        PtyProtocol.Status payload ->
            ( { state
                | preview = { port_ = Just payload.ports.preview, agentChatPort = Just payload.ports.agentChat, url = state.preview.url, canGoBack = state.preview.canGoBack, canGoForward = state.preview.canGoForward }
                , session = { uuid = state.session.uuid, name = payload.session.name, workDir = payload.session.workDir, assistant = payload.session.assistant, viewers = payload.session.viewers }
                , features = payload.features
              }
            , [ ConnectDebugWebSocket payload.ports.preview ]
            )

        PtyProtocol.ChatMsg _ ->
            ( state, [] )

        PtyProtocol.FileUploaded _ ->
            ( state, [] )

        PtyProtocol.Exit _ ->
            ( state, [] )


{-| Handle a message from the Debug UI WebSocket (WS 3/4).

Only 4 message types are actually acted on by terminal-ui:
`UrlChange`, `Init`, `NavState`, and `Open`.
The rest (`Console`, `Error`, `Fetch`, etc.) are ignored here —
they are consumed by MCP tools via in-process subscribers.

**BUG:** `Open` is broadcast to ALL UI observers by DebugHub.
With 2 terminal-ui instances, both receive the message,
both call `openIframePane -> setPreviewURL -> confirm()`.
Result: 2x "Open in new tab?" dialogs for external URLs.

**Note:** `Init` and `UrlChange` call `pathFromProxyUrl` to strip the
proxy prefix and host:port, producing a bare Path. `Open` also goes
through `setPreviewURL` which extracts the path and ignores the port
from the incoming Url.

-}
onDebugMessage : { msg : AllDebugMsg, state : State } -> ( State, List Effect )
onDebugMessage { msg, state } =
    case msg of
        FromShellPage shellMsg ->
            case shellMsg of
                Init payload ->
                    ( { state | preview = { port_ = state.preview.port_, agentChatPort = state.preview.agentChatPort, url = Just payload.url, canGoBack = state.preview.canGoBack, canGoForward = state.preview.canGoForward } }
                    , [ UpdateUrlBar (pathFromProxyUrl state.session.uuid payload.url) ]
                    )

                UrlChange payload ->
                    ( { state | preview = { port_ = state.preview.port_, agentChatPort = state.preview.agentChatPort, url = Just payload.url, canGoBack = state.preview.canGoBack, canGoForward = state.preview.canGoForward } }
                    , [ UpdateUrlBar (pathFromProxyUrl state.session.uuid payload.url) ]
                    )

                NavState payload ->
                    ( { state | preview = { port_ = state.preview.port_, agentChatPort = state.preview.agentChatPort, url = state.preview.url, canGoBack = payload.canGoBack, canGoForward = payload.canGoForward } }
                    , [ EnableBackButton payload.canGoBack
                      , EnableForwardButton payload.canGoForward
                      ]
                    )

        FromInject _ ->
            ( state, [] )

        Open payload ->
            ( state
            , [ OpenIframePane { pane = "preview", url = payload.url } ]
            )


{-| Strip /proxy/{uuid}/preview prefix and host:port from a proxy URL,
returning only the path + query + fragment.

    pathFromProxyUrl (SessionUuid "abc")
        (Url "https://host:9898/proxy/abc/preview/dashboard?tab=1#s")
    --> Path "/dashboard?tab=1#s"

    pathFromProxyUrl (SessionUuid "abc")
        (Url "http://localhost:23000/dashboard?tab=1#s")
    --> Path "/dashboard?tab=1#s"

    pathFromProxyUrl (SessionUuid "abc")
        (Url "https://host:9898/proxy/abc/preview/")
    --> Path "/"

    pathFromProxyUrl (SessionUuid "abc")
        (Url "https://host:9898/proxy/abc/preview")
    --> Path "/"

Port-based URLs have no proxy prefix, so the pathname passes through unchanged.
Path-based URLs have the prefix stripped. In both cases, the scheme, host, and
port are discarded — the UI supplies `localhost:{PreviewPort}` separately.

Mirrors JS: `terminal-ui.js` `pathFromProxyUrl(proxyUrl)`.

-}
pathFromProxyUrl : SessionUuid -> Url -> Path
pathFromProxyUrl (SessionUuid uuid) (Url raw) =
    let
        -- Strip scheme + authority: everything after "://" up to the first "/"
        afterAuthority =
            case splitOnce "://" raw of
                Just ( _, rest ) ->
                    case splitOnce "/" rest of
                        Just ( _, pathAndMore ) ->
                            "/" ++ pathAndMore

                        Nothing ->
                            "/"

                Nothing ->
                    -- No scheme — treat the whole string as a path
                    raw

        -- Separate path from query+fragment
        ( pathname, queryAndFragment ) =
            splitPathFromQueryFragment afterAuthority

        -- The proxy prefix to strip (path-based mode)
        prefix =
            "/proxy/" ++ uuid ++ "/preview"

        strippedPath =
            if String.startsWith prefix pathname then
                let
                    remainder =
                        String.dropLeft (String.length prefix) pathname
                in
                if remainder == "" then
                    "/"

                else
                    remainder

            else
                -- Port-based URL or no prefix — pass through
                pathname
    in
    Path (strippedPath ++ queryAndFragment)


{-| Split a string on the first occurrence of a separator.
-}
splitOnce : String -> String -> Maybe ( String, String )
splitOnce sep str =
    case String.indexes sep str of
        [] ->
            Nothing

        i :: _ ->
            Just
                ( String.left i str
                , String.dropLeft (i + String.length sep) str
                )


{-| Separate pathname from query string and fragment.

    splitPathFromQueryFragment "/foo?bar=1#baz"
    --> ( "/foo", "?bar=1#baz" )

    splitPathFromQueryFragment "/foo#baz?bar=1"
    --> ( "/foo", "#baz?bar=1" )

    splitPathFromQueryFragment "/foo"
    --> ( "/foo", "" )

-}
splitPathFromQueryFragment : String -> ( String, String )
splitPathFromQueryFragment str =
    let
        -- Find first '?' or '#' — whichever comes first
        qIdx =
            String.indexes "?" str |> List.head

        hIdx =
            String.indexes "#" str |> List.head

        splitIdx =
            case ( qIdx, hIdx ) of
                ( Just q, Just h ) ->
                    Just (min q h)

                ( Just q, Nothing ) ->
                    Just q

                ( Nothing, Just h ) ->
                    Just h

                ( Nothing, Nothing ) ->
                    Nothing
    in
    case splitIdx of
        Just i ->
            ( String.left i str, String.dropLeft i str )

        Nothing ->
            ( str, "" )



-- ── WebSocket reconnection state machine ───────────────────────


{-| Which WebSocket channel — each has different reconnect timing.
-}
type WsChannel
    = PtyChannel {- PTY WebSocket (WS 1/2). Backoff: 1s → 60s. -}
    | DebugChannel



{- Debug UI WebSocket (WS 3/4). Backoff: 1s → 10s.
   On reconnect: reloads iframe if placeholder still visible.
-}


{-| Reconnect timing per channel.

    wsConfig PtyChannel == { baseDelayMs = 1000, maxDelayMs = 60000 }

    wsConfig DebugChannel == { baseDelayMs = 1000, maxDelayMs = 10000 }

Delay for attempt N (0-indexed): `min(baseDelay * 2^N, maxDelay)`.
Sequence for PTY: 1s, 2s, 4s, 8s, 16s, 32s, 60s, 60s, ...
Sequence for Debug: 1s, 2s, 4s, 8s, 10s, 10s, ...

Mirrors JS: `reconnect.js` `createReconnectState()`.

-}
wsConfig : WsChannel -> { baseDelayMs : Int, maxDelayMs : Int }
wsConfig channel =
    case channel of
        PtyChannel ->
            { baseDelayMs = 1000, maxDelayMs = 60000 }

        DebugChannel ->
            { baseDelayMs = 1000, maxDelayMs = 10000 }


{-| WebSocket connection lifecycle.

    Disconnected 0
      → Connecting              (initial connect or reconnect after backoff)

    Connecting
      → Connected               (onopen fires)
      → Disconnected (N+1)      (onerror/onclose fires)

    Connected
      → Disconnected (N+1)      (onclose fires)

    ProcessExited               — terminal; no reconnection attempted

PTY-specific: if `processExited` flag is set when onclose fires,
transitions to `ProcessExited` instead of `Disconnected`.

Debug-specific: on `Connecting → Connected`, if placeholder is still
visible (`_previewWaiting`), reloads the iframe.

-}
type WsPhase
    = Disconnected Int {- Attempt counter (0 = never connected yet). Used to compute backoff delay. -}
    | Connecting
    | Connected
    | ProcessExited



{- Session ended; PTY WS will not reconnect. -}


{-| Events that drive WebSocket state transitions.
-}
type WsEvent
    = Connect {- Timer expired or initial connection attempt. -}
    | Opened {- WebSocket onopen fired. -}
    | Closed { processExited : Bool } {- WebSocket onclose fired. processExited flag prevents PTY reconnection. -}
    | Error



{- WebSocket onerror fired. Always followed by Closed in practice. -}


{-| Transition the WebSocket state machine.

    wsTransition Connect (Disconnected 0)
    --> Connecting

    wsTransition Opened Connecting
    --> Connected

    wsTransition (Closed { processExited = False }) Connected
    --> Disconnected 1

    wsTransition (Closed { processExited = True }) Connected
    --> ProcessExited

-}
wsTransition : WsEvent -> WsPhase -> WsPhase
wsTransition event phase =
    case ( event, phase ) of
        ( Connect, Disconnected _ ) ->
            Connecting

        ( Opened, Connecting ) ->
            Connected

        ( Closed { processExited }, Connected ) ->
            if processExited then
                ProcessExited

            else
                Disconnected 1

        ( Closed { processExited }, Connecting ) ->
            if processExited then
                ProcessExited

            else
                Disconnected 1

        ( Error, _ ) ->
            -- onerror is always followed by onclose; stay in current phase
            phase

        _ ->
            phase



-- ── Placeholder lifecycle ──────────────────────────────────────


{-| Placeholder overlay state machine for preview and agent chat panels.

    Hidden
      → Shown           (setPreviewURL() called, or agentChatPort received)

    Shown
      → Probing          (probe started via probeUntilReady())

    Probing
      → IframeSrcSet     (probe succeeded, iframe.src assigned)

    IframeSrcSet
      → Hidden           (dismissed via DebugWebSocket or IframeOnLoad)

    Preview can cycle: Hidden → Shown → ... → Hidden → Shown → ...
    (each setPreviewURL() call restarts the cycle).

    Agent Chat goes through the cycle once and stays Hidden.

The placeholder CSS class `hidden` is toggled. When visible, the
placeholder covers the iframe with a loading message.

-}
type PlaceholderPhase
    = Hidden
    | Shown {- Placeholder visible, probe not yet started. -}
    | Probing {- Probe running (see HttpProxy.ProbePhase for probe sub-state). -}
    | IframeSrcSet



{- iframe.src assigned, waiting for load/debug WS dismissal event. -}


{-| Events that drive placeholder transitions.
-}
type PlaceholderEvent
    = ShowPlaceholder {- setPreviewURL() called (preview) or agentChatPort received (agent chat). -}
    | ProbeStarted {- probeUntilReady() called. -}
    | ProbeSucceeded {- Proxy header detected; iframe.src will be set. -}
    | Dismissed



{- DebugWebSocket init/urlchange (preview primary) or iframe.onload (fallback / agent chat). -}


{-| Transition the placeholder state machine.

    placeholderTransition ShowPlaceholder Hidden
    --> Shown

    placeholderTransition ProbeStarted Shown
    --> Probing

    placeholderTransition ProbeSucceeded Probing
    --> IframeSrcSet

    placeholderTransition Dismissed IframeSrcSet
    --> Hidden

    placeholderTransition ShowPlaceholder Hidden
    --> Shown  (preview can restart the cycle)

-}
placeholderTransition : PlaceholderEvent -> PlaceholderPhase -> PlaceholderPhase
placeholderTransition event phase =
    case ( event, phase ) of
        ( ShowPlaceholder, Hidden ) ->
            Shown

        -- Preview restart: any phase can be interrupted by a new URL
        ( ShowPlaceholder, _ ) ->
            Shown

        ( ProbeStarted, Shown ) ->
            Probing

        ( ProbeSucceeded, Probing ) ->
            IframeSrcSet

        ( Dismissed, IframeSrcSet ) ->
            Hidden

        _ ->
            phase



-- ── Navigation queuing ─────────────────────────────────────────


{-| Pending navigation state for the preview tab.

When a URL arrives while the preview tab is not active (e.g., during
probe or while another tab is focused), the iframe src is stashed
rather than applied immediately.

    NoNav                — nothing pending
    Pending iframeSrc    — stashed; applied when user switches to preview tab

On tab switch to preview:

  - If `Pending src`: apply src immediately, transition to `NoNav`
  - If `NoNav`: call setPreviewURL(null) to start a fresh probe

Mirrors JS: `terminal-ui.js` `_pendingPreviewIframeSrc`.

-}
type PendingNav
    = NoNav
    | Pending String
