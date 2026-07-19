---
description: Snapshot this session into .swe-swe/TODO.md (in the current worktree) so a future agent can resume from a clean handoff
---

# Save Session

Snapshot the current session into a handoff file so a future agent (or this one
after a restart) can resume cleanly.

**Location**: the handoff file is `<root>/.swe-swe/TODO.md` where `<root>` is
`git rev-parse --show-toplevel` run from the session's working directory. In a
worktree session that is the worktree's own root -- never the primary
workspace's, and never another checkout's `.swe-swe/`.

## Steps

### 1. Check for an existing TODO.md

If `<root>/.swe-swe/TODO.md` already exists, show its contents to the user via
`send_message` and ask whether to **overwrite**, **append**, or **abort**. Do
not silently clobber a prior handoff.

### 2. Gather what to save

Reflect on the current conversation and assemble:

- **Goal** -- one or two sentences: what is this session trying to accomplish?
- **Done** -- concrete things already completed (files written, commits made, decisions reached). Reference paths and commit SHAs where useful.
- **Pending / Next steps** -- an ordered checklist of what's left. Be specific enough that a fresh agent could pick up step 1 without re-deriving context.
- **Key files** -- paths the next agent will need to read first.
- **Gotchas** -- anything non-obvious: failed approaches, user preferences voiced this session, in-flight processes, dirty state, things to avoid.
- **How to verify** -- how the next agent can confirm work is complete (tests, commands, expected output).

### 3. Write the file

Use the Write tool on `<root>/.swe-swe/TODO.md`. Format as Markdown with the
sections above as headings. Keep it tight and skimmable -- this is a handoff
note, not a journal.

### 4. Confirm

Report to the user via `send_message`: the path written and a one-line summary
of what's pending. The file is a local handoff note -- `.swe-swe/` is
conventionally gitignored; if this repo does not ignore it, do not commit it.
