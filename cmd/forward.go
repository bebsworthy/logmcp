package cmd

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/bebsworthy/logmcp/internal/config"
	"github.com/bebsworthy/logmcp/internal/runner"
	"github.com/spf13/cobra"
)

var (
	// Forward command flags
	forwardLabel string
)

// forwardCmd represents the forward command
var forwardCmd = &cobra.Command{
	Use:   "forward [--label LABEL] [--server-url URL] <source>",
	Short: "Forward logs from files or stdin to the LogMCP server",
	Long: `Forward logs from existing sources to the LogMCP server.

This command can forward logs from various sources:
- Log files (with tail -f like behavior)
- stdin (for piping from other commands)
- Named pipes and FIFOs

The forwarder establishes a WebSocket connection to the LogMCP server and streams
log entries in real-time. It handles:
- File watching and tailing
- Stdin reading and buffering
- WebSocket connection management
- Reconnection logic on connection failures

If no label is provided, one will be auto-generated based on the source.`,
	Example: `  # Forward from a log file
  logmcp forward --label nginx /var/log/nginx/access.log

  # Forward from stdin (piped from another command)
  kubectl logs -f my-pod | logmcp forward --label k8s-app

  # Forward to remote server
  logmcp forward --server-url ws://remote:8765 --label prod-api /var/log/api.log

  # Forward from multiple sources (run multiple instances)
  logmcp forward --label database /var/log/postgres.log
  logmcp forward --label web-server /var/log/nginx/error.log`,
	Args: func(cmd *cobra.Command, args []string) error {
		if len(args) == 0 {
			// Check if we're reading from stdin
			if isStdinPipe() {
				return nil // stdin is valid source
			}
			return fmt.Errorf("source is required (file path, stdin, or named pipe)")
		}
		if len(args) > 1 {
			return fmt.Errorf("only one source can be specified")
		}
		return nil
	},
	RunE: runForward,
}

func init() {
	rootCmd.AddCommand(forwardCmd)

	// Forward-specific flags
	forwardCmd.Flags().StringVar(&forwardLabel, "label", "", "label for the session (auto-generated based on source if not provided)")
}

func runForward(cmd *cobra.Command, args []string) error {
	var source string
	var sourceType string

	// Determine source
	if len(args) == 0 {
		// Reading from stdin
		source = "stdin"
		sourceType = "stdin"
	} else {
		source = args[0]
		// Determine source type
		if source == "-" {
			source = "stdin"
			sourceType = "stdin"
		} else {
			sourceType = determineSourceType(source)
		}
	}

	// Generate label if not provided
	if forwardLabel == "" {
		forwardLabel = generateLabelFromSource(source, sourceType)
		if verbose {
			fmt.Printf("Auto-generated label: %s\n", forwardLabel)
		}
	}

	// Validate server URL
	if serverURL == "" {
		return fmt.Errorf("server URL cannot be empty")
	}

	// Validate source
	if err := validateSource(source, sourceType); err != nil {
		return fmt.Errorf("invalid source: %w", err)
	}

	if verbose {
		fmt.Printf("Starting log forwarder...\n")
		fmt.Printf("Label: %s\n", forwardLabel)
		fmt.Printf("Server URL: %s\n", serverURL)
		fmt.Printf("Source: %s (%s)\n", source, sourceType)
		fmt.Printf("Working directory: %s\n", getCurrentWorkingDir())
		fmt.Println()
	}

	// Implement the actual forwarder logic
	fmt.Printf("📡 LogMCP Log Forwarder\n")
	fmt.Printf("   Label: %s\n", forwardLabel)
	fmt.Printf("   Server: %s\n", serverURL)
	fmt.Printf("   Source: %s (%s)\n", source, sourceType)
	fmt.Printf("   Working dir: %s\n", getCurrentWorkingDir())
	fmt.Println()

	// Load configuration
	cfg, err := config.LoadConfig(configFile)
	if err != nil {
		return fmt.Errorf("failed to load configuration: %w", err)
	}

	// Create log forwarder with LogMCP configuration
	forwarder := runner.NewLogForwarderWithLogMCPConfig(source, forwardLabel, serverURL, cfg)

	// Set up callbacks
	setupForwarderCallbacks(forwarder)

	// Connect to server with retry logic
	if verbose {
		fmt.Printf("Connecting to server...\n")
	}

	if err := forwarder.GetWebSocketClient().ConnectWithRetry(); err != nil {
		return fmt.Errorf("failed to connect to server: %w", err)
	}

	if verbose {
		fmt.Printf("Connected successfully! Label: %s\n", forwarder.GetWebSocketClient().GetLabel())
	}

	// Run the forwarder
	if verbose {
		fmt.Printf("Starting log forwarding from %s...\n", source)
	}

	return forwarder.Run()
}

// determineSourceType determines the type of source (file, named_pipe, etc.)
func determineSourceType(source string) string {
	if source == "stdin" || source == "-" {
		return "stdin"
	}

	// Check if it's a file or named pipe
	if info, err := os.Stat(source); err == nil {
		if info.Mode()&os.ModeNamedPipe != 0 {
			return "named_pipe"
		}
		if info.Mode().IsRegular() {
			return "file"
		}
	}

	// Default to file (even if it doesn't exist yet - might be created)
	return "file"
}

// generateLabelFromSource generates a label based on the source
func generateLabelFromSource(source, sourceType string) string {
	switch sourceType {
	case "stdin":
		return generateSessionLabel("stdin")
	case "file":
		// Use basename of file path
		base := filepath.Base(source)
		// Remove extension for cleaner label
		if ext := filepath.Ext(base); ext != "" {
			base = strings.TrimSuffix(base, ext)
		}
		return base
	case "named_pipe":
		return filepath.Base(source)
	default:
		return generateSessionLabel("forward")
	}
}

// validateSource validates that the source is accessible
func validateSource(source, sourceType string) error {
	switch sourceType {
	case "stdin":
		// Always valid for stdin
		return nil
	case "file":
		// Check if file exists and is readable
		if _, err := os.Stat(source); err != nil {
			// File might not exist yet but could be created - warn but don't fail
			if verbose {
				fmt.Printf("Warning: source file does not exist yet: %s\n", source)
			}
		}
		return nil
	case "named_pipe":
		// Check if named pipe exists
		if info, err := os.Stat(source); err != nil {
			return fmt.Errorf("named pipe not found: %s", source)
		} else if info.Mode()&os.ModeNamedPipe == 0 {
			return fmt.Errorf("not a named pipe: %s", source)
		}
		return nil
	default:
		return fmt.Errorf("unsupported source type: %s", sourceType)
	}
}

// isStdinPipe checks if stdin has data piped to it
func isStdinPipe() bool {
	if stat, err := os.Stdin.Stat(); err == nil {
		return stat.Mode()&os.ModeCharDevice == 0
	}
	return false
}

// setupForwarderCallbacks sets up callbacks for the log forwarder
func setupForwarderCallbacks(forwarder *runner.LogForwarder) {
	forwarder.OnLogLine = func(content string) {
		if verbose {
			fmt.Printf("[forward] %s\n", content)
		}
	}

	forwarder.OnError = func(err error) {
		log.Printf("Forwarder error: %v", err)
	}
}
