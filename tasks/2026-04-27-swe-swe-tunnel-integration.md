# swe-swe ↔ swe-swe-tunnel integration

## Context

`swe-swe-tunnel` is a separately-built reverse-tunnel pair (server + client) that lets a swe-swe host be reachable from the public internet without any inbound DNS, TLS, or port exposure. From swe-swe's point of view it is a black-box upstream:

> A swe-swe instance running behind the tunnel is reachable at `{port}.{unique}-tunnel.example.com:443`. The tunnel terminates browser TLS, demuxes by SNI, and forwards each stream to `127.0.0.1:{port}` on the swe-swe host. Browser → tunnel-server → tunnel-client (localhost on swe-swe host) → swe-swe-server (localhost on swe-swe host) → swe-swe-server.

This task is the **swe-swe-side** work needed so the frontend builds correct cross-port URLs when behind the tunnel. The tunnel client/server code is out of scope — assume it Just Works and exposes the contract above.

Companion research notes: `/workspace/research/2026-04-26-swe-swe-tunnel-integration.md`.

## Concurrent work to be aware of

At the time this task was written:

- Uncommitted edits exist in `cmd/swe-swe/templates/host/swe-swe-server/{main.go,cred_store.go}` for the per-session credential broker.
- Recent commits on `main` (`8a4fa87fb`, `bf8240fbe`, `49afc56d6`, `ff110181c`, `941c5864c`) are credential-broker work.

Do **not** start a feature branch — commit directly on `main` per user instruction. But coordinate with whoever holds those uncommitted edits before touching `swe-swe-server/main.go` flag block (lines ~1664–1673) or `auth.go` (later phases).

---

## v1 slice — env-only, frontend label-swap branching

The simplest, smallest, no-op-by-default change. Goal: ship the URL-builder branching with a flag; verify in e2e that **nothing breaks when the flag is unset** (the only state today). Then verify it produces correct subdomain URLs when set.

### The decision the code is making

Given a target port and a public hostname, build a URL:

```
publicHostname == ""   →  port mode:      ${proto}//${location.hostname}:${proxyPortOffset + targetPort}${path}
publicHostname != ""   →  subdomain mode: ${proto}//${targetPort}.${publicHostname}${path}
```

The `proxyPortOffset` (default 20000) **only matters in port mode** — it exists because Traefik listens on a different host port per session. In subdomain mode there is no Traefik in front of swe-swe; the tunnel demuxes by leftmost label and forwards directly to `127.0.0.1:{targetPort}`. Subdomain mode uses the **raw target port** as the leftmost label.

### Backend changes

**File**: `cmd/swe-swe/templates/host/swe-swe-server/main.go`

1. Add CLI flag + env at the flag block (around line 1664–1673):
   ```go
   publicHostname := flag.String("public-hostname", "",
       "Public hostname swe-swe is reachable at when behind a reverse tunnel "+
       "(e.g. abc-tunnel.example.com). Env: SWE_PUBLIC_HOSTNAME. "+
       "When set, frontend builds cross-port URLs as {port}.{public-hostname} instead of using proxy-port offsets.")
   ```
   Resolve precedence: CLI flag → `SWE_PUBLIC_HOSTNAME` env → empty (unchanged behavior). Match the pattern used by `tailscale.go:resolveListenAddr` — pull resolution into a tiny helper `resolvePublicHostname(flag, env string) string` for testability.

2. Add the resolved value to `BroadcastStatus()` at lines 806–842:
   ```go
   "publicHostname": s.PublicHostname,  // empty string when unset
   ```
   Wire it through `Session` so `BroadcastStatus` can read it. No other Go-side changes.

**No new file required** for v1. State-file fallback (`/workspace/.swe-swe/tunnel-state.json`) is **deferred** to a later commit (see "Next steps" below).

### Frontend changes

**File**: `cmd/swe-swe/templates/host/swe-swe-server/static/modules/url-builder.js`

Add three subdomain-mode helpers next to the existing port-based ones (lines 91–124):

```js
export function buildSubdomainPreviewUrl(location, targetPort, publicHostname) {
  return `${location.protocol}//${targetPort}.${publicHostname}`;
}

export function buildSubdomainAgentChatUrl(location, targetPort, publicHostname) {
  return `${location.protocol}//${targetPort}.${publicHostname}`;
}

export function buildSubdomainProxyUrl(location, targetPort, publicHostname, targetURL) {
  // Mirror buildPortBasedProxyUrl semantics but with the subdomain base.
  // ...
}
```

**File**: `cmd/swe-swe/templates/host/swe-swe-server/static/terminal-ui.js`

1. Receive `publicHostname` in the WebSocket `onmessage` handler at lines 1355–1366:
   ```js
   this.publicHostname = msg.publicHostname || "";
   ```

2. Branch in `getPreviewBaseUrl()` at lines 4202–4207:
   ```js
   getPreviewBaseUrl() {
     if (this.publicHostname) {
       return buildSubdomainPreviewUrl(window.location, this.previewPort, this.publicHostname);
     }
     // Existing port-based path:
     return this._proxyMode
       ? buildPortBasedPreviewUrl(window.location, this.previewProxyPort)
       : buildPreviewUrl(window.location, this.uuid);
   }
   ```

   Same branching for the agent-chat URL builder.

### What does NOT need branching (intentionally — keep the diff small)

Verify these claims hold and leave them alone:

1. **Main terminal WS** (`terminal-ui.js:1066–1067`) — uses `window.location.host`. Same-origin to whatever subdomain the browser loaded; works in tunnel mode unchanged.
2. **Agent-chat WS** (`agent-chat-dist/app.js:1828–1831`) — same: `location.host`. No branch.
3. **Debug WS** (`terminal-ui.js:4763–4770`) — derives from `previewBase` via `replace(/^http/, 'ws')`. Once `getPreviewBaseUrl()` branches, this follows for free. No branch at the call site.

### Tests for the v1 slice

Test bar (per user mandate): unit + e2e for every feature, no live-only validation.

**Go unit tests** (new, alongside `swe-swe-server/auth_test.go` style):
- `resolvePublicHostname` table: `(flag="", env="")` → `""`; `(flag="foo", env="")` → `"foo"`; `(flag="", env="bar")` → `"bar"`; `(flag="foo", env="bar")` → `"foo"` (flag precedence).
- `BroadcastStatus` JSON shape: marshal a `Session{PublicHostname: "x.example.com"}`, assert the `publicHostname` field appears with the expected value. And with `PublicHostname: ""`, assert it appears as empty string (or omitted — either is acceptable, but be explicit).

**JS unit tests** (extend whatever exists for `url-builder.js`; if there are no JS unit tests yet, add a minimal Node-runnable test file using the project's existing test pattern — check `cmd/swe-swe/templates/host/swe-swe-server/static/modules/` for any `*_test.js` first):
- `buildSubdomainPreviewUrl({hostname:"1977.foo.com",protocol:"https:"}, 3000, "foo.com")` → `https://3000.foo.com`
- Edge: `protocol:"http:"` → `http://...`
- Edge: hostname is `localhost` and `publicHostname=""` → falls through to existing port-based builder (covered by checking `getPreviewBaseUrl()` chooses the right branch)

**Playwright e2e** (`/workspace/e2e/`):
Two passes against the same fixture:
1. **Default (publicHostname unset)**: existing behavior — iframe `src` matches `:{proxyPort}` shape. **Goal: prove no regression.**
2. **`SWE_PUBLIC_HOSTNAME=fake-tunnel.example.com`**: iframe `src` matches `https://{port}.fake-tunnel.example.com` shape. The page won't actually load (no real DNS), but the `src` attribute is what we assert on. No real tunnel needed.

Run the existing e2e suite (`make e2e-up-simple && make e2e-test-simple && make e2e-down-simple`, plus the compose variant if it's in scope) under both env settings. The default-unset run is the regression gate — every existing test must still pass.

### Acceptance criteria for v1

- [ ] All existing tests pass with `SWE_PUBLIC_HOSTNAME` unset.
- [ ] New unit tests for `resolvePublicHostname` and `buildSubdomain*` pass.
- [ ] e2e in subdomain mode produces correct iframe `src` attribute shape.
- [ ] No new files created in `cmd/swe-swe/templates/host/swe-swe-server/` other than test files (state-file fallback is deferred).
- [ ] Commit message references this task file path.

---

## Next steps (separate commits, in this order)

These build on v1 and are each independently mergeable. Each gets its own task file when picked up.

### 2 — Cookie domain  (small, important)

When `SWE_PUBLIC_HOSTNAME` is set, browser requests to `1977.{publicHostname}` and `3000.{publicHostname}` need to share auth cookies.

- `cmd/swe-swe/templates/host/swe-swe-server/auth.go` lines 336–344 (`http.SetCookie` in `authLoginPostHandler`):
  Set `Cookie.Domain = "." + publicHostname` when non-empty. Leave unset (host-only) otherwise.
- Verify `resolveCookieSecure` (`auth.go:279–294`) doesn't gate on source IP — audit confirmed it does not. Add a test that asserts this explicitly so a future change can't regress it.
- **Tests**: unit (table for cookie-domain resolution); e2e with Go `cookiejar.New(nil)` — cookie set on `https://1977.fake-tunnel.example.com` is sent on a request to `https://3000.fake-tunnel.example.com` and **not** on `https://other.com`.

### 3 — `--bind` / `SWE_BIND` flag  (recommended but optional)

In tunnel mode the tunnel client (also localhost) is the only thing that should reach swe-swe. Extending `tailscale.go:resolveListenAddr` to support `--bind` (default `0.0.0.0:9898`, recommended `127.0.0.1:9898` in tunnel mode) removes network attack surface.

- **Tests**: unit precedence (flag → env → default).

### 4 — State-file fallback for `publicHostname`

The tunnel client (out of scope for this repo) is responsible for writing a state file at a known path so swe-swe can discover the hostname without the user setting an env var manually. From swe-swe's side, this is purely a **read**: parse a JSON file if it exists.

Contract (the tunnel writes this, swe-swe reads it):

```
/workspace/.swe-swe/tunnel-state.json
{
  "hostname":      "abc-tunnel.example.com",
  "unique":        "abc",
  "registered_at": "2026-04-26T10:00:00Z"
}
```

Resolution order becomes: CLI flag → env → state file → empty.

- Add `readTunnelState(path string) (hostname string, err error)` in a new `cmd/swe-swe/templates/host/swe-swe-server/tunnel_state.go`. Treat missing file as "no hostname configured" (not an error). Treat malformed JSON as a logged warning + empty string (don't crash startup).
- Call it from `resolvePublicHostname` when flag and env are both empty.
- **Tests**: unit only — table for missing file / valid file / malformed JSON / empty hostname field. No e2e needed for this slice (covered by v1 e2e once the env path works).

### 5 — Skip own LE/Traefik when tunnel mode active

A tunneled swe-swe host has no inbound exposure — no port 80, no port 443, no LE, no Traefik. The compose template needs a `{{IF TUNNEL}}` branch in `templates.go:264–383` that:

- Drops the `traefik:` service entirely.
- Binds swe-swe-server to `127.0.0.1:1977` (via `--bind` from step 3).
- Skips the LE block and per-port entrypoints.
- Preserves `{{PREVIEW_PORTS}}` etc. — the tunnel client still needs target ports.

Highest-blast-radius change in this whole task. Land everything else first.

- **Tests**: unit (golden compose templates per branch); e2e (render in tunnel mode → assert no `traefik:` block, swe-swe binds `127.0.0.1`, LE absent).

### 6 — Docs page

`www/swe-swe-tunnel.md`, parallel to `www/swe-swe-tailscale.md`. Sections: what it does, when to use it, setup, comparison to Tailscale Funnel / Cloudflare named tunnels / ngrok, cost ($0 self-hosted + 1 VPS), caveats (LE rate limits, terminate-at-server trust model). No tests.

---

## Audit reference (touchpoints — verified at task-write time)

| Concern | Already exists? | File:Line |
|---|---|---|
| `--public-hostname` / `SWE_PUBLIC_HOSTNAME` | **No** | new — `swe-swe-server/main.go:1664–1673` |
| `--bind` / `SWE_BIND` | partial — only `--addr` | `swe-swe-server/main.go:1664`, resolver `tailscale.go:57–74` |
| `Cookie.Domain` set anywhere | **No** | `auth.go:336–344` (omits Domain) |
| `X-Forwarded-Proto` trust | **Yes**, no IP gate | `auth.go:279–294` (`resolveCookieSecure`) |
| URL-builder helpers | port-based only | `static/modules/url-builder.js:91–124` |
| Preview iframe URL selector | `:port` mode | `terminal-ui.js:4202–4207` (`getPreviewBaseUrl`) |
| WS URL builders (main / agent / debug) | direct `window.location.host` | `terminal-ui.js:1066–1067, 4763–4770`; `agent-chat-dist/app.js:1828–1831` |
| Bootstrap config delivery | WebSocket status broadcast | `swe-swe-server/main.go:806–842` (`BroadcastStatus`) |
| Traefik / LE wiring | per-port routers + entrypoints | `templates.go:264–383`; `templates/host/docker-compose.yml`; `traefik-dynamic.yml` |

## Test bar reminder

Per the user's standing mandate (carried over from the swe-swe-tunnel build): **every feature ships with extensive unit AND e2e tests**. Live smoke tests (curl, browser by hand) are a starting point but not a substitute. This task is no exception — the v1 slice has both unit (Go + JS) and e2e (Playwright) coverage.

When in doubt about whether a scenario warrants a test: if you ran it manually to convince yourself it works, write the same scenario as an automated test before claiming the slice done.
