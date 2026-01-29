# Bug: Session Name Extraction Too Strict for SSH URLs with Subdirectories

**Status: FIXED** (2026-01-29)

## Summary

New sessions from external git clones don't set the session name properly. The expected format is `{owner}/{repo}@{branch}`, but the pattern matching for SSH URLs returns the entire path after the colon instead of just the last two segments.

## Root Cause

**File**: `cmd/swe-swe/templates/host/swe-swe-server/main.go:2906-2935`

The `extractOwnerRepo()` function handles SSH URLs incorrectly when there are subdirectories (GitLab groups, nested namespaces, etc.):

```go
func extractOwnerRepo(gitURL string) string {
    gitURL = strings.TrimSpace(gitURL)
    gitURL = strings.TrimSuffix(gitURL, ".git")

    // Handle SSH format: git@github.com:owner/repo
    if strings.HasPrefix(gitURL, "git@") {
        // git@github.com:owner/repo -> owner/repo
        if idx := strings.Index(gitURL, ":"); idx != -1 {
            return gitURL[idx+1:]  // <-- BUG: returns EVERYTHING after ":"
        }
    }
    // ...HTTPS case correctly uses last 2 segments...
}
```

### Bug Behavior

| URL Format | Input | Expected | Actual |
|------------|-------|----------|--------|
| SSH (simple) | `git@github.com:owner/repo` | `owner/repo` | `owner/repo` |
| SSH (nested) | `git@gitlab.com:group/subgroup/owner/repo` | `owner/repo` | `group/subgroup/owner/repo` |
| HTTPS (nested) | `https://gitlab.com/group/subgroup/owner/repo` | `owner/repo` | `owner/repo` |

The HTTPS case correctly extracts the last two path segments using:
```go
parts := strings.Split(gitURL, "/")
if len(parts) >= 2 {
    return parts[len(parts)-2] + "/" + parts[len(parts)-1]
}
```

But the SSH case simply returns everything after the colon.

## Proposed Fix

Make the SSH case also extract only the last two path segments:

```go
// Handle SSH format: git@github.com:owner/repo or git@gitlab.com:group/subgroup/owner/repo
if strings.HasPrefix(gitURL, "git@") {
    if idx := strings.Index(gitURL, ":"); idx != -1 {
        pathPart := gitURL[idx+1:]
        parts := strings.Split(pathPart, "/")
        if len(parts) >= 2 {
            return parts[len(parts)-2] + "/" + parts[len(parts)-1]
        }
        return pathPart // fallback for edge cases
    }
}
```

## Affected Code Paths

1. **Session name derivation**: `deriveDefaultSessionName()` at line 2937-2958
   - Calls `getRepoOriginURL()` → `extractOwnerRepo()`
   - Used when creating new sessions for external repos

2. **Similar function in init.go**: `deriveAliasFromURL()` at `cmd/swe-swe/init.go:158-189`
   - Has a more robust implementation that handles the `:` case within the path segments

## Test Cases to Add

```go
func TestExtractOwnerRepo(t *testing.T) {
    tests := []struct {
        input    string
        expected string
    }{
        // Standard cases
        {"https://github.com/owner/repo", "owner/repo"},
        {"https://github.com/owner/repo.git", "owner/repo"},
        {"git@github.com:owner/repo", "owner/repo"},
        {"git@github.com:owner/repo.git", "owner/repo"},

        // Nested paths (GitLab groups, self-hosted with subdirs)
        {"https://gitlab.com/group/subgroup/owner/repo", "owner/repo"},
        {"git@gitlab.com:group/subgroup/owner/repo", "owner/repo"},
        {"https://git.company.com/internal/projects/team/owner/repo", "owner/repo"},
        {"git@git.company.com:internal/projects/team/owner/repo", "owner/repo"},

        // Other protocols
        {"ssh://git@github.com/owner/repo", "owner/repo"},
        {"git://github.com/owner/repo", "owner/repo"},
        {"http://github.com/owner/repo", "owner/repo"},
    }

    for _, tt := range tests {
        result := extractOwnerRepo(tt.input)
        if result != tt.expected {
            t.Errorf("extractOwnerRepo(%q) = %q, want %q", tt.input, result, tt.expected)
        }
    }
}
```

## Related Files

- `cmd/swe-swe/templates/host/swe-swe-server/main.go` - Primary fix location
- `cmd/swe-swe/init.go` - Reference implementation at `deriveAliasFromURL()`
- Golden test files will need update after `make build golden-update`

## Priority

Medium - Affects user experience for external repos with nested namespaces (common in GitLab, Gitea, and enterprise self-hosted Git servers).
