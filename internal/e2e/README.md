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

## Test Coverage Requirements

To achieve comprehensive E2E test coverage, the following tests need to be implemented:

### MCP Protocol Tests
- [ ] **Protocol Handshake** - Full initialize/initialized sequence validation
- [ ] **Error Handling** - Invalid requests, malformed JSON, protocol violations
- [ ] **Concurrent Requests** - Multiple simultaneous MCP requests

### Tool-Specific Tests

#### list_sessions
- [ ] Empty session list
- [ ] Multiple active sessions with different statuses
- [ ] Session metadata accuracy (PID, buffer size, log count)
- [ ] Sessions with duplicate labels

#### get_logs
- [ ] Basic log retrieval from single session
- [ ] Multi-session log aggregation
- [ ] Pattern filtering with regex
- [ ] Stream filtering (stdout/stderr/both)
- [ ] Time-based filtering (since parameter)
- [ ] Line count limiting
- [ ] Max results limiting across sessions
- [ ] Non-existent session handling

#### start_process
- [ ] Basic process startup
- [ ] Process with custom working directory
- [ ] Process with environment variables
- [ ] Process with different restart policies
- [ ] Long-running process management
- [ ] Process that exits immediately
- [ ] Process that crashes
- [ ] Invalid command handling

#### control_process
- [ ] Process restart functionality
- [ ] Signal sending (SIGTERM, SIGKILL, SIGINT, SIGHUP, SIGUSR1, SIGUSR2)
- [ ] Control of non-existent session
- [ ] Control of already stopped process

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

#### send_stdin
- [ ] Basic stdin forwarding
- [ ] Multi-line input
- [ ] Binary data handling
- [ ] Stdin to non-existent session
- [ ] Stdin to process without stdin capability

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

✅ Basic MCP server initialization and tools/list functionality
✅ Direct tool invocation (list_sessions)

The remaining tests listed above need to be implemented to ensure comprehensive coverage of all LogMCP features and edge cases.

## Implementation Strategy

### File Structure Organization

Tests will be organized by category for better maintainability and parallel execution:

```
internal/e2e/
├── framework.go            # Base test framework with common utilities
├── mcp_protocol_test.go    # MCP protocol-level tests
├── list_sessions_test.go   # Tests for list_sessions tool
├── get_logs_test.go        # Tests for get_logs tool
├── start_process_test.go   # Tests for start_process tool
├── control_process_test.go # Tests for control_process tool
├── send_stdin_test.go      # Tests for send_stdin tool
├── integration_test.go     # WebSocket, buffer, and session lifecycle tests
├── performance_test.go     # Performance and load tests
├── resilience_test.go      # Resilience and recovery tests
└── test_helpers/           # Test applications and utilities
    ├── simple_app.go       # Simple test application
    ├── crash_app.go        # App that crashes after startup
    ├── fork_app.go         # App that creates child processes
    └── stdin_app.go        # App that reads from stdin
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