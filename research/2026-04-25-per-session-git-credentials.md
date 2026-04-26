# Per-Session Git Push Credentials in a Shared swe-swe Instance

**Research Date:** 2026-04-25
**Context:** Multiple users share a single swe-swe instance (one auth password, one container, one `app` UID) and want to `git push` as themselves without leaking their credentials to other users in the same instance.

---

## Executive Summary

swe-swe today is a single-credential model: `swe-swe init --copy-home-paths .gitconfig,.ssh/config` bakes the operator's identity into the project at init time, and `SWE_SWE_PASSWORD` (ADR 0008) authenticates everyone as the same principal. There is no per-user credential layer.

The hard constraint is that **only environment variables are isolated between sessions** — files all live under the shared `app` UID, and `/proc/<pid>/environ` is readable across same-UID processes anyway. So the design cannot rely on env-stored secrets, on-disk credentials, or any filesystem-pathed primitive.

The recommended architecture is a **credential broker**:
1. The browser session page collects the user's token + commit identity, persists locally in `localStorage`.
2. Values are pushed over the existing WebSocket/REST channel into swe-swe-server **memory**, keyed by session ID.
3. Inside the container, a tiny `swe-swe-credential-helper` binary plugs into git's standard credential-helper protocol and asks the server for the secret over a **per-session inherited file descriptor**.
4. The secret is returned just-in-time to git's stdin pipeline. It is never written to disk and never set as an env var.

This gives users who configure credentials a working `git push`, leaves users who don't with a clean failure (no silent fallback to someone else's token), and contains the secret to "Go server memory + this one git invocation's stdin pipe" with no leak surface in the shared filesystem or `/proc`.

---

## Why the Obvious Designs Fail

### Single-credential (status quo)
`--copy-home-paths .gitconfig,.ssh/config` writes the operator's identity into the project's `home/` dir. Every session uses it. No per-user attribution; revocation requires re-init.

### Per-session env vars (e.g. `GITHUB_TOKEN`)
Sessions *do* get isolated env, and git supports a number of env-driven credential paths (`GIT_ASKPASS`, `GIT_CONFIG_COUNT/KEY/VALUE`, `gh` reading `GH_TOKEN`). But:

- Same-UID processes can read `/proc/<other-pid>/environ`. Linux YAMA `ptrace_scope` blocks `ptrace(2)` and `process_vm_readv(2)` but not `/proc` reads, which only require the same UID and `dumpable` set. swe-swe sessions all run as `app`, so any session can lift another session's env.
- An env var is a passive, long-lived bearer secret. Once leaked, no observability, no revocation.

### Per-session SSH agent (`SSH_AUTH_SOCK`)
The socket lives on the shared filesystem owned by `app`. Any other session can `connect(2)` to it. Same problem for `~/.ssh/id_*` keys. SSH primitives all assume per-UID isolation, which we don't have.

### One-time tokens injected in env
Same `/proc/environ` problem as above. Any same-UID session can read the token and impersonate.

---

## Proposed Architecture

```
Browser (session page)                Go server                    Container session shell
─────────────────────                 ─────────                    ──────────────────────
[Settings UI: token, name, email]
   │ localStorage (client persist)
   │
   │ WS/REST  ──────────────►  sessionCreds[sid] = {token,name,email}
                                  (in-memory only)
                                                                    git push origin main
                                                                       │
                                                                       │ git invokes
                                                                       ▼
                                                                    swe-swe-credential-helper
                                                                       │ stdin: protocol=https
                                                                       │        host=github.com
                                                                       │
                                                                       │ ask broker over fd 3
                                                                       ▼
                               ◄─── {op:"get", host:"github.com"}
   sessionCreds[sid] looked up
                                ───► {username:"x-access-token",
                                      password:"<TOKEN>"}
                                                                       │
                                                                       │ stdout: username=...
                                                                       │         password=...
                                                                       ▼
                                                                    git completes push
```

### Components

#### 1. Session settings UI

- New "Credentials" section on the session settings page.
- Fields: `git_token`, `git_name`, `git_email`. Optional: `host` for non-GitHub remotes.
- `localStorage` for client-side persistence so the user doesn't re-enter on every reload.
- Submit pushes values to the server over the existing session WebSocket as a `setSessionCreds` message.

#### 2. In-memory credential store (Go server)

- `map[sessionID]CredentialBag` protected by a mutex.
- Cleared on session end.
- Survival across server restarts is **out of scope for v1** — users re-enter on reboot. (v2 could persist encrypted with a user-supplied passphrase.)
- Audit log: every `(sid, host, op, timestamp)` request is appended to a per-session log surfaced in the UI.

#### 3. Credential broker channel

The critical question: how does the helper inside the container prove "I am session X" without sending a forgeable bearer token over a shared-FS path?

**Three options, in order of preference:**

##### Option A — Inherited file descriptor (recommended)

swe-swe-server already spawns the session shell. At spawn time:
1. `socketpair(AF_UNIX, SOCK_STREAM)` returns `(srvEnd, shellEnd)`.
2. `cmd.ExtraFiles = []*os.File{shellEnd}` so the shell inherits it as fd 3.
3. Set env `SWE_SWE_BROKER_FD=3` — just an integer pointer, not a secret.
4. Server retains `srvEnd`, mapped to `sessionID` in a `map[*net.UnixConn]sid`.
5. Children of the shell inherit fd 3 (helper, gh, agent, etc.).

Properties:
- Other sessions' processes do not have this fd. Listing another process's `/proc/<pid>/fd/` shows symlinks but you cannot use those symlinks to communicate on the fd.
- No filesystem path. Nothing to traverse, nothing to permission.
- The fd ↔ sid binding is established at fd creation and is unforgeable.
- Closure on session end is automatic (server closes its end; helper gets EOF).

##### Option B — Per-session unix socket + SO_PEERCRED

Socket at `/run/swe-swe/sid-<sid>.sock`. Server uses `getsockopt(SO_PEERCRED)` on connect to get peer pid, walks `/proc/<pid>/...` to confirm the peer is a descendant of the session's shell pid.

Properties:
- Doesn't require touching shell-spawn flow.
- Slightly weaker: the path is enumerable; relies on a process-ancestry check that has TOCTOU edges. Mitigable but more code than Option A.
- Easy fallback if fd-passing turns out to be awkward to plumb.

##### Option C — One-time random token in env (rejected)

Same `/proc/<pid>/environ` leak as the naive design. Skipped.

#### 4. Credential helper binary

Tiny Go binary, shipped in the container. Wired into git via per-session config injection:

```
GIT_CONFIG_COUNT=2
GIT_CONFIG_KEY_0=credential.helper
GIT_CONFIG_VALUE_0=swe-swe-credential-helper
GIT_CONFIG_KEY_1=credential.useHttpPath
GIT_CONFIG_VALUE_1=true
```

Behavior:
- Parse git's stdin (`protocol=...\nhost=...\nusername=...?\n`).
- Open `os.NewFile(3, "broker")`.
- Write `{op:"get",host:"...",protocol:"https"}` (newline-delimited JSON or length-prefixed).
- Read response. On `{username,password}`, print `username=...\npassword=...\n` to stdout. On `{error}`, exit non-zero.
- No fallback, no caching, no on-disk state.

#### 5. Commit identity

`GIT_AUTHOR_NAME`, `GIT_AUTHOR_EMAIL`, `GIT_COMMITTER_NAME`, `GIT_COMMITTER_EMAIL` are env-only. Name and email are **not secrets** — leaking them across sessions is a privacy nuisance, not a security failure — so injecting them in the session env at shell init is acceptable. Source values from the broker on session start.

(Alternative: the agent or shell could query the broker on every commit and set these per-invocation. Worth doing if the privacy cost matters. Not v1.)

---

## Two Design Choices Worth Deciding Upfront

### Per-push human-in-the-loop confirmation

The agent (Claude et al.) inherits fd 3 too — by design, since you want the agent to be able to push on the user's behalf. This means a hostile prompt or compromised agent can silently exfiltrate the token by issuing a `get` to the broker.

Two levels of mitigation:

1. **No gate (v1).** Agent can push freely. Keep the audit log and surface it prominently in the UI so the user sees what was pushed. Acceptable if push targets are constrained (the agent wouldn't normally push to arbitrary remotes).
2. **Per-push approval gate.** Broker, on receiving a `get`, sends a `confirm` event to the session UI: "Agent wants to push to github.com/foo/bar — Allow once / Allow for 1h / Deny." Blocks the helper until the user clicks. Converts silent exfil into a noisy, user-visible event.

Recommendation: ship v1 without the gate, log every request, add the gate as v2 once the broker protocol is stable.

### Token format and lifecycle

PAT-paste-into-Settings is the easiest v1. Long-lived bearer secrets in browser localStorage are not great — XSS or device theft and the token is loose for a year.

v2 should use **device-flow OAuth** initiated from the Settings UI. Server stores a short-lived access token + refresh token, rotates on the user's behalf. The browser only ever sees a session-bound flag like `connected:true`. The broker protocol is unchanged — only the storage layer swaps.

Design the broker with this swap in mind: `sessionCreds[sid]` should be a `CredentialProvider` interface, not a `(token, expires)` struct. v1 implementation = "static token from UI"; v2 = "OAuth-rotating provider."

---

## Proof-of-Concept Plan

Goal: validate that fd inheritance works through the actual swe-swe spawn pipeline (Go server → PTY shell → grandchildren like `git` → its credential helper) and that sessions are isolated from each other — without disturbing existing functionality if the technique turns out wrong, and with no off-switch beyond `git revert`.

### Design principle: always on, fail open

No `SWE_SWE_BROKER_PROBE` flag. The fd attachment path is wired in unconditionally, but every step that could fail is non-fatal:

- `socketpair(2)` failure → log, skip fd attach, session continues exactly as today.
- Broker goroutine panic → `recover()` + log; session keeps working without the channel.
- Probe binary in the container exits 0 whether fd 3 is wired or not — it's a measurement tool, not a guard.

The bet is that adding one inherited fd to the session shell is harmless to every program that doesn't explicitly look for it. (`socketpair(2)` is essentially a memory-allocation operation; the only failure mode in practice is `EMFILE`/`ENFILE`, and we fail open if it hits.)

### Spawn-point reality check

swe-swe-server has exactly two session-shell spawn sites, both already routed through `buildSessionEnv()`:

- `cmd/swe-swe/templates/host/swe-swe-server/main.go:1143` — `RestartProcess`
- `cmd/swe-swe/templates/host/swe-swe-server/main.go:4342` — initial session create

Both call `pty.Start(cmd)`. `pty.Start` forwards `cmd.ExtraFiles` to `os/exec`, so a single helper called immediately before `pty.Start` does the plumbing for both sites.

### Phase 0 — Baseline measurement

Before any code change, inspect what fds the session shell has at baseline. (Use `echo /proc/$$/fd/*` and `readlink /proc/$$/fd/N` — `ls /proc/self/fd/` is misleading because `ls` itself opens fd 3 to readdir.)

**Finding (2026-04-25):** in current sessions, fd 3 is **already occupied** by a leaked dup of the slave pty, courtesy of how `script(1)` (util-linux) sets up its child shell. Trace:

```
swe-swe-server (193)
└── script -q -f -T <timing> -I <input> -O <log> -c /bin/bash -l   (9702)
    │   fds:  0,1,2 → host-side pty
    │         3     → /dev/pts/ptmx   (master pty — script grabs lowest free fd)
    │         4     → /dev/pts/13     (slave pty)
    │         5     → signalfd
    │         6,7,8 → recording log/timing/input files
    │
    └── /bin/bash -l                                              (9703)
            fds: 0,1,2 → /dev/pts/13  (slave pty via login_tty)
                 3     → /dev/pts/13  ← leaked dup of slave pty
                 255   → /dev/pts/13  (bash's own job-control dup)
```

The leak is functionally harmless (fd 3 is just another handle to the pty, no one reads it), but it confirms the broker design must not assume fd 3 is empty. **It also confirms we cannot let the helper hardcode `3`** — the env-var contract `SWE_SWE_BROKER_FD` is the only safe source of truth.

### Why no renumber preamble

We considered wrapping the spawn with a tiny shell stub that does `exec 9<&3 3<&-` to relocate the broker fd to fd 9 before script runs. **It's not necessary.** Trace what happens when we just inject `cmd.ExtraFiles[0] = brokerChildEnd` directly:

1. swe-swe-server spawns with broker → kernel puts broker on fd 3 (lowest free after 0,1,2 from `pty.Start`).
2. `script` starts with fd 3 already taken by the broker. It opens master pty on fd 4 (next free), slave on fd 5, log/timing/input on 6/7/8, signalfd on 9.
3. `script` forks. Child inherits all fds, broker still on fd 3.
4. Child does `login_tty(slave_fd)` → `dup2(slave, 0/1/2); close(slave_fd)`. login_tty does **not** iterate over high fds — it only touches the slave fd. Broker on fd 3 untouched.
5. Child does its own cleanup of master/log/timing (fds 4,6,7,8). Broker not in script's tracked-fd list.
6. Child execs `/bin/bash`. `exec(2)` preserves all fds without `FD_CLOEXEC`. Go's `os.File` doesn't set CLOEXEC by default for `ExtraFiles`. Broker survives.
7. Bash inherits fd 3 = broker. Bash startup does not touch arbitrary high fds.
8. `git` and the credential helper inherit broker across `fork()` + `exec()`. No CLOEXEC issue.

The "leaked slave pty on fd 3" we saw at baseline goes away *because we inject our broker first* — script picks fd 4 for the master pty instead of fd 3.

The only future failure modes are speculative:
- A swap of `script` for a recorder that does `closefrom(3)` in the child. Modern util-linux doesn't.
- A new bashrc/profile that explicitly does `exec 3<&-`. We grepped the current profile chain — nothing does.
- A future Go change to `os/exec`'s ExtraFiles → fd numbering. Documented and stable.

The smoke test in Phase 3 catches all of these immediately. Not worth pre-engineering around.

### Phase 1 — Inert probe binary

Ship `swe-swe-broker-probe` (~30 LOC Go) at `/usr/local/bin/swe-swe-broker-probe` in the container.

```
fdNum, ok := strconv.Atoi(os.Getenv("SWE_SWE_BROKER_FD"))
if !ok:
    print "SWE_SWE_BROKER_FD not set"; exit 0
fd := os.NewFile(uintptr(fdNum), "broker")
if fd not usable:
    print "fd not available"; exit 0
write {"op":"ping","pid":<pid>} to fd
read one line, print it; exit 0
```

Critical properties:
- **Reads fd number from env, never hardcodes.** Future-proof against changing the number.
- **Always exits 0.** Dropping the binary into the container before the server change is safe — nothing depends on it.

### Phase 2 — Unconditional fd plumbing in swe-swe-server

```go
// fail-open helper alongside buildSessionEnv
func attachBrokerFD(cmd *exec.Cmd, sid string) {
    srv, child, err := socketpair()  // unix.Socketpair(AF_UNIX, SOCK_STREAM, 0)
    if err != nil {
        log.Printf("broker socketpair failed for sid=%s: %v (continuing without)", sid, err)
        return  // FAIL OPEN: session start is never blocked
    }
    cmd.ExtraFiles = append(cmd.ExtraFiles, child)  // becomes fd 3 in child
    cmd.Env = append(cmd.Env, "SWE_SWE_BROKER_FD=3")
    go func() {
        defer func() {
            if r := recover(); r != nil {
                log.Printf("broker goroutine panicked for sid=%s: %v", sid, r)
            }
        }()
        brokerEcho(sid, srv)
    }()
}
```

Add the call site immediately before `pty.Start(cmd)` in both `RestartProcess` (~line 1143) and the initial session-create path (~line 4342).

`brokerEcho` for the PoC is intentionally trivial — JSON in, `{sid, ts, echoed}` out. No real broker, no credential lookup. Just measures whether the bytes arrive and the right `sid` comes back.

### Phase 3 — Verification matrix

With the change deployed, in **session A**:

| # | Test | Expected |
|---|------|----------|
| 1 | `swe-swe-broker-probe` from the session shell | echo with `sid=A` |
| 2 | `bash -c swe-swe-broker-probe` (1 layer) | echo with `sid=A` |
| 3 | `bash -c "bash -c 'bash -c swe-swe-broker-probe'"` (3 layers) | echo with `sid=A` — confirms fd 3 inherits through grandchildren |
| 4 | `GIT_CONFIG_COUNT=1 GIT_CONFIG_KEY_0=credential.helper GIT_CONFIG_VALUE_0='!swe-swe-broker-probe' git credential fill <<<"protocol=https\nhost=example.com\n"` | probe runs as git's helper, echoes `sid=A` — proves git can drive the channel |
| 5 | Open **session B**, run probe | echo with `sid=B`, not A — proves per-session isolation |
| 6 | From session B: try to read session A's broker via `/proc/<sessionA-shell-pid>/fd/<n>` (where `n` = `$SWE_SWE_BROKER_FD` from session A) | EACCES or no useful data — fd table is per-process; the symlink target is visible but you cannot communicate on someone else's open fd as if it were yours |
| 7 | Restart swe-swe-server while session A is open | session A reconnects (existing behavior). New session C gets a fresh broker fd |
| 8 | Run any existing functional test of the session shell | unchanged from baseline — confirms always-on attachment doesn't disturb anything |

Tests 1-4 confirm the inheritance depth we need (`git → helper` is depth 2 from the shell). Test 5 confirms isolation. Test 6 is the security-critical one — if it succeeds, the technique is broken and we fall back to Option B (SO_PEERCRED). Test 8 is the regression check.

### What we explicitly do NOT do in the PoC

- No `credential.helper` wired up by default. Test 4 sets it inline so any failure can't leak into anything real.
- No on-disk state, no `/etc/profile.d/` changes.
- No real broker logic — just echo.
- No UI work. Settings page lands once the server-side mechanic is proven.

### Rollback

The PoC is a single commit touching one server file plus the probe binary. Rollback = `git revert`. Because the runtime path is fail-open, even if the goroutine has a latent bug, the worst case is "the channel doesn't work" — never "session won't start."

### Effort

Probe binary ~30 LOC. Server hook ~40 LOC including the goroutine + recover. Single PR. Touches 1 server file, adds 1 binary.

---

## Open Questions

- **Multiple remotes per session.** GitHub + GitLab + private host. Broker `get` already keys by `host`, so the data model supports it. UI needs to allow "add another credential."
- **Server restarts.** v1 = users re-enter; acceptable but annoying. v2 could persist `sessionCreds` encrypted with a user-derived key from the auth password. Out of scope here.
- **`gh` CLI.** Reads `GH_TOKEN`/`GITHUB_TOKEN` env directly, bypassing the helper. For `gh`, either (a) wrap `gh` in a shim that fetches from the broker and execs `gh` with the token in env (env exists for the lifetime of the gh process, less leak surface than long-lived shell env), or (b) configure `gh` to use git's credential.helper for HTTPS. Option (a) is simpler.
- **Multi-user audit / attribution.** If the swe-swe instance auth gets per-user accounts (currently single shared password — see ADR 0008), the broker can also key on the authenticated user, not just the session, so creds survive across that user's sessions. Today, sessions are the only identity unit available.

---

## Implementation Sketch (rough)

Not a plan, but a rough surface-area estimate so it's not a black box.

| Piece | Effort | Notes |
|-------|--------|-------|
| Settings UI section + WS message | S | Existing session page has settings tab; add a section. |
| In-memory cred store + audit log | S | Simple map + ring buffer. |
| `swe-swe-credential-helper` binary | S | <100 LOC of Go. |
| Server fd-passing on shell spawn | M | Modify shell-spawn path to inject fd 3; the broker goroutine accepts on `srvEnd`. |
| Per-session `GIT_CONFIG_*` injection at shell init | S | Bash profile snippet. |
| `gh` shim | S | Tiny exec wrapper. |
| Audit log UI | S | Surface in session settings. |
| Per-push confirmation gate (v2) | M | Round-trips through the WS to the UI; needs timeout + queueing. |
| Device-flow OAuth provider (v2) | M | Replaces static-token storage backend. |

---

## References

- ADR 0008 — Forwardauth unified auth (single shared password model)
- ADR 0021 — Worktree mount path (relevant for which dir `git push` runs in)
- `tasks/2026-01-08-copy-home-paths.md` — current operator-credential flow
- `research/2026-02-03-fav-3plus2-gap-analysis.md:45` — gap note: swe-swe is single-user-focused
- `research/2026-01-12-swe-swe-sandboxing-integration-analysis.md` — packnplay's per-session credential forwarding (cited as the feature swe-swe lacks)
- `research/2026-01-12-ai-agent-sandboxing-comparison.md:44` — packnplay credential-forwarding scope (Git, SSH, GPG, npm, AWS, Keychain)
- git docs: [credential-helpers](https://git-scm.com/docs/gitcredentials), [GIT_CONFIG_COUNT](https://git-scm.com/docs/git-config#ENVIRONMENT)

---

## Addendum (2026-04-26): PoC results and design correction

The Phase 3 verification matrix was run against commit `72871cb63` in a freshly rebuilt container on 2026-04-25, driven via `mcp__swe-swe__send_session_input` into the user shell session `c3bf1648-...`. Two independent flaws in the original design surfaced. The fd-passing transport itself works as predicted; the integration with real shells does not.

### Phase 3 results

| # | Test | Result | Notes |
|---|------|--------|-------|
| 1 | Probe from session shell | FAIL via `SWE_SWE_BROKER_FD=3`, PASS via `SWE_SWE_BROKER_FD=43` | Bash mangles fd 3 — see "Correction" below |
| 2 | `bash -c probe` | PASS via fd 43 | Same workaround |
| 3 | `bash -c "bash -c probe"` (3 layers) | PASS via fd 43 | Grandchild inheritance works once the fd is correct |
| 4 | `git credential fill` via inline helper | PASS | git invoked the probe; got the JSON echo; `warning: invalid credential line` exactly as predicted |
| 5 | Cross-session distinct sid | PASS by construction | Three live sessions show three distinct socket inodes (`26244787` / `26246993` / `26249223`); probe in shell session `c3bf1648` returned `sid=c3bf1648-...` |
| 6 | Cross-session leak via `/proc/<pid>/fd/N` | PASS — kernel-blocked | `open()` of a unix socket via another process's `/proc/<pid>/fd/N` returns `ENXIO`. Tested with `cat`, `dd`, and Python `os.open()`. `ptrace_scope` did not matter |
| 7 | swe-swe-server restart preserves session A | not tested in this run | Out of scope of this PoC; orthogonal to the broker design |
| 8 | Existing functional behavior of the session shell | PASS | Recording log `session-7ee10e54-...log` continued growing through the run (733KB at end); no regression |

The transport-level claims (per-session isolation, kernel-enforced unforgeable identity, no `/proc` leak) all hold. The integration claim ("bash and the agent will inherit fd 3 cleanly") does not.

### Correction to "Why no renumber preamble"

The doc claimed (Phase 2, step 7):

> *"Bash inherits fd 3 = broker. Bash startup does not touch arbitrary high fds."*

That is wrong. Bash specifically targets fd 3 at startup: it dups the inherited fd to a high slot (fd 43 in this run) and sets `O_CLOEXEC` on the original. Children see fd 3 as closed at `exec(2)` time. Reproduction in the user shell:

```
$ readlink /proc/$$/fd/3       # parent bash
socket:[26246993]
$ swe-swe-broker-probe          # parent execs probe; CLOEXEC closes fd 3 at exec
write to fd 3 failed: write broker: bad file descriptor
$ SWE_SWE_BROKER_FD=43 swe-swe-broker-probe
{"echoed":...,"sid":"c3bf1648-...","ts":...}
```

Cause: bash treats fds < 10 as user-redirection space (so a script can `exec 3<file` without colliding with bash's internal state), and moves any inherited extras into a high slot under `FD_CLOEXEC`. The original is kept open so the parent shell can still reference it via `>&3`, but exec'd children never see it. This is documented bash behavior at least since 4.4.

The renumber preamble we ruled out as unnecessary turns out to be necessary — or, equivalently, swe-swe-server must inject the actual high-slot fd number into `SWE_SWE_BROKER_FD`. Both routes inherit a fragility on bash's choice of relocation slot, which we did not verify is stable across sessions.

### New finding: claude-code closes the broker fd in the agent's child shells

Inside the agent process (claude PID 222 in this run), the broker socket lives at fd 19, not fd 3 — claude-code (built on Node/libuv) renumbered it during its own startup. More importantly, the fd is **not** propagated to shells that claude-code spawns via its Bash tool. Reproduced by running `swe-swe-broker-probe` from inside the agent's Bash tool: same `bad file descriptor` error, but here fd 3 is not even present (CLOEXEC happened at the libuv layer before bash got involved).

This breaks the design intent stated at line 155 of this doc:

> *"The agent (Claude et al.) inherits fd 3 too — by design, since you want the agent to be able to push on the user's behalf."*

In practice, with the fd-passing approach: user shells can use the broker (with the bash workaround); the agent's own `git push` cannot, because the fd is closed before git sees it. This is a second, independent attack on fd-passing — fixing bash does not fix the agent.

### Recommendation flip: Option B (SO_PEERCRED) preferred

The fd-passing approach assumed every shell and agent in the call chain leaves fd 3 alone. Two independent counter-examples emerged in PoC validation. Each can be patched (a `/etc/profile.d` snippet for bash; a claude-code fork or a wrapper for the agent), but the patches accumulate, and every new shell or wrapper added to the stack is a fresh integration risk.

Option B was originally rated "weaker but easier fallback." Given the empirical fragility above, it is the **stronger** choice in practice. It also matches the accept-and-validate pattern used by every long-lived credential agent on Linux — gpg-agent, ssh-agent, polkit, dbus, dockerd's `/var/run/docker.sock` — for the same reason.

### Option B concrete sketch

#### Mental model

> *Instead of "the kernel routes the message to the right session's private socket," it is "the kernel tells us who is calling, and we look them up in our session map."*

Identity moves from "which socket you have" (fd-passing) to "who you are" (PID + ancestry).

#### Transport

- swe-swe-server listens on an abstract-namespace unix socket: `@swe-swe-broker` (NULL-prefixed name; no filesystem entry).
- All sessions share the same listener.
- The credential helper inside the container connects with `net.Dial("unix", "@swe-swe-broker")`.

Why abstract: no file to mount, permission, or clean up; no path-traversal concerns; visible only via `cat /proc/net/unix` (informational; connecting still requires the kernel-validated PID dance below).

#### Identity (server side)

```go
func (b *Broker) handle(c *net.UnixConn) {
    defer c.Close()

    // 1. Get peer credentials at connect time. The peer cannot forge these.
    raw, err := c.SyscallConn()
    if err != nil { return }
    var ucred *unix.Ucred
    raw.Control(func(fd uintptr) {
        ucred, _ = unix.GetsockoptUcred(int(fd), unix.SOL_SOCKET, unix.SO_PEERCRED)
    })
    if ucred == nil { return }

    // 2. Walk the PPid chain until we hit a known session-shell pid.
    sid, ok := b.findSession(int(ucred.Pid))
    if !ok {
        writeJSON(c, map[string]string{"error": "unknown session"})
        return
    }

    // 3. Serve requests bound to sid.
    b.serve(c, sid)
}

func (b *Broker) findSession(pid int) (string, bool) {
    b.mu.RLock(); defer b.mu.RUnlock()
    for ; pid > 1; pid = ppidOf(pid) {
        if sid, ok := b.shellPidToSid[pid]; ok {
            return sid, true
        }
    }
    return "", false
}
```

`b.shellPidToSid` is maintained when sessions spawn / exit — already partially tracked in swe-swe-server's session struct. `ppidOf(pid)` reads `/proc/<pid>/status` and parses the `PPid:` line; about 10 LOC.

#### Identity (helper side)

```go
// swe-swe-credential-helper, replaces the fd plumbing in the current PoC
func main() {
    conn, err := net.Dial("unix", "@swe-swe-broker")
    if err != nil { fail(err) }
    defer conn.Close()

    req := parseGitStdin(os.Stdin)        // protocol=...\nhost=...\n
    if err := json.NewEncoder(conn).Encode(req); err != nil { fail(err) }

    var resp credResponse
    if err := json.NewDecoder(conn).Decode(&resp); err != nil { fail(err) }
    fmt.Printf("username=%s\npassword=%s\n", resp.Username, resp.Password)
}
```

No env var, no inherited fd. The helper is a normal binary doing a normal `connect(2)`. Anything that can run a binary can use it — bash users, the agent, sub-shells, scripts, all the same.

#### Bootstrap

Per-session `GIT_CONFIG_*` injection is still needed (so git invokes our helper for HTTPS remotes). That happens at session shell init and is a single bash profile snippet — same as the original Option A design. No change in surface.

#### TOCTOU mitigation

Between "kernel says peer pid is X" and "I read `/proc/X/status` to walk ancestry", a fast `execve(2)` could in theory swap pid X into something else. Two mitigations, in order of preference:

- **Linux >= 6.5: use `SO_PEERPIDFD`**, which returns a pidfd that survives `execve` and can be `pidfd_open`'d to read `/proc/self/fd/<pidfd>/status` race-free.
- **Older kernels:** accept the small race window. Same-UID, local socket, micro-second between accept and `/proc` read; the attacker would need to win a pid-recycling race against the kernel. Document the limit.

#### Lifecycle

- Server map updated on session create / destroy (already done elsewhere; just add the bidirectional pid index).
- Stale entries swept periodically: `kill(pid, 0) == ESRCH` removes dead pids from the map. Cheap.
- One broker goroutine per accepted connection; closes when the helper disconnects.

#### Migration path from current PoC

1. Land Option B alongside the existing fd code; do not rip the fd path out yet — they can coexist.
2. When `swe-swe-credential-helper` is added (the real binary, replacing the `swe-swe-broker-probe` measurement tool), wire it to use Option B by default.
3. Once Option B is validated in production, remove `attachBrokerFD`, `brokerEcho`, the env var, and the probe binary's build stage. Keep the probe in `research/` as a reference exhibit.

#### Effort vs original Option A

| Piece | Option A (fd-passing) | Option B (SO_PEERCRED) |
|-------|-----------------------|------------------------|
| Helper binary | ~50 LOC | ~50 LOC |
| Server hook | ~40 LOC (`attachBrokerFD` + `brokerEcho` + spawn-site changes in 2 places) | ~60 LOC (single listener goroutine + ancestry walk) |
| Container template surface | new probe binary, Dockerfile build stage, two spawn-site edits | none — listener lives wherever swe-swe-server's other goroutines do |
| Per-shell setup | `/etc/profile.d` patch + bash CLOEXEC workaround | none |
| Agent integration | requires claude-code change or wrapper | works as-is |

Net: Option B is similar code volume, fewer files touched, and substantially more robust against shell/agent variation.

### What stays valid from the original doc

- The session-settings UI, in-memory cred store, audit log, commit-identity env vars, `gh` shim, per-push approval gate, device-flow OAuth (Sections 1, 2, 4, 5 and "Two design choices") are all transport-agnostic. They apply unchanged under Option B.
- The "no on-disk credentials, no env-stored bearer secrets" constraint is preserved — Option B touches neither.

### What this addendum supersedes

- "Why no renumber preamble" — the empirical claim is wrong; preamble (or fd-renumbering, or Option B) is required.
- The Option-A-as-recommended ordering in "Three options, in order of preference."

## Addendum 2 (2026-04-26): post-deploy validation findings (v1.1 follow-ups)

After the Option B implementation shipped (commits `ff110181c` through `42688cd1d`), we walked it through a real-environment validation and surfaced two follow-ups for v1.1.

### Finding 1: Settings-UI-bound creds don't apply to free Terminal panes

**Symptom.** User saves a PAT via the chat session's Settings UI, then runs `git pull` in a "Terminal" pane (the free-shell session, not the agent's pane). Git falls through to a username prompt. Server log shows `[BROKER] sid=<terminal-sid> no credential for host="github.com"`.

**Root cause.** The Settings UI is bound to a single WebSocket conn, which is scoped to one session UUID. Free Terminal panes spawned alongside that chat session are *separate sessions* with their own UUIDs — they have a sibling relationship, not a parent-child one. `setCredential(sess.UUID, host, ...)` keys the cred map by the WS session, so siblings never see it.

**Validation evidence.**
| Pane | sid | Has creds? | git pull |
|---|---|---|---|
| Chat (LLM agent's pane) | `0a4f5d15-...` | yes | succeeds silently |
| Free Terminal (sibling) | `0ab49df0-...` | no | falls through to prompt |

This is *also* the security claim — sibling sessions cannot read each other's creds. So the same property is both the v1 security guarantee and the v1.1 UX gap. Any fix must preserve sibling-isolation between *different users' sessions* while making *the same user's sibling sessions* shareable.

**Design space for v1.1.**

1. **Per-user creds, scoped above session.** Store at the user level (whatever auth identity the WS connects as) and serve to any session belonging to that user. Cleanest UX, requires the broker to also know "what user does this sid belong to."
2. **Settings UI exposes session UUIDs and lets you save against multiple.** Explicit, no design change to the broker, but burdens the user.
3. **Terminal panes inherit from their parent chat session.** Track parent-child relationships in `pidToSid`; broker walks both up the PPid chain *and* sideways to a parent session's sid if the current sid has none. Keeps the broker self-contained but makes "parent session" a load-bearing concept.

Option 1 fits cleanest with the existing user identity model. Option 3 is the smallest code change but adds a new concept.

### Finding 2: helper output is leakable when invoked outside git

**Symptom.** During diagnostics, `printf 'protocol=https\nhost=github.com\n\n' | git credential-swe-swe get` prints the password line to stdout. If that runs inside an agent's tool buffer or a logged shell, the PAT enters the chat transcript / log. Normal git usage never leaks because git owns the helper's stdout pipe.

**Fix shipped same-day** (`feat(broker): credential helper refuses helper invocation outside git`). The helper now reads `/proc/$PPID/comm` at the top of `get`/`fill` actions and exits 1 with a stderr warning if it isn't `"git"`. Real git invocations are unchanged; direct shell/agent invocations are blocked before any `password=` line is written.

Not a hard security boundary (a determined caller can `exec` itself under a process named `git`), but it stops the dominant accidental-leak vectors: agents running diagnostic pipes, screenshots, shell history, copy-pasted command output. Friction-as-defense.

**Open question for v2.** Should we also gate on `isatty(0)` being false? Git always pipes stdin to the helper. A direct caller using `printf | helper` also pipes stdin, so that check alone doesn't add coverage; but `helper get` typed at a prompt with no redirection (where stdin IS a tty) would be caught. Worth pairing with the parent-comm check for belt-and-suspenders. Defer to v2 unless we see an actual instance.
