package server

// MCPSystemContext provides context information for LLMs using LogMCP
const MCPSystemContext = `
# LogMCP - Log Monitoring and Process Control

LogMCP enables you to monitor processes, search logs, and control running applications through the Model Context Protocol.

## Quick Start
1. Use 'list_sessions' to see available processes
2. Use 'get_logs' with a session label to view output
3. Use 'start_process' to launch new monitored processes
4. Use 'control_process' to restart or send signals

## Key Concepts
- **Session**: A monitored process identified by a unique label
- **Label**: Use descriptive names like "backend-api" or "test-runner"
- **Streams**: stdout (normal output) vs stderr (errors)
- **Buffer**: Last 5 minutes or 5MB of logs per session

## Common Patterns
- Debug errors: get_logs with pattern "error|fail|exception" and stream "stderr"
- Monitor startup: start_process then immediately get_logs
- Restart crashed service: control_process with action "restart"
- Graceful shutdown: control_process with action "signal" and signal "SIGTERM"

## Tips
- Always check list_sessions first to get valid labels
- Use regex patterns to search across large log volumes
- Send newlines (\n) with stdin input when expected
- Session labels are reused if the previous session disconnected
`

// GetMCPContext returns the system context for LLMs
func GetMCPContext() string {
	return MCPSystemContext
}
