<!-- agent-chat export
title: swe-swe 2.30.0 npm release
date: 2026-07-19
index: 02
slug: swe-swe-2-30-0-npm-release
session: 77a49b9b19c1d56c
agent: claude
version: 0.8.14 (b858a6b)
-->

# swe-swe 2.30.0 npm release

_2026-07-19 · 02 · claude · agent-chat 0.8.14 (b858a6b)_

**USER**

> /npm-login-and-publish

<small>took 28.6s</small><br>
**AGENT**

> Ready to release **swe-swe** (main package + 6 platform packages). Currently published: **2.29.1** (local matches, main is pushed — recent merges include swe-npx + Agent View tunnel).
>
> What version should I bump to? I'd suggest **2.30.0** since the unreleased work includes new features.

[Quick replies]
- 2.30.0
- 2.29.2
- Cancel

**USER**

> 2.30.0

<small>took 1m 13s</small><br>
**AGENT**

> Bumped to **2.30.0** (package.json + regenerated docs), but I hit a conflict before committing:
>
> Another session is editing `cmd/swe-swe/dockerless.go` and `cmd/swe-swe/init.go` **right now** (file mtimes are seconds old — a `clearDockerlessMarker` fix, uncommitted). If I build/publish now, that half-finished code gets baked into the 2.30.0 binaries without being in the release commit.
>
> How do you want to proceed?
> - **Wait** — I'll hold until that session commits, then release 2.30.0 including it
> - **Proceed anyway** — publish with whatever is in the tree (not recommended)

[Quick replies]
- Wait for the other session to commit
- Proceed anyway
- Cancel the release

**USER**

> Proceed anyway

<small>took 1m 0s</small><br>
**AGENT**

> npm token was stale — please log in here while I build the platform binaries:
>
> **https://www.npmjs.com/login?next=/login/cli/[REDACTED]**
>
> (Release commit 3c6912db1 is pushed. Building all 6 platform binaries now; I'll ask for the publish OTP once the build is done.)

**USER**

> Logged in. Commit rebuild then ask for my otp

<small>took 16.5s</small><br>
**AGENT**

> Login confirmed (`choonkeat`) and all 6 platform binaries are built and stamped **2.30.0**.
>
> Send me a fresh 6-digit OTP and I'll publish all 7 packages.

<small>took 1m 7s</small><br>
**AGENT**

> Done — the dockerless-marker fix (ac5b9b03d) is pushed and all 6 platform binaries are rebuilt from that committed SHA, stamped 2.30.0.
>
> Ready to publish all 7 packages — what's your fresh 6-digit OTP?

[Quick replies]
- Cancel

**USER**

> [REDACTED]

<small>took 1m 29s</small><br>
**AGENT**

> Publish succeeded for all 7 packages. 6/7 already confirmed live on the registry; waiting ~30-60s for `@choonkeat/swe-swe-win32-arm64` to propagate (normal CDN lag), then I'll send the final summary.

<small>took 8.3s</small><br>
**AGENT**

> **swe-swe 2.30.0 released** — all 7 packages published and confirmed live on the registry:
>
> - `swe-swe@2.30.0` (main)
> - `@choonkeat/swe-swe-linux-x64@2.30.0`
> - `@choonkeat/swe-swe-linux-arm64@2.30.0`
> - `@choonkeat/swe-swe-darwin-x64@2.30.0`
> - `@choonkeat/swe-swe-darwin-arm64@2.30.0`
> - `@choonkeat/swe-swe-win32-x64@2.30.0`
> - `@choonkeat/swe-swe-win32-arm64@2.30.0`
>
> Binaries were built from pushed SHA **ac5b9b03d**, which includes the release bump (3c6912db1) and the sibling session's dockerless-marker fix. Registry propagation for win32-arm64 lagged ~30s but is now confirmed.

[Quick replies]
- Thanks, done

