package e2e

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// TestMinimalMCPServerToolsList tests a minimal MCP server to isolate the tools/list issue
func TestMinimalMCPServerToolsList(t *testing.T) {
	// Create the most minimal MCP server possible
	s := server.NewMCPServer(
		"minimal-test",
		"1.0.0",
		server.WithToolCapabilities(true),
	)

	// Add a single simple tool using NewToolWithRawSchema to avoid marshaling issues
	schema := json.RawMessage(`{
		"type": "object",
		"properties": {
			"message": {
				"type": "string",
				"description": "Test message"
			}
		},
		"required": ["message"]
	}`)

	tool := mcp.NewToolWithRawSchema(
		"echo",
		"Echo a message",
		schema,
	)

	s.AddTool(tool, func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args := request.GetArguments()
		message := args["message"].(string)
		return mcp.NewToolResultText("Echo: " + message), nil
	})

	// Try to serialize the server info manually to check if tools are registered
	t.Logf("Server created with tool 'echo'")

	// Use ServeStdio in a test context to see if it starts correctly
	// Note: This is just to verify the server can be created, not to actually serve
	t.Log("Minimal server setup complete")
}

// TestCompareToolCreation compares different ways to create tools
func TestCompareToolCreation(t *testing.T) {
	// Method 1: Using NewTool with builder pattern
	tool1 := mcp.NewTool("test1",
		mcp.WithDescription("Test tool 1"),
		mcp.WithString("param1", 
			mcp.Required(),
			mcp.Description("Parameter 1"),
		),
	)

	// Method 2: Using NewToolWithRawSchema
	schema := json.RawMessage(`{
		"type": "object",
		"properties": {
			"param1": {
				"type": "string",
				"description": "Parameter 1"
			}
		},
		"required": ["param1"]
	}`)

	tool2 := mcp.NewToolWithRawSchema(
		"test2",
		"Test tool 2",
		schema,
	)

	// Log tool structures for comparison
	t.Logf("Tool1 (NewTool): %+v", tool1)
	t.Logf("Tool2 (NewToolWithRawSchema): %+v", tool2)

	// Try to marshal both tools to see which one causes issues
	data1, err1 := json.Marshal(tool1)
	if err1 != nil {
		t.Logf("Error marshaling tool1: %v", err1)
	} else {
		t.Logf("Tool1 JSON: %s", string(data1))
	}

	data2, err2 := json.Marshal(tool2)
	if err2 != nil {
		t.Logf("Error marshaling tool2: %v", err2)
	} else {
		t.Logf("Tool2 JSON: %s", string(data2))
	}
}