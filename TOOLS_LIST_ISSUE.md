# tools/list Timeout Issue Investigation

## Issue Summary
The MCP server times out when responding to `tools/list` requests with the error:
```
json: error calling MarshalJSON for type mcp.Tool: json: error calling MarshalJSON for type mcp.ToolInputSchema: json: unsupported type: mcp.ToolOption
```

## Root Cause
The mcp-go library v0.32.0 has a marshaling issue with the Tool type. Even when using `NewToolWithRawSchema` to avoid the ToolOption type, the library still fails to marshal tools for the tools/list response.

## Evidence
1. A minimal test server using the same mcp-go library works correctly
2. Our LogMCP server fails with the marshaling error
3. The error persists even when:
   - Using NewToolWithRawSchema instead of NewTool
   - Removing all tool parameters
   - Simplifying the server configuration

## Workaround
Currently, tools can be called directly using `tools/call` even though `tools/list` fails. This suggests the tools are registered correctly but the listing mechanism has a bug.

## Recommended Solutions
1. **Report the bug** to the mcp-go library maintainers
2. **Downgrade** to an earlier version of mcp-go that doesn't have this issue
3. **Fork and fix** the mcp-go library to handle tool marshaling correctly
4. **Document** that LLM clients should use direct tool calls without listing

## Test Results
- Standalone test server: ✅ tools/list works
- LogMCP integrated server: ❌ tools/list fails with marshaling error
- Direct tool calls: ✅ Work correctly even without tools/list

## Next Steps
For now, we'll document this limitation and ensure that direct tool calls work correctly. The MCP protocol allows clients to call tools directly if they know the tool names and parameters.