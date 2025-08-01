package protocol

import (
	"encoding/json"
	"fmt"
	"time"
)

// RunnerMode represents the mode in which a session was created
type RunnerMode string

const (
	ModeRun     RunnerMode = "run"     // logmcp run <command>
	ModeForward RunnerMode = "forward" // logmcp forward <source>
	ModeManaged RunnerMode = "managed" // Started via MCP start_process
)

// MCP Tool Definitions

// MCPTool represents a tool available through the MCP interface
type MCPTool struct {
	Name        string      `json:"name"`
	Description string      `json:"description"`
	InputSchema interface{} `json:"inputSchema"`
}

// GetMCPTools returns all available MCP tools
func GetMCPTools() []MCPTool {
	return []MCPTool{
		{
			Name:        "list_sessions",
			Description: "List all active log sessions to see what processes are being monitored. Returns session labels, status (running/stopped/crashed), process information, and buffer statistics. Always use this first to discover available sessions before retrieving logs.",
			InputSchema: map[string]interface{}{
				"type":       "object",
				"properties": map[string]interface{}{},
			},
		},
		{
			Name:        "get_logs",
			Description: "Retrieve and search log entries from one or more sessions. Use this to debug issues, monitor output, or search for specific patterns. Returns log entries with timestamps, content, and stream type (stdout/stderr). Always call list_sessions first to get valid session labels.",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"labels": map[string]interface{}{
						"type":        "array",
						"items":       map[string]interface{}{"type": "string"},
						"description": "Session labels to query. Get these from list_sessions. Can query multiple sessions at once.",
					},
					"lines": map[string]interface{}{
						"type":        "number",
						"default":     100,
						"description": "Number of log lines to return per session. Use larger values to see more history.",
					},
					"since": map[string]interface{}{
						"type":        "string",
						"description": "ISO timestamp to get logs after this time. Example: '2024-01-15T10:30:00Z'",
					},
					"stream": map[string]interface{}{
						"type":        "string",
						"enum":        []string{"stdout", "stderr", "both"},
						"default":     "both",
						"description": "Filter by stream type. Use 'stderr' to see only errors, 'stdout' for regular output, or 'both' for everything.",
					},
					"pattern": map[string]interface{}{
						"type":        "string",
						"description": "Regex pattern to search for in log content. Example: 'error|fail|exception' to find errors.",
					},
					"max_results": map[string]interface{}{
						"type":        "number",
						"default":     1000,
						"description": "Maximum total results across all queried sessions. Prevents overwhelming responses.",
					},
				},
				"required": []string{"labels"},
			},
		},
		{
			Name:        "start_process",
			Description: "Launch a new managed process that LogMCP will monitor and control. The process output is automatically captured and available via get_logs. Use this to start services, run scripts, or execute any command that you need to monitor. The process can be controlled later with control_process.",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"command": map[string]interface{}{
						"type":        "string",
						"description": "The executable command to run. Examples: 'python', 'node', 'npm', './script.sh'",
					},
					"arguments": map[string]interface{}{
						"type":        "array",
						"description": "Array of command arguments. Example: ['server.py', '--port', '8080'] for 'python server.py --port 8080'",
						"items": map[string]interface{}{
							"type": "string",
						},
					},
					"label": map[string]interface{}{
						"type":        "string",
						"description": "Unique identifier for this process session. Use descriptive names like 'backend-api' or 'test-runner'. This label is used in other commands.",
					},
					"working_dir": map[string]interface{}{
						"type":        "string",
						"description": "Directory to run the process in. Defaults to current directory. Use absolute paths like '/home/user/project'",
					},
					"environment": map[string]interface{}{
						"type":        "object",
						"description": "Additional environment variables for the process. Example: {'NODE_ENV': 'production', 'PORT': '3000'}",
						"additionalProperties": map[string]interface{}{
							"type": "string",
						},
					},
					"restart_policy": map[string]interface{}{
						"type":        "string",
						"enum":        []string{"never", "on-failure", "always"},
						"default":     "never",
						"description": "Automatic restart behavior. 'never': don't restart, 'on-failure': restart if process crashes, 'always': restart whenever it stops",
					},
					"collect_startup_logs": map[string]interface{}{
						"type":        "boolean",
						"default":     true,
						"description": "Whether to capture and store logs from process startup. Set to false for very noisy processes.",
					},
				},
				"required": []string{"command", "label"},
			},
		},
		{
			Name:        "control_process",
			Description: "Send control commands to a managed process. Use this to restart services, send signals for graceful shutdown, or force-kill unresponsive processes. Only works with processes started via start_process or 'logmcp run' command.",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"label": map[string]interface{}{
						"type":        "string",
						"description": "The session label of the process to control. Get this from list_sessions.",
					},
					"action": map[string]interface{}{
						"type":        "string",
						"enum":        []string{"restart", "signal"},
						"description": "Action to perform. 'restart': stop and start the process, 'signal': send a specific Unix signal",
					},
					"signal": map[string]interface{}{
						"type":        "string",
						"enum":        []string{"SIGTERM", "SIGKILL", "SIGINT", "SIGHUP", "SIGUSR1", "SIGUSR2"},
						"description": "Unix signal to send (only for 'signal' action). SIGTERM: graceful shutdown, SIGKILL: force kill, SIGINT: interrupt (Ctrl+C), SIGHUP: reload config",
					},
				},
				"required": []string{"label", "action"},
			},
		},
		{
			Name:        "send_stdin",
			Description: "Send input to a process's stdin stream for interactive commands. Use this to provide input to running processes that are waiting for user input, send commands to REPLs, or interact with command-line applications. The input is sent exactly as provided.",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"label": map[string]interface{}{
						"type":        "string",
						"description": "The session label of the process to send input to. Process must be running and support stdin.",
					},
					"input": map[string]interface{}{
						"type":        "string",
						"description": "Text to send to the process stdin. Include newlines (\\n) if the process expects them. Example: 'yes\\n' to confirm a prompt.",
					},
				},
				"required": []string{"label", "input"},
			},
		},
	}
}

// MCP Request/Response Structures

// ListSessionsRequest represents a request to list sessions
type ListSessionsRequest struct{}

// ListSessionsResponse represents the response from list_sessions
type ListSessionsResponse struct {
	Success bool `json:"success"`
	Data    struct {
		Sessions []SessionInfo `json:"sessions"`
	} `json:"data"`
	Meta struct {
		TotalCount  int `json:"total_count"`
		ActiveCount int `json:"active_count"`
	} `json:"meta"`
}

// SessionInfo represents information about a session
type SessionInfo struct {
	Label        string                 `json:"label"`
	Status       SessionStatus          `json:"status"`
	PID          *int                   `json:"pid"`
	Command      string                 `json:"command"`
	WorkingDir   string                 `json:"working_dir"`
	StartTime    time.Time              `json:"start_time"`
	ExitTime     *time.Time             `json:"exit_time"`
	LogCount     int                    `json:"log_count"`
	BufferSize   string                 `json:"buffer_size"`
	ExitCode     *int                   `json:"exit_code"`
	RunnerMode   RunnerMode             `json:"runner_mode"`
	RunnerArgs   map[string]interface{} `json:"runner_args"`
	Capabilities []string               `json:"capabilities,omitempty"`
}

// GetLogsRequest represents a request to get logs
type GetLogsRequest struct {
	Labels     []string `json:"labels" validate:"required"`
	Lines      *int     `json:"lines,omitempty"`
	Since      *string  `json:"since,omitempty"`
	Stream     *string  `json:"stream,omitempty"`
	Pattern    *string  `json:"pattern,omitempty"`
	MaxResults *int     `json:"max_results,omitempty"`
}

// GetLogsResponse represents the response from get_logs
type GetLogsResponse struct {
	Success bool `json:"success"`
	Data    struct {
		Logs []LogEntry `json:"logs"`
	} `json:"data"`
	Meta struct {
		TotalResults     int        `json:"total_results"`
		Truncated        bool       `json:"truncated"`
		SessionsQueried  []string   `json:"sessions_queried"`
		SessionsNotFound []string   `json:"sessions_not_found"`
		TimeRange        *TimeRange `json:"time_range,omitempty"`
	} `json:"meta"`
}

// LogEntry represents a single log entry
type LogEntry struct {
	Label     string     `json:"label"`
	Content   string     `json:"content"`
	Timestamp time.Time  `json:"timestamp"`
	Stream    StreamType `json:"stream"`
	PID       int        `json:"pid"`
}

// TimeRange represents a time range for log queries
type TimeRange struct {
	Oldest time.Time `json:"oldest"`
	Newest time.Time `json:"newest"`
}

// StartProcessRequest represents a request to start a process
type StartProcessRequest struct {
	Command            string            `json:"command" validate:"required"`
	Arguments          []string          `json:"arguments,omitempty"`
	Label              string            `json:"label" validate:"required"`
	WorkingDir         *string           `json:"working_dir,omitempty"`
	Environment        map[string]string `json:"environment,omitempty"`
	RestartPolicy      *string           `json:"restart_policy,omitempty"`
	CollectStartupLogs *bool             `json:"collect_startup_logs,omitempty"`
}

// StartProcessResponse represents the response from start_process
type StartProcessResponse struct {
	Success bool `json:"success"`
	Data    struct {
		Message string      `json:"message"`
		Session SessionInfo `json:"session"`
	} `json:"data"`
}

// ControlProcessRequest represents a request to control a process
type ControlProcessRequest struct {
	Label  string  `json:"label" validate:"required"`
	Action string  `json:"action" validate:"required,oneof=restart signal"`
	Signal *string `json:"signal,omitempty"`
}

// ControlProcessResponse represents the response from control_process
type ControlProcessResponse struct {
	Success bool `json:"success"`
	Data    struct {
		Message string      `json:"message"`
		Session SessionInfo `json:"session"`
	} `json:"data"`
}

// SendStdinRequest represents a request to send stdin
type SendStdinRequest struct {
	Label string `json:"label" validate:"required"`
	Input string `json:"input" validate:"required"`
}

// SendStdinResponse represents the response from send_stdin
type SendStdinResponse struct {
	Success bool `json:"success"`
	Data    struct {
		Message   string `json:"message"`
		BytesSent int    `json:"bytes_sent"`
	} `json:"data"`
}

// GetHelpResponse represents the response from get_help
type GetHelpResponse struct {
	Success bool `json:"success"`
	Data    struct {
		Content string `json:"content"`
	} `json:"data"`
}

// MCPErrorResponse represents an error response from MCP tools
type MCPErrorResponse struct {
	Success bool   `json:"success"`
	Error   string `json:"error"`
	Code    string `json:"code,omitempty"`
}

// Runner argument structures for different modes

// RunArgs represents arguments for run mode
type RunArgs struct {
	Command string `json:"command"`
	Label   string `json:"label"`
}

// ForwardArgs represents arguments for forward mode
type ForwardArgs struct {
	Source string `json:"source"`
	Label  string `json:"label"`
}

// ManagedArgs represents arguments for managed mode
type ManagedArgs struct {
	Command       string            `json:"command"`
	Arguments     []string          `json:"arguments"`
	Label         string            `json:"label"`
	WorkingDir    string            `json:"working_dir"`
	Environment   map[string]string `json:"environment"`
	RestartPolicy string            `json:"restart_policy,omitempty"`
}

// Helper functions for creating responses

// NewListSessionsResponse creates a new list sessions response
func NewListSessionsResponse(sessions []SessionInfo) *ListSessionsResponse {
	response := &ListSessionsResponse{
		Success: true,
	}
	response.Data.Sessions = sessions
	response.Meta.TotalCount = len(sessions)

	activeCount := 0
	for _, session := range sessions {
		if session.Status == StatusRunning {
			activeCount++
		}
	}
	response.Meta.ActiveCount = activeCount

	return response
}

// NewGetLogsResponse creates a new get logs response
func NewGetLogsResponse(logs []LogEntry, queriedSessions, notFoundSessions []string, truncated bool) *GetLogsResponse {
	response := &GetLogsResponse{
		Success: true,
	}
	response.Data.Logs = logs
	response.Meta.TotalResults = len(logs)
	response.Meta.Truncated = truncated
	response.Meta.SessionsQueried = queriedSessions
	response.Meta.SessionsNotFound = notFoundSessions

	if len(logs) > 0 {
		oldest := logs[0].Timestamp
		newest := logs[0].Timestamp

		for _, log := range logs {
			if log.Timestamp.Before(oldest) {
				oldest = log.Timestamp
			}
			if log.Timestamp.After(newest) {
				newest = log.Timestamp
			}
		}

		response.Meta.TimeRange = &TimeRange{
			Oldest: oldest,
			Newest: newest,
		}
	}

	return response
}

// NewStartProcessResponse creates a new start process response
func NewStartProcessResponse(message string, session SessionInfo) *StartProcessResponse {
	return &StartProcessResponse{
		Success: true,
		Data: struct {
			Message string      `json:"message"`
			Session SessionInfo `json:"session"`
		}{
			Message: message,
			Session: session,
		},
	}
}

// NewControlProcessResponse creates a new control process response
func NewControlProcessResponse(message string, session SessionInfo) *ControlProcessResponse {
	return &ControlProcessResponse{
		Success: true,
		Data: struct {
			Message string      `json:"message"`
			Session SessionInfo `json:"session"`
		}{
			Message: message,
			Session: session,
		},
	}
}

// NewSendStdinResponse creates a new send stdin response
func NewSendStdinResponse(message string, bytesSent int) *SendStdinResponse {
	return &SendStdinResponse{
		Success: true,
		Data: struct {
			Message   string `json:"message"`
			BytesSent int    `json:"bytes_sent"`
		}{
			Message:   message,
			BytesSent: bytesSent,
		},
	}
}

// NewMCPErrorResponse creates a new MCP error response
func NewMCPErrorResponse(error, code string) *MCPErrorResponse {
	return &MCPErrorResponse{
		Success: false,
		Error:   error,
		Code:    code,
	}
}

// Utility functions for JSON handling

// ParseMCPRequest parses a JSON request into the appropriate struct
func ParseMCPRequest(toolName string, data []byte) (interface{}, error) {
	switch toolName {
	case "list_sessions":
		var req ListSessionsRequest
		if err := json.Unmarshal(data, &req); err != nil {
			return nil, err
		}
		return &req, nil

	case "get_logs":
		var req GetLogsRequest
		if err := json.Unmarshal(data, &req); err != nil {
			return nil, err
		}
		return &req, nil

	case "start_process":
		var req StartProcessRequest
		if err := json.Unmarshal(data, &req); err != nil {
			return nil, err
		}
		return &req, nil

	case "control_process":
		var req ControlProcessRequest
		if err := json.Unmarshal(data, &req); err != nil {
			return nil, err
		}
		return &req, nil

	case "send_stdin":
		var req SendStdinRequest
		if err := json.Unmarshal(data, &req); err != nil {
			return nil, err
		}
		return &req, nil

	default:
		return nil, json.Unmarshal(data, &map[string]interface{}{})
	}
}

// SerializeMCPResponse serializes an MCP response to JSON
func SerializeMCPResponse(response interface{}) ([]byte, error) {
	return json.Marshal(response)
}

// ValidateMCPRequest performs validation on MCP requests
func ValidateMCPRequest(toolName string, req interface{}) error {
	switch toolName {
	case "get_logs":
		if r, ok := req.(*GetLogsRequest); ok {
			if len(r.Labels) == 0 {
				return fmt.Errorf("labels is required and cannot be empty")
			}
			if r.Stream != nil && *r.Stream != "stdout" && *r.Stream != "stderr" && *r.Stream != "both" {
				return fmt.Errorf("stream must be stdout, stderr, or both")
			}
		}

	case "start_process":
		if r, ok := req.(*StartProcessRequest); ok {
			if r.Command == "" {
				return fmt.Errorf("command is required")
			}
			if r.Label == "" {
				return fmt.Errorf("label is required")
			}
			if r.RestartPolicy != nil {
				if *r.RestartPolicy != "never" && *r.RestartPolicy != "on-failure" && *r.RestartPolicy != "always" {
					return fmt.Errorf("restart_policy must be never, on-failure, or always")
				}
			}
		}

	case "control_process":
		if r, ok := req.(*ControlProcessRequest); ok {
			if r.Label == "" {
				return fmt.Errorf("label is required")
			}
			if r.Action == "" {
				return fmt.Errorf("action is required")
			}
			if r.Action != "restart" && r.Action != "signal" {
				return fmt.Errorf("action must be restart or signal")
			}
			if r.Action == "signal" && r.Signal == nil {
				return fmt.Errorf("signal is required when action is signal")
			}
			if r.Signal != nil {
				validSignals := []string{"SIGTERM", "SIGKILL", "SIGINT", "SIGHUP", "SIGUSR1", "SIGUSR2"}
				valid := false
				for _, sig := range validSignals {
					if *r.Signal == sig {
						valid = true
						break
					}
				}
				if !valid {
					return fmt.Errorf("invalid signal: %s", *r.Signal)
				}
			}
		}

	case "send_stdin":
		if r, ok := req.(*SendStdinRequest); ok {
			if r.Label == "" {
				return fmt.Errorf("label is required")
			}
			if r.Input == "" {
				return fmt.Errorf("input is required")
			}
		}
	}

	return nil
}
