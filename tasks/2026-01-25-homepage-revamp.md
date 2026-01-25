# Homepage Revamp: Interleaved Recordings + New Session Dialog

## Goal

Revamp the swe-swe homepage to:
1. Interleave recordings (kept and not-kept) sorted by session end time descending
2. Add "New Session" dialog with progressive form: Repository URL → Branch → Agent → Start

## Design Summary

### Interleaved Recordings
- Single list combining kept and not-kept recordings
- Sorted by `EndedAt` descending
- Each row shows status ("Saved" or "Expires in Xm") and appropriate actions

### New Session Dialog

**Structure:**
```
┌─────────────────────────────────────────────────────┐
│  + New Session                                   X  │
├─────────────────────────────────────────────────────┤
│  Repository                                         │
│  [combo box with history    ▼] [Next]               │
│                                                     │
│  Branch (optional)                      [disabled]  │
│  [combo box with branches   ▼] [Next]               │
│                                                     │
│  Agent                                  [disabled]  │
│  ○ Claude  ○ Gemini  ○ Aider  ...                   │
│                                                     │
│                              [ Start Session ]      │
└─────────────────────────────────────────────────────┘
```

**Path Resolution:**
| Repo | Branch | Working Directory |
|------|--------|-------------------|
| `/workspace` | (blank) | `/workspace` |
| `/workspace` | `feature-x` | `/worktrees/feature-x` |
| External URL | (blank) | `/repos/{sanitized-url}/workspace` |
| External URL | `feature-x` | `/repos/{sanitized-url}/worktree/feature-x` |

**URL Sanitization:** Replace invalid filesystem characters with `-`

**Branch Algorithm:**
```
if branch is blank:
    pwd = base_workspace (no worktree)
else if local_branch_exists(branch) or remote_branch_exists("origin/" + branch):
    git worktree add {path} {branch}  # checkout existing
else:
    git worktree add -b {branch} {path}  # create new
```

**State Transitions:**
1. Repo Next → loading → git fetch/clone → populate branches → enable Branch
2. Branch Next (or select) → enable Agent
3. Agent select → enable Start Session
4. Changing earlier field → resets downstream fields

---

## Phase 1: Interleave Recordings ✅ COMPLETED

### What will be achieved
The recordings section displays all recordings in a single interleaved list, sorted by session end time descending.

### Steps
1. ✅ **Modify Go backend** (`main.go`): Added `ExpiresIn` field to `RecordingInfo` struct, calculate remaining time until auto-deletion
2. ✅ **Update sorting logic**: Recordings already sorted by `EndedAt` descending in `loadEndedRecordings()`
3. ✅ **Modify HTML template** (`selection.html`): Replaced two recording sections with single loop, shows "Saved"/"Expires in Xm" status and Keep/Delete buttons conditionally

### Verification
1. ✅ `make test` - all tests pass
2. ✅ `make build golden-update` - golden tests updated
3. ✅ **Manual browser test**:
   - Started test container, navigated to homepage
   - Created two sessions, kept one, left other expiring
   - Verified single "Recordings" section with interleaved recordings
   - Verified "Saved" recordings show Delete button, "Expires in Xm" show Keep button
   - Verified sorting by end time descending (most recent first)

---

## Phase 2: New Session Dialog UI

### What will be achieved
Modal dialog with repo URL combo box, branch combo box, agent selection, and Start Session button with progressive enablement.

### Steps
1. **Add dialog HTML structure** (`selection.html`):
   - Modal overlay with form layout
   - Combo box for repo URL with datalist for history
   - "Next" button for repo step
   - Combo box for branch with datalist (initially disabled)
   - "Next" button for branch step (initially disabled)
   - Agent radio buttons (initially disabled)
   - "Start Session" button (initially disabled)
   - Loading spinner element
   - Error message display area

2. **Add dialog CSS**:
   - Modal positioning and backdrop
   - Combo box styling consistent with dark theme
   - Disabled state styling
   - Loading spinner animation
   - Error message styling

3. **Add dialog open/close logic**:
   - Wire "+ New Session" button to open dialog
   - Close on X button, ESC key, backdrop click
   - Reset form state on close

4. **Add localStorage integration**:
   - Load repo URL history on page load
   - Populate datalist with history
   - Save new URLs to history on successful prepare

5. **Add progressive enablement logic** (UI only):
   - Repo Next → enables branch field
   - Branch Next (or select) → enables agent selection
   - Agent select → enables Start Session button
   - Changing earlier field → resets downstream fields

### Verification
1. `make build golden-update` - verify template changes
2. **Manual browser test**:
   - Click "+ New Session" → dialog opens
   - Verify branch/agent/start disabled initially
   - Enter repo URL, click Next → branch enables
   - Select/enter branch → agent enables
   - Select agent → Start Session enables
   - Change repo URL → downstream resets
   - Close via X, ESC, backdrop → form resets

---

## Phase 3: Git Operations Backend

### What will be achieved
New API endpoints: `POST /api/repo/prepare` and `GET /api/repo/branches`

### Steps
1. **Add URL sanitization function**:
   - Replace invalid filesystem characters with `-`
   - Handle https, git@, ssh:// formats

2. **Add `POST /api/repo/prepare` endpoint**:
   - Input: `{ "url": "https://..." }`
   - If URL matches `/workspace` origin: `git fetch --all`
   - Else: clone to `/repos/{sanitized-url}/workspace` (skip if exists)
   - Return: `{ "path": "/repos/...", "isWorkspace": bool }`
   - Return error JSON on failure

3. **Add `GET /api/repo/branches` endpoint**:
   - Input: `?path=/repos/...` or `?path=/workspace`
   - Run `git branch -a` in directory
   - Return: `{ "branches": ["main", "origin/feature-x", ...] }`

4. **Add path resolution function**:
   - `(repoPath, branchName)` → working directory
   - Branch blank: return repoPath
   - `/workspace` + branch: `/worktrees/{branch}`
   - External + branch: `/repos/{sanitized-url}/worktree/{branch}`

5. **Add worktree creation logic**:
   - Existing worktree path → use it
   - Local branch exists → `git worktree add {path} {branch}`
   - Remote branch exists → `git worktree add --track -b {branch} {path} origin/{branch}`
   - New branch → `git worktree add -b {branch} {path}`

### Verification
1. Add unit tests for URL sanitization
2. `make build golden-update`
3. **Manual test via curl**:
   - `POST /api/repo/prepare` with valid URL → returns path
   - `GET /api/repo/branches?path=/workspace` → returns branches
   - Verify `/repos/` directory structure
   - Test error cases

---

## Phase 4: Dialog Logic & Integration

### What will be achieved
Wire dialog UI to backend APIs, handle loading/errors, start sessions with correct pwd.

### Steps
1. **Wire Repo "Next" button**:
   - Show loading, disable inputs
   - Call `POST /api/repo/prepare`
   - On success: call `/api/repo/branches`, populate datalist, enable branch
   - On error: show error message
   - Save URL to localStorage on success

2. **Wire Branch "Next" / selection**:
   - Dropdown select → auto-advance
   - Custom value + Next → advance
   - Store branch name

3. **Wire Agent selection**:
   - On change → enable Start Session
   - Store agent

4. **Wire "Start Session" button**:
   - Show loading
   - Compute working directory
   - Create worktree if needed
   - Navigate to session URL

5. **Modify session creation**:
   - Accept `?pwd=...` parameter
   - Create worktree before starting PTY
   - Start PTY in specified directory

6. **Handle edge cases**:
   - Empty branch = base workspace
   - Existing worktree = reuse
   - Default repo URL from `/workspace/.git/config`

### Verification
1. `make build golden-update`
2. **Full flow test** (MCP browser):
   - Use default `/workspace` URL
   - Select branch, agent
   - Start session → verify correct pwd

3. **External repo test**:
   - Enter GitHub URL
   - Clone, select branch, start session
   - Verify `/repos/.../worktree/...` path

4. **Error handling test**:
   - Invalid URL → error shown
   - Dialog remains usable after errors

5. **Blank branch test**:
   - Leave branch empty
   - Session starts in base workspace (no worktree)

---

## Testing Workflow

For all phases:
```bash
# 1. Edit templates
vim cmd/swe-swe/templates/host/swe-swe-server/...

# 2. Build and update golden tests
make build golden-update
git diff --cached -- cmd/swe-swe/testdata/golden

# 3. Start test container
./scripts/test-container/01-init.sh
./scripts/test-container/02-build.sh
./scripts/test-container/03-run.sh

# 4. Test via MCP browser at http://host.docker.internal:19770/

# 5. Teardown
./scripts/test-container/04-down.sh
```

## Files to Modify

- `cmd/swe-swe/templates/host/swe-swe-server/main.go` - Backend logic
- `cmd/swe-swe/templates/host/swe-swe-server/static/selection.html` - Homepage UI
