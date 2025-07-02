package protocol

import (
	"encoding/json"
	"fmt"
	"time"
)

// MessageType represents the type of WebSocket message
type MessageType string

const (
	MessageTypeLog        MessageType = "log"
	MessageTypeCommand    MessageType = "command"
	MessageTypeStdin      MessageType = "stdin"
	MessageTypeAck        MessageType = "ack"
	MessageTypeError      MessageType = "error"
	MessageTypeStatus     MessageType = "status"
	MessageTypeRegister   MessageType = "register"
)

// StreamType represents stdout or stderr
type StreamType string

const (
	StreamStdout StreamType = "stdout"
	StreamStderr StreamType = "stderr"
)

// SessionStatus represents the current status of a session
type SessionStatus string

const (
	StatusRunning    SessionStatus = "running"
	StatusStopped    SessionStatus = "stopped"
	StatusCrashed    SessionStatus = "crashed"
	StatusRestarting SessionStatus = "restarting"
)

// CommandAction represents actions that can be sent to runners
type CommandAction string

const (
	ActionRestart CommandAction = "restart"
	ActionSignal  CommandAction = "signal"
)

// Signal types for process control
type Signal string

const (
	SignalTERM Signal = "SIGTERM"
	SignalKILL Signal = "SIGKILL"
	SignalINT  Signal = "SIGINT"
	SignalHUP  Signal = "SIGHUP"
	SignalUSR1 Signal = "SIGUSR1"
	SignalUSR2 Signal = "SIGUSR2"
)

// BaseMessage contains common fields for all message types
type BaseMessage struct {
	Type      MessageType `json:"type" validate:"required"`
	SessionID string      `json:"session_id" validate:"required"`
}

// LogMessage represents a log entry from runner to server
type LogMessage struct {
	BaseMessage
	Label     string    `json:"label" validate:"required"`
	Content   string    `json:"content" validate:"required"`
	Timestamp time.Time `json:"timestamp" validate:"required"`
	Stream    StreamType `json:"stream" validate:"required,oneof=stdout stderr"`
	PID       int       `json:"pid"`
}

// CommandMessage represents a command from server to runner
type CommandMessage struct {
	BaseMessage
	Action    CommandAction `json:"action" validate:"required,oneof=restart signal"`
	Signal    *Signal       `json:"signal,omitempty" validate:"omitempty,oneof=SIGTERM SIGKILL SIGINT SIGHUP SIGUSR1 SIGUSR2"`
	CommandID string        `json:"command_id,omitempty"`
}

// StdinMessage represents input to be sent to a process
type StdinMessage struct {
	BaseMessage
	Input string `json:"input" validate:"required"`
}

// AckMessage represents acknowledgment of a command
type AckMessage struct {
	BaseMessage
	CommandID string `json:"command_id,omitempty"`
	Success   bool   `json:"success"`
	Message   string `json:"message,omitempty"`
	NewPID    *int   `json:"new_pid,omitempty"`
}

// ErrorMessage represents an error in communication
type ErrorMessage struct {
	BaseMessage
	ErrorCode string                 `json:"error_code" validate:"required"`
	Message   string                 `json:"message" validate:"required"`
	Details   map[string]interface{} `json:"details,omitempty"`
}

// StatusMessage represents process status updates
type StatusMessage struct {
	BaseMessage
	Status   SessionStatus `json:"status" validate:"required,oneof=running stopped crashed restarting"`
	PID      *int          `json:"pid,omitempty"`
	ExitCode *int          `json:"exit_code,omitempty"`
	Message  string        `json:"message,omitempty"`
}

// SessionRegistrationMessage represents a runner registering with the server
type SessionRegistrationMessage struct {
	BaseMessage
	Label        string   `json:"label" validate:"required"`
	Command      string   `json:"command,omitempty"`
	WorkingDir   string   `json:"working_dir,omitempty"`
	Capabilities []string `json:"capabilities,omitempty"`
}

// Message represents any WebSocket message
type Message struct {
	Type      MessageType     `json:"type"`
	SessionID string          `json:"session_id,omitempty"`
	Data      json.RawMessage `json:",inline"`
}

// ParseMessage parses a raw JSON message into the appropriate struct
func ParseMessage(data []byte) (interface{}, error) {
	var base BaseMessage
	if err := json.Unmarshal(data, &base); err != nil {
		return nil, fmt.Errorf("failed to parse base message: %w", err)
	}

	switch base.Type {
	case MessageTypeLog:
		var msg LogMessage
		if err := json.Unmarshal(data, &msg); err != nil {
			return nil, fmt.Errorf("failed to parse log message: %w", err)
		}
		return &msg, nil

	case MessageTypeCommand:
		var msg CommandMessage
		if err := json.Unmarshal(data, &msg); err != nil {
			return nil, fmt.Errorf("failed to parse command message: %w", err)
		}
		return &msg, nil

	case MessageTypeStdin:
		var msg StdinMessage
		if err := json.Unmarshal(data, &msg); err != nil {
			return nil, fmt.Errorf("failed to parse stdin message: %w", err)
		}
		return &msg, nil

	case MessageTypeAck:
		var msg AckMessage
		if err := json.Unmarshal(data, &msg); err != nil {
			return nil, fmt.Errorf("failed to parse ack message: %w", err)
		}
		return &msg, nil

	case MessageTypeError:
		var msg ErrorMessage
		if err := json.Unmarshal(data, &msg); err != nil {
			return nil, fmt.Errorf("failed to parse error message: %w", err)
		}
		return &msg, nil

	case MessageTypeStatus:
		var msg StatusMessage
		if err := json.Unmarshal(data, &msg); err != nil {
			return nil, fmt.Errorf("failed to parse status message: %w", err)
		}
		return &msg, nil

	case MessageTypeRegister:
		var msg SessionRegistrationMessage
		if err := json.Unmarshal(data, &msg); err != nil {
			return nil, fmt.Errorf("failed to parse registration message: %w", err)
		}
		return &msg, nil

	default:
		return nil, fmt.Errorf("unknown message type: %s", base.Type)
	}
}

// SerializeMessage serializes a message struct to JSON
func SerializeMessage(msg interface{}) ([]byte, error) {
	data, err := json.Marshal(msg)
	if err != nil {
		return nil, fmt.Errorf("failed to serialize message: %w", err)
	}
	return data, nil
}

// ValidateMessage performs basic validation on a message
func ValidateMessage(msg interface{}) error {
	switch m := msg.(type) {
	case *LogMessage:
		if m.Type != MessageTypeLog {
			return fmt.Errorf("invalid message type for LogMessage")
		}
		if m.SessionID == "" {
			return fmt.Errorf("session_id is required")
		}
		if m.Label == "" {
			return fmt.Errorf("label is required")
		}
		if m.Content == "" {
			return fmt.Errorf("content is required")
		}
		if m.Stream != StreamStdout && m.Stream != StreamStderr {
			return fmt.Errorf("stream must be stdout or stderr")
		}

	case *CommandMessage:
		if m.Type != MessageTypeCommand {
			return fmt.Errorf("invalid message type for CommandMessage")
		}
		if m.SessionID == "" {
			return fmt.Errorf("session_id is required")
		}
		if m.Action != ActionRestart && m.Action != ActionSignal {
			return fmt.Errorf("action must be restart or signal")
		}
		if m.Action == ActionSignal && m.Signal == nil {
			return fmt.Errorf("signal is required when action is signal")
		}

	case *StdinMessage:
		if m.Type != MessageTypeStdin {
			return fmt.Errorf("invalid message type for StdinMessage")
		}
		if m.SessionID == "" {
			return fmt.Errorf("session_id is required")
		}
		if m.Input == "" {
			return fmt.Errorf("input is required")
		}

	case *AckMessage:
		if m.Type != MessageTypeAck {
			return fmt.Errorf("invalid message type for AckMessage")
		}
		if m.SessionID == "" {
			return fmt.Errorf("session_id is required")
		}

	case *ErrorMessage:
		if m.Type != MessageTypeError {
			return fmt.Errorf("invalid message type for ErrorMessage")
		}
		if m.SessionID == "" {
			return fmt.Errorf("session_id is required")
		}
		if m.ErrorCode == "" {
			return fmt.Errorf("error_code is required")
		}
		if m.Message == "" {
			return fmt.Errorf("message is required")
		}

	case *StatusMessage:
		if m.Type != MessageTypeStatus {
			return fmt.Errorf("invalid message type for StatusMessage")
		}
		if m.SessionID == "" {
			return fmt.Errorf("session_id is required")
		}
		if m.Status != StatusRunning && m.Status != StatusStopped && 
		   m.Status != StatusCrashed && m.Status != StatusRestarting {
			return fmt.Errorf("invalid status value")
		}

	case *SessionRegistrationMessage:
		if m.Type != MessageTypeRegister {
			return fmt.Errorf("invalid message type for SessionRegistrationMessage")
		}
		if m.SessionID == "" {
			return fmt.Errorf("session_id is required")
		}
		if m.Label == "" {
			return fmt.Errorf("label is required")
		}

	default:
		return fmt.Errorf("unknown message type for validation")
	}

	return nil
}

// Common error codes
const (
	ErrorCodeProcessNotFound     = "PROCESS_NOT_FOUND"
	ErrorCodeInvalidCommand      = "INVALID_COMMAND"
	ErrorCodePermissionDenied    = "PERMISSION_DENIED"
	ErrorCodeConnectionLost      = "CONNECTION_LOST"
	ErrorCodeInvalidMessage      = "INVALID_MESSAGE"
	ErrorCodeSessionNotFound     = "SESSION_NOT_FOUND"
	ErrorCodeProcessFailed       = "PROCESS_FAILED"
	ErrorCodeTimeout             = "TIMEOUT"
	ErrorCodeInternalError       = "INTERNAL_ERROR"
	ErrorCodeCapabilityNotSupported = "CAPABILITY_NOT_SUPPORTED"
)

// Helper functions to create common messages

// NewLogMessage creates a new log message
func NewLogMessage(sessionID, label, content string, stream StreamType, pid int) *LogMessage {
	return &LogMessage{
		BaseMessage: BaseMessage{
			Type:      MessageTypeLog,
			SessionID: sessionID,
		},
		Label:     label,
		Content:   content,
		Timestamp: time.Now(),
		Stream:    stream,
		PID:       pid,
	}
}

// NewCommandMessage creates a new command message
func NewCommandMessage(sessionID string, action CommandAction, signal *Signal) *CommandMessage {
	msg := &CommandMessage{
		BaseMessage: BaseMessage{
			Type:      MessageTypeCommand,
			SessionID: sessionID,
		},
		Action: action,
	}
	if signal != nil {
		msg.Signal = signal
	}
	return msg
}

// NewStdinMessage creates a new stdin message
func NewStdinMessage(sessionID, input string) *StdinMessage {
	return &StdinMessage{
		BaseMessage: BaseMessage{
			Type:      MessageTypeStdin,
			SessionID: sessionID,
		},
		Input: input,
	}
}

// NewAckMessage creates a new acknowledgment message
func NewAckMessage(sessionID string, success bool, message string) *AckMessage {
	return &AckMessage{
		BaseMessage: BaseMessage{
			Type:      MessageTypeAck,
			SessionID: sessionID,
		},
		Success: success,
		Message: message,
	}
}

// NewErrorMessage creates a new error message
func NewErrorMessage(sessionID, errorCode, message string) *ErrorMessage {
	return &ErrorMessage{
		BaseMessage: BaseMessage{
			Type:      MessageTypeError,
			SessionID: sessionID,
		},
		ErrorCode: errorCode,
		Message:   message,
		Details:   make(map[string]interface{}),
	}
}

// NewStatusMessage creates a new status message
func NewStatusMessage(sessionID string, status SessionStatus, pid *int) *StatusMessage {
	return &StatusMessage{
		BaseMessage: BaseMessage{
			Type:      MessageTypeStatus,
			SessionID: sessionID,
		},
		Status: status,
		PID:    pid,
	}
}

// NewSessionRegistrationMessage creates a new session registration message
func NewSessionRegistrationMessage(sessionID, label, command, workingDir string, capabilities []string) *SessionRegistrationMessage {
	return &SessionRegistrationMessage{
		BaseMessage: BaseMessage{
			Type:      MessageTypeRegister,
			SessionID: sessionID,
		},
		Label:        label,
		Command:      command,
		WorkingDir:   workingDir,
		Capabilities: capabilities,
	}
}