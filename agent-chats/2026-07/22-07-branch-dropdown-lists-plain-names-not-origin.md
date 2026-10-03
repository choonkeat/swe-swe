<!-- agent-chat export
title: Branch Dropdown Lists Plain Names Not Origin
date: 2026-07-22
index: 07
slug: branch-dropdown-lists-plain-names-not-origin
session: 2d1753f6b0de05ba
agent: claude
version: 0.8.17 (a9ccfee)
-->

# Branch Dropdown Lists Plain Names Not Origin

_2026-07-22 · 07 · claude · agent-chat 0.8.17 (a9ccfee)_

**USER**

> bug: the branches dropdown in homepage > new session. It currently includes listing remote branches too, e.g. origin/abc, and if user chooses it, now there’s a local origin/abc branch and we get wholly confused.
>
> Discuss

<small>took 1m 36s</small><br>
**AGENT**

> Root cause is in `handleRepoBranchesAPI` — it shells out to `git branch -a` and returns remote refs verbatim (main.go:4306). Everything downstream inherits that.
>
> What I found (read the source, no container needed):
>
> 1. **The server already strips it — since 2 days ago.** `stripRemotePrefix` (main.go:4401-4430, commit a463eee46, 2026-07-20) turns `origin/abc` into `abc` before `git worktree add`. So on current main you get a proper local `abc` tracking `origin/abc`, not a local `origin/abc`. Before that commit you got a **detached HEAD**. Check what version the box you hit this on is running — if it predates 2.33.0, the bad-branch part is already fixed.
>
> 2. **But the session still records the raw string.** `BranchName: p.Branch` (main.go:5536, 5574) stores `origin/abc` while the worktree is actually `/worktrees/abc` on branch `abc`. That's the "wholly confused" you're seeing: card says `origin/abc`, git says `abc`, and a recording prefill re-offers `origin/abc`.
>
> 3. **The dropdown is duplicated by construction.** `git branch -a` yields both `main` and `origin/main`; the sort just pushes remotes to the bottom (main.go:4338-4346). Every branch appears twice, and one of the two is always the wrong pick.
>
> My recommendation — fix it at the API, not the UI:
>
> - Replace `git branch -a` with `git for-each-ref refs/heads refs/remotes`. Emit **bare names only**: all local branches, plus remote-only branches under their bare name. `origin/abc` becomes `abc`; `origin/main` disappears (dedup against local `main`). A repo that genuinely has `refs/heads/origin/x` keeps that name, because it came from `refs/heads`.
> - Keep `stripRemotePrefix` as belt-and-braces for old clients and hand-typed input.
> - Apply the strip to `p.Branch` before storing `BranchName`, so the card, the worktree, and git agree.
>
> Open question for you: when a branch is remote-only, do you want the dropdown to *say so* (`abc — from origin`, checkout will track it), or stay a flat list of plain names? The flat list is simpler and matches "just give me that branch"; the label is honest about the fetch that's about to happen.
>
> Next: tell me flat-vs-labelled and I'll write it — about 30 minutes including a `for-each-ref` unit test in `worktree_test.go`.

[Quick replies]
- Flat list of plain names
- Label remote-only ones
- Show me the diff first, don't write yet
- What version is the box running?

**USER**

> flat list is simpler

<small>took 7m 6s</small><br>
**AGENT**

> Done, tests green, uncommitted. `make test` passes; 4 new subtests cover the branch listing.
>
> What changed (`cmd/swe-swe/templates/host/swe-swe-server/main.go`):
>
> 1. **New `listBranchNames`** (main.go:4264) replaces `git branch -a` with `git for-each-ref refs/heads refs/remotes`. Emits plain names only: all locals, then remote-only branches under their bare name. `origin/main` is gone (deduped against local `main`); `origin/abc` is listed as `abc`.
> 2. **`p.Branch` normalized before it's recorded** (main.go:5282). `BranchName` used to store `origin/abc` while the worktree sat on `abc` — card and git now agree.
> 3. **`stripRemotePrefix` kept** as the guard for hand-typed input and old clients; only its stale comment changed.
>
> Edge cases pinned by tests (`worktree_test.go:2102`): hierarchical remote branch keeps its slashes (`feat/add-login`), a repo that genuinely has local `refs/heads/origin/weird` still lists it verbatim, a second remote with the same branch name doesn't duplicate the entry, and `origin/HEAD` is dropped.
>
> Golden files regenerated — 49 `main.go` variants, no other file touched. `agent-chats/index.html` is also dirty but that's the chat-log exporter, not me; I'll leave it out of any commit.
>
> Next: say the word and I'll commit these two source files plus the golden dir as `fix(new-session): list plain branch names, never origin/*`.

[Quick replies]
- Commit it
- Show me the main.go diff first
- Test it live in a container
- Leave it uncommitted

**USER**

> Test it live in a container need remote git repo with branches etc; easy to get? Otherwise we can commit and git push to remote branch and /reboot-light to test fully

<small>took 42.6s</small><br>
**AGENT**

> Yes, easy — no GitHub repo needed. A bare repo on disk (`git init --bare`, push a few branches) is a real remote as far as `refs/remotes/*` goes, which is all the dropdown reads. Bringing up `make e2e-up-simple` (port 9780) now and wiring one in; will report what the dropdown shows.

<small>took 4m 24s</small><br>
**AGENT**

> Verified live in a container — no GitHub repo needed, a bare repo on disk is a real remote.
>
> Setup: `git init --bare /tmp/fake-remote.git`, pushed `main`, `feature-a`, `feat/add-login`, `release-1.0` to it, kept `mine-local` local-only. That's the same `refs/remotes/*` shape a GitHub clone has.
>
> What the dropdown now shows (first screenshot): `main`, `mine-local`, `feat/add-login`, `feature-a`, `release-1.0`. Five entries, one per branch. No `origin/` anywhere, no duplicate `main`.
>
> Three things confirmed on top of that:
>
> 1. **Picking `feature-a` (remote-only) does the right thing.** Worktree at `/worktrees/feature-a` on local branch `feature-a` tracking `origin/feature-a`. No `origin/feature-a` local branch was created.
> 2. **Old-client path still safe.** POSTed `branch=origin/release-1.0` straight at `/api/session/new` — server logged `Branch "origin/release-1.0" is a remote-tracking ref; using local branch "release-1.0" instead` and made `/worktrees/release-1.0` tracking `origin/release-1.0`.
> 3. **Session card agrees with git** (second screenshot): the `origin/release-1.0` request shows `release-1.0` on the card. That's the `BranchName` fix — before, the card would have said `origin/release-1.0`.
>
> Stack torn down (`make e2e-down`). No reboot needed to prove this out; a `/reboot-light` is only worth it if you want the change running on this box.
>
> Next: say go and I'll commit the two source files + golden dir as `fix(new-session): list plain branch names, never origin/*`.
>
> <div style="display:flex;flex-wrap:wrap;gap:8px;">
> <a href="./assets/2026-07-22-07-1-1d574e9a31c0.png" style="flex:0 1 calc(33% - 8px);max-width:calc(33% - 8px);"><img src="./assets/2026-07-22-07-1-1d574e9a31c0.png" alt="branch-dropdown.png" style="width:100%;height:auto;display:block;border-radius:6px;"></a>
> <a href="./assets/2026-07-22-07-2-41cfcd9cd761.png" style="flex:0 1 calc(33% - 8px);max-width:calc(33% - 8px);"><img src="./assets/2026-07-22-07-2-41cfcd9cd761.png" alt="session-cards-2.png" style="width:100%;height:auto;display:block;border-radius:6px;"></a>
> </div>

[Quick replies]
- Commit it
- Commit and push
- Commit, push, then /reboot-light
- Leave it uncommitted

**USER**

> Commit, push, then /reboot-light

**USER**

> Oh remember to /wrap-up before reboot

