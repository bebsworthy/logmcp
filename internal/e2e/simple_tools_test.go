package e2e

import (
	"context"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// TestSimpleToolsServer tests a minimal MCP server with tools to debug tools/list
func TestSimpleToolsServer(t *testing.T) {
	// Create a simple MCP server with a single tool
	s := server.NewMCPServer(
		"test-server",
		"1.0.0",
		server.WithToolCapabilities(true),
	)

	// Add a simple tool
	tool := mcp.NewTool("test_tool",
		mcp.WithDescription("A test tool"),
		mcp.WithString("message", 
			mcp.Required(),
			mcp.Description("Test message"),
		),
	)

	s.AddTool(tool, func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		message, _ := request.RequireString("message")
		return mcp.NewToolResultText("Echo: " + message), nil
	})

	t.Logf("Created server with test tool")

	// Now test with our client
	suite, err := NewClientTestSuite(t)
	if err != nil {
		t.Fatalf("Failed to create client test suite: %v", err)
	}
	defer suite.Cleanup()

	// Instead of starting logmcp serve, we need to test with the simple server
	// This test is to verify if the issue is with our server or the protocol
	t.Log("This test demonstrates that we need a way to test the server directly")
}

// TestLogMCPToolsDebug tests our actual server with debug logging
func TestLogMCPToolsDebug(t *testing.T) {
	// Set debug environment variable
	t.Setenv("MCP_DEBUG", "1")
	
	suite, err := NewClientTestSuite(t)
	if err != nil {
		t.Fatalf("Failed to create client test suite: %v", err)
	}
	defer suite.Cleanup()

	// Start the MCP server
	if err := suite.StartMCPServer(); err != nil {
		t.Fatalf("Failed to start MCP server: %v", err)
	}

	// Try calling a tool directly first to see if tools work at all
	t.Run("DirectToolCall", func(t *testing.T) {
		result, err := suite.CallListSessions()
		if err != nil {
			t.Fatalf("Failed to call list_sessions directly: %v", err)
		}
		t.Logf("Direct tool call succeeded: %+v", result)
	})

	// Now try ListTools which is timing out
	t.Run("ListTools", func(t *testing.T) {
		result, err := suite.ListTools()
		if err != nil {
			t.Fatalf("ListTools failed: %v", err)
		}
		t.Logf("ListTools succeeded: %+v", result)
	})
}