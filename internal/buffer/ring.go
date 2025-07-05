package buffer

import (
	"context"
	"fmt"
	"regexp"
	"sync"
	"time"

	"github.com/bebsworthy/logmcp/internal/protocol"
)

const (
	// MaxBufferSize is the maximum size of the ring buffer in bytes (5MB)
	MaxBufferSize = 5 * 1024 * 1024

	// MaxBufferAge is the maximum age of log entries in the buffer (5 minutes)
	MaxBufferAge = 5 * time.Minute

	// MaxLineSize is the maximum size of a single log line (64KB)
	MaxLineSize = 64 * 1024

	// CleanupInterval is how often the background cleanup runs (30 seconds)
	CleanupInterval = 30 * time.Second
)

// LogEntry represents a log entry in the ring buffer
type LogEntry struct {
	Label     string
	Content   string
	Timestamp time.Time
	Stream    protocol.StreamType
	PID       int
}

// NewLogEntry creates a new LogEntry from a LogMessage
func NewLogEntry(msg *protocol.LogMessage) *LogEntry {
	content := msg.Content
	// Truncate content if it exceeds MaxLineSize
	if len(content) > MaxLineSize {
		content = content[:MaxLineSize]
	}

	return &LogEntry{
		Label:     msg.Label,
		Content:   content,
		Timestamp: msg.Timestamp,
		Stream:    msg.Stream,
		PID:       msg.PID,
	}
}

// Size returns the approximate size of the log entry in bytes
func (e *LogEntry) Size() int {
	return len(e.Label) + len(e.Content) + len(string(e.Stream)) + 8 + 4 // +8 for timestamp, +4 for PID
}

// Matches checks if the log entry matches the given filters
func (e *LogEntry) Matches(stream protocol.StreamType, pattern *regexp.Regexp) bool {
	// Check stream filter
	if stream != "" && stream != "both" && e.Stream != stream {
		return false
	}

	// Check pattern filter
	if pattern != nil && !pattern.MatchString(e.Content) {
		return false
	}

	return true
}

// RingBuffer is a thread-safe ring buffer for log entries with size and time limits
type RingBuffer struct {
	mutex     sync.RWMutex
	entries   []*LogEntry
	head      int // Points to the next position to write
	tail      int // Points to the oldest entry
	size      int // Number of entries in the buffer
	capacity  int // Maximum number of entries
	totalSize int // Total size in bytes
	ctx       context.Context
	cancel    context.CancelFunc
	cleanupWg sync.WaitGroup
}

// NewRingBuffer creates a new ring buffer with the specified capacity
func NewRingBuffer(capacity int) *RingBuffer {
	ctx, cancel := context.WithCancel(context.Background())

	rb := &RingBuffer{
		entries:  make([]*LogEntry, capacity),
		head:     0,
		tail:     0,
		size:     0,
		capacity: capacity,
		ctx:      ctx,
		cancel:   cancel,
	}

	// Start background cleanup goroutine
	rb.cleanupWg.Add(1)
	go rb.backgroundCleanup()

	return rb
}

// NewDefaultRingBuffer creates a new ring buffer with default capacity
func NewDefaultRingBuffer() *RingBuffer {
	// Default capacity of 10000 entries should be sufficient for most use cases
	return NewRingBuffer(10000)
}

// Add adds a new log entry to the ring buffer
func (rb *RingBuffer) Add(entry *LogEntry) {
	rb.mutex.Lock()
	defer rb.mutex.Unlock()

	rb.addUnsafe(entry)

	// Check if we need to evict entries due to size limit
	rb.evictBySizeUnsafe()
}

// AddFromMessage adds a log entry from a LogMessage
func (rb *RingBuffer) AddFromMessage(msg *protocol.LogMessage) {
	entry := NewLogEntry(msg)
	rb.Add(entry)
}

// addUnsafe adds an entry without locking (internal use)
func (rb *RingBuffer) addUnsafe(entry *LogEntry) {
	entrySize := entry.Size()

	// Add the entry
	rb.entries[rb.head] = entry
	rb.head = (rb.head + 1) % rb.capacity
	rb.totalSize += entrySize

	// If buffer is full, advance tail
	if rb.size == rb.capacity {
		// Remove the old entry at tail
		oldEntry := rb.entries[rb.tail]
		if oldEntry != nil {
			rb.totalSize -= oldEntry.Size()
		}
		rb.tail = (rb.tail + 1) % rb.capacity
	} else {
		rb.size++
	}
}

// evictBySizeUnsafe evicts entries if the buffer exceeds the size limit
func (rb *RingBuffer) evictBySizeUnsafe() {
	for rb.totalSize > MaxBufferSize && rb.size > 0 {
		// Remove the oldest entry
		oldEntry := rb.entries[rb.tail]
		if oldEntry != nil {
			rb.totalSize -= oldEntry.Size()
		}
		rb.entries[rb.tail] = nil
		rb.tail = (rb.tail + 1) % rb.capacity
		rb.size--
	}
}

// evictByTimeUnsafe evicts entries that are older than MaxBufferAge
func (rb *RingBuffer) evictByTimeUnsafe() {
	now := time.Now()
	cutoff := now.Add(-MaxBufferAge)

	for rb.size > 0 {
		entry := rb.entries[rb.tail]
		if entry == nil || entry.Timestamp.After(cutoff) {
			break
		}

		// Remove the old entry
		rb.totalSize -= entry.Size()
		rb.entries[rb.tail] = nil
		rb.tail = (rb.tail + 1) % rb.capacity
		rb.size--
	}
}

// backgroundCleanup runs time-based eviction in the background
func (rb *RingBuffer) backgroundCleanup() {
	defer rb.cleanupWg.Done()

	ticker := time.NewTicker(CleanupInterval)
	defer ticker.Stop()

	for {
		select {
		case <-rb.ctx.Done():
			return
		case <-ticker.C:
			rb.mutex.Lock()
			rb.evictByTimeUnsafe()
			rb.mutex.Unlock()
		}
	}
}

// Get retrieves log entries with optional filters
func (rb *RingBuffer) Get(opts GetOptions) []*LogEntry {
	rb.mutex.RLock()
	defer rb.mutex.RUnlock()

	var result []*LogEntry

	// If no entries, return empty slice
	if rb.size == 0 {
		return result
	}

	// Compile regex pattern if provided
	var pattern *regexp.Regexp
	if opts.Pattern != "" {
		var err error
		pattern, err = regexp.Compile(opts.Pattern)
		if err != nil {
			// If pattern is invalid, return empty result
			return result
		}
	}

	// Convert stream filter
	streamFilter := protocol.StreamType("")
	if opts.Stream != "" {
		streamFilter = protocol.StreamType(opts.Stream)
	}

	// Collect entries
	entries := rb.getAllEntriesUnsafe()

	// Apply time filter
	if !opts.Since.IsZero() {
		var filteredEntries []*LogEntry
		for _, entry := range entries {
			if entry.Timestamp.After(opts.Since) || entry.Timestamp.Equal(opts.Since) {
				filteredEntries = append(filteredEntries, entry)
			}
		}
		entries = filteredEntries
	}

	// Apply stream and pattern filters
	for _, entry := range entries {
		if entry.Matches(streamFilter, pattern) {
			result = append(result, entry)
		}
	}

	// Apply line limit (take the last N entries)
	if opts.Lines > 0 && len(result) > opts.Lines {
		result = result[len(result)-opts.Lines:]
	}

	// Apply max results limit
	if opts.MaxResults > 0 && len(result) > opts.MaxResults {
		result = result[len(result)-opts.MaxResults:]
	}

	return result
}

// getAllEntriesUnsafe returns all entries in chronological order without locking
func (rb *RingBuffer) getAllEntriesUnsafe() []*LogEntry {
	if rb.size == 0 {
		return nil
	}

	result := make([]*LogEntry, 0, rb.size)

	// Start from tail (oldest) and go to head (newest)
	for i := 0; i < rb.size; i++ {
		idx := (rb.tail + i) % rb.capacity
		if rb.entries[idx] != nil {
			result = append(result, rb.entries[idx])
		}
	}

	return result
}

// GetByTimeRange retrieves log entries within a specific time range
func (rb *RingBuffer) GetByTimeRange(start, end time.Time) []*LogEntry {
	rb.mutex.RLock()
	defer rb.mutex.RUnlock()

	var result []*LogEntry

	if rb.size == 0 {
		return result
	}

	entries := rb.getAllEntriesUnsafe()

	for _, entry := range entries {
		if (entry.Timestamp.After(start) || entry.Timestamp.Equal(start)) &&
			(entry.Timestamp.Before(end) || entry.Timestamp.Equal(end)) {
			result = append(result, entry)
		}
	}

	return result
}

// Search searches for log entries matching a regex pattern
func (rb *RingBuffer) Search(pattern string) ([]*LogEntry, error) {
	regex, err := regexp.Compile(pattern)
	if err != nil {
		return nil, fmt.Errorf("invalid regex pattern: %w", err)
	}

	rb.mutex.RLock()
	defer rb.mutex.RUnlock()

	var result []*LogEntry

	if rb.size == 0 {
		return result, nil
	}

	entries := rb.getAllEntriesUnsafe()

	for _, entry := range entries {
		if regex.MatchString(entry.Content) {
			result = append(result, entry)
		}
	}

	return result, nil
}

// GetStats returns statistics about the ring buffer
func (rb *RingBuffer) GetStats() Stats {
	rb.mutex.RLock()
	defer rb.mutex.RUnlock()

	var oldestTimestamp, newestTimestamp *time.Time

	if rb.size > 0 {
		entries := rb.getAllEntriesUnsafe()
		if len(entries) > 0 {
			oldestTimestamp = &entries[0].Timestamp
			newestTimestamp = &entries[len(entries)-1].Timestamp
		}
	}

	return Stats{
		EntryCount:       rb.size,
		TotalSizeBytes:   rb.totalSize,
		Capacity:         rb.capacity,
		OldestTimestamp:  oldestTimestamp,
		NewestTimestamp:  newestTimestamp,
		MemoryUsageBytes: rb.totalSize,
	}
}

// Clear removes all entries from the ring buffer
func (rb *RingBuffer) Clear() {
	rb.mutex.Lock()
	defer rb.mutex.Unlock()

	for i := 0; i < rb.capacity; i++ {
		rb.entries[i] = nil
	}

	rb.head = 0
	rb.tail = 0
	rb.size = 0
	rb.totalSize = 0
}

// Close stops the background cleanup goroutine and cleans up resources
func (rb *RingBuffer) Close() {
	rb.cancel()
	rb.cleanupWg.Wait()
}

// GetOptions represents options for retrieving log entries
type GetOptions struct {
	Lines      int       // Maximum number of lines to return (0 = no limit)
	Since      time.Time // Only return entries after this timestamp
	Stream     string    // Filter by stream type ("stdout", "stderr", "both", or "")
	Pattern    string    // Regex pattern to match against log content
	MaxResults int       // Maximum number of results to return (0 = no limit)
}

// Stats represents statistics about the ring buffer
type Stats struct {
	EntryCount       int        // Number of entries in the buffer
	TotalSizeBytes   int        // Total size of all entries in bytes
	Capacity         int        // Maximum number of entries the buffer can hold
	OldestTimestamp  *time.Time // Timestamp of the oldest entry (nil if empty)
	NewestTimestamp  *time.Time // Timestamp of the newest entry (nil if empty)
	MemoryUsageBytes int        // Current memory usage in bytes
}

// String returns a human-readable string representation of the stats
func (s Stats) String() string {
	if s.EntryCount == 0 {
		return "Ring buffer is empty"
	}

	return fmt.Sprintf("Ring buffer: %d/%d entries, %d bytes, oldest: %v, newest: %v",
		s.EntryCount, s.Capacity, s.TotalSizeBytes,
		s.OldestTimestamp, s.NewestTimestamp)
}
