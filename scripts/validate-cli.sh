#!/bin/bash
set -e
EXIT_CODE=0

# Validate that no legacy commands are present in the codebase.
# This prevents developers from introducing standalone CLI invocations while
# allowing the Linux systemd service name (dso-agent.service).

echo "🔍 Scanning for legacy CLI usage (dso-agent, standalone dso)..."

# Exceptions: 
# - README.md (mentions deprecation)
# - CHANGELOG.md (historical)
# - ADOPTERS.md
# - CONTRIBUTING.md (unless it's a command example we missed)
# - scripts/uninstall.sh (might need to handle legacy cleanup)

EXCLUDE_PATHS=(
  ':!README.md'
  ':!CHANGELOG.md'
  ':!ADOPTERS.md'
  ':!CONTRIBUTING.md'
  ':!docs/index.md'
  ':!scripts/uninstall.sh'
  ':!scripts/install.sh'
  ':!scripts/install.ps1'
  ':!scripts/validate-cli.sh'
  ':!Dockerfile'
  ':!CNCF_SANDBOX_APPLICATION.md'
)

# Scan tracked text files only, so ignored local build artifacts cannot fail CI.
# dso-agent is allowed as the systemd service name but not as a direct command.
LEGACY_AGENT=$(git grep -nI -E '(^|[[:space:]`"'"'"'])(sudo[[:space:]]+)?dso-agent([[:space:]`"'"'"']|$)' -- . "${EXCLUDE_PATHS[@]}" \
  | grep -vE 'dso-agent\.service|systemd|systemctl|journalctl|service|daemon|Agent:|Monitor:|Ensure .*dso-agent.*running|Cloud Mode Agent' \
  | grep -v '"-u", "dso-agent"' || true)

# Block standalone "dso <command>" examples/usages. The Docker CLI plugin form is
# "docker dso <command>", and the internal systemd ExecStart is explicitly allowed.
LEGACY_DSO=$(git grep -nI -E '(^|[[:space:]`"'"'"'])(sudo[[:space:]]+)?dso[[:space:]]+(init|apply|sync|inject|diff|env|secret|system|up|down|logs|watch|fetch|compose|inspect|legacy-agent)\b' -- . "${EXCLUDE_PATHS[@]}" \
  | grep -vE 'docker dso|docker-dso|ExecStart=/usr/local/bin/dso legacy-agent' || true)

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
