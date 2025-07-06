// Package runner provides process execution and management functionality for LogMCP.
//
// The process runner component handles:
// - Process spawning and lifecycle management
// - stdout/stderr capture and streaming
// - Signal handling and graceful shutdown
// - Process restart capabilities
// - Integration with WebSocket client for log streaming
//
// Example usage:
//
//	runner := runner.NewProcessRunner("npm run server", "backend", "/app", nil)
//	runner.SetWebSocketClient(client)
//
//	if err := runner.Start(); err != nil {
//		log.Fatal(err)
//	}
//
//	// Wait for process or handle signals
//	runner.Wait()
package runner

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"log"
	"log/slog"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/bebsworthy/logmcp/internal/config"
	"github.com/bebsworthy/logmcp/internal/errors"
	"github.com/bebsworthy/logmcp/internal/protocol"
)

// ProcessRunner manages process execution and log streaming
type ProcessRunner struct {
	// Process configuration
	command     string
	args        []string
	label       string
	workingDir  string
	environment map[string]string

	// Process state
	cmd      *exec.Cmd
	process  *os.Process
	pid      int
	running  bool
	exitCode *int
	mutex    sync.RWMutex

	// Pipes and I/O
	stdout      io.ReadCloser
	stderr      io.ReadCloser
	stdin       io.WriteCloser
	stdinBuffer chan string

	// Context and lifecycle
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	// WebSocket client for log streaming
	client *WebSocketClient

	// Configuration
	restartOnFailure bool
	maxRestarts      int
	restartCount     int
	restartDelay     time.Duration

	// Callbacks
	OnProcessStart func(pid int)
	OnProcessExit  func(exitCode int)
	OnLogLine      func(content, stream string)
	OnError        func(error)
}

// ProcessRunnerConfig contains configuration options for the process runner
type ProcessRunnerConfig struct {
	WorkingDir       string
	Environment      map[string]string
	RestartOnFailure bool
	MaxRestarts      int
	RestartDelay     time.Duration
}

// DefaultProcessRunnerConfig returns default configuration for the process runner
func DefaultProcessRunnerConfig() ProcessRunnerConfig {
	return ProcessRunnerConfig{
		WorkingDir:       "",
		Environment:      nil,
		RestartOnFailure: false,
		MaxRestarts:      3,
		RestartDelay:     5 * time.Second,
	}
}

// NewProcessRunner creates a new process runner
func NewProcessRunner(command, label string) *ProcessRunner {
	return NewProcessRunnerWithConfig(command, label, DefaultProcessRunnerConfig())
}

// NewProcessRunnerWithLogMCPConfig creates a new process runner using LogMCP configuration
func NewProcessRunnerWithLogMCPConfig(command, label, serverURL string, cfg *config.Config) *ProcessRunner {
	ctx, cancel := context.WithCancel(context.Background())

	// Parse command into parts
	parts := strings.Fields(command)
	if len(parts) == 0 {
		parts = []string{command}
	}

	workingDir := cfg.Process.DefaultWorkingDir
	if workingDir == "" || workingDir == "." {
		if wd, err := os.Getwd(); err == nil {
			workingDir = wd
		}
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
	wsClient.SetCommand(command, workingDir, []string{"process"})

	pr := &ProcessRunner{
		command:          parts[0],
		args:             parts[1:],
		label:            label,
		workingDir:       workingDir,
		environment:      make(map[string]string),
		ctx:              ctx,
		cancel:           cancel,
		client:           wsClient,
		restartOnFailure: false,
		maxRestarts:      cfg.Process.MaxRestarts,
		restartDelay:     cfg.Process.RestartDelay,
		stdinBuffer:      make(chan string, 100),
	}

	// Set up client callbacks
	if wsClient != nil {
		wsClient.OnCommand = pr.handleCommand
		wsClient.OnStdinMessage = pr.handleStdinMessage
	}

	return pr
}

// NewProcessRunnerWithConfig creates a new process runner with custom configuration
func NewProcessRunnerWithConfig(command, label string, config ProcessRunnerConfig) *ProcessRunner {
	ctx, cancel := context.WithCancel(context.Background())

	// Parse command into parts
	parts := strings.Fields(command)
	if len(parts) == 0 {
		parts = []string{command}
	}

	workingDir := config.WorkingDir
	if workingDir == "" {
		if wd, err := os.Getwd(); err == nil {
			workingDir = wd
		}
	}

	return &ProcessRunner{
		command:          parts[0],
		args:             parts[1:],
		label:            label,
		workingDir:       workingDir,
		environment:      config.Environment,
		ctx:              ctx,
		cancel:           cancel,
		restartOnFailure: config.RestartOnFailure,
		maxRestarts:      config.MaxRestarts,
		restartDelay:     config.RestartDelay,
		stdinBuffer:      make(chan string, 100),
	}
}

// SetWebSocketClient sets the WebSocket client for log streaming
func (pr *ProcessRunner) SetWebSocketClient(client *WebSocketClient) {
	pr.client = client

	// Set up client callbacks
	if client != nil {
		client.OnCommand = pr.handleCommand
		client.OnStdinMessage = pr.handleStdinMessage
	}
}

// Start starts the process
func (pr *ProcessRunner) Start() error {
	pr.mutex.Lock()
	defer pr.mutex.Unlock()

	if pr.running {
		return fmt.Errorf("process already running")
	}

	// Create command with main context (not startup timeout)
	// The process should run until completion or explicit cancellation
	pr.cmd = exec.Command(pr.command, pr.args...)
	pr.cmd.Dir = pr.workingDir

	// Set environment
	pr.cmd.Env = os.Environ()
	for key, value := range pr.environment {
		pr.cmd.Env = append(pr.cmd.Env, fmt.Sprintf("%s=%s", key, value))
	}

	// Create pipes
	stdout, err := pr.cmd.StdoutPipe()
	if err != nil {
		return errors.ProcessError("PIPE_CREATION_FAILED", "Failed to create stdout pipe", err)
	}
	pr.stdout = stdout

	stderr, err := pr.cmd.StderrPipe()
	if err != nil {
		return errors.ProcessError("PIPE_CREATION_FAILED", "Failed to create stderr pipe", err)
	}
	pr.stderr = stderr

	stdin, err := pr.cmd.StdinPipe()
	if err != nil {
		return errors.ProcessError("PIPE_CREATION_FAILED", "Failed to create stdin pipe", err)
	}
	pr.stdin = stdin

	// Start the process
	if err := pr.cmd.Start(); err != nil {
		return errors.ProcessError("PROCESS_START_FAILED", "Failed to start process", err)
	}

	// Update state
	pr.process = pr.cmd.Process
	pr.pid = pr.cmd.Process.Pid
	pr.running = true
	pr.exitCode = nil

	// Send status update to server
	if pr.client != nil {
		if err := pr.client.SendStatusMessage(protocol.StatusRunning, pr.pid, nil); err != nil {
			slog.Warn("Failed to send status message",
				slog.String("label", pr.label),
				slog.String("error", err.Error()))
		}
	}

	// Start goroutines for I/O handling
	pr.wg.Add(4)
	go pr.handleStdout()
	go pr.handleStderr()
	go pr.handleStdin()
	go pr.handleProcess()

	// Call callback
	if pr.OnProcessStart != nil {
		pr.OnProcessStart(pr.pid)
	}

	log.Printf("Process started: %s (PID: %d)", pr.getCommandString(), pr.pid)
	return nil
}

// handleStdout reads and streams stdout
func (pr *ProcessRunner) handleStdout() {
	defer pr.wg.Done()
	defer func() {
		if pr.stdout != nil {
			_ = pr.stdout.Close()
		}
	}()

	reader := bufio.NewReaderSize(pr.stdout, 64*1024) // 64KB buffer
	lineChan := make(chan string, 100) // Buffer for performance
	errChan := make(chan error, 1)
	
	// Start a goroutine to read lines
	readerDone := make(chan struct{})
	go func() {
		defer close(readerDone)
		defer close(lineChan)
		defer close(errChan)
		
		for {
			line, err := reader.ReadString('\n')
			if err != nil {
				if err != io.EOF {
					select {
					case errChan <- err:
					case <-pr.ctx.Done():
					}
				} else if len(line) > 0 {
					// Handle partial line at EOF
					select {
					case lineChan <- strings.TrimRight(line, "\n\r"):
					case <-pr.ctx.Done():
					}
				}
				return
			}
			
			// Send line without blocking indefinitely
			select {
			case lineChan <- strings.TrimRight(line, "\n\r"):
			case <-pr.ctx.Done():
				return
			}
		}
	}()

	// Process lines until done
	for {
		select {
		case <-pr.ctx.Done():
			// Context cancelled, close the pipe to unblock the reader
			if pr.stdout != nil {
				_ = pr.stdout.Close()
			}
			// Wait briefly for reader to finish
			select {
			case <-readerDone:
			case <-time.After(100 * time.Millisecond):
				// Reader didn't finish in time, but we need to exit
			}
			return
			
		case line, ok := <-lineChan:
			if !ok {
				// Channel closed, reader finished
				return
			}
			
			// Send to WebSocket client
			if pr.client != nil {
				pr.client.SendLogMessage(line, "stdout", pr.pid)
			}

			// Call callback
			if pr.OnLogLine != nil {
				pr.OnLogLine(line, "stdout")
			}
			
		case err, ok := <-errChan:
			if !ok {
				// Channel closed
				return
			}
			if err != nil && pr.OnError != nil {
				pr.OnError(fmt.Errorf("stdout read error: %w", err))
			}
			// Wait for reader to finish
			<-readerDone
			return
		}
	}
}

// handleStderr reads and streams stderr
func (pr *ProcessRunner) handleStderr() {
	defer pr.wg.Done()
	defer func() {
		if pr.stderr != nil {
			pr.stderr.Close()
		}
	}()

	reader := bufio.NewReaderSize(pr.stderr, 64*1024) // 64KB buffer
	lineChan := make(chan string, 100) // Buffer for performance
	errChan := make(chan error, 1)
	
	// Start a goroutine to read lines
	readerDone := make(chan struct{})
	go func() {
		defer close(readerDone)
		defer close(lineChan)
		defer close(errChan)
		
		for {
			line, err := reader.ReadString('\n')
			if err != nil {
				if err != io.EOF {
					select {
					case errChan <- err:
					case <-pr.ctx.Done():
					}
				} else if len(line) > 0 {
					// Handle partial line at EOF
					select {
					case lineChan <- strings.TrimRight(line, "\n\r"):
					case <-pr.ctx.Done():
					}
				}
				return
			}
			
			// Send line without blocking indefinitely
			select {
			case lineChan <- strings.TrimRight(line, "\n\r"):
			case <-pr.ctx.Done():
				return
			}
		}
	}()

	// Process lines until done
	for {
		select {
		case <-pr.ctx.Done():
			// Context cancelled, close the pipe to unblock the reader
			if pr.stderr != nil {
				_ = pr.stderr.Close()
			}
			// Wait briefly for reader to finish
			select {
			case <-readerDone:
			case <-time.After(100 * time.Millisecond):
				// Reader didn't finish in time, but we need to exit
			}
			return
			
		case line, ok := <-lineChan:
			if !ok {
				// Channel closed, reader finished
				return
			}
			
			// Send to WebSocket client
			if pr.client != nil {
				pr.client.SendLogMessage(line, "stderr", pr.pid)
			}

			// Call callback
			if pr.OnLogLine != nil {
				pr.OnLogLine(line, "stderr")
			}
			
		case err, ok := <-errChan:
			if !ok {
				// Channel closed
				return
			}
			if err != nil && pr.OnError != nil {
				pr.OnError(fmt.Errorf("stderr read error: %w", err))
			}
			// Wait for reader to finish
			<-readerDone
			return
		}
	}
}

// handleStdin processes stdin input
func (pr *ProcessRunner) handleStdin() {
	defer pr.wg.Done()
	defer func() {
		if pr.stdin != nil {
			pr.stdin.Close()
		}
	}()

	for {
		select {
		case <-pr.ctx.Done():
			return
		case input, ok := <-pr.stdinBuffer:
			if !ok {
				// Channel closed
				return
			}
			if _, err := pr.stdin.Write([]byte(input)); err != nil {
				if pr.OnError != nil {
					pr.OnError(fmt.Errorf("stdin write error: %w", err))
				}
				return
			}
		}
	}
}

// handleProcess waits for process completion
func (pr *ProcessRunner) handleProcess() {
	defer pr.wg.Done()

	// Use channel to handle process completion with context cancellation
	done := make(chan error, 1)
	go func() {
		done <- pr.cmd.Wait()
	}()

	var err error
	select {
	case err = <-done:
		// Process completed normally
	case <-pr.ctx.Done():
		// Context cancelled, kill process and wait for cleanup
		if pr.cmd.Process != nil {
			pr.cmd.Process.Kill()
		}
		// Wait for cmd.Wait() to return after kill
		<-done
		return
	}

	// Update state
	pr.mutex.Lock()
	pr.running = false
	exitCode := 0
	if err != nil {
		if exitError, ok := err.(*exec.ExitError); ok {
			exitCode = exitError.ExitCode()
		} else {
			exitCode = 1
		}
	}
	pr.exitCode = &exitCode
	pr.mutex.Unlock()

	// Determine status
	var status protocol.SessionStatus
	if exitCode == 0 {
		status = protocol.StatusStopped
	} else {
		status = protocol.StatusCrashed
	}

	// Send status update to server
	if pr.client != nil {
		pr.client.SendStatusMessage(status, pr.pid, &exitCode)
	}

	// Call callback
	if pr.OnProcessExit != nil {
		pr.OnProcessExit(exitCode)
	}

	log.Printf("Process exited: %s (PID: %d, Exit Code: %d)", pr.getCommandString(), pr.pid, exitCode)

	// Cancel context to signal all goroutines to terminate
	pr.cancel()

	// Handle restart if configured
	if pr.restartOnFailure && exitCode != 0 && pr.restartCount < pr.maxRestarts {
		pr.restartCount++
		log.Printf("Restarting process in %v (attempt %d/%d)", pr.restartDelay, pr.restartCount, pr.maxRestarts)

		// Send restarting status
		if pr.client != nil {
			pr.client.SendStatusMessage(protocol.StatusRestarting, pr.pid, &exitCode)
		}

		// Wait for restart delay with context cancellation
		select {
		case <-pr.ctx.Done():
			return
		case <-time.After(pr.restartDelay):
			if err := pr.Start(); err != nil && pr.OnError != nil {
				pr.OnError(fmt.Errorf("restart failed: %w", err))
			}
		}
	}
}

// SendStdin sends input to the process stdin
func (pr *ProcessRunner) SendStdin(input string) error {
	if !pr.IsRunning() {
		return fmt.Errorf("process not running")
	}

	select {
	case pr.stdinBuffer <- input:
		return nil
	case <-pr.ctx.Done():
		return pr.ctx.Err()
	default:
		return fmt.Errorf("stdin buffer full")
	}
}

// handleStdinMessage handles stdin messages from WebSocket client
func (pr *ProcessRunner) handleStdinMessage(input string) error {
	return pr.SendStdin(input)
}

// handleCommand handles control commands from WebSocket client
func (pr *ProcessRunner) handleCommand(action string, signal *protocol.Signal) error {
	switch action {
	case "restart":
		return pr.Restart()
	case "signal":
		if signal == nil {
			return fmt.Errorf("signal parameter required for signal action")
		}
		return pr.SendSignal(string(*signal))
	default:
		return fmt.Errorf("unsupported action: %s", action)
	}
}

// Restart restarts the process
func (pr *ProcessRunner) Restart() error {
	log.Printf("Restarting process: %s", pr.getCommandString())

	// Stop current process
	if err := pr.Stop(); err != nil {
		return fmt.Errorf("failed to stop process for restart: %w", err)
	}

	// Wait for process to fully stop
	pr.Wait()

	// Start new process
	return pr.Start()
}

// SendSignal sends a signal to the process
func (pr *ProcessRunner) SendSignal(signalName string) error {
	pr.mutex.RLock()
	process := pr.process
	running := pr.running
	pr.mutex.RUnlock()

	if !running || process == nil {
		return fmt.Errorf("process not running")
	}

	// Map signal name to syscall.Signal
	var sig syscall.Signal
	switch signalName {
	case "SIGTERM":
		sig = syscall.SIGTERM
	case "SIGKILL":
		sig = syscall.SIGKILL
	case "SIGINT":
		sig = syscall.SIGINT
	case "SIGHUP":
		sig = syscall.SIGHUP
	case "SIGUSR1":
		sig = syscall.SIGUSR1
	case "SIGUSR2":
		sig = syscall.SIGUSR2
	default:
		return fmt.Errorf("unsupported signal: %s", signalName)
	}

	log.Printf("Sending signal %s to process %d", signalName, pr.pid)
	return process.Signal(sig)
}

// Stop stops the process gracefully
func (pr *ProcessRunner) Stop() error {
	pr.mutex.RLock()
	process := pr.process
	running := pr.running
	pr.mutex.RUnlock()

	if !running || process == nil {
		return nil // Already stopped
	}

	// Send SIGTERM first
	if err := process.Signal(syscall.SIGTERM); err != nil {
		// If SIGTERM fails, try SIGKILL
		if killErr := process.Kill(); killErr != nil {
			return killErr
		}
		// Update running state after successful kill
		pr.mutex.Lock()
		pr.running = false
		pr.mutex.Unlock()
		return nil
	}

	// Wait for graceful shutdown with timeout
	done := make(chan struct{})
	go func() {
		pr.cmd.Wait()
		close(done)
	}()

	select {
	case <-done:
		// Process stopped successfully, update running state
		pr.mutex.Lock()
		pr.running = false
		pr.mutex.Unlock()
		return nil
	case <-time.After(10 * time.Second):
		// Force kill if graceful shutdown takes too long
		if err := process.Kill(); err != nil {
			return err
		}
		// Update running state after successful kill
		pr.mutex.Lock()
		pr.running = false
		pr.mutex.Unlock()
		return nil
	}
}

// Kill forcefully kills the process
func (pr *ProcessRunner) Kill() error {
	pr.mutex.RLock()
	process := pr.process
	running := pr.running
	pr.mutex.RUnlock()

	if !running || process == nil {
		return nil // Already stopped
	}

	log.Printf("Killing process: %d", pr.pid)
	return process.Kill()
}

// Wait waits for the process to complete
func (pr *ProcessRunner) Wait() error {
	if pr.cmd == nil {
		return nil
	}

	// Wait for all goroutines to complete with timeout
	done := make(chan struct{})
	go func() {
		pr.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		return nil
	case <-time.After(30 * time.Second):
		log.Printf("Warning: goroutines did not complete within timeout")
		return nil
	}
}

// Close shuts down the process runner
func (pr *ProcessRunner) Close() error {
	// Cancel context to signal shutdown
	pr.cancel()

	// Close stdin buffer channel to unblock handleStdin goroutine
	if pr.stdinBuffer != nil {
		close(pr.stdinBuffer)
	}

	// Stop process if running
	if pr.IsRunning() {
		pr.Stop()
	}

	// Wait for completion
	pr.Wait()

	return nil
}

// IsRunning returns true if the process is currently running
func (pr *ProcessRunner) IsRunning() bool {
	pr.mutex.RLock()
	defer pr.mutex.RUnlock()
	return pr.running
}

// GetPID returns the process ID
func (pr *ProcessRunner) GetPID() int {
	pr.mutex.RLock()
	defer pr.mutex.RUnlock()
	return pr.pid
}

// GetExitCode returns the exit code if the process has exited
func (pr *ProcessRunner) GetExitCode() *int {
	pr.mutex.RLock()
	defer pr.mutex.RUnlock()
	return pr.exitCode
}

// GetLabel returns the process label
func (pr *ProcessRunner) GetLabel() string {
	return pr.label
}

// getCommandString returns the full command string for logging
func (pr *ProcessRunner) getCommandString() string {
	if len(pr.args) == 0 {
		return pr.command
	}
	return pr.command + " " + strings.Join(pr.args, " ")
}

// WaitForProcessCompletion waits for the process to exit without timeout
func (pr *ProcessRunner) WaitForProcessCompletion() error {
	if pr.cmd == nil {
		return nil
	}

	// Create a channel to signal when the process exits
	processDone := make(chan struct{})
	
	// Monitor the handleProcess goroutine completion
	go func() {
		// Wait for the process handler goroutine to finish
		// This happens when the process exits
		for {
			pr.mutex.RLock()
			running := pr.running
			pr.mutex.RUnlock()
			
			if !running {
				close(processDone)
				return
			}
			time.Sleep(100 * time.Millisecond)
		}
	}()

	// Wait for process to exit
	<-processDone
	
	// Process has exited normally, return nil even if context was cancelled
	// The context is cancelled by handleProcess after the process exits, which is normal
	return nil
}

// RunWithSignalHandling runs the process with signal handling for graceful shutdown
func (pr *ProcessRunner) RunWithSignalHandling() error {
	// Set up signal handling
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(sigChan)

	// Start the process
	if err := pr.Start(); err != nil {
		return err
	}

	// Handle signals in a goroutine
	signalReceived := make(chan struct{})
	go func() {
		<-sigChan
		log.Printf("Received shutdown signal, stopping process...")
		close(signalReceived)
		pr.Stop()
	}()

	// Wait for process to complete or signal
	err := pr.WaitForProcessCompletion()
	
	// If we got a signal, wait briefly for graceful shutdown
	select {
	case <-signalReceived:
		// Give the process a moment to clean up after Stop()
		time.Sleep(100 * time.Millisecond)
	default:
	}
	
	// Now wait for all goroutines to complete (with timeout is OK here)
	pr.Wait()
	
	return err
}

// Health check methods

// IsHealthy returns true if the process runner is operating normally
func (pr *ProcessRunner) IsHealthy() bool {
	return pr.ctx.Err() == nil
}

// GetHealth returns detailed health information about the process runner
func (pr *ProcessRunner) GetHealth() ProcessRunnerHealth {
	pr.mutex.RLock()
	defer pr.mutex.RUnlock()

	return ProcessRunnerHealth{
		IsHealthy:    pr.IsHealthy(),
		Running:      pr.running,
		PID:          pr.pid,
		ExitCode:     pr.exitCode,
		RestartCount: pr.restartCount,
		Label:        pr.label,
		Command:      pr.getCommandString(),
	}
}

// GetWebSocketClient returns the WebSocket client
func (pr *ProcessRunner) GetWebSocketClient() *WebSocketClient {
	return pr.client
}

// ProcessRunnerHealth represents the health status of the process runner
type ProcessRunnerHealth struct {
	IsHealthy    bool   `json:"is_healthy"`
	Running      bool   `json:"running"`
	PID          int    `json:"pid"`
	ExitCode     *int   `json:"exit_code"`
	RestartCount int    `json:"restart_count"`
	Label        string `json:"label"`
	Command      string `json:"command"`
}
