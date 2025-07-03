package server

import (
	"testing"
	"time"

	"github.com/logmcp/logmcp/internal/protocol"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewSessionManager(t *testing.T) {
	sm := NewSessionManager()
	defer sm.Close()

	assert.NotNil(t, sm)
	assert.NotNil(t, sm.sessions)
	assert.NotNil(t, sm.cleanupScheduled)
	assert.Equal(t, DefaultCleanupDelay, sm.cleanupDelay)
}

func TestCreateSession(t *testing.T) {
	sm := NewSessionManager()
	defer sm.Close()

	args := RunArgs{
		Command: "test command",
		Label:   "test",
	}

	session, err := sm.CreateSession("test", "test command", "/tmp", []string{"process_control"}, ModeRun, args)
	require.NoError(t, err)
	require.NotNil(t, session)

	assert.Equal(t, "test", session.Label)
	assert.Equal(t, "test command", session.Command)
	assert.Equal(t, "/tmp", session.WorkingDir)
	assert.Equal(t, protocol.StatusRunning, session.Status)
	assert.Equal(t, []string{"process_control"}, session.Capabilities)
	assert.Equal(t, ModeRun, session.RunnerMode)
	assert.Equal(t, args, session.RunnerArgs)
	assert.NotNil(t, session.LogBuffer)
	assert.Equal(t, ConnectionDisconnected, session.ConnectionStatus)
}

func TestLabelConflictResolution(t *testing.T) {
	sm := NewSessionManager()
	defer sm.Close()

	// Create first session with label "test"
	session1, err := sm.CreateSession("test", "cmd1", "/tmp", nil, ModeRun, nil)
	require.NoError(t, err)
	assert.Equal(t, "test", session1.Label)

	// Create second session with same label - should get "test-2"
	session2, err := sm.CreateSession("test", "cmd2", "/tmp", nil, ModeRun, nil)
	require.NoError(t, err)
	assert.Equal(t, "test-2", session2.Label)

	// Create third session with same label - should get "test-3"
	session3, err := sm.CreateSession("test", "cmd3", "/tmp", nil, ModeRun, nil)
	require.NoError(t, err)
	assert.Equal(t, "test-3", session3.Label)

	// Remove session2 and create another - should reuse "test-2" since it's available
	err = sm.RemoveSession(session2.Label)
	require.NoError(t, err)

	session4, err := sm.CreateSession("test", "cmd4", "/tmp", nil, ModeRun, nil)
	require.NoError(t, err)
	assert.Equal(t, "test-2", session4.Label)
}

func TestGetSession(t *testing.T) {
	sm := NewSessionManager()
	defer sm.Close()

	// Test getting non-existent session
	_, err := sm.GetSession("non-existent")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "session not found")

	// Create a session
	session, err := sm.CreateSession("test", "cmd", "/tmp", nil, ModeRun, nil)
	require.NoError(t, err)

	// Test getting existing session
	retrieved, err := sm.GetSession(session.Label)
	require.NoError(t, err)
	assert.Equal(t, session.Label, retrieved.Label)
	assert.Equal(t, session.Label, retrieved.Label)
}

func TestGetSessionByLabel(t *testing.T) {
	sm := NewSessionManager()
	defer sm.Close()

	// Test getting non-existent label
	_, err := sm.GetSession("non-existent")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "session not found")

	// Create sessions
	session1, err := sm.CreateSession("test", "cmd1", "/tmp", nil, ModeRun, nil)
	require.NoError(t, err)

	session2, err := sm.CreateSession("test", "cmd2", "/tmp", nil, ModeRun, nil)
	require.NoError(t, err)

	// Test getting session by label
	retrieved, err := sm.GetSession("test")
	require.NoError(t, err)
	assert.Equal(t, session1.Label, retrieved.Label)

	// Test getting session by resolved label
	retrieved2, err := sm.GetSession("test-2")
	require.NoError(t, err)
	assert.Equal(t, session2.Label, retrieved2.Label)
}

func TestGetSessionsByLabel(t *testing.T) {
	sm := NewSessionManager()
	defer sm.Close()

	// Test getting non-existent label
	session := sm.GetSessionsByLabel("non-existent")
	assert.Nil(t, session)

	// Create sessions with same original label
	session1, err := sm.CreateSession("test", "cmd1", "/tmp", nil, ModeRun, nil)
	require.NoError(t, err)

	session2, err := sm.CreateSession("test", "cmd2", "/tmp", nil, ModeRun, nil)
	require.NoError(t, err)

	// Test getting sessions by original label (should return session1)
	retrieved := sm.GetSessionsByLabel("test")
	assert.NotNil(t, retrieved)
	assert.Equal(t, session1.Label, retrieved.Label)

	// Test getting sessions by resolved label (should return session2)
	retrieved2 := sm.GetSessionsByLabel("test-2")
	assert.NotNil(t, retrieved2)
	assert.Equal(t, session2.Label, retrieved2.Label)
}

func TestListSessions(t *testing.T) {
	sm := NewSessionManager()
	defer sm.Close()

	// Test empty list
	sessions := sm.ListSessions()
	assert.Empty(t, sessions)

	// Create some sessions
	session1, err := sm.CreateSession("test1", "cmd1", "/tmp", nil, ModeRun, nil)
	require.NoError(t, err)

	session2, err := sm.CreateSession("test2", "cmd2", "/tmp", nil, ModeForward, nil)
	require.NoError(t, err)

	// Test listing sessions
	sessions = sm.ListSessions()
	assert.Len(t, sessions, 2)

	// Check that both sessions are present (order not guaranteed)
	sessionLabels := []string{sessions[0].Label, sessions[1].Label}
	assert.Contains(t, sessionLabels, session1.Label)
	assert.Contains(t, sessionLabels, session2.Label)
}

func TestUpdateSessionStatus(t *testing.T) {
	sm := NewSessionManager()
	defer sm.Close()

	// Test updating non-existent session
	err := sm.UpdateSessionStatus("non-existent", protocol.StatusStopped, 0, nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "session not found")

	// Create a session
	session, err := sm.CreateSession("test", "cmd", "/tmp", nil, ModeRun, nil)
	require.NoError(t, err)

	// Test updating status
	exitCode := 0
	err = sm.UpdateSessionStatus(session.Label, protocol.StatusStopped, 1234, &exitCode)
	require.NoError(t, err)

	// Verify status was updated
	retrieved, err := sm.GetSession(session.Label)
	require.NoError(t, err)
	assert.Equal(t, protocol.StatusStopped, retrieved.Status)
	assert.Equal(t, 1234, retrieved.PID)
	assert.NotNil(t, retrieved.ExitCode)
	assert.Equal(t, 0, *retrieved.ExitCode)
	assert.NotNil(t, retrieved.ExitTime)
}

func TestSetConnectionAndDisconnect(t *testing.T) {
	sm := NewSessionManager()
	defer sm.Close()

	// Create a session
	session, err := sm.CreateSession("test", "cmd", "/tmp", nil, ModeRun, nil)
	require.NoError(t, err)

	// Initially should be disconnected
	assert.Equal(t, ConnectionDisconnected, session.ConnectionStatus)
	assert.Nil(t, session.Connection)

	// Set connection (we'll use nil as mock connection for test purposes)
	err = sm.SetConnection(session.Label, nil)
	require.NoError(t, err)

	// Verify connection status
	retrieved, err := sm.GetSession(session.Label)
	require.NoError(t, err)
	assert.Equal(t, ConnectionConnected, retrieved.ConnectionStatus)

	// Disconnect
	err = sm.DisconnectSession(session.Label)
	require.NoError(t, err)

	// Verify disconnection
	retrieved, err = sm.GetSession(session.Label)
	require.NoError(t, err)
	assert.Equal(t, ConnectionDisconnected, retrieved.ConnectionStatus)
	assert.NotNil(t, retrieved.connectionLostTime)
}

func TestRemoveSession(t *testing.T) {
	sm := NewSessionManager()
	defer sm.Close()

	// Test removing non-existent session
	err := sm.RemoveSession("non-existent")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "session not found")

	// Create a session
	session, err := sm.CreateSession("test", "cmd", "/tmp", nil, ModeRun, nil)
	require.NoError(t, err)

	// Verify session exists
	_, err = sm.GetSession(session.Label)
	require.NoError(t, err)

	// Remove session
	err = sm.RemoveSession(session.Label)
	require.NoError(t, err)

	// Verify session no longer exists
	_, err = sm.GetSession(session.Label)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "session not found")

	// Verify session removed from label map
	sessions := sm.GetSessionsByLabel("test")
	assert.Empty(t, sessions)
}

func TestScheduleCleanup(t *testing.T) {
	sm := NewSessionManager()
	defer sm.Close()

	// Create a session
	session, err := sm.CreateSession("test", "cmd", "/tmp", nil, ModeRun, nil)
	require.NoError(t, err)

	// Mark as both disconnected and terminated
	err = sm.DisconnectSession(session.Label)
	require.NoError(t, err)

	exitCode := 0
	err = sm.UpdateSessionStatus(session.Label, protocol.StatusStopped, 1234, &exitCode)
	require.NoError(t, err)

	// Verify cleanup is scheduled
	sm.mutex.RLock()
	cleanupTime, scheduled := sm.cleanupScheduled[session.Label]
	sm.mutex.RUnlock()

	assert.True(t, scheduled)
	assert.True(t, cleanupTime.After(time.Now()))

	// Verify session is marked for cleanup
	retrieved, err := sm.GetSession(session.Label)
	require.NoError(t, err)
	assert.True(t, retrieved.cleanupScheduled)
}

func TestSessionManagerStats(t *testing.T) {
	sm := NewSessionManager()
	defer sm.Close()

	// Test empty stats
	stats := sm.GetSessionStats()
	assert.Equal(t, 0, stats.TotalSessions)
	assert.Equal(t, 0, stats.ActiveSessions)
	assert.Equal(t, 0, stats.ConnectedSessions)

	// Create some sessions
	session1, err := sm.CreateSession("test1", "cmd1", "/tmp", nil, ModeRun, nil)
	require.NoError(t, err)

	session2, err := sm.CreateSession("test2", "cmd2", "/tmp", nil, ModeRun, nil)
	require.NoError(t, err)

	// Connect one session
	err = sm.SetConnection(session1.Label, nil)
	require.NoError(t, err)

	// Stop one session
	exitCode := 0
	err = sm.UpdateSessionStatus(session2.Label, protocol.StatusStopped, 1234, &exitCode)
	require.NoError(t, err)

	// Check stats
	stats = sm.GetSessionStats()
	assert.Equal(t, 2, stats.TotalSessions)
	assert.Equal(t, 1, stats.ActiveSessions)
	assert.Equal(t, 1, stats.StoppedSessions)
	assert.Equal(t, 1, stats.ConnectedSessions)
	assert.Equal(t, 1, stats.DisconnectedSessions)
}

func TestHealthCheck(t *testing.T) {
	sm := NewSessionManager()
	defer sm.Close()

	// Test healthy state
	assert.True(t, sm.IsHealthy())

	health := sm.GetHealth()
	assert.True(t, health.IsHealthy)
	assert.NotNil(t, health.Stats)

	// Close and test unhealthy state
	sm.Close()
	assert.False(t, sm.IsHealthy())
}

func TestRunnerModeAndArgs(t *testing.T) {
	sm := NewSessionManager()
	defer sm.Close()

	// Test run mode
	runArgs := RunArgs{Command: "npm start", Label: "app"}
	session1, err := sm.CreateSession("app", "npm start", "/app", nil, ModeRun, runArgs)
	require.NoError(t, err)
	assert.Equal(t, ModeRun, session1.RunnerMode)
	assert.Equal(t, runArgs, session1.RunnerArgs)

	// Test forward mode
	forwardArgs := ForwardArgs{Source: "/var/log/app.log", Label: "app-logs"}
	session2, err := sm.CreateSession("app-logs", "file forward", "/var/log", nil, ModeForward, forwardArgs)
	require.NoError(t, err)
	assert.Equal(t, ModeForward, session2.RunnerMode)
	assert.Equal(t, forwardArgs, session2.RunnerArgs)

	// Test managed mode
	managedArgs := ManagedArgs{
		Command:     "python app.py",
		Label:       "python-app",
		WorkingDir:  "/app",
		Environment: map[string]string{"ENV": "test"},
	}
	session3, err := sm.CreateSession("python-app", "python app.py", "/app", nil, ModeManaged, managedArgs)
	require.NoError(t, err)
	assert.Equal(t, ModeManaged, session3.RunnerMode)
	assert.Equal(t, managedArgs, session3.RunnerArgs)
}

func TestConcurrentAccess(t *testing.T) {
	sm := NewSessionManager()
	defer sm.Close()

	// Test concurrent session creation
	const numGoroutines = 10
	const numSessionsPerGoroutine = 5

	// Channel to collect created sessions
	sessionChan := make(chan *Session, numGoroutines*numSessionsPerGoroutine)
	errorChan := make(chan error, numGoroutines*numSessionsPerGoroutine)

	// Start goroutines
	for i := 0; i < numGoroutines; i++ {
		go func(goroutineID int) {
			for j := 0; j < numSessionsPerGoroutine; j++ {
				label := "concurrent-test"
				session, err := sm.CreateSession(label, "test", "/tmp", nil, ModeRun, nil)
				if err != nil {
					errorChan <- err
					return
				}
				sessionChan <- session
			}
		}(i)
	}

	// Collect results
	var sessions []*Session
	for i := 0; i < numGoroutines*numSessionsPerGoroutine; i++ {
		select {
		case session := <-sessionChan:
			sessions = append(sessions, session)
		case err := <-errorChan:
			t.Fatalf("Error during concurrent session creation: %v", err)
		case <-time.After(5 * time.Second):
			t.Fatal("Timeout waiting for concurrent session creation")
		}
	}

	// Verify all sessions were created
	assert.Len(t, sessions, numGoroutines*numSessionsPerGoroutine)

	// Verify all sessions have unique IDs
	seenIDs := make(map[string]bool)
	for _, session := range sessions {
		assert.False(t, seenIDs[session.Label], "Duplicate session ID: %s", session.Label)
		seenIDs[session.Label] = true
	}

	// Verify label conflict resolution worked
	labelCounts := make(map[string]int)
	for _, session := range sessions {
		labelCounts[session.Label]++
	}

	// Each resolved label should appear exactly once
	for label, count := range labelCounts {
		assert.Equal(t, 1, count, "Label %s appeared %d times, expected 1", label, count)
	}
}

func TestCleanupAfterProcessTerminationAndDisconnection(t *testing.T) {
	// Create session manager with very short cleanup delay and interval for testing
	sm := NewSessionManagerWithConfig(100*time.Millisecond, CleanupCheckIntervalForTesting)
	defer sm.Close()

	// Create a session
	session, err := sm.CreateSession("test", "cmd", "/tmp", nil, ModeRun, nil)
	require.NoError(t, err)

	// Connect and then disconnect
	err = sm.SetConnection(session.Label, nil)
	require.NoError(t, err)

	err = sm.DisconnectSession(session.Label)
	require.NoError(t, err)

	// Terminate process
	exitCode := 0
	err = sm.UpdateSessionStatus(session.Label, protocol.StatusStopped, 1234, &exitCode)
	require.NoError(t, err)

	// Session should still exist immediately
	_, err = sm.GetSession(session.Label)
	require.NoError(t, err)

	// Verify cleanup is scheduled
	sm.mutex.RLock()
	_, scheduled := sm.cleanupScheduled[session.Label]
	sm.mutex.RUnlock()
	assert.True(t, scheduled, "Cleanup should be scheduled")

	// Wait for cleanup to occur - allow more time for cleanup goroutine
	time.Sleep(500 * time.Millisecond)

	// Session should be cleaned up
	_, err = sm.GetSession(session.Label)
	if err == nil {
		t.Logf("Session still exists, checking cleanup state...")
		sm.mutex.RLock()
		_, stillScheduled := sm.cleanupScheduled[session.Label]
		sm.mutex.RUnlock()
		t.Logf("Session still scheduled for cleanup: %v", stillScheduled)
	}
	assert.Error(t, err)
	if err != nil {
		assert.Contains(t, err.Error(), "session not found")
	}
}

func TestReconnectionCancelsCleanup(t *testing.T) {
	// Create session manager with short cleanup delay for testing
	sm := NewSessionManagerWithConfig(100*time.Millisecond, CleanupCheckIntervalForTesting)
	defer sm.Close()

	// Create a session
	session, err := sm.CreateSession("test", "cmd", "/tmp", nil, ModeRun, nil)
	require.NoError(t, err)

	// Connect, disconnect, and terminate
	err = sm.SetConnection(session.Label, nil)
	require.NoError(t, err)

	err = sm.DisconnectSession(session.Label)
	require.NoError(t, err)

	exitCode := 0
	err = sm.UpdateSessionStatus(session.Label, protocol.StatusStopped, 1234, &exitCode)
	require.NoError(t, err)

	// Verify cleanup is scheduled
	sm.mutex.RLock()
	_, scheduled := sm.cleanupScheduled[session.Label]
	sm.mutex.RUnlock()
	assert.True(t, scheduled)

	// Reconnect before cleanup occurs
	err = sm.SetConnection(session.Label, nil)
	require.NoError(t, err)

	// Verify cleanup is cancelled
	sm.mutex.RLock()
	_, scheduled = sm.cleanupScheduled[session.Label]
	sm.mutex.RUnlock()
	assert.False(t, scheduled)

	// Wait past cleanup time
	time.Sleep(200 * time.Millisecond)

	// Session should still exist
	_, err = sm.GetSession(session.Label)
	require.NoError(t, err)
}