# Playwright Permission Dialog Tests

End-to-end tests for swe-swe permission dialog handling to prevent regression of permission-related bugs.

## Prerequisites

1. **swe-swe server must be running** at `localhost:7000`:
   ```bash
   cd ../.. && bin/swe-swe -agent claude
   ```

2. **Install test dependencies**:
   ```bash
   cd tests/playwright
   npm install
   ```

## Running Tests

### Quick Start (Recommended)
```bash
cd tests/playwright
./run-tests.sh
```

This script will:
- Check if the server is running
- Install dependencies if needed
- Run all permission tests in headed mode
- Show results summary

### Manual Test Execution

#### Run all tests (headless):
```bash
npx playwright test
```

#### Run with visible browser (headed mode):
```bash
npx playwright test --headed
```

#### Run specific test:
```bash
npx playwright test specs/permission-simple.spec.ts --headed
```

#### Debug mode (step through tests):
```bash
npx playwright test --debug
```

#### Open Playwright UI:
```bash
npx playwright test --ui
```

## Test Scenarios

The test suite covers 5 critical scenarios:

1. **Basic Permission Detection**: Verifies permission dialog appears for commands requiring file/tool access
2. **Permission Grant Flow**: Tests allowing permissions and verifying Claude continues execution
3. **Permission Deny Flow**: Tests denying permissions and verifying proper error handling
4. **No Duplicate Dialogs**: Ensures only one permission dialog appears (no cascade)
5. **Process Termination**: Verifies Claude process stops while waiting for permission

## Test Commands Used

All test commands are harmless and work with `/tmp` directory:

- `Create a test file at /tmp/swe-swe-test-{timestamp}.txt` - Tests Write permission
- `Create a directory /tmp/swe-swe-test-dir-{timestamp}` - Tests Bash mkdir permission
- `List the files in /tmp directory using the ls command` - Tests Bash ls permission
- `Run git status to check the repository state` - Tests Bash git permission
- `Check if the file /tmp/test-{timestamp}.txt exists` - Tests Read permission

## Using with Playwright MCP

Since you have Playwright MCP installed (`claude mcp add playwright`), you can ask Claude to:

1. **Run the tests**:
   ```
   "Run the Playwright tests in tests/playwright/specs/permission-simple.spec.ts"
   ```

2. **Debug a failing test**:
   ```
   "Debug the permission grant flow test and show me what's happening"
   ```

3. **Add new test scenarios**:
   ```
   "Add a test for sequential permission requests to the test suite"
   ```

## CI Integration (Future)

To add these tests to CI:

1. Add to `.github/workflows/test.yml`:
   ```yaml
   - name: Start swe-swe server
     run: |
       bin/swe-swe -agent claude &
       sleep 5
   
   - name: Run permission tests
     run: |
       cd tests/playwright
       npm install
       npx playwright test
   ```

## Troubleshooting

### Server not running
```
❌ Error: swe-swe server is not running at localhost:7000
```
**Solution**: Start the server with `bin/swe-swe -agent claude`

### Permission dialog not appearing
- Check that you're using a Claude agent (`-agent claude`)
- Verify the test commands actually require permissions
- Check browser console for errors

### Tests timing out
- Increase timeout in `playwright.config.ts`
- Ensure server is responsive
- Check network connectivity to localhost:7000

## Test Output

Successful test run looks like:
```
✅ Test 1 passed: Permission dialog detected successfully
✅ Test 2 passed: Permission grant flow works correctly
✅ Test 3 passed: Permission deny flow works correctly
✅ Test 4 passed: No duplicate permission dialogs
✅ Test 5 passed: Process stops while waiting for permission
```

## Contributing

When adding new tests:
1. Use harmless commands (prefer `/tmp` directory operations)
2. Include timestamp in filenames to avoid conflicts
3. Clean up after tests when possible
4. Document what each test verifies
5. Keep tests independent (don't rely on previous test state)