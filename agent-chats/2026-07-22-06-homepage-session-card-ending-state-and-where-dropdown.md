<!-- agent-chat export
title: Homepage Session Card Ending State And Where Dropdown
date: 2026-07-22
index: 06
slug: homepage-session-card-ending-state-and-where-dropdown
session: 612e2fc653a0cde5
agent: claude
version: 0.8.17 (a9ccfee)
-->

# Homepage Session Card Ending State And Where Dropdown

_2026-07-22 · 06 · claude · agent-chat 0.8.17 (a9ccfee)_

**USER**

> 1. New session should open up that dropdown by default?
> 2. When keeping session log + end, should we be able to indicate in the homepage that this session is ending?
>
> <div style="display:flex;flex-wrap:wrap;gap:8px;">
> <a href="./assets/2026-07-22-06-1-43e44cf683d9.jpeg" style="flex:0 1 calc(33% - 8px);max-width:calc(33% - 8px);"><img src="./assets/2026-07-22-06-1-43e44cf683d9.jpeg" alt="IMG_0251.jpeg" style="width:100%;height:auto;display:block;border-radius:6px;"></a>
> <a href="./assets/2026-07-22-06-2-2a5c3db02c9c.jpeg" style="flex:0 1 calc(33% - 8px);max-width:calc(33% - 8px);"><img src="./assets/2026-07-22-06-2-2a5c3db02c9c.jpeg" alt="IMG_0249.jpeg" style="width:100%;height:auto;display:block;border-radius:6px;"></a>
> <a href="./assets/2026-07-22-06-3-78d42eeb3d8c.jpeg" style="flex:0 1 calc(33% - 8px);max-width:calc(33% - 8px);"><img src="./assets/2026-07-22-06-3-78d42eeb3d8c.jpeg" alt="IMG_0247.jpeg" style="width:100%;height:auto;display:block;border-radius:6px;"></a>
> </div>

<small>took 1m 24s</small><br>
**AGENT**

> Both are doable; #1 is already half-shipped, #2 needs a new flag. Assessment first, no code changed yet.
>
> **1. Auto-open the Where dropdown — already true on desktop, deliberately off on touch**
>
> `new-session-dialog.js:1084-1092`: focus (which opens the listbox) is gated on `matchMedia('(hover: hover)')`. The comment says touch was excluded because "the dialog's first frame was its own content hidden behind a dropdown, with the soft keyboard already up."
>
> That reason is now weak: everything below the dropdown (`#new-session-fields`) is hidden until you pick, so on your iPad there is no content to obscure. And `combo-box._open()` (combo-box.js:561) does **not** focus the input, so it opens the list with no soft keyboard.
>
> Fix: call `whereCombo._open()` on touch instead of `.focus()`, and do it inside `reposLoaded.then(...)` so the repo options are in the list before it paints. ~15 minutes incl. golden-update.
>
> **2. "Commit the log, then end" leaves no trace on the homepage — correct, and fixable cheaply**
>
> `main.go:8838-8849`: the commit branch returns 202 *without* calling `markSessionEnding`, on purpose — the agent needs its tools and working tree, so the session must stay joinable. So `Ending` stays false and the card looks identical to a live session.
>
> The rendering half already exists: `selection.html:1689` `session-card--ending`, `:1727` "Ending..." label, fed by the `/api/sessions/live` poll (`main.go:9019`).
>
> Fix: add a second, non-latching `EndRequested` flag on `Session` set in the commit branch, surface it next to `Ending` in the live payload, and render a distinct label — "Committing log, then ending..." — that greys the End button but leaves Join enabled. ~45 minutes.
>
> Say which to build; I'd do #1 first since it's the smaller one and you hit it every session.

[Quick replies]
- Do both, #1 first
- Just #1
- Just #2
- Neither yet, let me think

**USER**

> git push first, then just do #2 and show me screenshot.

<small>took 17.8s</small><br>
**AGENT**

> Push blocked, not by anything I can fix: timestamp-shifter refuses because rewriting 3 commits would land them in the future ("Rewrite would create timestamps in the future"). It needs wall-clock time to pass. I'll retry after building #2.
>
> Starting #2 now: EndRequested flag + homepage label.

<small>took 12m 10s</small><br>
**AGENT**

> #2 is built, tested and committed as `aafcf6412`→`e977bf826` (SHA moved, see push note). Screenshot: right card is committing, left is a normal live session.
>
> **What the card does now**
>
> Banner "Committing chat log, then ending..." with a pulsing dot, shown from the moment the commit request reaches the agent. Join *and* End both stay — the agent is instructed to refuse and not end if the log can't be safely committed, so hiding End would strand that session inside the banner. When the agent's own `end_session` lands, the existing "Ending..." state takes over and the card disappears.
>
> Also removed the `alert()` that used to be the only feedback: it fired in one tab and vanished on reload. The banner is server state, so every tab sees it and it survives F5.
>
> Verified live in `make e2e-up-simple` against the real code path (`POST /api/session/{uuid}/end?chatlog=commit` → 202, `/api/sessions/live` → `endRequested: true`, reload → `session-card--committing`), plus the both-classes case where teardown correctly wins. `make test` green, `make build golden-update` diff is exactly the 3 files I touched.
>
> Files: `main.go` (endRequested flag + live API + SessionInfo), `page-templates/selection.html` (banner + CSS), `static/homepage-main.js` (poll + local mark).
>
> **Push is blocked until 19:03 today, and it is not something I can work around**
>
> timestamp-shifter maps the 4 unpushed commits onto its schedule and the last one lands at 19:03 SGT — in the future — so it refuses ("Wait for time to pass or reduce the number of commits"). I confirmed the rule in `/repos/ts-prepush-hook/workspace/rewrite-history.sh:143`. Nothing reached GitHub; `origin/main` is still `e9f0f40dc`. Local main has all 4 commits, contents intact, SHAs rewritten twice by the hook's normal behaviour. The other three are from your prctx/agent-chats sessions, so squashing to fit the window isn't mine to do.
>
> Retry `git push origin main` after 19:03 SGT and it should go through in the usual two attempts.
>
> Next: say the word and I'll do #1 (auto-open the Where dropdown on touch) while we wait on the clock.
>
> <div style="display:flex;flex-wrap:wrap;gap:8px;">
> <a href="./assets/2026-07-22-06-4-de1f74e2ab59.png" style="flex:0 1 calc(33% - 8px);max-width:calc(33% - 8px);"><img src="./assets/2026-07-22-06-4-de1f74e2ab59.png" alt="committing-vs-live-dark2.png" style="width:100%;height:auto;display:block;border-radius:6px;"></a>
> </div>

[Quick replies]
- Do #1 now
- Squash the 4 commits and push now
- Leave the push, I'll do it at 19:03
- Show me the code diff

**USER**

> I don’t like that it is wordy and breaks layout. Do we have space to reword the `Join Session` as `Join Session (Ending)` ?

**USER**

> And question for homepage: when last message is user bubble and agent is busy, it seems like homepages don’t show what user said
>
> <div style="display:flex;flex-wrap:wrap;gap:8px;">
> <a href="./assets/2026-07-22-06-5-784368c3c304.jpeg" style="flex:0 1 calc(33% - 8px);max-width:calc(33% - 8px);"><img src="./assets/2026-07-22-06-5-784368c3c304.jpeg" alt="IMG_0254.jpeg" style="width:100%;height:auto;display:block;border-radius:6px;"></a>
> <a href="./assets/2026-07-22-06-6-478d99722c7b.jpeg" style="flex:0 1 calc(33% - 8px);max-width:calc(33% - 8px);"><img src="./assets/2026-07-22-06-6-478d99722c7b.jpeg" alt="IMG_0253.jpeg" style="width:100%;height:auto;display:block;border-radius:6px;"></a>
> </div>

<small>took 7m 21s</small><br>
**AGENT**

> Reworked to `Join Session (Ending)` — banner gone, card height unchanged (both cards measure 191px). Amended into `327562e16`.
>
> Implementation is a hidden `<span> (Ending)</span>` inside the Join label, revealed by the `--committing` class. Nothing new in the DOM flow, no extra row. Wrapped label and suffix in one span so the button's 8px icon gap doesn't open up before "(Ending)". The "why" now lives in a tooltip on the button instead of body text.
>
> Re-verified in a fresh container on both paths: reload (server-rendered class) and the 3s live poll (other tabs pick it up without a reload). `make test` green, golden diff is only `selection.html` + `homepage-main.js`.
>
> ---
>
> **Your homepage question: found it, it's a real bug, one-line-ish fix**
>
> `main.go:7292` `getSessionSummaryFromChat` reads only the *last* JSONL line. agent-chat writes a bookkeeping event `userMessagesConsumed` with empty text right after your message lands, and that type falls into the `default:` branch at `main.go:7357-7360`, which renders `"Agent: " + ""`. That's your empty `Agent:` — your message is one line above it, intact.
>
> I confirmed it against the live log for this very session: the tail is `userMessage` ("I don't like that it is wordy...") followed by `userMessagesConsumed` with `text=''`.
>
> Fix: walk backwards to the last event that actually renders something instead of trusting line one-from-the-end. Status stays "red" either way, so the red/green dot is unaffected. About 20 minutes with a test over a synthetic events file.
>
> Say the word and I'll fix it. Push is still clock-blocked until 19:03 SGT (now 5 commits — a sibling session added one).
>
> <div style="display:flex;flex-wrap:wrap;gap:8px;">
> <a href="./assets/2026-07-22-06-7-ff51ceb91a7d.png" style="flex:0 1 calc(33% - 8px);max-width:calc(33% - 8px);"><img src="./assets/2026-07-22-06-7-ff51ceb91a7d.png" alt="join-ending-final.png" style="width:100%;height:auto;display:block;border-radius:6px;"></a>
> </div>

[Quick replies]
- Fix the summary line
- Do #1 (Where dropdown) first
- Leave it, just push later
- Show me the diff

**USER**

> Fix summary line, where dropdown, then /wrap-up and git push

