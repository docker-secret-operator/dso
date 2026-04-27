#!/bin/bash
# ==============================================================================
# Docker Secret Operator (DSO) - Uninstaller (v3.2)
# ==============================================================================
# Removes all DSO binaries, plugins, sockets, services, and optionally
# the local Native Vault (~/.dso).
#
# Mirrors the path logic of install.sh:
#   Root  → system paths (/usr/local/bin, /usr/local/lib/docker/cli-plugins)
#   User  → local paths  ($HOME/.local/bin, $DOCKER_CONFIG/cli-plugins)
# ==============================================================================

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[0;33m'
BLUE='\033[0;34m'
NC='\033[0m'

DOCKER_CONFIG=${DOCKER_CONFIG:-$HOME/.docker}

# Resolve install paths (mirroras install.sh)
if [ "$EUID" -eq 0 ]; then
    PLUGIN_DIR="/usr/local/lib/docker/cli-plugins"
    SYSTEM_BIN_DIR="/usr/local/bin"
    IS_SYSTEM_INSTALL=true
else
    PLUGIN_DIR="$DOCKER_CONFIG/cli-plugins"
    SYSTEM_BIN_DIR="$HOME/.local/bin"
    IS_SYSTEM_INSTALL=false
fi

echo -e "${BLUE}==========================================${NC}"
echo -e "${RED}    Uninstalling Docker Secret Operator    ${NC}"
echo -e "${BLUE}    Version: v3.2                         ${NC}"
echo -e "${BLUE}==========================================${NC}"

# ------------------------------------------------------------------------------
# Helpers
# ------------------------------------------------------------------------------

# Remove a file or symlink, printing status.
safe_remove() {
    local path="$1"
    if [ -e "$path" ] || [ -L "$path" ]; then
        if rm -f "$path"; then
            echo -e "  ${GREEN}Removed:${NC} $path"
        else
            echo -e "  ${YELLOW}Warning: could not remove $path${NC}"
        fi
    fi
}

# Remove a directory tree, printing status.
safe_remove_dir() {
    local path="$1"
    if [ -d "$path" ]; then
        if rm -rf "$path"; then
            echo -e "  ${GREEN}Removed:${NC} $path"
        else
            echo -e "  ${YELLOW}Warning: could not remove $path${NC}"
        fi
    fi
}

# ------------------------------------------------------------------------------
# Step 1 — Stop and disable systemd services (system installs only)
# ------------------------------------------------------------------------------
echo -e "\n${GREEN}[1/4] Stopping services...${NC}"

if [ "$IS_SYSTEM_INSTALL" = true ]; then
    for svc in dso dso-agent; do
        if systemctl is-active --quiet "$svc" 2>/dev/null; then
            systemctl stop "$svc" 2>/dev/null \
                && echo -e "  Stopped $svc" \
                || echo -e "  ${YELLOW}Warning: could not stop $svc${NC}"
        fi
        if systemctl is-enabled --quiet "$svc" 2>/dev/null; then
            systemctl disable "$svc" 2>/dev/null || true
        fi
        safe_remove "/etc/systemd/system/${svc}.service"
    done
    systemctl daemon-reload 2>/dev/null || true
else
    echo -e "  Skipped (user-level install — no systemd services managed)."
fi

# ------------------------------------------------------------------------------
# Step 2 — Remove sockets
# ------------------------------------------------------------------------------
echo -e "\n${GREEN}[2/4] Cleaning up sockets...${NC}"

# IPC socket used by the agent for CLI-to-agent RPC
safe_remove "/var/run/dso.sock"

# Docker V2 Secret Driver socket (created by 'dso agent --driver-socket')
safe_remove "/run/docker/plugins/dso.sock"

# ------------------------------------------------------------------------------
# Step 3 — Remove binaries, symlinks, and Docker CLI plugin
# ------------------------------------------------------------------------------
echo -e "\n${GREEN}[3/4] Removing binaries and plugin...${NC}"

# Primary paths (match current install.sh)
safe_remove "$PLUGIN_DIR/docker-dso"
safe_remove "$SYSTEM_BIN_DIR/docker-dso"
safe_remove "$SYSTEM_BIN_DIR/dso"          # symlink created by install.sh

# Legacy paths from pre-v3.2 installs (best-effort cleanup)
if [ "$IS_SYSTEM_INSTALL" = true ]; then
    safe_remove "/usr/local/bin/dso-agent"
    safe_remove_dir "/usr/local/lib/dso"   # old plugin binary directory
fi

# ------------------------------------------------------------------------------
# Step 4 — Optionally remove Native Vault data
# ------------------------------------------------------------------------------
echo -e "\n${GREEN}[4/4] Native Vault cleanup...${NC}"

VAULT_DIR="$HOME/.dso"

if [ -d "$VAULT_DIR" ]; then
    echo -e "  Found vault data at ${YELLOW}$VAULT_DIR${NC}"
    echo -e "  Contents:"
    ls -lh "$VAULT_DIR" 2>/dev/null | tail -n +2 | sed 's/^/    /'
    echo -e ""
    echo -e "  ${YELLOW}WARNING:${NC} This contains your encrypted vault (vault.enc) and"
    echo -e "  master key (master.key). Removal is permanent and unrecoverable."
    echo -e ""
    read -r -p "  Remove vault data? [y/N] " confirm
    if [[ "$confirm" =~ ^[Yy]$ ]]; then
        safe_remove_dir "$VAULT_DIR"
        echo -e "  ${GREEN}Vault data removed.${NC}"
    else
        echo -e "  ${YELLOW}Vault data kept at $VAULT_DIR — remove manually when ready.${NC}"
    fi
else
    echo -e "  No vault data found at $VAULT_DIR."
fi

# ------------------------------------------------------------------------------
# Done
# ------------------------------------------------------------------------------
echo -e "\n${BLUE}==========================================${NC}"
echo -e "${GREEN}   DSO v3.2 successfully uninstalled.     ${NC}"
echo -e "${BLUE}==========================================${NC}"
