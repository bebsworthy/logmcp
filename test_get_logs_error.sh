#!/bin/bash

# Test script to verify get_logs error handling with non-existent session

# Start the server in the background
./logmcp serve &
SERVER_PID=$!

# Give server time to start
sleep 2

# Test get_logs with non-existent session using MCP inspector
cat << 'EOF' | nc localhost 8765
{
  "jsonrpc": "2.0",
  "method": "tools/call",
  "params": {
    "name": "get_logs",
    "arguments": {
      "labels": ["xxxx"]
    }
  },
  "id": 1
}
EOF

# Kill the server
kill $SERVER_PID

echo "Test completed"