---
description: Title this session's chat log, scrub sensitive information, and git commit it (with its assets) alone
---

# Commit Session Chat Log

This session's conversation is auto-archived at `./agent-chats/YYYY-MM-DD-NN-{slug}.md`.

1. **Title**: if the log is still `...-untitled.md`, call the `set_chat_title` tool with a short descriptive title -- the exporter renames the file and rewrites its header.
2. **Scrub**: read the log and redact sensitive values -- credentials, tokens, one-time codes, private URLs or hostnames, personal data -- replacing each value with `[REDACTED]`. Also check the image assets the log references; if a screenshot leaks a secret, do not commit it -- surface it to the user instead.
3. **Commit it alone**: stage ONLY this session's log file plus the `agent-chats/assets/` files it references (otherwise the log renders with broken images). Never `git add -A` or `git add .`. Verify with `git diff --cached --name-only` that nothing else is staged, then commit as `docs(agent-chats): <short title>`. Do not push.

The log keeps streaming after the commit; an uncommitted tail afterwards is normal.

Report the result with `send_message`: the title, what was redacted (by kind, never by value), and the commit hash.
