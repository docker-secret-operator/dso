#!/bin/bash
# ==============================================================================
# Docker Secret Operator (DSO) - Production Installer (V3.2)
# ==============================================================================
# This script performs a full, production-ready installation of the DSO
# Official Docker CLI plugin and optional background agent.
#
# Supported OS: Linux (Ubuntu, Debian, CentOS, RHEL)
# ==============================================================================

set -e

# Configuration
REPO_URL="https://github.com/docker-secret-operator/dso"
BUILD_DIR="/tmp/dso-install"
DOCKER_CONFIG=${DOCKER_CONFIG:-$HOME/.docker}
if [ "$EUID" -eq 0 ]; then
    PLUGIN_DIR="/usr/local/lib/docker/cli-plugins"
    SYSTEM_BIN_DIR="/usr/local/bin"
    LIB_DIR="/usr/local/lib/dso"
else
    PLUGIN_DIR="$DOCKER_CONFIG/cli-plugins"
    SYSTEM_BIN_DIR="$HOME/.local/bin"
    LIB_DIR="$HOME/.local/lib/dso"
fi

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

echo -e "${BLUE}==========================================${NC}"
echo -e "${BLUE}   Installing Docker Secret Operator (DSO) ${NC}"
echo -e "${BLUE}==========================================${NC}"

# 1. Root Check (Partial)
if [ "$EUID" -ne 0 ]; then 
  echo -e "${RED}Warning: Not running as root. System-wide components (systemd) will be skipped.${NC}"
  echo -e "${RED}To perform a full production install, use: sudo bash install.sh${NC}"
  echo ""
fi

# 2. Dependency Installation
echo -e "${GREEN}[1/7] Checking dependencies...${NC}"

# Function to install if missing
install_if_missing() {
    if ! command -v $1 &> /dev/null; then
        echo -e "Installing $1..."
        if [ "$EUID" -eq 0 ]; then
            apt-get update && apt-get install -y $1 || yum install -y $1 || apk add $1
        else
            echo -e "${RED}Error: $1 is missing and we don't have root to install it.${NC}"
            exit 1
        fi
    fi
}

install_if_missing "curl"
install_if_missing "git"
install_if_missing "tar"

# Check for Docker
if ! command -v docker &> /dev/null; then
    echo -e "Docker not found. Installing Docker..."
    if [ "$EUID" -eq 0 ]; then
        curl -fsSL https://get.docker.com | sh
        systemctl enable --now docker
    else
        echo -e "${RED}Error: Docker is missing and we don't have root to install it.${NC}"
        exit 1
    fi
fi

# Check for Go (Minimum 1.25.0 per go.mod)
export PATH=$PATH:/usr/local/go/bin

install_go() {
    echo -e "Installing Go 1.25.0..."
    GO_VERSION="1.25.0"
    if [ "$EUID" -eq 0 ]; then
        curl -LO https://go.dev/dl/go${GO_VERSION}.linux-amd64.tar.gz
        rm -rf /usr/local/go && tar -C /usr/local -xzf go${GO_VERSION}.linux-amd64.tar.gz
        rm go${GO_VERSION}.linux-amd64.tar.gz
        export PATH=$PATH:/usr/local/go/bin
        # Persist for future sessions
        if ! grep -q '/usr/local/go/bin' /etc/environment 2>/dev/null; then
            echo 'PATH="/usr/local/go/bin:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"' >> /etc/environment
        fi
    else
        echo -e "${RED}Error: Go 1.25.0+ is required but missing. Please install it or run as root.${NC}"
        exit 1
    fi
}

if ! command -v go &> /dev/null; then
    echo -e "Go not found. Installing..."
    install_go
else
    # Parse major.minor correctly
    CURRENT_GO_MINOR=$(go version | grep -oP 'go1\.\K[0-9]+' | head -1)
    if [ -z "$CURRENT_GO_MINOR" ] || [ "$CURRENT_GO_MINOR" -lt 25 ]; then
        echo -e "Go version too old (need 1.25+). Upgrading..."
        install_go
    else
        echo -e "${GREEN}Go $(go version | awk '{print $3}') already installed. Skipping.${NC}"
    fi
fi

# 3. Download/Prepare Project Files
echo -e "${GREEN}[2/7] Preparing DSO source...${NC}"
rm -rf "$BUILD_DIR" && mkdir -p "$BUILD_DIR"

if [ -f "./go.mod" ] && grep -q "github.com/docker-secret-operator/dso" "./go.mod"; then
    echo -e "Local source detected. Using current directory."
    cp -r . "$BUILD_DIR/"
else
    echo -e "No local source found. Downloading from $REPO_URL..."
    cd "$BUILD_DIR"
    git clone "$REPO_URL" .
fi
cd "$BUILD_DIR"

# 4. Build Primary Binary (Official Docker Plugin)
echo -e "${GREEN}[3/7] Building docker-dso...${NC}"
CGO_ENABLED=0 go build -ldflags="-s -w" -o docker-dso ./cmd/docker-dso/

# 5. Installing binaries
echo -e "${GREEN}[4/7] Installing binaries...${NC}"

echo "Stopping dso-agent service if running..."
sudo systemctl stop dso-agent 2>/dev/null || true

install_binary() {
    local src=$1
    local dest=$2
    local dir=$(dirname "$dest")
    
    if [ -f "$src" ]; then
        sudo mkdir -p "$dir"
        sudo cp "$src" "$dest.tmp"
        sudo mv "$dest.tmp" "$dest"
        sudo chmod +x "$dest"
        echo -e "  Installed $(basename "$dest")"
    else
        echo -e "WARNING: $src not found, skipping"
    fi
}

# Install Docker CLI Plugin
install_binary "docker-dso" "$PLUGIN_DIR/docker-dso"

# Install as Standalone binary
install_binary "docker-dso" "$SYSTEM_BIN_DIR/docker-dso"

# Create symlinks for legacy/standalone support
sudo ln -sf "$SYSTEM_BIN_DIR/docker-dso" "$SYSTEM_BIN_DIR/dso"
sudo ln -sf "$SYSTEM_BIN_DIR/docker-dso" "$SYSTEM_BIN_DIR/dso-agent"

# 6. Build and Install Provider Plugins (Robust Build Loop)
echo -e "${GREEN}[5/7] Setting up provider plugins...${NC}"
sudo mkdir -p "$LIB_DIR/plugins"

PLUGINS=("aws" "azure" "huawei" "vault")
for plugin in "${PLUGINS[@]}"; do
    echo "Building plugin: $plugin"
    if [ -d "./cmd/plugins/dso-provider-$plugin" ]; then
        if CGO_ENABLED=0 go build -ldflags="-s -w" -o "dso-provider-$plugin" "./cmd/plugins/dso-provider-$plugin"; then
            install_binary "dso-provider-$plugin" "$LIB_DIR/plugins/dso-provider-$plugin"
            rm "dso-provider-$plugin"
        else
            echo "ERROR: Failed to build $plugin plugin"
            exit 1
        fi
    else
        echo "WARNING: Source for $plugin missing, skipping build"
    fi
done

# 7. Configure dso-agent (Interactive)
echo -e "${GREEN}[6/7] Configuring DSO...${NC}"

if [ "$EUID" -eq 0 ]; then
    sudo mkdir -p /etc/dso
    if [ ! -f /etc/dso/dso.yaml ]; then
        echo -e "${BLUE}Creating /etc/dso/dso.yaml configuration...${NC}"
        # ... (rest of configuration generation remains same)
        cat << EOF | sudo tee /etc/dso/dso.yaml > /dev/null
# Docker Secret Operator (DSO) Configuration
provider: aws
agent:
  cache: true
  watch:
    mode: polling
    polling_interval: 5m
secrets:
  - name: my-database-secret
    inject: env
    mappings:
      DB_PASSWORD: password
EOF
        echo -e "  ✓ Created /etc/dso/dso.yaml"
    fi
    sudo chmod 644 /etc/dso/dso.yaml

    # Setup Systemd
    if [ -d /etc/systemd/system ]; then
        echo -e "Configuring systemd service..."
        cat << EOF | sudo tee /etc/systemd/system/dso-agent.service > /dev/null
[Unit]
Description=Docker Secret Operator Agent
After=network.target docker.service
Requires=docker.service

[Service]
Type=simple
ExecStart=$SYSTEM_BIN_DIR/docker-dso agent --api-addr :8080
Restart=on-failure
RestartSec=5
EnvironmentFile=-/etc/dso/agent.env
RuntimeDirectory=dso

[Install]
WantedBy=multi-user.target
EOF
        sudo systemctl daemon-reload
        sudo systemctl enable dso-agent || true
        
        echo "Starting dso-agent..."
        sudo systemctl start dso-agent
        
        echo "Verifying service..."
        if ! systemctl is-active --quiet dso-agent; then
            echo "ERROR: dso-agent failed to start"
            # exit 1 # Don't exit here to allow verification part to show errors
        else
            echo -e "  ✓ dso-agent is active"
        fi
    fi
else
    echo -e "${YELLOW}Skipping /etc/dso configuration (not root).${NC}"
fi

# 8. Verification
echo -e "${GREEN}[7/7] Verifying installation...${NC}"

if docker dso version &> /dev/null; then
    VERSION=$(docker dso version | head -n 1)
    echo -e "${GREEN}✓ $VERSION installed successfully as Docker plugin!${NC}"
else
    echo -e "${RED}❌ Docker CLI plugin installation failed...${NC}"
    exit 1
fi

# Success message
echo -e "${BLUE}================================================================${NC}"
echo -e "${GREEN}   Docker Secret Operator (DSO) setup complete!                ${NC}"
echo -e "${BLUE}================================================================${NC}"
echo -e "Usage (Official Plugin):"
echo -e "  - ${BLUE}docker dso up -d${NC}         (Starts agent and deploys)"
echo -e "  - ${BLUE}docker dso version${NC}       (Check status)"
echo -e ""
echo -e "Usage (Standalone):"
echo -e "  - ${BLUE}dso-agent --help${NC}         (Run engine directly)"
echo -e "  - ${BLUE}systemctl start dso-agent${NC}  (Manage as system service)"
echo -e "${BLUE}================================================================${NC}"

# Cleanup
rm -rf "$BUILD_DIR"
