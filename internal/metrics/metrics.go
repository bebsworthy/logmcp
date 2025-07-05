// Package metrics provides performance monitoring and metrics collection for LogMCP.
//
// This package implements:
// - Operation timing and performance metrics
// - Error rate tracking and categorization
// - Connection and session metrics
// - Resource usage monitoring
// - Integration with structured logging
//
// Example usage:
//
//	monitor := metrics.NewMonitor()
//	defer monitor.LogTiming("websocket_connect", time.Now())
//
//	// Track operation with context
//	ctx = metrics.WithOperation(ctx, "process_start")
//	metrics.TrackOperation(ctx, func() error {
//		return startProcess()
//	})
package metrics

import (
	"context"
	"log/slog"
	"sync"
	"time"
)

// OperationKey is the context key for operation names
type OperationKey struct{}

// Monitor provides performance monitoring functionality
type Monitor struct {
	logger *slog.Logger
	mu     sync.RWMutex

	// Counters
	operations  map[string]*OperationMetrics
	errors      map[string]*ErrorMetrics
	connections *ConnectionMetrics

	// Configuration
	enableTiming   bool
	enableErrors   bool
	enableRequests bool
}

// OperationMetrics tracks metrics for specific operations
type OperationMetrics struct {
	Name            string        `json:"name"`
	Count           int64         `json:"count"`
	TotalDuration   time.Duration `json:"total_duration"`
	AverageDuration time.Duration `json:"average_duration"`
	MinDuration     time.Duration `json:"min_duration"`
	MaxDuration     time.Duration `json:"max_duration"`
	LastExecution   time.Time     `json:"last_execution"`
	Errors          int64         `json:"errors"`
	Successes       int64         `json:"successes"`
}

// ErrorMetrics tracks error occurrences and patterns
type ErrorMetrics struct {
	Type         string    `json:"type"`
	Code         string    `json:"code"`
	Count        int64     `json:"count"`
	LastOccurred time.Time `json:"last_occurred"`
	Component    string    `json:"component"`
	Message      string    `json:"message"`
}

// ConnectionMetrics tracks connection-related metrics
type ConnectionMetrics struct {
	ActiveConnections    int64         `json:"active_connections"`
	TotalConnections     int64         `json:"total_connections"`
	FailedConnections    int64         `json:"failed_connections"`
	ReconnectionAttempts int64         `json:"reconnection_attempts"`
	AverageConnectTime   time.Duration `json:"average_connect_time"`
	LastConnectTime      time.Time     `json:"last_connect_time"`
}

// NewMonitor creates a new performance monitor
func NewMonitor() *Monitor {
	return &Monitor{
		operations:     make(map[string]*OperationMetrics),
		errors:         make(map[string]*ErrorMetrics),
		connections:    &ConnectionMetrics{},
		enableTiming:   true,
		enableErrors:   true,
		enableRequests: true,
	}
}

// SetLogger sets the logger for metrics output
func (m *Monitor) SetLogger(logger *slog.Logger) {
	m.logger = logger.With(slog.String("component", "metrics"))
}

// WithOperation adds operation context
func WithOperation(ctx context.Context, operation string) context.Context {
	return context.WithValue(ctx, OperationKey{}, operation)
}

// GetOperation retrieves operation name from context
func GetOperation(ctx context.Context) string {
	if op, ok := ctx.Value(OperationKey{}).(string); ok {
		return op
	}
	return ""
}

// TrackOperation tracks the execution of an operation
func (m *Monitor) TrackOperation(ctx context.Context, operation string, fn func() error) error {
	if !m.enableTiming {
		return fn()
	}

	start := time.Now()
	err := fn()
	duration := time.Since(start)

	m.recordOperation(operation, duration, err == nil)

	if m.logger != nil {
		level := slog.LevelInfo
		status := "success"
		if err != nil {
			level = slog.LevelError
			status = "error"
		}

		m.logger.LogAttrs(ctx, level, "Operation completed",
			slog.String("operation", operation),
			slog.Duration("duration", duration),
			slog.String("status", status),
		)

		if err != nil {
			m.logger.LogAttrs(ctx, slog.LevelError, "Operation failed",
				slog.String("operation", operation),
				slog.String("error", err.Error()),
			)
		}
	}

	return err
}

// recordOperation records operation metrics
func (m *Monitor) recordOperation(name string, duration time.Duration, success bool) {
	m.mu.Lock()
	defer m.mu.Unlock()

	metrics, exists := m.operations[name]
	if !exists {
		metrics = &OperationMetrics{
			Name:        name,
			MinDuration: duration,
			MaxDuration: duration,
		}
		m.operations[name] = metrics
	}

	metrics.Count++
	metrics.TotalDuration += duration
	metrics.LastExecution = time.Now()

	if duration < metrics.MinDuration {
		metrics.MinDuration = duration
	}
	if duration > metrics.MaxDuration {
		metrics.MaxDuration = duration
	}

	metrics.AverageDuration = time.Duration(int64(metrics.TotalDuration) / metrics.Count)

	if success {
		metrics.Successes++
	} else {
		metrics.Errors++
	}
}

// LogTiming logs timing information for an operation
func (m *Monitor) LogTiming(operation string, start time.Time, attrs ...slog.Attr) {
	if !m.enableTiming {
		return
	}

	duration := time.Since(start)
	m.recordOperation(operation, duration, true)

	if m.logger != nil {
		allAttrs := []slog.Attr{
			slog.String("operation", operation),
			slog.Duration("duration", duration),
			slog.String("metric_type", "timing"),
		}
		allAttrs = append(allAttrs, attrs...)

		m.logger.LogAttrs(context.Background(), slog.LevelInfo,
			"Operation timing", allAttrs...)
	}
}

// TrackError tracks error occurrences
func (m *Monitor) TrackError(ctx context.Context, errorType, code, component, message string) {
	if !m.enableErrors {
		return
	}

	key := errorType + ":" + code

	m.mu.Lock()
	errorMetrics, exists := m.errors[key]
	if !exists {
		errorMetrics = &ErrorMetrics{
			Type:      errorType,
			Code:      code,
			Component: component,
			Message:   message,
		}
		m.errors[key] = errorMetrics
	}

	errorMetrics.Count++
	errorMetrics.LastOccurred = time.Now()
	m.mu.Unlock()

	if m.logger != nil {
		m.logger.ErrorContext(ctx, "Error tracked",
			slog.String("error_type", errorType),
			slog.String("error_code", code),
			slog.String("component", component),
			slog.Int64("count", errorMetrics.Count),
			slog.String("message", message),
		)
	}
}

// TrackConnection tracks connection-related metrics
func (m *Monitor) TrackConnection(event string, duration time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()

	switch event {
	case "connect":
		m.connections.TotalConnections++
		m.connections.ActiveConnections++
		m.connections.LastConnectTime = time.Now()

		// Update average connect time
		if m.connections.TotalConnections > 0 {
			totalTime := time.Duration(m.connections.AverageConnectTime.Nanoseconds() *
				(m.connections.TotalConnections - 1))
			m.connections.AverageConnectTime = (totalTime + duration) /
				time.Duration(m.connections.TotalConnections)
		} else {
			m.connections.AverageConnectTime = duration
		}

	case "disconnect":
		if m.connections.ActiveConnections > 0 {
			m.connections.ActiveConnections--
		}

	case "connect_failed":
		m.connections.FailedConnections++

	case "reconnect_attempt":
		m.connections.ReconnectionAttempts++
	}

	if m.logger != nil {
		m.logger.Info("Connection event",
			slog.String("event", event),
			slog.Duration("duration", duration),
			slog.Int64("active_connections", m.connections.ActiveConnections),
			slog.Int64("total_connections", m.connections.TotalConnections),
		)
	}
}

// GetOperationMetrics returns metrics for a specific operation
func (m *Monitor) GetOperationMetrics(operation string) *OperationMetrics {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if metrics, exists := m.operations[operation]; exists {
		// Return a copy to avoid race conditions
		copy := *metrics
		return &copy
	}
	return nil
}

// GetAllOperationMetrics returns all operation metrics
func (m *Monitor) GetAllOperationMetrics() map[string]*OperationMetrics {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make(map[string]*OperationMetrics)
	for name, metrics := range m.operations {
		copy := *metrics
		result[name] = &copy
	}
	return result
}

// GetErrorMetrics returns all error metrics
func (m *Monitor) GetErrorMetrics() map[string]*ErrorMetrics {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make(map[string]*ErrorMetrics)
	for key, metrics := range m.errors {
		copy := *metrics
		result[key] = &copy
	}
	return result
}

// GetConnectionMetrics returns connection metrics
func (m *Monitor) GetConnectionMetrics() *ConnectionMetrics {
	m.mu.RLock()
	defer m.mu.RUnlock()

	copy := *m.connections
	return &copy
}

// LogMetricsSummary logs a summary of all collected metrics
func (m *Monitor) LogMetricsSummary(ctx context.Context) {
	if m.logger == nil {
		return
	}

	m.mu.RLock()
	defer m.mu.RUnlock()

	// Log operation metrics summary
	m.logger.InfoContext(ctx, "Metrics Summary - Operations",
		slog.Int("total_operations", len(m.operations)),
	)

	for name, metrics := range m.operations {
		successRate := float64(0)
		if metrics.Count > 0 {
			successRate = float64(metrics.Successes) / float64(metrics.Count) * 100
		}

		m.logger.InfoContext(ctx, "Operation metrics",
			slog.String("operation", name),
			slog.Int64("count", metrics.Count),
			slog.Duration("avg_duration", metrics.AverageDuration),
			slog.Duration("min_duration", metrics.MinDuration),
			slog.Duration("max_duration", metrics.MaxDuration),
			slog.Float64("success_rate", successRate),
		)
	}

	// Log error metrics summary
	m.logger.InfoContext(ctx, "Metrics Summary - Errors",
		slog.Int("total_error_types", len(m.errors)),
	)

	for _, metrics := range m.errors {
		m.logger.InfoContext(ctx, "Error metrics",
			slog.String("error_type", metrics.Type),
			slog.String("error_code", metrics.Code),
			slog.String("component", metrics.Component),
			slog.Int64("count", metrics.Count),
			slog.Time("last_occurred", metrics.LastOccurred),
		)
	}

	// Log connection metrics
	m.logger.InfoContext(ctx, "Connection metrics",
		slog.Int64("active_connections", m.connections.ActiveConnections),
		slog.Int64("total_connections", m.connections.TotalConnections),
		slog.Int64("failed_connections", m.connections.FailedConnections),
		slog.Int64("reconnection_attempts", m.connections.ReconnectionAttempts),
		slog.Duration("avg_connect_time", m.connections.AverageConnectTime),
	)
}

// Reset clears all metrics
func (m *Monitor) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.operations = make(map[string]*OperationMetrics)
	m.errors = make(map[string]*ErrorMetrics)
	m.connections = &ConnectionMetrics{}
}

// Global monitor instance
var defaultMonitor = NewMonitor()

// SetDefaultMonitor sets the global default monitor
func SetDefaultMonitor(monitor *Monitor) {
	defaultMonitor = monitor
}

// Default returns the default monitor instance
func Default() *Monitor {
	return defaultMonitor
}

// Package-level convenience functions

// TrackOperation tracks an operation using the default monitor
func TrackOperation(ctx context.Context, operation string, fn func() error) error {
	return defaultMonitor.TrackOperation(ctx, operation, fn)
}

// LogTiming logs timing using the default monitor
func LogTiming(operation string, start time.Time, attrs ...slog.Attr) {
	defaultMonitor.LogTiming(operation, start, attrs...)
}

// TrackError tracks an error using the default monitor
func TrackError(ctx context.Context, errorType, code, component, message string) {
	defaultMonitor.TrackError(ctx, errorType, code, component, message)
}

// TrackConnection tracks a connection event using the default monitor
func TrackConnection(event string, duration time.Duration) {
	defaultMonitor.TrackConnection(event, duration)
}

// Timer provides convenient timing functionality
type Timer struct {
	start     time.Time
	operation string
	monitor   *Monitor
}

// NewTimer creates a new timer for an operation
func NewTimer(operation string) *Timer {
	return &Timer{
		start:     time.Now(),
		operation: operation,
		monitor:   defaultMonitor,
	}
}

// NewTimerWithMonitor creates a new timer with a specific monitor
func NewTimerWithMonitor(operation string, monitor *Monitor) *Timer {
	return &Timer{
		start:     time.Now(),
		operation: operation,
		monitor:   monitor,
	}
}

// Stop stops the timer and logs the timing
func (t *Timer) Stop(attrs ...slog.Attr) {
	t.monitor.LogTiming(t.operation, t.start, attrs...)
}

// StopWithError stops the timer and tracks an error if provided
func (t *Timer) StopWithError(ctx context.Context, err error) {
	duration := time.Since(t.start)
	t.monitor.recordOperation(t.operation, duration, err == nil)

	if err != nil && t.monitor.logger != nil {
		t.monitor.logger.ErrorContext(ctx, "Timed operation failed",
			slog.String("operation", t.operation),
			slog.Duration("duration", duration),
			slog.String("error", err.Error()),
		)
	}
}
