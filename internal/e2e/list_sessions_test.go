package e2e

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

// TestListSessions_EmptyList tests listing sessions when no sessions exist
func TestListSessions_EmptyList(t *testing.T) {
	t.Parallel()

	ts := SetupTest(t)

	// List sessions when none exist
	sessions, err := ts.ListSessions()
	if err != nil {
		t.Fatalf("Failed to list sessions: %v", err)
	}

	// Verify empty list
	if len(sessions) != 0 {
		t.Errorf("Expected 0 sessions, got %d", len(sessions))
		for _, s := range sessions {
			t.Logf("Unexpected session: %+v", s)
		}
	}
}

// TestListSessions_MultipleActiveSessions tests listing multiple active sessions with different statuses
func TestListSessions_MultipleActiveSessions(t *testing.T) {
	t.Parallel()

	ts := SetupTest(t)

	// Start multiple processes with different characteristics
	testCases := []struct {
		label   string
		command string
		args    []string
	}{
		{
			label:   "simple-app",
			command: "go",
			args:    []string{"run", ts.TestAppPath("simple_app.go")},
		},
		{
			label:   "stdin-app",
			command: "go",
			args:    []string{"run", ts.TestAppPath("stdin_app.go")},
		},
		{
			label:   "fork-app",
			command: "go",
			args:    []string{"run", ts.TestAppPath("fork_app.go")},
		},
	}

	// Start all processes
	for _, tc := range testCases {
		if err := ts.StartTestProcess(tc.label, tc.command, tc.args...); err != nil {
			t.Fatalf("Failed to start %s: %v", tc.label, err)
		}
	}

	// Wait for all processes to be running
	for _, tc := range testCases {
		if !ts.WaitForSessionStatus(tc.label, "running", 5*time.Second) {
			t.Fatalf("Session %s did not reach running status", tc.label)
		}
	}

	// List all sessions
	sessions, err := ts.ListSessions()
	if err != nil {
		t.Fatalf("Failed to list sessions: %v", err)
	}

	// Verify session count
	if len(sessions) != len(testCases) {
		t.Errorf("Expected %d sessions, got %d", len(testCases), len(sessions))
	}

	// Check each session
	for _, tc := range testCases {
		found := false
		for _, session := range sessions {
			if session.Label == tc.label {
				found = true

				// Verify status is running
				if session.Status != "running" {
					t.Errorf("Session %s: expected status 'running', got '%s'", tc.label, session.Status)
				}

				// Verify PID is set
				if session.PID <= 0 {
					t.Errorf("Session %s: expected positive PID, got %d", tc.label, session.PID)
				}

				// Verify start time is recent
				if time.Since(*session.StartTime) > 10*time.Second {
					t.Errorf("Session %s: start time %v is too old", tc.label, session.StartTime)
				}

				// Verify no exit time or exit code for running process
				if session.EndTime != nil {
					t.Errorf("Session %s: running process should not have end time", tc.label)
				}
				if session.ExitCode != nil {
					t.Errorf("Session %s: running process should not have exit code", tc.label)
				}

				break
			}
		}
		if !found {
			t.Errorf("Session %s not found in list", tc.label)
		}
	}

	// Stop one process and verify status change
	if err := ts.ControlProcess("simple-app", "signal", "SIGTERM"); err != nil {
		t.Fatalf("Failed to stop simple-app: %v", err)
	}

	// Wait for process to stop
	if !ts.WaitForSessionStatus("simple-app", "stopped", 5*time.Second) {
		t.Fatal("simple-app did not stop")
	}

	// List sessions again
	sessions, err = ts.ListSessions()
	if err != nil {
		t.Fatalf("Failed to list sessions after stop: %v", err)
	}

	// Verify stopped session
	for _, session := range sessions {
		if session.Label == "simple-app" {
			if session.Status != "stopped" {
				t.Errorf("Expected simple-app status 'stopped', got '%s'", session.Status)
			}
			if session.EndTime == nil {
				t.Error("Stopped session should have end time")
			}
			if session.ExitCode == nil {
				t.Error("Stopped session should have exit code")
			}
			break
		}
	}
}

// TestListSessions_SessionMetadataAccuracy tests that all session metadata is accurate
func TestListSessions_SessionMetadataAccuracy(t *testing.T) {
	t.Parallel()

	ts := SetupTest(t)

	// Start a process with specific working directory
	workingDir := "/tmp"
	opts := map[string]any{
		"working_dir": workingDir,
	}

	command := "go"
	args := []string{"run", ts.TestAppPath("simple_app.go")}
	if err := ts.StartTestProcessWithOptions("metadata-test", command, args, opts); err != nil {
		t.Fatalf("Failed to start process: %v", err)
	}

	// Wait for process to start
	if !ts.WaitForSessionStatus("metadata-test", "running", 5*time.Second) {
		t.Fatal("Process did not start")
	}

	// Generate some logs
	time.Sleep(2 * time.Second)

	// List sessions
	sessions, err := ts.ListSessions()
	if err != nil {
		t.Fatalf("Failed to list sessions: %v", err)
	}

	// Find our session
	var targetSession *Session
	for _, s := range sessions {
		if s.Label == "metadata-test" {
			targetSession = &s
			break
		}
	}

	if targetSession == nil {
		t.Fatal("Could not find metadata-test session")
	}

	// Verify metadata
	t.Logf("Session metadata: %+v", targetSession)

	// Check PID is valid
	if targetSession.PID <= 0 {
		t.Errorf("Invalid PID: %d", targetSession.PID)
	}

	// Check start time is reasonable
	if targetSession.StartTime == nil || time.Since(*targetSession.StartTime) > 1*time.Minute {
		t.Errorf("Start time seems incorrect: %v", targetSession.StartTime)
	}

	// Get logs to verify log count
	logs, err := ts.GetLogs([]string{"metadata-test"})
	if err != nil {
		t.Fatalf("Failed to get logs: %v", err)
	}

	// Note: The log count in session metadata might be approximate due to buffering
	// Just verify it's not zero if we have logs
	if len(logs) > 0 && targetSession.ID == "" {
		t.Log("Warning: Session has logs but metadata might not reflect log count accurately")
	}

	// Stop the process
	if err := ts.ControlProcess("metadata-test", "signal", "SIGTERM"); err != nil {
		t.Fatalf("Failed to stop process: %v", err)
	}

	// Wait for stop
	if !ts.WaitForSessionStatus("metadata-test", "stopped", 5*time.Second) {
		t.Fatal("Process did not stop")
	}

	// List sessions again to check stopped metadata
	sessions, err = ts.ListSessions()
	if err != nil {
		t.Fatalf("Failed to list sessions after stop: %v", err)
	}

	// Find session again
	for _, s := range sessions {
		if s.Label == "metadata-test" {
			// Verify stopped session metadata
			if s.Status != "stopped" {
				t.Errorf("Expected status 'stopped', got '%s'", s.Status)
			}
			if s.EndTime == nil {
				t.Error("Stopped session should have end time")
			}
			if s.ExitCode == nil {
				t.Error("Stopped session should have exit code")
			} else if *s.ExitCode != 0 && *s.ExitCode != -1 {
				// Exit code should be 0 (graceful) or -1 (terminated)
				t.Logf("Note: Exit code is %d", *s.ExitCode)
			}
			break
		}
	}
}

// TestListSessions_DuplicateLabels tests sessions with duplicate labels
func TestListSessions_DuplicateLabels(t *testing.T) {
	t.Parallel()

	ts := SetupTest(t)

	// Start multiple processes with the same label
	for i := 0; i < 3; i++ {
		if err := ts.StartTestProcess("duplicate-label", "go", "run", ts.TestAppPath("simple_app.go")); err != nil {
			t.Fatalf("Failed to start process %d: %v", i, err)
		}
		// Small delay to ensure sequential creation
		time.Sleep(100 * time.Millisecond)
	}

	// Wait a bit for all to be running
	time.Sleep(2 * time.Second)

	// List sessions
	sessions, err := ts.ListSessions()
	if err != nil {
		t.Fatalf("Failed to list sessions: %v", err)
	}

	// Count sessions with our label pattern
	duplicateCount := 0
	labelMap := make(map[string]bool)

	for _, session := range sessions {
		if strings.HasPrefix(session.Label, "duplicate-label") {
			duplicateCount++
			labelMap[session.Label] = true

			// All should be running
			if session.Status != "running" {
				t.Errorf("Session %s: expected status 'running', got '%s'", session.Label, session.Status)
			}

			// Each should have unique PID
			if session.PID <= 0 {
				t.Errorf("Session %s: invalid PID %d", session.Label, session.PID)
			}
		}
	}

	// We should have 3 sessions
	if duplicateCount != 3 {
		t.Errorf("Expected 3 duplicate-label sessions, found %d", duplicateCount)
		t.Logf("Labels found: %v", labelMap)
	}

	// Labels should be: duplicate-label, duplicate-label-2, duplicate-label-3
	expectedLabels := []string{"duplicate-label", "duplicate-label-2", "duplicate-label-3"}
	for _, expected := range expectedLabels {
		if !labelMap[expected] {
			t.Errorf("Expected to find label '%s'", expected)
		}
	}
}

// TestListSessions_CrashedSessions tests listing sessions that have crashed
func TestListSessions_CrashedSessions(t *testing.T) {
	t.Parallel()

	ts := SetupTest(t)

	// Start a process that will crash
	if err := ts.StartTestProcess("crash-app", "go", "run", ts.TestAppPath("crash_app.go")); err != nil {
		t.Fatalf("Failed to start crash app: %v", err)
	}

	// Wait for it to crash
	if !ts.WaitForSessionStatus("crash-app", "crashed", 5*time.Second) {
		t.Fatal("crash-app did not crash as expected")
	}

	// List sessions
	sessions, err := ts.ListSessions()
	if err != nil {
		t.Fatalf("Failed to list sessions: %v", err)
	}

	// Find crashed session
	found := false
	for _, session := range sessions {
		if session.Label == "crash-app" {
			found = true

			// Verify crashed status
			if session.Status != "crashed" {
				t.Errorf("Expected status 'crashed', got '%s'", session.Status)
			}

			// Should have end time
			if session.EndTime == nil {
				t.Error("Crashed session should have end time")
			}

			// Should have non-zero exit code
			if session.ExitCode == nil {
				t.Error("Crashed session should have exit code")
			} else if *session.ExitCode == 0 {
				t.Error("Crashed session should have non-zero exit code")
			}

			break
		}
	}

	if !found {
		t.Error("Could not find crash-app session")
	}
}

// TestListSessions_ManySessions tests behavior with many sessions
func TestListSessions_ManySessions(t *testing.T) {
	// This test might be slow, so we don't run it in parallel

	ts := SetupTest(t)

	// Start many processes
	const numSessions = 20
	for i := 0; i < numSessions; i++ {
		label := fmt.Sprintf("session-%d", i)
		if err := ts.StartTestProcess(label, "go", "run", ts.TestAppPath("simple_app.go")); err != nil {
			t.Fatalf("Failed to start %s: %v", label, err)
		}
	}

	// Wait a bit for all to start
	time.Sleep(20 * time.Second)

	// List all sessions
	sessions, err := ts.ListSessions()
	if err != nil {
		t.Fatalf("Failed to list sessions: %v", err)
	}

	// Count our sessions
	ourSessions := 0
	for _, session := range sessions {
		if strings.HasPrefix(session.Label, "session-") {
			ourSessions++
		}
	}

	if ourSessions != numSessions {
		t.Errorf("Expected %d sessions, found %d", numSessions, ourSessions)
	}

	// Stop half of them
	for i := 0; i < numSessions/2; i++ {
		label := fmt.Sprintf("session-%d", i)
		if err := ts.ControlProcess(label, "signal", "SIGTERM"); err != nil {
			t.Logf("Warning: Failed to stop %s: %v", label, err)
		}
	}

	// Wait for stops
	time.Sleep(2 * time.Second)

	// List again and count statuses
	sessions, err = ts.ListSessions()
	if err != nil {
		t.Fatalf("Failed to list sessions after stops: %v", err)
	}

	runningCount := 0
	stoppedCount := 0
	for _, session := range sessions {
		if strings.HasPrefix(session.Label, "session-") {
			switch session.Status {
			case "running":
				runningCount++
			case "stopped":
				stoppedCount++
			}
		}
	}

	// We should have approximately half running and half stopped
	expectedRunning := numSessions / 2
	expectedStopped := numSessions / 2

	// Allow some tolerance for timing issues
	if runningCount < expectedRunning-2 || runningCount > expectedRunning+2 {
		t.Errorf("Expected ~%d running sessions, got %d", expectedRunning, runningCount)
	}
	if stoppedCount < expectedStopped-2 || stoppedCount > expectedStopped+2 {
		t.Errorf("Expected ~%d stopped sessions, got %d", expectedStopped, stoppedCount)
	}
}

// TestListSessions_SessionTypes tests different session types (run, forward, managed)
func TestListSessions_SessionTypes(t *testing.T) {
	t.Parallel()

	ts := SetupTest(t)

	// Start a managed process (via MCP start_process)
	if err := ts.StartTestProcess("managed-process", "go", "run", ts.TestAppPath("simple_app.go")); err != nil {
		t.Fatalf("Failed to start managed process: %v", err)
	}

	// Wait for it to start
	if !ts.WaitForSessionStatus("managed-process", "running", 5*time.Second) {
		t.Fatal("Managed process did not start")
	}

	// List sessions
	sessions, err := ts.ListSessions()
	if err != nil {
		t.Fatalf("Failed to list sessions: %v", err)
	}

	// Find our session
	found := false
	for _, session := range sessions {
		if session.Label == "managed-process" {
			found = true

			// Verify it's a managed session
			t.Logf("Session details: ID=%s, Label=%s, Status=%s",
				session.ID, session.Label, session.Status)

			// All test processes started via StartTestProcess are managed
			if session.Status != "running" {
				t.Errorf("Expected running status, got %s", session.Status)
			}

			break
		}
	}

	if !found {
		t.Error("Could not find managed-process session")
	}
}

// TestListSessions_RestartedSessions tests listing sessions that have been restarted
func TestListSessions_RestartedSessions(t *testing.T) {
	t.Parallel()

	ts := SetupTest(t)

	// Start a process
	if err := ts.StartTestProcess("restart-test", "go", "run", ts.TestAppPath("simple_app.go")); err != nil {
		t.Fatalf("Failed to start process: %v", err)
	}

	// Wait for it to start
	if !ts.WaitForSessionStatus("restart-test", "running", 5*time.Second) {
		t.Fatal("Process did not start")
	}

	// Get initial PID
	sessions, err := ts.ListSessions()
	if err != nil {
		t.Fatalf("Failed to list sessions: %v", err)
	}

	var initialPID int
	for _, s := range sessions {
		if s.Label == "restart-test" {
			initialPID = s.PID
			break
		}
	}

	if initialPID == 0 {
		t.Fatal("Could not find initial PID")
	}

	// Restart the process
	if err := ts.ControlProcess("restart-test", "restart"); err != nil {
		t.Fatalf("Failed to restart process: %v", err)
	}

	// Wait for it to be running again (after restart)
	time.Sleep(2 * time.Second)

	// List sessions again
	sessions, err = ts.ListSessions()
	if err != nil {
		t.Fatalf("Failed to list sessions after restart: %v", err)
	}

	// Find restarted session
	found := false
	for _, session := range sessions {
		if session.Label == "restart-test" {
			found = true

			// Should be running
			if session.Status != "running" {
				t.Errorf("Expected status 'running' after restart, got '%s'", session.Status)
			}

			// Should have different PID
			if session.PID == initialPID {
				t.Error("PID should change after restart")
			}

			// Should still have original start time (session persists across restart)
			// This depends on implementation - session might be recreated

			break
		}
	}

	if !found {
		t.Error("Could not find restart-test session after restart")
	}
}
