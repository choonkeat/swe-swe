---
description: Title, scrub, and commit agent-chats/ chat logs -- current session plus backlog from ended sessions
---

# Export Chat Logs

Chat sessions stream their conversation into `./agent-chats/YYYY-MM-DD-NN-{slug}.md` (plus `assets/` files and a regenerated `index.html`) as the chat progresses. This command reconciles those files into git: make sure logs are titled, scrubbed of sensitive content, and committed. Report progress with `send_progress` and the final result with `send_message`. Never push -- pushing is the user's decision.

## 1. Title the current session's log

If this session's log is still `...-untitled.md`, call the `set_chat_title` tool with a short descriptive title (e.g. "Auth bug fix"). The exporter renames the file and rewrites its header -- nothing else to do for the current session.

## 2. Title backlog logs from ended sessions

`git status --short agent-chats/` lists untracked and modified logs. For each remaining `-untitled.md` file:

- **Skip it if its session may still be live.** Check `list_sessions` if available (a busy session is still streaming, and renaming under it fights the exporter). If you cannot tell, use mtime: leave files modified within the last hour alone and say so in the report.
- Otherwise read the log, derive a short title, and retitle it by hand to match what `set_chat_title` would have produced:
  - rename the file to `YYYY-MM-DD-NN-{new-slug}.md` (keep the date and `NN`),
  - update `title:` and `slug:` in the `<!-- agent-chat export ... -->` header comment and the `# H1` line,
  - update the matching entry in the `const MANIFEST` array in `agent-chats/index.html` (`md:` path and `title:`).
- Asset files (`agent-chats/assets/YYYY-MM-DD-NN-*`) are keyed by date + `NN`, not slug -- no renames needed.

## 3. Scrub sensitive content

Read every log about to be committed and redact anything unsafe for this repo's audience: credentials, tokens, API keys, passwords, private URLs and internal hostnames, personal data. Replace only the sensitive span with `[REDACTED]`, keeping the surrounding conversation intact. Also check image assets referenced by those logs -- screenshots can leak secrets; if one does, do not commit it and surface it to the user instead. If you are unsure whether something is safe, ask the user before committing.

## 4. Commit by explicit path

Never `git add -A` or `git add .` -- other sessions may be writing concurrently. Stage exactly the reconciled files:

```
git status --short agent-chats/     # confirm what changed
git add -- agent-chats/<file>.md agent-chats/index.html agent-chats/assets/<...>
git diff --cached --name-only       # verify nothing else rode in
git commit -m "docs(agent-chats): <short summary>" -- <same explicit paths>
```

The current session's log keeps streaming after the commit -- an uncommitted tail is expected, and a later run picks it up. Never delete or rewrite entries for other sessions.

If a later merge or cherry-pick conflicts on the `MANIFEST` array in `index.html`, keep BOTH entries (newest first). Duplicate date + `NN` labels are cosmetic; only an exact `.md` filepath collision needs an `NN` bump on the newer file.

## 5. Report

Summarize via `send_message`: which files were titled or renamed, what was redacted (by kind, never by value), which files were skipped as possibly-live, and the commit hash. Do not push.
