# Bug: Playwright Tests - Massive Duplication and Ineffective Tests

## Summary
The Playwright test suite in `tests/playwright/` has severe duplication issues and contains ineffective tests that provide false confidence without actually verifying the claimed behaviors.

## Critical Issues

### 1. Massive Duplication (70% redundant code)

**3 spec files doing essentially the same thing:**
- `permission-basic.spec.ts` - Single test for Write permission
- `permission-simple.spec.ts` - 5 tests, all using Write commands  
- `permission-working.spec.ts` - 7 tests, all using Write commands

**Duplicated helper functions across files:**
- `sendMessage()` - identical implementation in 2 files
- `waitForPermissionDialog()` - identical implementation in 2 files

### 2. Ineffective Tests That Don't Test What They Claim

#### Test 6: "Session Context Preservation" (`permission-working.spec.ts:170-219`)
- **Claims**: Tests session context preservation after permission grant
- **Actually does**: Checks if AI response contains specific strings (`testId` and content text)
- **Problem**: Doesn't verify real session state - could pass even if session was completely reset
- **False confidence**: String matching ≠ session state verification

#### Test 7: "Quiet Session Warming" (`permission-working.spec.ts:221-259`)
- **Claims**: Tests that session warming runs quietly during permission dialog
- **Actually does**: Counts DOM elements before/after 8 second wait
- **Problem**: Doesn't verify actual session warming behavior or background processes
- **False confidence**: DOM element count ≠ session warming verification

#### Test 5: "Process Stops During Permission Wait" (in both files)
- **Claims**: Verifies process stops while waiting for permission
- **Actually does**: Checks message count in chat UI
- **Problem**: Process could be running wild in background and test would still pass
- **False confidence**: UI message count ≠ actual process state

### 3. Poor Test Design Patterns

- **All tests use identical Write commands** - no diversity in permission types
- **Heavy reliance on string matching** instead of state verification
- **Magic timeouts** (`8000ms`, `10000ms`) instead of proper state waiting
- **Surface-level assertions** that don't test core functionality

## Impact

1. **Maintenance burden** - 3x the code to maintain for no additional coverage
2. **False confidence** - Tests pass but don't verify claimed behaviors
3. **Poor coverage** - Only tests Write permissions, ignores other permission types
4. **Brittle tests** - String matching and DOM counting are fragile approaches

## Recommended Fixes

### Phase 1: Consolidation
1. **Consolidate to 1 spec file** - eliminate 70% duplication
2. **Extract shared helpers** to common utilities
3. **Remove redundant tests** that test identical scenarios

### Phase 2: Effective Testing
1. **Test different permission types** (Read, Bash, etc.) instead of just Write
2. **Replace string-matching assertions** with actual state verification
3. **Remove pseudo-tests** that don't actually test claimed behavior
4. **Add proper state waiting** instead of magic timeouts

### Phase 3: Real Verification
1. **Session context**: Test actual session state, not response text matching
2. **Process behavior**: Monitor actual process state, not UI element counts
3. **Background operations**: Verify actual background processes, not DOM changes

## Files Affected
- `tests/playwright/specs/permission-basic.spec.ts`
- `tests/playwright/specs/permission-simple.spec.ts` 
- `tests/playwright/specs/permission-working.spec.ts`

## Priority
**High** - Current tests provide false confidence and waste maintenance effort while not actually testing critical permission system behaviors.