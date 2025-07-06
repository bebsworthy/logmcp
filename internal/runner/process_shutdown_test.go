package runner

import (
	"testing"
	"time"
)

// TestProcessRunner_NoTimeoutWarning specifically tests that processes with no output
// can shut down without the 30-second timeout warning
func TestProcessRunner_NoTimeoutWarning(t *testing.T) {
	// Test commands that produce no output
	testCases := []struct {
		name    string
		command string
	}{
		{"Sleep", "sleep 3600"},
		{"TailNull", "tail -f /dev/null"},
		{"Cat", "cat"}, // cat with no input blocks forever
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			runner := NewProcessRunner(tc.command, "test-"+tc.name)

			// Start the process
			if err := runner.Start(); err != nil {
				t.Fatalf("Failed to start process: %v", err)
			}

			// Let it run briefly
			time.Sleep(100 * time.Millisecond)

			// Verify it's running
			if !runner.IsRunning() {
				t.Error("Expected process to be running")
			}

			// Stop and close
			if err := runner.Stop(); err != nil {
				t.Errorf("Failed to stop process: %v", err)
			}

			if err := runner.Close(); err != nil {
				t.Errorf("Failed to close runner: %v", err)
			}

			// Measure wait time
			startWait := time.Now()
			waitDone := make(chan error, 1)
			
			go func() {
				waitDone <- runner.Wait()
			}()

			select {
			case err := <-waitDone:
				waitDuration := time.Since(startWait)
				t.Logf("Wait completed in %v", waitDuration)
				
				if err != nil {
					t.Errorf("Wait returned error: %v", err)
				}
				
				// Should complete quickly, not take 30 seconds
				if waitDuration > 2*time.Second {
					t.Errorf("Wait took too long: %v (should be under 2s)", waitDuration)
				}
				
			case <-time.After(5 * time.Second):
				t.Error("Wait timed out - goroutines are likely blocked")
			}
		})
	}
}

// TestProcessRunner_CleanShutdownWithOutput tests processes that produce output
func TestProcessRunner_CleanShutdownWithOutput(t *testing.T) {
	runner := NewProcessRunner("go run ../e2e/test_helpers/simple_app.go 100ms", "test-output")

	// Track output
	outputCount := 0
	runner.OnLogLine = func(content, stream string) {
		outputCount++
		t.Logf("Received %s: %s", stream, content)
	}

	// Start the process
	if err := runner.Start(); err != nil {
		t.Fatalf("Failed to start process: %v", err)
	}

	// Wait for some output
	time.Sleep(200 * time.Millisecond)

	if outputCount == 0 {
		t.Error("Expected to receive some output")
	}

	// Stop and measure shutdown time
	startShutdown := time.Now()
	
	if err := runner.Stop(); err != nil {
		t.Errorf("Failed to stop process: %v", err)
	}

	if err := runner.Close(); err != nil {
		t.Errorf("Failed to close runner: %v", err)
	}

	if err := runner.Wait(); err != nil {
		t.Errorf("Wait returned error: %v", err)
	}

	shutdownDuration := time.Since(startShutdown)
	t.Logf("Shutdown completed in %v with %d output lines", shutdownDuration, outputCount)

	if shutdownDuration > 1*time.Second {
		t.Errorf("Shutdown took too long: %v", shutdownDuration)
	}
}