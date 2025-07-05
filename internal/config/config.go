// Package config provides configuration management for LogMCP.
//
// This package handles loading configuration from multiple sources:
// - Configuration files (YAML, JSON, TOML)
// - Environment variables
// - Command line flags
// - Default values
//
// Configuration is loaded in order of precedence (highest to lowest):
// 1. Command line flags
// 2. Environment variables
// 3. Configuration file
// 4. Default values
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/viper"
)

// Config represents the complete LogMCP configuration
type Config struct {
	Server      ServerConfig      `mapstructure:"server" yaml:"server"`
	Buffer      BufferConfig      `mapstructure:"buffer" yaml:"buffer"`
	Process     ProcessConfig     `mapstructure:"process" yaml:"process"`
	WebSocket   WebSocketConfig   `mapstructure:"websocket" yaml:"websocket"`
	Logging     LoggingConfig     `mapstructure:"logging" yaml:"logging"`
	Development DevelopmentConfig `mapstructure:"development" yaml:"development"`
}

// ServerConfig contains server-specific configuration
type ServerConfig struct {
	Host          string `mapstructure:"host" yaml:"host"`
	WebSocketPort int    `mapstructure:"websocket_port" yaml:"websocket_port"`
	MCPTransport  string `mapstructure:"mcp_transport" yaml:"mcp_transport"`
}

// BufferConfig contains ring buffer configuration
type BufferConfig struct {
	MaxAge          time.Duration `mapstructure:"max_age" yaml:"max_age"`
	MaxSize         string        `mapstructure:"max_size" yaml:"max_size"`
	MaxLineSize     string        `mapstructure:"max_line_size" yaml:"max_line_size"`
	CleanupInterval time.Duration `mapstructure:"cleanup_interval" yaml:"cleanup_interval"`
}

// ProcessConfig contains process management configuration
type ProcessConfig struct {
	Timeout                 time.Duration `mapstructure:"timeout" yaml:"timeout"`
	DefaultWorkingDir       string        `mapstructure:"default_working_dir" yaml:"default_working_dir"`
	MaxRestarts             int           `mapstructure:"max_restarts" yaml:"max_restarts"`
	RestartDelay            time.Duration `mapstructure:"restart_delay" yaml:"restart_delay"`
	GracefulShutdownTimeout time.Duration `mapstructure:"graceful_shutdown_timeout" yaml:"graceful_shutdown_timeout"`
}

// WebSocketConfig contains WebSocket client configuration
type WebSocketConfig struct {
	ReconnectInitialDelay time.Duration `mapstructure:"reconnect_initial_delay" yaml:"reconnect_initial_delay"`
	ReconnectMaxDelay     time.Duration `mapstructure:"reconnect_max_delay" yaml:"reconnect_max_delay"`
	ReconnectMaxAttempts  int           `mapstructure:"reconnect_max_attempts" yaml:"reconnect_max_attempts"`
	PingInterval          time.Duration `mapstructure:"ping_interval" yaml:"ping_interval"`
	WriteTimeout          time.Duration `mapstructure:"write_timeout" yaml:"write_timeout"`
	ReadTimeout           time.Duration `mapstructure:"read_timeout" yaml:"read_timeout"`
	HandshakeTimeout      time.Duration `mapstructure:"handshake_timeout" yaml:"handshake_timeout"`
}

// LoggingConfig contains logging configuration
type LoggingConfig struct {
	Level      string `mapstructure:"level" yaml:"level"`
	Format     string `mapstructure:"format" yaml:"format"`
	OutputFile string `mapstructure:"output_file" yaml:"output_file"`
	Verbose    bool   `mapstructure:"verbose" yaml:"verbose"`
}

// DevelopmentConfig contains development and debugging options
type DevelopmentConfig struct {
	EnableProfiling bool `mapstructure:"enable_profiling" yaml:"enable_profiling"`
	ProfilingPort   int  `mapstructure:"profiling_port" yaml:"profiling_port"`
	DebugMode       bool `mapstructure:"debug_mode" yaml:"debug_mode"`
	MetricsEnabled  bool `mapstructure:"metrics_enabled" yaml:"metrics_enabled"`
}

// DefaultConfig returns the default configuration
func DefaultConfig() *Config {
	return &Config{
		Server: ServerConfig{
			Host:          "localhost",
			WebSocketPort: 8765,
			MCPTransport:  "stdio",
		},
		Buffer: BufferConfig{
			MaxAge:          5 * time.Minute,
			MaxSize:         "5MB",
			MaxLineSize:     "64KB",
			CleanupInterval: 30 * time.Second,
		},
		Process: ProcessConfig{
			Timeout:                 30 * time.Second,
			DefaultWorkingDir:       ".",
			MaxRestarts:             3,
			RestartDelay:            5 * time.Second,
			GracefulShutdownTimeout: 10 * time.Second,
		},
		WebSocket: WebSocketConfig{
			ReconnectInitialDelay: 1 * time.Second,
			ReconnectMaxDelay:     30 * time.Second,
			ReconnectMaxAttempts:  10,
			PingInterval:          30 * time.Second,
			WriteTimeout:          10 * time.Second,
			ReadTimeout:           60 * time.Second,
			HandshakeTimeout:      10 * time.Second,
		},
		Logging: LoggingConfig{
			Level:      "info",
			Format:     "text",
			OutputFile: "",
			Verbose:    false,
		},
		Development: DevelopmentConfig{
			EnableProfiling: false,
			ProfilingPort:   6060,
			DebugMode:       false,
			MetricsEnabled:  false,
		},
	}
}

// LoadConfig loads configuration from various sources
func LoadConfig(configFile string) (*Config, error) {
	v := viper.New()

	// Set defaults
	setDefaults(v)

	// Configure environment variable handling
	v.SetEnvPrefix("LOGMCP")
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()

	// Set config file if provided
	if configFile != "" {
		v.SetConfigFile(configFile)
	} else {
		// Search for config file in common locations
		v.SetConfigName("config")
		v.SetConfigType("yaml")
		v.AddConfigPath(".")
		v.AddConfigPath("$HOME/.logmcp")
		v.AddConfigPath("/etc/logmcp")
	}

	// Read config file (optional)
	if err := v.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); ok {
			// If a specific config file was provided and not found, that's an error
			if configFile != "" {
				return nil, fmt.Errorf("config file not found: %s", configFile)
			}
			// Otherwise, config file not found is okay, we'll use defaults
		} else {
			// Other errors are always reported
			return nil, fmt.Errorf("failed to read config file: %w", err)
		}
	}

	// Unmarshal into config struct
	var config Config
	if err := v.Unmarshal(&config); err != nil {
		return nil, fmt.Errorf("failed to unmarshal config: %w", err)
	}

	// Validate configuration
	if err := validateConfig(&config); err != nil {
		return nil, fmt.Errorf("invalid configuration: %w", err)
	}

	return &config, nil
}

// setDefaults sets default values in viper
func setDefaults(v *viper.Viper) {
	defaults := DefaultConfig()

	// Server defaults
	v.SetDefault("server.host", defaults.Server.Host)
	v.SetDefault("server.websocket_port", defaults.Server.WebSocketPort)
	v.SetDefault("server.mcp_transport", defaults.Server.MCPTransport)

	// Buffer defaults
	v.SetDefault("buffer.max_age", defaults.Buffer.MaxAge)
	v.SetDefault("buffer.max_size", defaults.Buffer.MaxSize)
	v.SetDefault("buffer.max_line_size", defaults.Buffer.MaxLineSize)
	v.SetDefault("buffer.cleanup_interval", defaults.Buffer.CleanupInterval)

	// Process defaults
	v.SetDefault("process.timeout", defaults.Process.Timeout)
	v.SetDefault("process.default_working_dir", defaults.Process.DefaultWorkingDir)
	v.SetDefault("process.max_restarts", defaults.Process.MaxRestarts)
	v.SetDefault("process.restart_delay", defaults.Process.RestartDelay)
	v.SetDefault("process.graceful_shutdown_timeout", defaults.Process.GracefulShutdownTimeout)

	// WebSocket defaults
	v.SetDefault("websocket.reconnect_initial_delay", defaults.WebSocket.ReconnectInitialDelay)
	v.SetDefault("websocket.reconnect_max_delay", defaults.WebSocket.ReconnectMaxDelay)
	v.SetDefault("websocket.reconnect_max_attempts", defaults.WebSocket.ReconnectMaxAttempts)
	v.SetDefault("websocket.ping_interval", defaults.WebSocket.PingInterval)
	v.SetDefault("websocket.write_timeout", defaults.WebSocket.WriteTimeout)
	v.SetDefault("websocket.read_timeout", defaults.WebSocket.ReadTimeout)
	v.SetDefault("websocket.handshake_timeout", defaults.WebSocket.HandshakeTimeout)

	// Logging defaults
	v.SetDefault("logging.level", defaults.Logging.Level)
	v.SetDefault("logging.format", defaults.Logging.Format)
	v.SetDefault("logging.output_file", defaults.Logging.OutputFile)
	v.SetDefault("logging.verbose", defaults.Logging.Verbose)

	// Development defaults
	v.SetDefault("development.enable_profiling", defaults.Development.EnableProfiling)
	v.SetDefault("development.profiling_port", defaults.Development.ProfilingPort)
	v.SetDefault("development.debug_mode", defaults.Development.DebugMode)
	v.SetDefault("development.metrics_enabled", defaults.Development.MetricsEnabled)
}

// validateConfig validates the loaded configuration
func validateConfig(config *Config) error {
	// Validate server configuration
	if config.Server.Host == "" {
		return fmt.Errorf("server.host cannot be empty")
	}

	if config.Server.WebSocketPort < 1 || config.Server.WebSocketPort > 65535 {
		return fmt.Errorf("server.websocket_port must be between 1 and 65535, got %d", config.Server.WebSocketPort)
	}

	if config.Server.MCPTransport != "stdio" && !strings.HasPrefix(config.Server.MCPTransport, "unix:") {
		return fmt.Errorf("server.mcp_transport must be 'stdio' or 'unix:/path/to/socket', got %s", config.Server.MCPTransport)
	}

	// Validate buffer configuration
	if config.Buffer.MaxAge <= 0 {
		return fmt.Errorf("buffer.max_age must be positive, got %v", config.Buffer.MaxAge)
	}

	if config.Buffer.CleanupInterval <= 0 {
		return fmt.Errorf("buffer.cleanup_interval must be positive, got %v", config.Buffer.CleanupInterval)
	}

	// Validate buffer size strings
	if err := validateSizeString(config.Buffer.MaxSize, "buffer.max_size"); err != nil {
		return err
	}

	if err := validateSizeString(config.Buffer.MaxLineSize, "buffer.max_line_size"); err != nil {
		return err
	}

	// Validate process configuration
	if config.Process.Timeout <= 0 {
		return fmt.Errorf("process.timeout must be positive, got %v", config.Process.Timeout)
	}

	if config.Process.MaxRestarts < 0 {
		return fmt.Errorf("process.max_restarts must be non-negative, got %d", config.Process.MaxRestarts)
	}

	if config.Process.RestartDelay < 0 {
		return fmt.Errorf("process.restart_delay must be non-negative, got %v", config.Process.RestartDelay)
	}

	// Validate WebSocket configuration
	if config.WebSocket.ReconnectMaxAttempts < 0 {
		return fmt.Errorf("websocket.reconnect_max_attempts must be non-negative, got %d", config.WebSocket.ReconnectMaxAttempts)
	}

	if config.WebSocket.PingInterval <= 0 {
		return fmt.Errorf("websocket.ping_interval must be positive, got %v", config.WebSocket.PingInterval)
	}

	// Validate logging configuration
	validLogLevels := map[string]bool{
		"debug": true, "info": true, "warn": true, "error": true, "fatal": true,
	}
	if !validLogLevels[config.Logging.Level] {
		return fmt.Errorf("logging.level must be one of: debug, info, warn, error, fatal, got %s", config.Logging.Level)
	}

	validLogFormats := map[string]bool{
		"text": true, "json": true,
	}
	if !validLogFormats[config.Logging.Format] {
		return fmt.Errorf("logging.format must be 'text' or 'json', got %s", config.Logging.Format)
	}

	// Validate development configuration
	if config.Development.ProfilingPort < 1 || config.Development.ProfilingPort > 65535 {
		return fmt.Errorf("development.profiling_port must be between 1 and 65535, got %d", config.Development.ProfilingPort)
	}

	return nil
}

// validateSizeString validates size strings like "5MB", "64KB"
func validateSizeString(size, field string) error {
	if size == "" {
		return fmt.Errorf("%s cannot be empty", field)
	}

	// Parse size string
	_, err := ParseSize(size)
	if err != nil {
		return fmt.Errorf("invalid %s: %w", field, err)
	}

	return nil
}

// ParseSize parses size strings like "5MB", "64KB" into bytes
func ParseSize(size string) (int64, error) {
	if size == "" {
		return 0, fmt.Errorf("size string cannot be empty")
	}

	// Convert to uppercase for case-insensitive parsing
	size = strings.ToUpper(size)

	// Define size units in order (longest first to avoid conflicts)
	units := []struct {
		suffix     string
		multiplier int64
	}{
		{"TB", 1024 * 1024 * 1024 * 1024},
		{"GB", 1024 * 1024 * 1024},
		{"MB", 1024 * 1024},
		{"KB", 1024},
		{"B", 1},
	}

	// Find the unit
	var multiplier int64 = 1 // Default to bytes
	var valueStr string

	for _, unit := range units {
		if strings.HasSuffix(size, unit.suffix) {
			multiplier = unit.multiplier
			valueStr = strings.TrimSuffix(size, unit.suffix)
			break
		}
	}

	// If no unit found, assume bytes
	if valueStr == "" {
		valueStr = size
		multiplier = 1
	}

	// Parse the numeric value (handle float values by rejecting them)
	var value int64
	var floatValue float64

	// Check if it's a float first
	if n, err := fmt.Sscanf(valueStr, "%f", &floatValue); err == nil && n == 1 {
		// If it parsed as float but is actually an integer, it's okay
		if floatValue == float64(int64(floatValue)) {
			value = int64(floatValue)
		} else {
			return 0, fmt.Errorf("float values not supported in size string: %s", valueStr)
		}
	} else {
		return 0, fmt.Errorf("invalid numeric value in size string: %s", valueStr)
	}

	if value < 0 {
		return 0, fmt.Errorf("size value cannot be negative: %d", value)
	}

	return value * multiplier, nil
}

// GetConfigPaths returns the paths where config files are searched
func GetConfigPaths() []string {
	paths := []string{
		"./config.yaml",
		"./config.yml",
		"./config.json",
		"./config.toml",
	}

	if home, err := os.UserHomeDir(); err == nil {
		paths = append(paths,
			filepath.Join(home, ".logmcp", "config.yaml"),
			filepath.Join(home, ".logmcp", "config.yml"),
			filepath.Join(home, ".logmcp", "config.json"),
			filepath.Join(home, ".logmcp", "config.toml"),
		)
	}

	paths = append(paths,
		"/etc/logmcp/config.yaml",
		"/etc/logmcp/config.yml",
		"/etc/logmcp/config.json",
		"/etc/logmcp/config.toml",
	)

	return paths
}

// GetEnvVarName returns the environment variable name for a config key
func GetEnvVarName(key string) string {
	return "LOGMCP_" + strings.ToUpper(strings.ReplaceAll(key, ".", "_"))
}

// ExampleConfig returns an example configuration for documentation
func ExampleConfig() *Config {
	config := DefaultConfig()

	// Customize some values for the example
	config.Server.Host = "0.0.0.0"
	config.Logging.Level = "debug"
	config.Logging.Verbose = true
	config.Development.DebugMode = true
	config.Development.MetricsEnabled = true

	return config
}
