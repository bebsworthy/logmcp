package integration

import (
	"testing"
	"time"

	"github.com/bebsworthy/logmcp/internal/runner"
)

// TestNoOutputProcessShutdown tests that processes with no output can be shut down cleanly
func TestNoOutputProcessShutdown(t *testing.T) {
	server := NewTestWebSocketServer(t)
	defer server.Close()

	// Test with various no-output commands
	testCases := []struct {
		name    string
		command string
		label   string
	}{
		{"Sleep", "sleep 3600", "sleep-test"},
		{"TailWait", "tail -f /dev/null", "tail-test"},
		{"CatWait", "cat", "cat-test"}, // cat with no input waits forever
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Create process runner
			pr := runner.NewProcessRunner(tc.command, tc.label)
			
			// Create and set WebSocket client
			client := runner.NewWebSocketClient(server.WebSocketURL(), tc.label)
			client.SetCommand(tc.command, "/tmp", []string{"process"})
			pr.SetWebSocketClient(client)

			// Connect client
			if err := client.Connect(); err != nil {
				t.Fatalf("Failed to connect client: %v", err)
			}
			defer client.Close()

			// Start process
			if err := pr.Start(); err != nil {
				t.Fatalf("Failed to start process: %v", err)
			}

			// Let process run for a bit
			time.Sleep(100 * time.Millisecond)

			// Verify process is running
			if !pr.IsRunning() {
				t.Error("Expected process to be running")
			}

			// Measure shutdown time
			startShutdown := time.Now()
			
			// Stop the process
			if err := pr.Stop(); err != nil {
				t.Errorf("Failed to stop process: %v", err)
			}

			// Close runner to trigger context cancellation
			if err := pr.Close(); err != nil {
				t.Errorf("Failed to close runner: %v", err)
			}

			// Wait should complete quickly (not timeout)
			done := make(chan error, 1)
			go func() {
				done <- pr.Wait()
			}()

			select {
			case err := <-done:
				shutdownDuration := time.Since(startShutdown)
				t.Logf("Process shutdown completed in %v", shutdownDuration)
				
				if err != nil {
					t.Errorf("Wait returned error: %v", err)
				}
				
				// Shutdown should be fast (under 1 second)
				if shutdownDuration > 1*time.Second {
					t.Errorf("Shutdown took too long: %v", shutdownDuration)
				}
				
			case <-time.After(5 * time.Second):
				t.Error("Process shutdown timed out - goroutines likely blocked")
			}

			// Verify process is no longer running
			if pr.IsRunning() {
				t.Error("Expected process to be stopped")
			}
		})
	}
}

// TestLongRunningProcessWithSparseOutput tests processes that produce output occasionally
func TestLongRunningProcessWithSparseOutput(t *testing.T) {
	server := NewTestWebSocketServer(t)
	defer server.Close()

	// Create a process that outputs occasionally
	command := "go run ../e2e/test_helpers/sparse_output_app.go"
	label := "sparse-output"

	pr := runner.NewProcessRunner(command, label)
	
	client := runner.NewWebSocketClient(server.WebSocketURL(), label)
	client.SetCommand(command, "/tmp", []string{"process"})
	pr.SetWebSocketClient(client)

	if err := client.Connect(); err != nil {
		t.Fatalf("Failed to connect client: %v", err)
	}
	defer client.Close()

	// Track received logs
	logCount := 0
	pr.OnLogLine = func(content, stream string) {
		logCount++
		t.Logf("Received log %d: %s", logCount, content)
	}

	// Start process
	if err := pr.Start(); err != nil {
		t.Fatalf("Failed to start process: %v", err)
	}

	// Wait for at least 2 outputs
	waitStart := time.Now()
	for logCount < 2 && time.Since(waitStart) < 5*time.Second {
		time.Sleep(100 * time.Millisecond)
	}

	if logCount < 2 {
		t.Errorf("Expected at least 2 log lines, got %d", logCount)
	}

	// Now test shutdown
	startShutdown := time.Now()
	
	if err := pr.Stop(); err != nil {
		t.Errorf("Failed to stop process: %v", err)
	}

	if err := pr.Close(); err != nil {
		t.Errorf("Failed to close runner: %v", err)
	}

	// Wait should complete quickly
	done := make(chan error, 1)
	go func() {
		done <- pr.Wait()
	}()

	select {
	case err := <-done:
		shutdownDuration := time.Since(startShutdown)
		t.Logf("Process with sparse output shutdown completed in %v", shutdownDuration)
		
		if err != nil {
			t.Errorf("Wait returned error: %v", err)
		}
		
		if shutdownDuration > 1*time.Second {
			t.Errorf("Shutdown took too long: %v", shutdownDuration)
		}
		
	case <-time.After(5 * time.Second):
		t.Error("Process shutdown timed out")
	}
}

// TestProcessReaderGoroutineCleanup specifically tests that reader goroutines terminate
func TestProcessReaderGoroutineCleanup(t *testing.T) {
	// Count goroutines before test
	initialGoroutines := countGoroutines()
	t.Logf("Initial goroutine count: %d", initialGoroutines)

	server := NewTestWebSocketServer(t)
	defer server.Close()

	// Run multiple iterations to check for goroutine leaks
	for i := 0; i < 5; i++ {
		label := "goroutine-test"
		pr := runner.NewProcessRunner("sleep 3600", label)
		
		client := runner.NewWebSocketClient(server.WebSocketURL(), label)
		client.SetCommand("sleep 3600", "/tmp", []string{"process"})
		pr.SetWebSocketClient(client)

		if err := client.Connect(); err != nil {
			t.Fatalf("Iteration %d: Failed to connect client: %v", i, err)
		}

		if err := pr.Start(); err != nil {
			client.Close()
			t.Fatalf("Iteration %d: Failed to start process: %v", i, err)
		}

		// Quick shutdown
		time.Sleep(50 * time.Millisecond)
		
		pr.Stop()
		pr.Close()
		pr.Wait()
		client.Close()

		// Let goroutines clean up
		time.Sleep(200 * time.Millisecond)
	}

	// Allow some time for final cleanup
	time.Sleep(500 * time.Millisecond)

	// Check final goroutine count
	finalGoroutines := countGoroutines()
	t.Logf("Final goroutine count: %d", finalGoroutines)

	// Allow for some variance but check for leaks
	goroutineIncrease := finalGoroutines - initialGoroutines
	if goroutineIncrease > 10 {
		t.Errorf("Possible goroutine leak detected: %d additional goroutines", goroutineIncrease)
	}
}