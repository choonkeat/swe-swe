<!-- agent-chat export
title: Mcp Less Preview Auth Fix
date: 2026-07-03
index: 02
slug: mcp-less-preview-auth-fix
agent: claude
version: 0.8.6 (1470dec)
-->

# Mcp Less Preview Auth Fix

_2026-07-03 · 02 · claude · agent-chat 0.8.6 (1470dec)_

**USER**

> how many mcp-cli-proxy is running? Can we test each one (this message should be testing the agent-chat one already?)

<small>took 3m 53s</small><br>
**AGENT**

> 5 mcp-cli-proxy processes are running — one per server. Test results:
>
> ✅ swe-swe-agent-chat (PID 626) — proven live by this very conversation
> ✅ swe-swe (PID 642) — list_sessions returned real data
> ✅ swe-swe-playwright (PID 627) — real browser_console_messages call worked (page: duckduckgo new tab, 0 msgs)
> ✅ swe-swe-whiteboard (PID 636) — tools/list handshake succeeded (didn't call draw/message since they block on viewer)
> ❌ swe-swe-preview (PID 628) — proxy is alive but hangs on MCP initialize (timed out at 60s AND 120s)
>
> The preview issue: the bridge (http://localhost:1977/proxy/<session>/preview/mcp) returns HTTP 302 instantly instead of an MCP streamable-HTTP response, and the preview app on port 3000 isn't listening (connection refused). So the proxy process is fine — it's the upstream preview target that's down/redirecting, which the reverse-proxy doesn't handle, causing the hang.
>
> Want me to dig into why the preview bridge 302s (and whether it should surface an error instead of hanging)?

[Quick replies]
- Yes, investigate the preview hang

**USER**

> Let me know what chat log for swe-swe session e870b81f-e5cf-450d-9f0d-58a96d659ccc Is about

<small>took 26.1s</small><br>
**AGENT**

> Session e870b81f is the parent session that BUILT this mcp-less mode — the same branch/worktree we're in now. Arc of the chat:
>
> 1. Rebased mcp-less onto main (all 4 commits already applied via an earlier merge).
>
> 2. Live-dogfooded the CLI path: /mcp-disconnected the native swe-swe-playwright, hand-launched an mcp-cli-proxy, and drove a real browser to example.com via 'mcp swe-swe-playwright browser_navigate' — proving socket-dir-as-registry + schema-synthesized flags.
>
> 3. Switched agent-chat itself to the proxy path — the whole rest of the convo ran through 'mcp swe-swe-agent-chat send_message' (blocking round-trip).
>
> 4. Found + TDD-fixed a real bug: nullable arrays come as union JSON Schema types (["null","array"]), which the CLI mis-parsed as string → array flags rejected. Committed 89c9850bb.
>
> 5. Exported + security-scrubbed the chat log (befbfab75).
>
> 6. Then the big design + build push (your steering): make --mcp-less the DEFAULT, have swe-swe-server launch the per-session mcp-cli-proxy fleet (5 proxies for chat sessions, 4 for terminal — agent-chat omitted), and gate EVERY agent's native MCP-config write behind SWE_MCP_LESS. Landed as commits 8dd097a5e, 7c94b8d69, 38991b6d9 — the exact 3 commits on top of the merge in THIS branch's log.
>
> 7. Ended mid-reboot: fast-forwarded main to the mcp-less code, hit a project-name-hash detour (md5 of the path string, not the flag), and brought the stack down so restart-loop2.sh would rebuild on the new default-mcp-less code.
>
> In short: that's the session where mcp-less went from a hand-run experiment to the shipped default — and the 5 proxies I just tested for you are the fleet it wired up. Phase D (the real e2e boot assertion) was the one step left unfinished.

[Quick replies]
- Finish Phase D e2e now

**USER**

> Ok i don’t think swe-swe-preview not working has anything to do with mcp less (because it was listed as failed in /mcp output since awhile ago). 
>
> Can we debug why it failed? Source code should be in /repos somewhere

<small>took 1m 49s</small><br>
**AGENT**

> Debugged it. swe-swe-preview hangs because its MCP endpoint is behind BROWSER-COOKIE auth that a headless proxy can never satisfy — a pre-existing gap, not an mcp-less regression.
>
> THE CHAIN:
> - 'mcp swe-swe-preview' → agent-reverse-proxy POSTs initialize to http://localhost:1977/proxy/<uuid>/preview/mcp
> - swe-swe-server's authMiddleware (auth.go:564) checks that path, finds no session cookie, and returns 302 → /swe-swe-auth/login. I reproduced the exact 302 with curl.
> - The reverse-proxy isn't a browser, has no cookie, and doesn't follow the redirect into a login page → it just hangs on the initialize handshake → our 60s/120s timeouts.
>
> WHY THE OTHER 4 WORK: their endpoints are explicitly EXEMPTED from cookie auth (auth.go:570-591):
> - /mcp → whole-path exempt (main swe-swe proxy)
> - /api/session/.../browser/start → exempt (playwright)
> - /api/autocomplete/... → exempt (agent-chat autocomplete)
> - /proxy/<uuid>/preview/__agent-reverse-proxy-debug__/open → exempt via per-session MCP key (sessionKeyMatchesPath)
>
> Notice the preview EXEMPTION only covers the .../open control endpoint — the actual .../preview/mcp MCP endpoint was never added to the exempt list. So it falls through to the cookie check. Two-part gap:
> 1. auth.go: /proxy/<uuid>/preview/mcp isn't MCP-key-exempted like its sibling /open endpoint.
> 2. mcp_less.go:75 (and templates.go:889): the preview bridge URL is the ONLY spec with no ?key=$MCP_AUTH_KEY appended — so even after exempting the path, no key is being sent.
>
> THE FIX (mirrors the existing browser-start / open-control pattern, TDD-able):
> - auth.go: add a proxyPreviewMCPPath(path) helper (suffix /preview/mcp) and exempt it when sessionKeyMatchesPath(r, uuid), exactly like the /open endpoint.
> - mcp_less.go + templates.go: append ?key=$MCP_AUTH_KEY to the preview bridge URL.
>
> This has been broken in native MCP mode too (the agent's preview MCP client also had no cookie) — matches your 'failed in /mcp since a while ago.'
>
> Want me to implement + TDD it (RED test: preview/mcp with valid key → 200/pass, without → 401; then wire the key into the spec + golden-update)?

[Quick replies]
- Yes — TDD the fix

**USER**

> Hmm. Wait, can we run a static web server at PORT env?

<small>took 1m 4s</small><br>
**AGENT**

> Yes — done, a static server is now live on PORT=3000 (python3 -m http.server, serving a probe index.html). Results:
>
> - Direct to localhost:3000 → HTTP 200 ✅ (PORT env respected, app serving)
> - Through the preview PROXY /proxy/<uuid>/preview/ → still 302 (that path is ALSO cookie-gated — but your BROWSER has the cookie, so the App Preview tab should now render 'swe-swe preview is live'. Try it: http://localhost:4000 → App Preview tab.)
> - Preview MCP endpoint /preview/mcp → still 302 (UNCHANGED)
>
> The key takeaway: running an app on PORT does NOT fix swe-swe-preview's MCP failure. They're two different halves:
> - Preview APP half (serve on :3000 → iframe): works in-browser now, because the browser carries the auth cookie.
> - Preview MCP-TOOL half (the 'mcp swe-swe-preview' CLI / agent client): still 302s, because the headless proxy has no cookie and /preview/mcp isn't key-exempted. The missing upstream was never the cause — even with :3000 up, the auth middleware rejects before the request ever reaches the proxy target.
>
> So the static server is a nice-to-have (gives the preview tools real content to snapshot ONCE auth is fixed), but the actual bug fix is still the two-part auth change: key-exempt /proxy/<uuid>/preview/mcp + append ?key=$MCP_AUTH_KEY to its bridge spec.
>
> Want me to (a) proceed with the TDD auth fix now, (b) leave the :3000 server up for you to eyeball the App Preview tab first, or (c) both — fix, then rebuild and verify the preview tool end-to-end against this static server?

[Quick replies]
- Both — fix then verify against :3000

**USER**

> Both — fix then verify against :3000

<small>took 3m 5s</small><br>
**AGENT**

> Fix implemented + TDD green (RED confirmed first). auth.go now key-exempts /proxy/<uuid>/preview/mcp via proxyPreviewMCPPath+sessionKeyMatchesPath; wired ?key=$MCP_AUTH_KEY into ALL preview specs (mcp_less fleet, claude, opencode, codex+env_vars, gemini, goose) and enabled the pi bridge's preview endpoint (a stale comment there had documented this exact auth blocker as 'out of scope'). Running make build + golden-update now.

<small>took 3m 4s</small><br>
**AGENT**

> Fix committed: a6063b91f. Full make test + golden-update green (238-ish golden files, only my 4 sources churned: auth.go, mcp_less.go, entrypoint.sh, mcp-bridge.ts — zero new/unrelated files).
>
> WHAT'S PROVEN NOW (handler-level, live in tests):
> - TestAuthMiddlewarePreviewMCPKeyAuth: /proxy/<uuid>/preview/mcp with a valid session key → 200 (reaches inner handler); no key → 401; cross-session key → 401.
> - TestProxyPreviewMCPPath: path parsing (rejects empty uuid, nested slashes, /preview/ without /mcp, trailing /extra).
> That IS the fix, exercised through the real authMiddleware + issueSessionKey.
>
> WHAT'S NOT YET PROVEN: the live end-to-end 'mcp swe-swe-preview browser... against :3000'. The catch is the SAME one from your last reboot session — the running swe-swe-server is the OLD binary (built before this commit). The auth exemption only takes effect once the new binary is live. To truly drive the preview tool against the :3000 static server, main needs this commit and the stack must rebuild.
>
> So 'verify against :3000' = a reboot, which drops this chat (returns on new code, server auto-launches the fleet, I re-test all 5 incl preview). Options:
>
> 1. GO: I ff main to a6063b91f + reboot (established path from last session), then verify preview→:3000 end-to-end. Chat goes dark, comes back on fixed code.
> 2. HOLD: leave it committed on mcp-less; you reboot at your convenience and I verify after.
> 3. Standalone: I build+run the fixed swe-swe-server on a spare port with a throwaway session and curl /preview/mcp?key=... to show 200-not-302 — lighter, no chat drop, but not the full fleet path.
>
> I've left the :3000 static server running either way. Which?

[Quick replies]
- GO: ff main + reboot + verify

**USER**

> /ck:export-chat-logs

<small>took 5.9s</small><br>
**AGENT**

> Exporting chat log via mcp swe-swe-agent-chat export_chat_md...

