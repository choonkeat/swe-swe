# File Drag-Drop and Paste Support for Browser Terminal

## Goal
Intercept file drag-drop and paste events in the browser terminal and send file content (including binary) to the terminal as if the user typed/pasted it.

## Background
- Native terminals (iTerm2, Terminal.app) insert file paths when files are dragged
- Claude CLI uses `@path/to/file` reference system to read files
- For our browser terminal, we want to paste actual file contents

## Implementation Plan

### Step 1: Add drag-drop event listeners with visual feedback
- [x] Add `dragover`, `dragleave`, `drop` event listeners to terminal container
- [x] Prevent default browser behavior (opening file in new tab)
- [x] Add visual feedback (overlay) when dragging over terminal
- [x] Test: Drag a file over terminal, verify visual feedback appears/disappears

### Step 2: Handle text file drops
- [x] Read dropped text files using FileReader API
- [x] Send text content to terminal via existing WebSocket binary channel
- [x] Show status feedback in status bar (not inline terminal)
- [ ] Test: Drop a .txt file, verify contents appear in terminal

### Step 3: Handle binary file drops (binary upload)
- [x] For binary files, send raw binary data with 0x01 prefix
- [x] Server saves file directly to working directory
- [x] Add isTextFile() detection by MIME type and extension
- [x] Test: Drop a small binary file (e.g., .png), verify file saved correctly

### Step 4: Add clipboard paste support for files
- [x] Listen for `paste` event on document
- [x] Check for file items in clipboard (e.g., pasted images)
- [x] Handle pasted files same as dropped files
- [ ] Test: Copy an image, paste into terminal, verify base64 output

### Step 5: Add file type detection and smart handling
- [x] Detect common text file extensions (.js, .ts, .go, .py, .md, .json, .yaml, etc.)
- [x] For text-like files, paste content directly
- [x] For binary files, use base64 with heredoc decode command
- [ ] Test: Drop various file types, verify appropriate handling

### Step 6: Add progress feedback for large files
- [ ] Show upload progress in status bar for large files
- [ ] Add file size limit check (warn for files > 1MB)
- [ ] Test: Drop a large file, verify progress indication

## Technical Notes

### Text file detection
```javascript
const textExtensions = /\.(txt|md|js|ts|jsx|tsx|go|py|rb|rs|c|cpp|h|hpp|java|sh|bash|zsh|fish|json|yaml|yml|toml|xml|html|css|scss|sass|less|sql|graphql|proto)$/i;
const textMimeTypes = /^text\/|^application\/(json|javascript|typescript|xml|yaml)/;
```

### Binary to base64 with shell decode
```bash
# For small files, inline decode:
echo "BASE64_CONTENT" | base64 -d > filename

# For larger files, heredoc:
base64 -d << 'EOF' > filename
BASE64_CONTENT
EOF
```

### Browser APIs used
- `FileReader.readAsText()` - for text files
- `FileReader.readAsArrayBuffer()` - for binary files
- `btoa()` / `Uint8Array` - for base64 encoding
- `DataTransfer.files` - for drag-drop
- `ClipboardEvent.clipboardData` - for paste

## Progress Log

- 2025-12-23 17:51: Plan created
- 2025-12-23: Step 1 implemented (drag-drop overlay UI)
- 2025-12-23: Resuming from Step 2
- 2025-12-23: Steps 2-5 implemented (text files, base64 binary, clipboard paste, file type detection)
- 2025-12-23 19:06: Changed binary upload from base64 to raw binary transfer
  - Client sends binary with 0x01 prefix: `[0x01, name_len_hi, name_len_lo, ...name, ...data]`
  - Server saves file directly to working directory
  - Server responds with JSON `{type: "file_upload", success: true/false, filename, error}`
  - Tested with PNG file - works correctly
