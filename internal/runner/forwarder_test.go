package runner

import (
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/logmcp/logmcp/internal/errors"
)

// TestLogForwarder_NewLogForwarder tests forwarder creation
func TestLogForwarder_NewLogForwarder(t *testing.T) {
	forwarder := NewLogForwarder("/tmp/test.log", "test-log")
	
	if forwarder == nil {
		t.Fatal("Expected forwarder to be created")
	}
	
	if forwarder.source != "/tmp/test.log" {
		t.Errorf("Expected source to be '/tmp/test.log', got '%s'", forwarder.source)
	}
	
	if forwarder.label != "test-log" {
		t.Errorf("Expected label to be 'test-log', got '%s'", forwarder.label)
	}
	
	if forwarder.sourceType != SourceTypeFile {
		t.Errorf("Expected sourceType to be 'file', got '%s'", forwarder.sourceType)
	}
}

// TestLogForwarder_NewLogForwarderWithConfig tests custom configuration
func TestLogForwarder_NewLogForwarderWithConfig(t *testing.T) {
	config := LogForwarderConfig{
		PollInterval:   2 * time.Second,
		BufferSize:     128 * 1024,
		MaxLineLength:  128 * 1024,
		FollowRotation: false,
	}
	
	forwarder := NewLogForwarderWithConfig("/tmp/test.log", "test-log", config)
	
	if forwarder.pollInterval != 2*time.Second {
		t.Errorf("Expected pollInterval to be 2s, got %v", forwarder.pollInterval)
	}
	
	if forwarder.bufferSize != 128*1024 {
		t.Errorf("Expected bufferSize to be 128KB, got %d", forwarder.bufferSize)
	}
	
	if forwarder.maxLineLength != 128*1024 {
		t.Errorf("Expected maxLineLength to be 128KB, got %d", forwarder.maxLineLength)
	}
	
	if forwarder.followRotation {
		t.Error("Expected followRotation to be false")
	}
}

// TestLogForwarder_DetermineSourceType tests source type detection
func TestLogForwarder_DetermineSourceType(t *testing.T) {
	tests := []struct {
		source     string
		expected   SourceType
	}{
		{"stdin", SourceTypeStdin},
		{"-", SourceTypeStdin},
		{"/tmp/test.log", SourceTypeFile},
		{"nonexistent.log", SourceTypeFile},
	}
	
	for _, test := range tests {
		result := determineSourceType(test.source)
		if result != test.expected {
			t.Errorf("For source '%s', expected '%s', got '%s'", 
				test.source, test.expected, result)
		}
	}
}

// TestLogForwarder_SetWebSocketClient tests WebSocket client assignment
func TestLogForwarder_SetWebSocketClient(t *testing.T) {
	forwarder := NewLogForwarder("/tmp/test.log", "test-log")
	client := NewWebSocketClient("ws://localhost:8765", "test-label")
	
	forwarder.SetWebSocketClient(client)
	
	if forwarder.client != client {
		t.Error("Expected client to be assigned")
	}
}

// TestLogForwarder_IsRunning tests running state
func TestLogForwarder_IsRunning(t *testing.T) {
	forwarder := NewLogForwarder("/tmp/test.log", "test-log")
	
	if forwarder.IsRunning() {
		t.Error("Expected forwarder to not be running initially")
	}
	
	// Manually set running state
	forwarder.mutex.Lock()
	forwarder.running = true
	forwarder.mutex.Unlock()
	
	if !forwarder.IsRunning() {
		t.Error("Expected forwarder to be running after setting state")
	}
}

// TestLogForwarder_Start_UnsupportedSourceType tests starting with unsupported source
func TestLogForwarder_Start_UnsupportedSourceType(t *testing.T) {
	forwarder := NewLogForwarder("/tmp/test.log", "test-log")
	// Force unsupported source type
	forwarder.sourceType = SourceType("unsupported")
	
	err := forwarder.Start()
	
	if err == nil {
		t.Fatal("Expected error for unsupported source type")
	}
	
	// Check if it's our custom error type
	if !errors.IsType(err, errors.ErrorTypeValidation) {
		t.Errorf("Expected validation error, got %v", err)
	}
}

// TestLogForwarder_Start_FileNotFound tests starting with non-existent file
func TestLogForwarder_Start_FileNotFound(t *testing.T) {
	forwarder := NewLogForwarder("/tmp/nonexistent.log", "test-log")
	
	err := forwarder.Start()
	
	// Should not error - will monitor for file creation
	if err != nil {
		t.Errorf("Expected no error for non-existent file, got %v", err)
	}
	
	if !forwarder.IsRunning() {
		t.Error("Expected forwarder to be running (monitoring for file creation)")
	}
	
	forwarder.Stop()
	forwarder.Wait()
}

// TestLogForwarder_FileForwarding tests file log forwarding
func TestLogForwarder_FileForwarding(t *testing.T) {
	// Create temporary file
	tmpfile, err := ioutil.TempFile("", "logtest")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer os.Remove(tmpfile.Name())
	
	// Write initial content
	tmpfile.WriteString("initial log line\n")
	tmpfile.Close()
	
	var capturedLogs []string
	var mu sync.Mutex
	
	forwarder := NewLogForwarder(tmpfile.Name(), "test-log")
	forwarder.OnLogLine = func(content string) {
		mu.Lock()
		capturedLogs = append(capturedLogs, content)
		mu.Unlock()
	}
	
	err = forwarder.Start()
	if err != nil {
		t.Fatalf("Failed to start forwarder: %v", err)
	}
	defer func() {
		forwarder.Stop()
		forwarder.Wait()
	}()
	
	// Append more content
	file, err := os.OpenFile(tmpfile.Name(), os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		t.Fatalf("Failed to open file for append: %v", err)
	}
	
	file.WriteString("new log line\n")
	file.WriteString("another log line\n")
	file.Close()
	
	// Wait for forwarding
	time.Sleep(2 * time.Second)
	
	// Check captured logs
	mu.Lock()
	defer mu.Unlock()
	
	if len(capturedLogs) == 0 {
		t.Error("Expected to capture log lines")
	}
	
	// Should have captured new lines (not initial content since we seek to end)
	found := false
	for _, log := range capturedLogs {
		if strings.Contains(log, "new log line") || strings.Contains(log, "another log line") {
			found = true
			break
		}
	}
	
	if !found {
		t.Errorf("Expected to find new log lines, got %v", capturedLogs)
	}
}

// TestLogForwarder_StdinForwarding tests stdin forwarding
func TestLogForwarder_StdinForwarding(t *testing.T) {
	var capturedLogs []string
	var mu sync.Mutex
	
	forwarder := NewLogForwarder("stdin", "test-stdin")
	forwarder.OnLogLine = func(content string) {
		mu.Lock()
		capturedLogs = append(capturedLogs, content)
		mu.Unlock()
	}
	
	if forwarder.sourceType != SourceTypeStdin {
		t.Errorf("Expected sourceType to be stdin, got %s", forwarder.sourceType)
	}
	
	// Note: Testing actual stdin forwarding is complex in unit tests
	// as it would require mocking os.Stdin or using pipes
	// We'll just test the setup for now
	
	err := forwarder.Start()
	if err != nil {
		t.Errorf("Expected no error starting stdin forwarder, got %v", err)
	}
	
	if !forwarder.IsRunning() {
		t.Error("Expected forwarder to be running")
	}
	
	forwarder.Stop()
	forwarder.Wait()
}

// TestLogForwarder_NamedPipeForwarding tests named pipe detection
func TestLogForwarder_NamedPipeForwarding(t *testing.T) {
	// Create temporary directory
	tmpdir, err := ioutil.TempDir("", "pipetest")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpdir)
	
	// Create a regular file (not a named pipe)
	regularFile := filepath.Join(tmpdir, "regular.txt")
	ioutil.WriteFile(regularFile, []byte("test"), 0644)
	
	forwarder := NewLogForwarder(regularFile, "test-pipe")
	
	// Should be detected as file, not named pipe
	if forwarder.sourceType != SourceTypeFile {
		t.Errorf("Expected sourceType to be file, got %s", forwarder.sourceType)
	}
	
	// Test with non-existent pipe
	pipePath := filepath.Join(tmpdir, "testpipe")
	forwarder2 := NewLogForwarder(pipePath, "test-pipe2")
	
	// Force it to be treated as named pipe
	forwarder2.sourceType = SourceTypeNamedPipe
	
	err = forwarder2.Start()
	if err == nil {
		t.Error("Expected error for non-existent named pipe")
	}
}

// TestLogForwarder_Stop tests forwarder stop
func TestLogForwarder_Stop(t *testing.T) {
	forwarder := NewLogForwarder("/tmp/test.log", "test-log")
	
	// Start and then stop
	forwarder.Start() // Will monitor for file creation
	
	if !forwarder.IsRunning() {
		t.Error("Expected forwarder to be running")
	}
	
	err := forwarder.Stop()
	if err != nil {
		t.Errorf("Expected no error on stop, got %v", err)
	}
	
	if forwarder.IsRunning() {
		t.Error("Expected forwarder to be stopped")
	}
}

// TestLogForwarder_Close tests forwarder close
func TestLogForwarder_Close(t *testing.T) {
	forwarder := NewLogForwarder("/tmp/test.log", "test-log")
	
	forwarder.Start()
	
	err := forwarder.Close()
	if err != nil {
		t.Errorf("Expected no error on close, got %v", err)
	}
	
	// Check if context was cancelled
	select {
	case <-forwarder.ctx.Done():
		// Expected
	case <-time.After(100 * time.Millisecond):
		t.Error("Expected context to be cancelled")
	}
}

// TestLogForwarder_Health tests health checking
func TestLogForwarder_Health(t *testing.T) {
	forwarder := NewLogForwarder("/tmp/test.log", "test-log")
	
	if !forwarder.IsHealthy() {
		t.Error("Expected forwarder to be healthy initially")
	}
	
	health := forwarder.GetHealth()
	
	if health.Source != "/tmp/test.log" {
		t.Errorf("Expected source in health, got '%s'", health.Source)
	}
	
	if health.SourceType != string(SourceTypeFile) {
		t.Errorf("Expected sourceType in health, got '%s'", health.SourceType)
	}
	
	if health.Label != "test-log" {
		t.Errorf("Expected label in health, got '%s'", health.Label)
	}
	
	// Close forwarder and check health
	forwarder.Close()
	
	if forwarder.IsHealthy() {
		t.Error("Expected forwarder to be unhealthy after close")
	}
}

// TestLogForwarder_WithWebSocket tests integration with WebSocket client
func TestLogForwarder_WithWebSocket(t *testing.T) {
	// Create temporary file
	tmpfile, err := ioutil.TempFile("", "logtest")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer os.Remove(tmpfile.Name())
	tmpfile.Close()
	
	var sentCount int
	var mu sync.Mutex
	
	// Create mock client
	client := NewWebSocketClient("ws://localhost:8765", "test-label")
	// Client uses label for identification now
	client.connMutex.Lock()
	client.connected = true
	client.connMutex.Unlock()
	
	// Count sent messages
	go func() {
		for range client.messageChan {
			mu.Lock()
			sentCount++
			mu.Unlock()
		}
	}()
	
	forwarder := NewLogForwarder(tmpfile.Name(), "test-log")
	forwarder.SetWebSocketClient(client)
	
	err = forwarder.Start()
	if err != nil {
		t.Fatalf("Failed to start forwarder: %v", err)
	}
	defer func() {
		forwarder.Stop()
		forwarder.Wait()
	}()
	
	// Append content to file
	file, err := os.OpenFile(tmpfile.Name(), os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		t.Fatalf("Failed to open file for append: %v", err)
	}
	
	file.WriteString("websocket test line\n")
	file.Close()
	
	// Wait for forwarding
	time.Sleep(2 * time.Second)
	
	// Check if messages were sent
	mu.Lock()
	defer mu.Unlock()
	
	if sentCount == 0 {
		t.Error("Expected to send messages via WebSocket")
	}
}

// TestLogForwarder_GenerateLabelFromSource tests label generation
func TestLogForwarder_GenerateLabelFromSource(t *testing.T) {
	tests := []struct {
		source     string
		sourceType SourceType
		expected   string
	}{
		{"/var/log/nginx/access.log", SourceTypeFile, "access"},
		{"/tmp/app.log", SourceTypeFile, "app"},
		{"test.txt", SourceTypeFile, "test"},
		{"stdin", SourceTypeStdin, "stdin-"},
		{"/tmp/pipe", SourceTypeNamedPipe, "pipe"},
	}
	
	for _, test := range tests {
		result := generateLabelFromSource(test.source, test.sourceType)
		
		if test.sourceType == SourceTypeStdin {
			// For stdin, we expect the prefix
			if !strings.HasPrefix(result, test.expected) {
				t.Errorf("For source '%s', expected prefix '%s', got '%s'", 
					test.source, test.expected, result)
			}
		} else {
			if result != test.expected {
				t.Errorf("For source '%s', expected '%s', got '%s'", 
					test.source, test.expected, result)
			}
		}
	}
}

// TestLogForwarder_FileRotation tests file rotation detection
func TestLogForwarder_FileRotation(t *testing.T) {
	// Create temporary file
	tmpfile, err := ioutil.TempFile("", "rotatetest")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	originalName := tmpfile.Name()
	defer os.Remove(originalName)
	
	tmpfile.WriteString("initial content\n")
	tmpfile.Close()
	
	forwarder := NewLogForwarder(originalName, "test-rotate")
	forwarder.followRotation = true
	
	err = forwarder.Start()
	if err != nil {
		t.Fatalf("Failed to start forwarder: %v", err)
	}
	defer func() {
		forwarder.Stop()
		forwarder.Wait()
	}()
	
	// Give it time to open the file
	time.Sleep(100 * time.Millisecond)
	
	// Simulate rotation by moving the file and creating a new one
	rotatedName := originalName + ".1"
	err = os.Rename(originalName, rotatedName)
	if err != nil {
		t.Fatalf("Failed to rotate file: %v", err)
	}
	defer os.Remove(rotatedName)
	
	// Create new file with same name
	newFile, err := os.Create(originalName)
	if err != nil {
		t.Fatalf("Failed to create new file: %v", err)
	}
	newFile.WriteString("new content after rotation\n")
	newFile.Close()
	
	// Test the rotation detection
	if forwarder.file != nil {
		err = forwarder.checkFileRotation()
		if err != nil {
			t.Errorf("Expected no error checking file rotation, got %v", err)
		}
	}
}

// BenchmarkLogForwarder_ForwardLogLine benchmarks log line forwarding
func BenchmarkLogForwarder_ForwardLogLine(b *testing.B) {
	forwarder := NewLogForwarder("/tmp/test.log", "test-log")
	
	b.ResetTimer()
	
	for i := 0; i < b.N; i++ {
		forwarder.forwardLogLine("test log line")
	}
}