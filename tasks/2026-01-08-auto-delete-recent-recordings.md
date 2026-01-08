# Auto-delete Recent Recordings

## Goal

Implement auto-cleanup for recordings with a "Recent vs Kept" model:
- **Recent recordings**: Keep max 5 per agent, auto-delete after 1h (whichever limit hits first)
- **Kept recordings**: Persist forever, user explicitly keeps, manually delete only

## UI Design

```
┌─────────────────────────────────────────────────────────────┐
│ Claude                                                      │
├─────────────────────────────────────────────────────────────┤
│ Active sessions                                             │
│ ┌─────────────────────────────────────────────────────────┐ │
│ │ fix-auth          2 viewers                      15m    │ │
│ └─────────────────────────────────────────────────────────┘ │
│ ┌ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ┐ │
│   + Start new session                                       │
│ └ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ┘ │
│                                                             │
│ Recent (last 5, auto-deletes after 1h)                      │
│ ┌─────────────────────────────────────────────────────────┐ │
│ │ session-abc123         15m ago          [View] [Keep]   │ │
│ │ session-def456         45m ago          [View] [Keep]   │ │
│ │ session-ghi789         58m ago          [View] [Keep]   │ │
│ └─────────────────────────────────────────────────────────┘ │
│                                                             │
│ Kept                                                        │
│ ┌─────────────────────────────────────────────────────────┐ │
│ │ fix-login-bug          2 days ago       [View] [Delete] │ │
│ │ demo-for-client        1 week ago       [View] [Delete] │ │
│ └─────────────────────────────────────────────────────────┘ │
└─────────────────────────────────────────────────────────────┘
```

---

## Phase 1: Data model - Add `kept_at` field to `RecordingMetadata`

- [x] Step 1.1: Add `KeptAt *time.Time` field to `RecordingMetadata` struct in `main.go`
- [x] Step 1.2: Add `KeptAt` field to `RecordingInfo` struct (for homepage display)
- [x] Step 1.3: Add `KeptAt` field to `RecordingListItem` struct (for API response)
- [x] Step 1.4: Update `loadEndedRecordings()` to populate `KeptAt` from metadata

### Verification
- Run `go build` to ensure no compilation errors
- Run existing tests `go test ./...` to ensure no regressions
- Manually verify existing recordings still load (field is optional/omitempty)

---

## Phase 2: Keep API - Add endpoint to keep a recording

- [x] Step 2.1: Add route handling for `POST /api/recording/{uuid}/keep` in `handleRecordingAPI()`
- [x] Step 2.2: Implement `handleKeepRecording()` function that reads metadata, sets `KeptAt`, writes back
- [x] Step 2.3: Add test `TestKeepRecording_Success`
- [x] Step 2.4: Add test `TestKeepRecording_NotFound`
- [x] Step 2.5: Add test `TestKeepRecording_AlreadyKept`

### Verification
- Run `go test ./...` to ensure all tests pass

---

## Phase 3: Cleanup logic - Add auto-deletion in `sessionReaper()`

- [ ] Step 3.1: Add `cleanupRecentRecordings()` function that scans, groups by agent, deletes old/excess
- [ ] Step 3.2: Call `cleanupRecentRecordings()` from `sessionReaper()` loop
- [ ] Step 3.3: Add test `TestCleanupRecentRecordings_DeletesOldUnkept`
- [ ] Step 3.4: Add test `TestCleanupRecentRecordings_KeepsKeptRecordings`
- [ ] Step 3.5: Add test `TestCleanupRecentRecordings_KeepsRecentUnkept`
- [ ] Step 3.6: Add test `TestCleanupRecentRecordings_DeletesBeyondLimit`
- [ ] Step 3.7: Add test `TestCleanupRecentRecordings_LimitPerAgent`

### Verification
- Run `go test ./...` to ensure all tests pass

---

## Phase 4: UI updates - Update homepage to show Recent vs Kept sections

- [ ] Step 4.1: Update `RecordingInfo` struct to include `IsKept bool`
- [ ] Step 4.2: Update `loadEndedRecordings()` to set `IsKept` based on `KeptAt != nil`
- [ ] Step 4.3: Update `AgentWithSessions` to have `RecentRecordings` and `KeptRecordings` fields
- [ ] Step 4.4: Update homepage handler to populate both recording lists
- [ ] Step 4.5: Update `selection.html` template with Recent and Kept sections
- [ ] Step 4.6: Add JavaScript `keepRecording(uuid, button)` function
- [ ] Step 4.7: Update CSS for any new elements

### Verification
- Manual testing via browser
- Run `go test ./...` to ensure no regressions
