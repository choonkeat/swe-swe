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

## Phase 2: Recording List API Tests

### Goal
Test `GET /api/recording/list` with all session/recording states.

### Test Cases
| Test | Setup | Expected |
|------|-------|----------|
| Empty recordings directory | No files | `{"recordings":[]}` |
| Single ended recording with metadata | .log + .metadata.json | Recording with name, agent, dates |
| Single ended recording without metadata | .log only | Recording with UUID and size only |
| Multiple recordings sorted by date | Multiple .log files | Newest first |
| Active recording (process running) | Session with ProcessState=nil | `is_active: true` |
| Ended recording (session in map, process exited) | Session with ProcessState!=nil | `is_active: false` |
| Recording with timing file | .log + .timing | `has_timing: true` |
| Recording without timing file | .log only | `has_timing: false` |
| Recordings directory doesn't exist | No directory | Empty list, not error |

---

## Phase 3: Recording Delete API Tests

### Goal
Test `DELETE /api/recording/{uuid}` including the exact bug scenario we fixed.

### Test Cases
| Test | Setup | Expected |
|------|-------|----------|
| Delete non-existent recording | No files | 404 |
| Delete recording with no session | .log file, no session | 204, files removed |
| Delete recording with active session | Session with ProcessState=nil | 409 Conflict |
| Delete recording with ended session | Session with ProcessState!=nil | 204 (the bug fix) |
| Delete removes all related files | .log + .timing + .metadata.json | All files deleted |
| Delete with only .log file | .log only | 204 |
| Invalid UUID format | Short UUID | 400 |
| Wrong HTTP method | GET /api/recording/{uuid} | 404 |

---

## Phase 4: Recording Download API Tests

### Goal
Test `GET /api/recording/{uuid}/download` for zip creation and edge cases.

### Test Cases
| Test | Setup | Expected |
|------|-------|----------|
| Download non-existent recording | No files | 404 |
| Download with all files | .log + .timing + .metadata.json | Zip with all 3 files |
| Download with only .log | .log only | Zip with session.log |
| Download with .log and .timing | .log + .timing | Zip with 2 files |
| Content-Type header | Any recording | `application/zip` |
| Content-Disposition header | Any recording | `attachment; filename="recording-{uuid8}.zip"` |
| Invalid UUID format | Short UUID | 400 |
| Zip contents valid | Any recording | Unzip succeeds, contents match |

---

## Phase 5: Recording Playback Page Tests

### Goal
Test `GET /recording/{uuid}` HTML rendering for animated and static modes.

### Test Cases
| Test | Setup | Expected |
|------|-------|----------|
| Non-existent recording | No files | 404 |
| Invalid UUID format | Short UUID | 400 |
| Animated mode (with timing) | .log + .timing | HTML with play/pause, timeline, frame data |
| Static mode (no timing) | .log only | HTML with static output, no controls |
| Recording with metadata | .metadata.json with name | Page title uses name |
| Recording without metadata | No .metadata.json | Page title uses `session-{uuid8}` |
| Content-Type header | Any recording | `text/html; charset=utf-8` |
| Back link | Any recording | Contains link to `/` |
| Max dimensions from metadata | .metadata.json with max_cols/rows | Passed to renderer |

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
