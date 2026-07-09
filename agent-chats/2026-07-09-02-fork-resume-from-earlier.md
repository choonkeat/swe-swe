<!-- agent-chat export
title: Fork Resume From Earlier
date: 2026-07-09
index: 02
slug: fork-resume-from-earlier
agent: claude
version: 0.8.7 (2089b8b)
-->

# Fork Resume From Earlier

_2026-07-09 · 02 · claude · agent-chat 0.8.7 (2089b8b)_

**USER**

> For our fork, I sometimes see
>
> ```
> Cannot fork this conversation
>
> This session’s agent is mid-work on a tool call. Wait for it to finish, then try forking again
>
> [Back to sessions]
> ```
>
> But this could be a dead session. Can we have a link `[Fork before that tool use]` that looks like a button as default (bottom right) and keep the `[Back to sessions]` as bottom left?

<small>took 1m 46s</small><br>
**AGENT**

> Found it. The "mid-work" guard in handleForkExecute blanket-refuses. Adding a primary `[Fork before that tool use]` button (bottom-right) + keeping `[Back to sessions]` bottom-left. It forks at the last chat reply (before the in-flight tool call), bypassing the guard — safe for dead sessions. Implementing now.

<small>took 3m 56s</small><br>
**AGENT**

> Done. The "mid-work on a tool call" fork error now offers a recovery button.
>
> **What changed**
> - The error modal now shows **`[Back to sessions]`** bottom-left and a default-styled **`[Fork before that tool use]`** button bottom-right (new `.actions-split` layout, space-between).
> - The button POSTs `bypass_active=1`, which skips the active-tail guard and forks at the **last chat reply** — the point *before* the in-flight tool call. That anchor (`AnchorLastChatReply`) never truncates mid-tool-call, so it's safe; it just drops the in-flight work, which is exactly what you want for a dead/stuck session.
> - Reworded the error to: *"…If it finishes, try forking again. If the session is dead or stuck, fork from before that tool use instead."*
>
> **Why it's sound**: a whole-session fork already uses that same last-reply anchor — the guard was blanket-blocking it out of caution. This just makes it an explicit, opt-in escape hatch.
>
> **Files**: `swe-swe-server/main.go` (new `bypass_active` path + `OfferForkBeforeActive` field) and `page-templates/fork-confirm.html`. `make build` + `make golden-update` done, golden staged, `make test` green (incl. TestGoldenFiles + forkconvo).
>
> Two notes:
> 1. I couldn't drive the live error page (it needs a real session frozen mid-tool-call), so it's verified via build + golden + tests, not a browser click. Want me to spin up an e2e container and simulate it?
> 2. Nothing committed yet. Want me to commit (main.go + template + golden)?

[Quick replies]
- Commit it
- Verify in e2e first
- Leave it, I'll review

**USER**

> Actually `[Back to sessions`] can be kept as bottom right… and the new `Resume from earlier` can be a bottom left _link_ ; all while _keeping_ its dialog style of appearing more like an error

<small>took 2m 48s</small><br>
**AGENT**

> Reworked. The error dialog now:
>
> - **`Resume from earlier`** — a quiet underlined **link**, bottom-**left** (new `.btn-link` style: no border/background, secondary text color). Still POSTs `bypass_active=1` → forks at the last reply, before the in-flight tool call.
> - **`Back to sessions`** — kept as the button, bottom-**right** (an empty placeholder pins it right even when there's no recovery link).
> - Red error box unchanged, so it still reads as an error with a subtle escape hatch.
>
> Also applied the same treatment to the other error case ("Fork the whole session instead" is now the same quiet left link), and reworded the message to match: *"…resume from earlier (before that tool use) instead."*
>
> `make build` + `make golden-update` done, golden staged, `make test` green (1213 pass, exit 0).
>
> Still nothing committed. Commit it (main.go + template + golden)?

[Quick replies]
- Commit it
- Verify in e2e first
- Leave it, I'll review

**USER**

> /ck:export-chat-logs and git commit

<small>took 43.4s</small><br>
**AGENT**

> Feature committed (97178f991). Now exporting the chat log...

