package protocol

import (
	"encoding/json"
	"testing"
	"time"
)

func TestGetMCPTools(t *testing.T) {
	tools := GetMCPTools()

	expectedTools := []string{"list_sessions", "get_logs", "start_process", "control_process", "send_stdin"}

	if len(tools) != len(expectedTools) {
		t.Fatalf("Expected %d tools, got %d", len(expectedTools), len(tools))
	}

	toolNames := make(map[string]bool)
	for _, tool := range tools {
		toolNames[tool.Name] = true
	}

	for _, expected := range expectedTools {
		if !toolNames[expected] {
			t.Errorf("Missing expected tool: %s", expected)
		}
	}
}

func TestParseMCPRequest(t *testing.T) {
	// Test GetLogsRequest
	getLogsJSON := `{
		"labels": ["test-session"],
		"lines": 100,
		"stream": "stdout",
		"pattern": "error.*"
	}`

	req, err := ParseMCPRequest("get_logs", []byte(getLogsJSON))
	if err != nil {
		t.Fatalf("Failed to parse get_logs request: %v", err)
	}

	getLogsReq, ok := req.(*GetLogsRequest)
	if !ok {
		t.Fatalf("Expected GetLogsRequest, got %T", req)
	}

	if len(getLogsReq.Labels) != 1 || getLogsReq.Labels[0] != "test-session" {
		t.Errorf("Expected labels ['test-session'], got %v", getLogsReq.Labels)
	}
	if getLogsReq.Lines == nil || *getLogsReq.Lines != 100 {
		t.Errorf("Expected lines 100, got %v", getLogsReq.Lines)
	}
	if getLogsReq.Stream == nil || *getLogsReq.Stream != "stdout" {
		t.Errorf("Expected stream 'stdout', got %v", getLogsReq.Stream)
	}
}

func TestValidateMCPRequest(t *testing.T) {
	// Test valid GetLogsRequest
	validReq := &GetLogsRequest{
		Labels: []string{"test-session"},
	}
	if err := ValidateMCPRequest("get_logs", validReq); err != nil {
		t.Errorf("Valid request failed validation: %v", err)
	}

	// Test invalid GetLogsRequest (empty labels)
	invalidReq := &GetLogsRequest{
		Labels: []string{},
	}
	if err := ValidateMCPRequest("get_logs", invalidReq); err == nil {
		t.Error("Invalid request passed validation")
	}

	// Test ControlProcessRequest with signal action
	controlReq := &ControlProcessRequest{
		Label:  "test-session",
		Action: "signal",
		Signal: stringPtr("SIGTERM"),
	}
	if err := ValidateMCPRequest("control_process", controlReq); err != nil {
		t.Errorf("Valid control request failed validation: %v", err)
	}

	// Test ControlProcessRequest with signal action but no signal
	invalidControlReq := &ControlProcessRequest{
		Label:  "test-session",
		Action: "signal",
	}
	if err := ValidateMCPRequest("control_process", invalidControlReq); err == nil {
		t.Error("Invalid control request passed validation")
	}
}

func TestNewListSessionsResponse(t *testing.T) {
	sessions := []SessionInfo{
		{
			Label:      "test-session-1",
			Status:     StatusRunning,
			PID:        intPtr(1234),
			Command:    "test command",
			WorkingDir: "/test",
			StartTime:  time.Now(),
			RunnerMode: ModeRun,
		},
		{
			Label:      "test-session-2",
			Status:     StatusStopped,
			Command:    "test command 2",
			WorkingDir: "/test2",
			StartTime:  time.Now(),
			RunnerMode: ModeForward,
		},
	}

	response := NewListSessionsResponse(sessions)

	if !response.Success {
		t.Error("Expected success to be true")
	}

	if len(response.Data.Sessions) != 2 {
		t.Errorf("Expected 2 sessions, got %d", len(response.Data.Sessions))
	}

	if response.Meta.TotalCount != 2 {
		t.Errorf("Expected total count 2, got %d", response.Meta.TotalCount)
	}

	if response.Meta.ActiveCount != 1 {
		t.Errorf("Expected active count 1, got %d", response.Meta.ActiveCount)
	}
}

func TestSerializeMCPResponse(t *testing.T) {
	response := NewMCPErrorResponse("Test error", "TEST_ERROR")

	data, err := SerializeMCPResponse(response)
	if err != nil {
		t.Fatalf("Failed to serialize response: %v", err)
	}

	var parsed map[string]interface{}
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("Failed to parse serialized response: %v", err)
	}

	if parsed["success"] != false {
		t.Errorf("Expected success false, got %v", parsed["success"])
	}
	if parsed["error"] != "Test error" {
		t.Errorf("Expected error 'Test error', got %v", parsed["error"])
	}
	if parsed["code"] != "TEST_ERROR" {
		t.Errorf("Expected code 'TEST_ERROR', got %v", parsed["code"])
	}
}

func TestRunnerModes(t *testing.T) {
	if ModeRun != "run" {
		t.Errorf("Expected ModeRun to be 'run', got '%s'", ModeRun)
	}
	if ModeForward != "forward" {
		t.Errorf("Expected ModeForward to be 'forward', got '%s'", ModeForward)
	}
	if ModeManaged != "managed" {
		t.Errorf("Expected ModeManaged to be 'managed', got '%s'", ModeManaged)
	}
}

// Helper functions for tests
func stringPtr(s string) *string {
	return &s
}

func intPtr(i int) *int {
	return &i
}
