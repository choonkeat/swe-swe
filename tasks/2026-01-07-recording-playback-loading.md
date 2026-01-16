# Task: Fix Recording Playback Loading State

> **Date**: 2026-01-07
> **Status**: In Progress

## Problem

The recording playback page shows misleading video-player UI (play button, timeline, speed selector) during the loading state, but the final rendered state is just a terminal content view where controls are below the fold or not prominent. This mismatch is jarring, especially for large recordings where loading takes noticeable time.

## Goal

Replace the loading skeleton with a neutral loading indicator that shows:
- A spinner
- "Loading X.X MB..." text (showing the data size)
- No video player controls visible during load

---

## Phase 1: Add loading overlay to render.go

### What will be achieved
The recording playback page will show a centered "Loading recording..." message with file size info and a spinner, instead of showing misleading video-player controls on a black screen.

### Small steps

1. **Modify `RenderPlaybackHTML` in `render.go`**:
   - Calculate `dataSize := len(framesJSON)` after JSON marshaling
   - Format as human-readable (e.g., "2.5 MB")
   - Pass to HTML template

2. **Add CSS for the loading overlay** in the `<style>` block:
   - `.loading-overlay` - covers terminal and controls area
   - Centered content with spinner animation and text
   - `@keyframes spin` for spinner rotation

3. **Add HTML for the loading overlay** after the header, before `.terminal-wrapper`:
   - Div with "Loading X.X MB..." text
   - CSS spinner

4. **Add JavaScript to hide the overlay** after content renders:
   - After `seekTo(totalDuration)` and `trimEmptyRows()` complete
   - Remove the loading overlay element

### Verification / No regression

1. **Run `make build golden-update`**:
   ```bash
   make build golden-update
   ```

2. **Verify golden diff shows only expected changes**:
   ```bash
   git add -A cmd/swe-swe/testdata/golden
   git diff --cached -- cmd/swe-swe/testdata/golden
   ```
   - Should show loading overlay added to all `render.go` copies
   - No unexpected changes

3. **Manual testing with test container + MCP browser**:
   ```bash
   ./scripts/01-test-container-init.sh
   ./scripts/02-test-container-build.sh
   HOST_PORT=9899 HOST_IP=host.docker.internal ./scripts/03-test-container-run.sh
   ```
   - Use MCP browser to navigate to `http://host.docker.internal:9899/`
   - Create sessions (alternate between blank session name and prefilled value)
   - Let sessions run to generate recordings
   - Navigate to the recording page
   - Verify: loading state shows spinner + size info (no video player controls)
   - Verify: after load, playback controls work (play/pause, seek, speed)
   - Verify: keyboard shortcuts work (space, arrows)

4. **Teardown**:
   ```bash
   ./scripts/04-test-container-down.sh
   ```

---

## Phase 2: Verify and test

### What will be achieved
Confirm the implementation works correctly and no regressions were introduced.

### Small steps

1. **Run the Go test suite** (if any tests exist for playback):
   ```bash
   go test ./cmd/swe-swe/templates/host/swe-swe-server/playback/...
   ```

2. **Run full test suite**:
   ```bash
   make test
   ```

3. **Verify golden tests pass**:
   ```bash
   make golden-test
   ```

### Verification / No regression

This phase IS the verification - it confirms:
- All existing tests pass
- Golden files are correctly updated
- Manual browser testing confirms visual behavior

---

## Files to modify

- `cmd/swe-swe/templates/host/swe-swe-server/playback/render.go` - Main implementation
- `cmd/swe-swe/testdata/golden/*/swe-swe-server/playback/render.go` - Updated via `make golden-update`
