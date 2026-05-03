module PtyProtocol exposing (ClientMsg(..), ServerMsg(..), StatusPayload, ExitPayload, FileUploadResult)

{-| WS 1,2 -- PTY WebSocket protocol.

Endpoint: `/ws/{uuid}` on swe-swe-server (:1977).
One connection per terminal-ui instance, unique UUID.

    terminal-ui #1 (Agent Terminal) <-> swe-swe-server  via /ws/{uuid1}
    terminal-ui #2 (Terminal)       <-> swe-swe-server  via /ws/{uuid2}

Carries: binary PTY data (xterm I/O) + JSON control messages.

@docs ClientMsg, ServerMsg, StatusPayload, ExitPayload, FileUploadResult

-}

import Domain exposing (AgentChatPort(..), AgentChatProxyPort(..), Bytes(..), PreviewPort(..), PreviewProxyPort(..), PublicPort(..), SessionUuid(..), Url(..))


{-| Messages sent by terminal-ui to swe-swe-server.

Binary messages carry raw PTY input; JSON messages carry control commands.

Wire `type` discriminator (snake\_case unless noted):

  - `ping`: opaque `data` field passes through; server echoes in `pong`.
  - `rename_session`: `{ name }`.
  - `toggle_yolo`: no payload; server toggles current state.
  - `set_credentials`: `{ host, username, token, name, email }`.
    Write-only -- there is no read-back API. Server stores per-session
    via the credential broker (`setCredential` / `setAuthor` /
    `writeSessionGitconfig`) and acks with `credentials_stored` on
    ServerMsg. `main.go:4990-5028`.

-}
type ClientMsg
    = PtyInput Bytes
    | Resize { cols : Int, rows : Int }
    | FileUpload { filename : String, data : Bytes }
    | Ping
    | RenameSession { name : String }
    | ToggleYolo
    | Chat { userName : String, text : String }
    | SetCredentials
        { host : String
        , username : String
        , token : String
        , name : String
        , email : String
        }


{-| Messages received by terminal-ui from swe-swe-server.

The `Status` message is critical -- its `previewPort` triggers
`connectDebugWebSocket()`, opening WS 3/4 to the agent-reverse-proxy.

`credentials_stored` (`{ host, hosts }`) is emitted after a successful
`set_credentials` round-trip. `host` echoes the host just stored;
`hosts` is the full list of hosts this session currently has
credentials for. `main.go:5022-5025`.

-}
type ServerMsg
    = PtyOutput Bytes
    | Pong
    | Status StatusPayload
    | ChatMsg { userName : String, text : String, timestamp : String }
    | FileUploaded FileUploadResult
    | Exit ExitPayload
    | CredentialsStored { host : String, hosts : List String }


{-| Payload of the `status` JSON message.
Delivered periodically by swe-swe-server.
`sessionUUID` is used by the browser to build path-based proxy URLs.
`ports.preview` triggers the debug WebSocket connection to agent-reverse-proxy.

Note: the wire format is flat JSON (`previewPort`, `agentChatPort` as top-level keys).
The nesting here groups related fields for clarity (see HOW-TO.md #3.14).
`agentChatPort` is `0` on the wire for terminal sessions (JS treats as falsy).

`browserStarted` is set to `True` after `/api/session/{uuid}/browser/start` is called.
terminal-ui auto-switches to the Agent View tab when this becomes `True`.
The Agent View tab is hidden until `browserStarted` is `True`.

-}
type alias StatusPayload =
    { sessionUUID : SessionUuid
    , ports :
        { preview : PreviewPort
        , agentChat : AgentChatPort
        , previewProxy : PreviewProxyPort
        , agentChatProxy : Maybe AgentChatProxyPort
        , public : PublicPort
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
        , browserStarted : Bool
        }
    }


{-| Exit message payload -- exit code plus optional worktree info.
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


{-| Result of a file upload -- success with filename or failure with error.
-}
type alias FileUploadResult =
    { filename : String
    , result : Result { error : String } {}
    }
