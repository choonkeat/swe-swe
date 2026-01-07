# Recording API HTTP Test Suite

**Created:** 2026-01-07
**Status:** In Progress
**Goal:** Create comprehensive httptest coverage for recording API endpoints to prevent regression bugs

## Background

We fixed a bug where `DELETE /api/recording/{uuid}` returned 409 Conflict for ended sessions because `handleDeleteRecording` didn't check if the process was still running. This same bug pattern had been fixed in `loadEndedRecordings` and `handleListRecordings` in commit `7ddf83a` but was missed for the delete handler.

To prevent future whack-a-mole bugs, we need thorough test coverage for all recording-related functionality.

## Recording API Surface

| Endpoint | Handler | Description |
|----------|---------|-------------|
| `GET /api/recording/list` | `handleListRecordings` | List all recordings with metadata |
| `DELETE /api/recording/{uuid}` | `handleDeleteRecording` | Delete recording files |
| `GET /api/recording/{uuid}/download` | `handleDownloadRecording` | Download recording as zip |
| `GET /recording/{uuid}` | `handleRecordingPage` | Playback page (HTML) |
| (internal) | `loadEndedRecordings` | Homepage recording list |
| (internal) | `loadEndedRecordingsByAgent` | Group recordings by agent |

---

## Phase 1: Test Infrastructure Setup [COMPLETED]

### Goal
Create test file with helpers to mock sessions, recording files, and exercise handlers without PTY/process dependencies.

### Steps
1. [x] Create `cmd/swe-swe/templates/host/swe-swe-server/recording_test.go`
2. [x] Add helper to create temp recordings directory with mock `.log`, `.timing`, `.metadata.json` files
3. [x] Add helper to create mock Session objects with controllable `ProcessState` (nil = running, non-nil = exited)
4. [x] Add helper to register mock sessions in the global `sessions` map
5. [x] Add test cleanup function to reset global state between tests
6. [x] Create httptest server using the existing handlers
7. [x] Change `recordingsDir` from const to var to allow test override

### Verification
- [x] Smoke test: create mock recording, call `GET /api/recording/list`, verify 200 and recording in response

---

## Phase 2: Recording List API Tests [COMPLETED]

### Goal
Test `GET /api/recording/list` with all session/recording states.

### Test Cases
| Test | Setup | Expected | Status |
|------|-------|----------|--------|
| Empty recordings directory | No files | `{"recordings":[]}` | [x] |
| Single ended recording with metadata | .log + .metadata.json | Recording with name, agent, dates | [x] |
| Single ended recording without metadata | .log only | Recording with UUID and size only | [x] |
| Multiple recordings sorted by date | Multiple .log files | Newest first | [x] |
| Active recording (process running) | Session with ProcessState=nil | `is_active: true` | [x] |
| Ended recording (session in map, process exited) | Session with ProcessState!=nil | `is_active: false` | [x] |
| Recording with timing file | .log + .timing | `has_timing: true` | [x] |
| Recording without timing file | .log only | `has_timing: false` | [x] |
| Recordings directory doesn't exist | No directory | Empty list, not error | [x] |

---

## Phase 3: Recording Delete API Tests [COMPLETED]

### Goal
Test `DELETE /api/recording/{uuid}` including the exact bug scenario we fixed.

### Test Cases
| Test | Setup | Expected | Status |
|------|-------|----------|--------|
| Delete non-existent recording | No files | 404 | [x] |
| Delete recording with no session | .log file, no session | 204, files removed | [x] |
| Delete recording with active session | Session with ProcessState=nil | 409 Conflict | [x] |
| Delete recording with ended session | Session with ProcessState!=nil | 204 (the bug fix) | [x] |
| Delete removes all related files | .log + .timing + .metadata.json | All files deleted | [x] |
| Delete with only .log file | .log only | 204 | [x] |
| Invalid UUID format | Short UUID | 400 | [x] |
| Wrong HTTP method | GET /api/recording/{uuid} | 404 | [x] |

---

## Phase 4: Recording Download API Tests [COMPLETED]

### Goal
Test `GET /api/recording/{uuid}/download` for zip creation and edge cases.

### Test Cases
| Test | Setup | Expected | Status |
|------|-------|----------|--------|
| Download non-existent recording | No files | 404 | [x] |
| Download with all files | .log + .timing + .metadata.json | Zip with all 3 files | [x] |
| Download with only .log | .log only | Zip with session.log | [x] |
| Download with .log and .timing | .log + .timing | Zip with 2 files | [x] |
| Content-Type header | Any recording | `application/zip` | [x] |
| Content-Disposition header | Any recording | `attachment; filename="recording-{uuid8}.zip"` | [x] |
| Invalid UUID format | Short UUID | 400 | [x] |
| Zip contents valid | Any recording | Unzip succeeds, contents match | [x] |

---

## Phase 5: Recording Playback Page Tests [COMPLETED]

### Goal
Test `GET /recording/{uuid}` HTML rendering for animated and static modes.

### Test Cases
| Test | Setup | Expected | Status |
|------|-------|----------|--------|
| Non-existent recording | No files | 404 | [x] |
| Invalid UUID format | Short UUID | 400 | [x] |
| Animated mode (with timing) | .log + .timing | HTML with play/pause, timeline, frame data | [x] |
| Static mode (no timing) | .log only | HTML with static notice, xterm.js, no playback controls | [x] |
| Recording with metadata | .metadata.json with name | Page title uses name | [x] |
| Recording without metadata | No .metadata.json | Page title uses `session-{uuid8}` | [x] |
| Content-Type header | Any recording | `text/html; charset=utf-8` | [x] |
| Back link | Any recording | Contains link to `/` | [x] |
| Max dimensions from metadata | .metadata.json with max_cols/rows | Passed to renderer | (covered in animated mode) |

### Notes
- Static mode shows "Timing data not available" notice and renders final state using xterm.js
- Content is base64-encoded in JavaScript for proper handling of terminal escape sequences

---

## Phase 6: Homepage Recording Display Tests

### Goal
Test internal functions that populate homepage recording lists.

### Test Cases for `loadEndedRecordings`
| Test | Setup | Expected |
|------|-------|----------|
| Empty directory | No files | Empty slice |
| Directory missing | No recordings dir | nil (no error) |
| Excludes active recordings | Session with ProcessState=nil | Not in list |
| Includes ended recordings | Session with ProcessState!=nil | In list |
| Includes orphan recordings | No session in map | In list |
| EndedAgo from metadata.ended_at | .metadata.json with ended_at | Correct relative time |
| EndedAgo from file mtime | No metadata | Uses file modification time |

### Test Cases for `loadEndedRecordingsByAgent`
| Test | Setup | Expected |
|------|-------|----------|
| Groups by agent binary name | Multiple recordings | Grouped correctly |
| Maps display names to binary | Recording with agent="Claude" | Grouped under "claude" |
| Handles unknown agents | Recording with agent="Unknown" | Falls back to lowercase |

---

## Implementation Notes

### Mock Session Creation
```go
// Create a mock session with controllable process state
func createMockSession(uuid, recordingUUID string, processExited bool) *Session {
    sess := &Session{
        UUID:          uuid,
        RecordingUUID: recordingUUID,
        Cmd:           &exec.Cmd{},
        // ...
    }
    if processExited {
        // Set ProcessState to non-nil to indicate process exited
        sess.Cmd.ProcessState = &os.ProcessState{}
    }
    return sess
}
```

### Test Isolation
- Each test should use `t.TempDir()` for recordings directory
- Override `recordingsDir` package variable or pass as parameter
- Clear `sessions` map before each test
- Use `t.Cleanup()` to restore state

### File Structure
```
cmd/swe-swe/templates/host/swe-swe-server/
├── main.go
├── recording_test.go  # New test file
└── playback/
    ├── types.go
    ├── timing.go
    └── render.go
```
