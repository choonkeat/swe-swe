module Domain exposing (Url(..), Path(..), SessionUuid(..), PreviewPort(..), AgentChatPort(..), Bytes(..), Timestamp(..), ServerAddr(..))

{-| Shared primitive types used across the architecture.

System overview:

  - Browser page hosts 2x terminal-ui web components + 1x Preview iframe
  - Container runs swe-swe-server (PTY + embedded preview/agentchat proxy)
  - 4 WebSockets + 1 HTTP endpoint connect them (+ 2 more when Preview iframe active)
  - Preview proxy is path-based (/proxy/{uuid}/preview/) on the main server port
  - Agent Chat proxy is path-based (/proxy/{uuid}/agentchat/) on the main server port
  - Only 1 port goes through Traefik — no per-session proxy ports needed

@docs Url, Path, SessionUuid, PreviewPort, AgentChatPort, Bytes, Timestamp, ServerAddr

-}


{-| URL string wrapper. Prevents mixing with other strings.
-}
type Url
    = Url String


{-| URL path with query and fragment (no scheme, host, or port).

Extracted from a full Url by stripping `localhost:{port}` or the proxy prefix.
Used by the Preview URL bar which shows a fixed `localhost:{PreviewPort}` prefix
and an editable path — the port is NEVER derived from the incoming URL because
the preview proxy can only target one port per session.

-}
type Path
    = Path String


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


{-| Container-internal address where swe-swe-server listens.

    localhost:9898

Configured in:

  - main.go: `-addr` flag (default `:9898`)
  - docker-compose.yml: `loadbalancer.server.port=9898`
  - entrypoint.sh: bridge `--bridge http://localhost:9898/...`
  - entrypoint.sh: open shim `curl http://localhost:9898/...`

The stdio bridge, open shim, and proxy chains all target this address.

-}
type ServerAddr
    = ServerAddr
