package e2e

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// TestStdinForwardingTermination verifies that the stdin forwarding goroutine
// terminates cleanly when the process exits, without causing a timeout
func TestStdinForwardingTermination(t *testing.T) {
	// Build the binary
	if err := exec.Command("go", "build", "-o", "logmcp_test", "../..").Run(); err != nil {
		t.Fatalf("Failed to build binary: %v", err)
	}
	defer exec.Command("rm", "-f", "logmcp_test").Run()

	server := SetupTest(t)
	defer server.Cleanup()

	// Run a short-lived process
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "./logmcp_test", "run", "--label", "stdin-test", "--", "sleep", "2")
	
	// Capture output
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	
	// Redirect stdin to /dev/null to avoid blocking
	cmd.Stdin = nil

	startTime := time.Now()
	err := cmd.Run()
	duration := time.Since(startTime)

	if err != nil && ctx.Err() == nil {
		t.Fatalf("Command failed: %v\nStdout: %s\nStderr: %s", err, stdout.String(), stderr.String())
	}

	// Check output for timeout warning
	output := stdout.String() + stderr.String()
	if strings.Contains(output, "goroutines did not complete within timeout") {
		t.Errorf("Found timeout warning in output:\n%s", output)
	}

	// Process should complete in about 2 seconds, not 30+
	if duration > 5*time.Second {
		t.Errorf("Process took too long to exit: %v (expected ~2s)", duration)
	}

	t.Logf("Process completed in %v without timeout warning", duration)
}

// TestLongRunningProcessInterruption tests that long-running processes can be
// interrupted cleanly without timeout warnings
func TestLongRunningProcessInterruption(t *testing.T) {
	// Build the binary
	if err := exec.Command("go", "build", "-o", "logmcp_test", "../..").Run(); err != nil {
		t.Fatalf("Failed to build binary: %v", err)
	}
	defer exec.Command("rm", "-f", "logmcp_test").Run()

	server := SetupTest(t)
	defer server.Cleanup()

	// Start a very long-running process
	cmd := exec.Command("./logmcp_test", "run", "--label", "long-test", "--", "sleep", "3600")
	
	// Capture output
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	cmd.Stdin = nil

	if err := cmd.Start(); err != nil {
		t.Fatalf("Failed to start command: %v", err)
	}

	// Let it run for a bit
	time.Sleep(2 * time.Second)

	// Send interrupt signal
	startInterrupt := time.Now()
	if err := cmd.Process.Signal(os.Interrupt); err != nil {
		t.Fatalf("Failed to send interrupt: %v", err)
	}

	// Wait for process to exit
	done := make(chan error, 1)
	go func() {
		done <- cmd.Wait()
	}()

	select {
	case <-done:
		interruptDuration := time.Since(startInterrupt)
		t.Logf("Process terminated in %v after interrupt", interruptDuration)
		
		// Check for timeout warning
		output := stdout.String() + stderr.String()
		if strings.Contains(output, "goroutines did not complete within timeout") {
			t.Errorf("Found timeout warning after interrupt:\n%s", output)
		}
		
		// Should terminate quickly after interrupt
		if interruptDuration > 2*time.Second {
			t.Errorf("Process took too long to terminate after interrupt: %v", interruptDuration)
		}
		
	case <-time.After(5 * time.Second):
		cmd.Process.Kill()
		t.Error("Process did not terminate within 5 seconds of interrupt")
	}
}