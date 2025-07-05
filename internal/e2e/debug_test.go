package e2e

import (
	"testing"
	"time"
)

// TestDebugSimpleEcho tests if basic echo commands work
func TestDebugSimpleEcho(t *testing.T) {
	ts := SetupTest(t)
	defer ts.Cleanup()

	// Start a simple echo process using sh -c to ensure output is captured
	err := ts.StartTestProcess("echo-test", "sh", "-c", "echo 'Hello World'")
	if err != nil {
		t.Fatalf("Failed to start echo process: %v", err)
	}

	// Wait for process to complete
	time.Sleep(2 * time.Second)

	// Check sessions
	sessions, err := ts.ListSessions()
	if err != nil {
		t.Fatalf("Failed to list sessions: %v", err)
	}

	t.Logf("Sessions: %d", len(sessions))
	for _, s := range sessions {
		t.Logf("  Session %s: status=%s, pid=%d, exit_code=%v", s.Label, s.Status, s.PID, s.ExitCode)
	}

	// Get logs
	logs, err := ts.GetLogs([]string{"echo-test"})
	if err != nil {
		t.Fatalf("Failed to get logs: %v", err)
	}

	t.Logf("Logs: %d", len(logs))
	for i, log := range logs {
		t.Logf("  Log[%d]: stream=%s, content='%s'", i, log.Stream, log.Content)
	}

	// Check if we got the expected output
	found := false
	for _, log := range logs {
		if log.Content == "Hello World" {
			found = true
			break
		}
	}

	if !found {
		t.Error("Did not find 'Hello World' in logs")
	}
}

// TestDebugLongRunning tests a process that outputs multiple lines
func TestDebugLongRunning(t *testing.T) {
	ts := SetupTest(t)
	defer ts.Cleanup()

	// Use the test helper directory
	helperPath := "./test_helpers/simple_app.go"

	// Start the simple app
	err := ts.StartTestProcess("simple-test", "go", "run", helperPath)
	if err != nil {
		t.Fatalf("Failed to start simple app: %v", err)
	}

	// Wait for some output
	time.Sleep(3 * time.Second)

	// Get logs
	logs, err := ts.GetLogs([]string{"simple-test"})
	if err != nil {
		t.Fatalf("Failed to get logs: %v", err)
	}

	t.Logf("Logs from simple app: %d", len(logs))
	for i, log := range logs {
		if i < 10 { // Print first 10 logs
			t.Logf("  Log[%d]: stream=%s, content='%s'", i, log.Stream, log.Content)
		}
	}

	// Stop the process
	err = ts.ControlProcess("simple-test", "signal", "SIGTERM")
	if err != nil {
		t.Fatalf("Failed to stop process: %v", err)
	}
}
