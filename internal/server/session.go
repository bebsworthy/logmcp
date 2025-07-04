// Package server provides session management functionality for the LogMCP server.
//
// The session management system handles:
// - Session creation with label-based identification
// - Label conflict resolution with automatic numbering (backend, backend-2, backend-3, etc.)
// - Connection state tracking (connected/disconnected)
// - Process lifecycle management (running, stopped, crashed, restarting)
// - Automatic cleanup of terminated and disconnected sessions
// - Thread-safe operations with concurrent access support
// - Health monitoring and statistics collection
//
// Example usage:
//
//	sm := server.NewSessionManager()
//	defer sm.Close()
//
//	// Create a session for a backend process
//	session, err := sm.CreateSession("backend", "npm run server", "/app", 
//		[]string{"process_control"}, server.ModeRun, runArgs)
//	if err != nil {
//		log.Fatal(err)
//	}
//
//	// Update session status when process state changes
//	err = sm.UpdateSessionStatus(session.Label, protocol.StatusRunning, 1234, nil)
//
//	// Set WebSocket connection
//	err = sm.SetConnection(session.Label, conn)
//
//	// Get session statistics
//	stats := sm.GetSessionStats()
//	fmt.Printf("Active sessions: %d\n", stats.ActiveSessions)
package server

import (
	"context"
	"fmt"
	"log"
	"os"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/logmcp/logmcp/internal/buffer"
	"github.com/logmcp/logmcp/internal/protocol"
)

// RunnerMode represents how a session was created
type RunnerMode string

const (
	ModeRun     RunnerMode = "run"     // logmcp run <command>
	ModeForward RunnerMode = "forward" // logmcp forward <source>
	ModeManaged RunnerMode = "managed" // Started via MCP start_process
)

// ConnectionStatus represents the status of the WebSocket connection
type ConnectionStatus string

const (
	ConnectionConnected    ConnectionStatus = "connected"
	ConnectionDisconnected ConnectionStatus = "disconnected"
)

// Session represents a logging session with a process or log source
type Session struct {
	Label            string                 `json:"label"`             // User-friendly name (also serves as unique identifier)
	Command          string                 `json:"command"`           // Command being executed or source description
	WorkingDir       string                 `json:"working_dir"`       // Working directory
	Status           protocol.SessionStatus `json:"status"`            // running|stopped|crashed|restarting
	PID              int                    `json:"pid"`               // Process ID (0 if not applicable)
	ExitCode         *int                   `json:"exit_code"`         // nil if still running
	StartTime        time.Time              `json:"start_time"`        // When the session was created
	ExitTime         *time.Time             `json:"exit_time"`         // nil if still running
	LogBuffer        *buffer.RingBuffer     `json:"-"`                 // Ring buffer for logs
	Connection       *websocket.Conn        `json:"-"`                 // WebSocket connection
	ConnectionStatus ConnectionStatus       `json:"connection_status"` // Connection state
	Process          *os.Process            `json:"-"`                 // Process handle for managed processes
	Capabilities     []string               `json:"capabilities"`      // Available capabilities
	RunnerMode       RunnerMode             `json:"runner_mode"`       // How this session was created
	RunnerArgs       interface{}            `json:"runner_args"`       // Mode-specific arguments

	// Internal fields for session management
	mutex              sync.RWMutex  `json:"-"`
	restartMutex       sync.Mutex    `json:"-"` // Serializes restart operations
	lastActivityTime   time.Time     `json:"-"`
	processTerminated  bool          `json:"-"`
	connectionLostTime *time.Time    `json:"-"`
	cleanupScheduled   bool          `json:"-"`
}

// RunnerArgs structs for different modes
type RunArgs struct {
	Command string `json:"command"`
	Label   string `json:"label"`
}

type ForwardArgs struct {
	Source string `json:"source"`
	Label  string `json:"label"`
}

type ManagedArgs struct {
	Command       string            `json:"command"`
	Arguments     []string          `json:"arguments"`
	Label         string            `json:"label"`
	WorkingDir    string            `json:"working_dir"`
	Environment   map[string]string `json:"environment"`
	RestartPolicy string            `json:"restart_policy,omitempty"`
}

// SessionManager manages all active sessions
type SessionManager struct {
	mutex            sync.RWMutex
	sessions         map[string]*Session          // label -> Session
	cleanupScheduled map[string]time.Time         // label -> cleanup time
	ctx              context.Context
	cancel           context.CancelFunc
	cleanupWg        sync.WaitGroup
	startTime        time.Time                    // When the session manager was created

	// Configuration
	cleanupDelay    time.Duration // How long to wait after disconnection before cleanup
	cleanupInterval time.Duration // How often to check for cleanup
}

const (
	// DefaultCleanupDelay is the default time to wait before cleaning up disconnected sessions
	DefaultCleanupDelay = 1 * time.Hour

	// CleanupCheckInterval is how often to check for sessions that need cleanup
	CleanupCheckInterval = 1 * time.Minute

	// CleanupCheckIntervalForTesting is a shorter interval for tests
	CleanupCheckIntervalForTesting = 50 * time.Millisecond

	// LabelCounterSeparator is the separator used for label conflict resolution
	LabelCounterSeparator = "-"
)

// NewSessionManager creates a new session manager
func NewSessionManager() *SessionManager {
	return NewSessionManagerWithConfig(DefaultCleanupDelay, CleanupCheckInterval)
}

// NewSessionManagerWithConfig creates a new session manager with custom configuration
func NewSessionManagerWithConfig(cleanupDelay, cleanupInterval time.Duration) *SessionManager {
	ctx, cancel := context.WithCancel(context.Background())

	sm := &SessionManager{
		sessions:         make(map[string]*Session),
		cleanupScheduled: make(map[string]time.Time),
		ctx:              ctx,
		cancel:           cancel,
		cleanupDelay:     cleanupDelay,
		cleanupInterval:  cleanupInterval,
		startTime:        time.Now(),
	}

	// Start background cleanup goroutine
	sm.cleanupWg.Add(1)
	go sm.backgroundCleanup()

	return sm
}

// CreateSession creates a new session with label conflict resolution
func (sm *SessionManager) CreateSession(label, command, workingDir string, capabilities []string, mode RunnerMode, args interface{}) (*Session, error) {
	sm.mutex.Lock()
	defer sm.mutex.Unlock()

	// Check if we can reuse the label by removing a disconnected session
	if existingSession, exists := sm.sessions[label]; exists {
		existingSession.mutex.RLock()
		connStatus := existingSession.ConnectionStatus
		existingSession.mutex.RUnlock()
		
		if connStatus == ConnectionDisconnected {
			// Remove the old disconnected session to reuse the label
			sm.removeSessionUnsafe(label)
		}
	}

	// Resolve label conflicts
	resolvedLabel := sm.resolveLabelConflictUnsafe(label)

	// Create new session
	session := &Session{
		Label:            resolvedLabel,
		Command:          command,
		WorkingDir:       workingDir,
		Status:           protocol.StatusRunning,
		PID:              0,
		ExitCode:         nil,
		StartTime:        time.Now(),
		ExitTime:         nil,
		LogBuffer:        buffer.NewDefaultRingBuffer(),
		Connection:       nil,
		ConnectionStatus: ConnectionDisconnected,
		Process:          nil,
		Capabilities:     capabilities,
		RunnerMode:       mode,
		RunnerArgs:       args,
		lastActivityTime: time.Now(),
	}

	// Add to session map
	sm.sessions[resolvedLabel] = session

	return session, nil
}

// resolveLabelConflictUnsafe generates a unique label by appending a counter if needed
func (sm *SessionManager) resolveLabelConflictUnsafe(label string) string {
	// Check if label is already in use
	if _, exists := sm.sessions[label]; !exists {
		return label
	}

	// Find the next available counter for this label
	counter := 2
	for {
		candidateLabel := fmt.Sprintf("%s%s%d", label, LabelCounterSeparator, counter)
		if _, exists := sm.sessions[candidateLabel]; !exists {
			return candidateLabel
		}
		counter++
	}
}

// GetSession retrieves a session by label
func (sm *SessionManager) GetSession(label string) (*Session, error) {
	sm.mutex.RLock()
	defer sm.mutex.RUnlock()

	session, exists := sm.sessions[label]
	if !exists {
		return nil, fmt.Errorf("session not found: %s", label)
	}

	return session, nil
}

// GetSessionsByLabel retrieves the session with the given label
func (sm *SessionManager) GetSessionsByLabel(label string) *Session {
	sm.mutex.RLock()
	defer sm.mutex.RUnlock()

	session, exists := sm.sessions[label]
	if !exists {
		return nil
	}

	return session
}

// ListSessions returns all active sessions
func (sm *SessionManager) ListSessions() []*Session {
	sm.mutex.RLock()
	defer sm.mutex.RUnlock()

	sessions := make([]*Session, 0, len(sm.sessions))
	for _, session := range sm.sessions {
		sessions = append(sessions, session)
	}

	return sessions
}

// UpdateSessionStatus updates the status of a session
func (sm *SessionManager) UpdateSessionStatus(label string, status protocol.SessionStatus, pid int, exitCode *int) error {
	sm.mutex.RLock()
	session, exists := sm.sessions[label]
	sm.mutex.RUnlock()

	if !exists {
		return fmt.Errorf("session not found: %s", label)
	}

	session.mutex.Lock()
	defer session.mutex.Unlock()

	// Debug: Log status change
	oldStatus := session.Status
	session.Status = status
	session.lastActivityTime = time.Now()

	if pid > 0 {
		session.PID = pid
	}

	if exitCode != nil {
		session.ExitCode = exitCode
		session.processTerminated = true
		now := time.Now()
		session.ExitTime = &now

		// Debug: Log exit details
		log.Printf("[DEBUG] Session %s: status %s->%s, exitCode=%d, exitTime=%v", 
			label, oldStatus, status, *exitCode, now)

		// If the connection is also lost, schedule cleanup
		if session.ConnectionStatus == ConnectionDisconnected {
			sm.scheduleCleanup(label)
		}
	} else {
		log.Printf("[DEBUG] Session %s: status %s->%s, pid=%d", 
			label, oldStatus, status, pid)
	}

	return nil
}

// SetConnection sets the WebSocket connection for a session
func (sm *SessionManager) SetConnection(label string, conn *websocket.Conn) error {
	sm.mutex.RLock()
	session, exists := sm.sessions[label]
	sm.mutex.RUnlock()

	if !exists {
		return fmt.Errorf("session not found: %s", label)
	}

	session.mutex.Lock()
	defer session.mutex.Unlock()

	session.Connection = conn
	session.ConnectionStatus = ConnectionConnected
	session.connectionLostTime = nil
	session.lastActivityTime = time.Now()

	// Cancel any scheduled cleanup since connection is restored
	sm.mutex.Lock()
	delete(sm.cleanupScheduled, label)
	session.cleanupScheduled = false
	sm.mutex.Unlock()

	return nil
}

// DisconnectSession marks a session as disconnected
func (sm *SessionManager) DisconnectSession(label string) error {
	sm.mutex.RLock()
	session, exists := sm.sessions[label]
	sm.mutex.RUnlock()

	if !exists {
		return fmt.Errorf("session not found: %s", label)
	}

	session.mutex.Lock()
	defer session.mutex.Unlock()

	session.Connection = nil
	session.ConnectionStatus = ConnectionDisconnected
	now := time.Now()
	session.connectionLostTime = &now

	// If the process is also terminated, schedule cleanup
	if session.processTerminated {
		sm.scheduleCleanup(label)
	}

	return nil
}

// scheduleCleanup schedules a session for cleanup after the configured delay
func (sm *SessionManager) scheduleCleanup(label string) {
	sm.mutex.Lock()
	defer sm.mutex.Unlock()

	cleanupTime := time.Now().Add(sm.cleanupDelay)
	sm.cleanupScheduled[label] = cleanupTime

	if session, exists := sm.sessions[label]; exists {
		session.cleanupScheduled = true
	}
}

// RemoveSession removes a session from the manager
func (sm *SessionManager) RemoveSession(label string) error {
	sm.mutex.Lock()
	defer sm.mutex.Unlock()

	session, exists := sm.sessions[label]
	if !exists {
		return fmt.Errorf("session not found: %s", label)
	}

	// Clean up the session
	session.mutex.Lock()
	if session.LogBuffer != nil {
		session.LogBuffer.Close()
	}
	if session.Connection != nil {
		session.Connection.Close()
	}
	session.mutex.Unlock()

	// Remove from sessions map
	delete(sm.sessions, label)

	// Remove from cleanup schedule
	delete(sm.cleanupScheduled, label)

	return nil
}

// backgroundCleanup runs periodic cleanup of old sessions
func (sm *SessionManager) backgroundCleanup() {
	defer sm.cleanupWg.Done()

	ticker := time.NewTicker(sm.cleanupInterval)
	defer ticker.Stop()

	for {
		select {
		case <-sm.ctx.Done():
			return
		case <-ticker.C:
			sm.performCleanup()
		}
	}
}

// performCleanup checks for sessions that need to be cleaned up
func (sm *SessionManager) performCleanup() {
	now := time.Now()
	var sessionsToCleanup []string

	sm.mutex.RLock()
	for label, cleanupTime := range sm.cleanupScheduled {
		if now.After(cleanupTime) {
			sessionsToCleanup = append(sessionsToCleanup, label)
		}
	}
	sm.mutex.RUnlock()

	// Clean up sessions that are due for cleanup
	for _, label := range sessionsToCleanup {
		if err := sm.RemoveSession(label); err != nil {
			// Log error but continue with cleanup
			fmt.Printf("Error removing session %s during cleanup: %v\n", label, err)
		}
	}
}

// GetSessionStats returns statistics about all sessions
func (sm *SessionManager) GetSessionStats() SessionManagerStats {
	sm.mutex.RLock()
	defer sm.mutex.RUnlock()

	stats := SessionManagerStats{
		TotalSessions:   len(sm.sessions),
		ActiveSessions:  0,
		StoppedSessions: 0,
		CrashedSessions: 0,
		ConnectedSessions: 0,
		DisconnectedSessions: 0,
		ScheduledForCleanup: len(sm.cleanupScheduled),
		TotalLogEntries: 0,
		TotalBufferSize: 0,
	}

	for _, session := range sm.sessions {
		session.mutex.RLock()
		switch session.Status {
		case protocol.StatusRunning, protocol.StatusRestarting:
			stats.ActiveSessions++
		case protocol.StatusStopped:
			stats.StoppedSessions++
		case protocol.StatusCrashed:
			stats.CrashedSessions++
		}

		if session.ConnectionStatus == ConnectionConnected {
			stats.ConnectedSessions++
		} else {
			stats.DisconnectedSessions++
		}

		if session.LogBuffer != nil {
			bufferStats := session.LogBuffer.GetStats()
			stats.TotalLogEntries += bufferStats.EntryCount
			stats.TotalBufferSize += bufferStats.TotalSizeBytes
		}
		session.mutex.RUnlock()
	}

	return stats
}

// Close stops the session manager and cleans up all resources
func (sm *SessionManager) Close() {
	// Cancel background cleanup
	sm.cancel()
	sm.cleanupWg.Wait()

	// Clean up all sessions
	sm.mutex.Lock()
	defer sm.mutex.Unlock()

	for label := range sm.sessions {
		sm.removeSessionUnsafe(label)
	}
}

// removeSessionUnsafe removes a session without locking (internal use)
func (sm *SessionManager) removeSessionUnsafe(label string) {
	session, exists := sm.sessions[label]
	if !exists {
		return
	}

	// Clean up the session
	session.mutex.Lock()
	if session.LogBuffer != nil {
		session.LogBuffer.Close()
	}
	if session.Connection != nil {
		session.Connection.Close()
	}
	session.mutex.Unlock()

	// Remove from maps
	delete(sm.sessions, label)
	delete(sm.cleanupScheduled, label)
}

// SessionManagerStats represents statistics about the session manager
type SessionManagerStats struct {
	TotalSessions        int `json:"total_sessions"`
	ActiveSessions       int `json:"active_sessions"`
	StoppedSessions      int `json:"stopped_sessions"`
	CrashedSessions      int `json:"crashed_sessions"`
	ConnectedSessions    int `json:"connected_sessions"`
	DisconnectedSessions int `json:"disconnected_sessions"`
	ScheduledForCleanup  int `json:"scheduled_for_cleanup"`
	TotalLogEntries      int `json:"total_log_entries"`
	TotalBufferSize      int `json:"total_buffer_size"`
}

// String returns a human-readable string representation of the stats
func (s SessionManagerStats) String() string {
	return fmt.Sprintf("Sessions: %d total (%d active, %d stopped, %d crashed), Connections: %d connected, %d disconnected, %d scheduled for cleanup, Logs: %d entries (%d bytes)",
		s.TotalSessions, s.ActiveSessions, s.StoppedSessions, s.CrashedSessions,
		s.ConnectedSessions, s.DisconnectedSessions, s.ScheduledForCleanup,
		s.TotalLogEntries, s.TotalBufferSize)
}

// Health check methods

// IsHealthy returns true if the session manager is operating normally
func (sm *SessionManager) IsHealthy() bool {
	sm.mutex.RLock()
	defer sm.mutex.RUnlock()

	// Simple health check: manager should be able to handle basic operations
	return sm.ctx.Err() == nil
}

// GetHealth returns detailed health information about the session manager
func (sm *SessionManager) GetHealth() SessionManagerHealth {
	stats := sm.GetSessionStats()

	return SessionManagerHealth{
		IsHealthy:     sm.IsHealthy(),
		Stats:         stats,
		UptimeSeconds: int(time.Since(sm.startTime).Seconds()),
	}
}

// SessionManagerHealth represents the health status of the session manager
type SessionManagerHealth struct {
	IsHealthy     bool                `json:"is_healthy"`
	Stats         SessionManagerStats `json:"stats"`
	UptimeSeconds int                 `json:"uptime_seconds"`
}