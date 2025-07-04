// Package server provides WebSocket server functionality for the LogMCP server.
//
// The WebSocket server handles bidirectional communication between the server and runners.
// It supports the following message types:
//
// Runner to Server Messages:
// - Session Registration - Runner announces itself with label, command details
// - Log Entries - Continuous stream of log lines with metadata
// - Status Updates - Process state changes
// - Acknowledgments - Responses to server commands
//
// Server to Runner Messages:
// - Process Control Commands - Instructions to restart or signal processes
// - Stdin Forwarding - Input data for process stdin
// - Configuration Updates - Logging settings changes
// - Health Checks - Ping requests
//
// The server handles multiple concurrent connections, message routing, and proper
// connection lifecycle management with cleanup on disconnect.
//
// Example usage:
//
//	sm := server.NewSessionManager()
//	wsServer := server.NewWebSocketServer(sm)
//	
//	// Create HTTP server with WebSocket endpoint
//	http.HandleFunc("/", wsServer.HandleWebSocket)
//	log.Fatal(http.ListenAndServe(":8765", nil))
package server

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/logmcp/logmcp/internal/protocol"
)

// WebSocketServer manages WebSocket connections and message routing
type WebSocketServer struct {
	sessionManager *SessionManager
	upgrader       websocket.Upgrader
	connections    map[*websocket.Conn]*ConnectionInfo
	connMutex      sync.RWMutex
	writeMutexes   map[*websocket.Conn]*sync.Mutex
	writeMutexLock sync.RWMutex
	ctx            context.Context
	cancel         context.CancelFunc
	wg             sync.WaitGroup
	
	// Configuration
	readTimeout  time.Duration
	writeTimeout time.Duration
	pingInterval time.Duration
}

// ConnectionInfo stores information about a WebSocket connection
type ConnectionInfo struct {
	Label        string
	LastPing     time.Time
	LastActivity time.Time
	mutex        sync.RWMutex
	writeMutex   sync.Mutex  // Protects WebSocket writes
}

// WebSocketServerConfig contains configuration options for the WebSocket server
type WebSocketServerConfig struct {
	ReadTimeout  time.Duration
	WriteTimeout time.Duration
	PingInterval time.Duration
	CheckOrigin  func(r *http.Request) bool
}

// DefaultWebSocketServerConfig returns default configuration for the WebSocket server
func DefaultWebSocketServerConfig() WebSocketServerConfig {
	return WebSocketServerConfig{
		ReadTimeout:  60 * time.Second,
		WriteTimeout: 10 * time.Second,
		PingInterval: 30 * time.Second,
		CheckOrigin:  nil, // Allow all origins by default (dev mode)
	}
}

// NewWebSocketServer creates a new WebSocket server
func NewWebSocketServer(sessionManager *SessionManager) *WebSocketServer {
	return NewWebSocketServerWithConfig(sessionManager, DefaultWebSocketServerConfig())
}

// NewWebSocketServerWithConfig creates a new WebSocket server with custom configuration
func NewWebSocketServerWithConfig(sessionManager *SessionManager, config WebSocketServerConfig) *WebSocketServer {
	ctx, cancel := context.WithCancel(context.Background())
	
	upgrader := websocket.Upgrader{
		ReadBufferSize:  1024,
		WriteBufferSize: 1024,
		CheckOrigin:     config.CheckOrigin,
	}
	
	if upgrader.CheckOrigin == nil {
		upgrader.CheckOrigin = func(r *http.Request) bool { return true }
	}

	return &WebSocketServer{
		sessionManager: sessionManager,
		upgrader:       upgrader,
		connections:    make(map[*websocket.Conn]*ConnectionInfo),
		writeMutexes:   make(map[*websocket.Conn]*sync.Mutex),
		ctx:            ctx,
		cancel:         cancel,
		readTimeout:    config.ReadTimeout,
		writeTimeout:   config.WriteTimeout,
		pingInterval:   config.PingInterval,
	}
}

// HandleWebSocket handles HTTP requests and upgrades them to WebSocket connections
func (ws *WebSocketServer) HandleWebSocket(w http.ResponseWriter, r *http.Request) {
	
	// Upgrade HTTP connection to WebSocket
	conn, err := ws.upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("WebSocket upgrade failed: %v", err)
		return
	}

	// Handle the WebSocket connection
	ws.handleConnection(conn)
}

// handleConnection manages a single WebSocket connection
func (ws *WebSocketServer) handleConnection(conn *websocket.Conn) {
	defer conn.Close()

	// Initialize connection info
	connInfo := &ConnectionInfo{
		LastPing:     time.Now(),
		LastActivity: time.Now(),
	}

	// Register connection
	ws.connMutex.Lock()
	ws.connections[conn] = connInfo
	ws.connMutex.Unlock()
	
	// Register write mutex
	ws.writeMutexLock.Lock()
	ws.writeMutexes[conn] = &sync.Mutex{}
	ws.writeMutexLock.Unlock()

	// Start connection handlers
	ctx, cancel := context.WithCancel(ws.ctx)
	defer cancel()

	var wg sync.WaitGroup
	wg.Add(2)

	// Start ping handler
	go func() {
		defer wg.Done()
		ws.handlePing(ctx, conn, connInfo)
	}()
	
	// Start message reader
	go func() {
		defer wg.Done()
		ws.handleMessages(ctx, conn, connInfo)
	}()

	// Wait for handlers to complete
	wg.Wait()

	// Clean up connection
	ws.cleanup(conn, connInfo)
}

// handleMessages processes incoming messages from a WebSocket connection
func (ws *WebSocketServer) handleMessages(ctx context.Context, conn *websocket.Conn, connInfo *ConnectionInfo) {
	
	// Set read deadline
	conn.SetReadDeadline(time.Now().Add(ws.readTimeout))
	
	// Set pong handler
	conn.SetPongHandler(func(string) error {
		connInfo.mutex.Lock()
		connInfo.LastPing = time.Now()
		connInfo.mutex.Unlock()
		conn.SetReadDeadline(time.Now().Add(ws.readTimeout))
		return nil
	})

	for {
		select {
		case <-ctx.Done():
			return
		default:
			// Read message
			_, message, err := conn.ReadMessage()
			if err != nil {
				if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
					log.Printf("WebSocket read error: %v", err)
				}
				return
			}

			// Update last activity
			connInfo.mutex.Lock()
			connInfo.LastActivity = time.Now()
			connInfo.mutex.Unlock()

			// Process message
			if err := ws.processMessage(conn, connInfo, message); err != nil {
				log.Printf("Error processing message: %v", err)
				
				// Send error response if we can identify the session
				if connInfo.Label != "" {
					errorMsg := protocol.NewErrorMessage(
						connInfo.Label,
						protocol.ErrorCodeInvalidMessage,
						fmt.Sprintf("Message processing failed: %v", err),
					)
					ws.sendMessage(conn, errorMsg)
				}
			}

			// Reset read deadline
			conn.SetReadDeadline(time.Now().Add(ws.readTimeout))
		}
	}
}

// processMessage processes a single incoming message
func (ws *WebSocketServer) processMessage(conn *websocket.Conn, connInfo *ConnectionInfo, message []byte) error {
	
	// Parse message
	msg, err := protocol.ParseMessage(message)
	if err != nil {
		return fmt.Errorf("failed to parse message: %w", err)
	}

	// Validate message
	if err := protocol.ValidateMessage(msg); err != nil {
		return fmt.Errorf("invalid message: %w", err)
	}

	// Route message based on type
	switch m := msg.(type) {
	case *protocol.SessionRegistrationMessage:
		return ws.handleRegistration(conn, connInfo, m)
	case *protocol.LogMessage:
		return ws.handleLogMessage(connInfo, m)
	case *protocol.StatusMessage:
		return ws.handleStatusMessage(connInfo, m)
	case *protocol.AckMessage:
		return ws.handleAckMessage(connInfo, m)
	case *protocol.ErrorMessage:
		return ws.handleErrorMessage(connInfo, m)
	default:
		return fmt.Errorf("unsupported message type: %T", msg)
	}
}

// handleRegistration processes session registration messages
func (ws *WebSocketServer) handleRegistration(conn *websocket.Conn, connInfo *ConnectionInfo, msg *protocol.SessionRegistrationMessage) error {
	// Determine runner mode and create args based on command
	var mode RunnerMode
	var args interface{}
	
	if msg.Command != "" {
		// This is a process runner
		mode = ModeRun
		args = RunArgs{
			Command: msg.Command,
			Label:   msg.Label,
		}
	} else {
		// This is likely a log forwarder
		mode = ModeForward
		args = ForwardArgs{
			Label:  msg.Label,
			Source: "unknown", // We don't have source info in registration message
		}
	}

	// Create session
	session, err := ws.sessionManager.CreateSession(
		msg.Label,
		msg.Command,
		msg.WorkingDir,
		msg.Capabilities,
		mode,
		args,
	)
	if err != nil {
		// Send error response
		errorMsg := protocol.NewErrorMessage(
			msg.Label,
			protocol.ErrorCodeInternalError,
			fmt.Sprintf("Failed to create session: %v", err),
		)
		return ws.sendMessage(conn, errorMsg)
	}

	// Update connection info with the assigned label (which may be different due to deduplication)
	connInfo.mutex.Lock()
	connInfo.Label = session.Label
	connInfo.mutex.Unlock()

	// Associate connection with session
	if err := ws.sessionManager.SetConnection(session.Label, conn); err != nil {
		log.Printf("Warning: Failed to set connection for session %s: %v", session.Label, err)
	}

	// Send acknowledgment with assigned label
	ackMsg := protocol.NewAckMessage(session.Label, true, fmt.Sprintf("Session registered with label: %s", session.Label))
	return ws.sendMessage(conn, ackMsg)
}

// handleLogMessage processes log messages
func (ws *WebSocketServer) handleLogMessage(connInfo *ConnectionInfo, msg *protocol.LogMessage) error {
	if connInfo.Label == "" {
		return fmt.Errorf("session not registered")
	}

	// Get session
	session, err := ws.sessionManager.GetSession(connInfo.Label)
	if err != nil {
		return fmt.Errorf("session not found: %w", err)
	}

	// Add to session's ring buffer using the protocol message directly
	session.mutex.RLock()
	ringBuffer := session.LogBuffer
	session.mutex.RUnlock()

	if ringBuffer != nil {
		ringBuffer.AddFromMessage(msg)
	}

	return nil
}

// handleStatusMessage processes status update messages
func (ws *WebSocketServer) handleStatusMessage(connInfo *ConnectionInfo, msg *protocol.StatusMessage) error {
	if connInfo.Label == "" {
		return fmt.Errorf("session not registered")
	}

	// Update session status
	pid := 0
	if msg.PID != nil {
		pid = *msg.PID
	}

	return ws.sessionManager.UpdateSessionStatus(connInfo.Label, msg.Status, pid, msg.ExitCode)
}

// handleAckMessage processes acknowledgment messages
func (ws *WebSocketServer) handleAckMessage(connInfo *ConnectionInfo, msg *protocol.AckMessage) error {
	// For now, just log the acknowledgment
	// In a more complete implementation, this would match against pending commands
	log.Printf("Received acknowledgment from session %s: success=%v, message=%s", 
		connInfo.Label, msg.Success, msg.Message)
	return nil
}

// handleErrorMessage processes error messages from runners
func (ws *WebSocketServer) handleErrorMessage(connInfo *ConnectionInfo, msg *protocol.ErrorMessage) error {
	// Log the error
	log.Printf("Received error from session %s: code=%s, message=%s", 
		connInfo.Label, msg.ErrorCode, msg.Message)
	return nil
}

// handlePing manages ping/pong heartbeat for a connection
func (ws *WebSocketServer) handlePing(ctx context.Context, conn *websocket.Conn, connInfo *ConnectionInfo) {
	
	ticker := time.NewTicker(ws.pingInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			// Get write mutex
			ws.writeMutexLock.RLock()
			writeMutex, exists := ws.writeMutexes[conn]
			ws.writeMutexLock.RUnlock()
			
			if !exists {
				return // Connection already closed
			}
			
			// Lock for writing
			writeMutex.Lock()
			
			// Set write deadline
			conn.SetWriteDeadline(time.Now().Add(ws.writeTimeout))
			
			// Send ping
			err := conn.WriteMessage(websocket.PingMessage, nil)
			writeMutex.Unlock()
			
			if err != nil {
				log.Printf("Failed to send ping: %v", err)
				return
			}
		}
	}
}

// SendCommand sends a command message to a specific session
func (ws *WebSocketServer) SendCommand(label string, action protocol.CommandAction, signal *protocol.Signal) error {
	// Get session
	session, err := ws.sessionManager.GetSession(label)
	if err != nil {
		return fmt.Errorf("session not found: %w", err)
	}

	// Get connection
	session.mutex.RLock()
	conn := session.Connection
	session.mutex.RUnlock()

	if conn == nil {
		return fmt.Errorf("session not connected")
	}

	// Create command message
	cmdMsg := protocol.NewCommandMessage(session.Label, action, signal)
	
	// Generate command ID for tracking
	cmdMsg.CommandID = fmt.Sprintf("cmd-%d", time.Now().UnixNano())

	// Send message
	return ws.sendMessage(conn, cmdMsg)
}

// SendStdin sends input to a specific session's stdin
func (ws *WebSocketServer) SendStdin(label, input string) error {
	// Get session
	session, err := ws.sessionManager.GetSession(label)
	if err != nil {
		return fmt.Errorf("session not found: %w", err)
	}

	// Get connection
	session.mutex.RLock()
	conn := session.Connection
	session.mutex.RUnlock()

	if conn == nil {
		return fmt.Errorf("session not connected")
	}

	// Create stdin message
	stdinMsg := protocol.NewStdinMessage(session.Label, input)

	// Send message
	return ws.sendMessage(conn, stdinMsg)
}

// sendMessage sends a message over a WebSocket connection
func (ws *WebSocketServer) sendMessage(conn *websocket.Conn, msg interface{}) error {
	// Get write mutex for this connection
	ws.writeMutexLock.RLock()
	writeMutex, exists := ws.writeMutexes[conn]
	ws.writeMutexLock.RUnlock()
	
	if !exists {
		return fmt.Errorf("connection not registered")
	}
	
	// Lock for writing
	writeMutex.Lock()
	defer writeMutex.Unlock()
	
	// Serialize message
	data, err := protocol.SerializeMessage(msg)
	if err != nil {
		return fmt.Errorf("failed to serialize message: %w", err)
	}

	// Set write deadline
	conn.SetWriteDeadline(time.Now().Add(ws.writeTimeout))

	// Send message
	return conn.WriteMessage(websocket.TextMessage, data)
}

// BroadcastMessage sends a message to all connected sessions
func (ws *WebSocketServer) BroadcastMessage(msg interface{}) error {
	ws.connMutex.RLock()
	connections := make([]*websocket.Conn, 0, len(ws.connections))
	for conn := range ws.connections {
		connections = append(connections, conn)
	}
	ws.connMutex.RUnlock()

	var errors []error
	for _, conn := range connections {
		if err := ws.sendMessage(conn, msg); err != nil {
			errors = append(errors, err)
		}
	}

	if len(errors) > 0 {
		return fmt.Errorf("failed to send to %d connections: %v", len(errors), errors[0])
	}

	return nil
}

// cleanup removes a connection and updates associated session
func (ws *WebSocketServer) cleanup(conn *websocket.Conn, connInfo *ConnectionInfo) {
	// Get label from connection info
	connInfo.mutex.RLock()
	label := connInfo.Label
	connInfo.mutex.RUnlock()

	// Update session first before removing connection
	if label != "" {
		if err := ws.sessionManager.DisconnectSession(label); err != nil {
			log.Printf("Warning: Failed to disconnect session %s: %v", label, err)
		}
	}

	// Remove connection from tracking
	ws.connMutex.Lock()
	delete(ws.connections, conn)
	ws.connMutex.Unlock()
	
	// Remove write mutex
	ws.writeMutexLock.Lock()
	delete(ws.writeMutexes, conn)
	ws.writeMutexLock.Unlock()

	log.Printf("WebSocket connection closed for session: %s", label)
}

// GetConnectionStats returns statistics about active connections
func (ws *WebSocketServer) GetConnectionStats() ConnectionStats {
	ws.connMutex.RLock()
	defer ws.connMutex.RUnlock()

	stats := ConnectionStats{
		TotalConnections:   len(ws.connections),
		RegisteredSessions: 0,
	}

	for _, connInfo := range ws.connections {
		connInfo.mutex.RLock()
		if connInfo.Label != "" {
			stats.RegisteredSessions++
		}
		connInfo.mutex.RUnlock()
	}

	return stats
}

// Close shuts down the WebSocket server
func (ws *WebSocketServer) Close() error {
	// Cancel context to signal shutdown
	ws.cancel()

	// Close all connections
	ws.connMutex.Lock()
	for conn := range ws.connections {
		conn.Close()
	}
	ws.connMutex.Unlock()

	// Wait for all handlers to complete
	ws.wg.Wait()

	return nil
}

// ConnectionStats represents statistics about WebSocket connections
type ConnectionStats struct {
	TotalConnections   int `json:"total_connections"`
	RegisteredSessions int `json:"registered_sessions"`
}

// String returns a human-readable string representation of the connection stats
func (s ConnectionStats) String() string {
	return fmt.Sprintf("Connections: %d total, %d with registered sessions", 
		s.TotalConnections, s.RegisteredSessions)
}

// Health check methods

// IsHealthy returns true if the WebSocket server is operating normally
func (ws *WebSocketServer) IsHealthy() bool {
	return ws.ctx.Err() == nil
}

// GetHealth returns detailed health information about the WebSocket server
func (ws *WebSocketServer) GetHealth() WebSocketServerHealth {
	stats := ws.GetConnectionStats()
	
	return WebSocketServerHealth{
		IsHealthy:        ws.IsHealthy(),
		ConnectionStats:  stats,
		SessionManagerOK: ws.sessionManager.IsHealthy(),
	}
}

// WebSocketServerHealth represents the health status of the WebSocket server
type WebSocketServerHealth struct {
	IsHealthy        bool            `json:"is_healthy"`
	ConnectionStats  ConnectionStats `json:"connection_stats"`
	SessionManagerOK bool            `json:"session_manager_ok"`
}

// Helper function to create a standard HTTP handler
func (ws *WebSocketServer) HTTPHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ws.HandleWebSocket(w, r)
	}
}

// Helper function to start a WebSocket server on a specific address
func (ws *WebSocketServer) ListenAndServe(addr string) error {
	http.HandleFunc("/", ws.HandleWebSocket)
	log.Printf("WebSocket server starting on %s", addr)
	return http.ListenAndServe(addr, nil)
}