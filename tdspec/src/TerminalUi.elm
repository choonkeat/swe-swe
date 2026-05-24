module TerminalUi exposing
    ( State, Connection(..), Effect(..)
    , LayoutPreset(..), allLayoutPresets, presetSlots
    , Pane(..), allPanes
    , SlotId(..), SlotState
    , MobileView(..)
    , TabGesture(..), onTabGesture
    , onPtyMessage, onDebugMessage
    , WsChannel(..), WsPhase(..), WsEvent(..), wsTransition, wsConfig
    , PlaceholderPhase(..), PlaceholderEvent(..), placeholderTransition
    , PendingNav(..)
    )

{-| terminal-ui -- custom web component mounted in the browser.

Two instances are active simultaneously:

  - **#1** Agent Terminal (assistant session)
  - **#2** Terminal (shell session)

Each instance opens TWO WebSocket connections:

    this.ws       -> WS 1/2  /ws/{uuid}        -> swe-swe-server     (PTY)
    this._debugWs -> WS 3/4  /.../debug.../ui   -> agent-reverse-proxy (preview)

Total: 4 WebSockets from 2 terminal-ui instances.

The UI is a **layout preset grid** (8 presets, each carving the
viewport into 1-4 slots). Each slot is a mini multi-tab area: it has
its own tab bar with a `+` replace-menu, tracks an active pane
plus a tab list, and persists per-preset to `localStorage`. Panes
can be dragged-resized at the gutter (snap at 50% and device widths)
and popped out into a new browser tab via middle-click /
Ctrl-or-Cmd-click on the tab.

In tunnel mode the iframe URL builder switches from port-based
(`<host>:<previewProxyPort>/...`) to subdomain-based
(`<previewProxyPort>.<publicHostname>/...`); see `TunnelMode`.

@docs State, Connection, Effect
@docs LayoutPreset, allLayoutPresets, presetSlots
@docs Pane, allPanes
@docs SlotId, SlotState
@docs MobileView
@docs TabGesture, onTabGesture
@docs onPtyMessage, onDebugMessage
@docs WsChannel, WsPhase, WsEvent, wsTransition, wsConfig
@docs PlaceholderPhase, PlaceholderEvent, placeholderTransition
@docs PendingNav

-}

import DebugProtocol exposing (..)
import Domain exposing (AgentChatPort(..), AgentChatProxyPort(..), Path(..), PreviewPort(..), PreviewProxyPort(..), SessionUuid(..), Url(..), VncProxyPort(..))
import PtyProtocol
import TunnelMode exposing (PublicHostname(..), TunnelStatus)



-- -- Layout grid (preset / slot / pane) --------------------------


{-| The eight layout presets. Each preset specifies a slot list and
default pane assignments.

The icon column is omitted from the spec (purely visual --
`buildPresetIcon` in JS).

| Preset | Slots | Defaults |
|---|---|---|
| `Classic` | a, b | a=agent-terminal, b=preview |
| `Single` | a | a=preview |
| `TwoRow` | a, b | a=preview, b=agent-terminal |
| `LBigR` | a, b | a=agent-terminal, b=preview |
| `StackedR` | a, b, c | a=agent-terminal, b=preview, c=agent-chat |
| `StackedL` | a, b, c | a=agent-terminal, b=agent-chat, c=preview |
| `TSplitBot` | a, b, c | a=preview, b=agent-terminal, c=agent-chat |
| `Quadrants` | a, b, c, d | a=agent-terminal, b=preview, c=agent-chat, d=shell |

Source: `static/terminal-ui.js:41-48` (`LAYOUT_PRESETS`).

-}
type LayoutPreset
    = Classic
    | Single
    | TwoRow
    | LBigR
    | StackedR
    | StackedL
    | TSplitBot
    | Quadrants


{-| All eight presets in display order.
-}
allLayoutPresets : List LayoutPreset
allLayoutPresets =
    [ Classic, Single, TwoRow, LBigR, StackedR, StackedL, TSplitBot, Quadrants ]


{-| The slots a preset uses. Every preset uses `SlotA`; multi-slot
presets add `SlotB`/`SlotC`/`SlotD` in that order.

    presetSlots Single --> [ SlotA ]

    presetSlots Classic --> [ SlotA, SlotB ]

    presetSlots StackedR --> [ SlotA, SlotB, SlotC ]

    presetSlots Quadrants --> [ SlotA, SlotB, SlotC, SlotD ]

-}
presetSlots : LayoutPreset -> List SlotId
presetSlots preset =
    case preset of
        Classic ->
            [ SlotA, SlotB ]

        Single ->
            [ SlotA ]

        TwoRow ->
            [ SlotA, SlotB ]

        LBigR ->
            [ SlotA, SlotB ]

        StackedR ->
            [ SlotA, SlotB, SlotC ]

        StackedL ->
            [ SlotA, SlotB, SlotC ]

        TSplitBot ->
            [ SlotA, SlotB, SlotC ]

        Quadrants ->
            [ SlotA, SlotB, SlotC, SlotD ]


{-| Slot identifiers within a preset. Wire values: `"a"`, `"b"`,
`"c"`, `"d"` (lowercase letters in the localStorage `activeBySlot`
map).
-}
type SlotId
    = SlotA
    | SlotB
    | SlotC
    | SlotD


{-| The pane types a slot can host. Each slot's tab bar is populated
from this set; a pane appears in **at most one slot** at a time
(`dedupePanesAcrossSlots` enforces this on layout load).

  - `AgentTerminal`: live PTY for the assistant session. No popout
    URL (terminal output isn't a URL).
  - `AgentChat`: agent-chat sidecar iframe. Popout URL is the iframe
    src.
  - `Preview`: user app iframe (preview proxy). Popout URL is the
    last navigated URL or the preview base.
  - `Files`: per-session read-only repo browser served by md-serve
    (renders Markdown + source files), shown in an iframe. Always
    cross-origin: port-based locally, subdomain in tunnel mode.
  - `Vscode`: code-server iframe (when enabled). Popout URL is the
    workdir-scoped VSCode URL.
  - `Shell`: extra terminal pane (manual shell).
  - `Browser`: noVNC + remote-controlled Chromium for Agent View.
    Popout URL is the browser-view URL. Pane visibility is gated on
    `features.browserStarted`.

Source: `static/terminal-ui.js:4546-4565` (`panePopoutUrl`).

-}
type Pane
    = AgentTerminal
    | AgentChat
    | Preview
    | Files
    | Vscode
    | Shell
    | Browser


{-| All seven panes.
-}
allPanes : List Pane
allPanes =
    [ AgentTerminal, AgentChat, Preview, Files, Vscode, Shell, Browser ]


{-| Per-slot tab state.

  - `tabs`: ordered list of panes mounted in this slot.
  - `active`: the currently-focused pane (must be a member of `tabs`).

Persisted to `localStorage[LAYOUT_STATE_KEY]` along with the active
preset; `dedupePanesAcrossSlots` re-runs on load so a pane that ends
up in two slots (older state, manual edit) collapses to its
canonical home slot.

Source: `static/terminal-ui.js:178-200`.

-}
type alias SlotState =
    { tabs : List Pane
    , active : Pane
    }


{-| Mobile fallback view -- the per-slot grid collapses to a single
visible pane on narrow viewports.

`Nothing` means desktop layout (preset grid is rendered directly).

  - `MobileAgentTerminal`: default mobile view.
  - `MobileAgentChat`: shown when the chat iframe is reachable. The
    chat-probe spinner animates while the probe is in flight.
  - `MobilePreview`: user explicitly switched to preview.
  - `MobileBrowser`: shown after `browserStarted`.

Wire: a `data-mobile-active` attribute on the host element drives
the visible pane via CSS.

Source: `static/terminal-ui.js` mobile-view handling (search for
`mobileActiveView` / `data-mobile-active`).

-}
type MobileView
    = MobileAgentTerminal
    | MobileAgentChat
    | MobilePreview
    | MobileBrowser


{-| Per-instance state of a terminal-ui component.

The big rewrite-from-the-2026-03-02-spec here is the layout grid:
`preset` + `activeBySlot` replace the old single-tab `preview`
substructure. Tunnel-related fields (`publicHostname`,
`tunnelStatus`) come in via `PtyProtocol.StatusPayload`. Proxy ports
(`previewProxy`, `agentChatProxy`, `vncProxy`) are tracked because
the iframe builders branch on tunnel mode + the auth-proxy port.

`pendingPreviewIframeSrc` and `agentChatProbe` are intentionally
un-detailed here -- they are short-lived UI-internal fields the
spec doesn't need to model exhaustively. See the JS for the full
state.

-}
type alias State =
    { session :
        { uuid : SessionUuid
        , name : String
        , workDir : String
        , assistant : String
        , viewers : Int
        }
    , layout :
        { preset : LayoutPreset
        , activeBySlot :
            { a : Maybe SlotState
            , b : Maybe SlotState
            , c : Maybe SlotState
            , d : Maybe SlotState
            }
        , mobileActiveView : Maybe MobileView
        }
    , preview :
        { url : Maybe Url
        , canGoBack : Bool
        , canGoForward : Bool
        }
    , ports :
        { preview : Maybe PreviewPort
        , agentChat : Maybe AgentChatPort
        , previewProxy : Maybe PreviewProxyPort
        , agentChatProxy : Maybe AgentChatProxyPort
        , vncProxy : Maybe VncProxyPort
        }
    , features :
        { yoloMode : Bool
        , yoloSupported : Bool
        , browserStarted : Bool
        }
    , tunnel :
        { publicHostname : PublicHostname
        , tunnelStatus : Maybe TunnelStatus
        }
    }


{-| The two WebSocket connections each terminal-ui instance maintains.
-}
type Connection
    = PtyWs SessionUuid
    | DebugUiWs PreviewPort


{-| Side effects produced by terminal-ui message handlers.

**Invariant:** The preview URL bar is split into a fixed prefix
(`localhost:{PreviewPort}`) and an editable Path. The port shown in
the URL bar is ALWAYS `state.ports.preview` -- never extracted from
the incoming Url. This prevents xdg-open or other sources from
injecting a misleading port (e.g. `AgentChatPort 4000` when
`PreviewPort` is 3000).

Layout effects (`ActivateInSlot`, `AutoAddPaneToHome`,
`PersistLayoutState`, `PopoutPaneToNewWindow`) are how the runtime
mutates the live DOM and `localStorage`. The spec deliberately keeps
the layout sub-state immutable in `State` and emits effects so a
test can assert "this message produces this effect" without snapshot
diffing the whole grid.

  - `UpdateUrlBar`: emits the path only; prefix is fixed to
    `state.ports.preview`.
  - `ActivateInSlot`: left-click on a tab; focus it within its slot.
  - `AutoAddPaneToHome`: e.g. on `browserStarted -> True`, add
    `Browser` to its preset's home slot. Does NOT auto-focus -- that
    was removed in commit `04732ea1e`.
  - `PersistLayoutState`: write `{preset, activeBySlot,
    sizesByPreset}` to `localStorage[LAYOUT_STATE_KEY]`.
  - `PopoutPaneToNewWindow`: middle-click / Cmd-or-Ctrl-click on a
    tab whose pane has a popout URL (Preview, AgentChat, Vscode,
    Browser). Calls `window.open(url, '_blank')` without activating
    the tab. AgentTerminal and Shell have no popout URL.
  - `RenderTunnelStatusBanner`: update the tunnel-status banner.
    `Nothing` hides it (compose mode). Present states drive
    countdown / connected hostname / fatal-error rendering.
  - `SetMobileActiveView`: toggles the `data-mobile-active`
    attribute. `Nothing` means desktop layout.
  - `OpenIframePane`: legacy `xdg-open` fallback path. The producer
    side was removed (no `t: 'open'` sender remains in the
    codebase), so this effect should be unreachable in practice.
    Kept in the Effect set so the consumer at
    `static/terminal-ui.js:5211` still type-checks.

-}
type Effect
    = SendPty PtyProtocol.ClientMsg
    | SendDebug UiCommand
    | UpdateUrlBar Path
    | EnableBackButton Bool
    | EnableForwardButton Bool
    | ConnectDebugWebSocket PreviewPort
    | ActivateInSlot SlotId Pane
    | AutoAddPaneToHome Pane
    | PersistLayoutState
    | PopoutPaneToNewWindow Pane Url
    | RenderTunnelStatusBanner (Maybe TunnelStatus)
    | SetMobileActiveView (Maybe MobileView)
    | OpenIframePane { pane : Pane, url : Url }



-- -- Tab gestures (left-click / middle-click / mod-click) --------


{-| What the user did to a tab.

  - `LeftClick`: activates the tab in its slot.
  - `MiddleClick` / `ModClick`: opens the pane's popout URL in a
    new browser tab without changing the active tab. `ModClick`
    means Ctrl-click on Linux/Windows, Cmd-click on macOS.

A platform-aware tooltip (`Middle-click or Ctrl/Cmd+click to open
in new browser tab`) is shown on hover with a dotted accent
underline (`_popoutHintText` in JS, commit `79959442d`).

The popout gesture is **only meaningful** for panes whose
`panePopoutUrl` returns a string -- AgentTerminal and Shell are
URL-less and so the gesture is a no-op for them.

Source: `static/terminal-ui.js:4546-4565` (`panePopoutUrl`),
`:_popoutHintText`, and the tab-bar event wiring around `:4337`.

-}
type TabGesture
    = LeftClick
    | MiddleClick
    | ModClick


{-| Resolve a tab gesture into an effect.

    onTabGesture { gesture = LeftClick, slot = SlotA, pane = Preview }
        --> ActivateInSlot SlotA Preview

For middle / mod clicks, the effect is `PopoutPaneToNewWindow`. The
URL must be supplied by the caller (`panePopoutUrl` is a
runtime-derived value -- we don't model it as a pure function here).

If the caller can't resolve a popout URL (e.g. AgentTerminal),
they pass `Nothing` and we fall back to the same `ActivateInSlot`
effect (graceful degradation).

-}
onTabGesture : { gesture : TabGesture, slot : SlotId, pane : Pane, popoutUrl : Maybe Url } -> Effect
onTabGesture { gesture, slot, pane, popoutUrl } =
    case ( gesture, popoutUrl ) of
        ( LeftClick, _ ) ->
            ActivateInSlot slot pane

        ( MiddleClick, Just url ) ->
            PopoutPaneToNewWindow pane url

        ( ModClick, Just url ) ->
            PopoutPaneToNewWindow pane url

        ( MiddleClick, Nothing ) ->
            ActivateInSlot slot pane

        ( ModClick, Nothing ) ->
            ActivateInSlot slot pane



-- -- Message handlers --------------------------------------------


{-| Handle a message from the PTY WebSocket (WS 1/2).

The `Status` message delivers `previewPort`, triggering the debug
WS connection. It also carries the proxy ports, the
tunnel-mode-only `publicHostname` / `tunnelStatus`, and the
`browserStarted` flag.

When `browserStarted` flips to `True`, the runtime adds the
`Browser` pane to its preset's home slot but does **not** switch
focus to it (commit `04732ea1e` removed the focus auto-switch). We
emit `AutoAddPaneToHome Browser` only on the rising edge.

-}
onPtyMessage : { msg : PtyProtocol.ServerMsg, state : State } -> ( State, List Effect )
onPtyMessage { msg, state } =
    case msg of
        PtyProtocol.PtyOutput _ ->
            ( state, [] )

        PtyProtocol.Pong ->
            ( state, [] )

        PtyProtocol.Status payload ->
            let
                browserJustStarted =
                    payload.features.browserStarted && not state.features.browserStarted

                tunnelChanged =
                    payload.tunnel.tunnelStatus /= state.tunnel.tunnelStatus
            in
            ( { state
                | session =
                    { uuid = state.session.uuid
                    , name = payload.session.name
                    , workDir = payload.session.workDir
                    , assistant = payload.session.assistant
                    , viewers = payload.session.viewers
                    }
                , ports =
                    { preview = Just payload.ports.preview
                    , agentChat = Just payload.ports.agentChat
                    , previewProxy = Just payload.ports.previewProxy
                    , agentChatProxy = payload.ports.agentChatProxy
                    , vncProxy = Just payload.ports.vncProxy
                    }
                , features =
                    { yoloMode = payload.features.yoloMode
                    , yoloSupported = payload.features.yoloSupported
                    , browserStarted = payload.features.browserStarted
                    }
                , tunnel =
                    { publicHostname = payload.tunnel.publicHostname
                    , tunnelStatus = payload.tunnel.tunnelStatus
                    }
              }
            , List.concat
                [ [ ConnectDebugWebSocket payload.ports.preview ]
                , if browserJustStarted then
                    [ AutoAddPaneToHome Browser ]

                  else
                    []
                , if tunnelChanged then
                    [ RenderTunnelStatusBanner payload.tunnel.tunnelStatus ]

                  else
                    []
                ]
            )

        PtyProtocol.ChatMsg _ ->
            ( state, [] )

        PtyProtocol.FileUploaded _ ->
            ( state, [] )

        PtyProtocol.Exit _ ->
            ( state, [] )

        PtyProtocol.CredentialsStored _ ->
            ( state, [] )


{-| Handle a message from the Debug UI WebSocket (WS 3/4).

Only 3 message types are actually acted on by terminal-ui:
`UrlChange`, `Init`, `NavState`. Everything else (`Console`,
`Error`, `Fetch`, `Xhr`, `*Result`, etc.) is ignored here -- those
are consumed by MCP tools via in-process subscribers on the Go side.

`Init` and `UrlChange` go through `pathFromProxyUrl` to strip the
proxy prefix and host:port, producing a bare Path for the URL bar.

The vestigial `Open { url }` constructor in `AllDebugMsg` has no
producer in the source today -- the historical `xdg-open` HTTP
fallback was removed -- so we keep a defensive branch that emits
the legacy `OpenIframePane` effect, but in practice it is
unreachable.

-}
onDebugMessage : { msg : AllDebugMsg, state : State } -> ( State, List Effect )
onDebugMessage { msg, state } =
    case msg of
        FromShellPage shellMsg ->
            case shellMsg of
                Init payload ->
                    ( { state
                        | preview =
                            { url = Just payload.url
                            , canGoBack = state.preview.canGoBack
                            , canGoForward = state.preview.canGoForward
                            }
                      }
                    , [ UpdateUrlBar (pathFromProxyUrl state.session.uuid payload.url) ]
                    )

                UrlChange payload ->
                    ( { state
                        | preview =
                            { url = Just payload.url
                            , canGoBack = state.preview.canGoBack
                            , canGoForward = state.preview.canGoForward
                            }
                      }
                    , [ UpdateUrlBar (pathFromProxyUrl state.session.uuid payload.url) ]
                    )

                NavState payload ->
                    ( { state
                        | preview =
                            { url = state.preview.url
                            , canGoBack = payload.canGoBack
                            , canGoForward = payload.canGoForward
                            }
                      }
                    , [ EnableBackButton payload.canGoBack
                      , EnableForwardButton payload.canGoForward
                      ]
                    )

        FromInject _ ->
            ( state, [] )

        Open payload ->
            -- Defensive only: no producer remains in source.
            ( state
            , [ OpenIframePane { pane = Preview, url = payload.url } ]
            )


{-| Strip /proxy/{uuid}/preview prefix and host:port from a proxy URL,
returning only the path + query + fragment.

    pathFromProxyUrl (SessionUuid "abc")
        (Url "https://host:1977/proxy/abc/preview/dashboard?tab=1#s")
    --> Path "/dashboard?tab=1#s"

    pathFromProxyUrl (SessionUuid "abc")
        (Url "http://localhost:23000/dashboard?tab=1#s")
    --> Path "/dashboard?tab=1#s"

    pathFromProxyUrl (SessionUuid "abc")
        (Url "https://host:1977/proxy/abc/preview/")
    --> Path "/"

    pathFromProxyUrl (SessionUuid "abc")
        (Url "https://host:1977/proxy/abc/preview")
    --> Path "/"

Port-based URLs have no proxy prefix, so the pathname passes through
unchanged. Path-based URLs have the prefix stripped. In both cases,
the scheme, host, and port are discarded -- the UI supplies
`localhost:{PreviewPort}` separately.

In tunnel mode, the iframe URLs use the
`<previewProxyPort>.<publicHostname>` form (see
`TunnelMode.IframeUrlBuilding`). The path-stripping logic still
applies; the host part is discarded the same way.

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
                    -- No scheme -- treat the whole string as a path
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
                -- Port-based URL or no prefix -- pass through
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
        -- Find first '?' or '#' -- whichever comes first
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



-- -- WebSocket reconnection state machine -----------------------


{-| Which WebSocket channel -- each has different reconnect timing.

  - `PtyChannel`: PTY WebSocket (WS 1/2). Backoff: 1s -> 60s.
  - `DebugChannel`: Debug UI WebSocket (WS 3/4). Backoff: 1s -> 10s.
    On reconnect, reloads iframe if placeholder still visible.

-}
type WsChannel
    = PtyChannel
    | DebugChannel


{-| Reconnect timing per channel.

    wsConfig PtyChannel --> { baseDelayMs = 1000, maxDelayMs = 60000 }

    wsConfig DebugChannel --> { baseDelayMs = 1000, maxDelayMs = 10000 }

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
      -> Connecting              (initial connect or reconnect after backoff)

    Connecting
      -> Connected               (onopen fires)
      -> Disconnected (N+1)      (onerror/onclose fires)

    Connected
      -> Disconnected (N+1)      (onclose fires)

    ProcessExited                -- terminal; no reconnection attempted

PTY-specific: if `processExited` flag is set when onclose fires,
transitions to `ProcessExited` instead of `Disconnected`.

Debug-specific: on `Connecting -> Connected`, if placeholder is still
visible (`_previewWaiting`), reloads the iframe.

Variants:

  - `Disconnected n`: attempt counter (0 = never connected yet).
    Used to compute backoff delay.
  - `Connecting`: WebSocket constructor called, awaiting onopen.
  - `Connected`: onopen fired, traffic flowing.
  - `ProcessExited`: session ended; PTY WS will not reconnect.

-}
type WsPhase
    = Disconnected Int
    | Connecting
    | Connected
    | ProcessExited


{-| Events that drive WebSocket state transitions.

  - `Connect`: timer expired or initial connection attempt.
  - `Opened`: WebSocket onopen fired.
  - `Closed { processExited }`: onclose fired. `processExited`
    prevents PTY reconnection.
  - `Error`: onerror fired. Always followed by Closed in practice.

-}
type WsEvent
    = Connect
    | Opened
    | Closed { processExited : Bool }
    | Error


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



-- -- Placeholder lifecycle --------------------------------------


{-| Placeholder overlay state machine for preview and agent chat panes.

    Hidden
      -> Shown           (setPreviewURL() called, or agentChatPort received)

    Shown
      -> Probing          (probe started via probeUntilReady())

    Probing
      -> IframeSrcSet     (probe succeeded, iframe.src assigned)

    IframeSrcSet
      -> Hidden           (dismissed via DebugWebSocket or IframeOnLoad)

Preview can cycle: `Hidden -> Shown -> ... -> Hidden -> Shown -> ...`
(each `setPreviewURL()` call restarts the cycle).

Agent Chat goes through the cycle once and stays Hidden.

The placeholder CSS class `hidden` is toggled. When visible, the
placeholder covers the iframe with a loading message.

Variants:

  - `Hidden`: iframe is the visible content.
  - `Shown`: placeholder visible, probe not yet started.
  - `Probing`: probe running (see `HttpProxy.ProbePhase` for probe
    sub-state).
  - `IframeSrcSet`: iframe.src assigned, waiting for load/debug WS
    dismissal event.

-}
type PlaceholderPhase
    = Hidden
    | Shown
    | Probing
    | IframeSrcSet


{-| Events that drive placeholder transitions.

  - `ShowPlaceholder`: `setPreviewURL()` called (preview) or
    `agentChatPort` received (agent chat).
  - `ProbeStarted`: `probeUntilReady()` called.
  - `ProbeSucceeded`: proxy header detected; iframe.src will be set.
  - `Dismissed`: DebugWebSocket init/urlchange (preview primary) or
    iframe.onload (fallback / agent chat).

-}
type PlaceholderEvent
    = ShowPlaceholder
    | ProbeStarted
    | ProbeSucceeded
    | Dismissed


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



-- -- Navigation queuing -----------------------------------------


{-| Pending navigation state for the preview pane.

When a URL arrives while the preview pane is not active in any slot
(e.g. during probe, or while another tab has focus across all
slots), the iframe src is stashed rather than applied immediately.

  - `NoNav`: nothing pending.
  - `Pending iframeSrc`: stashed; applied when user activates the
    preview pane in any slot.

On activation:

  - If `Pending src`: apply src immediately, transition to `NoNav`.
  - If `NoNav`: call `setPreviewURL(null)` to start a fresh probe.

Mirrors JS: `terminal-ui.js` `_pendingPreviewIframeSrc`.

-}
type PendingNav
    = NoNav
    | Pending String
