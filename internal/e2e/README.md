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