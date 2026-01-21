#!/bin/bash
#
# MAP CLI Installer
# Usage: curl -fsSL https://raw.githubusercontent.com/pmarsceill/mapcli/main/install.sh | bash
#

set -e

REPO="pmarsceill/mapcli"
INSTALL_DIR="${MAP_INSTALL_DIR:-$HOME/.local/bin}"

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

info() {
    echo -e "${GREEN}[INFO]${NC} $1"
}

warn() {
    echo -e "${YELLOW}[WARN]${NC} $1"
}

error() {
    echo -e "${RED}[ERROR]${NC} $1"
    exit 1
}

# Detect OS
detect_os() {
    local os
    os=$(uname -s)
    case "$os" in
        Linux)
            echo "linux"
            ;;
        Darwin)
            echo "darwin"
            ;;
        *)
            error "Unsupported operating system: $os"
            ;;
    esac
}

# Detect architecture
detect_arch() {
    local arch
    arch=$(uname -m)
    case "$arch" in
        x86_64|amd64)
            echo "amd64"
            ;;
        arm64|aarch64)
            echo "arm64"
            ;;
        *)
            error "Unsupported architecture: $arch"
            ;;
    esac
}

# Get latest release version from GitHub API
get_latest_version() {
    local version
    version=$(curl -fsSL "https://api.github.com/repos/${REPO}/releases/latest" | grep '"tag_name":' | sed -E 's/.*"([^"]+)".*/\1/')
    if [ -z "$version" ]; then
        error "Failed to fetch latest version"
    fi
    echo "$version"
}

main() {
    info "Installing MAP CLI..."

    # Detect platform
    local os arch platform
    os=$(detect_os)
    arch=$(detect_arch)
    platform="${os}-${arch}"
    info "Detected platform: $platform"

    # Get latest version
    local version
    version=$(get_latest_version)
    info "Latest version: $version"

    # Construct download URL
    local download_url="https://github.com/${REPO}/releases/download/${version}/map-${platform}.tar.gz"
    info "Downloading from: $download_url"

    # Create install directory
    mkdir -p "$INSTALL_DIR"

    # Download and extract
    local tmp_dir
    tmp_dir=$(mktemp -d)
    trap 'rm -rf "$tmp_dir"' EXIT

    if ! curl -fsSL "$download_url" -o "$tmp_dir/map.tar.gz"; then
        error "Failed to download release"
    fi

    if ! tar -xzf "$tmp_dir/map.tar.gz" -C "$tmp_dir"; then
        error "Failed to extract archive"
    fi

    # Install binaries
    mv "$tmp_dir/map" "$INSTALL_DIR/map"
    mv "$tmp_dir/mapd" "$INSTALL_DIR/mapd"
    chmod +x "$INSTALL_DIR/map" "$INSTALL_DIR/mapd"

    info "Installed map and mapd to $INSTALL_DIR"

    # Verify installation
    if "$INSTALL_DIR/map" --version > /dev/null 2>&1; then
        info "Installation verified: $($INSTALL_DIR/map --version)"
    else
        warn "Installation completed but verification failed"
    fi

    # Check if install dir is in PATH
    if [[ ":$PATH:" != *":$INSTALL_DIR:"* ]]; then
        warn "$INSTALL_DIR is not in your PATH"
        echo ""
        echo "Add it to your shell profile:"
        echo "  export PATH=\"\$PATH:$INSTALL_DIR\""
        echo ""
    fi

    info "Installation complete!"
}

main "$@"
