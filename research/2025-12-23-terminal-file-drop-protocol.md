# Terminal File Drag & Drop Protocol Research

Research Date: 2025-12-23 19:10

## Research Questions

1. When you drag a file into iTerm2/Terminal.app, what protocol/escape sequences are sent to the running application?
2. How does a TUI application detect that a file was dropped (vs text pasted)?
3. Research OSC 52 (clipboard), iTerm2 file transfer protocol, Kitty file transfer protocol
4. Look for any documentation on how Claude CLI specifically handles file drops (it shows `[Image #1]` when you drop an image)
5. Search for "terminal drag drop file protocol", "iTerm2 OSC file", "terminal image protocol"

## Key Findings

### 1. Standard Terminal Drag & Drop Behavior

**Basic Mechanism:**
- When you drag and drop a file or folder into most terminal emulators, the terminal **inserts the absolute file path as plain text** into the input stream
- This is handled at the GUI/windowing system level, not through escape sequences
- The communication is facilitated through protocols like Extended Window Manager Hints (EWMH) and X DND protocol on X11 systems
- Wayland implements similar mechanisms for inter-process communication

**File Path Formatting:**
- Paths are typically wrapped in single quotes for shell safety
- Special characters may or may not be escaped depending on the terminal emulator
- Terminal.app and iTerm2 both insert the path wherever the cursor is positioned

**MIME Type Protocol:**
- Drag-and-drop uses the `text/uri-list` MIME type
- Format: `file://host/path` (standard file URI)
- Each file is on a separate line, ending with `\r\n`
- Different terminal emulators have compatibility variations (e.g., xfce4-terminal uses text/uri-list, gnome-terminal prefers text/x-moz-url)

**Important Note:**
From the application's perspective running inside the terminal, drag-and-drop typically appears as **regular text input** containing the file path. The terminal emulator handles the windowing system's drag-and-drop protocol and converts it to text insertion.

### 2. Detecting File Drop vs Text Paste

**The Challenge:**
- From a TUI application's perspective, there's **no reliable way to distinguish** between a file path that was dropped vs. a file path that was manually typed or pasted
- Both appear as regular text input through stdin
- The drag-and-drop event handling happens at the terminal emulator level, not at the application level

**Bracketed Paste Mode:**
While it doesn't specifically detect file drops, bracketed paste mode helps distinguish pasted content from typed content:

**How It Works:**
- Enable with escape sequence: `\e[?2004h` (to stdout)
- Disable with: `\e[?2004l` (to stdout)
- When user pastes, terminal wraps content with:
  - Opening: `\033[200~` (or `\e[200~`)
  - Closing: `\033[201~` (or `\e[201~`)
- Application reads these markers from stdin to detect paste events

**Applications:**
- Prevents accidental execution of pasted commands
- Allows special handling of multi-line pastes (e.g., in vim, ipython)
- Used by terminal multiplexers like tmux, neovim, and TUI frameworks

**Limitations for File Detection:**
- Bracketed paste can tell you content was pasted, not typed
- But it **cannot** tell you whether the content came from:
  - A drag-and-drop operation
  - A clipboard paste
  - A pasted screenshot
  - Regular text paste

### 3. OSC 52 - Clipboard Protocol

**Overview:**
OSC 52 is an ANSI terminal escape sequence for accessing the system clipboard remotely, even over SSH.

**Format:**
```
\e]52;<board>;<content>\x07
```
or
```
ESC ] 52 ; <board> ; <content> BEL
```

**Parameters:**
- `<board>`: clipboard selector
  - `c` = clipboard (universal)
  - `p` = primary (X11 selection, Linux only)
  - macOS only supports `c`
- `<content>`: base64-encoded clipboard data

**Capabilities:**
- **Write (copy)**: Set system clipboard content
- **Read (paste)**: Query clipboard content (less widely supported)
- Works over SSH connections
- Supported by most modern terminal emulators

**Support Status:**
- Write operation: widely supported
- Read operation: less widely supported
- Native support added to Neovim 0.10
- Supported by tmux, kitty, iTerm2, and many others

**Important Limitations:**
- OSC 52 handles **plain text only**
- Cannot transfer binary data, images, or rich text
- For more advanced clipboard features (images, rich text), see Kitty's extended protocol

### 4. iTerm2 File Transfer Protocol (OSC 1337)

**Inline Images Protocol:**

iTerm2 uses a proprietary escape sequence for transferring files and displaying images inline.

**Format:**
```
ESC ] 1337 ; File = [arguments] : <base64-encoded-file-contents> BEL
```
or
```
\e]1337;File=[arguments]:<base64-data>\x07
```

**Key Arguments (semicolon-separated key=value pairs):**

- `name=<base64>`: Base64-encoded filename (defaults to "Unnamed file")
- `size=<bytes>`: File size in bytes; transfer canceled if exceeded
- `width=<value>`: Display width
  - `N` = N character cells
  - `Npx` = N pixels
  - `N%` = N percent of session width
  - `auto` = image's inherent size
- `height=<value>`: Display height (same units as width)
- `inline=<0|1>`:
  - `1` = display inline in terminal
  - `0` or omitted = download to Downloads folder
- `preserveAspectRatio=<0|1>`: Maintain aspect ratio

**File Type Support:**
- Any file can be transferred
- Only images display inline
- Supported formats: any format macOS supports (PNG, GIF, JPEG, PDF, PICT, etc.)

**Example:**
```bash
# Display image inline
printf '\e]1337;File=name=%s;inline=1:%s\a' \
  "$(echo -n 'photo.png' | base64)" \
  "$(base64 < photo.png)"
```

**History:**
- Originally used OSC 50, but changed to 1337 to avoid conflicts with xterm
- Pioneered by the FinalTerm emulator
- Now widely adopted by other terminal emulators

**Drag & Drop with iTerm2:**
- When you drag a file with the **Option key** held, iTerm2 offers to upload via SCP to remote host
- Without modifiers, it simply inserts the file path as text
- This is a GUI-level feature, not triggered by escape sequences

### 5. Kitty File Transfer Protocol

**Overview:**
Kitty implements a sophisticated file transfer protocol that allows transferring files over the TTY device, including over SSH connections.

**Key Features:**

- **Transfer Types:**
  - Regular files
  - Directories (recursive)
  - Symbolic links
  - Hard links

- **Advanced Capabilities:**
  - Preserves metadata (permissions, timestamps, etc.)
  - Optional compression
  - Binary delta transfers (rsync algorithm)
  - Efficient for large files with small changes

- **Security:**
  - Requires user confirmation before transfers
  - Prevents abuse of file transfer protocol

- **Integration:**
  - Works automatically with Kitty's SSH kitten
  - Available on remote machines via SSH kitten
  - Tool: `kitten transfer` or `kitten-transfer`

**Usage Example:**
```bash
# Using kitten transfer
kitten transfer local-file.txt remote:/path/to/destination/

# Transfer with compression
kitten transfer --compress large-file.dat remote:/tmp/
```

**Graphics Protocol:**

Kitty also has an advanced clipboard protocol that extends OSC 52:

- **Supports arbitrary data types:**
  - Plain text (like OSC 52)
  - Images
  - Rich text documents
  - Any MIME type

- **Advantages over OSC 52:**
  - Not limited to plain text
  - More efficient for binary data
  - Better suited for modern applications

**Protocol Design:**
- Not an image format itself, but a **transport protocol** for displaying images
- Simpler than Sixel
- Avoids pixel re-processing
- Full color (not paletted like Sixel)
- More efficient byte-wise than Sixel

### 6. Terminal Image Protocols Comparison

**Sixel:**
- **History:** Introduced by DEC for dot matrix printers (LA50), later used in VT240, VT241, VT330, VT340 terminals
- **Format:** Bitmap graphics format encoding images as 6-pixel-high horizontal strips
- **Encoding:** Each vertical column in a strip forms a "sixel"
- **Limitations:**
  - Primitive and wasteful format
  - Paletted color (not full color)
  - Slower performance
  - Lower picture quality
  - Requires pixel re-processing
- **Modern Support:** mintty (v2.6.0+), mlterm (v3.1.9+), xterm, and others
- **Use Case:** Legacy compatibility, widely supported

**iTerm2 Protocol (OSC 1337):**
- **Advantages:**
  - Full color support
  - No pixel re-processing
  - Fewer bytes than Sixel
  - Supports any macOS-compatible image format
  - Can transfer files (not just images)
- **Support:** iTerm2, WezTerm, Tabby (partial), some others
- **Use Case:** macOS-centric terminals

**Kitty Graphics Protocol:**
- **Philosophy:** Transport protocol, not an image format
- **Advantages:**
  - Simpler than Sixel
  - Solves inherent Sixel issues
  - Full color, no palette
  - No pixel re-processing required
  - Efficient binary transfer
- **Support:** Kitty, some other modern terminals
- **Use Case:** Modern terminals, high performance

**Performance Comparison:**
- Sixel: Slowest, lowest quality
- iTerm2 & Kitty: Much faster, better quality, more efficient

### 7. Claude CLI File Handling

Based on the research and looking at related issues:

**File Drop Behavior:**
- When you drag an image file into Claude CLI, it displays `[Image #1]`
- This suggests Claude CLI is using one of the inline image protocols (likely iTerm2 OSC 1337 or similar)
- The file is processed and sent to the AI model as an image attachment

**File Reference System:**
- Claude CLI uses `@path/to/file` syntax to reference files
- This is typed manually or can be tab-completed
- The CLI reads the file content and sends it to the API

**Image Paste Support:**
- Copy/paste of images works in some terminals (macOS iTerm2 with Ctrl+V)
- Platform-specific behavior varies (macOS vs Linux vs Windows/WSL)
- Clipboard paste may use OSC 52 or terminal-specific clipboard integration

**Known Issues:**
- Bug #4705: File paths sometimes incorrectly converted to `[IMAGE]`
- Bug #3134: Bracketed paste mode can cause corruption in some scenarios
- Platform inconsistencies in clipboard/paste behavior

**Implementation Speculation:**
- Likely using a Go terminal library (possibly Bubble Tea or similar)
- Bubble Tea supports bracketed paste mode
- File detection probably via:
  1. File path analysis (checking if path exists on filesystem)
  2. MIME type detection
  3. File extension checking
  4. Reading file content to determine type

### 8. How TUI Applications Handle Input

**Go Libraries (Bubble Tea):**

**Bracketed Paste Support:**
- Enabled by default
- Disabled with `WithoutBracketedPaste()` option
- Automatically disabled when program quits
- Pasted content marked with `Paste` field in `KeyMsg`
- Allows different handling of pasted vs. typed content

**Input Event Types:**
- `.key` - individual key presses
- `.raw` - raw input data
- `.paste` - bracketed paste events

**File Handling:**
- Bubble Tea ecosystem includes "bubbles" component library
- Includes file picker components
- No built-in file drop detection (relies on application logic)

**Terminal State Management:**
- ProcessTerminal implementations:
  - Put stdin in raw mode
  - Normalize modifier encodings
  - Turn bracketed paste on/off
  - Emit TerminalInput events

### 9. Web Terminal Implementations (xterm.js)

**Paste Handling:**
- Binds to paste events without requiring `contentEditable="true"`
- Supports keyboard paste and right-click paste
- Supports Bracketed Paste Mode via CSI ? 2004 h
- Useful for applications like ipython with multi-line pastes

**Drag and Drop:**
- xterm.js **intentionally does not handle drop events**
- Design decision: altering terminal contents via drag-drop considered unwanted
- Prevents accidental modifications through drag operations

**File Transfer via SFTP:**
- Not built-in to xterm.js
- Would require custom addon module
- Implementation steps:
  1. Write terminal addon with SFTP functionality
  2. On drop event, trigger connection setup
  3. Grab file data and send through SFTP connection

**Browser Compatibility:**
- Various browser and platform considerations
- Paste functionality has been ongoing topic of discussion
- Platform-specific behaviors affect implementation

## Summary: How File Drop Actually Works

### From User's Perspective:
1. User drags file from file manager (Finder, Explorer, etc.)
2. Drops it onto terminal window
3. File path appears in terminal as text

### Under the Hood:

**At Windowing System Level:**
1. OS/Desktop environment detects drag operation
2. Drag source (file manager) provides file data via drag-and-drop protocol:
   - X DND protocol (X11)
   - EWMH (Extended Window Manager Hints)
   - Wayland protocols
   - Native macOS/Windows APIs
3. Data includes file URI in `text/uri-list` MIME type: `file://host/path`

**At Terminal Emulator Level:**
1. Terminal emulator receives drop event from windowing system
2. Extracts file path from URI
3. Formats path (often with quotes, sometimes with escaping)
4. Inserts formatted path into terminal's input buffer
5. Sends path to application via stdin **as regular text**

**At Application Level (TUI app like Claude CLI):**
1. Application reads text from stdin
2. **Cannot distinguish** file drop from:
   - Manually typed path
   - Pasted path
   - Any other text input
3. May use heuristics to detect file paths:
   - Check if text is a valid file path
   - Check if file exists on filesystem
   - Detect file extensions
   - Read file to determine type

**For Special File Handling (Images, etc.):**
1. Application detects file path in input
2. Reads file from filesystem
3. Determines file type (MIME, extension, magic bytes)
4. For images:
   - May encode as base64
   - Send via iTerm2 OSC 1337 protocol to display inline
   - Or send to API (Claude AI) as attachment
5. Shows user feedback (e.g., `[Image #1]`)

## Recommendations for Implementation

### For Browser Terminal (like this project):

**Current Implementation:**
- Browser can detect drag-drop events directly via JavaScript
- Browser can access file contents via FileReader API
- No need to rely on terminal escape sequences

**Advantages Over Native Terminals:**
- Direct access to file data (not just paths)
- Can distinguish drag-drop from paste
- Can handle binary data directly
- Can show upload progress
- Better UX possibilities

**Protocol Design:**
1. **File Drop:**
   - Detect via `drop` event
   - Read file via FileReader API
   - Send to backend via WebSocket with custom protocol

2. **Clipboard Paste:**
   - Detect via `paste` event
   - Check `clipboardData` for file items
   - Handle same as file drop

3. **Binary Transfer Protocol:**
   - Use binary WebSocket messages
   - Custom framing: `[0x01, name_len_hi, name_len_lo, ...name_bytes, ...file_data]`
   - Server saves directly to working directory
   - JSON response: `{type: "file_upload", success: bool, filename: string, error?: string}`

4. **Text File Handling:**
   - Detect text MIME types or extensions
   - Send text content directly to terminal
   - Let shell/application handle as if typed

**This approach is already implemented in the project** (see `/tasks/2025-12-23-file-drop-paste.md`)

### For Native TUI Applications:

**Detection Strategy:**
1. **Enable Bracketed Paste Mode**
   - Distinguish paste events from typing
   - But still can't distinguish file drop from path paste

2. **Heuristic File Detection:**
   ```go
   func detectFilePath(input string) (string, bool) {
       // Check for file path patterns
       if strings.HasPrefix(input, "/") ||
          strings.HasPrefix(input, "./") ||
          strings.HasPrefix(input, "~/") {
           // Check if file exists
           if _, err := os.Stat(expandPath(input)); err == nil {
               return input, true
           }
       }
       return "", false
   }
   ```

3. **File Type Detection:**
   ```go
   import "net/http"

   func detectFileType(path string) (string, error) {
       file, err := os.Open(path)
       if err != nil {
           return "", err
       }
       defer file.Close()

       // Read first 512 bytes for MIME detection
       buffer := make([]byte, 512)
       _, err = file.Read(buffer)
       if err != nil {
           return "", err
       }

       return http.DetectContentType(buffer), nil
   }
   ```

4. **Image Display (iTerm2):**
   ```go
   func displayImageInline(path string) error {
       data, err := ioutil.ReadFile(path)
       if err != nil {
           return err
       }

       encoded := base64.StdEncoding.EncodeToString(data)
       filename := base64.StdEncoding.EncodeToString([]byte(filepath.Base(path)))

       fmt.Printf("\x1b]1337;File=name=%s;inline=1:%s\x07", filename, encoded)
       return nil
   }
   ```

## Sources

### iTerm2 Documentation:
- [Inline Images Protocol](https://iterm2.com/documentation-images.html)
- [Proprietary Escape Codes - iTerm2](https://iterm2.com/documentation-escape-codes.html)
- [iTerm2 Shell Integration](https://iterm2.com/shell_integration.html)
- [iTerm2 General Usage](https://iterm2.com/documentation-general-usage.html)

### OSC 52 Clipboard:
- [Copying to clipboard using OSC 52](https://sunaku.github.io/tmux-yank-osc52.html)
- [OSC-52 - oppi.li](https://oppi.li/posts/OSC-52/)
- [Clipboards, Terminals, and Linux](https://dev.to/djmitche/clipboards-terminals-and-linux-3pk5)
- [OSC: Access system clipboard using ANSI OSC52](https://github.com/theimpostor/osc)
- [Copying all data types to clipboard - Kitty](https://sw.kovidgoyal.net/kitty//clipboard/)

### Kitty Terminal:
- [Transfer files - kitty](https://sw.kovidgoyal.net/kitty/kittens/transfer/)
- [File transfer over TTY - kitty](https://sw.kovidgoyal.net/kitty/file-transfer-protocol/)
- [Terminal graphics protocol - kitty](https://sw.kovidgoyal.net/kitty/graphics-protocol/)

### Terminal Protocols:
- [State of the Terminal](https://gpanders.com/blog/state-of-the-terminal/)
- [xterm Control Sequences](https://invisible-island.net/xterm/ctlseqs/ctlseqs.html)
- [Standards for ANSI escape codes](https://jvns.ca/blog/2025/03/07/escape-code-standards/)
- [Hyperlinks in Terminal Emulators](https://gist.github.com/egmontkob/eb114294efbcd5adb1944c9f3cb5feda)
- [WezTerm Escape Sequences](https://wezterm.org/escape-sequences.html)

### Bracketed Paste:
- [Bracketed paste mode](https://cirw.in/blog/bracketed-paste)
- [Bracketed-paste - Wikipedia](https://en.wikipedia.org/wiki/Bracketed-paste)
- [Bracketed Paste Mode in Terminal](https://jdhao.github.io/2021/02/01/bracketed_paste_mode/)
- [XTerm – bracketed-paste](https://invisible-island.net/xterm/xterm-paste64.html)

### Drag and Drop Protocols:
- [Drag And Drop Files And Folders In Terminal](https://itsfoss.gitlab.io/post/drag-and-drop-files-and-folders-in-terminal-to-print-their-absolute-path/)
- [The text/uri-list format](https://phpspot.net/php/man/gtk/tutorials.filednd.urilist.html)
- [Drag-and-Drop for files](https://www.accum.se/~vatten/dragging_files.html)
- [Wayland clipboard and drag & drop](https://emersion.fr/blog/2020/wayland-clipboard-drag-and-drop/)
- [Drag and drop warts - freedesktop.org](https://www.freedesktop.org/wiki/Draganddropwarts/)

### Image Protocols:
- [Are We Sixel Yet?](https://www.arewesixelyet.com/)
- [Sixel - Wikipedia](https://en.wikipedia.org/wiki/Sixel)
- [Sixel for terminal graphics](https://konfou.xyz/posts/sixel-for-terminal-graphics/)
- [rasterm: encode images to iTerm/Kitty/SIXEL protocols](https://github.com/BourgeoisBear/rasterm)

### TUI Frameworks:
- [Bubble Tea - Go Package](https://pkg.go.dev/github.com/charmbracelet/bubbletea)
- [Bubble Tea - GitHub](https://github.com/charmbracelet/bubbletea)
- [Missing support for bracketed paste - Bubble Tea Issue #404](https://github.com/charmbracelet/bubbletea/issues/404)
- [Textual - Paste Events](https://textual.textualize.io/events/paste/)
- [libvaxis - modern tui library in zig](https://github.com/rockorager/libvaxis)

### Web Terminals (xterm.js):
- [Browser Copy/Paste support - xterm.js #2478](https://github.com/xtermjs/xterm.js/issues/2478)
- [Drag and drop files - xterm.js #4956](https://github.com/xtermjs/xterm.js/issues/4956)
- [Support bracketed paste mode - xterm.js commit](https://github.com/xtermjs/xterm.js/commit/1dbcf70cee9ae88c69cf9724745cbdb7e5364dcc)

### Claude CLI:
- [How to Paste Images in Claude Code](https://www.arsturn.com/blog/claude-code-paste-image-guide)
- [Can Claude Code see images in 2025?](https://www.cometapi.com/can-claude-code-see-images-and-how-does-that-work-in-2025/)
- [Claude Code: Best practices](https://www.anthropic.com/engineering/claude-code-best-practices)
- [Quick Fix: Claude Code Image Paste in Linux](https://blog.shukebeta.com/2025/07/11/quick-fix-claude-code-image-paste-in-linux-terminal/)
- [BUG: File path converted to IMAGE - Issue #4705](https://github.com/anthropics/claude-code/issues/4705)
- [BUG: Bracketed Paste Corruption - Issue #3134](https://github.com/anthropics/claude-code/issues/3134)

### Other Resources:
- [Use Drag and Drop in Mac Terminal](https://www.thefastcode.com/en-usd/article/use-drag-and-drop-to-speed-up-mac-terminal-commands)
- [Drag and drop from terminal - Blog](https://blog.meain.io/2022/terminal-drag-and-drop/)
- [Developing a terminal UI in Go with Bubble Tea](https://packagemain.tech/p/terminal-ui-bubble-tea)
