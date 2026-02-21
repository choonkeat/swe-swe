module Domain exposing (Url(..), SessionUuid(..), PreviewPort(..), AgentChatPort(..), PreviewProxyPort(..), AgentChatProxyPort(..), Bytes(..), Timestamp(..))

{-| Shared primitive types used across the architecture.

System overview:

  - Browser page hosts 2x terminal-ui web components + 1x Preview iframe
  - Container runs swe-swe-server (PTY + embedded preview proxy)
  - 4 WebSockets + 1 HTTP endpoint connect them (+ 2 more when Preview iframe active)
  - Preview proxy is path-based (/proxy/{uuid}/preview/) on the main server port
  - Agent Chat proxy chain routes through Traefik to container backend

@docs Url, SessionUuid, PreviewPort, AgentChatPort, PreviewProxyPort, AgentChatProxyPort, Bytes, Timestamp

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


{-| Port where the MCP sidecar (agent chat backend) listens.
Derived from preview port: `previewPort + 1000`.
-}
type AgentChatPort
    = AgentChatPort Int


{-| Port where the preview reverse proxy used to listen (e.g., 23000).
Derived from preview port + offset (default 20000).
Retained for port reservation in `findAvailablePortPair`, but preview
traffic now goes through the main server port via path-based routing.
-}
type PreviewProxyPort
    = PreviewProxyPort Int


{-| Port where the agent chat reverse proxy listens (e.g., 24000).
Derived from agent chat port + offset (default 20000).
-}
type AgentChatProxyPort
    = AgentChatProxyPort Int


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
