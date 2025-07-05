// Package runner provides log forwarding functionality for LogMCP.
//
// The log forwarder component handles:
// - File tailing with inotify/polling
// - stdin reading and buffering
// - Named pipe monitoring
// - Integration with WebSocket client for log streaming
// - Automatic file rotation detection
//
// Example usage:
//
//	forwarder := runner.NewLogForwarder("/var/log/app.log", "app-logs")
//	forwarder.SetWebSocketClient(client)
//
//	if err := forwarder.Start(); err != nil {
//		log.Fatal(err)
//	}
//
//	// Run until interrupted
//	forwarder.Run()
package runner

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/bebsworthy/logmcp/internal/config"
	"github.com/bebsworthy/logmcp/internal/errors"
)

// SourceType represents the type of log source
type SourceType string

const (
	SourceTypeFile      SourceType = "file"
	SourceTypeStdin     SourceType = "stdin"
	SourceTypeNamedPipe SourceType = "named_pipe"
)

// LogForwarder forwards logs from various sources to the WebSocket server
type LogForwarder struct {
	// Source configuration
	source     string
	sourceType SourceType
	label      string

	// State
	running bool
	mutex   sync.RWMutex

	// File-specific state
	file     *os.File
	fileInfo os.FileInfo

	// Context and lifecycle
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	// WebSocket client for log streaming
	client *WebSocketClient

	// Configuration
	pollInterval   time.Duration
	bufferSize     int
	maxLineLength  int
	followRotation bool

	// Callbacks
	OnLogLine func(content string)
	OnError   func(error)
}

// LogForwarderConfig contains configuration options for the log forwarder
type LogForwarderConfig struct {
	PollInterval   time.Duration
	BufferSize     int
	MaxLineLength  int
	FollowRotation bool
}

// DefaultLogForwarderConfig returns default configuration for the log forwarder
func DefaultLogForwarderConfig() LogForwarderConfig {
	return LogForwarderConfig{
		PollInterval:   1 * time.Second,
		BufferSize:     64 * 1024, // 64KB
		MaxLineLength:  64 * 1024, // 64KB per line
		FollowRotation: true,
	}
}

// NewLogForwarder creates a new log forwarder
func NewLogForwarder(source, label string) *LogForwarder {
	return NewLogForwarderWithConfig(source, label, DefaultLogForwarderConfig())
}

// NewLogForwarderWithLogMCPConfig creates a new log forwarder using LogMCP configuration
func NewLogForwarderWithLogMCPConfig(source, label, serverURL string, cfg *config.Config) *LogForwarder {
	ctx, cancel := context.WithCancel(context.Background())

	sourceType := determineSourceType(source)

	// Parse buffer configuration
	maxLineSize, _ := config.ParseSize(cfg.Buffer.MaxLineSize) // Use default if parse fails
	if maxLineSize == 0 {
		maxLineSize = 64 * 1024 // Default 64KB
	}

	// Create WebSocket client with configuration
	wsClientConfig := WebSocketClientConfig{
		ReconnectDelay:       cfg.WebSocket.ReconnectInitialDelay,
		MaxReconnectDelay:    cfg.WebSocket.ReconnectMaxDelay,
		MaxReconnectAttempts: cfg.WebSocket.ReconnectMaxAttempts,
		PingInterval:         cfg.WebSocket.PingInterval,
		WriteTimeout:         cfg.WebSocket.WriteTimeout,
		ReadTimeout:          cfg.WebSocket.ReadTimeout,
	}

	wsClient := NewWebSocketClientWithConfig(serverURL, label, wsClientConfig)
	wsClient.SetCommand("", "", []string{"log_forwarder"})

	return &LogForwarder{
		source:         source,
		sourceType:     sourceType,
		label:          label,
		ctx:            ctx,
		cancel:         cancel,
		client:         wsClient,
		pollInterval:   1 * time.Second, // Fixed polling interval
		bufferSize:     64 * 1024,       // 64KB buffer
		maxLineLength:  int(maxLineSize),
		followRotation: true,
	}
}

// NewLogForwarderWithConfig creates a new log forwarder with custom configuration
func NewLogForwarderWithConfig(source, label string, config LogForwarderConfig) *LogForwarder {
	ctx, cancel := context.WithCancel(context.Background())

	sourceType := determineSourceType(source)

	return &LogForwarder{
		source:         source,
		sourceType:     sourceType,
		label:          label,
		ctx:            ctx,
		cancel:         cancel,
		pollInterval:   config.PollInterval,
		bufferSize:     config.BufferSize,
		maxLineLength:  config.MaxLineLength,
		followRotation: config.FollowRotation,
	}
}

// SetWebSocketClient sets the WebSocket client for log streaming
func (lf *LogForwarder) SetWebSocketClient(client *WebSocketClient) {
	lf.client = client
}

// GetWebSocketClient returns the WebSocket client
func (lf *LogForwarder) GetWebSocketClient() *WebSocketClient {
	return lf.client
}

// Start starts the log forwarder
func (lf *LogForwarder) Start() error {
	lf.mutex.Lock()
	defer lf.mutex.Unlock()

	if lf.running {
		return fmt.Errorf("forwarder already running")
	}

	switch lf.sourceType {
	case SourceTypeFile:
		if err := lf.startFileForwarding(); err != nil {
			return errors.WrapError(err, "failed to start file forwarding")
		}
	case SourceTypeStdin:
		if err := lf.startStdinForwarding(); err != nil {
			return errors.WrapError(err, "failed to start stdin forwarding")
		}
	case SourceTypeNamedPipe:
		if err := lf.startNamedPipeForwarding(); err != nil {
			return errors.WrapError(err, "failed to start named pipe forwarding")
		}
	default:
		return errors.ValidationError("UNSUPPORTED_SOURCE_TYPE", fmt.Sprintf("Unsupported source type: %s", lf.sourceType), nil)
	}

	lf.running = true
	log.Printf("Log forwarder started: %s (%s)", lf.source, lf.sourceType)
	return nil
}

// startFileForwarding starts forwarding from a file
func (lf *LogForwarder) startFileForwarding() error {
	// Open file for reading
	file, err := os.Open(lf.source)
	if err != nil {
		// File might not exist yet, create a placeholder
		if os.IsNotExist(err) {
			log.Printf("File does not exist yet, will monitor for creation: %s", lf.source)
			lf.wg.Add(1)
			go lf.monitorFileCreation()
			return nil
		}
		return errors.FileError("FILE_OPEN_FAILED", "Failed to open file", err)
	}

	// Get file info
	fileInfo, err := file.Stat()
	if err != nil {
		file.Close()
		return fmt.Errorf("failed to stat file: %w", err)
	}

	lf.file = file
	lf.fileInfo = fileInfo

	// Seek to end of file to start tailing
	if _, err := file.Seek(0, io.SeekEnd); err != nil {
		file.Close()
		return fmt.Errorf("failed to seek to end of file: %w", err)
	}

	// Start tailing goroutine
	lf.wg.Add(1)
	go lf.tailFile()

	return nil
}

// startStdinForwarding starts forwarding from stdin
func (lf *LogForwarder) startStdinForwarding() error {
	// Start stdin reading goroutine
	lf.wg.Add(1)
	go lf.readStdin()
	return nil
}

// startNamedPipeForwarding starts forwarding from a named pipe
func (lf *LogForwarder) startNamedPipeForwarding() error {
	// Verify it's a named pipe
	if info, err := os.Stat(lf.source); err != nil {
		return fmt.Errorf("named pipe not found: %w", err)
	} else if info.Mode()&os.ModeNamedPipe == 0 {
		return fmt.Errorf("not a named pipe: %s", lf.source)
	}

	// Start named pipe reading goroutine
	lf.wg.Add(1)
	go lf.readNamedPipe()
	return nil
}

// monitorFileCreation monitors for file creation
func (lf *LogForwarder) monitorFileCreation() {
	defer lf.wg.Done()

	ticker := time.NewTicker(lf.pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-lf.ctx.Done():
			return
		case <-ticker.C:
			if _, err := os.Stat(lf.source); err == nil {
				// File exists now, start forwarding
				if err := lf.startFileForwarding(); err != nil {
					if lf.OnError != nil {
						lf.OnError(fmt.Errorf("failed to start file forwarding after creation: %w", err))
					}
				}
				return
			}
		}
	}
}

// tailFile tails a file and forwards new lines
func (lf *LogForwarder) tailFile() {
	defer lf.wg.Done()
	defer func() {
		if lf.file != nil {
			lf.file.Close()
		}
	}()

	ticker := time.NewTicker(lf.pollInterval)
	defer ticker.Stop()

	scanner := bufio.NewScanner(lf.file)
	scanner.Buffer(make([]byte, lf.bufferSize), lf.maxLineLength)

	for {
		select {
		case <-lf.ctx.Done():
			return
		case <-ticker.C:
			// Check for file rotation
			if lf.followRotation {
				if err := lf.checkFileRotation(); err != nil {
					if lf.OnError != nil {
						lf.OnError(fmt.Errorf("file rotation check failed: %w", err))
					}
					continue
				}
			}

			// Read new lines
			for scanner.Scan() {
				line := scanner.Text()
				lf.forwardLogLine(line)
			}

			if err := scanner.Err(); err != nil {
				if lf.OnError != nil {
					lf.OnError(fmt.Errorf("scanner error: %w", err))
				}
			}
		}
	}
}

// checkFileRotation checks if the file has been rotated and reopens if necessary
func (lf *LogForwarder) checkFileRotation() error {
	// Check if current file still exists and matches our file descriptor
	currentInfo, err := lf.file.Stat()
	if err != nil {
		return fmt.Errorf("failed to stat current file: %w", err)
	}

	// Check if file on disk exists
	diskInfo, err := os.Stat(lf.source)
	if err != nil {
		if os.IsNotExist(err) {
			// File was removed, wait for new one
			return nil
		}
		return fmt.Errorf("failed to stat file on disk: %w", err)
	}

	// Use cross-platform file comparison to detect rotation
	// os.SameFile works on all platforms and handles both inode-based (Unix)
	// and other file identification methods (Windows)
	if !os.SameFile(currentInfo, diskInfo) {
		// File was rotated, reopen
		log.Printf("File rotation detected, reopening: %s", lf.source)

		lf.file.Close()

		newFile, err := os.Open(lf.source)
		if err != nil {
			return fmt.Errorf("failed to reopen rotated file: %w", err)
		}

		lf.file = newFile
		lf.fileInfo = diskInfo
	}

	return nil
}

// readStdin reads from stdin and forwards lines
func (lf *LogForwarder) readStdin() {
	defer lf.wg.Done()

	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, lf.bufferSize), lf.maxLineLength)

	for scanner.Scan() {
		select {
		case <-lf.ctx.Done():
			return
		default:
			line := scanner.Text()
			lf.forwardLogLine(line)
		}
	}

	if err := scanner.Err(); err != nil {
		if lf.OnError != nil {
			lf.OnError(fmt.Errorf("stdin scanner error: %w", err))
		}
	}
}

// readNamedPipe reads from a named pipe and forwards lines
func (lf *LogForwarder) readNamedPipe() {
	defer lf.wg.Done()

	for {
		select {
		case <-lf.ctx.Done():
			return
		default:
			// Open named pipe for reading
			file, err := os.Open(lf.source)
			if err != nil {
				if lf.OnError != nil {
					lf.OnError(fmt.Errorf("failed to open named pipe: %w", err))
				}
				time.Sleep(lf.pollInterval)
				continue
			}

			scanner := bufio.NewScanner(file)
			scanner.Buffer(make([]byte, lf.bufferSize), lf.maxLineLength)

			for scanner.Scan() {
				select {
				case <-lf.ctx.Done():
					file.Close()
					return
				default:
					line := scanner.Text()
					lf.forwardLogLine(line)
				}
			}

			if err := scanner.Err(); err != nil {
				if lf.OnError != nil {
					lf.OnError(fmt.Errorf("named pipe scanner error: %w", err))
				}
			}

			file.Close()

			// Named pipes need to be reopened after EOF
			time.Sleep(100 * time.Millisecond)
		}
	}
}

// forwardLogLine forwards a log line to the WebSocket client
func (lf *LogForwarder) forwardLogLine(line string) {
	// Send to WebSocket client
	if lf.client != nil {
		// For log forwarders, we don't have a real PID, so use 0
		lf.client.SendLogMessage(line, "stdout", 0)
	}

	// Call callback
	if lf.OnLogLine != nil {
		lf.OnLogLine(line)
	}
}

// Stop stops the log forwarder
func (lf *LogForwarder) Stop() error {
	lf.mutex.Lock()
	defer lf.mutex.Unlock()

	if !lf.running {
		return nil
	}

	// Cancel context to signal shutdown
	lf.cancel()

	// Close file if open
	if lf.file != nil {
		lf.file.Close()
		lf.file = nil
	}

	lf.running = false
	return nil
}

// Wait waits for the forwarder to complete
func (lf *LogForwarder) Wait() error {
	lf.wg.Wait()
	return nil
}

// Close shuts down the log forwarder
func (lf *LogForwarder) Close() error {
	// Always cancel context regardless of running state
	lf.cancel()

	if err := lf.Stop(); err != nil {
		return err
	}
	return lf.Wait()
}

// IsRunning returns true if the forwarder is currently running
func (lf *LogForwarder) IsRunning() bool {
	lf.mutex.RLock()
	defer lf.mutex.RUnlock()
	return lf.running
}

// GetSource returns the log source
func (lf *LogForwarder) GetSource() string {
	return lf.source
}

// GetSourceType returns the source type
func (lf *LogForwarder) GetSourceType() SourceType {
	return lf.sourceType
}

// GetLabel returns the forwarder label
func (lf *LogForwarder) GetLabel() string {
	return lf.label
}

// Run runs the forwarder until interrupted
func (lf *LogForwarder) Run() error {
	if err := lf.Start(); err != nil {
		return err
	}

	// Wait for completion
	return lf.Wait()
}

// determineSourceType determines the source type from a source string
func determineSourceType(source string) SourceType {
	if source == "stdin" || source == "-" {
		return SourceTypeStdin
	}

	// Check if it's a file or named pipe
	if info, err := os.Stat(source); err == nil {
		if info.Mode()&os.ModeNamedPipe != 0 {
			return SourceTypeNamedPipe
		}
		if info.Mode().IsRegular() {
			return SourceTypeFile
		}
	}

	// Default to file (even if it doesn't exist yet)
	return SourceTypeFile
}

// generateLabelFromSource generates a label based on the source
func generateLabelFromSource(source string, sourceType SourceType) string {
	switch sourceType {
	case SourceTypeStdin:
		return "stdin-" + fmt.Sprintf("%d", time.Now().Unix())
	case SourceTypeFile:
		// Use basename of file path
		base := filepath.Base(source)
		// Remove extension for cleaner label
		if ext := filepath.Ext(base); ext != "" {
			base = strings.TrimSuffix(base, ext)
		}
		return base
	case SourceTypeNamedPipe:
		return filepath.Base(source)
	default:
		return "forwarder-" + fmt.Sprintf("%d", time.Now().Unix())
	}
}

// Health check methods

// IsHealthy returns true if the log forwarder is operating normally
func (lf *LogForwarder) IsHealthy() bool {
	return lf.ctx.Err() == nil
}

// GetHealth returns detailed health information about the log forwarder
func (lf *LogForwarder) GetHealth() LogForwarderHealth {
	lf.mutex.RLock()
	defer lf.mutex.RUnlock()

	return LogForwarderHealth{
		IsHealthy:  lf.IsHealthy(),
		Running:    lf.running,
		Source:     lf.source,
		SourceType: string(lf.sourceType),
		Label:      lf.label,
	}
}

// LogForwarderHealth represents the health status of the log forwarder
type LogForwarderHealth struct {
	IsHealthy  bool   `json:"is_healthy"`
	Running    bool   `json:"running"`
	Source     string `json:"source"`
	SourceType string `json:"source_type"`
	Label      string `json:"label"`
}
