package protocol

import (
	"encoding/json"
	"testing"
)

func TestParseMessage(t *testing.T) {
	// Test LogMessage parsing
	logJSON := `{
		"type": "log",
		"session_id": "test-session",
		"label": "test-label",
		"content": "test content",
		"timestamp": "2025-06-30T10:30:00Z",
		"stream": "stdout",
		"pid": 1234
	}`

	msg, err := ParseMessage([]byte(logJSON))
	if err != nil {
		t.Fatalf("Failed to parse log message: %v", err)
	}

	logMsg, ok := msg.(*LogMessage)
	if !ok {
		t.Fatalf("Expected LogMessage, got %T", msg)
	}

	if logMsg.Type != MessageTypeLog {
		t.Errorf("Expected type %s, got %s", MessageTypeLog, logMsg.Type)
	}
	if logMsg.SessionID != "test-session" {
		t.Errorf("Expected session_id 'test-session', got '%s'", logMsg.SessionID)
	}
	if logMsg.Content != "test content" {
		t.Errorf("Expected content 'test content', got '%s'", logMsg.Content)
	}
	if logMsg.Stream != StreamStdout {
		t.Errorf("Expected stream %s, got %s", StreamStdout, logMsg.Stream)
	}
	if logMsg.PID != 1234 {
		t.Errorf("Expected PID 1234, got %d", logMsg.PID)
	}
}

func TestSerializeMessage(t *testing.T) {
	// Test LogMessage serialization
	logMsg := NewLogMessage("test-session", "test-label", "test content", StreamStdout, 1234)

	data, err := SerializeMessage(logMsg)
	if err != nil {
		t.Fatalf("Failed to serialize log message: %v", err)
	}

	// Parse it back to verify
	var parsed map[string]interface{}
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("Failed to parse serialized message: %v", err)
	}

	if parsed["type"] != string(MessageTypeLog) {
		t.Errorf("Expected type %s, got %v", MessageTypeLog, parsed["type"])
	}
	if parsed["session_id"] != "test-session" {
		t.Errorf("Expected session_id 'test-session', got %v", parsed["session_id"])
	}
}

func TestValidateMessage(t *testing.T) {
	// Test valid message
	validMsg := NewLogMessage("test-session", "test-label", "test content", StreamStdout, 1234)
	if err := ValidateMessage(validMsg); err != nil {
		t.Errorf("Valid message failed validation: %v", err)
	}

	// Test invalid message (missing session_id)
	invalidMsg := &LogMessage{
		BaseMessage: BaseMessage{Type: MessageTypeLog},
		Label:       "test-label",
		Content:     "test content",
		Stream:      StreamStdout,
	}
	if err := ValidateMessage(invalidMsg); err == nil {
		t.Error("Invalid message passed validation")
	}
}

func TestCommandMessage(t *testing.T) {
	signal := SignalTERM
	cmdMsg := NewCommandMessage("test-session", ActionSignal, &signal)

	if cmdMsg.Type != MessageTypeCommand {
		t.Errorf("Expected type %s, got %s", MessageTypeCommand, cmdMsg.Type)
	}
	if cmdMsg.Action != ActionSignal {
		t.Errorf("Expected action %s, got %s", ActionSignal, cmdMsg.Action)
	}
	if *cmdMsg.Signal != SignalTERM {
		t.Errorf("Expected signal %s, got %s", SignalTERM, *cmdMsg.Signal)
	}

	// Test validation
	if err := ValidateMessage(cmdMsg); err != nil {
		t.Errorf("Valid command message failed validation: %v", err)
	}
}

func TestErrorMessage(t *testing.T) {
	errMsg := NewErrorMessage("test-session", ErrorCodeProcessNotFound, "Process not found")

	if errMsg.Type != MessageTypeError {
		t.Errorf("Expected type %s, got %s", MessageTypeError, errMsg.Type)
	}
	if errMsg.ErrorCode != ErrorCodeProcessNotFound {
		t.Errorf("Expected error code %s, got %s", ErrorCodeProcessNotFound, errMsg.ErrorCode)
	}

	// Test validation
	if err := ValidateMessage(errMsg); err != nil {
		t.Errorf("Valid error message failed validation: %v", err)
	}
}