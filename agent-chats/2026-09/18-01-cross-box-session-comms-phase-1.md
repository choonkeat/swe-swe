<!-- agent-chat export
title: Cross Box Session Comms Phase 1
date: 2026-09-18
index: 01
slug: cross-box-session-comms-phase-1
session: 702862a83046830e
agent: claude
version: 0.13.1 (02e72ab)
-->

# Cross Box Session Comms Phase 1

_2026-09-18 · 01 · claude · agent-chat 0.13.1 (02e72ab)_

**USER**

> While we have mcp that helps sessions coordinate with each other, when I have multiple machines, I’m reminded of the lesson learnt by Erlang
> - we solved distributed computing first and turns out concurrency comes for free
> - the rest of the world solves concurrency and grapple with no good solution for distributed computing 
>
> I think doing swe-swe session comms solves “concurrency” when i should’ve looked to solve “distributed computing”

<small>took 12.0s</small><br>
**AGENT**

> Checking how the current session-to-session tools are wired (what is machine-local vs. what could cross machines), then writing the assessment.

<small>took 42.2s</small><br>
**AGENT**

> Decide one thing: should a session's full address be `<box unique>/<session uuid>`? That decision is the whole pivot. Details below.
>
> ## Your diagnosis is right, and here is the concrete reason
>
> Every one of the 12 coordination tools (list_sessions, create_session, send_chat_message, and the rest) assumes it lives inside one server process on one machine:
>
> 1. The session list is a table in that server's memory. There is no notion of a session that lives elsewhere.
> 2. Each session is reached by a port number on localhost (a "port" is a numbered door on the same machine). A port number means nothing on another machine.
> 3. `repo_path` and worktrees are folder paths on the local disk.
> 4. The key that proves which session is calling is minted by, and only checked by, that same local server.
> 5. Delivery is assumed instant and always successful. No tool has a concept of "the other side is unreachable right now".
>
> So today it is 12 ways to talk within one machine, and zero ways to name or reach a session on a second machine.
>
> ## Why the Erlang direction gives you the local case for free
>
> If a message is defined as "deliver to (machine, session), and the other side may be slow, gone, or unreachable", then the same-machine case is simply the one where machine equals yourself. Nothing extra to build. Going the other way does not work: the local-only design silently baked in shared memory, shared disk, and instant delivery, and none of those survive a second box.
>
> ## What this would actually cost
>
> Do not rebuild the tools. Re-address them.
>
> 1. Give every session a fully qualified address: box unique plus session uuid. About a day.
> 2. Make list_sessions, send_chat_message, and get_chat_history accept that address, and have the local server forward the call to the other box when the unique is not itself. About two to three days. The tunnel daemon (swe-swe-tunneld, the relay each box already connects to) is the obvious carrier, but it is currently inbound-only and one unique per box, so it needs a box-to-box path.
> 3. Make "unreachable" a normal answer, not an error the tool never expected.
>
> The hard part, which is a separate question: create_session and worktrees assume the repo is on the caller's disk. Cross-machine session creation means "which box has this repo" becomes a real question.
>
> Next step: reply yes or no on the address format above.

[Quick replies]
- Yes, unique/uuid
- No, different format
- Talk through the repo question first

**USER**

> Thinking if i can just point at a throwaway git repo to power this? Redis works but expensive hosting? swe-swe server then starts up with a configurable git repo as comms channel. Somehow decide how & when to git push. Can even introduce “history of changes since you last look at the cluster” by way of git history ..
>
> Discuss

<small>took 25.8s</small><br>
**AGENT**

> Working through git-as-comms-channel: what works, what breaks, and the shape that avoids the breakage.

<small>took 17.7s</small><br>
**AGENT**

> Decide one thing: is a delay of a few seconds per message acceptable for box-to-box agent coordination? If yes, git works and is the cheapest option you have. If you need sub-second, it does not, and the rest of this is moot.
>
> ## Why git fits this specific problem
>
> 1. Hosting is already solved. Any private repo on GitHub or GitLab, or a bare repo on a box you own. Zero new infrastructure, zero monthly cost.
> 2. Credentials are already solved. Every swe-swe box already has git auth wired in. No new secrets to distribute.
> 3. Unreachable is the normal case, not an error. A box commits locally and pushes when it can. That is exactly the tolerance the Erlang point was about.
> 4. "What changed since I last looked" is native. Store the last commit id you processed; the answer is the diff from that id to the newest.
> 5. Debuggable by hand. `git log` is the audit trail. No special tooling to inspect the cluster.
>
> ## The three things that break, and the design that avoids each
>
> 1. Merge conflicts. Two boxes editing the same file will conflict. Fix: each box writes only inside its own folder, named by its unique. Nobody else ever writes there. Then merges always succeed automatically.
> 2. Push races. Even without conflicts, a push is rejected if someone else pushed first. Fix: every push is a loop of fetch, replay your commits on top, push, retry. This is a standard loop, about 20 lines.
> 3. Repo growth. One commit per message, forever. Fix: each box uses a shallow clone (only the recent history is downloaded), and a nightly job on one box squashes history older than 30 days.
>
> ## The shape
>
> One folder per box. Inside it, two kinds of files:
>
> - `sessions.json`, overwritten every heartbeat. This replaces list_sessions across boxes.
> - `outbox/<timestamp>-<to-box>-<to-session>.json`, one file per message, append-only. A box delivers to itself by scanning every other box's outbox for files addressed to it, and remembers the last commit id it scanned.
>
> When to push: immediately after writing a message, plus a heartbeat push every 30 seconds for the session list.
> When to pull: on a timer, and always right before pushing (the race loop does this anyway).
>
> ## Honest latency
>
> Send: commit plus push, 1 to 3 seconds to GitHub.
> Receive: whatever the pull timer is, 5 to 15 seconds is the sane range. Faster than 5 seconds across many boxes will trip GitHub abuse detection eventually.
> End to end: roughly 5 to 20 seconds.
>
> That is fine for "agent on box A hands a task to box B". It is not fine for anything a human is watching in real time.
>
> Next step: reply with the delay verdict above.
>
> Separately: swe-swe-tunneld is a relay you already host and could carry messages in real time. Compare that against git next?

[Quick replies]
- Seconds is fine, go with git
- Need real-time, compare tunneld
- Question about the shape first

**USER**

> Not necessarily git. Suggest. Also, if git, how does 1 distributed node session look at another distributed node session? Long wait? Swe-swe server polls git repo to death? Callback not possible.. a free Postgres db like neon?

<small>took 1m 7s</small><br>
**AGENT**

> Recommendation: use swe-swe-tunneld as the relay. Not git, not Postgres. Reason and ranking below.
>
> ## Why tunneld wins
>
> I checked its source. Every box already holds one persistent, multiplexed connection to tunneld (multiplexed = many independent conversations over one connection), registered under the box's unique. Tunneld already uses that to push inbound web requests into the box. So "box A asks box B something" is a small addition:
>
> 1. Box A sends a normal request to tunneld with a header naming the target unique.
> 2. Tunneld looks up B in its registry and opens a fresh conversation into B over the existing connection.
> 3. B's swe-swe server answers from memory, exactly as it does today for a local caller.
>
> No polling. No new hosting. No new credentials, since tunneld already knows each box's key. If B is not connected, tunneld answers "unreachable" immediately, which is the Erlang semantic you wanted.
>
> ## How one node's session looks at another node's session
>
> list_sessions with a target unique becomes: local server, one hop to tunneld, one hop into the target box, answer. Same tool, same answer shape.
>
> | Option | Round trip | Wake-up on new message |
> |---|---|---|
> | tunneld relay | 100 to 300 ms | Yes, instant |
> | Postgres (Neon) | 100 to 500 ms | Yes, but see below |
> | Git | 5 to 20 s | No, poll only |
>
> ## Why not Postgres
>
> Neon and Supabase do offer instant wake-up (Postgres LISTEN/NOTIFY, a built-in "tell me when something changes"). Two catches. It only works on a connection held open permanently, and on Neon's free tier a held connection keeps the database awake, which spends the monthly free hours in about a week. Through their connection pooler it does not work at all. Plus you put a database password on every box and depend on a third party's free-tier terms.
>
> ## Why not git alone
>
> It is poll-only. Not "polled to death": one lightweight check every 10 seconds from 5 boxes is about 43,000 small requests a day, which GitHub tolerates. But every answer is stale by up to the poll interval, and there is no way to be woken. Git is worth keeping for one thing only: if you want a durable "what changed in the cluster since I last looked" history. Then tunneld carries the wake-up and git carries the record. Skip it otherwise.
>
> ## Cost
>
> About 2 to 3 days: tunneld gains one forwarding verb, swe-swe server gains "if target unique is not me, forward". Repo and worktree location for cross-box create_session stays a separate question.
>
> Next step: reply "tunneld" and I will write the plan file.

[Quick replies]
- tunneld, write the plan
- tunneld plus git history
- Question first

**USER**

> Is that scope creep to swe-swe-tunnel? Does it mean without adding swe-swe-tunnel we have single-instance swe-swe comms, when connected we transparently get distributed swe-swe comms?

<small>took 16.4s</small><br>
**AGENT**

> Answering both: whether this is creep for tunneld, and whether the no-tunnel case stays purely local.

<small>took 12.3s</small><br>
**AGENT**

> Yes to the second question, and that shape is exactly right. On the first: not creep if one rule is kept. Details below.
>
> ## 1. Is it scope creep for swe-swe-tunnel?
>
> Tunneld's job today: authenticate a box by its key, then route a request to that box by its unique. Box-to-box forwarding is the same routing step with a different sender: an authenticated box instead of the public internet. The registry lookup and the stream-open code already exist.
>
> What is genuinely new is one policy question, not transport: may box A talk to box B? On a tunneld serving only your own boxes, the answer is "any box on this tunneld". On a tunneld shared by several people it must be "same owner only". That rule is the whole addition, about 50 lines plus tests.
>
> It becomes creep only if tunneld starts holding messages, queues, or history. Keep it stateless: forward or answer "unreachable", nothing else. Then it stays a router.
>
> ## 2. No tunnel means local only, tunnel means distributed, transparently?
>
> Yes. Every session gets the address `<unique>/<uuid>`. The server resolves it in one branch:
>
> 1. Unique is me, or absent: answer from memory. This is today's code, untouched.
> 2. Unique is someone else and a tunnel is configured: forward through the tunnel.
> 3. Unique is someone else and no tunnel: answer "unreachable".
>
> Same 12 tools, same answer shapes. A box with no tunnel is simply a cluster of one. Local is the special case of distributed, which was the Erlang point.
>
> One honest limit: "transparent" holds for the 9 tools that read or message a session. It does not hold for create_session, prepare_repo, and list_worktrees, because they touch a disk. Those work remotely only as "create a session on box B using a path that exists on box B". The caller has to know B's paths.
>
> Next step: confirm "stateless tunneld, same-owner rule" and I write the plan file under tasks/.

[Quick replies]
- Confirmed, write the plan
- Change something first
- Hold, not now

**USER**

> Small change to swe-swe session comms (regardless of underlying mechanism tunnel or not) to pass along “from” session address. And we change the message payload to always carry a `\n\n(via <mcp tool name> @ <unique>/<uuid>)` signoff; allowing receiving agent to reply
>
> Other suggestions for underlying mechanism?

<small>took 21.7s</small><br>
**AGENT**

> Assessing the "from" signoff (agrees, with one tweak) and ranking four more transport options.

<small>took 11.9s</small><br>
**AGENT**

> Agree on the signoff, with one tweak: the server stamps it, not the agent. Then four more transport options, ranked.
>
> ## The "from" signoff
>
> The server already knows which session is calling, because every request carries that session's private key. So it should append the signoff itself. Two reasons:
>
> 1. It cannot be forged. An agent cannot claim to be a different session.
> 2. Every agent gets it right on every message, including agents that never read the instructions.
>
> Suggested wording, so the receiver knows how to answer without guessing:
>
> ```
> (via send_chat_message from <unique>/<uuid>; reply with send_chat_message to that address)
> ```
>
> This also means send_chat_message and friends must accept `<unique>/<uuid>` as the target. A bare uuid keeps meaning "on this box". That change is mechanism-independent and is step 1 of any plan, so it can ship before the transport is chosen.
>
> ## Other transport options, ranked by fit
>
> 1. Self-hosted NATS. One small binary. Built-in request-reply and, with its storage feature, durable history per subject. Best semantic fit of anything here. Cost: one more process to host and keep up. Pick this only if you want cluster history.
> 2. Slack or Discord channel as the bus. Free, push, about 1 second latency, and you can read the cluster chatter yourself. Cost: a third party, message size limits, and rate limits. Pick this if human visibility of agent-to-agent traffic matters more than latency.
> 3. Cloudflare Worker with a Durable Object. Free tier, real-time, roughly 150 lines. Cost: new code in a new place, and a Cloudflare dependency.
> 4. Direct box-to-box over a private network (WireGuard or similar). Zero relay code, each box just calls the other's address. Cost: a mesh network on every box, which you removed from the stack in July.
>
> Tunneld still wins for the plain case: no new hosting, no new secrets, real-time, and already connected. NATS is the upgrade path if history becomes a requirement.
>
> Next step: confirm "server-stamped signoff, tunneld transport" and I write the plan file with the signoff as step 1.

[Quick replies]
- Confirmed, write the plan
- Signoff only for now, no transport yet
- Change something first

**USER**

> Server stamped `(via send_chat_message from <unique>/<uuid>)` suffice. No need too verbose.
>
> > write the plan
>
> What repo need what changes? Where’s the plan? I’d prefer swe-swe-tunnel agent make its own changes not us doing it from here without their context

<small>took 18.7s</small><br>
**AGENT**

> Reading the server's tunnel wiring and the six session-addressed tools, then writing tasks/2026-09-18-cross-box-session-comms.md. About 10 minutes.

<small>took 25.8s</small><br>
**AGENT**

> Tunneld side read (ed25519 identity per box, /v1/connect handshake, host-based routing). Writing the plan file now.

<small>took 2m 17s</small><br>
**AGENT**

> Plan written. Two files, one per repo's concern:
>
> 1. `tasks/2026-09-18-cross-box-session-comms.md` (swe-swe, 151 lines). Four phases, files and line numbers named.
> 2. `tasks/2026-09-18-cross-box-relay-tunneld-contract.md` (76 lines). The interface swe-swe will code against. Meant to be handed to a session in /repos/swe-swe-tunnel. Internals are left to that agent.
>
> ## What changes where
>
> **swe-swe (this repo), no tunnel needed, ships first.** Phase 1: `<unique>/<uuid>` address parsing, the 6 session-addressed tools accept it, list_sessions reports each session's address, and the server stamps `(via send_chat_message from <unique>/<uuid>)` from the caller's auth-key identity. Foreign unique returns "unreachable" until phase 3. About one day.
>
> **swe-swe-tunnel, its own agent.** Phase 2: the client binary gets a loopback relay listener that turns each request into a stream on the existing connection. Tunneld accepts those streams, stamps the caller's unique into a header it strips from all public traffic, and hands the request to its existing routing with the target set to a relay-only port. Not connected returns 503 "unreachable". Policy flag off/all/owner. About one to two days.
>
> **swe-swe again, needs phase 2 binaries.** Phase 3: server binds a loopback-only relay port, learns the client's relay address from the supervisor's event stream, and forwards non-local addresses through it. Two-server e2e plus a live proof against the DO droplet, which works without the DNS blocker because relay never needs per-unique DNS. About one day.
>
> Phase 4: tunnel docs section and one-sentence CHANGELOG.
>
> Next step: reply "spawn" and I create a swe-swe-tunnel session pointed at the contract file, or "phase 1" and I start the swe-swe side now.

[Quick replies]
- phase 1
- spawn
- both
- Change the plan first

**USER**

> Is <unique> the same <unique> value used for tunneld? When `send_chat_message` is called on <unique>, do we actually call the mcp endpoint at <unique> Instance — how much reuse (best) how much totally new path?

<small>took 18.8s</small><br>
**AGENT**

> Checking whether the target's existing /mcp handler can be reused as-is on the relay listener, and what MCP client code already exists in the server.

<small>took 35.8s</small><br>
**AGENT**

> Yes to both. Same unique, and the call lands on the target box's real MCP endpoint. Plan updated to say so (phase 3, items 1 and 3).
>
> ## 1. Same unique?
>
> Yes. It is SWE_TUNNEL_UNIQUE, the value the box registers with tunneld. Tunneld stores it internally as `<unique>-tunnel`; the address uses the bare unique.
>
> ## 2. Does send_chat_message on `<unique>/<uuid>` call the MCP endpoint on that instance?
>
> Yes, the identical handler. Two facts make this nearly free:
>
> 1. The server's `/mcp` handler is stateless streamable HTTP. No handshake per call. It can be mounted a second time, on the loopback relay port, unchanged.
> 2. The server already talks MCP as a client today. It sends a hand-rolled JSON-RPC tools/call POST to each session's agent-chat. Forwarding is that same 30-line function pointed at the relay listener with a target header.
>
> So a remote box can call exactly the tools a local agent can, no new tool surface.
>
> ## Reuse versus new
>
> Reused as-is: the `/mcp` handler and all tool implementations, the JSON-RPC caller, the box's existing tunnel connection and identity, tunneld's routing and port policy, the supervisor's event stream.
>
> New, in swe-swe:
> 1. Address parse and the one resolve branch, about 60 lines plus tests.
> 2. Second loopback listener with a relay auth middleware that trusts tunneld's stamp instead of a session key, about 40 lines.
> 3. Generalize the JSON-RPC caller to take a URL and headers, about 15 lines.
> 4. Signoff stamp, about 5 lines.
>
> New, in swe-swe-tunnel: client relay listener, tunneld accepting client-initiated streams, the stamp and strip, the policy flag. That is the one genuinely new path, and it is the phase 2 contract.
>
> Next step: reply "phase 1" to start the swe-swe side, or "spawn" for the tunnel session.

[Quick replies]
- phase 1
- spawn
- both
- Question first

**USER**

> phase 1

<small>took 12.9s</small><br>
**AGENT**

> Phase 1 started, step 1 of 6: creating a worktree on branch feat/cross-box-addresses and reading the tunnel runtime + session snapshot code.

<small>took 57.3s</small><br>
**AGENT**

> Step 2 of 6: writing session_address.go (address parse, local-unique lookup, signoff) and wiring the 6 tools in main.go.

<small>took 1m 8s</small><br>
**AGENT**

> Step 3 of 6: code wired. Compiling and running the server template tests (make test-server), about 3 minutes.

<small>took 37.9s</small><br>
**AGENT**

> Step 4 of 6: unit tests pass and the server compiles. Golden files regenerating in the background; meanwhile adding a wire-level test that the signoff actually reaches the target session's chat.

<small>took 48.5s</small><br>
**AGENT**

> Golden files regenerated (only main.go + the new file across 49 variants). Writing the wire-level signoff test, then running the full make test.

<small>took 1m 4s</small><br>
**AGENT**

> Step 5 of 6: all 8 new tests pass (address parse, unreachable on every session tool, list_sessions address, and the signoff proven on the wire: "hello\n\n(via send_chat_message from here/sender-1)"). Full make test running now, about 5 to 8 minutes. Commit follows on green.

<small>took 2m 7s</small><br>
**AGENT**

> Full suite: one failure, TestApplyEvent_RegisterOK_SetsLiveHostname in the tunnel supervisor tests. Log shows another test's supervisor child racing it. Re-running that file in isolation to tell pre-existing flake from a real break.

<small>took 1m 25s</small><br>
**AGENT**

> The tunnel test failure is a pre-existing race between two supervisor tests (another test's child was still exiting inside it); it passes in isolation on both main and my branch, and the full server package passed twice in a row on my branch. Re-running the complete make test for an honest green, about 8 minutes.

<small>took 1m 12s</small><br>
**AGENT**

> Phase 1 done: commit 3e8a3b8b1 on branch feat/cross-box-addresses (worktree /repos/worktrees/swe-swe-cross-box). Full make test green. Not merged to main.
>
> What now works:
> 1. Every session-addressed tool accepts `<unique>/<uuid>` or a bare uuid: end_session, set_session_name, get_session_output, send_session_input, send_chat_message, get_chat_history. A foreign unique answers "unreachable: <unique> (no relay configured on this box)".
> 2. list_sessions reports each session's `address`.
> 3. send_chat_message is server-stamped. Proven on the wire in a test: "hello" arrives as "hello\n\n(via send_chat_message from here/sender-1)", identity taken from the caller's auth key, applied to local sends too.
>
> Tests: 8 new (address parse, unreachable across all 6 tools, list_sessions address, wire-level signoff). Golden: main.go plus the new file across 49 variants, nothing else.
>
> One note: TestApplyEvent_RegisterOK_SetsLiveHostname failed once in the first full run. It is a pre-existing race between two tunnel supervisor tests (another test's child was still exiting inside it). It passes in isolation on main and on the branch, and the full suite passed clean on rerun. Not caused by this change.
>
> To see it live: reboot after merge, then from any session call list_sessions and send_chat_message with the address it shows.
>
> Next step: reply "merge" and I no-ff merge the branch into main (about 2 minutes).
>
> Separately: spawn the swe-swe-tunnel session for phase 2 next?

[Quick replies]
- merge
- merge, then spawn tunnel session
- Hold, review the diff first

**USER**

> Tell session e80a54f8-b786-47cb-a93a-cfee9097fa41 to do it

<small>took 5.6s</small><br>
**AGENT**

> Checking what session e80a54f8 is (repo and branch), then handing it the phase 2 contract.

<small>took 36.3s</small><br>
**AGENT**

> Handoff sent to session e80a54f8 (choonkeat/swe-swe-tunnel@main, fresh, 1 browser attached). It has the full phase 2 contract inline plus the file paths, the "stateless, own branch, don't touch swe-swe" rules, and my session uuid to reply to when done or if the contract does not fit tunneld's internals.
>
> State: phase 1 committed on feat/cross-box-addresses (unmerged). Phase 2 now with the tunnel session. Phase 3 (swe-swe forwarding) waits on their binaries.
>
> Next step: reply "merge" and I no-ff merge phase 1 into main, about 2 minutes.

[Quick replies]
- merge
- Wait for the tunnel session first
- Review the diff first

**USER**

> merge

<small>took 4.0s</small><br>
**AGENT**

> Merging feat/cross-box-addresses into main with a no-ff merge from /workspace.

<small>took 32.0s</small><br>
**AGENT**

> Merged: main is now 18203a45b (no-ff merge of feat/cross-box-addresses). Worktree removed, branch kept. Main is not pushed, and the running server still has the old code until the next reboot.
>
> State:
> 1. Phase 1 (addresses + signoff): on main.
> 2. Phase 2 (tunnel relay): with session e80a54f8, will reply to this session when done.
> 3. Phase 3 (swe-swe forwarding): waits on phase 2 binaries.
>
> Next step: reply "reboot" to make phase 1 live now (about 3 minutes of downtime, all sessions end, including the tunnel session), or "wait" to reboot after phase 2 lands.

[Quick replies]
- wait
- reboot
- push main too

**USER**

> /commit-log-then-end

<small>took 6.9s</small><br>
**AGENT**

> Running /commit-log-then-end: freezing this session's chat log, scrubbing, committing it alone, then ending the session.

