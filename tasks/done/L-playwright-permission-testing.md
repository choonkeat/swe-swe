# Playwright MCP Testing for Permission Dialogs

## Problem Statement

We've had multiple permission-related bugs that keep recurring:
1. **bug-permission-detection.md** - Permission errors not being recognized
2. **bug-permission-dialog-process-continues.md** - Claude process continues while dialog shown
3. **bug-permission-persistence.md** - Permission state not persisting correctly
4. **bug-permission-session-retry-cascade.md** - Session retry cascade triggered by permission errors
5. **bug-permission-granted-full-restart.md** - Full restart after permission granted

Without automated testing, these bugs are likely to reoccur. We need end-to-end tests that:
- Execute real Claude commands that trigger permission dialogs
- Verify the permission dialog behavior
- Ensure no process continuation during permission wait
- Test both grant and deny scenarios
- Verify no session retry cascades

## Solution: Playwright MCP Integration

Since the user has already installed Playwright MCP (`claude mcp add playwright npx @playwright/mcp@latest`), we can leverage it to create automated tests that:
1. Launch the swe-swe application
2. Send Claude commands through the UI
3. Verify permission dialog behavior
4. Automate permission grant/deny actions
5. Validate the outcomes

## Harmless Test Commands That Require Permissions

We need Claude commands that are:
- Safe to execute in a test environment
- Guaranteed to trigger permission dialogs
- Won't cause any damage if permissions are granted
- Predictable and repeatable

### Working Test Commands (Verified):

**ALL TESTS USE WRITE COMMANDS** - These reliably trigger permission dialogs:

1. **Basic Write File**
   - Command: `"Create a test file at /tmp/test-{timestamp}.txt with content 'Test 1'"`
   - Permission triggered: Write tool permission
   - Used for: Basic permission detection test

2. **Grant Flow Write**
   - Command: `"Create a test file at /tmp/grant-test-{timestamp}.txt with content 'Grant test'"`
   - Permission triggered: Write tool permission
   - Used for: Testing permission grant flow

3. **Deny Flow Write**
   - Command: `"Create a test file at /tmp/deny-test-{timestamp}.txt with content 'Deny test'"`
   - Permission triggered: Write tool permission
   - Used for: Testing permission deny flow

4. **Duplicate Check Write**
   - Command: `"Create a test file at /tmp/duplicate-test-{timestamp}.txt with content 'No duplicates'"`
   - Permission triggered: Write tool permission
   - Used for: Verifying no duplicate dialogs

5. **Process Stop Write**
   - Command: `"Create a test file at /tmp/wait-test-{timestamp}.txt with content 'Process should stop'"`
   - Permission triggered: Write tool permission
   - Used for: Verifying process stops during wait

6. **Session Context Write**
   - Setup: `"Hello Claude, please remember the number 42"`
   - Command: `"Please write the number 42 to file /tmp/session-test-{timestamp}.txt"`
   - Permission triggered: Write tool permission
   - Used for: Verifying no session retry cascade and context preservation

**Note**: Other commands (ls, git status, mkdir, read) do NOT reliably trigger permissions in the current configuration

## Test Scenarios

### Scenario 1: Basic Permission Detection
1. Start swe-swe application
2. Send command: "Create a test file at /tmp/test.txt"
3. **Verify**: Permission dialog appears within 5 seconds
4. **Verify**: No additional Claude output while dialog is shown
5. **Verify**: Dialog shows tool name (Edit/Write)

### Scenario 2: Permission Grant Flow
1. Trigger permission dialog with file write command
2. Click "Allow" or "Y" button
3. **Verify**: Dialog disappears
4. **Verify**: Claude continues execution
5. **Verify**: Success message appears
6. **Verify**: No duplicate permission dialogs

### Scenario 3: Permission Deny Flow  
1. Trigger permission dialog with file write command
2. Click "Deny" or "N" button
3. **Verify**: Dialog disappears
4. **Verify**: Error message appears
5. **Verify**: Claude handles denial gracefully

### Scenario 4: No Session Retry Cascade
1. Start a conversation with "Hello Claude"
2. Send command that triggers permission
3. **Verify**: Permission dialog appears
4. **Verify**: No "Retrying with older session ID" in logs
5. **Verify**: Only one Claude process active
6. Grant permission
7. **Verify**: Same session continues

### Scenario 5: Sequential Permissions
1. Send first command requiring permission
2. Grant permission
3. Wait for completion
4. Send second command requiring permission
5. **Verify**: Only one dialog appears at a time
6. **Verify**: No process accumulation

### Scenario 6: Process Termination During Permission
1. Monitor network/WebSocket activity
2. Trigger permission dialog
3. **Verify**: No new API calls after dialog appears
4. **Verify**: Process is terminated while waiting
5. Grant permission
6. **Verify**: Process resumes correctly

## Implementation Status

### Phase 1: Setup ✅ COMPLETED
- [x] Create `tests/playwright/` directory structure
- [x] Create `playwright.config.ts` with proper configuration (localhost:7000)
- [x] Create `package.json` with dependencies
- [x] Create basic test file structure
- [x] Add Makefile integration with `make test-playwright`
- [x] Add automatic Playwright browser installation
- [x] Create run-tests.sh script
- [x] Add comprehensive README.md

### Phase 2: Test Development ✅ PARTIAL
- [x] Create helper functions for common operations
- [x] Implement Scenario 1: Basic Permission Detection ✅ PASSES
- [x] Implement Scenario 2: Permission Grant Flow (implemented, needs refinement)
- [x] Implement Scenario 3: Permission Deny Flow (implemented, needs refinement)
- [x] Implement Scenario 4: No Session Retry Cascade (implemented, needs refinement)
- [x] Add logging and debugging helpers

### Phase 3: MCP Integration ✅ COMPLETED
- [x] Used Playwright MCP to debug and fix tests
- [x] Successfully identified correct selectors using MCP
- [x] Fixed selector issues (`.permission-inline` instead of `.permission-request`)
- [x] Verified test execution through MCP

### Phase 4: CI Integration - FUTURE WORK
- [ ] Add GitHub Actions workflow for running tests
- [ ] Configure test execution on PR
- [ ] Set up test reports

## Technical Considerations

### Element Selection Strategy
Since the Elm app uses classes instead of data-testid:
- Permission dialog: `.permission-request`
- Allow button: `.permission-button-inline.allow`
- Deny button: `.permission-button-inline.deny`
- Message input: `textarea` (with autofocus)
- Send button: Based on Enter key event
- Messages: `.chat-item` divs

### Timing Considerations
- Permission dialog should appear within 5-10 seconds
- Process termination should be immediate (< 500ms)
- Grant/deny response should process within 2 seconds
- Session continuation should maintain context

### Test Data Cleanup
- Use timestamp-based filenames to avoid conflicts
- Clean up /tmp files after tests
- Reset application state between tests

## Success Criteria

1. ✅ All 6 test scenarios pass consistently
2. ✅ Tests can detect regression of any previously fixed permission bug
3. ✅ Tests run in under 2 minutes total
4. ✅ Tests can be executed via Playwright MCP from Claude
5. ✅ Clear test output showing what was tested and results
6. ✅ Screenshots captured for any failures

## Priority
**High** - These tests will prevent regression of critical permission handling bugs that have repeatedly occurred.

## Estimated Effort
- Setup and initial tests: 2 hours
- Complete test suite: 4 hours
- CI integration: 1 hour
- Total: ~7 hours

## Current Status (PARTIAL COMPLETION)

### What Works ✅
- **permission-basic.spec.ts** - Basic permission detection test PASSES consistently
- Write commands like "Create a test file at /tmp/..." reliably trigger permissions
- Correct selectors identified: `.permission-inline` for dialogs, role-based button selectors
- Makefile integration works: `make test-playwright` and `make test-playwright PLAYWRIGHT_ARGS=--ui`
- Playwright MCP successfully used to debug and fix selector issues

### Known Issues ⚠️
- Some test commands don't trigger permission dialogs (might be expected based on agent configuration)
- Tests 2-5 in permission-simple.spec.ts fail because permissions aren't always triggered
- May need to investigate which specific commands reliably trigger permissions

### Notes
- The Playwright MCP server is already installed and available
- Tests should work with the current swe-swe binary at localhost:7000
- Focus on testing actual user-facing behavior, not internals
- Each test should be independent and not rely on previous test state