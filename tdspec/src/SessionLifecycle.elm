module SessionLifecycle exposing
    ( SessionPorts, sessionPorts
    , ProcessGroup(..), ProcessGroupKill(..), killSequence
    , DescendantCollection(..), collectDescendants
    , EndSessionFlow(..), EndSessionStep(..), endSessionSteps
    , PublicPortCheck(..), PublicPortCheckResult(..), checkPublicPort
    )

{-| Session lifecycle — process management and end-session flow.

Each session spawns a shell process via PTY. That shell becomes the root
of a process tree that may include AI agents, MCP servers, dev servers,
and other child processes.

    Session process tree (example)

    swe-swe-server
    └── bash (session PID, own process group via pty.Start + Setsid)
        ├── claude (AI agent)
        │   ├── node mcp-server (may call setsid → different PGID)
        │   │   └── python3 -m http.server 3006
        │   └── node agent-chat-mcp
        └── vim

Ending a session must:

1.  Confirm with user if something is listening on the public port
2.  Cascade-end all child sessions (shell sessions opened from agent sessions)
3.  Kill the entire process tree (including escaped descendants)
4.  Close session resources (PTY, WebSockets, proxy servers)
5.  Defer port reuse until all cleanup completes

@docs SessionPorts, sessionPorts
@docs ProcessGroup, ProcessGroupKill, killSequence
@docs DescendantCollection, collectDescendants
@docs EndSessionFlow, EndSessionStep, endSessionSteps
@docs PublicPortCheck, PublicPortCheckResult, checkPublicPort

-}

import Domain exposing (AgentChatPort(..), PreviewPort(..), PublicPort(..), SessionUuid(..))



-- ── Session port allocation ──────────────────────────────────────


{-| The three ports allocated per session, derived from a single preview port.

    sessionPorts (PreviewPort 3006)
    --> { preview = PreviewPort 3006
    --> , agentChat = AgentChatPort 4006
    --> , public = PublicPort 5006
    --> }

All three ports are reserved atomically when a session starts and released
together when cleanup completes (deferred deletion prevents reuse races).

Source: `findAvailablePortTriple` in main.go.

-}
type alias SessionPorts =
    { preview : PreviewPort
    , agentChat : AgentChatPort
    , public : PublicPort
    }


{-| Derive all three session ports from a preview port.

    sessionPorts (PreviewPort 3000)
    --> { preview = PreviewPort 3000
    --> , agentChat = AgentChatPort 4000
    --> , public = PublicPort 5000
    --> }

-}
sessionPorts : PreviewPort -> SessionPorts
sessionPorts (PreviewPort p) =
    { preview = PreviewPort p
    , agentChat = AgentChatPort (p + 1000)
    , public = PublicPort (p + 2000)
    }



-- ── Process group management ─────────────────────────────────────


{-| A Unix process group created by `pty.Start` with `Setsid=true`.

`pty.Start` creates a new session AND process group for the shell process.
This means `kill(-pid, signal)` sends the signal to all processes in the group.

**Limitation**: Child processes can escape the group by calling `setsid()` or
`setpgid()`. MCP servers spawned with `detached: true` commonly do this.

Source: main.go `pty.Start(cmd)` — `Setsid=true` is set automatically.

-}
type ProcessGroup
    = ProcessGroup { pid : Int }


{-| Two-stage process group kill sequence.

Stage 1 — group-wide signals:

1.  `SIGTERM` to entire process group (`kill(-pid, SIGTERM)`)
2.  Wait up to 3 seconds for graceful shutdown
3.  If still alive: `SIGKILL` to entire process group

Stage 2 — escaped descendant cleanup:

1.  Individually `SIGKILL` each descendant that escaped the process group
    (collected BEFORE stage 1 via `/proc` traversal — see `DescendantCollection`)

Source: `killSessionProcessGroup()` in main.go.

-}
type ProcessGroupKill
    = GroupSigterm
    | WaitGraceful { timeoutSeconds : Int }
    | GroupSigkill
    | KillEscapedDescendants


{-| The kill sequence in order.

    killSequence
    --> [ GroupSigterm
    --> , WaitGraceful { timeoutSeconds = 3 }
    --> , GroupSigkill
    --> , KillEscapedDescendants
    --> ]

-}
killSequence : List ProcessGroupKill
killSequence =
    [ GroupSigterm
    , WaitGraceful { timeoutSeconds = 3 }
    , GroupSigkill
    , KillEscapedDescendants
    ]



-- ── Descendant collection ────────────────────────────────────────


{-| How escaped descendant PIDs are collected before killing.

**Critical ordering**: Descendants must be collected BEFORE sending any signals.
Once the parent process dies, orphaned children get reparented to PID 1 and
the parent-child lineage is lost.

Algorithm:

1.  Scan `/proc` directory for all PIDs
2.  Parse `/proc/{pid}/stat` to extract PPID (parent PID)
3.  Build a `PPID → [child PIDs]` map
4.  BFS from session root PID to collect all descendants

This catches processes in different process groups (different PGID) that would
not receive group-wide signals — e.g., MCP servers spawned with `setsid()`.

**Known limitation**: Does NOT catch double-forked daemons whose intermediate
process has already exited (orphan reparented to PID 1 before collection).

Source: `collectDescendantPIDs()` in main.go.

-}
type DescendantCollection
    = ScanProc
    | ParsePpid
    | BuildParentChildMap
    | BfsFromRoot


{-| The descendant collection steps in order.

    collectDescendants
    --> [ ScanProc, ParsePpid, BuildParentChildMap, BfsFromRoot ]

-}
collectDescendants : List DescendantCollection
collectDescendants =
    [ ScanProc, ParsePpid, BuildParentChildMap, BfsFromRoot ]



-- ── End session flow ─────────────────────────────────────────────


{-| The complete end-session flow, triggered by `POST /api/session/{uuid}/end`.

    End Session Flow

    Browser                          swe-swe-server
    ──────                          ──────────────
    [End] button click
        │
        ├── checkPublicPort()
        │   (client-side fetch to
        │    public port)
        │
        ├── if listening: prompt
        │   user to type port
        │   number to confirm
        │
        ├── POST /api/session/{uuid}/end ──→ handleSessionEndAPI()
        │                                      │
        │                                      ├── Find session + children
        │                                      │   (keep in map for port reservation)
        │                                      │
        │                                      ├── For each child session:
        │                                      │   ├── killSessionProcessGroup(child)
        │                                      │   └── child.Close()
        │                                      │
        │                                      ├── killSessionProcessGroup(session)
        │                                      │   ├── collectDescendantPIDs
        │                                      │   ├── SIGTERM process group
        │                                      │   ├── wait 3s / SIGKILL
        │                                      │   └── kill escaped descendants
        │                                      │
        │                                      ├── session.Close()
        │                                      │   ├── save metadata (EndedAt)
        │                                      │   ├── cancel context
        │                                      │   ├── shutdown proxy servers
        │                                      │   ├── close WebSocket clients
        │                                      │   ├── SIGKILL process group (safety)
        │                                      │   └── close PTY
        │                                      │
        │                                      └── delete from sessions map
        │                                          (ports now safe to reuse)
        │
        ←── 204 No Content ────────────────────┘

Source: `handleSessionEndAPI()` and `end-session.js` in swe-swe-server.

-}
type EndSessionFlow
    = EndSessionFlow


{-| Individual steps in the end-session sequence.

Port reservation is deferred: sessions stay in the map during cleanup so
their ports can't be grabbed by a new session while processes are still dying.
Only after all cleanup completes are they removed.

-}
type EndSessionStep
    = CheckPublicPort
    | FindSessionAndChildren
    | CascadeEndChildren
    | KillProcessTree
    | CloseSessionResources
    | ReleasePortReservation


{-| The end-session steps in order.

    endSessionSteps
    --> [ CheckPublicPort
    --> , FindSessionAndChildren
    --> , CascadeEndChildren
    --> , KillProcessTree
    --> , CloseSessionResources
    --> , ReleasePortReservation
    --> ]

-}
endSessionSteps : List EndSessionStep
endSessionSteps =
    [ CheckPublicPort
    , FindSessionAndChildren
    , CascadeEndChildren
    , KillProcessTree
    , CloseSessionResources
    , ReleasePortReservation
    ]



-- ── Public port check ────────────────────────────────────────────


{-| Client-side public port check before ending a session.

The browser probes the public port to detect if something is listening.
If a server responds, the user must type the port number to confirm.

**BUG (current implementation)**: Uses `fetch(url, { mode: 'cors' })`.
Servers without CORS headers (e.g., `python3 -m http.server`) cause the
fetch to reject with a CORS error. The `.catch()` handler interprets this
as "nothing listening" and skips the confirmation prompt — defeating the
safety check entirely.

Source: `checkPublicPortAndEndSession()` in end-session.js.

-}
type PublicPortCheck
    = ProbePublicPort PublicPort
    | PromptUserToConfirm { publicPort : PublicPort, pageTitle : Maybe String }
    | SkipCheckNoPublicPort


{-| Result of the public port probe.

**BUG**: `NothingListening` is currently also returned for CORS errors,
making the check unreliable for servers without CORS headers.

-}
type PublicPortCheckResult
    = ServerResponded { pageTitle : Maybe String }
    | NothingListening
    | CorsError


{-| Determine the check result and required action.

    checkPublicPort ServerResponded { pageTitle = Just "My App" }
    --> PromptUserToConfirm { publicPort = PublicPort 5000, pageTitle = Just "My App" }

    checkPublicPort NothingListening
    --> SkipCheckNoPublicPort  -- safe to proceed

    checkPublicPort CorsError
    --> SkipCheckNoPublicPort  -- BUG: should prompt, but CORS hides the response

-}
checkPublicPort : PublicPortCheckResult -> PublicPortCheck
checkPublicPort result =
    case result of
        ServerResponded { pageTitle } ->
            PromptUserToConfirm { publicPort = PublicPort 0, pageTitle = pageTitle }

        NothingListening ->
            SkipCheckNoPublicPort

        -- BUG: CORS error is indistinguishable from "nothing listening"
        CorsError ->
            SkipCheckNoPublicPort
