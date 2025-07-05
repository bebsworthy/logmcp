package errors

import (
	"errors"
	"net"
	"os"
	"syscall"
	"testing"

	"github.com/bebsworthy/logmcp/internal/protocol"
)

// TestLogMCPError_Error tests error string formatting
func TestLogMCPError_Error(t *testing.T) {
	// Test error without underlying error
	err := ProcessError("PROCESS_FAILED", "Process execution failed", nil)
	expected := "process (PROCESS_FAILED): Process execution failed"
	if err.Error() != expected {
		t.Errorf("Expected '%s', got '%s'", expected, err.Error())
	}
	
	// Test error with underlying error
	underlying := errors.New("original error")
	err2 := NetworkError("CONNECTION_FAILED", "Connection failed", underlying)
	expected2 := "network (CONNECTION_FAILED): Connection failed: original error"
	if err2.Error() != expected2 {
		t.Errorf("Expected '%s', got '%s'", expected2, err2.Error())
	}
}

// TestLogMCPError_Unwrap tests error unwrapping
func TestLogMCPError_Unwrap(t *testing.T) {
	underlying := errors.New("original error")
	err := ProcessError("PROCESS_FAILED", "Process execution failed", underlying)
	
	unwrapped := err.Unwrap()
	if unwrapped != underlying {
		t.Errorf("Expected unwrapped error to be original error")
	}
	
	// Test without underlying error
	err2 := ProcessError("PROCESS_FAILED", "Process execution failed", nil)
	unwrapped2 := err2.Unwrap()
	if unwrapped2 != nil {
		t.Errorf("Expected unwrapped error to be nil")
	}
}

// TestLogMCPError_Is tests error comparison
func TestLogMCPError_Is(t *testing.T) {
	err1 := ProcessError("PROCESS_FAILED", "Process execution failed", nil)
	err2 := ProcessError("PROCESS_FAILED", "Process execution failed", nil)
	err3 := NetworkError("CONNECTION_FAILED", "Connection failed", nil)
	
	if !err1.Is(err2) {
		t.Error("Expected errors with same type and code to be equal")
	}
	
	if err1.Is(err3) {
		t.Error("Expected errors with different type/code to not be equal")
	}
	
	// Test with non-LogMCP error
	regularErr := errors.New("regular error")
	if err1.Is(regularErr) {
		t.Error("Expected LogMCP error to not match regular error")
	}
}

// TestLogMCPError_ToProtocolError tests protocol error conversion
func TestLogMCPError_ToProtocolError(t *testing.T) {
	err := ProcessError("PROCESS_FAILED", "Process execution failed", nil)
	protocolErr := err.ToProtocolError("test-label")
	
	if protocolErr.Type != protocol.MessageTypeError {
		t.Errorf("Expected message type error, got %s", protocolErr.Type)
	}
	
	if protocolErr.Label != "test-label" {
		t.Errorf("Expected label 'test-label', got '%s'", protocolErr.Label)
	}
	
	if protocolErr.ErrorCode != "PROCESS_FAILED" {
		t.Errorf("Expected error code 'PROCESS_FAILED', got '%s'", protocolErr.ErrorCode)
	}
	
	if protocolErr.Message != "Process execution failed" {
		t.Errorf("Expected message 'Process execution failed', got '%s'", protocolErr.Message)
	}
}

// TestLogMCPError_WithDetails tests adding details to error
func TestLogMCPError_WithDetails(t *testing.T) {
	err := ProcessError("PROCESS_FAILED", "Process execution failed", nil)
	err.WithDetails("pid", 1234)
	err.WithDetails("command", "npm run server")
	
	if err.Details["pid"] != 1234 {
		t.Errorf("Expected pid detail to be 1234, got %v", err.Details["pid"])
	}
	
	if err.Details["command"] != "npm run server" {
		t.Errorf("Expected command detail to be 'npm run server', got %v", err.Details["command"])
	}
}

// TestErrorConstructors tests all error constructor functions
func TestErrorConstructors(t *testing.T) {
	underlying := errors.New("test underlying")
	
	tests := []struct {
		name        string
		constructor func(string, string, error) *LogMCPError
		expectedType ErrorType
	}{
		{"ProcessError", ProcessError, ErrorTypeProcess},
		{"NetworkError", NetworkError, ErrorTypeNetwork},
		{"ValidationError", ValidationError, ErrorTypeValidation},
		{"FileError", FileError, ErrorTypeFile},
		{"SessionError", SessionError, ErrorTypeSession},
		{"ProtocolError", ProtocolError, ErrorTypeProtocol},
		{"TimeoutError", TimeoutError, ErrorTypeTimeout},
		{"PermissionError", PermissionError, ErrorTypePermission},
		{"InternalError", InternalError, ErrorTypeInternal},
	}
	
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := test.constructor("TEST_CODE", "Test message", underlying)
			
			if err.Type != test.expectedType {
				t.Errorf("Expected type %s, got %s", test.expectedType, err.Type)
			}
			
			if err.Code != "TEST_CODE" {
				t.Errorf("Expected code 'TEST_CODE', got '%s'", err.Code)
			}
			
			if err.Message != "Test message" {
				t.Errorf("Expected message 'Test message', got '%s'", err.Message)
			}
			
			if err.Underlying != underlying {
				t.Errorf("Expected underlying error to be set")
			}
		})
	}
}

// TestPredefinedErrors tests predefined error instances
func TestPredefinedErrors(t *testing.T) {
	tests := []struct {
		name string
		err  *LogMCPError
		expectedType ErrorType
		expectedCode string
	}{
		{"ErrProcessNotFound", ErrProcessNotFound, ErrorTypeProcess, protocol.ErrorCodeProcessNotFound},
		{"ErrProcessFailed", ErrProcessFailed, ErrorTypeProcess, protocol.ErrorCodeProcessFailed},
		{"ErrConnectionLost", ErrConnectionLost, ErrorTypeNetwork, protocol.ErrorCodeConnectionLost},
		{"ErrSessionNotFound", ErrSessionNotFound, ErrorTypeSession, protocol.ErrorCodeSessionNotFound},
		{"ErrInvalidMessage", ErrInvalidMessage, ErrorTypeProtocol, protocol.ErrorCodeInvalidMessage},
		{"ErrFileNotFound", ErrFileNotFound, ErrorTypeFile, "FILE_NOT_FOUND"},
		{"ErrPermissionDenied", ErrPermissionDenied, ErrorTypePermission, protocol.ErrorCodePermissionDenied},
		{"ErrInternalServerError", ErrInternalServerError, ErrorTypeInternal, protocol.ErrorCodeInternalError},
	}
	
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if test.err.Type != test.expectedType {
				t.Errorf("Expected type %s, got %s", test.expectedType, test.err.Type)
			}
			
			if test.err.Code != test.expectedCode {
				t.Errorf("Expected code '%s', got '%s'", test.expectedCode, test.err.Code)
			}
		})
	}
}

// TestClassifyError tests error classification
func TestClassifyError(t *testing.T) {
	// Test with nil error
	result := ClassifyError(nil)
	if result != nil {
		t.Error("Expected nil result for nil error")
	}
	
	// Test with already classified error
	original := ProcessError("TEST_CODE", "Test message", nil)
	result = ClassifyError(original)
	if result != original {
		t.Error("Expected same error instance for already classified error")
	}
	
	// Test with os.ErrNotExist
	result = ClassifyError(os.ErrNotExist)
	if result.Type != ErrorTypeFile || result.Code != "FILE_NOT_FOUND" {
		t.Errorf("Expected file not found error, got %s/%s", result.Type, result.Code)
	}
	
	// Test with permission error
	result = ClassifyError(os.ErrPermission)
	if result.Type != ErrorTypePermission {
		t.Errorf("Expected permission error, got %s", result.Type)
	}
	
	// Test with timeout error (mock timeout error)
	timeoutErr := os.ErrDeadlineExceeded
	result = ClassifyError(timeoutErr)
	if result.Type != ErrorTypeTimeout {
		t.Errorf("Expected timeout error, got %s", result.Type)
	}
	
	// Test with network error
	netErr := &net.OpError{Op: "dial", Err: errors.New("connection refused")}
	result = ClassifyError(netErr)
	if result.Type != ErrorTypeNetwork {
		t.Errorf("Expected network error, got %s", result.Type)
	}
	
	// Test with process error
	processErr := syscall.ESRCH // No such process
	result = ClassifyError(processErr)
	if result.Type != ErrorTypeProcess {
		t.Errorf("Expected process error, got %s", result.Type)
	}
	
	// Test with unknown error
	unknownErr := errors.New("unknown error")
	result = ClassifyError(unknownErr)
	if result.Type != ErrorTypeInternal || result.Code != "UNKNOWN_ERROR" {
		t.Errorf("Expected internal unknown error, got %s/%s", result.Type, result.Code)
	}
}

// TestWrapError tests error wrapping
func TestWrapError(t *testing.T) {
	// Test with nil error
	result := WrapError(nil, "context message")
	if result != nil {
		t.Error("Expected nil result for nil error")
	}
	
	// Test with regular error
	originalErr := errors.New("original error")
	result = WrapError(originalErr, "context message")
	if result == nil {
		t.Fatal("Expected wrapped error")
	}
	
	if result.Underlying != originalErr {
		t.Error("Expected underlying error to be preserved")
	}
	
	// Test with already classified error
	classifiedErr := ProcessError("PROCESS_FAILED", "Process failed", nil)
	result = WrapError(classifiedErr, "context message")
	if result.Type != ErrorTypeProcess {
		t.Error("Expected to preserve error type")
	}
}

// TestNewErrorf tests formatted error creation
func TestNewErrorf(t *testing.T) {
	err := NewErrorf(ErrorTypeValidation, "INVALID_INPUT", "Invalid value: %d", 42)
	
	if err.Type != ErrorTypeValidation {
		t.Errorf("Expected validation error type, got %s", err.Type)
	}
	
	if err.Code != "INVALID_INPUT" {
		t.Errorf("Expected code 'INVALID_INPUT', got '%s'", err.Code)
	}
	
	if err.Message != "Invalid value: 42" {
		t.Errorf("Expected formatted message 'Invalid value: 42', got '%s'", err.Message)
	}
}

// TestIsType tests error type checking
func TestIsType(t *testing.T) {
	processErr := ProcessError("PROCESS_FAILED", "Process failed", nil)
	regularErr := errors.New("regular error")
	
	if !IsType(processErr, ErrorTypeProcess) {
		t.Error("Expected process error to match process type")
	}
	
	if IsType(processErr, ErrorTypeNetwork) {
		t.Error("Expected process error to not match network type")
	}
	
	if IsType(regularErr, ErrorTypeProcess) {
		t.Error("Expected regular error to not match any type")
	}
}

// TestIsCode tests error code checking
func TestIsCode(t *testing.T) {
	processErr := ProcessError("PROCESS_FAILED", "Process failed", nil)
	regularErr := errors.New("regular error")
	
	if !IsCode(processErr, "PROCESS_FAILED") {
		t.Error("Expected process error to match process failed code")
	}
	
	if IsCode(processErr, "OTHER_CODE") {
		t.Error("Expected process error to not match other code")
	}
	
	if IsCode(regularErr, "PROCESS_FAILED") {
		t.Error("Expected regular error to not match any code")
	}
}

// TestGetCode tests error code extraction
func TestGetCode(t *testing.T) {
	processErr := ProcessError("PROCESS_FAILED", "Process failed", nil)
	regularErr := errors.New("regular error")
	
	code := GetCode(processErr)
	if code != "PROCESS_FAILED" {
		t.Errorf("Expected code 'PROCESS_FAILED', got '%s'", code)
	}
	
	code = GetCode(regularErr)
	if code != "UNKNOWN_ERROR" {
		t.Errorf("Expected code 'UNKNOWN_ERROR' for regular error, got '%s'", code)
	}
}

// TestGetType tests error type extraction
func TestGetType(t *testing.T) {
	processErr := ProcessError("PROCESS_FAILED", "Process failed", nil)
	regularErr := errors.New("regular error")
	
	errorType := GetType(processErr)
	if errorType != ErrorTypeProcess {
		t.Errorf("Expected type %s, got %s", ErrorTypeProcess, errorType)
	}
	
	errorType = GetType(regularErr)
	if errorType != ErrorTypeInternal {
		t.Errorf("Expected type %s for regular error, got %s", ErrorTypeInternal, errorType)
	}
}

// BenchmarkErrorCreation benchmarks error creation
func BenchmarkErrorCreation(b *testing.B) {
	for i := 0; i < b.N; i++ {
		ProcessError("PROCESS_FAILED", "Process execution failed", nil)
	}
}

// BenchmarkClassifyError benchmarks error classification
func BenchmarkClassifyError(b *testing.B) {
	err := errors.New("test error")
	
	b.ResetTimer()
	
	for i := 0; i < b.N; i++ {
		ClassifyError(err)
	}
}