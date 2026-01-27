# New Session Dialog Redesign

**Date:** 2026-01-27
**Status:** Complete (pending golden update)

## Problem

First-time users hitting SSH/credentials errors when trying to clone repos:
```
Git fetch failed: Host key verification failed.
fatal: Could not read from remote repository.
```

Current UX shows hard error for any git operation failure, blocking the user.

## Solution

Redesign "New Session" dialog with 3 distinct paths via dropdown (Option C):

```
┌──────────────────────────────────────────────────────────────────┐
│                        New Session Dialog                         │
├──────────────────────────────────────────────────────────────────┤
│  Repository Type: [ Default workspace ▼ ]                        │
│                   ├─ Default workspace (/workspace)              │
│                   ├─ Clone external repository                   │
│                   └─ Create new project                          │
├──────────────────────────────────────────────────────────────────┤
│                                                                  │
│  Path 1: WORKSPACE          Path 2: CLONE         Path 3: CREATE │
│  ─────────────────          ───────────────       ─────────────── │
│  • No input needed          • URL input           • Name input    │
│  • git fetch (soft fail)    • git clone           • mkdir + init  │
│  • Show warning if fail     • HARD FAIL           • No branches   │
│  • Use cached branches      • if clone fails      • Skip to agent │
│                                                                   │
└──────────────────────────────────────────────────────────────────┘
```

## Implementation Progress

### Backend (DONE)

**File:** `cmd/swe-swe/templates/host/swe-swe-server/main.go`

Changed `handleRepoPrepareAPI` to accept:
```json
{
  "mode": "workspace|clone|create",
  "url": "...",   // for clone mode
  "name": "..."   // for create mode
}
```

Created 3 handler functions:
- [x] `handleRepoPrepareWorkspace()` - soft fail on fetch, returns `warning` field
- [x] `handleRepoPrepareClone()` - hard fail on git errors
- [x] `handleRepoPrepareCreate()` - validates name, mkdir, git init

**Response format:**
```json
{
  "path": "/workspace",
  "isWorkspace": true,
  "warning": "Unable to fetch latest changes. Using cached branches.",  // optional
  "isNew": true  // only for create mode
}
```

### Frontend (DONE)

**File:** `cmd/swe-swe/templates/host/swe-swe-server/static/selection.html`

#### 1. Add CSS for new elements (DONE)

Add styles for:
- `.dialog__select` - dropdown styling
- `.dialog__warning` - soft warning text (amber/yellow, not red)
- `.dialog__field--hidden` - hide fields based on mode

```css
.dialog__select {
    flex: 1;
    padding: 12px 14px;
    font-size: 14px;
    background: #0f172a;
    border: 1px solid #334155;
    border-radius: 8px;
    color: #f8fafc;
    font-family: inherit;
    cursor: pointer;
}
.dialog__select:focus {
    outline: none;
    border-color: #7c3aed;
}
.dialog__warning {
    color: #f59e0b;
    font-size: 12px;
    margin-top: 4px;
}
.dialog__field--hidden {
    display: none;
}
```

#### 2. Update Dialog HTML (lines 862-906) (DONE)

Replace current Repository field with:

```html
<!-- Mode selector -->
<div class="dialog__field">
    <label class="dialog__label">Repository Type</label>
    <select class="dialog__select" id="new-session-mode">
        <option value="workspace">Default workspace (/workspace)</option>
        <option value="clone">Clone external repository</option>
        <option value="create">Create new project</option>
    </select>
</div>

<!-- Clone URL field (shown when mode=clone) -->
<div class="dialog__field dialog__field--hidden" id="clone-url-field">
    <label class="dialog__label">Repository URL</label>
    <div class="dialog__row">
        <input type="text" class="dialog__input" id="new-session-url"
               list="repo-history" placeholder="https://github.com/... or git@github.com:...">
        <datalist id="repo-history"></datalist>
    </div>
</div>

<!-- Project name field (shown when mode=create) -->
<div class="dialog__field dialog__field--hidden" id="create-name-field">
    <label class="dialog__label">Project Name</label>
    <div class="dialog__row">
        <input type="text" class="dialog__input" id="new-session-name"
               placeholder="my-project (letters, numbers, dashes)">
    </div>
</div>

<!-- Next button (prepare repo) -->
<div class="dialog__field">
    <button class="dialog__next" id="new-session-prepare" style="width: 100%;">Next</button>
    <div class="dialog__warning" id="new-session-warning"></div>
</div>

<!-- Branch field (hidden for create mode) -->
<div class="dialog__field" id="branch-field">
    <label class="dialog__label">Branch (optional)</label>
    <div class="dialog__row">
        <input type="text" class="dialog__input" id="new-session-branch"
               list="branch-list" placeholder="Leave blank for default branch" disabled>
        <datalist id="branch-list"></datalist>
        <button class="dialog__next" id="new-session-branch-next" disabled>Next</button>
    </div>
</div>
```

#### 3. Update JavaScript (lines 1198-1483) (DONE)

Key changes needed:

```javascript
// New state
var dialogState = {
    sessionUUID: '',
    debug: false,
    mode: 'workspace',      // NEW
    repoPath: '',
    selectedBranch: '',
    selectedAgent: '',
    preSelectedAgent: '',
    isNewProject: false     // NEW
};

// Mode change handler
modeSelect.addEventListener('change', function() {
    dialogState.mode = modeSelect.value;

    // Show/hide relevant fields
    cloneUrlField.classList.toggle('dialog__field--hidden', dialogState.mode !== 'clone');
    createNameField.classList.toggle('dialog__field--hidden', dialogState.mode !== 'create');

    // Reset downstream fields
    resetBranchAndAgent();
});

// Prepare button handler
prepareBtn.addEventListener('click', function() {
    var body = { mode: dialogState.mode };

    if (dialogState.mode === 'clone') {
        body.url = urlInput.value.trim();
        if (!body.url) {
            showError('Please enter a repository URL');
            return;
        }
    } else if (dialogState.mode === 'create') {
        body.name = nameInput.value.trim();
        if (!body.name) {
            showError('Please enter a project name');
            return;
        }
    }

    showLoading('Preparing repository...');

    fetch('/api/repo/prepare', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(body)
    })
    .then(function(response) {
        if (!response.ok) {
            return response.json().then(function(data) {
                throw new Error(data.error || 'Failed to prepare repository');
            });
        }
        return response.json();
    })
    .then(function(data) {
        dialogState.repoPath = data.path;
        dialogState.isNewProject = data.isNew || false;

        // Show warning if present (soft fail)
        if (data.warning) {
            warningDiv.textContent = data.warning;
            warningDiv.style.display = 'block';
        } else {
            warningDiv.style.display = 'none';
        }

        if (dialogState.isNewProject) {
            // Skip branch selection for new projects
            hideLoading();
            branchField.classList.add('dialog__field--hidden');
            enableAgentSelection();
        } else {
            // Fetch branches
            return fetch('/api/repo/branches?path=' + encodeURIComponent(data.path))
                .then(handleBranchResponse);
        }
    })
    .catch(function(err) {
        hideLoading();
        showError(err.message);
    });
});
```

## Testing

After implementing, test via App Preview:

```bash
# From host machine
cd /workspace
make run
# Open browser to the dev server URL
```

Test scenarios:
1. **Workspace mode** - should show branches, soft fail on fetch error
2. **Clone mode** - should clone repo, hard fail on error
3. **Create mode** - should create project, skip to agent selection

## Files to Modify

1. `cmd/swe-swe/templates/host/swe-swe-server/main.go` - DONE
2. `cmd/swe-swe/templates/host/swe-swe-server/static/selection.html` - DONE
   - CSS (lines ~543-736) - DONE
   - HTML dialog (lines 862-906) - DONE
   - JavaScript (lines 1198-1483) - DONE

## Browser Testing Results

All 3 modes tested via MCP browser at `http://swe-swe:3000`:

1. **Workspace mode** - PASS
   - Dropdown shows 3 options
   - Next button prepares repo
   - Branch field enabled
   - Agent selection works

2. **Clone mode** - PASS
   - Selecting "Clone external repository" shows URL input field

3. **Create mode** - PASS
   - Selecting "Create new project" shows name input field
   - After Next, branch field is hidden
   - Goes directly to agent selection

## After Implementation

```bash
make build golden-update
git add -A cmd/swe-swe/testdata/golden
git diff --cached -- cmd/swe-swe/testdata/golden
```
