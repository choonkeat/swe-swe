# Session & Recording Summaries

The homepage (selection page) shows a one-line summary on each active session card and a multi-line summary on each recording card. These summaries help users quickly identify what each session was about without opening it.

## Data Sources

### Chat events (primary)

For sessions in `chat` mode, the agent writes events to a JSONL file:
```
/workspace/.swe-swe/recordings/session-{parentUUID}-{childUUID}.events.jsonl
```

The server reads the **last 4KB** of this file to find the most recent complete JSON line, making it efficient even for large chat logs (hundreds of MB).

Each event has a `type`, `text`, and optional `quick_replies`:

| Event Type | Summary Prefix | Status |
|-----------|---------------|--------|
| `userMessage` | "You: ..." | red (agent hasn't replied) |
| `agentMessage`, `verbalReply` | "Agent: ..." | green if `quick_replies` present (waiting for user), red otherwise |
| `draw` | "Agent: [diagram]" | green if `quick_replies` present, red otherwise |
| `agentProgress`, etc. | "Agent: ..." | red (still working) |

### Terminal output (fallback, active sessions only)

For active sessions without chat events (or non-chat sessions), the server reads the last non-empty line from the session's virtual terminal buffer. Prompt-only lines (just `❯`) are skipped. This fallback is not used for recordings since the terminal state is not available after a session ends.

## Display Differences

| Aspect | Active Sessions | Recordings |
|--------|----------------|------------|
| Max lines | 1 (single-line, CSS `white-space: nowrap`) | 3 (multi-line, CSS `-webkit-line-clamp: 3`) |
| Overflow | Ellipsis | Ellipsis after 3rd line |
| Status indicator | Green dot (waiting for user) / Red dot (agent busy) | None (session is ended) |
| Terminal fallback | Yes | No |
| Min height | 20px (reserves space even when empty) | None (hidden when empty) |

## Text Sanitization

`sanitizeSummaryText()` normalizes chat messages for display:
- Replaces `\n`, `\r`, `\t` with spaces
- Collapses multiple consecutive spaces
- Trims leading/trailing whitespace

## Implementation

### Key functions in `main.go`

| Function | Purpose |
|----------|---------|
| `getSessionSummaryFromChat(recordingUUID)` | Reads last 4KB of JSONL, parses last event, returns `(summaryLine, status)` |
| `getSessionSummaryFromTerminal(sess)` | Reads VT buffer for last non-empty line (active sessions only) |
| `sanitizeSummaryText(s)` | Normalizes text for single/multi-line display |
| `findChatEventsFile(parentUUID)` | Globs for `session-{uuid}-*.events.jsonl` |

### Data flow

**Active sessions** (on `GET /`):
1. Collect session info under read lock
2. **Outside the lock** (to avoid blocking on file I/O): call `getSessionSummaryFromChat()` for chat sessions, fall back to `getSessionSummaryFromTerminal()`
3. Set `SessionInfo.SummaryLine` and `SessionInfo.SummaryStatus`

**Recordings** (in `loadEndedRecordings()`):
1. Build `RecordingInfo` from file scan
2. For recordings with `.HasChat == true`: call `getSessionSummaryFromChat()`
3. Set `RecordingInfo.SummaryLine` (no status needed)

### Template rendering

```html
<!-- Active session (single line) -->
<div class="session-card__summary">{{.SummaryLine}}</div>

<!-- Recording (multi-line, only shown if non-empty) -->
{{if .SummaryLine}}<div class="recording-card__summary" title="{{.SummaryLine}}">{{.SummaryLine}}</div>{{end}}
```
