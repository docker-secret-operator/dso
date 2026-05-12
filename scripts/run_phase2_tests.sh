#!/bin/bash
# Phase 2 Test Validation Script
set -eo pipefail

echo "================================================="
echo "Phase 2 Test Suite Validation: Injector & CLI"
echo "================================================="
echo ""

PACKAGES="./internal/injector/... ./internal/resolver/... ./internal/providers/... ./internal/cli/..."

# Test 1: Build & Execution check
echo "▶ 1. Checking builds and running tests..."
if ! go test -count=1 -v $PACKAGES 2>&1 > test_output.log; then
    echo "❌ Tests failed! See test_output.log for details"
    cat test_output.log | tail -20
    exit 1
fi
echo "✓ Tests executed successfully"
echo ""

# Test 2: Race detector
echo "▶ 2. Running with race detector (concurrency safety)..."
if ! go test -count=1 -race $PACKAGES 2>&1 > race_output.log; then
    echo "❌ Race detector failed! See race_output.log for details"
    cat race_output.log | tail -20
    exit 1
fi
echo "✓ Race detector passed"
echo ""

# Test 3: Coverage with Thresholds
echo "▶ 3. Checking coverage..."

# Define minimum coverage thresholds (Package:Threshold)
THRESHOLDS=(
    "github.com/docker-secret-operator/dso/internal/injector:85"
    "github.com/docker-secret-operator/dso/internal/resolver:90"
    "github.com/docker-secret-operator/dso/internal/providers:85"
    "github.com/docker-secret-operator/dso/internal/cli:20"
)

# Generate coverage output
go test -count=1 -cover $PACKAGES > coverage.log

FAIL=0
for ENTRY in "${THRESHOLDS[@]}"; do
    PKG="${ENTRY%%:*}"
    TARGET="${ENTRY##*:}"
    
    # Extract coverage percentage for the package
    COV_LINE=$(grep "^ok.*$PKG" coverage.log || grep "^FAIL.*$PKG" coverage.log || true)
    
    if [ -z "$COV_LINE" ]; then
        echo "❌ No coverage found for $PKG"
        FAIL=1
        continue
    fi
    
    # Example COV_LINE: ok  	github.com/docker-secret-operator/dso/internal/injector	0.207s	coverage: 72.0% of statements
    COV_PCT=$(echo "$COV_LINE" | grep -oE 'coverage: [0-9.]+' | awk '{print $2}')
    
    if [ -z "$COV_PCT" ]; then
        echo "❌ Could not parse coverage for $PKG"
        FAIL=1
        continue
    fi
    
    # Compare float
    if awk "BEGIN {exit !($COV_PCT >= $TARGET)}"; then
        echo "✓ $PKG coverage $COV_PCT% (Target: $TARGET%)"
    else
        echo "❌ $PKG coverage $COV_PCT% (Target: $TARGET%)"
        FAIL=1
    fi
done

echo ""
if [ "$FAIL" -eq 1 ]; then
    echo "================================================="
    echo "❌ Phase 2 tests failed due to coverage targets"
    echo "================================================="
    exit 1
fi

echo "================================================="
echo "✓ All Phase 2 tests completed successfully"
echo "================================================="
