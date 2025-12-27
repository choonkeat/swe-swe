# Task: Add AI Assistant Selection to Homepage

## Overview
Modify the terminal server to show a homepage with available AI coding assistants. Users select an assistant, then a session starts with the corresponding shell commands.

## Assistant Configurations

| Assistant | Shell Command | Shell-Restart Command | Binary |
|-----------|---------------|----------------------|--------|
| Claude | `claude` | `claude --continue` | `claude` |
| Gemini | `gemini` | `gemini --resume` | `gemini` |
| Codex | `codex` | `codex resume --last` | `codex` |
| Goose | `goose run --interactive --text ready` | `goose run --resume` | `goose` |
| Aider | `aider` | `aider --restore-chat-history` | `aider` |
| Custom | (from `-shell` flag) | (from `-shell-restart` flag) | N/A |

## Requirements
1. Custom only appears if `-shell` flag was provided at server startup
2. Each session can use a different assistant (per-session, not global)
3. Show assistant name in both terminal title and status bar
4. If no assistants detected AND no custom provided, show error

---

## Implementation Steps

### Step 1: Define Assistant Configuration Types and Map
**Files:** `main.go`

**Changes:**
- Add `AssistantConfig` struct with `Name`, `ShellCmd`, `ShellRestartCmd`, `Binary` fields
- Add global `assistantConfigs` map with predefined assistants
- Add `availableAssistants` slice populated at startup

**Test:**
- `go build` succeeds
- Existing functionality unchanged (run server, connect, verify claude still works)

**Status:** [x] Complete

---

### Step 2: Add detectAvailableAssistants() Function
**Files:** `main.go`

**Changes:**
- Add `detectAvailableAssistants()` function using `exec.LookPath`
- Call it in `main()` after flag parsing
- Log which assistants are available
- Handle custom assistant from `-shell` flag

**Test:**
- Run server with no flags, verify it detects which assistants exist on system
- Run server with `-shell "bash"`, verify custom appears in available list
- Error shown if no assistants available and no custom

**Status:** [x] Complete

---

### Step 3: Create Selection Homepage (selection.html)
**Files:** `static/selection.html`

**Changes:**
- Create new template for homepage
- Simple, clean UI with cards/buttons for each available assistant
- Show assistant name and brief description
- On click, redirect to `/session/{uuid}?assistant={name}`
- Style consistent with existing terminal-ui.css (dark theme, same fonts)

**Test:**
- Visit `/` and verify selection page appears
- Verify only detected assistants are shown
- Click an assistant and verify redirect URL is correct

**Status:** [x] Complete

---

### Step 4: Add selectionTemplate and Modify Routing
**Files:** `main.go`

**Changes:**
- Add `selectionTemplate` parsed from `static/selection.html`
- Modify `/` handler: serve selection page instead of redirect
- Pass available assistants to template
- Modify `/session/{uuid}` handler: read `?assistant=` query param
- Store assistant in URL for WebSocket to pick up

**Test:**
- Visit `/` shows selection page
- Visit `/session/{uuid}?assistant=claude` shows terminal page
- WebSocket connection includes assistant param

**Status:** [x] Complete

---

### Step 5: Modify Session Struct to Store Assistant
**Files:** `main.go`

**Changes:**
- Add `Assistant string` field to `Session` struct
- Modify `getOrCreateSession()` to accept assistant name parameter
- Use assistant-specific shell commands instead of global `shellCmd`
- Modify `RestartProcess()` to use assistant's restart command
- Pass assistant name in BroadcastStatus

**Test:**
- Create session with `?assistant=claude`, verify claude command runs
- Create session with `?assistant=aider`, verify aider command runs
- Process restart uses correct assistant's restart command

**Status:** [x] Complete

---

### Step 6: Update terminal-ui.js to Show Assistant Name
**Files:** `static/terminal-ui.js`, `static/index.html`

**Changes:**
- Read assistant from URL query param or page data
- Update page title to include assistant name: `{Assistant} - {UUID}`
- Show assistant name in status bar
- Update status message format to include assistant

**Test:**
- Open terminal with `?assistant=claude`, verify title shows "Claude - {uuid}"
- Verify status bar shows assistant name
- Verify status broadcasts include assistant

**Status:** [x] Complete

---

### Step 7: End-to-End Testing
**Files:** N/A (manual testing)

**Test cases:**
1. Start server with no flags, verify detection works
2. Start server with `-shell "bash" -shell-restart "bash"`, verify Custom shows
3. Select Claude from homepage, verify session works
4. Select a different assistant, verify different shell runs
5. Multiple sessions with different assistants simultaneously
6. Process death triggers correct restart command for each assistant
7. Verify title and status bar show correct assistant name

**Status:** [x] Complete

---

## Progress Log

| Date | Step | Status | Notes |
|------|------|--------|-------|
| 2025-12-23 | Step 1 | Complete | Added AssistantConfig struct and assistantConfigs map |
| 2025-12-23 | Step 2 | Complete | Added detectAvailableAssistants(), tested with default and custom shell |
| 2025-12-23 | Step 3 | Complete | Created selection.html with assistant cards |
| 2025-12-23 | Step 4 | Complete | Added selectionTemplate, modified routing, tested with playwright |
| 2025-12-23 | Step 5 | Complete | Modified Session struct, use assistant-specific shell commands |
| 2025-12-23 | Step 6 | Complete | Updated terminal-ui.js to show assistant name in status bar |
| 2025-12-23 | Step 7 | Complete | E2E tested: selection page, aider session, custom shell (bash) |

---

## Git Commits

Each step should be committed separately with conventional commit format:
- Step 1: `feat: add assistant configuration types and map`
- Step 2: `feat: detect available AI assistants at startup`
- Step 3: `feat: add selection homepage for choosing assistant`
- Step 4: `feat: route homepage to selection, pass assistant to session`
- Step 5: `feat: store assistant in session, use for shell commands`
- Step 6: `feat: show assistant name in title and status bar`
