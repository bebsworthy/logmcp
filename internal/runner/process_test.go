package runner

import (
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/logmcp/logmcp/internal/errors"
	"github.com/logmcp/logmcp/internal/protocol"
)

// TestProcessRunner_NewProcessRunner tests process runner creation
func TestProcessRunner_NewProcessRunner(t *testing.T) {
	runner := NewProcessRunner("echo hello", "test-echo")
	
	if runner == nil {
		t.Fatal("Expected runner to be created")
	}
	
	if runner.command != "echo" {
		t.Errorf("Expected command to be 'echo', got '%s'", runner.command)
	}
	
	if len(runner.args) != 1 || runner.args[0] != "hello" {
		t.Errorf("Expected args ['hello'], got %v", runner.args)
	}
	
	if runner.label != "test-echo" {
		t.Errorf("Expected label to be 'test-echo', got '%s'", runner.label)
	}
	
	if runner.stdinBuffer == nil {
		t.Error("Expected stdinBuffer to be initialized")
	}
}

// TestProcessRunner_NewProcessRunnerWithConfig tests custom configuration
func TestProcessRunner_NewProcessRunnerWithConfig(t *testing.T) {
	config := ProcessRunnerConfig{
		WorkingDir:       "/tmp",
		Environment:      map[string]string{"TEST": "value"},
		RestartOnFailure: true,
		MaxRestarts:      5,
		RestartDelay:     10 * time.Second,
	}
	
	runner := NewProcessRunnerWithConfig("echo hello", "test-echo", config)
	
	if runner.workingDir != "/tmp" {
		t.Errorf("Expected workingDir to be '/tmp', got '%s'", runner.workingDir)
	}
	
	if runner.environment["TEST"] != "value" {
		t.Errorf("Expected environment TEST=value, got %v", runner.environment)
	}
	
	if !runner.restartOnFailure {
		t.Error("Expected restartOnFailure to be true")
	}
	
	if runner.maxRestarts != 5 {
		t.Errorf("Expected maxRestarts to be 5, got %d", runner.maxRestarts)
	}
	
	if runner.restartDelay != 10*time.Second {
		t.Errorf("Expected restartDelay to be 10s, got %v", runner.restartDelay)
	}
}

// TestProcessRunner_SetWebSocketClient tests WebSocket client assignment
func TestProcessRunner_SetWebSocketClient(t *testing.T) {
	runner := NewProcessRunner("echo hello", "test-echo")
	client := NewWebSocketClient("ws://localhost:8765", "test-label")
	
	runner.SetWebSocketClient(client)
	
	if runner.client != client {
		t.Error("Expected client to be assigned")
	}
	
	// Check if callbacks are set
	if client.OnCommand == nil {
		t.Error("Expected OnCommand callback to be set")
	}
	
	if client.OnStdinMessage == nil {
		t.Error("Expected OnStdinMessage callback to be set")
	}
}

// TestProcessRunner_IsRunning tests running state
func TestProcessRunner_IsRunning(t *testing.T) {
	runner := NewProcessRunner("echo hello", "test-echo")
	
	if runner.IsRunning() {
		t.Error("Expected runner to not be running initially")
	}
	
	// Manually set running state
	runner.mutex.Lock()
	runner.running = true
	runner.mutex.Unlock()
	
	if !runner.IsRunning() {
		t.Error("Expected runner to be running after setting state")
	}
}

// TestProcessRunner_Start_InvalidCommand tests starting with invalid command
func TestProcessRunner_Start_InvalidCommand(t *testing.T) {
	runner := NewProcessRunner("/nonexistent/command", "test-invalid")
	
	err := runner.Start()
	
	if err == nil {
		t.Fatal("Expected error for invalid command")
	}
	
	// Check if it's our custom error type
	if !errors.IsType(err, errors.ErrorTypeProcess) {
		t.Errorf("Expected process error, got %v", err)
	}
	
	if !runner.IsRunning() {
		// Expected - process should not be running
	}
}

// TestProcessRunner_Start_Success tests successful process start
func TestProcessRunner_Start_Success(t *testing.T) {
	// Use a command that exists on most systems
	runner := NewProcessRunner("sleep 0.1", "test-sleep")
	
	err := runner.Start()
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}
	
	if !runner.IsRunning() {
		t.Error("Expected runner to be running after start")
	}
	
	if runner.GetPID() <= 0 {
		t.Errorf("Expected valid PID, got %d", runner.GetPID())
	}
	
	// Wait for process to complete with a shorter timeout for debugging
	done := make(chan error, 1)
	go func() {
		done <- runner.Wait()
	}()
	
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("Wait() returned error: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Error("Wait() timed out after 5 seconds - goroutine leak detected")
		return
	}
	
	// Check exit code
	exitCode := runner.GetExitCode()
	if exitCode == nil {
		t.Error("Expected exit code to be set")
	} else if *exitCode != 0 {
		t.Errorf("Expected exit code 0, got %d", *exitCode)
	}
}

// TestProcessRunner_SendStdin tests stdin input
func TestProcessRunner_SendStdin(t *testing.T) {
	runner := NewProcessRunner("cat", "test-cat")
	
	// Start cat process (reads from stdin, writes to stdout)
	err := runner.Start()
	if err != nil {
		t.Fatalf("Failed to start process: %v", err)
	}
	defer func() {
		runner.Kill()
		runner.Wait()
	}()
	
	// Send stdin input
	err = runner.SendStdin("hello\n")
	if err != nil {
		t.Errorf("Expected no error sending stdin, got %v", err)
	}
	
	// Test sending when not running
	runner.Stop()
	runner.Wait()
	
	err = runner.SendStdin("hello\n")
	if err == nil {
		t.Error("Expected error when sending stdin to stopped process")
	}
}

// TestProcessRunner_SendSignal tests signal sending
func TestProcessRunner_SendSignal(t *testing.T) {
	runner := NewProcessRunner("sleep 10", "test-sleep")
	
	err := runner.Start()
	if err != nil {
		t.Fatalf("Failed to start process: %v", err)
	}
	defer func() {
		runner.Kill()
		runner.Wait()
	}()
	
	// Send SIGTERM
	err = runner.SendSignal("SIGTERM")
	if err != nil {
		t.Errorf("Expected no error sending SIGTERM, got %v", err)
	}
	
	// Wait for process to exit
	runner.Wait()
	
	// Test sending signal when not running
	err = runner.SendSignal("SIGTERM")
	if err == nil {
		t.Error("Expected error when sending signal to stopped process")
	}
	
	// Test invalid signal
	runner2 := NewProcessRunner("sleep 10", "test-sleep2")
	runner2.Start()
	defer func() {
		runner2.Kill()
		runner2.Wait()
	}()
	
	err = runner2.SendSignal("INVALID")
	if err == nil {
		t.Error("Expected error for invalid signal")
	}
}

// TestProcessRunner_Restart tests process restart
func TestProcessRunner_Restart(t *testing.T) {
	runner := NewProcessRunner("sleep 0.1", "test-sleep")
	
	err := runner.Start()
	if err != nil {
		t.Fatalf("Failed to start process: %v", err)
	}
	
	originalPID := runner.GetPID()
	
	// Wait for process to complete
	runner.Wait()
	
	// Restart
	err = runner.Restart()
	if err != nil {
		t.Errorf("Expected no error on restart, got %v", err)
	}
	
	newPID := runner.GetPID()
	
	if newPID == originalPID {
		t.Error("Expected different PID after restart")
	}
	
	// Clean up
	runner.Stop()
	runner.Wait()
}

// TestProcessRunner_Stop tests graceful stop
func TestProcessRunner_Stop(t *testing.T) {
	runner := NewProcessRunner("sleep 10", "test-sleep")
	
	err := runner.Start()
	if err != nil {
		t.Fatalf("Failed to start process: %v", err)
	}
	
	if !runner.IsRunning() {
		t.Error("Expected process to be running")
	}
	
	err = runner.Stop()
	if err != nil {
		t.Errorf("Expected no error on stop, got %v", err)
	}
	
	// Wait for process to complete
	runner.Wait()
	
	if runner.IsRunning() {
		t.Error("Expected process to be stopped")
	}
}

// TestProcessRunner_Kill tests forceful kill
func TestProcessRunner_Kill(t *testing.T) {
	runner := NewProcessRunner("sleep 10", "test-sleep")
	
	err := runner.Start()
	if err != nil {
		t.Fatalf("Failed to start process: %v", err)
	}
	
	err = runner.Kill()
	if err != nil {
		t.Errorf("Expected no error on kill, got %v", err)
	}
	
	// Wait for process to complete
	runner.Wait()
	
	if runner.IsRunning() {
		t.Error("Expected process to be killed")
	}
}

// TestProcessRunner_HandleCommand tests command handling
func TestProcessRunner_HandleCommand(t *testing.T) {
	runner := NewProcessRunner("sleep 1", "test-sleep")
	
	// Test restart command (should work even when not running - it will just start the process)
	err := runner.handleCommand("restart", nil)
	if err != nil {
		t.Errorf("Expected restart to work when not running, got error: %v", err)
	}
	
	// Start process
	runner.Start()
	defer func() {
		runner.Kill()
		runner.Wait()
	}()
	
	// Test signal command without signal
	err = runner.handleCommand("signal", nil)
	if err == nil {
		t.Error("Expected error for signal command without signal")
	}
	
	// Test signal command with signal
	signal := protocol.SignalTERM
	err = runner.handleCommand("signal", &signal)
	if err != nil {
		t.Errorf("Expected no error for signal command, got %v", err)
	}
	
	// Test invalid command
	err = runner.handleCommand("invalid", nil)
	if err == nil {
		t.Error("Expected error for invalid command")
	}
}

// TestProcessRunner_Close tests runner shutdown
func TestProcessRunner_Close(t *testing.T) {
	runner := NewProcessRunner("sleep 0.1", "test-sleep")
	
	runner.Start()
	
	err := runner.Close()
	if err != nil {
		t.Errorf("Expected no error on close, got %v", err)
	}
	
	// Check if context was cancelled
	select {
	case <-runner.ctx.Done():
		// Expected
	case <-time.After(100 * time.Millisecond):
		t.Error("Expected context to be cancelled")
	}
}

// TestProcessRunner_Health tests health checking
func TestProcessRunner_Health(t *testing.T) {
	runner := NewProcessRunner("sleep 0.1", "test-sleep")
	
	if !runner.IsHealthy() {
		t.Error("Expected runner to be healthy initially")
	}
	
	health := runner.GetHealth()
	
	if health.Label != "test-sleep" {
		t.Errorf("Expected label in health, got '%s'", health.Label)
	}
	
	if health.Command != "sleep 0.1" {
		t.Errorf("Expected command in health, got '%s'", health.Command)
	}
	
	// Close runner and check health
	runner.Close()
	
	if runner.IsHealthy() {
		t.Error("Expected runner to be unhealthy after close")
	}
}

// TestProcessRunner_LogCapture tests stdout/stderr capture
func TestProcessRunner_LogCapture(t *testing.T) {
	var capturedLogs []string
	var mu sync.Mutex
	
	runner := NewProcessRunner("echo test message", "test-echo")
	runner.OnLogLine = func(content, stream string) {
		mu.Lock()
		capturedLogs = append(capturedLogs, content)
		mu.Unlock()
	}
	
	err := runner.Start()
	if err != nil {
		t.Fatalf("Failed to start process: %v", err)
	}
	
	runner.Wait()
	
	// Check captured logs
	mu.Lock()
	defer mu.Unlock()
	
	if len(capturedLogs) == 0 {
		t.Error("Expected to capture log output")
	}
	
	found := false
	for _, log := range capturedLogs {
		if strings.Contains(log, "test message") {
			found = true
			break
		}
	}
	
	if !found {
		t.Errorf("Expected to find 'test message' in logs, got %v", capturedLogs)
	}
}

// TestProcessRunner_WithWebSocket tests integration with WebSocket client
func TestProcessRunner_WithWebSocket(t *testing.T) {
	var sentMessages []*protocol.LogMessage
	var mu sync.Mutex
	
	// Create mock client with same label as runner
	client := NewWebSocketClient("ws://localhost:8765", "test-echo")
	client.sessionID = "test-session"
	client.connMutex.Lock()
	client.connected = true
	client.connMutex.Unlock()
	
	// Capture sent messages
	go func() {
		for msg := range client.messageChan {
			if logMsg, ok := msg.(*protocol.LogMessage); ok {
				mu.Lock()
				sentMessages = append(sentMessages, logMsg)
				mu.Unlock()
			}
		}
	}()
	
	// Create runner with WebSocket client
	runner := NewProcessRunner("echo websocket test", "test-echo")
	runner.SetWebSocketClient(client)
	
	err := runner.Start()
	if err != nil {
		t.Fatalf("Failed to start process: %v", err)
	}
	
	runner.Wait()
	
	// Give some time for message processing
	time.Sleep(100 * time.Millisecond)
	
	// Check sent messages
	mu.Lock()
	defer mu.Unlock()
	
	if len(sentMessages) == 0 {
		t.Error("Expected to send log messages via WebSocket")
	}
	
	found := false
	for _, msg := range sentMessages {
		if strings.Contains(msg.Content, "websocket test") {
			found = true
			if msg.Label != "test-echo" {
				t.Errorf("Expected label 'test-echo', got '%s'", msg.Label)
			}
			if msg.SessionID != "test-session" {
				t.Errorf("Expected sessionID 'test-session', got '%s'", msg.SessionID)
			}
			break
		}
	}
	
	if !found {
		t.Error("Expected to find 'websocket test' in sent messages")
	}
}

// BenchmarkProcessRunner_Start benchmarks process startup
func BenchmarkProcessRunner_Start(b *testing.B) {
	for i := 0; i < b.N; i++ {
		runner := NewProcessRunner("echo hello", "test-echo")
		runner.Start()
		runner.Wait()
	}
}

// BenchmarkProcessRunner_SendStdin benchmarks stdin sending
func BenchmarkProcessRunner_SendStdin(b *testing.B) {
	runner := NewProcessRunner("cat", "test-cat")
	runner.Start()
	defer func() {
		runner.Kill()
		runner.Wait()
	}()
	
	b.ResetTimer()
	
	for i := 0; i < b.N; i++ {
		runner.SendStdin("test\n")
	}
}