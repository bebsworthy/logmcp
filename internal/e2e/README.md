# E2E Testing for LogMCP

## Overview

This directory contains end-to-end tests for the LogMCP server that verify the complete MCP protocol implementation and integration between all components.

## Test Structure

- `mcp_server_test.go` - Main E2E test file containing:
  - `TestLogMCPServerDebug` - Verifies MCP server initialization and tools/list functionality
  - `TestDirectToolCall` - Tests direct MCP tool invocation without listing

- `exploration/` - Directory containing experimental tests and debugging utilities used during development

## Running Tests

```bash
# Build the binary first
go build -o logmcp main.go

# Run all E2E tests
go test -v ./internal/e2e

# Run specific test
go test -v ./internal/e2e -run TestLogMCPServerDebug
```

## Next Priority Tests

Based on the current implementation status, the following tests should be prioritized:

### 1. send_stdin_test.go (High Priority)
Framework support exists but no tests implemented. This is a core feature that needs testing:
- Basic stdin forwarding to interactive processes
- Multi-line input handling
- Binary data support
- Error cases (non-existent session, process without stdin capability)

### 2. control_process_test.go (High Priority)
Consolidate scattered control tests into a dedicated file:
- All signal types (SIGTERM, SIGKILL, SIGINT, SIGHUP, SIGUSR1, SIGUSR2)
- Process restart with various states
- Error handling for edge cases
- Concurrent control operations

### 3. Integration Tests (Medium Priority)
- WebSocket reconnection and resilience
- Buffer management limits and eviction
- Session lifecycle from creation to cleanup
- Process runner and forwarder modes

### 4. Performance & Security Tests (Lower Priority)
- Load testing with many sessions/high log volume
- Security validation for command injection
- Resource exhaustion handling

## Test Coverage Requirements

The following tests remain to be implemented for comprehensive E2E coverage:

### Tool-Specific Tests

#### send_stdin (Not Implemented)
- [ ] Basic stdin forwarding
- [ ] Multi-line input
- [ ] Binary data handling
- [ ] Stdin to non-existent session
- [ ] Stdin to process without stdin capability

#### control_process (Needs Dedicated Test File)
- [ ] Comprehensive signal testing (all 6 signal types)
- [ ] Control of non-existent session
- [ ] Control of already stopped process
- [ ] Rapid successive control commands
- [ ] Control during process startup/shutdown

#### terminating process
- [ ] SIGTERM graceful shutdown verification
- [ ] SIGKILL force termination
- [ ] MCP server shutdown with active processes
- [ ] Process exit code capture
- [ ] Cleanup of process resources
- [ ] Termination of hung/zombie processes

#### process tree cleanup
- [ ] Parent process termination kills all children
- [ ] Bash script spawning subprocesses (e.g., `bash -c "npm start & npm run worker"`)
- [ ] npm/node process trees with multiple workers
- [ ] Docker compose process groups
- [ ] Process groups and session leaders
- [ ] Orphaned process detection and cleanup
- [ ] Recursive termination of nested process trees


### Integration Tests

#### WebSocket Communication
- [ ] Runner registration and label assignment
- [ ] Log streaming from runners
- [ ] Status updates propagation
- [ ] Reconnection handling
- [ ] Multiple concurrent runners

#### Buffer Management
- [ ] 5MB size limit enforcement
- [ ] 5-minute time limit enforcement
- [ ] FIFO eviction behavior
- [ ] Thread-safe concurrent access
- [ ] 64KB line limit handling

#### Session Lifecycle
- [ ] Session creation and cleanup
- [ ] Disconnection handling (process continues)
- [ ] 1-hour cleanup after termination
- [ ] Label conflict resolution

#### Process Runner Mode
- [ ] `logmcp run` command execution
- [ ] Stdout/stderr capture
- [ ] Process exit code handling
- [ ] Signal forwarding

#### Log Forwarder Mode
- [ ] File forwarding
- [ ] Stdin forwarding
- [ ] Named pipe forwarding
- [ ] File rotation handling
- [ ] Large file handling

### Performance Tests
- [ ] High-volume log ingestion
- [ ] Many concurrent sessions
- [ ] Large individual log lines
- [ ] Memory usage under load
- [ ] CPU usage optimization

### Resilience Tests
- [ ] Server restart with active sessions
- [ ] Network interruption recovery
- [ ] Resource exhaustion handling
- [ ] Graceful shutdown
- [ ] Process cleanup on unexpected server termination
- [ ] Signal propagation during server shutdown

### Security Tests
- [ ] Command injection prevention
- [ ] Path traversal prevention
- [ ] Resource limit enforcement

## Current Implementation Status

### ✅ Fully Implemented

**Core Infrastructure:**
- Basic MCP server initialization and tools/list functionality
- Test framework with dynamic port management (`framework.go`)
- Test helper applications (simple_app, crash_app, fork_app, stdin_app)
- Port allocation with conflict prevention for parallel execution

**MCP Protocol Tests (`mcp_protocol_test.go`):**
- Protocol handshake and initialization validation
- Error handling for invalid requests and protocol violations
- Concurrent request handling (up to 50 simultaneous requests)
- Additional edge cases: large payloads, context cancellation, special characters

**Tool-Specific Tests:**
- **list_sessions** (`list_sessions_test.go`) - All planned scenarios plus:
  - Crashed sessions detection
  - Many sessions (20+) handling
  - Session type differentiation
  - Restarted session tracking
- **get_logs** (`get_logs_test.go`) - All planned scenarios implemented:
  - Single/multi-session retrieval
  - Pattern, stream, and time-based filtering
  - Line and result limiting
  - Error handling for non-existent sessions
- **start_process** (`start_process_test.go`) - All planned scenarios plus:
  - Invalid working directory handling
  - Duplicate label resolution
  - Complex command execution
  - Process tree management
  - Startup log collection toggle

**Resilience Tests (`resilience_test.go`):**
- Process crash detection with exit codes
- High-frequency logging stress tests
- Long-running process management
- Multiple concurrent process handling

### ⚠️ Partially Implemented

- **control_process** - Functionality exists but scattered across multiple test files:
  - SIGTERM in resilience_test.go
  - SIGKILL in framework_test.go
  - Restart in list_sessions_test.go
  - Missing: dedicated test file, all signal types, comprehensive error cases

- **Process tree cleanup** - Basic testing in start_process_test.go but not comprehensive

### ❌ Not Implemented

**Tool-Specific Tests:**
- **send_stdin** - Framework support exists but no test cases implemented
- **Comprehensive termination tests** - Only basic signal tests exist

**Integration Tests:**
- WebSocket communication patterns
- Buffer management (5MB/5min limits)
- Session lifecycle and cleanup
- Process runner mode (`logmcp run`)
- Log forwarder mode (`logmcp forward`)

**Advanced Tests:**
- Performance and load tests
- Security tests (command injection, path traversal)
- Network interruption recovery
- Resource exhaustion handling

## Implementation Strategy

### Current File Structure

```
internal/e2e/
├── framework.go            # ✅ Base test framework with port management
├── framework_test.go       # ✅ Framework functionality tests
├── mcp_protocol_test.go    # ✅ MCP protocol-level tests (comprehensive)
├── mcp_server_test.go      # ✅ MCP server integration tests
├── list_sessions_test.go   # ✅ Tests for list_sessions tool (complete)
├── get_logs_test.go        # ✅ Tests for get_logs tool (complete)
├── start_process_test.go   # ✅ Tests for start_process tool (complete)
├── resilience_test.go      # ✅ Basic resilience and recovery tests
├── debug_test.go           # ✅ Simple debugging tests
├── control_process_test.go # ❌ Not implemented (tests scattered)
├── send_stdin_test.go      # ❌ Not implemented
├── integration_test.go     # ❌ Not implemented
├── performance_test.go     # ❌ Not implemented
└── test_helpers/           # ✅ Test applications implemented
    ├── README.md           # Documentation for test helpers
    ├── simple_app.go       # ✅ Long-running test app
    ├── crash_app.go        # ✅ App that crashes with exit code
    ├── fork_app.go         # ✅ App that creates child processes
    └── stdin_app.go        # ✅ Interactive app accepting stdin
```

### Base Framework Design with Port Management

The `framework.go` will provide a robust test infrastructure with automatic port allocation to prevent conflicts during parallel test execution:

```go
type TestServer struct {
    MCPClient     *client.Client
    WebSocketPort int          // Dynamically allocated port
    ServerCmd     *exec.Cmd
    t             *testing.T
}

// Dynamic port allocation to avoid conflicts
func getFreePort() (int, error) {
    listener, err := net.Listen("tcp", ":0")
    if err != nil {
        return 0, err
    }
    defer listener.Close()
    return listener.Addr().(*net.TCPAddr).Port, nil
}
```

**Key Framework Features:**
- `SetupTest(t *testing.T) *TestServer` - Creates server with dynamic port allocation
- `StartTestProcess(label, command string) error` - Helper to start processes via MCP
- `GetLogs(labels []string, opts ...LogOption) ([]LogEntry, error)` - Log retrieval helper
- `WaitForCondition(timeout, check func() bool)` - Polling helper for async operations
- `AssertSessionStatus(label, expectedStatus string)` - Status verification helper
- Automatic cleanup with port release in `t.Cleanup()`

### Implementation Phases

**Phase 1: Core Infrastructure (Week 1)**
1. Create `framework.go` with port management and test utilities
2. Migrate existing tests to use the new framework
3. Create test helper applications
4. Implement `mcp_protocol_test.go` for protocol validation

**Phase 2: Tool-Specific Tests (Week 2-3)**
1. `list_sessions_test.go` - Simplest tool, good starting point
2. `start_process_test.go` - Core functionality for other tests
3. `get_logs_test.go` - Most complex query functionality
4. `control_process_test.go` - Process lifecycle management
5. `send_stdin_test.go` - Interactive process testing

**Phase 3: Integration Tests (Week 4)**
1. WebSocket communication tests
2. Buffer management tests
3. Session lifecycle tests
4. Process runner and forwarder mode tests

**Phase 4: Advanced Tests (Week 5)**
1. Performance and load tests
2. Resilience and recovery tests
3. Security tests

### Test Pattern Example

```go
func TestListSessionsMultipleActive(t *testing.T) {
    t.Parallel() // Safe to run in parallel with port management
    
    server := SetupTest(t)
    defer server.Cleanup() // Ensures port release
    
    // Start multiple processes
    processes := []struct{label, cmd string}{
        {"backend", "go run test_helpers/simple_app.go"},
        {"frontend", "go run test_helpers/simple_app.go"},
        {"worker", "go run test_helpers/simple_app.go"},
    }
    
    for _, p := range processes {
        err := server.StartTestProcess(p.label, p.cmd)
        require.NoError(t, err)
    }
    
    // Wait for all processes to be running
    server.WaitForCondition(5*time.Second, func() bool {
        sessions, _ := server.ListSessions()
        return len(sessions) == 3
    })
    
    // Verify session details
    sessions, err := server.ListSessions()
    require.NoError(t, err)
    assert.Len(t, sessions, 3)
    
    for _, session := range sessions {
        assert.Equal(t, "running", session.Status)
        assert.NotZero(t, session.PID)
        assert.NotNil(t, session.StartTime)
    }
}
```

### Special Considerations

**Parallel Test Execution:**
- All tests use `t.Parallel()` for faster execution
- Dynamic port allocation prevents conflicts
- Each test gets isolated server instance
- Port tracking with mutex protection

**CI/CD Environment:**
- Use high port ranges (30000-40000) to avoid system conflicts
- Implement retry logic for port allocation failures
- Graceful cleanup even on test failures
- Support for containerized test environments

**Process Tree Testing:**
- Use bash scripts that spawn subprocesses
- Test with npm/node applications creating workers
- Verify complete process tree termination
- Handle orphaned process cleanup

**Known Limitations:**
- `tools/list` has marshaling issues - use direct tool calls
- Document workarounds in test comments
- Skip tools/list tests until fixed

**Test Execution:**
```bash
# Run tests sequentially (safer but slower)
go test -p 1 ./internal/e2e/...

# Run tests in parallel (faster, uses port management)
go test -parallel 8 ./internal/e2e/...
```