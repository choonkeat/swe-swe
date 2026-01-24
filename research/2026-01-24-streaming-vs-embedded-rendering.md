# Streaming vs Embedded Recording Playback Rendering Differences

**Date**: 2026-01-24
**Status**: Fixed - metadata dimensions now passed to streaming playback

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

---

## Terminal Cleared Delimiter Encoding Issue (2026-01-24 continued)

### Symptom

After reboot, most content renders correctly, but the "terminal cleared" delimiter looks wrong:

| Mode | Rendering |
|------|-----------|
| Embedded | `── terminal cleared ──` (correct box-drawing chars) |
| Streaming | `âââââââââ terminal cleared âââââââââ` (broken) |

### Source Code Location

The record-tui source is at `dist/choonkeat/record-tui` (git repo).

### Root Cause: JavaScript String Escape vs UTF-8 Mismatch

**Go constant (clear.go:9):**
```go
const ClearSeparator = "\n\n──────── terminal cleared ────────\n\n"
```
Uses actual UTF-8 character `─` (U+2500), stored as 3 bytes: `E2 94 80`

**JS constant (cleaner-core.js:13):**
```javascript
const CLEAR_SEPARATOR = '\n\n\xe2\x94\x80\xe2\x94\x80... terminal cleared ...\n\n';
```
Uses escape sequences that create **three separate characters**, not one.

### Why `\xe2\x94\x80` Doesn't Work in Browser

JavaScript string escapes like `\xe2` create characters by **code point**, not bytes:
- `'\xe2'` → character U+00E2 → `â` (Latin Small Letter A With Circumflex)
- `'\x94'` → character U+0094 → C1 control character (invisible)
- `'\x80'` → character U+0080 → C1 control character (invisible)

So `'\xe2\x94\x80'` is a **3-character string** showing as `â` + invisible + invisible.

The correct way to represent `─` (U+2500) in JavaScript would be:
- `'\u2500'` (Unicode escape)
- `'─'` (literal character)

### Why This Design Exists (Test Infrastructure Parity)

The cleaner-core.js comment explains:
```javascript
// Using raw UTF-8 bytes for '─' (U+2500) = 0xe2 0x94 0x80 to ensure byte-level parity with Go
// when processing files as latin1 (raw bytes)
```

The test infrastructure (generate_output.js:85-89) reads files as Latin-1:
```javascript
content = fs.readFileSync(realPath, 'latin1');
```

When bytes `E2 94 80` are read as Latin-1:
- `E2` → JS char `\xe2` (â)
- `94` → JS char `\x94`
- `80` → JS char `\x80`

The CLEAR_SEPARATOR with `\xe2\x94\x80` correctly matches these transformed characters. Output is written as Latin-1, so `\xe2` → byte `E2`. This achieves **byte-level parity** with Go.

### The Gap: Browser Uses UTF-8, Not Latin-1

Browser streaming (template_streaming.go:122-137):
```javascript
const reader = response.body.getReader();
const decoder = new TextDecoder();  // Default: UTF-8
// ...
cleaner.write(decoder.decode(result.value, { stream: true }));
```

1. `fetch()` gets raw bytes
2. `TextDecoder()` with UTF-8 decodes bytes `E2 94 80` → single char `─`
3. JS cleaner inserts `CLEAR_SEPARATOR` with chars `â`, `\x94`, `\x80`
4. `xterm.write()` displays those Latin-1 characters literally

**The cleaner was designed for Latin-1 byte processing, but browser uses UTF-8 string processing.**

### Other Edge Cases to Watch For

This encoding mismatch could affect any multi-byte UTF-8 characters:
- Box-drawing characters: `─│┌┐└┘├┤┬┴┼` etc.
- Emojis in terminal output
- International characters in filenames or output

If the session.log contains UTF-8 multi-byte sequences, they would be decoded correctly by TextDecoder. The issue is specifically with **inserted** text (like CLEAR_SEPARATOR) that uses wrong escapes.

### Solution Options

**Option 1: Use ASCII-Only Separator**
Change both Go and JS to use simple dashes:
```
\n\n-------- terminal cleared --------\n\n
```
Pros: Works everywhere, no encoding issues
Cons: Less visually distinctive

**Option 2: Use Unicode Escapes in JS**
Change cleaner-core.js:
```javascript
const CLEAR_SEPARATOR = '\n\n\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500 terminal cleared \u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\n\n';
```
Pros: Correct UTF-8 output in browser
Cons: Breaks byte-level test parity (test reads Latin-1)

**Option 3: Conditional Constants**
Use different constants for:
- Test mode (Latin-1 byte processing): `\xe2\x94\x80`
- Browser mode (UTF-8 string processing): `\u2500`
Pros: Correct everywhere
Cons: Complex, two sources of truth

**Option 4: Change Test Infrastructure to UTF-8**
Modify generate_output.js to read/write as UTF-8:
```javascript
content = fs.readFileSync(realPath, 'utf8');
fs.appendFileSync(outputPath, cleanedContent, 'utf8');
```
Update CLEAR_SEPARATOR to use `\u2500` or literal `─`.
Pros: Simplifies everything, UTF-8 is modern standard
Cons: Need to verify Go also processes as UTF-8 (it does: Go strings are UTF-8)

### Recommended Solution

**Option 4 is likely the cleanest fix.** Go strings are already UTF-8, so the test infrastructure's Latin-1 approach is an artificial constraint. By switching tests to UTF-8:
1. JS CLEAR_SEPARATOR can use proper Unicode (`\u2500` or `'─'`)
2. Browser streaming works correctly
3. Test parity is maintained (both process UTF-8)
4. No conditional logic needed

The Latin-1 approach was likely chosen to handle arbitrary binary in session logs, but ANSI terminal output is fundamentally text-based and should be UTF-8 compatible.

### Design Tension: Single Source of Truth Problem

From commit 6c7e1b68:
> refactor(cleaner): single source of truth for JS cleaner logic
> - cleaner-core.js → cleaner.js → generate_output.js (test-js)
> - cleaner-core.js → embed.go → template_streaming.go (browser)

The design aimed to have `cleaner-core.js` be the single source of truth for both:
1. **Test infrastructure**: Node.js reading files as Latin-1 (byte-preserving)
2. **Browser streaming**: TextDecoder reading as UTF-8 (proper text decoding)

These are **incompatible encodings**. The `\xe2\x94\x80` escape sequence works for Latin-1 byte processing but produces garbage in UTF-8 string processing.

**Why Latin-1 for Tests?**

Go's `os.ReadFile()` → `string()` treats bytes as raw bytes (no encoding validation). To match this in JS:
```javascript
// Go: string(bytes) - bytes as-is
// JS: fs.readFileSync(path, 'latin1') - bytes as code points
```

Latin-1 is "transparent" - byte 0xE2 becomes JS char `\xe2` (code point 226). This preserves arbitrary binary data through the processing pipeline.

**Why This Doesn't Matter for Session Logs**

Terminal session logs are fundamentally UTF-8 text (Claude Code outputs UTF-8). The Latin-1 approach was overly conservative. The test infrastructure could safely use UTF-8:
- Go's `string(bytes)` works correctly on UTF-8 (Go strings ARE UTF-8)
- JS's `fs.readFileSync(path, 'utf8')` would match

### Summary

The encoding mismatch is an artifact of the test infrastructure choosing Latin-1 for byte-level parity with Go's `string([]byte)`, without considering that:
1. Session logs are UTF-8 text, not arbitrary binary
2. The same code would run in browsers with UTF-8 TextDecoder
3. Go strings are UTF-8 internally, so UTF-8 parity is the correct goal

---

## Blank Vertical Space at End of Page (2026-01-24 continued)

### Symptom

After charset issue was fixed, streaming mode shows extra blank vertical space at the end of the page (after the content, before the footer). Embedded mode does not have this issue.

### Key Code Differences

**Embedded mode (template.go):**
```javascript
// Starts with estimated rows based on content pre-analysis
const estimatedRows = Math.max(maxUsedRow, lineCount, 24);
const xterm = new Terminal({ rows: estimatedRows, ... });

// Post-render resize (setTimeout 0ms)
setTimeout(() => {
  // Find last content row
  const actualHeight = Math.max(lastContentRow, cursorRow, 1);
  // Only resize if shrinking
  if (actualHeight < estimatedRows) {
    xterm.resize(contentCols, actualHeight);
  }
}, 0);
```

**Streaming mode (template_streaming.go):**
```javascript
// Starts with large fixed value
const xterm = new Terminal({ rows: 1000, ... });

// Post-render resize (setTimeout 100ms)
setTimeout(function() {
  resizeToFitContent(xterm, COLS);
}, 100);

function resizeToFitContent(xterm, cols) {
  // Find last content row
  const actualHeight = Math.max(lastContentRow, 1); // NOTE: ignores cursor
  xterm.resize(cols, actualHeight); // Always resizes
}
```

### Differences That Could Cause Blank Space

1. **Initial rows**: Embedded estimates closely, streaming starts at 1000
2. **Cursor inclusion**: Embedded includes cursor position, streaming ignores it
3. **Resize timing**: Embedded uses 0ms, streaming uses 100ms
4. **Resize condition**: Embedded only shrinks if needed, streaming always resizes

### xterm.js Resize Behavior Research

From [xterm.js issues](https://github.com/xtermjs/xterm.js/issues):

1. **Issue #98**: "Reducing rows in a resize messes up the terminal" - problems occur "when there are empty lines at the bottom of the terminal"

2. **Issue #3564**: xterm-addon-fit shrinking issues - viewport width gets locked to initial value

3. **Viewport.ts scrollHeight calculation**:
   ```
   scrollHeight = cellHeight * buffer.lines.length
   ```
   If `buffer.lines.length` doesn't shrink after resize, scrollHeight remains large.

### Hypothesis

When streaming mode creates terminal with 1000 rows, then resizes to actual content (e.g., 200 rows):

1. xterm creates DOM elements for 1000 rows initially
2. `xterm.resize(cols, 200)` updates internal state
3. BUT: `buffer.lines.length` may still be 1000
4. Viewport scrollHeight = 1000 * cellHeight (too large)
5. This causes blank scrollable area at bottom

**Alternative hypothesis**: The `.xterm-screen` or `.xterm-viewport` DOM elements retain their initial height CSS values and don't shrink properly.

### Investigation Plan

1. Boot test container
2. Open same recording in embedded vs streaming mode
3. Use browser DevTools to compare:
   - `xterm.buffer.active.length` after render
   - `.xterm-screen` computed height
   - `.xterm-viewport` computed height
   - `#terminal` container height
4. Identify which element has extra height

### Potential Solutions

**Solution 1: Pre-estimate streaming content size**
Make initial fetch to count newlines/size before creating terminal with closer estimate.

**Solution 2: Force DOM height after resize**
```javascript
function resizeToFitContent(xterm, cols) {
  // ... existing logic ...
  xterm.resize(cols, actualHeight);

  // Force DOM update
  const screen = document.querySelector('.xterm-screen');
  const cellHeight = /* get from xterm internals */;
  screen.style.height = (actualHeight * cellHeight) + 'px';
}
```

**Solution 3: Use FitAddon in reverse**
After resize, set container height to match content, then call `fitAddon.fit()`.

**Solution 4: Recreate terminal after streaming**
1. Stream to hidden terminal
2. Count actual rows
3. Create visible terminal with correct size
4. Write content to visible terminal

### Next Steps

Empirical testing needed to confirm hypothesis and identify exact source of blank space.

---

## Better Solution: Use swe-swe Metadata (2026-01-24 discussion)

### Key Insight

swe-swe already tracks terminal dimensions during recording:

```go
// RecordingMetadata (main.go:184-195)
type RecordingMetadata struct {
    // ...
    MaxCols   uint16     `json:"max_cols,omitempty"` // Max terminal columns during recording
    MaxRows   uint16     `json:"max_rows,omitempty"` // Max terminal rows during recording
}
```

These are updated during the session (main.go:427-433):
```go
// Track max dimensions for recording playback
if minCols > s.Metadata.MaxCols {
    s.Metadata.MaxCols = minCols
}
if minRows > s.Metadata.MaxRows {
    s.Metadata.MaxRows = minRows
}
```

### Current Gap

In `handleRecordingPage` (main.go:3695-3775):
- Metadata is loaded: `metadata *RecordingMetadata`
- But dimensions are **not passed** to render functions:
  ```go
  // Streaming mode - no dimensions passed
  recordtui.RenderStreamingHTML(recordtui.StreamingOptions{
      Title:   name,
      DataURL: recordingUUID + "/session.log",
      // Missing: Cols, Rows
  })

  // Embedded mode - no dimensions passed
  recordtui.RenderHTML([]recordtui.Frame{...}, recordtui.Options{
      Title: name,
      // Missing: Cols, Rows
  })
  ```

### Proposed Solution

**1. Add dimension options to record-tui:**

```go
// internal/html/template_streaming.go
type StreamingOptions struct {
    Title      string
    DataURL    string
    FooterLink FooterLink
    Cols       uint16  // NEW: Optional terminal columns (0 = auto-detect)
    Rows       uint16  // NEW: Optional terminal rows (0 = auto-detect)
}

// internal/html/template.go (embedded)
// Similar addition to Options struct
```

**2. Use dimensions in templates:**

```javascript
// Streaming template
const COLS = opts.Cols > 0 ? opts.Cols : 240;  // Use provided or fallback
const ROWS = opts.Rows > 0 ? opts.Rows : 1000; // Use provided or fallback to large

const xterm = new Terminal({
    cols: COLS,
    rows: ROWS,
    // ...
});
```

**3. Update swe-swe to pass dimensions:**

```go
// handleRecordingPage
if metadata != nil && metadata.MaxCols > 0 && metadata.MaxRows > 0 {
    html, err := recordtui.RenderStreamingHTML(recordtui.StreamingOptions{
        Title:   name,
        DataURL: recordingUUID + "/session.log",
        Cols:    metadata.MaxCols,
        Rows:    metadata.MaxRows,  // Exact rows, no guessing!
    })
}
```

### Why This Is Better

| Approach | Initial Rows | Post-render Resize | Blank Space Risk |
|----------|-------------|-------------------|------------------|
| Current streaming | 1000 (guess) | Shrink dramatically | High |
| Current embedded | Estimated from content | Small adjustment | Low |
| **With metadata** | Exact from recording | None needed | None |

### Graceful Degradation

For record-tui CLI (standalone, no swe-swe metadata):
- Continue using current "best effort" estimation
- Accept some blank space may occur
- The fix benefits swe-swe-rendered recordings specifically

### Implementation Steps

1. **record-tui changes:**
   - Add `Cols`, `Rows` to `StreamingOptions` and `Options`
   - Update templates to use these when provided
   - Fall back to current behavior when 0

2. **swe-swe changes:**
   - Pass `metadata.MaxCols`, `metadata.MaxRows` to render functions
   - No other changes needed - metadata already tracked

### Trade-offs

**Pros:**
- Eliminates blank space problem for swe-swe recordings
- No post-render resize jank
- Simpler, more predictable rendering
- Uses accurate data instead of heuristics

**Cons:**
- Requires changes in both repos (record-tui + swe-swe)
- Standalone record-tui CLI still has the issue (but that's expected)
- Metadata file must exist (but it always does for swe-swe recordings)

---

## Implementation (2026-01-24)

### record-tui changes

Commit `9d1f74e` on `dev` branch:
- Added `Cols`, `Rows` fields to `StreamingOptions` struct
- When provided (non-zero), terminal created with exact dimensions
- When 0 (default), falls back to auto-detect (240 cols, 1000 rows + post-resize)

```go
type StreamingOptions struct {
    Title      string
    DataURL    string
    FooterLink FooterLink
    Cols       uint16  // Terminal columns (0 = auto-detect, default 240)
    Rows       uint16  // Terminal rows (0 = auto-detect via post-render resize)
}
```

### swe-swe changes

Commit `616e3011`:
- Pass `metadata.MaxCols`, `metadata.MaxRows` to `RenderStreamingHTML` when available
- Updated record-tui dependency to `v0.0.0-20260124064553-9d1f74edf92c`

```go
opts := recordtui.StreamingOptions{
    Title:   name,
    DataURL: recordingUUID + "/session.log",
    // ...
}
if metadata != nil && metadata.MaxCols > 0 && metadata.MaxRows > 0 {
    opts.Cols = metadata.MaxCols
    opts.Rows = metadata.MaxRows
}
html, err := recordtui.RenderStreamingHTML(opts)
```

### Verification

After reboot:
1. Open a recording with `?render=streaming`
2. Verify no blank space at bottom
3. Check browser console for `TERM_COLS` and `TERM_ROWS` values matching metadata

---

## MaxRows Truncates Scroll History (2026-01-24 continued)

### Symptom

After metadata dimensions fix, streaming mode renders a short page. Scrolling up is cut short - earlier content is missing.

### Root Cause Analysis

**What `MaxRows` actually represents:**
```go
// swe-swe tracking (main.go:427-433)
if minRows > s.Metadata.MaxRows {
    s.Metadata.MaxRows = minRows  // This is the viewport HEIGHT, not content height!
}
```

`MaxRows` is the terminal **window size** during recording (e.g., 50 rows viewport), NOT the **content height** (could be 500+ rows of output).

**The bug in streaming template:**
```go
// template_streaming.go:38-41
autoResizeEnabled := rows == 0
if rows == 0 {
    rows = 1000 // Large initial value for auto-detect mode
}
```

When `opts.Rows` is set from metadata:
1. `rows = 50` (viewport size from metadata)
2. `autoResizeEnabled = false`
3. Terminal created with only 50 rows of buffer
4. Content exceeding 50 rows is truncated (no scroll history)
5. `AUTO_RESIZE = false` so `resizeToFitContent()` never runs

**Compare to embedded mode:**
```javascript
// template.go:114-125 - Pre-analyzes content
const lineCount = content.split('\n').length;
const estimatedRows = Math.max(maxUsedRow, lineCount, 24);
// Creates terminal with rows = content height, not viewport height
```

Embedded mode counts content lines and creates terminal with enough rows for ALL content.

### The Semantic Mismatch

| Metadata Field | Meaning | Correct Usage |
|----------------|---------|---------------|
| `MaxCols` | Terminal width during recording | Use for xterm cols ✓ |
| `MaxRows` | Terminal viewport height | **NOT** for xterm rows ✗ |

xterm's `rows` parameter controls scrollback buffer size. Using viewport height truncates history.

### Fix

**Option 1: Only pass Cols, not Rows** (simplest)

In swe-swe's `handleRecordingPage`:
```go
// Before (broken)
if metadata != nil && metadata.MaxCols > 0 && metadata.MaxRows > 0 {
    opts.Cols = metadata.MaxCols
    opts.Rows = metadata.MaxRows  // ← Causes truncation!
}

// After (fixed)
if metadata != nil && metadata.MaxCols > 0 {
    opts.Cols = metadata.MaxCols
    // Don't pass Rows - let streaming use large buffer + post-resize
}
```

This keeps:
- Correct terminal width from metadata
- Large buffer (1000 rows) for full scroll history
- Post-render resize to remove blank space

**Option 2: Track content height separately** (more work)

Add a new metadata field during recording:
```go
type RecordingMetadata struct {
    MaxCols       uint16 // Viewport width
    MaxRows       uint16 // Viewport height (not useful for playback)
    ContentRows   uint32 // NEW: Total output lines (useful for playback)
}
```

But this requires tracking content during session, which is complex.

### Recommended Fix

Option 1 - only pass `Cols` to streaming, not `Rows`. The original "blank space" problem was solved by post-render resize, which works when `AUTO_RESIZE = true`.

### Fix Applied

Commit pending:
```go
// Before (broken)
if metadata != nil && metadata.MaxCols > 0 && metadata.MaxRows > 0 {
    opts.Cols = metadata.MaxCols
    opts.Rows = metadata.MaxRows  // ← Caused truncation!
}

// After (fixed)
if metadata != nil && metadata.MaxCols > 0 {
    opts.Cols = metadata.MaxCols
    // Don't pass Rows - let streaming use large buffer + post-resize
}
```

**File changed:** `cmd/swe-swe/templates/host/swe-swe-server/main.go`

### Verification After Reboot

1. Open a recording with `?render=streaming`
2. Scroll up - should see full session history (Claude Code logo at top)
3. Browser console should show `TERM_ROWS: 1000` and `AUTO_RESIZE: true`
4. After streaming completes, blank space at bottom should be removed by resize

---

## xterm.js Scrollback Limit (2026-01-24 continued)

### Symptom

After fixing `MaxRows` truncation, streaming playback still doesn't show the beginning of long sessions. Scrolling up shows mid-session content, not the Claude Code logo.

### Root Cause

xterm.js has two separate buffer concepts:
- `rows`: Visible viewport height (default: 24)
- `scrollback`: Lines kept above viewport (default: 1000)

Total buffer = `rows + scrollback`. With `rows: 1000` and default `scrollback: 1000`, only ~2000 lines are kept.

For a session with 250k+ lines, only the last ~2000 are retained.

### Test Results

```javascript
// Session 0cccf614 test:
viewportScrollHeight: 51000  // ~1500 rows in buffer
viewportClientHeight: 34000  // 1000 rows visible
// First content at scroll top: git commits (mid-session)
// NOT Claude Code logo (beginning of session)
```

### Difference from Embedded Mode

**Embedded template:**
```javascript
const estimatedRows = Math.max(maxUsedRow, lineCount, 24);
const xterm = new Terminal({
  rows: estimatedRows,  // Set to content size (e.g., 250k)
  // No scrollback needed - rows IS the buffer
});
```

Embedded mode sets `rows` to match content, so ALL content fits in the main buffer.

**Streaming template:**
```javascript
const xterm = new Terminal({
  rows: 1000,  // Fixed large value
  // scrollback defaults to 1000
  // Total: 2000 lines max
});
```

Streaming has no way to know content size upfront (it streams), so it uses fixed values.

### Fix Options

**Option 1: Large scrollback value**
```javascript
const xterm = new Terminal({
  rows: TERM_ROWS,
  scrollback: 1000000,  // 1 million lines
  ...
});
```
Pros: Simple
Cons: Memory usage for large buffers

**Option 2: Pre-fetch content length**
1. HEAD request to get Content-Length
2. Estimate lines from size
3. Set appropriate scrollback

Cons: Extra request, complexity

**Option 3: Dynamic scrollback**
Use xterm.js option `scrollOnUserInput: false` and manage buffer manually.

Cons: Complex, may not be well supported

### Recommended Fix

Option 1 - set `scrollback: 1000000` for playback. Memory is acceptable for static playback (not interactive terminal).

### Implementation

In `template_streaming.go`:
```javascript
const xterm = new Terminal({
  cols: TERM_COLS,
  rows: TERM_ROWS,
  scrollback: 1000000,  // Large buffer for playback
  fontSize: 15,
  ...
});
```

This change needs to be made in record-tui, then dependency updated in swe-swe.

---

## Better Approach: swe-swe Supplies Estimated Rows (2026-01-24 discussion)

### Key Insight

The embedded template uses `rows: estimatedRows` which is calculated from content line count, so it doesn't need scrollback. Streaming needs large scrollback because content size is unknown upfront.

**But this is only true for record-tui CLI usage.** swe-swe's usage is different:

| Context | Session.log available at render time? | Can pre-calculate rows? |
|---------|--------------------------------------|------------------------|
| record-tui CLI (streaming) | No (streams from network) | No |
| swe-swe server | Yes (reads file from disk) | Yes |

swe-swe already reads session.log to render embedded mode. It could calculate `estimatedRows` the same way and pass it to the streaming renderer.

### Proposed API Change

**record-tui's `StreamingOptions`:**
```go
type StreamingOptions struct {
    Title      string
    DataURL    string
    FooterLink FooterLink
    Cols       uint16  // Terminal columns (0 = auto-detect)
    Rows       uint16  // Terminal rows - this is viewport height, NOT useful

    // NEW: Optional content-based row estimate
    // If provided, streaming template uses this instead of large scrollback
    // If 0, falls back to large scrollback buffer (for CLI usage)
    EstimatedRows uint32
}
```

**Streaming template logic:**
```javascript
// If caller provided content-based estimate, use it (swe-swe case)
// Otherwise use large scrollback for arbitrary streaming (CLI case)
const useEstimatedRows = ESTIMATED_ROWS > 0;

const xterm = new Terminal({
  cols: TERM_COLS,
  rows: useEstimatedRows ? ESTIMATED_ROWS : 1000,
  scrollback: useEstimatedRows ? 0 : 1000000,  // No scrollback needed if rows = content
  ...
});
```

**swe-swe's `handleRecordingPage`:**
```go
// Calculate estimated rows same way embedded does
content, _ := os.ReadFile(logPath)
lineCount := bytes.Count(content, []byte("\n"))

opts := recordtui.StreamingOptions{
    Title:         name,
    DataURL:       recordingUUID + "/session.log",
    EstimatedRows: uint32(lineCount + 10), // Same margin as embedded
}
if metadata != nil && metadata.MaxCols > 0 {
    opts.Cols = metadata.MaxCols
}
html, err := recordtui.RenderStreamingHTML(opts)
```

### Why This Is Best

| Approach | swe-swe | record-tui CLI |
|----------|---------|----------------|
| Large scrollback (current) | Works but uses memory | Works |
| Estimated rows (proposed) | Optimal - exact sizing | Falls back to large scrollback |

Benefits:
- swe-swe recordings get exact sizing like embedded mode
- No wasted memory on scrollback buffer
- record-tui CLI still works with fallback
- Single source of truth for row calculation (same logic as embedded)

### Implementation Steps

1. **record-tui:** Add `EstimatedRows` to `StreamingOptions`
2. **record-tui:** Update streaming template to use `EstimatedRows` when provided
3. **swe-swe:** Calculate line count from session.log, pass as `EstimatedRows`

### Trade-off: Reading File Twice

swe-swe would read session.log twice:
1. To calculate `EstimatedRows` for the template
2. When browser fetches `{uuid}/session.log` endpoint

This is acceptable because:
- OS file cache makes second read fast
- Session logs are typically <10MB
- This matches what embedded mode already does (reads entire file)

---

## Implementation Complete (2026-01-24)

### record-tui changes

Commit `d0b9b3b` pushed to `dev` branch:
- Added `EstimatedRows uint32` to `StreamingOptions`
- When `EstimatedRows > 0`: uses it as `rows`, sets `scrollback: 0`
- When `EstimatedRows == 0`: falls back to `rows: 1000`, `scrollback: 1000000`

### swe-swe changes

Updated `handleRecordingPage` in template:
```go
// Calculate estimated rows from session.log content (same as embedded mode)
// This gives streaming mode exact sizing without needing large scrollback buffer
if content, err := os.ReadFile(logPath); err == nil {
    lineCount := bytes.Count(content, []byte("\n"))
    opts.EstimatedRows = uint32(lineCount + 10) // Small margin like embedded mode
}
```

Updated dependency: `github.com/choonkeat/record-tui v0.0.0-20260124085617-d0b9b3b4b2d9`

### Verification After Reboot

1. Open a recording with `?render=streaming`
2. Check browser console: `TERM_SCROLLBACK` should be `0` (not `1000000`)
3. Verify no blank space at bottom
4. Scroll up - full session history should be visible (Claude Code logo at top)
