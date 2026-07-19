---
description: Discuss and resolve a GitHub PR / GitLab MR over chat using the prctx CLI, then flush replies/comments/verdict upstream
---

# Work a PR / MR with prctx

Drive a GitHub pull request or GitLab merge request conversationally: pull the
review context in, discuss and draft with the user over chat, then push
everything back upstream in one go. Uses the bundled `prctx` CLI (already on
`$PATH`).

`$ARGUMENTS` is a PR/MR url or number (a bare number resolves owner/repo from
the git `origin` remote). If empty, ask the user which PR/MR, or use the
last-fetched one.

## Auth

`prctx` reads `GH_TOKEN` (GitHub) / `GITLAB_TOKEN` (GitLab) from the env. In a
swe-swe session these are exported automatically from the token you saved in
Settings > Credentials > Git HTTPS for github.com / gitlab.com. If a call fails
with "GITHUB_TOKEN is not set" (or the GitLab equivalent), tell the user to save
the host token in that panel and reopen the session (env is injected at session
start).

## Workflow

### 1. Fetch and show

```bash
prctx fetch "$ARGUMENTS"
```

This pulls review threads + top-level notes into local state and prints them.
The code diff itself is the worktree (via git) -- read files directly for
context. Summarize the open threads for the user.

### 2. Discuss and stage (nothing is sent yet)

Everything below is local-only until `flush`. Address the review with the user,
then stage:

```bash
prctx reply   <thread-id> "your reply text"     # reply to an existing thread
prctx comment <file>:<line> "new comment text"  # new inline comment
prctx resolve <thread-id>                        # mark a thread resolved
prctx drop    <thread-id|draft-id>               # unstage something
prctx show                                       # review what is staged
```

Thread ids come from `prctx show` (e.g. GitHub `PRRT_...`). Make any actual code
changes as normal edits + commits on the branch -- that is plain git, not part
of `prctx`.

### 3. Flush and verdict (the only steps that write upstream)

Confirm with the user before flushing. Push code first if you made commits
(`prctx flush` warns and refuses if your local HEAD is ahead of the fetched PR
head; it never pushes for you).

```bash
prctx flush                    # post staged replies/comments/resolves (idempotent)
prctx approve                  # or: prctx reject   -- verdict is separate + atomic
```

Report back what was posted and the resulting PR/MR state.

## Rules

- Never `flush`/`approve`/`reject` without explicit user confirmation -- these
  are outward-facing writes as the user's own identity.
- Replies anchor to thread ids, not line numbers, so they survive code changes.
- `flush` is idempotent (posted items are stamped); a re-run after a partial
  failure resumes without double-posting.
