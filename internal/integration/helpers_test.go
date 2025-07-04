package integration

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/logmcp/logmcp/internal/protocol"
	"github.com/logmcp/logmcp/internal/runner"
	"github.com/logmcp/logmcp/internal/server"
)

// ReceivedMessage tracks messages received by the test server
type ReceivedMessage struct {
	ConnectionID string
	Message      interface{} // Can be any protocol message type
	Timestamp    time.Time
}

// TestWebSocketServer wraps a test server with message tracking
type TestWebSocketServer struct {
	*httptest.Server
	wsServer     *server.WebSocketServer
	sessionMgr   *server.SessionManager
	connections  sync.Map // connectionID -> *websocket.Conn
	messages     []ReceivedMessage
	mu           sync.Mutex
	messageChan  chan ReceivedMessage
}

// NewTestWebSocketServer creates a new test WebSocket server
func NewTestWebSocketServer(t *testing.T) *TestWebSocketServer {
	t.Helper()
	
	// Create session manager with short cleanup intervals for testing
	sessionMgr := server.NewSessionManagerWithConfig(
		100*time.Millisecond, // cleanup delay
		50*time.Millisecond,  // cleanup interval
	)
	
	// Create WebSocket server with short timeouts for testing
	config := server.WebSocketServerConfig{
		ReadTimeout:  5 * time.Second,  // Shorter timeout for tests
		WriteTimeout: 5 * time.Second,
		PingInterval: 10 * time.Second,
		CheckOrigin:  nil,
	}
	wsServer := server.NewWebSocketServerWithConfig(sessionMgr, config)
	
	ts := &TestWebSocketServer{
		wsServer:    wsServer,
		sessionMgr:  sessionMgr,
		messageChan: make(chan ReceivedMessage, 100),
	}
	
	// Create HTTP test server
	ts.Server = httptest.NewServer(http.HandlerFunc(wsServer.HandleWebSocket))
	
	// Start message collector
	go ts.collectMessages()
	
	return ts
}

// collectMessages collects messages into the messages slice
func (ts *TestWebSocketServer) collectMessages() {
	for msg := range ts.messageChan {
		ts.mu.Lock()
		ts.messages = append(ts.messages, msg)
		ts.mu.Unlock()
	}
}

// GetMessages returns a copy of all received messages
func (ts *TestWebSocketServer) GetMessages() []ReceivedMessage {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	
	messages := make([]ReceivedMessage, len(ts.messages))
	copy(messages, ts.messages)
	return messages
}

// GetMessageCount returns the number of messages received
func (ts *TestWebSocketServer) GetMessageCount() int {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	return len(ts.messages)
}

// Close shuts down the test server
func (ts *TestWebSocketServer) Close() {
	close(ts.messageChan)
	ts.Server.Close()
	ts.sessionMgr.Close()
	ts.wsServer.Close()
}

// WebSocketURL returns the WebSocket URL for the test server
func (ts *TestWebSocketServer) WebSocketURL() string {
	return "ws" + ts.Server.URL[4:] // Replace http with ws
}

// TestClient wraps a WebSocket client with message tracking
type TestClient struct {
	*runner.WebSocketClient
	receivedMessages []protocol.Message
	mu               sync.Mutex
	connected        chan bool
	disconnected     chan bool
	t                *testing.T
}

// NewTestClient creates a new test client
func NewTestClient(t *testing.T, serverURL, label string) *TestClient {
	t.Helper()
	
	tc := &TestClient{
		WebSocketClient: runner.NewWebSocketClient(serverURL, label),
		connected:       make(chan bool, 1),
		disconnected:    make(chan bool, 1),
		t:               t,
	}
	
	// Set up callbacks
	tc.WebSocketClient.OnConnected = func(assignedLabel string) {
		t.Logf("Client connected with label: %s", assignedLabel)
		select {
		case tc.connected <- true:
		default:
		}
	}
	
	tc.WebSocketClient.OnDisconnected = func() {
		t.Logf("Client disconnected")
		select {
		case tc.disconnected <- true:
		default:
		}
	}
	
	tc.WebSocketClient.OnError = func(err error) {
		t.Logf("Client error: %v", err)
	}
	
	return tc
}

// WaitForConnection waits for the client to connect or times out
func (tc *TestClient) WaitForConnection(timeout time.Duration) error {
	select {
	case <-tc.connected:
		return nil
	case <-time.After(timeout):
		return fmt.Errorf("connection timeout after %v", timeout)
	}
}

// WaitForDisconnection waits for the client to disconnect or times out
func (tc *TestClient) WaitForDisconnection(timeout time.Duration) error {
	select {
	case <-tc.disconnected:
		return nil
	case <-time.After(timeout):
		return fmt.Errorf("disconnection timeout after %v", timeout)
	}
}

// GetReceivedMessages returns a copy of all received messages
func (tc *TestClient) GetReceivedMessages() []protocol.Message {
	tc.mu.Lock()
	defer tc.mu.Unlock()
	
	messages := make([]protocol.Message, len(tc.receivedMessages))
	copy(messages, tc.receivedMessages)
	return messages
}

// Utility functions

// waitForCondition waits for a condition to be true or times out
func waitForCondition(timeout time.Duration, condition func() bool) error {
	deadline := time.Now().Add(timeout)
	interval := 10 * time.Millisecond
	
	for time.Now().Before(deadline) {
		if condition() {
			return nil
		}
		time.Sleep(interval)
	}
	
	return fmt.Errorf("condition not met within %v", timeout)
}

// assertMessageReceived checks if a specific message type was received
func assertMessageReceived(t *testing.T, messages []ReceivedMessage, expectedType protocol.MessageType) bool {
	t.Helper()
	
	for _, msg := range messages {
		// Check message type based on the actual message type
		switch m := msg.Message.(type) {
		case *protocol.LogMessage:
			if expectedType == protocol.MessageTypeLog {
				return true
			}
		case *protocol.StatusMessage:
			if expectedType == protocol.MessageTypeStatus {
				return true
			}
		case *protocol.SessionRegistrationMessage:
			if expectedType == protocol.MessageTypeRegister {
				return true
			}
		case *protocol.CommandMessage:
			if expectedType == protocol.MessageTypeCommand {
				return true
			}
		case *protocol.AckMessage:
			if expectedType == protocol.MessageTypeAck {
				return true
			}
		default:
			_ = m // unused
		}
	}
	
	t.Errorf("Expected message type %s not found in %d messages", expectedType, len(messages))
	return false
}

// assertNoRaceCondition verifies that registration completes without race conditions
func assertNoRaceCondition(t *testing.T, client *TestClient) {
	t.Helper()
	
	// This test specifically checks for the race condition we found
	// where handleMessages goroutine might consume the registration acknowledgment
	
	// The client should connect successfully
	err := client.Connect()
	if err != nil {
		t.Fatalf("Connection failed: %v", err)
	}
	
	// If we get here, registration was successful
	// In the race condition case, the client would hang waiting for acknowledgment
	t.Log("Registration completed successfully without race condition")
}

// createMultipleClients creates multiple test clients
func createMultipleClients(t *testing.T, serverURL string, count int) []*TestClient {
	t.Helper()
	
	clients := make([]*TestClient, count)
	for i := 0; i < count; i++ {
		label := fmt.Sprintf("client-%d", i)
		clients[i] = NewTestClient(t, serverURL, label)
	}
	
	return clients
}

// simulateHighThroughput sends many messages rapidly
func simulateHighThroughput(t *testing.T, client *TestClient, messageCount int) error {
	t.Helper()
	
	for i := 0; i < messageCount; i++ {
		err := client.SendLogMessage(
			fmt.Sprintf("Log message %d", i),
			"stdout",
			1234,
		)
		if err != nil {
			return fmt.Errorf("failed to send message %d: %w", i, err)
		}
	}
	
	return nil
}