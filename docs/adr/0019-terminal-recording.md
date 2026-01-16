# ADR-019: Terminal recording with script command

**Status**: Accepted
**Date**: 2026-01-08

## Context

Users want to review terminal sessions after they end - for debugging, sharing, or auditing. Recording must work with any agent binary without modifications.

## Decision

Use Linux `script` command to wrap agent execution:

1. **Capture mechanism**: `script -q -T timing typescript` wraps the agent command
   - Captures all PTY output including ANSI escape sequences
   - Timing file records delays between output chunks for accurate playback

2. **Storage format**: `/workspace/.swe-swe/recordings/{uuid}/`
   - `typescript` - raw terminal output
   - `timing` - timing data for playback sync
   - `metadata.json` - session info (agent, start/end time, name, visitors)

3. **Playback**: Custom web player using timing data
   - Speed controls (0.5x to 4x)
   - Progress bar with seek
   - Pause/resume

4. **Cleanup model - Recent vs Kept**:
   - **Recent**: Auto-delete after 1 hour OR when count exceeds 5 per agent
   - **Kept**: User explicitly clicks "Keep" - persists until manually deleted
   - Prevents unbounded disk growth while preserving important recordings

## Consequences

Good:
- Zero modification to agent binaries
- Standard format compatible with `scriptreplay`
- Automatic cleanup prevents disk exhaustion
- Explicit "Keep" action for important recordings

Bad:
- Slightly larger than raw output (timing overhead)
- Playback accuracy depends on timing file precision
- Cannot edit/trim recordings after capture
