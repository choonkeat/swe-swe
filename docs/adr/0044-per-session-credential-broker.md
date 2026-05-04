# ADR-0044: Per-session credential broker (SO_PEERCRED + ancestry walk)

**Status**: Accepted
**Date**: 2026-05-04
**Research**: [research/2026-04-25-per-session-git-credentials.md](../../research/2026-04-25-per-session-git-credentials.md)
**Related**: [ADR-0008](0008-forwardauth-unified-auth.md) (single shared password model that motivates this work)

## Context

A swe-swe instance is shared: one `SWE_SWE_PASSWORD`, one container, one `app` UID. Every session shell, agent, and child process runs under the same kernel UID, and `/proc/<pid>/environ` is readable across same-UID processes. Multiple humans collaborating on a single instance need to `git push` as themselves without leaking their HTTPS credentials to siblings.

Three obvious designs all fail:

- **Per-session env vars (`GITHUB_TOKEN`, `GIT_ASKPASS`).** Same-UID `/proc/<pid>/environ` is readable. An env var is a passive long-lived bearer; once leaked, no observability and no revocation.
- **Per-session `SSH_AUTH_SOCK`.** The socket lives on the shared filesystem owned by `app`. Any other session can `connect(2)` to it. SSH primitives all assume per-UID isolation, which we don't have.
- **`--copy-home-paths .gitconfig,.ssh/config` (status quo).** Bakes the operator's identity into the project at init time. Single principal, no per-user attribution, revocation requires re-init.

The PoC in commit `72871cb63` validated a fourth approach -- pass an inherited socket fd into the session shell -- and uncovered two integration failures: bash relocates inherited fds < 10 to a high slot under `FD_CLOEXEC` (so `exec`'d children see fd 3 closed), and claude-code's libuv runtime closes inherited fds before its Bash tool spawns. Both can be patched but the patches accumulate across every shell and agent in the call chain. See the addendum in the research doc for the empirical results.

## Decision

The credential broker uses **`SO_PEERCRED` + `/proc` ancestry walk over an abstract-namespace unix socket** to identify which session is calling. Identity moves from "which fd you have" to "who you are."

Concretely:

1. **swe-swe-server** listens on `@swe-swe-broker` (abstract namespace; `\0swe-swe-broker`). One listener for the whole process; no per-session sockets.
2. On `Accept`, the server calls `getsockopt(SO_PEERCRED)` to read the peer's kernel-reported pid. The peer cannot forge this -- the kernel writes it at `connect(2)` time.
3. The server walks `/proc/<pid>/status` PPid chain upward (max 32 hops, stops at pid 1) until it hits a pid registered by a running session shell. That session's UUID becomes the connection's identity.
4. **Browser writes only.** Credentials enter the server via the authenticated WebSocket (`set_credentials` message), keyed by session UUID. They live in a `map[sessionID]map[host]CredentialBag` plus a parallel `map[sessionID]AuthorIdent` for commit identity. There is no API to read them back to the browser; the only outbound path is the broker's `get` op.
5. **Inside the container**, every session has injected env:
   ```
   GIT_CONFIG_COUNT=1
   GIT_CONFIG_KEY_0=credential.helper
   GIT_CONFIG_VALUE_0=swe-swe
   GIT_CONFIG_GLOBAL=/tmp/swe-swe-gitconfig-<sid>
   ```
   The `GIT_CONFIG_GLOBAL` file holds `[user] name=... email=...` derived from `setAuthor(sid, ...)`. Per-session injection means siblings see different gitconfigs even though they share the FS.
6. **`git-credential-swe-swe`** (`cmd/swe-swe/templates/host/git-credential-swe-swe/main.go`) is the helper binary. On `git`'s `get`/`fill` action, it dials `@swe-swe-broker`, sends `{op:"get", host, protocol}`, and emits the standard `protocol/host/username/password` lines on success. On any error or "no credential" response, it emits nothing -- git falls back to its next configured helper (or prompts the user). Fail-open, never fall through to a different user's creds.
7. **Anti-leak gate.** Before serving, the helper reads `/proc/<ppid>/comm` and refuses unless the parent is `git`. Stops accidental leaks via direct shell or agent invocation (an agent running `printf ... | git-credential-swe-swe get` would otherwise print the password to its own stdout buffer). Not a hard security boundary -- a determined caller can reparent under a process named `git` -- but it eliminates the dominant accidental-leak vectors.

## Why SO_PEERCRED beats fd-passing

The fd-passing design (research doc Option A) assumed every shell and agent in the call chain leaves an inherited fd 3 alone. The PoC found two independent counter-examples in our actual stack:

- **Bash mangles fds < 10 at startup.** Bash treats fds 0-9 as user-redirection space and dups any inherited extras to a high slot (fd 43 in the PoC) with `FD_CLOEXEC` set on the original. `exec`'d children see fd 3 closed. Documented bash behavior since 4.4. Reproduction:
  ```
  $ readlink /proc/$$/fd/3      # parent bash: socket:[26246993]
  $ swe-swe-broker-probe         # write fails: bad file descriptor
  $ SWE_SWE_BROKER_FD=43 swe-swe-broker-probe   # works
  ```
- **claude-code's libuv runtime closes the fd before Bash-tool spawn.** Inside the agent process, the broker socket starts at fd 19 (libuv renumbered it) and is gone by the time the agent's Bash tool spawns a shell. The agent's own `git push` cannot reach the broker via the inherited fd at all.

Each can be patched (`/etc/profile.d` snippet for bash; a fork or wrapper for claude-code), but every new shell or wrapper added to the stack is a fresh integration risk. SO_PEERCRED is the same pattern every long-lived credential agent on Linux already uses (gpg-agent, ssh-agent, polkit, dbus, dockerd) for the same reason: it accepts arbitrary callers and validates identity at connect time, instead of assuming identity is preserved through process spawn.

## Why abstract-namespace, not a filesystem path

`@swe-swe-broker` (Linux abstract namespace, NULL-prefixed) has no filesystem entry. There is no path to mount, permission, traverse, or clean up; no concern about `app`-readable sockets the way `SSH_AUTH_SOCK` would have been. The socket is visible in `cat /proc/net/unix` (informational only), but connecting still has to pass through the kernel-validated peer-pid -> ancestry-walk -> registered-shell-pid pipeline.

## Threat model the broker addresses

| Threat | Outcome |
|---|---|
| Sibling session reads `/proc/<other-pid>/environ` for token | No token in env. Nothing to read. |
| Sibling session opens `/proc/<other-pid>/fd/N` to use someone else's broker connection | `open()` of a unix socket via another process's `/proc/<pid>/fd/N` returns `ENXIO` (kernel-enforced). |
| Sibling session connects to `@swe-swe-broker` and asks for sid X's creds | SO_PEERCRED returns the caller's pid. Ancestry walk lands on the caller's session, not X. Server serves the caller's creds (or no-credential), never X's. |
| Agent (claude-code et al.) silently uses the user's PAT | v1: yes. The agent's processes are descendants of the same session shell, so `findSessionForPID` returns the same sid. This is by design -- the agent must be able to push for the user. Per-push approval gate is deferred to v2. Audit log on every `get` is the v1 mitigation. |
| Direct shell invocation of `git-credential-swe-swe get` to capture stdout | Helper reads `/proc/<ppid>/comm`, refuses unless parent is `git`. |

## Threat model the broker does NOT address

- **Hostile agent producing a malicious commit and pushing it.** The agent is on the same trust level as the user when the user is collaborating with it; the broker's job is credential confidentiality, not commit-content review. (Per-push approval gate is queued for v2; see research doc.)
- **Server compromise.** Tokens live in server memory. A reader-of-process-memory wins. Out of scope -- if the server is compromised, single-tenant guarantees are gone anyway.
- **Sibling Terminal panes that are not the chat session itself.** Settings UI bound to the chat session WS does not populate the sibling terminal's sid. Documented as v1.1 follow-up (research doc Addendum 2 Finding 1). Workarounds: per-user storage scoped above sid, or terminal-pane-inherits-from-parent.
- **TOCTOU between SO_PEERCRED and `/proc/<pid>/status` read.** Same-UID, micro-second window; attacker would need to win a pid-recycling race. Mitigation on Linux >= 6.5 is `SO_PEERPIDFD` (race-free, deferred). Acceptable v1 risk.

## Consequences

**Good.**
- No on-disk credentials. No env-stored bearer secrets. No paths to permission. No agent extension points to coordinate.
- Identity is kernel-validated at connect time, so the helper binary itself contains no trust state -- anything that can `connect(2)` can use it, but the connection's identity is whatever the kernel says it is.
- Works for bash users, the agent, sub-shells, scripts, and the agent's tools uniformly. No per-shell setup beyond `GIT_CONFIG_*` env injection.
- Listener lives in the existing swe-swe-server goroutine pool. No new daemon, no new container, no new compose service.

**Bad.**
- Server holds tokens in memory. Restart loses them; users re-enter via Settings. Acceptable v1; v2 could persist encrypted under a user-derived key.
- Sibling Terminal panes don't inherit the chat session's creds. Known UX gap, fix requires deciding the storage scope.
- The `/proc` ancestry walk is Linux-specific. swe-swe runs in a Linux container so this is fine in production, but the helper binary and broker do not portably build for macOS hosts. (Not a constraint we care about -- the binary lives inside the container.)

## Alternatives Considered

- **Inherited fd (research doc Option A).** Rejected after PoC: bash CLOEXEC-on-relocate and libuv-runtime fd cleanup both break inheritance through the actual call chain. See research doc addendum 1.
- **Per-session unix sockets at `/run/swe-swe/sid-<sid>.sock` with `SO_PEERCRED`.** Slightly weaker variant of the chosen design (path-on-FS, more lifecycle to manage). The abstract-namespace listener subsumes it.
- **One-time random token in env.** Same `/proc/environ` leak as the rejected env-var design. Skipped.
- **Per-session GNUPGHOME / SSH agent.** Rejected for the same reason as `SSH_AUTH_SOCK`: socket lives on shared FS owned by `app`, same-UID processes can `connect`.

## v1.1 follow-ups (open)

Tracked in the research doc Addendum 2; not blocking this ADR.

1. **Settings-UI creds don't reach sibling Terminal panes.** Decide between per-user storage (cleanest UX, requires user identity model), explicit multi-sid save (simple, burdens user), or terminal-inherits-from-parent (smallest code change, adds parent-session concept).
2. **Signing.** The broker's `get` op only delivers HTTPS bearer secrets. SSH commit signing (and later GPG) wants a `sign` op that returns a signature blob. See `tasks/2026-05-04-sshsig-commit-signing.md` for the planned extension.
3. **`SO_PEERPIDFD` on kernels >= 6.5** to close the TOCTOU window between peer-pid read and `/proc` ancestry walk.
4. **Per-push approval gate.** Round-trip a `confirm` event to the session UI on every `get`, blocking the helper until the user clicks. Converts silent agent-driven exfil into a noisy event.
5. **Server-restart persistence.** Encrypt `sessionCreds` under a user-derived key from the auth password.
6. **Device-flow OAuth provider** as a `CredentialProvider` interface implementation, replacing static-PAT storage without touching the broker protocol.

## References

- Research doc: [research/2026-04-25-per-session-git-credentials.md](../../research/2026-04-25-per-session-git-credentials.md) (full design + 2 addenda)
- Code:
  - `cmd/swe-swe/templates/host/swe-swe-server/broker.go` (listener + ancestry walk)
  - `cmd/swe-swe/templates/host/swe-swe-server/cred_store.go` (in-memory store)
  - `cmd/swe-swe/templates/host/git-credential-swe-swe/main.go` (helper binary)
  - `cmd/swe-swe/templates/host/swe-swe-server/main.go:517` (`buildSessionEnv` -- `GIT_CONFIG_*` injection)
  - `cmd/swe-swe/templates/host/swe-swe-server/main.go:4990` (`set_credentials` WS handler)
- Tdspec: `tdspec/audit/2026-05-03-tunnel-mode-and-multi-tab-drift.md` flags this as a missing module; tracked separately.
