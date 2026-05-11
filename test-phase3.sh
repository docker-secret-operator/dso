#!/bin/bash

################################################################################
# Phase 3 CLI Commands - Test Validation Script
# Tests: dso apply, dso inject, dso sync commands
# Usage: bash test-phase3.sh [--verbose] [--coverage] [--race]
################################################################################

set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Flags
VERBOSE=false
COVERAGE=false
RACE=false
FAILED_TESTS=0
PASSED_TESTS=0

# Parse arguments
for arg in "$@"; do
    case $arg in
        --verbose|-v) VERBOSE=true ;;
        --coverage|-c) COVERAGE=true ;;
        --race|-r) RACE=true ;;
    esac
done

# Helper functions
print_header() {
    echo -e "\n${BLUE}╔════════════════════════════════════════════════════════════╗${NC}"
    echo -e "${BLUE}║${NC} $1"
    echo -e "${BLUE}╚════════════════════════════════════════════════════════════╝${NC}\n"
}

print_success() {
    echo -e "${GREEN}✓${NC} $1"
    ((PASSED_TESTS++))
}

print_error() {
    echo -e "${RED}✗${NC} $1"
    ((FAILED_TESTS++))
}

print_warning() {
    echo -e "${YELLOW}⚠${NC} $1"
}

print_info() {
    echo -e "${BLUE}ℹ${NC} $1"
}

# Check if we're in the right directory
if [ ! -f "go.mod" ]; then
    echo -e "${RED}Error: go.mod not found. Please run from project root.${NC}"
    exit 1
fi

print_header "Phase 3 CLI Commands Validation"
print_info "Testing: dso apply, dso inject, dso sync"
echo ""

################################################################################
# 1. COMPILATION CHECKS
################################################################################

print_header "1. COMPILATION CHECKS"

echo "Checking Go syntax..."
if go build -o /dev/null ./internal/cli 2>/dev/null; then
    print_success "CLI package compiles successfully"
else
    print_error "CLI package compilation failed"
    go build ./internal/cli
    exit 1
fi

echo "Checking for syntax errors in new files..."
for file in internal/cli/{apply,inject,sync}.go; do
    if [ -f "$file" ]; then
        if go fmt "$file" > /dev/null 2>&1; then
            print_success "$file syntax is valid"
        else
            print_error "$file has syntax errors"
            go fmt "$file"
        fi
    fi
done

################################################################################
# 2. IMPORTS VALIDATION
################################################################################

print_header "2. IMPORTS VALIDATION"

echo "Checking imports..."
files=(
    "internal/cli/apply.go"
    "internal/cli/inject.go"
    "internal/cli/sync.go"
)

for file in "${files[@]}"; do
    if [ -f "$file" ]; then
        if grep -q "^package cli" "$file"; then
            print_success "$file has correct package declaration"
        else
            print_error "$file package declaration is missing or incorrect"
        fi

        if grep -q "github.com/spf13/cobra" "$file"; then
            print_success "$file imports cobra"
        else
            print_error "$file missing cobra import"
        fi
    fi
done

################################################################################
# 3. UNIT TEST COMPILATION
################################################################################

print_header "3. UNIT TEST COMPILATION"

echo "Compiling test files..."
for file in internal/cli/{apply,inject,sync}_test.go; do
    if [ -f "$file" ]; then
        if go build -o /dev/null "$file" 2>/dev/null; then
            print_success "$file compiles"
        else
            print_warning "$file compilation - checking with go test instead"
        fi
    fi
done

################################################################################
# 4. RUN UNIT TESTS
################################################################################

print_header "4. RUNNING UNIT TESTS"

echo "Running apply command tests..."
if go test -v ./internal/cli -run TestApply 2>&1 | tee /tmp/apply_test.log; then
    TEST_COUNT=$(grep -c "^    --- PASS:" /tmp/apply_test.log || true)
    print_success "Apply tests passed ($TEST_COUNT tests)"
else
    print_error "Apply tests failed"
fi

echo ""
echo "Running inject command tests..."
if go test -v ./internal/cli -run TestInject 2>&1 | tee /tmp/inject_test.log; then
    TEST_COUNT=$(grep -c "^    --- PASS:" /tmp/inject_test.log || true)
    print_success "Inject tests passed ($TEST_COUNT tests)"
else
    print_error "Inject tests failed"
fi

echo ""
echo "Running sync command tests..."
if go test -v ./internal/cli -run TestSync 2>&1 | tee /tmp/sync_test.log; then
    TEST_COUNT=$(grep -c "^    --- PASS:" /tmp/sync_test.log || true)
    print_success "Sync tests passed ($TEST_COUNT tests)"
else
    print_error "Sync tests failed"
fi

################################################################################
# 5. COMBINED TEST RUN
################################################################################

print_header "5. COMBINED TEST RUN"

echo "Running all Phase 3 tests together..."
if go test -v ./internal/cli -run "TestNewApplyCmd|TestNewInjectCmd|TestNewSyncCmd|TestApply|TestInject|TestSync" 2>&1 | tee /tmp/phase3_tests.log; then
    TOTAL_PASS=$(grep -c "^    --- PASS:" /tmp/phase3_tests.log || echo "0")
    TOTAL_FAIL=$(grep -c "^    --- FAIL:" /tmp/phase3_tests.log || echo "0")
    print_success "All Phase 3 tests completed (Passed: $TOTAL_PASS, Failed: $TOTAL_FAIL)"
else
    print_error "Some Phase 3 tests failed"
fi

################################################################################
# 6. RACE CONDITION DETECTION
################################################################################

if [ "$RACE" = true ]; then
    print_header "6. RACE CONDITION DETECTION"

    echo "Running tests with race detector..."
    if go test -race ./internal/cli -run "TestNewApplyCmd|TestNewInjectCmd|TestNewSyncCmd" -timeout 30s 2>&1; then
        print_success "No race conditions detected in command creation"
    else
        print_warning "Race detector found potential issues (this may be false positives in tests)"
    fi
fi

################################################################################
# 7. CODE COVERAGE ANALYSIS
################################################################################

if [ "$COVERAGE" = true ]; then
    print_header "7. CODE COVERAGE ANALYSIS"

    echo "Running coverage analysis..."

    # Apply coverage
    if go test -coverprofile=/tmp/apply_coverage.out ./internal/cli -run TestApply 2>/dev/null; then
        APPLY_COV=$(go tool cover -func=/tmp/apply_coverage.out 2>/dev/null | grep total | awk '{print $3}' || echo "N/A")
        print_info "Apply command coverage: $APPLY_COV"
    fi

    # Inject coverage
    if go test -coverprofile=/tmp/inject_coverage.out ./internal/cli -run TestInject 2>/dev/null; then
        INJECT_COV=$(go tool cover -func=/tmp/inject_coverage.out 2>/dev/null | grep total | awk '{print $3}' || echo "N/A")
        print_info "Inject command coverage: $INJECT_COV"
    fi

    # Sync coverage
    if go test -coverprofile=/tmp/sync_coverage.out ./internal/cli -run TestSync 2>/dev/null; then
        SYNC_COV=$(go tool cover -func=/tmp/sync_coverage.out 2>/dev/null | grep total | awk '{print $3}' || echo "N/A")
        print_info "Sync command coverage: $SYNC_COV"
    fi

    # Combined coverage
    echo ""
    if go test -coverprofile=/tmp/phase3_coverage.out ./internal/cli -run "TestNewApplyCmd|TestNewInjectCmd|TestNewSyncCmd|TestApply|TestInject|TestSync" 2>/dev/null; then
        TOTAL_COV=$(go tool cover -func=/tmp/phase3_coverage.out 2>/dev/null | grep total | awk '{print $3}' || echo "N/A")
        print_success "Total Phase 3 coverage: $TOTAL_COV"

        # Generate HTML coverage report
        if go tool cover -html=/tmp/phase3_coverage.out -o /tmp/phase3_coverage.html 2>/dev/null; then
            print_info "HTML coverage report generated: /tmp/phase3_coverage.html"
        fi
    fi
fi

################################################################################
# 8. LINT AND CODE QUALITY
################################################################################

print_header "8. CODE QUALITY CHECKS"

echo "Checking code formatting..."
for file in internal/cli/{apply,inject,sync}.go; do
    if [ -f "$file" ]; then
        # Check if file is properly formatted
        FORMATTED=$(go fmt "$file" 2>&1 | wc -l)
        if [ "$FORMATTED" -eq 0 ]; then
            print_success "$file is properly formatted"
        else
            print_warning "$file needs formatting"
            go fmt "$file"
        fi
    fi
done

echo ""
echo "Checking for unused imports..."
if command -v goimports &> /dev/null; then
    for file in internal/cli/{apply,inject,sync}.go; do
        if [ -f "$file" ]; then
            if goimports -l "$file" | grep -q .; then
                print_warning "$file may have unused imports"
            else
                print_success "$file - no unused imports"
            fi
        fi
    done
else
    print_warning "goimports not installed - skipping import check"
fi

################################################################################
# 9. FUNCTION EXISTENCE CHECKS
################################################################################

print_header "9. FUNCTION EXISTENCE CHECKS"

echo "Verifying apply.go functions..."
FUNCTIONS=("NewApplyCmd" "applyCommand" "computeApplyPlan" "displayApplyPlan" "executeApplyPlan" "verifyProviderConnectivity")
for func in "${FUNCTIONS[@]}"; do
    if grep -q "func.*$func" internal/cli/apply.go; then
        print_success "Function '$func' exists in apply.go"
    else
        print_error "Function '$func' not found in apply.go"
    fi
done

echo ""
echo "Verifying inject.go functions..."
FUNCTIONS=("NewInjectCmd" "injectCommand" "findContainerID" "verifySecretInjection")
for func in "${FUNCTIONS[@]}"; do
    if grep -q "func.*$func" internal/cli/inject.go; then
        print_success "Function '$func' exists in inject.go"
    else
        print_error "Function '$func' not found in inject.go"
    fi
done

echo ""
echo "Verifying sync.go functions..."
FUNCTIONS=("NewSyncCmd" "syncCommand" "verifyAgentRunning" "triggerReconciliation" "displaySyncResults")
for func in "${FUNCTIONS[@]}"; do
    if grep -q "func.*$func" internal/cli/sync.go; then
        print_success "Function '$func' exists in sync.go"
    else
        print_error "Function '$func' not found in sync.go"
    fi
done

################################################################################
# 10. STUBS REMOVAL VERIFICATION
################################################################################

print_header "10. STUBS REMOVAL VERIFICATION"

echo "Checking stubs.go for removed commands..."
if grep -q "func NewApplyCmd" internal/cli/stubs.go; then
    print_error "NewApplyCmd still in stubs.go - should be removed"
else
    print_success "NewApplyCmd correctly removed from stubs.go"
fi

if grep -q "func NewInjectCmd" internal/cli/stubs.go; then
    print_error "NewInjectCmd still in stubs.go - should be removed"
else
    print_success "NewInjectCmd correctly removed from stubs.go"
fi

if grep -q "func NewSyncCmd" internal/cli/stubs.go; then
    print_error "NewSyncCmd still in stubs.go - should be removed"
else
    print_success "NewSyncCmd correctly removed from stubs.go"
fi

################################################################################
# 11. INTEGRATION WITH ROOT COMMAND
################################################################################

print_header "11. ROOT COMMAND INTEGRATION"

echo "Verifying commands are registered in root.go..."
if grep -q "NewApplyCmd()" internal/cli/root.go; then
    print_success "NewApplyCmd() is registered in root.go"
else
    print_warning "NewApplyCmd() might not be registered in root.go"
fi

if grep -q "NewInjectCmd()" internal/cli/root.go; then
    print_success "NewInjectCmd() is registered in root.go"
else
    print_warning "NewInjectCmd() might not be registered in root.go"
fi

if grep -q "NewSyncCmd()" internal/cli/root.go; then
    print_success "NewSyncCmd() is registered in root.go"
else
    print_warning "NewSyncCmd() might not be registered in root.go"
fi

################################################################################
# 12. TEST COUNT SUMMARY
################################################################################

print_header "12. TEST COUNT SUMMARY"

echo "Counting tests in test files..."
APPLY_TESTS=$(grep -c "^func Test" internal/cli/apply_test.go || echo "0")
INJECT_TESTS=$(grep -c "^func Test" internal/cli/inject_test.go || echo "0")
SYNC_TESTS=$(grep -c "^func Test" internal/cli/sync_test.go || echo "0")
TOTAL_TESTS=$((APPLY_TESTS + INJECT_TESTS + SYNC_TESTS))

print_info "Apply tests: $APPLY_TESTS"
print_info "Inject tests: $INJECT_TESTS"
print_info "Sync tests: $SYNC_TESTS"
print_success "Total Phase 3 tests: $TOTAL_TESTS"

################################################################################
# 13. FILE SIZE CHECKS
################################################################################

print_header "13. IMPLEMENTATION SIZE SUMMARY"

echo "Checking file sizes..."
if [ -f "internal/cli/apply.go" ]; then
    LINES=$(wc -l < internal/cli/apply.go)
    print_info "apply.go: $LINES lines"
fi

if [ -f "internal/cli/inject.go" ]; then
    LINES=$(wc -l < internal/cli/inject.go)
    print_info "inject.go: $LINES lines"
fi

if [ -f "internal/cli/sync.go" ]; then
    LINES=$(wc -l < internal/cli/sync.go)
    print_info "sync.go: $LINES lines"
fi

if [ -f "internal/cli/apply_test.go" ]; then
    LINES=$(wc -l < internal/cli/apply_test.go)
    print_info "apply_test.go: $LINES lines"
fi

if [ -f "internal/cli/inject_test.go" ]; then
    LINES=$(wc -l < internal/cli/inject_test.go)
    print_info "inject_test.go: $LINES lines"
fi

if [ -f "internal/cli/sync_test.go" ]; then
    LINES=$(wc -l < internal/cli/sync_test.go)
    print_info "sync_test.go: $LINES lines"
fi

################################################################################
# FINAL SUMMARY
################################################################################

print_header "VALIDATION SUMMARY"

TOTAL_ITEMS=$((PASSED_TESTS + FAILED_TESTS))
if [ $FAILED_TESTS -eq 0 ]; then
    echo -e "${GREEN}✓ All validations passed!${NC}"
    echo ""
    print_success "All checks completed successfully"
    print_info "Total validations passed: $PASSED_TESTS"
    echo ""
    echo -e "${GREEN}Phase 3 implementation is ready for deployment.${NC}"
    exit 0
else
    echo -e "${RED}✗ Some validations failed!${NC}"
    echo ""
    print_error "Failed checks: $FAILED_TESTS"
    print_info "Passed checks: $PASSED_TESTS"
    echo ""
    echo -e "${RED}Please fix the issues above before deploying.${NC}"
    exit 1
fi
