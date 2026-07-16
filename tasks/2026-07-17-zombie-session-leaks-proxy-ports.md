# Signal-killed agents leave invisible zombie sessions that squat proxy ports

Status: FIXED. Root cause confirmed and both fixes landed:

- auth symptom (guest 401 on a squatted port) -- a255eee3b
- this leak (reaper predicate + restart guard) -- 6ff87d4e7, see "Fix applied"

This was a resource leak (leaked proxy ports + zombie sessions accumulating
until the next server restart), not an auth hole.

## Symptom (how it surfaced)

A shared-session guest saw `unauthorized` in their Agent Chat pane, while the
session owner saw the pane fine. Reported against a **worktree** session; sharing
the **default-branch** session worked.

Reproduced live: the guest's cookie was present, correctly scoped, and delivered
(port-based `host:24001`, plain http, so not a Secure/https issue) -- yet 401.

## Diagnosis

The per-port proxy listeners (agent-chat/preview/vnc/files, at
`20000 + <agentPort>`) are gated by `requireAuthCookie`. It authorized against
the session UUID captured when the listener was created. An owner uses an
*unscoped* cookie (passes every gate); a guest's cookie is *scoped* to their live
session UUID.

On the live box, the listener on port 24000 rejected BOTH live sessions' scopes
while still admitting the unscoped owner -- i.e. the port was owned by a session
that was not in the live set. Two live sessions cannot share an agent port
(`findAvailablePortQuintuple`), so the owner had to be a dead session.

### Root cause: `ProcessState.Exited()` is WIFEXITED

`(*os.ProcessState).Exited()` reports `WIFEXITED` -- true only for a **normal**
exit. For a **signal-killed** process it is **false**. Verified empirically
(Go 1.23):

```
normal exit : ProcessState!=nil=true  Exited()=true
SIGKILLed   : ProcessState!=nil=true  Exited()=false
SIGTERMed   : ProcessState!=nil=true  Exited()=false
```

So when an agent dies by signal -- **OOM kill (exit 137 = SIGKILL)**, a crash, or
an external kill -- three predicates line up badly:

- `sessionReaper` (main.go ~4283): reaps only if
  `Cmd.ProcessState != nil && Cmd.ProcessState.Exited()` -> **never reaps it**.
- `getOrCreateSession` reconnect cleanup (main.go ~4969): same predicate ->
  **never cleans it**.
- `list_sessions` (main.go ~8786): `if sess.Cmd.ProcessState != nil { continue }`
  -> **hides it**.

Net effect: the session stays in the `sessions` map forever, invisible to
`list_sessions`, still holding all four per-port proxy listeners.

### Why that produces the reported 401

The zombie's *agent* port frees (the process is dead), so a new session is handed
that agent port -- but the zombie's *proxy* port is still bound, so the new
session's proxy listener fails to bind and the zombie's listener keeps serving
the port. Its auth gate is pinned to the dead UUID, so:

- owner (unscoped cookie) -> passes -> "works for me"
- every scoped guest of the new, legitimate session -> 401

Worktree sessions churn and die far more than the long-lived default session, so
they are the ones that land on a squatted port. That is the
"worktree fails / default works" signature.

This ties directly to this box's documented OOM kills -- see
`tasks/2026-07-12-e2e-simple-memory-admission-accumulation.md` (same memory
pressure; OOM-killed sessions are precisely the ones that become zombies).

## Already shipped (a255eee3b, on main)

Auth is no longer affected by an orphaned listener:

- `requireAuthCookie` now takes an authorizer predicate; `scopeOwnsProxyPort`
  resolves the guest's **live** session at request time and admits it iff that
  session currently maps to this proxy port.
- Hardening: `startProxyListener` binds synchronously and stores a real
  `*http.Server` before serving; `Session.closed` + `trackProxyServer` tear down
  (rather than store) a listener created for an already-closed session.

Verified live post-reboot: guest -> own pane = 200; guest -> another session's
port = 401 (isolation intact).

## Fix applied

`Session.reapable()` now reaps on `ProcessState != nil` alone -- `Wait()` having
returned means the process is finished, however it ended. This also makes the
reaper and the reconnect cleanup consistent with `list_sessions`, which already
treats `ProcessState != nil` as "gone".

Sites switched to `sess.reapable()`:
- `sessionReaper`
- `getOrCreateSession` reconnect cleanup

### The restart guard

Broadening the predicate alone would reap a session mid-restart, so
`Session.restarting` (guarded by `mu`) was added.

Note it is NOT needed for a direct `RestartProcess` call: that holds `mu` across
its whole Wait-and-reassign, so an `mu`-synchronized reader can never catch the
transition half-done. The guard exists for `startPTYReader`, which calls
`cmd.Wait()` **outside** `mu` and only afterwards calls `RestartProcess` -- that
window is where a finished old `Cmd` is observable while a restart is pending.
`startPTYReader` sets the flag before the Wait and clears it on every path out
(including a FAILED restart -- holding it there would make the session
permanently unreapable, reintroducing this very leak).

`reapable()` reads under `mu`, which also removes the pre-existing unsynchronized
read of `sess.Cmd` in the reaper.

### Tests (session_reap_test.go)

- `TestSignalKilledProcessIsNotExited` -- pins the Go semantics: SIGKILL/SIGTERM
  give `ProcessState != nil` but `Exited() == false`; a normal exit gives true.
- `TestSessionReapableAfterSignalKill` -- the leak itself.
- `TestSessionReapableAfterNormalExit`, `TestSessionNotReapableWhileRunning`.
- `TestSessionNotReapableWhileRestarting` -- guard holds, and clearing it
  restores reapability.

Both fixes were mutation-tested (restoring the old predicate / dropping the
guard each turns the corresponding test red).

## Note on reclaiming already-leaked ports

Zombie sessions from BEFORE this fix clear on swe-swe-server restart. After the
fix they are reaped within the reaper's 60s tick.
