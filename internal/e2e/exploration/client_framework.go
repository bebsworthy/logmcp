// Package e2e provides true end-to-end testing of LogMCP using the mcp-go client library.
//
// These tests use the official mcp-go client to communicate with the LogMCP server,
// ensuring proper protocol compliance and realistic testing of:
//
// - Correct MCP protocol implementation (JSON-RPC 2.0)
// - Full initialization handshake (initialize → response → initialized)
// - Tool discovery and execution via typed API
// - Session management and lifecycle
// - Log forwarding and retrieval
//
// This approach provides confidence that LogMCP will work correctly with real LLM clients
// that use the MCP protocol.
package e2e

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/client/transport"
	"github.com/mark3labs/mcp-go/mcp"
)

// ClientTestSuite provides true end-to-end testing using mcp-go client library
type ClientTestSuite struct {
	t          *testing.T
	mcpClient  *client.Client
	binaryPath string
	tempDir    string
	cleanup    []func() error
	mu         sync.RWMutex
	ctx        context.Context
	cancel     context.CancelFunc
}

// NewClientTestSuite creates a new test suite using mcp-go client
func NewClientTestSuite(t *testing.T) (*ClientTestSuite, error) {
	// Create temporary directory for test artifacts
	tempDir, err := os.MkdirTemp("", "logmcp-e2e-client-")
	if err != nil {
		return nil, fmt.Errorf("failed to create temp dir: %w", err)
	}

	// Find the logmcp binary path
	binaryPath, err := findBinary()
	if err != nil {
		os.RemoveAll(tempDir)
		return nil, fmt.Errorf("failed to find logmcp binary: %w", err)
	}

	ctx, cancel := context.WithCancel(context.Background())

	suite := &ClientTestSuite{
		t:          t,
		tempDir:    tempDir,
		binaryPath: binaryPath,
		cleanup:    make([]func() error, 0),
		ctx:        ctx,
		cancel:     cancel,
	}

	// Add temp dir cleanup
	suite.addCleanup(func() error {
		return os.RemoveAll(tempDir)
	})

	t.Logf("Client test suite initialized with temp dir: %s", tempDir)
	t.Logf("Using logmcp binary at: %s", binaryPath)
	return suite, nil
}

// findBinary finds the logmcp binary for E2E testing
func findBinary() (string, error) {
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

// StartMCPServer starts the MCP server and initializes the client connection
func (s *ClientTestSuite) StartMCPServer() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Create stdio transport to communicate with logmcp serve
	stdioTransport := transport.NewStdio(s.binaryPath, nil, "serve")

	// Create the MCP client
	s.mcpClient = client.NewClient(stdioTransport)

	// Add cleanup for client
	s.addCleanup(func() error {
		if s.mcpClient != nil {
			return s.mcpClient.Close()
		}
		return nil
	})

	// Start the client (this launches the server process)
	if err := s.mcpClient.Start(s.ctx); err != nil {
		return fmt.Errorf("failed to start MCP client: %w", err)
	}

	// Initialize the MCP connection with proper protocol version
	initRequest := mcp.InitializeRequest{}
	initRequest.Params.ProtocolVersion = mcp.LATEST_PROTOCOL_VERSION
	initRequest.Params.ClientInfo = mcp.Implementation{
		Name:    "e2e-test-client",
		Version: "1.0.0",
	}
	initRequest.Params.Capabilities = mcp.ClientCapabilities{}

	serverInfo, err := s.mcpClient.Initialize(s.ctx, initRequest)
	if err != nil {
		return fmt.Errorf("failed to initialize MCP connection: %w", err)
	}

	s.t.Logf("MCP server initialized successfully: %+v", serverInfo)
	return nil
}

// ListTools returns all available tools from the server
func (s *ClientTestSuite) ListTools() (*mcp.ListToolsResult, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.mcpClient == nil {
		return nil, fmt.Errorf("MCP client not initialized")
	}

	request := mcp.ListToolsRequest{}
	return s.mcpClient.ListTools(s.ctx, request)
}

// CallTool executes a tool and returns the result
func (s *ClientTestSuite) CallTool(toolName string, arguments map[string]any) (*mcp.CallToolResult, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.mcpClient == nil {
		return nil, fmt.Errorf("MCP client not initialized")
	}

	request := mcp.CallToolRequest{}
	request.Params.Name = toolName
	request.Params.Arguments = arguments

	return s.mcpClient.CallTool(s.ctx, request)
}

// CallListSessions is a convenience method for calling the list_sessions tool
func (s *ClientTestSuite) CallListSessions() (map[string]any, error) {
	result, err := s.CallTool("list_sessions", map[string]any{})
	if err != nil {
		return nil, err
	}

	// Extract the JSON response from the tool result
	return s.extractJSONFromToolResult(result)
}

// CallGetLogs is a convenience method for calling the get_logs tool
func (s *ClientTestSuite) CallGetLogs(labels []string, options map[string]any) (map[string]any, error) {
	args := map[string]any{
		"labels": labels,
	}
	// Merge additional options
	for k, v := range options {
		args[k] = v
	}

	result, err := s.CallTool("get_logs", args)
	if err != nil {
		return nil, err
	}

	return s.extractJSONFromToolResult(result)
}

// CallStartProcess is a convenience method for calling the start_process tool
func (s *ClientTestSuite) CallStartProcess(command, label string, options map[string]any) (map[string]any, error) {
	args := map[string]any{
		"command": command,
		"label":   label,
	}
	// Set default working_dir if not provided
	if _, ok := options["working_dir"]; !ok {
		args["working_dir"] = s.tempDir
	}
	// Merge additional options
	for k, v := range options {
		args[k] = v
	}

	result, err := s.CallTool("start_process", args)
	if err != nil {
		return nil, err
	}

	return s.extractJSONFromToolResult(result)
}

// CallControlProcess is a convenience method for calling the control_process tool
func (s *ClientTestSuite) CallControlProcess(label, action string, signal string) (map[string]any, error) {
	args := map[string]any{
		"label":  label,
		"action": action,
	}
	if signal != "" {
		args["signal"] = signal
	}

	result, err := s.CallTool("control_process", args)
	if err != nil {
		return nil, err
	}

	return s.extractJSONFromToolResult(result)
}

// CallSendStdin is a convenience method for calling the send_stdin tool
func (s *ClientTestSuite) CallSendStdin(label, input string) (map[string]any, error) {
	args := map[string]any{
		"label": label,
		"input": input,
	}

	result, err := s.CallTool("send_stdin", args)
	if err != nil {
		return nil, err
	}

	return s.extractJSONFromToolResult(result)
}

// extractJSONFromToolResult extracts JSON data from MCP tool result
func (s *ClientTestSuite) extractJSONFromToolResult(result *mcp.CallToolResult) (map[string]any, error) {
	if result == nil {
		return nil, fmt.Errorf("nil tool result")
	}

	// MCP tools return content array with text items
	if len(result.Content) == 0 {
		return nil, fmt.Errorf("empty tool result content")
	}

	// Get first content item (should be text with JSON)
	firstContent := result.Content[0]

	// The content should be a TextContent
	var textContent string
	switch c := firstContent.(type) {
	case mcp.TextContent:
		textContent = c.Text
	case *mcp.TextContent:
		textContent = c.Text
	default:
		return nil, fmt.Errorf("unexpected content type: %T", firstContent)
	}

	// Parse JSON from text content
	var jsonData map[string]any
	if err := json.Unmarshal([]byte(textContent), &jsonData); err != nil {
		return nil, fmt.Errorf("failed to parse JSON response: %w", err)
	}

	return jsonData, nil
}

// addCleanup adds a cleanup function to be called on test completion
func (s *ClientTestSuite) addCleanup(cleanup func() error) {
	s.cleanup = append(s.cleanup, cleanup)
}

// Cleanup performs cleanup of all test resources
func (s *ClientTestSuite) Cleanup() {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Cancel context
	if s.cancel != nil {
		s.cancel()
	}

	// Execute cleanup functions in reverse order
	for i := len(s.cleanup) - 1; i >= 0; i-- {
		if err := s.cleanup[i](); err != nil {
			s.t.Logf("Cleanup error: %v", err)
		}
	}

	s.t.Logf("Client test suite cleanup completed")
}

// WaitForCondition waits for a condition to become true within a timeout
func (s *ClientTestSuite) WaitForCondition(condition func() bool, timeout time.Duration, message string) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if condition() {
			return true
		}
		time.Sleep(100 * time.Millisecond)
	}
	s.t.Errorf("Condition not met within %v: %s", timeout, message)
	return false
}
