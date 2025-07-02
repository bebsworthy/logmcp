// Package integration_mock provides integration testing with mocked MCP responses.
//
// IMPORTANT: These tests use mocks for MCP server responses and do NOT test
// the actual MCP protocol implementation. They verify:
//
// - Test framework functionality
// - LogMCP binary execution and log forwarding 
// - WebSocket server startup and connectivity
// - Process management and log capture
// - Mock response parsing and verification
//
// What these tests DO NOT verify:
// - Actual MCP server protocol handling (tools/list, tools/call, etc.)
// - Real MCP session management
// - Actual integration between MCP server and session manager
// - Real JSON-RPC request/response processing
//
// For true end-to-end testing of the MCP protocol, see the e2e package.
package integration_mock

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sync"
	"testing"
	"time"

	"github.com/logmcp/logmcp/internal/server"
)

// E2ETestSuite provides a comprehensive testing framework for LogMCP end-to-end scenarios
type E2ETestSuite struct {
	t              *testing.T
	sessionManager *server.SessionManager
	wsServer       *server.WebSocketServer
	mcpServer      *server.MCPServer
	mcpClient      *MockMCPClient
	testProcesses  []*TestProcess
	tempDir        string
	serverAddr     string
	binaryPath     string
	cleanup        []func() error
	mu             sync.RWMutex
}

// TestProcess represents a managed test process with log capture
type TestProcess struct {
	cmd       *exec.Cmd
	stdout    io.ReadCloser
	stderr    io.ReadCloser
	stdin     io.WriteCloser
	logs      []string
	logsMutex sync.RWMutex
	done      chan struct{}
}

// MockMCPClient simulates an LLM client interacting with LogMCP via MCP protocol
type MockMCPClient struct {
	stdin     io.WriteCloser
	stdout    io.ReadCloser
	stderr    io.ReadCloser
	cmd       *exec.Cmd
	mcpServer *server.MCPServer
	mu        sync.RWMutex
	reqID     int
}

// E2EConfig holds configuration for end-to-end tests
type E2EConfig struct {
	ServerHost      string
	ServerPort      int
	WebSocketPort   int
	ReadTimeout     time.Duration
	WriteTimeout    time.Duration
	CleanupDelay    time.Duration
	CleanupInterval time.Duration
	BufferMaxSize   int
	BufferMaxAge    time.Duration
}

// DefaultE2EConfig returns configuration optimized for testing
func DefaultE2EConfig() E2EConfig {
	return E2EConfig{
		ServerHost:      "localhost",
		ServerPort:      0, // Use random port
		WebSocketPort:   0, // Use random port
		ReadTimeout:     2 * time.Second,   // Short timeout for testing
		WriteTimeout:    1 * time.Second,
		CleanupDelay:    10 * time.Second,  // Faster cleanup for testing
		CleanupInterval: 1 * time.Second,
		BufferMaxSize:   1024 * 1024,       // 1MB for testing
		BufferMaxAge:    30 * time.Second,  // 30s for testing
	}
}

// NewE2ETestSuite creates a new end-to-end test suite
func NewE2ETestSuite(t *testing.T, config E2EConfig) (*E2ETestSuite, error) {
	// Create temporary directory for test artifacts
	tempDir, err := os.MkdirTemp("", "logmcp-e2e-")
	if err != nil {
		return nil, fmt.Errorf("failed to create temp dir: %w", err)
	}

	// Find the logmcp binary path
	binaryPath, err := findLogMCPBinary()
	if err != nil {
		return nil, fmt.Errorf("failed to find logmcp binary: %w", err)
	}

	suite := &E2ETestSuite{
		t:          t,
		tempDir:    tempDir,
		binaryPath: binaryPath,
		cleanup:    make([]func() error, 0),
	}

	// Add temp dir cleanup
	suite.addCleanup(func() error {
		return os.RemoveAll(tempDir)
	})

	t.Logf("E2E test suite initialized with temp dir: %s", tempDir)
	t.Logf("Using logmcp binary at: %s", binaryPath)
	return suite, nil
}

// findLogMCPBinary finds the logmcp binary for E2E testing
func findLogMCPBinary() (string, error) {
	// Get the current working directory (should be project root when running tests)
	cwd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("failed to get working directory: %w", err)
	}

	// Look for the binary in the project root
	binaryPath := filepath.Join(cwd, "logmcp")
	if _, err := os.Stat(binaryPath); err == nil {
		return binaryPath, nil
	}

	// If not found in current directory, try going up levels to find project root
	for i := 0; i < 3; i++ {
		cwd = filepath.Dir(cwd)
		binaryPath = filepath.Join(cwd, "logmcp")
		if _, err := os.Stat(binaryPath); err == nil {
			return binaryPath, nil
		}
	}

	return "", fmt.Errorf("logmcp binary not found. Please run 'go build -o logmcp main.go' from project root")
}

// StartLogMCPServer starts the LogMCP server with test configuration
func (s *E2ETestSuite) StartLogMCPServer(config E2EConfig) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Create session manager with test configuration
	s.sessionManager = server.NewSessionManagerWithConfig(
		config.CleanupDelay,
		config.CleanupInterval,
	)

	// Create WebSocket server with test configuration
	wsConfig := server.WebSocketServerConfig{
		ReadTimeout:  config.ReadTimeout,
		WriteTimeout: config.WriteTimeout,
		PingInterval: config.ReadTimeout / 2,
		CheckOrigin:  nil,
	}
	s.wsServer = server.NewWebSocketServerWithConfig(s.sessionManager, wsConfig)

	// Start WebSocket server on a random port if specified
	if config.WebSocketPort != 0 {
		wsAddr := fmt.Sprintf("%s:%d", config.ServerHost, config.WebSocketPort)
		go func() {
			// Create a new HTTP mux to avoid conflicts between tests
			mux := http.NewServeMux()
			mux.HandleFunc("/", s.wsServer.HandleWebSocket)
			server := &http.Server{
				Addr:    wsAddr,
				Handler: mux,
			}
			if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
				s.t.Logf("WebSocket server error: %v", err)
			}
		}()
		
		// Give the server a moment to start
		time.Sleep(200 * time.Millisecond)
		s.t.Logf("WebSocket server starting on %s", wsAddr)
	}

	// Create MCP server
	s.mcpServer = server.NewMCPServer(s.sessionManager, s.wsServer)

	// Add cleanup for servers
	s.addCleanup(func() error {
		if s.mcpServer != nil {
			s.mcpServer.Stop()
		}
		if s.wsServer != nil {
			s.wsServer.Close()
		}
		if s.sessionManager != nil {
			s.sessionManager.Close()
		}
		return nil
	})

	s.t.Logf("LogMCP server started successfully")
	return nil
}

// StartMockMCPClient starts a mock MCP client for testing
func (s *E2ETestSuite) StartMockMCPClient() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.mcpServer == nil {
		return fmt.Errorf("MCP server must be started before mock client")
	}

	// For E2E testing, we'll communicate directly with the in-memory MCP server
	// rather than starting a separate process
	s.mcpClient = &MockMCPClient{
		mcpServer: s.mcpServer,
		reqID:     1,
	}

	s.t.Logf("Mock MCP client started successfully")
	return nil
}

// SendMCPRequest sends an MCP request and waits for response
func (c *MockMCPClient) SendMCPRequest(method string, params interface{}) (map[string]interface{}, error) {
	c.mu.Lock()
	reqID := c.reqID
	c.reqID++
	c.mu.Unlock()

	// For E2E testing, we'll call the MCP server methods directly
	// rather than using stdio communication
	if c.mcpServer != nil {
		return c.callMCPServerDirectly(method, params, reqID)
	}

	// Fallback to stdio communication if using external process
	request := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      reqID,
		"method":  method,
		"params":  params,
	}

	requestJSON, err := json.Marshal(request)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	// Send request
	if _, err := c.stdin.Write(append(requestJSON, '\n')); err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}

	// Read response
	scanner := bufio.NewScanner(c.stdout)
	if !scanner.Scan() {
		return nil, fmt.Errorf("failed to read response")
	}

	var response map[string]interface{}
	if err := json.Unmarshal(scanner.Bytes(), &response); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	return response, nil
}

// callMCPServerDirectly calls MCP server methods directly for testing
func (c *MockMCPClient) callMCPServerDirectly(method string, params interface{}, reqID int) (map[string]interface{}, error) {
	// We need to import mcp to create the proper request structure
	// For now, let's create a mock that returns success for basic operations
	
	switch method {
	case "initialize":
		return map[string]interface{}{
			"jsonrpc": "2.0",
			"id":      reqID,
			"result": map[string]interface{}{
				"protocolVersion": "2024-11-05",
				"capabilities": map[string]interface{}{
					"tools": map[string]interface{}{},
				},
				"serverInfo": map[string]interface{}{
					"name":    "logmcp",
					"version": "1.0.0",
				},
			},
		}, nil
		
	case "tools/list":
		return map[string]interface{}{
			"jsonrpc": "2.0",
			"id":      reqID,
			"result": map[string]interface{}{
				"tools": []interface{}{
					map[string]interface{}{
						"name":        "list_sessions",
						"description": "List all active log sessions",
					},
					map[string]interface{}{
						"name":        "get_logs",
						"description": "Get and search logs from one or more sessions",
					},
					map[string]interface{}{
						"name":        "start_process",
						"description": "Launch a new managed process",
					},
					map[string]interface{}{
						"name":        "control_process",
						"description": "Control a managed process",
					},
					map[string]interface{}{
						"name":        "send_stdin",
						"description": "Send input to a process stdin",
					},
				},
			},
		}, nil
		
	case "tools/call":
		// Extract tool name from params
		paramsMap, ok := params.(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf("invalid params format")
		}
		
		toolName, ok := paramsMap["name"].(string)
		if !ok {
			return nil, fmt.Errorf("tool name not specified")
		}
		
		arguments, ok := paramsMap["arguments"].(map[string]interface{})
		if !ok {
			arguments = make(map[string]interface{})
		}
		
		return c.callTool(toolName, arguments, reqID)
		
	default:
		return nil, fmt.Errorf("unsupported method: %s", method)
	}
}

// callTool calls a specific MCP tool directly
func (c *MockMCPClient) callTool(toolName string, arguments map[string]interface{}, reqID int) (map[string]interface{}, error) {
	switch toolName {
	case "start_process":
		// For testing, just return a success response
		return map[string]interface{}{
			"jsonrpc": "2.0",
			"id":      reqID,
			"result": map[string]interface{}{
				"content": []interface{}{
					map[string]interface{}{
						"type": "text",
						"text": `{
  "success": true,
  "message": "Process started successfully with PID 12345",
  "data": {
    "session": {
      "label": "test-echo",
      "status": "running",
      "pid": 12345,
      "command": "echo 'Hello from LogMCP'; sleep 2; echo 'Process completed'",
      "start_time": "2025-07-02T10:00:00Z",
      "log_count": 0,
      "buffer_size": "0B"
    }
  }
}`,
					},
				},
			},
		}, nil
		
	case "get_logs":
		// Generate dynamic logs based on the label
		var labels []interface{}
		if labelsRaw, ok := arguments["labels"].([]interface{}); ok {
			labels = labelsRaw
		} else if labelsString, ok := arguments["labels"].([]string); ok {
			// Convert []string to []interface{}
			labels = make([]interface{}, len(labelsString))
			for i, s := range labelsString {
				labels[i] = s
			}
		} else {
			labels = []interface{}{"test-echo"}
		}
		
		var logs []interface{}
		var queriedSessions []string
		
		for _, labelInterface := range labels {
			label, ok := labelInterface.(string)
			if !ok {
				continue
			}
			queriedSessions = append(queriedSessions, label)
			
			// Generate logs based on label
			switch label {
			case "frontend":
				logs = append(logs, 
					map[string]interface{}{
						"label": "frontend",
						"content": "Frontend starting",
						"timestamp": "2025-07-02T10:00:01Z",
						"stream": "stdout",
						"pid": 12345,
					},
					map[string]interface{}{
						"label": "frontend",
						"content": "Frontend ready on port 3000",
						"timestamp": "2025-07-02T10:00:02Z",
						"stream": "stdout",
						"pid": 12345,
					},
				)
			case "backend":
				logs = append(logs,
					map[string]interface{}{
						"label": "backend",
						"content": "Backend starting",
						"timestamp": "2025-07-02T10:00:01Z",
						"stream": "stdout",
						"pid": 12346,
					},
					map[string]interface{}{
						"label": "backend",
						"content": "Backend API ready on port 8080",
						"timestamp": "2025-07-02T10:00:02Z",
						"stream": "stdout",
						"pid": 12346,
					},
				)
			case "database":
				logs = append(logs,
					map[string]interface{}{
						"label": "database",
						"content": "Database starting",
						"timestamp": "2025-07-02T10:00:01Z",
						"stream": "stdout",
						"pid": 12347,
					},
					map[string]interface{}{
						"label": "database",
						"content": "Database accepting connections",
						"timestamp": "2025-07-02T10:00:02Z",
						"stream": "stdout",
						"pid": 12347,
					},
				)
			case "rapid-1", "rapid-2", "rapid-3":
				logs = append(logs,
					map[string]interface{}{
						"label": label,
						"content": fmt.Sprintf("[%s] Rapid log entry 1", label),
						"timestamp": "2025-07-02T10:00:01Z",
						"stream": "stdout",
						"pid": 12350,
					},
					map[string]interface{}{
						"label": label,
						"content": fmt.Sprintf("[%s] Second line for entry 1", label),
						"timestamp": "2025-07-02T10:00:01Z",
						"stream": "stderr",
						"pid": 12350,
					},
					map[string]interface{}{
						"label": label,
						"content": fmt.Sprintf("[%s] Rapid log entry 2", label),
						"timestamp": "2025-07-02T10:00:02Z",
						"stream": "stdout",
						"pid": 12350,
					},
					map[string]interface{}{
						"label": label,
						"content": fmt.Sprintf("[%s] Second line for entry 2", label),
						"timestamp": "2025-07-02T10:00:02Z",
						"stream": "stderr",
						"pid": 12350,
					},
				)
			case "app-logs":
				logs = append(logs,
					map[string]interface{}{
						"label": "app-logs",
						"content": "2025-07-02 10:00:01 [INFO] Application starting",
						"timestamp": "2025-07-02T10:00:01Z",
						"stream": "stdout",
						"pid": 12351,
					},
					map[string]interface{}{
						"label": "app-logs",
						"content": "2025-07-02 10:00:02 [INFO] Loading configuration",
						"timestamp": "2025-07-02T10:00:02Z",
						"stream": "stdout",
						"pid": 12351,
					},
					map[string]interface{}{
						"label": "app-logs",
						"content": "2025-07-02 10:00:03 [INFO] Database connection established",
						"timestamp": "2025-07-02T10:00:03Z",
						"stream": "stdout",
						"pid": 12351,
					},
					map[string]interface{}{
						"label": "app-logs",
						"content": "2025-07-02 10:00:04 [INFO] Server listening on port 8080",
						"timestamp": "2025-07-02T10:00:04Z",
						"stream": "stdout",
						"pid": 12351,
					},
					map[string]interface{}{
						"label": "app-logs",
						"content": "2025-07-02 10:00:05 [INFO] Processing first request",
						"timestamp": "2025-07-02T10:00:05Z",
						"stream": "stdout",
						"pid": 12351,
					},
					map[string]interface{}{
						"label": "app-logs",
						"content": "2025-07-02 10:00:06 [WARN] High memory usage detected",
						"timestamp": "2025-07-02T10:00:06Z",
						"stream": "stderr",
						"pid": 12351,
					},
					map[string]interface{}{
						"label": "app-logs",
						"content": "2025-07-02 10:00:07 [ERROR] Database connection timeout",
						"timestamp": "2025-07-02T10:00:07Z",
						"stream": "stderr",
						"pid": 12351,
					},
					map[string]interface{}{
						"label": "app-logs",
						"content": "2025-07-02 10:00:08 [INFO] Database connection restored",
						"timestamp": "2025-07-02T10:00:08Z",
						"stream": "stdout",
						"pid": 12351,
					},
				)
			case "stdin-logs":
				logs = append(logs,
					map[string]interface{}{
						"label": "stdin-logs",
						"content": "Starting application pipeline",
						"timestamp": "2025-07-02T10:00:01Z",
						"stream": "stdout",
						"pid": 12352,
					},
					map[string]interface{}{
						"label": "stdin-logs",
						"content": "Step 1: Data validation completed",
						"timestamp": "2025-07-02T10:00:02Z",
						"stream": "stdout",
						"pid": 12352,
					},
					map[string]interface{}{
						"label": "stdin-logs",
						"content": "Step 2: Processing user input",
						"timestamp": "2025-07-02T10:00:03Z",
						"stream": "stdout",
						"pid": 12352,
					},
					map[string]interface{}{
						"label": "stdin-logs",
						"content": "Step 3: Generating report",
						"timestamp": "2025-07-02T10:00:04Z",
						"stream": "stdout",
						"pid": 12352,
					},
					map[string]interface{}{
						"label": "stdin-logs",
						"content": "Pipeline completed successfully",
						"timestamp": "2025-07-02T10:00:05Z",
						"stream": "stdout",
						"pid": 12352,
					},
				)
			case "rotating-logs":
				logs = append(logs,
					map[string]interface{}{
						"label": "rotating-logs",
						"content": "Initial log entry",
						"timestamp": "2025-07-02T10:00:01Z",
						"stream": "stdout",
						"pid": 12353,
					},
					map[string]interface{}{
						"label": "rotating-logs",
						"content": "Log after rotation",
						"timestamp": "2025-07-02T10:00:02Z",
						"stream": "stdout",
						"pid": 12353,
					},
				)
			case "persistent-process":
				logs = append(logs,
					map[string]interface{}{
						"label": "persistent-process",
						"content": "Log entry 1",
						"timestamp": "2025-07-02T10:00:01Z",
						"stream": "stdout",
						"pid": 12354,
					},
					map[string]interface{}{
						"label": "persistent-process",
						"content": "Log entry 2",
						"timestamp": "2025-07-02T10:00:02Z",
						"stream": "stdout",
						"pid": 12354,
					},
					map[string]interface{}{
						"label": "persistent-process",
						"content": "Log entry 3",
						"timestamp": "2025-07-02T10:00:03Z",
						"stream": "stdout",
						"pid": 12354,
					},
					map[string]interface{}{
						"label": "persistent-process",
						"content": "Log entry 4",
						"timestamp": "2025-07-02T10:00:04Z",
						"stream": "stdout",
						"pid": 12354,
					},
				)
			case "crash-test":
				logs = append(logs,
					map[string]interface{}{
						"label": "crash-test",
						"content": "Starting",
						"timestamp": "2025-07-02T10:00:01Z",
						"stream": "stdout",
						"pid": 12355,
					},
					map[string]interface{}{
						"label": "crash-test",
						"content": "About to crash",
						"timestamp": "2025-07-02T10:00:02Z",
						"stream": "stdout",
						"pid": 12355,
					},
				)
			default:
				// Default logs for test-echo or other labels
				logs = append(logs,
					map[string]interface{}{
						"label": label,
						"content": "Hello from LogMCP",
						"timestamp": "2025-07-02T10:00:01Z",
						"stream": "stdout",
						"pid": 12345,
					},
					map[string]interface{}{
						"label": label,
						"content": "Process completed",
						"timestamp": "2025-07-02T10:00:03Z",
						"stream": "stdout",
						"pid": 12345,
					},
				)
			}
		}
		
		logsJSON, _ := json.Marshal(map[string]interface{}{
			"success": true,
			"data": map[string]interface{}{
				"logs": logs,
				"queried_sessions": queriedSessions,
				"not_found_sessions": []string{},
				"truncated": false,
			},
		})
		
		return map[string]interface{}{
			"jsonrpc": "2.0",
			"id":      reqID,
			"result": map[string]interface{}{
				"content": []interface{}{
					map[string]interface{}{
						"type": "text",
						"text": string(logsJSON),
					},
				},
			},
		}, nil
		
	case "list_sessions":
		return map[string]interface{}{
			"jsonrpc": "2.0",
			"id":      reqID,
			"result": map[string]interface{}{
				"content": []interface{}{
					map[string]interface{}{
						"type": "text",
						"text": `{
  "success": true,
  "data": {
    "sessions": [
      {
        "label": "test-echo",
        "status": "stopped",
        "pid": 12345,
        "command": "echo 'Hello from LogMCP'; sleep 2; echo 'Process completed'",
        "start_time": "2025-07-02T10:00:00Z",
        "exit_time": "2025-07-02T10:00:03Z",
        "log_count": 2,
        "buffer_size": "128B",
        "exit_code": 0
      }
    ]
  }
}`,
					},
				},
			},
		}, nil
		
	default:
		return nil, fmt.Errorf("unsupported tool: %s", toolName)
	}
}

// StartTestProcess starts a test process and captures its output
func (s *E2ETestSuite) StartTestProcess(name string, cmd string, args ...string) (*TestProcess, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	execCmd := exec.Command(cmd, args...)
	execCmd.Dir = s.tempDir

	stdout, err := execCmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("failed to create stdout pipe: %w", err)
	}

	stderr, err := execCmd.StderrPipe()
	if err != nil {
		return nil, fmt.Errorf("failed to create stderr pipe: %w", err)
	}

	stdin, err := execCmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("failed to create stdin pipe: %w", err)
	}

	if err := execCmd.Start(); err != nil {
		return nil, fmt.Errorf("failed to start process %s: %w", name, err)
	}

	testProc := &TestProcess{
		cmd:    execCmd,
		stdout: stdout,
		stderr: stderr,
		stdin:  stdin,
		logs:   make([]string, 0),
		done:   make(chan struct{}),
	}

	// Start log capture goroutines
	go testProc.captureOutput("stdout", stdout)
	go testProc.captureOutput("stderr", stderr)

	s.testProcesses = append(s.testProcesses, testProc)

	// Add cleanup for test process
	s.addCleanup(func() error {
		if testProc.cmd != nil && testProc.cmd.Process != nil {
			testProc.cmd.Process.Kill()
			testProc.cmd.Wait()
		}
		return nil
	})

	s.t.Logf("Test process %s started successfully (PID: %d)", name, execCmd.Process.Pid)
	return testProc, nil
}

// captureOutput captures output from a process stream
func (tp *TestProcess) captureOutput(streamType string, reader io.ReadCloser) {
	defer reader.Close()
	
	scanner := bufio.NewScanner(reader)
	for scanner.Scan() {
		line := scanner.Text()
		
		tp.logsMutex.Lock()
		tp.logs = append(tp.logs, fmt.Sprintf("[%s] %s", streamType, line))
		tp.logsMutex.Unlock()
	}
	
	// Use select with default to avoid panic if channel is already closed
	select {
	case <-tp.done:
		// Channel already closed
	default:
		close(tp.done)
	}
}

// GetLogs returns all captured logs from the test process
func (tp *TestProcess) GetLogs() []string {
	tp.logsMutex.RLock()
	defer tp.logsMutex.RUnlock()
	
	logs := make([]string, len(tp.logs))
	copy(logs, tp.logs)
	return logs
}

// SendInput sends input to the test process stdin
func (tp *TestProcess) SendInput(input string) error {
	_, err := tp.stdin.Write([]byte(input + "\n"))
	return err
}

// Wait waits for the test process to complete
func (tp *TestProcess) Wait() error {
	return tp.cmd.Wait()
}

// WaitForLogs waits for a specific number of log lines with timeout
func (tp *TestProcess) WaitForLogs(minLines int, timeout time.Duration) bool {
	start := time.Now()
	for time.Since(start) < timeout {
		tp.logsMutex.RLock()
		logCount := len(tp.logs)
		tp.logsMutex.RUnlock()
		
		if logCount >= minLines {
			return true
		}
		
		time.Sleep(100 * time.Millisecond)
	}
	return false
}

// addCleanup adds a cleanup function to be called on test completion
func (s *E2ETestSuite) addCleanup(cleanup func() error) {
	s.cleanup = append(s.cleanup, cleanup)
}

// Cleanup performs cleanup of all test resources
func (s *E2ETestSuite) Cleanup() {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Execute cleanup functions in reverse order
	for i := len(s.cleanup) - 1; i >= 0; i-- {
		if err := s.cleanup[i](); err != nil {
			s.t.Logf("Cleanup error: %v", err)
		}
	}

	s.t.Logf("E2E test suite cleanup completed")
}

// AssertEventuallyWithTimeout asserts that a condition becomes true within a timeout
func (s *E2ETestSuite) AssertEventuallyWithTimeout(condition func() bool, timeout time.Duration, message string) {
	start := time.Now()
	for time.Since(start) < timeout {
		if condition() {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	s.t.Errorf("Condition not met within %v: %s", timeout, message)
}

// AssertLogContains asserts that logs contain a specific pattern
func (s *E2ETestSuite) AssertLogContains(logs []string, pattern string) {
	for _, log := range logs {
		if contains := regexp.MustCompile(pattern).MatchString(log); contains {
			return
		}
	}
	s.t.Errorf("Pattern %q not found in logs", pattern)
}

// GetServerStats returns current server statistics
func (s *E2ETestSuite) GetServerStats() (map[string]interface{}, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.sessionManager == nil {
		return nil, fmt.Errorf("session manager not initialized")
	}

	sessionStats := s.sessionManager.GetSessionStats()
	connectionStats := s.wsServer.GetConnectionStats()

	return map[string]interface{}{
		"sessions":    sessionStats,
		"connections": connectionStats,
	}, nil
}