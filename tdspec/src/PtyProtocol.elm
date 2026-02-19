module PtyProtocol exposing (ClientMsg(..), ServerMsg(..), StatusPayload)

{-| WS 1,2 — PTY WebSocket protocol.

Endpoint: `/ws/{uuid}` on swe-swe-server (:3000).
One connection per terminal-ui instance, unique UUID.

    terminal-ui #1 (Agent Terminal) <-> swe-swe-server  via /ws/{uuid1}
    terminal-ui #2 (Terminal)       <-> swe-swe-server  via /ws/{uuid2}

Carries: binary PTY data (xterm I/O) + JSON control messages.

@docs ClientMsg, ServerMsg, StatusPayload

-}

import Domain exposing (Bytes(..), PreviewPort(..), SessionUuid(..), Url(..))


{-| Messages sent by terminal-ui to swe-swe-server.
Binary messages carry raw PTY input; JSON messages carry control commands.
-}
type ClientMsg
    = PtyInput Bytes
    | Resize { cols : Int, rows : Int }
    | FileUpload { filename : String, data : Bytes }
    | Ping
    | RenameSession { name : String }
    | ToggleYolo { enabled : Bool }
    | Chat { text : String }


{-| Messages received by terminal-ui from swe-swe-server.
The `Status` message is critical — its `previewPort` triggers
`connectDebugWebSocket()`, opening WS 3/4 to the agent-reverse-proxy.
-}
type ServerMsg
    = PtyOutput Bytes
    | FileDownloadChunk Bytes
    | Pong
    | Status StatusPayload
    | ChatResponse { text : String }
    | FileUploadAck { filename : String }
    | Exit


{-| Payload of the `status` JSON message.
Delivered periodically by swe-swe-server.
`previewPort` triggers the debug WebSocket connection to agent-reverse-proxy.
-}
type alias StatusPayload =
    { previewPort : PreviewPort
    , workDir : String
    , viewers : Int
    , sessionName : String
    , yoloMode : Bool
    }
