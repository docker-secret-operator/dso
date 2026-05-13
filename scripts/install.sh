#!/usr/bin/env bash
# ==============================================================================
# Docker Secret Operator (DSO) — Thin Installer
# ==============================================================================
# Responsibilities:
#   1. Detect OS + architecture
#   2. Download the correct prebuilt binary from GitHub Releases
#   3. Place it in the appropriate path (root vs. user)
#   4. chmod +x
#   5. Print version, location, and context-aware next steps
#
# Does NOT:
#   - Compile any Go code
#   - Set up systemd services
#   - Initialize the vault
#   - Install plugins
# ==============================================================================

set -euo pipefail

# ── Configuration ──────────────────────────────────────────────────────────────
REPO="docker-secret-operator/dso"
RELEASE_BASE="https://github.com/${REPO}/releases/download"
BINARY_NAME="docker-dso"

# Colour codes
RED='\033[0;31m'
GREEN='\033[0;32m'
BLUE='\033[0;34m'
YELLOW='\033[1;33m'
NC='\033[0m'

# ── Resolve target paths (root vs. user) ───────────────────────────────────────
if [ "$(id -u)" -eq 0 ]; then
    IS_ROOT=true
    INSTALL_DIR="/usr/local/bin"
    PLUGIN_DIR="/usr/local/lib/docker/cli-plugins"
else
    IS_ROOT=false
    INSTALL_DIR="${HOME}/.local/bin"
    PLUGIN_DIR="${HOME}/.docker/cli-plugins"
fi

# ── Docker Daemon Check ────────────────────────────────────────────────────────
if ! docker info >/dev/null 2>&1; then
    echo -e "${YELLOW}⚠️  WARNING: Docker daemon is not running.${NC}"
    echo -e "${YELLOW}   DSO requires Docker at runtime.${NC}"
    echo -e "${YELLOW}   You can start Docker later.${NC}"
fi

# ── Detect OS ──────────────────────────────────────────────────────────────────
OS="$(uname -s | tr '[:upper:]' '[:lower:]')"
case "${OS}" in
    linux)  
        GOOS="linux"
        if [ "${IS_ROOT}" = true ] && ! command -v systemctl &>/dev/null; then
            echo -e "${RED}Error: Systemd is required for Cloud Mode installation${NC}"
            exit 1
        fi
        if command -v systemctl &>/dev/null; then
            SYS_STATE="$(systemctl is-system-running 2>/dev/null || true)"
            if [ "${SYS_STATE}" = "degraded" ]; then
                echo -e "${YELLOW}⚠️  WARNING: Systemd state is degraded. DSO may not start properly.${NC}"
            fi
        fi
        if grep -qE "(Microsoft|WSL)" /proc/version 2>/dev/null; then
            echo -e "${YELLOW}⚠️  WARNING: WSL detected. Systemd may not be fully supported without WSL2 systemd enablement.${NC}"
        fi
        ;;
    darwin) GOOS="darwin" ;;
    *)
        echo -e "${RED}Error: Unsupported OS '${OS}'. DSO supports linux and darwin.${NC}"
        exit 1
        ;;
esac

# ── Detect architecture ────────────────────────────────────────────────────────
ARCH="$(uname -m)"
case "${ARCH}" in
    x86_64)           GOARCH="amd64" ;;
    aarch64|arm64)    GOARCH="arm64" ;;
    *)
        echo -e "${RED}Error: Unsupported architecture '${ARCH}'. DSO supports amd64 and arm64.${NC}"
        exit 1
        ;;
esac

# ── Resolve version ────────────────────────────────────────────────────────────
# Use the VERSION env var if provided, otherwise fetch the latest release tag.
if [ -z "${DSO_VERSION:-}" ]; then
    echo -e "${BLUE}Fetching latest DSO version...${NC}"
    DSO_VERSION="$(curl -fsSL "https://api.github.com/repos/${REPO}/releases/latest" \
        | grep '"tag_name"' \
        | head -1 \
        | sed 's/.*"tag_name": *"\(.*\)".*/\1/')"
fi

if [ -z "${DSO_VERSION}" ]; then
    echo -e "${RED}Error: Failed to fetch the latest DSO version.${NC}"
    echo -e "${RED}       This may be caused by a GitHub API rate limit.${NC}"
    echo -e ""
    echo -e "${YELLOW}Fix: Set the version manually and re-run:${NC}"
    echo -e "       export DSO_VERSION=v3.3.0"
    echo -e "       curl -fsSL https://raw.githubusercontent.com/${REPO}/main/scripts/install.sh | bash"
    exit 1
fi

if [ -f "${PLUGIN_DIR}/${BINARY_NAME}" ]; then
    echo -e "${BLUE}Reinstalling / upgrading existing DSO installation...${NC}"
else
    echo -e "${BLUE}Installing DSO ${DSO_VERSION} (${GOOS}/${GOARCH})...${NC}"
fi

# ── Construct download URL ─────────────────────────────────────────────────────
# File naming convention: dso-VERSION-OS-ARCH.tar.gz (e.g., dso-3.3.0-linux-amd64.tar.gz)
TARBALL_NAME="dso-${DSO_VERSION#v}-${GOOS}-${GOARCH}.tar.gz"
TARBALL_URL="${RELEASE_BASE}/${DSO_VERSION}/${TARBALL_NAME}"
CHECKSUM_URL="${RELEASE_BASE}/${DSO_VERSION}/dso-${DSO_VERSION#v}-checksums.txt"

# ── PATH shadowing guard ───────────────────────────────────────────────────────
# If a global binary exists but we are running without root, warn loudly.
if [ "${IS_ROOT}" = false ]; then
    GLOBAL_BINARY="/usr/local/lib/docker/cli-plugins/${BINARY_NAME}"
    if [ -f "${GLOBAL_BINARY}" ]; then
        echo -e "${YELLOW}⚠️  WARNING: A global DSO installation was detected at ${GLOBAL_BINARY}.${NC}"
        echo -e "${YELLOW}   Installing locally (~/.docker) will create a PATH conflict.${NC}"
        echo -e "${YELLOW}   This may cause unexpected behavior if multiple versions exist.${NC}"
        echo -e "${YELLOW}   To upgrade globally instead, re-run with: sudo bash install.sh${NC}"
        echo -e "${YELLOW}   Proceeding with local install...${NC}"
        echo ""
    fi
fi

# ── Download to temp directory ─────────────────────────────────────────────────
TMP_DIR="$(mktemp -d)"
trap 'rm -rf "${TMP_DIR}"' EXIT

TARBALL_PATH="${TMP_DIR}/${TARBALL_NAME}"
curl -fsSL \
    --retry 2 \
    --max-time 90 \
    --connect-timeout 10 \
    --output "${TARBALL_PATH}" \
    "${TARBALL_URL}" || {
    echo -e "${RED}Error: Failed to download binary from ${TARBALL_URL}${NC}"
    exit 1
}

echo -e "  Downloading checksum..."
CHECKSUM_PATH="${TARBALL_PATH}.sha256"
curl -fsSL \
    --retry 2 \
    --max-time 30 \
    --connect-timeout 10 \
    --output "${CHECKSUM_PATH}" \
    "${CHECKSUM_URL}" || {
    echo -e "${RED}Error: Failed to download checksum from ${CHECKSUM_URL}${NC}"
    exit 1
}

# ── Validate checksum ──────────────────────────────────────────────────────────
echo -e "  Validating integrity (SHA256)..."
EXPECTED_HASH="$(grep "${TARBALL_NAME}" "${CHECKSUM_PATH}" | awk '{print $1}')"

if [ -z "${EXPECTED_HASH}" ]; then
    echo -e "${RED}Error: Checksum for ${TARBALL_NAME} not found in ${CHECKSUM_PATH}${NC}"
    echo -e "${RED}       Contents of checksum file:${NC}"
    cat "${CHECKSUM_PATH}"
    exit 1
fi

if command -v sha256sum &>/dev/null; then
    ACTUAL_HASH="$(sha256sum "${TARBALL_PATH}" | awk '{print $1}')"
elif command -v shasum &>/dev/null; then
    ACTUAL_HASH="$(shasum -a 256 "${TARBALL_PATH}" | awk '{print $1}')"
else
    echo -e "${RED}Error: No sha256sum or shasum found. Cannot verify integrity.${NC}"
    exit 1
fi

if [ "${ACTUAL_HASH}" != "${EXPECTED_HASH}" ]; then
    echo -e "${RED}Error: Integrity check FAILED.${NC}"
    echo -e "  Expected: ${EXPECTED_HASH}"
    echo -e "  Actual:   ${ACTUAL_HASH}"
    exit 1
fi

# ── Extract binary ─────────────────────────────────────────────────────────────
echo -e "  Extracting binary..."
tar -xzf "${TARBALL_PATH}" -C "${TMP_DIR}"

# ── Validate extracted binary exists ───────────────────────────────────────────
# Try multiple possible binary names from the tarball
EXTRACTED_BINARY=""
for possible_name in "${BINARY_NAME}" "dso" "docker-dso"; do
    if [ -f "${TMP_DIR}/${possible_name}" ]; then
        EXTRACTED_BINARY="${TMP_DIR}/${possible_name}"
        break
    fi
done

if [ -z "${EXTRACTED_BINARY}" ]; then
    echo -e "${RED}Error: Binary not found in tarball after extraction.${NC}"
    echo -e "${RED}       Looked for: docker-dso, dso${NC}"
    echo -e "${RED}       Contents of extraction dir:${NC}"
    ls -la "${TMP_DIR}/"
    exit 1
fi

# ── Install binary ─────────────────────────────────────────────────────────────
mkdir -p "${PLUGIN_DIR}" "${INSTALL_DIR}"

cp "${EXTRACTED_BINARY}" "${PLUGIN_DIR}/${BINARY_NAME}"
chmod +x "${PLUGIN_DIR}/${BINARY_NAME}"

# Symlink for standalone dso usage
ln -sf "${PLUGIN_DIR}/${BINARY_NAME}" "${INSTALL_DIR}/dso"

# ── Print result and context-aware next steps ──────────────────────────────────
echo ""
echo -e "${GREEN}✅ DSO ${DSO_VERSION} installed successfully!${NC}"
echo -e "   Plugin:     ${PLUGIN_DIR}/${BINARY_NAME}"
echo -e "   Standalone: ${INSTALL_DIR}/dso (symlink)"
echo ""

if [ "${IS_ROOT}" = true ]; then
    echo -e "${BLUE}Installed globally.${NC} Context: root"
    echo ""
    echo "Next steps:"
    echo ""
    echo "  For LOCAL mode (each user runs):"
    echo "    docker dso init"
    echo ""
    echo "  For CLOUD mode (enterprise / systemd agent):"
    echo "    sudo docker dso system setup"
    echo ""
else
    echo -e "${BLUE}Installed for current user.${NC} Context: non-root"
    echo ""

    # Check if ~/.local/bin is on PATH
    if [[ ":${PATH}:" != *":${INSTALL_DIR}:"* ]]; then
        echo -e "${YELLOW}⚠️  '${INSTALL_DIR}' is not on your PATH.${NC}"
        echo "   Add it with:"
        echo "     export PATH=\"\${HOME}/.local/bin:\${PATH}\""
        echo ""
    fi

    echo "Next steps:"
    echo ""
    echo "  Initialize your local vault:"
    echo "    docker dso init"
    echo ""
    echo "  Store a secret:"
    echo "    docker dso secret set app/db_pass"
    echo ""
    echo "  Deploy:"
    echo "    docker dso up -d"
    echo ""
    echo -e "${YELLOW}⚠️  Note: Mode is NOT yet configured. 'docker dso init' is required before first use.${NC}"
fi

# ── Post-install Verification ──────────────────────────────────────────────────
if [ "${IS_ROOT}" = true ]; then
    echo -e "${BLUE}Running post-install system diagnostics...${NC}"
    if ! "${PLUGIN_DIR}/${BINARY_NAME}" system doctor; then
        echo -e "${RED}Error: Post-install diagnostics failed. Please review the output above.${NC}"
        echo -e "${YELLOW}Fix: Run 'sudo docker dso system setup' to properly initialize Cloud Mode.${NC}"
        exit 1
    fi
fi

