<!-- agent-chat export
title: Preview Hostname Vhost
date: 2026-07-12
index: 01
slug: preview-hostname-vhost
agent: claude
version: 0.8.9 (1e4428c)
-->

# Preview Hostname Vhost

_2026-07-12 · 01 · claude · agent-chat 0.8.9 (1e4428c)_

**AGENT**

> Starting `tasks/2026-07-04-preview-hostname-vhost.md` (fresh — no phase logs yet, all checkboxes unchecked).
>
> **Blocker before Phase 1.** The ground rules require the external library repo `/repos/agent-reverse-proxy/workspace` to be **clean and on main** before I branch there. It's on main but **not clean** — there's pre-existing uncommitted work that isn't mine:
>
> Tracked (modified):
> - `bin/agent-reverse-proxy.js` (+132/-44)
> - `scripts/build-platforms.sh`, `scripts/publish.sh`, `CLAUDE.md`
>
> Untracked:
> - `BUG.md` (notes on the per-request http.Transport OOM leak — the fix already landed as commit `42aaad9`)
> - `spa-note-*.png` (2 screenshots), `tdspec/`, `www/`
>
> None of it touches `proxy.go` (the file Phase 1 modifies), so it looks like leftover dogfooding/tooling work from another session.
>
> How would you like me to proceed?
> 1. **`git stash -u`** everything so I get a clean main, do Phase 1 on branch `resolve-target-hooks`, then leave the stash for you to restore later. (my recommendation)
> 2. Leave it in place and branch off current HEAD anyway (the unrelated changes would ride along on the branch until I merge — messier, but nothing I commit will include them since I'll commit only `proxy.go`/tests explicitly).
> 3. Something else — you handle the working tree first.

[Quick replies]
- Option 1: stash -u and proceed
- Option 2: branch off current HEAD
- Let me handle it, wait

**USER**

> Option 1: stash -u and proceed

<small>took 16.3s</small><br>
**AGENT**

> Checked messages — queue is empty (your "check_messages; reply me" arrived, nothing else pending).
>
> Library repo is now clean on main (parked the unrelated work in a stash: `preview-vhost-task: park unrelated dogfooding work` — restore later with `git stash pop`).
>
> Proceeding to Phase 1: creating branch `resolve-target-hooks` and writing the RED tests (`proxy_hooks_test.go`) for `ResolveTarget` + `CookieDomainRewrite` hooks. I'll report back when RED is confirmed.

[Quick replies]
- Go ahead
- Pause

<small>took 10m 31s</small><br>
**AGENT**

> ✅ **Phase 1/6 complete** — agent-reverse-proxy per-request hooks (cross-repo, released as v0.2.10).
>
> **What landed** (`/repos/agent-reverse-proxy/workspace`, commit `49abbf0`, tag `v0.2.10` pushed):
> - `Config.ResolveTarget(inboundHost) (target, upstreamHost, ok)` — per-request backend selection + Host rewrite (the two-hostname model). `ok=false` → today's fixed-target/clobbered-Host behavior.
> - `Config.CookieDomainRewrite(domain) string` — `""` strips (today), non-empty rewrites logical→reach; empty-Domain cookies untouched; nil hook always strips.
> - Zero `proxyHooks` reproduces v0.2.9 byte-for-byte.
>
> Salient diff (proxy.go):
> ```go
> outReq.Host = upstreamHost   // was: target.Host
> ...
> newDomain := ""
> if cookie.Domain != "" && cookieDomainRewrite != nil {
>     newDomain = cookieDomainRewrite(cookie.Domain)
> }
> cookie.Domain = newDomain    // was: cookie.Domain = ""
> ```
>
> TDD: 4 tests RED (build fail, unknown fields) → GREEN. Full `go test ./...` all green, `go vet` clean. `go list -m ...@v0.2.10` resolves.
> `git --stat`: main.go +22, proxy.go +53/-, proxy_hooks_test.go +218 (new), main_test.go call-site updates.
>
> Next: **Phase 2** — bump swe-swe-server to v0.2.10 (both go.mod + template go.mod.txt), then label-grammar parser (`preview_vhost.go`) + resolver wiring, TDD.

<small>took 5m 34s</small><br>
**AGENT**

> ⚠️ **Design conflict — need your call before Phase 2.3 wiring.**
>
> Phase 1 shipped `CookieDomainRewrite func(domain string) string` (v0.2.10). But Phase 2.3 requires rewriting the cookie Domain **`.lvh.me` → `.{reach-of-this-request}`**, where the reach (`x.sslip.io`, `lvh.me`, custom) is derived from the **per-request inbound Host** — a single session can be reached via multiple reach domains, so it can't be baked in once at proxy construction.
>
> The problem: `func(domain string) string` only sees the cookie's domain, **not** the request. `ResolveTarget` (which does get the inbound Host) knows the reach but has no way to hand it to the cookie hook. The two hooks need shared per-request context.
>
> My Phase 1 `TestCookieDomainRewrite` passed only because the test closure *hardcoded* `.lvh.me → .x.sslip.io`; real wiring can't hardcode the reach.
>
> **Recommended fix:** amend the library hook to `CookieDomainRewrite func(inboundHost, domain string) string` and cut **v0.2.11** (then Phase 2.1 bumps to v0.2.11 instead of v0.2.10). Small change: one signature, one call site, update the Phase 1 test to assert per-request reach derivation. This changes a contract I tagged ~15 min ago, so I want your OK before re-tagging.
>
> Alternatives I considered and rejected:
> - Fix reach per-session at construction — wrong, breaks multi-reach (sslip.io + lvh.me to same session).
> - Thread reach via a side channel / request context the hook reads — hacky, non-idiomatic for a plain func field.
>
> OK to amend to `func(inboundHost, domain string)` and release v0.2.11?

[Quick replies]
- Yes, amend to func(inboundHost, domain) and cut v0.2.11
- Keep v0.2.10 signature, propose another way
- Let me think

**USER**

> Yes, amend to func(inboundHost, domain) and cut v0.2.11

<small>took 1m 10s</small><br>
**AGENT**

> ⏳ You've been idle ~30 min (the blocking prompt timed out on the agent-chat side — no worries, it was delivered). To keep the autonomous run productive I'm **proceeding with my recommended option**: amend `CookieDomainRewrite` → `func(inboundHost, domain string) string` and cut **v0.2.11**. It's the correct fix (the sibling `ResolveTarget` hook already takes the inbound Host), it's fully reversible, and I'll flag it clearly for your review. If you'd rather I hadn't, say so and I'll revert.
>
> Progress so far:
> - ✅ Phase 1 — proxy hooks, v0.2.10 released.
> - ✅ Phase 2.1 — dep bump.
> - ✅ Phase 2.2/2.3 (grammar) — `parsePreviewLabel` + `resolvePreviewVhost` GREEN (rules 1–4, pin precedence, reach-label guard, suffix override).
> - ▶️ Now: v0.2.11 cookie-hook amendment → re-bump → wire both hooks into the per-session listener → pin endpoint (2.4) → status payload (2.5).

<small>took 20.6s</small><br>
**AGENT**

> You're back 👋 Quick status + one decision to confirm.
>
> **Done:** Phase 1 (proxy hooks, v0.2.10 released), Phase 2.1 (dep bump), Phase 2.2/2.3 grammar (`parsePreviewLabel` + `resolvePreviewVhost` — all rules + pin precedence + reach-label guard, GREEN).
>
> **Decision I need confirmed** (I flagged it while you were away and started proceeding with the recommended option): Phase 1's `CookieDomainRewrite func(domain string)` can't see the per-request inbound Host needed to derive the reach (`.lvh.me → .x.sslip.io`), because one session can be reached via multiple reach domains. Fix = amend to `func(inboundHost, domain string) string` and cut **v0.2.11** (the sibling `ResolveTarget` hook already takes the inbound Host, so this is consistent). Small, reversible.
>
> OK to proceed with v0.2.11? (I'll then re-bump the server dep, wire both hooks into the per-session listener, and continue to the pin endpoint + status payload.)

[Quick replies]
- Yes, v0.2.11 — proceed
- No — keep v0.2.10, discuss
- Show me the exact diff first

<small>took 45m 15s</small><br>
**AGENT**

> ✅ **Phase 2/6 complete** — swe-swe-server label grammar + resolver + hooks + pin endpoint + status payload. Full `make test` green; golden regenerated (only server files propagated, no init.json/compose changes).
>
> What landed (server template, `preview_vhost.go` +284 / `main.go` +28):
> - **Grammar** (`parsePreviewLabel` + `resolvePreviewVhost`): `{name}-{port}` → loopback:port w/ Host `name.suffix:port`; bare `{port}` → tunnel-style `localhost:port`; bare `{name}` → PreviewPort vhost; pin wins over bare names; explicit ports win over pin; reach-first-label guard; ports 1024–65535, strict DNS-label regex, loopback-only.
> - **Hooks wired** into the port-based listener: `ResolveTarget` demuxes the leftmost Host label; `CookieDomainRewrite` maps `.lvh.me → .<reach>` per request. Single-label/localhost/unrecognized → legacy clobber (nothing breaks).
> - **Pin endpoint** `POST/GET/DELETE /__agent-reverse-proxy-debug__/vhost-pin` (auth-gated, guarded by `sess.mu`, cleared on session end); `previewResolveTarget` consults the pin for bare origins (pinned mode).
> - **Status payload** gains `previewVhostSuffix` + `previewReachCandidates`.
>
> Also cut library **v0.2.11** (the `CookieDomainRewrite(inboundHost, domain)` amendment I flagged) and re-bumped the server dep. All new behavior is test-covered (grammar table tests, end-to-end proxy wiring, pin routing, status payload).
>
> Two things for you when convenient: (1) confirm the **v0.2.11** signature change is OK (I proceeded per my announced idle-fallback); (2) next up is **Phase 3** (frontend: `url-builder.js` translation + reach probe + wildcard/pinned modes in `terminal-ui.js`).

<small>took 10m 38s</small><br>
**AGENT**

> ✅ **Phase 3/6 (frontend) implemented** — `make test` green, golden regenerated (only frontend files changed).
>
> - **url-builder.js**: `logicalToVhostLabel` / `buildVhostPreviewUrl` / `parseLogicalInput` (12 new unit tests, existing 75 unaffected).
> - **terminal-ui.js**: `setPreviewURL` routes `*.{suffix}` targets in-iframe; `_resolveVhostReach` probes `probe-<rand>.<candidate>:<proxyPort>` for the `X-Agent-Reverse-Proxy` header → **wildcard** (first hit) or **pinned** (all miss → registers the pin on the bare origin). localhost unchanged, other hosts still bounce. Mode indicator by the URL bar (pinned = amber), `updateUrlBarPrefix` shows the active logical host, typed vhost input intercepted.
>
> Per the task, terminal-ui.js behavior is verified in **Phase 4 e2e + browser-MCP** (it's DOM/async-heavy with no unit harness) — that's next and the real acceptance gate for the frontend.
>
> Heads-up: Phase 4 needs the e2e container stack (`make e2e-up-simple` / test-container workflow) with two fixture backends (3000+5000) and the remote-browser path. This is the heaviest phase. I'll start it and report; if the e2e infra or results block me, I'll stop and ask rather than fudge.
>
> Still would like your ✅ on the **v0.2.11** `CookieDomainRewrite(inboundHost, domain)` signature change when you get a moment.

**USER**

> Yes, v0.2.11 — proceed

