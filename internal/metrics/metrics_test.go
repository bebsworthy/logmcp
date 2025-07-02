package metrics

import (
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"
)

// TestNewMonitor tests monitor creation
func TestNewMonitor(t *testing.T) {
	monitor := NewMonitor()
	
	if monitor == nil {
		t.Fatal("Expected monitor to be created")
	}
	
	if monitor.operations == nil {
		t.Error("Expected operations map to be initialized")
	}
	
	if monitor.errors == nil {
		t.Error("Expected errors map to be initialized")
	}
	
	if monitor.connections == nil {
		t.Error("Expected connections to be initialized")
	}
}

// TestTrackOperation tests operation tracking
func TestTrackOperation(t *testing.T) {
	monitor := NewMonitor()
	
	// Test successful operation
	err := monitor.TrackOperation(context.Background(), "test_operation", func() error {
		time.Sleep(10 * time.Millisecond)
		return nil
	})
	
	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}
	
	// Check metrics were recorded
	metrics := monitor.GetOperationMetrics("test_operation")
	if metrics == nil {
		t.Fatal("Expected operation metrics to be recorded")
	}
	
	if metrics.Count != 1 {
		t.Errorf("Expected count 1, got %d", metrics.Count)
	}
	
	if metrics.Successes != 1 {
		t.Errorf("Expected successes 1, got %d", metrics.Successes)
	}
	
	if metrics.Errors != 0 {
		t.Errorf("Expected errors 0, got %d", metrics.Errors)
	}
	
	if metrics.TotalDuration <= 0 {
		t.Error("Expected positive total duration")
	}
	
	// Test failed operation
	testErr := errors.New("test error")
	err = monitor.TrackOperation(context.Background(), "test_operation", func() error {
		return testErr
	})
	
	if err != testErr {
		t.Errorf("Expected test error to be returned, got %v", err)
	}
	
	// Check updated metrics
	metrics = monitor.GetOperationMetrics("test_operation")
	if metrics.Count != 2 {
		t.Errorf("Expected count 2, got %d", metrics.Count)
	}
	
	if metrics.Successes != 1 {
		t.Errorf("Expected successes 1, got %d", metrics.Successes)
	}
	
	if metrics.Errors != 1 {
		t.Errorf("Expected errors 1, got %d", metrics.Errors)
	}
}

// TestLogTiming tests timing logging
func TestLogTiming(t *testing.T) {
	monitor := NewMonitor()
	
	start := time.Now()
	time.Sleep(5 * time.Millisecond)
	
	monitor.LogTiming("timing_test", start)
	
	metrics := monitor.GetOperationMetrics("timing_test")
	if metrics == nil {
		t.Fatal("Expected timing metrics to be recorded")
	}
	
	if metrics.Count != 1 {
		t.Errorf("Expected count 1, got %d", metrics.Count)
	}
	
	if metrics.TotalDuration <= 0 {
		t.Error("Expected positive duration")
	}
}

// TestTrackError tests error tracking
func TestTrackError(t *testing.T) {
	monitor := NewMonitor()
	
	// Track first error
	monitor.TrackError(context.Background(), "network", "CONNECTION_FAILED", "client", "Connection failed")
	
	errorMetrics := monitor.GetErrorMetrics()
	key := "network:CONNECTION_FAILED"
	
	if _, exists := errorMetrics[key]; !exists {
		t.Fatal("Expected error metrics to be recorded")
	}
	
	metrics := errorMetrics[key]
	if metrics.Count != 1 {
		t.Errorf("Expected count 1, got %d", metrics.Count)
	}
	
	if metrics.Type != "network" {
		t.Errorf("Expected type 'network', got '%s'", metrics.Type)
	}
	
	if metrics.Code != "CONNECTION_FAILED" {
		t.Errorf("Expected code 'CONNECTION_FAILED', got '%s'", metrics.Code)
	}
	
	// Track same error again
	monitor.TrackError(context.Background(), "network", "CONNECTION_FAILED", "client", "Connection failed")
	
	errorMetrics = monitor.GetErrorMetrics()
	metrics = errorMetrics[key]
	if metrics.Count != 2 {
		t.Errorf("Expected count 2, got %d", metrics.Count)
	}
}

// TestTrackConnection tests connection tracking
func TestTrackConnection(t *testing.T) {
	monitor := NewMonitor()
	
	// Test connect event
	monitor.TrackConnection("connect", 100*time.Millisecond)
	
	connMetrics := monitor.GetConnectionMetrics()
	if connMetrics.TotalConnections != 1 {
		t.Errorf("Expected total connections 1, got %d", connMetrics.TotalConnections)
	}
	
	if connMetrics.ActiveConnections != 1 {
		t.Errorf("Expected active connections 1, got %d", connMetrics.ActiveConnections)
	}
	
	if connMetrics.AverageConnectTime != 100*time.Millisecond {
		t.Errorf("Expected average connect time 100ms, got %v", connMetrics.AverageConnectTime)
	}
	
	// Test disconnect event
	monitor.TrackConnection("disconnect", 0)
	
	connMetrics = monitor.GetConnectionMetrics()
	if connMetrics.ActiveConnections != 0 {
		t.Errorf("Expected active connections 0, got %d", connMetrics.ActiveConnections)
	}
	
	// Test failed connection
	monitor.TrackConnection("connect_failed", 0)
	
	connMetrics = monitor.GetConnectionMetrics()
	if connMetrics.FailedConnections != 1 {
		t.Errorf("Expected failed connections 1, got %d", connMetrics.FailedConnections)
	}
	
	// Test reconnection attempt
	monitor.TrackConnection("reconnect_attempt", 0)
	
	connMetrics = monitor.GetConnectionMetrics()
	if connMetrics.ReconnectionAttempts != 1 {
		t.Errorf("Expected reconnection attempts 1, got %d", connMetrics.ReconnectionAttempts)
	}
}

// TestOperationContext tests operation context functionality
func TestOperationContext(t *testing.T) {
	ctx := WithOperation(context.Background(), "test_operation")
	
	operation := GetOperation(ctx)
	if operation != "test_operation" {
		t.Errorf("Expected operation 'test_operation', got '%s'", operation)
	}
	
	// Test empty context
	emptyOp := GetOperation(context.Background())
	if emptyOp != "" {
		t.Errorf("Expected empty operation, got '%s'", emptyOp)
	}
}

// TestGetAllOperationMetrics tests retrieving all operation metrics
func TestGetAllOperationMetrics(t *testing.T) {
	monitor := NewMonitor()
	
	// Track multiple operations
	monitor.LogTiming("op1", time.Now().Add(-10*time.Millisecond))
	monitor.LogTiming("op2", time.Now().Add(-20*time.Millisecond))
	monitor.LogTiming("op3", time.Now().Add(-30*time.Millisecond))
	
	allMetrics := monitor.GetAllOperationMetrics()
	
	if len(allMetrics) != 3 {
		t.Errorf("Expected 3 operations, got %d", len(allMetrics))
	}
	
	expectedOps := []string{"op1", "op2", "op3"}
	for _, op := range expectedOps {
		if _, exists := allMetrics[op]; !exists {
			t.Errorf("Expected operation '%s' in metrics", op)
		}
	}
}

// TestReset tests metrics reset functionality
func TestReset(t *testing.T) {
	monitor := NewMonitor()
	
	// Add some metrics
	monitor.LogTiming("test_op", time.Now().Add(-10*time.Millisecond))
	monitor.TrackError(context.Background(), "test", "TEST_ERROR", "test", "test message")
	monitor.TrackConnection("connect", 100*time.Millisecond)
	
	// Verify metrics exist
	if len(monitor.GetAllOperationMetrics()) == 0 {
		t.Error("Expected operation metrics before reset")
	}
	
	if len(monitor.GetErrorMetrics()) == 0 {
		t.Error("Expected error metrics before reset")
	}
	
	connMetrics := monitor.GetConnectionMetrics()
	if connMetrics.TotalConnections == 0 {
		t.Error("Expected connection metrics before reset")
	}
	
	// Reset and verify everything is cleared
	monitor.Reset()
	
	if len(monitor.GetAllOperationMetrics()) != 0 {
		t.Error("Expected no operation metrics after reset")
	}
	
	if len(monitor.GetErrorMetrics()) != 0 {
		t.Error("Expected no error metrics after reset")
	}
	
	connMetrics = monitor.GetConnectionMetrics()
	if connMetrics.TotalConnections != 0 {
		t.Error("Expected no connection metrics after reset")
	}
}

// TestTimer tests timer functionality
func TestTimer(t *testing.T) {
	timer := NewTimer("timer_test")
	
	if timer.operation != "timer_test" {
		t.Errorf("Expected operation 'timer_test', got '%s'", timer.operation)
	}
	
	if timer.start.IsZero() {
		t.Error("Expected start time to be set")
	}
	
	// Sleep briefly and stop timer
	time.Sleep(5 * time.Millisecond)
	timer.Stop()
	
	// Check if metrics were recorded
	metrics := Default().GetOperationMetrics("timer_test")
	if metrics == nil {
		t.Fatal("Expected timer metrics to be recorded")
	}
	
	if metrics.Count != 1 {
		t.Errorf("Expected count 1, got %d", metrics.Count)
	}
}

// TestTimerWithError tests timer error handling
func TestTimerWithError(t *testing.T) {
	monitor := NewMonitor()
	timer := NewTimerWithMonitor("timer_error_test", monitor)
	
	// Test with error
	ctx := context.Background()
	testErr := errors.New("timer test error")
	
	time.Sleep(5 * time.Millisecond)
	timer.StopWithError(ctx, testErr)
	
	metrics := monitor.GetOperationMetrics("timer_error_test")
	if metrics == nil {
		t.Fatal("Expected timer metrics to be recorded")
	}
	
	if metrics.Count != 1 {
		t.Errorf("Expected count 1, got %d", metrics.Count)
	}
	
	if metrics.Errors != 1 {
		t.Errorf("Expected errors 1, got %d", metrics.Errors)
	}
	
	if metrics.Successes != 0 {
		t.Errorf("Expected successes 0, got %d", metrics.Successes)
	}
	
	// Test without error
	timer2 := NewTimerWithMonitor("timer_success_test", monitor)
	time.Sleep(5 * time.Millisecond)
	timer2.StopWithError(ctx, nil)
	
	metrics2 := monitor.GetOperationMetrics("timer_success_test")
	if metrics2 == nil {
		t.Fatal("Expected timer metrics to be recorded")
	}
	
	if metrics2.Successes != 1 {
		t.Errorf("Expected successes 1, got %d", metrics2.Successes)
	}
	
	if metrics2.Errors != 0 {
		t.Errorf("Expected errors 0, got %d", metrics2.Errors)
	}
}

// TestGlobalFunctions tests package-level convenience functions
func TestGlobalFunctions(t *testing.T) {
	// Reset default monitor
	defaultMonitor.Reset()
	
	// Test TrackOperation
	err := TrackOperation(context.Background(), "global_test", func() error {
		time.Sleep(5 * time.Millisecond)
		return nil
	})
	
	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}
	
	// Test LogTiming
	LogTiming("global_timing", time.Now().Add(-10*time.Millisecond))
	
	// Test TrackError
	TrackError(context.Background(), "global", "GLOBAL_ERROR", "test", "global error")
	
	// Test TrackConnection
	TrackConnection("connect", 50*time.Millisecond)
	
	// Verify all metrics were recorded
	allOps := Default().GetAllOperationMetrics()
	if len(allOps) < 2 {
		t.Errorf("Expected at least 2 operations, got %d", len(allOps))
	}
	
	allErrors := Default().GetErrorMetrics()
	if len(allErrors) != 1 {
		t.Errorf("Expected 1 error type, got %d", len(allErrors))
	}
	
	connMetrics := Default().GetConnectionMetrics()
	if connMetrics.TotalConnections != 1 {
		t.Errorf("Expected 1 total connection, got %d", connMetrics.TotalConnections)
	}
}

// TestMetricsWithLogger tests metrics with logger integration
func TestMetricsWithLogger(t *testing.T) {
	monitor := NewMonitor()
	
	// Create a test logger (using default handler for simplicity)
	logger := slog.Default()
	monitor.SetLogger(logger)
	
	// Test operation tracking with logger
	err := monitor.TrackOperation(context.Background(), "logged_operation", func() error {
		time.Sleep(5 * time.Millisecond)
		return nil
	})
	
	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}
	
	// Test error tracking with logger
	monitor.TrackError(context.Background(), "logged", "LOGGED_ERROR", "test", "logged error")
	
	// Test connection tracking with logger
	monitor.TrackConnection("connect", 25*time.Millisecond)
	
	// Test metrics summary logging
	monitor.LogMetricsSummary(context.Background())
	
	// If we get here without panicking, the logger integration works
}

// BenchmarkTrackOperation benchmarks operation tracking
func BenchmarkTrackOperation(b *testing.B) {
	monitor := NewMonitor()
	ctx := context.Background()
	
	b.ResetTimer()
	
	for i := 0; i < b.N; i++ {
		monitor.TrackOperation(ctx, "benchmark_op", func() error {
			return nil
		})
	}
}

// BenchmarkLogTiming benchmarks timing logging
func BenchmarkLogTiming(b *testing.B) {
	monitor := NewMonitor()
	
	b.ResetTimer()
	
	for i := 0; i < b.N; i++ {
		monitor.LogTiming("benchmark_timing", time.Now().Add(-time.Microsecond))
	}
}

// BenchmarkTimer benchmarks timer functionality
func BenchmarkTimer(b *testing.B) {
	b.ResetTimer()
	
	for i := 0; i < b.N; i++ {
		timer := NewTimer("benchmark_timer")
		timer.Stop()
	}
}