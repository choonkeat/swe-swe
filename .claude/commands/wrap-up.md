---
description: End-of-session wrap-up -- commit this session's work, then scrub and commit this session's chat log
---

# Wrap Up This Session

1. **Commit the work**: review `git status` for changes THIS session made and
   commit anything still pending. Other sessions share this checkout, so never
   `git add -A` -- stage by explicit path, verify with
   `git diff --cached --name-only` immediately before each commit, and leave
   other sessions' files (including their `agent-chats/` logs) alone. Group
   related changes into coherent conventional commits. Do not push.
2. **Chat log**: invoke the `/swe-swe:commit-session-chat-log` command (via the
   Skill tool) -- it freezes this session's chat log (`chatlog_close`), scrubs
   sensitive values from the log and its referenced assets, and commits it
   alone.
3. **Report** with `send_message`: each work commit (hash + one-liner) and the
   chat-log commit hash.
