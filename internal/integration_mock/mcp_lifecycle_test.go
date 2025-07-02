package integration_mock

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// TestMCPLifecycle tests the complete MCP workflow:
// 1. LLM connects to LogMCP via MCP protocol
// 2. LLM starts a process via start_process tool
// 3. Process generates logs
// 4. LLM retrieves logs via get_logs tool  
// 5. LLM controls process via control_process tool
// 6. Process cleanup and session termination
func TestMCPLifecycle(t *testing.T) {
	config := DefaultE2EConfig()
	suite, err := NewE2ETestSuite(t, config)
	if err != nil {
		t.Fatalf("Failed to create E2E test suite: %v", err)
	}
	defer suite.Cleanup()

	// Step 1: Start LogMCP server
	if err := suite.StartLogMCPServer(config); err != nil {
		t.Fatalf("Failed to start LogMCP server: %v", err)
	}

	// Step 2: Start mock MCP client (simulates LLM)
	if err := suite.StartMockMCPClient(); err != nil {
		t.Fatalf("Failed to start mock MCP client: %v", err)
	}

	// Step 3: Initialize MCP connection
	initResponse, err := suite.mcpClient.SendMCPRequest("initialize", map[string]interface{}{})
	if err != nil {
		t.Fatalf("MCP initialize failed: %v", err)
	}
	
	t.Logf("MCP initialized: %+v", initResponse)

	// Step 4: List available tools
	toolsResponse, err := suite.mcpClient.SendMCPRequest("tools/list", map[string]interface{}{})
	if err != nil {
		t.Fatalf("Tools list failed: %v", err)
	}
	
	t.Logf("Available tools: %+v", toolsResponse)

	// Step 5: Start a test process via MCP
	startProcessResponse, err := suite.mcpClient.SendMCPRequest("tools/call", map[string]interface{}{
		"name": "start_process",
		"arguments": map[string]interface{}{
			"command":     "echo 'Hello from LogMCP'; sleep 2; echo 'Process completed'",
			"label":       "test-echo",
			"working_dir": suite.tempDir,
		},
	})
	if err != nil {
		t.Fatalf("Start process failed: %v", err)
	}
	
	t.Logf("Process started: %+v", startProcessResponse)

	// Step 6: Wait for process to generate some logs
	time.Sleep(1 * time.Second)

	// Step 7: Retrieve logs via MCP
	getLogsResponse, err := suite.mcpClient.SendMCPRequest("tools/call", map[string]interface{}{
		"name": "get_logs",
		"arguments": map[string]interface{}{
			"labels": []string{"test-echo"},
			"lines":  10,
			"stream": "both",
		},
	})
	if err != nil {
		t.Fatalf("Get logs failed: %v", err)
	}
	
	t.Logf("Retrieved logs: %+v", getLogsResponse)

	// Step 8: Verify logs contain expected content
	suite.verifyLogsResponse(t, getLogsResponse, []string{"Hello from LogMCP"})

	// Step 9: Wait for process to complete
	time.Sleep(3 * time.Second)

	// Step 10: Get final logs
	finalLogsResponse, err := suite.mcpClient.SendMCPRequest("tools/call", map[string]interface{}{
		"name": "get_logs",
		"arguments": map[string]interface{}{
			"labels": []string{"test-echo"},
			"lines":  10,
		},
	})
	if err != nil {
		t.Fatalf("Get final logs failed: %v", err)
	}

	// Step 11: Verify process completion logs
	suite.verifyLogsResponse(t, finalLogsResponse, []string{"Hello from LogMCP", "Process completed"})

	// Step 12: List sessions to verify session state
	listSessionsResponse, err := suite.mcpClient.SendMCPRequest("tools/call", map[string]interface{}{
		"name":      "list_sessions",
		"arguments": map[string]interface{}{},
	})
	if err != nil {
		t.Fatalf("List sessions failed: %v", err)
	}

	suite.verifySessionsResponse(t, listSessionsResponse, "test-echo")

	t.Logf("MCP lifecycle test completed successfully")
}

// TestMultiProcessWorkflow tests managing multiple processes simultaneously
func TestMultiProcessWorkflow(t *testing.T) {
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

	// Start multiple processes
	processes := []struct {
		label   string
		command string
	}{
		{"frontend", "echo 'Frontend starting'; sleep 1; echo 'Frontend ready on port 3000'"},
		{"backend", "echo 'Backend starting'; sleep 1; echo 'Backend API ready on port 8080'"},
		{"database", "echo 'Database starting'; sleep 1; echo 'Database accepting connections'"},
	}

	// Start all processes
	for _, proc := range processes {
		_, err := suite.mcpClient.SendMCPRequest("tools/call", map[string]interface{}{
			"name": "start_process",
			"arguments": map[string]interface{}{
				"command":     proc.command,
				"label":       proc.label,
				"working_dir": suite.tempDir,
			},
		})
		if err != nil {
			t.Fatalf("Failed to start process %s: %v", proc.label, err)
		}
		t.Logf("Started process: %s", proc.label)
	}

	// Wait for all processes to generate logs
	time.Sleep(3 * time.Second)

	// Query logs from all sessions
	allLogsResponse, err := suite.mcpClient.SendMCPRequest("tools/call", map[string]interface{}{
		"name": "get_logs",
		"arguments": map[string]interface{}{
			"labels": []string{"frontend", "backend", "database"},
			"lines":  20,
		},
	})
	if err != nil {
		t.Fatalf("Get all logs failed: %v", err)
	}

	// Verify we got logs from all processes
	expectedContent := []string{
		"Frontend starting", "Frontend ready",
		"Backend starting", "Backend API ready",
		"Database starting", "Database accepting",
	}
	suite.verifyLogsResponse(t, allLogsResponse, expectedContent)

	// Query logs from specific session
	backendLogsResponse, err := suite.mcpClient.SendMCPRequest("tools/call", map[string]interface{}{
		"name": "get_logs",
		"arguments": map[string]interface{}{
			"labels": []string{"backend"},
		},
	})
	if err != nil {
		t.Fatalf("Get backend logs failed: %v", err)
	}

	suite.verifyLogsResponse(t, backendLogsResponse, []string{"Backend starting", "Backend API ready"})

	t.Logf("Multi-process workflow test completed successfully")
}

// verifyLogsResponse verifies that a get_logs response contains expected content
func (s *E2ETestSuite) verifyLogsResponse(t *testing.T, response map[string]interface{}, expectedContent []string) {
	// Parse the response
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

	// Parse the JSON text data
	var logsData map[string]interface{}
	if err := json.Unmarshal([]byte(textData), &logsData); err != nil {
		t.Fatalf("Failed to parse logs data: %v", err)
	}

	// Extract logs array
	data, ok := logsData["data"].(map[string]interface{})
	if !ok {
		t.Fatalf("No data section in logs: %+v", logsData)
	}

	logs, ok := data["logs"].([]interface{})
	if !ok {
		t.Fatalf("No logs array in data: %+v", data)
	}

	// Build content string from all logs
	var allContent string
	for _, log := range logs {
		logEntry, ok := log.(map[string]interface{})
		if !ok {
			continue
		}
		content, ok := logEntry["content"].(string)
		if ok {
			allContent += content + " "
		}
	}

	// Verify expected content is present
	for _, expected := range expectedContent {
		if !strings.Contains(allContent, expected) {
			t.Errorf("Expected content %q not found in logs: %s", expected, allContent)
		}
	}

	t.Logf("Logs verification passed: found %d log entries", len(logs))
}

// verifySessionsResponse verifies that a list_sessions response contains expected session
func (s *E2ETestSuite) verifySessionsResponse(t *testing.T, response map[string]interface{}, expectedLabel string) {
	// Parse the response similar to logs response
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

	// Parse the JSON text data
	var sessionsData map[string]interface{}
	if err := json.Unmarshal([]byte(textData), &sessionsData); err != nil {
		t.Fatalf("Failed to parse sessions data: %v", err)
	}

	// Extract sessions array
	data, ok := sessionsData["data"].(map[string]interface{})
	if !ok {
		t.Fatalf("No data section in sessions: %+v", sessionsData)
	}

	sessions, ok := data["sessions"].([]interface{})
	if !ok {
		t.Fatalf("No sessions array in data: %+v", data)
	}

	// Look for expected session
	found := false
	for _, session := range sessions {
		sessionEntry, ok := session.(map[string]interface{})
		if !ok {
			continue
		}
		label, ok := sessionEntry["label"].(string)
		if ok && label == expectedLabel {
			found = true
			break
		}
	}

	if !found {
		t.Errorf("Expected session with label %q not found in sessions", expectedLabel)
	}

	t.Logf("Sessions verification passed: found session %q", expectedLabel)
}