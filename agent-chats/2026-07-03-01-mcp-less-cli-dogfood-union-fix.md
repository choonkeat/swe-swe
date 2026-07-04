<!-- agent-chat export
title: Mcp Less Cli Dogfood Union Fix
date: 2026-07-03
index: 01
slug: mcp-less-cli-dogfood-union-fix
agent: claude
version: 0.8.6 (1470dec)
-->

# Mcp Less Cli Dogfood Union Fix

_2026-07-03 · 01 · claude · agent-chat 0.8.6 (1470dec)_

**USER**

> rebase this branch to local main

<small>took 27.1s</small><br>
**AGENT**

> Rebasing mcp-less onto local main. Note: main already contains a merge of mcp-less (commit c1fed201a), so the 4 branch commits will likely be dropped as already-applied. Running the rebase now.

<small>took 13.4s</small><br>
**AGENT**

> Rebased `mcp-less` onto local `main`. ✅
>
> As expected, all 4 branch commits were skipped as *already applied* — main already had them via the earlier merge (`c1fed201a Merge branch 'mcp-less' into main`). Git rewrote the SHAs during that merge, so they showed as distinct commits but were identical content.
>
> Result: `mcp-less` now points at the exact same commit as `main` (`54d78913`). No conflicts, no divergence — the branch is fully caught up. There are no unique commits left on the branch.

[Quick replies]
- Thanks
- Delete the branch

**USER**

> I want to test mcp and mcp-cli-proxy. here. I can `/mcp` disconnect `Agent View` then you execute the equivalent `mcp-cli-proxy` to boot it up, then we use `mcp` cli to make mcp browser visit example.com ?

<small>took 1m 23s</small><br>
**AGENT**

> Ready to test. Here's exactly what I'll run once you disconnect.
>
> **Setup verified:**
> - Binaries live on PATH: `mcp`, `mcp-cli-proxy`, `mcp-lazy-init` ✅
> - Env vars present: `SWE_SERVER_PORT=1977`, `SESSION_UUID`, `MCP_AUTH_KEY`, `BROWSER_CDP_PORT=6001` ✅
> - Socket dir created: `/workspace/.swe-swe/run/mcp` (the `mcp` client's default) ✅
> - Pulled the exact playwright launch spec from `.claude.json`.
>
> **The proxy command I'll launch (background daemon):**
> ```
> mcp-cli-proxy --name swe-swe-playwright \
>   --socket /workspace/.swe-swe/run/mcp/swe-swe-playwright.sock \
>   -- sh -c 'exec mcp-lazy-init --init-method POST \
>       --init-url http://localhost:$SWE_SERVER_PORT/api/session/$SESSION_UUID/browser/start?key=$MCP_AUTH_KEY \
>       -- npx -y @playwright/mcp@latest --cdp-endpoint http://localhost:$BROWSER_CDP_PORT'
> ```
>
> **Then the client calls (`mcp__X__Y` → `mcp X Y`):**
> ```
> mcp swe-swe-playwright browser_navigate --url https://example.com
> mcp swe-swe-playwright browser_snapshot
> ```
>
> **Your move first:** run `/mcp` and disconnect **Agent View** (`swe-swe-playwright`) so we're not two clients racing the same CDP browser. Reply once it's disconnected and I'll boot the proxy + drive it.

[Quick replies]
- Disconnected — go
- Wait, don't launch yet
- Test agent-chat instead

**USER**

> Disabled swe-swe-playwright
>
> <div style="display:flex;flex-wrap:wrap;gap:8px;">
> <a href="./assets/2026-07-03-01-1-a8de93fa406c.jpeg" style="flex:0 1 calc(33% - 8px);max-width:calc(33% - 8px);"><img src="./assets/2026-07-03-01-1-a8de93fa406c.jpeg" alt="IMG_0148.jpeg" style="width:100%;height:auto;display:block;border-radius:6px;"></a>
> </div>

<small>took 2m 27s</small><br>
**AGENT**

> 🎉 **MCP-less mode works end-to-end.** The browser visited example.com entirely through `mcp-cli-proxy` + the `mcp` CLI — zero MCP config on the agent side. Screenshot attached (captured via the CLI, image result written to a file, path printed).
>
> **What happened:**
>
> 1. **Proxy booted** — `mcp-cli-proxy --name swe-swe-playwright` exec'd the exact spec from `.claude.json` (`sh -c 'exec mcp-lazy-init ... -- npx @playwright/mcp ...'`), did the `initialize` handshake, and opened `swe-swe-playwright.sock`. Child chain logged with PIDs (proxy PID 1666 → mcp-lazy-init → playwright PID 1674), per the no-silent-Wait rule.
>
> 2. **`mcp -h`** — discovered the server purely by reading the socket dir. No config, no ports.
>
> 3. **`mcp swe-swe-playwright -h`** — live `tools/list`, all 23 browser tools.
>
> 4. **`mcp swe-swe-playwright browser_navigate --url https://example.com`** — the first call fired `mcp-lazy-init`'s browser/start POST, then navigated. Returned:
>    > Page URL: https://example.com/ · Page Title: **Example Domain**
>
> 5. **`browser_snapshot`** returned the a11y tree (heading "Example Domain", the paragraph, the "Learn more" → iana.org link).
>
> 6. **`browser_take_screenshot`** — the flag synthesis from `inputSchema` proved itself: it *rejected* the call until I supplied the required `--type` (enum png|jpeg) and `--scale` (enum css|device), and treated `--fullPage` as a bare boolean flag. Image came back as a file + printed path.
>
> Every layer of the design held: socket-dir-as-registry, `mcp__X__Y` → `mcp X Y` symmetry, schema-driven typed flags with enum validation, and image-result-to-file.
>
> The proxy is still running in the background. Want me to also test **agent-chat** (the blocking `send_message`/`check_messages` id-mux — the existential one), tear this down and re-enable the real `swe-swe-playwright`, or leave it running?
>
> <div style="display:flex;flex-wrap:wrap;gap:8px;">
> <a href="./assets/2026-07-03-01-2-edcf25a57670.png" style="flex:0 1 calc(33% - 8px);max-width:calc(33% - 8px);"><img src="./assets/2026-07-03-01-2-edcf25a57670.png" alt="swe-swe-playwright-browser_take_screenshot-2274618456.png" style="width:100%;height:auto;display:block;border-radius:6px;"></a>
> </div>

[Quick replies]
- Test agent-chat next
- Tear down + re-enable playwright
- Leave it running
- Test whiteboard too

<small>took 2m 26s</small><br>
**AGENT**

> 📡 agent-chat proxy is live (child PID 1937). This status came through `mcp swe-swe-agent-chat send_progress` — no MCP client involved.

<small>took 19.4s</small><br>
**AGENT**

> ✅ We're chatting entirely over the MCP-less path now — swe-swe-agent-chat is disabled in Claude Code, and this message came out of `mcp swe-swe-agent-chat send_message` → mcp-cli-proxy → unix socket → child stdio.
>
> This call is **blocking** right now, exactly as designed: your reply is RETURNED as this command's stdout.
>
> (Heads up — I hit a real CLI bug: array flags like `--more_quick_replies` / `--image_urls` are passed as raw strings and rejected by the server schema. So no quick-reply buttons on this one. Details after you reply.)
>
> Say something and I'll show you the round-trip.

[Quick replies]
- Hello over the socket!

**USER**

> I understand we found a bug? repro in test and tdd fix it
>
> <div style="display:flex;flex-wrap:wrap;gap:8px;">
> <a href="./assets/2026-07-03-01-3-ed2eb89fdf26.jpeg" style="flex:0 1 calc(33% - 8px);max-width:calc(33% - 8px);"><img src="./assets/2026-07-03-01-3-ed2eb89fdf26.jpeg" alt="IMG_0149.jpeg" style="width:100%;height:auto;display:block;border-radius:6px;"></a>
> </div>

<small>took 4m 55s</small><br>
**AGENT**

> probe

<small>took 9m 55s</small><br>
**AGENT**

> 🟢 Bug fixed, TDD, live-proven — and THIS message is the proof: it carries quick-reply buttons AND an inline image (`--more_quick_replies` + `--image_urls`), the exact array flags that errored before.
>
> **Root cause:** real MCP schemas type nullable arrays as a *union* — `"type": ["null","array"]` — not `"array"`. The client's `property.Type` was a plain Go `string`, so unmarshaling the JSON array into it silently failed → type became `""` → defaulted to `string` → the value was forwarded raw → server's schema rejected it.
>
> **Fix (`cmd/mcp/main.go`):** new union-aware `typeName` that unmarshals string-or-[]string, plus `property.kind()` that skips `"null"` to resolve the real type. Wired into coerce / bare-boolean detection / help rendering.
>
> **TDD:** 3 RED tests first (union coerces to array · union rejects raw string · help shows `array` not `string`) → fix → GREEN. Synced the bundled template copy (`make sync-mcp`); full `make test` passes.
>
> Attached: the example.com screenshot from the earlier browser test, now delivered via the array `--image_urls` flag. Reply and I'll wrap up.
>
> <div style="display:flex;flex-wrap:wrap;gap:8px;">
> <a href="./assets/2026-07-03-01-4-edcf25a57670.png" style="flex:0 1 calc(33% - 8px);max-width:calc(33% - 8px);"><img src="./assets/2026-07-03-01-4-edcf25a57670.png" alt="swe-swe-playwright-browser_take_screenshot-2274618456.png" style="width:100%;height:auto;display:block;border-radius:6px;"></a>
> </div>

[Quick replies]
- Nice — commit it
- Show me the diff
- Tear everything down

**USER**

> Nice — commit it

<small>took 32.5s</small><br>
**AGENT**

> ✅ Committed as `89c9850bb` on branch `mcp-less`:
> `fix(mcp): parse union JSON Schema types so nullable array flags coerce`
> — touches `cmd/mcp/main.go`, its test, and the synced template copy. `make test` green.
>
> **Session recap — MCP-less mode validated live:**
> - `mcp-cli-proxy` booted **playwright** → `mcp swe-swe-playwright browser_navigate` drove a real browser to example.com (navigate + snapshot + screenshot, schema-synthesized flags with enum validation).
> - `mcp-cli-proxy` booted **agent-chat** → this entire conversation since then has run over `mcp swe-swe-agent-chat send_message`/`send_progress`/`check_messages` — the blocking round-trip works, your replies come back as stdout.
> - Found + TDD-fixed a real bug (union `["null","array"]` types) surfaced *by* dogfooding the CLI.
>
> **Current state (heads up):** two proxies are still running in the background, and in Claude Code both `swe-swe-playwright` and `swe-swe-agent-chat` are still **disabled**. Also: I built the fixed `mcp` to a scratchpad path — `/usr/local/bin/mcp` is still the old binary (fine for non-array calls).
>
> What next?

[Quick replies]
- Tear down proxies + re-enable both MCPs
- Push the branch
- Leave everything running
- Install the fixed mcp binary too

**USER**

> /ck:export-chat-logs

<small>took 1m 31s</small><br>
**AGENT**

> Exporting chat log via `mcp swe-swe-agent-chat export_chat_md` (MCP tool is disabled, using the CLI proxy)...

