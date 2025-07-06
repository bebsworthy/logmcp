package e2e

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// TestLongRunningProcess_NoTimeoutWarning verifies that processes can run
// for more than 30 seconds without triggering timeout warnings
func TestLongRunningProcess_NoTimeoutWarning(t *testing.T) {
	// Skip in short mode since this test takes 35+ seconds
	if testing.Short() {
		t.Skip("Skipping long-running test in short mode")
	}

	server := SetupTest(t)
	defer server.Cleanup()

	// Start a process that will run for 40 seconds
	processLabel := "long-runner-test"
	err := server.StartTestProcess(processLabel, "sleep", "40")
	if err != nil {
		t.Fatalf("Failed to start long-running process: %v", err)
	}

	// Wait for 35 seconds (past the old 30-second timeout)
	t.Log("Waiting 35 seconds to ensure no timeout warning...")
	time.Sleep(35 * time.Second)

	// Check that the process is still running
	sessions, err := server.ListSessions()
	if err != nil {
		t.Fatalf("Failed to list sessions: %v", err)
	}

	found := false
	for _, session := range sessions {
		if session.Label == processLabel && session.Status == "running" {
			found = true
			t.Logf("Process still running after 35 seconds: PID=%d", session.PID)
			break
		}
	}

	if !found {
		t.Error("Expected process to still be running after 35 seconds")
	}

	// Get logs to check for timeout warning
	logs, err := server.GetLogs([]string{processLabel})
	if err != nil {
		t.Fatalf("Failed to get logs: %v", err)
	}

	// Check logs for timeout warning
	for _, log := range logs {
		if strings.Contains(strings.ToLower(log.Content), "timeout") {
			t.Errorf("Found timeout warning in logs: %s", log.Content)
		}
	}

	// Stop the process
	err = server.ControlProcess(processLabel, "signal", "SIGTERM")
	if err != nil {
		t.Errorf("Failed to stop process: %v", err)
	}

	t.Log("Successfully ran process for 35+ seconds without timeout warning")
}

// TestLongRunningProcess_CLIRunner tests the CLI runner directly with long-running processes
func TestLongRunningProcess_CLIRunner(t *testing.T) {
	// Skip in short mode
	if testing.Short() {
		t.Skip("Skipping long-running test in short mode")
	}

	// Build the binary
	buildCmd := exec.Command("go", "build", "-o", "logmcp_e2e_test", "../..")
	if err := buildCmd.Run(); err != nil {
		t.Fatalf("Failed to build binary: %v", err)
	}
	defer os.Remove("logmcp_e2e_test")

	server := SetupTest(t)
	defer server.Cleanup()

	// Start the runner with a 40-second sleep
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "./logmcp_e2e_test", "run", 
		"--server-url", fmt.Sprintf("ws://localhost:%d", server.WebSocketPort),
		"--label", "cli-long-runner",
		"--", "sleep", "40")

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	// Start the process
	startTime := time.Now()
	if err := cmd.Start(); err != nil {
		t.Fatalf("Failed to start runner: %v", err)
	}

	// Wait 35 seconds
	time.Sleep(35 * time.Second)

	// Check if process is still running
	if cmd.Process == nil {
		t.Fatal("Process exited before 35 seconds")
	}

	// Send interrupt signal
	if err := cmd.Process.Signal(os.Interrupt); err != nil {
		t.Errorf("Failed to send interrupt: %v", err)
	}

	// Wait for process to exit
	done := make(chan error, 1)
	go func() {
		done <- cmd.Wait()
	}()

	select {
	case <-done:
		duration := time.Since(startTime)
		t.Logf("Runner process completed after %v", duration)
	case <-time.After(5 * time.Second):
		cmd.Process.Kill()
		t.Error("Process did not exit within 5 seconds of interrupt")
	}

	// Check output for timeout warning - search both stdout and stderr
	output := stdout.String() + stderr.String()
	outputLower := strings.ToLower(output)
	if strings.Contains(outputLower, "timeout") && !strings.Contains(outputLower, "timeout 45s") {
		t.Errorf("Found timeout warning in CLI output:\n%s", output)
	}

	// Verify the process ran for at least 35 seconds
	if time.Since(startTime) < 35*time.Second {
		t.Error("Process exited too early")
	}
}

// TestLongRunningProcess_MultipleProcesses tests multiple long-running processes simultaneously
func TestLongRunningProcess_MultipleProcesses(t *testing.T) {
	// Skip in short mode
	if testing.Short() {
		t.Skip("Skipping long-running test in short mode")
	}

	server := SetupTest(t)
	defer server.Cleanup()

	// Start multiple long-running processes
	processes := []string{"long-1", "long-2", "long-3"}
	for _, label := range processes {
		err := server.StartTestProcess(label, "sleep", "40")
		if err != nil {
			t.Fatalf("Failed to start process %s: %v", label, err)
		}
		// Stagger the starts slightly
		time.Sleep(100 * time.Millisecond)
	}

	// Wait 35 seconds
	t.Log("Waiting 35 seconds with multiple long-running processes...")
	time.Sleep(35 * time.Second)

	// Verify all processes are still running
	sessions, err := server.ListSessions()
	if err != nil {
		t.Fatalf("Failed to list sessions: %v", err)
	}

	runningCount := 0
	for _, session := range sessions {
		for _, expectedLabel := range processes {
			if session.Label == expectedLabel && session.Status == "running" {
				runningCount++
				t.Logf("Process %s still running: PID=%d", session.Label, session.PID)
			}
		}
	}

	if runningCount != len(processes) {
		t.Errorf("Expected %d running processes, found %d", len(processes), runningCount)
	}

	// Check logs for any timeout warnings
	for _, label := range processes {
		logs, err := server.GetLogs([]string{label})
		if err != nil {
			t.Errorf("Failed to get logs for %s: %v", label, err)
			continue
		}

		for _, log := range logs {
			if strings.Contains(strings.ToLower(log.Content), "timeout") {
				t.Errorf("Found timeout warning in logs for %s: %s", label, log.Content)
			}
		}
	}

	// Clean up processes
	for _, label := range processes {
		server.ControlProcess(label, "signal", "SIGTERM")
	}

	t.Log("Successfully ran multiple long processes without timeout warnings")
}

// TestLongRunningProcess_WithOutput tests long-running processes that produce output
func TestLongRunningProcess_WithOutput(t *testing.T) {
	// Skip in short mode
	if testing.Short() {
		t.Skip("Skipping long-running test in short mode")
	}

	server := SetupTest(t)
	defer server.Cleanup()

	// Create a test script that outputs every 5 seconds
	scriptContent := `#!/bin/bash
for i in {1..10}; do
    echo "Output $i at $(date)"
    sleep 5
done
`
	scriptPath := "/tmp/long_output_test.sh"
	if err := os.WriteFile(scriptPath, []byte(scriptContent), 0755); err != nil {
		t.Fatalf("Failed to create test script: %v", err)
	}
	defer os.Remove(scriptPath)

	// Start the process
	processLabel := "long-output-test"
	err := server.StartTestProcess(processLabel, "bash", scriptPath)
	if err != nil {
		t.Fatalf("Failed to start process: %v", err)
	}

	// Wait 35 seconds (will get ~7 outputs)
	time.Sleep(35 * time.Second)

	// Get logs
	logs, err := server.GetLogs([]string{processLabel})
	if err != nil {
		t.Fatalf("Failed to get logs: %v", err)
	}

	// Count outputs and check for timeout
	outputCount := 0
	for _, log := range logs {
		if strings.Contains(log.Content, "Output") {
			outputCount++
		}
		if strings.Contains(strings.ToLower(log.Content), "timeout") {
			t.Errorf("Found timeout warning: %s", log.Content)
		}
	}

	if outputCount < 6 {
		t.Errorf("Expected at least 6 outputs, got %d", outputCount)
	}

	// Stop the process
	server.ControlProcess(processLabel, "signal", "SIGTERM")

	t.Logf("Successfully ran process with output for 35+ seconds (got %d outputs)", outputCount)
}