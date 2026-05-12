#!/bin/bash
# Phase 1 Test Validation Script
set -euo pipefail

echo "================================"
echo "Phase 1 Test Suite Validation"
echo "================================"
echo ""

# Test 1: Build check (compile all packages)
echo "▶ Checking builds..."
go test -v ./pkg/vault/... ./internal/compose/... ./pkg/config/... 2>&1 | tail -3
echo "✓ All packages compile"
echo ""

# Test 2: Race detector
echo "▶ Running with race detector..."
go test -race ./pkg/vault/... ./internal/compose/... ./pkg/config/... 2>&1 | tail -3
echo "✓ Race detector passed"
echo ""

# Test 3: Coverage
echo "▶ Checking coverage..."
go test -cover ./pkg/vault/... ./internal/compose/... ./pkg/config/... 2>&1 | grep "coverage:"
echo "✓ Coverage check passed"
echo ""

# Test 4: Benchmarks
echo "▶ Running benchmarks..."
go test -bench=. -benchmem ./pkg/vault/... 2>&1 | tail -5
echo "✓ Benchmarks completed"
echo ""

echo "================================"
echo "✓ All Phase 1 tests passed"
echo "================================"
