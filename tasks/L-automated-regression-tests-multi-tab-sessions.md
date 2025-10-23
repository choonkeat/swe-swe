# Automated Regression Tests for Multi-Tab Sessions

## Status: 📋 Ready for Implementation (Updated for Hybrid Architecture)

## Overview
Create comprehensive automated regression tests to protect the multi-tab session functionality from future regressions. The session functionality is currently production-ready and manually tested, but lacks automated test coverage for the complex session management logic.

### **🔄 Updated for Hybrid URL Fragment Architecture**
**Critical Change**: Tests updated to reflect hybrid approach where:
- ✅ **Browser session IDs**: Always generated fresh per tab (never in URL) → Maintains tab independence
- ✅ **Claude session IDs**: Stored in URL fragments only → Enables conversation persistence and sharing
- ✅ **Copy-paste safe**: URL copying creates independent tabs that can share Claude conversations

## Current Test Infrastructure Analysis

### ✅ Existing Infrastructure
- **Go Tests**: `cmd/swe-swe/websocket_test.go`, `cmd/swe-swe/fuzzy_matcher_test.go`
- **Elm Tests**: `elm/tests/ClaudeJSONTest.elm`, `elm/tests/AnsiTest.elm` (53 passing tests)
- **Test Commands**: `make test` runs both Go (`go test ./...`) and Elm (`elm-test`) suites
- **CI Integration**: Tests run automatically via Makefile
- **Test Coverage**: Currently focuses on JSON parsing, ANSI handling, line scanning

### ✅ Session Implementation Status
**Backend (Go):**
- `Client` struct has `browserSessionID`, `claudeSessionID`, `hasStartedSession` fields
- `ClientMessage` struct includes `sessionID` field for WebSocket communication
- Claude session ID extraction from stream-json output (implemented)
- Command building with `--resume` logic (implemented)

**Frontend (Elm):**  
- `Model` includes `browserSessionID : Maybe String` field
- Browser session ID generation and persistence via sessionStorage
- Session ID transmission in WebSocket messages

## Implementation Plan

### Phase 1: Backend Session Logic Tests

#### 1.1 Create `cmd/swe-swe/session_test.go`
**File Location:** `/workspace/cmd/swe-swe/session_test.go`

**Test Functions:**
```go
// Test browser session ID assignment and tracking
func TestClientBrowserSessionTracking(t *testing.T)

// Test Claude session ID extraction from stream-json
func TestClaudeSessionExtractionFromJSON(t *testing.T)  

// Test --resume flag construction for Claude commands
func TestClaudeResumeCommandBuilding(t *testing.T)

// Test hasStartedSession flag behavior
func TestSessionStartedFlagTracking(t *testing.T)

// Test session isolation between multiple clients
func TestMultiClientSessionIsolation(t *testing.T)

// Test error handling for invalid/missing session IDs
func TestSessionErrorHandling(t *testing.T)
```

#### 1.2 Mock Infrastructure Setup
**Required Test Helpers:**
- Mock WebSocket connection factory
- Mock Claude stream-json response generator
- Test client creation utilities
- Session state assertion helpers

#### 1.3 Test Data Setup
**Mock Stream-JSON Responses:**
```json
{
  "type": "assistant",
  "session_id": "test-claude-session-123",
  "message": { ... }
}
```

**Mock WebSocket Messages:**
```json
{
  "sender": "USER",
  "content": "test message",
  "sessionID": "session_1672531200000_abc123def",
  "firstMessage": true
}
```

### Phase 2: Frontend Session Tests

#### 2.1 Extend `elm/tests/ClaudeJSONTest.elm`
**New Test Suite:** "Session Management"

**Test Cases:**
```elm
describe "Session Management"
    [ test "extracts session_id from Claude stream-json response" <|
        -- Test ClaudeMessage decoder handles session_id field
        
    , test "includes browserSessionID in ClientMessage encoding" <|
        -- Test outgoing WebSocket message format
        
    , test "tracks browserSessionID in model state" <|
        -- Test Model.browserSessionID persistence
        
    , test "handles missing session_id gracefully" <|
        -- Test optional session_id field in JSON
        
    , test "preserves session state across model updates" <|
        -- Test session persistence through update cycles
    ]
```

#### 2.2 Session-Aware Message Processing Tests
**Test Session ID Inclusion:**
- Verify all outgoing messages include `sessionID` field
- Test session ID persistence across page refreshes (via flags)
- Test model initialization with browser session ID

### Phase 3: Integration Tests

#### 3.1 Create `cmd/swe-swe/integration_test.go`
**File Location:** `/workspace/cmd/swe-swe/integration_test.go`

**Test Functions:**
```go
// End-to-end multi-tab session workflow
func TestMultiTabSessionWorkflow(t *testing.T)

// Session persistence across reconnections  
func TestSessionPersistenceOnReconnect(t *testing.T)

// Concurrent session handling under load
func TestConcurrentSessionHandling(t *testing.T)

// Claude session resumption integration
func TestClaudeSessionResumptionIntegration(t *testing.T)
```

#### 3.2 WebSocket Test Server Setup
**Required Infrastructure:**
- Test WebSocket server with session tracking
- Multiple concurrent client simulation
- Mock Claude CLI with session support
- Stream-json response simulation

### Phase 4: Edge Case & Error Handling Tests

#### 4.1 Error Scenario Coverage
**Test Cases:**
- Invalid Claude session ID handling
- Missing browser session ID recovery
- Corrupted stream-json parsing
- WebSocket reconnection with session state
- Session cleanup on client disconnect

#### 4.2 Boundary Condition Tests
**Scenarios:**
- Very long session IDs
- Special characters in session IDs
- Rapid session creation/destruction
- Memory usage with many sessions
- Session state consistency under race conditions

### Phase 5: URL Fragment Persistence Tests (NEW)

#### 5.1 URL Fragment Parsing Tests (Hybrid Architecture)
**Test Cases:**
- Parse valid URL fragments with Claude session ID: `#claude=abc-def`
- Parse empty URL fragments (no existing Claude session)
- Handle malformed URL fragments gracefully
- Test URL fragment generation with Claude session ID only
- Verify browser session ID is always generated fresh (never from URL)

#### 5.2 Session Persistence Tests
**Scenarios:**
- Page refresh preserves Claude session via URL fragment
- Page refresh generates fresh browser session ID (not from URL)
- Browser back/forward maintains Claude conversation state
- Bookmarked URLs restore correct Claude conversation
- **Critical**: URL copying creates independent tabs with shared Claude session

#### 5.3 URL Fragment Integration Tests
**Test Functions:**
```javascript
// Test URL fragment parsing (Claude session only)
function testClaudeSessionURLParsing()

// Test URL updating when Claude session received (browser session NOT included)
function testClaudeSessionURLUpdating()

// Test Claude conversation restoration from bookmarked URL
function testBookmarkedClaudeConversationRestoration()

// CRITICAL: Test URL copy-paste creates independent tabs
function testURLCopyPasteIndependence()

// Test fresh browser session ID generation on every page load
function testFreshBrowserSessionGeneration()
```

#### 5.4 Multi-Tab Independence Tests
**Critical Test Cases:**
```javascript
// Test that copied URL creates independent tabs with shared Claude session
function testCopyPasteURLBehavior() {
    // 1. Tab A: Start Claude conversation → URL: #claude=abc-def
    // 2. Copy URL to Tab B → Tab B gets fresh browser session ID
    // 3. Verify: Tab A and B have different browser session IDs
    // 4. Verify: Both tabs can continue same Claude conversation
    // 5. Verify: Messages in Tab A don't appear in Tab B
}

// Test backend properly isolates tabs with same Claude session
function testBackendTabIsolationWithSharedClaude() {
    // Verify BroadcastToSession works correctly when multiple tabs share Claude session
}
```

#### 5.5 Frontend Port Tests
**Elm Test Cases:**
```elm
describe "URL Fragment Management (Hybrid Architecture)"
    [ test "updateURLFragment port called with Claude session ID only" <|
        -- Test port invocation excludes browser session ID
        
    , test "model initialized with fresh browser ID and URL Claude ID" <|
        -- Test flags parsing generates fresh browser ID always
        
    , test "URL fragment contains only Claude session after extraction" <|
        -- Test URL format: #claude=abc-def (no browser session)
        
    , test "URL copying preserves Claude session, generates fresh browser session" <|
        -- Test copy-paste behavior maintains tab independence
    ]
```

#### 5.5 Cross-Browser Compatibility Tests
**Browser Testing:**
- Chrome: URL fragment parsing and updating
- Firefox: Session persistence across reloads
- Safari: Port communication and URL handling
- Edge: Fragment encoding and decoding

## Detailed Test Specifications

### Backend Test Details

#### TestClientBrowserSessionTracking
**Purpose:** Verify browser session ID assignment and storage
**Setup:**
1. Create mock WebSocket connection
2. Send ClientMessage with sessionID
3. Verify Client.browserSessionID is set correctly
4. Test session ID persistence across multiple messages

**Assertions:**
- Client.browserSessionID matches sent sessionID
- Session ID remains consistent for client lifetime
- Different clients have different session IDs

#### TestClaudeSessionExtractionFromJSON
**Purpose:** Test Claude session ID parsing from stream-json
**Setup:**
1. Create mock stream-json responses with session_id
2. Process through existing JSON parsing logic
3. Verify Client.claudeSessionID extraction

**Test Data:**
```json
{"type": "assistant", "session_id": "claude-123", "message": {...}}
{"type": "user", "session_id": "claude-123", "message": {...}}
{"type": "result", "session_id": "claude-123", "result": "success"}
```

**Assertions:**
- Session ID correctly extracted and stored
- Different JSON message types handled
- Missing session_id handled gracefully

#### TestClaudeResumeCommandBuilding
**Purpose:** Verify --resume flag logic for Claude commands
**Setup:**
1. Create client with hasStartedSession = false (first message)
2. Verify command built without --resume
3. Set hasStartedSession = true, claudeSessionID = "test-id"
4. Verify subsequent commands include --resume

**Expected Command Patterns:**
```bash
# First message
["claude", "--output-format", "stream-json", "--verbose", "--print", "message"]

# Subsequent messages
["claude", "--resume", "claude-session-123", "--output-format", "stream-json", "--verbose", "--print", "message"]
```

#### TestMultiClientSessionIsolation
**Purpose:** Ensure sessions don't interfere with each other
**Setup:**
1. Create two clients with different browserSessionIDs
2. Send messages on both clients
3. Verify independent Claude session IDs
4. Verify no cross-contamination

**Test Scenario:**
- Client A: browserSessionID = "session-A", gets claudeSessionID = "claude-A"
- Client B: browserSessionID = "session-B", gets claudeSessionID = "claude-B"
- Messages on Client A use "claude-A" for --resume
- Messages on Client B use "claude-B" for --resume

### Frontend Test Details

#### Session ID Extraction Test
**Test JSON Input:**
```json
{
  "type": "assistant",
  "session_id": "adc10fa5-61dc-47fd-a1af-47fdd6d2007c",
  "message": {
    "role": "assistant",
    "content": [{"type": "text", "text": "Hello"}]
  }
}
```

**Assertions:**
- ClaudeMessage.sessionID correctly decoded
- Session ID available for model updates
- Missing session_id handled without errors

#### WebSocket Message Encoding Test
**Expected Output Format:**
```json
{
  "sender": "USER",
  "content": "test message",
  "firstMessage": true,
  "sessionID": "session_1672531200000_abc123def"
}
```

**Assertions:**
- sessionID field present in all outgoing messages
- Correct browser session ID value transmitted
- Message structure matches backend expectations

### Integration Test Details

#### TestMultiTabSessionWorkflow
**Test Flow:**
1. Start two WebSocket connections
2. Send different browser session IDs
3. Exchange Claude messages on both connections
4. Verify independent Claude session creation
5. Send follow-up messages
6. Verify --resume usage with correct session IDs
7. Verify no cross-talk between sessions

**Success Criteria:**
- Two independent Claude sessions created
- Subsequent messages use correct --resume flags
- No session state leakage between connections

## Test Data Requirements

### Mock Stream-JSON Responses
**Session Creation Response:**
```json
{
  "type": "assistant",
  "session_id": "adc10fa5-61dc-47fd-a1af-47fdd6d2007c",
  "message": {
    "role": "assistant", 
    "content": [{"type": "text", "text": "Session started"}]
  }
}
```

**Session Resume Response:**
```json
{
  "type": "assistant", 
  "session_id": "adc10fa5-61dc-47fd-a1af-47fdd6d2007c",
  "message": {
    "role": "assistant",
    "content": [{"type": "text", "text": "Continuing conversation"}]
  }
}
```

**Error Response:**
```json
{
  "type": "result",
  "subtype": "error", 
  "result": "Invalid session ID: expired-session-456"
}
```

### Mock WebSocket Messages
**First Message:**
```json
{
  "sender": "USER",
  "content": "Hello Claude",
  "firstMessage": true,
  "sessionID": "session_1672531200000_abc123def"
}
```

**Subsequent Message:**
```json
{
  "sender": "USER", 
  "content": "Continue our chat",
  "firstMessage": false,
  "sessionID": "session_1672531200000_abc123def"
}
```

## Implementation Steps

### Step 1: Backend Session Tests
**Estimated Time:** 4-6 hours
**Files to Create:**
- `/workspace/cmd/swe-swe/session_test.go`

**Tasks:**
1. Set up test infrastructure and mocks
2. Implement client session tracking tests
3. Implement Claude session extraction tests  
4. Implement command building tests
5. Implement multi-client isolation tests
6. Run tests and fix any issues

### Step 2: Frontend Session Tests  
**Estimated Time:** 2-3 hours
**Files to Modify:**
- `/workspace/elm/tests/ClaudeJSONTest.elm`

**Tasks:**
1. Add session management test suite
2. Test session ID JSON parsing
3. Test WebSocket message encoding
4. Test model session state tracking
5. Run elm-test and verify all pass

### Step 3: Integration Tests
**Estimated Time:** 6-8 hours
**Files to Create:**
- `/workspace/cmd/swe-swe/integration_test.go`

**Tasks:**
1. Set up WebSocket test server
2. Implement multi-tab workflow test
3. Implement session persistence test  
4. Implement concurrent session test
5. Add comprehensive error handling tests
6. Performance and stress testing

### Step 4: URL Fragment Persistence Tests
**Estimated Time:** 3-4 hours
**Files to Create:**
- `/workspace/test/url-fragment-test.js` (Browser-based testing)
- Add URL fragment test cases to existing Elm tests

**Tasks:**
1. Implement URL fragment parsing tests
2. Create session persistence test scenarios  
3. Test browser navigation and bookmarking
4. Add Elm port testing for URL updates
5. Cross-browser compatibility testing

### Step 5: CI Integration & Documentation
**Estimated Time:** 1-2 hours  
**Tasks:**
1. Verify `make test` runs all new tests
2. Update task documentation with test status
3. Add test maintenance notes
4. Document test data and mock requirements
5. **NEW:** Document URL fragment testing procedures

## Expected Outcomes

### Test Coverage Metrics
- **Backend Session Logic:** 90%+ coverage of session-related functions
- **Frontend Session Parsing:** 100% coverage of session JSON handling
- **Integration Workflows:** Complete multi-tab and persistence scenarios
- **URL Fragment Management:** Complete coverage of parsing, updating, and persistence
- **Error Handling:** All edge cases and failure modes covered

### Regression Protection
**Protected Functionality:**
- Browser session ID generation and tracking
- Claude session ID extraction from stream-json  
- Command building with --resume logic
- Multi-client session isolation
- Session persistence across reconnections
- **NEW:** URL fragment parsing and generation
- **NEW:** Session persistence via URL fragments (page refreshes, bookmarks)
- **NEW:** Cross-browser URL fragment compatibility
- Error handling and graceful degradation

### Performance Validation
**Load Testing:**
- 10+ concurrent sessions without interference
- Session creation/destruction cycles
- Memory usage validation
- WebSocket connection handling under load

## Maintenance Requirements

### Test Data Updates
**When to Update:**
- Claude CLI output format changes
- WebSocket message structure changes  
- Session ID format modifications
- New session-related features added

### Test Environment Setup
**Dependencies:**
- Go testing framework (built-in)
- elm-test framework (already installed)
- WebSocket testing utilities (to be added)
- Mock generation tools (to be implemented)

### Continuous Integration
**Test Execution:**
- All tests run via `make test` 
- No additional CI configuration needed
- Tests should complete in <30 seconds
- Zero tolerance for flaky tests

## Risk Assessment

### Implementation Risks
**Low Risk:**
- Backend unit tests (well-defined interfaces)
- Frontend JSON parsing tests (existing patterns)
- Mock infrastructure (standard Go testing)

**Medium Risk:**  
- WebSocket integration tests (complex async behavior)
- Concurrent session testing (potential race conditions)
- Performance testing (environment-dependent)

**Mitigation Strategies:**
- Start with unit tests, progress to integration
- Use deterministic test data and timeouts
- Isolate tests to avoid interference
- Clear cleanup between test runs

### Maintenance Overhead
**Minimal Ongoing Work:**
- Test data updates for format changes
- New test cases for new session features
- Performance threshold adjustments
- Mock updates for API changes

## Success Criteria

### Definition of Done
- [ ] All backend session logic tests implemented and passing
- [ ] All frontend session parsing tests implemented and passing  
- [ ] All integration tests implemented and passing
- [ ] Tests run successfully via `make test`
- [ ] Test coverage reports generated and reviewed
- [ ] Documentation updated with test information
- [ ] Zero false positives or flaky tests
- [ ] Performance benchmarks established

### Quality Gates
- All tests must pass consistently (10 consecutive runs)
- Test execution time under 30 seconds total
- No test dependencies or ordering requirements  
- Clear, descriptive test names and failure messages
- Comprehensive error scenario coverage
- Mock data closely matches production behavior

## Future Enhancements

### Phase 5: Goose Session Tests (Optional)
**When Goose Integration is Implemented:**
- Test `goose session --name` command building
- Test `goose session --resume --name` logic  
- Test first vs subsequent message handling for Goose
- Integration tests with Goose session management

### Phase 6: Advanced Testing (Optional)
**Advanced Scenarios:**
- Browser crash and recovery testing
- Long-running session stability tests
- Cross-browser compatibility testing
- Mobile browser session handling
- Session migration testing

### Phase 7: Performance & Monitoring (Optional)
**Production Monitoring:**
- Session creation/failure rate metrics
- Average session duration tracking  
- Memory usage per session monitoring
- WebSocket connection health checks
- Automated performance regression detection

## Conclusion

This comprehensive test plan will provide robust regression protection for the multi-tab session functionality while building on the existing solid test infrastructure. The phased approach allows for incremental implementation and validation, with clear success criteria and maintenance guidelines.

The implementation should take approximately 12-18 hours total and will result in a production-ready test suite that protects against regressions while supporting future enhancements like Goose session integration.