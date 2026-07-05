# Fix #7 -- Session cookie: timestamp-only HMAC, non-revocable, Secure default

## Status

**Draft for discussion.** Not started. From CTF.md finding #7. This is NOT one
switch -- it is three sub-changes with very different blast radii. Treat them
separately.

## Problem

`authSignCookie` (auth.go:214) produces `timestamp|HMAC(timestamp, secret)`:

- The cookie binds to nothing but the issue time: no user id, no random session
  id, no key version. There is no revocation/logout -- a captured cookie is
  valid for the full 7 days (`authCookieMaxAge`).
- The HMAC key IS the password (`secret`), so the cookie's security collapses to
  password entropy, and two deployments sharing a password issue interchangeable
  cookies.
- `Secure` is decided from `X-Forwarded-Proto` / `SWE_COOKIE_SECURE` and
  defaults to **false** (`resolveCookieSecure`, auth.go:414), so on a plain-HTTP
  hop the long-lived cookie travels in cleartext.

## Proposed changes (ranked by safety)

### 7a. Separate signing key from the password -- MEDIUM impact

Generate a dedicated cookie-signing secret instead of using the password as the
HMAC key.

- If the key is **ephemeral** (regenerated each process start), every server
  restart / redeploy invalidates all outstanding cookies -> **all users must
  re-login after every restart.** Annoying; for some operators unacceptable.
- Fix: **persist** the key (e.g. a file under the server's state dir, mode 0600,
  created on first boot). Then restarts preserve sessions. This is the
  recommended form.
- Add a key *version* prefix to the cookie so we can rotate without a hard
  cutover.

### 7b. Logout + key rotation endpoint -- LOW impact

- Add `POST /swe-swe-auth/logout` that clears the cookie.
- Rotation = bump the persisted key version; old cookies fail verification.
  Additive, no impact on anyone who does not call it.

### 7c. Default `Secure` to true -- DO NOT DO BLINDLY

The current `false` default is **intentional**. `--tunnel-local-ports`, LAN, and
Tailscale access are frequently plain HTTP, and a `Secure` cookie is simply
never sent over `http://`, which would silently break login on those paths. The
existing `resolveCookieSecure` comment documents this. Leave it gated on
`X-Forwarded-Proto` / `SWE_COOKIE_SECURE` as-is. (If we ever want a stricter
default, it must be opt-in per deployment, not a blanket flip.)

## Recommended scope

Ship **7a (persisted key) + 7b (logout/rotate)**. Leave 7c alone. That removes
the "password is the HMAC key" weakness and gives a revocation story, without
breaking plain-HTTP access or logging everyone out on restart.

## Operational impact on deployed instances

- 7a persisted: **LOW** once persistence exists -- first upgrade re-issues
  cookies on next login (existing cookies, signed with the old password-derived
  key, fail verification once -> one forced re-login at upgrade). Worth calling
  out in the changelog.
- 7a ephemeral (rejected): re-login on *every* restart. Do not ship this form.
- 7b: **NONE** until used.
- 7c: **HIGH risk of silent login breakage** on non-TLS access -> excluded.

## Open questions

1. Where does the server already persist per-install state? (Reuse that dir for
   the signing key rather than inventing a path.) Candidates: the tunnel/ts
   state dir, `.swe-swe/`. Need to confirm a writable, non-volatile location
   that survives container restarts in Fly/Railway/compose.
2. Is a forced one-time re-login at upgrade acceptable, or do we need a
   transition window that accepts both old (password-keyed) and new
   (key-keyed) cookies for one release? Recommend the clean cutover + changelog
   note; the transition window is extra complexity for a 7-day-max token.
3. Do we want per-session ids (true revocation of a single session) or is
   global key rotation (revoke everything) enough? Global rotation is far
   simpler and probably sufficient for a single-password tool.

## Test plan (TDD)

- Key load/persist: first boot generates + writes key (0600); second boot reads
  the same key; cookies signed before/after a simulated restart still verify.
- Verification uses the dedicated key, not the password (cookie signed with key
  K verifies regardless of the password value).
- Versioned cookie: a cookie with an old version prefix fails after rotation.
- Logout: clears the cookie (Set-Cookie maxage<=0) and a subsequent request is
  unauthenticated.
- Guard test (mirrors existing `TestResolveCookieSecure*`): `Secure` still
  follows `X-Forwarded-Proto`, default false -- unchanged.
