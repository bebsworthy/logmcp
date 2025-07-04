# LogMCP LLM Usage Guide

## Overview

LogMCP is a Model Context Protocol (MCP) server that provides real-time log streaming and process management capabilities. It enables you to:

- Monitor running processes and their output
- Search through logs to debug issues
- Start and control processes
- Send input to interactive applications

## Key Concepts

### Sessions
- A **session** represents a monitored process or log source
- Each session has a unique **label** that identifies it (e.g., "backend-api", "test-runner")
- Sessions can be in different states: running, stopped, crashed, restarting
- Sessions automatically capture stdout and stderr output

### Log Buffers
- Each session maintains a ring buffer of recent logs (5 minutes or 5MB)
- Logs include timestamps, content, and stream type (stdout/stderr)
- Older logs are automatically removed when limits are reached

## Common Workflows

### 1. Debugging a Running Process
```
1. Use list_sessions to see all active sessions
2. Use get_logs with the session label to view recent output
3. Use get_logs with pattern to search for errors (e.g., pattern: "error|fail|exception")
4. Use control_process to restart if needed
```

### 2. Starting and Monitoring a Service
```
1. Use start_process to launch the service with a descriptive label
2. Use get_logs to monitor startup logs
3. Use send_stdin if the process needs input
4. Use control_process with action: "signal" and signal: "SIGTERM" for graceful shutdown
```

### 3. Analyzing Multiple Services
```
1. Use list_sessions to see all services
2. Use get_logs with multiple labels to correlate logs across services
3. Use pattern matching to find related events
```

## Response Examples

### list_sessions Response
```json
{
  "success": true,
  "data": {
    "sessions": [
      {
        "label": "backend-api",
        "status": "running",
        "pid": 12345,
        "command": "python app.py",
        "working_dir": "/home/user/project",
        "start_time": "2024-01-15T10:30:00Z",
        "exit_time": null,
        "log_count": 150,
        "buffer_size": "2.3MB",
        "exit_code": null,
        "runner_mode": "managed",
        "capabilities": ["process_control", "stdin"]
      }
    ]
  },
  "meta": {
    "total_count": 1,
    "active_count": 1
  }
}
```

### get_logs Response
```json
{
  "success": true,
  "data": {
    "logs": [
      {
        "label": "backend-api",
        "content": "Server started on port 8080",
        "timestamp": "2024-01-15T10:30:15.123Z",
        "stream": "stdout",
        "pid": 12345
      },
      {
        "label": "backend-api",
        "content": "ERROR: Database connection failed",
        "timestamp": "2024-01-15T10:30:16.456Z",
        "stream": "stderr",
        "pid": 12345
      }
    ]
  },
  "meta": {
    "total_results": 2,
    "truncated": false,
    "sessions_queried": ["backend-api"],
    "sessions_not_found": [],
    "time_range": {
      "oldest": "2024-01-15T10:30:15.123Z",
      "newest": "2024-01-15T10:30:16.456Z"
    }
  }
}
```

### Field Explanations

#### Session Fields
- `label`: Unique identifier for the session
- `status`: Current state (running/stopped/crashed/restarting)
- `pid`: Process ID (null for non-process sessions)
- `command`: The command that was executed
- `working_dir`: Directory where the process runs
- `exit_code`: Process exit code (0 = success, non-zero = error)
- `log_count`: Number of log entries in buffer
- `buffer_size`: Current size of log buffer
- `runner_mode`: How the session was created (run/forward/managed)
- `capabilities`: What operations are supported

#### Log Entry Fields
- `label`: Session this log belongs to
- `content`: The actual log message
- `timestamp`: When the log was generated
- `stream`: stdout (normal output) or stderr (errors)
- `pid`: Process that generated the log

## Best Practices

1. **Always start with list_sessions** to understand what's available
2. **Use descriptive labels** when starting processes for easy identification
3. **Filter logs by stream** to separate errors (stderr) from normal output (stdout)
4. **Use regex patterns** to search for specific issues across large log volumes
5. **Check session status** before sending control commands
6. **Include newlines in stdin** when processes expect them (e.g., "yes\n")

## Error Handling

Common error responses:
- Session not found: The label doesn't exist or session was removed
- Process not running: Can't control or send stdin to stopped processes
- Invalid signal: Only specific Unix signals are supported
- Connection lost: The monitored process lost connection to the server

## Advanced Usage

### Monitoring Multiple Services
Query multiple sessions simultaneously:
```json
{
  "labels": ["frontend", "backend", "database"],
  "pattern": "error|exception",
  "stream": "stderr",
  "lines": 50
}
```

### Time-based Log Retrieval
Get logs after a specific time:
```json
{
  "labels": ["backend-api"],
  "since": "2024-01-15T10:00:00Z",
  "max_results": 500
}
```

### Process Restart Policies
- `never`: Process won't restart automatically
- `on-failure`: Restart only if process crashes (non-zero exit)
- `always`: Restart whenever process stops