package e2e

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

// TestProcessCrashDetection tests detection and handling of process crashes
func TestProcessCrashDetection(t *testing.T) {
	t.Parallel()

	ts := SetupTest(t)

	// Start a process that will crash
	err := ts.StartTestProcess("crash-detection", "sh", "-c", "echo 'Starting'; sleep 1; echo 'About to crash'; exit 42")
	if err != nil {
		t.Fatalf("Failed to start crashing process: %v", err)
	}

	// Wait for process to crash
	if !ts.WaitForSessionStatus("crash-detection", "crashed", 5*time.Second) {
		t.Fatal("Process did not crash as expected")
	}

	// Verify crash was detected and logged
	logs, err := ts.GetLogs([]string{"crash-detection"})
	if err != nil {
		t.Fatalf("Failed to get crash logs: %v", err)
	}

	// Verify we got the expected logs
	foundStarting := false
	foundCrashing := false
	for _, log := range logs {
		if strings.Contains(log.Content, "Starting") {
			foundStarting = true
		}
		if strings.Contains(log.Content, "About to crash") {
			foundCrashing = true
		}
	}

	if !foundStarting {
		t.Error("Did not find 'Starting' log")
	}
	if !foundCrashing {
		t.Error("Did not find 'About to crash' log")
	}

	// Check session status shows crash
	sessions, err := ts.ListSessions()
	if err != nil {
		t.Fatalf("Failed to list sessions: %v", err)
	}

	found := false
	for _, session := range sessions {
		if session.Label == "crash-detection" {
			found = true
			if session.Status != "crashed" {
				t.Errorf("Expected status 'crashed', got '%s'", session.Status)
			}
			if session.ExitCode == nil {
				t.Error("Exit code should be set for crashed process")
			} else if *session.ExitCode != 42 {
				t.Errorf("Expected exit code 42, got %d", *session.ExitCode)
			}
			break
		}
	}

	if !found {
		t.Error("Crashed process not found in session list")
	}
}

// TestHighFrequencyLogging tests system behavior under rapid log generation
func TestHighFrequencyLogging(t *testing.T) {
	t.Parallel()

	ts := SetupTest(t)

	// Start multiple high-frequency logging processes
	processes := []string{"rapid-1", "rapid-2", "rapid-3"}
	
	for i, label := range processes {
		cmd := fmt.Sprintf(`for j in $(seq 1 50); do echo "[%s] Rapid log entry $j"; echo "[%s] Second line for entry $j" >&2; done`, label, label)
		err := ts.StartTestProcess(label, "sh", "-c", cmd)
		if err != nil {
			t.Fatalf("Failed to start rapid process %s: %v", label, err)
		}
		// Small delay between starts to avoid overwhelming the system
		if i < len(processes)-1 {
			time.Sleep(100 * time.Millisecond)
		}
	}

	// Wait for all processes to complete
	time.Sleep(3 * time.Second)

	// Query all logs together
	logs, err := ts.GetLogs(processes, WithLimit(300))
	if err != nil {
		t.Fatalf("Failed to get all rapid logs: %v", err)
	}

	// Verify we got logs from all processes
	for _, label := range processes {
		foundStdout := false
		foundStderr := false
		
		for _, log := range logs {
			if log.Label == label {
				if strings.Contains(log.Content, fmt.Sprintf("[%s] Rapid log entry", label)) {
					foundStdout = true
				}
				if strings.Contains(log.Content, fmt.Sprintf("[%s] Second line for entry", label)) {
					foundStderr = true
				}
			}
		}
		
		if !foundStdout {
			t.Errorf("Did not find stdout logs for %s", label)
		}
		if !foundStderr {
			t.Errorf("Did not find stderr logs for %s", label)
		}
	}

	// Test filtering with high volume - stderr only
	stderrLogs, err := ts.GetLogs(processes, WithStream("stderr"))
	if err != nil {
		t.Fatalf("Failed to get stderr logs: %v", err)
	}

	// Verify stderr filtering worked
	for _, log := range stderrLogs {
		if log.Stream != "stderr" {
			t.Errorf("Expected only stderr logs, but got stream '%s'", log.Stream)
		}
		if !strings.Contains(log.Content, "Second line for entry") {
			t.Errorf("Unexpected stderr content: %s", log.Content)
		}
	}

	t.Logf("Successfully processed %d total logs from %d processes", len(logs), len(processes))
}

// TestLongRunningProcessResilience tests handling of long-running processes
func TestLongRunningProcessResilience(t *testing.T) {
	t.Parallel()

	ts := SetupTest(t)

	// Start a long-running process
	err := ts.StartTestProcess("long-runner", "sh", "-c", `
		for i in $(seq 1 30); do 
			echo "Heartbeat $i at $(date)"; 
			sleep 1; 
		done
	`)
	if err != nil {
		t.Fatalf("Failed to start long-running process: %v", err)
	}

	// Verify process starts
	if !ts.WaitForSessionStatus("long-runner", "running", 5*time.Second) {
		t.Fatal("Long-running process did not start")
	}

	// Wait and collect logs periodically
	time.Sleep(5 * time.Second)
	
	logs1, err := ts.GetLogs([]string{"long-runner"})
	if err != nil {
		t.Fatalf("Failed to get logs (first check): %v", err)
	}
	
	initialCount := len(logs1)
	if initialCount < 4 {
		t.Errorf("Expected at least 4 heartbeats, got %d", initialCount)
	}

	// Wait more and check logs increased
	time.Sleep(5 * time.Second)
	
	logs2, err := ts.GetLogs([]string{"long-runner"})
	if err != nil {
		t.Fatalf("Failed to get logs (second check): %v", err)
	}
	
	secondCount := len(logs2)
	if secondCount <= initialCount {
		t.Errorf("Expected more logs after waiting, got %d (was %d)", secondCount, initialCount)
	}

	// Send SIGTERM to stop gracefully
	err = ts.ControlProcess("long-runner", "signal", "SIGTERM")
	if err != nil {
		t.Fatalf("Failed to send SIGTERM: %v", err)
	}

	// Wait for process to stop
	if !ts.WaitForSessionStatus("long-runner", "stopped", 10*time.Second) {
		t.Fatal("Process did not stop after SIGTERM")
	}

	// Verify we can still get all logs after process stopped
	finalLogs, err := ts.GetLogs([]string{"long-runner"})
	if err != nil {
		t.Fatalf("Failed to get final logs: %v", err)
	}

	if len(finalLogs) < secondCount {
		t.Errorf("Lost logs after process stopped: had %d, now %d", secondCount, len(finalLogs))
	}

	t.Logf("Long-running process handled %d heartbeats successfully", len(finalLogs))
}

// TestMultipleProcessManagement tests handling many concurrent processes
func TestMultipleProcessManagement(t *testing.T) {
	t.Parallel()

	ts := SetupTest(t)

	// Start many processes with different behaviors
	processes := []struct {
		label   string
		command string
	}{
		{"quick-exit", "echo 'Quick process' && exit 0"},
		{"slow-exit", "sleep 2 && echo 'Slow process done' && exit 0"},
		{"fail-fast", "echo 'Will fail' && exit 1"},
		{"fail-slow", "sleep 1 && echo 'Will fail slowly' && exit 2"},
		{"logger-1", "for i in 1 2 3 4 5; do echo \"Log $i\"; sleep 0.5; done"},
		{"logger-2", "for i in A B C D E; do echo \"Log $i\"; sleep 0.5; done"},
	}

	// Start all processes
	for _, p := range processes {
		err := ts.StartTestProcess(p.label, "sh", "-c", p.command)
		if err != nil {
			t.Fatalf("Failed to start process %s: %v", p.label, err)
		}
	}

	// Wait for processes to run
	time.Sleep(4 * time.Second)

	// List all sessions
	sessions, err := ts.ListSessions()
	if err != nil {
		t.Fatalf("Failed to list sessions: %v", err)
	}

	// Verify all processes were tracked
	if len(sessions) < len(processes) {
		t.Errorf("Expected at least %d sessions, got %d", len(processes), len(sessions))
	}

	// Check specific process states
	expectations := map[string]struct {
		status   string
		exitCode *int
	}{
		"quick-exit": {"stopped", intPtr(0)},
		"fail-fast":  {"crashed", intPtr(1)},
		"fail-slow":  {"crashed", intPtr(2)},
	}

	for label, expected := range expectations {
		found := false
		for _, session := range sessions {
			if session.Label == label {
				found = true
				if session.Status != expected.status {
					t.Errorf("%s: expected status %s, got %s", label, expected.status, session.Status)
				}
				if expected.exitCode != nil {
					if session.ExitCode == nil {
						t.Errorf("%s: expected exit code %d, got nil", label, *expected.exitCode)
					} else if *session.ExitCode != *expected.exitCode {
						t.Errorf("%s: expected exit code %d, got %d", label, *expected.exitCode, *session.ExitCode)
					}
				}
				break
			}
		}
		if !found {
			t.Errorf("Process %s not found in session list", label)
		}
	}

	// Get logs from all processes
	var allLabels []string
	for _, p := range processes {
		allLabels = append(allLabels, p.label)
	}

	logs, err := ts.GetLogs(allLabels)
	if err != nil {
		t.Fatalf("Failed to get logs from all processes: %v", err)
	}

	// Verify we got logs from each process
	logsByLabel := make(map[string]int)
	for _, log := range logs {
		logsByLabel[log.Label]++
	}

	for _, p := range processes {
		if count, ok := logsByLabel[p.label]; !ok || count == 0 {
			t.Errorf("No logs found for process %s", p.label)
		}
	}

	t.Logf("Successfully managed %d concurrent processes with %d total log entries", len(processes), len(logs))
}

// Helper function to create int pointer
func intPtr(i int) *int {
	return &i
}