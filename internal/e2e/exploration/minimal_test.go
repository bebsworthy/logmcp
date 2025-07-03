package e2e

import (
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"testing"
	"time"
)

// TestRawMCPProtocol tests raw MCP protocol to debug tools/list
func TestRawMCPProtocol(t *testing.T) {
	// Find binary
	binaryPath, err := findBinary()
	if err != nil {
		t.Skipf("Skipping test: %v", err)
	}

	// Start the server process
	cmd := exec.Command(binaryPath, "serve")
	
	// Create pipes
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatalf("Failed to create stdin pipe: %v", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatalf("Failed to create stdout pipe: %v", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		t.Fatalf("Failed to create stderr pipe: %v", err)
	}

	// Start the process
	if err := cmd.Start(); err != nil {
		t.Fatalf("Failed to start server: %v", err)
	}
	defer cmd.Process.Kill()

	// Read stderr in background
	go func() {
		buf := make([]byte, 1024)
		for {
			n, err := stderr.Read(buf)
			if err != nil {
				return
			}
			if n > 0 {
				t.Logf("STDERR: %s", string(buf[:n]))
			}
		}
	}()

	// Wait for server to start
	time.Sleep(500 * time.Millisecond)

	// Send initialize request
	initReq := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "initialize",
		"params": map[string]interface{}{
			"protocolVersion": "2024-11-05",
			"capabilities":    map[string]interface{}{},
			"clientInfo": map[string]interface{}{
				"name":    "test-client",
				"version": "1.0.0",
			},
		},
	}

	if err := sendJSON(stdin, initReq); err != nil {
		t.Fatalf("Failed to send initialize: %v", err)
	}

	// Read initialize response
	initResp, err := readJSON(stdout)
	if err != nil {
		t.Fatalf("Failed to read initialize response: %v", err)
	}
	t.Logf("Initialize response: %+v", initResp)

	// Send initialized notification
	initializedNotif := map[string]interface{}{
		"jsonrpc": "2.0",
		"method":  "notifications/initialized",
		"params":  map[string]interface{}{},
	}

	if err := sendJSON(stdin, initializedNotif); err != nil {
		t.Fatalf("Failed to send initialized: %v", err)
	}

	// Wait a bit
	time.Sleep(100 * time.Millisecond)

	// Send tools/list request
	toolsReq := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      2,
		"method":  "tools/list",
		"params":  map[string]interface{}{},
	}

	t.Log("Sending tools/list request...")
	if err := sendJSON(stdin, toolsReq); err != nil {
		t.Fatalf("Failed to send tools/list: %v", err)
	}

	// Try to read response with timeout
	done := make(chan interface{})
	var toolsResp interface{}
	var readErr error

	go func() {
		toolsResp, readErr = readJSON(stdout)
		close(done)
	}()

	select {
	case <-done:
		if readErr != nil {
			t.Fatalf("Failed to read tools/list response: %v", readErr)
		}
		t.Logf("tools/list response: %+v", toolsResp)
	case <-time.After(5 * time.Second):
		t.Fatal("Timeout waiting for tools/list response")
	}
}

func sendJSON(w io.Writer, msg interface{}) error {
	data, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(w, "%s\n", data)
	return err
}

func readJSON(r io.Reader) (interface{}, error) {
	decoder := json.NewDecoder(r)
	var msg interface{}
	if err := decoder.Decode(&msg); err != nil {
		return nil, err
	}
	return msg, nil
}