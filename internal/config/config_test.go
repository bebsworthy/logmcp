package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestDefaultConfig tests the default configuration
func TestDefaultConfig(t *testing.T) {
	config := DefaultConfig()

	if config == nil {
		t.Fatal("Expected default config to be non-nil")
	}

	// Test server defaults
	if config.Server.Host != "localhost" {
		t.Errorf("Expected default host 'localhost', got '%s'", config.Server.Host)
	}

	if config.Server.WebSocketPort != 8765 {
		t.Errorf("Expected default websocket port 8765, got %d", config.Server.WebSocketPort)
	}

	if config.Server.MCPTransport != "stdio" {
		t.Errorf("Expected default MCP transport 'stdio', got '%s'", config.Server.MCPTransport)
	}

	// Test buffer defaults
	if config.Buffer.MaxAge != 5*time.Minute {
		t.Errorf("Expected default max age 5m, got %v", config.Buffer.MaxAge)
	}

	if config.Buffer.MaxSize != "5MB" {
		t.Errorf("Expected default max size '5MB', got '%s'", config.Buffer.MaxSize)
	}

	// Test process defaults
	if config.Process.Timeout != 30*time.Second {
		t.Errorf("Expected default timeout 30s, got %v", config.Process.Timeout)
	}

	if config.Process.MaxRestarts != 3 {
		t.Errorf("Expected default max restarts 3, got %d", config.Process.MaxRestarts)
	}

	// Test WebSocket defaults
	if config.WebSocket.ReconnectMaxAttempts != 10 {
		t.Errorf("Expected default max attempts 10, got %d", config.WebSocket.ReconnectMaxAttempts)
	}

	// Test logging defaults
	if config.Logging.Level != "info" {
		t.Errorf("Expected default log level 'info', got '%s'", config.Logging.Level)
	}

	if config.Logging.Format != "text" {
		t.Errorf("Expected default log format 'text', got '%s'", config.Logging.Format)
	}
}

// TestLoadConfig_NoFile tests loading config when no file exists
func TestLoadConfig_NoFile(t *testing.T) {
	// Test 1: Explicit non-existent file should error
	_, err := LoadConfig("/tmp/nonexistent-config.yaml")
	if err == nil {
		t.Error("Expected error when specific config file doesn't exist")
	}

	// Test 2: Empty config file path should use defaults
	config, err := LoadConfig("")
	if err != nil {
		t.Fatalf("Expected no error when no config file specified, got %v", err)
	}

	if config == nil {
		t.Fatal("Expected config to be loaded with defaults")
	}

	// Should have default values
	if config.Server.Host != "localhost" {
		t.Errorf("Expected default host, got '%s'", config.Server.Host)
	}
}

// TestLoadConfig_WithFile tests loading config from file
func TestLoadConfig_WithFile(t *testing.T) {
	// Create temporary config file
	tmpfile, err := os.CreateTemp("", "logmcp-config-*.yaml")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer os.Remove(tmpfile.Name())

	// Write test config
	configContent := `
server:
  host: "0.0.0.0"
  websocket_port: 9000
  mcp_transport: "unix:/tmp/mcp.sock"

buffer:
  max_age: "10m"
  max_size: "10MB"

logging:
  level: "debug"
  verbose: true
`

	if _, err := tmpfile.WriteString(configContent); err != nil {
		t.Fatalf("Failed to write config file: %v", err)
	}
	tmpfile.Close()

	// Load config
	config, err := LoadConfig(tmpfile.Name())
	if err != nil {
		t.Fatalf("Failed to load config: %v", err)
	}

	// Verify values were loaded
	if config.Server.Host != "0.0.0.0" {
		t.Errorf("Expected host '0.0.0.0', got '%s'", config.Server.Host)
	}

	if config.Server.WebSocketPort != 9000 {
		t.Errorf("Expected port 9000, got %d", config.Server.WebSocketPort)
	}

	if config.Server.MCPTransport != "unix:/tmp/mcp.sock" {
		t.Errorf("Expected MCP transport 'unix:/tmp/mcp.sock', got '%s'", config.Server.MCPTransport)
	}

	if config.Buffer.MaxAge != 10*time.Minute {
		t.Errorf("Expected max age 10m, got %v", config.Buffer.MaxAge)
	}

	if config.Buffer.MaxSize != "10MB" {
		t.Errorf("Expected max size '10MB', got '%s'", config.Buffer.MaxSize)
	}

	if config.Logging.Level != "debug" {
		t.Errorf("Expected log level 'debug', got '%s'", config.Logging.Level)
	}

	if !config.Logging.Verbose {
		t.Error("Expected verbose logging to be true")
	}
}

// TestLoadConfig_InvalidFile tests loading invalid config file
func TestLoadConfig_InvalidFile(t *testing.T) {
	// Create temporary invalid config file
	tmpfile, err := os.CreateTemp("", "logmcp-config-*.yaml")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer os.Remove(tmpfile.Name())

	// Write invalid YAML
	if _, err := tmpfile.WriteString("invalid: yaml: content:\n  - broken"); err != nil {
		t.Fatalf("Failed to write config file: %v", err)
	}
	tmpfile.Close()

	// Load config should fail
	_, err = LoadConfig(tmpfile.Name())
	if err == nil {
		t.Error("Expected error when loading invalid config file")
	}
}

// TestValidateConfig tests configuration validation
func TestValidateConfig(t *testing.T) {
	tests := []struct {
		name         string
		modifyConfig func(*Config)
		expectError  bool
	}{
		{
			name: "valid config",
			modifyConfig: func(c *Config) {
				// Keep defaults
			},
			expectError: false,
		},
		{
			name: "empty host",
			modifyConfig: func(c *Config) {
				c.Server.Host = ""
			},
			expectError: true,
		},
		{
			name: "invalid port - too low",
			modifyConfig: func(c *Config) {
				c.Server.WebSocketPort = 0
			},
			expectError: true,
		},
		{
			name: "invalid port - too high",
			modifyConfig: func(c *Config) {
				c.Server.WebSocketPort = 70000
			},
			expectError: true,
		},
		{
			name: "invalid MCP transport",
			modifyConfig: func(c *Config) {
				c.Server.MCPTransport = "invalid"
			},
			expectError: true,
		},
		{
			name: "negative max age",
			modifyConfig: func(c *Config) {
				c.Buffer.MaxAge = -1 * time.Second
			},
			expectError: true,
		},
		{
			name: "invalid size string",
			modifyConfig: func(c *Config) {
				c.Buffer.MaxSize = "invalid"
			},
			expectError: true,
		},
		{
			name: "negative timeout",
			modifyConfig: func(c *Config) {
				c.Process.Timeout = -1 * time.Second
			},
			expectError: true,
		},
		{
			name: "negative max restarts",
			modifyConfig: func(c *Config) {
				c.Process.MaxRestarts = -1
			},
			expectError: true,
		},
		{
			name: "invalid log level",
			modifyConfig: func(c *Config) {
				c.Logging.Level = "invalid"
			},
			expectError: true,
		},
		{
			name: "invalid log format",
			modifyConfig: func(c *Config) {
				c.Logging.Format = "invalid"
			},
			expectError: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			config := DefaultConfig()
			test.modifyConfig(config)

			err := validateConfig(config)

			if test.expectError && err == nil {
				t.Error("Expected validation error but got none")
			}

			if !test.expectError && err != nil {
				t.Errorf("Expected no validation error but got: %v", err)
			}
		})
	}
}

// TestParseSize tests size string parsing
func TestParseSize(t *testing.T) {
	tests := []struct {
		input    string
		expected int64
		hasError bool
	}{
		{"1B", 1, false},
		{"1KB", 1024, false},
		{"1MB", 1024 * 1024, false},
		{"1GB", 1024 * 1024 * 1024, false},
		{"5MB", 5 * 1024 * 1024, false},
		{"64KB", 64 * 1024, false},
		{"1", 1, false},      // No unit = bytes
		{"1kb", 1024, false}, // Case insensitive
		{"1mb", 1024 * 1024, false},
		{"", 0, true},        // Empty string
		{"invalid", 0, true}, // Invalid format
		{"-1MB", 0, true},    // Negative value
		{"1.5MB", 0, true},   // Float value
	}

	for _, test := range tests {
		t.Run(test.input, func(t *testing.T) {
			result, err := ParseSize(test.input)

			if test.hasError {
				if err == nil {
					t.Errorf("Expected error for input '%s'", test.input)
				}
			} else {
				if err != nil {
					t.Errorf("Unexpected error for input '%s': %v", test.input, err)
				} else if result != test.expected {
					t.Errorf("For input '%s', expected %d bytes, got %d", test.input, test.expected, result)
				}
			}
		})
	}
}

// TestGetConfigPaths tests config path discovery
func TestGetConfigPaths(t *testing.T) {
	paths := GetConfigPaths()

	if len(paths) == 0 {
		t.Error("Expected at least some config paths")
	}

	// Should include current directory
	found := false
	for _, path := range paths {
		if filepath.Dir(path) == "." {
			found = true
			break
		}
	}

	if !found {
		t.Error("Expected current directory to be in config paths")
	}
}

// TestGetEnvVarName tests environment variable name generation
func TestGetEnvVarName(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"server.host", "LOGMCP_SERVER_HOST"},
		{"server.websocket_port", "LOGMCP_SERVER_WEBSOCKET_PORT"},
		{"buffer.max_age", "LOGMCP_BUFFER_MAX_AGE"},
		{"logging.level", "LOGMCP_LOGGING_LEVEL"},
	}

	for _, test := range tests {
		result := GetEnvVarName(test.input)
		if result != test.expected {
			t.Errorf("For input '%s', expected '%s', got '%s'", test.input, test.expected, result)
		}
	}
}

// TestExampleConfig tests the example configuration
func TestExampleConfig(t *testing.T) {
	config := ExampleConfig()

	if config == nil {
		t.Fatal("Expected example config to be non-nil")
	}

	// Example config should have some different values from defaults
	if config.Server.Host != "0.0.0.0" {
		t.Errorf("Expected example host '0.0.0.0', got '%s'", config.Server.Host)
	}

	if config.Logging.Level != "debug" {
		t.Errorf("Expected example log level 'debug', got '%s'", config.Logging.Level)
	}

	if !config.Logging.Verbose {
		t.Error("Expected example verbose logging to be true")
	}

	if !config.Development.DebugMode {
		t.Error("Expected example debug mode to be true")
	}

	// Should still be valid
	if err := validateConfig(config); err != nil {
		t.Errorf("Example config should be valid: %v", err)
	}
}

// TestEnvironmentVariables tests environment variable loading
func TestEnvironmentVariables(t *testing.T) {
	// Set test environment variables
	testEnvVars := map[string]string{
		"LOGMCP_SERVER_HOST":           "test-host",
		"LOGMCP_SERVER_WEBSOCKET_PORT": "9999",
		"LOGMCP_LOGGING_LEVEL":         "debug",
		"LOGMCP_LOGGING_VERBOSE":       "true",
	}

	// Set environment variables
	for key, value := range testEnvVars {
		os.Setenv(key, value)
		defer os.Unsetenv(key)
	}

	// Load config (no file)
	config, err := LoadConfig("")
	if err != nil {
		t.Fatalf("Failed to load config: %v", err)
	}

	// Verify environment variables were used
	if config.Server.Host != "test-host" {
		t.Errorf("Expected host 'test-host', got '%s'", config.Server.Host)
	}

	if config.Server.WebSocketPort != 9999 {
		t.Errorf("Expected port 9999, got %d", config.Server.WebSocketPort)
	}

	if config.Logging.Level != "debug" {
		t.Errorf("Expected log level 'debug', got '%s'", config.Logging.Level)
	}

	if !config.Logging.Verbose {
		t.Error("Expected verbose logging to be true")
	}
}

// BenchmarkLoadConfig benchmarks configuration loading
func BenchmarkLoadConfig(b *testing.B) {
	for i := 0; i < b.N; i++ {
		_, err := LoadConfig("")
		if err != nil {
			b.Fatalf("Failed to load config: %v", err)
		}
	}
}

// BenchmarkParseSize benchmarks size parsing
func BenchmarkParseSize(b *testing.B) {
	for i := 0; i < b.N; i++ {
		_, err := ParseSize("5MB")
		if err != nil {
			b.Fatalf("Failed to parse size: %v", err)
		}
	}
}
