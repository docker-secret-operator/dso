#!/bin/bash
# ==============================================================================
# Docker Secret Operator (DSO) - Production Installer (V3.2)
# ==============================================================================
# This script performs a clean installation of the DSO Official Docker CLI plugin
#
# Supported OS: Linux (Ubuntu, Debian, CentOS, RHEL), macOS
# ==============================================================================

set -e

# Configuration
REPO_URL="https://github.com/docker-secret-operator/dso"
DOCKER_CONFIG=${DOCKER_CONFIG:-$HOME/.docker}

if [ "$EUID" -eq 0 ]; then
    PLUGIN_DIR="/usr/local/lib/docker/cli-plugins"
    SYSTEM_BIN_DIR="/usr/local/bin"
else
    PLUGIN_DIR="$DOCKER_CONFIG/cli-plugins"
    SYSTEM_BIN_DIR="$HOME/.local/bin"
fi

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

echo -e "${BLUE}==========================================${NC}"
echo -e "${BLUE}   Installing Docker Secret Operator (DSO) ${NC}"
echo -e "${BLUE}==========================================${NC}"

# 1. Dependency Checking
echo -e "${GREEN}[1/5] Checking dependencies...${NC}"

install_if_missing() {
    if ! command -v "$1" &> /dev/null; then
        echo -e "Installing $1..."
        if [ "$EUID" -eq 0 ]; then
            apt-get update && apt-get install -y "$1" || yum install -y "$1" || apk add "$1" || brew install "$1"
        else
            echo -e "${RED}Error: '$1' is missing. Please install it manually.${NC}"
            exit 1
        fi
    fi
}

install_if_missing "curl"
install_if_missing "git"
install_if_missing "tar"

# Check for Docker
if ! command -v docker &> /dev/null; then
    echo -e "${RED}Error: Docker is not installed. Please install Docker manually and try again.${NC}"
    exit 1
fi

# Check for Go (Minimum 1.21.0)
check_go() {
    if command -v go &> /dev/null; then
        GO_VERSION_OUT=$(go version 2>&1)
        if [[ $GO_VERSION_OUT != *"cannot find GOROOT"* ]] && [[ $GO_VERSION_OUT == *"go version"* ]]; then
            return 0
        fi
    fi
    return 1
}

if ! check_go; then
    echo -e "Go not found or broken. Searching for Go in common locations..."
    if [ -x "/usr/local/go/bin/go" ]; then
        export PATH="/usr/local/go/bin:$PATH"
        export GOROOT="/usr/local/go"
        echo -e "Found Go at /usr/local/go/bin/go. Using it."
    fi
fi

if ! check_go; then
    echo -e "${RED}Error: Go 1.21+ is required but not found or is broken. Please install Go manually and try again.${NC}"
    exit 1
else
    CURRENT_GO_MINOR=$(go version | grep -oP 'go1\.\K[0-9]+' | head -1)
    if [ -z "$CURRENT_GO_MINOR" ] || [ "$CURRENT_GO_MINOR" -lt 21 ]; then
        echo -e "${RED}Error: Go version is too old (need 1.21+). Please upgrade Go manually and try again.${NC}"
        exit 1
    else
        echo -e "${GREEN}Go $(go version | awk '{print $3}') detected.${NC}"
    fi
fi

# 2. Download/Prepare Project Files
echo -e "${GREEN}[2/5] Preparing DSO source...${NC}"

if [ -f "./go.mod" ] && grep -q "github.com/docker-secret-operator/dso" "./go.mod"; then
    echo -e "Local source detected. Using current directory."
    BUILD_DIR="."
else
    BUILD_DIR="/tmp/dso-install"
    echo -e "No local source found. Downloading from $REPO_URL..."
    rm -rf "$BUILD_DIR" && mkdir -p "$BUILD_DIR"
    git clone "$REPO_URL" "$BUILD_DIR"
fi

pushd "$BUILD_DIR" > /dev/null

# 3. Build Primary Binary (Official Docker Plugin)
echo -e "${GREEN}[3/5] Building docker-dso...${NC}"
CGO_ENABLED=0 go build -ldflags="-s -w" -o docker-dso ./cmd/docker-dso/

# 4. Installing binaries
echo -e "${GREEN}[4/5] Installing binaries...${NC}"

install_binary() {
    local src=$1
    local dest=$2
    local dir=$(dirname "$dest")
    
    if [ -f "$src" ]; then
        mkdir -p "$dir"
        cp "$src" "$dest.tmp"
        mv "$dest.tmp" "$dest"
        chmod +x "$dest"
        echo -e "  Installed $(basename "$dest") to $dir"
    else
        echo -e "${RED}ERROR: $src not found. Binary was not built.${NC}"
        exit 1
    fi
}

mkdir -p "$PLUGIN_DIR"
install_binary "docker-dso" "$PLUGIN_DIR/docker-dso"
install_binary "docker-dso" "$SYSTEM_BIN_DIR/docker-dso"

# Create symlink for standalone support
ln -sf "$SYSTEM_BIN_DIR/docker-dso" "$SYSTEM_BIN_DIR/dso"

# 5. Verification
echo -e "${GREEN}[5/5] Verifying installation...${NC}"

if docker dso version &> /dev/null; then
    VERSION=$(docker dso version | head -n 1)
    echo -e "${GREEN}✓ $VERSION installed successfully as Docker plugin!${NC}"
else
    echo -e "${RED}❌ Docker CLI plugin installation failed. Ensure $PLUGIN_DIR is in Docker's path.${NC}"
    exit 1
fi

# Success message
echo -e "${BLUE}================================================================${NC}"
echo -e "Usage (Official Plugin):"
echo -e "  - ${BLUE}docker dso init${NC}          (Initialize local vault)"
echo -e "  - ${BLUE}docker dso secret set${NC}    (Store secrets securely)"
echo -e "  - ${BLUE}docker dso up -d${NC}         (Deploy and inject secrets automatically)"
echo -e "${BLUE}================================================================${NC}"

# Cleanup
popd > /dev/null
if [ "$BUILD_DIR" != "." ]; then
    rm -rf "$BUILD_DIR"
fi
