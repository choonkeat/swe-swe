module Domain exposing (Url(..), SessionUuid(..), PreviewPort(..), Bytes(..))

{-| Shared primitive types used across the WebSocket architecture.

System overview:

  - Browser page hosts 2x terminal-ui web components + 1x Preview iframe
  - Container runs swe-swe-server (PTY) + agent-reverse-proxy (debug/preview)
  - 6 WebSockets + 1 HTTP endpoint connect them

@docs Url, SessionUuid, PreviewPort, Bytes

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
