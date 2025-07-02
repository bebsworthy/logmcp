package buffer_test

import (
	"fmt"
	"time"

	"github.com/logmcp/logmcp/internal/buffer"
	"github.com/logmcp/logmcp/internal/protocol"
)

// ExampleRingBuffer demonstrates basic usage of the ring buffer
func ExampleRingBuffer() {
	// Create a new ring buffer with capacity for 100 entries
	rb := buffer.NewRingBuffer(100)
	defer rb.Close()

	// Add some log entries
	now := time.Now()
	
	// Add entries directly
	entry1 := &buffer.LogEntry{
		Label:     "web-server",
		Content:   "Server started on port 8080",
		Timestamp: now,
		Stream:    protocol.StreamStdout,
		PID:       1234,
	}
	rb.Add(entry1)

	// Add entries from protocol messages
	msg := &protocol.LogMessage{
		BaseMessage: protocol.BaseMessage{
			Type:      protocol.MessageTypeLog,
			SessionID: "session-1",
		},
		Label:     "web-server",
		Content:   "Received GET request for /api/users",
		Timestamp: now.Add(1 * time.Second),
		Stream:    protocol.StreamStdout,
		PID:       1234,
	}
	rb.AddFromMessage(msg)

	// Get recent log entries
	entries := rb.Get(buffer.GetOptions{Lines: 10})
	for _, entry := range entries {
		fmt.Printf("[%s] %s: %s\n", entry.Stream, entry.Label, entry.Content)
	}

	// Search for specific patterns
	errorEntries, err := rb.Search("ERROR|error")
	if err != nil {
		fmt.Printf("Search failed: %v\n", err)
	} else {
		fmt.Printf("Found %d error entries\n", len(errorEntries))
	}

	// Get buffer statistics
	stats := rb.GetStats()
	fmt.Printf("Buffer contains %d entries\n", stats.EntryCount)

	// Output:
	// [stdout] web-server: Server started on port 8080
	// [stdout] web-server: Received GET request for /api/users
	// Found 0 error entries
	// Buffer contains 2 entries
}

// ExampleRingBuffer_filtering demonstrates advanced filtering capabilities
func ExampleRingBuffer_filtering() {
	rb := buffer.NewRingBuffer(100)
	defer rb.Close()

	now := time.Now()

	// Add mixed log entries
	entries := []*buffer.LogEntry{
		{Label: "app", Content: "INFO: Application started", Stream: protocol.StreamStdout, PID: 1, Timestamp: now.Add(-5 * time.Minute)},
		{Label: "app", Content: "ERROR: Database connection failed", Stream: protocol.StreamStderr, PID: 1, Timestamp: now.Add(-4 * time.Minute)},
		{Label: "app", Content: "INFO: Retrying database connection", Stream: protocol.StreamStdout, PID: 1, Timestamp: now.Add(-3 * time.Minute)},
		{Label: "app", Content: "ERROR: Authentication failed", Stream: protocol.StreamStderr, PID: 1, Timestamp: now.Add(-2 * time.Minute)},
		{Label: "app", Content: "INFO: User logged in successfully", Stream: protocol.StreamStdout, PID: 1, Timestamp: now.Add(-1 * time.Minute)},
	}

	for _, entry := range entries {
		rb.Add(entry)
	}

	// Get only error messages
	errorLogs := rb.Get(buffer.GetOptions{
		Stream:  string(protocol.StreamStderr),
		Pattern: "ERROR",
	})
	fmt.Printf("Error logs: %d entries\n", len(errorLogs))

	// Get logs from the last 3 minutes
	recentLogs := rb.Get(buffer.GetOptions{
		Since: now.Add(-3*time.Minute - 30*time.Second),
	})
	fmt.Printf("Recent logs: %d entries\n", len(recentLogs))

	// Get logs in a specific time range
	timeRangeLogs := rb.GetByTimeRange(
		now.Add(-4*time.Minute - 30*time.Second),
		now.Add(-2*time.Minute + 30*time.Second),
	)
	fmt.Printf("Time range logs: %d entries\n", len(timeRangeLogs))

	// Output:
	// Error logs: 2 entries
	// Recent logs: 3 entries
	// Time range logs: 3 entries
}

// ExampleRingBuffer_sizeManagement demonstrates size and time-based eviction
func ExampleRingBuffer_sizeManagement() {
	// Create a small buffer to demonstrate eviction
	rb := buffer.NewRingBuffer(3)
	defer rb.Close()

	// Add entries that will cause overflow
	for i := 1; i <= 5; i++ {
		entry := &buffer.LogEntry{
			Label:     "test",
			Content:   fmt.Sprintf("Message %d", i),
			Stream:    protocol.StreamStdout,
			PID:       i,
			Timestamp: time.Now(),
		}
		rb.Add(entry)
	}

	// Only the last 3 entries should remain
	entries := rb.Get(buffer.GetOptions{})
	fmt.Printf("Buffer contains %d entries after overflow\n", len(entries))
	if len(entries) > 0 {
		fmt.Printf("First entry: %s\n", entries[0].Content)
		fmt.Printf("Last entry: %s\n", entries[len(entries)-1].Content)
	}

	// Check buffer statistics
	stats := rb.GetStats()
	fmt.Printf("Buffer has %d/%d entries\n", stats.EntryCount, stats.Capacity)

	// Output:
	// Buffer contains 3 entries after overflow
	// First entry: Message 3
	// Last entry: Message 5
	// Buffer has 3/3 entries
}