#!/bin/bash
set -e

echo "=== STEP 1: REPOSITORY AUDIT ==="
echo ""

# Check for dead code and unused imports
echo "1. Go Unused Variables Check..."
go vet ./internal/webui ./internal/cli 2>&1 | head -20 || true

echo ""
echo "2. Go Build Info..."
go build -v ./cmd/dso 2>&1 | grep -E "(webui|ui)" | head -10 || true

echo ""
echo "3. Checking for unused dependencies in go.mod..."
# Get current dependencies used
grep -E "require|indirect" go.mod || true

echo ""
echo "4. File Structure Review..."
ls -lah internal/webui/
echo ""
ls -lah internal/cli/ | grep ui

echo ""
echo "5. Web Asset Structure..."
ls -lah web/out 2>/dev/null | head -20 || echo "web/out missing - will be built"
ls -lah internal/webui/assets 2>/dev/null | head -20 || echo "internal/webui/assets missing - will be built"

echo ""
echo "6. Searching for TODO/FIXME/HACK comments..."
grep -r "TODO\|FIXME\|HACK\|XXX" internal/webui internal/cli/ui.go web/lib web/hooks 2>/dev/null || echo "No TODOs found"

echo ""
echo "7. Checking for console.log in production code..."
grep -r "console.log\|console.error\|console.warn" web/lib web/hooks web/components 2>/dev/null | grep -v ".test.ts" | grep -v "// log" | head -10 || echo "No problematic console statements found"

echo ""
echo "8. Dead code analysis (unused functions)..."
go list -m all | head -10

