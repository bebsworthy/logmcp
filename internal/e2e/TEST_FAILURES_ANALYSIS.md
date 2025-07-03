# E2E Test Failures Analysis

## Summary of Findings

After adding diagnostic logging and running individual tests, here are the key findings:

### 1. Basic Functionality Works
- Simple `echo` commands work correctly
- Process starting, stopping, and log retrieval work for basic cases
- The test framework and MCP communication are functioning

### 2. Complex Command Issues
The main issue appears to be with complex shell commands. When running:
```bash
sh -c "echo TEST_VAR1=$TEST_VAR1; echo TEST_VAR2=$TEST_VAR2"
```

The process starts (gets a PID), completes successfully (exit code 0), but produces empty log content.

### 3. Pattern of Failures

All failing tests share similar characteristics:
- **Environment Variables Test**: Shell command with environment variable expansion - empty logs
- **Crash Test**: Looking for specific output patterns that may not match
- **Restart Policies**: Expecting processes to restart but they remain in "crashed" state
- **Many Sessions**: Processes not transitioning to expected states

### 4. Root Cause Identified

The primary issue is in `internal/server/mcp.go` at line 622 in the `startManagedProcess` function:

```go
parts := strings.Fields(req.Command)
```

This uses `strings.Fields()` to split the command, which breaks on ALL whitespace. For shell commands like:
```
sh -c "echo TEST_VAR1=$TEST_VAR1; echo TEST_VAR2=$TEST_VAR2"
```

It incorrectly splits into:
- `["sh", "-c", "echo", "TEST_VAR1=$TEST_VAR1;", "echo", "TEST_VAR2=$TEST_VAR2"]`

Instead of the correct:
- `["sh", "-c", "echo TEST_VAR1=$TEST_VAR1; echo TEST_VAR2=$TEST_VAR2"]`

This explains why:
- Environment variable tests fail (shell command is broken)
- Complex commands produce empty output
- Simple commands like `echo Hello World` work fine

### 5. Secondary Issues

1. **Process State Management**: 
   - Restart policies may not be implemented correctly
   - "stopped" vs "crashed" distinction needs verification

2. **Test Expectations**:
   - Some tests look for specific output that may not match actual output
   - Exit codes might not be captured correctly

## Recommended Fix

The server needs to properly handle shell commands. Options:

1. **Fix Command Parsing** (Recommended):
   - Use proper shell command parsing that respects quotes
   - Or accept command and args as separate fields in the API
   - Example libraries: `github.com/mattn/go-shellwords`

2. **Adjust Tests**:
   - Avoid shell syntax in tests
   - Use direct commands without shell interpretation
   - This would limit functionality significantly

## Summary

The root cause is that `strings.Fields()` breaks shell commands with quoted arguments. This affects:
- All tests using shell syntax (`sh -c "..."`)
- Environment variable expansion tests
- Complex command tests

The fix requires changing how commands are parsed in the server, either by:
- Using a proper shell parser
- Accepting pre-split command and arguments
- Using a different splitting approach that respects quotes