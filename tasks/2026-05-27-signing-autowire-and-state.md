# Per-session signing/credential auto-wire + connect-time state

## Status

**In progress.**
- [x] **Phase 0** -- per-sid serialize + atomic-write (gitconfig/allowed_signers). Done 2026-05-27.
- [x] **Phase 1** -- connect-time state snapshot + broadcast + SSH-pane indicator. Done 2026-05-27.
- [x] **Phase 2** -- server-side author-email derivation (effective git email fallback for allowed_signers). Done 2026-05-27. Combined Phase 1+2 browser e2e PASSED: key + local identity, no Save -> "verifies locally" + `git commit -S` verifies via `git log --show-signature`; negative local gpg override -> "inactive -- local .git/config override". See -phase1.log.
- [x] **Phase 3** -- trust-gated HTTPS auto-send + host autofill + "Forget HTTPS on this device". Done 2026-05-27. e2e PASSED: host autofill -> origin host; trusted new session auto-sends creds+key in ONE combined message (gitconfig written once); Forget clears shared trust + PAT; untrusted session sends nothing. See -phase3.log.
- [ ] Phase 4 -- adjacent papercuts

Follow-up to the SSH commit-signing work
(`tasks/2026-05-04-sshsig-commit-signing.md`, shipped in v2.24.0). That
feature made signing *possible*; this one makes it *reliable on revisit*
and *legible without re-Saving* -- and serializes the per-session
gitconfig writes those features all funnel through.

## Problem

On a fresh page load of an existing session (same browser, localStorage
already populated), the three pieces of signing setup behave
asymmetrically:

- **Signing key** auto-wires on `ws.onopen` via
  `_maybeAutoRestoreSigningKey()` (terminal-ui.js:2715), gated on a
  per-device `(origin, init_sha)` trust entry + TLS/loopback + no
  passphrase. Its "On" badge is driven by the server `signing_key_stored`
  ack, so it reflects real server state.
- **HTTPS PAT + author name/email** do NOT auto-wire. The form is
  rehydrated from localStorage (`populateCredentialsSection`,
  terminal-ui.js:3112) but `set_credentials` only fires on a manual
  **Save** (`_saveCredentials`, :2448). The server never *pushes* its
  current credential/author state on connect -- it only echoes state in
  acks to `set_*` messages.

Consequence chain (the bug that started this): `allowedSignersFile` is
only emitted by `writeSessionGitconfigFile` (session_gitconfig.go) when
`getAuthor(sid).Email != ""`, and that email arrives *only* via the HTTPS
Save. Since the Host field hardcodes `github.com` (terminal-ui.js:590)
with no autodetect from the workdir `origin`, a GitLab user never even
sees their creds until they manually switch Host + Save. Until then: no
session author email -> no `allowedSignersFile` -> `git log
--show-signature` fails locally, with no visible reason.

Two user-facing asks:

1. Same browser revisiting a session should "just work" without
   re-Saving.
2. The panel should show, accurately, what is already set up server-side
   -- so the user does not Save "just in case".

A third, surfaced in review: every one of these features funnels through
`writeSessionGitconfig`, which is **not concurrency-safe** today (see
below). Auto-wire + multi-browser turns a latent race into a frequent
one, so the serialization fix is a prerequisite, not an afterthought.

## Goals / non-goals

- **Goal:** signing verifies locally with zero clicks whenever the repo
  already has an identity, for any browser/session.
- **Goal:** the Settings panel reflects true server-side state on load
  and explains *why* signing is inactive when it is.
- **Goal:** same-browser revisit re-establishes the PAT without a manual
  Save, under the same trust gate signing already uses.
- **Goal:** concurrent/multi-browser writes to the per-session gitconfig
  never produce a torn file or a dangling `allowedSignersFile`.
- **Non-goal:** auto-leaking a PAT to a *different* operator/browser. The
  per-device trust + secure-context gate stays the hard boundary.
- **Non-goal:** changing the on-disk secret model. Keys/PATs remain
  server-memory-only; passphrases never persist.

## Concurrency model (frames every phase)

Current state: each store map is independently `RWMutex`-guarded
(`sessionCredsMu`, `sessionAuthorMu`, `sessionSigningKeyMu` in
cred_store.go / sign_store.go). But there is **no lock around the
gitconfig file write**, and `writeSessionGitconfigFile` does **direct
`os.WriteFile`** (not atomic temp+rename) on **two files that must stay
mutually consistent**: the gitconfig and the `<sid>.allowed_signers` it
references. Multiple WS connections to one `sid` each run their read loop
on a separate goroutine, so handlers run concurrently.

Race inventory (each phase's specific race is noted in its section):

- **Latent today:** two concurrent `writeSessionGitconfig(sid)` calls
  non-atomically rewrite the same path; the agent's `git` can read a
  half-written gitconfig. Rare now because one connection's messages are
  sequential.
- **Phase 2/3 amplify it:** a `git config` subprocess inside the write
  path widens the two-file window; connect-time auto-send fires two writes
  at once; N browsers reconnecting = 2N concurrent rewrites of one
  session's files; TOCTOU against an in-flight `git commit -S`.
- **Phase 1's own race:** a torn read across the three store maps + a
  stale-across-co-viewers snapshot (acks go only to the writing `conn`,
  not broadcast).

**Phase 0 exists to close all of the file-write races up front** so the
later phases can fire writes freely.

## Phase 0 -- Serialize + atomic-write the gitconfig/allowed_signers pair (prerequisite)

This is the foundation; do it first. It also fixes the pre-existing
latent race, so it stands on its own merits.

**Server (session_gitconfig.go):**
- Add a **per-sid write mutex** (a `map[string]*sync.Mutex` keyed by sid,
  or a sharded/single mutex) held across the whole "assemble contents +
  write allowed_signers + write gitconfig" sequence, so the two files are
  always a consistent pair and concurrent writers serialize.
- **Atomic writes:** write to `<path>.tmp` then `os.Rename` (atomic on the
  same filesystem) so `git` never reads a partial file. Order: write +
  rename `allowed_signers` first, then write + rename the gitconfig that
  references it. In `removeSessionGitconfig`, remove the gitconfig
  **before** the allowed_signers (never leave a live `allowedSignersFile
  =` pointing at a deleted file).
- Resolve any subprocess-derived inputs (Phase 2's effective email)
  **outside** the mutex and pass the value in -- never shell out while
  holding the lock.

**Tests:** a concurrency test that fires N goroutines calling
`writeSessionGitconfig(sid)` with different author/key combinations and
asserts the final gitconfig + allowed_signers are internally consistent
(the `allowedSignersFile` path exists and its content matches the author
email in `[user]`), and that no intermediate read sees a truncated file
(read-in-a-loop while writers run). Reuse the `sessionGitconfigDir`
redirect-to-`t.TempDir()` already added in the v2.24.0 work.

## Phase 1 -- Connect-time state snapshot

Make the panel tell the truth on load.

**Server.** In the session WS handler (main.go, immediately after upgrade
+ auth, near the existing initial `status` send around main.go:834),
send:

```jsonc
{
  "type": "session_cred_state",
  "hosts": ["sgts.gitlab-dedicated.com"], // listCredentialHosts(sid)
  "author_email_set": true,               // getAuthor(sid).Email != ""
  "signing_fingerprint": "SHA256:...",     // getSigningKey(sid) if present
  "local_gpg_overrides": "",               // readLocalSigningOverrides(workDir)
  "signing_active": true,                  // computed, see below
  "signing_inactive_reason": ""            // "no email" | "local .git/config override" | "no signing key" | "passphrase needed this session"
}
```

`signing_active` = signing key present AND an author email is resolvable
(session author OR workdir effective email, see Phase 2) AND
`readLocalSigningOverrides` returned empty.

**Race + mitigation (Phase 1-specific):** assemble the snapshot under a
single short critical section (snapshot all three maps together) so
`signing_active`/reason cannot reflect a half-applied update. And
**broadcast** cred/signing-state changes to all conns of the `sid`
(not just the acking one) so co-viewers do not go stale; the state is
idempotent, so guard only against an echo loop, not against ordering.

**Frontend.** Add `case 'session_cred_state'` to the message switch
(terminal-ui.js:1492 region): set `_credsStoredHosts`,
`_signingFingerprint`, and new `_signingActive`/`_signingInactiveReason`,
then `_refreshCredsStatus()` + `_refreshSigningStatus()`. The badges
(`_refreshSettingsNavBadges`) are already server-sourced -- this just
feeds them on load. Add a one-line SSH-pane indicator: "Signing: verifies
locally" (ok) or "Signing: inactive -- <reason>" (warn).

**Tests:** unit-test the `signing_active`/reason computation
(table-driven). e2e: load a session with a stored key + local git
identity, assert the indicator reads "verifies locally" with no Save.

## Phase 2 -- Server-side author-email derivation (browser-independent)

Make `allowedSignersFile` emit without depending on `set_credentials`.

**Server, `writeSessionGitconfigFile` (session_gitconfig.go).** When a
signing key is set but the session author email is empty, fall back to the
workdir's *effective* git email (`git -C <workDir> config user.email`,
which honors local-then-global precedence -- the email commits are
actually authored as) as the allow_signers principal. Skip only when
neither is available. Factor an `effectiveGitEmail(workDir)` helper next
to `readLocalGitUser` (use `git config` so global is included, not just a
`.git/config` parse).

**Race + mitigation:** the `git config` subprocess MUST run outside the
Phase 0 mutex (resolve the value first, pass it in) -- otherwise it blocks
all author/cred ops for the subprocess duration. Cache it per session
(refresh on `set_credentials`) to avoid a fork on every rewrite.

**Why it matches:** SSH verify matches the signature principal against the
*committer* email; local `.git/config [user] email` wins for that, so
deriving from the effective email keeps principal and committer in
lockstep -- exactly why the manual fix in this thread worked once the
right email reached the server.

**Tests:** extend `session_gitconfig_test.go` -- key set + no session
author + workdir email present => allow_signers written with that email +
`allowedSignersFile` line emitted. Key set + no email anywhere => skipped
(no behavior change).

## Phase 3 -- Trust-gated HTTPS auto-send + host autofill (the "just works")

Bring HTTPS up to parity with signing for the same-browser case.

- **Host autofill.** Add `LocalRemoteHost` to the page-data struct
  (main.go:2396 region) via a `readLocalRemoteHost(workDir)` helper
  (`git -C workDir remote get-url origin`, parse host from scp-style or
  URL form). Inject as `data-local-remote-host`. In
  `populateCredentialsSection`, default the Host input to it (falling back
  to `github.com`).
- **Auto-send on connect.** Generalize the signing trust path: on
  `ws.onopen`, if a `(origin, init_sha)` trust entry exists AND
  `_signingAutoSendSafe()` passes AND localStorage has creds for the
  resolved host, auto-send credentials -- mirroring
  `_maybeAutoRestoreSigningKey`.
- **Forget control + scope.** Add "Forget HTTPS on this device" paralleling
  the signing one; reuse the existing trust entry so one decision covers
  both PAT and key.

**Race + mitigation (Phase 3-specific):** to avoid two concurrent rewrites
on connect, send **one combined** message (creds + key together) so the
server writes the gitconfig once; or debounce server-side so multiple
`set_*` within a short window collapse to a single rewrite. With Phase 0
serialization the worst case is harmless last-writer-wins, but coalescing
avoids the redundant 2N-write thundering herd when many browsers
reconnect.

**Security framing (call out in review):** auto-sending a stored PAT on
connect widens the window where a browser-held secret leaves the browser.
It is gated identically to the shipped signing auto-restore (explicit
per-device trust bound to `(origin, init_sha)` + TLS/loopback only); the
recycled-hostname attack is covered by the `init_sha` binding. No new
trust assumption beyond what signing already made.

**Tests:** e2e -- trusted device, reconnect, assert "Stored on server for:
<gitlab host>" with no manual Save and a commit verifies. Untrusted /
insecure-context -- assert nothing auto-sends.

## Phase 4 -- Adjacent papercuts (cheap, independent)

- **GitLab-aware Test connection.** `testGitCredentials` (main.go:8658)
  does `GET https://{host}/` for non-github hosts -> 404 on GitLab. Detect
  GitLab (host contains `gitlab`, or try `/api/v4/user` with
  `PRIVATE-TOKEN`/bearer and fall back to the generic GET).
- **Verify-key no-op.** `_verifySigningKey` (terminal-ui.js:2562) sends
  only the textarea value; on rehydrate the textarea may be empty though a
  key is registered server-side, so it silently does nothing. Either
  rehydrate the textarea from `swe-swe:signing-key:<fp>` before verify, or
  add a server-side `verify_stored_signing_key` op that verifies the
  already-registered key without re-sending the PEM. Confirm exact current
  behavior with a browser test first.

## Sequencing

1. **Phase 0** -- per-sid serialization + atomic writes. Prerequisite for
   everything else; also closes the pre-existing latent race. Ship alone.
2. **Phase 1** -- state snapshot (+ broadcast). Removes "Save just in case"
   and surfaces reasons. No new secret surface.
3. **Phase 2** -- email derivation. Fixes the verify-fails bug for
   everyone; no client work, no new secret flow.
4. **Phase 3** -- HTTPS auto-send + host autofill. The only phase with a
   security tradeoff; do last, behind the existing trust gate, with an
   e2e for the untrusted path.
5. **Phase 4** -- anytime; independent.

Phases 0 + 1 + 2 together resolve the user's core complaint (safe,
legible, works without clicks for the identity/signing side). Phase 3 is
the full hands-off PAT experience.

## Conventions

- Templates live under `cmd/swe-swe/templates/host/swe-swe-server/`;
  changes there require `make build golden-update` and committing the
  regenerated `cmd/swe-swe/testdata/golden/` (see CLAUDE.md). The Go
  server source is part of the binary, not re-emitted at runtime, so an
  existing workspace needs a binary upgrade + restart to get these.
- ASCII-only source (`make ascii-check`).
- Tests via `make test`.
- e2e via `make e2e-up-simple` (port 9780) per
  `docs/dev/swe-swe-server-workflow.md`.
