---
description: Execute pending steps in a task plan file with logging and verification
---

$ARGUMENTS

**Progress reporting**: The user may not be watching your terminal. Use `send_progress` (non-blocking) to report status after each step completes and before starting a new one. Use `send_message` (blocking) when you need user input or hit a blocker. This keeps the chat UI informed even when terminal output is noisy.

**Cadence**: Report via `send_progress` at the end of EVERY phase and AT LEAST once every 5 minutes during long steps -- never go silent longer than that. Each progress report includes: (a) what just happened / what's next, (b) the salient code diff (the few lines that matter, not a wall), (c) `git --stat` of work since the last report, and (d) a screenshot when the change is visual (UI/browser) -- capture with the browser MCP and attach via `send_progress` image args.

**TDD**: Implement test-first. For each step that changes behavior: write the failing test first, log it RED (expected failure observed), then write the minimum code to pass, log it GREEN. No production change lands without a test that exercised it. If a step is genuinely untestable, say so in the log and get permission rather than skipping silently.

1. Do the next pending step in the task file.
    - write/extend the test FIRST and confirm it fails for the right reason before touching production code
    - for any test or verification that you are doing, log the expected-and-gotten, i.e.
        - before doing it, echo {hhmmss in localtime}, what will be done, and what to expect >> tasks/{task filename}-{phase}.log and git commit it
        - after doing it, echo {hhmmss in localtime}, what you observed, and what you got (regardless of whether it was what we expected) >> tasks/{task filename}-{phase}.log and git commit it
2. After successfully completing a step
    2.1. verify tasks/{task filename}-{phase}.log against the task's list of mcp browser: if we did not get the expected outcome, echo {hhmmss in localtime}, redoing because {reasons} >> tasks/{task filename}-{phase}.log, git commit it and go back to redo (1)
    2.3. update the task file to indicate progress
    2.4. git commit only the relevant files (do not bluntly git add everything) with conventional commit message style (specifying it is phase n/m of this task)
    2.5. send_progress with the phase summary: salient diff, `git --stat`, and a screenshot if the change is visual
3. Loop back to (1) unless there are no more pending steps in the task file.

When you're all done
- for each tasks/{task filename}-{phase}.log file
    - take me through its content (what you've done, problems encountered, and conclusion)
    - ask me if i'm ok before proceeding to the next file
- then commit only the relevant files (do not bluntly `git add -A` the whole pwd) with conventional commit message style and let me know the current git status after that.

If we need any excuses for any verification failing, please stop work and get my permission; do NOT plough on with compromises.
