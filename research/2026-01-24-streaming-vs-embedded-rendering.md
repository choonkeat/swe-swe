# Streaming vs Embedded Recording Playback Rendering Differences

**Date**: 2026-01-24
**Status**: Root cause found - stale template dependencies; fix pending reboot

## Problem

When rendering recording playback, streaming mode produces visibly different DOM output compared to embedded mode:
- Streaming mode was missing the beginning content (Claude Code logo, initial prompt)
- Streaming started from content that appeared later in the session (e.g., "unzip: command not found" error)
- Different terminal dimensions: streaming used 120 cols vs embedded's 240 cols
- Different row counts: streaming had 310 rows vs embedded's 181 rows

## Background

Two rendering modes exist for recording playback:

**Embedded mode** (default, stable):
1. Server reads session.log
2. Server calls `StripMetadata()` (Go) to pre-process content
3. Server embeds pre-processed content into HTML via `RenderHTML()`
4. Browser renders the embedded content

**Streaming mode** (experimental, `?render=streaming`):
1. Server returns lightweight streaming HTML (no data embedded)
2. Browser JS fetches `{uuid}/session.log` endpoint
3. JS cleaner processes content (header/footer stripping, clear sequence neutralization)
4. JS streams processed content to xterm.js

## Research Findings

### DOM Analysis

Extracted HTML DOMs from Chrome after rendering completed:
- `embedded-dom.html`: 256KB, 181 terminal rows, width=2160px
- `streaming-dom.html`: 152KB, 310 terminal rows, width=1080px

Content comparison:
```
Embedded row 1:  ▐▛███▜▌   Claude Code v2.1.19
Streaming row 1: /bin/bash: line 1: unzip: command not found
```

Streaming was missing embedded rows 1-14 (logo, prompt, bash command, error line).

### Version Mismatch

The streaming DOM showed `cols: 120` but current record-tui uses `cols: 240`:
- Indicates deployed container had older record-tui version
- Old version: `v0.0.0-20260123100207-52b1bfde59b1` (cols: 120, rows: 50)
- Current version: `v0.0.0-20260124000954-6c7e1b68b4dd` (cols: 240, rows: 1000)

### Double-Processing Issue

Commit `2213ee21` added server-side pre-processing to the session.log endpoint:
```go
processed := recordtui.StripMetadata(string(content))
w.Write([]byte(processed))
```

This caused **double-processing**:
1. Server pre-processes with Go's `StripMetadata()` (strips header/footer, neutralizes clear sequences)
2. JS cleaner runs on already-processed content (should be no-op but may have subtle interactions)

### Content Processing Differences

Go's `NeutralizeClearSequences`:
- Properly handles clears at start/end (no separator if no content before/after)
- Uses `\n\n` in separator

JS's `neutralizeClearSequences`:
- Simple regex replace, always inserts separator
- Uses `\r\n\r\n` in separator (old version)

### record-tui Test Infrastructure

The record-tui library has `make test` that verifies Go and JS cleaners produce identical output:
- `internal/session/output_test.go`: Generates Go output
- `internal/js/generate_output.js`: Generates JS output
- `internal/session/compare_output_test.go`: Compares outputs byte-by-byte

## Solution

**Approach**: Remove server-side pre-processing, let JS cleaner be the single source of truth for streaming mode.

**Commit**: `1a14130b fix(recording): serve raw session.log for streaming playback`

**Change**:
```go
// Before (double-processing)
func handleRecordingSessionLog(...) {
    content, err := os.ReadFile(logPath)
    processed := recordtui.StripMetadata(string(content))
    w.Write([]byte(processed))
}

// After (JS handles everything)
func handleRecordingSessionLog(...) {
    http.ServeFile(w, r, logPath)
}
```

**Rationale**:
- Embedded mode: Go does the cleaning (StripMetadata before RenderHTML)
- Streaming mode: JS does the cleaning (createStreamingCleaner)
- Each mode has a single source of truth
- record-tui's test suite ensures Go/JS cleaners produce identical output

## Files Changed

- `cmd/swe-swe/templates/host/swe-swe-server/main.go`: Reverted to `http.ServeFile()`
- 32 golden test files updated automatically

## Verification

After reboot with new code:
1. Open a recording in embedded mode (default)
2. Open same recording in streaming mode (`?render=streaming`)
3. Compare rendered content - should match
4. Both should show Claude Code logo at the start

## Related Commits

- `2dc571c8` feat(recording): use streaming HTML for playback - original streaming implementation
- `2213ee21` fix(recording): default to embedded playback, streaming is experimental - added pre-processing (caused issue)
- `1a14130b` fix(recording): serve raw session.log for streaming playback - removed pre-processing (this fix)

---

## Post-Reboot Investigation (2026-01-24 continued)

After reboot, streaming mode still showed incorrect rendering. Further investigation revealed a second root cause.

### Symptoms After Reboot

Extracted DOMs from `.swe-swe/uploads/html.zip`:
- `embedded-dom.html`: 11.5MB, 135 xterm-rows, width=2160px (240 cols)
- `streaming-dom.html`: 159KB, 10 xterm-rows, width=1080px (120 cols)

Content still mismatched:
```
Embedded row 1:  ▐▛███▜▌   Claude Code v2.1.19
Streaming row 1: index 3aea1d59..375f4484 100644  (git diff line from mid-session)
```

### Root Cause: Stale Template Dependencies

The swe-swe-server is built inside Docker using its own `go.mod`, separate from the main project:

| File | record-tui version |
|------|-------------------|
| `/workspace/go.mod` | `v0.0.0-20260124000954-6c7e1b68b4dd` (new, cols: 240) |
| `templates/.../go.mod.txt` | `v0.0.0-20260123100207-52b1bfde59b1` (old, cols: 120) |

The restart scripts work correctly, but the Docker build used the template's stale go.mod.txt, pulling the old record-tui version.

### Why Reboot Didn't Fix It

1. `pre-restart.sh` runs `make build` → updates main swe-swe binary with new go.mod
2. `swe-swe init` → copies templates (including stale go.mod.txt) to output
3. Docker build → `COPY swe-swe-server/go.mod` uses the stale version
4. `go mod download` → fetches OLD record-tui (cols: 120)
5. Container runs with old streaming template configuration

### Fix Applied

Updated template files to match main go.mod:
- `cmd/swe-swe/templates/host/swe-swe-server/go.mod.txt`
- `cmd/swe-swe/templates/host/swe-swe-server/go.sum.txt`

Also updated `docs/dev/how-to-restart.md` with prerequisite check for dependency sync.

### Next Steps

After next reboot:
1. Verify streaming uses cols: 240 (check xterm-screen width = 2160px)
2. Verify streaming content starts with Claude Code logo
3. Compare embedded vs streaming DOM content for parity
