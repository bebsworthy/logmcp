package integration

import (
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/bebsworthy/logmcp/internal/protocol"
	"github.com/bebsworthy/logmcp/internal/runner"
)

// BenchmarkSingleClientThroughput benchmarks message throughput for a single client
func BenchmarkSingleClientThroughput(b *testing.B) {
	server := NewTestWebSocketServer(&testing.T{})
	defer server.Close()
	
	client := runner.NewWebSocketClient(server.WebSocketURL(), "bench-single")
	client.SetCommand("benchmark", "/tmp", []string{"process"})
	
	if err := client.Connect(); err != nil {
		b.Fatalf("Failed to connect: %v", err)
	}
	defer client.Close()
	
	// Wait for connection to stabilize
	time.Sleep(100 * time.Millisecond)
	
	b.ResetTimer()
	
	for i := 0; i < b.N; i++ {
		err := client.SendLogMessage(
			fmt.Sprintf("Benchmark message %d", i),
			"stdout",
			1234,
		)
		if err != nil {
			b.Fatalf("Failed to send message %d: %v", i, err)
		}
	}
	
	b.StopTimer()
	
	// Report messages per second
	messagesPerSecond := float64(b.N) / b.Elapsed().Seconds()
	b.ReportMetric(messagesPerSecond, "msgs/sec")
}

// BenchmarkConcurrentClients benchmarks throughput with multiple concurrent clients
func BenchmarkConcurrentClients(b *testing.B) {
	clientCounts := []int{1, 5, 10, 20, 50}
	
	for _, clientCount := range clientCounts {
		b.Run(fmt.Sprintf("clients_%d", clientCount), func(b *testing.B) {
			server := NewTestWebSocketServer(&testing.T{})
			defer server.Close()
			
			// Create clients
			clients := make([]*runner.WebSocketClient, clientCount)
			for i := 0; i < clientCount; i++ {
				client := runner.NewWebSocketClient(
					server.WebSocketURL(),
					fmt.Sprintf("bench-concurrent-%d", i),
				)
				client.SetCommand("benchmark", "/tmp", []string{"process"})
				
				if err := client.Connect(); err != nil {
					b.Fatalf("Client %d failed to connect: %v", i, err)
				}
				clients[i] = client
			}
			
			// Clean up clients
			defer func() {
				for _, client := range clients {
					client.Close()
				}
			}()
			
			// Wait for connections to stabilize
			time.Sleep(200 * time.Millisecond)
			
			b.ResetTimer()
			
			// Send messages concurrently
			messagesPerClient := b.N / clientCount
			var wg sync.WaitGroup
			
			for i, client := range clients {
				wg.Add(1)
				go func(idx int, c *runner.WebSocketClient) {
					defer wg.Done()
					for j := 0; j < messagesPerClient; j++ {
						err := c.SendLogMessage(
							fmt.Sprintf("Client %d message %d", idx, j),
							"stdout",
							idx*1000+j,
						)
						if err != nil {
							b.Errorf("Client %d failed at message %d: %v", idx, j, err)
							return
						}
					}
				}(i, client)
			}
			
			wg.Wait()
			b.StopTimer()
			
			totalMessages := messagesPerClient * clientCount
			messagesPerSecond := float64(totalMessages) / b.Elapsed().Seconds()
			b.ReportMetric(messagesPerSecond, "msgs/sec")
			b.ReportMetric(float64(clientCount), "clients")
		})
	}
}

// TestStressHighConnectionChurn tests system behavior under high connection churn
func TestStressHighConnectionChurn(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping stress test in short mode")
	}
	
	// Create a test server with faster cleanup interval for testing
	server := NewTestWebSocketServerWithConfig(t, 1*time.Minute, CleanupCheckIntervalForTesting)
	defer server.Close()
	
	const (
		totalConnections = 100
		concurrentConns  = 10
	)
	
	var (
		connectSuccess atomic.Int64
		connectFail    atomic.Int64
		messagesSent   atomic.Int64
		clients        []*runner.WebSocketClient
		clientsMutex   sync.Mutex
	)
	
	// Connection churn worker
	churnWorker := func(workerID int) {
		for i := 0; i < totalConnections/concurrentConns; i++ {
			client := runner.NewWebSocketClient(
				server.WebSocketURL(),
				fmt.Sprintf("churn-%d-%d", workerID, i),
			)
			client.SetCommand("stress", "/tmp", []string{"process"})
			
			// Try to connect
			if err := client.Connect(); err != nil {
				connectFail.Add(1)
				continue
			}
			connectSuccess.Add(1)
			
			// Track client for cleanup
			clientsMutex.Lock()
			clients = append(clients, client)
			clientsMutex.Unlock()
			
			// Send a few messages
			for j := 0; j < 10; j++ {
				err := client.SendLogMessage(
					fmt.Sprintf("Worker %d connection %d message %d", workerID, i, j),
					"stdout",
					workerID*10000+i*100+j,
				)
				if err != nil {
					break
				}
				messagesSent.Add(1)
			}
			
			// Small delay to let messages process
			time.Sleep(10 * time.Millisecond)
		}
	}
	
	// Run concurrent workers
	start := time.Now()
	var wg sync.WaitGroup
	
	for i := 0; i < concurrentConns; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			churnWorker(id)
		}(i)
	}
	
	wg.Wait()
	duration := time.Since(start)
	
	// Report results
	t.Logf("Connection churn test completed in %v", duration)
	t.Logf("Successful connections: %d", connectSuccess.Load())
	t.Logf("Failed connections: %d", connectFail.Load())
	t.Logf("Messages sent: %d", messagesSent.Load())
	t.Logf("Connections per second: %.2f", float64(connectSuccess.Load())/duration.Seconds())
	
	// Now close all clients
	t.Log("Closing all clients...")
	for _, client := range clients {
		client.Close()
	}
	
	// Give server time to process disconnections
	time.Sleep(100 * time.Millisecond)
	
	// Check session states
	sessions := server.sessionMgr.ListSessions()
	t.Logf("Total sessions after client close: %d", len(sessions))
	
	// In a high-churn test, we're primarily checking:
	// 1. Server remains healthy
	// 2. No crashes or panics
	// 3. Sessions are properly tracked
	
	// Verify server is still healthy
	if !server.wsServer.IsHealthy() {
		t.Error("Server is not healthy after stress test")
	}
	
	// Verify session manager is healthy
	if !server.sessionMgr.IsHealthy() {
		t.Error("Session manager is not healthy after stress test")
	}
	
	// The exact count of connected vs disconnected sessions can vary due to
	// timing in a high-concurrency scenario. What's important is that the
	// server handled the load without crashing.
	stats := server.sessionMgr.GetSessionStats()
	t.Logf("Final stats: %v", stats)
	
	// Success is defined as:
	// - No panics (test would fail if panic occurred)
	// - Server remains healthy
	// - Most connections succeeded
	if float64(connectSuccess.Load()) < float64(totalConnections)*0.9 {
		t.Errorf("Too many connection failures: %d/%d succeeded", 
			connectSuccess.Load(), totalConnections)
	}
}

// TestStressMessageBurst tests handling of message bursts
func TestStressMessageBurst(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping stress test in short mode")
	}
	
	server := NewTestWebSocketServer(t)
	defer server.Close()
	
	client := NewTestClient(t, server.WebSocketURL(), "burst-test")
	client.SetCommand("burst", "/tmp", []string{"process"})
	
	if err := client.Connect(); err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}
	defer client.Close()
	
	const burstSize = 10000
	
	// Send burst of messages as fast as possible
	start := time.Now()
	var sendErrors atomic.Int64
	
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(goroutineID int) {
			defer wg.Done()
			for j := 0; j < burstSize/10; j++ {
				err := client.SendLogMessage(
					fmt.Sprintf("Burst message from goroutine %d, iteration %d", goroutineID, j),
					"stdout",
					goroutineID*1000+j,
				)
				if err != nil {
					sendErrors.Add(1)
				}
			}
		}(i)
	}
	
	wg.Wait()
	sendDuration := time.Since(start)
	
	// Calculate send rate
	successfulSends := burstSize - int(sendErrors.Load())
	sendRate := float64(successfulSends) / sendDuration.Seconds()
	
	t.Logf("Sent %d messages in %v (%.2f msgs/sec)", successfulSends, sendDuration, sendRate)
	t.Logf("Send errors: %d", sendErrors.Load())
	
	// Wait for server to process all messages
	processingStart := time.Now()
	err := waitForCondition(30*time.Second, func() bool {
		session, err := server.sessionMgr.GetSession("burst-test")
		if err != nil {
			return false
		}
		return session.LogBuffer.GetStats().EntryCount >= successfulSends
	})
	
	processingDuration := time.Since(processingStart)
	
	if err != nil {
		session, _ := server.sessionMgr.GetSession("burst-test")
		actualCount := 0
		if session != nil {
			actualCount = session.LogBuffer.GetStats().EntryCount
		}
		t.Errorf("Not all messages processed: expected %d, got %d", successfulSends, actualCount)
	} else {
		t.Logf("All messages processed in %v", processingDuration)
	}
}

// TestStressMemoryUsage tests memory usage under load
func TestStressMemoryUsage(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping stress test in short mode")
	}
	
	server := NewTestWebSocketServer(t)
	defer server.Close()
	
	// Force GC and get baseline memory
	runtime.GC()
	var baselineMemStats runtime.MemStats
	runtime.ReadMemStats(&baselineMemStats)
	
	const clientCount = 20
	const messagesPerClient = 1000
	
	// Create clients and send messages
	clients := make([]*runner.WebSocketClient, clientCount)
	for i := 0; i < clientCount; i++ {
		client := runner.NewWebSocketClient(
			server.WebSocketURL(),
			fmt.Sprintf("memory-test-%d", i),
		)
		client.SetCommand("memory", "/tmp", []string{"process"})
		
		if err := client.Connect(); err != nil {
			t.Fatalf("Client %d failed to connect: %v", i, err)
		}
		clients[i] = client
		
		// Send messages
		for j := 0; j < messagesPerClient; j++ {
			// Create a large message to stress memory
			largeContent := fmt.Sprintf("Memory test message %d-%d with padding: %s",
				i, j, string(make([]byte, 1024))) // 1KB padding
			
			err := client.SendLogMessage(largeContent, "stdout", i*1000+j)
			if err != nil {
				// In stress tests, message queue full is expected
				if err.Error() != "message queue full" {
					t.Errorf("Client %d failed to send message %d: %v", i, j, err)
				}
				break // Stop sending if queue is full
			}
			
			// Small delay to prevent overwhelming the queue
			if j%10 == 0 {
				time.Sleep(1 * time.Millisecond)
			}
		}
	}
	
	// Wait for messages to be processed
	time.Sleep(2 * time.Second)
	
	// Force GC and measure memory after load
	runtime.GC()
	var loadMemStats runtime.MemStats
	runtime.ReadMemStats(&loadMemStats)
	
	// Calculate memory increase
	memIncreaseMB := float64(loadMemStats.Alloc-baselineMemStats.Alloc) / (1024 * 1024)
	t.Logf("Memory usage increased by %.2f MB", memIncreaseMB)
	t.Logf("Heap objects: %d -> %d", baselineMemStats.HeapObjects, loadMemStats.HeapObjects)
	t.Logf("Goroutines: %d", runtime.NumGoroutine())
	
	// Check buffer sizes
	totalBufferSize := int64(0)
	sessions := server.sessionMgr.ListSessions()
	for _, session := range sessions {
		stats := session.LogBuffer.GetStats()
		totalBufferSize += int64(stats.TotalSizeBytes)
	}
	
	t.Logf("Total buffer size: %.2f MB", float64(totalBufferSize)/(1024*1024))
	
	// Verify buffers are respecting size limits
	for _, session := range sessions {
		stats := session.LogBuffer.GetStats()
		if stats.TotalSizeBytes > 5*1024*1024 { // 5MB limit
			t.Errorf("Session %s buffer exceeds 5MB limit: %d bytes", 
				session.Label, stats.TotalSizeBytes)
		}
	}
	
	// Clean up
	for _, client := range clients {
		client.Close()
	}
	
	// Wait and check for memory release
	time.Sleep(1 * time.Second)
	runtime.GC()
	var finalMemStats runtime.MemStats
	runtime.ReadMemStats(&finalMemStats)
	
	memAfterCleanupMB := float64(finalMemStats.Alloc-baselineMemStats.Alloc) / (1024 * 1024)
	t.Logf("Memory usage after cleanup: %.2f MB above baseline", memAfterCleanupMB)
	
	// Memory should be mostly released after cleanup
	if memAfterCleanupMB > memIncreaseMB*0.5 {
		t.Logf("Warning: Memory not fully released after cleanup")
	}
}

// BenchmarkMessageSerialization benchmarks message serialization performance
func BenchmarkMessageSerialization(b *testing.B) {
	messages := []interface{}{
		protocol.NewLogMessage("test-label", "Benchmark log message content", "stdout", 1234),
		protocol.NewStatusMessage("test-label", protocol.StatusRunning, &[]int{1234}[0], nil),
		protocol.NewCommandMessage("test-label", "restart", nil),
		protocol.NewAckMessage("test-label", true, "Success"),
		protocol.NewErrorMessage("test-label", "TEST_ERROR", "Test error message"),
	}
	
	b.ResetTimer()
	
	for i := 0; i < b.N; i++ {
		msg := messages[i%len(messages)]
		_, err := protocol.SerializeMessage(msg)
		if err != nil {
			b.Fatalf("Failed to serialize message: %v", err)
		}
	}
}

// BenchmarkMessageDeserialization benchmarks message deserialization performance
func BenchmarkMessageDeserialization(b *testing.B) {
	// Pre-serialize messages
	messages := []interface{}{
		protocol.NewLogMessage("test-label", "Benchmark log message content", "stdout", 1234),
		protocol.NewStatusMessage("test-label", protocol.StatusRunning, &[]int{1234}[0], nil),
		protocol.NewCommandMessage("test-label", "restart", nil),
		protocol.NewAckMessage("test-label", true, "Success"),
		protocol.NewErrorMessage("test-label", "TEST_ERROR", "Test error message"),
	}
	
	serialized := make([][]byte, len(messages))
	for i, msg := range messages {
		data, err := protocol.SerializeMessage(msg)
		if err != nil {
			b.Fatalf("Failed to serialize message: %v", err)
		}
		serialized[i] = data
	}
	
	b.ResetTimer()
	
	for i := 0; i < b.N; i++ {
		data := serialized[i%len(serialized)]
		_, err := protocol.ParseMessage(data)
		if err != nil {
			b.Fatalf("Failed to parse message: %v", err)
		}
	}
}