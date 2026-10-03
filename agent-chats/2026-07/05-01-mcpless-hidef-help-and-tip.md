<!-- agent-chat export
title: Mcpless Hidef Help And Tip
date: 2026-07-05
index: 01
slug: mcpless-hidef-help-and-tip
agent: claude
version: dev (unknown)
-->

# Mcpless Hidef Help And Tip

_2026-07-05 · 01 · claude · agent-chat dev (unknown)_

**USER**

> The past swe-swe recordings have been using mcp less mode. I am finding agent’s quick replies are fewer, curt, and less helpful. Is there some information missing when using mcp over mcp-cli-proxy vs regular mcp?

<small>took 30.2s</small><br>
**AGENT**

> Let me dig into how the mcp CLI proxy surfaces tool schemas vs regular MCP - checking whether the quick-reply parameter descriptions are getting lost.

<small>took 2m 12s</small><br>
**AGENT**

> Yes — real information is missing, and it explains both symptoms exactly.
>
> **In regular MCP mode**, the full tool definition of send_message is injected into the agent's context on every single turn. That description contains the quick-reply guidance: that first_quick_reply is the primary suggested reply (with example "Yes, proceed"), and that **more_quick_replies** is an array of additional options (example ["Wait", "Cancel"]).
>
> **In MCP-less mode**, none of that reaches the agent unless it voluntarily runs `mcp swe-swe-agent-chat send_message -h` — which it almost never does, because the global CLAUDE.md already gives it a working recipe. And that recipe is the whole problem. It says only:
>
>     mcp swe-swe-agent-chat send_message --text "..." --first_quick_reply "..."
>
> So the agent's only always-present instruction mentions exactly ONE quick reply and zero guidance on phrasing. It doesn't even know more_quick_replies (or image_urls) exists. Hence: fewer (usually one), curt (no examples of good phrasing).
>
> I checked the other channels too — the reply-instructions block appended to every check_messages/send_message result (agent-reply.tmpl) says nothing about quick replies, so it doesn't compensate. Minor extra gap: the mcp CLI truncates per-flag descriptions to their first line in -h output (cmd/mcp/main.go:437), though for agent-chat specifically the flags have no descriptions so this doesn't bite here.
>
> Two fixes, not mutually exclusive:
> 1. **CLAUDE.md snippet**: extend the recipe to include --more_quick_replies '["...","..."]' plus one line of guidance (offer 2-4 replies phrased as the user's likely next messages).
> 2. **agent-reply.tmpl** (mode-neutral, fixes every agent every turn): add the quick-reply guidance to the reply-instructions block that's already appended to every check_messages result. Redundant-but-harmless in native MCP mode.
>
> I'd do both — #2 is the durable one. Want me to?

[Quick replies]
- Yes, do both
- Just the template fix (#2)
- Just the CLAUDE.md fix (#1)
- Show me the exact diffs first

**USER**

> Can we fix mcp -h to be more hi-def and real full info of what would’ve been injected? Then change the Claude recipe to use mcp cli to be mcp -h instead? Don’t scatter our own summaries? Discuss

<small>took 1m 37s</small><br>
**AGENT**

> I like it — render the server's own docs verbatim and stop maintaining parallel summaries. Here's what it takes and the one caveat.
>
> **What 'hi-def -h' needs (current gaps in cmd/mcp/main.go):**
> 1. **Flag descriptions are truncated to their first line** (main.go:437, firstLine()). Print them in full, indented. This is the only outright lie in today's -h; for agent-chat it happens not to bite because its params have no descriptions — all guidance is in the tool description, which -h already prints in full.
> 2. **Schema fidelity**: today -h renders only type / enum / required. Native clients get the whole inputSchema JSON. I'd render description+type+enum+default+array-item-type properly, and for anything exotic (nested objects, oneOf) fall back to printing the raw schema JSON so nothing is ever silently dropped. That makes the 'real full info' claim honest by construction.
> 3. **mcp <server> with no tool**: keep the one-line index, but add `mcp <server> --full` that dumps every tool's full description + schema — that's literally 'what native MCP would have injected', in one command.
> 4. **Server instructions field**: native clients inject the MCP initialize instructions into the system prompt. mcp-cli-proxy performs initialize itself and discards the result. Small proxy change: cache it, expose it, and `mcp <server>` prints it as a header. agent-chat doesn't set instructions today, but the plumbing makes the guarantee complete.
>
> **The recipe change**: CLAUDE.md keeps ONLY the cross-tool workflow (start turn with check_messages; send_message blocks and ends the turn; send_progress for status) plus one pointer: 'Before first use — and again after context compaction — run `mcp swe-swe-agent-chat send_message -h` and follow it exactly.' Zero parameter details in CLAUDE.md, so nothing to drift.
>
> **The honest caveat**: native MCP injects tool docs into EVERY turn; -h is pull-based and evaporates on compaction. Content parity yes, presence parity no. Wrong-flag mistakes self-correct (the CLI errors name the flag), but 'only ever offers one quick reply' is not an error — nothing forces rediscovery of more_quick_replies. If that residual bothers us, the non-scattering mitigation is a one-line POINTER (not summary) in the check_messages return: 'full send_message usage: mcp swe-swe-agent-chat send_message -h'. Pointers don't drift.
>
> Scope if approved: mcp CLI (items 1-3), proxy+CLI (item 4), CLAUDE.md template in cmd/swe-swe/templates + golden-update. Optionally the check_messages pointer in the agent-chat repo.

[Quick replies]
- Approved, full scope + the pointer
- Approved, but skip item 4 (instructions plumbing)
- Just items 1-2 + recipe change
- What would the new -h output look like?

**USER**

> What would the new -h output look like?

<small>took 1m 23s</small><br>
**AGENT**

> Here's the mockup, using a real tool where today's -h actually drops things. I pulled the raw schema off the socket to compare.
>
> **Today** (`mcp swe-swe-playwright browser_take_screenshot -h`):
>
>     Take a screenshot of the current page. You can't perform actions based on the screenshot...
>
>     Flags:
>       --scale string (required)
>           Image resolution scale. "css" produces a screenshot sized in CSS pixels...
>           one of: [css device]
>       --type string (required)
>           Image format for the screenshot. Default is png.
>           one of: [png jpeg]
>       ...
>
> What the raw schema has that this silently drops: `annotations` (title "Take a screenshot", readOnlyHint=true), `default: "png"` / `default: "css"` (only visible today because the description happens to repeat them in prose), and any multi-line flag description gets cut at line 1.
>
> **Proposed** (same command):
>
>     mcp swe-swe-playwright browser_take_screenshot [flags]
>     Take a screenshot  [read-only]
>
>     Take a screenshot of the current page. You can't perform actions based on
>     the screenshot, use browser_snapshot for actions.
>
>     Flags:
>       --scale string (required, default: css)
>           Image resolution scale. "css" produces a screenshot sized in CSS pixels
>           (smaller, consistent across devices). "device" produces a high-resolution
>           screenshot using device pixels (larger, accounts for the device pixel ratio).
>           one of: css, device
>       --type string (required, default: png)
>           Image format for the screenshot.
>           one of: png, jpeg
>       --fullPage boolean
>           When true, takes a screenshot of the full scrollable page, instead of the
>           currently visible viewport. Cannot be used with element screenshots.
>       ...
>
> Rules: title + hints from `annotations` on line 2; `default:` from the schema, not from prose; full multi-line descriptions, wrapped + indented; required flags listed first; anything the renderer doesn't understand (nested objects, oneOf) prints as a raw JSON block under the flag instead of vanishing.
>
> **Server view** (`mcp swe-swe-agent-chat`):
>
>     swe-swe-agent-chat
>
>     <server 'instructions' from MCP initialize, verbatim — new, needs proxy passthrough>
>
>     tools:
>       check_messages  Drain pending user messages...   (unchanged one-line index)
>       ...
>
>     Full docs:  mcp swe-swe-agent-chat <tool> -h
>     Everything: mcp swe-swe-agent-chat --full   <- all tools, full descriptions + flags;
>                                                    byte-for-byte what native MCP injects
>
> **And the CLAUDE.md recipe shrinks to workflow + pointer:**
>
>     - Start each turn with `mcp swe-swe-agent-chat check_messages`.
>     - ALL user-visible output goes through swe-swe-agent-chat. Before your first
>       send_message — and again after context compaction — run:
>           mcp swe-swe-agent-chat send_message -h
>       and follow it exactly. send_message BLOCKS until the user replies; never
>       background it; end every turn on it.
>     - Non-blocking status: send_progress.
>
> Note for agent-chat specifically: its send_message -h is ALREADY full-fidelity today (all guidance lives in the tool description; its flags have no schema descriptions). So for the curt-quick-replies symptom, the recipe change does the heavy lifting; the -h fixes make the guarantee hold for every other server (playwright, whiteboard, ...).

[Quick replies]
- Ship it — full scope
- Ship it, but keep flags alphabetical
- Drop the [read-only] annotation bit
- Also strengthen quick-reply guidance in the tool description

**USER**

> Btw this quick replies was good. So the problem is just losing context later on is it?
>
> <div style="display:flex;flex-wrap:wrap;gap:8px;">
> <a href="./assets/2026-07-05-01-1-8c3e80514ec8.png" style="flex:0 1 calc(33% - 8px);max-width:calc(33% - 8px);"><img src="./assets/2026-07-05-01-1-8c3e80514ec8.png" alt="IMG_2876.png" style="width:100%;height:auto;display:block;border-radius:6px;"></a>
> </div>

<small>took 39.0s</small><br>
**AGENT**

> Almost — it's two problems, and this session accidentally dodged both:
>
> 1. **Never acquired**: a normal MCP-less session starts with only the CLAUDE.md recipe, which shows one --first_quick_reply and no phrasing guidance. Nothing ever triggers the agent to run -h, so it never learns more_quick_replies exists. That's the steady state of your past recordings — it's not decay, the info was never in context at all.
>
> 2. **Lost later**: even an agent that did read the full docs loses them at context compaction. Native MCP re-injects tool definitions on every turn, so it can't forget; -h knowledge evaporates with the summary.
>
> My replies were good this time only because the task itself was investigating send_message — I read the tool description as evidence. That's the exception that proves rule 1.
>
> The plan covers both: the recipe's "run send_message -h before first use" fixes acquisition; "re-run after compaction" plus (optionally) the one-line pointer in every check_messages return fixes decay. The pointer is the stronger fix for #2 since compaction is invisible to the agent — it can't know it forgot.

[Quick replies]
- Makes sense — proceed with full scope + pointer
- Proceed, skip the instructions plumbing (item 4)
- Hold on, let me think

**USER**

> Proceed, skip the instructions plumbing (item 4)

<small>took 40.2s</small><br>
**AGENT**

> Proceeding: (1) hi-def -h in cmd/mcp (full descriptions, defaults, annotations, required-first, raw-JSON fallback), (2) mcp <server> --full dump, (3) CLAUDE.md recipe -> pointer, (4) check_messages pointer in agent-chat. Will report back with diffs + test results.

<small>took 10m 34s</small><br>
**AGENT**

> Implementation done, verifying: mcp CLI hi-def -h + --full shipped (all unit tests green, live smoke test against playwright matches the mockup), CLAUDE.md recipe rewritten in the entrypoint template + goldens regenerated (only entrypoint.sh and mcp/main.go changed across variants, full swe-swe suite green), and the SWE_MCP_LESS-gated -h pointer added to agent-chat's reply instructions. Now running agent-chat's full unit+e2e suite.

<small>took 31m 32s</small><br>
**AGENT**

> Done, everything green. Four changes across two repos:
>
> **swe-swe repo** (uncommitted):
> 1. **cmd/mcp: hi-def -h** — full multi-line flag descriptions (was: first line only), `default:` pulled from the schema, `[read-only]`/`[destructive]` + title from annotations, required flags listed first, `array of string` item types, and a raw-JSON `schema: {...}` fallback for any keyword the renderer doesn't model — nothing can be silently dropped anymore. Verified live against playwright: output matches the mockup you approved. The fallback even caught a real case: draw's `instructions` flag shows `schema: {"items":true}`.
> 2. **mcp <server> --full** — dumps every tool's full docs in one command; the literal "what native MCP would have injected" view. Live smoke on agent-chat: all 7 tools, 102 lines.
> 3. **CLAUDE.md recipe** (entrypoint.sh template) — now workflow + pointer only: "Before your first send_message — and again after any context compaction — run `mcp swe-swe-agent-chat send_message -h` and follow it exactly. Never guess flags." No parameter details left to drift. Goldens regenerated: only entrypoint.sh (39 variants) and mcp/main.go (47 variants) changed. Full `make test` green.
> 4. **agent-chat repo** (uncommitted): reply-instructions now append, only when SWE_MCP_LESS is set, one pointer line: "Full send_message usage (multiple quick replies, inline images): run `mcp swe-swe-agent-chat send_message -h` ... does not survive compaction." Rides on every check_messages/send_message return, so it heals the forget-after-compaction case. Unit + all 62 e2e tests green.
>
> **Rollout note**: the live container keeps the old binary + old CLAUDE.md until the next image rebuild + reboot (binary is root-owned; and the new CLAUDE.md mentions --full which the old binary lacks, so updating it early would desync). Everything lands together on the next reboot cycle.
>
> Want me to commit? (swe-swe: 2 commits — mcp CLI, then recipe+goldens; agent-chat: 1 commit)

[Quick replies]
- Commit both repos
- Show me the diffs first
- Commit swe-swe only, hold agent-chat
- Also /reboot after committing

**USER**

> Commit both repos

<small>took 1m 26s</small><br>
**AGENT**

> Committed, both trees clean.
>
> **swe-swe** (main):
> - `b6e3ef372` feat(mcp): full-fidelity tool help + --full server dump (50 files: cmd/mcp + synced template bundle + 47 golden variants)
> - `76f85d150` feat(entrypoint): MCP-less recipe points at -h instead of summarizing (40 files: template + 39 golden variants)
>
> **agent-chat** (main):
> - `49a7396` feat(prompts): MCP-less pointer to send_message -h in reply instructions (gated on SWE_MCP_LESS; unit + 62 e2e green)
>
> Also reverted two bits of collateral churn that snuck into the working trees during testing: ascii-fix had rewritten agent-chats/assets/viewer.{css,js} in swe-swe, and the e2e build regenerated client-dist/canvas-bundle.js in agent-chat with only path-comment changes. Neither was committed.
>
> Everything takes effect on the next image rebuild + reboot (new mcp binary, new CLAUDE.md steering, new agent-chat prompts). Nothing is pushed yet.

[Quick replies]
- Push both + /reboot
- Push both, no reboot
- Leave unpushed for now

**USER**

> Maybe whenever agent calls `mcp` for usage, e.g. `mcp server123 tool456 flags789`, it can end/start the response with `[mcp] tip: call “mcp server123 -h”` to refresh yourself on this tool’s instructions for better use, or “mcp -h” for all mcp servers`. This would help with the forget, and only applies to tools that are actually used?

<small>took 46.4s</small><br>
**AGENT**

> I like it — it generalizes the agent-chat pointer to EVERY server, lives in one place (the mcp CLI), needs zero server cooperation, and self-targets exactly the tools in active use. Three design choices to settle:
>
> **1. Throttle, or every call?** A tip on every call is noisy — check_messages fires every turn, so the agent would read the same line hundreds of times (token cost, and repeated text gets ignored like a banner). The mcp CLI is a one-shot process, but the socket dir is per-session and writable, so I can throttle statefully: touch a tiny marker file per (server, tool) and only re-print the tip when it's older than ~45 min. That roughly matches compaction cadence — fresh context sees the tip again, hot context isn't spammed. Cost: one stat per call.
>
> **2. stdout or stderr?** stderr. The agent still sees it in the command result, but stdout stays byte-clean for the actual tool result (JSON parsing, piping, image paths). A tip glued onto stdout could corrupt `mcp ... | jq` style usage.
>
> **3. Wording** — small correction to your sketch: `mcp server123 -h` lists tool names only; the per-tool docs are `mcp server123 tool456 -h`, and the everything-dump is `mcp server123 --full`. So:
>
>     [mcp] tip: tool docs are not in your context and fade after compaction.
>           refresh: mcp server123 tool456 -h   (or: mcp server123 --full)
>
> Printed after a successful tools/call only — never on -h itself, never on flag errors (those already point at -h).
>
> **Relation to what we just shipped**: this makes the agent-chat SWE_MCP_LESS pointer (49a7396) mostly redundant — same reminder, delivered by the CLI instead of one server. I'd keep both for now (harmless, and agent-chat's covers voice mode wording) and consider dropping the agent-chat one after the CLI tip proves itself in recordings.
>
> Want me to build it with those three choices?

[Quick replies]
- Yes, build it exactly like that
- Build it, but no throttle (every call)
- Build it, but tip on stdout
- Also revert the agent-chat pointer then

**USER**

> 1. Add --remind-help-text-throttle default 30m 
> 2. Stderr; 1-liner not multi line text. Maybe wrap with <mcp>{content}</mcp> instead?
> 3. Is there a need for “lists tool names only”? We should rename `--full` to overwrite `-h`.
>
> We should discard the agent-chat’s change then

<small>took 1m 9s</small><br>
**AGENT**

> Building it: <mcp>tip</mcp> one-liner on stderr after successful tool calls, throttled by --remind-help-text-throttle (default 30m, marker files in the session socket dir), mcp <server> -h becomes the full dump (removing --full), recipe updated, and reverting the agent-chat pointer commit.

<small>took 6m 41s</small><br>
**AGENT**

> Done, all three decisions implemented and committed. Live behavior with the new binary:
>
>     $ mcp swe-swe-playwright browser_snapshot
>     ...result on stdout...
>     <mcp>tip: this tool's docs are not in your context and fade after compaction; refresh: mcp swe-swe-playwright browser_snapshot -h (all tools: mcp swe-swe-playwright -h)</mcp>
>
>     $ mcp swe-swe-playwright browser_snapshot      # within 30m: no tip
>     $ mcp --remind-help-text-throttle=0 ...        # tip every call
>
> Details: tip is one line on stderr (stdout stays byte-clean for piping), skipped for -h calls and isError results, throttled per (server, tool) via marker files in the session socket dir. `mcp <server>` and `mcp <server> -h` are now the same full dump — the --full flag and the one-line tool index are gone, and the CLAUDE.md recipe no longer mentions them.
>
> Commits (swe-swe main, unpushed): `a391f2be3` (CLI: tip + full-dump help, 47 golden variants) and `83d229ce0` (recipe wording, 39 golden variants). Full make test green; live smoke verified all four behaviors.
>
> **agent-chat**: the pointer commit 49a7396 is discarded (reset, was never pushed) — that repo is back to origin/main with a clean tree, and its e2e-verified state is untouched.
>
> So the session total on swe-swe main is now 4 unpushed commits: b6e3ef372, 76f85d150, a391f2be3, 83d229ce0. Push?

[Quick replies]
- Push + /reboot to dogfood
- Push only
- Hold, review diffs first

**USER**

> > one-liner on stderr after succesful tool calls
>
> Failed tool calls seem like they need help more than success? Can we send as first line of stderr without waiting for tool calls to succeed or fail? What that be confusing to agent?

<small>took 7m 52s</small><br>
**AGENT**

> Done — the tip now prints BEFORE the call, so failures carry it too. Committed as `862df03b0`.
>
> What changed: the <mcp>tip</mcp> line is emitted as soon as the tool name resolves — before flag parsing and the RPC. So a wrong-flags failure (the forgotten-docs case, which previously got NO -h pointer at all), an isError result, and even a blocking send_message killed mid-wait all still deliver the tip, and it appears as the first stderr line ahead of any error. Unknown tool/server still get no tip since the refresh command it suggests wouldn't resolve (that error already says "run: mcp <server> -h"). Not confusing to the agent — it reads like a compiler note, and the <mcp>...</mcp> wrapper marks it as channel metadata rather than tool output. Tests cover tip-before-error ordering, isError, and the unknown-tool case.
>
> Two things you should know:
>
> 1. **Another session is working in this checkout** (session-title-set / set_session_name feature: entrypoint.sh, swe-swe-server/main.go, new slash command — all uncommitted). Its `git add` raced into my commit — git's index is shared per checkout, so my `git commit` briefly swept in 180 of its golden files. I caught it, reset, and recommitted surgically; its work is intact and back to unstaged. Saved a memory note to always verify `git diff --cached` before committing here.
> 2. The full test suite had two transient failures from that same concurrency (shared /tmp dirs); every target passes on re-run, and make test-mcp is fully green.
>
> swe-swe main now has 5 unpushed commits: b6e3ef372, 76f85d150, a391f2be3, 83d229ce0, 862df03b0. Pushing publishes only these — the other session's uncommitted work stays local. Push?

[Quick replies]
- Push
- Push + /reboot to dogfood
- Hold until the other session commits

**USER**

> Is our binary recompiled i.e. next new session would be executing our new mcp-cli-proxy?
>
> /ck:export-chat-logs

<small>took 41.6s</small><br>
**AGENT**

> Answering the binary question after export. First: exporting this session's chat log...

