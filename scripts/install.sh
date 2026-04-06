#!/bin/bash
# ==============================================================================
# Docker Secret Operator (DSO) - Production Installer (V3.1)
# ==============================================================================
# This script performs a full, production-ready installation of the DSO
# Docker CLI plugin.
#
# Supported OS: Linux, macOS, WSL
# ==============================================================================

set -e

# Configuration
REPO_URL="https://github.com/docker-secret-operator/dso"
BUILD_DIR="/tmp/dso-install"
DOCKER_CONFIG=${DOCKER_CONFIG:-$HOME/.docker}
if [ "$EUID" -eq 0 ]; then
    PLUGIN_DIR="/usr/local/lib/docker/cli-plugins"
else
    PLUGIN_DIR="$DOCKER_CONFIG/cli-plugins"
fi
SYSTEM_BIN_DIR="/usr/local/bin"

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

echo -e "${BLUE}==========================================${NC}"
echo -e "${BLUE}   Installing Docker Secret Operator (DSO) ${NC}"
echo -e "${BLUE}==========================================${NC}"

# 1. Dependency Check
echo -e "${GREEN}[1/6] Checking dependencies...${NC}"

# Check for Docker
if ! command -v docker &> /dev/null; then
    echo -e "${RED}Error: Docker not found. Please install Docker first.${NC}"
    exit 1
fi

# Check for Go (Minimum 1.22)
if ! command -v go &> /dev/null; then
    echo -e "${RED}Error: Go not found. DSO requires Go 1.22+ to build.${NC}"
    exit 1
fi

# 2. Download Project Files
echo -e "${GREEN}[2/6] Downloading DSO source...${NC}"
rm -rf "$BUILD_DIR" && mkdir -p "$BUILD_DIR"
cd "$BUILD_DIR"
git clone "$REPO_URL" .

# 3. Build Core Binary (Single-Binary Architecture)
echo -e "${GREEN}[3/6] Building docker-dso...${NC}"
CGO_ENABLED=0 go build -ldflags="-s -w" -o docker-dso ./cmd/docker-dso/

# 4. Install Docker CLI Plugin
echo -e "${GREEN}[4/6] Installing plugin...${NC}"
mkdir -p "$PLUGIN_DIR"

# Safe Overwrite
if [ -f "$PLUGIN_DIR/docker-dso" ]; then
    echo -e "${BLUE}Existing plugin found, updating...${NC}"
    rm "$PLUGIN_DIR/docker-dso"
fi

cp docker-dso "$PLUGIN_DIR/docker-dso"
chmod +x "$PLUGIN_DIR/docker-dso"

# 5. Optional System-wide install for Agent Service
if [ "$EUID" -eq 0 ]; then
    echo -e "${GREEN}[5/6] Configuring system-wide agent...${NC}"
    cp docker-dso "$SYSTEM_BIN_DIR/docker-dso"
    chmod +x "$SYSTEM_BIN_DIR/docker-dso"
    
    # Setup Config
    mkdir -p /etc/dso
    if [ ! -f /etc/dso/dso.yaml ]; then
        cp config/dso.example.yaml /etc/dso/dso.yaml 2>/dev/null || \
        echo "providers: {}" > /etc/dso/dso.yaml
    fi

    # Setup Systemd (Linux only)
    if [ -d /etc/systemd/system ]; then
        cat << EOF > /etc/systemd/system/dso.service
[Unit]
Description=Docker Secret Operator Agent
After=network.target docker.service
Requires=docker.service

[Service]
Type=simple
ExecStart=$SYSTEM_BIN_DIR/docker-dso agent
Restart=on-failure
RestartSec=5
RuntimeDirectory=dso

[Install]
WantedBy=multi-user.target
EOF
        systemctl daemon-reload
        systemctl enable dso || true
        echo -e "${GREEN}✓ systemd service 'dso' created.${NC}"
    fi
else
    echo -e "${BLUE}[5/6] Skipping system-wide install (not root).${NC}"
    echo -e "To run DSO as a system service, please run the installer with sudo."
fi

# 6. Verification
echo -e "${GREEN}[6/6] Verifying installation...${NC}"

if docker dso version &> /dev/null; then
    VERSION=$(docker dso version | head -n 1)
    echo -e "${GREEN}✓ $VERSION installed successfully!${NC}"
else
    echo -e "${RED}❌ Docker CLI plugin installation failed...${NC}"
    echo -e "${RED}Ensure $PLUGIN_DIR is in your Docker config path.${NC}"
    exit 1
fi

# Success message
echo -e "${BLUE}================================================================${NC}"
echo -e "${GREEN}   DSO is now a native Docker CLI plugin!                       ${NC}"
echo -e "${BLUE}================================================================${NC}"
echo -e "Usage:"
echo -e "  - ${BLUE}docker dso up${NC}         (Starts agent if needed and deploys)"
echo -e "  - ${BLUE}docker dso agent${NC}      (Rund engine in foreground)"
echo -e "  - ${BLUE}docker dso version${NC}    (Check status)"
echo -e ""
if [[ "$(uname -r)" == *"microsoft"* ]]; then
    echo -e "${BLUE}WSL detected: Your installation is complete.${NC}"
fi
echo -e "${BLUE}================================================================${NC}"

# Cleanup
rm -rf "$BUILD_DIR"
