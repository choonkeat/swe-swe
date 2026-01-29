# Feature: Add rename capability for recordings

**Date:** 2026-01-29
**Status:** Open
**Priority:** Low
**Type:** Feature Request

## Summary

Allow users to rename recordings from the homepage UI. The metadata infrastructure already exists (`Name` field in `RecordingMetadata`), but there's no API or UI to modify it.

## Current State

Recordings are stored in `.swe-swe/recordings/` with files:
- `session-{UUID}.log` - terminal output
- `session-{UUID}.timing` - playback timing
- `session-{UUID}.metadata.json` - metadata with existing `Name` field

The `RecordingMetadata` struct already has a `Name` field (`cmd/swe-swe/templates/host/swe-swe-server/main.go`):

```go
type RecordingMetadata struct {
    UUID           string
    Name           string     // <-- Already exists, just not editable
    Agent          string
    StartedAt      time.Time
    EndedAt        *time.Time
    // ...
}
```

## Proposed Implementation

### 1. API Endpoint

Add handler in `cmd/swe-swe/templates/host/swe-swe-server/main.go`:

```go
// PATCH /api/recording/{UUID}/rename
// Request: {"name": "new name"}
// Response: 200 OK or 400/404/500 error
```

**Validation** (reuse session rename pattern):
- Max 256 characters
- Allowed characters: alphanumeric, spaces, hyphens, underscores, slashes, dots, @

**Logic:**
1. Extract UUID from path
2. Read `session-{UUID}.metadata.json`
3. Validate new name
4. Update `Name` field
5. Write back to disk with `json.MarshalIndent`
6. Return success

### 2. Router Registration

Add route near other recording endpoints (~line 2900):

```go
mux.HandleFunc("PATCH /api/recording/{uuid}/rename", handleRecordingRename)
```

### 3. UI Changes

In `cmd/swe-swe/templates/host/swe-swe-server/static/homepage-main.js`:

1. Add rename button to recording cards (similar to delete/keep buttons)
2. Add simple prompt or inline edit for new name
3. Call API endpoint on submit
4. Update card display on success

### Example Handler Code

```go
func handleRecordingRename(w http.ResponseWriter, r *http.Request) {
    uuid := r.PathValue("uuid")
    if len(uuid) < 32 {
        http.Error(w, "Invalid recording UUID", http.StatusBadRequest)
        return
    }

    var req struct {
        Name string `json:"name"`
    }
    if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
        http.Error(w, "Invalid request body", http.StatusBadRequest)
        return
    }

    // Validate name (reuse session validation pattern)
    if len(req.Name) > 256 {
        http.Error(w, "Name too long (max 256 characters)", http.StatusBadRequest)
        return
    }
    if req.Name != "" && !isValidSessionName(req.Name) {
        http.Error(w, "Invalid characters in name", http.StatusBadRequest)
        return
    }

    metadataPath := filepath.Join(recordingsDir, "session-"+uuid+".metadata.json")
    data, err := os.ReadFile(metadataPath)
    if err != nil {
        if os.IsNotExist(err) {
            http.Error(w, "Recording not found", http.StatusNotFound)
            return
        }
        http.Error(w, "Failed to read metadata", http.StatusInternalServerError)
        return
    }

    var metadata RecordingMetadata
    if err := json.Unmarshal(data, &metadata); err != nil {
        http.Error(w, "Failed to parse metadata", http.StatusInternalServerError)
        return
    }

    metadata.Name = req.Name

    newData, err := json.MarshalIndent(metadata, "", "  ")
    if err != nil {
        http.Error(w, "Failed to encode metadata", http.StatusInternalServerError)
        return
    }

    if err := os.WriteFile(metadataPath, newData, 0644); err != nil {
        http.Error(w, "Failed to save metadata", http.StatusInternalServerError)
        return
    }

    w.WriteHeader(http.StatusOK)
}
```

## Why This Is Easy

| Aspect | Complexity | Reason |
|--------|------------|--------|
| File operations | None | UUID stays in filename, only metadata changes |
| Active recording | Allow | No harm in renaming while recording |
| Validation | Reuse | Same rules as session rename |
| UI | Simple | Add button + prompt to existing card |

## Comparison: Session vs Recording Rename

| Aspect | Session Rename | Recording Rename |
|--------|---------------|------------------|
| File operations | Rename directory | None |
| WebSocket broadcast | Yes | No |
| Child propagation | Yes | No |
| Lines of code | ~50 | ~30 |

## Files to Modify

| File | Changes |
|------|---------|
| `cmd/swe-swe/templates/host/swe-swe-server/main.go` | Add handler + route |
| `cmd/swe-swe/templates/host/swe-swe-server/static/homepage-main.js` | Add UI button + API call |

## Testing

1. Create a recording (start/stop a session)
2. Rename via API: `curl -X PATCH localhost:9898/api/recording/{uuid}/rename -d '{"name":"test"}'`
3. Verify metadata.json updated
4. Verify UI shows new name after refresh
5. Test validation: empty name, long name, invalid characters
