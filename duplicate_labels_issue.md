# Duplicate Labels Issue - RESOLVED

## Problem (FIXED)
The e2e tests `TestStartProcess_DuplicateLabels` and `TestListSessions_DuplicateLabels` were failing. 

When starting two managed processes via `start_process` with the same label, the expected behavior is:
1. First process gets label "duplicate"
2. Second process gets label "duplicate-2"

## Root Cause
The issue was that the `logmcp` binary was not built before running the tests. The test framework depends on having the compiled binary available to launch the MCP server.

## Resolution
1. Built the logmcp binary with `go build -o logmcp .`
2. Fixed a compilation error (unused variable `pid` in session.go)
3. Added build tags to test helper applications to prevent them from being compiled as part of the test package

## Verification
After building the binary, the tests pass correctly:
- First process gets label "duplicate" with PID X
- Second process correctly gets label "duplicate-2" with PID Y
- The SessionManager's label conflict resolution works as designed

## Status
✅ RESOLVED - The duplicate label handling works correctly for managed processes.