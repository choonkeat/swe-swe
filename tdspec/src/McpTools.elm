module McpTools exposing
    ( McpServer(..), PreviewTool(..), AgentChatTool(..)
    , PreviewResource(..), AgentChatResource(..)
    , SnapshotResult, ConsoleEntry(..)
    , DrawReply(..)
    , allPreviewTools, allAgentChatTools
    )

{-| MCP Tools — the AI-facing tools hosted by two independent MCP servers.

Two MCP servers expose tools to the AI agent (Claude):

    Preview MCP (agent-reverse-proxy, embedded in swe-swe-server)
      Endpoint: /proxy/{uuid}/preview/mcp (StreamableHTTP)
      Tools:    preview_browser_snapshot, preview_browser_console_messages
      Reads:    DebugHub in-process subscriber channel (inject.js messages)

    Agent Chat MCP (agent-chat sidecar, separate process)
      Endpoint: http://localhost:{AGENT_CHAT_PORT}/mcp (StreamableHTTP + stdio)
      Tools:    send_message, draw, send_progress, check_messages
      Reads:    Event bus connected to browser WebSocket

Message flow for preview tools:

    inject.js → WS → DebugHub → subscriber channel → MCP tool → response

Message flow for agent chat tools:

    Claude → MCP → agent-chat → event bus → WS → browser
    browser → user click → WS → event bus → MCP response

@docs McpServer, PreviewTool, AgentChatTool
@docs PreviewResource, AgentChatResource
@docs SnapshotResult, ConsoleEntry
@docs DrawReply
@docs allPreviewTools, allAgentChatTools

-}

import Domain exposing (Timestamp(..), Url(..))



-- ── MCP servers ────────────────────────────────────────────────


{-| The two independent MCP servers in the system.
-}
type McpServer
    = PreviewMcp
      {- Embedded in swe-swe-server via agent-reverse-proxy Go library.
         Endpoint: /proxy/{uuid}/preview/mcp
         ToolPrefix: "preview" (tools named preview_browser_*)
         Connects to DebugHub as in-process subscriber.
      -}
    | AgentChatMcp



{- Separate sidecar process (agent-chat binary).
   Endpoint: http://localhost:{AGENT_CHAT_PORT}/mcp
   Also available via stdio MCP transport.
   Communicates with browser via event bus + WebSocket.
-}
-- ── Preview MCP tools ──────────────────────────────────────────


{-| Tools exposed by the preview MCP server.

Both tools consume DebugHub messages from inject.js via in-process
subscriber channels (buffer size 64, non-blocking fan-out).

-}
type PreviewTool
    = BrowserSnapshot
      {- Capture DOM element by CSS selector.

         Input:  { selector : String }
         Output: SnapshotResult

         Sends a QueryCommand with unique ID to inject.js (via DebugHub).
         Waits up to 5 seconds for matching QueryResult response.
         If no match: returns { found = False }.
      -}
    | BrowserConsoleMessages



{- Listen for console/error/network activity for N seconds.

   Input:  { durationSeconds : Float }  -- clamped to 0.1–30, default 5
   Output: List ConsoleEntry (one per line, newline-delimited JSON)

   Subscribes to DebugHub, collects all iframe messages for the
   specified duration, then unsubscribes and returns.
-}


{-| Result of a browser snapshot query.
-}
type alias SnapshotResult =
    { found : Bool
    , text : Maybe String
    , html : Maybe String
    , visible : Bool
    , rect : Maybe { x : Float, y : Float, width : Float, height : Float }
    }


{-| A single entry from browser console messages.

Each variant maps to a `t` field in the JSON wire format.

-}
type ConsoleEntry
    = Console { level : String, args : List String, ts : Timestamp } {- level: "log" | "warn" | "error" | "info" | "debug" -}
    | ErrorEntry { msg : String, file : String, line : Int, col : Int, stack : Maybe String, ts : Timestamp }
    | Rejection { reason : String, ts : Timestamp }
    | FetchEntry { url : Url, method : String, durationMs : Float, status : Maybe Int, ok : Maybe Bool, error : Maybe String, ts : Timestamp }
    | XhrEntry { url : Url, method : String, status : Int, ok : Bool, durationMs : Float, ts : Timestamp }
    | UrlChangeEntry { url : Url, ts : Timestamp }
    | NavStateEntry { canGoBack : Bool, canGoForward : Bool }
    | WsUpgrade { from : Url, to : Url, ts : Timestamp }


{-| All preview MCP tools.

    allPreviewTools == [ BrowserSnapshot, BrowserConsoleMessages ]

-}
allPreviewTools : List PreviewTool
allPreviewTools =
    [ BrowserSnapshot, BrowserConsoleMessages ]



-- ── Preview MCP resources ──────────────────────────────────────


{-| Resources exposed by the preview MCP server.

URI scheme: `preview-browser://{name}`

-}
type PreviewResource
    = PreviewReference
      {- preview-browser://reference (app-preview.md)
         How the App Preview works: MCP tools, debug messages, port configuration.
      -}
    | PreviewHelp



{- preview-browser://help (help.md)
   How to debug web apps using the App Preview: tool examples, workflow, tips.
-}
-- ── Agent Chat MCP tools ───────────────────────────────────────


{-| Tools exposed by the agent chat MCP server.

Send/draw are blocking (wait for user interaction). Progress/check are
non-blocking (return immediately).

-}
type AgentChatTool
    = SendMessage
      {- Send text to whiteboard chat, wait for user reply.

         Input:  { text : String
                 , quickReply : String
                 , moreQuickReplies : Maybe (List String)
                 }
         Output: "User responded: {reply}"

         Lazily starts HTTP server and opens browser on first call.
         Blocks until user clicks a reply button.
      -}
    | Draw
      {- Draw diagram slide on whiteboard canvas, wait for acknowledgment.

         Input:  { text : String          -- caption
                 , instructions : List Json  -- drawing instructions
                 , quickReply : String
                 , moreQuickReplies : Maybe (List String)
                 }
         Output: DrawReply

         Each draw call = one slide. Gradual reveal across multiple calls.
         Sends text as chat bubble, then creates canvas bubble.
         Blocks until viewer clicks acknowledgment.
      -}
    | SendProgress
      {- Non-blocking progress update.

         Input:  { text : String }
         Output: "Progress sent."

         Fire-and-forget. Does not wait for subscriber.
      -}
    | CheckMessages



{- Non-blocking check for queued user messages.

   Input:  (none)
   Output: "User said: {msg}" or "No new messages."

   Drains message queue without blocking. Call periodically
   between tasks to stay responsive to user input.
-}


{-| What the viewer sends back after a draw call.
-}
type DrawReply
    = Acknowledged {- Viewer clicked the primary quick-reply button (e.g., "Continue"). -}
    | ViewerFeedback String



{- Viewer typed a response or clicked a secondary button.
   Common feedback: "Slower pace", "More detail", "Skip ahead".
-}


{-| All agent chat MCP tools.

    allAgentChatTools == [ SendMessage, Draw, SendProgress, CheckMessages ]

-}
allAgentChatTools : List AgentChatTool
allAgentChatTools =
    [ SendMessage, Draw, SendProgress, CheckMessages ]



-- ── Agent Chat MCP resources ───────────────────────────────────


{-| Resources exposed by the agent chat MCP server.

URI scheme: `whiteboard://{name}`

-}
type AgentChatResource
    = WhiteboardInstructions
      {- whiteboard://instructions (instruction-reference.md)
         Complete reference of all 16+ drawing instruction types:
         drawRect, drawCircle, writeText, moveTo, lineTo, setColor,
         setLineWidth, setFill, setStroke, drawArc, drawPolygon,
         drawLine, fillRect, strokeRect, clearRect, drawImage.
      -}
    | WhiteboardDiagrammingGuide
      {- whiteboard://diagramming-guide (diagramming-guide.md)
         Layout rules, cognitive principles, readability guidelines.
      -}
    | WhiteboardQuickReference



{- whiteboard://quick-reference (quick-reference.md)
   Condensed cheat sheet: essential instructions, JSON format, colors, arrows.
-}
