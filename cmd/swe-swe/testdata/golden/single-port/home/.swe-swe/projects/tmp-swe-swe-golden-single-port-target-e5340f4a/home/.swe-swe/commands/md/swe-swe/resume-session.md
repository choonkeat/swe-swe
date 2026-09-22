---
description: Resume a session saved with /swe-swe:save-session -- read .swe-swe/TODO.md, confirm the plan, then proceed
---

# Resume Session

Resume a session previously saved with `/swe-swe:save-session`. Reads the
handoff file, confirms the plan with the user, then proceeds.

**Location**: the handoff file is `<root>/.swe-swe/TODO.md` where `<root>` is
`git rev-parse --show-toplevel` run from the session's working directory. In a
worktree session that is the worktree's own root -- never the primary
workspace's, and never another checkout's `.swe-swe/`.

## Steps

### 1. Locate the handoff file

Check for `<root>/.swe-swe/TODO.md`. If it does not exist, tell the user "no
saved session found" via `send_message` and stop.

### 2. Detect a stale resume

If `<root>/.swe-swe/TODO.resumed.md` **also** exists, a prior resume was
started but never cleared. This is a warning sign -- the previous agent may
have begun the work and crashed, or may have completed it without housekeeping.

Show both files' contents to the user via `send_message` and ask:
- **Discard the old `TODO.resumed.md`** (assume it's stale, proceed with the fresh `TODO.md`), or
- **Use `TODO.resumed.md` instead** (continue what the prior agent started), or
- **Abort** and let the user clean up manually.

Do not proceed until the user picks one.

### 3. Read and restate the plan

Read the chosen file. Via `send_message`, restate back to the user:
- The goal
- What's already done
- The next concrete step you intend to take
- Any gotchas you noticed

Ask for confirmation to proceed. Do not act yet.

### 4. Claim the handoff

Once confirmed, **rename** `<root>/.swe-swe/TODO.md` ->
`<root>/.swe-swe/TODO.resumed.md` (use `mv` via Bash). This marks it as
claimed so a second agent walking in won't act on it. If the user chose to
continue from an existing `TODO.resumed.md` in step 2, skip the rename.

### 5. Proceed with the work

Execute the pending steps from the handoff, tracking progress if the list is
non-trivial.

### 6. On completion

Leave `<root>/.swe-swe/TODO.resumed.md` in place as a breadcrumb -- it tells
future agents "this handoff was picked up and worked on." The next
`/swe-swe:save-session` will create a new `TODO.md` alongside it.
