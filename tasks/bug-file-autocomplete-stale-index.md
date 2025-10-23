# Bug: File Autocomplete Doesn't Detect New Files After Boot

## Problem Description
The `@` file mention autocomplete feature doesn't detect new files created after the application starts up. This indicates a caching/indexing issue where the file list becomes stale.

## Root Cause Analysis

### Current Implementation
The file autocomplete is implemented using a fuzzy matcher system with these components:

**Frontend (Elm):**
- `elm/src/Main.elm:294-302` - Triggers fuzzy matcher when "@" is typed
- Displays dropdown with file matches and highlighting

**Backend (Go):**
- `cmd/swe-swe/fuzzy_matcher.go` - Core fuzzy matching and indexing logic
- `cmd/swe-swe/websocket.go:721-750` - WebSocket handler for fuzzy search

### File Indexing Process
1. **One-time indexing**: Files are indexed only once at startup in `NewChatService()`
2. **Directory walking**: Uses `filepath.WalkDir()` to scan entire directory tree
3. **Built-in exclusions**: Excludes common directories (node_modules, .git, etc.)
4. **Gitignore support**: Loads .gitignore patterns for additional exclusions
5. **Storage**: Files stored in static `[]FileInfo` slice

### The Bug
**Critical Issue: No automatic re-indexing**

The `FuzzyMatcher` struct performs indexing only once:
```go
// In NewChatService() - cmd/swe-swe/main.go
go func() {
    if err := fuzzyMatcher.IndexFiles(); err != nil {
        log.Printf("Failed to index files: %v", err)
    } else {
        log.Printf("Indexed %d files for fuzzy matching", fuzzyMatcher.GetFileCount())
    }
}()
```

**Problems:**
1. **Static file list**: Once indexed, `fm.files` is never updated
2. **No file system watching**: No mechanism to detect file changes
3. **No periodic refresh**: No scheduled re-indexing 
4. **No manual refresh**: No API to trigger re-indexing

**Code Location**: `cmd/swe-swe/fuzzy_matcher.go:124-188` (`IndexFiles()` method)

## Impact
- Users cannot autocomplete newly created files
- File autocomplete becomes increasingly incomplete over time
- Workflow disruption when working with new files

## Fix Plan

### Option 1: Periodic Re-indexing (Simple)
Add background goroutine that re-indexes files every N seconds:
```go
// In NewChatService()
go func() {
    ticker := time.NewTicker(30 * time.Second)
    defer ticker.Stop()
    for range ticker.C {
        fuzzyMatcher.IndexFiles()
    }
}()
```

### Option 2: File System Watching (Optimal)  
Use `fsnotify` library to watch for file system changes:
```go
import "github.com/fsnotify/fsnotify"

func (fm *FuzzyMatcher) StartWatching() {
    watcher, err := fsnotify.NewWatcher()
    // Watch directory and re-index on changes
}
```

### Option 3: Manual Refresh Command (Quick fix)
Add WebSocket message type for manual re-indexing:
```go
// In websocket.go message handler
case "refresh_files":
    go func() {
        c.fuzzyMatcher.IndexFiles()
        // Send completion notification
    }()
```

### Option 4: Lazy Re-indexing (Hybrid)
Re-index when file count seems stale (e.g., after 5+ minutes):
```go
func (fm *FuzzyMatcher) Search(query string) []FileMatch {
    if time.Since(fm.lastUpdate) > 5*time.Minute {
        go fm.IndexFiles()
    }
    // Continue with search...
}
```

## Recommended Solution
**Option 2 (File System Watching)** is optimal for user experience but requires adding dependency.
**Option 1 (Periodic Re-indexing)** is simpler to implement and provides good-enough experience.
**Option 3 (Manual Refresh)** can be added as immediate workaround.

## Implementation Priority
1. **Immediate**: Add manual refresh command (Option 3)
2. **Short-term**: Implement periodic re-indexing (Option 1)  
3. **Long-term**: Consider file system watching (Option 2)

## Files to Modify
- `cmd/swe-swe/fuzzy_matcher.go` - Add refresh methods
- `cmd/swe-swe/websocket.go` - Add refresh message handler
- `elm/src/Main.elm` - Add refresh button/shortcut (optional)