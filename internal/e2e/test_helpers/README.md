# Test Helper Applications

This directory contains simple Go applications used for end-to-end testing of LogMCP functionality.

## Applications

### simple_app.go
A basic application that logs periodically to stdout, stderr, and using the log package.

**Usage:**
```bash
go run simple_app.go [interval]
```
- `interval`: Optional duration between log messages (default: 1s)

**Behavior:**
- Logs initial startup messages
- Periodically logs tick messages
- Alternates between stdout and stderr for output

### crash_app.go
An application that crashes after a specified delay with a configurable exit code.

**Usage:**
```bash
go run crash_app.go [delay] [exit_code]
```
- `delay`: Optional duration before crash (default: 2s)
- `exit_code`: Optional exit code (default: 1)

**Behavior:**
- Logs normally for the specified delay
- Outputs error messages before crashing
- Exits with the specified exit code

### fork_app.go
An application that creates child processes to test process tree handling.

**Usage:**
```bash
go run fork_app.go [num_children]
```
- `num_children`: Number of child processes to create (default: 2, max: 10)

**Behavior:**
- Creates the specified number of child processes
- Each child runs for 5 seconds logging periodically
- Parent continues running after children exit
- Tests process tree cleanup functionality

### stdin_app.go
An interactive application that reads from stdin and processes commands.

**Usage:**
```bash
go run stdin_app.go
```

**Commands:**
- `quit` or `exit`: Gracefully exit the application
- `error`: Generate an error message to stderr
- `crash`: Trigger a panic for testing crash handling
- `status`: Display current status
- `upper:<text>`: Convert text to uppercase
- `lower:<text>`: Convert text to lowercase
- `repeat:<count>:<text>`: Repeat text the specified number of times

**Behavior:**
- Echoes all input
- Processes special commands
- Logs all interactions
- Provides interactive prompt

## Building Test Applications

These applications are typically run directly with `go run` during tests, but can be compiled if needed:

```bash
go build -o simple_app simple_app.go
go build -o crash_app crash_app.go
go build -o fork_app fork_app.go
go build -o stdin_app stdin_app.go
```

## Usage in Tests

The test framework provides helper methods to run these applications:

```go
// Start simple app
err := server.StartTestProcess("simple-test", "go", "run", server.TestAppPath("simple_app.go"))

// Start crash app with custom delay
err := server.StartTestProcess("crash-test", "go", "run", server.TestAppPath("crash_app.go"), "5s", "42")

// Start stdin app and send input
err := server.StartTestProcess("stdin-test", "go", "run", server.TestAppPath("stdin_app.go"))
server.SendStdin("stdin-test", "Hello World\n")
server.SendStdin("stdin-test", "quit\n")
```