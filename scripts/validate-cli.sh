#!/bin/bash
set -e

# Validate that no legacy commands are present in the codebase.
# This prevents developers from introducing standalone 'dso' or 'dso-agent' references.

echo "🔍 Scanning for legacy CLI usage (dso-agent, standalone dso)..."

# Exceptions: 
# - README.md (mentions deprecation)
# - CHANGELOG.md (historical)
# - ADOPTERS.md
# - CONTRIBUTING.md (unless it's a command example we missed)
# - scripts/uninstall.sh (might need to handle legacy cleanup)

EXCLUDE_FILES="README.md|CHANGELOG.md|ADOPTERS.md|CONTRIBUTING.md|docs/index.md|scripts/uninstall.sh|scripts/install.sh|scripts/install.ps1|scripts/validate-cli.sh|Dockerfile|CNCF_SANDBOX_APPLICATION.md"

LEGACY_AGENT=$(grep -rE "\bdso-agent\b" . --exclude-dir=.git | grep -vE "$EXCLUDE_FILES" || true)
# Only block 'dso ' if NOT preceded by 'docker ' and it's followed by a space
LEGACY_DSO=$(grep -r "dso " . --exclude-dir=.git | grep -v "docker-dso" | grep -v "docker dso" | grep -vE "$EXCLUDE_FILES" || true)

if [ -n "$LEGACY_AGENT" ]; then
    echo "❌ Found legacy 'dso-agent' usage:"
    echo "$LEGACY_AGENT"
    EXIT_CODE=1
fi

if [ -n "$LEGACY_DSO" ]; then
    echo "❌ Found legacy 'dso' standalone usage:"
    echo "$LEGACY_DSO"
    EXIT_CODE=1
fi

if [ "$EXIT_CODE" = "1" ]; then
    echo "🚨 FAIL: Legacy commands detected. Use 'docker dso ...' instead."
    exit 1
fi

echo "✅ No legacy commands found outside allowed files."
exit 0
