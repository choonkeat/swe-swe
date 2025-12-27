# Claude Code File/Image Handling Research

**Date:** 2025-12-23 19:15
**Task:** Investigate how Claude Code CLI handles file drag & drop and image display

## Overview

Claude Code is distributed as a compiled binary (Mach-O executable on macOS) that bundles JavaScript code. The source code is not publicly available on GitHub - the repository at https://github.com/anthropics/claude-code contains only plugins and documentation, not the CLI source code.

## Investigation Approach

Since the source code is not available, I examined the compiled binary at `/Users/choonkeatchew/.local/bin/claude` using `strings` to extract readable text patterns.

## Findings

### 1. OSC Sequence Usage

Found evidence of **OSC 1337** usage in the binary strings. OSC (Operating System Command) sequences are escape codes used for advanced terminal features.

**OSC 1337** is iTerm2's proprietary escape sequence protocol commonly used for:
- Inline image display
- File transfer
- Clipboard operations

### 2. Base64 Encoding

Multiple references to base64 encoding/decoding found:
- `base64` type loaders
- Base64 string handling
- Data URL support (`data:;base64,`)

This suggests files/images are likely:
1. Read from the filesystem or terminal input
2. Encoded as base64
3. Transmitted via OSC sequences

### 3. Image Display Pattern

While I didn't find the exact `[Image #1]` pattern in strings (could be dynamically generated), the binary contains:
- Image error handling CSS/HTML (Bun error overlay templates)
- Data URL image embedding
- PNG/JPEG file type handling

### 4. Terminal Integration

Evidence of terminal control sequences:
- Paste detection (`paste-start`, `paste-end` key names)
- Drop events (`drop`, `dropRequest` references)
- File handling infrastructure

## Likely Implementation

Based on the strings analysis, Claude Code likely handles file/image drops as follows:

### File Drop/Paste Detection
1. Terminal emulator detects file drop or paste event
2. Modern terminals can send file data via:
   - **OSC 52** (clipboard operations)
   - **OSC 1337** (iTerm2's File= protocol)
   - Bracketed paste mode with file paths

### Image Handling
When an image is dropped:
1. Terminal sends file path or base64-encoded data via OSC sequence
2. Claude Code:
   - Detects the OSC sequence
   - Reads file if only path was provided
   - Encodes image as base64 if needed
   - Displays `[Image #N]` placeholder in terminal
   - Stores image data for API submission

### iTerm2's OSC 1337 Protocol
The most likely protocol for image display:
```
ESC ] 1337 ; File = [arguments] : base64-data ESC \
```

Arguments can include:
- `name=filename.png` - filename
- `size=12345` - file size in bytes
- `width=auto` - display width
- `height=auto` - display height
- `inline=1` - display inline vs download

## Key Technical Details

### Why Not Standard OSC 52?
OSC 52 is for clipboard operations (copy/paste text). For file/image handling, terminals use:
- **OSC 1337** (iTerm2)
- **OSC 5379** (kitty graphics protocol)
- Sixel graphics protocol

Claude Code appears to target OSC 1337, which is supported by:
- iTerm2 (native)
- WezTerm
- Some other modern terminals

### Detection vs Regular Text
The terminal can distinguish between:
- **Regular paste:** Text within bracketed paste markers
- **File drop:** Special OSC sequences with file metadata
- **Drag & drop:** Terminal-specific events that trigger OSC sequence generation

## Related Protocols

1. **Bracketed Paste Mode:**
   ```
   ESC [?2004h  # Enable
   ESC [200~    # Paste start marker
   <pasted text>
   ESC [201~    # Paste end marker
   ```

2. **OSC 52 (Clipboard):**
   ```
   ESC ] 52 ; c ; base64-data ESC \
   ```

3. **OSC 1337 (File Transfer):**
   ```
   ESC ] 1337 ; File = inline=1 : <base64> ESC \
   ```

## Implications for swe-swe

To implement similar file/image handling in swe-swe:

1. **Parse OSC 1337 sequences** from terminal input
2. **Detect file drops** vs regular input
3. **Extract base64 data** or file paths
4. **Display placeholders** like `[Image #1]` in terminal
5. **Store image data** to send with API requests

### Implementation Notes

- Need to handle both **file paths** and **inline base64 data**
- Must parse OSC sequence parameters (name, size, etc.)
- Should support multiple images in same conversation
- May need to decode base64 and verify image format

## Terminal Compatibility

OSC 1337 support varies:
- ✅ iTerm2 - Full support
- ✅ WezTerm - Good support
- ⚠️  kitty - Has own graphics protocol (OSC 5379)
- ❌ Most basic terminals - No support

For broader compatibility, may need to support multiple protocols or fall back to file path detection only.

## Implementation Findings (2025-12-23)

### Key Discovery: OSC 1337 is NOT for Input

After testing, we discovered that **OSC 1337 is for terminal OUTPUT, not input**:
- Terminals use OSC 1337 to DISPLAY images (app → terminal)
- NOT for sending images TO applications (terminal → app)

When you drag a file into iTerm2:
1. iTerm2 inserts the **file path** as plain text
2. Claude Code detects the path is a valid file
3. Claude Code reads the file from disk
4. Shows `[Image #1]` placeholder

### swe-swe Implementation

For our browser terminal, we implemented:

1. **Browser side** (`terminal-ui.js`):
   - Intercept file drop/paste events
   - Read file as binary using `FileReader.readAsArrayBuffer()`
   - Send with `0x01` prefix: `[0x01, name_len(2), name_bytes, file_data]`

2. **Server side** (`main.go`):
   - Parse `0x01` binary message
   - Save file to `.swe-swe/uploads/<filename>`
   - Send absolute file path to PTY as text
   - Claude Code reads the file from disk

### Protocol Summary

```
Browser                    Server                     PTY (Claude Code)
   |                          |                            |
   |-- 0x01 + name + data --> |                            |
   |                          |-- save to disk             |
   |                          |-- "/abs/path/file.png" --> |
   |                          |                            |-- reads file
   |                          |                            |-- shows [Image #1]
```

### What Didn't Work

1. **Raw binary to PTY**: Corrupted terminal display
2. **OSC 1337 to PTY**: Claude showed `[Pasted text #1]` and base64 content
3. **Base64 heredoc**: Works but clunky, requires shell command execution

### What Works

Saving file to disk + sending file path = Claude Code recognizes it as `[Image #1]`

## References

- iTerm2 Inline Images Protocol: https://iterm2.com/documentation-images.html
- OSC sequences: https://invisible-island.net/xterm/ctlseqs/ctlseqs.html
- Bracketed Paste Mode: https://cirw.in/blog/bracketed-paste

## Limitations of This Research

Since Claude Code is closed source:
- Cannot verify exact implementation details
- Cannot see error handling or edge cases
- Cannot determine full feature set
- Findings are based on binary string analysis only

For definitive implementation details, would need to:
- Reverse engineer the binary (beyond scope/ethics)
- Contact Anthropic for technical details
- Test behavior empirically with different inputs
