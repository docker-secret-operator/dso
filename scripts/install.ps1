# ==============================================================================
# Docker Secret Operator (DSO) - Windows installer: REFUSES TO INSTALL (M0-05)
# ==============================================================================
# DSO publishes signed release artifacts for linux/amd64, linux/arm64, darwin/amd64
# and darwin/arm64 only (.goreleaser.yml builds no Windows target, and the release
# workflow signs only those artifacts). There is therefore no authenticated Windows
# artifact for this script to verify.
#
# The previous version of this script cloned the default branch of the repository
# and compiled it, with no signature, tag or commit verification. That installed
# unauthenticated code, so it has been removed: this script now fails closed and
# installs nothing.
#
# Supported ways to run DSO from a Windows machine:
#   * Run the Linux release inside WSL2 or a Linux VM: install it there with
#     scripts/install.sh (verifies the cosign signature of the release).
#   * Use the container image on Docker Desktop.
#   * Build from source yourself at a tag you have verified
#     (git verify-tag / review the commit), with your own toolchain. That build is
#     not an authenticated release artifact and is not supported by this script.
# See docs/getting-started.md.
# ==============================================================================

$ErrorActionPreference = "Stop"

Write-Host "Error: DSO does not publish a signed Windows release, so this installer will not install anything." -ForegroundColor Red
Write-Host "       Supported paths: scripts/install.sh inside WSL2/Linux, or the container image." -ForegroundColor Red
Write-Host "       See docs/getting-started.md (Installation)." -ForegroundColor Red
exit 1
