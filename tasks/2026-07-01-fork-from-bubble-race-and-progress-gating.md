# Fork-from-bubble: latest-bubble race + progress-bubble gating

Date: 2026-07-01
Status: steps 1,2,4 implemented (server, this repo); step 3 filed as a bug in
the agent-chat repo (tasks/2026-07-01-fork-menu-hide-on-progress-bubbles.md)

## Problem

Tapping "Fork from here" on the **newest** chat bubble failed with a raw
409 dumped onto a blank page:

```
resolve bubble anchor: locate send_message call #4 in
/home/app/.claude/projects/-repos-github-com-choonkeat-tiny-form-fields-workspace/
9b11f61e-...jsonl: only 3 of 4 expected send_message calls present
```

Manually forking the swe-swe session id worked. Two distinct issues surfaced.

### Issue A -- latest-bubble flush race (the reported failure)

`fork_resolve.go:resolveBubbleAnchor` reads the bubble's stamp
(`AgentToolName=send_message`, `AgentToolSeq=N`) from the agent-chat events
log, then `findNthMCPToolCall` counts `mcp__swe-swe-agent-chat__send_message`
tool_use lines in Claude's transcript and needs the Nth.

The forked bubble was the **latest** send_message -- the one the agent was
still **blocked inside**, waiting for the user's reply (the message with the
quick-reply buttons). agent-chat publishes + stamps the bubble the instant
send_message is called, but Claude Code has not flushed that assistant
tool_use line into its `.jsonl` yet. For a moment the events log is one bubble
ahead of the transcript: `N=4`, transcript has `3`. Resolver errors.

Manual session-id fork works because it uses `AnchorLastChatReply` --
forkconvo anchors on the last reply *present in the transcript* (the 3rd),
independent of the events-log stamp.

The existing ACTIVE-tail guard (`main.go:7705`) intentionally does NOT catch
this: it only blocks forks with an in-flight **non-chat** tool call. A blocking
send_message is the normal state when forking, so it passes through into the
off-by-one.

### Issue B -- progress bubbles are forkable and shouldn't be

In agent-chat `workspace/tools.go`, `send_progress` publishes
`Event{Type:"agentMessage", AgentToolName:"send_progress"}` -- **same Type as a
real reply**; only `AgentToolName` differs. The resolver's `case "agentMessage"`
blindly takes `bubble.AgentToolName`, so forking a progress bubble *succeeds*
and anchors after the non-blocking `send_progress` tool_use -- a mid-turn cut
point where the agent kept working. forkconvo truncates live work silently.
Worse than Issue A's visible error. Same applies to `send_verbal_progress`
(published as `verbalReply` with `AgentToolName=send_verbal_progress`).

## Design decisions

- **No new wire protocol.** The existing stamp (`AgentToolName` + `AgentToolSeq`)
  plus the events log's ordering give the server everything it needs. Server
  distinguishes race from invalid-id locally; agent-chat just stops *offering*
  doomed forks.
- **Race vs invalid, precisely:**
  - Race (safe fallback): `N == M+1` AND the bubble is the newest bubble for
    that tool in the events log (nothing after it).
  - Invalid / inconsistent (keep erroring): `N > M+1`, or the bubble is missing
    but is NOT the tail. A blind fallback here could anchor at the wrong place.
- **Fallback target = `AnchorLastChatReply`**, i.e. exactly what the manual
  session-id fork did and the user already accepted. It forks after the last
  persisted reply (the (N-1)th), omitting the not-yet-persisted in-flight
  bubble -- which is an unanswered question anyway, so "fork after it" is
  ill-defined regardless.

## Steps

### Step 1 -- server: reject progress-tool anchors (defense-in-depth)
File: `cmd/swe-swe/templates/host/swe-swe-server/fork_resolve.go`
- In `resolveBubbleAnchor`, after computing `toolName`, reject
  `send_progress` / `send_verbal_progress` with a clear sentinel error
  (`ErrProgressBubbleNotForkable`). These are not turn boundaries.
- Verify: unit test that a bubble stamped `send_progress` returns the sentinel,
  not a resolved anchor.

### Step 2 -- server: latest-bubble race fallback
Files: `fork_resolve.go`, `main.go` (~7741 caller)
- Add a helper to determine whether `bubbleSeq` is the newest bubble for its
  tool in the events log (reuse the single scan in `loadBubbleAndConsume` /
  add a tail-check).
- When `findNthMCPToolCall` fails with the "only M of N" shape AND
  `N == M+1` AND bubble is the tail: return a typed
  `ErrAnchorNotYetPersisted` (carrying enough for the caller to fall back).
- In the `main.go` caller: on `ErrAnchorNotYetPersisted`, set
  `forkOpts.Anchor = forkconvo.AnchorLastChatReply` (+ `Tool = send_message`)
  and log an INFO that it fell back. Any other resolver error -> keep the 409.
- Verify: unit test with a synthetic events log (N=4 tail bubble) + transcript
  (3 send_message lines) resolves to the fallback; a non-tail / N>M+1 case still
  errors.

### Step 3 -- agent-chat UI: gate the Fork menu item
Repo: `.swe-swe/repos/agent-chat` (Elm frontend)
- Only render "Fork from here" when the bubble's tool is
  `send_message` / `send_verbal_reply` (agent side) or it's a `userMessage`.
- Hide it for `send_progress` / `send_verbal_progress` bubbles.
- Requires surfacing `AgentToolName` to the Elm view model if not already
  present (confirm in `workspace/tdspec/src/EventBus.elm` / the WS payload).
- Verify: progress bubble shows no fork item; reply bubble does.

### Step 4 -- error UX for the remaining genuine-error case
File: fork confirm/modal flow (find where the 409 body is rendered)
- Catch non-2xx from `/api/fork` and show a readable message instead of a
  blank page with raw text (screenshot IMG_2848), plus a "fork the whole
  session instead" affordance.
- Verify: forcing a genuine anchor error shows the friendly UI, not raw text.

## Verification (end to end)
1. Boot test container (docs/dev/test-container-workflow.md).
2. Drive a session so the agent is blocked in a send_message; tap "Fork from
   here" on that latest bubble -> fork succeeds via fallback (was 409).
3. Confirm a `send_progress` bubble offers no fork item.
4. `make test` green; `make build golden-update` if any template/main.go change
   touches golden output; review the golden diff.

## Notes
- The reported screenshot bubble was a `send_message` (question + quick
  replies) => the observed failure was Issue A. Issue B is a latent
  separate bug the user anticipated.
- Keep changes in `templates/host/...` (the embedded source), not the golden
  copies under `testdata/`.
