#!/bin/bash

# LogMCP Installation Script
# This script is AI-generated - use at your own risk

set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# Configuration
REPO="bebsworthy/logmcp"
INSTALL_DIR="$HOME/.local/bin"
BINARY_NAME="logmcp"

# Functions
error() {
    echo -e "${RED}Error: $1${NC}" >&2
    exit 1
}

success() {
    echo -e "${GREEN}$1${NC}" >&2
}

info() {
    echo -e "${YELLOW}$1${NC}" >&2
}

# Detect OS and architecture
detect_platform() {
    OS=$(uname -s | tr '[:upper:]' '[:lower:]')
    ARCH=$(uname -m)

    case "$OS" in
        darwin)
            PLATFORM="darwin"
            ;;
        linux)
            PLATFORM="linux"
            ;;
        mingw*|cygwin*|msys*)
            PLATFORM="windows"
            ;;
        *)
            error "Unsupported operating system: $OS"
            ;;
    esac

    case "$ARCH" in
        x86_64|amd64)
            ARCH="amd64"
            ;;
        arm64|aarch64)
            ARCH="arm64"
            ;;
        *)
            error "Unsupported architecture: $ARCH"
            ;;
    esac

    # Special case: macOS arm64 is supported, but not Linux arm64 yet
    if [ "$PLATFORM" = "linux" ] && [ "$ARCH" = "arm64" ]; then
        error "Linux ARM64 is not supported yet"
    fi

    # Windows uses .exe extension
    if [ "$PLATFORM" = "windows" ]; then
        BINARY_SUFFIX=".exe"
    else
        BINARY_SUFFIX=""
    fi

    ASSET_NAME="${BINARY_NAME}-${PLATFORM}-${ARCH}${BINARY_SUFFIX}"
}

# Get release URL (latest or specific version)
get_release_url() {
    if [ -n "$1" ]; then
        # Specific version requested
        RELEASE_TAG="$1"
        info "Installing specific version: $RELEASE_TAG"
    else
        # Get latest release
        RELEASE_TAG=$(curl -s "https://api.github.com/repos/${REPO}/releases/latest" | grep '"tag_name":' | sed -E 's/.*"([^"]+)".*/\1/')
        
        if [ -z "$RELEASE_TAG" ]; then
            error "Could not determine latest release"
        fi
        info "Installing latest version: $RELEASE_TAG"
    fi
    
    DOWNLOAD_URL="https://github.com/${REPO}/releases/download/${RELEASE_TAG}/${ASSET_NAME}"
}

# Download binary
download_binary() {
    info "Downloading LogMCP for ${PLATFORM}-${ARCH}..."
    
    TEMP_FILE=$(mktemp)
    
    if ! curl -L -o "$TEMP_FILE" "$DOWNLOAD_URL"; then
        rm -f "$TEMP_FILE"
        error "Failed to download binary from $DOWNLOAD_URL"
    fi
    
    # Make executable
    chmod +x "$TEMP_FILE"
    
    echo "$TEMP_FILE"
}

# Install binary
install_binary() {
    local binary_path=$1
    
    info "Installing to ${INSTALL_DIR}/${BINARY_NAME}..."
    
    # Create install directory if it doesn't exist
    if [ ! -d "$INSTALL_DIR" ]; then
        info "Creating directory: $INSTALL_DIR"
        mkdir -p "$INSTALL_DIR"
    fi
    
    # Install the binary
    mv "$binary_path" "${INSTALL_DIR}/${BINARY_NAME}"
    
    # Check if ~/.local/bin is in PATH
    if ! echo "$PATH" | grep -q "$HOME/.local/bin"; then
        echo "" >&2
        info "Note: $HOME/.local/bin is not in your PATH."
        info "Add this line to your shell configuration file (.bashrc, .zshrc, etc.):"
        echo "  export PATH=\"\$HOME/.local/bin:\$PATH\"" >&2
        echo "" >&2
    fi
}

# Verify installation
verify_installation() {
    if command -v "$BINARY_NAME" >/dev/null 2>&1; then
        success "LogMCP installed successfully!"
        echo "" >&2
        "$BINARY_NAME" version >&2
        echo "" >&2
        info "Run 'logmcp serve' to start the server"
    else
        error "Installation verification failed"
    fi
}

# Show usage
usage() {
    echo "Usage: $0 [-y|--yes] [VERSION]"
    echo ""
    echo "Install LogMCP binary"
    echo ""
    echo "Options:"
    echo "  -y, --yes  Skip confirmation prompt"
    echo ""
    echo "Arguments:"
    echo "  VERSION    Optional: specific version to install (e.g., 20240705143215)"
    echo "             If not specified, installs the latest release"
    echo ""
    echo "Environment variables:"
    echo "  LOGMCP_INSTALL_YES=true  Skip confirmation prompt"
    echo ""
    echo "Examples:"
    echo "  $0                    # Install latest version (interactive)"
    echo "  $0 -y                 # Install latest version (non-interactive)"
    echo "  $0 20240705143215     # Install specific version"
    echo "  $0 -y 20240705143215  # Install specific version (non-interactive)"
    echo ""
    echo "Non-interactive installation:"
    echo "  curl -sSL https://raw.githubusercontent.com/bebsworthy/logmcp/main/install.sh | bash -s -- -y"
    exit 0
}

# Main installation flow
main() {
    # Check for help
    if [ "$1" = "-h" ] || [ "$1" = "--help" ]; then
        usage
    fi
    
    # Check for -y flag
    SKIP_CONFIRM=""
    if [ "$1" = "-y" ] || [ "$1" = "--yes" ]; then
        SKIP_CONFIRM="$1"
        shift # Remove the flag from arguments
    fi
    
    # Check for version parameter
    VERSION=""
    if [ $# -gt 0 ]; then
        VERSION="$1"
    fi
    
    echo "LogMCP Installer" >&2
    echo "================" >&2
    echo "" >&2
    echo "⚠️  WARNING: This software is 100% AI-generated." >&2
    echo "   Use at your own risk. No warranty provided." >&2
    echo "" >&2
    
    # Check if we should skip confirmation (for CI or automated installs)
    if [ -n "$SKIP_CONFIRM" ] || [ "$LOGMCP_INSTALL_YES" = "true" ]; then
        echo "Proceeding with installation (confirmation skipped)..." >&2
    else
        # Try to read from terminal if available, otherwise skip confirmation
        if [ -t 0 ]; then
            read -p "Do you want to continue? (y/N) " -n 1 -r
            echo >&2
            if [[ ! $REPLY =~ ^[Yy]$ ]]; then
                echo "Installation cancelled." >&2
                exit 0
            fi
        else
            echo "Running in non-interactive mode. To skip this prompt, use:" >&2
            echo "  curl -sSL https://raw.githubusercontent.com/bebsworthy/logmcp/main/install.sh | bash -s -- -y" >&2
            echo "" >&2
            echo "Or set environment variable:" >&2
            echo "  curl -sSL https://raw.githubusercontent.com/bebsworthy/logmcp/main/install.sh | LOGMCP_INSTALL_YES=true bash" >&2
            echo "" >&2
            echo "Installation cancelled. Run with -y flag or LOGMCP_INSTALL_YES=true to proceed." >&2
            exit 1
        fi
    fi
    
    detect_platform
    get_release_url "$VERSION"
    
    BINARY_PATH=$(download_binary)
    install_binary "$BINARY_PATH"
    verify_installation
    
    echo "" >&2
    echo "Next steps:" >&2
    echo "1. Start the server: logmcp serve" >&2
    echo "2. Run a process: logmcp run --label my-app -- <command>" >&2
    echo "3. Configure Claude Code: See https://github.com/${REPO}#claude-code-integration" >&2
}

# Run main function
main "$@"