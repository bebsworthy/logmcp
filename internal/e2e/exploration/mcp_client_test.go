package e2e

import (
	"testing"
	"time"
)

// TestMCPClientProtocolBasics tests basic MCP protocol functionality using mcp-go client
func TestMCPClientProtocolBasics(t *testing.T) {
	suite, err := NewClientTestSuite(t)
	if err != nil {
		t.Fatalf("Failed to create client test suite: %v", err)
	}
	defer suite.Cleanup()

	// Start the MCP server and initialize connection
	if err := suite.StartMCPServer(); err != nil {
		t.Fatalf("Failed to start MCP server: %v", err)
	}

	// Test 1: Skip tools/list due to known mcp-go v0.32.0 marshaling bug
	t.Run("ListTools", func(t *testing.T) {
		t.Skip("Skipping tools/list test due to known mcp-go v0.32.0 marshaling bug - see TOOLS_LIST_ISSUE.md")
		
		// Note: When the bug is fixed, uncomment the test below:
		/*
		result, err := suite.ListTools()
		if err != nil {
			t.Fatalf("Failed to list tools: %v", err)
		}

		if result == nil || result.Tools == nil {
			t.Fatal("No tools returned from server")
		}

		// Verify we have the expected tools
		expectedTools := map[string]bool{
			"list_sessions":   false,
			"get_logs":        false,
			"start_process":   false,
			"control_process": false,
			"send_stdin":      false,
		}

		for _, tool := range result.Tools {
			if _, expected := expectedTools[tool.Name]; expected {
				expectedTools[tool.Name] = true
				t.Logf("Found tool: %s - %s", tool.Name, tool.Description)
			}
		}

		// Check all expected tools were found
		for toolName, found := range expectedTools {
			if !found {
				t.Errorf("Expected tool %s not found", toolName)
			}
		}
		*/
	})

	// Test 2: Call list_sessions tool
	t.Run("ListSessions", func(t *testing.T) {
		result, err := suite.CallListSessions()
		if err != nil {
			t.Fatalf("Failed to call list_sessions: %v", err)
		}

		// Verify response structure
		success, ok := result["success"].(bool)
		if !ok || !success {
			t.Fatalf("list_sessions did not return success: %+v", result)
		}

		data, ok := result["data"].(map[string]any)
		if !ok {
			t.Fatalf("list_sessions response missing data field: %+v", result)
		}

		sessions, ok := data["sessions"].([]any)
		if !ok {
			t.Fatalf("list_sessions data missing sessions array: %+v", data)
		}

		t.Logf("Found %d sessions", len(sessions))
	})
}

// TestMCPClientProcessLifecycle tests process management via MCP
func TestMCPClientProcessLifecycle(t *testing.T) {
	suite, err := NewClientTestSuite(t)
	if err != nil {
		t.Fatalf("Failed to create client test suite: %v", err)
	}
	defer suite.Cleanup()

	// Start the MCP server and initialize connection
	if err := suite.StartMCPServer(); err != nil {
		t.Fatalf("Failed to start MCP server: %v", err)
	}

	// Test: Complete process lifecycle
	t.Run("ProcessLifecycle", func(t *testing.T) {
		// Start a process
		startResult, err := suite.CallStartProcess(
			"echo 'Hello from MCP client test'",
			"test-echo",
			map[string]any{},
		)
		if err != nil {
			t.Fatalf("Failed to start process: %v", err)
		}

		// Verify start was successful
		success, ok := startResult["success"].(bool)
		if !ok || !success {
			t.Fatalf("Process start not successful: %+v", startResult)
		}

		// Wait a moment for process to generate logs
		time.Sleep(500 * time.Millisecond)

		// Get logs from the process
		logsResult, err := suite.CallGetLogs(
			[]string{"test-echo"},
			map[string]any{"lines": 10},
		)
		if err != nil {
			t.Fatalf("Failed to get logs: %v", err)
		}

		// Verify we got logs
		success, ok = logsResult["success"].(bool)
		if !ok || !success {
			t.Fatalf("get_logs not successful: %+v", logsResult)
		}

		data, ok := logsResult["data"].(map[string]any)
		if !ok {
			t.Fatalf("get_logs response missing data: %+v", logsResult)
		}

		logs, ok := data["logs"].([]any)
		if !ok {
			t.Fatalf("get_logs data missing logs array: %+v", data)
		}

		// Verify we got the expected log
		foundHello := false
		for _, logEntry := range logs {
			log, ok := logEntry.(map[string]any)
			if !ok {
				continue
			}
			content, ok := log["content"].(string)
			if ok {
				t.Logf("Log content: %s", content)
				if content == "'Hello from MCP client test'" {
					foundHello = true
					t.Logf("Found expected log: %s", content)
				}
			}
		}

		if !foundHello {
			t.Error("Did not find expected 'Hello from MCP client test' in logs")
		}

		// List sessions to verify our process is tracked
		sessionsResult, err := suite.CallListSessions()
		if err != nil {
			t.Fatalf("Failed to list sessions: %v", err)
		}

		// Find our session
		sessionsData, _ := sessionsResult["data"].(map[string]any)
		sessions, _ := sessionsData["sessions"].([]any)
		
		foundSession := false
		for _, sessionEntry := range sessions {
			session, ok := sessionEntry.(map[string]any)
			if !ok {
				continue
			}
			label, _ := session["label"].(string)
			if label == "test-echo" {
				foundSession = true
				t.Logf("Found session: %+v", session)
			}
		}

		if !foundSession {
			t.Error("Did not find test-echo session in list")
		}
	})
}

// TestMCPClientAllTools tests all available MCP tools
func TestMCPClientAllTools(t *testing.T) {
	suite, err := NewClientTestSuite(t)
	if err != nil {
		t.Fatalf("Failed to create client test suite: %v", err)
	}
	defer suite.Cleanup()

	// Start the MCP server and initialize connection
	if err := suite.StartMCPServer(); err != nil {
		t.Fatalf("Failed to start MCP server: %v", err)
	}

	// Test: start_process with various options
	t.Run("StartProcessWithOptions", func(t *testing.T) {
		result, err := suite.CallStartProcess(
			"sh -c 'echo Starting; sleep 1; echo Running; sleep 1; echo Done'",
			"long-runner",
			map[string]any{
				"environment": map[string]any{
					"TEST_VAR": "test_value",
				},
			},
		)
		if err != nil {
			t.Fatalf("Failed to start process with options: %v", err)
		}

		success, _ := result["success"].(bool)
		if !success {
			t.Fatalf("Process start failed: %+v", result)
		}
	})

	// Test: control_process - send signal
	t.Run("ControlProcessSignal", func(t *testing.T) {
		// Start a long-running process
		_, err := suite.CallStartProcess(
			"sh -c 'while true; do echo Running; sleep 1; done'",
			"signal-test",
			map[string]any{},
		)
		if err != nil {
			t.Fatalf("Failed to start process for signal test: %v", err)
		}

		// Wait for process to start and generate some logs
		time.Sleep(1 * time.Second)

		// Send SIGTERM
		result, err := suite.CallControlProcess("signal-test", "signal", "SIGTERM")
		if err != nil {
			t.Fatalf("Failed to send signal: %v", err)
		}

		success, _ := result["success"].(bool)
		if !success {
			t.Fatalf("Signal send failed: %+v", result)
		}
	})

	// Test: send_stdin
	t.Run("SendStdin", func(t *testing.T) {
		// Start an interactive process
		_, err := suite.CallStartProcess(
			"sh -c 'read input; echo You entered: $input'",
			"stdin-test",
			map[string]any{},
		)
		if err != nil {
			t.Fatalf("Failed to start interactive process: %v", err)
		}

		// Send input
		result, err := suite.CallSendStdin("stdin-test", "Hello stdin!")
		if err != nil {
			t.Fatalf("Failed to send stdin: %v", err)
		}

		success, _ := result["success"].(bool)
		if !success {
			t.Fatalf("Send stdin failed: %+v", result)
		}

		// Wait for process to handle input
		time.Sleep(500 * time.Millisecond)

		// Get logs to verify input was processed
		logsResult, err := suite.CallGetLogs(
			[]string{"stdin-test"},
			map[string]any{},
		)
		if err != nil {
			t.Fatalf("Failed to get logs after stdin: %v", err)
		}

		data, _ := logsResult["data"].(map[string]any)
		logs, _ := data["logs"].([]any)

		foundEcho := false
		for _, logEntry := range logs {
			log, _ := logEntry.(map[string]any)
			content, _ := log["content"].(string)
			if content == "You entered: Hello stdin!" {
				foundEcho = true
				t.Log("Successfully verified stdin was processed")
			}
		}

		if !foundEcho {
			t.Error("Did not find expected echo of stdin input")
		}
	})

	// Test: get_logs with filtering
	t.Run("GetLogsWithFiltering", func(t *testing.T) {
		// Create multiple processes
		for i := 1; i <= 3; i++ {
			_, err := suite.CallStartProcess(
				"sh -c 'echo Starting process; echo ERROR: Test error; echo Process done'",
				"filter-test",
				map[string]any{},
			)
			if err != nil {
				t.Fatalf("Failed to start process %d: %v", i, err)
			}
		}

		// Wait for logs
		time.Sleep(500 * time.Millisecond)

		// Get logs with pattern filter
		result, err := suite.CallGetLogs(
			[]string{"filter-test"},
			map[string]any{
				"pattern": "ERROR",
				"lines":   100,
			},
		)
		if err != nil {
			t.Fatalf("Failed to get filtered logs: %v", err)
		}

		data, _ := result["data"].(map[string]any)
		logs, _ := data["logs"].([]any)

		// All logs should contain ERROR
		errorCount := 0
		for _, logEntry := range logs {
			log, _ := logEntry.(map[string]any)
			content, _ := log["content"].(string)
			if content == "ERROR: Test error" {
				errorCount++
			}
		}

		if errorCount == 0 {
			t.Error("Pattern filter did not return any ERROR logs")
		} else {
			t.Logf("Found %d ERROR logs with pattern filter", errorCount)
		}
	})
}