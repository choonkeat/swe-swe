# Share a single live session via scoped-cookie link + password

Executable task plan for `/swe-swe:execute-step-by-step` (via
`/swe-swe:execute-in-worktree tasks/2026-07-08-share-live-session-scoped-cookie.md`).
Log convention: `tasks/2026-07-08-share-live-session-scoped-cookie.md-phase{N}.log`.

Origin: agent-chat design session 2026-07-08 (session "Share live session").
Decisions settled below -- read the whole Design before Phase 1; every step
references it.

## Ground rules for the executing agent

- ASCII only in all code/markdown (no em-dashes, no smart quotes, no non-ASCII).
- Run tests with `make test`, never bare `go test`.
- Canonical server source is `cmd/swe-swe/templates/host/swe-swe-server/`. After
  ANY change under `cmd/swe-swe/templates/`: `make build golden-update`, then
  `git add -A cmd/swe-swe/testdata/golden` and review
  `git diff --cached -- cmd/swe-swe/testdata/golden` before committing. The
  golden fixtures embed the server source, so these `.go` edits WILL show up
  there -- that is expected, verify the diff is only your changes.
- Never log the master password (`SWE_SWE_PASSWORD`) or any share password. Share
  passwords are shown to the owner in the UI response body ONCE; never `log.Printf`
  them.
- Line numbers below are approximate anchors; grep the named symbol to locate.
- If any verification fails and a workaround is tempting: STOP and ask via
  send_message. No silent compromises.

## Design (settled -- do not relitigate)

### Goal
Let a full user share ONE live session with an external person via a unique link
plus a password. The guest logs in with that password and is "boxed in": they
can reach only that one session (its page, WebSocket, and per-session proxies)
and nothing else on the server. Within the session the guest is a **full
participant** (read-write PTY, same as any user) -- NOT a spectator.

### Settled decisions (from the design chat)
1. **Full collaborate**, not read-only. => NO changes to the WebSocket read loop /
   PTY input gating. A scoped guest is a normal participant of their one session.
2. **No revocation / no persistence.** The share credential lives on the
   in-memory `Session` and dies when the session ends. No DB, no registry, no
   "revoke" button. Ending the session is the revocation.
3. **Embedded-auth mode first** (i.e. `SWE_SWE_PASSWORD` set; `setupEmbeddedAuth`
   active). Compose/Traefik mode is out of scope for this iteration; enforcement
   still lives at the handler level so it is not silently bypassed there, but the
   share-issue UX targets embedded mode.

### It is NOT JWT
The auth cookie today is `timestamp|HMAC-SHA256(timestamp, secret)` where the
HMAC key IS the login password (`SWE_SWE_PASSWORD`). Stateless, carries no
identity/role/scope (`auth.go:212-257`). This plan adds an optional **scope
segment** to that signed cookie. "Expand the JWT scope" becomes "add a scope
field to the signed cookie and enforce it at every UUID-resolving handler."

### Cookie format change (backward compatible)
- Unscoped (existing + full users): `timestamp|hmac(timestamp)` -- UNCHANGED, so
  every cookie already in the wild keeps working.
- Scoped (guest): `timestamp|scope|hmac(timestamp + "|" + scope)` where `scope`
  is the session UUID.
- The HMAC is ALWAYS keyed by the master `secret` (`SWE_SWE_PASSWORD`). The share
  password is ONLY a login credential; it is NEVER an HMAC key. => a guest cannot
  forge an unscoped cookie or tamper their scope: they don't know the master
  secret, so any change to `timestamp` or `scope` fails `hmac.Equal`.

### Security invariants (must hold)
- A scoped guest cannot reach any session UUID other than `scope`.
- A scoped guest cannot create/fork/list sessions.
- A scoped guest cannot open any recording (`/recording/*`) -- none, including a
  replay of their own session after it ends.
- A scoped guest cannot reach another session's per-port proxy
  (preview/agentchat/vnc) -- those listeners are session-owned, so the check must
  compare `scope` against the OWNING session UUID (see `requireAuthCookie`).
- Share passwords must be high-entropy (>= 128 bits, e.g. 32 hex chars or a
  base32 token) so the existing global rate-limit ceiling (200/5min,
  `authGlobalRateLimitMax`) makes brute force infeasible.
- Guessing the URL alone is not enough; the password is required. Guessing the
  password alone is not enough without the session UUID (it is scoped to one).

### Enforcement: centralize in the auth service (settled 2026-07-09)
The auth service is the single gate. We do NOT sprinkle scope checks into each
handler. In embedded mode `authMiddleware` wraps the ENTIRE default mux
(`setupEmbeddedAuth`, `auth.go:699`), so every route on the main path -- the
`/session/{uuid}` page, the `/ws/{uuid}` agent terminal, `/proxy/{uuid}/...`,
`/api/session/{uuid}/...`, `/recording/...`, and `/` -- already passes through
one function. Put the scope enforcement THERE and downstream handlers stay
untouched and "just work" unchanged. This is the whole architecture.

Central helper: `scopeAllows(scope, uuid string) bool { return scope == "" || scope == uuid }`.

**In `authMiddleware` (`auth.go:583`)**, after verifying the cookie and obtaining
`scope`:
- `scope == ""` (full user): behave exactly as today. Zero change to their
  experience.
- `scope != ""` (guest): allow scope-free plumbing paths (`/static/...`,
  `/favicon.ico`, `/swe-swe-auth/...`, `/__probe__`, and any other UUID-less
  asset the session page needs). For everything else:
  - `/` (homepage): render ONLY the `scope` session (or 302 to
    `/session/{scope}`). Guest sees just their one live session.
  - `/session/{uuid}`, `/ws/{uuid}`, `/proxy/{uuid}/...`,
    `/api/session/{uuid}/...`: require `scopeAllows(scope, uuid)` else 403/redirect.
  - `/recording/...` (ANY recording): DENY for a scoped cookie. No replays at
    all, including the guest's own session after it ends (settled: ending the
    session is the cutoff). Recordings use a different ID than the live session,
    so there is nothing to allow-match anyway.
  - Spawn/fork/list: `/api/session/new`, `/api/fork/*` -> 403 (guest cannot
    create or fork).

Extract the path->UUID parsing and the guest allow/deny decision into ONE
function (e.g. `scopedRequestAllowed(scope, r) bool`) that `authMiddleware` calls,
so the guest policy lives in a single place and is unit-testable. Default DENY
for any unrecognized UUID-bearing path.

**The four per-port side listeners** (preview/`PreviewPort`,
agent-chat/`AgentChatPort`, files/`FilesPort`, vnc/`VNCPort`) are separate
listeners that do NOT go through the default mux -- they are guarded by
`requireAuthCookie` (`auth.go:660`). Each listener belongs to exactly one session,
so this is ONE uniform change, not per-service logic: thread the owning session
UUID into `requireAuthCookie` at construction and add a single comparison --
allow a scoped cookie only if `scopeAllows(scope, owningUUID)`. Same change
applied at all four setup sites. (The agent terminal itself is `/ws/{uuid}` on
the main mux, so it is already covered by the central gate for free.)

`list_sessions` MCP (`main.go:8645`) is agent-side (MCP-key auth), not reachable
by a browser guest -- no change.

### Where the share password lives
Add `SharePassword string` to `Session` (`main.go:496`), read/written under
`sessionsMu`. It is set when the owner enables sharing and disappears when the
session leaves the `sessions` map. `authLoginPostHandler` (same package) reads
`sessions`/`sessionsMu` directly to validate a guest login.

### Login flow for a guest
1. Owner (full cookie) on the session page clicks **Share** ->
   `POST /api/session/{uuid}/share`.
2. Handler (owner-cookie-authed; scoped cookies get 403) generates a random
   `SharePassword` if none exists, stores it on the session, and returns JSON:
   `{ "url": "<origin>/swe-swe-auth/login?scope=<uuid>&redirect=<url-encoded /session/{uuid}?assistant=...>", "password": "<share password>" }`.
   The `redirect` is built from the session's assistant so the guest lands
   correctly after login.
3. Owner sends url + password to the guest out-of-band.
4. Guest opens the url -> login form. The form now carries a hidden `scope` field
   (from the query) in addition to the existing hidden `redirect`. Optional: show
   a "You're joining a shared session" note when `scope` is present.
5. Guest submits the share password. `authLoginPostHandler`:
   - If `scope` is empty: existing behavior -- constant-time compare vs master
     `secret`, issue UNSCOPED cookie.
   - If `scope` is non-empty: look up `sessions[scope]`; if missing or no
     `SharePassword`, treat as invalid (do NOT leak which). Constant-time compare
     submitted password vs `sess.SharePassword`; on match issue a cookie stamped
     `scope=<uuid>` via `authSignCookie(secret, scope)`.
   - Rate limiting (`authLoginLimiter` + `authGlobalLimiter`) applies unchanged to
     BOTH branches.
6. Guest is redirected (via `safeRedirect`) to their session page and joins the
   WebSocket as a full participant.

### API signature changes
- `authSignCookie(secret string) string` -> `authSignCookie(secret, scope string) string`.
- `authVerifyCookie(cookie, secret string) bool` ->
  `authVerifyCookie(cookie, secret string) (scope string, valid bool)`.
  Update all 3 callers: `authMiddleware` (`auth.go:627`), `requireAuthCookie`
  (`auth.go:670`), `authVerifyHandler` (`auth.go:517`). `authVerifyHandler` and
  the base `requireAuthCookie` path only need `valid`; `authMiddleware` and the
  scope-aware `requireAuthCookie` use `scope`.
- `requireAuthCookie(secret string, next http.Handler)` gains the owning session
  UUID: `requireAuthCookie(secret, owningUUID string, next http.Handler)` (or a
  variant), so it can compare scope. Locate its call sites (per-port proxy setup)
  and pass the session UUID.

## Phases

### Phase 1 -- Cookie scope primitive + unit tests (no behavior change yet)
1. Change `authSignCookie` / `authVerifyCookie` / `authComputeHMAC` usage per
   "Cookie format change" above. Add `scopeAllows`.
2. Update the 3 `authVerifyCookie` callers to the new 2-return signature,
   preserving today's behavior (ignore scope for now -- pure refactor).
3. Add `auth_test.go` cases: unscoped round-trip; scoped round-trip returns the
   scope; a 2-part legacy cookie still verifies with `scope == ""`; tampering the
   scope segment fails; tampering timestamp fails; expiry still enforced.
- Verify: `make test` green. `make build golden-update`; confirm golden diff is
  only the refactor. Commit.

### Phase 2 -- Share password on Session + issue endpoint
1. Add `SharePassword string` to `Session`. Add a helper to generate a
   >=128-bit token (crypto/rand, hex/base32).
2. Add `POST /api/session/{uuid}/share`: owner-cookie required (scoped cookie ->
   403). Generates+stores `SharePassword` if absent (idempotent: return existing),
   returns `{url, password}` JSON. NEVER logs the password.
3. Extend `authLoginPostHandler` to handle the `scope` branch (validate vs
   `sess.SharePassword`, issue scoped cookie). Add the hidden `scope` field to
   `authRenderLoginForm` and thread it through GET (`authLoginHandler`) + POST.
- Verify: `make test` green; unit test the login POST scope branch (valid share
  pw -> scoped cookie; wrong pw -> 401; unknown scope -> 401, no leak).
  `make build golden-update`, review, commit.

### Phase 3 -- Centralized scope enforcement in the auth service (security-critical)
1. Add `scopeAllows` and `scopedRequestAllowed(scope, r)` (the single guest
   policy: homepage-only, own-session paths, deny recordings, deny spawn/fork/
   other-session). Wire it into `authMiddleware` (`auth.go:583`) on the
   `scope != ""` branch. Full users (`scope == ""`) are unaffected.
2. Make `requireAuthCookie` scope-aware: add an owning-UUID parameter and the
   `scopeAllows(scope, owningUUID)` check; update all four per-port setup sites
   (preview/agent-chat/files/vnc) to pass their session UUID.
- Verify: `make test` green, incl. unit tests for `scopedRequestAllowed`
  covering: homepage shows only own session, own `/session` + `/ws` + `/proxy` +
  `/api/session` allowed, another UUID denied, `/recording/*` denied,
  `/api/session/new` + `/api/fork/*` denied, static/asset/auth paths allowed. Plus
  a `requireAuthCookie` test: scoped cookie for the owning session allowed, for a
  different session denied. `make build golden-update`, review, commit.

### Phase 4 -- Owner Share UI
1. Add a **Share** button to the session page (terminal-ui). On click, `POST`
   the share endpoint, then show the returned url + password with a copy button.
   Put the comment BEFORE the element in any `.elm`/template edits.
2. Verify end-to-end with the test-container workflow (docs/dev/
   test-container-workflow.md) + browser MCP:
   - Owner opens a session, clicks Share, gets url+password.
   - In a fresh browser context (no cookie), open the url, enter the password ->
     lands on the session, can type into the agent (full participant).
   - From that guest context, try to open another session's `/session/{X}`,
     `/ws/{X}`, `/proxy/{X}/preview/`, any `/recording/{X}`, and
     `/api/session/new` -> all denied (redirect-to-own-session or 403), while the
     OWN session works. `/` shows only their session. Confirm the four side ports
     (preview/chat/files/vnc) of another session are unreachable from the guest.
   - Wrong password on the share url -> rejected.
   - End the session -> the scoped cookie is now useless (session gone).
- `make build golden-update`, review, commit. Tear down the test container.

## Open items to confirm with the user before/at Phase 4
- Share button placement/label and whether to show the raw password inline vs a
  one-click "copy link that embeds nothing" model (current plan: password shown
  once in the response, owner relays it).
- Whether a full user reaching `/session/{X}` while ALSO holding a scoped cookie
  is possible in practice (it is not -- a cookie is either scoped or not; a user
  logs in once). No action expected; noted for completeness.
