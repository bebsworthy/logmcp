package logging

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/logmcp/logmcp/internal/config"
)

// TestNewLogger tests logger creation with different configurations
func TestNewLogger(t *testing.T) {
	tests := []struct {
		name   string
		config config.LoggingConfig
		valid  bool
	}{
		{
			name: "valid_text_logger",
			config: config.LoggingConfig{
				Level:  "info",
				Format: "text",
			},
			valid: true,
		},
		{
			name: "valid_json_logger",
			config: config.LoggingConfig{
				Level:  "debug",
				Format: "json",
			},
			valid: true,
		},
		{
			name: "invalid_level",
			config: config.LoggingConfig{
				Level:  "invalid",
				Format: "text",
			},
			valid: false,
		},
		{
			name: "invalid_format",
			config: config.LoggingConfig{
				Level:  "info",
				Format: "invalid",
			},
			valid: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logger, err := NewLogger(tt.config)
			
			if tt.valid {
				if err != nil {
					t.Errorf("Expected no error, got %v", err)
				}
				if logger == nil {
					t.Error("Expected logger to be created")
				}
			} else {
				if err == nil {
					t.Error("Expected error for invalid config")
				}
			}
			
			if logger != nil {
				logger.Close()
			}
		})
	}
}

// TestLoggerOutput tests that logger produces expected output
func TestLoggerOutput(t *testing.T) {
	var buf bytes.Buffer
	
	// Create logger with custom writer
	cfg := config.LoggingConfig{
		Level:  "debug",
		Format: "json",
	}
	
	logger, err := NewLogger(cfg)
	if err != nil {
		t.Fatalf("Failed to create logger: %v", err)
	}
	
	// Replace the writer with our buffer
	logger.writer = &buf
	
	// Recreate logger with buffer
	handler := slog.NewJSONHandler(&buf, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	})
	logger.Logger = slog.New(&CorrelationHandler{Handler: handler})
	
	// Test different log levels
	logger.Debug("Debug message", slog.String("key", "value"))
	logger.Info("Info message", slog.Int("number", 42))
	logger.Warn("Warning message")
	logger.Error("Error message", slog.String("error", "test error"))
	
	output := buf.String()
	lines := strings.Split(strings.TrimSpace(output), "\n")
	
	if len(lines) != 4 {
		t.Errorf("Expected 4 log lines, got %d", len(lines))
	}
	
	// Verify JSON format
	for i, line := range lines {
		var logEntry map[string]interface{}
		if err := json.Unmarshal([]byte(line), &logEntry); err != nil {
			t.Errorf("Line %d is not valid JSON: %v", i+1, err)
		}
		
		// Check required fields
		if _, ok := logEntry["time"]; !ok {
			t.Errorf("Line %d missing 'time' field", i+1)
		}
		if _, ok := logEntry["level"]; !ok {
			t.Errorf("Line %d missing 'level' field", i+1)
		}
		if _, ok := logEntry["msg"]; !ok {
			t.Errorf("Line %d missing 'msg' field", i+1)
		}
	}
}

// TestCorrelationID tests correlation ID functionality
func TestCorrelationID(t *testing.T) {
	var buf bytes.Buffer
	
	cfg := config.LoggingConfig{
		Level:  "info",
		Format: "json",
	}
	
	logger, err := NewLogger(cfg)
	if err != nil {
		t.Fatalf("Failed to create logger: %v", err)
	}
	
	// Replace the writer with our buffer
	handler := slog.NewJSONHandler(&buf, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	})
	logger.Logger = slog.New(&CorrelationHandler{Handler: handler})
	
	// Test with correlation ID
	ctx := WithCorrelationID(context.Background(), "test-correlation-123")
	logger.InfoContext(ctx, "Test message with correlation ID")
	
	output := buf.String()
	
	if !strings.Contains(output, "test-correlation-123") {
		t.Error("Expected correlation ID in log output")
	}
	
	if !strings.Contains(output, "correlation_id") {
		t.Error("Expected correlation_id field in log output")
	}
	
	// Test correlation ID retrieval
	retrievedID := GetCorrelationID(ctx)
	if retrievedID != "test-correlation-123" {
		t.Errorf("Expected correlation ID 'test-correlation-123', got '%s'", retrievedID)
	}
	
	// Test without correlation ID
	emptyID := GetCorrelationID(context.Background())
	if emptyID != "" {
		t.Errorf("Expected empty correlation ID, got '%s'", emptyID)
	}
}

// TestComponentSpecificLoggers tests component-specific logger creation
func TestComponentSpecificLoggers(t *testing.T) {
	cfg := config.LoggingConfig{
		Level:  "info",
		Format: "text",
	}
	
	tests := []struct {
		name     string
		createFn func() (*Logger, error)
	}{
		{
			name: "server_logger",
			createFn: func() (*Logger, error) {
				return NewServerLogger(cfg)
			},
		},
		{
			name: "runner_logger",
			createFn: func() (*Logger, error) {
				return NewRunnerLogger(cfg, "session-123", "test-runner")
			},
		},
		{
			name: "forwarder_logger",
			createFn: func() (*Logger, error) {
				return NewForwarderLogger(cfg, "session-456", "test-forwarder", "/var/log/test.log")
			},
		},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logger, err := tt.createFn()
			if err != nil {
				t.Errorf("Failed to create %s: %v", tt.name, err)
			}
			if logger == nil {
				t.Errorf("Expected logger to be created for %s", tt.name)
			}
			
			if logger != nil {
				logger.Close()
			}
		})
	}
}

// TestLogTiming tests performance timing logging
func TestLogTiming(t *testing.T) {
	var buf bytes.Buffer
	
	cfg := config.LoggingConfig{
		Level:  "info",
		Format: "json",
	}
	
	logger, err := NewLogger(cfg)
	if err != nil {
		t.Fatalf("Failed to create logger: %v", err)
	}
	
	// Replace the writer with our buffer
	handler := slog.NewJSONHandler(&buf, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	})
	logger.Logger = slog.New(&CorrelationHandler{Handler: handler})
	
	// Test timing log
	start := time.Now()
	time.Sleep(10 * time.Millisecond) // Small delay
	
	ctx := context.Background()
	logger.LogTiming(ctx, "test_operation", start, slog.String("component", "test"))
	
	output := buf.String()
	
	// Verify expected fields are present
	expectedFields := []string{
		"operation",
		"duration",
		"performance",
		"component",
		"Operation completed",
	}
	
	for _, field := range expectedFields {
		if !strings.Contains(output, field) {
			t.Errorf("Expected field '%s' in timing log output", field)
		}
	}
}

// TestLogError tests error logging
func TestLogError(t *testing.T) {
	var buf bytes.Buffer
	
	cfg := config.LoggingConfig{
		Level:  "error",
		Format: "json",
	}
	
	logger, err := NewLogger(cfg)
	if err != nil {
		t.Fatalf("Failed to create logger: %v", err)
	}
	
	// Replace the writer with our buffer
	handler := slog.NewJSONHandler(&buf, &slog.HandlerOptions{
		Level: slog.LevelError,
	})
	logger.Logger = slog.New(&CorrelationHandler{Handler: handler})
	
	// Test error logging
	testErr := &TestError{message: "test error"}
	ctx := context.Background()
	
	logger.LogError(ctx, "Operation failed", testErr, slog.String("component", "test"))
	
	output := buf.String()
	
	// Verify expected fields are present
	expectedFields := []string{
		"error",
		"error_type",
		"component",
		"test error",
		"Operation failed",
	}
	
	for _, field := range expectedFields {
		if !strings.Contains(output, field) {
			t.Errorf("Expected field '%s' in error log output", field)
		}
	}
}

// TestLogRequest tests request logging
func TestLogRequest(t *testing.T) {
	var buf bytes.Buffer
	
	cfg := config.LoggingConfig{
		Level:  "info",
		Format: "json",
	}
	
	logger, err := NewLogger(cfg)
	if err != nil {
		t.Fatalf("Failed to create logger: %v", err)
	}
	
	// Replace the writer with our buffer
	handler := slog.NewJSONHandler(&buf, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	})
	logger.Logger = slog.New(&CorrelationHandler{Handler: handler})
	
	// Test request logging
	ctx := context.Background()
	duration := 150 * time.Millisecond
	
	logger.LogRequest(ctx, "GET", "/api/sessions", 200, duration)
	
	output := buf.String()
	
	// Verify expected fields are present
	expectedFields := []string{
		"method",
		"path",
		"status_code",
		"duration",
		"type",
		"GET",
		"/api/sessions",
		"200",
		"Request processed",
	}
	
	for _, field := range expectedFields {
		if !strings.Contains(output, field) {
			t.Errorf("Expected field '%s' in request log output", field)
		}
	}
}

// TestGlobalDefaultLogger tests global logger functionality
func TestGlobalDefaultLogger(t *testing.T) {
	// Test default logger creation
	defaultLogger := Default()
	if defaultLogger == nil {
		t.Error("Expected default logger to be created")
	}
	
	// Create custom logger and set as default
	cfg := config.LoggingConfig{
		Level:  "debug",
		Format: "text",
	}
	
	customLogger, err := NewLogger(cfg)
	if err != nil {
		t.Fatalf("Failed to create custom logger: %v", err)
	}
	
	SetDefault(customLogger)
	
	// Verify default is now the custom logger
	if Default() != customLogger {
		t.Error("Expected default logger to be the custom logger")
	}
	
	// Test package-level functions
	Info("Test info message")
	Error("Test error message")
	Debug("Test debug message")
	Warn("Test warning message")
	
	// Test context versions
	ctx := context.Background()
	InfoContext(ctx, "Test info with context")
	ErrorContext(ctx, "Test error with context")
	DebugContext(ctx, "Test debug with context")
	WarnContext(ctx, "Test warning with context")
	
	customLogger.Close()
}

// TestError is a simple error type for testing
type TestError struct {
	message string
}

func (e *TestError) Error() string {
	return e.message
}

// BenchmarkLogger benchmarks logging performance
func BenchmarkLogger(b *testing.B) {
	cfg := config.LoggingConfig{
		Level:  "info",
		Format: "json",
	}
	
	logger, err := NewLogger(cfg)
	if err != nil {
		b.Fatalf("Failed to create logger: %v", err)
	}
	defer logger.Close()
	
	ctx := WithCorrelationID(context.Background(), "bench-test")
	
	b.ResetTimer()
	
	for i := 0; i < b.N; i++ {
		logger.InfoContext(ctx, "Benchmark message",
			slog.Int("iteration", i),
			slog.String("component", "benchmark"),
		)
	}
}

// BenchmarkLoggerWithError benchmarks error logging performance  
func BenchmarkLoggerWithError(b *testing.B) {
	cfg := config.LoggingConfig{
		Level:  "error",
		Format: "json",
	}
	
	logger, err := NewLogger(cfg)
	if err != nil {
		b.Fatalf("Failed to create logger: %v", err)
	}
	defer logger.Close()
	
	ctx := context.Background()
	testErr := &TestError{message: "benchmark error"}
	
	b.ResetTimer()
	
	for i := 0; i < b.N; i++ {
		logger.LogError(ctx, "Benchmark error", testErr,
			slog.Int("iteration", i),
		)
	}
}