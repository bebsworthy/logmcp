package cmd

import (
	"bufio"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/logmcp/logmcp/internal/config"
	"github.com/logmcp/logmcp/internal/runner"
)

var (
	// Run command flags
	runLabel      string
	silenceOutput bool
)

// runCmd represents the run command
var runCmd = &cobra.Command{
	Use:   "run [--label LABEL] [--server-url URL] -- <command>",
	Short: "Run a process and stream its logs to the LogMCP server",
	Long: `Run a process and stream its logs to the LogMCP server.

This command wraps the execution of another command, capturing its stdout and stderr
and streaming the logs in real-time to the LogMCP server via WebSocket connection.

The process is managed by the runner, which handles:
- Process startup and monitoring
- Log capture and streaming  
- WebSocket connection management
- Graceful shutdown and cleanup

If no label is provided, one will be auto-generated (e.g., session-1, session-2).`,
	Example: `  # Run a Node.js server with custom label
  logmcp run --label backend -- npm run server

  # Run with auto-generated label
  logmcp run -- python app.py

  # Run connecting to remote LogMCP server
  logmcp run --server-url ws://remote:8765 --label api -- ./api-server

  # Run a command with arguments
  logmcp run --label build -- npm run build --production`,
	Args: func(cmd *cobra.Command, args []string) error {
		if len(args) == 0 {
			return fmt.Errorf("command is required after -- separator")
		}
		return nil
	},
	RunE: runCommand,
}

func init() {
	rootCmd.AddCommand(runCmd)

	// Run-specific flags
	runCmd.Flags().StringVar(&runLabel, "label", "", "label for the session (auto-generated if not provided)")
	runCmd.Flags().BoolVar(&silenceOutput, "silence-output", false, "suppress local display of process output (logs still sent to server)")
}

func runCommand(cmd *cobra.Command, args []string) error {
	// Generate label if not provided
	if runLabel == "" {
		runLabel = generateSessionLabel("session")
		if verbose {
			fmt.Printf("Auto-generated label: %s\n", runLabel)
		}
	}

	// Validate server URL
	if serverURL == "" {
		return fmt.Errorf("server URL cannot be empty")
	}

	// Build command string for display
	commandStr := strings.Join(args, " ")

	if verbose {
		fmt.Printf("Starting process runner...\n")
		fmt.Printf("Label: %s\n", runLabel)
		fmt.Printf("Server URL: %s\n", serverURL)
		fmt.Printf("Command: %s\n", commandStr)
		fmt.Printf("Working directory: %s\n", getCurrentWorkingDir())
		fmt.Println()
	}

	// Validate that command exists and is executable
	if len(args) == 0 {
		return fmt.Errorf("no command specified")
	}

	// Import runner package at the top of the file
	// For now, let's implement the actual runner logic
	fmt.Printf("🏃 LogMCP Process Runner\n")
	fmt.Printf("   Label: %s\n", runLabel)
	fmt.Printf("   Server: %s\n", serverURL)
	fmt.Printf("   Command: %s\n", commandStr)
	fmt.Printf("   Working dir: %s\n", getCurrentWorkingDir())
	fmt.Println()

	// Load configuration
	cfg, err := config.LoadConfig(configFile)
	if err != nil {
		return fmt.Errorf("failed to load configuration: %w", err)
	}

	// Create process runner with LogMCP configuration
	runner := runner.NewProcessRunnerWithLogMCPConfig(commandStr, runLabel, serverURL, cfg)
	
	// Set up callbacks
	setupRunnerCallbacks(runner)
	
	// Debug: Check if WebSocket client exists
	if runner.GetWebSocketClient() == nil {
		return fmt.Errorf("WebSocket client not initialized")
	}
	
	// Connect to server with retry logic
	fmt.Printf("Connecting to server at %s...\n", serverURL)
	
	if err := runner.GetWebSocketClient().ConnectWithRetry(); err != nil {
		return fmt.Errorf("failed to connect to server: %w", err)
	}
	
	// Always show connection confirmation
	fmt.Printf("✓ Connected to server successfully! Session: %s\n", runner.GetWebSocketClient().GetLabel())
	
	// Run the process with signal handling
	if verbose {
		fmt.Printf("Starting process...\n")
	}
	
	// Start stdin forwarding goroutine
	go forwardStdin(runner)
	
	// Run the process
	err = runner.RunWithSignalHandling()
	
	// Ensure all messages are sent before exiting
	if wsClient := runner.GetWebSocketClient(); wsClient != nil {
		if flushErr := wsClient.FlushMessages(); flushErr != nil {
			if verbose {
				log.Printf("Warning: Failed to flush messages: %v", flushErr)
			}
		}
		// Close the WebSocket connection gracefully
		wsClient.Close()
	}
	
	return err
}

// getCurrentWorkingDir returns the current working directory
func getCurrentWorkingDir() string {
	if wd, err := os.Getwd(); err == nil {
		return wd
	}
	return "unknown"
}

// createWebSocketClient creates and configures a WebSocket client
func createWebSocketClient(serverURL, label, command, workingDir string) *runner.WebSocketClient {
	client := runner.NewWebSocketClient(serverURL, label)
	client.SetCommand(command, workingDir, []string{"process_control", "stdin"})
	
	// Set up client callbacks
	client.OnConnected = func(label string) {
		if verbose {
			log.Printf("Session registered with label: %s", label)
		}
	}
	
	client.OnDisconnected = func() {
		if verbose {
			log.Printf("Disconnected from server")
		}
	}
	
	client.OnError = func(err error) {
		log.Printf("WebSocket error: %v", err)
	}
	
	return client
}

// createProcessRunner creates and configures a process runner
func createProcessRunner(command, label, workingDir string) *runner.ProcessRunner {
	config := runner.ProcessRunnerConfig{
		WorkingDir:       workingDir,
		Environment:      nil,
		RestartOnFailure: false,
		MaxRestarts:      0,
		RestartDelay:     0,
	}
	
	return runner.NewProcessRunnerWithConfig(command, label, config)
}

// setupRunnerCallbacks sets up callbacks for the process runner
func setupRunnerCallbacks(processRunner *runner.ProcessRunner) {
	processRunner.OnProcessStart = func(pid int) {
		fmt.Printf("Process started with PID: %d\n", pid)
	}
	
	processRunner.OnProcessExit = func(exitCode int) {
		if exitCode == 0 {
			fmt.Printf("Process completed successfully.\n")
		} else {
			fmt.Printf("Process exited with code: %d\n", exitCode)
		}
	}
	
	processRunner.OnLogLine = func(content, stream string) {
		// Display output by default, unless silenced
		if !silenceOutput {
			fmt.Printf("[%s] %s\n", stream, content)
		}
	}
	
	processRunner.OnError = func(err error) {
		log.Printf("Process error: %v", err)
	}
}

// forwardStdin reads from stdin and forwards input to the process
func forwardStdin(processRunner *runner.ProcessRunner) {
	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		line := scanner.Text()
		// Add newline since scanner strips it
		if err := processRunner.SendStdin(line + "\n"); err != nil {
			if verbose {
				log.Printf("Failed to send stdin: %v", err)
			}
			// Exit goroutine if we can't send stdin (process likely exited)
			return
		}
	}
	
	if err := scanner.Err(); err != nil {
		if verbose {
			log.Printf("Stdin scanner error: %v", err)
		}
	}
}