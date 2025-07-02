package server

import (
	"fmt"
	"time"

	"github.com/logmcp/logmcp/internal/protocol"
)

// ExampleSessionManager demonstrates basic usage of the SessionManager
func ExampleSessionManager() {
	// Create a new session manager
	sm := NewSessionManager()
	defer sm.Close()

	// Create a session for a backend process
	runArgs := RunArgs{
		Command: "npm run server",
		Label:   "backend",
	}
	session, err := sm.CreateSession("backend", "npm run server", "/app", []string{"process_control"}, ModeRun, runArgs)
	if err != nil {
		fmt.Printf("Error creating session: %v\n", err)
		return
	}

	fmt.Printf("Created session with label: %s\n", session.Label)

	// Create another session with the same label - should get a different label
	session2, err := sm.CreateSession("backend", "npm run dev", "/app", []string{"process_control"}, ModeRun, runArgs)
	if err != nil {
		fmt.Printf("Error creating session: %v\n", err)
		return
	}

	fmt.Printf("Created session with label: %s\n", session2.Label)

	// List all sessions
	sessions := sm.ListSessions()
	fmt.Printf("Total sessions: %d\n", len(sessions))

	// Update session status
	exitCode := 0
	err = sm.UpdateSessionStatus(session.ID, protocol.StatusStopped, 1234, &exitCode)
	if err != nil {
		fmt.Printf("Error updating session: %v\n", err)
		return
	}

	fmt.Printf("Updated session %s status to stopped\n", session.Label)

	// Get session statistics
	stats := sm.GetSessionStats()
	fmt.Printf("Active sessions: %d, Stopped sessions: %d\n", stats.ActiveSessions, stats.StoppedSessions)

	// Output:
	// Created session with label: backend
	// Created session with label: backend-2
	// Total sessions: 2
	// Updated session backend status to stopped
	// Active sessions: 1, Stopped sessions: 1
}

// ExampleSessionManager_logBuffering demonstrates log buffering functionality
func ExampleSessionManager_logBuffering() {
	sm := NewSessionManager()
	defer sm.Close()

	// Create a session
	session, err := sm.CreateSession("app", "python app.py", "/app", nil, ModeRun, nil)
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}

	// Add some log entries to the session's buffer
	logMsg1 := protocol.NewLogMessage(session.ID, session.Label, "Application starting...", protocol.StreamStdout, 1234)
	session.LogBuffer.AddFromMessage(logMsg1)

	logMsg2 := protocol.NewLogMessage(session.ID, session.Label, "Database connected", protocol.StreamStdout, 1234)
	session.LogBuffer.AddFromMessage(logMsg2)

	logMsg3 := protocol.NewLogMessage(session.ID, session.Label, "Error: Connection failed", protocol.StreamStderr, 1234)
	session.LogBuffer.AddFromMessage(logMsg3)

	// Get buffer statistics
	stats := session.LogBuffer.GetStats()
	fmt.Printf("Log entries in buffer: %d\n", stats.EntryCount)
	fmt.Printf("Buffer size: %d+ bytes\n", stats.TotalSizeBytes/10*10) // Round down to nearest 10 for consistent output

	// Search for error logs
	errorLogs, err := session.LogBuffer.Search("Error:")
	if err != nil {
		fmt.Printf("Search error: %v\n", err)
		return
	}

	fmt.Printf("Found %d error logs\n", len(errorLogs))
	if len(errorLogs) > 0 {
		fmt.Printf("Error log: %s\n", errorLogs[0].Content)
	}

	// Output:
	// Log entries in buffer: 3
	// Buffer size: 120+ bytes
	// Found 1 error logs
	// Error log: Error: Connection failed
}

// ExampleSessionManager_cleanup demonstrates session cleanup functionality
func ExampleSessionManager_cleanup() {
	// Use a very short cleanup delay for demonstration
	sm := NewSessionManagerWithConfig(50*time.Millisecond, 10*time.Millisecond)
	defer sm.Close()

	// Create a session
	session, err := sm.CreateSession("temp", "echo hello", "/tmp", nil, ModeRun, nil)
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}

	fmt.Printf("Created session: %s\n", session.Label)

	// Simulate process termination and disconnection
	err = sm.DisconnectSession(session.ID)
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}

	exitCode := 0
	err = sm.UpdateSessionStatus(session.ID, protocol.StatusStopped, 1234, &exitCode)
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}

	fmt.Printf("Session marked as stopped and disconnected\n")

	// Session should still exist immediately
	_, err = sm.GetSession(session.ID)
	if err != nil {
		fmt.Printf("Unexpected error: %v\n", err)
		return
	}

	fmt.Printf("Session still exists immediately after termination\n")

	// Wait for cleanup
	time.Sleep(100 * time.Millisecond)

	// Session should be cleaned up now
	_, err = sm.GetSession(session.ID)
	if err != nil {
		fmt.Printf("Session cleaned up successfully\n")
	} else {
		fmt.Printf("Session still exists after cleanup delay\n")
	}

	// Output:
	// Created session: temp
	// Session marked as stopped and disconnected
	// Session still exists immediately after termination
	// Session cleaned up successfully
}