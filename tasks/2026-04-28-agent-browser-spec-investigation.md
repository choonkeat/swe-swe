# agent-browser.spec.js — investigation TODO

## Symptom

`e2e/tests/agent-browser.spec.js:17` ("OpenCode chat session: playwright
visits example.com and takes screenshot") fails with:

```
Error: expect(received).toBe(expected)
Expected: true
Received: false
  > 107 |     expect(found).toBe(true);
```

i.e. after 120 s of polling (24 × 5 s), the test saw neither:
- an inline `<img>` in `.bubble.agent`, nor
- an agent reply containing "screenshot" + ("example.com" | "Example
  Domain" | ".png")

Reproduces in a fresh e2e simple container against `origin/main`
(verified after the e2e port-range fix `5ae85b1df`, so this is *not*
the port-pool-exhaustion issue).

## What is known

- OpenCode default config works for plain chat — user confirmed via
  screenshot (a manual "testing 123" prompt got a Build/big-pickle
  reply, no API key needed).
- The chat session connects (`[system] Connected` is observable in
  the iframe per the test's earlier waits). MCP probe succeeds.
- The test then sends "use playwright to visit example.com and take a
  screenshot" and expects the agent to actually invoke a playwright /
  browser tool that fetches example.com.
- The test was last touched **2026-04-23** in `7e4dcb9ab` (test(e2e):
  refresh stale selectors and raise probe timeout). It has been
  through several maintenance commits since `371ec2978` (initial).

## Suspected causes (rank-ordered)

1. **OpenCode build's playwright tooling isn't reachable / wired in
   the e2e simple container.** The default OpenCode model can chat
   but may not have a playwright tool available in this image.
   Verify: in a live e2e container, send the same prompt manually
   and watch what the agent actually does.

2. **The agent reasons but never picks the right tool within 120 s.**
   Default model is fast for chat but might thrash on tool selection.
   Verify: check the agent transcript for tool-call attempts; raise
   the timeout to 300 s and see if it eventually completes.

3. **Network reachability to example.com from inside the e2e
   container's chrome.** Less likely (the live stack's chrome works),
   but worth confirming via a curl from inside the container.

4. **Test selector drift.** `quickReplyBtn = #quick-replies .chip`
   and `anyImg = .bubble.agent img` may have moved in the chat-iframe
   markup since the last refresh. Verify by inspecting the live chat
   iframe DOM.

## How to investigate

1. Bring up e2e simple, log in to the chat session manually via the
   `host.docker.internal:9780` URL.
2. Send the exact prompt the test sends:
   `use playwright to visit example.com and take a screenshot`
3. Watch the chat iframe — what does the agent actually do? Does it
   announce a tool call? Does it claim it can't? Does it produce
   any image at all?
4. If it hangs: check `docker exec ... ps aux` for chrome / playwright
   processes. Check the swe-swe-server logs for MCP errors.
5. If selector mismatch: open DevTools on the chat iframe; compare
   the actual quick-reply button and image-bubble selectors against
   what the spec uses.

## What this task is NOT

- Not blocking the tunnel-integration v1 work (already shipped on
  `origin/main`).
- Not blocking any other e2e spec — every other test in the suite
  passes in a fresh container after the e2e port-range fix
  (`5ae85b1df`).

## Where to record the fix

When this is resolved, either:
- Update `e2e/tests/agent-browser.spec.js` (selector / timeout
  changes), or
- Add a `test.skip` with a reason if the test is judged not viable
  in CI without an API key, or
- Mark the test as `@manual` / `tests/manual/` if it depends on a
  live LLM reasoning path that isn't reproducible in CI.

Either way, delete this TODO when the test is reliably green or
deliberately skipped.
