# LogMCP

A Model Context Protocol (MCP) server that provides real-time log streaming and process management for AI assistants like Claude. LogMCP allows AI tools to monitor, analyze, and interact with running processes through live log feeds.

## What is LogMCP?

LogMCP bridges the gap between AI assistants and your running applications by:

- **Real-time log streaming** - Monitor live output from any process
- **Process management** - Start, stop, and control processes through AI
- **Session persistence** - Logs remain available even after disconnection
- **Multi-process support** - Track multiple applications simultaneously
- **MCP integration** - Works seamlessly with Claude Code and other MCP clients

## Quick Start

### Installation

1. **Clone and build:**
   ```bash
   git clone <repository-url>
   cd logmcp
   go build -o logmcp main.go
   ```

2. **Start the LogMCP server:**
   ```bash
   ./logmcp serve
   ```

### Basic Usage

**Monitor a single process:**
```bash
# Terminal 1: Start LogMCP server
./logmcp serve

# Terminal 2: Run your application with log streaming
./logmcp run --label "my-app" -- python my_script.py
```

**Monitor multiple processes:**
```bash
./logmcp run --label "frontend" -- npm start
./logmcp run --label "backend" -- python api.py
./logmcp run --label "database" -- docker run postgres
```

## Claude Code Integration

LogMCP is designed to work seamlessly with Claude Code and other MCP clients.

### Setup for Claude Code

1. **Add LogMCP to your Claude Code configuration:**

   Create or edit your Claude Code MCP configuration file:

   **macOS/Linux:** `~/.config/claude/mcp_servers.json`
   **Windows:** `%APPDATA%\Claude\mcp_servers.json`

   ```json
   {
     "mcpServers": {
       "logmcp": {
         "command": "/path/to/your/logmcp",
         "args": ["serve"],
         "cwd": "/path/to/your/project"
       }
     }
   }
   ```

2. **Start Claude Code** - LogMCP will automatically be available as an MCP server

3. **Use LogMCP through Claude Code:**
   ```
   You: Start monitoring my web application
   Claude: I'll start your web application and monitor its logs using LogMCP...
   
   You: Show me the recent error logs
   Claude: Here are the recent errors from your application...
   
   You: The app seems slow, what's happening?
   Claude: Looking at the logs, I can see database connection timeouts...
   ```

### Available Claude Code Commands

Once LogMCP is configured, Claude can:

- **Start processes:** "Start my React development server"
- **Monitor logs:** "Show me the last 50 log entries from the backend"
- **Debug issues:** "What errors occurred in the last 5 minutes?"
- **Process control:** "Restart the crashed service"
- **Multi-app coordination:** "Start all my microservices and monitor them"

## Configuration

### Server Configuration

Create `config.yaml`:
```yaml
server:
  host: "localhost"
  websocket_port: 8765
  mcp_transport: "stdio"

buffer:
  max_size: "5MB"
  max_age: "5m"

process:
  graceful_shutdown_timeout: "30s"
  cleanup_delay: "1h"
```

### Environment Variables

```bash
LOGMCP_CONFIG_FILE=/path/to/config.yaml
LOGMCP_HOST=localhost
LOGMCP_WEBSOCKET_PORT=8765
LOGMCP_VERBOSE=true
```

## Use Cases

### Development Workflow
```bash
# Start your development stack with LogMCP
./logmcp run --label "frontend" -- npm run dev
./logmcp run --label "backend" -- python manage.py runserver  
./logmcp run --label "db" -- docker-compose up postgres

# Claude can now monitor all services and help debug issues
```

### CI/CD Integration
```bash
# In your CI pipeline
./logmcp run --label "tests" -- pytest --verbose
./logmcp run --label "build" -- docker build .
./logmcp run --label "deploy" -- kubectl apply -f k8s/

# AI can analyze build logs and deployment status
```

### Production Monitoring
```bash
# Monitor production services
./logmcp run --label "api-server" -- ./my-api-server
./logmcp run --label "worker" -- celery worker
./logmcp run --label "scheduler" -- cron

# AI assistant can alert on errors and suggest fixes
```

## Command Reference

### `logmcp serve`
Start the LogMCP server for MCP clients.

```bash
./logmcp serve [flags]

Flags:
  --host string              Host to bind to (default "localhost")
  --websocket-port int       WebSocket port (default 8765)
  --config string           Config file path
  --verbose                 Enable verbose logging
```

### `logmcp run`
Run a process with log streaming to LogMCP server.

```bash
./logmcp run [flags] -- <command>

Flags:
  --label string            Label for the process session
  --server-url string       LogMCP server URL (default "ws://localhost:8765")
  --working-dir string      Working directory for the process
```

### `logmcp forward`
Forward logs from files or stdin to LogMCP server.

```bash
./logmcp forward [flags]

Flags:
  --label string            Label for the log session
  --server-url string       LogMCP server URL
  --file string            File to monitor (default: stdin)
```

## Advanced Features

### Session Persistence
- Logs persist for 1 hour after process termination
- AI can access historical logs even after reconnection
- Ring buffer prevents memory overflow (5MB/5min per session)

### Multi-Client Support
- Multiple AI assistants can connect simultaneously
- Real-time log sharing across clients
- Concurrent process management

### Error Handling
- Automatic reconnection on connection loss
- Graceful process termination
- Comprehensive error reporting to AI clients

## Troubleshooting

### Common Issues

**LogMCP server won't start:**
```bash
# Check if port is already in use
netstat -an | grep 8765

# Try a different port
./logmcp serve --websocket-port 9000
```

**Claude Code can't find LogMCP:**
- Verify the path in `mcp_servers.json` is absolute
- Ensure LogMCP binary has execute permissions
- Check Claude Code logs for connection errors

**Processes not appearing in logs:**
- Verify WebSocket connection: `ws://localhost:8765`
- Check firewall settings
- Ensure LogMCP server is running before starting processes

### Debug Mode

Enable verbose logging for troubleshooting:
```bash
./logmcp serve --verbose
./logmcp run --verbose --label "debug" -- your-command
```

## Contributing

We welcome contributions! Please see our [contributing guidelines](CONTRIBUTING.md) for details.

## License

This project is licensed under the MIT License - see the [LICENSE](LICENSE) file for details.