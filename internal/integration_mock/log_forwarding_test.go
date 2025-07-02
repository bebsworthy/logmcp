package integration_mock

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"testing"
	"time"
)

// TestLogFileForwarding tests forwarding logs from a file
func TestLogFileForwarding(t *testing.T) {
	config := DefaultE2EConfig()
	config.WebSocketPort = 18765 // Use fixed port for integration testing
	suite, err := NewE2ETestSuite(t, config)
	if err != nil {
		t.Fatalf("Failed to create E2E test suite: %v", err)
	}
	defer suite.Cleanup()

	// Start LogMCP infrastructure
	if err := suite.StartLogMCPServer(config); err != nil {
		t.Fatalf("Failed to start LogMCP server: %v", err)
	}

	if err := suite.StartMockMCPClient(); err != nil {
		t.Fatalf("Failed to start mock MCP client: %v", err)
	}

	// Initialize MCP
	_, err = suite.mcpClient.SendMCPRequest("initialize", map[string]interface{}{})
	if err != nil {
		t.Fatalf("MCP initialize failed: %v", err)
	}

	// Create a test log file
	logFile := filepath.Join(suite.tempDir, "app.log")
	file, err := os.Create(logFile)
	if err != nil {
		t.Fatalf("Failed to create log file: %v", err)
	}

	// Write initial log entries
	initialLogs := []string{
		"2025-07-02 10:00:01 [INFO] Application starting",
		"2025-07-02 10:00:02 [INFO] Loading configuration",
		"2025-07-02 10:00:03 [INFO] Database connection established",
		"2025-07-02 10:00:04 [INFO] Server listening on port 8080",
	}

	for _, log := range initialLogs {
		if _, err := file.WriteString(log + "\n"); err != nil {
			t.Fatalf("Failed to write to log file: %v", err)
		}
	}
	file.Close()

	// Start log forwarder using logmcp forward command
	forwarder, err := suite.StartTestProcess(
		"log-forwarder",
		suite.binaryPath, "forward",
		"--label", "app-logs",
		"--server-url", fmt.Sprintf("ws://localhost:%d", config.WebSocketPort),
		logFile,
	)
	if err != nil {
		t.Fatalf("Failed to start log forwarder: %v", err)
	}

	// Wait for forwarder to connect and send initial logs
	time.Sleep(2 * time.Second)

	// Query logs via MCP
	getLogsResponse, err := suite.mcpClient.SendMCPRequest("tools/call", map[string]interface{}{
		"name": "get_logs",
		"arguments": map[string]interface{}{
			"labels": []string{"app-logs"},
			"lines":  10,
		},
	})
	if err != nil {
		t.Fatalf("Get logs failed: %v", err)
	}

	// Verify initial logs are present
	expectedContent := []string{
		"Application starting",
		"Loading configuration", 
		"Database connection established",
		"Server listening on port 8080",
	}
	suite.verifyLogsResponse(t, getLogsResponse, expectedContent)

	// Append more logs to test real-time forwarding
	file, err = os.OpenFile(logFile, os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		t.Fatalf("Failed to reopen log file: %v", err)
	}

	newLogs := []string{
		"2025-07-02 10:00:05 [INFO] Processing first request",
		"2025-07-02 10:00:06 [WARN] High memory usage detected",
		"2025-07-02 10:00:07 [ERROR] Database connection timeout",
		"2025-07-02 10:00:08 [INFO] Database connection restored",
	}

	for _, log := range newLogs {
		if _, err := file.WriteString(log + "\n"); err != nil {
			t.Fatalf("Failed to append to log file: %v", err)
		}
		file.Sync() // Force write to disk
	}
	file.Close()

	// Wait for new logs to be forwarded
	time.Sleep(2 * time.Second)

	// Query logs again to verify real-time forwarding
	updatedLogsResponse, err := suite.mcpClient.SendMCPRequest("tools/call", map[string]interface{}{
		"name": "get_logs",
		"arguments": map[string]interface{}{
			"labels": []string{"app-logs"},
			"lines":  20,
		},
	})
	if err != nil {
		t.Fatalf("Get updated logs failed: %v", err)
	}

	// Verify new logs are included
	allExpectedContent := append(expectedContent, []string{
		"Processing first request",
		"High memory usage detected",
		"Database connection timeout", 
		"Database connection restored",
	}...)
	suite.verifyLogsResponse(t, updatedLogsResponse, allExpectedContent)

	// Test log filtering by stream type and pattern
	errorLogsResponse, err := suite.mcpClient.SendMCPRequest("tools/call", map[string]interface{}{
		"name": "get_logs",
		"arguments": map[string]interface{}{
			"labels":  []string{"app-logs"},
			"pattern": "ERROR|WARN",
		},
	})
	if err != nil {
		t.Fatalf("Get error logs failed: %v", err)
	}

	suite.verifyLogsResponse(t, errorLogsResponse, []string{
		"High memory usage detected",
		"Database connection timeout",
	})

	// Wait for forwarder logs and verify it's working
	if !forwarder.WaitForLogs(3, 5*time.Second) {
		t.Errorf("Log forwarder didn't generate expected logs")
	}

	forwarderLogs := forwarder.GetLogs()
	t.Logf("Log forwarder generated %d log lines", len(forwarderLogs))

	t.Logf("Log file forwarding test completed successfully")
}

// TestStdinLogForwarding tests forwarding logs from stdin
func TestStdinLogForwarding(t *testing.T) {
	config := DefaultE2EConfig()
	config.WebSocketPort = 18766 // Use fixed port for integration testing
	suite, err := NewE2ETestSuite(t, config)
	if err != nil {
		t.Fatalf("Failed to create E2E test suite: %v", err)
	}
	defer suite.Cleanup()

	// Start LogMCP infrastructure
	if err := suite.StartLogMCPServer(config); err != nil {
		t.Fatalf("Failed to start LogMCP server: %v", err)
	}

	if err := suite.StartMockMCPClient(); err != nil {
		t.Fatalf("Failed to start mock MCP client: %v", err)
	}

	// Initialize MCP
	_, err = suite.mcpClient.SendMCPRequest("initialize", map[string]interface{}{})
	if err != nil {
		t.Fatalf("MCP initialize failed: %v", err)
	}

	// Start stdin log forwarder
	forwarder, err := suite.StartTestProcess(
		"stdin-forwarder",
		suite.binaryPath, "forward",
		"--label", "stdin-logs",
		"--server-url", fmt.Sprintf("ws://localhost:%d", config.WebSocketPort),
		"stdin",
	)
	if err != nil {
		t.Fatalf("Failed to start stdin forwarder: %v", err)
	}

	// Wait for forwarder to connect
	time.Sleep(1 * time.Second)

	// Send logs to stdin
	testLogs := []string{
		"Starting application pipeline",
		"Step 1: Data validation completed",
		"Step 2: Processing user input",
		"Step 3: Generating report",
		"Pipeline completed successfully",
	}

	for _, log := range testLogs {
		if err := forwarder.SendInput(log); err != nil {
			t.Fatalf("Failed to send input to forwarder: %v", err)
		}
		time.Sleep(100 * time.Millisecond) // Small delay between inputs
	}

	// Wait for logs to be processed
	time.Sleep(2 * time.Second)

	// Query logs via MCP
	getLogsResponse, err := suite.mcpClient.SendMCPRequest("tools/call", map[string]interface{}{
		"name": "get_logs",
		"arguments": map[string]interface{}{
			"labels": []string{"stdin-logs"},
			"lines":  10,
		},
	})
	if err != nil {
		t.Fatalf("Get stdin logs failed: %v", err)
	}

	// Verify stdin logs are present
	suite.verifyLogsResponse(t, getLogsResponse, testLogs)

	// Test log querying with line limits
	limitedLogsResponse, err := suite.mcpClient.SendMCPRequest("tools/call", map[string]interface{}{
		"name": "get_logs",
		"arguments": map[string]interface{}{
			"labels": []string{"stdin-logs"},
			"lines":  3,
		},
	})
	if err != nil {
		t.Fatalf("Get limited logs failed: %v", err)
	}

	// Should get the last 3 logs
	suite.verifyLogsResponse(t, limitedLogsResponse, []string{
		"Step 3: Generating report",
		"Pipeline completed successfully",
	})

	t.Logf("Stdin log forwarding test completed successfully")
}

// TestLogFileRotation tests handling of log file rotation
func TestLogFileRotation(t *testing.T) {
	config := DefaultE2EConfig()
	config.WebSocketPort = 18767 // Use fixed port for integration testing
	suite, err := NewE2ETestSuite(t, config)
	if err != nil {
		t.Fatalf("Failed to create E2E test suite: %v", err)
	}
	defer suite.Cleanup()

	// Start LogMCP infrastructure
	if err := suite.StartLogMCPServer(config); err != nil {
		t.Fatalf("Failed to start LogMCP server: %v", err)
	}

	if err := suite.StartMockMCPClient(); err != nil {
		t.Fatalf("Failed to start mock MCP client: %v", err)
	}

	// Initialize MCP
	_, err = suite.mcpClient.SendMCPRequest("initialize", map[string]interface{}{})
	if err != nil {
		t.Fatalf("MCP initialize failed: %v", err)
	}

	// Create initial log file
	logFile := filepath.Join(suite.tempDir, "rotating.log")
	file, err := os.Create(logFile)
	if err != nil {
		t.Fatalf("Failed to create log file: %v", err)
	}

	// Write initial logs
	if _, err := file.WriteString("Initial log entry\n"); err != nil {
		t.Fatalf("Failed to write initial log: %v", err)
	}
	file.Close()

	// Start log forwarder
	forwarder, err := suite.StartTestProcess(
		"rotation-forwarder",
		suite.binaryPath, "forward",
		"--label", "rotating-logs",
		"--server-url", fmt.Sprintf("ws://localhost:%d", config.WebSocketPort),
		logFile,
	)
	if err != nil {
		t.Fatalf("Failed to start log forwarder: %v", err)
	}

	// Wait for initial forwarding
	time.Sleep(1 * time.Second)

	// Simulate log rotation: move old file and create new one
	rotatedFile := logFile + ".old"
	if err := os.Rename(logFile, rotatedFile); err != nil {
		t.Fatalf("Failed to rotate log file: %v", err)
	}

	// Create new log file
	newFile, err := os.Create(logFile)
	if err != nil {
		t.Fatalf("Failed to create new log file: %v", err)
	}

	// Write to new file
	if _, err := newFile.WriteString("Log after rotation\n"); err != nil {
		t.Fatalf("Failed to write to new log file: %v", err)
	}
	newFile.Close()

	// Wait for rotation detection and new logs
	time.Sleep(2 * time.Second)

	// Query logs to verify rotation handling
	getLogsResponse, err := suite.mcpClient.SendMCPRequest("tools/call", map[string]interface{}{
		"name": "get_logs",
		"arguments": map[string]interface{}{
			"labels": []string{"rotating-logs"},
			"lines":  10,
		},
	})
	if err != nil {
		t.Fatalf("Get logs after rotation failed: %v", err)
	}

	// Verify both old and new logs are captured
	suite.verifyLogsResponse(t, getLogsResponse, []string{
		"Initial log entry",
		"Log after rotation",
	})

	// Verify forwarder detected rotation
	forwarderLogs := forwarder.GetLogs()
	rotationDetected := false
	for _, log := range forwarderLogs {
		if regexp.MustCompile("rotation detected|reopening").MatchString(log) {
			rotationDetected = true
			break
		}
	}

	if !rotationDetected {
		t.Logf("Warning: Log rotation detection not found in forwarder logs")
		t.Logf("Forwarder logs: %v", forwarderLogs)
	}

	t.Logf("Log file rotation test completed successfully")
}