module SessionLifecycle exposing
    ( SessionPorts, sessionPorts
    , ProcessGroup(..), ProcessGroupKill(..), killSequence
    , DescendantCollection(..), collectDescendants
    , PortCleanup(..), portCleanupSteps
    , EndSessionFlow(..), EndSessionStep(..), endSessionSteps
    , PublicPortCheck(..), PublicPortCheckResult(..), checkPublicPort
    )

{-| Session lifecycle -- process management and end-session flow.

Each session spawns a shell process via PTY. That shell becomes the root
of a process tree that may include AI agents, MCP servers, dev servers,
and other child processes.

    Session process tree (example)

    swe-swe-server
    +-- bash (session PID, own process group via pty.Start + Setsid)
        +-- claude (AI agent)
        |   +-- node mcp-server (may call setsid -> different PGID)
        |   |   +-- python3 -m http.server 3006
        |   +-- node agent-chat-mcp
        +-- vim

Ending a session must:

1.  Confirm with user if something is listening on the public port
2.  Cascade-end all child sessions (shell sessions opened from agent sessions)
3.  Kill the entire process tree (including escaped descendants)
4.  Kill any remaining processes on session ports (belt-and-suspenders)
5.  Close session resources (PTY, WebSockets, proxy servers)
6.  Defer port reuse until all cleanup completes

@docs SessionPorts, sessionPorts
@docs ProcessGroup, ProcessGroupKill, killSequence
@docs DescendantCollection, collectDescendants
@docs PortCleanup, portCleanupSteps
@docs EndSessionFlow, EndSessionStep, endSessionSteps
@docs PublicPortCheck, PublicPortCheckResult, checkPublicPort

-}

import Domain exposing (AgentChatPort(..), PreviewPort(..), PublicPort(..), SessionUuid(..))



-- -- Session port allocation --------------------------------------


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



-- -- Process group management -------------------------------------


{-| A Unix process group created by `pty.Start` with `Setsid=true`.

`pty.Start` creates a new session AND process group for the shell process.
This means `kill(-pid, signal)` sends the signal to all processes in the group.

**Limitation**: Child processes can escape the group by calling `setsid()` or
`setpgid()`. MCP servers spawned with `detached: true` commonly do this.

Source: main.go `pty.Start(cmd)` -- `Setsid=true` is set automatically.

-}
type ProcessGroup
    = ProcessGroup { pid : Int }


{-| Two-stage process group kill sequence.

Stage 1 -- group-wide signals:

1.  `SIGTERM` to entire process group (`kill(-pid, SIGTERM)`)
2.  Wait up to 3 seconds for graceful shutdown
3.  If still alive: `SIGKILL` to entire process group

Stage 2 -- escaped descendant cleanup:

1.  Individually `SIGKILL` each descendant that escaped the process group
    (collected BEFORE stage 1 via `/proc` traversal -- see `DescendantCollection`)

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



-- -- Descendant collection ----------------------------------------


{-| How escaped descendant PIDs are collected before killing.

**Critical ordering**: Descendants must be collected BEFORE sending any signals.
Once the parent process dies, orphaned children get reparented to PID 1 and
the parent-child lineage is lost.

Algorithm:

1.  Scan `/proc` directory for all PIDs
2.  Parse `/proc/{pid}/stat` to extract PPID (parent PID)
3.  Build a `PPID -> [child PIDs]` map
4.  BFS from session root PID to collect all descendants

This catches processes in different process groups (different PGID) that would
not receive group-wide signals -- e.g., MCP servers spawned with `setsid()`.

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



-- -- Port-based cleanup -------------------------------------------


{-| Last-resort port-based process cleanup.

After killing the process tree (group signals + descendant scan), scan
`/proc/net/tcp` for any process still listening on the session's ports
and kill it. This catches processes that escaped both mechanisms -- e.g.,
double-forked daemons reparented to PID 1.

Algorithm:

1.  Parse `/proc/net/tcp` for LISTEN sockets (state `0A`)
2.  Match socket ports against session ports (preview, agentChat, public)
3.  Extract socket inode numbers for matching ports
4.  Scan `/proc/{pid}/fd/` symlinks to find PIDs holding those inodes
5.  `SIGKILL` each matching PID (skipping swe-swe-server's own PID)

Source: `killProcessesOnPorts()` in main.go.

-}
type PortCleanup
    = ParseProcNetTcp
    | MatchSessionPorts
    | ExtractSocketInodes
    | FindPidsForInodes
    | KillLingeringProcesses


{-| The port cleanup steps in order.

    portCleanupSteps
    --> [ ParseProcNetTcp
    --> , MatchSessionPorts
    --> , ExtractSocketInodes
    --> , FindPidsForInodes
    --> , KillLingeringProcesses
    --> ]

-}
portCleanupSteps : List PortCleanup
portCleanupSteps =
    [ ParseProcNetTcp
    , MatchSessionPorts
    , ExtractSocketInodes
    , FindPidsForInodes
    , KillLingeringProcesses
    ]



-- -- End session flow ---------------------------------------------


{-| The complete end-session flow, triggered by `POST /api/session/{uuid}/end`.

    End Session Flow (server-side public port check)

    Browser                              swe-swe-server
    ------                              --------------
    [End] button click
        |
        +-- POST /api/session/{uuid}/end ---> handleSessionEndAPI()
        |                                      |
        |                                      +-- probePort(publicPort)
        |                                      |   (TCP connect to 127.0.0.1:port)
        |                                      |
        |   +-- if listening ------------------+
        |   |                                  +-- 409 Conflict
        |   |                                      {"publicPort":5007, "message":"..."}
        <----+
        |
        +-- prompt user to type port
        |   number to confirm
        |
        +-- POST with header ---------------> handleSessionEndAPI()
        |   X-Confirm-Public-Port: 5007        |
        |                                      +-- Find session + children
        |                                      |   (keep in map for port reservation)
        |                                      |
        |                                      +-- For each child session:
        |                                      |   +-- killSessionProcessGroup(child)
        |                                      |   +-- child.Close()
        |                                      |
        |                                      +-- killSessionProcessGroup(session)
        |                                      |   +-- collectDescendantPIDs
        |                                      |   +-- SIGTERM process group
        |                                      |   +-- wait 3s / SIGKILL
        |                                      |   +-- kill escaped descendants
        |                                      |
        |                                      +-- session.Close()
        |                                      |   +-- save metadata (EndedAt)
        |                                      |   +-- cancel context
        |                                      |   +-- shutdown proxy servers
        |                                      |   +-- close WebSocket clients
        |                                      |   +-- SIGKILL process group (safety)
        |                                      |   +-- close PTY
        |                                      |
        |                                      +-- killProcessesOnPorts(sessionPorts)
        |                                      |   (belt-and-suspenders: kill any
        |                                      |    remaining port listeners)
        |                                      |
        |                                      +-- delete from sessions map
        |                                          (ports now safe to reuse)
        |
        <--- 204 No Content --------------------+

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
    | KillLingeringPortProcesses
    | ReleasePortReservation


{-| The end-session steps in order.

    endSessionSteps
    --> [ CheckPublicPort
    --> , FindSessionAndChildren
    --> , CascadeEndChildren
    --> , KillProcessTree
    --> , CloseSessionResources
    --> , KillLingeringPortProcesses
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
    , KillLingeringPortProcesses
    , ReleasePortReservation
    ]



-- -- Public port check --------------------------------------------


{-| Server-side public port check before ending a session.

The server probes the public port via HTTP GET to `http://127.0.0.1:port/`
(500ms timeout), falling back to raw TCP connect if HTTP fails.
No CORS issues since this runs server-side. If the response contains
a `<title>` tag, it is extracted and shown to the user in the confirmation
prompt as a best-effort hint about what is running.

Two-phase protocol:

1.  Browser sends `POST /api/session/{uuid}/end`
2.  Server probes public port; if something is listening, returns
    `409 Conflict` with JSON `{"publicPort": 5007, "pageTitle": "My App", "message": "..."}`
3.  Browser prompts user (showing page title if available) to type port to confirm
4.  Browser re-sends `POST /api/session/{uuid}/end` with header
    `X-Confirm-Public-Port: 5007`
5.  Server skips the probe and proceeds with ending

Source: `probePort()` and `handleSessionEndAPI()` in main.go,
`doEndSession()` in end-session.js.

-}
type PublicPortCheck
    = ProbePublicPort PublicPort
    | PromptUserToConfirm { publicPort : PublicPort, pageTitle : Maybe String }
    | ProceedWithEnd


{-| Result of the server-side public port probe.
-}
type PublicPortCheckResult
    = PortActive
    | PortInactive
    | PortConfirmed


{-| Determine the check result and required action.

    checkPublicPort PortActive
    --> PromptUserToConfirm { publicPort = PublicPort 5007, pageTitle = Just "My App" }

    checkPublicPort PortInactive
    --> ProceedWithEnd

    checkPublicPort PortConfirmed
    --> ProceedWithEnd

-}
checkPublicPort : PublicPortCheckResult -> PublicPortCheck
checkPublicPort result =
    case result of
        PortActive ->
            PromptUserToConfirm { publicPort = PublicPort 0, pageTitle = Nothing }

        PortInactive ->
            ProceedWithEnd

        PortConfirmed ->
            ProceedWithEnd
