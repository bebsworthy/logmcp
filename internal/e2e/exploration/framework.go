// Package e2e provides true end-to-end testing of LogMCP using real MCP server processes.
//
// These tests start actual MCP server processes and communicate with them via
// stdin/stdout using the MCP protocol. They verify:
//
// - Real MCP protocol implementation (JSON-RPC 2.0)
// - Actual MCP server responses to tools/list, tools/call, etc.
// - True integration between MCP server and session manager
// - Real session lifecycle management
// - Actual log forwarding and WebSocket connectivity
//
// Unlike integration_mock tests, these tests provide real confidence that
// the MCP protocol implementation works correctly with actual LLM clients.
package e2e

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// E2ETestSuite provides true end-to-end testing using real MCP server processes
type E2ETestSuite struct {
	t          *testing.T
	mcpCmd     *exec.Cmd
	mcpStdin   io.WriteCloser
	mcpStdout  io.ReadCloser
	mcpStderr  io.ReadCloser
	binaryPath string
	tempDir    string
	cleanup    []func() error
	mu         sync.RWMutex
	reqID      int
}

// MCPRequest represents a JSON-RPC 2.0 request
type MCPRequest struct {
	JSONRpc string      `json:"jsonrpc"`
	ID      int         `json:"id"`
	Method  string      `json:"method"`
	Params  interface{} `json:"params,omitempty"`
}

// MCPResponse represents a JSON-RPC 2.0 response
type MCPResponse struct {
	JSONRpc string      `json:"jsonrpc"`
	ID      int         `json:"id"`
	Result  interface{} `json:"result,omitempty"`
	Error   interface{} `json:"error,omitempty"`
}

// NewE2ETestSuite creates a new true end-to-end test suite
func NewE2ETestSuite(t *testing.T) (*E2ETestSuite, error) {
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
		reqID:      1,
	}

	// Add temp dir cleanup
	suite.addCleanup(func() error {
		return os.RemoveAll(tempDir)
	})

	t.Logf("True E2E test suite initialized with temp dir: %s", tempDir)
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

// StartMCPServer starts the real MCP server process
func (s *E2ETestSuite) StartMCPServer() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Start the MCP server in serve mode
	s.mcpCmd = exec.Command(s.binaryPath, "serve")
	s.mcpCmd.Dir = s.tempDir

	// Set up pipes for communication
	stdin, err := s.mcpCmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("failed to create stdin pipe: %w", err)
	}
	s.mcpStdin = stdin

	stdout, err := s.mcpCmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("failed to create stdout pipe: %w", err)
	}
	s.mcpStdout = stdout

	stderr, err := s.mcpCmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("failed to create stderr pipe: %w", err)
	}
	s.mcpStderr = stderr

	// Start the process
	if err := s.mcpCmd.Start(); err != nil {
		return fmt.Errorf("failed to start MCP server: %w", err)
	}

	// Add cleanup for MCP server
	s.addCleanup(func() error {
		if s.mcpCmd != nil && s.mcpCmd.Process != nil {
			s.mcpCmd.Process.Kill()
			s.mcpCmd.Wait()
		}
		return nil
	})

	// Give the server a moment to start
	time.Sleep(100 * time.Millisecond)

	s.t.Logf("Real MCP server started successfully (PID: %d)", s.mcpCmd.Process.Pid)
	return nil
}

// SendMCPRequest sends a JSON-RPC 2.0 request to the real MCP server and waits for response
func (s *E2ETestSuite) SendMCPRequest(method string, params interface{}) (*MCPResponse, error) {
	s.mu.Lock()
	reqID := s.reqID
	s.reqID++
	s.mu.Unlock()

	// Create the request
	request := MCPRequest{
		JSONRpc: "2.0",
		ID:      reqID,
		Method:  method,
		Params:  params,
	}

	// Marshal to JSON
	requestJSON, err := json.Marshal(request)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	// Send request to MCP server
	if _, err := s.mcpStdin.Write(append(requestJSON, '\n')); err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}

	s.t.Logf("Sent MCP request: %s", string(requestJSON))

	// Read response with timeout
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	responseChan := make(chan *MCPResponse, 1)
	errorChan := make(chan error, 1)

	go func() {
		scanner := bufio.NewScanner(s.mcpStdout)
		for scanner.Scan() {
			line := scanner.Text()
			s.t.Logf("Received line from MCP server: %s", line)
			
			// Skip non-JSON lines (like startup messages)
			if !json.Valid([]byte(line)) {
				continue
			}

			var response MCPResponse
			if err := json.Unmarshal([]byte(line), &response); err != nil {
				s.t.Logf("Failed to parse JSON response: %v", err)
				continue
			}
			
			// Check if this response is for our request
			if response.ID == reqID {
				responseChan <- &response
				return
			}
		}
		errorChan <- fmt.Errorf("failed to read valid JSON response from MCP server")
	}()

	select {
	case response := <-responseChan:
		return response, nil
	case err := <-errorChan:
		return nil, err
	case <-ctx.Done():
		return nil, fmt.Errorf("timeout waiting for MCP response")
	}
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

	s.t.Logf("True E2E test suite cleanup completed")
}