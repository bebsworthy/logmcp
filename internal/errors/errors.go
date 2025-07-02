// Package errors provides structured error types for LogMCP.
//
// This package defines custom error types that provide better error handling,
// error categorization, and integration with the protocol layer for consistent
// error reporting across the application.
package errors

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"os"
	"runtime"
	"syscall"
	"time"

	"github.com/logmcp/logmcp/internal/protocol"
)

// ErrorType represents the category of error
type ErrorType string

const (
	ErrorTypeProcess    ErrorType = "process"
	ErrorTypeNetwork    ErrorType = "network"
	ErrorTypeValidation ErrorType = "validation"
	ErrorTypeFile       ErrorType = "file"
	ErrorTypeSession    ErrorType = "session"
	ErrorTypeProtocol   ErrorType = "protocol"
	ErrorTypeTimeout    ErrorType = "timeout"
	ErrorTypePermission ErrorType = "permission"
	ErrorTypeInternal   ErrorType = "internal"
)

// LogMCPError is the base error type for all LogMCP errors
type LogMCPError struct {
	Type       ErrorType
	Code       string
	Message    string
	Underlying error
	Details    map[string]interface{}
	Context    context.Context
	StackTrace []string
	Timestamp  time.Time
}

// Error implements the error interface
func (e *LogMCPError) Error() string {
	if e.Underlying != nil {
		return fmt.Sprintf("%s (%s): %s: %v", e.Type, e.Code, e.Message, e.Underlying)
	}
	return fmt.Sprintf("%s (%s): %s", e.Type, e.Code, e.Message)
}

// Unwrap returns the underlying error
func (e *LogMCPError) Unwrap() error {
	return e.Underlying
}

// Is checks if the error matches another error
func (e *LogMCPError) Is(target error) bool {
	if t, ok := target.(*LogMCPError); ok {
		return e.Type == t.Type && e.Code == t.Code
	}
	return false
}

// ToProtocolError converts the error to a protocol ErrorMessage
func (e *LogMCPError) ToProtocolError(sessionID string) *protocol.ErrorMessage {
	return protocol.NewErrorMessage(sessionID, e.Code, e.Message)
}

// WithDetails adds details to the error
func (e *LogMCPError) WithDetails(key string, value interface{}) *LogMCPError {
	if e.Details == nil {
		e.Details = make(map[string]interface{})
	}
	e.Details[key] = value
	return e
}

// Common error constructors

// ProcessError creates a process-related error
func ProcessError(code, message string, underlying error) *LogMCPError {
	return &LogMCPError{
		Type:       ErrorTypeProcess,
		Code:       code,
		Message:    message,
		Underlying: underlying,
	}
}

// NetworkError creates a network-related error
func NetworkError(code, message string, underlying error) *LogMCPError {
	return &LogMCPError{
		Type:       ErrorTypeNetwork,
		Code:       code,
		Message:    message,
		Underlying: underlying,
	}
}

// ValidationError creates a validation error
func ValidationError(code, message string, underlying error) *LogMCPError {
	return &LogMCPError{
		Type:       ErrorTypeValidation,
		Code:       code,
		Message:    message,
		Underlying: underlying,
	}
}

// FileError creates a file-related error
func FileError(code, message string, underlying error) *LogMCPError {
	return &LogMCPError{
		Type:       ErrorTypeFile,
		Code:       code,
		Message:    message,
		Underlying: underlying,
	}
}

// SessionError creates a session-related error
func SessionError(code, message string, underlying error) *LogMCPError {
	return &LogMCPError{
		Type:       ErrorTypeSession,
		Code:       code,
		Message:    message,
		Underlying: underlying,
	}
}

// ProtocolError creates a protocol-related error
func ProtocolError(code, message string, underlying error) *LogMCPError {
	return &LogMCPError{
		Type:       ErrorTypeProtocol,
		Code:       code,
		Message:    message,
		Underlying: underlying,
	}
}

// TimeoutError creates a timeout error
func TimeoutError(code, message string, underlying error) *LogMCPError {
	return &LogMCPError{
		Type:       ErrorTypeTimeout,
		Code:       code,
		Message:    message,
		Underlying: underlying,
	}
}

// PermissionError creates a permission error
func PermissionError(code, message string, underlying error) *LogMCPError {
	return &LogMCPError{
		Type:       ErrorTypePermission,
		Code:       code,
		Message:    message,
		Underlying: underlying,
	}
}

// InternalError creates an internal error
func InternalError(code, message string, underlying error) *LogMCPError {
	return &LogMCPError{
		Type:       ErrorTypeInternal,
		Code:       code,
		Message:    message,
		Underlying: underlying,
	}
}

// Predefined error instances

var (
	// Process errors
	ErrProcessNotFound     = ProcessError(protocol.ErrorCodeProcessNotFound, "Process not found", nil)
	ErrProcessFailed       = ProcessError(protocol.ErrorCodeProcessFailed, "Process execution failed", nil)
	ErrProcessTimeout      = TimeoutError(protocol.ErrorCodeTimeout, "Process startup timeout", nil)
	ErrProcessKilled       = ProcessError("PROCESS_KILLED", "Process was killed", nil)
	ErrProcessExited       = ProcessError("PROCESS_EXITED", "Process exited unexpectedly", nil)

	// Network errors
	ErrConnectionLost      = NetworkError(protocol.ErrorCodeConnectionLost, "Connection lost", nil)
	ErrConnectionRefused   = NetworkError("CONNECTION_REFUSED", "Connection refused", nil)
	ErrConnectionTimeout   = TimeoutError("CONNECTION_TIMEOUT", "Connection timeout", nil)

	// Session errors
	ErrSessionNotFound     = SessionError(protocol.ErrorCodeSessionNotFound, "Session not found", nil)
	ErrSessionExists       = SessionError("SESSION_EXISTS", "Session already exists", nil)
	ErrSessionClosed       = SessionError("SESSION_CLOSED", "Session is closed", nil)

	// Protocol errors
	ErrInvalidMessage      = ProtocolError(protocol.ErrorCodeInvalidMessage, "Invalid message format", nil)
	ErrInvalidCommand      = ProtocolError(protocol.ErrorCodeInvalidCommand, "Invalid command", nil)
	ErrUnsupportedCapability = ProtocolError(protocol.ErrorCodeCapabilityNotSupported, "Capability not supported", nil)

	// File errors
	ErrFileNotFound        = FileError("FILE_NOT_FOUND", "File not found", nil)
	ErrFilePermission      = PermissionError("FILE_PERMISSION", "File permission denied", nil)
	ErrFileRotated         = FileError("FILE_ROTATED", "File was rotated", nil)

	// Validation errors
	ErrInvalidInput        = ValidationError("INVALID_INPUT", "Invalid input", nil)
	ErrMissingRequired     = ValidationError("MISSING_REQUIRED", "Missing required field", nil)

	// Permission errors
	ErrPermissionDenied    = PermissionError(protocol.ErrorCodePermissionDenied, "Permission denied", nil)

	// Internal errors
	ErrInternalServerError = InternalError(protocol.ErrorCodeInternalError, "Internal server error", nil)
)

// Helper functions to classify standard Go errors

// ClassifyError attempts to classify a standard Go error into a LogMCP error
func ClassifyError(err error) *LogMCPError {
	if err == nil {
		return nil
	}

	// Check if it's already a LogMCP error
	if logmcpErr, ok := err.(*LogMCPError); ok {
		return logmcpErr
	}

	// Classify based on error type
	switch {
	case os.IsNotExist(err):
		return FileError("FILE_NOT_FOUND", "File not found", err)
	case os.IsPermission(err):
		return PermissionError("PERMISSION_DENIED", "Permission denied", err)
	case os.IsTimeout(err):
		return TimeoutError("TIMEOUT", "Operation timeout", err)
	case isProcessError(err):
		return ProcessError("PROCESS_ERROR", "Process error", err)
	case isNetworkError(err):
		return NetworkError("NETWORK_ERROR", "Network error", err)
	default:
		return InternalError("UNKNOWN_ERROR", "Unknown error", err)
	}
}

// isNetworkError checks if the error is network-related
func isNetworkError(err error) bool {
	if _, ok := err.(net.Error); ok {
		return true
	}
	if _, ok := err.(*net.OpError); ok {
		return true
	}
	return false
}

// isProcessError checks if the error is process-related
func isProcessError(err error) bool {
	if errno, ok := err.(syscall.Errno); ok {
		switch errno {
		case syscall.ESRCH, syscall.ECHILD, syscall.EPERM:
			return true
		}
	}
	return false
}

// WrapError wraps an existing error with additional context
func WrapError(err error, message string) *LogMCPError {
	if err == nil {
		return nil
	}

	classified := ClassifyError(err)
	if classified != nil {
		classified.Message = message + ": " + classified.Message
		return classified
	}

	return InternalError("WRAPPED_ERROR", message, err)
}

// NewErrorf creates a new LogMCP error with formatted message
func NewErrorf(errorType ErrorType, code, format string, args ...interface{}) *LogMCPError {
	return &LogMCPError{
		Type:    errorType,
		Code:    code,
		Message: fmt.Sprintf(format, args...),
	}
}

// IsType checks if an error is of a specific type
func IsType(err error, errorType ErrorType) bool {
	if logmcpErr, ok := err.(*LogMCPError); ok {
		return logmcpErr.Type == errorType
	}
	return false
}

// IsCode checks if an error has a specific code
func IsCode(err error, code string) bool {
	if logmcpErr, ok := err.(*LogMCPError); ok {
		return logmcpErr.Code == code
	}
	return false
}

// GetCode extracts the error code from an error
func GetCode(err error) string {
	if logmcpErr, ok := err.(*LogMCPError); ok {
		return logmcpErr.Code
	}
	return "UNKNOWN_ERROR"
}

// GetType extracts the error type from an error
func GetType(err error) ErrorType {
	if logmcpErr, ok := err.(*LogMCPError); ok {
		return logmcpErr.Type
	}
	return ErrorTypeInternal
}

// Enhanced error creation with context and stack trace

// newErrorWithContext creates a new LogMCP error with context and stack trace
func newErrorWithContext(ctx context.Context, errorType ErrorType, code, message string, underlying error) *LogMCPError {
	err := &LogMCPError{
		Type:       errorType,
		Code:       code,
		Message:    message,
		Underlying: underlying,
		Context:    ctx,
		Timestamp:  time.Now(),
		Details:    make(map[string]interface{}),
	}
	
	// Capture stack trace
	err.StackTrace = captureStackTrace(2) // Skip this function and the caller
	
	return err
}

// captureStackTrace captures the current stack trace
func captureStackTrace(skip int) []string {
	var stack []string
	pc := make([]uintptr, 16)
	n := runtime.Callers(skip+1, pc)
	
	frames := runtime.CallersFrames(pc[:n])
	for {
		frame, more := frames.Next()
		stack = append(stack, fmt.Sprintf("%s:%d %s", frame.File, frame.Line, frame.Function))
		if !more {
			break
		}
	}
	
	return stack
}

// Context-aware error constructors

// ProcessErrorCtx creates a process-related error with context
func ProcessErrorCtx(ctx context.Context, code, message string, underlying error) *LogMCPError {
	return newErrorWithContext(ctx, ErrorTypeProcess, code, message, underlying)
}

// NetworkErrorCtx creates a network-related error with context
func NetworkErrorCtx(ctx context.Context, code, message string, underlying error) *LogMCPError {
	return newErrorWithContext(ctx, ErrorTypeNetwork, code, message, underlying)
}

// ValidationErrorCtx creates a validation error with context
func ValidationErrorCtx(ctx context.Context, code, message string, underlying error) *LogMCPError {
	return newErrorWithContext(ctx, ErrorTypeValidation, code, message, underlying)
}

// FileErrorCtx creates a file-related error with context
func FileErrorCtx(ctx context.Context, code, message string, underlying error) *LogMCPError {
	return newErrorWithContext(ctx, ErrorTypeFile, code, message, underlying)
}

// SessionErrorCtx creates a session-related error with context
func SessionErrorCtx(ctx context.Context, code, message string, underlying error) *LogMCPError {
	return newErrorWithContext(ctx, ErrorTypeSession, code, message, underlying)
}

// ProtocolErrorCtx creates a protocol-related error with context
func ProtocolErrorCtx(ctx context.Context, code, message string, underlying error) *LogMCPError {
	return newErrorWithContext(ctx, ErrorTypeProtocol, code, message, underlying)
}

// TimeoutErrorCtx creates a timeout error with context
func TimeoutErrorCtx(ctx context.Context, code, message string, underlying error) *LogMCPError {
	return newErrorWithContext(ctx, ErrorTypeTimeout, code, message, underlying)
}

// PermissionErrorCtx creates a permission error with context
func PermissionErrorCtx(ctx context.Context, code, message string, underlying error) *LogMCPError {
	return newErrorWithContext(ctx, ErrorTypePermission, code, message, underlying)
}

// InternalErrorCtx creates an internal error with context
func InternalErrorCtx(ctx context.Context, code, message string, underlying error) *LogMCPError {
	return newErrorWithContext(ctx, ErrorTypeInternal, code, message, underlying)
}

// WrapErrorCtx wraps an existing error with additional context
func WrapErrorCtx(ctx context.Context, err error, message string) *LogMCPError {
	if err == nil {
		return nil
	}

	classified := ClassifyError(err)
	if classified != nil {
		classified.Message = message + ": " + classified.Message
		classified.Context = ctx
		classified.Timestamp = time.Now()
		if classified.StackTrace == nil {
			classified.StackTrace = captureStackTrace(1)
		}
		return classified
	}

	return newErrorWithContext(ctx, ErrorTypeInternal, "WRAPPED_ERROR", message, err)
}

// Logging integration

// LogAttrs returns slog attributes for the error
func (e *LogMCPError) LogAttrs() []slog.Attr {
	attrs := []slog.Attr{
		slog.String("error_type", string(e.Type)),
		slog.String("error_code", e.Code),
		slog.String("error_message", e.Message),
		slog.Time("error_timestamp", e.Timestamp),
	}
	
	if e.Underlying != nil {
		attrs = append(attrs, slog.String("underlying_error", e.Underlying.Error()))
	}
	
	// Add details
	for key, value := range e.Details {
		attrs = append(attrs, slog.Any(fmt.Sprintf("error_detail_%s", key), value))
	}
	
	// Add stack trace (first few frames only for brevity)
	if len(e.StackTrace) > 0 {
		maxFrames := 3
		if len(e.StackTrace) < maxFrames {
			maxFrames = len(e.StackTrace)
		}
		attrs = append(attrs, slog.Any("error_stack", e.StackTrace[:maxFrames]))
	}
	
	return attrs
}

// Performance and metrics helpers

// ErrorMetrics represents error metrics
type ErrorMetrics struct {
	Type      ErrorType `json:"type"`
	Code      string    `json:"code"`
	Count     int64     `json:"count"`
	LastSeen  time.Time `json:"last_seen"`
	Component string    `json:"component"`
}

// WithMetric adds metric information to an error
func (e *LogMCPError) WithMetric(component string) *LogMCPError {
	return e.WithDetails("component", component)
}

// WithOperation adds operation context to an error
func (e *LogMCPError) WithOperation(operation string) *LogMCPError {
	return e.WithDetails("operation", operation)
}

// WithDuration adds timing information to an error
func (e *LogMCPError) WithDuration(duration time.Duration) *LogMCPError {
	return e.WithDetails("duration", duration.String())
}

// Recovery helpers for goroutines

// RecoverError recovers from a panic and converts it to a LogMCP error
func RecoverError(ctx context.Context) *LogMCPError {
	if r := recover(); r != nil {
		var err error
		if e, ok := r.(error); ok {
			err = e
		} else {
			err = fmt.Errorf("panic: %v", r)
		}
		
		return newErrorWithContext(ctx, ErrorTypeInternal, "PANIC_RECOVERED", 
			"Recovered from panic", err)
	}
	return nil
}

// WithRecover wraps a function call with panic recovery
func WithRecover(ctx context.Context, fn func() error) (err error) {
	defer func() {
		if recovered := RecoverError(ctx); recovered != nil {
			err = recovered
		}
	}()
	
	return fn()
}