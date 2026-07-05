# Fix #6 -- Host-header open redirect in the ForwardAuth verify handler

## Status

**Draft for discussion.** Not started. From CTF.md finding #6. MEDIUM risk on
rollout -- this is the compose/Traefik login path and must be verified against a
real Traefik flow before shipping.

## Problem

`authVerifyHandler` (auth.go:514) builds the login redirect from
request-supplied forwarded headers and emits it as `Location`:

```go
// auth.go (condensed, ~514-540)
scheme := r.Header.Get("X-Forwarded-Proto"); if scheme == "" { scheme = "http" }
host   := r.Header.Get("X-Forwarded-Host");  if host == "" { host = r.Host }   // auth.go:527
loginURL := scheme + "://" + host + "/swe-swe-auth/login?redirect=" + url.QueryEscape(redirectURI)
w.Header().Set("Location", loginURL)
```

A request to `/swe-swe-auth/verify` with `X-Forwarded-Host: evil.example`
yields a 302 to `https://evil.example/swe-swe-auth/login?...`. The verify
endpoint is auth-exempt (`authMiddleware`), so this is reachable and gives an
open redirect / login-form-spoofing primitive on the trusted flow.

This handler is used by **Traefik ForwardAuth** in compose mode. Dockerfile-only
/ tunnel deployments do not route through it, so they are unaffected either way.

## Proposed fix (two options)

### Option A (preferred): relative Location

Emit a path-only `Location`:

```go
w.Header().Set("Location", "/swe-swe-auth/login?redirect="+url.QueryEscape(redirectURI))
```

The browser resolves a relative `Location` against its own current origin, so no
host is needed and the header can no longer be poisoned. ForwardAuth returns the
non-2xx response (with our `Location`) to the browser, which then follows it --
so a relative target should resolve to the correct public origin without us ever
trusting `X-Forwarded-Host`.

Also run `redirectURI` (from `X-Forwarded-Uri`) through `safeRedirect` (added in
the #1-3 commit) so the nested `redirect=` param can't carry an off-site value
either.

### Option B: validate host against an allow-list

Keep building an absolute URL but only accept `X-Forwarded-Host` when it matches
the configured public hostname (or its subdomains). More code, and requires the
canonical host to be known to this handler; only worth it if Option A turns out
to misbehave behind Traefik (e.g. a setup that needs an absolute redirect).

## Operational impact on deployed instances

**MEDIUM -- verify before rollout.** This is load-bearing for every compose-mode
deployment's login redirect. Option A *should* be behaviorally identical for
them (browser resolves relative against the same public origin it was already
on), but "should" needs confirming against the actual Traefik ForwardAuth flow,
because a mistake here locks legitimate users out of login. No impact on
tunnel/dockerfile-only mode.

## Open questions / what I need

1. **Test target:** is there a compose/Traefik instance I can exercise the login
   redirect against? Without one I will keep this analysis-only and not ship.
2. Does any ForwardAuth config rely on an *absolute* redirect (some setups do)?
   If so, Option B with an allow-list is the fallback.
3. Confirm `safeRedirect` is the right normalizer for `X-Forwarded-Uri` (it is a
   path+query, which is exactly what safeRedirect accepts).

## Test plan (TDD)

- Handler test: `GET /swe-swe-auth/verify` with no cookie and
  `X-Forwarded-Host: evil.example` -> assert `Location` does NOT contain
  `evil.example` (Option A: starts with `/swe-swe-auth/login`).
- With a valid cookie -> 200, no redirect.
- `X-Forwarded-Uri` carrying an off-site `redirect` value -> normalized to `/`.
- Manual: real Traefik compose login round-trip (the gating item for rollout).
