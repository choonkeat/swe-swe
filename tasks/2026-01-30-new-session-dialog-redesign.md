# New Session Dialog Redesign

## Goal

Redesign the "New Session" dialog to replace the "Repository Type" selector with a smarter "Where" dropdown that lists previously-cloned repos, auto-triggers prepare for known repos, shows branch + agent fields simultaneously, and uses inline Next buttons for clone/create modes.

## Changes Summary

1. **"Where" dropdown** replaces "Repository Type": includes `-- choose one --` placeholder, Default workspace, existing repos from `/repos/`, "Clone external repository...", "Create new project..."
2. **Inline Next buttons**: clone URL and project name inputs have compact Next buttons to their right (not full-width rows)
3. **Branch + Agent together**: after prepare succeeds, branch and agent fields appear simultaneously; branch Next button is removed
4. **Hidden until chosen**: all fields below the dropdown are hidden until user picks something other than placeholder
5. **Sticky color per Where**: color saved/loaded per specific repo path or URL, not per mode

## Dev Server Workflow

All phases use `docs/dev/swe-swe-server-workflow.md`:
- Start: `make run > /tmp/server.log 2>&1 &`
- Test: MCP browser at `http://swe-swe:3000`
- Stop: `make stop`
- Unit tests: `make test-server`

---

## Phase 1: Backend — Add `GET /api/repos` endpoint ✅ DONE

### What will be achieved
A new API endpoint that scans `/repos/*/workspace/` directories, detects git repos, extracts remote URLs, and returns a JSON list. Also extend `POST /api/repo/prepare` to accept an optional `path` field for existing repos.

### Steps

1. **Add route** in `main.go`: handler for `GET /api/repos` alongside existing `/api/repo/prepare` and `/api/repo/branches`
2. **Implement handler**: Scan `/repos/` directory entries. For each subdirectory, check if `/repos/{name}/workspace/.git` exists. If so, run `git -C /repos/{name}/workspace remote get-url origin` to get the remote URL (may fail for local-only repos). Return JSON: `{"repos": [{"path": "/repos/{name}/workspace", "remoteURL": "https://...", "dirName": "{name}"}]}`
3. **Handle edge cases**: `/repos/` doesn't exist → return `{"repos":[]}`. Repo has no remote → remoteURL is empty string. Git command fails → skip entry or return dirName only.
4. **Extend prepare endpoint**: When `mode: "workspace"` and a `"path"` field is present, use that path instead of hardcoded `/workspace`. Validate path starts with `/repos/` for security.
5. **Add test** in `main_test.go` for the new endpoint.

### Verification
- `make test-server` passes with the new test
- Start dev server, `curl http://localhost:3000/api/repos` returns valid JSON
- Empty `/repos/` returns `{"repos":[]}`
- Repos with and without remotes are handled correctly
- Prepare with `{"mode":"workspace","path":"/repos/foo/workspace"}` works

---

## Phase 2: HTML — Restructure dialog markup ✅ DONE

### What will be achieved
Update `selection.html` dialog: "Where" dropdown with placeholder, inline Next buttons, branch field without Next button, fields wrapped in hideable groups.

### Steps

1. **Replace dropdown**: Change label "Repository Type" → "Where". Change `<select>`:
   - `<option value="" disabled selected>-- choose one --</option>`
   - `<option value="workspace">Default workspace (/workspace)</option>`
   - (Existing repo options injected dynamically by JS)
   - `<option value="clone">Clone external repository...</option>`
   - `<option value="create">Create new project...</option>`

2. **Wrap post-selection fields**: Add `<div id="new-session-fields" class="dialog__field--hidden">` around everything below the dropdown.

3. **Inline Next buttons**: Add a Next button inside clone URL field's `dialog__row` (after input, ID: `clone-next-btn`). Same for create name field (ID: `create-next-btn`). Remove standalone full-width Next button (`new-session-prepare`).

4. **Remove branch Next button**: Remove `<button id="new-session-branch-next">` from branch field.

5. **Add post-prepare group**: Wrap color picker + branch field + agent field in `<div id="post-prepare-fields" class="dialog__field--hidden">`.

6. **Field order** inside `#new-session-fields`:
   - Clone URL field (with inline Next) — hidden by default
   - Create name field (with inline Next) — hidden by default
   - Warning div
   - `#post-prepare-fields`:
     - Color picker
     - Branch field
     - Agent field
   - Footer (error, loading, start button)

### Verification
- Start dev server, open MCP browser
- Dialog opens showing only "Where" dropdown with `-- choose one --`
- No other fields visible
- Page loads without JS errors (check console)
- HTML structure valid

---

## Phase 3: JS — Rewrite dialog logic

### What will be achieved
New `new-session-dialog.js` file with all dialog logic. Old dialog IIFE removed from `homepage-main.js`. New flow: populate dropdown with existing repos, auto-prepare for workspace/existing repos, show branch+agent together, inline Next for clone/create, sticky color per Where.

### Steps

1. **Create `static/new-session-dialog.js`**: Move all dialog logic here. Remove the dialog IIFE (lines ~139-570) from `homepage-main.js`. Add `<script>` tag in `selection.html`.

2. **Fetch repos on dialog open**: In `openNewSessionDialog()`, call `GET /api/repos`. Populate `<select>` with dynamic `<option>` elements (value=path, text=remoteURL or dirName) between "Default workspace" and "Clone external repository..." options. Remove stale dynamic options before re-populating.

3. **Rewrite mode change handler**: On dropdown change:
   - `""` (placeholder): hide `#new-session-fields`
   - `"workspace"` or `/repos/...` path: show `#new-session-fields`, hide clone/create fields, show loading, auto-trigger prepare. On success: show `#post-prepare-fields`, load color, enable branch+agent.
   - `"clone"`: show `#new-session-fields`, show clone URL field, hide `#post-prepare-fields`
   - `"create"`: show `#new-session-fields`, show create name field, hide `#post-prepare-fields`

4. **Wire inline Next buttons**: `clone-next-btn` and `create-next-btn` call prepare logic. Show loading indicator during API call. On success: show `#post-prepare-fields`, load color, enable branch+agent (or just agent for create).

5. **Remove branch Next button logic**: After prepare succeeds and branches are fetched, call `enableBranchAndAgent()` which enables both branch input and agent selection in one step.

6. **Sticky color per Where**:
   - **Load**: After prepare succeeds, compute `whereKey`: `"workspace"` for workspace, repo path for existing repos, clone URL for clone, project name for create. Load from `swe-swe-color-repo-{whereKey}`.
   - **Save**: On session start, save to same key.

7. **Reset behavior**: Dropdown change resets all downstream: hide `#post-prepare-fields`, clear branch/agent/color, clear inputs.

8. **Loading indicators**: Show footer loading spinner during all prepare calls (auto-prepare and manual Next). Disable dropdown during prepare to prevent double-triggers.

### Verification
- Start dev server, open MCP browser
- **Placeholder state**: only dropdown visible
- **Workspace flow**: select → loading → color/branch/agent appear
- **Existing repo flow**: select → loading → color/branch/agent appear, saved color restored
- **Clone flow**: select → URL+Next appear → enter URL, Next → loading → color/branch/agent appear
- **Create flow**: select → name+Next appear → enter name, Next → loading → color+agent appear (no branch)
- **Sticky color**: set color, start session, reopen dialog, select same Where → color restored
- **Reset**: switch dropdown → downstream resets
- No JS console errors

---

## Phase 4: CSS — Styling adjustments

### What will be achieved
CSS updates for inline Next buttons, hidden field groups, and dropdown placeholder styling.

### Steps

1. **Inline Next button**: In the clone/create `dialog__row`, input gets `flex: 1`, Next button gets `width: auto; padding: 8px 16px;` (compact, not full-width).

2. **Hidden field groups**: `#new-session-fields` and `#post-prepare-fields` use `dialog__field--hidden` class. No new CSS classes needed.

3. **Dropdown placeholder**: `-- choose one --` disabled option styled by browser natively. Add subtle `color: #64748b` on the select when placeholder is selected if needed.

4. **Clean up**: Remove any styles specific to the old branch Next button or standalone prepare button if they existed.

### Verification
- Start dev server, open MCP browser
- Clone URL: input + Next button side-by-side, input fills remaining space
- Create name: same inline layout
- `-- choose one --` appears muted/grayed
- Loading spinner visible during prepare
- No visual regressions (agent cards, start button, color picker)
- Dialog looks correct at its max-width

---

## Phase 5: Manual browser testing — Full end-to-end

### Steps

1. Start dev server
2. **Placeholder state**: click "+ New Session" → only "Where" dropdown, `-- choose one --` selected, nothing below
3. **Default workspace**: select → loading → color/branch/agent appear → pick agent → Start Session → session loads
4. **Clone external**: reopen dialog → select "Clone external repository..." → URL input + inline Next → enter repo URL → Next → loading → color/branch/agent → pick agent → Start Session
5. **Existing repo in dropdown**: reopen dialog → previously-cloned repo appears in dropdown → select it → auto-prepare → fields appear
6. **Create new project**: select "Create new project..." → name input + inline Next → enter name → Next → loading → color+agent (no branch) → pick agent → Start Session
7. **Sticky color**: set color for workspace, start session → reopen, select workspace → color restored
8. **Reset between selections**: select clone, type URL, switch to workspace → clone field disappears, auto-prepare fires
9. **Ellipsis labels**: dropdown shows "Clone external repository..." and "Create new project..."
10. **Console errors**: check for JS errors throughout all flows
11. Stop dev server

---

## Bug: Session name shows branch only, not `{owner/repo}@{branch}`

### Description
When a session is a worktree of the default workspace, the session name displayed in the header only shows the branch/worktree name (e.g. `new-session-dialog`) instead of `{owner/repo}@new-session-dialog`. The full qualified name should include the repo identity so users can distinguish worktrees across different repos.

### Status
- [ ] Investigate where session name is derived (likely in session creation or the header template)
- [ ] Fix to prefix with `{owner/repo}@` when the session is a worktree
