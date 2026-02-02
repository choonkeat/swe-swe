# Identifying User Input in Terminal Recordings for TOC Navigation

**Date**: 2026-02-02
**Related**: record-tui HTML playback, swe-swe `wrapWithScript()`

## Goal

Add table-of-contents style navigation to recording HTML playback, allowing viewers to jump to scroll positions where the user typed something (commands, prompts, etc.).

## Problem

The `session.log` file is a raw stream of terminal bytes — user input (echoed by the shell) and program output are interleaved with no markers to distinguish them. Today's recording pipeline captures only output timing.

## Linux `script` (util-linux 2.35+) — Has What We Need

The current `wrapWithScript` in swe-swe-server uses:

```go
return "script", []string{
    "-q", "-f",
    "--log-timing=" + timingPath,
    "-c", fullCmd,
    logPath,
}
```

This produces the **classic timing format** (output only):

```
0.009404 16      # 0.009s delay, 16 bytes of output
0.440731 35      # 0.441s delay, 35 bytes of output
```

### Advanced Multi-Stream Format

When both input and output logging are enabled, `script` automatically switches to the **advanced format** with type identifiers:

```
O 0.009404 16    # Output: 16 bytes
I 0.500000 1     # Input: 1 byte (user keystroke)
O 0.001234 1     # Output: 1 byte (echo of keystroke)
H 0.000000 ...   # Header
S 0.000000 ...   # Signal
```

Type identifiers: `I` (input), `O` (output), `H` (header), `S` (signal).

### Options for Enabling Input Logging

**Option A: Separate input log file (least disruptive)**

```go
inputPath := fmt.Sprintf("%s/session-%s.input", recordingsDir, recordingUUID)

return "script", []string{
    "-q", "-f",
    "--log-in=" + inputPath,
    "--log-timing=" + timingPath,
    "-c", fullCmd,
    logPath,
}
```

Produces:
- `session.log` — output only (unchanged from today)
- `session.timing` — now in advanced format with `I`/`O` markers
- `session.input` — raw input bytes only

**Option B: Combined I/O log**

```go
return "script", []string{
    "-q", "-f",
    "--log-io=" + logPath,
    "--log-timing=" + timingPath,
    "-c", fullCmd,
}
```

Produces:
- `session.log` — interleaved input + output (timing file needed to separate)
- `session.timing` — advanced format with `I`/`O` markers

**Option A is recommended** — it preserves backward compatibility of `session.log` while adding input data.

### Security Note

From the man page: "Use this logging functionality carefully as it logs all input, including input when terminal has disabled echo flag (for example, password inputs)." This means `session.input` may contain passwords. Consider whether this file should be retained or stripped before sharing.

## macOS `script` (BSD) — More Limited

macOS uses the BSD version of `script`, which has different capabilities:

### Available Flags

| Flag | Description |
|------|-------------|
| `-a` | Append to output file |
| `-d` | Playback without pauses (with `-p`) |
| `-e` | Child command exit status becomes script's exit status |
| `-F` | Flush output after each write |
| `-f` | Create `.filemon` file for monitoring |
| `-k` | Log keys sent to the program as well as output |
| `-p` | Playback a recorded session (with `-r`) |
| `-q` | Quiet mode |
| `-r` | Record session with input, output, and timestamping |
| `-t time` | Set flush interval in seconds |
| `-T fmt` | Report timestamps (implies playback) |

### The `-k` Flag

Logs keystrokes alongside output into the **same** typescript file. The man page warns: "echo cancelling is far from ideal" — meaning the input bytes are mixed into the output stream without clean separation. No separate input file.

### The `-r` Flag

Records "with input, output, and timestamping" into a binary format that can only be replayed with `script -p`. The format is not documented and `cat` cannot display it. This is the closest BSD has to the Linux advanced format, but it's an opaque binary format rather than a parseable text timing file.

### macOS Summary

macOS `script` **cannot** cleanly separate input from output into distinct files or timing streams. Workarounds:

1. **Install util-linux via Homebrew**: `brew install util-linux` — gives the Linux `script` with `--log-in`/`--log-out` support
2. **Custom PTY wrapper**: Write a small Go program using `creack/pty` that tees input to a separate file (swe-swe already uses `creack/pty` for the WebSocket terminal, so this is architecturally familiar)
3. **Use `-k` and post-process**: Use `-k` flag and attempt to separate input from output heuristically (fragile, not recommended)

### Relevance to swe-swe

swe-swe runs inside Linux Docker containers, so this is **not a blocker** — the Linux `script` with `--log-in` is available. The macOS limitation only matters for the standalone record-tui CLI tool when used directly on a Mac.

## How to Build the TOC in the HTML Viewer

### Step 1: Parse the Advanced Timing File

Walk through the timing file entry by entry. Track cumulative byte offsets in the output stream.

```
O 0.009404 16    # output bytes 0-15
I 0.500000 1     # input byte (not in output stream)
O 0.001234 1     # output bytes 16-16 (echo of input)
O 0.300000 200   # output bytes 17-216
I 0.800000 5     # input: 5 bytes
O 0.001000 5     # output bytes 217-221 (echo)
```

### Step 2: Identify "User Said Something" Moments

Accumulate consecutive `I` entries into logical input groups. A "user command" is typically input bytes terminated by `\r` or `\n`. Filter out:
- Single control characters (Ctrl-C, arrow keys, escape sequences)
- Tab completion (single `\t`)
- Very short pauses between keystrokes (part of same typing burst)

### Step 3: Correlate Input Moments to Output Positions

Each input group corresponds to a byte offset in the output stream (the `O` entries before/after it). This byte offset maps to a position in the rendered terminal — i.e., a scroll position in the HTML viewer.

### Step 4: Generate TOC

For each identified command:
- Extract the text the user typed (from `session.input` or from `I` entries in timing)
- Record the output byte offset where it appears
- In the HTML, insert anchor elements or data attributes at those positions
- Render a sidebar/header TOC with links to those anchors

### Practical Considerations

- **Most user input is single keystrokes**: `I 0.5 1` = one byte after 0.5s pause. Need to aggregate into commands.
- **Some input is invisible**: arrow keys, ctrl sequences, tab completion — filter from TOC.
- **Password input**: May appear in input log even when echo is disabled. Consider scrubbing.
- **Long-running commands**: The interesting TOC entry is the command itself, not the output. Each input-then-output transition is a natural section boundary.
- **Alternative approach**: Instead of parsing timing files in the browser, the swe-swe server (or record-tui converter) could pre-compute the TOC and embed it as JSON metadata in the HTML.

## Existing Data in swe-swe Recordings

Each recording already has:
- `session-{uuid}.log` — raw terminal output
- `session-{uuid}.timing` — classic format timing (output only, currently)
- `session-{uuid}.metadata.json` — name, agent, timestamps, visitors, dimensions

The timing file format would change from classic to advanced when `--log-in` is added. A new `session-{uuid}.input` file would appear. The metadata JSON could be extended with a `toc` field containing pre-computed navigation points.

## Recommendation

1. **In swe-swe**: Add `--log-in` to `wrapWithScript()`. This is a one-line change that starts capturing input with zero impact on existing functionality.
2. **In record-tui**: Add a timing file parser that understands the advanced format and can extract input moments + output byte offsets.
3. **In HTML viewer**: Add a TOC sidebar/overlay that uses the extracted input moments as navigation anchors.
4. **For macOS record-tui**: Defer or use a custom PTY approach since BSD `script` lacks the capability.
