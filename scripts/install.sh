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
    PROVIDER_PLUGIN_DIR="/usr/local/lib/dso/plugins"
else
    IS_ROOT=false
    INSTALL_DIR="${HOME}/.local/bin"
    PLUGIN_DIR="${HOME}/.docker/cli-plugins"
    PROVIDER_PLUGIN_DIR="${HOME}/.dso/plugins"
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
            echo -e "${YELLOW}⚠️  WARNING: systemctl was not found.${NC}"
            echo -e "${YELLOW}   The CLI plugin will still be installed, but Cloud Mode service setup requires systemd.${NC}"
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

# ── Check if installing over existing version ────────────────────────────────────
if [ -f "${PLUGIN_DIR}/${BINARY_NAME}" ]; then
    echo -e "${BLUE}Reinstalling / upgrading existing DSO installation...${NC}"
else
    echo -e "${BLUE}Installing DSO (${GOOS}/${GOARCH})...${NC}"
fi

# ── Select the release ─────────────────────────────────────────────────────────
# DSO_VERSION pins an exact release (e.g. 3.6.0 or v3.6.0). Without it the latest
# release tag is resolved from GitHub's redirect. That redirect is NOT trusted for
# authenticity: whatever tag it names is only installed if its artifacts verify
# against the release workflow's signature below. (A hostile redirect could still
# steer an install to an older, genuinely signed release; pin DSO_VERSION to prevent
# that.)
if [ -n "${DSO_VERSION:-}" ]; then
    TAG="v${DSO_VERSION#v}"
else
    echo -e "  Resolving latest version..."
    LATEST_URL=$(curl -fsSLI -o /dev/null -w '%{url_effective}' "https://github.com/${REPO}/releases/latest" 2>/dev/null || true)
    TAG="${LATEST_URL##*/}"
fi

# The tag is interpolated into URLs, file names and the expected signer identity:
# accept only a strict version shape (no slashes, dots-only segments, whitespace).
if ! printf '%s' "${TAG}" | grep -Eq '^v[0-9]+\.[0-9]+\.[0-9]+([-+.][0-9A-Za-z.]+)?$'; then
    echo -e "${RED}Error: '${TAG}' is not a valid release tag (could not resolve or pin a version).${NC}"
    exit 1
fi

VERSION="${TAG#v}"
# File naming convention: dso-VERSION-OS-ARCH.tar.gz
TARBALL_NAME="dso-${VERSION}-${GOOS}-${GOARCH}.tar.gz"
CHECKSUM_NAME="dso-${VERSION}-checksums.txt"
# DSO_RELEASE_BASE only changes WHERE files are fetched from (mirror, test fixture).
# Authenticity does not depend on the location: it comes from the signature check.
BASE="${DSO_RELEASE_BASE:-${RELEASE_BASE}}"
TARBALL_URL="${BASE}/${TAG}/${TARBALL_NAME}"
CHECKSUM_URL="${BASE}/${TAG}/${CHECKSUM_NAME}"
SIG_URL="${CHECKSUM_URL}.sig"          # keyless (production): detached signature + certificate
CERT_URL="${CHECKSUM_URL}.pem"
BUNDLE_URL="${CHECKSUM_URL}.sigstore.json"  # key mode only: cosign bundle signed with a private key

# ── Verification tool (never bootstrapped by this script) ──────────────────────
# cosign must already be installed from a source you trust (your OS package
# manager, or the official sigstore release with its published checksum verified).
# This installer will NOT download and run a verifier on its own: that would let an
# attacker who controls the download also control the check.
COSIGN="${DSO_COSIGN_BIN:-cosign}"
if ! command -v "${COSIGN}" >/dev/null 2>&1; then
    echo -e "${RED}Error: cosign is required to verify the release signature and was not found.${NC}"
    echo -e "${RED}       Install it from a trusted source (https://docs.sigstore.dev/cosign/system_config/installation/),${NC}"
    echo -e "${RED}       verify its checksum, then re-run. Nothing was installed.${NC}"
    exit 1
fi

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

# ── Download to temp directory (nothing is installed until everything verifies) ──
TMP_DIR="$(mktemp -d)"

# ── Transaction state (see "Install" below) ────────────────────────────────────
# Everything that will be published is staged first, then published with atomic
# renames. If anything fails, or the installer is interrupted (INT/TERM/HUP), the
# EXIT handler rolls every already-published item back, so the previous working
# installation (core binary + provider plugins + symlink) is restored exactly.
# (SIGKILL / power loss cannot be trapped: see docs/getting-started.md.)
COMMITTED=0
ITEM_STAGE=(); ITEM_DEST=(); ITEM_HAD=(); ITEM_OLDLINK=(); ITEM_BAK=(); ITEM_STATE=()
CREATED_DIRS=()

rollback_item() { # rollback_item INDEX
    local i="$1" stage dest
    stage="${ITEM_STAGE[$i]}"; dest="${ITEM_DEST[$i]}"
    if [ -e "${stage}" ] || [ -L "${stage}" ]; then
        # Never renamed into place: the destination is untouched. Drop the staging file.
        rm -f "${stage}" 2>/dev/null || true
        [ -z "${ITEM_BAK[$i]}" ] || rm -f "${ITEM_BAK[$i]}" 2>/dev/null || true
        return 0
    fi
    [ "${ITEM_STATE[$i]}" -ge 1 ] || return 0
    # Renamed into place: put the previous state back (or remove a new file).
    if [ "${ITEM_HAD[$i]}" = 1 ]; then
        if [ -n "${ITEM_OLDLINK[$i]}" ]; then
            local tl="${dest}.dso-rb.$$"
            rm -f "${tl}" 2>/dev/null || true
            if ln -s "${ITEM_OLDLINK[$i]}" "${tl}" 2>/dev/null; then mv -f "${tl}" "${dest}" 2>/dev/null || true; fi
        elif [ -n "${ITEM_BAK[$i]}" ] && [ -e "${ITEM_BAK[$i]}" ]; then
            mv -f "${ITEM_BAK[$i]}" "${dest}" 2>/dev/null || true
        fi
    else
        rm -f "${dest}" 2>/dev/null || true
    fi
}

rollback_all() {
    local n="${#ITEM_STAGE[@]}" i d any=0
    i=$((n - 1))
    while [ "${i}" -ge 0 ]; do
        if [ "${ITEM_STATE[$i]}" -ge 1 ]; then any=1; fi
        rollback_item "${i}"
        i=$((i - 1))
    done
    for d in ${CREATED_DIRS[@]+"${CREATED_DIRS[@]}"}; do rmdir "${d}" 2>/dev/null || true; done
    if [ "${any}" = 1 ]; then
        echo -e "${YELLOW}Installation did not complete: the previous installation was restored.${NC}" >&2
    fi
}

on_exit() {
    local rc=$?
    trap - EXIT INT TERM HUP
    # shellcheck disable=SC2046
    kill $(jobs -p) 2>/dev/null || true
    if [ "${COMMITTED}" != 1 ]; then
        rollback_all
    fi
    rm -rf "${TMP_DIR}"
    exit "${rc}"
}
trap on_exit EXIT
trap 'exit 130' INT
trap 'exit 143' TERM
trap 'exit 129' HUP

fetch() { # fetch URL DEST  -- fails (non-zero) on any HTTP/network error or empty file
    local url="$1" dest="$2"
    if ! curl -fsSL --retry 3 --max-time 120 --connect-timeout 10 --output "${dest}" "${url}" 2>/dev/null || [ ! -s "${dest}" ]; then
        echo -e "${RED}Error: failed to download ${url}${NC}"
        echo -e "${RED}       Check the release exists: https://github.com/${REPO}/releases/tag/${TAG}${NC}"
        return 1
    fi
}

TARBALL_PATH="${TMP_DIR}/${TARBALL_NAME}"
CHECKSUM_PATH="${TMP_DIR}/${CHECKSUM_NAME}"
SIG_PATH="${CHECKSUM_PATH}.sig"
CERT_PATH="${CHECKSUM_PATH}.pem"
BUNDLE_PATH="${CHECKSUM_PATH}.sigstore.json"
fetch "${TARBALL_URL}" "${TARBALL_PATH}" || exit 1
fetch "${CHECKSUM_URL}" "${CHECKSUM_PATH}" || exit 1
if [ -n "${DSO_COSIGN_PUBKEY:-}" ]; then
    fetch "${BUNDLE_URL}" "${BUNDLE_PATH}" || { echo -e "${RED}       No signature bundle for the checksum file: refusing to install an unsigned release.${NC}"; exit 1; }
else
    fetch "${SIG_URL}" "${SIG_PATH}" || { echo -e "${RED}       The release has no signature for its checksum file: refusing to install an unsigned release.${NC}"; exit 1; }
    fetch "${CERT_URL}" "${CERT_PATH}" || { echo -e "${RED}       The release has no signing certificate: refusing to install.${NC}"; exit 1; }
fi

# ── Authenticate the checksum manifest BEFORE trusting any hash in it ───────────
# A checksum file fetched from the same place as the tarball proves nothing by
# itself. The manifest is signed (cosign keyless, by this repository's release
# workflow); we accept it only for that exact workflow identity and the GitHub
# Actions OIDC issuer, for the requested tag.
echo -e "  Verifying release signature..."
if [ -n "${DSO_COSIGN_PUBKEY:-}" ]; then
    # Explicit alternative trust root for private mirrors / air-gapped / self-signed
    # builds: the manifest must verify against THIS public key (a cosign bundle).
    # Key-based trust has no public transparency log, so the log check does not
    # apply in this mode only; the key signature itself is always enforced and there
    # is no option to skip verification.
    VERIFY_ARGS=(verify-blob --key "${DSO_COSIGN_PUBKEY}" --bundle "${BUNDLE_PATH}" --insecure-ignore-tlog=true "${CHECKSUM_PATH}")
else
    SIGNER_IDENTITY="https://github.com/${REPO}/.github/workflows/release.yml@refs/tags/${TAG}"
    SIGNER_ISSUER="https://token.actions.githubusercontent.com"
    VERIFY_ARGS=(verify-blob
        --certificate "${CERT_PATH}"
        --signature "${SIG_PATH}"
        --certificate-identity "${SIGNER_IDENTITY}"
        --certificate-oidc-issuer "${SIGNER_ISSUER}")
    # The release workflow signs with detached files (--output-signature /
    # --output-certificate). cosign 3.x defaults to bundles, so it must be told this
    # is the legacy detached format; older cosign has no such flag.
    if "${COSIGN}" verify-blob --help 2>&1 | grep -q -- '--new-bundle-format'; then
        VERIFY_ARGS+=(--new-bundle-format=false)
    fi
    VERIFY_ARGS+=("${CHECKSUM_PATH}")
fi
if ! "${COSIGN}" "${VERIFY_ARGS[@]}" >/dev/null 2>"${TMP_DIR}/cosign.err"; then
    echo -e "${RED}Error: release signature verification FAILED. Nothing was installed.${NC}"
    sed 's/^/       /' "${TMP_DIR}/cosign.err" | head -5
    exit 1
fi

# ── Validate the artifact against the AUTHENTICATED manifest ────────────────────
echo -e "  Validating integrity (SHA256)..."
# Exact file-name match (a substring match could select a different line, e.g.
# "<name>.sbom.json"); exactly one entry must exist.
MATCHES="$(awk -v n="${TARBALL_NAME}" '($2 == n || $2 == "*" n) {print $1}' "${CHECKSUM_PATH}")"
if [ -z "${MATCHES}" ] || [ "$(printf '%s\n' "${MATCHES}" | wc -l | tr -d ' ')" != "1" ]; then
    echo -e "${RED}Error: the signed manifest does not contain exactly one entry for ${TARBALL_NAME}.${NC}"
    exit 1
fi
EXPECTED_HASH="${MATCHES}"

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
# Even a verified archive is listed first: refuse absolute paths and ".." entries.
if tar -tzf "${TARBALL_PATH}" | grep -Eq '^/|(^|/)\.\.(/|$)'; then
    echo -e "${RED}Error: the archive contains unsafe paths. Nothing was installed.${NC}"
    exit 1
fi
tar -xzf "${TARBALL_PATH}" -C "${TMP_DIR}" --no-same-owner

# ── Validate extracted binary exists ───────────────────────────────────────────
# Try multiple possible binary names from the tarball
EXTRACTED_BINARY=""
for possible_name in "${BINARY_NAME}" "dso" "docker-dso"; do
    found_path="$(find "${TMP_DIR}" -maxdepth 2 -type f -name "${possible_name}" | head -1)"
    if [ -n "${found_path}" ]; then
        EXTRACTED_BINARY="${found_path}"
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

# ── Install (transactional) ────────────────────────────────────────────────────
# Test-only failure injection (DSO_INSTALL_TEST_FAILPOINT): it can ONLY make the
# installer fail or pause AFTER verification has passed; it cannot skip, weaken or
# replace any check. Used by test/installer to prove rollback at every stage.
failpoint() { # failpoint NAME  -> non-zero when that failure is injected
    case "${DSO_INSTALL_TEST_FAILPOINT:-}" in
        "$1") echo "injected failure at $1" >&2; return 1 ;;
        "pause:$1") echo "DSO_INSTALL_PAUSED $1"; sleep 60 & wait $! || true ;;
    esac
    return 0
}

ensure_dir() { # create DIR (and parents), remembering what we created for rollback
    local d="$1" chain=()
    while [ ! -e "${d}" ] && [ ! -L "${d}" ] && [ "${d}" != "/" ] && [ "${d}" != "." ]; do
        chain+=("${d}"); d="$(dirname "${d}")"
    done
    mkdir -p "$1" || return 1
    CREATED_DIRS+=(${chain[@]+"${chain[@]}"})
}

# Probe once: GNU mv -T never moves INTO an existing directory destination.
MV_RENAME=(mv -f)
if mv --help 2>&1 | grep -q -- ' -T'; then MV_RENAME=(mv -fT); fi

register_item() { # register_item STAGE DEST  (records the pre-existing state of DEST)
    local stage="$1" dest="$2"
    ITEM_STAGE+=("${stage}"); ITEM_DEST+=("${dest}"); ITEM_BAK+=(""); ITEM_STATE+=(0)
    if [ -L "${dest}" ]; then
        ITEM_HAD+=(1); ITEM_OLDLINK+=("$(readlink "${dest}")")
    elif [ -e "${dest}" ]; then
        ITEM_HAD+=(1); ITEM_OLDLINK+=("")
    else
        ITEM_HAD+=(0); ITEM_OLDLINK+=("")
    fi
}

stage_file() { # stage_file SRC DEST : copy SRC next to DEST under a temporary name
    local src="$1" dest="$2" tmp
    if [ -d "${dest}" ]; then echo -e "${RED}Error: ${dest} is a directory.${NC}"; return 1; fi
    tmp="$(mktemp "${dest}.dso-stage.XXXXXX")" || return 1
    register_item "${tmp}" "${dest}"
    cp "${src}" "${tmp}" && chmod 0755 "${tmp}"
}

stage_link() { # stage_link TARGET DEST : create a temporary symlink next to DEST
    local target="$1" dest="$2" tmp="${2}.dso-stage.$$"
    if [ -d "${dest}" ] && [ ! -L "${dest}" ]; then echo -e "${RED}Error: ${dest} is a directory.${NC}"; return 1; fi
    rm -f "${tmp}"
    register_item "${tmp}" "${dest}"
    ln -s "${target}" "${tmp}"
}

publish_item() { # publish_item INDEX : back up the old entry, then rename the new one over it
    local i="$1" dest stage bak
    dest="${ITEM_DEST[$i]}"; stage="${ITEM_STAGE[$i]}"
    ITEM_STATE[$i]=1
    if [ "${ITEM_HAD[$i]}" = 1 ] && [ -z "${ITEM_OLDLINK[$i]}" ]; then
        # A hard link keeps the old bytes (and mode) intact after the rename replaces
        # the name; fall back to a copy where hard links are unavailable.
        bak="${dest}.dso-bak.$$"
        rm -f "${bak}"
        ln "${dest}" "${bak}" 2>/dev/null || cp -p "${dest}" "${bak}" || return 1
        ITEM_BAK[$i]="${bak}"
    fi
    "${MV_RENAME[@]}" "${stage}" "${dest}" || return 1
    ITEM_STATE[$i]=2
}

fail_install() { echo -e "${RED}Error: $1 The existing installation was left unchanged.${NC}"; exit 1; }

ensure_dir "${PLUGIN_DIR}" && ensure_dir "${INSTALL_DIR}" || fail_install "could not create the install directories."

# Stage 1 - build everything under temporary names next to its destination (same
# filesystem, so the later rename is atomic). Nothing live changes in this stage.
PROVIDER_STAGE_FIRST=${#ITEM_STAGE[@]}
PROVIDER_COUNT=0
while IFS= read -r provider_binary; do
    [ -n "${provider_binary}" ] || continue
    ensure_dir "${PROVIDER_PLUGIN_DIR}" || fail_install "could not create ${PROVIDER_PLUGIN_DIR}."
    failpoint stage_provider || fail_install "could not stage $(basename "${provider_binary}")."
    stage_file "${provider_binary}" "${PROVIDER_PLUGIN_DIR}/$(basename "${provider_binary}")" || fail_install "could not stage $(basename "${provider_binary}")."
    PROVIDER_COUNT=$((PROVIDER_COUNT + 1))
done < <(find "${TMP_DIR}" -maxdepth 2 -type f -name 'dso-provider-*' | sort)
CORE_INDEX=${#ITEM_STAGE[@]}
failpoint stage_core || fail_install "could not stage the binary."
stage_file "${EXTRACTED_BINARY}" "${PLUGIN_DIR}/${BINARY_NAME}" || fail_install "could not stage the binary."
LINK_INDEX=${#ITEM_STAGE[@]}
failpoint stage_link || fail_install "could not stage the dso symlink."
stage_link "${PLUGIN_DIR}/${BINARY_NAME}" "${INSTALL_DIR}/dso" || fail_install "could not stage the dso symlink."

# Stage 2 - publish with atomic renames: providers, then the core binary, then the
# symlink. Any failure (or signal) rolls back everything already published.
publish_or_die() { # publish_or_die INDEX
    publish_item "$1" || { echo -e "${RED}Error: could not install ${ITEM_DEST[$1]}.${NC}"; exit 1; }
}
i=${PROVIDER_STAGE_FIRST}
while [ "${i}" -lt "${CORE_INDEX}" ]; do
    failpoint publish_provider || { echo -e "${RED}Error: could not install a provider plugin.${NC}"; exit 1; }
    publish_or_die "${i}"
    i=$((i + 1))
done
failpoint "after_providers"
failpoint publish_core || { echo -e "${RED}Error: could not install the binary.${NC}"; exit 1; }
publish_or_die "${CORE_INDEX}"
failpoint "after_core"
failpoint publish_link || { echo -e "${RED}Error: could not update the dso symlink.${NC}"; exit 1; }
publish_or_die "${LINK_INDEX}"

# Stage 3 - verify the published result (as root the binary is run once); a failure
# here rolls the whole installation back as well.
failpoint postcheck || { echo -e "${RED}Error: post-install verification failed.${NC}"; exit 1; }
if [ "${IS_ROOT}" = true ]; then
    echo -e "${BLUE}Verifying installed binary...${NC}"
    if ! "${PLUGIN_DIR}/${BINARY_NAME}" version >/dev/null 2>&1; then
        echo -e "${RED}Error: Installed binary did not run successfully.${NC}"
        echo -e "${YELLOW}Fix: Re-run the installer or inspect ${PLUGIN_DIR}/${BINARY_NAME}.${NC}"
        exit 1
    fi
fi

# Commit: the new installation is complete; discard the backups of the old one.
COMMITTED=1
for bak in ${ITEM_BAK[@]+"${ITEM_BAK[@]}"}; do
    [ -z "${bak}" ] || rm -f "${bak}" 2>/dev/null || true
done

# ── Print result and context-aware next steps ──────────────────────────────────
echo ""
echo -e "${GREEN}✅ DSO installed successfully!${NC}"
echo -e "   Plugin:     ${PLUGIN_DIR}/${BINARY_NAME}"
echo -e "   Standalone: ${INSTALL_DIR}/dso (symlink)"
if [ "${PROVIDER_COUNT}" -gt 0 ]; then
    echo -e "   Providers:  ${PROVIDER_COUNT} plugin(s) installed to ${PROVIDER_PLUGIN_DIR}"
else
    echo -e "${YELLOW}   Providers:  no provider plugins found in release archive${NC}"
fi
echo ""

if [ "${IS_ROOT}" = true ]; then
    echo -e "${BLUE}Installed globally.${NC} Context: root"
    echo ""
    echo "Next steps (choose one):"
    echo ""
    echo "  ⭐ RECOMMENDED: Interactive Setup Wizard (2-3 minutes)"
    echo "     docker dso setup"
    echo ""
    echo "  OR use manual bootstrap (advanced/lower-level):"
    echo "     1. LOCAL Mode (development):  docker dso bootstrap local"
    echo "     2. CLOUD Mode (production):   sudo docker dso bootstrap agent"
    echo ""
    echo "  Then verify installation:"
    echo "     docker dso doctor"
    echo ""
    echo "  See the full setup guide:"
    echo "     https://github.com/docker-secret-operator/dso/blob/main/docs/getting-started.md"
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
    echo "  ⭐ RECOMMENDED: Interactive Setup Wizard (2-3 minutes)"
    echo "     docker dso setup"
    echo ""
    echo "  OR manual setup (advanced LOCAL Mode):"
    echo "     docker dso bootstrap local"
    echo ""
    echo "  Then verify installation:"
    echo "     docker dso doctor"
    echo ""
    echo "  See the full setup guide:"
    echo "     https://github.com/docker-secret-operator/dso/blob/main/docs/getting-started.md"
    echo ""
fi
