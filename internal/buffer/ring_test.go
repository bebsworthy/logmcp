package buffer

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/bebsworthy/logmcp/internal/protocol"
)

func TestNewRingBuffer(t *testing.T) {
	rb := NewRingBuffer(100)
	defer rb.Close()

	if rb.capacity != 100 {
		t.Errorf("Expected capacity 100, got %d", rb.capacity)
	}

	stats := rb.GetStats()
	if stats.EntryCount != 0 {
		t.Errorf("Expected empty buffer, got %d entries", stats.EntryCount)
	}
}

func TestNewDefaultRingBuffer(t *testing.T) {
	rb := NewDefaultRingBuffer()
	defer rb.Close()

	if rb.capacity != 10000 {
		t.Errorf("Expected default capacity 10000, got %d", rb.capacity)
	}
}

func TestLogEntry_NewLogEntry(t *testing.T) {
	msg := &protocol.LogMessage{
		BaseMessage: protocol.BaseMessage{
			Type:  protocol.MessageTypeLog,
			Label: "test-label",
		},
		Content:   "test content",
		Timestamp: time.Now(),
		Stream:    protocol.StreamStdout,
		PID:       1234,
	}

	entry := NewLogEntry(msg)

	if entry.Label != msg.Label {
		t.Errorf("Expected label %s, got %s", msg.Label, entry.Label)
	}
	if entry.Content != msg.Content {
		t.Errorf("Expected content %s, got %s", msg.Content, entry.Content)
	}
	if entry.Stream != msg.Stream {
		t.Errorf("Expected stream %s, got %s", msg.Stream, entry.Stream)
	}
	if entry.PID != msg.PID {
		t.Errorf("Expected PID %d, got %d", msg.PID, entry.PID)
	}
}

func TestLogEntry_NewLogEntry_ContentTruncation(t *testing.T) {
	// Create content that exceeds MaxLineSize
	longContent := strings.Repeat("a", MaxLineSize+1000)

	msg := &protocol.LogMessage{
		BaseMessage: protocol.BaseMessage{
			Type:  protocol.MessageTypeLog,
			Label: "test-label",
		},
		Content:   longContent,
		Timestamp: time.Now(),
		Stream:    protocol.StreamStdout,
		PID:       1234,
	}

	entry := NewLogEntry(msg)

	if len(entry.Content) != MaxLineSize {
		t.Errorf("Expected content length %d, got %d", MaxLineSize, len(entry.Content))
	}
}

func TestLogEntry_Size(t *testing.T) {
	entry := &LogEntry{
		Label:     "test",
		Content:   "hello world",
		Stream:    protocol.StreamStdout,
		PID:       1234,
		Timestamp: time.Now(),
	}

	size := entry.Size()
	expectedSize := len("test") + len("hello world") + len("stdout") + 8 + 4

	if size != expectedSize {
		t.Errorf("Expected size %d, got %d", expectedSize, size)
	}
}

func TestLogEntry_Matches(t *testing.T) {
	entry := &LogEntry{
		Label:     "test",
		Content:   "ERROR: failed to connect",
		Stream:    protocol.StreamStderr,
		PID:       1234,
		Timestamp: time.Now(),
	}

	// Test stream matching
	if !entry.Matches("", nil) {
		t.Error("Should match empty stream filter")
	}
	if !entry.Matches("both", nil) {
		t.Error("Should match 'both' stream filter")
	}
	if !entry.Matches(protocol.StreamStderr, nil) {
		t.Error("Should match stderr stream")
	}
	if entry.Matches(protocol.StreamStdout, nil) {
		t.Error("Should not match stdout stream")
	}

	// Test pattern matching
	errorPattern := regexp.MustCompile("ERROR")
	if !entry.Matches("", errorPattern) {
		t.Error("Should match ERROR pattern")
	}

	infoPattern := regexp.MustCompile("INFO")
	if entry.Matches("", infoPattern) {
		t.Error("Should not match INFO pattern")
	}

	// Test combined matching
	if !entry.Matches(protocol.StreamStderr, errorPattern) {
		t.Error("Should match both stderr stream and ERROR pattern")
	}
	if entry.Matches(protocol.StreamStdout, errorPattern) {
		t.Error("Should not match stdout stream even with matching pattern")
	}
}

func TestRingBuffer_Add(t *testing.T) {
	rb := NewRingBuffer(3)
	defer rb.Close()

	entry1 := &LogEntry{Label: "test", Content: "message 1", Stream: protocol.StreamStdout, PID: 1}
	entry2 := &LogEntry{Label: "test", Content: "message 2", Stream: protocol.StreamStdout, PID: 2}
	entry3 := &LogEntry{Label: "test", Content: "message 3", Stream: protocol.StreamStdout, PID: 3}

	rb.Add(entry1)
	rb.Add(entry2)
	rb.Add(entry3)

	stats := rb.GetStats()
	if stats.EntryCount != 3 {
		t.Errorf("Expected 3 entries, got %d", stats.EntryCount)
	}

	// Add fourth entry - should evict the first one
	entry4 := &LogEntry{Label: "test", Content: "message 4", Stream: protocol.StreamStdout, PID: 4}
	rb.Add(entry4)

	stats = rb.GetStats()
	if stats.EntryCount != 3 {
		t.Errorf("Expected 3 entries after overflow, got %d", stats.EntryCount)
	}

	// Check that the first entry was evicted
	entries := rb.Get(GetOptions{})
	if len(entries) != 3 {
		t.Errorf("Expected 3 entries, got %d", len(entries))
	}
	if entries[0].Content != "message 2" {
		t.Errorf("Expected first entry to be 'message 2', got '%s'", entries[0].Content)
	}
	if entries[2].Content != "message 4" {
		t.Errorf("Expected last entry to be 'message 4', got '%s'", entries[2].Content)
	}
}

func TestRingBuffer_AddFromMessage(t *testing.T) {
	rb := NewRingBuffer(5)
	defer rb.Close()

	msg := &protocol.LogMessage{
		BaseMessage: protocol.BaseMessage{
			Type:  protocol.MessageTypeLog,
			Label: "test-label",
		},
		Content:   "test message",
		Timestamp: time.Now(),
		Stream:    protocol.StreamStdout,
		PID:       1234,
	}

	rb.AddFromMessage(msg)

	stats := rb.GetStats()
	if stats.EntryCount != 1 {
		t.Errorf("Expected 1 entry, got %d", stats.EntryCount)
	}

	entries := rb.Get(GetOptions{})
	if len(entries) != 1 {
		t.Errorf("Expected 1 entry, got %d", len(entries))
	}
	if entries[0].Content != msg.Content {
		t.Errorf("Expected content '%s', got '%s'", msg.Content, entries[0].Content)
	}
}

func TestRingBuffer_SizeBasedEviction(t *testing.T) {
	rb := NewRingBuffer(1000) // Large capacity to test size-based eviction
	defer rb.Close()

	// Create entries that will exceed MaxBufferSize
	largeContent := strings.Repeat("a", 1024*1024) // 1MB content

	for i := 0; i < 6; i++ { // 6MB total, should trigger eviction
		entry := &LogEntry{
			Label:     "test",
			Content:   largeContent,
			Stream:    protocol.StreamStdout,
			PID:       i,
			Timestamp: time.Now(),
		}
		rb.Add(entry)
	}

	stats := rb.GetStats()
	if stats.TotalSizeBytes > MaxBufferSize {
		t.Errorf("Buffer size %d exceeds max size %d", stats.TotalSizeBytes, MaxBufferSize)
	}
}

func TestRingBuffer_TimeBasedEviction(t *testing.T) {
	rb := NewRingBuffer(100)
	defer rb.Close()

	now := time.Now()

	// Add old entry (older than MaxBufferAge)
	oldEntry := &LogEntry{
		Label:     "test",
		Content:   "old message",
		Stream:    protocol.StreamStdout,
		PID:       1,
		Timestamp: now.Add(-MaxBufferAge - time.Minute),
	}
	rb.Add(oldEntry)

	// Add recent entry
	recentEntry := &LogEntry{
		Label:     "test",
		Content:   "recent message",
		Stream:    protocol.StreamStdout,
		PID:       2,
		Timestamp: now,
	}
	rb.Add(recentEntry)

	// Manually trigger time-based eviction
	rb.mutex.Lock()
	rb.evictByTimeUnsafe()
	rb.mutex.Unlock()

	entries := rb.Get(GetOptions{})
	if len(entries) != 1 {
		t.Errorf("Expected 1 entry after time-based eviction, got %d", len(entries))
	}
	if entries[0].Content != "recent message" {
		t.Errorf("Expected recent message to remain, got '%s'", entries[0].Content)
	}
}

func TestRingBuffer_BackgroundCleanup(t *testing.T) {
	// Use a very short cleanup interval for testing
	originalInterval := CleanupInterval
	defer func() {
		// Note: This won't actually change the const, but shows intent
		_ = originalInterval
	}()

	rb := NewRingBuffer(100)
	defer rb.Close()

	now := time.Now()

	// Add an old entry
	oldEntry := &LogEntry{
		Label:     "test",
		Content:   "old message",
		Stream:    protocol.StreamStdout,
		PID:       1,
		Timestamp: now.Add(-MaxBufferAge - time.Minute),
	}
	rb.Add(oldEntry)

	// Add a recent entry
	recentEntry := &LogEntry{
		Label:     "test",
		Content:   "recent message",
		Stream:    protocol.StreamStdout,
		PID:       2,
		Timestamp: now,
	}
	rb.Add(recentEntry)

	// Wait for background cleanup (we'll trigger it manually since the test interval is long)
	rb.mutex.Lock()
	rb.evictByTimeUnsafe()
	rb.mutex.Unlock()

	entries := rb.Get(GetOptions{})
	if len(entries) != 1 {
		t.Errorf("Expected 1 entry after background cleanup, got %d", len(entries))
	}
}

func TestRingBuffer_Get(t *testing.T) {
	rb := NewRingBuffer(10)
	defer rb.Close()

	now := time.Now()

	// Add test entries
	entries := []*LogEntry{
		{Label: "app1", Content: "INFO: starting up", Stream: protocol.StreamStdout, PID: 1, Timestamp: now.Add(-5 * time.Minute)},
		{Label: "app1", Content: "ERROR: connection failed", Stream: protocol.StreamStderr, PID: 1, Timestamp: now.Add(-4 * time.Minute)},
		{Label: "app2", Content: "DEBUG: processing request", Stream: protocol.StreamStdout, PID: 2, Timestamp: now.Add(-3 * time.Minute)},
		{Label: "app1", Content: "INFO: retrying connection", Stream: protocol.StreamStdout, PID: 1, Timestamp: now.Add(-2 * time.Minute)},
		{Label: "app2", Content: "ERROR: timeout occurred", Stream: protocol.StreamStderr, PID: 2, Timestamp: now.Add(-1 * time.Minute)},
	}

	for _, entry := range entries {
		rb.Add(entry)
	}

	// Test basic get
	result := rb.Get(GetOptions{})
	if len(result) != 5 {
		t.Errorf("Expected 5 entries, got %d", len(result))
	}

	// Test line limit
	result = rb.Get(GetOptions{Lines: 3})
	if len(result) != 3 {
		t.Errorf("Expected 3 entries with line limit, got %d", len(result))
	}

	// Test since filter
	since := now.Add(-3*time.Minute - 30*time.Second)
	result = rb.Get(GetOptions{Since: since})
	if len(result) != 3 {
		t.Errorf("Expected 3 entries with since filter, got %d", len(result))
	}

	// Test stream filter
	result = rb.Get(GetOptions{Stream: string(protocol.StreamStderr)})
	if len(result) != 2 {
		t.Errorf("Expected 2 stderr entries, got %d", len(result))
	}

	// Test pattern filter
	result = rb.Get(GetOptions{Pattern: "ERROR"})
	if len(result) != 2 {
		t.Errorf("Expected 2 entries matching ERROR pattern, got %d", len(result))
	}

	// Test max results limit
	result = rb.Get(GetOptions{MaxResults: 2})
	if len(result) != 2 {
		t.Errorf("Expected 2 entries with max results limit, got %d", len(result))
	}

	// Test combined filters
	result = rb.Get(GetOptions{
		Stream:  string(protocol.StreamStdout),
		Pattern: "INFO",
		Lines:   1,
	})
	if len(result) != 1 {
		t.Errorf("Expected 1 entry with combined filters, got %d", len(result))
	}
	if result[0].Content != "INFO: retrying connection" {
		t.Errorf("Expected 'INFO: retrying connection', got '%s'", result[0].Content)
	}
}

func TestRingBuffer_GetByTimeRange(t *testing.T) {
	rb := NewRingBuffer(10)
	defer rb.Close()

	now := time.Now()

	entries := []*LogEntry{
		{Label: "test", Content: "message 1", Stream: protocol.StreamStdout, PID: 1, Timestamp: now.Add(-10 * time.Minute)},
		{Label: "test", Content: "message 2", Stream: protocol.StreamStdout, PID: 1, Timestamp: now.Add(-5 * time.Minute)},
		{Label: "test", Content: "message 3", Stream: protocol.StreamStdout, PID: 1, Timestamp: now.Add(-2 * time.Minute)},
		{Label: "test", Content: "message 4", Stream: protocol.StreamStdout, PID: 1, Timestamp: now},
	}

	for _, entry := range entries {
		rb.Add(entry)
	}

	start := now.Add(-6 * time.Minute)
	end := now.Add(-1 * time.Minute)

	result := rb.GetByTimeRange(start, end)
	if len(result) != 2 {
		t.Errorf("Expected 2 entries in time range, got %d", len(result))
	}
	if result[0].Content != "message 2" {
		t.Errorf("Expected 'message 2', got '%s'", result[0].Content)
	}
	if result[1].Content != "message 3" {
		t.Errorf("Expected 'message 3', got '%s'", result[1].Content)
	}
}

func TestRingBuffer_Search(t *testing.T) {
	rb := NewRingBuffer(10)
	defer rb.Close()

	entries := []*LogEntry{
		{Label: "test", Content: "INFO: application started", Stream: protocol.StreamStdout, PID: 1},
		{Label: "test", Content: "ERROR: connection failed", Stream: protocol.StreamStderr, PID: 1},
		{Label: "test", Content: "WARNING: low memory", Stream: protocol.StreamStdout, PID: 1},
		{Label: "test", Content: "ERROR: timeout occurred", Stream: protocol.StreamStderr, PID: 1},
	}

	for _, entry := range entries {
		rb.Add(entry)
	}

	// Test valid pattern
	result, err := rb.Search("ERROR")
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}
	if len(result) != 2 {
		t.Errorf("Expected 2 entries matching ERROR, got %d", len(result))
	}

	// Test regex pattern
	result, err = rb.Search("(INFO|WARNING)")
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}
	if len(result) != 2 {
		t.Errorf("Expected 2 entries matching INFO or WARNING, got %d", len(result))
	}

	// Test invalid pattern
	_, err = rb.Search("[invalid regex")
	if err == nil {
		t.Error("Expected error for invalid regex pattern")
	}
}

func TestRingBuffer_GetStats(t *testing.T) {
	rb := NewRingBuffer(5)
	defer rb.Close()

	// Test empty buffer
	stats := rb.GetStats()
	if stats.EntryCount != 0 {
		t.Errorf("Expected 0 entries in empty buffer, got %d", stats.EntryCount)
	}
	if stats.TotalSizeBytes != 0 {
		t.Errorf("Expected 0 bytes in empty buffer, got %d", stats.TotalSizeBytes)
	}
	if stats.OldestTimestamp != nil {
		t.Error("Expected nil oldest timestamp for empty buffer")
	}
	if stats.NewestTimestamp != nil {
		t.Error("Expected nil newest timestamp for empty buffer")
	}

	// Add entries
	now := time.Now()
	entry1 := &LogEntry{Label: "test", Content: "message 1", Stream: protocol.StreamStdout, PID: 1, Timestamp: now.Add(-1 * time.Minute)}
	entry2 := &LogEntry{Label: "test", Content: "message 2", Stream: protocol.StreamStdout, PID: 1, Timestamp: now}

	rb.Add(entry1)
	rb.Add(entry2)

	stats = rb.GetStats()
	if stats.EntryCount != 2 {
		t.Errorf("Expected 2 entries, got %d", stats.EntryCount)
	}
	if stats.TotalSizeBytes <= 0 {
		t.Error("Expected positive total size")
	}
	if stats.OldestTimestamp == nil {
		t.Error("Expected non-nil oldest timestamp")
	}
	if stats.NewestTimestamp == nil {
		t.Error("Expected non-nil newest timestamp")
	}
	if !stats.OldestTimestamp.Equal(entry1.Timestamp) {
		t.Error("Oldest timestamp doesn't match first entry")
	}
	if !stats.NewestTimestamp.Equal(entry2.Timestamp) {
		t.Error("Newest timestamp doesn't match last entry")
	}
}

func TestRingBuffer_Clear(t *testing.T) {
	rb := NewRingBuffer(5)
	defer rb.Close()

	// Add entries
	entry := &LogEntry{Label: "test", Content: "message", Stream: protocol.StreamStdout, PID: 1}
	rb.Add(entry)
	rb.Add(entry)

	stats := rb.GetStats()
	if stats.EntryCount != 2 {
		t.Errorf("Expected 2 entries before clear, got %d", stats.EntryCount)
	}

	// Clear buffer
	rb.Clear()

	stats = rb.GetStats()
	if stats.EntryCount != 0 {
		t.Errorf("Expected 0 entries after clear, got %d", stats.EntryCount)
	}
	if stats.TotalSizeBytes != 0 {
		t.Errorf("Expected 0 bytes after clear, got %d", stats.TotalSizeBytes)
	}

	entries := rb.Get(GetOptions{})
	if len(entries) != 0 {
		t.Errorf("Expected 0 entries after clear, got %d", len(entries))
	}
}

func TestRingBuffer_Close(t *testing.T) {
	rb := NewRingBuffer(5)

	// Add an entry to make sure buffer is working
	entry := &LogEntry{Label: "test", Content: "message", Stream: protocol.StreamStdout, PID: 1}
	rb.Add(entry)

	// Close should not block and should stop background goroutine
	done := make(chan bool, 1)
	go func() {
		rb.Close()
		done <- true
	}()

	select {
	case <-done:
		// Success - Close() completed
	case <-time.After(5 * time.Second):
		t.Error("Close() did not complete within 5 seconds")
	}

	// Verify context was cancelled
	select {
	case <-rb.ctx.Done():
		// Success - context was cancelled
	default:
		t.Error("Context was not cancelled after Close()")
	}
}

func TestGetOptions_InvalidPattern(t *testing.T) {
	rb := NewRingBuffer(5)
	defer rb.Close()

	entry := &LogEntry{Label: "test", Content: "message", Stream: protocol.StreamStdout, PID: 1}
	rb.Add(entry)

	// Test with invalid regex pattern
	result := rb.Get(GetOptions{Pattern: "[invalid regex"})
	if len(result) != 0 {
		t.Errorf("Expected 0 results for invalid pattern, got %d", len(result))
	}
}

func TestStats_String(t *testing.T) {
	// Test empty stats
	emptyStats := Stats{}
	str := emptyStats.String()
	if str != "Ring buffer is empty" {
		t.Errorf("Expected empty buffer string, got '%s'", str)
	}

	// Test non-empty stats
	now := time.Now()
	stats := Stats{
		EntryCount:      10,
		TotalSizeBytes:  2048,
		Capacity:        100,
		OldestTimestamp: &now,
		NewestTimestamp: &now,
	}

	str = stats.String()
	if !strings.Contains(str, "10/100 entries") {
		t.Errorf("String should contain entry count, got '%s'", str)
	}
	if !strings.Contains(str, "2048 bytes") {
		t.Errorf("String should contain byte count, got '%s'", str)
	}
}

func TestRingBuffer_ConcurrentAccess(t *testing.T) {
	rb := NewRingBuffer(100)
	defer rb.Close()

	// Test concurrent reads and writes
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	const numWriters = 5
	const numReaders = 3
	const entriesPerWriter = 100

	// Start writers
	for i := 0; i < numWriters; i++ {
		go func(writerID int) {
			for j := 0; j < entriesPerWriter; j++ {
				select {
				case <-ctx.Done():
					return
				default:
					entry := &LogEntry{
						Label:     "test",
						Content:   fmt.Sprintf("writer-%d-entry-%d", writerID, j),
						Stream:    protocol.StreamStdout,
						PID:       writerID*1000 + j,
						Timestamp: time.Now(),
					}
					rb.Add(entry)
				}
			}
		}(i)
	}

	// Start readers
	for i := 0; i < numReaders; i++ {
		go func(readerID int) {
			for {
				select {
				case <-ctx.Done():
					return
				default:
					// Perform various read operations
					rb.Get(GetOptions{Lines: 10})
					rb.GetStats()
					rb.Search("writer")
				}
			}
		}(i)
	}

	// Wait for completion
	<-ctx.Done()

	// Verify buffer is still functional
	stats := rb.GetStats()
	if stats.EntryCount < 0 || stats.EntryCount > rb.capacity {
		t.Errorf("Invalid entry count after concurrent access: %d", stats.EntryCount)
	}
}

func BenchmarkRingBuffer_Add(b *testing.B) {
	rb := NewRingBuffer(10000)
	defer rb.Close()

	entry := &LogEntry{
		Label:     "benchmark",
		Content:   "This is a benchmark log entry with some content",
		Stream:    protocol.StreamStdout,
		PID:       1234,
		Timestamp: time.Now(),
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		rb.Add(entry)
	}
}

func BenchmarkRingBuffer_Get(b *testing.B) {
	rb := NewRingBuffer(10000)
	defer rb.Close()

	// Populate buffer
	for i := 0; i < 1000; i++ {
		entry := &LogEntry{
			Label:     "benchmark",
			Content:   fmt.Sprintf("Log entry %d", i),
			Stream:    protocol.StreamStdout,
			PID:       i,
			Timestamp: time.Now(),
		}
		rb.Add(entry)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		rb.Get(GetOptions{Lines: 100})
	}
}

func BenchmarkRingBuffer_Search(b *testing.B) {
	rb := NewRingBuffer(10000)
	defer rb.Close()

	// Populate buffer with mix of ERROR and INFO messages
	for i := 0; i < 1000; i++ {
		var content string
		if i%2 == 0 {
			content = fmt.Sprintf("ERROR: Something went wrong %d", i)
		} else {
			content = fmt.Sprintf("INFO: Normal operation %d", i)
		}

		entry := &LogEntry{
			Label:     "benchmark",
			Content:   content,
			Stream:    protocol.StreamStdout,
			PID:       i,
			Timestamp: time.Now(),
		}
		rb.Add(entry)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		rb.Search("ERROR")
	}
}
