#!/usr/bin/env bash
# ==============================================================================
# DSO Web UI Asset Builder
# ==============================================================================
# Builds the Next.js dashboard in web/ (deterministically, via `npm ci` +
# the lockfile) and copies its static export output into
# internal/webui/assets/, which internal/webui/embed.go embeds into the
# binary via `//go:embed assets/*`.
#
# This is the single source of truth for regenerating internal/webui/assets/.
# `make webui-build` calls this script; scripts/build-release.sh also calls
# it before producing a release tarball so a release never trusts whatever
# happens to be checked in. CI (.github/workflows/ci.yml, "webui" job) runs
# this same script and fails the build if the regenerated output differs
# from what's committed, so the two directories cannot silently drift apart.
#
# The output is independent of the directory the repository is checked out in
# (see the CheckoutPathNeutralModuleIds plugin in web/next.config.js and
# docs/audit/35), so a developer machine, CI and a release build produce the
# same bytes from the same source + lockfile + Node major.
#
# Usage: ./scripts/build-webui.sh
#   DSO_WEBUI_OUT_DIR=<dir>  publish into <dir> instead of internal/webui/assets
#                            (used by scripts/check-webui-assets.sh). <dir> must
#                            not exist or must be empty; it is never deleted.
# ==============================================================================

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(dirname "$SCRIPT_DIR")"
WEB_DIR="${PROJECT_ROOT}/web"
ASSETS_DIR="${PROJECT_ROOT}/internal/webui/assets"
CUSTOM_OUT=0
if [ -n "${DSO_WEBUI_OUT_DIR:-}" ]; then
    CUSTOM_OUT=1
    ASSETS_DIR="$DSO_WEBUI_OUT_DIR"
    if [ -L "$ASSETS_DIR" ]; then
        echo "Error: DSO_WEBUI_OUT_DIR (${ASSETS_DIR}) is a symlink; refusing." >&2
        exit 1
    fi
    if [ -e "$ASSETS_DIR" ] && { [ ! -d "$ASSETS_DIR" ] || [ -n "$(ls -A "$ASSETS_DIR")" ]; }; then
        echo "Error: DSO_WEBUI_OUT_DIR (${ASSETS_DIR}) exists and is not an empty directory; refusing." >&2
        exit 1
    fi
fi

if [ ! -d "$WEB_DIR" ]; then
    echo "Error: web/ directory not found at ${WEB_DIR}" >&2
    exit 1
fi

# ── Node toolchain check ────────────────────────────────────────────────────
# The supported Node major lives in web/.nvmrc (also enforced by `engines` in
# web/package.json and used by the CI `webui` job). Committed assets are diffed
# against a CI rebuild, so building with an unverified Node major risks drift;
# fail clearly rather than warn. Override (at your own risk) with
# DSO_ALLOW_UNSUPPORTED_NODE=1.
if ! command -v node >/dev/null 2>&1; then
    echo "Error: node not found on PATH (need Node $(tr -d '[:space:]v' < "${WEB_DIR}/.nvmrc").x; see web/.nvmrc)" >&2
    exit 1
fi
WANT_NODE_MAJOR="$(tr -d '[:space:]v' < "${WEB_DIR}/.nvmrc")"
HAVE_NODE_VERSION="$(node --version)"
HAVE_NODE_MAJOR="${HAVE_NODE_VERSION#v}"; HAVE_NODE_MAJOR="${HAVE_NODE_MAJOR%%.*}"
if [ "$HAVE_NODE_MAJOR" != "$WANT_NODE_MAJOR" ]; then
    if [ "${DSO_ALLOW_UNSUPPORTED_NODE:-}" = "1" ]; then
        echo "Warning: Node ${HAVE_NODE_VERSION} is not the supported major ${WANT_NODE_MAJOR} (DSO_ALLOW_UNSUPPORTED_NODE=1 set); continuing." >&2
    else
        echo "Error: Node ${HAVE_NODE_VERSION} found, but the Web UI build requires Node ${WANT_NODE_MAJOR}.x (web/.nvmrc)." >&2
        echo "       Use 'nvm use' in web/ or install Node ${WANT_NODE_MAJOR} LTS. Set DSO_ALLOW_UNSUPPORTED_NODE=1 to override." >&2
        exit 1
    fi
fi

echo "Building DSO Web UI (web/ -> internal/webui/assets/) with Node ${HAVE_NODE_VERSION}..."

# ── Install dependencies deterministically ─────────────────────────────────
echo ""
echo "Installing Node dependencies (npm ci)..."
( cd "$WEB_DIR" && npm ci )
echo "✓ Dependencies installed"

# ── Build + static export ───────────────────────────────────────────────────
# web/next.config.js sets `output: 'export'`, so `npm run build` (plain
# `next build`) already produces a static export in web/out/ -- no separate
# `next export` step is needed (removed in Next.js 14+).
echo ""
echo "Building static export (npm run build)..."
# Start from a clean build state so a stale incremental cache can never leak
# into the committed assets.
rm -rf "${WEB_DIR}/.next" "${WEB_DIR}/out"
( cd "$WEB_DIR" && npm run build )

if [ ! -d "${WEB_DIR}/out" ]; then
    echo "Error: expected static export output at ${WEB_DIR}/out but it does not exist" >&2
    exit 1
fi
echo "✓ Static export built"

# ── Copy into internal/webui/assets/ ────────────────────────────────────────
# embed.go's directive is `//go:embed assets/*`, so assets/ must contain the
# exported site's files directly (not nested under an extra "out/" level).
echo ""
echo "Copying export output into internal/webui/assets/..."
if [ "$CUSTOM_OUT" = "0" ]; then
    rm -rf "$ASSETS_DIR"
fi
mkdir -p "$ASSETS_DIR"
cp -r "${WEB_DIR}/out/." "$ASSETS_DIR/"
echo "✓ Assets copied to ${ASSETS_DIR}"

echo ""
echo "✅ Web UI build complete."
