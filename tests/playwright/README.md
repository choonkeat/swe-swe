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
npx playwright test specs/permission-tests.spec.ts --headed
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

The consolidated test suite covers comprehensive permission scenarios:

### Permission Type Coverage:
1. **Write Permissions**: File creation commands
2. **Read Permissions**: File reading commands  
3. **Edit Permissions**: File editing commands
4. **Additional Tools**: Glob search, MultiEdit operations

### Permission Flow Testing:
1. **Basic Detection**: Verifies permission dialogs appear for different command types
2. **Grant Flow**: Tests allowing permissions and verifying execution continues
3. **Deny Flow**: Tests denying permissions and verifying proper error handling
4. **No Duplicate Dialogs**: Ensures only one permission dialog appears
5. **Process Pausing**: Verifies process pauses while waiting for permission

### Advanced Scenarios:
1. **Session Context Preservation**: Verifies session state survives permission grants
2. **Background Process Behavior**: Ensures session warming runs quietly during permission waits

## Test Commands Used

All test commands are harmless and work with `./tmp` directory:

**IMPORTANT: Always use `./tmp` (relative) not `/tmp` (absolute) for test files!**

- **Write**: `Create a test file at ./tmp/test-{timestamp}.txt with content "..."`
- **Read**: `Read the file ./tmp/test-read-{timestamp}.txt and show me its contents`
- **Edit**: `Edit the file ./tmp/edit-test-{timestamp}.txt and add the line "..."`
- **MultiEdit**: `Update the file ./tmp/multi-edit-test-{timestamp}.txt by replacing any existing content with "..."`
- **Glob**: `Search for all .txt files in the ./tmp directory and show me what you find`

## Using with Playwright MCP

Since you have Playwright MCP installed (`claude mcp add playwright`), you can ask Claude to:

1. **Run the tests**:
   ```
   "Run the Playwright tests in tests/playwright/specs/permission-tests.spec.ts"
   ```

2. **Debug a failing test**:
   ```
   "Debug the permission grant flow test and show me what's happening"
   ```

3. **Run specific test groups**:
   ```
   "Run only the Bash permission tests from the test suite"
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
✅ Write permission detection works
✅ Write permission grant flow works  
✅ Write permission deny flow works
✅ Read permission detection works
✅ Bash permission detection works
✅ No duplicate dialogs appear
✅ Process pauses during permission wait
✅ Session context preserved after permission grant
✅ Session warming runs without visible output
```

## Contributing

When adding new tests:
1. Use harmless commands (prefer `/tmp` directory operations)
2. Include timestamp in filenames to avoid conflicts
3. Clean up after tests when possible
4. Document what each test verifies
5. Keep tests independent (don't rely on previous test state)