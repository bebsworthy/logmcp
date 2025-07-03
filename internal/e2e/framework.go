package e2e

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/client/transport"
	"github.com/mark3labs/mcp-go/mcp"
)

// TestServer provides a complete test environment for MCP server testing
type TestServer struct {
	MCPClient     *client.Client
	WebSocketPort int // Dynamically allocated port
	ServerCmd     *exec.Cmd
	t             *testing.T
	ctx           context.Context
	cancel        context.CancelFunc
	cleanup       []func()
}

// LogOption represents options for log retrieval
type LogOption func(map[string]any)

// WithPattern sets a regex pattern filter
func WithPattern(pattern string) LogOption {
	return func(args map[string]any) {
		args["pattern"] = pattern
	}
}

// WithStream filters by stream type
func WithStream(stream string) LogOption {
	return func(args map[string]any) {
		args["stream"] = stream
	}
}

// WithSince filters logs since a timestamp
func WithSince(since string) LogOption {
	return func(args map[string]any) {
		args["since"] = since
	}
}

// WithLimit sets the line count limit
func WithLimit(limit int) LogOption {
	return func(args map[string]any) {
		args["lines"] = limit
	}
}

// WithMaxResults sets the maximum results across all sessions
func WithMaxResults(maxResults int) LogOption {
	return func(args map[string]any) {
		args["max_results"] = maxResults
	}
}

// LogEntry represents a log entry from the MCP server
type LogEntry struct {
	Timestamp string
	Stream    string
	Content   string
	SessionID string
}

// Session represents a process session
type Session struct {
	ID        string
	Label     string
	Status    string
	PID       int
	StartTime *time.Time
	EndTime   *time.Time
	ExitCode  *int
}

// Port management
var (
	portMutex sync.Mutex
	usedPorts = make(map[int]bool)
)

// getFreePort dynamically allocates a free port to avoid conflicts
func getFreePort() (int, error) {
	portMutex.Lock()
	defer portMutex.Unlock()

	// Try up to 10 times to find a free port
	for i := 0; i < 10; i++ {
		listener, err := net.Listen("tcp", ":0")
		if err != nil {
			continue
		}
		port := listener.Addr().(*net.TCPAddr).Port
		listener.Close()

		// Check if port is in use by our tests
		if !usedPorts[port] {
			usedPorts[port] = true
			return port, nil
		}
	}

	return 0, fmt.Errorf("failed to find free port after 10 attempts")
}

// releasePort marks a port as available
func releasePort(port int) {
	portMutex.Lock()
	defer portMutex.Unlock()
	delete(usedPorts, port)
}

// SetupTest creates and initializes a new test server with dynamic port allocation
func SetupTest(t *testing.T) *TestServer {
	t.Helper()

	// Get a free port for WebSocket
	wsPort, err := getFreePort()
	if err != nil {
		t.Fatalf("Failed to allocate port: %v", err)
	}

	// Create test context
	ctx, cancel := context.WithCancel(context.Background())

	ts := &TestServer{
		WebSocketPort: wsPort,
		t:             t,
		ctx:           ctx,
		cancel:        cancel,
		cleanup:       []func(){},
	}

	// Register cleanup
	t.Cleanup(func() {
		ts.Cleanup()
	})

	// Find the logmcp binary
	binaryPath, err := findLogMCPBinary()
	if err != nil {
		t.Fatalf("Failed to find binary: %v", err)
	}

	// Create stdio transport
	stdioTransport := transport.NewStdio(binaryPath, nil, "serve")

	// Create MCP client
	ts.MCPClient = client.NewClient(stdioTransport)

	// Start client (launches server)
	if err := ts.MCPClient.Start(ctx); err != nil {
		t.Fatalf("Failed to start MCP client: %v", err)
	}

	// Initialize MCP connection
	initRequest := mcp.InitializeRequest{}
	initRequest.Params.ProtocolVersion = mcp.LATEST_PROTOCOL_VERSION
	initRequest.Params.ClientInfo = mcp.Implementation{
		Name:    "e2e-test-client",
		Version: "1.0.0",
	}
	initRequest.Params.Capabilities = mcp.ClientCapabilities{}

	serverInfo, err := ts.MCPClient.Initialize(ctx, initRequest)
	if err != nil {
		t.Fatalf("Failed to initialize MCP connection: %v", err)
	}

	t.Logf("MCP server initialized: %+v", serverInfo)

	// Give server time to fully initialize
	time.Sleep(500 * time.Millisecond)

	return ts
}

// Cleanup performs cleanup of test resources
func (ts *TestServer) Cleanup() {
	// Run any registered cleanup functions
	for i := len(ts.cleanup) - 1; i >= 0; i-- {
		ts.cleanup[i]()
	}

	// Cancel context first to signal shutdown
	if ts.cancel != nil {
		ts.cancel()
	}

	// Close MCP client with a timeout
	if ts.MCPClient != nil {
		// Give it a short time to close gracefully
		done := make(chan struct{})
		go func() {
			ts.MCPClient.Close()
			close(done)
		}()
		
		select {
		case <-done:
			// Closed successfully
		case <-time.After(2 * time.Second):
			// Timeout - force kill if we have a process
			if ts.ServerCmd != nil && ts.ServerCmd.Process != nil {
				ts.ServerCmd.Process.Kill()
			}
		}
	}

	// Kill server process if running
	if ts.ServerCmd != nil && ts.ServerCmd.Process != nil {
		ts.ServerCmd.Process.Kill()
		// Don't wait too long for it
		done := make(chan error)
		go func() {
			done <- ts.ServerCmd.Wait()
		}()
		select {
		case <-done:
			// Process exited
		case <-time.After(1 * time.Second):
			// Give up waiting
		}
	}

	// Release port
	releasePort(ts.WebSocketPort)
}

// StartTestProcess starts a process via MCP
func (ts *TestServer) StartTestProcess(label, command string, args ...string) error {
	callRequest := mcp.CallToolRequest{}
	callRequest.Params.Name = "start_process"
	
	// Combine command and args into a single string
	fullCommand := command
	if len(args) > 0 {
		fullCommand = fmt.Sprintf("%s %s", command, strings.Join(args, " "))
	}
	
	arguments := map[string]any{
		"label":   label,
		"command": fullCommand,
	}

	callRequest.Params.Arguments = arguments

	ctx, cancel := context.WithTimeout(ts.ctx, 10*time.Second)
	defer cancel()

	_, err := ts.MCPClient.CallTool(ctx, callRequest)
	return err
}

// StartTestProcessWithOptions starts a process with additional options
func (ts *TestServer) StartTestProcessWithOptions(label, command string, args []string, opts map[string]any) error {
	callRequest := mcp.CallToolRequest{}
	callRequest.Params.Name = "start_process"
	
	// Combine command and args into a single string
	fullCommand := command
	if len(args) > 0 {
		fullCommand = fmt.Sprintf("%s %s", command, strings.Join(args, " "))
	}
	
	arguments := map[string]any{
		"label":   label,
		"command": fullCommand,
	}

	// Add additional options
	for k, v := range opts {
		arguments[k] = v
	}

	callRequest.Params.Arguments = arguments

	ctx, cancel := context.WithTimeout(ts.ctx, 10*time.Second)
	defer cancel()

	_, err := ts.MCPClient.CallTool(ctx, callRequest)
	return err
}

// GetLogs retrieves logs from the MCP server
func (ts *TestServer) GetLogs(labels []string, opts ...LogOption) ([]LogEntry, error) {
	callRequest := mcp.CallToolRequest{}
	callRequest.Params.Name = "get_logs"
	arguments := map[string]any{
		"labels": labels,
	}

	// Apply options
	for _, opt := range opts {
		opt(arguments)
	}

	callRequest.Params.Arguments = arguments

	ctx, cancel := context.WithTimeout(ts.ctx, 5*time.Second)
	defer cancel()

	result, err := ts.MCPClient.CallTool(ctx, callRequest)
	if err != nil {
		return nil, err
	}

	// Parse result - MCP returns text content with JSON
	var logs []LogEntry
	if len(result.Content) > 0 {
		content := result.Content[0]
		
		var textContent string
		switch c := content.(type) {
		case mcp.TextContent:
			textContent = c.Text
		case *mcp.TextContent:
			textContent = c.Text
		default:
			// Use reflection as fallback
			if textField := reflect.ValueOf(content).FieldByName("Text"); textField.IsValid() {
				textContent = textField.String()
			} else {
				ts.t.Logf("Warning: unexpected content type for logs: %T", content)
				return logs, nil
			}
		}

		// Parse the JSON response - the server returns a wrapped response
		var response struct {
			Success bool `json:"success"`
			Data    struct {
				Logs []struct {
					Label     string    `json:"label"`
					Content   string    `json:"content"`
					Timestamp time.Time `json:"timestamp"`
					Stream    string    `json:"stream"`
					PID       int       `json:"pid"`
				} `json:"logs"`
			} `json:"data"`
			Meta struct {
				TotalResults     int      `json:"total_results"`
				Truncated        bool     `json:"truncated"`
				SessionsQueried  []string `json:"sessions_queried"`
				SessionsNotFound []string `json:"sessions_not_found"`
			} `json:"meta"`
		}
		if err := json.Unmarshal([]byte(textContent), &response); err != nil {
			ts.t.Logf("Warning: failed to parse logs JSON: %v", err)
			ts.t.Logf("Raw content: %s", textContent)
			return logs, nil
		}

		for _, log := range response.Data.Logs {
			entry := LogEntry{
				Timestamp: log.Timestamp.Format(time.RFC3339),
				Stream:    log.Stream,
				Content:   log.Content,
				SessionID: log.Label, // Use label as session ID
			}
			logs = append(logs, entry)
		}
	}

	return logs, nil
}

// ListSessions lists all sessions
func (ts *TestServer) ListSessions() ([]Session, error) {
	callRequest := mcp.CallToolRequest{}
	callRequest.Params.Name = "list_sessions"
	callRequest.Params.Arguments = map[string]any{}

	// Use a fresh context instead of ts.ctx
	ctx, cancel := context.WithTimeout(ts.ctx, 5*time.Second)
	defer cancel()

	result, err := ts.MCPClient.CallTool(ctx, callRequest)
	if err != nil {
		return nil, err
	}


	// Parse result - MCP returns text content with JSON
	var sessions []Session
	if len(result.Content) > 0 {
		// The Content is mcp.TextContent based on our testing
		content := result.Content[0]
		
		var textContent string
		switch c := content.(type) {
		case mcp.TextContent:
			textContent = c.Text
		case *mcp.TextContent:
			textContent = c.Text
		default:
			// Use reflection as fallback
			if textField := reflect.ValueOf(content).FieldByName("Text"); textField.IsValid() {
				textContent = textField.String()
			} else {
				ts.t.Logf("Warning: unexpected content type: %T", content)
				return sessions, nil
			}
		}

		// Parse the JSON response - the server returns a wrapped response
		var response struct {
			Success bool `json:"success"`
			Data    struct {
				Sessions []struct {
					ID        string     `json:"id"`
					Label     string     `json:"label"`
					Status    string     `json:"status"`
					PID       int        `json:"pid,omitempty"`
					StartTime *time.Time `json:"start_time,omitempty"`
					EndTime   *time.Time `json:"end_time,omitempty"`
					ExitCode  *int       `json:"exit_code,omitempty"`
				} `json:"sessions"`
			} `json:"data"`
			Meta struct {
				TotalCount  int `json:"total_count"`
				ActiveCount int `json:"active_count"`
			} `json:"meta"`
		}
		if err := json.Unmarshal([]byte(textContent), &response); err != nil {
			ts.t.Logf("Warning: failed to parse sessions JSON: %v", err)
			ts.t.Logf("Raw content: %s", textContent)
			return sessions, nil
		}

		for _, s := range response.Data.Sessions {
			session := Session{
				ID:        s.ID,
				Label:     s.Label,
				Status:    s.Status,
				PID:       s.PID,
				StartTime: s.StartTime,
				EndTime:   s.EndTime,
				ExitCode:  s.ExitCode,
			}
			sessions = append(sessions, session)
		}
	}

	return sessions, nil
}

// ControlProcess sends control commands to a process
func (ts *TestServer) ControlProcess(label, action string, signal ...string) error {
	callRequest := mcp.CallToolRequest{}
	callRequest.Params.Name = "control_process"
	arguments := map[string]any{
		"label":  label,
		"action": action,
	}

	if len(signal) > 0 {
		arguments["signal"] = signal[0]
	}

	callRequest.Params.Arguments = arguments

	ctx, cancel := context.WithTimeout(ts.ctx, 5*time.Second)
	defer cancel()

	_, err := ts.MCPClient.CallTool(ctx, callRequest)
	return err
}

// SendStdin sends input to a process's stdin
func (ts *TestServer) SendStdin(label, data string) error {
	callRequest := mcp.CallToolRequest{}
	callRequest.Params.Name = "send_stdin"
	callRequest.Params.Arguments = map[string]any{
		"label": label,
		"input": data, // The protocol expects "input" not "data"
	}

	ctx, cancel := context.WithTimeout(ts.ctx, 5*time.Second)
	defer cancel()

	_, err := ts.MCPClient.CallTool(ctx, callRequest)
	return err
}

// WaitForCondition polls for a condition to be true
func (ts *TestServer) WaitForCondition(timeout time.Duration, check func() bool) bool {
	deadline := time.Now().Add(timeout)
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		if check() {
			return true
		}

		select {
		case <-ticker.C:
			if time.Now().After(deadline) {
				return false
			}
		case <-ts.ctx.Done():
			return false
		}
	}
}

// AssertSessionStatus verifies a session has the expected status
func (ts *TestServer) AssertSessionStatus(label, expectedStatus string) {
	sessions, err := ts.ListSessions()
	if err != nil {
		ts.t.Fatalf("Failed to list sessions: %v", err)
	}

	for _, session := range sessions {
		if session.Label == label {
			if session.Status != expectedStatus {
				ts.t.Errorf("Session %s has status %s, expected %s", label, session.Status, expectedStatus)
			}
			return
		}
	}

	ts.t.Errorf("Session %s not found", label)
}

// WaitForSessionStatus waits for a session to reach a specific status
func (ts *TestServer) WaitForSessionStatus(label, status string, timeout time.Duration) bool {
	return ts.WaitForCondition(timeout, func() bool {
		sessions, err := ts.ListSessions()
		if err != nil {
			return false
		}
		for _, session := range sessions {
			if session.Label == label && session.Status == status {
				return true
			}
		}
		return false
	})
}

// WaitForLogContent waits for specific content to appear in logs
func (ts *TestServer) WaitForLogContent(label, content string, timeout time.Duration) bool {
	return ts.WaitForCondition(timeout, func() bool {
		logs, err := ts.GetLogs([]string{label})
		if err != nil {
			return false
		}
		for _, log := range logs {
			if strings.Contains(log.Content, content) {
				return true
			}
		}
		return false
	})
}

// RegisterCleanup adds a cleanup function to be called during test cleanup
func (ts *TestServer) RegisterCleanup(fn func()) {
	ts.cleanup = append(ts.cleanup, fn)
}

// Helper function to build test app path
func (ts *TestServer) TestAppPath(appName string) string {
	// Get the test directory
	_, filename, _, _ := runtime.Caller(1)
	testDir := filepath.Dir(filename)
	return filepath.Join(testDir, "test_helpers", appName)
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