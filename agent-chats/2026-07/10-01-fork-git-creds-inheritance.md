<!-- agent-chat export
title: Fork Git Creds Inheritance
date: 2026-07-10
index: 01
slug: fork-git-creds-inheritance
agent: claude
version: 0.8.7 (2089b8b)
-->

# Fork Git Creds Inheritance

_2026-07-10 · 01 · claude · agent-chat 0.8.7 (2089b8b)_

**USER**

> bug: fork does not carry the git http, ssh signing, and env vars sync
> TDD repro and fix

<small>took 2m 24s</small><br>
**AGENT**

> Found it: `handleForkExecute` stages the forked session **without** `InheritCredsFrom`, so the spawn path never runs `inheritSessionCredentials`/`inheritSessionEnv`. A create_session child (and terminal-child) sets `InheritCredsFrom` and gets HTTPS creds + author + SSH signing + env; a fork gets only the browser localStorage env blob. Writing the TDD repro now.

<small>took 5m 59s</small><br>
**AGENT**

> Fixed: fork now carries git HTTPS creds + author + SSH signing key + repo env, via the same `InheritCredsFrom` inheritance MCP `create_session` uses.
>
> Root cause: `handleForkExecute` staged the fork **without** `InheritCredsFrom`, so `inheritSessionCredentials`/`inheritSessionEnv` never ran — fork got only the browser localStorage env blob and none of the git auth.
>
> TDD:
> • Red → new `fork_inherit_creds_test.go`: `TestForkSessionParamsInheritsFromSource` (params carry `InheritCredsFrom = source UUID`) + `TestForkInheritanceEndToEnd` (seeds PAT/author/SSH key/env on a source, replays spawn order, asserts all four land on the child incl. signed gitconfig).
> • Green → extracted `buildForkSessionParams()` in main.go setting `InheritCredsFrom: src.UUID`; spawn ordering preserved.
>
> Caveat (documented): active source → carries everything; fully-ended source → creds/signing stores are already cleared so it's a no-op, env still falls back to the localStorage blob (unchanged behavior).
>
> Verified: `make test-server` green, golden regenerated + `go test ./cmd/swe-swe` green, ascii-check clean.
>
> Heads-up: first golden-update got clobbered by a concurrent session racing on the shared `/tmp/swe-swe-golden` — restored from git index, no data lost, re-ran clean.
>
> Staged but not committed. Commit it?

[Quick replies]
- Commit it
- Don't commit yet
- Show me the main.go diff

**USER**

> Commit it

<small>took 2m 24s</small><br>
**AGENT**

> Committed — but with a twist worth flagging.
>
> While I was working, a **concurrent session's commit `a2258c5d3` (feat: session-ended "View recorded")** swept in my uncommitted fork fix (the `buildForkSessionParams` change in main.go + the regenerated golden) via the shared /workspace git index — the exact shared-checkout race we've hit before. It left my new **test file** behind untracked.
>
> So the state now:
> • `a2258c5d3` — already contains the actual fix (`InheritCredsFrom = source UUID`) + golden. I verified golden in HEAD is consistent (3 `buildForkSessionParams` hits) and the tests pass against it.
> • `164f31a27` (just committed) — my orphaned TDD tests, so the fix isn't left unguarded.
>
> Net result: the fork inheritance fix + its tests are both in `main` history now, just split across two commits (the fix rode in under a session-ended commit message rather than its own). Nothing lost, tests green.
>
> Want me to leave it as-is, or reword/reorganize history (e.g. amend the test commit message, or note the fork fix in a follow-up)? I'd lean toward leaving it — rewriting shared main history is riskier than the cosmetic gain.

[Quick replies]
- Leave it as-is
- Push main
- Reword the commits

**USER**

> Reword the commits

<small>took 5m 57s</small><br>
**AGENT**

> Reworded. New history (both unpushed):
>
> • f18672379  feat(session-ended): offer "View recorded" chat/terminal + rename action
> • 82c1c9c39  test(fork): lock in git creds/signing/env inheritance on fork
>
> What I did / didn't touch:
> - f18672379 is the concurrent session's commit — I kept its **tree byte-identical** to the original a2258c5d3 (verified IDENTICAL) and preserved its original message + Co-Authored-By trailer, only **appending** a paragraph noting it also bundles the fork fix (buildForkSessionParams / InheritCredsFrom) and that it landed there via the shared-index race. I did not rewrite anyone's actual feature content or claim credit for the session-ended work.
> - 82c1c9c39 is my test commit, reworded to read as "tests for the fix in the previous commit" instead of narrating the mess.
>
> Note: the fork **fix hunk itself** still physically lives inside the session-ended commit (can't cleanly split it out without hunk-splitting the intermixed main.go + ~40 golden files, which is fiddly and risky on a shared commit). The message now makes that explicit. If you'd rather I fully split it into a standalone fix(fork) commit, I can — just say so.
>
> Nothing pushed. Push main, or leave it?

[Quick replies]
- Leave it, don't push
- Push main
- Fully split out the fork fix

**USER**

> /ck:export-chat-logs

