#!/usr/bin/env bash
# ==============================================================================
# DSO Release Builder
# ==============================================================================
# Builds DSO binary and all provider plugins for release distribution
# Usage: ./scripts/build-release.sh v3.5.4
# ==============================================================================

set -euo pipefail

VERSION="${1:-}"
if [ -z "$VERSION" ]; then
    echo "Usage: $0 <version>"
    echo "Example: $0 v3.5.4"
    exit 1
fi

# Remove 'v' prefix if present
VERSION_NUM="${VERSION#v}"

# ── Setup ──────────────────────────────────────────────────────────────────
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(dirname "$SCRIPT_DIR")"
BUILD_DIR="${PROJECT_ROOT}/build"
RELEASE_DIR="${BUILD_DIR}/dso-${VERSION}"

echo "Building DSO release ${VERSION}..."
echo "Project root: ${PROJECT_ROOT}"
echo "Release directory: ${RELEASE_DIR}"

# ── Clean previous builds ──────────────────────────────────────────────────
rm -rf "$BUILD_DIR"
mkdir -p "$RELEASE_DIR"

# ── Detect OS and architecture ─────────────────────────────────────────────
OS="$(uname -s | tr '[:upper:]' '[:lower:]')"
ARCH="$(uname -m)"

case "${ARCH}" in
    x86_64)           GOARCH="amd64" ;;
    aarch64|arm64)    GOARCH="arm64" ;;
    *)
        echo "Error: Unsupported architecture '${ARCH}'"
        exit 1
        ;;
esac

case "${OS}" in
    linux)  GOOS="linux" ;;
    darwin) GOOS="darwin" ;;
    *)
        echo "Error: Unsupported OS '${OS}'"
        exit 1
        ;;
esac

echo "Target: ${GOOS}/${GOARCH}"

# ── Build main binary ──────────────────────────────────────────────────────
echo ""
echo "Building DSO binary..."
cd "$PROJECT_ROOT"
GOOS="$GOOS" GOARCH="$GOARCH" go build -o "$RELEASE_DIR/dso" ./cmd/dso
chmod +x "$RELEASE_DIR/dso"
echo "✓ DSO binary built"

# ── Build provider plugins ────────────────────────────────────────────────
echo ""
echo "Building provider plugins..."

PROVIDERS=("aws" "azure" "vault" "huawei")
for provider in "${PROVIDERS[@]}"; do
    echo "  Building dso-provider-${provider}..."
    GOOS="$GOOS" GOARCH="$GOARCH" go build \
        -o "$RELEASE_DIR/dso-provider-${provider}" \
        "./cmd/plugins/dso-provider-${provider}"
    chmod +x "$RELEASE_DIR/dso-provider-${provider}"
done
echo "✓ All provider plugins built"

# ── Create tarball ────────────────────────────────────────────────────────
echo ""
echo "Creating release tarball..."
cd "$BUILD_DIR"
TAR_NAME="dso-${VERSION}-${GOOS}-${GOARCH}.tar.gz"
tar czf "$TAR_NAME" "dso-${VERSION}/"
echo "✓ Tarball created: $TAR_NAME"

# ── Checksums ─────────────────────────────────────────────────────────────
echo ""
echo "Generating checksums..."
cd "$BUILD_DIR"
sha256sum "$TAR_NAME" > "${TAR_NAME}.sha256"
cat "${TAR_NAME}.sha256"

# ── Summary ───────────────────────────────────────────────────────────────
echo ""
echo "✅ Release build complete!"
echo ""
echo "Release files:"
echo "  Tarball:   $BUILD_DIR/$TAR_NAME"
echo "  Checksum:  $BUILD_DIR/${TAR_NAME}.sha256"
echo ""
echo "Next steps:"
echo "  1. Create GitHub release tagged as ${VERSION}"
echo "  2. Upload these files to the release"
echo "  3. Tag commit: git tag ${VERSION}"
echo "  4. Push tag: git push origin ${VERSION}"
