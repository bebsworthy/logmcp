package cmd

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/spf13/cobra"
	"github.com/bebsworthy/logmcp/internal/server"
)

var (
	// Serve command flags
	websocketPort int
	host          string
)

// serveCmd represents the serve command
var serveCmd = &cobra.Command{
	Use:   "serve",
	Short: "Start the LogMCP server",
	Long: `Start the LogMCP server which provides:
- WebSocket server for log ingestion from runners
- MCP interface for LLM interaction via stdio
- Ring buffer management per session
- Process lifecycle management

The server exposes a WebSocket endpoint for runners to connect and stream logs,
while providing an MCP interface over stdio for LLM clients to interact with
the logged data and manage processes.`,
	Example: `  # Start server with default settings (localhost:8765)
  logmcp serve

  # Start server on specific host and port
  logmcp serve --host 0.0.0.0 --websocket-port 9000

  # Start with verbose logging
  logmcp serve --verbose`,
	RunE: runServe,
}

func init() {
	rootCmd.AddCommand(serveCmd)

	// Serve-specific flags (these will override config file values)
	serveCmd.Flags().IntVar(&websocketPort, "websocket-port", 0, "WebSocket server port (overrides config)")
	serveCmd.Flags().StringVar(&host, "host", "", "Host to bind the WebSocket server to (overrides config)")
}

func runServe(cmd *cobra.Command, args []string) error {
	// Get configuration
	config := GetConfig()
	
	// Use config values as defaults, override with flags if provided
	actualHost := config.Server.Host
	actualPort := config.Server.WebSocketPort
	
	// Override with command line flags if provided
	if host != "" {
		actualHost = host
	}
	if websocketPort != 0 {
		actualPort = websocketPort
	}
	
	if verbose || config.Logging.Verbose {
		fmt.Fprintf(os.Stderr, "Starting LogMCP server...\n")
		fmt.Fprintf(os.Stderr, "WebSocket endpoint: ws://%s:%d/\n", actualHost, actualPort)
		fmt.Fprintf(os.Stderr, "MCP interface: %s\n", config.Server.MCPTransport)
		if configFile != "" {
			fmt.Fprintf(os.Stderr, "Config file: %s\n", configFile)
		}
		fmt.Fprintf(os.Stderr, "Verbose logging: enabled\n")
		fmt.Fprintf(os.Stderr, "Ring buffer: %s/%v per session\n", config.Buffer.MaxSize, config.Buffer.MaxAge)
		fmt.Fprintln(os.Stderr)
	}

	// Validate configuration
	if actualPort < 1 || actualPort > 65535 {
		return fmt.Errorf("invalid websocket port: %d (must be 1-65535)", actualPort)
	}

	if actualHost == "" {
		return fmt.Errorf("host cannot be empty")
	}

	// Initialize server components
	fmt.Fprintf(os.Stderr, "🚀 LogMCP server starting...\n")
	fmt.Fprintf(os.Stderr, "   WebSocket server: ws://%s:%d/\n", actualHost, actualPort)
	fmt.Fprintf(os.Stderr, "   MCP interface: %s\n", config.Server.MCPTransport)
	fmt.Fprintf(os.Stderr, "   Ring buffer: %s/%v per session\n", config.Buffer.MaxSize, config.Buffer.MaxAge)
	fmt.Fprintf(os.Stderr, "   Process management: enabled\n")
	if config.Development.DebugMode {
		fmt.Fprintf(os.Stderr, "   Debug mode: enabled\n")
	}
	fmt.Fprintln(os.Stderr)

	// Create session manager with configuration
	sessionManager := server.NewSessionManager()
	defer sessionManager.Close()

	// Create WebSocket server with configuration
	wsServer := server.NewWebSocketServer(sessionManager)
	defer wsServer.Close()

	// Create MCP server with configuration
	mcpServer := server.NewMCPServer(sessionManager, wsServer)
	defer mcpServer.Stop()

	// Set up signal handling for graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// Start server components in goroutines
	var wg sync.WaitGroup
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start HTTP server for WebSocket connections
	wg.Add(1)
	go func() {
		defer wg.Done()
		
		// Set up HTTP server with WebSocket handler
		mux := http.NewServeMux()
		mux.HandleFunc("/", wsServer.HandleWebSocket)
		
		httpServer := &http.Server{
			Addr:         fmt.Sprintf("%s:%d", actualHost, actualPort),
			Handler:      mux,
			ReadTimeout:  config.WebSocket.ReadTimeout,
			WriteTimeout: config.WebSocket.WriteTimeout,
		}
		
		// Start HTTP server
		go func() {
			if verbose || config.Logging.Verbose {
				fmt.Fprintf(os.Stderr, "WebSocket server listening on %s\n", httpServer.Addr)
			}
			if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
				fmt.Fprintf(os.Stderr, "WebSocket server error: %v\n", err)
			}
		}()
		
		// Wait for shutdown signal
		<-ctx.Done()
		
		// Graceful shutdown with timeout
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), config.Process.GracefulShutdownTimeout)
		defer shutdownCancel()
		
		if err := httpServer.Shutdown(shutdownCtx); err != nil {
			fmt.Fprintf(os.Stderr, "WebSocket server shutdown error: %v\n", err)
		}
	}()

	// Start MCP server on stdio
	wg.Add(1)
	go func() {
		defer wg.Done()
		
		if verbose || config.Logging.Verbose {
			fmt.Fprintf(os.Stderr, "MCP server starting on %s\n", config.Server.MCPTransport)
		}
		
		// MCP server blocks until stdin is closed
		if err := mcpServer.Serve(); err != nil {
			fmt.Fprintf(os.Stderr, "MCP server error: %v\n", err)
		}
	}()

	fmt.Fprintf(os.Stderr, "Server is ready to accept connections.\n")
	fmt.Fprintf(os.Stderr, "Connect runners with: logmcp run --server-url ws://%s:%d -- <command>\n", actualHost, actualPort)
	fmt.Fprintf(os.Stderr, "Press Ctrl+C to stop the server.\n")

	// Wait for shutdown signal
	<-sigChan

	fmt.Fprintf(os.Stderr, "\n🛑 Shutting down LogMCP server...\n")
	
	// Cancel context to signal shutdown
	cancel()
	
	// Wait for all goroutines to finish with timeout
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()
	
	select {
	case <-done:
		fmt.Fprintf(os.Stderr, "Server stopped gracefully.\n")
	case <-time.After(config.Process.GracefulShutdownTimeout):
		fmt.Fprintf(os.Stderr, "Server shutdown timeout - force stopping.\n")
	}

	return nil
}