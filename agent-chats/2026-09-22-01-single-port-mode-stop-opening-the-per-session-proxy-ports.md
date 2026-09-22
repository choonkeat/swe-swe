<!-- agent-chat export
title: Single Port Mode Stop Opening The Per Session Proxy Ports
date: 2026-09-22
index: 01
slug: single-port-mode-stop-opening-the-per-session-proxy-ports
session: 5c9955cbb429c672
agent: claude
version: 0.13.1 (02e72ab)
-->

# Single Port Mode Stop Opening The Per Session Proxy Ports

_2026-09-22 · 01 · claude · agent-chat 0.13.1 (02e72ab)_

**USER**

> OpenHands seem to be able to be hosted without multiple ports or wildcard subdomains like swe-swe. Why? Can research?

<small>took 5.4s</small><br>
**AGENT**

> Researching how OpenHands serves everything from one port, then comparing with swe-swe. About 5 minutes.

<small>took 15.6s</small><br>
**AGENT**

> Step 1 of 3 done: read swe-swe's own design notes on ports. Now reading OpenHands' self-hosting docs.

<small>took 44.5s</small><br>
**AGENT**

> **Short answer: OpenHands doesn't avoid the problem. It gives up things swe-swe keeps.** Research is done.
>
> **What OpenHands gives up to use one port**
>
> 1. **Only one workspace at a time.** In their fixed-port setup, the docs say: "Only one sandbox can run at a time; concurrent conversations will collide on those ports." (A sandbox is one isolated workspace for one conversation.) If you want several at once, each one gets a random port. Then you need an extra routing server in front, which means many ports again.
> 2. **Your app gets only 2 fixed ports (8011 and 8012).** If the agent starts an app on any other port, such as 5000, you get an error page. People have reported this.
> 3. **The code editor lives at a sub-path (`/vscode`) of the same website.** The browser doesn't separate them, so they share logins and saved data. Their editor also breaks behind a proxy: it sends you back to the home page. They closed that bug as "not planned."
> 4. **Their own hosted service uses wildcard subdomains.** Their cloud addresses look like `work-1-fvvocjbmlliibebj.prod-runtime.all-hands.dev`, one subdomain per workspace. That's the same thing swe-swe asks for.
>
> **Why swe-swe needs separate addresses**
>
> - Many sessions run at once, and each can show its own app preview. Each preview needs its own address so apps don't clash.
> - A browser keeps saved data (logins, cookies) separate per address. Separate addresses stop one session's app from seeing another's.
> - swe-swe tried serving apps from sub-paths (like `/vscode`) and dropped it. Most web apps break when moved under a sub-path. The design notes on this are in `docs/adr/0025-per-session-preview-ports.md`.
>
> **What swe-swe already has:** a one-port mode, `--public-hostname=example.com`. You still need a wildcard subdomain (`*.example.com`) pointing at the box, but no tunnel and no extra ports. Details are in `docs/adr/0046-wildcard-host-demux.md`.
>
> **Next step (2 minutes):** choose whether swe-swe should get an OpenHands-style "one port, no wildcard" option, with one app preview and less isolation. If yes, I'll write the plan, which takes about 20 minutes.
>
> Sources: [OpenHands self-hosting](https://github.com/OpenHands/OpenHands/blob/main/docs/SELF_HOSTING.md) · [Docker sandbox docs](https://docs.openhands.dev/openhands/usage/sandboxes/docker) · [issue #12403 (fixed ports)](https://github.com/OpenHands/OpenHands/issues/12403) · [issue #13912 (editor behind proxy)](https://github.com/OpenHands/OpenHands/issues/13912) · [issue #6183 (subdomain forwarding)](https://github.com/OpenHands/OpenHands/issues/6183)

[Quick replies]
- No, keep the current design
- Yes, plan a one-port mode

**USER**

> Compare 1 Openhands workspace with 1 swe-swe sessions. swe-swe needs a lot more ports. How about Openhands?

<small>took 2.7s</small><br>
**AGENT**

> Counting the ports for 1 swe-swe session and 1 OpenHands workspace. About 3 minutes.

<small>took 21.0s</small><br>
**AGENT**

> **For one workspace each: OpenHands uses 4 ports and swe-swe uses 6.** Neither gets by on one port. (A port is a numbered entry point on the server, and each one becomes a separate web address.)
>
> **1 OpenHands workspace: 4 ports**
>
> 1. Agent server (8000): handles the chat, terminal, files, and the agent's browser. It shows that browser as still screenshots, not a live picture.
> 2. Code editor (8001).
> 3. Your app, slot 1 (8011).
> 4. Your app, slot 2 (8012). An app on any other port can't be reached.
>
> **1 swe-swe session: 6 ports**
>
> 1. App preview: your app, and it can use any port.
> 2. Chat panel: runs as its own small server.
> 3. Public app: a second address for your app that people can open without logging in.
> 4. Agent browser control: the connection the agent uses to drive its browser.
> 5. Agent browser live view: a moving picture of that browser, so you can watch or click along.
> 6. Files panel.
>
> The main page, terminal, and login share one extra port across all sessions. That's true for OpenHands too.
>
> **Why swe-swe's total looks much bigger:** each of the 6 kinds has 20 slots set aside, so 20 sessions can run side by side. That's 120 ports held in reserve. OpenHands can run several workspaces too, but each new one gets 4 random port numbers, which a front-door server then has to route. Its simple fixed-port setup allows only one workspace at a time.
>
> **Where the 2 extra ports come from:**
> - The live browser view: OpenHands sends screenshots instead.
> - The no-login public app address: OpenHands has no equivalent.
>
> **Next step (2 minutes):** decide whether swe-swe should be able to run with fewer ports per session, for example by folding the chat panel and files panel into the main port. If yes, I'll check how feasible it is. That takes about 30 minutes.

[Quick replies]
- No, that answers it
- Yes, check feasibility

**USER**

> How many ports are needed in OpenHands “simple fixed-port setup” (that only allows one workspace)
>
> How about our agent chat and files tabs be always powered by path-based proxy? Skip the wildcard subdomain & port? Discuss

<small>took 5.1s</small><br>
**AGENT**

> Step 1 of 2: checking how the chat and files panels are served today, and whether moving them onto a path would break them. About 10 minutes.

<small>took 24.7s</small><br>
**AGENT**

> Step 1 of 2 done: found that swe-swe can already serve chat and files from a path. Now writing up the trade-offs.

<small>took 6.8s</small><br>
**AGENT**

> **My recommendation: always use a path for the chat panel, but keep the Files panel on its own address.**
>
> **Q1. OpenHands' fixed-port setup uses 5 ports:** the main page, the agent server, the code editor, and two app slots. That's for one workspace in total.
>
> **Q2. Chat and files always on a path**
>
> swe-swe can already do this. Each session has path addresses like `/proxy/<session>/agentchat/` and `/proxy/<session>/files/`. Today the browser tries the separate address first and falls back to the path when that doesn't work. The code is in `static/modules/proxy-base.js`. So "always path" doesn't add anything new. It just skips the first try.
>
> **What we'd gain**
> 1. Two fewer ports per session, so 40 fewer held in reserve.
> 2. No test-and-wait step. On a server that silently blocks extra ports, that wait can last up to 5 seconds before the panel shows.
> 3. Simpler code for these two panels.
>
> **What we'd lose**
> 1. **Separation from the main page.** On a path, the panel counts as the same website as the terminal page. The browser then lets a web page shown inside the panel reach into the main page, including typing into the terminal.
>    - Chat: low risk, because it only shows swe-swe's own chat screen.
>    - Files: real risk, because it can open web-page files from the project, and anyone could have written those.
> 2. **Speed on plain (non-https) setups.** A browser opens only about 6 connections to one address at a time. The terminal, chat, and files would all share those 6.
> 3. **Fragile links in Files.** swe-swe rewrites the links in the Files pages so they still work under the path. Any link the rewrite misses breaks.
>
> **Next step (2 minutes):** reply "chat only" or "both". Making chat always use the path takes about 1 hour, including tests.

[Quick replies]
- Chat only
- Both
- Neither, keep as is

**USER**

> I just want to see how far I can push for swe-swe to NOT require any port or wildcard subdomains. I.e. user can try the product but not the full product
>
> So, hold that thought wrt Files tab. Investigate OpenHands’ @tmp/OpenHands’ agent browser not requiring exposing more ports (i believe it’s because it only shows screenshots of its browser to end user?)

<small>took 2.6s</small><br>
**AGENT**

> Step 1 of 2: reading the OpenHands code in /tmp/OpenHands to see how its agent browser reaches your screen. About 10 minutes.

<small>took 14.8s</small><br>
**AGENT**

> Step 1 of 2 done: confirmed that OpenHands shows only screenshots. Now checking whether swe-swe's live browser view can already work without an extra port.

<small>took 22.2s</small><br>
**AGENT**

> **Yes, you're right: OpenHands only shows screenshots.** And in swe-swe, the live browser view is the only panel that still needs its own port. Research is done (step 2 of 2).
>
> **How OpenHands does it (checked in `tmp/OpenHands`)**
> 1. Each time the agent does something in its browser, the result comes back with a picture of the page, saved as text inside the message. The file is `src/contexts/conversation-websocket-context.tsx`.
> 2. That picture arrives through the same connection that carries the chat. No extra port is needed.
> 3. The Browser tab is just an image plus the page address. You can't click, scroll, or watch it move. The picture only changes when the agent acts. The file is `src/components/features/browser/browser.tsx`.
>
> **swe-swe on a server that allows only one port**
>
> 1. App preview: already falls back to a path.
> 2. Chat panel: already falls back to a path.
> 3. Files panel: already falls back to a path.
> 4. **Agent View (live browser): no fallback.** It needs its own port or a wildcard subdomain. The code is in `static/terminal-ui.js`, in `getBrowserViewUrl`.
> 5. Public app address: a separate address by nature, so it simply isn't offered.
>
> The agent's own browser-control connection runs inside the server, so it needs no port that you open.
>
> **Two ways to close gap 4**
> - **A. Serve the live view from a path. This is my recommendation.** The live view is one web page swe-swe ships, plus one connection. That's the same kind of thing the other three panels already route through a path. You'd keep a live picture you can click, which is more than OpenHands gives. Estimate: about half a day.
> - **B. Screenshots, like OpenHands.** Less useful and not less work, because the agent's tools would need to send pictures back to the page.
>
> **Next step (2 minutes):** reply "A" to have me write a step-by-step plan for the path-based live view. The plan takes about 20 minutes.

[Quick replies]
- A: plan the path-based live view
- B: screenshots
- Stop here

**USER**

> (A) and also plan to have `None of the above` option for `My browser can reach it by` question for @www/index.html

<small>took 3.2s</small><br>
**AGENT**

> Writing the plan: (1) live Agent View served from a path, (2) a "None of the above" choice in www/index.html. About 20 minutes.

<small>took 34.2s</small><br>
**AGENT**

> **Please confirm the goal and the 3 phases below.** Once you do, I'll walk through each phase in detail. Planning is on step 1 of 5.
>
> **Goal:** on a server where only one port can be reached and there are no wildcard subdomains, every swe-swe panel still works, including the live Agent View. The setup page gets a matching answer, "None of the above".
>
> **Phases**
>
> 1. **Server: serve the live view from a path.** swe-swe adds an address `/proxy/<session>/vnc/` on the main port. It passes everything through to the live browser view the same way its own port does today. The chat, preview, and files panels already have addresses like this.
> 2. **Page: use the path when nothing else works.** Agent View tries the addresses in the order the other panels use: wildcard subdomain first, then its own port, then the path. One fix is needed along the way: the live view's own port doesn't yet answer the quick "are you there?" check the other panels use. Without that fix, the live view would always pick the path, even when its port works.
> 3. **Setup page: add "None of the above".** This is the "My browser can reach it by" question in `www/index.html`. Choosing it shows a plain note that you can try swe-swe on one port with no extra setup. It also lists what you lose: the public no-login app address, and each panel's separation from the main page. The script it gives is the ordinary one, because swe-swe falls back to paths by itself.
>
> **How we'll know it works:** there's already a test that blocks every port except the main one and checks that the preview, chat, and files panels still load (`e2e/tests/proxy-fallback.spec.js`). I'll add a check to it that Agent View shows a live picture too. I'll write that check first, so it fails before the fix and passes after.
>
> **Next step (2 minutes):** reply "ok" to get Phase 1 in detail.

[Quick replies]
- ok
- Change the phases

**USER**

> ok

<small>took 3.1s</small><br>
**AGENT**

> Step 2 of 5: detailing Phase 1 (server path for the live view).

<small>took 14.3s</small><br>
**AGENT**

> **Please confirm Phase 1.** Planning is on step 2 of 5: phase list agreed, now Phase 1 in detail.
>
> **What Phase 1 achieves:** the main port answers at `/proxy/<session>/vnc/...` and passes through to that session's live browser view. It carries both the view page itself and its live connection, the stream that carries the moving picture and your clicks. If the view comes from a browser on a different machine, that works too.
>
> **Small steps**
> 1. **Write the failing test first** (in `proxy_listener_test.go`). Start a stand-in for the live view that serves a page and echoes back anything sent over a live connection. Then check four things:
>    - a. Asking the main port for `/proxy/<session>/vnc/vnc_lite.html` returns that page.
>    - b. A live connection opened at `/proxy/<session>/vnc/websockify` echoes messages back.
>    - c. Without the login cookie, both are refused.
>    - d. A guest who was shared a different session is refused. (The share feature limits a guest to one session.)
>    All four should fail now, because the address doesn't exist yet.
> 2. **Make it pass** (in `main.go`): register the new address next to the existing chat and files addresses. Route it to the same pass-through the live view's port uses today, so there's one copy of that logic, not two.
> 3. **Tidy up:** move the pass-through setup to where both the port and the path can share it. No change in behaviour.
> 4. Run `make test`, the full automated test suite.
>
> **How we'll know nothing broke**
> - The three existing live-view tests (host-name fix-up, and refusing or allowing a live connection by cookie) must stay green, unchanged.
> - The live view's own port is untouched, so boxes that reach it today behave the same.
> - Nothing on the page uses the new address yet. That's Phase 2.
>
> **Estimate:** about 1.5 hours.
>
> **Next step (2 minutes):** reply "ok" to get Phase 2 in detail.

[Quick replies]
- ok
- Change Phase 1

**USER**

> ok

<small>took 12.2s</small><br>
**AGENT**

> **Please confirm Phase 2.** Planning is on step 3 of 5: phases agreed, Phase 1 agreed, now Phase 2 in detail.
>
> **What Phase 2 achieves:** Agent View picks the best address it can actually reach, and falls back to the path when nothing else works, so the live picture appears on a one-port server.
>
> **Small steps**
> 1. **Fix the missing "are you there?" answer.** Today the live view's own port doesn't answer that check, so the page can't tell a working port from a blocked one. Wrap that port in the same small piece of code the other three panels use, `corsWrapper` in `main.go`. Test first: ask the live view's port for `/__probe__` and expect the marker it should send back.
> 2. **Write the failing page tests** in `static/modules/url-builder.test.js`, the existing test file for address building. Three cases: wildcard subdomain available, own port available, neither available. The third should produce the path address with the live connection pointed at `/proxy/<session>/vnc/websockify`.
> 3. **Make them pass** in `static/terminal-ui.js`. Resolve the address once per session and remember it, the way the Files pane already does with `_filesResolvedBase`. Keep the order: subdomain, then port, then path.
> 4. **Fix the two other places that ask for that address.** The "open in a new tab" list and the panel switch both call it directly and expect an answer immediately. They'll use the remembered one, or the path.
> 5. **Add the real-browser check** to `e2e/tests/proxy-fallback.spec.js`, the test that blocks every port except the main one. Open Agent View and wait for the live picture to actually connect.
> 6. Run `make test`, then `make e2e-up-simple`, `make e2e-test`, `make e2e-down`.
>
> **How we'll know nothing broke**
> - The three ways of reaching the live view are decided by one existing shared chooser, so the wildcard and port paths keep their current behaviour and stay covered by today's tests.
> - `e2e/tests/agent-browser.spec.js` and `agent-view-remote.spec.js` cover the ordinary and the other-machine cases. Both must stay green.
> - The "is the browser up yet" check that runs before the picture loads is untouched.
>
> **Estimate:** about 2.5 hours.
>
> **Next step (2 minutes):** reply "ok" to get Phase 3 in detail.

[Quick replies]
- ok
- Change Phase 2

**USER**

> ok

<small>took 4.4s</small><br>
**AGENT**

> Step 4 of 5: detailing Phase 3 (the "None of the above" choice).

<small>took 15.3s</small><br>
**AGENT**

> **Please confirm Phase 3.** Planning is on step 4 of 5: phases 1 and 2 agreed, now Phase 3.
>
> **What Phase 3 achieves:** the setup page has a fifth answer, "None of the above", for a machine you can only reach on one port with no wildcard. Picking it gives the ordinary script plus an honest note about what you get and what you don't.
>
> **Small steps**
> 1. **Add the choice** in `www/index.html`, under "My browser can reach it by", after "any port on it" and before "Tailscale". Label: "one fixed port, nothing else". Short note underneath: a rented server with one port open, or a company network that blocks the rest.
> 2. **Add the "what this means" box,** shown only when that choice is picked:
>    - Works: the terminal, chat, files, app preview, and the live Agent View, all served from the one address you opened.
>    - Doesn't work: the public web address for your app that other people can open without logging in.
>    - One caveat: each panel shares the page's address, so a web page opened in the Files panel can reach into the main page.
> 3. **Leave the script alone.** No extra settings are needed, because swe-swe now finds the working route by itself. I'll confirm the generated script is byte-for-byte the "any port" one.
> 4. **Add a "how do I check?" note**, matching the other choices: if opening this machine on a second port fails from your browser, pick this.
>
> **How we'll know it works, and that nothing broke**
> - There are no automated tests for this page today. I'll add a small one: start the page locally with `node www/serve.cjs`, drive it with a real browser, click each of the five choices in turn, and check the script and the visible notes each time. That gives the other four answers cover they don't have now.
> - I'll write the check for the new choice before adding it, so it fails first.
> - Screenshots of the page before and after, so you can see it rather than take my word.
>
> **Estimate:** about 1.5 hours. All three phases: about 5.5 hours.
>
> **Next step (2 minutes):** reply "ok" and I'll write the whole plan to `tasks/2026-09-22-path-based-agent-view-and-single-port-option.md`.

[Quick replies]
- ok
- Change Phase 3

**USER**

> ok

<small>took 1.9s</small><br>
**AGENT**

> Step 5 of 5: writing the plan file now.

<small>took 51.8s</small><br>
**AGENT**

> **Plan written: `tasks/2026-09-22-path-based-agent-view-and-single-port-option.md`.** Planning is done, 5 of 5 steps.
>
> What's in it:
> 1. Phase 1, about 1.5 hours: the server answers at `/proxy/<session>/vnc/` and passes through to the live browser view.
> 2. Phase 2, about 2.5 hours: the page picks the address it can reach, falling back to that path. Includes the fix that lets it tell a working port from a blocked one.
> 3. Phase 3, about 1.5 hours: the "one fixed port, nothing else" answer on the setup page, plus that page's first automated tests.
>
> Also recorded: three things deliberately left out, so we don't re-argue them later. Forcing chat and files onto a path always. A switch to skip the address checks on servers that stall instead of refusing. Copying the screenshot-only approach.
>
> **Next step (2 minutes):** reply "start" and I'll begin Phase 1, writing the failing test first.

[Quick replies]
- start
- Not yet
- Change something first

**USER**

> /clear-and-then start

<small>took 11.9s</small><br>
**AGENT**

> Starting Phase 1: writing the failing test for the new `/proxy/<session>/vnc/` address. About 1.5 hours.

<small>took 20.3s</small><br>
**AGENT**

> Phase 1, step 1 of 4: read the existing routes in main.go. Writing the failing test next.

<small>took 20.8s</small><br>
**AGENT**

> Phase 1, step 1 of 4 in progress: checking how existing tests fake a session and a websocket, so the new test matches them.

<small>took 1m 9s</small><br>
**AGENT**

> Phase 1, step 1 of 4: test written. Running it now to confirm it fails first.

<small>took 49.3s</small><br>
**AGENT**

> Phase 1, steps 1-2 of 4 done: the test now passes. Checking the noVNC page's asset links, then running the full test suite.

<small>took 13.9s</small><br>
**AGENT**

> Phase 1, step 3 of 4: page links are all relative, so nothing needs rewriting. Running the full test suite now (a few minutes).

<small>took 2m 41s</small><br>
**AGENT**

> Phase 1, step 4 of 4: full test suite passed. Regenerating the golden files now.

<small>took 1m 5s</small><br>
**AGENT**

> Phase 1 of 3 done and committed (7602a43be). Starting Phase 2: the page picks the address it can actually reach. About 2.5 hours.

<small>took 52.2s</small><br>
**AGENT**

> Phase 2, step 1 of 6: fixing the "are you there?" check on the live view's own port. Writing its test first.

<small>took 1m 7s</small><br>
**AGENT**

> Phase 2, step 1 of 6 done: the live view's port now answers the reachability check (proven failing without the fix). Next: the address-building tests.

<small>took 53.8s</small><br>
**AGENT**

> Phase 2, steps 1-2 of 6 done: address builders written and tested (108 tests pass). Now wiring Agent View to pick the reachable one.

<small>took 54.2s</small><br>
**AGENT**

> Phase 2, step 3 of 6: Agent View now falls back to the path. Wiring the one-time reachability check next.

<small>took 41.0s</small><br>
**AGENT**

> Phase 2, steps 3-4 of 6 done: all four places that ask for the Agent View address now use the resolved one. Running unit tests, then the browser test.

<small>took 1m 46s</small><br>
**AGENT**

> Phase 2, step 5 of 6: adding the real-browser test for Agent View on a one-port box.

<small>took 50.8s</small><br>
**AGENT**

> Phase 2, step 5 of 6 done: browser test written. Step 6: booting the test environment to run it. About 10 minutes.

<small>took 2m 29s</small><br>
**AGENT**

> Phase 2, step 6 of 6: test environment is up. Running the single-port browser test now (a few minutes).

<small>took 1m 22s</small><br>
**AGENT**

> All 3 single-port browser tests passed. Double-checking the new route by hand against the live container, since the test finished unusually fast.

<small>took 2m 46s</small><br>
**AGENT**

> Adding one more test: on an ordinary box, Agent View must still prefer its own port (not the new path).

<small>took 3m 0s</small><br>
**AGENT**

> Phase 2 of 3 done and committed (e48eafdcf). Starting Phase 3: the "None of the above" choice on the setup page. About 1.5 hours.

<small>took 1m 3s</small><br>
**AGENT**

> Phase 3, step 1 of 6: the setup page has no tests today. Writing them first, covering all five answers.

<small>took 1m 42s</small><br>
**AGENT**

> Phase 3, step 1 of 6 done: all 7 setup-page tests fail as expected. Adding the choice now.

<small>took 2m 51s</small><br>
**AGENT**

> Phase 3 done, all tests green. Committing now.

<small>took 50.5s</small><br>
**AGENT**

> **Reboot the stack to see it live** — the change is in the server binary, so the running container has the old code.
>
> All 3 phases done, 4 commits on `main` (unpushed):
>
> 1. `7602a43be` — the server now answers at `/proxy/<session>/vnc/` on the main port and passes through to the live browser view.
> 2. `e48eafdcf` — the page picks the address it can reach: wildcard subdomain, then its own port, then the path.
> 3. `aa1a8cb4f` — the setup page's fifth answer, "one fixed port, nothing else".
> 4. `912111517` — plan file marked done.
>
> **What now works:** on a machine you can only reach on one port, with no wildcard and no tunnel, every panel works — terminal, chat, files, app preview, and the live Agent View. Only the no-login public app address is still unavailable.
>
> **The bug I found on the way:** the live view's own port never answered the "are you there?" check. Without fixing that, *every* machine would have quietly dropped to the shared address and lost the separation between panels. There's now a test for both directions.
>
> **Proof:** `make test` passes. Three browser tests with every other port blocked: the live view falls back to the path and the picture actually connects. One more on a normal machine: it still picks its own port.
>
> **One thing to check at release time:** the new answer's note says "Needs swe-swe 2.39.0 or newer". I guessed the next version number. If the bump differs, change it in `www/index.html`.
>
> **Next step (2 minutes):** reply "reboot" and I'll restart the stack so you can click through it.

[Quick replies]
- reboot
- Show me the setup page screenshots
- Not yet
- Push to origin

**USER**

> Btw I’m thinking to have a --single-port flag that skips trying port and/or subdomains and just go straight to the path-based proxy fallback. Makes it easy to proper e2e test here? Discuss

<small>took 4.7s</small><br>
**AGENT**

> Checking what a --single-port flag would actually buy us for testing. About 4 minutes.

<small>took 1m 15s</small><br>
**AGENT**

> **My answer: build the flag — but for the port saving, not for testing. Keep today's test either way.**
>
> **1. What the flag actually buys (the real prize)**
> Today every session opens 4 extra doors on the machine (a "port" is a numbered door; each is reachable from outside). With 20 sessions set aside, that's 80 open doors that exist only to be reached from outside. On a one-port machine nobody can reach them, so the flag would stop opening them at all. Fewer open doors is also less to attack.
>
> **2. Second thing it buys**
> Some firewalls don't refuse a connection, they just stay silent. swe-swe waits up to 5 seconds per panel before giving up and using the shared address. The flag skips the waiting entirely.
>
> **3. Why it doesn't replace today's test**
> Today's test blocks every other port at the browser and checks swe-swe *finds* its way to the shared address on its own. That's the case almost everyone will be in, because almost nobody will set a flag. A test run *with* the flag tests the flag, not the finding.
>
> **4. What it does make possible (and this is worth having)**
> Run the *whole* test suite against a machine set up that way, instead of one small test. And check that the extra doors really aren't open — today's test can't see that, it only blocks the browser's side.
>
> **5. One snag I found while checking**
> Three of the panels already cope with being given no port; they're built that way. Agent View decides "does this session even have a live view?" by looking at the very port number we'd stop handing out, so it would vanish instead of falling back. There's already a separate yes/no signal it should use instead. Small fix, but it has to be part of the work.
>
> **Also:** the setup page answer I just shipped gives the plain script. With the flag, that answer should hand out the flag instead, so that part changes too.
>
> **Time:** about half a day for the flag, the Agent View fix and the page. Another 1.5 hours for the extra test mode.
>
> **Next step (2 minutes):** reply "plan it" and I'll write the step-by-step plan. That takes about 20 minutes.

[Quick replies]
- plan it
- Just the flag, skip the extra test mode
- Reboot first, I want to see it live
- Not now

**USER**

> plan it

<small>took 3.0s</small><br>
**AGENT**

> Writing the plan for the single-port flag. About 20 minutes. Reading the port allocation and flag parsing code first.

<small>took 17.8s</small><br>
**AGENT**

> Read the server's flag list and port allocation. Now checking how `swe-swe up` passes settings through, then writing the plan file.

<small>took 43.0s</small><br>
**AGENT**

> Checks done: three panels need no change, Agent View needs one. Writing the plan file now.

<small>took 1m 13s</small><br>
**AGENT**

> **Plan written: `tasks/2026-09-22-single-port-mode-flag.md`** (committed, `16a4ba0b6`).
>
> **The four phases, about 5 hours total:**
>
> 1. **Server, 1.5h.** A setting you turn on once. swe-swe then stops opening the 80 extra doors, stops mentioning them to the page, and refuses to start if you combine it with wildcard subdomains or the tunnel (both need those doors).
> 2. **Page, 1h.** Agent View currently works out "does this session have a live view?" by looking at the very port number we'd stop handing out, so it would vanish. It gets the proper yes/no signal instead, which already exists.
> 3. **Setup page, 1h.** The "one fixed port" answer hands out the setting, instead of the plain script it gives today.
> 4. **Testing, 1.5h.** A fourth test setup that's told it has one port, so the whole test suite runs against it, plus the one check today's test can't make: that the extra doors really aren't open.
>
> **Two things I checked that shaped the plan:**
> - Three of the four panels need no change at all. They already treat "no port given" as "use the shared address".
> - The setup page answer I shipped an hour ago will change: its script stops being identical to the plain one. Its test changes with it.
>
> **One thing I deliberately left out:** guessing that a machine is single-port. The existing check already does that; the flag is for skipping the waiting, not for detecting.
>
> **Next step (2 minutes):** reply "start" and I'll begin Phase 1, writing the failing tests first.

[Quick replies]
- start
- Reboot first, I want to see today's work live
- Change a phase
- Not yet

**USER**

> /clear-and-then start

<small>took 12.8s</small><br>
**AGENT**

> Starting Phase 1 of 4: the server stops opening the extra doors. Failing tests first. About 1.5 hours.

<small>took 14.8s</small><br>
**AGENT**

> Phase 1, step 1 of 5: read the server's port-opening code and status message. Writing the failing tests next.

<small>took 30.4s</small><br>
**AGENT**

> Phase 1, step 1 of 5: checked how the "public app" port works -- nothing actually opens it, so there may be nothing to skip there. Writing the failing tests now.

<small>took 4m 45s</small><br>
**AGENT**

> Phase 1, step 2 of 5: tests written and failing for the right reason. Making them pass now.

<small>took 45.4s</small><br>
**AGENT**

> Phase 1, step 3 of 5: the new tests pass and the server's own suite is green (12s, no failures). Running the full test suite now -- a few minutes.

<small>took 3m 48s</small><br>
**AGENT**

> Phase 1, step 4 of 5: full test suite green. Regenerating the golden files, then committing.

<small>took 1m 22s</small><br>
**AGENT**

> Phase 1 of 4 done and committed (5ca614016). Starting Phase 2: the Agent View tab must not vanish when its port is gone. About 1 hour.

<small>took 2m 18s</small><br>
**AGENT**

> Phase 2, step 3 of 6: found a second tab with the same flaw (Files), fixing both. Wiring the page now.

<small>took 2m 47s</small><br>
**AGENT**

> Phase 2, step 5 of 6: all unit tests pass (457 page tests, full Go suite). Regenerating goldens and committing.

<small>took 1m 6s</small><br>
**AGENT**

> Phase 2 of 4 done and committed (1bf32166b). Starting Phase 3: the setup page hands out the new setting. About 1 hour.

<small>took 1m 34s</small><br>
**AGENT**

> Phase 3, step 4 of 5: setup page and Docker wiring done, 8/8 page tests green. Running the full suite and regenerating goldens.

<small>took 3m 23s</small><br>
**AGENT**

> Phase 3 of 4 done and committed (9382a2968). Starting Phase 4: a test setup that is told it has one port. About 1.5 hours.

<small>took 2m 12s</small><br>
**AGENT**

> Phase 4, step 3 of 4: test setup and new checks written. Building the one-port test container now -- about 10 minutes.

<small>took 2m 3s</small><br>
**AGENT**

> Phase 4: the 3 new checks pass against the one-port container. Now running the whole suite against it -- 20 to 40 minutes.

<small>took 10m 21s</small><br>
**AGENT**

> Phase 4: full suite running against the one-port container. Watching for pass/fail lines; nothing to do until it finishes.

<small>took 4m 7s</small><br>
**AGENT**

> All 4 phases committed. Last check: running the ordinary (multi-port) test setup to prove the tab changes did not break it -- about 20 minutes.

<small>took 45.2s</small><br>
**AGENT**

> Ordinary-setup container is building. Results in roughly 15 minutes; nothing needed from you.

<small>took 20.8s</small><br>
**AGENT**

> Still building/running the ordinary setup. I'll report as soon as it reports.

<small>took 24.6s</small><br>
**AGENT**

> **All 4 phases done, 5 commits on `main` (unpushed).** One last check is still running; I'll report its result when it lands.
>
> 1. `5ca614016` — the server: a new setting (`SWE_SINGLE_PORT=1`). With it on, swe-swe opens none of the per-session ports and tells the page they don't exist. It refuses to start if you combine it with wildcard domains or the tunnel, which both reach the panes through those ports.
> 2. `1bf32166b` — the page: Agent View and Files no longer decide "do I exist?" from a port number, so their tabs don't vanish when the ports are gone.
> 3. `9382a2968` — the setup page: the "one fixed port" answer now hands out the setting, plus the `swe-swe init` line it needs.
> 4. `c5695d10d` — a test setup that is *told* it has one port, with only that port published. 71 tests passed against it.
> 5. `a399302de` — plan marked done.
>
> **What now works:** on a machine reachable on one port, swe-swe opens 0 extra ports instead of 80, and no pane spends up to 5 seconds discovering that.
>
> **Two things I found that the plan had wrong** (both fixed, both written into the plan file):
> 1. **Files would have lost its tab too.** The plan said Files needed no page changes. Its address-building copes fine, but three other places read the tab's existence off the port. Files now has its own signal.
> 2. **Skipping the "public app" port was a non-task.** Nothing ever opened it; it's just a number handed to your app. Left alone.
>
> **One decision left for you (2 minutes):** under Docker, the extra ports are still *forwarded* by Docker itself, because that list is written when you run `swe-swe init`, before this setting exists. Nothing listens behind them, so they refuse connections. Want `swe-swe init --single-port` to leave them out of the list entirely?

[Quick replies]
- Yes, add swe-swe init --single-port
- No, leave it
- Wait for the last check first
- Push to origin

<small>took 2m 18s</small><br>
**AGENT**

> Ordinary setup so far: the "prefers its own port" and fallback tests both pass, single-port tests correctly skip. Waiting on the final count.

**USER**

> Yes, add swe-swe init --single-port

<small>took 6.4s</small><br>
**AGENT**

> Status: all 4 phases committed (5 commits). Last check running now -- the ordinary multi-port test setup, 68 of ~90 tests passed so far, no failures caused by my changes. Draining messages next.

<small>took 17.2s</small><br>
**AGENT**

> Starting `swe-swe init --single-port`. Two steps: (1) the flag exists and is recorded, (2) it makes the setup stop forwarding the extra ports. About 1 hour.

<small>took 4m 59s</small><br>
**AGENT**

> Step 1 of 2 done: the flag exists and is remembered (6c844d1c1). Now making it stop forwarding the extra ports.

<small>took 4m 56s</small><br>
**AGENT**

> Step 2 of 2 in progress: Docker setup no longer forwards the extra ports. Wiring the same for the no-Docker setup, then the setup page.

<small>took 4m 32s</small><br>
**AGENT**

> Step 2 of 2 committed (ba8dd01ad). Now proving it for real: the test setup will use `swe-swe init --single-port` instead of a hand-edited file. About 15 minutes.

<small>took 42.3s</small><br>
**AGENT**

> Rebuilding the one-port container from the real `swe-swe init --single-port` path. Results in about 10 minutes.

<small>took 8.8s</small><br>
**AGENT**

> Still building. Nothing needed from you; I'll report the result.

<small>took 10.1s</small><br>
**AGENT**

> Build still going. Will report as soon as the checks run.

<small>took 12.6s</small><br>
**AGENT**

> Container image building (the slow part). No action needed.

<small>took 7.6s</small><br>
**AGENT**

> Waiting on the build. I'll message you with the result.

<small>took 6.6s</small><br>
**AGENT**

> Build running. Next message from me will have the result.

<small>took 13.6s</small><br>
**AGENT**

> Image built; container starting, then the 3 checks run.

<small>took 8.5s</small><br>
**AGENT**

> Checks about to run.

<small>took 13.0s</small><br>
**AGENT**

> Check 1 of 3 passed: the extra ports answer nothing. Two to go.

<small>took 18.2s</small><br>
**AGENT**

> Check 2 of 3 passed. Confirmed the real setup now forwards only one port (9790). Last check running.

<small>took 9.6s</small><br>
**AGENT**

> Last check (live browser view over the single port) in progress.

<small>took 15.4s</small><br>
**AGENT**

> Still on the last check.

<small>took 8.8s</small><br>
**AGENT**

> Waiting on the browser check.

<small>took 14.4s</small><br>
**AGENT**

> Last check is the slow one (it starts a real browser inside the container). Up to 10 more minutes.

<small>took 7.6s</small><br>
**AGENT**

> Still waiting on the browser check.

<small>took 20.1s</small><br>
**AGENT**

> Browser check underway inside the container. I'll report when it settles.

<small>took 9.2s</small><br>
**AGENT**

> Waiting.

<small>took 5m 14s</small><br>
**AGENT**

> Found it: my own test typed the command before the terminal was ready, so it went nowhere. Not a product bug -- fixing the test.

<small>took 2m 38s</small><br>
**AGENT**

> **Done. 8 commits on `main`, unpushed. Everything passes.**
>
> Your `swe-swe init --single-port` is in, and it's proven live: a container built from it forwards exactly one port (9790) and every panel still works through it.
>
> **What's committed**
> 1. `5ca614016` — the server stops opening the extra ports.
> 2. `1bf32166b` — the Files and live-view tabs no longer disappear when those ports are gone.
> 3. `9382a2968` + `ba8dd01ad` — the setup page's "one fixed port" answer now hands you `swe-swe init --single-port`.
> 4. `6c844d1c1` + `ba8dd01ad` — the new setting: the generated setup forwards one port and nothing else, and refuses to be combined with the tunnel.
> 5. `c5695d10d` + `b2ab72067` + `a399302de` — a test setup built the same way, plus the plan marked done.
>
> **Proof, not claims**
> 1. Full test suite: passes.
> 2. One-port container: 3 checks pass, including the live browser view running over that single port.
> 3. Ordinary multi-port container: 76 tests pass — the tab changes broke nothing.
>
> **One thing I got wrong and fixed:** two of those one-port runs failed for 3 minutes each and looked like a product fault. It was my test typing a command into the terminal before the terminal was ready. Fixed in `b2ab72067`; the note is in the plan file so it isn't re-learned later.
>
> **Next step (2 minutes):** reply "reboot" and I'll restart the stack so you can click through it. Or "push" to send all 8 commits to origin.

[Quick replies]
- reboot
- push
- Show me the setup page
- Not yet

<small>took 19m 11s</small><br>
**AGENT**

> Ignore a stale "SOME TESTS FAILED" line if you see one -- it's an old log replay from before the fix. The one-port checks were re-run after `b2ab72067` and pass 3 of 3. Nothing changed; your reply to the previous message is still what I'm waiting on.

<small>took 5.6s</small><br>
**AGENT**

> Waiting on you. 8 commits on `main`, unpushed, all checks green.
>
> Next step (2 minutes): pick one.

[Quick replies]
- reboot so I can click through it
- push to origin
- Show me the setup page
- Nothing for now

**USER**

> /commit-log-then-end

<small>took 6.1s</small><br>
**AGENT**

> Saving this session's chat log, then ending the session.

