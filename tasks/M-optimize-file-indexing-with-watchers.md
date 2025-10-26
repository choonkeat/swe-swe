# Optimize File Indexing with File System Watchers

## Problem
Currently, the fuzzy file matcher in `/cmd/swe-swe/fuzzy_matcher.go` uses periodic re-indexing every 2 minutes to catch new files. This is inefficient because:
- It re-indexes all files even when nothing has changed
- It wastes CPU cycles
- The index can be stale for up to 2 minutes
- The logs show "Periodic re-index completed: 546 files" repeatedly

See the periodic re-indexing code in `/cmd/swe-swe/websocket.go:162-176`

## Solution
Replace periodic re-indexing with file system watchers that only trigger re-indexing when files actually change.

## Implementation Steps

### 1. Add fsnotify dependency
```bash
go get github.com/fsnotify/fsnotify
```

### 2. Modify FuzzyMatcher struct
Add fields to track the watcher:
```go
type FuzzyMatcher struct {
    // existing fields...
    watcher *fsnotify.Watcher
    stopWatcher chan bool
}
```

### 3. Implement file watcher
Create a new method `StartWatching()` that:
- Creates an fsnotify watcher
- Recursively adds all directories to watch (excluding .git, node_modules, etc)
- Listens for file events (Create, Remove, Rename, Write)
- Triggers incremental updates to the file index

### 4. Handle incremental updates
Instead of full re-indexing on every change:
- For file creation: Add the new file to the index
- For file deletion: Remove from the index
- For file rename: Update the path in the index
- For directory changes: Add/remove directory watchers as needed

### 5. Remove periodic re-indexing
Delete the ticker-based re-indexing code in `websocket.go:162-176`

### 6. Handle edge cases
- Initial indexing on startup (keep this)
- Graceful shutdown of watchers
- Handle watcher errors (fall back to manual re-index)
- Debounce rapid file changes (e.g., during git operations)
- Platform-specific considerations for Mac/Linux

## Testing
- Verify file creation is detected immediately
- Verify file deletion updates the index
- Verify moving/renaming files works
- Test with large file operations (git checkout, npm install)
- Ensure no memory leaks from watchers

## Benefits
- Real-time index updates
- Reduced CPU usage
- No more stale index
- Better user experience with immediate file discovery

## Code Locations
- Main fuzzy matcher: `/cmd/swe-swe/fuzzy_matcher.go`
- Websocket initialization: `/cmd/swe-swe/websocket.go:142-177`
- Current periodic re-indexing: `/cmd/swe-swe/websocket.go:162-176`

## Notes
- Consider using a debouncer to batch multiple rapid file changes
- May need platform-specific code for optimal performance
- Consider keeping manual refresh option as fallback