# Bug: Session Context Preservation Regression

## Issue Description

After removing the `validateClaudeSession` function (commit 43caf4b), the Playwright test for session context preservation is failing:

```
Testing session context preservation...
Sent message: Create a test file at ./tmp/context-test-1761566936045.txt with content "Session preservation test content"
Sent message: What was the exact filename and content of the file you just created?
AI response: "✓ Allowed Write access"
Remembers filename: false, Remembers content: false
❌ Session context NOT preserved
```

Expected behavior: AI should remember the specific file details from the previous operation.

## Root Cause Analysis

### What Changed
The optimization in commit 43caf4b removed `validateClaudeSession()` which was:
1. **Previously**: Fast pre-validation (5s timeout) before attempting session resume
2. **Now**: Direct session usage with error handling only after full command execution

### Timing Difference
- **Before**: Invalid sessions detected in ~1 second, immediate retry with next session ID
- **After**: Invalid sessions detected after full Claude process startup and failure (~5-10+ seconds)

### Error Detection Gap
Current error pattern matching at `websocket.go:927-928` may be incomplete:
```go
if strings.Contains(errorMsg, "session") && (strings.Contains(errorMsg, "not found") ||
    strings.Contains(errorMsg, "invalid") || strings.Contains(errorMsg, "expired"))
```

## Investigation Plan

### Phase 1: Diagnose Current Behavior (1-2 hours)

1. **Add Detailed Logging**
   - Log all session IDs being attempted in `tryExecuteWithSessionHistory`
   - Log exact error messages from failed Claude commands
   - Track session state changes and timing

2. **Run Isolated Session Test**
   - Create a minimal reproduction case
   - Test with known invalid session IDs
   - Verify current error detection patterns

3. **Analyze Playwright Test Logs**
   - Run test with verbose logging enabled
   - Capture all session-related log entries
   - Identify where session continuity breaks

### Phase 2: Root Cause Identification (2-3 hours)

1. **Compare Pre/Post Change Behavior**
   - Test on commit before 43caf4b (with validation)
   - Test on current commit (without validation)
   - Document behavioral differences

2. **Error Pattern Analysis**
   - Collect all possible Claude session error messages
   - Verify our error detection catches all cases
   - Test edge cases (network timeouts, partial failures)

3. **Session State Investigation**
   - Verify session ID extraction from Claude output
   - Check session history management
   - Validate browser-server session ID synchronization

### Phase 3: Solution Design (1-2 hours)

#### Option A: Lightweight Session Validation
Add a fast, non-token-wasting validation:
```go
func quickValidateSession(sessionID string) bool {
    // Check if session file exists in Claude's cache
    // Use claude --resume sessionID --dry-run or similar
    // Maximum 1-2 second timeout
}
```

#### Option B: Enhanced Error Detection
Improve error pattern matching and retry logic:
```go
// Expand error detection patterns
// Add fallback session creation on any resume failure
// Implement exponential backoff for retries
```

#### Option C: Hybrid Approach
Combine lightweight validation with robust error handling:
```go
// Quick file-based session existence check
// Full error handling as backup
// Graceful degradation to fresh sessions
```

#### Option D: Session State Recovery
Implement session warming and context preservation:
```go
// Maintain session context cache
// Pre-warm replacement sessions
// Recovery from session state corruption
```

### Phase 4: Implementation (3-4 hours)

1. **Implement Chosen Solution**
   - Code changes based on investigation results
   - Maintain optimization goals (no token waste, minimal delay)
   - Preserve existing functionality

2. **Add Comprehensive Testing**
   - Unit tests for session management
   - Integration tests for context preservation
   - Edge case testing (expired sessions, network issues)

3. **Performance Verification**
   - Ensure chat message delay optimization is preserved
   - Verify no regression in response times
   - Test under various session states

### Phase 5: Validation (1-2 hours)

1. **Playwright Test Suite**
   - Run full test suite to ensure all tests pass
   - Specifically verify session context preservation test
   - Test multiple consecutive messages with context

2. **Manual Testing**
   - Test session continuity across browser refreshes
   - Verify context preservation with file operations
   - Test permission flows with session resumption

3. **Performance Testing**
   - Measure chat message delay (should remain optimized)
   - Test with various session states (fresh, existing, expired)
   - Verify no token waste or unnecessary delays

## Success Criteria

- [ ] Session context preservation test passes consistently
- [ ] No regression in chat message delay optimization
- [ ] All existing Playwright tests continue to pass
- [ ] Session management is robust against edge cases
- [ ] No unnecessary token consumption
- [ ] Clear error messages for session-related issues

## Files to Modify

### Primary Files
- `cmd/swe-swe/websocket.go` - Session management logic
- `tests/playwright/specs/permission-tests.spec.ts` - Test verification

### Supporting Files
- Add logging configuration for session debugging
- Update error handling documentation
- Add session management unit tests

## Implementation Timeline

- **Day 1**: Phase 1-2 (Investigation and diagnosis)
- **Day 2**: Phase 3-4 (Solution design and implementation)  
- **Day 3**: Phase 5 (Testing and validation)

## Risk Assessment

### High Risk
- Breaking existing session functionality
- Reintroducing token waste or delays
- Complex timing issues in session management

### Medium Risk  
- Incomplete error pattern detection
- Edge cases not covered in testing
- Performance regression

### Low Risk
- Test flakiness unrelated to session management
- Minor logging or error message changes

## Rollback Plan

If the fix introduces regressions:
1. Revert to commit 43caf4b
2. Re-implement `validateClaudeSession` with token-free validation
3. Use file-based or metadata-based session checking
4. Maintain delay optimization while fixing context preservation

## ✅ RESOLUTION (2025-10-27)

**Root Cause Identified and Resolved**: The session error detection was failing because it only checked `err.Error()` (which returns "exit status 1") but not the actual STDERR content where Claude CLI outputs "No conversation found with session ID: X".

**Solution Implemented**:
1. **Added STDERR capture**: Modified `websocket.go` to capture STDERR content during command execution using mutex-protected string builder
2. **Enhanced error detection**: Updated session error detection to check both `err.Error()` and captured STDERR content for session failure patterns
3. **Improved pattern matching**: Added detection for "No conversation found" pattern specifically from Claude CLI

**Files Modified**:
- `cmd/swe-swe/websocket.go` - Added stderr capture and enhanced error detection logic

**Verification Results**:
- ✅ **Manual testing confirmed**: Session context is now preserved correctly after permission grants
- ✅ **STDERR capture working**: Debug logs show captured stderr content with actual Claude CLI error messages
- ✅ **Session error detection working**: Enhanced debug logs show proper error pattern matching
- ✅ **Session retry working**: System successfully retries with fallback session IDs when primary session fails

**Test Evidence**:
- Manual browser test showed AI correctly remembered filename (`/Users/choonkeatchew/git/choonkeat/swe-swe/tmp/context-test-manual.txt`) and content (`Manual test content`) after permission flow
- Server logs show SESSION DEBUG output with captured STDERR content: `"No conversation found with session ID: f712cb0a-c920-4d06-b539-2dbb30a7eee5"`
- Session ID retry mechanism now properly detects Claude CLI session errors and falls back gracefully

**Status**: ✅ **RESOLVED** - Session context preservation regression has been fixed. The optimization benefits from removing `validateClaudeSession()` are maintained while properly handling session errors.

## Related Issues

- `tasks/bug-chat-message-delay-before-processing.md` - Original optimization (maintained)
- Session warming implementation (working)
- Permission flow integration with session management (fixed)