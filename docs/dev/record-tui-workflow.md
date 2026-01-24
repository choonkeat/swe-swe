# Working with record-tui Codebase

## Location

The record-tui source is cloned at:
```
dist/choonkeat/record-tui
```

## Workflow

### 1. Enter the codebase
```bash
cd dist/choonkeat/record-tui
```
Stay in this directory until all record-tui changes are complete.

### 2. Make changes

**Important constraints:**
- `make test` must always pass
- `RenderHTML` (embedded mode) is the **source of truth** - do not modify unless consulted with a very strong reason
- The JS cleaner in `internal/js/cleaner-core.js` must produce identical output to the Go cleaner (tests verify this)

### 3. Test your changes
```bash
make test
```

### 4. Commit and push
```bash
git add -A
git commit -m "description of changes"
git push origin HEAD:dev
```

### 5. Return to swe-swe workspace
```bash
cd /workspace
```

### 6. Update swe-swe to use the new record-tui version
```bash
# Get the latest commit hash from the dev branch
go get github.com/choonkeat/record-tui@dev

# Verify go.mod was updated
grep record-tui go.mod
```

### 7. Update template dependencies
After updating the main `go.mod`, sync the template's dependencies:
```bash
# Copy updated dependencies to template
cp go.mod cmd/swe-swe/templates/host/swe-swe-server/go.mod.txt
cp go.sum cmd/swe-swe/templates/host/swe-swe-server/go.sum.txt

# Rebuild and update golden tests
make build golden-update
```

## Key Files in record-tui

| File | Purpose |
|------|---------|
| `internal/html/template.go` | Embedded HTML rendering (source of truth) |
| `internal/html/template_streaming.go` | Streaming HTML rendering |
| `internal/js/cleaner-core.js` | JS cleaner logic (shared by Node.js tests and browser) |
| `internal/session/output.go` | Go cleaner logic |
| `internal/session/compare_output_test.go` | Verifies Go/JS cleaner parity |

## Testing

The test suite ensures:
1. Go and JS cleaners produce **byte-identical output**
2. Both cleaners handle edge cases (headers, footers, clear sequences) consistently

If tests fail after changes, the Go and JS implementations have diverged.
