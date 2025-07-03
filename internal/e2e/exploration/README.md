# E2E Test Exploration Files

This directory contains various test files created during the exploration and debugging of the MCP protocol implementation, particularly the tools/list marshaling issue.

## Files

- `client_framework.go` - Framework for testing with mcp-go client library
- `mcp_client_test.go` - Tests using the client framework
- `simple_tools_test.go` - Simple debugging tests
- `minimal_test.go` - Raw protocol test that helped identify the issue
- `minimal_server_test.go` - Minimal server testing
- `mcp_protocol_test.go` - Original protocol tests
- `framework.go` - Original test framework

## Purpose

These files were instrumental in:
1. Understanding the tools/list marshaling error
2. Testing different approaches to fix the array parameter issue
3. Discovering that the issue was with `mcp.Items()` expecting a map instead of ToolOption
4. Proving that the server works correctly with raw protocol

## Status

These tests are kept for reference but are not part of the main test suite. The actual e2e test is in `../mcp_server_test.go` which properly tests the MCP server with correct timeouts and initialization sequence.