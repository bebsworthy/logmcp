package e2e

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/client/transport"
	"github.com/mark3labs/mcp-go/mcp"
)

// TestMCPProtocolHandshake tests the full MCP initialization sequence
func TestMCPProtocolHandshake(t *testing.T) {
	t.Parallel()

	// Find the logmcp binary
	binaryPath, err := findLogMCPBinary()
	if err != nil {
		t.Fatalf("Failed to find binary: %v", err)
	}

	// Create context for this test
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Create stdio transport
	stdioTransport := transport.NewStdio(binaryPath, nil, "serve")

	// Create MCP client
	mcpClient := client.NewClient(stdioTransport)

	// Start client (launches server)
	if err := mcpClient.Start(ctx); err != nil {
		t.Fatalf("Failed to start MCP client: %v", err)
	}
	defer mcpClient.Close()

	// Test 1: Verify server responds to initialize request
	t.Run("Initialize", func(t *testing.T) {
		initRequest := mcp.InitializeRequest{}
		initRequest.Params.ProtocolVersion = mcp.LATEST_PROTOCOL_VERSION
		initRequest.Params.ClientInfo = mcp.Implementation{
			Name:    "protocol-test-client",
			Version: "1.0.0",
		}
		initRequest.Params.Capabilities = mcp.ClientCapabilities{}

		serverInfo, err := mcpClient.Initialize(ctx, initRequest)
		if err != nil {
			t.Fatalf("Failed to initialize: %v", err)
		}

		// Verify server info
		if serverInfo.ServerInfo.Name == "" {
			t.Error("Server name should not be empty")
		}
		if serverInfo.ServerInfo.Version == "" {
			t.Error("Server version should not be empty")
		}
		if serverInfo.ProtocolVersion != mcp.LATEST_PROTOCOL_VERSION {
			t.Errorf("Protocol version mismatch: got %s, expected %s",
				serverInfo.ProtocolVersion, mcp.LATEST_PROTOCOL_VERSION)
		}

		// Verify capabilities - server should have tools
		if serverInfo.Capabilities.Tools == nil {
			t.Error("Server should have tools capability")
		}

		t.Logf("Server initialized successfully: %s v%s",
			serverInfo.ServerInfo.Name, serverInfo.ServerInfo.Version)
	})

	// Test 2: Verify double initialization is handled correctly
	t.Run("DoubleInitialization", func(t *testing.T) {
		// Try to initialize again - this should either fail or be handled gracefully
		initRequest := mcp.InitializeRequest{}
		initRequest.Params.ProtocolVersion = mcp.LATEST_PROTOCOL_VERSION
		initRequest.Params.ClientInfo = mcp.Implementation{
			Name:    "protocol-test-client",
			Version: "1.0.0",
		}

		// This might fail or succeed depending on implementation
		// Just verify it doesn't crash the server
		_, err := mcpClient.Initialize(ctx, initRequest)
		if err != nil {
			t.Logf("Second initialization failed as expected: %v", err)
		} else {
			t.Log("Server handled double initialization gracefully")
		}

		// Verify server is still responsive
		callRequest := mcp.CallToolRequest{}
		callRequest.Params.Name = "list_sessions"
		callRequest.Params.Arguments = map[string]any{}

		_, err = mcpClient.CallTool(ctx, callRequest)
		if err != nil {
			t.Fatalf("Server became unresponsive after double initialization: %v", err)
		}
	})
}

// TestMCPProtocolErrors tests error handling for various protocol violations
func TestMCPProtocolErrors(t *testing.T) {
	t.Parallel()

	server := SetupTest(t)
	defer server.Cleanup()

	ctx := context.Background()

	// Test 1: Invalid tool name
	t.Run("InvalidToolName", func(t *testing.T) {
		callRequest := mcp.CallToolRequest{}
		callRequest.Params.Name = "non_existent_tool"
		callRequest.Params.Arguments = map[string]any{}

		_, err := server.MCPClient.CallTool(ctx, callRequest)
		if err == nil {
			t.Error("Expected error for non-existent tool")
		} else {
			t.Logf("Got expected error: %v", err)
		}
	})

	// Test 2: Missing required arguments
	t.Run("MissingRequiredArguments", func(t *testing.T) {
		callRequest := mcp.CallToolRequest{}
		callRequest.Params.Name = "get_logs"
		// Missing required "labels" argument
		callRequest.Params.Arguments = map[string]any{}

		_, err := server.MCPClient.CallTool(ctx, callRequest)
		if err == nil {
			t.Error("Expected error for missing required arguments")
		} else {
			t.Logf("Got expected error: %v", err)
		}
	})

	// Test 3: Invalid argument types
	t.Run("InvalidArgumentTypes", func(t *testing.T) {
		callRequest := mcp.CallToolRequest{}
		callRequest.Params.Name = "get_logs"
		// labels should be array, not string
		callRequest.Params.Arguments = map[string]any{
			"labels": "not-an-array",
		}

		_, err := server.MCPClient.CallTool(ctx, callRequest)
		if err == nil {
			t.Error("Expected error for invalid argument type")
		} else {
			t.Logf("Got expected error: %v", err)
		}
	})

	// Test 4: Empty tool name
	t.Run("EmptyToolName", func(t *testing.T) {
		callRequest := mcp.CallToolRequest{}
		callRequest.Params.Name = ""
		callRequest.Params.Arguments = map[string]any{}

		_, err := server.MCPClient.CallTool(ctx, callRequest)
		if err == nil {
			t.Error("Expected error for empty tool name")
		} else {
			t.Logf("Got expected error: %v", err)
		}
	})

	// Test 5: Invalid JSON in arguments
	t.Run("InvalidArgumentFormat", func(t *testing.T) {
		callRequest := mcp.CallToolRequest{}
		callRequest.Params.Name = "start_process"
		callRequest.Params.Arguments = map[string]any{
			"label": "test",
			// Command with invalid characters that might break parsing
			"command": string([]byte{0x00, 0x01, 0x02}),
		}

		_, err := server.MCPClient.CallTool(ctx, callRequest)
		if err == nil {
			// The server might handle this gracefully
			t.Log("Server handled invalid characters in command")
		} else {
			t.Logf("Got error for invalid characters: %v", err)
		}
	})

	// Test 6: Null arguments
	t.Run("NullArguments", func(t *testing.T) {
		callRequest := mcp.CallToolRequest{}
		callRequest.Params.Name = "list_sessions"
		callRequest.Params.Arguments = nil

		// This should work as list_sessions doesn't require arguments
		_, err := server.MCPClient.CallTool(ctx, callRequest)
		if err != nil {
			t.Errorf("list_sessions should work with null arguments: %v", err)
		}
	})
}

// TestMCPConcurrentRequests tests handling of multiple simultaneous requests
func TestMCPConcurrentRequests(t *testing.T) {
	t.Parallel()

	server := SetupTest(t)
	defer server.Cleanup()

	// Start some test processes first
	processes := []string{"concurrent-1", "concurrent-2", "concurrent-3"}
	for _, label := range processes {
		err := server.StartTestProcess(label, "sleep", "10")
		if err != nil {
			t.Fatalf("Failed to start process %s: %v", label, err)
		}
	}

	// Wait for all processes to be running
	if !server.WaitForCondition(5*time.Second, func() bool {
		sessions, _ := server.ListSessions()
		return len(sessions) >= len(processes)
	}) {
		t.Fatal("Not all processes started")
	}

	// Test 1: Concurrent list_sessions calls
	t.Run("ConcurrentListSessions", func(t *testing.T) {
		const numRequests = 10
		var wg sync.WaitGroup
		errors := make(chan error, numRequests)
		results := make(chan int, numRequests)

		for i := 0; i < numRequests; i++ {
			wg.Add(1)
			go func(id int) {
				defer wg.Done()

				sessions, err := server.ListSessions()
				if err != nil {
					errors <- fmt.Errorf("request %d failed: %w", id, err)
					return
				}
				results <- len(sessions)
			}(i)
		}

		wg.Wait()
		close(errors)
		close(results)

		// Check for errors
		for err := range errors {
			t.Errorf("Concurrent request error: %v", err)
		}

		// Verify all requests got the same result
		var firstResult int
		first := true
		for result := range results {
			if first {
				firstResult = result
				first = false
			} else if result != firstResult {
				t.Errorf("Inconsistent results: got %d and %d sessions", firstResult, result)
			}
		}

		t.Logf("All %d concurrent requests succeeded with %d sessions", numRequests, firstResult)
	})

	// Test 2: Concurrent get_logs calls
	t.Run("ConcurrentGetLogs", func(t *testing.T) {
		const numRequests = 8
		var wg sync.WaitGroup
		errors := make(chan error, numRequests)

		for i := 0; i < numRequests; i++ {
			wg.Add(1)
			go func(id int) {
				defer wg.Done()

				// Each request gets logs for a different label
				label := processes[id%len(processes)]
				_, err := server.GetLogs([]string{label})
				if err != nil {
					errors <- fmt.Errorf("get_logs %d for %s failed: %w", id, label, err)
				}
			}(i)
		}

		wg.Wait()
		close(errors)

		// Check for errors
		for err := range errors {
			t.Errorf("Concurrent get_logs error: %v", err)
		}
	})

	// Test 3: Mixed concurrent operations
	t.Run("MixedConcurrentOperations", func(t *testing.T) {
		var wg sync.WaitGroup
		errors := make(chan error, 20)

		// Start new processes
		for i := 0; i < 3; i++ {
			wg.Add(1)
			go func(id int) {
				defer wg.Done()
				label := fmt.Sprintf("mixed-test-%d", id)
				err := server.StartTestProcess(label, "echo", fmt.Sprintf("Test %d", id))
				if err != nil {
					errors <- fmt.Errorf("start_process %d failed: %w", id, err)
				}
			}(i)
		}

		// List sessions
		for i := 0; i < 5; i++ {
			wg.Add(1)
			go func(id int) {
				defer wg.Done()
				_, err := server.ListSessions()
				if err != nil {
					errors <- fmt.Errorf("list_sessions %d failed: %w", id, err)
				}
			}(i)
		}

		// Get logs
		for i := 0; i < 3; i++ {
			wg.Add(1)
			go func(id int) {
				defer wg.Done()
				_, err := server.GetLogs([]string{processes[id]})
				if err != nil {
					errors <- fmt.Errorf("get_logs %d failed: %w", id, err)
				}
			}(i)
		}

		// Control processes
		for i := 0; i < 2; i++ {
			wg.Add(1)
			go func(id int) {
				defer wg.Done()
				err := server.ControlProcess(processes[id], "signal", "SIGTERM")
				if err != nil {
					errors <- fmt.Errorf("control_process %d failed: %w", id, err)
				}
			}(i)
		}

		wg.Wait()
		close(errors)

		// Check for errors
		errorCount := 0
		for err := range errors {
			t.Errorf("Mixed operation error: %v", err)
			errorCount++
		}

		if errorCount == 0 {
			t.Log("All mixed concurrent operations completed successfully")
		}
	})

	// Test 4: Rapid sequential requests (stress test)
	t.Run("RapidSequentialRequests", func(t *testing.T) {
		const numRequests = 50
		start := time.Now()

		for i := 0; i < numRequests; i++ {
			_, err := server.ListSessions()
			if err != nil {
				t.Fatalf("Request %d failed: %v", i, err)
			}
		}

		elapsed := time.Since(start)
		requestsPerSecond := float64(numRequests) / elapsed.Seconds()
		t.Logf("Completed %d requests in %v (%.2f req/s)", numRequests, elapsed, requestsPerSecond)
	})
}

// TestMCPProtocolMalformedJSON tests handling of malformed JSON payloads
func TestMCPProtocolMalformedJSON(t *testing.T) {
	t.Parallel()

	// This test would require lower-level access to send raw malformed JSON
	// Since we're using the mcp-go client which handles JSON marshaling,
	// we can only test what the client allows us to send

	server := SetupTest(t)
	defer server.Cleanup()

	// Test 1: Extra large arguments
	t.Run("ExtraLargeArguments", func(t *testing.T) {
		// Create a very large string
		largeString := make([]byte, 1024*1024) // 1MB
		for i := range largeString {
			largeString[i] = 'A'
		}

		callRequest := mcp.CallToolRequest{}
		callRequest.Params.Name = "start_process"
		callRequest.Params.Arguments = map[string]any{
			"label":   "large-test",
			"command": string(largeString),
		}

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		_, err := server.MCPClient.CallTool(ctx, callRequest)
		if err != nil {
			t.Logf("Server rejected large argument as expected: %v", err)
		} else {
			t.Log("Server handled large argument")
		}
	})

	// Test 2: Deeply nested structures
	t.Run("DeeplyNestedArguments", func(t *testing.T) {
		// Create deeply nested structure
		nested := map[string]any{"level": 0}
		current := nested
		for i := 1; i < 100; i++ {
			next := map[string]any{"level": i}
			current["nested"] = next
			current = next
		}

		callRequest := mcp.CallToolRequest{}
		callRequest.Params.Name = "start_process"
		callRequest.Params.Arguments = map[string]any{
			"label":   "nested-test",
			"command": "echo test",
			"nested":  nested,
		}

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		_, err := server.MCPClient.CallTool(ctx, callRequest)
		// The server should either handle this or reject it gracefully
		if err != nil {
			t.Logf("Server handled deeply nested structure: %v", err)
		} else {
			t.Log("Server accepted deeply nested structure")
		}
	})

	// Test 3: Unicode and special characters
	t.Run("UnicodeAndSpecialCharacters", func(t *testing.T) {
		specialStrings := []string{
			"Hello 世界 🌍",
			"Test\nwith\nnewlines",
			"Test\twith\ttabs",
			"Test with \\backslashes\\",
			`Test with "quotes" and 'apostrophes'`,
			"Test with \x00 null bytes",
		}

		for i, str := range specialStrings {
			label := fmt.Sprintf("unicode-test-%d", i)
			err := server.StartTestProcess(label, "echo", str)
			if err != nil {
				t.Logf("Failed with string %d (%q): %v", i, str, err)
			} else {
				t.Logf("Successfully handled string %d: %q", i, str)
			}
		}
	})
}

// TestMCPProtocolContextCancellation tests proper handling of context cancellation
func TestMCPProtocolContextCancellation(t *testing.T) {
	t.Parallel()

	server := SetupTest(t)
	defer server.Cleanup()

	// Test 1: Cancel during request
	t.Run("CancelDuringRequest", func(t *testing.T) {
		// Start a long-running process
		err := server.StartTestProcess("long-runner", "sleep", "30")
		if err != nil {
			t.Fatalf("Failed to start process: %v", err)
		}

		// Create a context that we'll cancel
		ctx, cancel := context.WithCancel(context.Background())

		// Start a request in a goroutine
		done := make(chan error, 1)
		go func() {
			callRequest := mcp.CallToolRequest{}
			callRequest.Params.Name = "get_logs"
			callRequest.Params.Arguments = map[string]any{
				"labels": []string{"long-runner"},
			}

			_, err := server.MCPClient.CallTool(ctx, callRequest)
			done <- err
		}()

		// Cancel the context quickly
		time.Sleep(10 * time.Millisecond)
		cancel()

		// Wait for the request to complete
		select {
		case err := <-done:
			if err == nil {
				t.Log("Request completed before cancellation")
			} else if err == context.Canceled {
				t.Log("Request was properly cancelled")
			} else {
				t.Logf("Request failed with error: %v", err)
			}
		case <-time.After(2 * time.Second):
			t.Error("Request did not complete after cancellation")
		}

		// Verify server is still responsive
		sessions, err := server.ListSessions()
		if err != nil {
			t.Fatalf("Server became unresponsive after cancellation: %v", err)
		}
		t.Logf("Server still responsive, found %d sessions", len(sessions))
	})

	// Test 2: Timeout during request
	t.Run("TimeoutDuringRequest", func(t *testing.T) {
		// Use a very short timeout
		ctx, cancel := context.WithTimeout(context.Background(), 1*time.Millisecond)
		defer cancel()

		callRequest := mcp.CallToolRequest{}
		callRequest.Params.Name = "list_sessions"
		callRequest.Params.Arguments = map[string]any{}

		_, err := server.MCPClient.CallTool(ctx, callRequest)
		if err == nil {
			t.Log("Request completed before timeout")
		} else if err == context.DeadlineExceeded {
			t.Log("Request timed out as expected")
		} else {
			t.Logf("Request failed with error: %v", err)
		}

		// Verify server is still working
		ctx2 := context.Background()
		_, err = server.MCPClient.CallTool(ctx2, callRequest)
		if err != nil {
			t.Fatalf("Server not responding after timeout: %v", err)
		}
	})
}

// TestMCPProtocolInvalidInitialization tests various invalid initialization scenarios
func TestMCPProtocolInvalidInitialization(t *testing.T) {
	t.Parallel()

	// Find the logmcp binary
	binaryPath, err := findLogMCPBinary()
	if err != nil {
		t.Fatalf("Failed to find binary: %v", err)
	}

	// Test 1: Wrong protocol version
	t.Run("WrongProtocolVersion", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		stdioTransport := transport.NewStdio(binaryPath, nil, "serve")
		mcpClient := client.NewClient(stdioTransport)

		if err := mcpClient.Start(ctx); err != nil {
			t.Fatalf("Failed to start client: %v", err)
		}
		defer mcpClient.Close()

		initRequest := mcp.InitializeRequest{}
		initRequest.Params.ProtocolVersion = "1.0.0" // Invalid version (server expects 2025-03-26)
		initRequest.Params.ClientInfo = mcp.Implementation{
			Name:    "test-client",
			Version: "1.0.0",
		}

		result, err := mcpClient.Initialize(ctx, initRequest)
		if err != nil {
			t.Logf("Server rejected wrong protocol version as expected: %v", err)
		} else {
			// The server might negotiate the protocol version
			t.Logf("Server accepted protocol version %s and negotiated to %s",
				initRequest.Params.ProtocolVersion, result.ProtocolVersion)
			if result.ProtocolVersion == initRequest.Params.ProtocolVersion {
				t.Error("Server should not accept protocol version 1.0.0")
			}
		}
	})

	// Test 2: Missing client info
	t.Run("MissingClientInfo", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		stdioTransport := transport.NewStdio(binaryPath, nil, "serve")
		mcpClient := client.NewClient(stdioTransport)

		if err := mcpClient.Start(ctx); err != nil {
			t.Fatalf("Failed to start client: %v", err)
		}
		defer mcpClient.Close()

		initRequest := mcp.InitializeRequest{}
		initRequest.Params.ProtocolVersion = mcp.LATEST_PROTOCOL_VERSION
		// Missing ClientInfo

		_, err := mcpClient.Initialize(ctx, initRequest)
		// Server might accept this, just log the result
		if err != nil {
			t.Logf("Server rejected missing client info: %v", err)
		} else {
			t.Log("Server accepted missing client info")
		}
	})
}

// TestMCPProtocolLargeResponse tests handling of responses with large payloads
func TestMCPProtocolLargeResponse(t *testing.T) {
	t.Parallel()

	server := SetupTest(t)
	defer server.Cleanup()

	// Start a process that generates a lot of output
	err := server.StartTestProcess("verbose", "sh", "-c", `
		for i in $(seq 1 1000); do
			echo "This is log line number $i with some additional text to make it longer"
			# Add a small sleep to ensure logs are captured before process exits
			if [ $((i % 100)) -eq 0 ]; then sleep 0.1; fi
		done
	`)
	if err != nil {
		t.Fatalf("Failed to start verbose process: %v", err)
	}

	// Wait for logs to be generated
	time.Sleep(3 * time.Second)

	// Test retrieving large log response
	t.Run("LargeLogResponse", func(t *testing.T) {
		start := time.Now()
		logs, err := server.GetLogs([]string{"verbose"})
		elapsed := time.Since(start)

		if err != nil {
			t.Fatalf("Failed to get logs: %v", err)
		}

		t.Logf("Retrieved %d log entries in %v", len(logs), elapsed)

		// Verify we got logs (the exact number may vary based on timing)
		if len(logs) < 10 {
			t.Errorf("Expected at least 10 log entries, got %d", len(logs))
		}

		// Calculate approximate response size
		totalSize := 0
		for _, log := range logs {
			totalSize += len(log.Content) + len(log.Timestamp) + len(log.Stream) + len(log.Label)
		}
		t.Logf("Approximate response size: %d bytes", totalSize)
	})

	// Test with multiple sessions generating logs
	t.Run("MultipleLargeSessions", func(t *testing.T) {
		// Start more verbose processes
		for i := 0; i < 3; i++ {
			label := fmt.Sprintf("verbose-%d", i)
			err := server.StartTestProcess(label, "sh", "-c", `
				for i in $(seq 1 500); do
					echo "Process $$ line $i"
				done
			`)
			if err != nil {
				t.Fatalf("Failed to start process %s: %v", label, err)
			}
		}

		// Wait for logs
		time.Sleep(2 * time.Second)

		// Get logs from all sessions
		labels := []string{"verbose", "verbose-0", "verbose-1", "verbose-2"}
		start := time.Now()
		logs, err := server.GetLogs(labels)
		elapsed := time.Since(start)

		if err != nil {
			t.Fatalf("Failed to get logs from multiple sessions: %v", err)
		}

		t.Logf("Retrieved %d total log entries from %d sessions in %v",
			len(logs), len(labels), elapsed)
	})
}

// TestMCPProtocolConnectionResilience tests the protocol's resilience to connection issues
func TestMCPProtocolConnectionResilience(t *testing.T) {
	t.Parallel()

	server := SetupTest(t)
	defer server.Cleanup()

	// Start a test process
	err := server.StartTestProcess("resilience-test", "sleep", "30")
	if err != nil {
		t.Fatalf("Failed to start process: %v", err)
	}

	// Test rapid repeated calls
	t.Run("RapidRepeatedCalls", func(t *testing.T) {
		// Make many calls without waiting for responses
		const numCalls = 20
		errors := make([]error, numCalls)
		var wg sync.WaitGroup

		for i := 0; i < numCalls; i++ {
			wg.Add(1)
			go func(idx int) {
				defer wg.Done()
				_, err := server.ListSessions()
				errors[idx] = err
			}(i)
			// Very short delay to create overlapping requests
			time.Sleep(1 * time.Millisecond)
		}

		wg.Wait()

		// Count errors
		errorCount := 0
		for i, err := range errors {
			if err != nil {
				t.Logf("Call %d failed: %v", i, err)
				errorCount++
			}
		}

		if errorCount > numCalls/2 {
			t.Errorf("Too many errors (%d/%d) during rapid calls", errorCount, numCalls)
		} else {
			t.Logf("Handled rapid calls well: %d/%d succeeded", numCalls-errorCount, numCalls)
		}
	})

	// Verify server is still functional
	sessions, err := server.ListSessions()
	if err != nil {
		t.Fatalf("Server not responding after stress test: %v", err)
	}
	t.Logf("Server still functional with %d sessions", len(sessions))
}

// Benchmarking tests for protocol performance
func BenchmarkMCPProtocolListSessions(b *testing.B) {
	server := SetupTest(&testing.T{})
	defer server.Cleanup()

	// Start some test processes
	for i := 0; i < 5; i++ {
		server.StartTestProcess(fmt.Sprintf("bench-%d", i), "sleep", "60")
	}

	time.Sleep(1 * time.Second)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := server.ListSessions()
		if err != nil {
			b.Fatalf("Failed to list sessions: %v", err)
		}
	}
}

func BenchmarkMCPProtocolGetLogs(b *testing.B) {
	server := SetupTest(&testing.T{})
	defer server.Cleanup()

	// Start a process that generates logs
	server.StartTestProcess("bench-logger", "sh", "-c", `
		for i in $(seq 1 100); do
			echo "Benchmark log line $i"
		done
	`)

	time.Sleep(1 * time.Second)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := server.GetLogs([]string{"bench-logger"})
		if err != nil {
			b.Fatalf("Failed to get logs: %v", err)
		}
	}
}
