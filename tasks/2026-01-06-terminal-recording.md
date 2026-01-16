# Terminal Recording Integration

## Goal
Integrate terminal session recording into swe-swe agents using Linux `script` command, with recordings stored in `/workspace/.swe-swe/recordings/`, and a web UI to list and playback recordings.

## Testing Approach
All phases use test container workflow:
```bash
./scripts/01-test-container-init.sh
./scripts/02-test-container-build.sh
HOST_PORT=11977 HOST_IP=host.docker.internal ./scripts/03-test-container-run.sh
```
Then MCP browser at `http://host.docker.internal:11977/`

---

## Phase 1: Recording Infrastructure ✅

### What Will Be Achieved
Wrap agent process execution with Linux `script` command to capture terminal output and timing data to `/workspace/.swe-swe/recordings/session-{uuid}.log` and `session-{uuid}.timing`.

### Steps

**Step 1.1: Add recording fields to Session struct**
```go
type Session struct {
    // ... existing fields ...
    RecordingUUID string  // UUID for recording files
}
```

**Step 1.2: Create recordings directory helper**
```go
func ensureRecordingsDir() error {
    return os.MkdirAll("/workspace/.swe-swe/recordings", 0755)
}
```

**Step 1.3: Create script wrapper function**
```go
func wrapWithScript(cmdName string, cmdArgs []string, recordingUUID string) (string, []string) {
    fullCmd := cmdName
    if len(cmdArgs) > 0 {
        fullCmd += " " + strings.Join(cmdArgs, " ")
    }
    logPath := fmt.Sprintf("/workspace/.swe-swe/recordings/session-%s.log", recordingUUID)
    timingPath := fmt.Sprintf("/workspace/.swe-swe/recordings/session-%s.timing", recordingUUID)

    return "script", []string{
        "-q", "-f",
        "--log-timing=" + timingPath,
        "-c", fullCmd,
        logPath,
    }
}
```

**Step 1.4: Integrate into `getOrCreateSession()`** (line ~1048)
- Generate `recordingUUID` using `uuid.New().String()`
- Call `ensureRecordingsDir()`
- Wrap command with `wrapWithScript()`
- Store `RecordingUUID` in session

**Step 1.5: Integrate into `RestartProcess()`** (line ~606)
- Reuse existing `RecordingUUID` (same session, continues recording)
- Wrap restart command with `wrapWithScript()`

**Step 1.6: Add uuid dependency**
- Already have `github.com/google/uuid` in server template

### Verification

1. **Manual verification**: Start container, select agent, interact briefly
2. **Check files exist**: `ls -la /workspace/.swe-swe/recordings/`
3. **Verify content**: `session-{uuid}.log` has terminal output, `session-{uuid}.timing` has timing data
4. **Test playback**: `scriptreplay session-{uuid}.timing session-{uuid}.log`
5. **Regression**: Agent still works normally, process restart works, multiple sessions get separate files

---

## Phase 2: Metadata Management ✅

### What Will Be Achieved
Create/update `session-{uuid}.metadata.json` when session name changes or visitors join.

### Steps

**Step 2.1: Define metadata struct**
```go
type RecordingMetadata struct {
    UUID      string     `json:"uuid"`
    Name      string     `json:"name,omitempty"`
    Agent     string     `json:"agent"`
    StartedAt time.Time  `json:"started_at"`
    EndedAt   *time.Time `json:"ended_at,omitempty"`
    Command   []string   `json:"command"`
    Visitors  []Visitor  `json:"visitors,omitempty"`
}

type Visitor struct {
    JoinedAt time.Time `json:"joined_at"`
    IP       string    `json:"ip"`
}
```

**Step 2.2: Add metadata tracking to Session**
```go
type Session struct {
    // ... existing fields ...
    RecordingUUID string
    Metadata      *RecordingMetadata
    metadataMu    sync.Mutex
}
```

**Step 2.3: Create metadata write helper**
```go
func (s *Session) saveMetadata() error {
    if s.Metadata == nil {
        return nil
    }
    path := fmt.Sprintf("/workspace/.swe-swe/recordings/session-%s.metadata.json", s.RecordingUUID)
    data, err := json.MarshalIndent(s.Metadata, "", "  ")
    if err != nil {
        return err
    }
    return os.WriteFile(path, data, 0644)
}
```

**Step 2.4: Initialize metadata on session creation**
- Create `RecordingMetadata` with UUID, Agent, StartedAt, Command
- Don't write file yet (only write when name set or visitor joins)

**Step 2.5: Hook into session name change**
- After name change: `s.Metadata.Name = newName; s.saveMetadata()`

**Step 2.6: Hook into visitor join**
- In `handleWebSocket()` when new client connects
- Append to `s.Metadata.Visitors`, call `s.saveMetadata()`

**Step 2.7: Set EndedAt on session end**
- When process exits: `s.Metadata.EndedAt = time.Now(); s.saveMetadata()`

### Verification

1. **Name change creates metadata**: Set name via status bar, verify file created
2. **Visitor join updates metadata**: Open second tab, verify visitor entry added
3. **EndedAt is set**: Exit agent, verify `ended_at` populated
4. **Regression**: Sessions without name changes don't create metadata file

---

## Phase 3: API Endpoints

### What Will Be Achieved
Add REST API endpoints:
- `GET /api/recording/list` - List all recordings
- `DELETE /api/recording/{uuid}` - Delete a recording
- `GET /api/recording/{uuid}/download` - Download as zip

### Steps

**Step 3.1: Define API response types**
```go
type RecordingListItem struct {
    UUID      string     `json:"uuid"`
    Name      string     `json:"name,omitempty"`
    Agent     string     `json:"agent"`
    StartedAt time.Time  `json:"started_at"`
    EndedAt   *time.Time `json:"ended_at,omitempty"`
    HasTiming bool       `json:"has_timing"`
    SizeBytes int64      `json:"size_bytes"`
}
```

**Step 3.2: Implement `GET /api/recording/list`**
- Read recordings directory
- Parse metadata files
- Filter out active sessions
- Sort by StartedAt descending

**Step 3.3: Implement `DELETE /api/recording/{uuid}`**
- Validate UUID format
- Delete all `session-{uuid}.*` files
- Return 204 or 404

**Step 3.4: Implement `GET /api/recording/{uuid}/download`**
- Create zip with log, timing, metadata files
- Set Content-Disposition header
- Stream zip to response

**Step 3.5: Register routes**
```go
if strings.HasPrefix(r.URL.Path, "/api/recording/") {
    handleRecordingAPI(w, r)
    return
}
```

### Verification

1. **List recordings**: `GET /api/recording/list` returns JSON
2. **Delete recording**: `DELETE /api/recording/{uuid}` removes files
3. **Download recording**: `GET /api/recording/{uuid}/download` returns zip
4. **Active session filtering**: Active sessions excluded or marked

---

## Phase 4: Homepage Integration

### What Will Be Achieved
Display ended recordings on homepage with playback and delete links.

### Steps

**Step 4.1: Add RecordingInfo type**
```go
type RecordingInfo struct {
    UUID      string
    UUIDShort string
    Name      string
    Agent     string
    EndedAt   time.Time
    EndedAgo  string
}
```

**Step 4.2: Create loadEndedRecordings helper**
- Read recordings directory
- Filter out active sessions
- Sort by EndedAt descending

**Step 4.3: Add recordings to template data**
```go
data := struct {
    Agents     []AgentWithSessions
    Recordings []RecordingInfo
    // ...
}
```

**Step 4.4: Update selection template HTML**
```html
{{if .Recordings}}
<div class="recordings-section">
    <h2>Recordings</h2>
    {{range .Recordings}}
    <div class="recording-item">
        <span>{{if .Name}}{{.Name}}{{else}}session-{{.UUIDShort}}{{end}}</span>
        <span>{{.Agent}}</span>
        <span>ended {{.EndedAgo}}</span>
        <a href="/recording/{{.UUID}}">Play</a>
        <button onclick="deleteRecording('{{.UUID}}')">Delete</button>
    </div>
    {{end}}
</div>
{{end}}
```

**Step 4.5: Add JavaScript for delete**

**Step 4.6: Add CSS styling**

### Verification

1. **Recordings appear**: Run session, exit, verify listing on homepage
2. **Unnamed shows UUID**: Session without name shows truncated UUID
3. **Delete works**: Button removes recording
4. **Play link navigates**: Goes to `/recording/{uuid}`
5. **Active sessions separate**: Not shown in recordings section

---

## Phase 5: Playback Page (Placeholder)

### What Will Be Achieved
`/recording/{uuid}` serves placeholder page with download link.

### Steps

**Step 5.1: Add route handler**
```go
if strings.HasPrefix(r.URL.Path, "/recording/") {
    recordingUUID := strings.TrimPrefix(r.URL.Path, "/recording/")
    handleRecordingPage(w, r, recordingUUID)
    return
}
```

**Step 5.2: Create recording page template**
- Back link to home
- Metadata display (name, agent, timestamps)
- Placeholder notice with scriptreplay instructions
- Download button
- Delete button

**Step 5.3: Implement page handler**
- Validate UUID
- Load metadata if exists
- Render template
- 404 if not found

**Step 5.4: Add delete-and-redirect JavaScript**

**Step 5.5: Style the page**

### Verification

1. **Page loads**: Valid UUID shows page with metadata
2. **Download works**: Zip file downloads correctly
3. **Delete works**: Removes files, redirects to home
4. **404 for invalid**: Unknown UUID returns 404
5. **Back link works**: Returns to homepage

---

## Phase 6: Playback Page (Full)

### What Will Be Achieved
Replace placeholder with full HTML playback using vendored record-tui code.

### Steps

**Step 6.1: Create vendor directory**
```
swe-swe-server/playback/
├── types.go
├── timing.go
├── cleaner.go
└── render.go
```

**Step 6.2: Vendor types.go**
```go
type PlaybackFrame struct {
    Timestamp float64 `json:"timestamp"`
    Content   string  `json:"content"`
}
```

**Step 6.3: Vendor cleaner.go**
- `StripMetadata()` function from record-tui

**Step 6.4: Create timing.go** (new)
```go
func ParseTimingFile(logContent, timingContent []byte) ([]PlaybackFrame, error) {
    // Parse Linux script timing format: "delay bytes\n"
    // Build frames with cumulative timestamps
}
```

**Step 6.5: Vendor/adapt render.go**
```go
func RenderPlaybackHTML(frames []PlaybackFrame, metadata *RecordingMetadata) (string, error) {
    // Generate HTML with xterm.js
    // Base64 encode frames
    // Include playback controls
}
```

**Step 6.6: Add playback controls**
- Play/Pause button
- Progress bar
- Speed selector (0.5x, 1x, 2x, 4x)
- Time display

**Step 6.7: Add JavaScript playback logic**
- Frame-by-frame rendering
- Timing-based delays
- Speed adjustment

**Step 6.8: Update handleRecordingPage**
- Load log and timing files
- Parse into frames
- Render HTML directly

### Verification

1. **Playback renders**: Terminal appears with content
2. **Animation works**: Content appears progressively
3. **Controls work**: Play/pause, speed, progress bar
4. **Fallback**: Missing timing file shows static content
5. **Colors preserved**: ANSI colors render correctly

---

## File Changes Summary

### Modified Files
- `cmd/swe-swe/templates/host/swe-swe-server/main.go` - Session struct, recording logic, API endpoints, routes

### New Files
- `cmd/swe-swe/templates/host/swe-swe-server/playback/types.go`
- `cmd/swe-swe/templates/host/swe-swe-server/playback/timing.go`
- `cmd/swe-swe/templates/host/swe-swe-server/playback/cleaner.go`
- `cmd/swe-swe/templates/host/swe-swe-server/playback/render.go`

### Runtime Directories
- `/workspace/.swe-swe/recordings/` - Created at runtime for recording files
