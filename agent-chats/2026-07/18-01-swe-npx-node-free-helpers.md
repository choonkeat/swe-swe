<!-- agent-chat export
title: Swe Npx Node Free Helpers
date: 2026-07-18
index: 01
slug: swe-npx-node-free-helpers
agent: claude
version: 0.8.13 (97b5d14)
-->

# Swe Npx Node Free Helpers

_2026-07-18 · 01 · claude · agent-chat 0.8.13 (97b5d14)_

**AGENT**

> Starting task: swe-npx node-free helpers (tasks/2026-07-18-swe-npx-node-free-helpers.md). Phase 1 first: TDD the swe-npx helper binary against a fake registry. Reading the mcp-lazy-init layout and Makefile now to mirror them exactly.

<small>took 6m 43s</small><br>
**AGENT**

> Phase 1/5 done (commit 4be428171): swe-npx helper binary, TDD.
>
> What: new `cmd/swe-swe/templates/host/swe-npx/` (main.go + main_test.go + go.mod.txt) + `make test-swe-npx` wired into `make test`. RED first (9 failing tests on stubs), then GREEN: all 13 tests pass, full `make test` green, standalone CGO_ENABLED=0 build produces a static stripped ELF.
>
> Salient code (the exec contract):
> ```go
> // resolve maps pkg@version to a cached executable path, downloading on miss
> binPath, err := resolve(opts, pkg, version)
> ...
> return execFn(binPath, append([]string{binPath}, rest...), os.Environ())
> ```
> Covers: platform-pkg derivation (@choonkeat/md-serve -> -linux-x64), latest memoized with 15m TTL (zero network within TTL), sha512 integrity (corrupt -> fatal, nothing cached), offline fallback to newest cached version, 404 -> "use real npx", concurrent-rename race keeps the winner.
>
> git stat since last report:
>  Makefile                                          | +13
>  cmd/swe-swe/templates/host/swe-npx/go.mod.txt     | +3
>  cmd/swe-swe/templates/host/swe-npx/main.go        | +429
>  cmd/swe-swe/templates/host/swe-npx/main_test.go   | +506
>  tasks/... (plan + phase1 log)
>
> Next: Phase 2 -- payload + Dockerfile wiring (8 -> 9 embedded binaries), RED test first.

<small>took 4m 8s</small><br>
**AGENT**

> Phase 2/5 done (commit decdfdac2): payload + Dockerfile wiring, 8 -> 9 embedded binaries.
>
> RED first: added "swe-npx" to dockerlessBinaries -> TestDockerlessPayloadEmbedsBinaries/swe-npx and TestExtractDockerlessBinaries failed (embed missing). GREEN: Makefile `_payload-helper NAME=swe-npx` + Dockerfile build stanza + COPY into /usr/local/bin. Payload dir now shows the 6MB static swe-npx ELF alongside the other 8.
>
> Salient diff (Dockerfile template):
> ```dockerfile
> +COPY swe-npx/main.go swe-npx/go.mod /build-swe-npx/
> +RUN cd /build-swe-npx && CGO_ENABLED=0 GOOS=linux go build ... -o /build-swe-npx/swe-npx .
> ...
> +COPY --from=server-builder /build-swe-npx/swe-npx /usr/local/bin/swe-npx
> ```
> Golden diff reviewed: 141 files, ONLY the stanza/COPY/comment + the emitted swe-npx/{main.go,go.mod} per init variant. No playwright lines, no strays.
>
> git stat since last report: Makefile +1, payload.go +5, templates/host/Dockerfile +6/-1, goldens +23735/-47 (auto-generated), phase2 log.
>
> Next: Phase 3 -- swap the five npx call sites to swe-npx (goldens will change again).

<small>took 6m 35s</small><br>
**AGENT**

> Phase 3/5 done (commit ea1a1bbd6): all @choonkeat npx call sites now use swe-npx.
>
> Swapped: md-serve Files-tab spawn (main.go), mcp-less proxy fleet x4 (mcp_less.go), dockerless .mcp.json x4 (dockerless.go), entrypoint.sh per-agent configs (claude JSON, gemini JSON, codex TOML, goose YAML -- including the whiteboard array/TOML/YAML `command: npx` forms), claude_mcp_setup (templates.go), pi mcp-bridge x2 (mcp-bridge.ts).
>
> Salient diff:
> ```go
> -	cmd := exec.Command("npx", "-y", "@choonkeat/md-serve@latest",
> +	cmd := exec.Command("swe-npx", "-y", "@choonkeat/md-serve@latest",
> ```
> Verified: filtered grep shows 0 remaining plain-npx @choonkeat spawns; @playwright/mcp count unchanged (12), zero playwright lines in the 308-file golden diff (all npx->swe-npx rewrites + comment updates only). One pre-existing test asserted whiteboard uses plain npx -- that is exactly the behavior this task changes, so its assertion moved to swe-npx. Full `make test` exit 0.
>
> Note logged: the task's literal verify grep ("returns nothing") is impossible since "swe-npx -y @choonkeat" contains the substring; verified the intent (no PLAIN npx spawns) instead.
>
> git stat: 13 source/template files + 308 golden files (+1815/-1768, auto-generated).
>
> Next: Phase 4 -- e2e verification (node-free dockerless smoke, docker-mode test container, collision repro, latency numbers).

<small>took 9m 33s</small><br>
**AGENT**

> Phase 4 part 1 done (commit 1de9981f0): the headline node-free claim is PROVEN.
>
> Ran the dockerless e2e with node/npx/npm masked out of the server's PATH (exit-127 shims, new E2E_POISON_NODE=1 mode in scripts/e2e-dockerless.sh):
> - all 9 payload binaries dumped (incl. swe-npx)
> - Playwright drove a REAL session (opencode -- it's a native ELF, needs no node) and all 4 live-tab tests passed, including "Files tab: md-serve answers on filesProxyPort" -- md-serve was fetched from the npm registry and exec'd by swe-npx with node dead
> - the .mcp.json agent-chat spawn shape (`sh -c 'exec swe-npx -y @choonkeat/agent-chat ...'`) runs node-free
> - poison control: plain `npx` really does fail on that PATH
> - unpoisoned control run also fully green
>
> Latency (item 4, real registry): swe-npx warm spawn 11ms vs npx ~1.15s (about 100x); cold download+verify+exec 1.43s.
>
> Fixed en route (script bugs, logged): env -u ordering, md-serve-only-spawns-on-real-session ordering, teardown SIGKILL false positive.
>
> Next: Phase 4 part 2 -- boot the docker-mode test container, browser-check Agent Chat/Files/whiteboard + the npx-cache dir, and the cwd-collision repro. This involves a full container image build, so it will take a while; I'll report as it progresses.

<small>took 7m 31s</small><br>
**AGENT**

> Phase 4/5 COMPLETE (commits 1de9981f0 + e5e326351). All four verification items green:
>
> 1. Node-free dockerless: poisoned-PATH e2e fully PASS (reported earlier).
> 2. Docker mode: built the image from the new templates, booted e2e-simple at host.docker.internal:9780, drove a real opencode chat session in the browser. Screenshots attached: Agent Chat connected (v0.8.13), Files tab rendering md-serve's index. In-container `ps` is the money shot -- all five spawns run DIRECTLY from the cache, no npx wrapper PIDs:
>    `/home/app/.swe-swe/npx-cache/@choonkeat/md-serve-linux-x64@0.6.2/bin/md-serve ...`
>    (same for agent-chat, whiteboard, 2x reverse-proxy). Cache lives at the container user's ~/.swe-swe/npx-cache as designed.
> 3. Collision repro FIXED: from a cwd that is an agent-chat repo checkout, plain `npx -y @choonkeat/agent-chat` fails with "agent-chat: not found" (the old cwd-shadowing bug, reproduced live); `swe-npx` from the same cwd execs the registry binary. A live session with pwd=/workspace/agent-chat-repro shows 5 MCP + Agent Chat connected (third screenshot), agent-chat PID confirmed cwd=repro dir.
> 4. Latency: warm 11ms vs npx 1.15s; cold 1.43s.
>
> Test container torn down (0 e2e containers left). One workaround noted in the log: the New Session dialog start buttons stalled (a KNOWN pre-existing issue from the e2e-llm capstone work, unrelated to this task) -- I used the POST /api/session/new flow that the e2e helpers themselves use.
>
> Next: Phase 5 -- docs + changelog (docs/dockerless.md dependencies, follow-up notes, CHANGELOG entry).
>
> <div style="display:flex;flex-wrap:wrap;gap:8px;">
> <a href="./assets/2026-07-18-01-1-2f44764f88b5.png" style="flex:0 1 calc(33% - 8px);max-width:calc(33% - 8px);"><img src="./assets/2026-07-18-01-1-2f44764f88b5.png" alt="session-agent-chat.png" style="width:100%;height:auto;display:block;border-radius:6px;"></a>
> <a href="./assets/2026-07-18-01-2-13fd889e0725.png" style="flex:0 1 calc(33% - 8px);max-width:calc(33% - 8px);"><img src="./assets/2026-07-18-01-2-13fd889e0725.png" alt="session-files-tab.png" style="width:100%;height:auto;display:block;border-radius:6px;"></a>
> <a href="./assets/2026-07-18-01-3-d51855631cee.png" style="flex:0 1 calc(33% - 8px);max-width:calc(33% - 8px);"><img src="./assets/2026-07-18-01-3-d51855631cee.png" alt="session-collision-repro.png" style="width:100%;height:auto;display:block;border-radius:6px;"></a>
> </div>

<small>took 7m 59s</small><br>
**AGENT**

> All 5 phases are done. As per the protocol, let me walk you through each phase log, one at a time.
>
> **Phase 1 log** (tasks/...-phase1.log) -- the swe-npx helper binary, TDD:
>
> What was done:
> - RED: wrote main_test.go first (fake httptest registry serving canned packuments/version-docs/tarballs built in-test) + stub main.go; confirmed 9 tests failed on assertions (not compile errors).
> - GREEN: implemented main.go (~430 lines, stdlib only): platform-package derivation (@choonkeat/md-serve + linux/amd64 -> @choonkeat/md-serve-linux-x64), -y swallowing + version split, dist-tags latest with a 15m TTL memo file, sha512 integrity verification, tar.gz package/-tree extraction with path-traversal guard, atomic rename with lose-the-race-gracefully semantics, offline fallback to newest cached version, 404 -> "use real npx" error, and syscall.Exec behind a stubbed var.
> - All 13 tests pass; `make test-swe-npx` wired into the `make test` chain (mirroring the git-sign-swe-swe temp-module pattern); standalone CGO_ENABLED=0 build produces a static stripped ELF; full `make test` green.
>
> Problems encountered: none in this phase -- tests passed on first full implementation run.
>
> Conclusion: swe-npx exists as a tested, self-contained template helper (commit 4be428171).
>
> OK to proceed to the Phase 2 log?

[Quick replies]
- OK, next
- Show me the full phase 1 log
- Questions first

**USER**

> Yes but could we build the binary and switch our current mcp json to use the cli we just built? Then when I start a new session (in this worktree?) it will kick in?

<small>took 33.9s</small><br>
**AGENT**

> On it: building swe-npx from the new template and switching this container's user-scope MCP config (~/.claude.json) to use it. /usr/local/bin is root-owned, but ~/.swe-swe/bin is on every session's PATH -- installing there. Will report with the exact diff of the config change.

<small>took 57.8s</small><br>
**AGENT**

> Done -- swe-npx is live in this container for new sessions.
>
> What I did:
> 1. Built swe-npx from the new template (static, CGO_ENABLED=0) and installed it to `~/.swe-swe/bin/swe-npx` (/usr/local/bin is root-owned; ~/.swe-swe/bin is already on every session's PATH).
> 2. Rewrote `~/.claude.json` (user scope, backup at `~/.claude.json.bak-swe-npx-*`): agent-chat / preview / swe-swe now `exec swe-npx -y @choonkeat/...`, whiteboard's command is `swe-npx`; playwright untouched on npx.
> 3. Pre-warmed the cache: all four tools resolved + cached under `~/.swe-swe/npx-cache/@choonkeat/` (agent-chat 0.8.13, reverse-proxy 0.2.10, whiteboard 0.3.2, md-serve 0.6.2), so first spawn is ~11ms.
>
> So yes: any NEW claude session you start (this worktree or any repo -- it's user-scope config) spawns its MCP servers via swe-npx. This session keeps its already-running servers.
>
> Two caveats:
> - The SERVER-side spawns (Files-tab md-serve, mcp-less fleet) still come from the running swe-swe-server binary, which predates this change -- those stay on npx until the next image rebuild + reboot.
> - On reboot, entrypoint.sh re-runs claude_mcp_setup and rewrites ~/.claude.json: with the OLD image that reverts this switch; once rebuilt from this branch it writes swe-npx anyway. So this is a dogfood preview of exactly what the next reboot ships.
>
> ---
>
> Resuming the wrap-up (you OK'd Phase 1). **Phase 2 log** -- payload + Dockerfile wiring:
>
> What was done: RED by adding "swe-npx" to dockerlessBinaries -- TestDockerlessPayloadEmbedsBinaries/swe-npx and TestExtractDockerlessBinaries failed on the missing 9th binary; GREEN via Makefile `_payload-helper NAME=swe-npx` + Dockerfile build stanza + COPY to /usr/local/bin. `make build golden-update`; golden diff reviewed: only the stanza/COPY/comment plus the emitted swe-npx template files across 141 files.
>
> Problems encountered: none.
>
> Conclusion: 9 embedded binaries, docker + dockerless both carry swe-npx (commit decdfdac2).
>
> OK to proceed to the Phase 3 log?

[Quick replies]
- OK, next
- Show me the full phase 2 log
- Questions first

**USER**

> OK, next

<small>took 11.9s</small><br>
**AGENT**

> **Phase 3 log** -- swap the call sites:
>
> What was done: every `@choonkeat` spawn moved from npx to swe-npx across all forms -- the sh-wrapped strings (entrypoint.sh claude/gemini blocks, mcp_less.go x4, dockerless.go, templates.go claude_mcp_setup, main.go md-serve spawn) AND the structured forms the task table didn't list (whiteboard's `"command": "npx"` JSON, codex TOML `command = "npx"`, goose YAML `cmd: npx`, pi mcp-bridge.ts SpawnedHttpService x2). Comments updated too: md-serve process-group comments now explain the captured PID IS md-serve (swe-npx execs), pgroup kept as defense; the mcp-cli-proxy usage example was synced in both its canonical and bundled copies.
>
> Problems encountered, both logged:
> 1. The task's literal verify ("grep 'npx -y @choonkeat' returns nothing") is unsatisfiable -- "swe-npx -y @choonkeat" contains that substring. I verified the intent instead: a filtered grep for PLAIN npx spawns returns 0, and @playwright occurrences are unchanged (12) with zero playwright lines in the 308-file golden diff.
> 2. `make test` caught a pre-existing test asserting whiteboard uses plain npx (TestWriteDockerlessMCPConfig) -- that assertion IS the behavior this task changes, so it moved to expect swe-npx. Full `make test` then exit 0.
>
> Conclusion: no plain-npx @choonkeat spawns remain anywhere in templates or CLI code; playwright untouched (commit ea1a1bbd6).
>
> OK to proceed to the Phase 4 log?

[Quick replies]
- OK, next
- Show me the full phase 3 log
- Questions first

**USER**

> OK, next

<small>took 15.1s</small><br>
**AGENT**

> **Phase 4 log** -- e2e verification (the long one):
>
> What was done:
> 1. Node-free dockerless (headline): extended scripts/e2e-dockerless.sh -- swe-npx + swe-run added to the payload asserts, new `E2E_POISON_NODE=1` mode masks node/npx/npm from the SERVER's PATH with exit-127 shims. Poisoned run: FULL PASS -- Playwright drove a real opencode session (opencode is a native ELF, no node needed) and "Files tab: md-serve answers on filesProxyPort" passed with node dead; agent-chat's .mcp.json spawn shape runs node-free; a poison-control assert proves npx really was masked. Unpoisoned control run also PASS.
> 2. Docker mode: built the image from the new templates, booted e2e-simple, drove sessions in the MCP browser. All five spawns run directly from ~/.swe-swe/npx-cache (ps shows the cache paths as argv[0] -- no npx wrapper PIDs). Agent Chat + Files verified visually; cache in the container user's home. Torn down cleanly.
> 3. Collision repro: from cwd = agent-chat checkout, plain npx fails ("agent-chat: not found" -- the old bug, reproduced live); swe-npx from the same cwd execs the registry binary; a live session with that pwd loaded the agent-chat MCP (5 MCP, Connected v0.8.13).
> 4. Latency: warm 11ms vs npx 1.15s; cold 1.43s.
>
> Problems encountered (all logged, none compromising the claims):
> - env(1) flag-ordering bug in my script edit (PATH= before -u) -- fixed.
> - md-serve only spawns on REAL session creation (no-ghost-session invariant), so my first hard assert fired before any session existed -- moved post-Playwright.
> - Session teardown SIGKILLs md-serve ("signal: killed"), which my grep first misread as a failure -- assert now ignores the teardown kill.
> - The mcp-less fleet binaries (mcp, mcp-cli-proxy) are not in the dockerless payload, so "agent-chat proxy port answers" was reinterpreted for dockerless as the .mcp.json spawn working node-free; the fleet path was covered by the docker-mode test instead.
> - The New Session dialog start-buttons stalled -- a KNOWN pre-existing issue (e2e-llm capstone notes), unrelated to this task; used the POST /api/session/new flow the e2e helpers themselves use.
>
> Conclusion: all four verification items green (commits 1de9981f0, e5e326351).
>
> OK to proceed to the Phase 5 log (last one)?

[Quick replies]
- OK, next
- Show me the full phase 4 log
- Questions first

**USER**

> OK, next

<small>took 10.4s</small><br>
**AGENT**

> **Phase 5 log** -- docs + changelog:
>
> What was done:
> - docs/dockerless.md Dependencies: node/npx demoted from required to optional ("only for Agent View (@playwright/mcp is real JS) and node-based agent CLIs"), plus a paragraph explaining swe-npx and the user-level ~/.swe-swe/npx-cache (one download serves all sessions/projects; resolution never consults the project cwd).
> - tasks/2026-06-27-dockerless-single-binary.md: follow-up note atop Status pointing at this task.
> - CHANGELOG: Unreleased feature entry covering the helper, the call-site swap, the 5-of-6-tabs-node-free claim, the collision fix, latency, and env overrides.
>
> Problem encountered: the task told me to mark portions of tasks/2026-07-11-mcp-npx-cwd-collision.md superseded -- that file does not exist anywhere in this repo's history (checked `git log --all`), so there was nothing to annotate; recorded that fact in the log instead.
>
> `make test` exit 0 after the doc changes (ascii-check included). Conclusion: docs and changelog land with the feature (commit cc9934b23).
>
> That's all five phase logs. Remaining wrap-up once you confirm: export this chat to ./agent-chats/ and commit it, then give you the final git status. OK?

[Quick replies]
- OK, wrap up
- Show me the full phase 5 log
- Questions first

**USER**

> OK, wrap up

