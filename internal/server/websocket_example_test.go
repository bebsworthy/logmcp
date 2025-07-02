package server_test

import (
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/logmcp/logmcp/internal/server"
)

// ExampleWebSocketServer demonstrates basic WebSocket server usage
func ExampleWebSocketServer() {
	// Create session manager
	sm := server.NewSessionManager()
	defer sm.Close()

	// Create WebSocket server
	ws := server.NewWebSocketServer(sm)
	defer ws.Close()

	// Create HTTP server with WebSocket endpoint
	mux := http.NewServeMux()
	mux.HandleFunc("/", ws.HandleWebSocket)

	// Start server (in a real application, this would be in a goroutine)
	server := &http.Server{
		Addr:    ":8765",
		Handler: mux,
	}

	// For demonstration, we'll just show the setup
	fmt.Printf("WebSocket server would start on %s\n", server.Addr)
	fmt.Printf("Server is healthy: %v\n", ws.IsHealthy())

	// Show connection stats
	stats := ws.GetConnectionStats()
	fmt.Printf("Active connections: %d\n", stats.TotalConnections)

	// Output:
	// WebSocket server would start on :8765
	// Server is healthy: true
	// Active connections: 0
}

// ExampleWebSocketServer_customConfig demonstrates WebSocket server with custom configuration
func ExampleWebSocketServer_customConfig() {
	// Create session manager with custom cleanup intervals for demo
	sm := server.NewSessionManagerWithConfig(1*time.Hour, 1*time.Minute)
	defer sm.Close()

	// Create custom WebSocket configuration
	config := server.WebSocketServerConfig{
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 10 * time.Second,
		PingInterval: 25 * time.Second,
		CheckOrigin: func(r *http.Request) bool {
			// In production, implement proper origin checking
			return true
		},
	}

	// Create WebSocket server with custom config
	ws := server.NewWebSocketServerWithConfig(sm, config)
	defer ws.Close()

	fmt.Printf("WebSocket server created with custom config\n")
	fmt.Printf("Server is healthy: %v\n", ws.IsHealthy())

	// Output:
	// WebSocket server created with custom config
	// Server is healthy: true
}

// ExampleWebSocketServer_messageRouting demonstrates message routing capabilities
func ExampleWebSocketServer_messageRouting() {
	// Create session manager
	sm := server.NewSessionManager()
	defer sm.Close()

	// Create WebSocket server
	ws := server.NewWebSocketServer(sm)
	defer ws.Close()

	// Create a test session to demonstrate command sending
	session, err := sm.CreateSession(
		"example-backend",
		"npm run server",
		"/app",
		[]string{"process_control", "stdin"},
		server.ModeRun,
		server.RunArgs{
			Command: "npm run server",
			Label:   "example-backend",
		},
	)
	if err != nil {
		log.Printf("Failed to create session: %v", err)
		return
	}

	fmt.Printf("Created session with label: %s\n", session.Label)
	fmt.Printf("Session status: %s\n", session.Status)
	fmt.Printf("Session capabilities: %v\n", session.Capabilities)

	// In a real scenario, you would send commands to connected clients
	// ws.SendCommand(session.ID, protocol.ActionRestart, nil)
	// ws.SendStdin(session.ID, "some input\n")

	// Show session stats
	sessionStats := sm.GetSessionStats()
	fmt.Printf("Total sessions: %d\n", sessionStats.TotalSessions)

	// Output:
	// Created session with label: example-backend
	// Session status: running
	// Session capabilities: [process_control stdin]
	// Total sessions: 1
}

// ExampleWebSocketServer_healthMonitoring demonstrates health monitoring
func ExampleWebSocketServer_healthMonitoring() {
	// Create session manager
	sm := server.NewSessionManager()
	defer sm.Close()

	// Create WebSocket server
	ws := server.NewWebSocketServer(sm)
	defer ws.Close()

	// Check WebSocket server health
	wsHealth := ws.GetHealth()
	fmt.Printf("WebSocket server healthy: %v\n", wsHealth.IsHealthy)
	fmt.Printf("Session manager OK: %v\n", wsHealth.SessionManagerOK)
	fmt.Printf("Total connections: %d\n", wsHealth.ConnectionStats.TotalConnections)

	// Check session manager health
	smHealth := sm.GetHealth()
	fmt.Printf("Session manager healthy: %v\n", smHealth.IsHealthy)
	fmt.Printf("Active sessions: %d\n", smHealth.Stats.ActiveSessions)
	fmt.Printf("Uptime: %d seconds\n", smHealth.UptimeSeconds)

	// Output:
	// WebSocket server healthy: true
	// Session manager OK: true
	// Total connections: 0
	// Session manager healthy: true
	// Active sessions: 0
	// Uptime: 0 seconds
}