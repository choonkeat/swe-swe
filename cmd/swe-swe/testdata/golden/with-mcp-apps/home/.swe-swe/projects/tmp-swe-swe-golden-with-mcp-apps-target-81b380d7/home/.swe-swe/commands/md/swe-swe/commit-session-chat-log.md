---
description: Freeze this session's chat log (chatlog_close), scrub sensitive information, and git commit it (with its assets) alone
---

# Commit Session Chat Log

This session's conversation is auto-archived at `./agent-chats/YYYY-MM-DD-NN-{slug}.md`.

1. **Close**: call the `chatlog_close` tool -- it freezes this session's log so the commit stays clean (no further appends), regenerates `index.html` one last time, and returns the exact paths to stage. If the log is still untitled, pass a short descriptive `title` in the same call; an already-titled log is never renamed here.
2. **Scrub**: read the frozen log and redact sensitive values -- credentials, tokens, one-time codes, private URLs or hostnames, personal data -- replacing each value with `[REDACTED]`. Also check the image assets the log references; if a screenshot leaks a secret, do not commit it -- surface it to the user instead.
3. **Commit it alone**: stage ONLY the paths returned by `chatlog_close`. Never `git add -A` or `git add .`. Verify with `git diff --cached --name-only` that nothing else is staged, then commit as `docs(agent-chats): <short title>`. Do not push.

Closing loses nothing: the JSONL event log keeps recording, and `set_chat_title` re-opens the export with a full-history backfill. But that backfill REWRITES the file from unredacted history -- do not re-open a scrubbed, committed log; if you must, re-scrub before re-committing.

Report the result with `send_message`: the title, what was redacted (by kind, never by value), and the commit hash.
