# Preview tab: non-localhost hostnames (vhost apps, remote-browser correct)

Executable task plan for `/swe-swe:execute-step-by-step` (via
`/swe-swe:execute-in-worktree tasks/2026-07-04-preview-hostname-vhost.md`).
Log convention: `tasks/2026-07-04-preview-hostname-vhost.md-phase{N}.log`.

Origin: recording `14605713-38f6-48b8-9621-f131051276a6` ("Preview hostname",
2026-06-13); decisions pinned 2026-07-04; reworked 2026-07-05 (remote-browser
correction + no-wildcard-assumption correction); amended 2026-07-12 (tunnel
degraded-mode note + Follow-up A tunneld grammar, docker-not-required design
note, 5.3 docker-free guide, 6.4 services.yml registration source,
Follow-up B socket alternatives). See "Design" below -- read it fully before
Phase 1; every step references it.

## Ground rules for the executing agent

- ASCII only in all code/markdown (no em-dashes, no smart quotes).
- Run tests with `make test`, never bare `go test` (except inside
  `/repos/agent-reverse-proxy/workspace`, which is a normal Go repo).
- After ANY change under `cmd/swe-swe/templates/`: `make build golden-update`,
  then `git add -A cmd/swe-swe/testdata/golden` and review
  `git diff --cached -- cmd/swe-swe/testdata/golden` before committing.
- `check-gomod-sync` (part of `make test`) requires the root `/workspace
  go.mod` and the template `go.mod.txt` to pin IDENTICAL versions of shared
  deps -- bump both together.
- The library lives OUTSIDE this worktree at
  `/repos/agent-reverse-proxy/workspace` (origin
  git@github.com:choonkeat/agent-reverse-proxy.git, latest tag v0.2.9,
  HEAD already ahead of the tag with unreleased fixes). Before touching it:
  `git -C /repos/agent-reverse-proxy/workspace status` must be clean and on
  main; do Phase 1 on a branch there; leave it merged back to main, pushed,
  tagged. If a push aborts asking you to "Run 'git push' again" (a local
  pre-push hook), just push again -- do not force-push.
- If any verification fails and a workaround is tempting: STOP and ask via
  send_message. No silent compromises.

## Design (settled -- do not relitigate)

### Problem
Users' compose stacks serve vhosts like `app1.lvh.me:3000` + `app1.lvh.me:5000`
on the swe-swe machine. The user's browser is generally on a DIFFERENT
machine, where `*.lvh.me` resolves to the USER's loopback -- typing
`app1.lvh.me:3000` there can never reach swe-swe. (Agent View solved its copy
of this in d5266dfb4 by remapping chromium's resolver -- possible only because
that chromium runs next to swe-swe. The Preview iframe runs in the user's
browser; no resolver tricks available.)

Current blockers (verified on main 2026-07-04):
1. Frontend: `static/terminal-ui.js` `setPreviewURL()` (~6406):
   `isExternal = host !== 'localhost' && host !== '127.0.0.1'` -> new-tab
   bounce for everything else.
2. Proxy: `agent-reverse-proxy` `proxy.go` "Set Host header to target host":
   `outReq.Host = target.Host` -- upstream never sees a vhost; and the proxy
   has a single fixed target, no per-request selection.
3. Proxy: `processProxyResponse` unconditionally strips `Set-Cookie`
   `Domain=` (`cookie.Domain = ""`).
Cosmetic: `updateUrlBarPrefix()` (~6601) hardcodes `localhost:{previewPort}`.

### Two-hostname model
Browser-facing URL and upstream (logical) Host are different names:

```
browser: http://app1-5000.<reach>:23000/...
   -> per-session listener :23000 demuxes leftmost label "app1-5000"
   -> proxies to 127.0.0.1:5000 with Host: app1.lvh.me:5000
   -> user's compose traefik matches Host(`app1.lvh.me`) as on a laptop
```

- `<reach>` = a domain whose `*.` wildcard resolves to the swe-swe machine
  FROM THE USER'S BROWSER. Candidates: `lvh.me` (only when browsing from the
  same machine), `<ip>.sslip.io`/`nip.io` (bare-IP deployments, resolves from
  anywhere), admin-owned wildcard DNS, explicit `SWE_PREVIEW_REACH_DOMAIN`.
- Upstream Host is REWRITTEN to the logical vhost (`app1.lvh.me:5000`), never
  passed through (no traefik rule matches `app1-5000.<ip>.sslip.io:23000`).
  Logical suffix default `lvh.me`, env `SWE_PREVIEW_VHOST_SUFFIX`.
- `Set-Cookie Domain=` is REWRITTEN logical->reach (`.lvh.me` -> `.<reach>`),
  not stripped (breaks shared auth), not preserved (browser rejects `.lvh.me`
  set on an `.sslip.io` page). No-Domain cookies keep today's strip behavior.

### Label grammar (listener resolution precedence)
1. `{name}-{port}`, port 1024-65535 -> target `127.0.0.1:{port}`, upstream
   Host `{name}.{suffix}:{port}`. Split on the LAST dash-number segment:
   `my-app-5000` -> name `my-app`, port `5000`.
2. bare `{port}` (numeric) -> target `127.0.0.1:{port}`, upstream Host
   `localhost:{port}` (tunnel-style).
3. bare `{name}` -> registered route if present (Phase 6), else target
   primary PreviewPort, upstream Host `{name}.{suffix}:{PreviewPort}`.
4. no label / unrecognized / label equals the reach's own first label ->
   current behavior: primary PreviewPort, Host clobbered to
   `localhost:{port}`. NOTHING EXISTING BREAKS.
Labels validate as `[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?`; targets loopback
only. Unknown labels must NOT auto-allocate ports.

### Reach discovery + pinned (degraded) mode -- wildcard NOT assumed
A swe-swe hostname does not always support wildcard subdomains (corporate
DNS, /etc/hosts, air-gapped LAN). Mirror ADR-033: browser probes, degrades
visibly.
- Server sends ordered candidates in the WS status payload
  (`previewReachCandidates`): `[SWE_PREVIEW_REACH_DOMAIN]` if set, else
  derived from how the page was reached (loopback -> `lvh.me`; IP ->
  `<ip>.sslip.io`; always append `window.location.hostname` last -- it might
  have wildcard DNS, only a probe can tell). Server also sends
  `previewVhostSuffix`.
- Browser probe (after existing path/port probes): fetch
  `http(s)://probe-<rand>.<candidate>:<proxyPort>/` expecting the
  `X-Agent-Reverse-Proxy` response header. First success -> wildcard mode
  with that reach. All fail -> pinned mode.
- Pinned mode: single origin `<hostname>:<proxyPort>`. URL bar still accepts
  `app1.lvh.me:5000`; frontend POSTs
  `/__agent-reverse-proxy-debug__/vhost-pin` `{"name":"app1","port":5000}`
  to the session listener; label-less requests then route to the pinned
  target with rewritten Host. Trade-offs (accepted): one vhost at a time per
  session, shared cookie jar, Domain stripped. This is the pre-ADR-025
  target-switch API resurrected PER-SESSION (the old sin was one global
  proxy, not mutability).
- Active mode + reach shown in the UI next to the URL bar. Degradation must
  be visible, never silent.

### Tunnel mode behavior (degrades by design; wildcard is Follow-up A)
tunneld already serves wildcard subdomains under `{unique}-tunnel.<suffix>`
and demuxes the LEFTMOST label as a RAW PORT NUMBER
(`{port}.{publicHostname}`, proxyPortOffset does not apply -- see
docs/tunnel-explained.md and url-builder.js buildSubdomainPreviewUrl).
Named labels (`app1-5000`) and probe labels (`probe-<rand>`) are not
numeric, so over the tunnel hostname the reach probe fails and the session
lands in PINNED mode. That is correct, designed behavior for THIS task --
do not special-case tunnel here. Wildcard-over-tunnel is Follow-up A.

### Docker not required (multi-service != compose)
The demux targets `127.0.0.1:{port}` and does not care how services are
run: docker compose, Procfile/foreman, process-compose, or
agent-backgrounded processes are all equivalent. The vhost Host rewrite
only matters when the user's stack contains its own Host-based router
(traefik/nginx inside their compose). The documented docker-free path is:
run services with process-compose (or plain processes), reach them via
bare `{port}` labels or Phase 6 named routes. swe-swe does NOT supervise
user processes (see Follow-up B).

### Out of scope (do not implement)
Tunnel-mode named labels (Follow-up A below, swe-swe-tunnel repo), a
process supervisor / "mini compose" runtime (Follow-up B keeps this
declarative-registration-only), TLS wildcard certs, path-based-fallback
vhosts, port-less :80/:443 ingress, nip.io/sslip.io in Agent View's
loopback remap list (they must keep resolving to real IPs).

---

## Phase 1 -- agent-reverse-proxy: per-request hooks (cross-repo)

Work in `/repos/agent-reverse-proxy/workspace`, branch
`resolve-target-hooks` off clean main. Inspect the existing option/config
surface first (how `noInject`, `pathPrefix`, `scriptTag` reach the handler)
and follow that pattern; names below are contracts, adapt placement to the
codebase's idiom.

- [x] 1.1 RED: add `proxy_hooks_test.go` with failing tests:
  - `TestResolveTargetPerRequest`: proxy configured with
    `ResolveTarget func(inboundHost string) (target *url.URL, upstreamHost string, ok bool)`;
    request with `Host: app1-5000.x.sslip.io:23000` reaches a second backend
    (httptest server) and that backend observes `Host: app1.lvh.me:5000`.
  - `TestResolveTargetFallback`: hook returns ok=false -> request goes to
    the fixed target with today's clobbered Host (back-compat).
  - `TestNoHookUnchanged`: nil hook -> byte-identical behavior to v0.2.9
    (fixed target, Host clobbered).
  - `TestCookieDomainRewrite`: with
    `CookieDomainRewrite func(domain string) string`, upstream
    `Set-Cookie: sid=1; Domain=.lvh.me` arrives at the browser as
    `Domain=.x.sslip.io`; empty-Domain cookies untouched except today's
    behavior; nil hook -> today's strip.
  Log RED (expected failures observed) before proceeding.
- [x] 1.2 GREEN: implement both hooks. `ResolveTarget` consulted where the
  outbound request is built (the `outReq.Host = target.Host` site);
  `CookieDomainRewrite` in `processProxyResponse`'s Set-Cookie loop
  (replace the unconditional `cookie.Domain = ""` with: rewrite if hook
  returns non-empty, else strip). Secure-flag and pathPrefix cookie handling
  unchanged. `go test ./...` green.
- [x] 1.3 Release: merge branch to main, `git tag v0.2.10`, push main +
  tags. Verify `go list -m github.com/choonkeat/agent-reverse-proxy@v0.2.10`
  resolves (from any module dir). Log expected/got.

## Phase 2 -- swe-swe-server: label grammar + resolver wiring

All in `cmd/swe-swe/templates/host/swe-swe-server/` unless stated.

- [ ] 2.1 Bump dependency BOTH places: root `/workspace/go.mod` (if it lists
  agent-reverse-proxy) and template `go.mod.txt` + `go.sum.txt` to v0.2.10.
  `make test` must pass `check-gomod-sync`. Commit.
- [ ] 2.2 RED: new `preview_vhost_test.go`, table-driven:
  - `TestParsePreviewLabel`: `app1-5000` -> (app1, 5000); `my-app-5000` ->
    (my-app, 5000); `3001` -> ("", 3001); `app1` -> (app1, 0); `probe-x` ->
    (probe-x, 0); invalid (`-foo`, `foo-`, uppercase, 70 chars, port 80,
    port 99999) -> reject.
  - `TestResolvePreviewVhost`: grammar precedence rules 1-4 from Design,
    including pin fallback (rule 4 + pin set -> pinned target and Host) and
    "label equals reach first label" guard.
  Log RED.
- [ ] 2.3 GREEN: new `preview_vhost.go`: `parsePreviewLabel`,
  `resolvePreviewVhost(label string, s *Session) (port int, upstreamHost string, ok bool)`,
  suffix from `SWE_PREVIEW_VHOST_SUFFIX` default `lvh.me`. Wire into the
  per-session listener construction (where the preview proxy handler is
  built with target `127.0.0.1:{PreviewPort}` -- find via
  `previewProxyPort`/proxy_listener code) as the v0.2.10 `ResolveTarget`
  hook: extract leftmost label of the inbound Host, resolve, return
  `127.0.0.1:{port}` + upstream Host. `CookieDomainRewrite`: map
  `.{suffix}`/`{suffix}` -> `.{reachSuffixOfInboundHost}` (derive from the
  inbound Host minus its leftmost label); anything else -> "" (strip).
  `make test` green.
- [ ] 2.4 RED then GREEN: pin endpoint. Test: POST
  `/__agent-reverse-proxy-debug__/vhost-pin` `{"name":"app1","port":5000}`
  on the session listener -> subsequent label-less request proxies to :5000
  with `Host: app1.lvh.me:5000`; GET returns current pin; DELETE clears;
  invalid port/name -> 400; pin cleared on session end. Reuse the auth
  gating pattern of existing per-port proxy debug routes (see ADR-0043 /
  per-port proxy auth wrap tests in `proxy_listener_test.go`).
- [ ] 2.5 RED then GREEN: status payload. `buildStatusPayload` gains
  `previewVhostSuffix` and ordered `previewReachCandidates` per Design
  (explicit env first; derive lvh.me/sslip.io/window-host variants server
  side from the request's Host where derivable, else send the building
  blocks: `["lvh.me"?, "<ip>.sslip.io"?, "<request-host>"]`). Extend the
  existing buildStatusPayload test.
- [ ] 2.6 `make build golden-update`; review golden diff (expect init.json /
  compose only if envs surfaced there -- otherwise none); commit phase.

## Phase 3 -- frontend: logical<->reachable translation + probe + modes

Files: `static/modules/url-builder.js` (+ its `.test.js`, run the same way
existing url-builder tests run -- check `Makefile`/`package.json` for the
node test invocation), `static/terminal-ui.js`.

- [ ] 3.1 RED: url-builder tests for pure functions:
  - `logicalToVhostLabel('app1.lvh.me:5000', 'lvh.me')` -> `app1-5000`;
    no port -> `app1`; nested `a.b.lvh.me` -> reject (flat labels only,
    v1); non-suffix host -> null.
  - `buildVhostPreviewUrl(label, reach, proxyPort, protocol)` ->
    `http://app1-5000.reach:23000`.
  - `parseLogicalInput(raw, suffix)` handling `app1.lvh.me:5000/path?q#h`.
- [ ] 3.2 GREEN: implement in url-builder.js (exported, same module style
  as `buildSubdomainPreviewUrl`).
- [ ] 3.3 terminal-ui.js wiring (behavior-test what the harness allows;
  otherwise cover via Phase 4 e2e and say so in the log):
  - `setPreviewURL()`: hosts matching `*.{previewVhostSuffix}` (and bare
    suffix) are IN-IFRAME targets, localhost/127.0.0.1 unchanged, all else
    keeps the new-tab bounce.
  - Reach probe after the existing ADR-033 probes: try
    `probe-<rand>.<candidate>:<previewProxyPort>/` for each candidate from
    the status payload, `isReady` = has `X-Agent-Reverse-Proxy` header;
    store `_vhostMode = {mode: 'wildcard'|'pinned', reach}` per session.
  - Wildcard mode: iframe base `http(s)://{label}.{reach}:{proxyPort}`,
    shell page `?path=` mechanics unchanged; verify debug WS + console
    forwarding still connect on the new origin.
  - Pinned mode: POST vhost-pin, then load bare origin; switching logical
    hosts re-pins + reloads; surface mode+reach as a small indicator
    element next to `.terminal-ui__iframe-url-bar`.
  - `updateUrlBarPrefix()`: show active logical host:port (falls back to
    `localhost:{previewPort}`).
- [ ] 3.4 `make build golden-update`; golden diff review; commit phase.

## Phase 4 -- e2e (acceptance) + browser-MCP verification

Add `e2e/tests/preview-vhost.spec.js` following `ports.spec.js` patterns;
fixture: two tiny HTTP servers (3000 + 5000) that respond
`vhost-echo: {Host}` and can set a `Domain=.lvh.me` cookie at
`/set-cookie`. Boot via the e2e workflow (`make e2e-up-simple` /
`scripts/e2e.sh` -- follow `docs/dev/test-container-workflow.md`; the
browser-backend chromium is the REMOTE-browser stand-in: it must NOT get
lvh.me loopback-remapped to itself for these tests -- use the sslip.io/real
hostname path).

- [ ] 4.1 Wildcard mode: navigate `app1-3000.<reach>:<proxyPort>` and
  `app1-5000.<reach>:<proxyPort>`; assert bodies show
  `app1.lvh.me:3000` / `app1.lvh.me:5000` respectively (distinct origins,
  same listener port).
- [ ] 4.2 Cookie rewrite: hit `/set-cookie` on the 5000 origin; assert the
  browser stores it scoped to `.<reach>` and sends it to the 3000 origin.
- [ ] 4.3 Pinned mode: force reach candidates unresolvable (env override to
  a garbage domain); assert probe falls back, `app1.lvh.me:5000` renders
  via pin on the bare origin, switching to `app1.lvh.me:3000` swaps the
  target, and the mode indicator says pinned.
- [ ] 4.4 Regression: plain `localhost:{port}` preview flow unchanged
  (existing e2e stays green); `make test` fully green.
- [ ] 4.5 Browser-MCP manual verification list (use swe-swe-playwright /
  swe-swe-preview MCP tools; log expected/got for each):
  - URL bar accepts `app1.lvh.me:5000/`, prefix shows it, iframe renders
    the 5000 echo.
  - Mode indicator visible and correct in both modes.
  - Preview console messages still stream (debug hub works on new origin).

## Phase 5 -- docs

- [ ] 5.1 `docs/adr/0045-preview-host-demux.md`: two-hostname model, label
  grammar, reach probe + pinned mode, relationship to ADR-025/028/032/033
  and d5266dfb4; why pass-through and Domain-strip/preserve are wrong.
- [ ] 5.2 `docs/configuration.md`: `SWE_PREVIEW_VHOST_SUFFIX`,
  `SWE_PREVIEW_REACH_DOMAIN`. `CHANGELOG.md` entry. ASCII check.
- [ ] 5.3 Docker-free multi-service guide (new `docs/multi-service.md` or a
  section in the preview docs + container-facing
  `templates/container/.swe-swe/docs/app-preview.md`): running several
  services as plain processes (process-compose / foreman / backgrounded),
  reaching them via bare `{port}` labels and named routes, when vhost
  Host-rewrite matters (own traefik/nginx), and the tunnel degraded-mode
  note (pinned until Follow-up A). Make explicit that none of this needs
  `--with-docker`, and cross-link ADR-0013's socket-is-root warning for
  users who do choose compose.

## Phase 6 (may defer to follow-up task) -- registration + MCP tools

- [ ] 6.1 `GET/POST/DELETE /__agent-reverse-proxy-debug__/routes` on the
  session listener: named aliases `{"auth": {"port": 5000, "host":
  "auth.lvh.me"}}` consulted by grammar rule 3; validation as per label
  rules; cleared on session end; tests mirror 2.4.
- [ ] 6.2 MCP tools `preview_register_route` / `preview_list_routes` on the
  swe-swe-preview server (agent reads user's compose and registers
  aliases -- this is the "auto" in auto-register).
- [ ] 6.3 Frontend: datalist/dropdown of registered routes by the URL bar.
- [ ] 6.4 Declarative registration source: read `.swe-swe/services.yml`
  (schema: `{name: {port: 5000, host: "auth.lvh.me"}}`) at session start
  and register its entries through the same 6.1 routes store (file entries
  are seeds; runtime API wins on conflict; re-read on session restart).
  This is a registration SOURCE only -- swe-swe never starts, stops, or
  supervises the listed services (that is process-compose / the user's
  compose / the agent). Validation identical to 6.1. Document the schema
  in the 5.3 guide.

## Follow-ups (separate deliverables -- do NOT implement in this worktree)

### Follow-up A -- tunneld named-label grammar (swe-swe-tunnel repo)
Goal: tunnel sessions get wildcard mode instead of pinned. Division of
labor keeps tunneld dumb: numeric leftmost label keeps today's raw-port
dispatch (back-compat); any NON-numeric leftmost label (named `app1-5000`,
probe `probe-<rand>`, alias `app1`) forwards the request UNCHANGED
(Host preserved) to the session's preview proxy listener port, where this
task's ResolveTarget hook already implements the grammar. tunneld learns
no grammar beyond "numeric or not". Reach probing then succeeds over
`*.{unique}-tunnel.<suffix>` with zero swe-swe-side changes (the
window-hostname candidate covers it). File as a tasks/ doc in the
swe-swe-tunnel repo once Phase 4 here is green; verify with the e2e
fixture from Phase 4 pointed at a local tunneld.

### Follow-up B -- docker-socket alternatives for compose users
Users whose stacks genuinely need compose currently need `--with-docker`
= host-root-equivalent socket (ADR-0013). Evaluate, in order of least
new-machinery: (1) docker-socket-proxy allowlist in front of the mounted
socket (block privileged, host binds, host network); (2) rootless
Docker/Podman socket instead of the rootful one; (3) sysbox-runc for a
real in-container daemon with no host socket. Outcome: an ADR picking one
(or documenting why status quo + 5.3's docker-free guidance is enough).

## Progress log
(execute-step-by-step updates checkboxes above and phase .log files;
summarize per-phase commits here as they land)
