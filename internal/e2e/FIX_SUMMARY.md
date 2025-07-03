# E2E Test Fix Summary

## Root Cause
The primary issue was that `start_process` tool used `strings.Fields()` to parse commands, which breaks shell commands with quoted arguments. For example:
```
sh -c "echo TEST_VAR1=$TEST_VAR1"
```
Was incorrectly parsed into: `["sh", "-c", "echo", "TEST_VAR1=$TEST_VAR1"]`

## Solution Implemented
Changed `start_process` to follow standard Unix conventions:
- Separate `command` (string) and `arguments` (array of strings) parameters
- Direct mapping to `exec.Command(cmd, args...)`
- No shell parsing complexity

## Changes Made

1. **Updated MCP Tool Definition** (`internal/server/mcp.go`):
   - Added `arguments` array parameter to `start_process` tool

2. **Updated Protocol** (`internal/protocol/mcp_tools.go`):
   - Added `Arguments []string` to `StartProcessRequest` struct
   - Added `arguments` to tool schema

3. **Updated Server Implementation** (`internal/server/mcp.go`):
   - Modified `handleStartProcess` to extract arguments array
   - Changed `startManagedProcess` to use `exec.Command(req.Command, req.Arguments...)`
   - Updated `ManagedArgs` struct to include Arguments field

4. **Updated Test Framework** (`internal/e2e/framework.go`):
   - Modified `StartTestProcess` and `StartTestProcessWithOptions` to pass command and arguments separately

5. **Updated Documentation** (`documentation/project.spec.md`):
   - Added `start_process` tool specification

## Test Results

### Fixed Tests ✅
- TestStartProcess_EnvironmentVariables
- TestStartProcess_ComplexCommand
- TestMCPProtocolLargeResponse

### Still Failing Tests ❌
- TestStartProcess_CrashingProcess - Test expects different output strings
- TestListSessions_* - Various session state management issues
- TestStartProcess_RestartPolicies - Restart policies not working correctly

## Remaining Issues
1. Some tests have incorrect expectations (e.g., looking for "CRITICAL ERROR" when app outputs "Fatal error occurred!")
2. Exit code handling may have issues
3. Restart policies implementation needs verification
4. Session state transitions (stopped vs crashed) need review

## Example Usage
Before:
```go
// Broken - command gets split incorrectly
command: "sh -c \"echo $VAR\""
```

After:
```go
// Works correctly
command: "sh"
arguments: ["-c", "echo $VAR"]
```