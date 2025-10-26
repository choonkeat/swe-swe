# CRITICAL: Architectural Refactoring

## Priority: 🔴 CRITICAL - Technical Debt Crisis

## Problem Statement

The codebase suffers from severe architectural problems that make it unmaintainable, untestable, and error-prone. The monolithic design with 800+ line functions, god objects, and tight coupling prevents safe modifications and guarantees bugs with every change.

## Architectural Anti-Patterns Identified

### 1. God Function: executeAgentCommandWithSession (800+ lines)

**Location:** `cmd/swe-swe/websocket.go:379-1200+`

**Current Horror:**
```go
func (svc *ChatService) executeAgentCommandWithSession(...) ExecutionResult {
    // 800+ lines doing:
    // - Session validation
    // - Permission checking
    // - Process spawning
    // - PTY management
    // - Stream processing
    // - Error handling
    // - Message broadcasting
    // - State management
    // - Retry logic
    // ... all in ONE function!
}
```

**Problems:**
1. **Untestable**: Can't test parts in isolation
2. **Unmaintainable**: Changes affect everything
3. **Error-prone**: Side effects everywhere
4. **Unreadable**: Impossible to understand flow
5. **Duplicated Logic**: Same patterns repeated
6. **Hidden Dependencies**: Unclear what depends on what

**Impact:**
- Every bug fix introduces 2 new bugs
- 90% of time spent understanding code
- New developers can't contribute
- Refactoring is impossible

### 2. God Object: Client Struct

**Location:** `cmd/swe-swe/websocket.go:23-38`

**Current Mess:**
```go
type Client struct {
    // Connection Management
    conn         *websocket.Conn
    cancelFunc   context.CancelFunc
    
    // Identity
    username         string
    browserSessionID string
    
    // Session State
    claudeSessionID       string
    claudeSessionHistory []string
    hasStartedSession    bool
    lastKilledSessionID  string
    
    // Process Management
    activeProcess *exec.Cmd
    processMutex  sync.Mutex
    
    // Permission State
    allowedTools          []string
    skipPermissions      bool
    pendingToolPermission string
    
    // Message State
    lastUserMessage string
}
```

**Problems:**
1. **Too Many Responsibilities**: Connection + Session + Process + Permissions + Messages
2. **High Coupling**: Every feature touches Client
3. **State Explosion**: Too many state combinations
4. **Concurrency Nightmare**: Multiple mutexes needed
5. **Testing Difficulty**: Can't mock parts

### 3. Tight Coupling Throughout

**Examples:**
```go
// WebSocket handler knows about Claude CLI details
if strings.Contains(line, "[PERMISSION_REQUEST]") {
    // Direct CLI parsing in WebSocket layer!
}

// ChatService knows about process management
cmd := exec.CommandContext(ctx, cmdArgs[0], cmdArgs[1:]...)

// Client knows about broadcast implementation
svc.BroadcastToSession(item, client.browserSessionID)
```

**Problems:**
1. **Layer Violations**: Business logic in transport layer
2. **No Abstraction**: Direct dependencies everywhere
3. **Hard-coded Behavior**: Can't swap implementations
4. **Testing Requires Everything**: Can't test in isolation

### 4. No Separation of Concerns

**Current Structure:**
```
websocket.go: 1200+ lines doing everything
├── WebSocket handling
├── Session management
├── Process execution
├── Permission handling
├── Message parsing
├── Error handling
└── Stream processing
```

**Should Be:**
```
transport/
├── websocket.go (100 lines)
├── http.go (50 lines)
domain/
├── session.go (200 lines)
├── client.go (150 lines)
├── permissions.go (100 lines)
process/
├── executor.go (200 lines)
├── stream.go (150 lines)
```

### 5. No Dependency Injection

**Current:**
```go
func (svc *ChatService) executeAgentCommand() {
    // Directly creates everything
    cmd := exec.Command(...)
    scanner := bufio.NewScanner(...)
    // Can't mock, can't test
}
```

**Should Be:**
```go
type ProcessExecutor interface {
    Execute(ctx context.Context, cmd Command) (*Process, error)
}

func (svc *ChatService) executeAgentCommand(executor ProcessExecutor) {
    process, err := executor.Execute(ctx, cmd)
    // Testable with mock executor
}
```

## Refactoring Plan

### Phase 1: Extract Core Domains

1. **Session Management Domain**
```go
// session/manager.go
type SessionManager interface {
    Create() (*Session, error)
    Resume(id string) (*Session, error)
    Validate(id string) bool
    List() ([]*Session, error)
}

// session/session.go
type Session struct {
    ID        string
    State     SessionState
    History   []Command
    Context   map[string]interface{}
}

// session/store.go
type SessionStore interface {
    Save(session *Session) error
    Load(id string) (*Session, error)
    Delete(id string) error
}
```

2. **Process Execution Domain**
```go
// process/executor.go
type Executor interface {
    Execute(ctx context.Context, cmd Command) (*Process, error)
    Terminate(process *Process) error
}

// process/process.go
type Process struct {
    ID       string
    Command  Command
    State    ProcessState
    Stdout   io.Reader
    Stderr   io.Reader
    Stdin    io.Writer
}

// process/stream.go
type StreamProcessor interface {
    Process(reader io.Reader, handler MessageHandler) error
}
```

3. **Permission Domain**
```go
// permission/manager.go
type PermissionManager interface {
    Check(tool string, user User) (bool, error)
    Request(tool string, user User) (*PermissionRequest, error)
    Grant(request *PermissionRequest) error
    Deny(request *PermissionRequest) error
}

// permission/policy.go
type Policy interface {
    Evaluate(tool string, user User) Decision
}
```

### Phase 2: Introduce Clean Architecture

```
cmd/swe-swe/
├── main.go (50 lines - just wiring)
├── config/
│   └── config.go
├── transport/
│   ├── websocket/
│   │   ├── handler.go
│   │   └── client.go
│   └── http/
│       └── handler.go
├── application/
│   ├── usecases/
│   │   ├── execute_command.go
│   │   ├── manage_session.go
│   │   └── handle_permission.go
│   └── ports/
│       ├── input.go
│       └── output.go
├── domain/
│   ├── entities/
│   │   ├── session.go
│   │   ├── client.go
│   │   └── permission.go
│   └── services/
│       ├── session_service.go
│       └── permission_service.go
└── infrastructure/
    ├── process/
    │   └── executor.go
    ├── persistence/
    │   └── session_store.go
    └── claude/
        └── client.go
```

### Phase 3: Implement Patterns

1. **Command Pattern for Operations**
```go
type Command interface {
    Execute(ctx context.Context) error
}

type ExecuteAgentCommand struct {
    Session  *Session
    Prompt   string
    Executor Executor
}

func (c *ExecuteAgentCommand) Execute(ctx context.Context) error {
    // Clean, focused implementation
    return c.Executor.Execute(ctx, c.Prompt)
}
```

2. **Observer Pattern for Events**
```go
type EventBus interface {
    Publish(event Event)
    Subscribe(eventType string, handler EventHandler)
}

type Event interface {
    Type() string
    Payload() interface{}
}

// Usage
eventBus.Publish(SessionCreatedEvent{
    SessionID: session.ID,
    Timestamp: time.Now(),
})
```

3. **Strategy Pattern for Processing**
```go
type MessageProcessor interface {
    Process(message Message) error
}

type ClaudeProcessor struct{}
type GooseProcessor struct{}

// Select strategy based on config
var processor MessageProcessor
if config.Agent == "claude" {
    processor = &ClaudeProcessor{}
} else {
    processor = &GooseProcessor{}
}
```

### Phase 4: Break Down God Function

**Before:** 800+ line monster

**After:** Composed of small, testable functions
```go
func (uc *ExecuteCommandUseCase) Execute(ctx context.Context, req Request) error {
    // Validate request (20 lines)
    if err := uc.validator.Validate(req); err != nil {
        return err
    }
    
    // Check permissions (delegated)
    if err := uc.checkPermissions(ctx, req); err != nil {
        return err
    }
    
    // Execute command (delegated)
    result, err := uc.executeCommand(ctx, req)
    if err != nil {
        return err
    }
    
    // Process output (delegated)
    return uc.processOutput(ctx, result)
}

func (uc *ExecuteCommandUseCase) checkPermissions(ctx context.Context, req Request) error {
    return uc.permissionService.Check(ctx, req.Tool, req.User)
}

func (uc *ExecuteCommandUseCase) executeCommand(ctx context.Context, req Request) (*Result, error) {
    return uc.executor.Execute(ctx, req.Command)
}

func (uc *ExecuteCommandUseCase) processOutput(ctx context.Context, result *Result) error {
    return uc.outputProcessor.Process(ctx, result)
}
```

### Phase 5: Introduce Testing

1. **Unit Tests for Each Component**
```go
func TestSessionManager_Create(t *testing.T) {
    store := &MockSessionStore{}
    manager := NewSessionManager(store)
    
    session, err := manager.Create()
    
    assert.NoError(t, err)
    assert.NotEmpty(t, session.ID)
    assert.Equal(t, 1, store.SaveCallCount())
}
```

2. **Integration Tests**
```go
func TestExecuteCommand_Integration(t *testing.T) {
    // Use real implementations
    executor := process.NewExecutor()
    processor := stream.NewProcessor()
    
    useCase := NewExecuteCommandUseCase(executor, processor)
    
    err := useCase.Execute(context.Background(), testRequest)
    assert.NoError(t, err)
}
```

3. **End-to-End Tests**
```go
func TestWebSocketFlow_E2E(t *testing.T) {
    server := startTestServer()
    defer server.Stop()
    
    client := connectWebSocket(server.URL)
    defer client.Close()
    
    client.Send(testMessage)
    response := client.Receive()
    
    assert.Equal(t, expectedResponse, response)
}
```

## Implementation Strategy

### Week 1: Foundation
1. Create domain models
2. Define interfaces
3. Set up project structure
4. Add dependency injection

### Week 2: Core Extraction
1. Extract session management
2. Extract process execution
3. Extract permission handling
4. Create service layer

### Week 3: God Function Breakdown
1. Identify responsibilities
2. Create use cases
3. Implement command pattern
4. Add event system

### Week 4: Testing & Migration
1. Write comprehensive tests
2. Parallel run old and new
3. Gradual migration
4. Performance validation

## Success Criteria

### Code Quality Metrics
1. ✅ No function > 100 lines
2. ✅ No file > 500 lines
3. ✅ Cyclomatic complexity < 10
4. ✅ Test coverage > 80%
5. ✅ No circular dependencies

### Maintainability Metrics
1. ✅ New feature in < 1 day
2. ✅ Bug fix in < 2 hours
3. ✅ Onboarding in < 1 week
4. ✅ Refactoring without fear

### Testing Metrics
1. ✅ Unit tests run in < 1 second
2. ✅ Integration tests in < 10 seconds
3. ✅ E2E tests in < 1 minute
4. ✅ All tests pass consistently

## Risk Mitigation

### Parallel Implementation
- Keep old code running
- Implement new alongside
- Feature flag for switching
- Gradual rollout

### Incremental Migration
```go
func (svc *ChatService) executeCommand(req Request) error {
    if featureFlag.UseNewArchitecture() {
        return svc.newExecutor.Execute(req)
    }
    return svc.legacyExecute(req)
}
```

### Rollback Plan
- Git tags at each phase
- Database migrations reversible
- Config to switch implementations
- Monitoring for regressions

## Long-term Benefits

### Development Velocity
- **Before**: 1 feature/week with bugs
- **After**: 3 features/week, bug-free

### Debugging Time
- **Before**: Hours to find issues
- **After**: Minutes with clear stack traces

### Onboarding
- **Before**: Weeks to understand
- **After**: Days to contribute

### Confidence
- **Before**: Fear of changes
- **After**: Refactor with confidence

## Notes

- Start with highest-value extractions
- Maintain backward compatibility
- Document architecture decisions
- Use ADRs (Architecture Decision Records)
- Regular architecture reviews
- Invest in developer tools

## References

- [Clean Architecture](https://blog.cleancoder.com/uncle-bob/2012/08/13/the-clean-architecture.html)
- [Domain-Driven Design](https://martinfowler.com/bliki/DomainDrivenDesign.html)
- [SOLID Principles](https://en.wikipedia.org/wiki/SOLID)
- [Go Best Practices](https://peter.bourgon.org/go-best-practices-2016/)
- [Refactoring](https://refactoring.guru/refactoring)