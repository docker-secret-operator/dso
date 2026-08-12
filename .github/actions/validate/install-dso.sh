#!/usr/bin/env bash
# Installs a specific, signature-verified DSO release for the "DSO Validate"
# composite action (../action.yml). Intentionally boring: download the
# release's checksums file, verify its cosign signature, verify the target
# platform archive's checksum against the (now-trusted) checksums file,
# extract, install. No validation logic lives here -- this script's only
# job ends the moment docker-dso is on disk and executable.
set -euo pipefail

REPO="docker-secret-operator/dso"
VERSION="${DSO_VERSION:?DSO_VERSION environment variable is required}"

# ── Version resolution ──────────────────────────────────────────────────
#
# "latest" is the one explicit, documented opt-in to a non-pinned install;
# it is resolved to a concrete tag via the GitHub Releases API right here,
# not deferred to a moving reference like a branch. Anything else must
# already look like a release tag -- this script never falls back to
# main/master on a bad or missing version.
if [[ "$VERSION" == "latest" ]]; then
	echo "::notice::Resolving 'latest' DSO release (a pinned version is recommended for deterministic CI)"
	VERSION="$(curl -sSfL "https://api.github.com/repos/${REPO}/releases/latest" |
		grep -m1 '"tag_name"' | sed -E 's/.*"tag_name": *"([^"]+)".*/\1/')"
	if [[ -z "$VERSION" ]]; then
		echo "::error::Failed to resolve the latest DSO release tag from the GitHub API"
		exit 1
	fi
	echo "::notice::Resolved 'latest' to ${VERSION}"
fi

if [[ ! "$VERSION" =~ ^v[0-9]+\.[0-9]+\.[0-9]+ ]]; then
	echo "::error::Invalid version '${VERSION}' -- expected a release tag like v3.5.21, or the literal 'latest'"
	exit 1
fi
VERSION_NO_V="${VERSION#v}"

# ── Platform mapping ─────────────────────────────────────────────────────
#
# Mirrors .goreleaser.yml exactly: builds are published for linux/darwin,
# amd64/arm64 only. No Windows artifacts exist -- fail clearly rather than
# attempt a download that cannot succeed.
OS_NAME="$(uname -s)"
ARCH_NAME="$(uname -m)"
case "$OS_NAME" in
Linux) DSO_OS="linux" ;;
Darwin) DSO_OS="darwin" ;;
*)
	echo "::error::Unsupported runner OS '${OS_NAME}'. DSO publishes release binaries for Linux and macOS only (see .goreleaser.yml) -- Windows runners are not supported by this action."
	exit 1
	;;
esac
case "$ARCH_NAME" in
x86_64 | amd64) DSO_ARCH="amd64" ;;
arm64 | aarch64) DSO_ARCH="arm64" ;;
*)
	echo "::error::Unsupported runner architecture '${ARCH_NAME}'. DSO publishes amd64 and arm64 release binaries only."
	exit 1
	;;
esac
PLATFORM="${DSO_OS}-${DSO_ARCH}"

BASE_URL="https://github.com/${REPO}/releases/download/${VERSION}"
ARCHIVE="dso-${VERSION_NO_V}-${PLATFORM}.tar.gz"
CHECKSUMS="dso-${VERSION_NO_V}-checksums.txt"

WORKDIR="$(mktemp -d)"
trap 'rm -rf "$WORKDIR"' EXIT
cd "$WORKDIR"

download() {
	local name="$1"
	if ! curl -sSfL -o "$name" "${BASE_URL}/${name}"; then
		echo "::error::Failed to download ${name} for release ${VERSION}. Confirm the release and this platform's artifact exist: ${BASE_URL}/${name}"
		exit 1
	fi
}

echo "::group::Downloading DSO ${VERSION} (${PLATFORM})"
download "$CHECKSUMS"
download "${CHECKSUMS}.sig"
download "${CHECKSUMS}.pem"
download "$ARCHIVE"
echo "::endgroup::"

# ── Signature verification ───────────────────────────────────────────────
#
# The checksums file is signed with cosign's keyless (Sigstore/OIDC)
# signing during release (.goreleaser.yml's `signs:` block, produced by
# .github/workflows/release.yml running as a GitHub Actions workflow in
# THIS repository). Verification is pinned to that exact identity: only a
# checksums file signed by a run of docker-secret-operator/dso's own
# release.yml workflow, issued by GitHub's OIDC token service, is
# accepted. A checksum match alone is not proof of authenticity --
# checksums.txt itself must be proven to come from the real release
# pipeline before its contents are trusted for anything.
echo "::group::Verifying release signature (cosign, keyless/Sigstore)"
if ! command -v cosign >/dev/null 2>&1; then
	echo "::error::cosign is not on PATH. This action's install-cosign step (sigstore/cosign-installer) must run before this script."
	exit 1
fi
if ! cosign verify-blob \
	--certificate "${CHECKSUMS}.pem" \
	--signature "${CHECKSUMS}.sig" \
	--certificate-identity-regexp "^https://github\\.com/${REPO}/\\.github/workflows/release\\.yml@refs/tags/.*\$" \
	--certificate-oidc-issuer "https://token.actions.githubusercontent.com" \
	"$CHECKSUMS"; then
	echo "::error::Signature verification FAILED for ${CHECKSUMS}. Refusing to install an unverified DSO release. This could mean the release is not authentic, or was not published by docker-secret-operator/dso's own release workflow."
	exit 1
fi
echo "Signature verified: ${CHECKSUMS} was signed by docker-secret-operator/dso's release workflow."
echo "::endgroup::"

# ── Archive checksum verification ────────────────────────────────────────
#
# Only now that checksums.txt itself is proven authentic does its content
# get used to validate the actual archive.
echo "::group::Verifying archive checksum"
EXPECTED_SUM="$(grep " ${ARCHIVE}\$" "$CHECKSUMS" | awk '{print $1}')"
if [[ -z "$EXPECTED_SUM" ]]; then
	echo "::error::No checksum entry for ${ARCHIVE} in the signature-verified checksums file. This platform's artifact may not have been published for ${VERSION}."
	exit 1
fi
if command -v sha256sum >/dev/null 2>&1; then
	ACTUAL_SUM="$(sha256sum "$ARCHIVE" | awk '{print $1}')"
elif command -v shasum >/dev/null 2>&1; then
	ACTUAL_SUM="$(shasum -a 256 "$ARCHIVE" | awk '{print $1}')"
else
	echo "::error::Neither sha256sum nor shasum is available on this runner -- cannot verify the archive checksum."
	exit 1
fi
if [[ "$EXPECTED_SUM" != "$ACTUAL_SUM" ]]; then
	echo "::error::Checksum mismatch for ${ARCHIVE}. Expected ${EXPECTED_SUM}, got ${ACTUAL_SUM}. Refusing to install a corrupted or tampered archive."
	exit 1
fi
echo "Checksum verified: ${ARCHIVE}"
echo "::endgroup::"

# ── Install ───────────────────────────────────────────────────────────────
echo "::group::Installing"
mkdir -p extracted
if ! tar -xzf "$ARCHIVE" -C extracted; then
	echo "::error::Failed to extract ${ARCHIVE} -- archive may be corrupted despite passing checksum verification."
	exit 1
fi
BINARY="$(find extracted -maxdepth 2 -type f -name 'docker-dso' | head -n1)"
if [[ -z "$BINARY" ]]; then
	echo "::error::docker-dso binary not found inside ${ARCHIVE} after extraction."
	exit 1
fi
chmod +x "$BINARY"

INSTALL_DIR="${RUNNER_TEMP:-/tmp}/dso-action-bin"
mkdir -p "$INSTALL_DIR"
cp "$BINARY" "${INSTALL_DIR}/docker-dso"
echo "::endgroup::"

if ! "${INSTALL_DIR}/docker-dso" version >/dev/null 2>&1; then
	echo "::error::Installed docker-dso binary failed to execute (docker-dso version returned an error)."
	exit 1
fi

{
	echo "dso-path=${INSTALL_DIR}/docker-dso"
} >>"${GITHUB_OUTPUT:?GITHUB_OUTPUT is not set -- this script must run inside a GitHub Actions step}"
echo "${INSTALL_DIR}" >>"${GITHUB_PATH:?GITHUB_PATH is not set -- this script must run inside a GitHub Actions step}"

echo "Installed DSO ${VERSION} (${PLATFORM}) -> ${INSTALL_DIR}/docker-dso"
