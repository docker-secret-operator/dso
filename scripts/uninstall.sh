#!/bin/bash
# ==============================================================================
# Docker Secret Operator (DSO) - Complete Uninstaller
# ==============================================================================
# Removes ALL DSO traces from the system:
#   - Binaries, plugins, symlinks
#   - Systemd service and timers
#   - Configuration and state directories
#   - Runtime sockets and directories
#   - Log directories
#   - DSO group and users
#   - All other DSO-related files
#
# After running this, the system will be as if DSO was never installed.
# ==============================================================================

set -euo pipefail

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[0;33m'
BLUE='\033[0;34m'
NC='\033[0m'

# ── Determine if running as root ────────────────────────────────────────────
IS_ROOT=false
if [ "$EUID" -eq 0 ]; then
    IS_ROOT=true
fi

DOCKER_CONFIG=${DOCKER_CONFIG:-$HOME/.docker}

# ── Resolve paths (local vs. system) ────────────────────────────────────────
if [ "$IS_ROOT" = true ]; then
    PLUGIN_DIR="/usr/local/lib/docker/cli-plugins"
    SYSTEM_BIN_DIR="/usr/local/bin"
    PROVIDER_PLUGIN_DIR="/usr/local/lib/dso/plugins"
    IS_SYSTEM_INSTALL=true
else
    PLUGIN_DIR="$DOCKER_CONFIG/cli-plugins"
    SYSTEM_BIN_DIR="$HOME/.local/bin"
    PROVIDER_PLUGIN_DIR="$HOME/.dso/plugins"
    IS_SYSTEM_INSTALL=false
fi

# ── Colors and helpers ──────────────────────────────────────────────────────
safe_remove() {
    local path="$1"
    if [ -e "$path" ] || [ -L "$path" ]; then
        if rm -f "$path" 2>/dev/null; then
            echo -e "  ${GREEN}✓ Removed:${NC} $path"
            return 0
        else
            echo -e "  ${YELLOW}⚠ Could not remove:${NC} $path"
            return 1
        fi
    fi
}

safe_remove_dir() {
    local path="$1"
    if [ -d "$path" ]; then
        if rm -rf "$path" 2>/dev/null; then
            echo -e "  ${GREEN}✓ Removed:${NC} $path/"
            return 0
        else
            echo -e "  ${YELLOW}⚠ Could not remove:${NC} $path/"
            return 1
        fi
    fi
}

safe_group_remove() {
    local group="$1"
    if getent group "$group" &>/dev/null; then
        if groupdel "$group" 2>/dev/null; then
            echo -e "  ${GREEN}✓ Removed group:${NC} $group"
            return 0
        else
            echo -e "  ${YELLOW}⚠ Could not remove group:${NC} $group"
            return 1
        fi
    fi
}

safe_user_remove() {
    local user="$1"
    if id "$user" &>/dev/null; then
        if userdel -r "$user" 2>/dev/null; then
            echo -e "  ${GREEN}✓ Removed user:${NC} $user"
            return 0
        else
            echo -e "  ${YELLOW}⚠ Could not remove user:${NC} $user"
            return 1
        fi
    fi
}

# ── Banner ──────────────────────────────────────────────────────────────────
echo -e "${BLUE}════════════════════════════════════════════════════${NC}"
echo -e "${RED}     Docker Secret Operator - Complete Uninstall    ${NC}"
echo -e "${BLUE}════════════════════════════════════════════════════${NC}"
echo ""
echo "This will remove ALL DSO traces from your system:"
echo "  - Binaries and plugins"
echo "  - Configuration and state"
echo "  - Systemd service"
echo "  - System users and groups"
echo "  - All related files and directories"
echo ""

if [ "$IS_ROOT" != true ]; then
    echo -e "${YELLOW}Running as non-root. Some operations may require sudo.${NC}"
    echo ""
fi

# Check if running interactively (not piped)
if [ -t 0 ]; then
    # Interactive mode - prompt for confirmation
    read -r -p "Are you sure? Type 'yes' to continue: " confirm
    if [ "$confirm" != "yes" ]; then
        echo "Uninstall cancelled."
        exit 0
    fi
else
    # Non-interactive mode (piped) - require explicit --force flag or env var
    FORCE_FLAG="${1:-}"
    if [ "${DSO_UNINSTALL_FORCE:-false}" != "true" ] && [ "$FORCE_FLAG" != "--force" ]; then
        echo "Error: Uninstall must be run interactively or with --force flag"
        echo "Usage:"
        echo "  Interactive: bash scripts/uninstall.sh"
        echo "  Non-interactive: DSO_UNINSTALL_FORCE=true bash scripts/uninstall.sh"
        echo "  Or: bash scripts/uninstall.sh --force"
        exit 1
    fi
fi

echo ""
echo -e "${BLUE}Proceeding with uninstall...${NC}"

# ── Step 1: Stop and disable services ──────────────────────────────────────
echo ""
echo -e "${GREEN}[1/7] Stopping services...${NC}"

if [ "$IS_SYSTEM_INSTALL" = true ]; then
    if [ "$EUID" -ne 0 ]; then
        echo -e "${RED}Error: System-wide uninstall requires root privileges${NC}"
        echo -e "Run: sudo bash scripts/uninstall.sh"
        exit 1
    fi

    # Stop dso-agent service
    if systemctl is-active --quiet dso-agent 2>/dev/null; then
        systemctl stop dso-agent 2>/dev/null && echo -e "  ${GREEN}✓ Stopped:${NC} dso-agent" || true
    fi

    # Disable service
    if systemctl is-enabled --quiet dso-agent 2>/dev/null; then
        systemctl disable dso-agent 2>/dev/null && echo -e "  ${GREEN}✓ Disabled:${NC} dso-agent" || true
    fi

    # Reload systemd
    systemctl daemon-reload 2>/dev/null || true
else
    echo -e "  (Skipped - user-level install)"
fi

# ── Step 2: Remove systemd files ───────────────────────────────────────────
echo ""
echo -e "${GREEN}[2/7] Removing systemd files...${NC}"

if [ "$IS_SYSTEM_INSTALL" = true ]; then
    safe_remove "/etc/systemd/system/dso-agent.service"
    safe_remove "/etc/systemd/system/dso-agent.timer"
else
    echo -e "  (Skipped - user-level install)"
fi

# ── Step 3: Remove binaries and plugins ────────────────────────────────────
echo ""
echo -e "${GREEN}[3/7] Removing binaries and plugins...${NC}"

# Docker CLI plugin
safe_remove "$PLUGIN_DIR/docker-dso"

# Standalone binaries
safe_remove "$SYSTEM_BIN_DIR/docker-dso"
safe_remove "$SYSTEM_BIN_DIR/dso"

# Legacy paths
if [ "$IS_SYSTEM_INSTALL" = true ]; then
    safe_remove "/usr/local/bin/dso-agent"
    safe_remove "/usr/local/bin/dso-provider-aws"
    safe_remove "/usr/local/bin/dso-provider-azure"
    safe_remove "/usr/local/bin/dso-provider-vault"
    safe_remove "/usr/local/bin/dso-provider-huawei"
fi

# ── Step 4: Remove provider plugins ────────────────────────────────────────
echo ""
echo -e "${GREEN}[4/7] Removing provider plugins...${NC}"

safe_remove_dir "$PROVIDER_PLUGIN_DIR"

# ── Step 5: Remove configuration and state (system installs) ──────────────
echo ""
echo -e "${GREEN}[5/7] Removing configuration and state...${NC}"

if [ "$IS_SYSTEM_INSTALL" = true ]; then
    safe_remove_dir "/etc/dso"
    safe_remove_dir "/var/lib/dso"
    safe_remove_dir "/var/log/dso"
else
    echo -e "  (Skipped - user-level install)"
fi

# ── Step 6: Remove runtime and socket directories ──────────────────────────
echo ""
echo -e "${GREEN}[6/7] Removing runtime directories...${NC}"

# Socket directories
safe_remove "/var/run/dso.sock"
safe_remove "/run/dso.sock"
safe_remove_dir "/var/run/dso"
safe_remove_dir "/run/dso"

# Docker plugin socket
safe_remove "/run/docker/plugins/dso.sock"
safe_remove_dir "/run/docker/plugins/dso"

# ── Step 7: Remove system user and group ───────────────────────────────────
echo ""
echo -e "${GREEN}[7/7] Removing system user and group...${NC}"

if [ "$IS_SYSTEM_INSTALL" = true ]; then
    # Remove dso user (if exists and not system-critical)
    if id dso &>/dev/null 2>&1; then
        echo -e "  Found DSO user. Attempting removal..."
        userdel -r dso 2>/dev/null && echo -e "  ${GREEN}✓ Removed user:${NC} dso" || echo -e "  ${YELLOW}⚠ Could not remove user 'dso'${NC}"
    fi

    # Remove dso group
    if getent group dso &>/dev/null; then
        groupdel dso 2>/dev/null && echo -e "  ${GREEN}✓ Removed group:${NC} dso" || echo -e "  ${YELLOW}⚠ Could not remove group 'dso'${NC}"
    fi
else
    echo -e "  (Skipped - user-level install)"
fi

# ── Optional: Remove local vault ────────────────────────────────────────────
echo ""
VAULT_DIR="$HOME/.dso"
if [ -d "$VAULT_DIR" ]; then
    echo -e "${YELLOW}Found local vault at: $VAULT_DIR${NC}"
    echo -e "  Size: $(du -sh "$VAULT_DIR" | cut -f1)"
    echo -e "  ${RED}WARNING:${NC} Contains encrypted vault.enc and master.key (unrecoverable if deleted)"
    echo ""
    read -r -p "Remove local vault? [y/N] " remove_vault
    if [[ "$remove_vault" =~ ^[Yy]$ ]]; then
        safe_remove_dir "$VAULT_DIR"
    else
        echo -e "  ${YELLOW}Vault kept at $VAULT_DIR - remove manually when ready${NC}"
    fi
else
    echo -e "  No local vault found"
fi

# ── Verification ──────────────────────────────────────────────────────────
echo ""
echo -e "${GREEN}[Verification]${NC} Checking for remaining DSO files..."

FOUND_FILES=()
for path in \
    "$PLUGIN_DIR/docker-dso" \
    "$SYSTEM_BIN_DIR/dso" \
    "/usr/local/lib/dso" \
    "/etc/dso" \
    "/var/lib/dso" \
    "/var/log/dso" \
    "/run/dso" \
    "/var/run/dso"; do
    if [ -e "$path" ] || [ -d "$path" ] || [ -L "$path" ]; then
        FOUND_FILES+=("$path")
    fi
done

if [ ${#FOUND_FILES[@]} -eq 0 ]; then
    echo -e "  ${GREEN}✓ No remaining DSO files found${NC}"
else
    echo -e "  ${YELLOW}⚠ Found remaining files:${NC}"
    for f in "${FOUND_FILES[@]}"; do
        echo -e "    - $f"
    done
fi

# ── Complete ────────────────────────────────────────────────────────────────
echo ""
echo -e "${BLUE}════════════════════════════════════════════════════${NC}"
echo -e "${GREEN}✓ DSO uninstall complete                           ${NC}"
echo -e "${BLUE}════════════════════════════════════════════════════${NC}"
echo ""
echo "The system is now clean of all DSO traces."
echo "It is as if DSO was never installed."
echo ""
