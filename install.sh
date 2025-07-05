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
    echo -e "${GREEN}$1${NC}"
}

info() {
    echo -e "${YELLOW}$1${NC}"
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
        echo ""
        info "Note: $HOME/.local/bin is not in your PATH."
        info "Add this line to your shell configuration file (.bashrc, .zshrc, etc.):"
        echo "  export PATH=\"\$HOME/.local/bin:\$PATH\""
        echo ""
    fi
}

# Verify installation
verify_installation() {
    if command -v "$BINARY_NAME" >/dev/null 2>&1; then
        success "LogMCP installed successfully!"
        echo ""
        "$BINARY_NAME" version
        echo ""
        info "Run 'logmcp serve' to start the server"
    else
        error "Installation verification failed"
    fi
}

# Show usage
usage() {
    echo "Usage: $0 [VERSION]"
    echo ""
    echo "Install LogMCP binary"
    echo ""
    echo "Arguments:"
    echo "  VERSION    Optional: specific version to install (e.g., 2024-07-05T14:32:15)"
    echo "             If not specified, installs the latest release"
    echo ""
    echo "Examples:"
    echo "  $0                    # Install latest version"
    echo "  $0 2024-07-05T14:32:15  # Install specific version"
    exit 0
}

# Main installation flow
main() {
    # Check for help
    if [ "$1" = "-h" ] || [ "$1" = "--help" ]; then
        usage
    fi
    
    # Check for version parameter
    VERSION=""
    if [ $# -gt 0 ]; then
        VERSION="$1"
    fi
    
    echo "LogMCP Installer"
    echo "================"
    echo ""
    echo "⚠️  WARNING: This software is 100% AI-generated."
    echo "   Use at your own risk. No warranty provided."
    echo ""
    read -p "Do you want to continue? (y/N) " -n 1 -r
    echo
    if [[ ! $REPLY =~ ^[Yy]$ ]]; then
        echo "Installation cancelled."
        exit 0
    fi
    
    detect_platform
    get_release_url "$VERSION"
    
    BINARY_PATH=$(download_binary)
    install_binary "$BINARY_PATH"
    verify_installation
    
    echo ""
    echo "Next steps:"
    echo "1. Start the server: logmcp serve"
    echo "2. Run a process: logmcp run --label my-app -- <command>"
    echo "3. Configure Claude Code: See https://github.com/${REPO}#claude-code-integration"
}

# Run main function
main "$@"