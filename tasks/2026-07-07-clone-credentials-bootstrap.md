# New Session clone: credentialed clone + fresh-user bootstrap

Executable task plan for `/swe-swe:execute-step-by-step` (via
`/swe-swe:execute-in-worktree tasks/2026-07-07-clone-credentials-bootstrap.md`).
Log convention: `tasks/2026-07-07-clone-credentials-bootstrap.md-phase{N}.log`.

Origin: agent-chat design session 2026-07-07 (session "Clone-repo credentials
UX"). Decisions settled below -- read the whole Design before Phase 1; every
step references it. Companion diagram: `docs/dev/clone-creds-sequence.html`.

## Ground rules for the executing agent

- ASCII only in all code/markdown (no em-dashes, no smart quotes, no non-ASCII).
- Run tests with `make test`, never bare `go test`.
- After ANY change under `cmd/swe-swe/templates/`: `make build golden-update`,
  then `git add -A cmd/swe-swe/testdata/golden` and review
  `git diff --cached -- cmd/swe-swe/testdata/golden` before committing.
- Never log a PAT. The broker already logs username only; keep it that way. The
  clone's `CombinedOutput` must never be echoed with a credential-in-URL (we do
  NOT embed creds in the URL -- keep it that way).
- Line numbers below are approximate anchors; grep the named symbol to locate.
- If any verification fails and a workaround is tempting: STOP and ask via
  send_message. No silent compromises.

## Design (settled -- do not relitigate)

### Problem
Two gaps, one root cause.

1. The homepage "Clone external repository" flow clones with a bare
   `git clone` (`handleRepoPrepareClone`, main.go ~3792) that sets no `cmd.Env`,
   no credential helper, no token -- it runs as an HTTP-handler goroutine
   BEFORE any session exists, so a private HTTPS repo fails with git's raw auth
   error and there is no affordance to supply credentials.
2. A fresh install has an empty `swe-swe-creds:*` localStorage and no session,
   and the homepage exposes NO credential entry point at all. Today the only
   way to clone a private repo is the manual dance: create a blank project,
   hand-edit `.git/config`, save credentials in the session Settings UI, then
   `git fetch` + `git reset --hard origin/main`. This plan removes that dance.

### The shared credential path (reuse, do not fork)
Existing `git fetch`/`git push` inside a live session resolve credentials via:
`git -> git-credential-swe-swe -> broker (@swe-swe-broker: SO_PEERCRED +
findSessionForPID ancestry walk -> sid) -> getCredential(sid, host) -> PAT ->
Remote`. The broker maps a caller to a sid ONLY through `registerSessionPid`
(broker.go) + a `/proc` PPid walk; it never trusts a client-supplied sid.

A pre-session clone can reuse this ENTIRE path by giving the clone process a
server-minted transient sid. The server (not the client) mints and registers
it, so the "never trust a client-supplied sid" invariant holds.

### Shared helper (Phase 1 linchpin)
Add to swe-swe-server:

    // runGitWithTransientCred runs `git args...` with a short-lived, per-call
    // credential context resolvable by the broker. host/username/token may be
    // empty -> behaves like a bare git call (no cred wired).
    func runGitWithTransientCred(host, username, token string, args ...string) ([]byte, error)

Behavior:
1. If token == "": run git with inherited env (current bare behavior). Return.
2. Else mint `transientID` = unguessable random string prefixed `prep-` (must
   not collide with real session sids; use crypto/rand, not Math/time).
3. `setCredential(transientID, host, CredentialBag{Username|"x-access-token", token})`.
4. `defer clearSessionCredentials(transientID)` (or a targeted clear) so it is
   gone whether git succeeds or fails; never persisted.
5. Build env = `os.Environ()` + the three vars buildSessionEnv uses (main.go
   ~628): `GIT_CONFIG_COUNT=1`, `GIT_CONFIG_KEY_0=credential.helper`,
   `GIT_CONFIG_VALUE_0=swe-swe`. Ensure PATH includes git-credential-swe-swe.
6. `cmd.Start()`; `registerSessionPid(cmd.Process.Pid, transientID)`;
   `defer unregisterSessionPid(cmd.Process.Pid)`; `cmd.Wait()`; capture output.
   (Register right after Start and before Wait so the helper -- a grandchild --
   resolves via the ancestry walk. If a race is observed, wrap the clone in a
   pre-forked shell whose pid is registered first.)

All git operations that need pre-session auth funnel through this one helper:
clone (Phase 1), and later the existing-repo background fetch (Phase 4).

### Three-state dialog credential behavior (Phase 2)
The New Session dialog gains a credential entry point (the SAME localStorage
store the Settings UI uses: `swe-swe-creds:<host>`, and the same field
component as `populateCredentialsSection`). Rule: **apply silently if we have
it; only surface fields when the user asks (Change) or when auth fails.**

- State FRESH (no PAT stored for the URL's host): reveal empty credential
  fields with host prefilled from the URL, username default `x-access-token`.
  User enters PAT -> stored to `swe-swe-creds:<host>` -> sent with prepare.
- State TRANSPARENT (PAT stored, valid): do NOT open fields. Attach the stored
  PAT to the prepare POST and clone silently. Show a small non-blocking line
  "Using saved credentials for <host> - Change". Change expands the fields
  PREFILLED (host, username, PAT masked) for one-off override.
- State REJECTED (PAT stored, auth fails): auto-reveal the prefilled fields
  with "Saved <host> token was rejected - update it?"; user fixes PAT -> retry.

Public repos never trigger any of this (bare clone succeeds; no fields).

### Session inheritance + trust pre-seed (Phase 3)
Because the PAT lives in the shared `swe-swe-creds:<host>` store, the session
created on the freshly-cloned repo auto-restores it via the existing
`_maybeAutoConnectSecrets` on connect -> fetch/push inside the repo work with
no extra setup. To avoid a second "trust this device?" prompt: `prepare`
already computes the repo `init_sha` (repoInitSHA, session_gitconfig.go ~365);
return it, and when the user supplied a PAT for the clone, pre-seed the
`(window.location.origin, init_sha)` autosync-trust entry (same key as
`_signingTrustKey`) so auto-restore fires silently for the new repo.

### Redundant-fetch removal (Phase 3)
For a fresh clone the refs are already local, so the dialog's background
`GET /api/repo/branches?...&fetch=1` (`refreshBranchesInBackground`, main.go
fetch ~3939) is redundant. `prepare` returns `justCloned:true`; the frontend
SKIPS the background fetch when justCloned. Net: a new clone makes zero
credentialed remote calls in the branch step (instant `git branch -a` only).

### Non-goals (this plan)
- SSH-URL clone auth (Settings SSH key is signing-only today) -- defer.
- A homepage-level standalone credentials manager -- optional follow-up; the
  shared store makes it additive with zero rework (Phase 4, optional).

## Phase 1 -- server: shared helper + credentialed clone (no UI yet) -- DONE (code+unit tests; manual container verify folded into Phase 2/3)

Steps:
1. [DONE] Implement `runGitWithTransientCred` (see Design). Unit-test it in isolation:
   - token == "" -> runs bare (assert no credential.helper in the child env,
     e.g. by pointing args at `git config --show-origin` or a fake git on PATH).
   - token != "" -> assert setCredential is called with transientID+host and
     that clearSessionCredentials + unregisterSessionPid run even when git
     exits non-zero (defer coverage). A fake `git-credential-swe-swe` +
     in-process broker can assert the helper resolves the transientID.
2. `handleRepoPrepareClone` (main.go ~3750): parse optional
   `credHost/credUsername/credToken` from the request; route the clone through
   `runGitWithTransientCred`. On success return `{path, justCloned:true,
   hasRemote, initSha}`. On auth failure return structured
   `{error, needsAuth:true, host}` (detect via git's "Authentication failed" /
   "could not read Username" / exit code) instead of raw text.
3. `handleRepoPrepareAPI` (main.go ~3653): thread the new fields into the
   request struct; `initSha` via repoInitSHA.
4. `prepare_repo` MCP tool clone case (main.go ~8971): same helper, so agents
   cloning private repos work too. Add optional token arg to `prepareRepoArgs`.

Verification:
- [DONE] `make test` green (new unit tests included: TestGitCredHelperEnv,
  TestRunGitWithTransientCredBare, TestRunGitWithTransientCredWiresAndClears,
  TestCloneNeedsAuth 7/7).
- [DEFERRED to Phase 2/3 container run] Manual (test container): POST
  /api/repo/prepare {mode:clone, url:<private https>, credHost, credUsername,
  credToken} clones successfully; wrong token returns `needsAuth:true`; public
  URL still clones with no creds. Folded into the single Phase 2/3 browser
  session so the container spins up once.
- [DONE] `make build golden-update`; golden diff = clone_cred.go + main.go
  mirror across all init variants (expected, template changed).

## Phase 2 -- frontend: three-state credential UX in the dialog -- DONE

Notes: host derivation done by a unit-tested pure module
(static/modules/clone-cred-host.js, parseCloneHost, exposed to the classic
dialog script via a module shim in selection.html). Design reconciliation:
per the top-line rule ("surface fields only on Change or auth failure"), FRESH
fields appear AFTER a bare clone returns needsAuth, not up front -- this keeps
public repos free of any credential UI.

Steps (static/new-session-dialog.js + page-templates/selection.html):
1. Add a credentials sub-section to the clone form (`#clone-url-field` area,
   selection.html ~1283) reusing the Settings field markup; hidden by default.
2. `prepareRepo` (new-session-dialog.js ~385): for clone mode derive host from
   the URL (reuse parseRemoteHost logic / `_resolvedCredHost` equivalent), read
   `swe-swe-creds:<host>`; if present attach `credHost/credUsername/credToken`
   and render the non-blocking "Using saved credentials for <host> - Change"
   line (TRANSPARENT). If absent, reveal empty fields, host prefilled (FRESH).
3. Handle the response: `needsAuth:true` -> auto-reveal fields prefilled (or
   empty) with the rejection notice (REJECTED); on user submit, write
   `swe-swe-creds:<host>` and retry prepare.
4. "Change" toggles the prefilled (masked) fields for one-off override.

Verification (test container + mcp browser) -- ALL GREEN:
- [DONE] Fresh (clear localStorage): private-no-token clone -> needsAuth
  returns INSTANTLY (see Phase 1 GIT_TERMINAL_PROMPT=0 fix), fields appear,
  host prefilled. Enter token -> written to `swe-swe-creds:<host>`.
- [DONE] Transparent: stored token attached to the prepare POST silently
  (verified in request body), "Using saved credentials for <host> - Change"
  line shown; persists on a successful (public) clone.
- [DONE] Rejected: bad token -> retry -> fields auto-reveal with "Saved
  <host> token was rejected - update it?" notice; token prefilled + masked.
- [DONE] Public repo -> no credential UI at all.
- [DONE] Change -> reveals prefilled (masked) fields for one-off override.
- Positive path (VALID PAT clones private repo successfully): accepted as
  covered by unit + shared-path coverage (user Option 3); not run live.

## Phase 3 -- session inheritance, trust pre-seed, drop redundant fetch -- DONE

Steps:
1. `refreshBranchesInBackground` (new-session-dialog.js ~348): skip when the
   prepare response was `justCloned:true`.
2. On a clone where the user supplied a PAT: pre-seed the `(origin, initSha)`
   autosync-trust entry (same key format as `_signingTrustKey`, terminal-ui.js
   ~3039) using the `initSha` returned by prepare.
3. Verify the created session's `_maybeAutoConnectSecrets` restores the PAT
   with no second trust prompt, and `git fetch`/`git push` inside the cloned
   repo work.

Verification:
- [DONE] Network panel: a fresh clone issues no `&fetch=1` branch call
  (verified live: prepare + exactly one plain `/api/repo/branches`).
- [ACCEPTED - not run live] New session on the cloned private repo: no "trust
  this device?" prompt; in-session `git fetch`/`git push` authenticates via
  the broker. Needs a valid PAT; user chose Option 3. preseedCredsTrust writes
  the same `(origin, initSha)` key format as terminal-ui `_signingTrustKey`.

## Phase 4 (optional follow-up) -- existing-repo fetch + homepage manager

- Route the existing/workspace-repo background fetch (main.go ~3939) through
  `runGitWithTransientCred` when the browser has a PAT for that host, so it
  stops silently soft-failing on private remotes. Low priority; keep separate.
- Optional homepage-level credentials manager (register host PATs up front).
  Additive: same `swe-swe-creds:<host>` store; no server change.

## Commit strategy
One commit per phase. Phase 1 is server-only + unit tests (safe to land
first). Phases 2-3 are the UX. Phase 4 is optional and independent.
