module CredentialBroker exposing
    ( Host(..), Username(..), Password(..), Pid(..)
    , BrokerSocketPath, brokerSocketPath
    , SessionGitconfigPath, sessionGitconfigPath, sessionGitconfigDir
    , CredentialBag, AuthorIdent
    , PidRegistryOp(..), findSessionForPid
    , GitCredentialAction(..), parseGitCredentialAction
    , BrokerOp(..), BrokerRequest, BrokerResponse(..)
    , SessionEnvInjection, sessionEnvInjection
    , GitconfigSection(..), gitconfigBody
    , BrokerLifecycleOp(..)
    )

{-| Per-session git credential broker.

The broker keeps HTTPS credentials and SSH signing keys for each
session inside `swe-swe-server` and exposes them over the abstract
Unix socket `@swe-swe-broker` to the in-container helper
`git-credential-swe-swe`. There is no read-back API: the browser
sends `set_credentials` over the PTY WebSocket; the server stores
them in memory and only flows the secret OUT to git via the
broker socket.

Two complementary mechanisms wire git up to use the broker:

1.  Per-session `GIT_CONFIG_COUNT=1 / KEY_0=credential.helper /
    VALUE_0=swe-swe` env vars injected into the session shell via
    `buildSessionEnv`. Tells git to invoke
    `git-credential-swe-swe`.
2.  Per-session `GIT_CONFIG_GLOBAL=/tmp/swe-swe-session-gitconfig/<sid>`
    pointing at a file that `[include]`s `~/.gitconfig` and layers
    `[user]` name/email + (when set) `[user] signingkey`,
    `[gpg] format = ssh`, `[gpg "ssh"] program = git-sign-swe-swe`,
    `[commit] gpgsign = true`, `[tag] gpgsign = true`.

Identification of the calling session is kernel-enforced. The
broker reads `SO_PEERCRED` on each accepted connection to learn
the peer pid, then walks `/proc/<pid>/status` PPid links until it
hits a pid registered via `registerSessionPid` (or it gives up
after 32 steps / pid 1). This means a process inside session A
cannot pretend to be session B -- its ancestors are determined
by the kernel.

The helper itself adds a defence-in-depth check: it refuses to
serve unless `/proc/<ppid>/comm == "git"`, blocking accidental
leaks into chat transcripts, screenshots, and shell history when
a user or agent runs the binary directly. This is **not** a hard
security boundary -- a determined caller can reparent through
git -- but it stops the easy mistakes.

Sources:

  - `swe-swe-server/broker.go` -- listener, peer-cred, ancestry walk
  - `swe-swe-server/cred_store.go` -- in-memory credential and author maps
  - `swe-swe-server/session_gitconfig.go` -- per-session gitconfig file
  - `swe-swe-server/main.go:505-550` -- env injection in `buildSessionEnv`
  - `swe-swe-server/main.go:1209,4467,6606-6609` -- PID registry + cleanup
  - `git-credential-swe-swe/main.go` -- the helper binary
  - `cmd/swe-swe/templates/host/Dockerfile:36-38,231` -- helper installation
  - `research/2026-04-25-per-session-git-credentials.md` -- design doc

@docs Host, Username, Password, Pid
@docs BrokerSocketPath, brokerSocketPath
@docs SessionGitconfigPath, sessionGitconfigPath, sessionGitconfigDir
@docs CredentialBag, AuthorIdent
@docs PidRegistryOp, findSessionForPid
@docs GitCredentialAction, parseGitCredentialAction
@docs BrokerOp, BrokerRequest, BrokerResponse
@docs SessionEnvInjection, sessionEnvInjection
@docs GitconfigSection, gitconfigBody
@docs BrokerLifecycleOp

-}

import Domain exposing (SessionUuid(..))



-- -- Identifiers --------------------------------------------------


{-| Host string from a git credential exchange. Example: `github.com`.
The broker keys credentials by host so a session can hold creds for
multiple remotes simultaneously (e.g. `github.com` + `gitlab.com`).
-}
type Host
    = Host String


{-| HTTPS basic-auth username portion of a credential.

If empty when the broker serves a `get`, the broker substitutes
`x-access-token` -- the conventional username for GitHub PATs.

-}
type Username
    = Username String


{-| HTTPS basic-auth password / PAT. Treated as opaque -- never
logged, never exposed to the browser, never written to disk in this
form (only the SSH signing public key is persisted).
-}
type Password
    = Password String


{-| Linux process id, kernel-reported. Always `> 0` for valid pids.
-}
type Pid
    = Pid Int


{-| Path of the abstract-namespace Unix socket the broker listens on.

The leading `@` makes this an _abstract_ socket (no filesystem entry).
Both the listener (`broker.go:29`) and the helper
(`git-credential-swe-swe/main.go:30`) must agree on this exact name.

-}
type alias BrokerSocketPath =
    String


{-| The single canonical broker socket path. Hard-coded; not configurable.
-}
brokerSocketPath : BrokerSocketPath
brokerSocketPath =
    "@swe-swe-broker"


{-| Filesystem path of a session's `GIT_CONFIG_GLOBAL` file.
-}
type alias SessionGitconfigPath =
    String


{-| Directory under which all per-session gitconfig files live.

Source: `session_gitconfig.go:28`.

-}
sessionGitconfigDir : String
sessionGitconfigDir =
    "/tmp/swe-swe-session-gitconfig"


{-| Compute the gitconfig path for a session.

    sessionGitconfigPath (SessionUuid "abc")
    --> "/tmp/swe-swe-session-gitconfig/abc"

-}
sessionGitconfigPath : SessionUuid -> SessionGitconfigPath
sessionGitconfigPath (SessionUuid sid) =
    sessionGitconfigDir ++ "/" ++ sid



-- -- Stored values -----------------------------------------------


{-| The per-(session, host) record the server holds in
`sessionCreds[sid][host]`. Browser-write-only -- there is no API
for reading credentials back out to the browser.

`token` carries either an HTTPS PAT or a generated bearer secret.
The wire field name on `set_credentials` is `token` (not `password`)
to match common terminology for PATs.

-}
type alias CredentialBag =
    { username : Username
    , token : Password
    }


{-| Per-session git author identity.

Lives at the session level (not per host) since `[user]` in
gitconfig is host-agnostic. The broker keeps this in
`sessionAuthor[sid]`; `writeSessionGitconfigFile` materialises it
into the session's `GIT_CONFIG_GLOBAL` file.

When EITHER `name` or `email` is present, the file gets a `[user]`
section with whichever field(s) are set. When BOTH are empty, the
record is deleted from the map and the section is omitted (the
include-line still applies the user's `~/.gitconfig`).

-}
type alias AuthorIdent =
    { name : String
    , email : String
    }



-- -- PID registry ------------------------------------------------


{-| Operations on the in-memory `pidToSid` map maintained by the
broker. The map is the _only_ link between a kernel pid and a
session uuid -- the broker has no other way to identify a caller.

  - `RegisterSessionPid`: called from `main.go:1209` (initial PTY
    spawn) and `main.go:4467` (worktree migration). Both spawns
    create a new process group via `pty.Start`; the registered pid
    is the group leader.
  - `UnregisterSessionPid`: called from `main.go:1166` (PID change
    on resize), `main.go:1267` (PTY exit), and `main.go:6607`
    (session end). After unregister, ancestry walks from descendants
    will no longer resolve back to this sid.
  - `LookupAncestor`: the broker's read path -- walk PPid up to
    32 steps or pid 1, returning the first registered ancestor.

The 32-step bound is generous for a real shell tree (usually < 10
deep) and prevents pathological loops if `/proc` is malformed.

-}
type PidRegistryOp
    = RegisterSessionPid Pid SessionUuid
    | UnregisterSessionPid Pid
    | LookupAncestor Pid


{-| Walk the PPid chain from `pid` upward. Returns the first
ancestor that has been registered as a session shell, or `Nothing`
if none in the first 32 ancestors is registered.

Modelled as a value-level signature only -- the actual walk is
done in Go (`broker.go:57-76`). The spec records the contract.

-}
findSessionForPid : Pid -> Maybe SessionUuid
findSessionForPid _ =
    Nothing



-- -- The helper protocol -----------------------------------------


{-| The command-line action git asks the helper to perform.

Git's credential-helper protocol: `git-credential-<name> <action>`
with kv-pair stdin and stdout. The helper supports a strict subset:

  - `Get`: git's "give me a credential for this host" call. Reads
    kv lines from stdin, dials the broker, writes kv lines to stdout.
    This is the only action that produces output.
  - `Fill`: legacy alias for `Get` from `git credential <op>` callers.
    Behaves identically.
  - `Store`: git's "remember this credential" call. **Silent no-op** --
    the broker is the source of truth, not git's helper chain.
  - `Erase`: git's "forget this credential" call. Silent no-op
    for the same reason.
  - `Other`: anything else -- silent no-op so future git versions
    don't error.

Source: `git-credential-swe-swe/main.go:36-42`.

-}
type GitCredentialAction
    = Get
    | Fill
    | Store
    | Erase
    | Other String


{-| Map a CLI argument string to a `GitCredentialAction`.
-}
parseGitCredentialAction : String -> GitCredentialAction
parseGitCredentialAction s =
    case s of
        "get" ->
            Get

        "fill" ->
            Fill

        "store" ->
            Store

        "erase" ->
            Erase

        _ ->
            Other s



-- -- The broker socket protocol ----------------------------------


{-| Operations carried over the broker socket as JSON.

Wire shape: a single JSON object per direction, NDJSON-style (one
encode per direction is sufficient because the connection is
short-lived and closed after one round-trip).

  - `BrokerGet`: lookup credential for a host. Request fields:
    `op = "get"`, `host`, `protocol`. The helper passes through
    whatever `protocol` git supplied (`https`, `http`); the broker
    currently does not branch on it.
  - `BrokerSignSsh`: SSH signature request from the
    `git-sign-swe-swe` wrapper. Request fields: `op = "sign-ssh"`,
    `namespace` (defaults to `git` if missing), `data` (base64).
    Used to back `[gpg "ssh"] program = git-sign-swe-swe` for
    commit/tag signing without exposing the private key.
  - `BrokerEcho`: any `op` not recognised falls through to a PoC
    echo handler that returns the request, the resolved sid, the
    peer pid, and a server timestamp. Kept around for the
    `swe-swe-broker-probe` diagnostic tool. Production callers
    do not rely on this.

Source: `broker.go:122-186`.

-}
type BrokerOp
    = BrokerGet
    | BrokerSignSsh
    | BrokerEcho


{-| The request envelope sent from helper -> broker.

Per-`op` field presence:

  - `BrokerGet`: `host`, `protocol`.
  - `BrokerSignSsh`: `namespace` (defaults to `"git"` if absent),
    `dataBase64` (the bytes to sign, base64-encoded).
  - `BrokerEcho`: any subset; the request is echoed back verbatim.

The broker also looks at `peerPid` (via `SO_PEERCRED`) and
`peerSid` (via the ancestry walk); those are NOT in the wire
payload, they are derived kernel-side.

-}
type alias BrokerRequest =
    { op : BrokerOp
    , host : Maybe Host
    , protocol : Maybe String
    , namespace : Maybe String
    , dataBase64 : Maybe String
    }


{-| The reply envelope sent from broker -> helper.

Errors carry the failure mode in `error`; success carries either a
credential pair (for `BrokerGet`) or an armoured signature (for
`BrokerSignSsh`).

  - `CredentialResponse`: success for `BrokerGet`. `username` may
    be the stored value or `x-access-token` if the stored value
    was empty.
  - `SignatureResponse`: success for `BrokerSignSsh`. The armour
    is the SSH-signature blob ready for git to consume.
  - `EchoResponse`: PoC fallback. Carries the resolved sid, the
    peer pid, the original request, and a server timestamp.
  - `ErrorResponse`: failure. Reasons we surface:
      - `peer credentials unavailable` (SO\_PEERCRED failed)
      - `unknown session` (no registered ancestor for peer pid)
      - `missing host` (Get with empty `host`)
      - `no credential for host` (Get for an unstored host)
      - `no signing key for session` (SignSsh with no key set)
      - `data not base64` (SignSsh with malformed `data`)
      - `sign failed` (SSH signer returned an error)

The helper interprets _any_ response with a non-empty `error` as
"no cred available" and falls through silently so git tries the
next configured helper or prompts.

-}
type BrokerResponse
    = CredentialResponse { username : Username, password : Password }
    | SignatureResponse { armor : String }
    | EchoResponse { sid : SessionUuid, peerPid : Pid, timestampUnix : Int }
    | ErrorResponse { error : String, host : Maybe Host, peerPid : Maybe Pid }



-- -- Session env injection ---------------------------------------


{-| The git-related env vars injected into a session shell so git
discovers and uses the broker.

Wire form (output of `buildSessionEnv`):

    GIT_CONFIG_COUNT=1
    GIT_CONFIG_KEY_0=credential.helper
    GIT_CONFIG_VALUE_0=swe-swe
    GIT_CONFIG_GLOBAL=/tmp/swe-swe-session-gitconfig/<sid>

The `GIT_CONFIG_*` triple injects an in-process gitconfig override
that adds `credential.helper = swe-swe` (which makes git invoke
`git-credential-swe-swe` from `$PATH`). The `GIT_CONFIG_GLOBAL`
override redirects `~/.gitconfig` reads to the session-scoped file.

`gitconfigGlobal` is `Nothing` only when `ensureSessionGitconfig`
fails (e.g. tmpfs not writable) -- per-session author identity is
disabled in that case but credential helper still works.

Source: `main.go:534-548`.

-}
type alias SessionEnvInjection =
    { gitConfigCount : Int
    , gitConfigKey0 : String
    , gitConfigValue0 : String
    , gitconfigGlobal : Maybe SessionGitconfigPath
    }


{-| Build the env injection record for a session.

    sessionEnvInjection (SessionUuid "abc")
    --> { gitConfigCount = 1
    -->  , gitConfigKey0 = "credential.helper"
    -->  , gitConfigValue0 = "swe-swe"
    -->  , gitconfigGlobal = Just "/tmp/swe-swe-session-gitconfig/abc"
    -->  }

-}
sessionEnvInjection : SessionUuid -> SessionEnvInjection
sessionEnvInjection sid =
    { gitConfigCount = 1
    , gitConfigKey0 = "credential.helper"
    , gitConfigValue0 = "swe-swe"
    , gitconfigGlobal = Just (sessionGitconfigPath sid)
    }



-- -- Per-session gitconfig file ----------------------------------


{-| Sections that may appear in a session's `GIT_CONFIG_GLOBAL` file.

The file is regenerated on every author / signing-key change by
`writeSessionGitconfigFile`. It is **not** an append log -- previous
contents are clobbered each write.

Order matters because git applies last-wins per key; the layout
chosen by `session_gitconfig.go:62-105` is:

  - `IncludeUserGitconfig`: `[include] path = $HOME/.gitconfig`.
    Always emitted when `$HOME` resolves so user globals
    (safe.directory, init.defaultBranch, aliases) survive.
  - `UserSection`: `[user]`. Emitted when at least one of the
    author identity OR the SSH signing pubkey is set. Body is
    `name`, `email`, and `signingkey` lines as applicable. The
    second argument is the signing pubkey (`Nothing` when no SSH
    signing key is set for the session).
  - `GpgSshProgram`: the `[gpg]` + `[gpg "ssh"]` + `[commit]` +
    `[tag]` block that turns on SSH commit/tag signing. Only
    emitted when a signing key exists -- users without one see no
    behaviour change.

-}
type GitconfigSection
    = IncludeUserGitconfig
    | UserSection AuthorIdent (Maybe String)
    | GpgSshProgram


{-| Compute which sections appear in the body, in order.
-}
gitconfigBody : { home : Maybe String, author : Maybe AuthorIdent, signingPub : Maybe String } -> List GitconfigSection
gitconfigBody { home, author, signingPub } =
    let
        includeSection =
            case home of
                Just _ ->
                    [ IncludeUserGitconfig ]

                Nothing ->
                    []

        userSection =
            case ( author, signingPub ) of
                ( Just a, _ ) ->
                    [ UserSection a signingPub ]

                ( Nothing, Just _ ) ->
                    [ UserSection { name = "", email = "" } signingPub ]

                ( Nothing, Nothing ) ->
                    []

        gpgSection =
            case signingPub of
                Just _ ->
                    [ GpgSshProgram ]

                Nothing ->
                    []
    in
    includeSection ++ userSection ++ gpgSection



-- -- Lifecycle ---------------------------------------------------


{-| Top-level credential-broker operations during a session lifecycle.

These are the points where the broker state is mutated. Modelled as
a flat union so a state-machine test can assert "this user action
produces this op sequence" without modelling the broker internals.

  - `StartListener`: server boot. Opens the broker socket. Fail-open --
    if `Listen` fails the rest of the server still runs; sessions
    just won't reach the broker (`broker.go:84-86`).
  - `RegisterPid`: session shell spawn. Adds the pid -> sid mapping.
  - `UnregisterPid`: session shell exit / migration. Removes the
    mapping. Subsequent helper calls from that pid's descendants
    will see `unknown session`.
  - `EnsureSessionGitconfig`: write the per-session
    `GIT_CONFIG_GLOBAL` file. Called on session spawn (from
    `buildSessionEnv`). Idempotent.
  - `WriteSessionGitconfig`: re-render the file. Called when the
    browser sends `set_credentials` with non-empty author fields,
    or when the SSH signing key changes.
  - `SetCredential`: store a host credential into
    `sessionCreds[sid][host]`. Triggered by
    `PtyProtocol.SetCredentials`.
  - `SetAuthor`: store name/email into `sessionAuthor[sid]`. Empty
    name AND email deletes the entry rather than storing it
    (`cred_store.go:87-90`).
  - `ClearSessionCredentials`: drop credentials, author, and the
    SSH signing key for the session. Called from
    `killSessionProcessGroup` BEFORE `session.Close()` (audit note
    L143-150 -- credential cleanup is folded into the kill step).
  - `RemoveSessionGitconfig`: delete the file. Same callsite as
    `ClearSessionCredentials`.

-}
type BrokerLifecycleOp
    = StartListener
    | RegisterPid Pid SessionUuid
    | UnregisterPid Pid
    | EnsureSessionGitconfig SessionUuid
    | WriteSessionGitconfig SessionUuid
    | SetCredential SessionUuid Host CredentialBag
    | SetAuthor SessionUuid AuthorIdent
    | ClearSessionCredentials SessionUuid
    | RemoveSessionGitconfig SessionUuid
