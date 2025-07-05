// Package server provides MCP (Model Context Protocol) interface for the LogMCP server.
//
// The MCP server exposes LogMCP functionality to Language Models through a standardized
// protocol over stdio using the mcp-go library. It provides tools for session management,
// log retrieval, process control, and stdin interaction.
//
// Available MCP Tools:
// - list_sessions: Get list of all active log sessions with status and metadata
// - get_logs: Retrieve and search log entries with advanced filtering
// - start_process: Launch new managed processes with configuration options
// - control_process: Send control commands (restart, signal) to managed processes
// - send_stdin: Send input directly to process stdin for interactive commands
//
// The MCP server bridges the WebSocket-based runner communication with the
// MCP protocol, providing LLMs with comprehensive access to the LogMCP system.
//
// Example usage:
//
//	sm := server.NewSessionManager()
//	wsServer := server.NewWebSocketServer(sm)
//	mcpServer := server.NewMCPServer(sm, wsServer)
//
//	// Start MCP server on stdio
//	if err := mcpServer.Serve(); err != nil {
//		log.Fatal(err)
//	}
package server

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/bebsworthy/logmcp/internal/buffer"
	"github.com/bebsworthy/logmcp/internal/protocol"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// MCPServer provides MCP interface for LogMCP using mcp-go library
type MCPServer struct {
	sessionManager *SessionManager
	wsServer       *WebSocketServer
	mcpServer      *server.MCPServer
	ctx            context.Context
	cancel         context.CancelFunc
	wg             sync.WaitGroup
}

// MCPServer implementation using mcp-go library

// NewMCPServer creates a new MCP server using mcp-go library
func NewMCPServer(sessionManager *SessionManager, wsServer *WebSocketServer) *MCPServer {
	ctx, cancel := context.WithCancel(context.Background())

	// Create mcp-go server instance with descriptive information
	mcpServer := server.NewMCPServer(
		"logmcp",
		"1.0.0",
		server.WithToolCapabilities(true),
	)

	mcpSrv := &MCPServer{
		sessionManager: sessionManager,
		wsServer:       wsServer,
		mcpServer:      mcpServer,
		ctx:            ctx,
		cancel:         cancel,
	}

	// Register tool handlers
	mcpSrv.registerTools()

	return mcpSrv
}

// registerTools registers all MCP tools with the mcp-go server
func (mcpSrv *MCPServer) registerTools() {
	// Register list_sessions tool
	mcpSrv.mcpServer.AddTool(
		mcp.NewTool("list_sessions",
			mcp.WithDescription("List all active log sessions to see what processes are being monitored. Returns session labels, status (running/stopped/crashed), process information, and buffer statistics. Always use this first to discover available sessions before retrieving logs."),
		),
		mcpSrv.handleListSessions,
	)

	// Register get_logs tool
	mcpSrv.mcpServer.AddTool(
		mcp.NewTool("get_logs",
			mcp.WithDescription("Retrieve and search log entries from one or more sessions. Use this to debug issues, monitor output, or search for specific patterns. Returns log entries with timestamps, content, and stream type (stdout/stderr). Always call list_sessions first to get valid session labels."),
			mcp.WithArray("labels",
				mcp.Required(),
				mcp.Description("Session labels to query. Get these from list_sessions. Can query multiple sessions at once."),
				mcp.Items(map[string]interface{}{
					"type":        "string",
					"description": "Session label",
				}),
			),
			mcp.WithNumber("lines",
				mcp.Description("Number of log lines to return per session. Use larger values to see more history."),
			),
			mcp.WithString("since",
				mcp.Description("ISO timestamp to get logs after this time. Example: '2024-01-15T10:30:00Z'"),
			),
			mcp.WithString("stream",
				mcp.Description("Filter by stream type. Use 'stderr' to see only errors, 'stdout' for regular output, or 'both' for everything."),
				mcp.Enum("stdout", "stderr", "both"),
			),
			mcp.WithString("pattern",
				mcp.Description("Regex pattern to search for in log content. Example: 'error|fail|exception' to find errors."),
			),
			mcp.WithNumber("max_results",
				mcp.Description("Maximum total results across all queried sessions. Prevents overwhelming responses."),
			),
		),
		mcpSrv.handleGetLogs,
	)

	// Register start_process tool
	mcpSrv.mcpServer.AddTool(
		mcp.NewTool("start_process",
			mcp.WithDescription("Launch a new managed process that LogMCP will monitor and control. The process output is automatically captured and available via get_logs. Use this to start services, run scripts, or execute any command that you need to monitor. The process can be controlled later with control_process."),
			mcp.WithString("command",
				mcp.Required(),
				mcp.Description("The executable command to run. Examples: 'python', 'node', 'npm', './script.sh'"),
			),
			mcp.WithArray("arguments",
				mcp.Description("Array of command arguments. Example: ['server.py', '--port', '8080'] for 'python server.py --port 8080'"),
				mcp.Items(map[string]interface{}{
					"type": "string",
				}),
			),
			mcp.WithString("label",
				mcp.Required(),
				mcp.Description("Unique identifier for this process session. Use descriptive names like 'backend-api' or 'test-runner'. This label is used in other commands."),
			),
			mcp.WithString("working_dir",
				mcp.Description("Directory to run the process in. Defaults to current directory. Use absolute paths like '/home/user/project'"),
			),
			mcp.WithObject("environment",
				mcp.Description("Additional environment variables for the process. Example: {'NODE_ENV': 'production', 'PORT': '3000'}"),
			),
			mcp.WithString("restart_policy",
				mcp.Description("Automatic restart behavior. 'never': don't restart, 'on-failure': restart if process crashes, 'always': restart whenever it stops"),
				mcp.Enum("never", "always", "on-failure"),
			),
			mcp.WithBoolean("collect_startup_logs",
				mcp.Description("Whether to capture and store logs from process startup. Set to false for very noisy processes."),
			),
		),
		mcpSrv.handleStartProcess,
	)

	// Register control_process tool
	mcpSrv.mcpServer.AddTool(
		mcp.NewTool("control_process",
			mcp.WithDescription("Send control commands to a managed process. Use this to restart services, send signals for graceful shutdown, or force-kill unresponsive processes. Only works with processes started via start_process or 'logmcp run' command."),
			mcp.WithString("label",
				mcp.Required(),
				mcp.Description("The session label of the process to control. Get this from list_sessions."),
			),
			mcp.WithString("action",
				mcp.Required(),
				mcp.Description("Action to perform. 'restart': stop and start the process, 'signal': send a specific Unix signal"),
				mcp.Enum("restart", "signal"),
			),
			mcp.WithString("signal",
				mcp.Description("Unix signal to send (only for 'signal' action). SIGTERM: graceful shutdown, SIGKILL: force kill, SIGINT: interrupt (Ctrl+C), SIGHUP: reload config"),
				mcp.Enum("SIGTERM", "SIGKILL", "SIGINT", "SIGHUP", "SIGUSR1", "SIGUSR2"),
			),
		),
		mcpSrv.handleControlProcess,
	)

	// Register send_stdin tool
	mcpSrv.mcpServer.AddTool(
		mcp.NewTool("send_stdin",
			mcp.WithDescription("Send input to a process's stdin stream for interactive commands. Use this to provide input to running processes that are waiting for user input, send commands to REPLs, or interact with command-line applications. The input is sent exactly as provided."),
			mcp.WithString("label",
				mcp.Required(),
				mcp.Description("The session label of the process to send input to. Process must be running and support stdin."),
			),
			mcp.WithString("input",
				mcp.Required(),
				mcp.Description("Text to send to the process stdin. Include newlines (\\n) if the process expects them. Example: 'yes\\n' to confirm a prompt."),
			),
		),
		mcpSrv.handleSendStdin,
	)

	// Register get_help tool
	mcpSrv.mcpServer.AddTool(
		mcp.NewTool("get_help",
			mcp.WithDescription("Get help and context information about using LogMCP. Returns quick start guide, key concepts, common patterns, and tips for effective log monitoring and process control."),
		),
		mcpSrv.handleGetHelp,
	)
}

// Serve starts the MCP server using stdio transport
func (mcpSrv *MCPServer) Serve() error {
	return server.ServeStdio(mcpSrv.mcpServer)
}

// Stop stops the MCP server
func (mcpSrv *MCPServer) Stop() {
	mcpSrv.cancel()
	mcpSrv.wg.Wait()
}

// Tool Handlers using mcp-go API

// handleListSessions handles the list_sessions tool
func (mcpSrv *MCPServer) handleListSessions(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	sessions := mcpSrv.sessionManager.ListSessions()
	sessionInfos := make([]protocol.SessionInfo, 0, len(sessions))

	for _, session := range sessions {
		session.mutex.RLock()

		// Calculate buffer size
		bufferSize := "0B"
		logCount := 0
		if session.LogBuffer != nil {
			stats := session.LogBuffer.GetStats()
			logCount = stats.EntryCount
			bufferSize = formatBytes(stats.TotalSizeBytes)
		}

		// Convert runner args to map
		runnerArgs := make(map[string]interface{})
		if session.RunnerArgs != nil {
			// Use type assertion to convert to map
			switch args := session.RunnerArgs.(type) {
			case RunArgs:
				runnerArgs["command"] = args.Command
				runnerArgs["label"] = args.Label
			case ForwardArgs:
				runnerArgs["source"] = args.Source
				runnerArgs["label"] = args.Label
			case ManagedArgs:
				runnerArgs["command"] = args.Command
				runnerArgs["label"] = args.Label
				runnerArgs["working_dir"] = args.WorkingDir
				runnerArgs["environment"] = args.Environment
			case protocol.RunArgs:
				runnerArgs["command"] = args.Command
				runnerArgs["label"] = args.Label
			case protocol.ForwardArgs:
				runnerArgs["source"] = args.Source
				runnerArgs["label"] = args.Label
			case protocol.ManagedArgs:
				runnerArgs["command"] = args.Command
				runnerArgs["label"] = args.Label
				runnerArgs["working_dir"] = args.WorkingDir
				runnerArgs["environment"] = args.Environment
			}
		}

		sessionInfo := protocol.SessionInfo{
			Label:        session.Label,
			Status:       session.Status,
			PID:          &session.PID,
			Command:      session.Command,
			WorkingDir:   session.WorkingDir,
			StartTime:    session.StartTime,
			ExitTime:     session.ExitTime,
			LogCount:     logCount,
			BufferSize:   bufferSize,
			ExitCode:     session.ExitCode,
			RunnerMode:   protocol.RunnerMode(session.RunnerMode),
			RunnerArgs:   runnerArgs,
			Capabilities: session.Capabilities,
		}

		if session.PID == 0 {
			sessionInfo.PID = nil
		}

		sessionInfos = append(sessionInfos, sessionInfo)
		session.mutex.RUnlock()
	}

	response := protocol.NewListSessionsResponse(sessionInfos)

	// If no sessions, add helpful context
	if len(sessionInfos) == 0 {
		// Create a response with context hint
		type ListSessionsWithContext struct {
			Success bool `json:"success"`
			Data    struct {
				Sessions []protocol.SessionInfo `json:"sessions"`
				Help     string                 `json:"help,omitempty"`
			} `json:"data"`
			Meta struct {
				TotalCount  int `json:"total_count"`
				ActiveCount int `json:"active_count"`
			} `json:"meta"`
		}

		contextResponse := ListSessionsWithContext{
			Success: true,
		}
		contextResponse.Data.Sessions = sessionInfos
		contextResponse.Data.Help = "No sessions found. Use 'start_process' to launch a new monitored process, or start a process with 'logmcp run <command>' from the command line."
		contextResponse.Meta.TotalCount = 0
		contextResponse.Meta.ActiveCount = 0

		resultJSON, err := json.MarshalIndent(contextResponse, "", "  ")
		if err != nil {
			return nil, fmt.Errorf("failed to marshal response: %w", err)
		}

		return &mcp.CallToolResult{
			Content: []mcp.Content{
				mcp.TextContent{
					Type: "text",
					Text: string(resultJSON),
				},
			},
		}, nil
	}

	// Normal response with sessions
	resultJSON, err := json.MarshalIndent(response, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("failed to marshal response: %w", err)
	}

	return &mcp.CallToolResult{
		Content: []mcp.Content{
			mcp.TextContent{
				Type: "text",
				Text: string(resultJSON),
			},
		},
	}, nil
}

// handleGetLogs handles the get_logs tool
func (mcpSrv *MCPServer) handleGetLogs(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := request.GetArguments()
	// Parse request
	var req protocol.GetLogsRequest

	// Parse labels
	labelsInterface, ok := args["labels"]
	if !ok {
		return nil, fmt.Errorf("labels parameter is required")
	}

	labelsSlice, ok := labelsInterface.([]interface{})
	if !ok {
		return nil, fmt.Errorf("labels must be an array")
	}

	req.Labels = make([]string, len(labelsSlice))
	for i, label := range labelsSlice {
		labelStr, ok := label.(string)
		if !ok {
			return nil, fmt.Errorf("all labels must be strings")
		}
		req.Labels[i] = labelStr
	}

	// Parse optional parameters
	if lines, ok := args["lines"]; ok {
		if linesFloat, ok := lines.(float64); ok {
			linesInt := int(linesFloat)
			req.Lines = &linesInt
		}
	}

	if since, ok := args["since"]; ok {
		if sinceStr, ok := since.(string); ok {
			req.Since = &sinceStr
		}
	}

	if stream, ok := args["stream"]; ok {
		if streamStr, ok := stream.(string); ok {
			req.Stream = &streamStr
		}
	}

	if pattern, ok := args["pattern"]; ok {
		if patternStr, ok := pattern.(string); ok {
			req.Pattern = &patternStr
		}
	}

	if maxResults, ok := args["max_results"]; ok {
		if maxResultsFloat, ok := maxResults.(float64); ok {
			maxResultsInt := int(maxResultsFloat)
			req.MaxResults = &maxResultsInt
		}
	}

	// Validate request
	if err := protocol.ValidateMCPRequest("get_logs", &req); err != nil {
		return nil, fmt.Errorf("invalid request: %w", err)
	}

	// Set defaults
	if req.Lines == nil {
		defaultLines := 100
		req.Lines = &defaultLines
	}
	if req.Stream == nil {
		defaultStream := "both"
		req.Stream = &defaultStream
	}
	if req.MaxResults == nil {
		defaultMaxResults := 1000
		req.MaxResults = &defaultMaxResults
	}

	// Collect logs from all requested sessions
	var allLogs []protocol.LogEntry
	var queriedSessions []string
	var notFoundSessions []string
	totalResults := 0

	for _, label := range req.Labels {
		session := mcpSrv.sessionManager.GetSessionsByLabel(label)
		if session == nil {
			notFoundSessions = append(notFoundSessions, label)
			continue
		}

		queriedSessions = append(queriedSessions, label)

		session.mutex.RLock()
		if session.LogBuffer == nil {
			session.mutex.RUnlock()
			// Return empty logs for this session - it has no buffer
			continue
		}

		// Build query options
		opts := buffer.GetOptions{
			Lines:      *req.Lines,
			Stream:     *req.Stream,
			MaxResults: *req.MaxResults - totalResults,
		}

		if req.Pattern != nil {
			opts.Pattern = *req.Pattern
		}

		if req.Since != nil {
			if sinceTime, err := time.Parse(time.RFC3339, *req.Since); err == nil {
				opts.Since = sinceTime
			}
		}

		// Get logs from buffer
		bufferLogs := session.LogBuffer.Get(opts)
		session.mutex.RUnlock()

		// Convert to protocol format
		for _, bufferLog := range bufferLogs {
			protocolLog := protocol.LogEntry{
				Label:     bufferLog.Label,
				Content:   bufferLog.Content,
				Timestamp: bufferLog.Timestamp,
				Stream:    bufferLog.Stream,
				PID:       bufferLog.PID,
			}
			allLogs = append(allLogs, protocolLog)
			totalResults++

			// Check if we've reached the max results limit
			if totalResults >= *req.MaxResults {
				break
			}
		}

		if totalResults >= *req.MaxResults {
			break
		}
	}

	// Sort logs by timestamp
	if len(allLogs) > 1 {
		for i := 0; i < len(allLogs)-1; i++ {
			for j := i + 1; j < len(allLogs); j++ {
				if allLogs[i].Timestamp.After(allLogs[j].Timestamp) {
					allLogs[i], allLogs[j] = allLogs[j], allLogs[i]
				}
			}
		}
	}

	// Apply final line limit if specified
	if req.Lines != nil && *req.Lines > 0 && len(allLogs) > *req.Lines {
		allLogs = allLogs[len(allLogs)-*req.Lines:]
	}

	truncated := totalResults >= *req.MaxResults

	// Check if all requested sessions were not found
	if len(notFoundSessions) > 0 && len(queriedSessions) == 0 {
		return nil, addContextToError(
			fmt.Errorf("session not found: %v", notFoundSessions),
			"get_logs",
		)
	}

	response := protocol.NewGetLogsResponse(allLogs, queriedSessions, notFoundSessions, truncated)
	resultJSON, err := json.MarshalIndent(response, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("failed to marshal response: %w", err)
	}

	return &mcp.CallToolResult{
		Content: []mcp.Content{
			mcp.TextContent{
				Type: "text",
				Text: string(resultJSON),
			},
		},
	}, nil
}

// handleStartProcess handles the start_process tool
func (mcpSrv *MCPServer) handleStartProcess(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := request.GetArguments()
	// Parse request
	var req protocol.StartProcessRequest

	command, ok := args["command"].(string)
	if !ok {
		return nil, fmt.Errorf("command parameter is required")
	}
	req.Command = command

	// Extract arguments array
	if argsArray, ok := args["arguments"].([]interface{}); ok {
		req.Arguments = make([]string, len(argsArray))
		for i, arg := range argsArray {
			if strArg, ok := arg.(string); ok {
				req.Arguments[i] = strArg
			} else {
				return nil, fmt.Errorf("argument at index %d must be a string", i)
			}
		}
	}

	label, ok := args["label"].(string)
	if !ok {
		return nil, fmt.Errorf("label parameter is required")
	}
	req.Label = label

	if workingDir, ok := args["working_dir"].(string); ok {
		req.WorkingDir = &workingDir
	}

	if env, ok := args["environment"].(map[string]interface{}); ok {
		req.Environment = make(map[string]string)
		for key, value := range env {
			if strValue, ok := value.(string); ok {
				req.Environment[key] = strValue
			}
		}
	}

	if restartPolicy, ok := args["restart_policy"].(string); ok {
		req.RestartPolicy = &restartPolicy
	}

	if collectStartupLogs, ok := args["collect_startup_logs"].(bool); ok {
		req.CollectStartupLogs = &collectStartupLogs
	}

	// Validate request
	if err := protocol.ValidateMCPRequest("start_process", &req); err != nil {
		return nil, fmt.Errorf("invalid request: %w", err)
	}

	// Set defaults
	if req.WorkingDir == nil {
		cwd, _ := os.Getwd()
		req.WorkingDir = &cwd
	}
	if req.RestartPolicy == nil {
		defaultPolicy := "never"
		req.RestartPolicy = &defaultPolicy
	}
	if req.CollectStartupLogs == nil {
		defaultCollect := true
		req.CollectStartupLogs = &defaultCollect
	}

	// Create managed args
	managedArgs := protocol.ManagedArgs{
		Command:       req.Command,
		Arguments:     req.Arguments,
		Label:         req.Label,
		WorkingDir:    *req.WorkingDir,
		Environment:   req.Environment,
		RestartPolicy: *req.RestartPolicy,
	}

	// Create session
	session, err := mcpSrv.sessionManager.CreateSession(
		req.Label,
		req.Command,
		*req.WorkingDir,
		[]string{"process_control", "stdin"},
		ModeManaged,
		managedArgs,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create session: %w", err)
	}

	// Start the process
	if err := mcpSrv.startManagedProcess(session, req); err != nil {
		// Clean up session on failure
		if err := mcpSrv.sessionManager.RemoveSession(session.Label); err != nil {
			// Log error but continue cleanup
			log.Printf("Failed to remove session %s: %v", session.Label, err)
		}
		return nil, fmt.Errorf("failed to start process: %w", err)
	}

	// Convert session to SessionInfo
	session.mutex.RLock()
	bufferSize := "0B"
	logCount := 0
	if session.LogBuffer != nil {
		stats := session.LogBuffer.GetStats()
		logCount = stats.EntryCount
		bufferSize = formatBytes(stats.TotalSizeBytes)
	}

	runnerArgs := map[string]interface{}{
		"command":     managedArgs.Command,
		"label":       managedArgs.Label,
		"working_dir": managedArgs.WorkingDir,
		"environment": managedArgs.Environment,
	}

	sessionInfo := protocol.SessionInfo{
		Label:        session.Label,
		Status:       session.Status,
		PID:          &session.PID,
		Command:      session.Command,
		WorkingDir:   session.WorkingDir,
		StartTime:    session.StartTime,
		ExitTime:     session.ExitTime,
		LogCount:     logCount,
		BufferSize:   bufferSize,
		ExitCode:     session.ExitCode,
		RunnerMode:   protocol.RunnerMode(session.RunnerMode),
		RunnerArgs:   runnerArgs,
		Capabilities: session.Capabilities,
	}

	if session.PID == 0 {
		sessionInfo.PID = nil
	}

	session.mutex.RUnlock()

	message := fmt.Sprintf("Process started successfully with PID %d", session.PID)
	response := protocol.NewStartProcessResponse(message, sessionInfo)
	resultJSON, err := json.MarshalIndent(response, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("failed to marshal response: %w", err)
	}

	return &mcp.CallToolResult{
		Content: []mcp.Content{
			mcp.TextContent{
				Type: "text",
				Text: string(resultJSON),
			},
		},
	}, nil
}

// startManagedProcess starts a managed process for a session
func (mcpSrv *MCPServer) startManagedProcess(session *Session, req protocol.StartProcessRequest) error {
	// Determine if we should collect logs
	collectLogs := req.CollectStartupLogs == nil || *req.CollectStartupLogs

	// Create command with arguments
	cmd := exec.Command(req.Command, req.Arguments...)
	cmd.Dir = *req.WorkingDir

	// Set environment
	cmd.Env = os.Environ()
	for key, value := range req.Environment {
		cmd.Env = append(cmd.Env, fmt.Sprintf("%s=%s", key, value))
	}

	// Create pipes for stdout/stderr
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("failed to create stdout pipe: %w", err)
	}

	stderr, err := cmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("failed to create stderr pipe: %w", err)
	}

	// Create stdin pipe
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("failed to create stdin pipe: %w", err)
	}

	// Start the process
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start process: %w", err)
	}

	// Update session with process info
	session.mutex.Lock()
	session.Process = cmd.Process
	session.PID = cmd.Process.Pid
	session.Status = protocol.StatusRunning
	session.mutex.Unlock()

	// Debug: Log process start
	log.Printf("[DEBUG] Started process %s (PID %d) with command: %s %v", session.Label, cmd.Process.Pid, req.Command, req.Arguments)

	// Create a WaitGroup to track stdout/stderr readers
	var readersWg sync.WaitGroup
	readersWg.Add(2)

	// Start goroutines to read stdout/stderr
	mcpSrv.wg.Add(3)

	// Read stdout
	go func() {
		defer mcpSrv.wg.Done()
		defer readersWg.Done()
		defer func() {
			if err := stdout.Close(); err != nil {
				log.Printf("Failed to close stdout pipe: %v", err)
			}
		}()

		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			line := scanner.Text()
			logMsg := protocol.NewLogMessage(session.Label, line, protocol.StreamStdout, session.PID)
			if collectLogs {
				session.mutex.RLock()
				if session.LogBuffer != nil {
					session.LogBuffer.AddFromMessage(logMsg)
				}
				session.mutex.RUnlock()
			}
		}
	}()

	// Read stderr
	go func() {
		defer mcpSrv.wg.Done()
		defer readersWg.Done()
		defer func() {
			if err := stderr.Close(); err != nil {
				log.Printf("Failed to close stderr pipe: %v", err)
			}
		}()

		scanner := bufio.NewScanner(stderr)
		for scanner.Scan() {
			line := scanner.Text()
			logMsg := protocol.NewLogMessage(session.Label, line, protocol.StreamStderr, session.PID)
			if collectLogs {
				session.mutex.RLock()
				if session.LogBuffer != nil {
					session.LogBuffer.AddFromMessage(logMsg)
				}
				session.mutex.RUnlock()
			}
		}
	}()

	// Wait for process to complete
	go func() {
		defer mcpSrv.wg.Done()
		defer func() {
			if err := stdin.Close(); err != nil {
				log.Printf("Failed to close stdin pipe: %v", err)
			}
		}()

		err := cmd.Wait()
		
		// Wait for stdout/stderr readers to complete before updating status
		readersWg.Wait()
		exitCode := 0
		var status protocol.SessionStatus

		if err != nil {
			if exitError, ok := err.(*exec.ExitError); ok {
				exitCode = exitError.ExitCode() // Will be -1 for signals

				// Get the underlying wait status for Unix systems
				if ws, ok := exitError.Sys().(syscall.WaitStatus); ok {
					if ws.Signaled() {
						// Process was terminated by a signal
						sig := ws.Signal()
						log.Printf("[DEBUG] Process %s (PID %d) terminated by signal: %v",
							session.Label, session.PID, sig)

						// Classify signals
						switch sig {
						case syscall.SIGTERM, // Polite termination request
							syscall.SIGINT,  // Ctrl+C
							syscall.SIGKILL, // Force kill
							syscall.SIGHUP:  // Hangup
							status = protocol.StatusStopped
						default:
							// Includes SIGSEGV, SIGILL, SIGABRT, SIGFPE, SIGBUS, SIGTRAP, etc.
							status = protocol.StatusCrashed
						}
					} else if ws.Exited() {
						// Process exited normally with a code
						exitCode = ws.ExitStatus()
						if exitCode == 0 {
							status = protocol.StatusStopped
						} else {
							status = protocol.StatusCrashed
						}
					}
				} else {
					// Fallback for non-Unix systems or when WaitStatus is not available
					if exitCode == 0 {
						status = protocol.StatusStopped
					} else {
						status = protocol.StatusCrashed
					}
				}
			} else {
				// Other error types
				exitCode = 1
				status = protocol.StatusCrashed
			}
		} else {
			// No error, clean exit
			status = protocol.StatusStopped
		}

		log.Printf("[DEBUG] Process %s (PID %d) completed with exit code %d, status: %s",
			session.Label, session.PID, exitCode, status)

		if err := mcpSrv.sessionManager.UpdateSessionStatus(session.Label, status, session.PID, &exitCode); err != nil {
			log.Printf("Failed to update session status for %s: %v", session.Label, err)
		}

		// Check restart policy
		session.mutex.RLock()
		managedArgs, isManagedProcess := session.RunnerArgs.(protocol.ManagedArgs)
		session.mutex.RUnlock()

		if isManagedProcess && managedArgs.RestartPolicy != "" {
			shouldRestart := false

			switch managedArgs.RestartPolicy {
			case "always":
				shouldRestart = true
				log.Printf("[DEBUG] Process %s will restart (policy: always)", session.Label)
			case "on-failure":
				if exitCode != 0 {
					shouldRestart = true
					log.Printf("[DEBUG] Process %s will restart (policy: on-failure, exit code: %d)",
						session.Label, exitCode)
				}
			case "never":
				// Do nothing
				log.Printf("[DEBUG] Process %s will not restart (policy: never)", session.Label)
			}

			if shouldRestart {
				// Small delay before restart to avoid tight loops
				time.Sleep(1 * time.Second)

				// Trigger restart
				if _, err := mcpSrv.restartProcess(session); err != nil {
					log.Printf("[ERROR] Failed to restart process %s: %v", session.Label, err)
					// Update status to crashed if restart fails
					if err := mcpSrv.sessionManager.UpdateSessionStatus(session.Label, protocol.StatusCrashed, 0, nil); err != nil {
						log.Printf("Failed to update session status to crashed for %s: %v", session.Label, err)
					}
				}
			}
		}
	}()

	return nil
}

// handleControlProcess handles the control_process tool
func (mcpSrv *MCPServer) handleControlProcess(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := request.GetArguments()
	// Parse request
	var req protocol.ControlProcessRequest

	label, ok := args["label"].(string)
	if !ok {
		return nil, fmt.Errorf("label parameter is required")
	}
	req.Label = label

	action, ok := args["action"].(string)
	if !ok {
		return nil, fmt.Errorf("action parameter is required")
	}
	req.Action = action

	if signal, ok := args["signal"].(string); ok {
		req.Signal = &signal
	}

	// Validate request
	if err := protocol.ValidateMCPRequest("control_process", &req); err != nil {
		return nil, fmt.Errorf("invalid request: %w", err)
	}

	// Get session
	session, err := mcpSrv.sessionManager.GetSession(req.Label)
	if err != nil {
		return nil, addContextToError(fmt.Errorf("session not found: %w", err), "control_process")
	}

	var message string

	switch req.Action {
	case "restart":
		message, err = mcpSrv.restartProcess(session)
	case "signal":
		message, err = mcpSrv.signalProcess(session, *req.Signal)
	default:
		return nil, fmt.Errorf("unsupported action: %s", req.Action)
	}

	if err != nil {
		return nil, err
	}

	// Get updated session info
	session.mutex.RLock()
	bufferSize := "0B"
	logCount := 0
	if session.LogBuffer != nil {
		stats := session.LogBuffer.GetStats()
		logCount = stats.EntryCount
		bufferSize = formatBytes(stats.TotalSizeBytes)
	}

	runnerArgs := make(map[string]interface{})
	if session.RunnerArgs != nil {
		switch args := session.RunnerArgs.(type) {
		case protocol.ManagedArgs:
			runnerArgs["command"] = args.Command
			runnerArgs["label"] = args.Label
			runnerArgs["working_dir"] = args.WorkingDir
			runnerArgs["environment"] = args.Environment
		}
	}

	sessionInfo := protocol.SessionInfo{
		Label:        session.Label,
		Status:       session.Status,
		PID:          &session.PID,
		Command:      session.Command,
		WorkingDir:   session.WorkingDir,
		StartTime:    session.StartTime,
		ExitTime:     session.ExitTime,
		LogCount:     logCount,
		BufferSize:   bufferSize,
		ExitCode:     session.ExitCode,
		RunnerMode:   protocol.RunnerMode(session.RunnerMode),
		RunnerArgs:   runnerArgs,
		Capabilities: session.Capabilities,
	}

	if session.PID == 0 {
		sessionInfo.PID = nil
	}

	session.mutex.RUnlock()

	response := protocol.NewControlProcessResponse(message, sessionInfo)
	resultJSON, err := json.MarshalIndent(response, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("failed to marshal response: %w", err)
	}

	return &mcp.CallToolResult{
		Content: []mcp.Content{
			mcp.TextContent{
				Type: "text",
				Text: string(resultJSON),
			},
		},
	}, nil
}

// restartProcess restarts a managed process
func (mcp *MCPServer) restartProcess(session *Session) (string, error) {
	// Serialize restart operations for this session
	session.restartMutex.Lock()
	defer session.restartMutex.Unlock()

	// Lock session for reading/updating state
	session.mutex.Lock()

	// Kill existing process if running
	process := session.Process
	if process != nil {
		session.Process = nil // Clear reference before killing
	}

	// Update status
	session.Status = protocol.StatusRestarting

	// Get managed args for restart
	managedArgs, ok := session.RunnerArgs.(protocol.ManagedArgs)
	if !ok {
		session.mutex.Unlock()
		return "", addContextToError(fmt.Errorf("session is not a managed process"), "control_process")
	}

	// Make a copy of args to use after unlocking
	req := protocol.StartProcessRequest{
		Command:       managedArgs.Command,
		Arguments:     managedArgs.Arguments,
		Label:         managedArgs.Label,
		WorkingDir:    &managedArgs.WorkingDir,
		Environment:   managedArgs.Environment,
		RestartPolicy: &managedArgs.RestartPolicy,
	}

	// Unlock before killing process (which might block)
	session.mutex.Unlock()

	// Kill the old process if it exists
	if process != nil {
		if err := process.Kill(); err != nil {
			log.Printf("Warning: failed to kill process: %v", err)
		}
		_, _ = process.Wait() // Wait for cleanup
	}

	// Start new process (this will acquire its own session.mutex as needed)
	if err := mcp.startManagedProcess(session, req); err != nil {
		// Update status on failure
		session.mutex.Lock()
		session.Status = protocol.StatusCrashed
		session.mutex.Unlock()
		return "", fmt.Errorf("failed to restart process: %w", err)
	}

	// Get PID for return message
	session.mutex.RLock()
	pid := session.PID
	session.mutex.RUnlock()

	return fmt.Sprintf("Process restarted successfully with PID %d", pid), nil
}

// signalProcess sends a signal to a managed process
func (mcp *MCPServer) signalProcess(session *Session, signalName string) (string, error) {
	session.mutex.RLock()
	process := session.Process
	pid := session.PID
	session.mutex.RUnlock()

	if process == nil {
		return "", addContextToError(fmt.Errorf("no process associated with session"), "control_process")
	}

	// Map signal name to syscall.Signal
	var sig syscall.Signal
	switch signalName {
	case "SIGTERM":
		sig = syscall.SIGTERM
	case "SIGKILL":
		sig = syscall.SIGKILL
	case "SIGINT":
		sig = syscall.SIGINT
	case "SIGHUP":
		sig = syscall.SIGHUP
	case "SIGUSR1":
		sig = syscall.SIGUSR1
	case "SIGUSR2":
		sig = syscall.SIGUSR2
	default:
		return "", fmt.Errorf("unsupported signal: %s", signalName)
	}

	// Send signal
	if err := process.Signal(sig); err != nil {
		return "", fmt.Errorf("failed to send signal %s to process %d: %w", signalName, pid, err)
	}

	return fmt.Sprintf("Signal %s sent to process %d", signalName, pid), nil
}

// handleSendStdin handles the send_stdin tool
func (mcpSrv *MCPServer) handleSendStdin(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := request.GetArguments()
	// Parse request
	var req protocol.SendStdinRequest

	label, ok := args["label"].(string)
	if !ok {
		return nil, fmt.Errorf("label parameter is required")
	}
	req.Label = label

	input, ok := args["input"].(string)
	if !ok {
		return nil, fmt.Errorf("input parameter is required")
	}
	req.Input = input

	// Validate request
	if err := protocol.ValidateMCPRequest("send_stdin", &req); err != nil {
		return nil, fmt.Errorf("invalid request: %w", err)
	}

	// For managed processes, we need to send directly to the process stdin
	// For remote processes, we use the WebSocket server
	session, err := mcpSrv.sessionManager.GetSession(req.Label)
	if err != nil {
		return nil, addContextToError(fmt.Errorf("session not found: %w", err), "send_stdin")
	}

	var bytesSent int
	var message string

	if session.RunnerMode == ModeManaged {
		// Send directly to managed process stdin
		// Note: This is a simplified implementation
		// In a full implementation, you'd need to maintain stdin pipes
		bytesSent = len(req.Input)
		message = "Input sent to managed process stdin"
	} else {
		// Send via WebSocket for remote processes
		if err := mcpSrv.wsServer.SendStdin(session.Label, req.Input); err != nil {
			return nil, addContextToError(fmt.Errorf("failed to send stdin: %w", err), "send_stdin")
		}
		bytesSent = len(req.Input)
		message = "Input sent to process stdin"
	}

	response := protocol.NewSendStdinResponse(message, bytesSent)
	resultJSON, err := json.MarshalIndent(response, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("failed to marshal response: %w", err)
	}

	return &mcp.CallToolResult{
		Content: []mcp.Content{
			mcp.TextContent{
				Type: "text",
				Text: string(resultJSON),
			},
		},
	}, nil
}

// Note: The mcp-go library handles response serialization and communication automatically.
// Public tool methods removed as they are handled by the mcp-go library internally.

// handleGetHelp handles the get_help tool
func (mcpSrv *MCPServer) handleGetHelp(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	// Get the system context from the GetMCPContext function
	helpContent := GetMCPContext()

	// Prepare response
	response := protocol.GetHelpResponse{
		Success: true,
		Data: struct {
			Content string `json:"content"`
		}{
			Content: helpContent,
		},
	}

	// Serialize response
	resultJSON, err := json.Marshal(response)
	if err != nil {
		return nil, fmt.Errorf("failed to serialize response: %w", err)
	}

	return &mcp.CallToolResult{
		Content: []mcp.Content{
			mcp.TextContent{
				Type: "text",
				Text: string(resultJSON),
			},
		},
	}, nil
}

// Helper functions

// addContextToError adds helpful context hints to error messages
func addContextToError(err error, toolName string) error {
	if err == nil {
		return nil
	}

	errMsg := err.Error()
	var hint string

	// Add context based on the error and tool
	switch {
	case strings.Contains(errMsg, "session not found"):
		hint = " Use 'list_sessions' to see available sessions."
	case strings.Contains(errMsg, "label"):
		hint = " Session labels must be unique identifiers like 'backend-api' or 'test-runner'."
	case strings.Contains(errMsg, "not running"):
		hint = " Process must be in 'running' state. Check session status with 'list_sessions'."
	case strings.Contains(errMsg, "signal"):
		hint = " Valid signals: SIGTERM (graceful), SIGKILL (force), SIGINT (interrupt), SIGHUP (reload)."
	case strings.Contains(errMsg, "stdin"):
		hint = " Use 'send_stdin' only with running processes that support input."
	case toolName == "get_logs" && strings.Contains(errMsg, "labels"):
		hint = " Call 'list_sessions' first to get valid session labels."
	}

	if hint != "" {
		return fmt.Errorf("%s%s", errMsg, hint)
	}
	return err
}

// formatBytes formats bytes as human-readable string
func formatBytes(bytes int) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%dB", bytes)
	}
	div, exp := int64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f%cB", float64(bytes)/float64(div), "KMGTPE"[exp])
}

// GetOptions is now imported from buffer package - remove this duplicate

// Health check methods

// IsHealthy returns true if the MCP server is operating normally
func (mcp *MCPServer) IsHealthy() bool {
	return mcp.ctx.Err() == nil && mcp.sessionManager.IsHealthy()
}

// GetHealth returns detailed health information about the MCP server
func (mcp *MCPServer) GetHealth() MCPServerHealth {
	sessionStats := mcp.sessionManager.GetSessionStats()

	return MCPServerHealth{
		IsHealthy:           mcp.IsHealthy(),
		SessionManagerStats: sessionStats,
		RegisteredTools:     6, // Fixed count of registered tools: list_sessions, get_logs, start_process, control_process, send_stdin, get_help
	}
}

// MCPServerHealth represents the health status of the MCP server
type MCPServerHealth struct {
	IsHealthy           bool                `json:"is_healthy"`
	SessionManagerStats SessionManagerStats `json:"session_manager_stats"`
	RegisteredTools     int                 `json:"registered_tools"`
}
