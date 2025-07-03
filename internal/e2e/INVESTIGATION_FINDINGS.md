# E2E Test Investigation Findings

## Summary
After investigating the failing e2e tests, I found that most failures were due to incorrect test expectations rather than actual bugs in the server implementation.

## Fixed Issues

### 1. TestStartProcess_CrashingProcess ✅
**Problem**: Test expected specific error strings that the crash app wasn't producing.
- Expected: "CRITICAL ERROR" and "panic:"
- Actual: "Fatal error occurred!" and "CRASH!"
- Also expected exit code 42 but got 1 because `go run` returns 1 for any non-zero child exit

**Fix**: Updated test to look for actual strings and expect exit code 1 with explanatory comment.

### 2. TestListSessions_CrashedSessions ✅
**Problem**: Test framework had field name mismatch.
- MCP protocol returns: `exit_time`
- Test framework expected: `end_time`

**Fix**: Updated JSON tag in framework.go from `json:"end_time"` to `json:"exit_time"`

## Not Implemented Features

### 1. Restart Policies ❌
**Issue**: Tests expect automatic restart functionality based on policies (never, on-failure, always)
**Finding**: Restart policies are accepted as parameters but not stored or implemented
- The `restart_policy` parameter is validated but discarded
- No automatic restart logic exists in the process completion handler
- Manual restart via control_process works, but automatic restarts don't

**Tests affected**:
- TestStartProcess_RestartPolicies (3 of 6 subtests fail)

## Diagnostic Logging Added
To aid in investigation, diagnostic logging was added to:
1. Process completion in mcp.go - logs exit codes and errors
2. Session state transitions in session.go - logs status changes
3. Process start in mcp.go - logs command execution details

These logs use `[DEBUG]` prefix and help understand the actual behavior vs test expectations.

## Recommendations
1. The restart policy feature needs to be implemented if those tests are expected to pass
2. The diagnostic logging could be kept (perhaps behind a debug flag) as it's useful for troubleshooting
3. Consider documenting which features are actually implemented vs planned