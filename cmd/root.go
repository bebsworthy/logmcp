package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/bebsworthy/logmcp/internal/config"
)

var (
	// Global flags
	configFile  string
	serverURL   string
	verbose     bool
	
	// Global configuration
	appConfig *config.Config
)

// rootCmd represents the base command when called without any subcommands
var rootCmd = &cobra.Command{
	Use:   "logmcp",
	Short: "LogMCP - Model Context Protocol server for real-time log streaming",
	Long: `LogMCP is a Model Context Protocol (MCP) server that provides real-time log 
streaming and process management capabilities for debugging and troubleshooting.

It enables LLMs to observe and control processes through a unified interface with
support for both local and remote log forwarding.`,
}

// Execute adds all child commands to the root command and sets flags appropriately.
// This is called by main.main(). It only needs to happen once to the rootCmd.
func Execute() error {
	return rootCmd.Execute()
}

func init() {
	cobra.OnInitialize(initConfig)

	// Global flags available to all commands
	rootCmd.PersistentFlags().StringVar(&configFile, "config", "", "config file (default is $LOGMCP_CONFIG or ./config.yaml)")
	rootCmd.PersistentFlags().StringVar(&serverURL, "server-url", getDefaultServerURL(), "server URL for runners (default is $LOGMCP_SERVER_URL or ws://localhost:8765)")
	rootCmd.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false, "verbose output")
}

// initConfig reads in config file and ENV variables if set.
func initConfig() {
	// Determine config file path
	configPath := configFile
	
	if configPath == "" {
		// Check for LOGMCP_CONFIG environment variable
		if envConfig := os.Getenv("LOGMCP_CONFIG"); envConfig != "" {
			configPath = envConfig
		}
		// Otherwise let config package handle auto-discovery
	}
	
	// Load configuration
	var err error
	appConfig, err = config.LoadConfig(configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading configuration: %v\n", err)
		os.Exit(1)
	}
	
	// Override verbose setting from command line flag if provided
	if verbose {
		appConfig.Logging.Verbose = true
	}
	
	// Override server URL from command line flag if provided
	if serverURL != "" && serverURL != getDefaultServerURL() {
		// Command line flag was explicitly set, keep it as is
		// The serverURL variable will be used by runner commands
	} else {
		// Use server URL from config for consistency
		serverURL = fmt.Sprintf("ws://%s:%d", appConfig.Server.Host, appConfig.Server.WebSocketPort)
	}
	
	if verbose || appConfig.Logging.Verbose {
		if configPath != "" {
			fmt.Printf("Loaded configuration from: %s\n", configPath)
		} else {
			fmt.Printf("Using default configuration\n")
		}
		
		if appConfig.Development.DebugMode {
			fmt.Printf("Debug mode enabled\n")
		}
	}
}

// getDefaultServerURL returns the default server URL, checking environment variables
func getDefaultServerURL() string {
	if url := os.Getenv("LOGMCP_SERVER_URL"); url != "" {
		return url
	}
	return "ws://localhost:8765"
}

// generateSessionLabel generates an auto-incrementing session label
func generateSessionLabel(prefix string) string {
	// TODO: Implement proper session label generation with persistence
	// For now, return a simple incremented label
	return fmt.Sprintf("%s-1", prefix)
}

// GetConfig returns the global configuration
// This should be called after cobra initialization
func GetConfig() *config.Config {
	if appConfig == nil {
		// Fallback to default config if not initialized
		return config.DefaultConfig()
	}
	return appConfig
}