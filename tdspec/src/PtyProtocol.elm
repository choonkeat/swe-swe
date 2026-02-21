module PtyProtocol exposing (ClientMsg(..), ServerMsg(..), StatusPayload, ExitPayload, FileUploadResult)

{-| WS 1,2 — PTY WebSocket protocol.

Endpoint: `/ws/{uuid}` on swe-swe-server (:3000).
One connection per terminal-ui instance, unique UUID.

    terminal-ui #1 (Agent Terminal) <-> swe-swe-server  via /ws/{uuid1}
    terminal-ui #2 (Terminal)       <-> swe-swe-server  via /ws/{uuid2}

Carries: binary PTY data (xterm I/O) + JSON control messages.

@docs ClientMsg, ServerMsg, StatusPayload, ExitPayload, FileUploadResult

-}

import Domain exposing (AgentChatPort(..), Bytes(..), PreviewPort(..), SessionUuid(..), Url(..))


{-| Messages sent by terminal-ui to swe-swe-server.
Binary messages carry raw PTY input; JSON messages carry control commands.
-}
type ClientMsg
    = PtyInput Bytes
    | Resize { cols : Int, rows : Int }
    | FileUpload { filename : String, data : Bytes }
    | Ping {- client sends { type: "ping", data?: {...} }; data is optional opaque pass-through (terminal-ui puts { ts } in it) -}
    | RenameSession { name : String }
    | ToggleYolo {- client sends { type: "toggleYolo" }; no payload — server toggles current state -}
    | Chat { userName : String, text : String }


{-| Messages received by terminal-ui from swe-swe-server.
The `Status` message is critical — its `previewPort` triggers
`connectDebugWebSocket()`, opening WS 3/4 to the agent-reverse-proxy.
-}
type ServerMsg
    = PtyOutput Bytes
    | Pong {- server echoes { type: "pong", data?: {...} }; data mirrors what client sent in Ping -}
    | Status StatusPayload
    | ChatMsg { userName : String, text : String, timestamp : String }
    | FileUploaded FileUploadResult
    | Exit ExitPayload


{-| Payload of the `status` JSON message.
Delivered periodically by swe-swe-server.
`ports.preview` triggers the debug WebSocket connection to agent-reverse-proxy.
-}
type alias StatusPayload =
    { ports :
        { preview : PreviewPort
        , agentChat : Maybe AgentChatPort
        }
    , terminal :
        { cols : Int
        , rows : Int
        }
    , session :
        { name : String
        , uuidShort : String
        , workDir : String
        , assistant : String
        , viewers : Int
        }
    , features :
        { yoloMode : Bool
        , yoloSupported : Bool
        }
    }


{-| Exit message payload — exit code plus optional worktree info.
-}
type alias ExitPayload =
    { exitCode : Int
    , worktree :
        Maybe
            { path : String
            , branch : String
            , targetBranch : String
            }
    }


{-| Result of a file upload — success with filename or failure with error.
-}
type alias FileUploadResult =
    { filename : String
    , result : Result { error : String } {}
    }
