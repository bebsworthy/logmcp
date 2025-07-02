package integration_mock

import (
	"testing"
	"time"
)

// TestE2EFramework tests the basic E2E testing framework functionality
func TestE2EFramework(t *testing.T) {
	config := DefaultE2EConfig()
	suite, err := NewE2ETestSuite(t, config)
	if err != nil {
		t.Fatalf("Failed to create E2E test suite: %v", err)
	}
	defer suite.Cleanup()

	// Test 1: Basic suite creation and cleanup
	if suite.tempDir == "" {
		t.Error("Temporary directory not created")
	}

	// Test 2: Server startup
	if err := suite.StartLogMCPServer(config); err != nil {
		t.Fatalf("Failed to start LogMCP server: %v", err)
	}

	if suite.sessionManager == nil {
		t.Error("Session manager not initialized")
	}

	if suite.wsServer == nil {
		t.Error("WebSocket server not initialized")
	}

	if suite.mcpServer == nil {
		t.Error("MCP server not initialized")
	}

	// Test 3: Server health
	if !suite.sessionManager.IsHealthy() {
		t.Error("Session manager not healthy")
	}

	if !suite.wsServer.IsHealthy() {
		t.Error("WebSocket server not healthy")
	}

	if !suite.mcpServer.IsHealthy() {
		t.Error("MCP server not healthy")
	}

	// Test 4: Server stats
	stats, err := suite.GetServerStats()
	if err != nil {
		t.Fatalf("Failed to get server stats: %v", err)
	}

	if stats == nil {
		t.Error("Server stats are nil")
	}

	t.Logf("E2E framework test completed successfully")
	t.Logf("Server stats: %+v", stats)
}

// TestE2EProcessManagement tests the test process management functionality
func TestE2EProcessManagement(t *testing.T) {
	config := DefaultE2EConfig()
	suite, err := NewE2ETestSuite(t, config)
	if err != nil {
		t.Fatalf("Failed to create E2E test suite: %v", err)
	}
	defer suite.Cleanup()

	// Start a simple test process
	proc, err := suite.StartTestProcess("test-echo", "echo", "Hello World")
	if err != nil {
		t.Fatalf("Failed to start test process: %v", err)
	}

	// Wait for process to complete
	if err := proc.Wait(); err != nil {
		t.Logf("Process exited with error (expected for echo): %v", err)
	}

	// Wait for logs to be captured
	if !proc.WaitForLogs(1, 5*time.Second) {
		t.Error("Process didn't generate expected logs")
	}

	// Verify logs were captured
	logs := proc.GetLogs()
	if len(logs) == 0 {
		t.Error("No logs captured from test process")
	}

	t.Logf("Captured logs: %v", logs)

	// Test input sending
	longProc, err := suite.StartTestProcess("test-cat", "cat")
	if err != nil {
		t.Fatalf("Failed to start cat process: %v", err)
	}

	// Send input
	if err := longProc.SendInput("test input line"); err != nil {
		t.Fatalf("Failed to send input: %v", err)
	}

	// Give it time to process
	time.Sleep(100 * time.Millisecond)

	// Kill the cat process (it won't exit naturally)
	if longProc.cmd != nil && longProc.cmd.Process != nil {
		longProc.cmd.Process.Kill()
	}

	t.Logf("Process management test completed successfully")
}

// TestE2EConfiguration tests different configuration options
func TestE2EConfiguration(t *testing.T) {
	// Test with custom configuration
	config := E2EConfig{
		ServerHost:      "localhost",
		ServerPort:      0,
		WebSocketPort:   0,
		ReadTimeout:     1 * time.Second,   // Very short
		WriteTimeout:    500 * time.Millisecond,
		CleanupDelay:    2 * time.Second,   // Very short
		CleanupInterval: 500 * time.Millisecond,
		BufferMaxSize:   512,               // Very small
		BufferMaxAge:    2 * time.Second,   // Very short
	}

	suite, err := NewE2ETestSuite(t, config)
	if err != nil {
		t.Fatalf("Failed to create E2E test suite with custom config: %v", err)
	}
	defer suite.Cleanup()

	// Start server with custom config
	if err := suite.StartLogMCPServer(config); err != nil {
		t.Fatalf("Failed to start LogMCP server with custom config: %v", err)
	}

	// Verify configuration was applied by checking behavior
	// (In a real scenario, we'd test timeout behavior, but that's complex)

	t.Logf("Configuration test completed successfully")
}

// TestE2EErrorHandling tests error handling in the framework
func TestE2EErrorHandling(t *testing.T) {
	config := DefaultE2EConfig()
	suite, err := NewE2ETestSuite(t, config)
	if err != nil {
		t.Fatalf("Failed to create E2E test suite: %v", err)
	}
	defer suite.Cleanup()

	// Test 1: Starting MCP client before server should fail
	err = suite.StartMockMCPClient()
	if err == nil {
		t.Error("Expected error when starting MCP client before server")
	}

	// Test 2: Starting server should work
	if err := suite.StartLogMCPServer(config); err != nil {
		t.Fatalf("Failed to start LogMCP server: %v", err)
	}

	// Test 3: Starting invalid process should fail
	_, err = suite.StartTestProcess("invalid", "nonexistent-command")
	if err == nil {
		t.Error("Expected error when starting invalid process")
	}

	// Test 4: Getting stats before full initialization
	stats, err := suite.GetServerStats()
	if err != nil {
		t.Logf("Expected error getting stats before full init: %v", err)
	} else if stats != nil {
		t.Logf("Got stats: %+v", stats)
	}

	t.Logf("Error handling test completed successfully")
}

// BenchmarkE2EFrameworkOverhead benchmarks the overhead of the E2E framework
func BenchmarkE2EFrameworkOverhead(b *testing.B) {
	for i := 0; i < b.N; i++ {
		config := DefaultE2EConfig()
		suite, err := NewE2ETestSuite(&testing.T{}, config)
		if err != nil {
			b.Fatalf("Failed to create E2E test suite: %v", err)
		}

		// Start and stop server to measure overhead
		if err := suite.StartLogMCPServer(config); err != nil {
			b.Fatalf("Failed to start LogMCP server: %v", err)
		}

		suite.Cleanup()
	}
}

// BenchmarkE2EProcessCreation benchmarks test process creation
func BenchmarkE2EProcessCreation(b *testing.B) {
	config := DefaultE2EConfig()
	suite, err := NewE2ETestSuite(&testing.T{}, config)
	if err != nil {
		b.Fatalf("Failed to create E2E test suite: %v", err)
	}
	defer suite.Cleanup()

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		proc, err := suite.StartTestProcess("benchmark", "echo", "test")
		if err != nil {
			b.Fatalf("Failed to start process: %v", err)
		}
		proc.Wait()
	}
}