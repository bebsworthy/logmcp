// Package logging provides structured logging functionality for LogMCP.
//
// This package implements a centralized logging system with:
// - Structured logging using Go's slog package
// - Configurable log levels and output formats
// - Request correlation and tracing
// - Performance metrics and timing
// - Integration with LogMCP configuration system
//
// Example usage:
//
//	logger := logging.NewLogger(config.Logging)
//	logger.Info("Server started", "port", 8765, "host", "localhost")
//	logger.Error("Connection failed", "error", err, "server", "ws://localhost:8765")
//
//	// With context for correlation
//	ctx = logging.WithCorrelationID(ctx, "req-123")
//	logger.InfoContext(ctx, "Processing request", "sessionID", sessionID)
//
package logging

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/bebsworthy/logmcp/internal/config"
)

// CorrelationIDKey is the context key for correlation IDs
type CorrelationIDKey struct{}

// Logger wraps slog.Logger with LogMCP-specific functionality
type Logger struct {
	*slog.Logger
	config config.LoggingConfig
	writer io.Writer
}

// LogLevel represents log levels
type LogLevel = slog.Level

const (
	LevelDebug = slog.LevelDebug
	LevelInfo  = slog.LevelInfo
	LevelWarn  = slog.LevelWarn
	LevelError = slog.LevelError
)

// NewLogger creates a new structured logger with the given configuration
func NewLogger(cfg config.LoggingConfig) (*Logger, error) {
	// Parse log level
	level, err := parseLogLevel(cfg.Level)
	if err != nil {
		return nil, fmt.Errorf("invalid log level %q: %w", cfg.Level, err)
	}

	// Set up output writer
	writer, err := createLogWriter(cfg.OutputFile)
	if err != nil {
		return nil, fmt.Errorf("failed to create log writer: %w", err)
	}

	// Create handler based on format
	var handler slog.Handler
	opts := &slog.HandlerOptions{
		Level: level,
		AddSource: cfg.Verbose,
		ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {
			// Add correlation ID to all log entries if present in context
			if a.Key == slog.TimeKey {
				// Format time consistently
				if t, ok := a.Value.Any().(time.Time); ok {
					return slog.String(slog.TimeKey, t.Format(time.RFC3339))
				}
			}
			return a
		},
	}

	switch strings.ToLower(cfg.Format) {
	case "json":
		handler = slog.NewJSONHandler(writer, opts)
	case "text":
		handler = slog.NewTextHandler(writer, opts)
	default:
		return nil, fmt.Errorf("unsupported log format %q", cfg.Format)
	}

	// Wrap handler to add correlation ID support
	handler = &CorrelationHandler{Handler: handler}

	logger := &Logger{
		Logger: slog.New(handler),
		config: cfg,
		writer: writer,
	}

	return logger, nil
}

// parseLogLevel converts string log level to slog.Level
func parseLogLevel(level string) (slog.Level, error) {
	switch strings.ToLower(level) {
	case "debug":
		return slog.LevelDebug, nil
	case "info":
		return slog.LevelInfo, nil
	case "warn", "warning":
		return slog.LevelWarn, nil
	case "error":
		return slog.LevelError, nil
	default:
		return slog.LevelInfo, fmt.Errorf("unknown log level: %s", level)
	}
}

// createLogWriter creates the appropriate writer for log output
func createLogWriter(outputFile string) (io.Writer, error) {
	if outputFile == "" {
		return os.Stderr, nil
	}

	// Ensure directory exists
	dir := filepath.Dir(outputFile)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create log directory %q: %w", dir, err)
	}

	// Open file for writing (append mode)
	file, err := os.OpenFile(outputFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return nil, fmt.Errorf("failed to open log file %q: %w", outputFile, err)
	}

	return file, nil
}

// CorrelationHandler wraps another handler to add correlation ID support
type CorrelationHandler struct {
	slog.Handler
}

// Handle processes log records and adds correlation ID if present in context
func (h *CorrelationHandler) Handle(ctx context.Context, r slog.Record) error {
	if correlationID := GetCorrelationID(ctx); correlationID != "" {
		r.AddAttrs(slog.String("correlation_id", correlationID))
	}
	return h.Handler.Handle(ctx, r)
}

// WithAttrs returns a new handler with the given attributes
func (h *CorrelationHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &CorrelationHandler{Handler: h.Handler.WithAttrs(attrs)}
}

// WithGroup returns a new handler with the given group
func (h *CorrelationHandler) WithGroup(name string) slog.Handler {
	return &CorrelationHandler{Handler: h.Handler.WithGroup(name)}
}

// Correlation ID helpers

// WithCorrelationID adds a correlation ID to the context
func WithCorrelationID(ctx context.Context, correlationID string) context.Context {
	return context.WithValue(ctx, CorrelationIDKey{}, correlationID)
}

// GetCorrelationID retrieves the correlation ID from the context
func GetCorrelationID(ctx context.Context) string {
	if id, ok := ctx.Value(CorrelationIDKey{}).(string); ok {
		return id
	}
	return ""
}

// Component-specific logger creation

// NewServerLogger creates a logger for the LogMCP server
func NewServerLogger(cfg config.LoggingConfig) (*Logger, error) {
	logger, err := NewLogger(cfg)
	if err != nil {
		return nil, err
	}
	
	// Add server-specific attributes
	logger.Logger = logger.Logger.With(
		slog.String("component", "server"),
		slog.String("service", "logmcp"),
	)
	
	return logger, nil
}

// NewRunnerLogger creates a logger for process runners
func NewRunnerLogger(cfg config.LoggingConfig, label string) (*Logger, error) {
	logger, err := NewLogger(cfg)
	if err != nil {
		return nil, err
	}
	
	// Add runner-specific attributes
	logger.Logger = logger.Logger.With(
		slog.String("component", "runner"),
		slog.String("service", "logmcp"),
		slog.String("label", label),
	)
	
	return logger, nil
}

// NewForwarderLogger creates a logger for log forwarders
func NewForwarderLogger(cfg config.LoggingConfig, label, source string) (*Logger, error) {
	logger, err := NewLogger(cfg)
	if err != nil {
		return nil, err
	}
	
	// Add forwarder-specific attributes
	logger.Logger = logger.Logger.With(
		slog.String("component", "forwarder"),
		slog.String("service", "logmcp"),
		slog.String("label", label),
		slog.String("source", source),
	)
	
	return logger, nil
}

// Performance logging helpers

// LogTiming logs the duration of an operation
func (l *Logger) LogTiming(ctx context.Context, operation string, start time.Time, attrs ...slog.Attr) {
	duration := time.Since(start)
	
	allAttrs := []slog.Attr{
		slog.String("operation", operation),
		slog.Duration("duration", duration),
		slog.String("performance", "timing"),
	}
	allAttrs = append(allAttrs, attrs...)
	
	l.LogAttrs(ctx, slog.LevelInfo, "Operation completed", allAttrs...)
}

// LogError logs an error with proper context and error details
func (l *Logger) LogError(ctx context.Context, msg string, err error, attrs ...slog.Attr) {
	allAttrs := []slog.Attr{
		slog.String("error", err.Error()),
		slog.String("error_type", fmt.Sprintf("%T", err)),
	}
	allAttrs = append(allAttrs, attrs...)
	
	l.LogAttrs(ctx, slog.LevelError, msg, allAttrs...)
}

// LogRequest logs request/response information
func (l *Logger) LogRequest(ctx context.Context, method, path string, statusCode int, duration time.Duration, attrs ...slog.Attr) {
	allAttrs := []slog.Attr{
		slog.String("method", method),
		slog.String("path", path),
		slog.Int("status_code", statusCode),
		slog.Duration("duration", duration),
		slog.String("type", "request"),
	}
	allAttrs = append(allAttrs, attrs...)
	
	level := slog.LevelInfo
	if statusCode >= 400 {
		level = slog.LevelWarn
	}
	if statusCode >= 500 {
		level = slog.LevelError
	}
	
	l.LogAttrs(ctx, level, "Request processed", allAttrs...)
}

// Close closes any file resources used by the logger
func (l *Logger) Close() error {
	if closer, ok := l.writer.(io.Closer); ok {
		return closer.Close()
	}
	return nil
}

// Global logger management

var defaultLogger *Logger

// SetDefault sets the default logger instance
func SetDefault(logger *Logger) {
	defaultLogger = logger
}

// Default returns the default logger instance
func Default() *Logger {
	if defaultLogger == nil {
		// Create a basic logger if none is set
		cfg := config.LoggingConfig{
			Level:  "info",
			Format: "text",
		}
		logger, _ := NewLogger(cfg)
		return logger
	}
	return defaultLogger
}

// Package-level convenience functions

// Info logs at Info level using the default logger
func Info(msg string, args ...any) {
	Default().Info(msg, args...)
}

// InfoContext logs at Info level with context using the default logger
func InfoContext(ctx context.Context, msg string, args ...any) {
	Default().InfoContext(ctx, msg, args...)
}

// Error logs at Error level using the default logger
func Error(msg string, args ...any) {
	Default().Error(msg, args...)
}

// ErrorContext logs at Error level with context using the default logger
func ErrorContext(ctx context.Context, msg string, args ...any) {
	Default().ErrorContext(ctx, msg, args...)
}

// Debug logs at Debug level using the default logger
func Debug(msg string, args ...any) {
	Default().Debug(msg, args...)
}

// DebugContext logs at Debug level with context using the default logger
func DebugContext(ctx context.Context, msg string, args ...any) {
	Default().DebugContext(ctx, msg, args...)
}

// Warn logs at Warn level using the default logger
func Warn(msg string, args ...any) {
	Default().Warn(msg, args...)
}

// WarnContext logs at Warn level with context using the default logger
func WarnContext(ctx context.Context, msg string, args ...any) {
	Default().WarnContext(ctx, msg, args...)
}