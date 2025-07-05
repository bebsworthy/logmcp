# LogMCP Makefile

.PHONY: all build test fmt lint check clean help

# Default target
all: check build

# Build the binary
build:
	@echo "Building logmcp..."
	@go build -o logmcp .

# Run tests
test:
	@echo "Running tests..."
	@go test ./...

# Format code
fmt:
	@echo "Formatting code..."
	@gofmt -s -w .
	@go mod tidy

# Run linter
lint:
	@echo "Running linter..."
	@golangci-lint run ./...

# Run all checks (format, lint, test)
check: fmt lint test
	@echo "All checks passed!"

# Clean build artifacts
clean:
	@echo "Cleaning..."
	@rm -f logmcp
	@rm -f logmcp-*
	@go clean -cache

# Install development tools
install-tools:
	@echo "Installing development tools..."
	@go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest

# Quick test - run only unit tests, not e2e
test-unit:
	@echo "Running unit tests..."
	@go test $(shell go list ./... | grep -v /e2e)

# Run e2e tests only
test-e2e: build
	@echo "Running e2e tests..."
	@go test ./internal/e2e/...

# Help
help:
	@echo "Available targets:"
	@echo "  make build       - Build the binary"
	@echo "  make test        - Run all tests"
	@echo "  make test-unit   - Run unit tests only"
	@echo "  make test-e2e    - Run e2e tests only"
	@echo "  make fmt         - Format code"
	@echo "  make lint        - Run linter"
	@echo "  make check       - Run fmt, lint, and tests"
	@echo "  make clean       - Clean build artifacts"
	@echo "  make install-tools - Install development tools"
	@echo "  make help        - Show this help"