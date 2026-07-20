<!-- agent-chat export
title: Swe Npx Hardening Verified Cache Offline Fallback Https Registry Size Caps Memo Downgrade
date: 2026-07-19
index: 08
slug: swe-npx-hardening-verified-cache-offline-fallback-https-registry-size-caps-memo-downgrade
session: 0900c1b7d9eab8eb
agent: claude
version: 0.8.16 (b49d6cc)
-->

# Swe Npx Hardening Verified Cache Offline Fallback Https Registry Size Caps Memo Downgrade

_2026-07-19 · 08 · claude · agent-chat 0.8.16 (b49d6cc)_

**USER**

> Does swe-npx resolve the latest version always? Use cached version when network is unavailable/fetch fails? Security concerns?

<small>took 58.5s</small><br>
**AGENT**

> Read `cmd/swe-swe/templates/host/swe-npx/main.go` end to end. Answers:
>
> ## 1. Does it always resolve latest?
> **No — it's "latest, memoized 15 min."**
>
> - All our call sites are unpinned (`swe-npx -y @choonkeat/agent-chat`), so they take the `latest` path (main.go:432).
> - `lookupLatest` writes `<cache>/<pkg>.latest` and, within `SWE_NPX_LATEST_TTL` (default **15m**, main.go:473), returns it **without any network call** (main.go:212).
> - After the TTL it re-hits `registry/<pkg>` and re-reads `dist-tags.latest`. So a fresh publish is picked up within ~15 min of the next launch, not instantly.
> - An explicit `@1.2.3` bypasses dist-tags entirely and is cached forever under `<pkg>@<ver>/`.
>
> ## 2. Cached fallback when the network fails?
> **Partly — and there's a real gap.**
>
> Works: if `Get(registry/<pkg>)` errors or returns non-200 (non-404), it falls back to the **newest cached version** with a stderr note (main.go:222-238).
>
> Gaps I'd flag:
> 1. **Stale memo + missing tarball = hard fail.** If the memo (or a reachable registry) says `latest = 0.9.0` but the cache only holds `0.8.0`, `resolve` goes straight to `downloadAndCache`, `fetchVersionDoc` fails offline, and it **errors out instead of falling back to the cached 0.8.0** (main.go:440-444). This is the likely real-world offline breakage: registry reachable enough to answer the packument but the tarball/CDN fetch fails, or the memo was written just before going offline.
> 2. **404 never falls back**, by design (treated as "wrong package kind"). Fine, but note a registry outage that serves 404s = no offline start.
> 3. **5s client timeout covers the whole body read** (main.go:485), not just connect. On a slow link a multi-MB tarball download will abort at 5s — and that failure has no cached fallback either (see #1).
>
> ## 3. Security
> Decent hygiene, but the trust model is thin. Ranked:
>
> **Real issues**
> - **Cache is never re-verified.** On a hit, `resolve` just `stat`s and `exec`s (main.go:441). Integrity is checked only at download. Anything running as the same UID can overwrite `~/.swe-swe/npx-cache/.../bin/<name>` and it gets exec'd on the next MCP start — a persistent, no-network code-exec foothold. Same for the `.latest` memo (a same-user process can pin an old vulnerable version = silent downgrade).
> - **No independent trust anchor.** `dist.integrity` and `dist.tarball` come from the *same* registry response, so the sha512 check only defends against CDN/transport tampering, not a hostile or compromised registry. No npm provenance/sigstore check. And when **both `integrity` and `shasum` are empty, the tarball is accepted silently** (main.go:301).
> - **`SWE_NPX_REGISTRY` is unvalidated** — no scheme check, so `http://` or an attacker-set env var redirects the whole supply chain. Also `dist.tarball` is fetched verbatim, no host check against the registry.
> - **Unbounded memory / decompression bomb.** `io.ReadAll` on the tarball (main.go:332) plus unbounded `io.Copy` per entry (main.go:410) — no size cap anywhere.
>
> **Handled well**
> - Path traversal rejected (`..` / absolute, main.go:393); non-regular entries incl. symlinks/hardlinks skipped (main.go:417); setuid/setgid stripped via `&0777`; atomic temp-dir + rename with a benign race loser; constant-time hash compare.
>
> **Minor**
> - `os.FileMode(hdr.Mode)&0777|0644` preserves a world-writable 0777 from the tarball — worth masking to 0755/0644.
>
> **If you want fixes**, my priority order: (a) fall back to newest cached version when the pinned/latest download fails, (b) record the verified digest at cache time and re-verify (or at least check perms/ownership) before exec, (c) reject non-https `SWE_NPX_REGISTRY`, (d) size caps + a longer download timeout separate from the metadata timeout.

[Quick replies]
- Fix (a) offline fallback to cached version
- Fix all four
- Just (c) https-only registry
- Not now, thanks

**USER**

> Fix all four

<small>took 8m 10s</small><br>
**AGENT**

> All four fixed, tested, golden-updated, committed as `cb44a5518` (unpushed).
>
> ## (a) Offline fallback now covers the download
> `resolve` (main.go) tracks whether the caller pinned a version. On any *non-security* download failure for an unpinned request it falls back to the newest **verified** cached version with a stderr note. Pinned `@1.2.3` still fails hard (falling back would violate the pin), and integrity failures are wrapped in a new `securityError` type so `errors.As` keeps them fatal — a tampered tarball can never trigger the fallback. `lookupLatest`'s existing fallback now also only considers verified entries.
>
> ## (b) Cache entries re-verified before every exec
> - `writeCacheDigest` records `sha256-<hex>` of the extracted binary in `.swe-npx-digest` inside the temp dir, so it lands atomically with the rename.
> - `verifyCacheEntry` re-hashes on every cache hit and also rejects a non-regular or group/world-writable binary.
> - A failing entry is reported (`discarding untrusted cache entry`) and `RemoveAll`'d, then re-downloaded — previously the rename-race branch would have handed the tampered copy straight back.
> - Entries written before this change have no digest, so they get re-downloaded once. Noted in the CHANGELOG.
>
> ## (c) https-only registry
> `validateRegistry` in `defaultOptions` (which now returns an error) rejects anything but https, allowing http for loopback hosts only. `checkTarballURL` holds the tarball location to the same rule and prints a note when the tarball host differs from the registry host. Also: a version doc advertising **neither** `integrity` nor `shasum` is now rejected instead of silently accepted.
>
> ## (d) Size caps + split timeouts
> - Caps: 8 MiB metadata (`io.LimitReader` on both JSON decodes), 128 MiB compressed tarball (`readCapped` — over-long is an error, not a truncation), 512 MiB total unpacked with a running budget in `extractPackageTree`.
> - Timeouts split: metadata client 15s, download client 5m (`options.downloadClient()`), so a slow multi-MB binary is no longer killed by the metadata budget.
> - Bonus from the "minor": extracted modes are masked `&0755|0644`, never group/world-writable.
>
> ## Verification
> - 11 new tests: unpinned-falls-back, pinned-never-falls-back, integrity-never-falls-back, tampered-cache-discarded, undigested-not-trusted, world-writable-not-trusted, missing-integrity-rejected, validateRegistry table, plain-http-tarball-rejected, unpacked-cap (streamed bomb, no 512 MB allocation in the test), readCapped.
> - Existing tests updated to seed caches through a `seedCache` helper that writes the digest.
> - `make test` green across all 10 packages; `make build golden-update` diff is **only** the 47 `swe-npx/main.go` golden copies — no other template drifted.
>
> Not done (wasn't in the four, say the word if you want it): the memo-downgrade vector — a same-UID process can rewrite `<pkg>.latest` to pin an old version for up to the TTL. The digest check makes the *binary* authentic, but authentic-and-old.

[Quick replies]
- Looks good, thanks
- Also fix the memo-downgrade vector
- Push it
- Show me the resolve() diff

**USER**

> Also fix the memo-downgrade vector

<small>took 40.6s</small><br>
**AGENT**

> Working on the memo-downgrade fix now (already in progress from your last reply — the redelivery was a duplicate, no action needed).

<small>took 4m 39s</small><br>
**AGENT**

> Committed `60ddfbf4b` — memo-downgrade closed. `make test` green, golden updated (49 `swe-npx/main.go` copies, nothing else drifted).
>
> **The fix, in `lookupLatest`:**
> 1. **The cache is the floor.** A memo naming a version older than the newest cached one is ignored and the registry re-checked. A tampered memo now buys a network round-trip, not a downgrade window.
> 2. **The floor is a directory listing, not a digest pass** — the warm path stays cheap. It is only ever used to *reject* a memo (fail-closed toward asking the registry); whatever is actually returned still goes through the digest check before exec.
> 3. **The memo records its own write time** (`<version> <unix>`), and both that stamp and the mtime must be inside the TTL — a restore or `touch` can no longer extend it, and a future-dated memo counts as expired rather than valid forever. Legacy bare-version memos still read, dated by mtime.
> 4. **Registry-declared rollbacks are honoured but announced** — a legitimately rolled-back dist-tag still wins (the registry is authoritative), it just no longer happens silently.
>
> **Tests added (5):** memo-below-floor ignored + registry re-checked + memo refreshed; memo-at-floor still does zero network; `readMemo` expiry table (fresh / stale-stamp-fresh-mtime / future-dated / legacy / stale-mtime); registry rollback announced.
>
> **Honest limit:** this is not a boundary against a same-UID attacker — they can overwrite the `swe-npx` binary itself, or `~/.bashrc`. It closes the cheapest *silent* path, which is what was actually cheap here.
>
> Next: `git push` when you want these two commits (`cb44a5518`, `60ddfbf4b`) upstream — note main also carries three commits from a sibling session.

[Quick replies]
- Push it
- Show me the new lookupLatest
- Done for now
- Run the dockerless e2e against it

**USER**

> /wrap-up

