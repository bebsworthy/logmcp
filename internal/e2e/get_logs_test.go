package e2e

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

// TestGetLogs_BasicSingleSession tests basic log retrieval from a single session
func TestGetLogs_BasicSingleSession(t *testing.T) {
	t.Parallel()

	ts := SetupTest(t)

	// Start a simple process
	err := ts.StartTestProcess("basic-logs", "go", "run", ts.TestAppPath("simple_app.go"), "500ms")
	if err != nil {
		t.Fatalf("Failed to start process: %v", err)
	}

	// Wait for process to generate some logs
	if !ts.WaitForLogContent("basic-logs", "Simple app started", 5*time.Second) {
		t.Fatal("Failed to capture startup logs")
	}

	// Wait a bit more for additional logs
	time.Sleep(2 * time.Second)

	// Get logs from the session
	logs, err := ts.GetLogs([]string{"basic-logs"})
	if err != nil {
		t.Fatalf("Failed to get logs: %v", err)
	}

	// Verify we got logs
	if len(logs) == 0 {
		t.Fatal("Expected logs but got none")
	}

	// Check log structure and content
	foundStartup := false
	foundStdout := false
	foundStderr := false

	for _, log := range logs {
		// Verify basic log structure
		if log.Label != "basic-logs" {
			t.Errorf("Expected label 'basic-logs', got '%s'", log.Label)
		}
		if log.Timestamp == "" {
			t.Error("Log entry missing timestamp")
		}
		if log.Stream != "stdout" && log.Stream != "stderr" {
			t.Errorf("Invalid stream type: %s", log.Stream)
		}

		// Check for expected content
		if strings.Contains(log.Content, "Simple app started") {
			foundStartup = true
		}
		if strings.Contains(log.Content, "Hello from simple app stdout") {
			foundStdout = true
		}
		if strings.Contains(log.Content, "Hello from simple app stderr") {
			foundStderr = true
		}
	}

	if !foundStartup {
		t.Error("Did not find startup log message")
	}
	if !foundStdout {
		t.Error("Did not find stdout message")
	}
	if !foundStderr {
		t.Error("Did not find stderr message")
	}
}

// TestGetLogs_MultiSessionAggregation tests log aggregation from multiple sessions
func TestGetLogs_MultiSessionAggregation(t *testing.T) {
	t.Parallel()

	ts := SetupTest(t)

	// Start multiple processes
	sessions := []string{"multi-1", "multi-2", "multi-3"}
	for _, label := range sessions {
		err := ts.StartTestProcess(label, "go", "run", ts.TestAppPath("simple_app.go"), "1s")
		if err != nil {
			t.Fatalf("Failed to start process %s: %v", label, err)
		}
	}

	// Wait for all processes to generate logs
	for _, label := range sessions {
		if !ts.WaitForLogContent(label, "Simple app started", 5*time.Second) {
			t.Fatalf("Failed to capture startup logs for %s", label)
		}
	}

	// Wait for more logs
	time.Sleep(3 * time.Second)

	// Get logs from all sessions
	logs, err := ts.GetLogs(sessions)
	if err != nil {
		t.Fatalf("Failed to get logs: %v", err)
	}

	// Verify we got logs from all sessions
	sessionCounts := make(map[string]int)
	for _, log := range logs {
		sessionCounts[log.Label]++
	}

	for _, label := range sessions {
		if count, ok := sessionCounts[label]; !ok || count == 0 {
			t.Errorf("No logs found for session %s", label)
		} else {
			t.Logf("Session %s: %d log entries", label, count)
		}
	}

	// Verify logs are chronologically ordered
	var lastTime time.Time
	for i, log := range logs {
		logTime, err := time.Parse(time.RFC3339, log.Timestamp)
		if err != nil {
			t.Errorf("Failed to parse timestamp for log %d: %v", i, err)
			continue
		}
		if i > 0 && logTime.Before(lastTime) {
			t.Errorf("Logs not in chronological order at index %d", i)
		}
		lastTime = logTime
	}
}

// TestGetLogs_PatternFiltering tests regex pattern filtering
func TestGetLogs_PatternFiltering(t *testing.T) {
	t.Parallel()

	ts := SetupTest(t)

	// Start a process
	err := ts.StartTestProcess("pattern-test", "go", "run", ts.TestAppPath("simple_app.go"), "500ms")
	if err != nil {
		t.Fatalf("Failed to start process: %v", err)
	}

	// Wait for logs
	time.Sleep(3 * time.Second)

	// Test various patterns
	testCases := []struct {
		name          string
		pattern       string
		shouldMatch   []string
		shouldNotMatch []string
	}{
		{
			name:          "Match tick messages",
			pattern:       "Tick [0-9]+",
			shouldMatch:   []string{"Tick"},
			shouldNotMatch: []string{"Hello", "Started"},
		},
		{
			name:          "Match stdout messages",
			pattern:       "Stdout message",
			shouldMatch:   []string{"Stdout message"},
			shouldNotMatch: []string{"Stderr message", "Tick"},
		},
		{
			name:          "Match stderr messages",
			pattern:       "Stderr message",
			shouldMatch:   []string{"Stderr message"},
			shouldNotMatch: []string{"Stdout message", "Tick"},
		},
		{
			name:          "Match all messages",
			pattern:       "message [0-9]+",
			shouldMatch:   []string{"message"},
			shouldNotMatch: []string{"Tick", "Started"},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			logs, err := ts.GetLogs([]string{"pattern-test"}, WithPattern(tc.pattern))
			if err != nil {
				t.Fatalf("Failed to get logs with pattern '%s': %v", tc.pattern, err)
			}

			// All logs should match the pattern
			for _, log := range logs {
				matched := false
				for _, shouldMatch := range tc.shouldMatch {
					if strings.Contains(log.Content, shouldMatch) {
						matched = true
						break
					}
				}
				if !matched {
					t.Errorf("Log doesn't match expected pattern: %s", log.Content)
				}

				// Check that logs don't contain what they shouldn't
				for _, shouldNotMatch := range tc.shouldNotMatch {
					if strings.Contains(log.Content, shouldNotMatch) {
						t.Errorf("Log contains unexpected content '%s': %s", shouldNotMatch, log.Content)
					}
				}
			}
			
			if len(logs) == 0 && len(tc.shouldMatch) > 0 {
				t.Errorf("No logs matched pattern '%s'", tc.pattern)
			}
		})
	}
}

// TestGetLogs_StreamFiltering tests filtering by stream type
func TestGetLogs_StreamFiltering(t *testing.T) {
	t.Parallel()

	ts := SetupTest(t)

	// Start a process
	err := ts.StartTestProcess("stream-test", "go", "run", ts.TestAppPath("simple_app.go"), "500ms")
	if err != nil {
		t.Fatalf("Failed to start process: %v", err)
	}

	// Wait for logs from both streams
	time.Sleep(3 * time.Second)

	// Test stdout only
	t.Run("StdoutOnly", func(t *testing.T) {
		logs, err := ts.GetLogs([]string{"stream-test"}, WithStream("stdout"))
		if err != nil {
			t.Fatalf("Failed to get stdout logs: %v", err)
		}

		for _, log := range logs {
			if log.Stream != "stdout" {
				t.Errorf("Expected only stdout logs, got stream: %s", log.Stream)
			}
		}

		// Should have stdout messages
		hasStdoutContent := false
		for _, log := range logs {
			if strings.Contains(log.Content, "stdout") || strings.Contains(log.Content, "Tick") {
				hasStdoutContent = true
				break
			}
		}
		if !hasStdoutContent {
			t.Error("No stdout content found")
		}
	})

	// Test stderr only
	t.Run("StderrOnly", func(t *testing.T) {
		logs, err := ts.GetLogs([]string{"stream-test"}, WithStream("stderr"))
		if err != nil {
			t.Fatalf("Failed to get stderr logs: %v", err)
		}

		for _, log := range logs {
			if log.Stream != "stderr" {
				t.Errorf("Expected only stderr logs, got stream: %s", log.Stream)
			}
		}

		// Should have stderr messages
		hasStderrContent := false
		for _, log := range logs {
			if strings.Contains(log.Content, "stderr") {
				hasStderrContent = true
				break
			}
		}
		if !hasStderrContent {
			t.Error("No stderr content found")
		}
	})

	// Test both streams (default)
	t.Run("BothStreams", func(t *testing.T) {
		logs, err := ts.GetLogs([]string{"stream-test"})
		if err != nil {
			t.Fatalf("Failed to get logs: %v", err)
		}

		stdoutCount := 0
		stderrCount := 0
		for _, log := range logs {
			switch log.Stream {
			case "stdout":
				stdoutCount++
			case "stderr":
				stderrCount++
			default:
				t.Errorf("Unexpected stream type: %s", log.Stream)
			}
		}

		if stdoutCount == 0 {
			t.Error("No stdout logs found")
		}
		if stderrCount == 0 {
			t.Error("No stderr logs found")
		}
		t.Logf("Found %d stdout and %d stderr logs", stdoutCount, stderrCount)
	})
}

// TestGetLogs_TimeFiltering tests time-based filtering with since parameter
func TestGetLogs_TimeFiltering(t *testing.T) {
	t.Parallel()

	ts := SetupTest(t)

	// Start a process
	err := ts.StartTestProcess("time-test", "go", "run", ts.TestAppPath("simple_app.go"), "200ms")
	if err != nil {
		t.Fatalf("Failed to start process: %v", err)
	}

	// Wait for initial logs
	time.Sleep(2 * time.Second)

	// Mark a point in time (truncate to second boundary and add 1 second to ensure we only get future logs)
	cutoffTime := time.Now().Truncate(time.Second).Add(1 * time.Second)
	
	// Wait for cutoff time to pass and more logs to be generated
	time.Sleep(3 * time.Second)

	// Get all logs first
	allLogs, err := ts.GetLogs([]string{"time-test"})
	if err != nil {
		t.Fatalf("Failed to get all logs: %v", err)
	}

	// Get logs since cutoff time
	sinceStr := cutoffTime.Format(time.RFC3339)
	recentLogs, err := ts.GetLogs([]string{"time-test"}, WithSince(sinceStr))
	if err != nil {
		t.Fatalf("Failed to get recent logs: %v", err)
	}

	// Verify we got fewer logs (should only have logs after cutoff)
	if len(recentLogs) >= len(allLogs) {
		t.Errorf("Expected fewer recent logs than all logs, got %d recent vs %d total", 
			len(recentLogs), len(allLogs))
	}
	
	// Should have at least some logs from the 2 second window
	if len(recentLogs) == 0 {
		t.Error("Expected some logs after cutoff time")
	}

	// Verify all returned logs are after cutoff time
	for _, log := range recentLogs {
		logTime, err := time.Parse(time.RFC3339, log.Timestamp)
		if err != nil {
			t.Errorf("Failed to parse log timestamp: %v", err)
			continue
		}
		// Since we already adjusted cutoff time, just compare directly
		if logTime.Before(cutoffTime) {
			t.Errorf("Got log from before cutoff time: %s (cutoff was %s)", 
				log.Timestamp, cutoffTime.Format(time.RFC3339))
		}
	}

	// Test with very recent time
	t.Run("VeryRecentTime", func(t *testing.T) {
		// Get logs from last 1 second
		veryRecent := time.Now().Add(-1 * time.Second).Format(time.RFC3339)
		logs, err := ts.GetLogs([]string{"time-test"}, WithSince(veryRecent))
		if err != nil {
			t.Fatalf("Failed to get logs with recent time: %v", err)
		}

		// Should have some recent logs but not all
		if len(logs) == 0 {
			t.Error("No logs found in last 1 second")
		}
		if len(logs) >= len(allLogs) {
			t.Error("Got too many logs for 1 second window")
		}
	})
}

// TestGetLogs_LineLimiting tests line count limiting
func TestGetLogs_LineLimiting(t *testing.T) {
	t.Parallel()

	ts := SetupTest(t)

	// Start a process that generates many logs
	err := ts.StartTestProcess("limit-test", "go", "run", ts.TestAppPath("simple_app.go"), "100ms")
	if err != nil {
		t.Fatalf("Failed to start process: %v", err)
	}

	// Wait for many logs
	time.Sleep(3 * time.Second)

	// Test different limits
	testCases := []struct {
		limit int
	}{
		{limit: 5},
		{limit: 10},
		{limit: 20},
		{limit: 50},
	}

	for _, tc := range testCases {
		t.Run(fmt.Sprintf("Limit%d", tc.limit), func(t *testing.T) {
			logs, err := ts.GetLogs([]string{"limit-test"}, WithLimit(tc.limit))
			if err != nil {
				t.Fatalf("Failed to get logs with limit %d: %v", tc.limit, err)
			}

			// Verify we got at most the requested number of logs
			if len(logs) > tc.limit {
				t.Errorf("Got %d logs, expected at most %d", len(logs), tc.limit)
			}

			// Logs should be the most recent ones
			// Verify they're in chronological order
			for i := 1; i < len(logs); i++ {
				prevTime, _ := time.Parse(time.RFC3339, logs[i-1].Timestamp)
				currTime, _ := time.Parse(time.RFC3339, logs[i].Timestamp)
				if currTime.Before(prevTime) {
					t.Error("Logs not in chronological order")
					break
				}
			}
		})
	}
}

// TestGetLogs_MaxResultsAcrossSessions tests max_results limiting across multiple sessions
func TestGetLogs_MaxResultsAcrossSessions(t *testing.T) {
	t.Parallel()

	ts := SetupTest(t)

	// Start multiple processes
	sessions := []string{"max-1", "max-2", "max-3"}
	for _, label := range sessions {
		err := ts.StartTestProcess(label, "go", "run", ts.TestAppPath("simple_app.go"), "200ms")
		if err != nil {
			t.Fatalf("Failed to start process %s: %v", label, err)
		}
	}

	// Wait for all processes to generate multiple logs
	for _, label := range sessions {
		if !ts.WaitForLogContent(label, "Tick", 5*time.Second) {
			t.Logf("Warning: session %s may not have generated tick logs", label)
		}
	}

	// Test max results across all sessions
	maxResults := 30  // Increased to ensure we can get logs from multiple sessions
	logs, err := ts.GetLogs(sessions, WithMaxResults(maxResults))
	if err != nil {
		t.Fatalf("Failed to get logs with max results: %v", err)
	}

	// Verify we got at most max results
	if len(logs) > maxResults {
		t.Errorf("Got %d logs, expected at most %d", len(logs), maxResults)
	}

	// Verify logs are from multiple sessions
	sessionCounts := make(map[string]int)
	for _, log := range logs {
		sessionCounts[log.Label]++
	}

	sessionsThatProvidedLogs := 0
	for session, count := range sessionCounts {
		if count > 0 {
			sessionsThatProvidedLogs++
			t.Logf("Session %s provided %d logs", session, count)
		}
	}

	// Should have logs from multiple sessions
	if sessionsThatProvidedLogs < 2 {
		t.Errorf("Expected logs from at least 2 sessions, got logs from %d sessions", sessionsThatProvidedLogs)
	}

	// Test interaction with per-session limits
	t.Run("WithPerSessionLimit", func(t *testing.T) {
		logs, err := ts.GetLogs(sessions, WithLimit(5), WithMaxResults(10))
		if err != nil {
			t.Fatalf("Failed to get logs with combined limits: %v", err)
		}

		// Total should not exceed max_results
		if len(logs) > 10 {
			t.Errorf("Got %d logs, expected at most 10", len(logs))
		}

		// No session should have more than 5 logs
		sessionCounts := make(map[string]int)
		for _, log := range logs {
			sessionCounts[log.Label]++
		}
		for session, count := range sessionCounts {
			if count > 5 {
				t.Errorf("Session %s has %d logs, expected at most 5", session, count)
			}
		}
	})
}

// TestGetLogs_NonExistentSession tests handling of non-existent sessions
func TestGetLogs_NonExistentSession(t *testing.T) {
	t.Parallel()

	ts := SetupTest(t)

	// Try to get logs from non-existent session
	logs, err := ts.GetLogs([]string{"non-existent"})
	if err != nil {
		t.Fatalf("Failed to get logs: %v", err)
	}

	// Should return empty logs, not error
	if len(logs) != 0 {
		t.Errorf("Expected no logs for non-existent session, got %d", len(logs))
	}

	// Test mixed existent and non-existent sessions
	t.Run("MixedSessions", func(t *testing.T) {
		// Start one real process
		err := ts.StartTestProcess("real-session", "go", "run", ts.TestAppPath("simple_app.go"))
		if err != nil {
			t.Fatalf("Failed to start process: %v", err)
		}

		// Wait for logs
		time.Sleep(2 * time.Second)

		// Get logs from both real and non-existent sessions
		logs, err := ts.GetLogs([]string{"real-session", "fake-session-1", "fake-session-2"})
		if err != nil {
			t.Fatalf("Failed to get logs: %v", err)
		}

		// Should only have logs from real session
		for _, log := range logs {
			if log.Label != "real-session" {
				t.Errorf("Got log from unexpected session: %s", log.Label)
			}
		}

		if len(logs) == 0 {
			t.Error("Expected logs from real session")
		}
	})
}

// TestGetLogs_CombinedFilters tests combining multiple filters
func TestGetLogs_CombinedFilters(t *testing.T) {
	t.Parallel()

	ts := SetupTest(t)

	// Start multiple processes
	sessions := []string{"combined-1", "combined-2"}
	for _, label := range sessions {
		err := ts.StartTestProcess(label, "go", "run", ts.TestAppPath("simple_app.go"), "200ms")
		if err != nil {
			t.Fatalf("Failed to start process %s: %v", label, err)
		}
	}

	// Wait for initial logs
	time.Sleep(2 * time.Second)
	
	// Mark cutoff time
	cutoffTime := time.Now()
	
	// Wait for more logs after cutoff
	time.Sleep(3 * time.Second)

	// Test combining all filters
	logs, err := ts.GetLogs(
		sessions,
		WithPattern("message [0-9]+"),
		WithStream("stdout"),
		WithSince(cutoffTime.Format(time.RFC3339)),
		WithLimit(5),
		WithMaxResults(8),
	)
	if err != nil {
		t.Fatalf("Failed to get logs with combined filters: %v", err)
	}

	// Verify all filters are applied
	for _, log := range logs {
		// Check pattern
		if !strings.Contains(log.Content, "message") {
			t.Errorf("Log doesn't match pattern: %s", log.Content)
		}

		// Check stream
		if log.Stream != "stdout" {
			t.Errorf("Expected stdout, got %s", log.Stream)
		}

		// Check time
		logTime, _ := time.Parse(time.RFC3339, log.Timestamp)
		if logTime.Before(cutoffTime) {
			t.Error("Got log from before cutoff time")
		}
	}

	// Check counts
	if len(logs) > 8 {
		t.Errorf("Got %d logs, expected at most 8 (max_results)", len(logs))
	}

	// Check per-session limit
	sessionCounts := make(map[string]int)
	for _, log := range logs {
		sessionCounts[log.Label]++
	}
	for session, count := range sessionCounts {
		if count > 5 {
			t.Errorf("Session %s has %d logs, expected at most 5 (limit)", session, count)
		}
	}
}

// TestGetLogs_EmptySession tests getting logs from a session with no output
func TestGetLogs_EmptySession(t *testing.T) {
	t.Parallel()

	ts := SetupTest(t)

	// Start a process that doesn't output anything
	err := ts.StartTestProcess("silent", "sleep", "10")
	if err != nil {
		t.Fatalf("Failed to start process: %v", err)
	}

	// Wait a bit
	time.Sleep(1 * time.Second)

	// Get logs
	logs, err := ts.GetLogs([]string{"silent"})
	if err != nil {
		t.Fatalf("Failed to get logs: %v", err)
	}

	// Should return empty array, not error
	if len(logs) != 0 {
		t.Errorf("Expected no logs from silent process, got %d", len(logs))
	}
}

// TestGetLogs_LargeLogVolume tests handling of high log volume
func TestGetLogs_LargeLogVolume(t *testing.T) {
	t.Parallel()

	ts := SetupTest(t)

	// Start a process that generates logs rapidly
	err := ts.StartTestProcess("verbose", "go", "run", ts.TestAppPath("simple_app.go"), "10ms")
	if err != nil {
		t.Fatalf("Failed to start process: %v", err)
	}

	// Let it generate many logs
	time.Sleep(5 * time.Second)

	// Try to get all logs
	start := time.Now()
	logs, err := ts.GetLogs([]string{"verbose"})
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("Failed to get logs: %v", err)
	}

	t.Logf("Retrieved %d logs in %v", len(logs), elapsed)

	// Should handle large volumes efficiently
	if elapsed > 5*time.Second {
		t.Errorf("Took too long to retrieve logs: %v", elapsed)
	}

	// Verify logs are properly structured even with high volume
	if len(logs) > 0 {
		// Check first and last log
		for _, idx := range []int{0, len(logs) - 1} {
			log := logs[idx]
			if log.Label != "verbose" {
				t.Errorf("Log %d has wrong label: %s", idx, log.Label)
			}
			if log.Timestamp == "" {
				t.Errorf("Log %d missing timestamp", idx)
			}
			if log.Stream == "" {
				t.Errorf("Log %d missing stream", idx)
			}
		}
	}
}