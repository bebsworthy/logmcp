# Error Handling Improvements

## Summary of Changes

### 1. Added `get_help` Tool
- Returns system context from `GetMCPContext()`
- Provides on-demand help for LLMs

### 2. Enhanced Error Messages with Context Hints
- Created `addContextToError()` helper function that adds helpful hints based on error type
- Examples:
  - "session not found" → adds " Use 'list_sessions' to see available sessions."
  - "signal" errors → adds " Valid signals: SIGTERM (graceful), SIGKILL (force), SIGINT (interrupt), SIGHUP (reload)."

### 3. Improved `get_logs` Error Handling
- Now returns an error when all requested sessions are not found
- Before: `{"success": true, "data": {"logs": null}, "meta": {"sessions_not_found": ["xxxx"]}}`
- After: Error with message "session 'xxxx' not found. Use 'list_sessions' to see available sessions."

### 4. Added Context to Empty `list_sessions` Response
- When no sessions exist, includes a help field
- Help text: "No sessions found. Use 'start_process' to launch a new monitored process, or start a process with 'logmcp run <command>' from the command line."

## Examples

### Example 1: get_logs with non-existent session
**Request:**
```json
{
  "name": "get_logs",
  "arguments": {
    "labels": ["non-existent-session"]
  }
}
```

**Response (Error):**
```
session 'non-existent-session' not found. Use 'list_sessions' to see available sessions.
```

### Example 2: Empty list_sessions
**Request:**
```json
{
  "name": "list_sessions"
}
```

**Response:**
```json
{
  "success": true,
  "data": {
    "sessions": [],
    "help": "No sessions found. Use 'start_process' to launch a new monitored process, or start a process with 'logmcp run <command>' from the command line."
  },
  "meta": {
    "total_count": 0,
    "active_count": 0
  }
}
```

### Example 3: Using get_help
**Request:**
```json
{
  "name": "get_help"
}
```

**Response:**
```json
{
  "success": true,
  "data": {
    "content": "# LogMCP - Log Monitoring and Process Control\n\nLogMCP enables you to monitor processes, search logs, and control running applications through the Model Context Protocol.\n\n## Quick Start\n1. Use 'list_sessions' to see available processes\n2. Use 'get_logs' with a session label to view output\n3. Use 'start_process' to launch new monitored processes\n4. Use 'control_process' to restart or send signals\n..."
  }
}
```

## Benefits

1. **Better Error Discovery**: LLMs immediately understand what went wrong and how to fix it
2. **Improved First-Time Experience**: Empty states provide guidance on next steps
3. **On-Demand Help**: LLMs can request help when needed without overwhelming every response
4. **Consistent Error Patterns**: All errors follow similar patterns with contextual hints