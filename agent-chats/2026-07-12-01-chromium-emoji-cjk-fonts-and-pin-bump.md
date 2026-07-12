<!-- agent-chat export
title: Chromium Emoji Cjk Fonts And Pin Bump
date: 2026-07-12
index: 01
slug: chromium-emoji-cjk-fonts-and-pin-bump
agent: claude
version: 0.8.9 (6301293)
-->

# Chromium Emoji Cjk Fonts And Pin Bump

_2026-07-12 · 01 · claude · agent-chat 0.8.9 (6301293)_

**USER**

> There’s a bug with our chromium. See chat log and discuss a fix to swe-swe
>
> [chat-export-2026-07-12-02-30-56.html](./assets/2026-07-12-01-1-95d28a5c7fdf.html)

<small>took 1m 5s</small><br>
**AGENT**

> Read the chat log + traced it in the source. The diagnosis holds: the container ships only `fonts-dejavu-core` (pulled in as a chromium dep), no color-emoji font, so Chromium draws `.notdef` tofu for every emoji. Not locale, not CLI flags.
>
> **Where it needs fixing (2 places, both install chromium but no fonts):**
> - `cmd/swe-swe/templates/host/Dockerfile:144` — the main all-in-one image (embedded template → golden tests)
> - `docker/browser-backend/Dockerfile:27` — the standalone lean browser-backend image
>
> **The fix** — add to both apt blocks:
> ```
> fonts-noto-color-emoji \
> ```
> That's the Debian package for the exact font the chat session hand-installed. ~10 MB, fixes emoji everywhere: Playwright screenshots, noVNC Agent View, Preview.
>
> **CJK question:** `fonts-noto-cjk` fixes Chinese/Japanese/Korean text tofu too, but it's ~110 MB added to the image (vs ~10 MB for emoji). I'd add emoji now (clearly the reported bug) and hold CJK unless you actually render CJK pages.
>
> **Process note:** the main Dockerfile is a template, so this needs `make build golden-update` + committing the regenerated `cmd/swe-swe/testdata/golden/` (every golden Dockerfile picks up the new line). The browser-backend one is a plain file, no golden churn.
>
> Want me to: (a) emoji only, both Dockerfiles, run golden-update, commit on a branch; (b) emoji + CJK; or (c) just show you the exact diffs first?

[Quick replies]
- (a) emoji only, both files, golden-update + branch
- (b) emoji + CJK
- (c) show diffs first
- Also apply the per-session local fix now

**USER**

> (b) emoji + CJK

<small>took 38.1s</small><br>
**AGENT**

> Both Dockerfiles patched (emoji + CJK + rationale comments). Running `make build golden-update` to regenerate golden Dockerfiles...

<small>took 1m 10s</small><br>
**AGENT**

> Done — fix committed on branch `fix/browser-emoji-cjk-fonts` (`00cc24f69`).
>
> **What changed** (both browser layers, since both install chromium with no emoji/CJK font):
> - `cmd/swe-swe/templates/host/Dockerfile` — main all-in-one image template
> - `docker/browser-backend/Dockerfile` — standalone lean browser-backend image
>
> Added to each apt block:
> ```
> fonts-noto-color-emoji \
> fonts-noto-cjk \
> ```
> plus a rationale comment so nobody strips them later.
>
> **Verification:**
> - `make build golden-update` regenerated all 46 golden Dockerfiles — diff is *only* these font lines + comment, nothing else.
> - `go test -run TestGolden` passes.
> - Excluded the unrelated pre-existing untracked `tasks/2026-07-11-mcp-npx-cwd-collision.md` from the commit (it was in the working tree before I started).
>
> **Effect once rebuilt:** emoji + CJK render correctly everywhere the container Chromium is used — Playwright screenshots, noVNC Agent View, Preview. No locale/flag change was needed; it was purely missing fonts.
>
> **Note:** this fixes the *containerized* paths. The dockerless path uses the host's system Chromium + host fonts, so on a machine without emoji fonts you'd still see tofu there — that's a host-provisioning concern, not something this image build can cover. Want me to push the branch / open a PR, or leave it local?

[Quick replies]
- Push branch + open PR
- Leave it local
- Merge to main locally
- Rebuild + test it live in a container

**USER**

> Rebuild + test it live in a container

<small>took 21.4s</small><br>
**AGENT**

> Got it — here and working. Kicking off the live container test now: acquire a test slot → build images (this compiles the new Dockerfile with the emoji + CJK fonts) → boot the stack → drive Chromium to an emoji/CJK page via the browser MCP and screenshot to confirm real glyphs instead of tofu → tear down.
>
> Build + boot takes a few minutes. I'll post progress and come back with the screenshot proof. Reply "go" or just wait.

[Quick replies]
- go
- Skip the live test, just push the branch
- Only rebuild, don't run browser test

**USER**

> go

<small>took 2m 8s</small><br>
**AGENT**

> Heads-up: the test-container harness auto-detected the wrong host path (a concurrent e2e-simple container threw off its detection), so `docker compose build` choked on a stale override. Pivoting to build the generated Dockerfile directly and verify Chromium's fontconfig actually sees the emoji + CJK fonts. Building now (chromium + font layer)...

<small>took 1m 0s</small><br>
**AGENT**

> Image building (compiling Go server + chromium/font layer, ~60 steps). I'll verify fonts land in fontconfig and render an emoji/CJK page as soon as it's up.

<small>took 212m 12s</small><br>
**AGENT**

> ✅ **Font fix proven live.** Screenshot attached: with `fonts-noto-color-emoji` + `fonts-noto-cjk` (exactly the two packages my change adds), containerized Chromium renders full-color emoji 🎉🔥😀🚀❤️👍🌈 and CJK — Chinese 你好世界, Japanese こんにちは, Korean 안녕하세요 — as real glyphs. Zero tofu. `fc-list` inside the image confirms both fonts are registered in fontconfig, which is exactly what Chromium queries for fallback.
>
> **How I tested:** the real swe-swe image wouldn't build (see blocker below), so I isolated the font layer — built a minimal image with the *current* chromium + the two Noto packages, wrote an emoji/CJK page, and rendered it headless. That cleanly answers the font question.
>
> ⚠️ **Blocker I hit — the swe-swe image is currently unbuildable, unrelated to fonts:** the Dockerfiles pin `chromium=147.0.7727.137-1~deb12u1`, but Debian bookworm/main has purged 147 — only `150.0.7871.100-1~deb12u1` is available now. So *any* build fails today with "Version 147... not found." This is the stale pin the Dockerfile comment predicted ("when bookworm/main's chromium eventually rolls, bump this version"). It just rolled.
>
> ℹ️ **Bonus data point:** chromium 150.0.7871.100 launched fine in my headless render (no SIGTRAP). The original SIGTRAP was on 150.0.7871.**46** in the full Xvfb/zygote path though — this is .**100** and headless — so it's encouraging but NOT conclusive that the Xvfb zygote path is fixed. Would need the real display-stack launch to be sure.
>
> **State:**
> - Branch `fix/browser-emoji-cjk-fonts` (`00cc24f69`) — font fix, both Dockerfiles + regenerated golden, golden tests pass. Nothing pushed.
> - Test harness torn down, slot released, test image removed.
>
> **Recommend next:** bump the chromium pin to 150.0.7871.100 in both Dockerfiles (it's a hard blocker — the image can't build at all right now), and do a real Xvfb-path launch test to confirm no SIGTRAP before shipping. Want me to take that on (same branch or a separate one), or just push the font branch as-is?
>
> <div style="display:flex;flex-wrap:wrap;gap:8px;">
> <a href="./assets/2026-07-12-01-2-54b9b057c96e.png" style="flex:0 1 calc(33% - 8px);max-width:calc(33% - 8px);"><img src="./assets/2026-07-12-01-2-54b9b057c96e.png" alt="render2.png" style="width:100%;height:auto;display:block;border-radius:6px;"></a>
> </div>

[Quick replies]
- Bump the chromium pin too + real launch test (same branch)
- Chromium pin on a separate branch
- Just push the font branch as-is
- Leave everything local for now

<small>took 6ms</small><br>
**AGENT**

> Live-test surfaced a *separate* blocker before I could get to fonts:
>
> **The swe-swe image can't build at all right now.** The Dockerfile pins `chromium=147.0.7727.137-1~deb12u1`, but Debian bookworm/main has rolled that away — the only version now available is `150.0.7871.100-1~deb12u1`. So `apt-get install chromium=147...` fails with "Version not found," which is exactly the "when bookworm/main's chromium eventually rolls, bump this version" case the Dockerfile comment predicted. This affects both Dockerfiles and is unrelated to my font change — it's a pre-existing stale pin that just went live.
>
> Note: the SIGTRAP task pinned to 147 *because* 150.0.7871.**46** crashed on launch. What's available now is 150.0.7871.**100** — a newer point release that may or may not still SIGTRAP; I'd need to test it.
>
> **For the font fix itself:** since I can't build the real image with the dead pin, I'll prove the fonts in isolation — build a small image with the *current* chromium + the two Noto packages, render an emoji+CJK page headless, and screenshot it. That cleanly answers "do the fonts fix the tofu."
>
> Proceeding with the isolated font proof now. Separately, want me to also bump the chromium pin to 150.0.7871.100 (with a launch test) so the image builds again? That's arguably the more urgent fix.

[Quick replies]
- Do the font proof, and yes bump chromium pin too (test it)
- Just the font proof for now
- Investigate the chromium pin separately first

<small>took 5m 55s</small><br>
**AGENT**

> Correct — full history from `tasks/2026-07-05-e2e-system-chromium-sigtrap.md`:
>
> - chromium **150.0.7871.46** (bookworm-security) **SIGTRAPs on launch** — zygote dies, every browser feature dead. Version-specific, not kernel: same host, 149 launched fine, 150 cored. Root-cause syscall was never captured (no gdb/strace) — presumed seccomp/zygote.
> - 149 worked but the security pocket purges old versions, so it wasn't a stable pin.
> - **147** from bookworm/**main** was the only permanently-available known-good pin → that's why it was pinned + held.
>
> **What's changed now:** 147 has been purged from main too. Only **150.0.7871.100** is available (main *and* security). So we've lost the safe pin — 147 and 149 are both gone. The only candidate left is a 150.x.
>
> **The encouraging bit from my test:** the SIGTRAP repro in that doc was literally `chromium --headless --no-sandbox --disable-gpu` cong (exit 133). In my font test, that *same* headless+no-sandbox invocation on 150.0.7871.**100** rendered fine (exit 0) on this same host kernel (6.8.0-134). So .100 appears to have fixed whatever .46 tripped — a later point release of the same major.
>
> **But not conclusive:** the real swe-swe path doesn't run `--headless`. It runs chromium under Xvfb + x11vnc with the **zygote** (which is exactly what died on .46). My headless test uses a different process model, so it doesn't exercise the zygote path.
>
> **The decision that matters:** since there's no known-good pin left, the only way forward is to validate 150.0.7871.100 in the *real* Xvfb/zygote path. If it launches there, bump both Dockerfiles to .100 (image builds again + fonts + browser all work). If it still SIGTRAPs, we're in a harder spot — no available working chromium — and would need to source it elsewhere or tackle the seccomp profile.
>
> Want me to run that Xvfb-path launch test on 150.0.7871.100 now? It's the gating question for both the font fix shipping and the image building at all.

[Quick replies]
- Yes, run the Xvfb-path test on 150.0.7871.100
- Just leave the font branch, don't touch the pin
- Investigate sourcing 147 elsewhere instead

**USER**

> we’ll test for real after our /swe-swe:reboot commit what we have and /ck:export-chat-logs

<small>took 1m 1s</small><br>
**AGENT**

> One blocker before I commit + reboot — and it changes the plan:
>
> `/swe-swe:reboot` step 1 **rebuilds the Docker images from templates**. With chromium 147 purged from Debian, that rebuild will **fail** ("Version 147 not found") — so the reboot can't produce a stack to test, and it'd tear down the current one in the process. The font fix alone won't reboot cleanly.
>
> So "test for real after reboot" actually *requires* bumping the pin to **150.0.7871.100** first — and conveniently, the reboot's image rebuild launches the real Xvfb/zygote chromium path, so **the reboot itself IS the for-real 150.0.7871.100 test.** If .100 SIGTRAPs, the rebooted browser feature will be dead and we'll see it immediately; if it works, fonts + browser all work.
>
> **Proposed "commit what we have":**
> 1. Bump chromium pin 147 → 150.0.7871.100 in both Dockerfiles + golden-update (same branch as the font fix).
> 2. Commit both (font fix is already committed; add the pin bump).
> 3. `/ck:export-chat-logs`.
> 4. Then you `/swe-swe:reboot` → that rebuild = the real .100 launch test.
>
> Two things to confirm:
> - **OK to bump the pin to .100?** It reverts the deliberate 147 safety pin, but there's no known-good pin left (147 + 149 both purged) and the reboot can't build without it.
> - **Commit to `main`, or keep on branch `fix/browser-emoji-cjk-fonts`?** Reboot builds from whatever's checked out. Your usual flow is straight-to-main; say which you want.

[Quick replies]
- Bump to .100, commit both to main, export logs
- Bump to .100 but keep on the branch
- Don't bump — just commit font fix + export logs (reboot may fail)
- Hold, let me think

**USER**

> Bump to .100, commit both to main, export logs

<small>took 1m 58s</small><br>
**AGENT**

> Committed both to main (ff): 00cc24f69 emoji+CJK fonts, 0a2a363f9 chromium 147→150.0.7871.100. Golden tests pass. Now running /ck:export-chat-logs.

