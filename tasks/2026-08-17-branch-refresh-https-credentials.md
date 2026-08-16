# Branch refresh over HTTPS: "Unable to fetch latest changes. Using cached branches."

Date: 2026-08-17
Status: IMPLEMENTED (uncommitted). `make test` green, golden files regenerated,
9/9 e2e new-session-dialog tests green against a live container.

Landed as:

- `swe-swe-server/branch_refresh.go` (new): httpsRemoteHost, the 20s-bounded
  fetch, and the warning wording. Tests in `branch_refresh_test.go`.
- `clone_cred.go`: context-aware `runGitWithTransientCredContext` plus
  `killGitGroupOnCancel` -- killing the direct child was not enough, git's
  https helper keeps the output pipes open, so the whole process group is
  killed and the post-kill wait is capped.
- `main.go`: branches endpoint takes POST with credentials; prepare and
  branches both return `remoteHost`.
- `static/combo-box.js`: setOptions preserves the filter and the highlight.
- `static/new-session-dialog.js`: refresh POSTs the saved token, is abortable,
  and clears a stale warning.
- `e2e/tests/new-session-dialog.spec.js`: three new tests (token attached,
  refresh does not disturb the branch box, slow refresh does not block Start).

## Symptom

On installs whose repo remote is HTTPS and whose sign-in details (username +
personal access token) are entered in swe-swe session settings, the New Session
dialog shows:

    Unable to fetch latest changes. Using cached branches.

Inside a session on the same install, `git fetch` and `git push` both work.

## Root cause

`/api/repo/branches?fetch=1` runs the refresh in the swe-swe server process,
which has no credentials.

- `cmd/swe-swe/templates/host/swe-swe-server/main.go:4366`

      exec.Command("git", "-C", repoPath, "fetch", "--all").CombinedOutput()

  Plain command. No credential helper wired, no token, and no
  `GIT_TERMINAL_PROMPT=0`.

- Credentials are stored per session, in memory, keyed by session id
  (`cred_store.go`: `sessionCreds[sid][host]`), and reach git only through the
  broker path `git -> git-credential-swe-swe -> @swe-swe-broker -> sid`.
  The New Session dialog runs before any session exists, so nothing is
  resolvable.

- SSH remotes are unaffected: the server reads its own key file, so the fetch
  succeeds without any typed-in token. That is why the dev box (origin is
  `git@github.com:choonkeat/swe-swe.git`) never shows the warning.

The homepage clone flow already solved the identical problem:
`clone_cred.go` mints a transient session id, stores the caller-supplied token
under it, registers the git pid, runs git, then clears both
(`runGitWithTransientCred`). Callers: `main.go:4114`, `4136`, `9964`, `9971`.
The branch refresh never got that treatment.

## Impact

Freshness only. The dropdown falls back to the branch list already on disk, so
a branch pushed elsewhere in the last few minutes is missing until something
inside a session fetches. Session creation itself is unaffected.

## Non-negotiable constraints

C1. A slow or hung refresh must never delay creating a session.
C2. A refresh result must never disturb what the user typed or is choosing in
    the branch box.

Current state against them:

- C1, client: already satisfied. `refreshBranchesInBackground` is fire-and-
  forget (`new-session-dialog.js:452`) and Create does not await it.
- C1, server: NOT satisfied. The fetch has no timeout
  (`exec.Command`, `main.go:4366`), so a stalled HTTPS connection keeps a git
  process and an HTTP request alive indefinitely, and can hold git ref locks
  that make a concurrent `git worktree add` (`main.go:4515-4521`) fail with
  "cannot lock ref".
- C2, typed text: already satisfied. `setOptions` never touches `.value`.
- C2, open dropdown: NOT satisfied. `setOptions` calls `_renderOptions()` with
  no filter (`combo-box.js:285-292`), which re-renders the FULL list and resets
  `#activeIndex = -1` (`combo-box.js:517`). If the refresh lands while the list
  is open and filtered to what the user typed, the suggestions jump back to
  every branch and the keyboard position is lost -- so the next Enter or arrow
  key can pick a different branch than the one they were on.

## Chosen approach

Browser supplies the token; server borrows it for one command.

The dialog already keeps per-host tokens in `localStorage` under
`swe-swe-creds:<host>` (`new-session-dialog.js:509-529`) and already sends
`credHost` / `credUsername` / `credToken` on the clone path. Reuse that.

Rejected alternative: have the server reuse a credential stored by some other
live session. It breaks when no session is open or after a server restart, and
it lets one session's token serve another browser's request -- a new trust
surface. The browser already holds the token, so client-supplied adds none.

## Steps

### Step 1 -- server: expose the repo's remote host

Add a Go `parseRemoteHost(url) string` mirroring
`static/modules/clone-cred-host.js` (handles `https://host/...`,
`https://user@host:port/...`, `git@host:owner/repo`, `ssh://git@host/...`;
lowercased; port stripped).

Return `remoteHost` in two responses so the dialog knows which localStorage
entry to read:

- `handleRepoPrepare` (near `main.go:4079`, where `hasRemote` is set)
- `handleRepoBranchesAPI` (`main.go:4384`, the `branchesResponse` map)

Source the URL from the existing `getRepoOriginURL(repoPath)` (`main.go:3799`),
falling back to the first remote when `origin` is absent. Emit `remoteHost`
only for HTTPS remotes -- SSH needs no token and must not trigger a prompt.

### Step 2 -- server: accept credentials on the refresh

Add `POST /api/repo/branches` alongside the existing GET, body:

    { "path": "...", "fetch": true,
      "credHost": "...", "credUsername": "...", "credToken": "..." }

Never accept the token as a query parameter -- URLs land in access logs.

Replace the bare `exec.Command` at `main.go:4366` with:

    runGitWithTransientCred(credHost, credUsername, credToken,
        "-C", repoPath, "fetch", "--all")

That path also sets `GIT_TERMINAL_PROMPT=0`, which fixes a second latent bug:
today's bare command can block on git's username prompt if the server ever has
a terminal attached.

Keep the soft-fail contract (cached branches always returned), but split the
warning using the existing `cloneNeedsAuth(output)` (`clone_cred.go:30`):

- auth failure -> `"Sign in to refresh branches: no HTTPS credentials saved for <host>."`
- anything else -> current wording, unchanged.

Keep the GET route working with no credentials so old cached frontends and the
existing tests keep passing.

### Step 3 -- client: send the saved token with the refresh

In `new-session-dialog.js`:

1. Store `dialogState.remoteHost` from the prepare response.
2. `refreshBranchesInBackground(repoPath)` (line 452): read
   `readCloneCreds(dialogState.remoteHost)`; if a token exists, POST the body
   from Step 2; otherwise POST without credentials (same as today).
3. On the auth-failure warning, reuse `revealCloneCredFields(...)` so the user
   can paste a token in place rather than reading a dead-end message. Save via
   the existing `writeCloneCreds`.

Never log or display the token.

### Step 4 -- never block Create (constraint C1)

Server:

- Run the fetch under `exec.CommandContext` with a 20s timeout, so a stalled
  connection cannot pin a git process or an HTTP request. On timeout: cached
  branches plus `"Refresh timed out. Using cached branches."`
- Fetch only; never write to the working tree from this endpoint.

Client:

- Give the refresh an `AbortController`; abort it when the dialog closes, when
  the repo selection changes, and when Create is submitted. A dropped refresh is
  harmless -- the cached list is already on screen.
- Keep the existing late-result guard (`dialogState.repoPath !== repoPath`).

Both together mean the worst case is a stale branch list, never a wait.

### Step 5 -- never disturb the branch box (constraint C2)

In `combo-box.js`, make `setOptions` preserve the live UI state:

1. Re-apply the current filter instead of clearing it -- reuse the exact
   expression `_open()` already uses (`combo-box.js:567`):

       this._renderOptions(
         this._input.value === this._labelForValue(this.#value) ? "" : this._input.value
       );

2. Preserve the keyboard position: capture the currently highlighted option's
   value before the re-render and re-highlight the same value if it is still
   visible, instead of the unconditional `#activeIndex = -1`.

3. Leave `.value` and the input text untouched, as today.

In `new-session-dialog.js`, `populateBranches` should also clear a previously
shown warning when a later refresh succeeds (today `warningDiv` is only ever
set, never hidden, so a stale error can outlive the failure).

Node test in `static/modules/` (or a combo-box test file) covering: options
replaced while a filter is typed keeps the filter applied and keeps the
highlight on the same branch.

### Step 6 -- tests

- `worktree_test.go` (near the existing block at line 2063): POST with a bad
  token against an unreachable remote -> cached branches + the auth-specific
  warning; POST with no credentials -> unchanged behaviour; GET -> unchanged.
- New Go unit test for `parseRemoteHost`, cases mirrored from
  `static/modules/clone-cred-host.test.js`.
- Node test for the client credential lookup if it lands in a module.
- `make test`.

Also add: a refresh that times out still returns cached branches promptly, and
an e2e check that Create succeeds while a refresh is still in flight.

### Step 7 -- regenerate golden files

Templates under `cmd/swe-swe/templates/host/` are embedded at build time:

    make build golden-update
    git add -A cmd/swe-swe/testdata/golden
    git diff --cached -- cmd/swe-swe/testdata/golden

Expect only the intended server/JS changes in the diff.

## Estimate

About 90 minutes: 20 min server credentials, 15 min client credentials,
20 min the two constraint fixes (timeout + combo-box state), 25 min tests plus
golden update.

## Workaround until then

Run `git fetch --all` in any open session on that install, then reopen the New
Session dialog. The list is read from disk and will be current.
