// Package runner provides WebSocket client functionality for LogMCP runners.
//
// The WebSocket client handles communication between runners (process runners and log forwarders)
// and the LogMCP server. It provides:
//
// - Connection management with automatic reconnection
// - Session registration and lifecycle management
// - Message serialization and routing
// - Command handling from server
// - Heartbeat and health monitoring
//
// The client supports exponential backoff reconnection and graceful shutdown.
//
// Example usage:
//
//	client := runner.NewWebSocketClient("ws://localhost:8765", "my-session")
//	client.OnLogMessage = func(content, stream string) {
//		// Handle log messages
//	}
//	client.OnCommand = func(action, signal string) {
//		// Handle control commands
//	}
//
//	if err := client.Connect(); err != nil {
//		log.Fatal(err)
//	}
//	defer client.Close()
package runner

import (
	"context"
	"fmt"
	"log/slog"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/bebsworthy/logmcp/internal/errors"
	"github.com/bebsworthy/logmcp/internal/metrics"
	"github.com/bebsworthy/logmcp/internal/protocol"
	"github.com/cenkalti/backoff/v4"
	"github.com/gorilla/websocket"
)

// registrationResponse holds the response from session registration
type registrationResponse struct {
	success bool
	message string
	err     error
}

// WebSocketClient manages WebSocket connection to LogMCP server
type WebSocketClient struct {
	// Connection settings
	serverURL    string
	label        string
	command      string
	workingDir   string
	capabilities []string

	// Connection state
	conn      *websocket.Conn
	connected bool
	connMutex sync.RWMutex

	// Registration handling
	registrationChan       chan registrationResponse
	waitingForRegistration bool
	registrationMutex      sync.RWMutex

	// Context and lifecycle
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	// Configuration
	reconnectDelay       time.Duration
	maxReconnectDelay    time.Duration
	reconnectAttempts    int
	maxReconnectAttempts int
	pingInterval         time.Duration
	writeTimeout         time.Duration
	readTimeout          time.Duration

	// Callbacks
	OnLogMessage   func(content, stream string, pid int)
	OnCommand      func(action string, signal *protocol.Signal) error
	OnStdinMessage func(input string) error
	OnConnected    func(label string)
	OnDisconnected func()
	OnError        func(error)

	// Logging and metrics
	logger  *slog.Logger
	monitor *metrics.Monitor

	// Internal channels
	messageChan chan interface{}
	commandChan chan *protocol.CommandMessage
	stdinChan   chan *protocol.StdinMessage
}

// WebSocketClientConfig contains configuration options for the WebSocket client
type WebSocketClientConfig struct {
	ReconnectDelay       time.Duration
	MaxReconnectDelay    time.Duration
	MaxReconnectAttempts int
	PingInterval         time.Duration
	WriteTimeout         time.Duration
	ReadTimeout          time.Duration
}

// DefaultWebSocketClientConfig returns default configuration for the WebSocket client
func DefaultWebSocketClientConfig() WebSocketClientConfig {
	return WebSocketClientConfig{
		ReconnectDelay:       1 * time.Second,
		MaxReconnectDelay:    30 * time.Second,
		MaxReconnectAttempts: 10,
		PingInterval:         30 * time.Second,
		WriteTimeout:         10 * time.Second,
		ReadTimeout:          60 * time.Second,
	}
}

// NewWebSocketClient creates a new WebSocket client
func NewWebSocketClient(serverURL, label string) *WebSocketClient {
	return NewWebSocketClientWithConfig(serverURL, label, DefaultWebSocketClientConfig())
}

// NewWebSocketClientWithConfig creates a new WebSocket client with custom configuration
func NewWebSocketClientWithConfig(serverURL, label string, config WebSocketClientConfig) *WebSocketClient {
	ctx, cancel := context.WithCancel(context.Background())

	return &WebSocketClient{
		serverURL:            serverURL,
		label:                label,
		ctx:                  ctx,
		cancel:               cancel,
		reconnectDelay:       config.ReconnectDelay,
		maxReconnectDelay:    config.MaxReconnectDelay,
		maxReconnectAttempts: config.MaxReconnectAttempts,
		pingInterval:         config.PingInterval,
		writeTimeout:         config.WriteTimeout,
		readTimeout:          config.ReadTimeout,
		messageChan:          make(chan interface{}, 100),
		commandChan:          make(chan *protocol.CommandMessage, 10),
		stdinChan:            make(chan *protocol.StdinMessage, 10),
		registrationChan:     make(chan registrationResponse, 1),
	}
}

// SetCommand sets the command information for the client (used by process runners)
func (c *WebSocketClient) SetCommand(command, workingDir string, capabilities []string) {
	c.command = command
	c.workingDir = workingDir
	c.capabilities = capabilities
}

// SetLogger sets the logger for the client
func (c *WebSocketClient) SetLogger(logger *slog.Logger) {
	c.logger = logger.With(
		slog.String("component", "websocket_client"),
		slog.String("server_url", c.serverURL),
		slog.String("label", c.label),
	)
}

// SetMonitor sets the metrics monitor for the client
func (c *WebSocketClient) SetMonitor(monitor *metrics.Monitor) {
	c.monitor = monitor
}

// Connect establishes connection to the WebSocket server
func (c *WebSocketClient) Connect() error {
	start := time.Now()
	var connectErr error

	defer func() {
		if c.monitor != nil {
			if connectErr != nil {
				c.monitor.TrackConnection("connect_failed", time.Since(start))
				c.monitor.TrackError(c.ctx, "network", errors.GetCode(connectErr), "websocket_client", connectErr.Error())
			} else {
				c.monitor.TrackConnection("connect", time.Since(start))
			}
		}
	}()

	c.connMutex.Lock()
	defer c.connMutex.Unlock()

	if c.connected {
		connectErr = fmt.Errorf("already connected")
		return connectErr
	}

	// Parse server URL
	u, err := url.Parse(c.serverURL)
	if err != nil {
		connectErr = errors.ValidationError("INVALID_URL", "Invalid server URL", err)
		return connectErr
	}

	// Establish WebSocket connection
	dialer := websocket.DefaultDialer
	dialer.HandshakeTimeout = 10 * time.Second

	conn, _, err := dialer.Dial(u.String(), nil)
	if err != nil {
		// Increment reconnection attempts under lock
		c.reconnectAttempts++
		connectErr = errors.NetworkError("CONNECTION_FAILED", "Failed to connect to server", err)
		return connectErr
	}

	c.conn = conn
	c.connected = true

	// Reset registration channel for new connection
	c.registrationChan = make(chan registrationResponse, 1)

	// Start connection handlers
	c.wg.Add(3)
	go c.handleMessages()
	go c.handleOutgoing()
	go c.handlePing()

	// Register session
	if err := c.registerSession(); err != nil {
		_ = c.conn.Close()
		c.connected = false
		connectErr = errors.WrapError(err, "failed to register session")
		return connectErr
	}

	// Reset reconnection attempts on successful connection
	c.reconnectAttempts = 0

	if c.logger != nil {
		c.logger.InfoContext(c.ctx, "WebSocket connection established",
			slog.String("label", c.label),
			slog.String("server_url", c.serverURL),
		)
	}

	if c.OnConnected != nil {
		c.OnConnected(c.label)
	}

	return nil
}

// ConnectWithRetry connects with automatic retry logic using exponential backoff
func (c *WebSocketClient) ConnectWithRetry() error {
	// Create exponential backoff configuration
	bo := backoff.NewExponentialBackOff()
	bo.InitialInterval = c.reconnectDelay
	bo.MaxInterval = c.maxReconnectDelay
	bo.MaxElapsedTime = 0 // No time limit, only attempt limit
	bo.Multiplier = 2.0
	bo.RandomizationFactor = 0.1 // Add jitter to prevent thundering herd

	// Wrap with context to handle cancellation
	contextBackoff := backoff.WithContext(bo, c.ctx)

	// Apply max attempts limit if configured
	var finalBackoff backoff.BackOff = contextBackoff
	if c.maxReconnectAttempts > 0 {
		finalBackoff = backoff.WithMaxRetries(contextBackoff, uint64(c.maxReconnectAttempts))
	}

	// Define the operation to retry
	operation := func() error {
		err := c.Connect()
		if err != nil {
			c.reconnectAttempts++

			// Check if it's a permanent error (shouldn't retry)
			if isPermanentError(err) {
				return backoff.Permanent(err)
			}

			// Track reconnection attempt
			if c.monitor != nil {
				c.monitor.TrackConnection("reconnect_attempt", 0)
			}

			if c.logger != nil {
				c.logger.WarnContext(c.ctx, "Connection attempt failed",
					slog.Int("attempt", c.reconnectAttempts),
					slog.String("error", err.Error()),
				)
			}
			if c.OnError != nil {
				c.OnError(fmt.Errorf("connection failed (attempt %d): %w", c.reconnectAttempts, err))
			}

			return err
		}

		// Success - reset attempts counter
		c.reconnectAttempts = 0
		return nil
	}

	// Retry the operation with exponential backoff
	return backoff.Retry(operation, finalBackoff)
}

// isPermanentError determines if an error should not be retried
func isPermanentError(err error) bool {
	// Check for specific error types that shouldn't be retried
	if logmcpErr, ok := err.(*errors.LogMCPError); ok {
		switch logmcpErr.Type {
		case errors.ErrorTypeValidation:
			// Configuration/validation errors are permanent
			return true
		case errors.ErrorTypePermission:
			// Permission errors are usually permanent
			return true
		}
	}

	// Check for specific error codes
	if errors.IsCode(err, "INVALID_URL") {
		return true
	}

	// Check for URL parsing errors (Go standard library)
	if _, ok := err.(*url.Error); ok {
		return true
	}

	// Check error message content for permanent conditions
	errorMsg := err.Error()

	// URL scheme errors are permanent
	if strings.Contains(errorMsg, "unsupported protocol scheme") {
		return true
	}

	// Invalid URL format is permanent
	if strings.Contains(errorMsg, "invalid URL") {
		return true
	}

	// Malformed WebSocket URL is permanent
	if strings.Contains(errorMsg, "malformed ws or wss URL") {
		return true
	}

	// Host resolution errors that indicate permanent DNS issues
	if strings.Contains(errorMsg, "no such host") {
		return true
	}

	// WebSocket upgrade failure due to invalid URL
	if strings.Contains(errorMsg, "bad handshake") {
		return true
	}

	// For now, treat most errors as temporary (can be retried)
	return false
}

// ConnectWithRetryLegacy provides the original simple exponential backoff implementation
// Kept for compatibility and testing
func (c *WebSocketClient) ConnectWithRetryLegacy() error {
	for {
		err := c.Connect()
		if err == nil {
			return nil
		}

		c.reconnectAttempts++
		if c.maxReconnectAttempts > 0 && c.reconnectAttempts >= c.maxReconnectAttempts {
			return fmt.Errorf("max reconnection attempts reached: %w", err)
		}

		// Calculate delay with exponential backoff
		delay := c.reconnectDelay * time.Duration(1<<uint(c.reconnectAttempts-1))
		if delay > c.maxReconnectDelay {
			delay = c.maxReconnectDelay
		}

		if c.OnError != nil {
			c.OnError(fmt.Errorf("connection failed, retrying in %v: %w", delay, err))
		}

		select {
		case <-c.ctx.Done():
			return c.ctx.Err()
		case <-time.After(delay):
			continue
		}
	}
}

// registerSession sends session registration message to server
func (c *WebSocketClient) registerSession() error {
	// Set waiting flag
	c.registrationMutex.Lock()
	c.waitingForRegistration = true
	c.registrationMutex.Unlock()

	// Ensure we clear the flag on exit
	defer func() {
		c.registrationMutex.Lock()
		c.waitingForRegistration = false
		c.registrationMutex.Unlock()
	}()

	regMsg := protocol.NewSessionRegistrationMessage(
		c.label,        // Label
		c.command,      // Command being executed
		c.workingDir,   // Working directory
		c.capabilities, // Capabilities
	)

	data, err := protocol.SerializeMessage(regMsg)
	if err != nil {
		return fmt.Errorf("failed to serialize registration message: %w", err)
	}

	_ = c.conn.SetWriteDeadline(time.Now().Add(c.writeTimeout))
	if err := c.conn.WriteMessage(websocket.TextMessage, data); err != nil {
		return fmt.Errorf("failed to send registration message: %w", err)
	}

	// Wait for acknowledgment via channel with timeout
	select {
	case resp := <-c.registrationChan:
		if resp.err != nil {
			return resp.err
		}
		if !resp.success {
			return fmt.Errorf("registration failed: %s", resp.message)
		}
		// Session registration successful
		return nil
	case <-time.After(c.readTimeout):
		return fmt.Errorf("registration timeout after %v", c.readTimeout)
	case <-c.ctx.Done():
		return fmt.Errorf("registration cancelled: %w", c.ctx.Err())
	}
}

// SendLogMessage sends a log message to the server
func (c *WebSocketClient) SendLogMessage(content, stream string, pid int) error {
	if !c.IsConnected() {
		return fmt.Errorf("not connected")
	}

	logMsg := protocol.NewLogMessage(c.label, content, protocol.StreamType(stream), pid)

	select {
	case c.messageChan <- logMsg:
		return nil
	case <-c.ctx.Done():
		return c.ctx.Err()
	default:
		return fmt.Errorf("message queue full")
	}
}

// SendStatusMessage sends a status update to the server
func (c *WebSocketClient) SendStatusMessage(status protocol.SessionStatus, pid int, exitCode *int) error {
	if !c.IsConnected() {
		return fmt.Errorf("not connected")
	}

	statusMsg := protocol.NewStatusMessage(c.label, status, &pid, exitCode)

	select {
	case c.messageChan <- statusMsg:
		return nil
	case <-c.ctx.Done():
		return c.ctx.Err()
	default:
		return fmt.Errorf("message queue full")
	}
}

// SendAckMessage sends an acknowledgment to the server
func (c *WebSocketClient) SendAckMessage(commandID string, success bool, message string) error {
	if !c.IsConnected() {
		return fmt.Errorf("not connected")
	}

	ackMsg := protocol.NewAckMessage(c.label, success, message)
	ackMsg.CommandID = commandID

	select {
	case c.messageChan <- ackMsg:
		return nil
	case <-c.ctx.Done():
		return c.ctx.Err()
	default:
		return fmt.Errorf("message queue full")
	}
}

// handleMessages processes incoming messages from the server
func (c *WebSocketClient) handleMessages() {
	defer c.wg.Done()
	defer func() {
		c.connMutex.Lock()
		c.connected = false
		c.connMutex.Unlock()

		if c.OnDisconnected != nil {
			c.OnDisconnected()
		}
	}()

	_ = c.conn.SetReadDeadline(time.Now().Add(c.readTimeout))
	c.conn.SetPongHandler(func(string) error {
		_ = c.conn.SetReadDeadline(time.Now().Add(c.readTimeout))
		return nil
	})

	for {
		select {
		case <-c.ctx.Done():
			return
		default:
			_, message, err := c.conn.ReadMessage()
			if err != nil {
				if !websocket.IsCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
					if c.OnError != nil {
						c.OnError(fmt.Errorf("read error: %w", err))
					}
				}
				return
			}

			if err := c.processMessage(message); err != nil {
				if c.OnError != nil {
					c.OnError(fmt.Errorf("message processing error: %w", err))
				}
			}

			_ = c.conn.SetReadDeadline(time.Now().Add(c.readTimeout))
		}
	}
}

// processMessage processes a single incoming message
func (c *WebSocketClient) processMessage(message []byte) error {
	msg, err := protocol.ParseMessage(message)
	if err != nil {
		return fmt.Errorf("failed to parse message: %w", err)
	}

	// Check if we're waiting for registration response
	c.registrationMutex.RLock()
	waitingForReg := c.waitingForRegistration
	c.registrationMutex.RUnlock()

	// Handle registration responses specially
	if waitingForReg {
		switch m := msg.(type) {
		case *protocol.AckMessage:
			// Update label if server assigned a different one
			if m.Label != "" && m.Label != c.label {
				c.label = m.Label
			}
			select {
			case c.registrationChan <- registrationResponse{
				success: m.Success,
				message: m.Message,
			}:
			default:
				// Channel full, registration already completed
			}
			return nil
		case *protocol.ErrorMessage:
			select {
			case c.registrationChan <- registrationResponse{
				success: false,
				message: m.Message,
				err:     fmt.Errorf("registration error: %s", m.Message),
			}:
			default:
				// Channel full, registration already completed
			}
			return nil
		}
	}

	// Normal message processing
	switch m := msg.(type) {
	case *protocol.CommandMessage:
		if c.OnCommand != nil {
			if err := c.OnCommand(string(m.Action), m.Signal); err != nil {
				// Send error acknowledgment
				if ackErr := c.SendAckMessage(m.CommandID, false, err.Error()); ackErr != nil {
					slog.Error("Failed to send error acknowledgment",
						slog.String("error", ackErr.Error()),
						slog.String("command_id", m.CommandID))
				}
			} else {
				// Send success acknowledgment
				if ackErr := c.SendAckMessage(m.CommandID, true, "Command executed successfully"); ackErr != nil {
					slog.Error("Failed to send success acknowledgment",
						slog.String("error", ackErr.Error()),
						slog.String("command_id", m.CommandID))
				}
			}
		}
	case *protocol.StdinMessage:
		if c.OnStdinMessage != nil {
			if err := c.OnStdinMessage(m.Input); err != nil {
				if c.OnError != nil {
					c.OnError(fmt.Errorf("stdin message error: %w", err))
				}
			}
		}
	case *protocol.ErrorMessage:
		if c.OnError != nil {
			c.OnError(fmt.Errorf("server error: %s", m.Message))
		}
	case *protocol.AckMessage:
		// Regular acknowledgment messages - not registration related
		if !m.Success && c.OnError != nil {
			c.OnError(fmt.Errorf("server ack error: %s", m.Message))
		}
	default:
		// Unknown message type - log but don't error
		if c.logger != nil {
			c.logger.WarnContext(c.ctx, "Received unknown message type",
				slog.String("message_type", fmt.Sprintf("%T", msg)),
			)
		}
	}

	return nil
}

// handleOutgoing processes outgoing messages
func (c *WebSocketClient) handleOutgoing() {
	defer c.wg.Done()

	for {
		select {
		case <-c.ctx.Done():
			return
		case msg := <-c.messageChan:
			if err := c.sendMessage(msg); err != nil {
				if c.OnError != nil {
					c.OnError(fmt.Errorf("send error: %w", err))
				}
			}
		}
	}
}

// sendMessage sends a message to the server
func (c *WebSocketClient) sendMessage(msg interface{}) error {
	c.connMutex.RLock()
	defer c.connMutex.RUnlock()

	if !c.connected || c.conn == nil {
		return fmt.Errorf("not connected")
	}

	data, err := protocol.SerializeMessage(msg)
	if err != nil {
		return fmt.Errorf("failed to serialize message: %w", err)
	}

	_ = c.conn.SetWriteDeadline(time.Now().Add(c.writeTimeout))
	return c.conn.WriteMessage(websocket.TextMessage, data)
}

// handlePing manages ping/pong heartbeat
func (c *WebSocketClient) handlePing() {
	defer c.wg.Done()

	ticker := time.NewTicker(c.pingInterval)
	defer ticker.Stop()

	for {
		select {
		case <-c.ctx.Done():
			return
		case <-ticker.C:
			c.connMutex.RLock()
			conn := c.conn
			connected := c.connected
			c.connMutex.RUnlock()

			if !connected || conn == nil {
				return
			}

			_ = conn.SetWriteDeadline(time.Now().Add(c.writeTimeout))
			if err := conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				if c.OnError != nil {
					c.OnError(fmt.Errorf("ping error: %w", err))
				}
				return
			}
		}
	}
}

// IsConnected returns true if the client is connected
func (c *WebSocketClient) IsConnected() bool {
	c.connMutex.RLock()
	defer c.connMutex.RUnlock()
	return c.connected
}

// GetLabel returns the label for this client
func (c *WebSocketClient) GetLabel() string {
	return c.label
}

// FlushMessages waits for all pending messages to be sent
func (c *WebSocketClient) FlushMessages() error {
	// Give messages time to be processed
	timeout := time.NewTimer(5 * time.Second)
	defer timeout.Stop()

	for {
		select {
		case <-timeout.C:
			return fmt.Errorf("timeout waiting for messages to flush")
		default:
			// Check if message channel is empty
			if len(c.messageChan) == 0 {
				// Wait a bit more to ensure the last message is sent
				time.Sleep(100 * time.Millisecond)
				return nil
			}
			// Small delay to avoid busy waiting
			time.Sleep(10 * time.Millisecond)
		}
	}
}

// Close closes the WebSocket connection
func (c *WebSocketClient) Close() error {
	// Close connection with proper close message
	c.connMutex.Lock()
	wasConnected := c.connected
	if c.conn != nil {
		// Send close message to server
		closeMsg := websocket.FormatCloseMessage(websocket.CloseNormalClosure, "Client closing")
		_ = c.conn.WriteControl(websocket.CloseMessage, closeMsg, time.Now().Add(time.Second))
		_ = c.conn.Close()
	}
	c.connected = false
	c.connMutex.Unlock()

	// Cancel context to signal shutdown
	c.cancel()

	// Track disconnection
	if wasConnected && c.monitor != nil {
		c.monitor.TrackConnection("disconnect", 0)
	}

	// Wait for handlers to complete
	c.wg.Wait()

	return nil
}

// RunWithReconnection runs the client with automatic reconnection
func (c *WebSocketClient) RunWithReconnection() error {
	for {
		// Try to connect with exponential backoff
		if err := c.ConnectWithRetry(); err != nil {
			return err
		}

		// Monitor connection and handle disconnections
		if err := c.monitorConnection(); err != nil {
			// Check if it's a context cancellation (graceful shutdown)
			if err == context.Canceled || err == context.DeadlineExceeded {
				return err
			}

			// Log disconnection and continue to reconnect
			if c.OnError != nil {
				c.OnError(fmt.Errorf("connection lost: %w", err))
			}

			// Connection lost, try to reconnect
			continue
		}

		// Should only reach here on graceful shutdown
		return nil
	}
}

// monitorConnection monitors the connection and returns when it's lost or context is done
func (c *WebSocketClient) monitorConnection() error {
	// Create a ticker for periodic connection health checks
	healthCheckInterval := c.pingInterval * 2 // Check less frequently than ping
	ticker := time.NewTicker(healthCheckInterval)
	defer ticker.Stop()

	for {
		select {
		case <-c.ctx.Done():
			return c.ctx.Err()

		case <-ticker.C:
			if !c.IsConnected() {
				return fmt.Errorf("connection lost")
			}

			// Optional: Add more sophisticated health checks here
			// such as checking last pong received time, message queue health, etc.
		}
	}
}

// Health check methods

// IsHealthy returns true if the client is connected and operating normally
func (c *WebSocketClient) IsHealthy() bool {
	return c.IsConnected() && c.ctx.Err() == nil
}

// GetHealth returns detailed health information about the client
func (c *WebSocketClient) GetHealth() WebSocketClientHealth {
	return WebSocketClientHealth{
		IsHealthy:         c.IsHealthy(),
		Connected:         c.IsConnected(),
		Label:             c.label,
		ServerURL:         c.serverURL,
		ReconnectAttempts: c.reconnectAttempts,
	}
}

// WebSocketClientHealth represents the health status of the WebSocket client
type WebSocketClientHealth struct {
	IsHealthy         bool   `json:"is_healthy"`
	Connected         bool   `json:"connected"`
	Label             string `json:"label"`
	ServerURL         string `json:"server_url"`
	ReconnectAttempts int    `json:"reconnect_attempts"`
}
