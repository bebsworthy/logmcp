package e2e

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// TestStartProcess_BasicStartup tests basic process startup functionality
func TestStartProcess_BasicStartup(t *testing.T) {
	t.Parallel()

	ts := SetupTest(t)

	// Start a simple process
	err := ts.StartTestProcess("basic-test", "go", "run", ts.TestAppPath("simple_app.go"))
	if err != nil {
		t.Fatalf("Failed to start process: %v", err)
	}

	// Verify process is running
	if !ts.WaitForSessionStatus("basic-test", "running", 5*time.Second) {
		t.Fatal("Process did not reach running status")
	}

	// Verify logs are being captured
	if !ts.WaitForLogContent("basic-test", "Simple app started", 5*time.Second) {
		t.Fatal("Failed to capture startup logs")
	}

	// List sessions to verify process details
	sessions, err := ts.ListSessions()
	if err != nil {
		t.Fatalf("Failed to list sessions: %v", err)
	}

	found := false
	for _, session := range sessions {
		if session.Label == "basic-test" {
			found = true
			if session.Status != "running" {
				t.Errorf("Expected status 'running', got '%s'", session.Status)
			}
			if session.PID <= 0 {
				t.Errorf("Expected positive PID, got %d", session.PID)
			}
			if session.StartTime == nil {
				t.Error("Expected start time to be set")
			}
			break
		}
	}

	if !found {
		t.Error("Process not found in session list")
	}
}

// TestStartProcess_CustomWorkingDirectory tests process startup with custom working directory
func TestStartProcess_CustomWorkingDirectory(t *testing.T) {
	t.Parallel()

	ts := SetupTest(t)

	// Create a test directory
	testDir := filepath.Join(os.TempDir(), fmt.Sprintf("logmcp-test-%d", time.Now().UnixNano()))
	if err := os.MkdirAll(testDir, 0755); err != nil {
		t.Fatalf("Failed to create test directory: %v", err)
	}
	defer os.RemoveAll(testDir)

	// Create a test file in the directory
	testFile := filepath.Join(testDir, "test.txt")
	if err := os.WriteFile(testFile, []byte("test content"), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	// Start process with custom working directory
	opts := map[string]any{
		"working_dir": testDir,
	}

	// Use a command that shows the working directory
	command := "pwd"
	if runtime.GOOS == "windows" {
		command = "cd"
	}

	err := ts.StartTestProcessWithOptions("workdir-test", command, []string{}, opts)
	if err != nil {
		t.Fatalf("Failed to start process: %v", err)
	}

	// Wait for process to complete
	time.Sleep(2 * time.Second)

	// Get logs to verify working directory
	logs, err := ts.GetLogs([]string{"workdir-test"})
	if err != nil {
		t.Fatalf("Failed to get logs: %v", err)
	}

	// Verify the output contains our test directory
	foundWorkDir := false
	for _, log := range logs {
		if strings.Contains(log.Content, filepath.Base(testDir)) {
			foundWorkDir = true
			break
		}
	}

	if !foundWorkDir {
		t.Error("Working directory was not set correctly")
		for _, log := range logs {
			t.Logf("Log: %s", log.Content)
		}
	}
}

// TestStartProcess_EnvironmentVariables tests process startup with environment variables
func TestStartProcess_EnvironmentVariables(t *testing.T) {
	t.Parallel()

	ts := SetupTest(t)

	// Set custom environment variables
	envVars := map[string]string{
		"TEST_VAR1": "value1",
		"TEST_VAR2": "value2",
		"TEST_PATH": "/custom/path:/another/path",
	}

	opts := map[string]any{
		"environment": envVars,
	}

	// Start a process that prints environment variables
	command := "sh"
	args := []string{"-c", "echo TEST_VAR1=$TEST_VAR1; echo TEST_VAR2=$TEST_VAR2; echo TEST_PATH=$TEST_PATH"}
	
	if runtime.GOOS == "windows" {
		command = "cmd"
		args = []string{"/c", "echo TEST_VAR1=%TEST_VAR1% && echo TEST_VAR2=%TEST_VAR2% && echo TEST_PATH=%TEST_PATH%"}
	}

	err := ts.StartTestProcessWithOptions("env-test", command, args, opts)
	if err != nil {
		t.Fatalf("Failed to start process: %v", err)
	}

	// Check if process started
	sessions, err := ts.ListSessions()
	if err != nil {
		t.Fatalf("Failed to list sessions: %v", err)
	}
	t.Logf("Sessions after start: %d sessions", len(sessions))
	for _, s := range sessions {
		t.Logf("  Session %s: status=%s, pid=%d", s.Label, s.Status, s.PID)
	}

	// Wait for process to complete
	time.Sleep(3 * time.Second)

	// Check final session status
	sessions2, _ := ts.ListSessions()
	for _, s := range sessions2 {
		if s.Label == "env-test" {
			t.Logf("Final session status: %s, pid=%d, exit_code=%v", s.Status, s.PID, s.ExitCode)
		}
	}

	// Get logs
	logs, err := ts.GetLogs([]string{"env-test"})
	if err != nil {
		t.Fatalf("Failed to get logs: %v", err)
	}


	// Verify environment variables were set
	expectedVars := map[string]string{
		"TEST_VAR1": "value1",
		"TEST_VAR2": "value2",
		"TEST_PATH": "/custom/path:/another/path",
	}

	for varName, expectedValue := range expectedVars {
		found := false
		for _, log := range logs {
			if strings.Contains(log.Content, varName) && strings.Contains(log.Content, expectedValue) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("Environment variable %s=%s not found in output", varName, expectedValue)
			for _, log := range logs {
				t.Logf("Log: %s", log.Content)
			}
		}
	}
}

// TestStartProcess_RestartPolicies tests different restart policies
func TestStartProcess_RestartPolicies(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name           string
		restartPolicy  string
		expectRestart  bool
		crashApp       bool
	}{
		{
			name:          "never-policy-normal-exit",
			restartPolicy: "never",
			expectRestart: false,
			crashApp:      false,
		},
		{
			name:          "never-policy-crash",
			restartPolicy: "never",
			expectRestart: false,
			crashApp:      true,
		},
		{
			name:          "on-failure-policy-normal-exit",
			restartPolicy: "on-failure",
			expectRestart: false,
			crashApp:      false,
		},
		{
			name:          "on-failure-policy-crash",
			restartPolicy: "on-failure",
			expectRestart: true,
			crashApp:      true,
		},
		{
			name:          "always-policy-normal-exit",
			restartPolicy: "always",
			expectRestart: true,
			crashApp:      false,
		},
		{
			name:          "always-policy-crash",
			restartPolicy: "always",
			expectRestart: true,
			crashApp:      true,
		},
	}

	for _, tc := range testCases {
		tc := tc // capture range variable
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ts := SetupTest(t)

			// Prepare command based on test case
			var command string
			var args []string
			
			if tc.crashApp {
				// Use crash app with short delay
				command = "go"
				args = []string{"run", ts.TestAppPath("crash_app.go"), "1s", "1"}
			} else {
				// Use a command that exits normally after a short delay
				command = "sh"
				args = []string{"-c", "echo 'Starting'; sleep 1; echo 'Exiting normally'; exit 0"}
				if runtime.GOOS == "windows" {
					command = "cmd"
					args = []string{"/c", "echo Starting && timeout /t 1 && echo Exiting normally && exit 0"}
				}
			}

			opts := map[string]any{
				"restart_policy": tc.restartPolicy,
			}

			label := fmt.Sprintf("restart-test-%s", tc.name)
			err := ts.StartTestProcessWithOptions(label, command, args, opts)
			if err != nil {
				t.Fatalf("Failed to start process: %v", err)
			}

			// Wait for initial process to start
			if !ts.WaitForSessionStatus(label, "running", 5*time.Second) {
				t.Fatal("Process did not start")
			}

			// Get initial PID
			sessions, err := ts.ListSessions()
			if err != nil {
				t.Fatalf("Failed to list sessions: %v", err)
			}

			var initialPID int
			for _, s := range sessions {
				if s.Label == label {
					initialPID = s.PID
					break
				}
			}

			if initialPID == 0 {
				t.Fatal("Could not find initial PID")
			}

			// Wait for process to exit
			time.Sleep(3 * time.Second)

			if tc.expectRestart {
				// Process should restart - wait for it
				time.Sleep(2 * time.Second)

				// Check if process restarted with different PID
				sessions, err = ts.ListSessions()
				if err != nil {
					t.Fatalf("Failed to list sessions after restart: %v", err)
				}

				found := false
				for _, s := range sessions {
					if s.Label == label {
						found = true
						if s.Status != "running" {
							t.Errorf("Expected restarted process to be running, got %s", s.Status)
						}
						if s.PID == initialPID {
							t.Error("Process PID should change after restart")
						}
						break
					}
				}

				if !found {
					t.Error("Process not found after expected restart")
				}
			} else {
				// Process should not restart
				sessions, err = ts.ListSessions()
				if err != nil {
					t.Fatalf("Failed to list sessions: %v", err)
				}

				for _, s := range sessions {
					if s.Label == label {
						if s.Status == "running" {
							t.Error("Process should not be running when restart policy is 'never'")
						}
						expectedStatus := "stopped"
						if tc.crashApp {
							expectedStatus = "crashed"
						}
						if s.Status != expectedStatus {
							t.Errorf("Expected status '%s', got '%s'", expectedStatus, s.Status)
						}
						break
					}
				}
			}
		})
	}
}

// TestStartProcess_LongRunningProcess tests management of long-running processes
func TestStartProcess_LongRunningProcess(t *testing.T) {
	t.Parallel()

	ts := SetupTest(t)

	// Start a long-running process
	err := ts.StartTestProcess("long-running", "go", "run", ts.TestAppPath("simple_app.go"), "500ms")
	if err != nil {
		t.Fatalf("Failed to start process: %v", err)
	}

	// Verify process starts and continues running
	if !ts.WaitForSessionStatus("long-running", "running", 5*time.Second) {
		t.Fatal("Process did not start")
	}

	// Wait and collect multiple log entries
	time.Sleep(3 * time.Second)

	logs, err := ts.GetLogs([]string{"long-running"})
	if err != nil {
		t.Fatalf("Failed to get logs: %v", err)
	}

	// Count tick messages
	tickCount := 0
	for _, log := range logs {
		if strings.Contains(log.Content, "Tick") {
			tickCount++
		}
	}

	// Should have multiple ticks (at least 4-5 with 500ms interval over 3 seconds)
	if tickCount < 4 {
		t.Errorf("Expected at least 4 tick messages, got %d", tickCount)
	}

	// Verify process is still running
	sessions, err := ts.ListSessions()
	if err != nil {
		t.Fatalf("Failed to list sessions: %v", err)
	}

	for _, s := range sessions {
		if s.Label == "long-running" {
			if s.Status != "running" {
				t.Errorf("Long-running process should still be running, got status %s", s.Status)
			}
			break
		}
	}
}

// TestStartProcess_ImmediateExit tests process that exits immediately
func TestStartProcess_ImmediateExit(t *testing.T) {
	t.Parallel()

	ts := SetupTest(t)

	// Start a process that exits immediately
	command := "echo"
	args := []string{"Quick exit"}

	err := ts.StartTestProcess("immediate-exit", command, args...)
	if err != nil {
		t.Fatalf("Failed to start process: %v", err)
	}

	// Wait for process to complete
	if !ts.WaitForSessionStatus("immediate-exit", "stopped", 5*time.Second) {
		t.Fatal("Process did not stop as expected")
	}

	// Verify logs were captured
	logs, err := ts.GetLogs([]string{"immediate-exit"})
	if err != nil {
		t.Fatalf("Failed to get logs: %v", err)
	}

	found := false
	for _, log := range logs {
		if strings.Contains(log.Content, "Quick exit") {
			found = true
			break
		}
	}

	if !found {
		t.Error("Failed to capture output from immediately exiting process")
	}

	// Verify exit code
	sessions, err := ts.ListSessions()
	if err != nil {
		t.Fatalf("Failed to list sessions: %v", err)
	}

	for _, s := range sessions {
		if s.Label == "immediate-exit" {
			if s.ExitCode == nil {
				t.Error("Exit code should be set")
			} else if *s.ExitCode != 0 {
				t.Errorf("Expected exit code 0, got %d", *s.ExitCode)
			}
			break
		}
	}
}

// TestStartProcess_CrashingProcess tests process that crashes
func TestStartProcess_CrashingProcess(t *testing.T) {
	t.Parallel()

	ts := SetupTest(t)

	// Start crash app
	err := ts.StartTestProcess("crash-test", "go", "run", ts.TestAppPath("crash_app.go"), "2s", "42")
	if err != nil {
		t.Fatalf("Failed to start crash app: %v", err)
	}

	// Wait for it to crash
	if !ts.WaitForSessionStatus("crash-test", "crashed", 5*time.Second) {
		t.Fatal("Process did not crash as expected")
	}

	// Verify crash was logged
	logs, err := ts.GetLogs([]string{"crash-test"})
	if err != nil {
		t.Fatalf("Failed to get logs: %v", err)
	}


	foundError := false
	foundExitMessage := false
	for _, log := range logs {
		// Look for actual error messages the crash app produces
		if strings.Contains(log.Content, "Fatal error occurred!") || strings.Contains(log.Content, "CRASH!") {
			foundError = true
		}
		if strings.Contains(log.Content, "EXITING: crash_app with code: 42") {
			foundExitMessage = true
		}
		// Debug: Print what we actually got
		t.Logf("Log content: %s", log.Content)
	}

	if !foundError {
		t.Error("Did not find error message in logs")
	}
	if !foundExitMessage {
		t.Error("Did not find exit message in logs")
	}

	// Verify exit code
	sessions, err := ts.ListSessions()
	if err != nil {
		t.Fatalf("Failed to list sessions: %v", err)
	}

	for _, s := range sessions {
		if s.Label == "crash-test" {
			// Debug: Print session details
			exitCodeValue := "nil"
			if s.ExitCode != nil {
				exitCodeValue = fmt.Sprintf("%d", *s.ExitCode)
			}
			t.Logf("Session details: status=%s, exitCode=%s, pid=%d", s.Status, exitCodeValue, s.PID)
			
			if s.Status != "crashed" {
				t.Errorf("Expected status 'crashed', got '%s'", s.Status)
			}
			if s.ExitCode == nil {
				t.Error("Exit code should be set for crashed process")
			} else if *s.ExitCode != 1 {
				// Note: When using 'go run', the exit code is 1 for any non-zero exit from the child
				t.Errorf("Expected exit code 1 (from go run), got %d", *s.ExitCode)
			}
			break
		}
	}
}

// TestStartProcess_InvalidCommand tests handling of invalid commands
func TestStartProcess_InvalidCommand(t *testing.T) {
	t.Parallel()

	ts := SetupTest(t)

	// Try to start a non-existent command
	err := ts.StartTestProcess("invalid-cmd", "this-command-does-not-exist-12345")
	if err == nil {
		t.Fatal("Expected error when starting invalid command, but got none")
	}

	// The error should indicate the command was not found
	if !strings.Contains(err.Error(), "not found") && !strings.Contains(err.Error(), "does not exist") {
		t.Logf("Error message: %v", err)
	}

	// Verify no session was created (or it failed immediately)
	sessions, err := ts.ListSessions()
	if err != nil {
		t.Fatalf("Failed to list sessions: %v", err)
	}

	for _, s := range sessions {
		if s.Label == "invalid-cmd" {
			// If a session was created, it should be in failed/crashed state
			if s.Status == "running" {
				t.Error("Invalid command should not result in a running process")
			}
			break
		}
	}
}

// TestStartProcess_InvalidWorkingDirectory tests handling of non-existent working directory
func TestStartProcess_InvalidWorkingDirectory(t *testing.T) {
	t.Parallel()

	ts := SetupTest(t)

	// Try to start process with non-existent working directory
	opts := map[string]any{
		"working_dir": "/this/directory/does/not/exist/12345",
	}

	err := ts.StartTestProcessWithOptions("invalid-dir", "echo", []string{"test"}, opts)
	if err == nil {
		t.Fatal("Expected error when using invalid working directory, but got none")
	}

	// The error should indicate directory issue
	if !strings.Contains(err.Error(), "directory") || !strings.Contains(err.Error(), "exist") {
		t.Logf("Unexpected error message: %v", err)
	}
}

// TestStartProcess_DuplicateLabels tests handling of duplicate labels
func TestStartProcess_DuplicateLabels(t *testing.T) {
	t.Parallel()

	ts := SetupTest(t)

	// Start first process
	err := ts.StartTestProcess("duplicate", "go", "run", ts.TestAppPath("simple_app.go"))
	if err != nil {
		t.Fatalf("Failed to start first process: %v", err)
	}

	// Wait for it to start
	if !ts.WaitForSessionStatus("duplicate", "running", 5*time.Second) {
		t.Fatal("First process did not start")
	}

	// Try to start another process with the same label
	err = ts.StartTestProcess("duplicate", "go", "run", ts.TestAppPath("simple_app.go"))
	if err != nil {
		t.Fatalf("Failed to start second process: %v", err)
	}

	// Wait a bit
	time.Sleep(2 * time.Second)

	// List sessions - should have both with modified labels
	sessions, err := ts.ListSessions()
	if err != nil {
		t.Fatalf("Failed to list sessions: %v", err)
	}

	duplicateCount := 0
	labels := []string{}
	for _, s := range sessions {
		if strings.HasPrefix(s.Label, "duplicate") {
			duplicateCount++
			labels = append(labels, s.Label)
		}
	}

	if duplicateCount != 2 {
		t.Errorf("Expected 2 processes with duplicate prefix, got %d", duplicateCount)
		t.Logf("Found labels: %v", labels)
	}

	// Should have "duplicate" and "duplicate-2"
	hasOriginal := false
	hasModified := false
	for _, label := range labels {
		if label == "duplicate" {
			hasOriginal = true
		}
		if label == "duplicate-2" {
			hasModified = true
		}
	}

	if !hasOriginal {
		t.Error("Original 'duplicate' label not found")
	}
	if !hasModified {
		t.Error("Modified 'duplicate-2' label not found")
	}
}

// TestStartProcess_CollectStartupLogs tests the collect_startup_logs option
func TestStartProcess_CollectStartupLogs(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name               string
		collectStartupLogs bool
		expectLogs         bool
	}{
		{
			name:               "collect-enabled",
			collectStartupLogs: true,
			expectLogs:         true,
		},
		{
			name:               "collect-disabled",
			collectStartupLogs: false,
			expectLogs:         false,
		},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ts := SetupTest(t)

			opts := map[string]any{
				"collect_startup_logs": tc.collectStartupLogs,
			}

			label := fmt.Sprintf("startup-logs-%s", tc.name)
			err := ts.StartTestProcessWithOptions(label, "go", []string{"run", ts.TestAppPath("simple_app.go")}, opts)
			if err != nil {
				t.Fatalf("Failed to start process: %v", err)
			}

			// Wait for process to start
			if !ts.WaitForSessionStatus(label, "running", 5*time.Second) {
				t.Fatal("Process did not start")
			}

			// Give it a moment to generate logs
			time.Sleep(1 * time.Second)

			// Get logs
			logs, err := ts.GetLogs([]string{label})
			if err != nil {
				t.Fatalf("Failed to get logs: %v", err)
			}

			if tc.expectLogs && len(logs) == 0 {
				t.Error("Expected to collect startup logs but got none")
			}

			if !tc.expectLogs && len(logs) > 0 {
				t.Errorf("Expected no startup logs but got %d entries", len(logs))
				for _, log := range logs {
					t.Logf("Unexpected log: %s", log.Content)
				}
			}
		})
	}
}

// TestStartProcess_ComplexCommand tests starting processes with complex command lines
func TestStartProcess_ComplexCommand(t *testing.T) {
	t.Parallel()

	ts := SetupTest(t)

	// Test command with pipes and redirects
	command := "sh"
	args := []string{"-c", "echo 'Line 1' && echo 'Error line' >&2 && echo 'Line 2'"}
	
	if runtime.GOOS == "windows" {
		command = "cmd"
		args = []string{"/c", "echo Line 1 && echo Error line 1>&2 && echo Line 2"}
	}

	err := ts.StartTestProcessWithOptions("complex-cmd", command, args, map[string]any{})
	if err != nil {
		t.Fatalf("Failed to start complex command: %v", err)
	}

	// Wait for completion
	time.Sleep(2 * time.Second)

	// Get logs
	logs, err := ts.GetLogs([]string{"complex-cmd"})
	if err != nil {
		t.Fatalf("Failed to get logs: %v", err)
	}

	// Verify we captured all output
	foundLine1 := false
	foundLine2 := false
	foundError := false
	
	for _, log := range logs {
		if strings.Contains(log.Content, "Line 1") {
			foundLine1 = true
		}
		if strings.Contains(log.Content, "Line 2") {
			foundLine2 = true
		}
		if strings.Contains(log.Content, "Error line") {
			foundError = true
			if log.Stream != "stderr" {
				t.Errorf("Error line should be on stderr, got %s", log.Stream)
			}
		}
	}

	if !foundLine1 {
		t.Error("Did not find 'Line 1' in output")
	}
	if !foundLine2 {
		t.Error("Did not find 'Line 2' in output")
	}
	if !foundError {
		t.Error("Did not find 'Error line' in output")
	}
}

// TestStartProcess_ProcessWithChildren tests starting processes that spawn children
func TestStartProcess_ProcessWithChildren(t *testing.T) {
	t.Parallel()

	ts := SetupTest(t)

	// Start fork app that creates child processes
	err := ts.StartTestProcess("parent-process", "go", "run", ts.TestAppPath("fork_app.go"), "3")
	if err != nil {
		t.Fatalf("Failed to start parent process: %v", err)
	}

	// Wait for parent and children to start
	if !ts.WaitForSessionStatus("parent-process", "running", 5*time.Second) {
		t.Fatal("Parent process did not start")
	}

	// Wait for children to be created
	time.Sleep(2 * time.Second)

	// Get logs to verify parent and children are logging
	logs, err := ts.GetLogs([]string{"parent-process"})
	if err != nil {
		t.Fatalf("Failed to get logs: %v", err)
	}

	// Count unique child messages
	childMessages := make(map[string]bool)
	parentFound := false
	
	for _, log := range logs {
		if strings.Contains(log.Content, "Parent process running") {
			parentFound = true
		}
		if strings.Contains(log.Content, "Child") && strings.Contains(log.Content, "running") {
			childMessages[log.Content] = true
		}
	}

	if !parentFound {
		t.Error("Parent process logs not found")
	}

	// Should have logs from 3 children
	if len(childMessages) < 3 {
		t.Errorf("Expected logs from at least 3 children, got %d", len(childMessages))
	}

	// Stop the parent process
	err = ts.ControlProcess("parent-process", "signal", "SIGTERM")
	if err != nil {
		t.Fatalf("Failed to stop parent process: %v", err)
	}

	// Wait for process to stop
	if !ts.WaitForSessionStatus("parent-process", "stopped", 10*time.Second) {
		t.Fatal("Parent process did not stop")
	}

	// Verify children were also terminated (check logs stopped coming)
	initialLogCount := len(logs)
	time.Sleep(2 * time.Second)
	
	logs2, err := ts.GetLogs([]string{"parent-process"})
	if err != nil {
		t.Fatalf("Failed to get logs after stop: %v", err)
	}

	// Log count should not increase significantly after stop
	if len(logs2) > initialLogCount+5 {
		t.Errorf("Logs continued after parent stopped: initial=%d, after=%d", initialLogCount, len(logs2))
	}
}