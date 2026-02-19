module Domain exposing (Url(..), SessionUuid(..), PreviewPort(..), Bytes(..), Timestamp(..))

{-| Shared primitive types used across the WebSocket architecture.

System overview:

  - Browser page hosts 2x terminal-ui web components + 1x Preview iframe
  - Container runs swe-swe-server (PTY) + agent-reverse-proxy (debug/preview)
  - 4 WebSockets + 1 HTTP endpoint connect them (+ 2 more when Preview iframe active)

@docs Url, SessionUuid, PreviewPort, Bytes, Timestamp

-}


{-| URL string wrapper. Prevents mixing with other strings.
-}
type Url
    = Url String


{-| Session identifier — each terminal-ui instance gets a unique UUID.
-}
type SessionUuid
    = SessionUuid String


{-| Port number for the preview dev server.
Received via `StatusPayload.previewPort` on the PTY WebSocket.
-}
type PreviewPort
    = PreviewPort Int


{-| Opaque binary data — PTY I/O, file upload/download chunks.
Not JSON-encoded; sent as WebSocket binary frames.
-}
type Bytes
    = Bytes


{-| Millisecond timestamp from `Date.now()`.
Used by inject.js and terminal-ui for telemetry timing.
-}
type Timestamp
    = Timestamp Int
