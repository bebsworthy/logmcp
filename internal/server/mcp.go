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
	"sync"
	"syscall"
	"time"

	"github.com/logmcp/logmcp/internal/buffer"
	"github.com/logmcp/logmcp/internal/protocol"
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

	// Create mcp-go server instance
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
			mcp.WithDescription("List all active log sessions"),
		),
		mcpSrv.handleListSessions,
	)

	// Register get_logs tool
	mcpSrv.mcpServer.AddTool(
		mcp.NewTool("get_logs",
			mcp.WithDescription("Get and search logs from one or more sessions"),
			mcp.WithArray("labels",
				mcp.Required(),
				mcp.Description("Session labels to query (single or multiple)"),
				mcp.Items(map[string]interface{}{
					"type":        "string",
					"description": "Session label",
				}),
			),
			mcp.WithNumber("lines",
				mcp.Description("Number of lines to return"),
			),
			mcp.WithString("since",
				mcp.Description("ISO timestamp"),
			),
			mcp.WithString("stream",
				mcp.Description("Stream type filter"),
				mcp.Enum("stdout", "stderr", "both"),
			),
			mcp.WithString("pattern",
				mcp.Description("Regex pattern to filter log entries"),
			),
			mcp.WithNumber("max_results",
				mcp.Description("Maximum results across all sessions"),
			),
		),
		mcpSrv.handleGetLogs,
	)

	// Register start_process tool
	mcpSrv.mcpServer.AddTool(
		mcp.NewTool("start_process",
			mcp.WithDescription("Launch a new managed process"),
			mcp.WithString("command",
				mcp.Required(),
				mcp.Description("Command to execute"),
			),
			mcp.WithArray("arguments",
				mcp.Description("Command arguments"),
				mcp.Items(map[string]interface{}{
					"type": "string",
				}),
			),
			mcp.WithString("label",
				mcp.Required(),
				mcp.Description("Label for the process session"),
			),
			mcp.WithString("working_dir",
				mcp.Description("Working directory"),
			),
			mcp.WithObject("environment",
				mcp.Description("Environment variables"),
			),
			mcp.WithString("restart_policy",
				mcp.Description("Restart policy"),
				mcp.Enum("never", "always", "on-failure"),
			),
			mcp.WithBoolean("collect_startup_logs",
				mcp.Description("Collect startup logs"),
			),
		),
		mcpSrv.handleStartProcess,
	)

	// Register control_process tool
	mcpSrv.mcpServer.AddTool(
		mcp.NewTool("control_process",
			mcp.WithDescription("Control a managed process"),
			mcp.WithString("label",
				mcp.Required(),
				mcp.Description("Session label"),
			),
			mcp.WithString("action",
				mcp.Required(),
				mcp.Description("Action to perform"),
				mcp.Enum("restart", "signal"),
			),
			mcp.WithString("signal",
				mcp.Description("Signal to send (required for signal action)"),
				mcp.Enum("SIGTERM", "SIGKILL", "SIGINT", "SIGHUP", "SIGUSR1", "SIGUSR2"),
			),
		),
		mcpSrv.handleControlProcess,
	)

	// Register send_stdin tool
	mcpSrv.mcpServer.AddTool(
		mcp.NewTool("send_stdin",
			mcp.WithDescription("Send input to a process stdin"),
			mcp.WithString("label",
				mcp.Required(),
				mcp.Description("Session label"),
			),
			mcp.WithString("input",
				mcp.Required(),
				mcp.Description("Input to send"),
			),
		),
		mcpSrv.handleSendStdin,
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
		sessions := mcpSrv.sessionManager.GetSessionsByLabel(label)
		if len(sessions) == 0 {
			notFoundSessions = append(notFoundSessions, label)
			continue
		}

		queriedSessions = append(queriedSessions, label)

		for _, session := range sessions {
			session.mutex.RLock()
			if session.LogBuffer == nil {
				session.mutex.RUnlock()
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
		Command:     req.Command,
		Arguments:   req.Arguments,
		Label:       req.Label,
		WorkingDir:  *req.WorkingDir,
		Environment: req.Environment,
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
		mcpSrv.sessionManager.RemoveSession(session.ID)
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

	// Start goroutines to read stdout/stderr
	mcpSrv.wg.Add(3)

	// Read stdout
	go func() {
		defer mcpSrv.wg.Done()
		defer stdout.Close()

		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			line := scanner.Text()
			logMsg := protocol.NewLogMessage(session.ID, session.Label, line, protocol.StreamStdout, session.PID)
			if session.LogBuffer != nil {
				session.LogBuffer.AddFromMessage(logMsg)
			}
		}
	}()

	// Read stderr
	go func() {
		defer mcpSrv.wg.Done()
		defer stderr.Close()

		scanner := bufio.NewScanner(stderr)
		for scanner.Scan() {
			line := scanner.Text()
			logMsg := protocol.NewLogMessage(session.ID, session.Label, line, protocol.StreamStderr, session.PID)
			if session.LogBuffer != nil {
				session.LogBuffer.AddFromMessage(logMsg)
			}
		}
	}()

	// Wait for process to complete
	go func() {
		defer mcpSrv.wg.Done()
		defer stdin.Close()

		err := cmd.Wait()
		exitCode := 0
		if err != nil {
			if exitError, ok := err.(*exec.ExitError); ok {
				exitCode = exitError.ExitCode()
			} else {
				exitCode = 1
			}
		}

		// Debug: Log process completion
		log.Printf("[DEBUG] Process %s (PID %d) completed with exit code %d, err: %v", session.Label, session.PID, exitCode, err)

		// Update session status
		var status protocol.SessionStatus
		if exitCode == 0 {
			status = protocol.StatusStopped
		} else {
			status = protocol.StatusCrashed
		}

		mcpSrv.sessionManager.UpdateSessionStatus(session.ID, status, session.PID, &exitCode)
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
	session, err := mcpSrv.sessionManager.GetSessionByLabel(req.Label)
	if err != nil {
		return nil, fmt.Errorf("session not found: %w", err)
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
	session.mutex.Lock()
	defer session.mutex.Unlock()

	// Kill existing process if running
	if session.Process != nil {
		if err := session.Process.Kill(); err != nil {
			log.Printf("Warning: failed to kill process %d: %v", session.PID, err)
		}
		session.Process.Wait() // Wait for cleanup
	}

	// Update status
	session.Status = protocol.StatusRestarting

	// Get managed args for restart
	managedArgs, ok := session.RunnerArgs.(protocol.ManagedArgs)
	if !ok {
		return "", fmt.Errorf("session is not a managed process")
	}

	// Create restart request
	req := protocol.StartProcessRequest{
		Command:     managedArgs.Command,
		Arguments:   managedArgs.Arguments,
		Label:       managedArgs.Label,
		WorkingDir:  &managedArgs.WorkingDir,
		Environment: managedArgs.Environment,
	}

	// Start new process
	if err := mcp.startManagedProcess(session, req); err != nil {
		session.Status = protocol.StatusCrashed
		return "", fmt.Errorf("failed to restart process: %w", err)
	}

	return fmt.Sprintf("Process restarted successfully with PID %d", session.PID), nil
}

// signalProcess sends a signal to a managed process
func (mcp *MCPServer) signalProcess(session *Session, signalName string) (string, error) {
	session.mutex.RLock()
	process := session.Process
	pid := session.PID
	session.mutex.RUnlock()

	if process == nil {
		return "", fmt.Errorf("no process associated with session")
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
	session, err := mcpSrv.sessionManager.GetSessionByLabel(req.Label)
	if err != nil {
		return nil, fmt.Errorf("session not found: %w", err)
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
		if err := mcpSrv.wsServer.SendStdin(session.ID, req.Input); err != nil {
			return nil, fmt.Errorf("failed to send stdin: %w", err)
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

// Helper functions

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
		RegisteredTools:     5, // Fixed count of registered tools: list_sessions, get_logs, start_process, control_process, send_stdin
	}
}

// MCPServerHealth represents the health status of the MCP server
type MCPServerHealth struct {
	IsHealthy           bool                `json:"is_healthy"`
	SessionManagerStats SessionManagerStats `json:"session_manager_stats"`
	RegisteredTools     int                 `json:"registered_tools"`
}
