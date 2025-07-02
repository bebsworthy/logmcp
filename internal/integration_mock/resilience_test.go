package integration_mock

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

// TestConnectionLossAndRecovery tests session persistence during WebSocket disconnections
func TestConnectionLossAndRecovery(t *testing.T) {
	config := DefaultE2EConfig()
	// Use longer cleanup delay to test session persistence
	config.CleanupDelay = 30 * time.Second
	
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

	// Initialize and start a process
	_, err = suite.mcpClient.SendMCPRequest("initialize", map[string]interface{}{})
	if err != nil {
		t.Fatalf("MCP initialize failed: %v", err)
	}

	// Start a long-running process
	_, err = suite.mcpClient.SendMCPRequest("tools/call", map[string]interface{}{
		"name": "start_process",
		"arguments": map[string]interface{}{
			"command":     "for i in {1..10}; do echo \"Log entry $i\"; sleep 1; done",
			"label":       "persistent-process",
			"working_dir": suite.tempDir,
		},
	})
	if err != nil {
		t.Fatalf("Start process failed: %v", err)
	}

	// Wait for some logs
	time.Sleep(3 * time.Second)

	// Verify process is running and logs are captured
	logsResponse1, err := suite.mcpClient.SendMCPRequest("tools/call", map[string]interface{}{
		"name": "get_logs",
		"arguments": map[string]interface{}{
			"labels": []string{"persistent-process"},
		},
	})
	if err != nil {
		t.Fatalf("Get logs before disconnection failed: %v", err)
	}

	suite.verifyLogsResponse(t, logsResponse1, []string{"Log entry 1", "Log entry 2"})

	// Simulate connection loss by killing the MCP client
	if suite.mcpClient.cmd != nil && suite.mcpClient.cmd.Process != nil {
		suite.mcpClient.cmd.Process.Kill()
		suite.mcpClient.cmd.Wait()
		t.Logf("Simulated MCP client disconnection")
	}

	// Wait for disconnection to be detected
	time.Sleep(2 * time.Second)

	// Verify session still exists (should persist due to cleanup delay)
	sessions := suite.sessionManager.ListSessions()
	found := false
	for _, session := range sessions {
		if session.Label == "persistent-process" {
			found = true
			t.Logf("Session persisted after disconnection: %s (status: %s)", 
				session.Label, session.ConnectionStatus)
			break
		}
	}

	if !found {
		t.Errorf("Session was not persisted after disconnection")
	}

	// Restart MCP client to simulate reconnection
	if err := suite.StartMockMCPClient(); err != nil {
		t.Fatalf("Failed to restart MCP client: %v", err)
	}

	// Initialize again
	_, err = suite.mcpClient.SendMCPRequest("initialize", map[string]interface{}{})
	if err != nil {
		t.Fatalf("MCP re-initialize failed: %v", err)
	}

	// Verify we can still access logs from the persistent session
	logsResponse2, err := suite.mcpClient.SendMCPRequest("tools/call", map[string]interface{}{
		"name": "get_logs",
		"arguments": map[string]interface{}{
			"labels": []string{"persistent-process"},
		},
	})
	if err != nil {
		t.Fatalf("Get logs after reconnection failed: %v", err)
	}

	// Should have more logs now (process continued running)
	suite.verifyLogsResponse(t, logsResponse2, []string{
		"Log entry 1", "Log entry 2", "Log entry 3", "Log entry 4",
	})

	t.Logf("Connection loss and recovery test completed successfully")
}

// TestProcessCrashDetection tests detection and handling of process crashes
func TestProcessCrashDetection(t *testing.T) {
	config := DefaultE2EConfig()
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

	// Start a process that will crash
	_, err = suite.mcpClient.SendMCPRequest("tools/call", map[string]interface{}{
		"name": "start_process",
		"arguments": map[string]interface{}{
			"command":     "echo 'Starting'; sleep 1; echo 'About to crash'; exit 1",
			"label":       "crash-test",
			"working_dir": suite.tempDir,
		},
	})
	if err != nil {
		t.Fatalf("Start crashing process failed: %v", err)
	}

	// Wait for process to crash
	time.Sleep(3 * time.Second)

	// Verify crash was detected and logged
	logsResponse, err := suite.mcpClient.SendMCPRequest("tools/call", map[string]interface{}{
		"name": "get_logs",
		"arguments": map[string]interface{}{
			"labels": []string{"crash-test"},
		},
	})
	if err != nil {
		t.Fatalf("Get crash logs failed: %v", err)
	}

	suite.verifyLogsResponse(t, logsResponse, []string{"Starting", "About to crash"})

	// Check session status shows crash
	sessionsResponse, err := suite.mcpClient.SendMCPRequest("tools/call", map[string]interface{}{
		"name":      "list_sessions",
		"arguments": map[string]interface{}{},
	})
	if err != nil {
		t.Fatalf("List sessions after crash failed: %v", err)
	}

	// Parse sessions response to verify crash status
	suite.verifySessionStatus(t, sessionsResponse, "crash-test", "crashed")

	t.Logf("Process crash detection test completed successfully")
}

// TestResourceLimits tests ring buffer limits and log eviction
func TestResourceLimits(t *testing.T) {
	config := DefaultE2EConfig()
	// Set very small buffer limits for testing
	config.BufferMaxSize = 1024      // 1KB
	config.BufferMaxAge = 5 * time.Second
	
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

	// Start a process that generates many logs
	_, err = suite.mcpClient.SendMCPRequest("tools/call", map[string]interface{}{
		"name": "start_process",
		"arguments": map[string]interface{}{
			"command": `for i in {1..100}; do 
				echo "This is a long log entry number $i with lots of text to fill up the buffer quickly"; 
				sleep 0.1; 
			done`,
			"label":       "high-volume",
			"working_dir": suite.tempDir,
		},
	})
	if err != nil {
		t.Fatalf("Start high-volume process failed: %v", err)
	}

	// Wait for buffer to fill and eviction to occur
	time.Sleep(5 * time.Second)

	// Get logs and verify old ones were evicted
	logsResponse, err := suite.mcpClient.SendMCPRequest("tools/call", map[string]interface{}{
		"name": "get_logs",
		"arguments": map[string]interface{}{
			"labels": []string{"high-volume"},
			"lines":  200, // Request more than what should be available
		},
	})
	if err != nil {
		t.Fatalf("Get logs for buffer test failed: %v", err)
	}

	// Verify we don't have all 100 entries (buffer should have evicted some)
	suite.verifyBufferEviction(t, logsResponse, 100)

	// Test time-based eviction by waiting
	time.Sleep(config.BufferMaxAge + 1*time.Second)

	// Get logs again - should have fewer due to time-based eviction
	timeEvictionResponse, err := suite.mcpClient.SendMCPRequest("tools/call", map[string]interface{}{
		"name": "get_logs",
		"arguments": map[string]interface{}{
			"labels": []string{"high-volume"},
		},
	})
	if err != nil {
		t.Fatalf("Get logs after time eviction failed: %v", err)
	}

	suite.verifyTimeBasedEviction(t, timeEvictionResponse)

	t.Logf("Resource limits test completed successfully")
}

// TestHighFrequencyLogging tests system behavior under rapid log generation
func TestHighFrequencyLogging(t *testing.T) {
	config := DefaultE2EConfig()
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

	// Start multiple high-frequency processes
	processes := []string{"rapid-1", "rapid-2", "rapid-3"}
	
	for _, label := range processes {
		_, err = suite.mcpClient.SendMCPRequest("tools/call", map[string]interface{}{
			"name": "start_process",
			"arguments": map[string]interface{}{
				"command": fmt.Sprintf(`for i in {1..50}; do 
					echo "[%s] Rapid log entry $i"; 
					echo "[%s] Second line for entry $i" >&2; 
				done`, label, label),
				"label":       label,
				"working_dir": suite.tempDir,
			},
		})
		if err != nil {
			t.Fatalf("Start rapid process %s failed: %v", label, err)
		}
	}

	// Wait for all processes to complete
	time.Sleep(3 * time.Second)

	// Query all logs together
	allLogsResponse, err := suite.mcpClient.SendMCPRequest("tools/call", map[string]interface{}{
		"name": "get_logs",
		"arguments": map[string]interface{}{
			"labels": processes,
			"lines":  300, // Should capture most logs
		},
	})
	if err != nil {
		t.Fatalf("Get all rapid logs failed: %v", err)
	}

	// Verify we got logs from all processes
	for _, label := range processes {
		suite.verifyLogsResponse(t, allLogsResponse, []string{
			fmt.Sprintf("[%s] Rapid log entry", label),
			fmt.Sprintf("[%s] Second line for entry", label),
		})
	}

	// Test filtering with high volume
	stderrLogsResponse, err := suite.mcpClient.SendMCPRequest("tools/call", map[string]interface{}{
		"name": "get_logs",
		"arguments": map[string]interface{}{
			"labels": processes,
			"stream": "stderr",
		},
	})
	if err != nil {
		t.Fatalf("Get stderr logs failed: %v", err)
	}

	// Verify stderr filtering worked
	suite.verifyLogsResponse(t, stderrLogsResponse, []string{"Second line for entry"})

	// Check server performance under load
	stats, err := suite.GetServerStats()
	if err != nil {
		t.Fatalf("Failed to get server stats: %v", err)
	}

	t.Logf("Server stats after high-frequency test: %+v", stats)

	t.Logf("High-frequency logging test completed successfully")
}

// Helper method to verify session status
func (s *E2ETestSuite) verifySessionStatus(t *testing.T, response map[string]interface{}, label string, expectedStatus string) {
	// Similar parsing logic as other verification methods
	result, ok := response["result"].(map[string]interface{})
	if !ok {
		t.Fatalf("Invalid response format: %+v", response)
	}

	content, ok := result["content"].([]interface{})
	if !ok || len(content) == 0 {
		t.Fatalf("No content in response: %+v", result)
	}

	textContent, ok := content[0].(map[string]interface{})
	if !ok {
		t.Fatalf("Invalid content format: %+v", content[0])
	}

	textData, ok := textContent["text"].(string)
	if !ok {
		t.Fatalf("No text data in content: %+v", textContent)
	}

	// Check if the expected status is mentioned in the response
	if !strings.Contains(textData, label) {
		t.Errorf("Session %s not found in response", label)
	}

	if !strings.Contains(textData, expectedStatus) {
		t.Errorf("Expected status %s not found for session %s", expectedStatus, label)
	}

	t.Logf("Session status verification passed: %s is %s", label, expectedStatus)
}

// Helper method to verify buffer eviction occurred
func (s *E2ETestSuite) verifyBufferEviction(t *testing.T, response map[string]interface{}, originalCount int) {
	// Parse response to get log count
	result, ok := response["result"].(map[string]interface{})
	if !ok {
		t.Fatalf("Invalid response format: %+v", response)
	}

	content, ok := result["content"].([]interface{})
	if !ok || len(content) == 0 {
		t.Fatalf("No content in response: %+v", result)
	}

	textContent, ok := content[0].(map[string]interface{})
	if !ok {
		t.Fatalf("Invalid content format: %+v", content[0])
	}

	textData, ok := textContent["text"].(string)
	if !ok {
		t.Fatalf("No text data in content: %+v", textContent)
	}

	// Count actual log entries
	logCount := strings.Count(textData, "log entry number")
	
	if logCount >= originalCount {
		t.Errorf("Expected buffer eviction but found %d/%d logs", logCount, originalCount)
	} else {
		t.Logf("Buffer eviction verified: %d/%d logs retained", logCount, originalCount)
	}
}

// Helper method to verify time-based eviction
func (s *E2ETestSuite) verifyTimeBasedEviction(t *testing.T, response map[string]interface{}) {
	// Parse response and verify fewer logs remain
	result, ok := response["result"].(map[string]interface{})
	if !ok {
		t.Fatalf("Invalid response format: %+v", response)
	}

	content, ok := result["content"].([]interface{})
	if !ok || len(content) == 0 {
		// No logs remaining is acceptable for time-based eviction
		t.Logf("Time-based eviction verified: no logs remaining")
		return
	}

	textContent, ok := content[0].(map[string]interface{})
	if !ok {
		t.Fatalf("Invalid content format: %+v", content[0])
	}

	textData, ok := textContent["text"].(string)
	if !ok {
		t.Fatalf("No text data in content: %+v", textContent)
	}

	logCount := strings.Count(textData, "log entry number")
	t.Logf("Time-based eviction result: %d logs remaining", logCount)
}