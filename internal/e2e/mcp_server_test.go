package e2e

import (
	"context"
	"testing"
	"time"

	"github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/client/transport"
	"github.com/mark3labs/mcp-go/mcp"
)

// TestLogMCPServerDebug tests our actual MCP server to debug the tools/list issue
func TestLogMCPServerDebug(t *testing.T) {
	// Find the logmcp binary
	binaryPath, err := findBinary()
	if err != nil {
		t.Skipf("Skipping test: %v", err)
	}

	// Create stdio transport
	stdioTransport := transport.NewStdio(binaryPath, nil, "serve")

	// Create client
	mcpClient := client.NewClient(stdioTransport)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Start client (launches server)
	if err := mcpClient.Start(ctx); err != nil {
		t.Fatalf("Failed to start client: %v", err)
	}
	defer mcpClient.Close()

	// Initialize
	initRequest := mcp.InitializeRequest{}
	initRequest.Params.ProtocolVersion = mcp.LATEST_PROTOCOL_VERSION
	initRequest.Params.ClientInfo = mcp.Implementation{
		Name:    "debug-client",
		Version: "1.0.0",
	}
	initRequest.Params.Capabilities = mcp.ClientCapabilities{}

	t.Log("Sending initialize request...")
	serverInfo, err := mcpClient.Initialize(ctx, initRequest)
	if err != nil {
		t.Fatalf("Failed to initialize: %v", err)
	}
	t.Logf("Server initialized: %+v", serverInfo)

	// Add delay to ensure server is fully ready
	time.Sleep(500 * time.Millisecond)

	// Create a shorter timeout for tools/list
	listCtx, listCancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer listCancel()

	// Try to list tools with debug info
	t.Log("Sending tools/list request...")
	listRequest := mcp.ListToolsRequest{}
	
	result, err := mcpClient.ListTools(listCtx, listRequest)
	if err != nil {
		t.Logf("Failed to list tools: %v", err)
		
		// Try calling a tool directly to see if that works
		t.Log("Trying to call list_sessions directly...")
		callRequest := mcp.CallToolRequest{}
		callRequest.Params.Name = "list_sessions"
		callRequest.Params.Arguments = map[string]any{}
		
		callResult, callErr := mcpClient.CallTool(ctx, callRequest)
		if callErr != nil {
			t.Logf("Direct tool call also failed: %v", callErr)
		} else {
			t.Logf("Direct tool call succeeded! Result: %+v", callResult)
		}
		
		t.Fatalf("tools/list failed but server seems to be running")
	}

	t.Logf("tools/list succeeded! Found %d tools", len(result.Tools))
	for _, tool := range result.Tools {
		t.Logf("Tool: %s - %s", tool.Name, tool.Description)
	}
}

// TestDirectToolCall tests calling a tool directly without listing first
func TestDirectToolCall(t *testing.T) {
	// Find the logmcp binary
	binaryPath, err := findBinary()
	if err != nil {
		t.Skipf("Skipping test: %v", err)
	}

	// Create stdio transport
	stdioTransport := transport.NewStdio(binaryPath, nil, "serve")

	// Create client
	mcpClient := client.NewClient(stdioTransport)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Start client
	if err := mcpClient.Start(ctx); err != nil {
		t.Fatalf("Failed to start client: %v", err)
	}
	defer mcpClient.Close()

	// Initialize
	initRequest := mcp.InitializeRequest{}
	initRequest.Params.ProtocolVersion = mcp.LATEST_PROTOCOL_VERSION
	initRequest.Params.ClientInfo = mcp.Implementation{
		Name:    "direct-test-client",
		Version: "1.0.0",
	}

	serverInfo, err := mcpClient.Initialize(ctx, initRequest)
	if err != nil {
		t.Fatalf("Failed to initialize: %v", err)
	}
	t.Logf("Server info: %+v", serverInfo)

	// Call list_sessions directly
	callRequest := mcp.CallToolRequest{}
	callRequest.Params.Name = "list_sessions"
	callRequest.Params.Arguments = map[string]any{}

	result, err := mcpClient.CallTool(ctx, callRequest)
	if err != nil {
		t.Fatalf("Failed to call list_sessions: %v", err)
	}

	t.Logf("Direct tool call succeeded: %+v", result)
}