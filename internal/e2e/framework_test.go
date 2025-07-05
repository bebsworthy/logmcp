package e2e

import (
	"strings"
	"testing"
	"time"
)

// TestFrameworkBasics verifies the test framework is working correctly
func TestFrameworkBasics(t *testing.T) {
	server := SetupTest(t)
	defer server.Cleanup()

	// Test listing sessions (should be empty initially)
	t.Log("Testing list sessions...")
	sessions, err := server.ListSessions()
	if err != nil {
		t.Fatalf("Failed to list sessions: %v", err)
	}
	t.Logf("Got %d sessions", len(sessions))
	if len(sessions) != 0 {
		t.Errorf("Expected 0 sessions, got %d", len(sessions))
	}

	// Test starting a process - use a simple echo command
	t.Log("Starting test process...")
	err = server.StartTestProcess("test-simple", "echo", "Test output 1")
	if err != nil {
		t.Fatalf("Failed to start test process: %v", err)
	}

	// Wait for process to appear
	t.Log("Waiting for process to appear...")
	if !server.WaitForCondition(5*time.Second, func() bool {
		sessions, _ = server.ListSessions()
		return len(sessions) == 1
	}) {
		t.Fatal("Process did not appear in session list")
	}

	// Verify session details
	t.Logf("Process appeared with label: %s, status: %s", sessions[0].Label, sessions[0].Status)
	if sessions[0].Label != "test-simple" {
		t.Errorf("Expected label 'test-simple', got '%s'", sessions[0].Label)
	}

	// Wait for some logs to be generated
	t.Log("Waiting for logs to be generated...")
	time.Sleep(2 * time.Second)

	// Check session status again
	sessions, _ = server.ListSessions()
	if len(sessions) > 0 {
		t.Logf("Session status after wait: %s", sessions[0].Status)
	}

	// Get logs
	t.Log("Getting logs...")
	logs, err := server.GetLogs([]string{"test-simple"})
	if err != nil {
		t.Fatalf("Failed to get logs: %v", err)
	}

	t.Logf("Got %d log entries", len(logs))

	// Should have at least one log entry with our test output
	foundOutput := false
	for _, log := range logs {
		t.Logf("Log [%s] %s: '%s'", log.Stream, log.Timestamp, log.Content)
		if strings.Contains(log.Content, "Test output") {
			foundOutput = true
		}
	}

	if len(logs) == 0 {
		t.Error("Expected to get at least one log entry")
	} else if !foundOutput {
		t.Error("Expected to find 'Test output' messages in logs")
	} else {
		t.Logf("Successfully found test output in logs")
	}
}

// TestFrameworkProcessControl tests process control functionality
func TestFrameworkProcessControl(t *testing.T) {
	server := SetupTest(t)
	defer server.Cleanup()

	// Start a long-running process
	err := server.StartTestProcess("sleeper", "sleep", "30")
	if err != nil {
		t.Fatalf("Failed to start process: %v", err)
	}

	// Wait for it to be running
	if !server.WaitForSessionStatus("sleeper", "running", 5*time.Second) {
		t.Fatal("Process did not reach running state")
	}

	// Send SIGKILL signal (more reliable for testing)
	t.Log("Sending SIGKILL signal...")
	err = server.ControlProcess("sleeper", "signal", "SIGKILL")
	if err != nil {
		t.Fatalf("Failed to send signal: %v", err)
	}

	// Wait for process to terminate or stop
	t.Log("Waiting for process to terminate...")
	if !server.WaitForCondition(5*time.Second, func() bool {
		sessions, _ := server.ListSessions()
		for _, s := range sessions {
			if s.Label == "sleeper" {
				t.Logf("Process status: %s", s.Status)
				if s.Status == "terminated" || s.Status == "stopped" || s.Status == "crashed" {
					return true
				}
			}
		}
		return false
	}) {
		sessions, _ := server.ListSessions()
		for _, s := range sessions {
			t.Logf("Final session: %+v", s)
		}
		t.Fatal("Process did not terminate")
	}

	// Verify it's terminated or stopped
	sessions, _ := server.ListSessions()
	for _, s := range sessions {
		if s.Label == "sleeper" {
			if s.Status != "terminated" && s.Status != "stopped" && s.Status != "crashed" {
				t.Errorf("Expected status terminated, stopped, or crashed, got %s", s.Status)
			}
			break
		}
	}
}
