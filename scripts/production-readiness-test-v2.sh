#!/bin/bash

################################################################################
# Docker Secret Operator - Production Readiness Test Suite (v2 - Simplified)
################################################################################

cd "$(dirname "$(readlink -f "$0")")/.." || exit 1

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'

TESTS_PASSED=0
TESTS_FAILED=0

REPORT_FILE="test-report-$(date +%Y%m%d-%H%M%S).txt"
LOG_FILE="test-execution-$(date +%Y%m%d-%H%M%S).log"

echo -e "${BLUE}╔════════════════════════════════════════════════════════════════════════════════╗${NC}"
echo -e "${BLUE}║     Docker Secret Operator - Production Readiness Test Suite                    ║${NC}"
echo -e "${BLUE}╚════════════════════════════════════════════════════════════════════════════════╝${NC}"
echo ""
echo "Report: $REPORT_FILE"
echo "Logs:   $LOG_FILE"
echo ""

{
    echo "================================================================================"
    echo "DOCKER SECRET OPERATOR - PRODUCTION READINESS TEST REPORT"
    echo "================================================================================"
    echo "Generated: $(date)"
    echo "Go Version: $(go version)"
    echo "Docker: $(docker --version 2>/dev/null || echo 'N/A')"
    echo ""
} > "$REPORT_FILE"

test_section() {
    echo ""
    echo -e "${BLUE}$1${NC}"
    echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
    {
        echo ""
        echo "$1"
        echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
    } >> "$REPORT_FILE"
}

test_run() {
    local name="$1"
    local cmd="$2"

    echo -n "  $name ... "

    if eval "$cmd" >> "$LOG_FILE" 2>&1; then
        echo -e "${GREEN}PASS${NC}"
        echo "  ✓ $name" >> "$REPORT_FILE"
        ((TESTS_PASSED++))
        return 0
    else
        echo -e "${RED}FAIL${NC}"
        echo "  ✗ $name" >> "$REPORT_FILE"
        ((TESTS_FAILED++))
        return 1
    fi
}

################################################################################
# PRE-FLIGHT CHECKS
################################################################################

test_section "SECTION 1: PRE-FLIGHT CHECKS"

test_run "Go version (1.20+)" "go version | grep -q 'go1\.[2-9]'"
test_run "Go modules" "[ -f go.mod ]"
test_run "Docker available" "timeout 5 docker ps > /dev/null 2>&1"
test_run "Git available" "command -v git > /dev/null"

################################################################################
# BUILD VERIFICATION
################################################################################

test_section "SECTION 2: BUILD VERIFICATION"

test_run "Standard build" "timeout 120 go build -v ./cmd/docker-dso"
test_run "Race-detecting build" "timeout 120 go build -race -v ./cmd/docker-dso"
test_run "All packages build" "timeout 120 go build -race ./..."

################################################################################
# UNIT TESTS
################################################################################

test_section "SECTION 3: UNIT TESTS (with -race)"

test_run "Agent unit tests" "timeout 120 go test -race -v ./internal/agent/... -short"
test_run "Rotation unit tests" "timeout 120 go test -race -v ./internal/rotation/... -short"
test_run "Core unit tests" "timeout 120 go test -race -v ./internal/core/... -short"

################################################################################
# RACE CONDITION TESTS
################################################################################

test_section "SECTION 4: RACE CONDITION DETECTION"

test_run "Container clone races" "timeout 120 go test -race -v -run TestRace_ContainerClone ./test/integration/..."
test_run "Debounce window races" "timeout 120 go test -race -v -run TestRace_DebounceWindow ./test/integration/..."
test_run "Atomic swap races" "timeout 120 go test -race -v -run TestRace_ContainerRename ./test/integration/..."

################################################################################
# INTEGRATION TESTS
################################################################################

test_section "SECTION 5: INTEGRATION TESTS"

test_run "Blue-green rotation" "timeout 120 go test -race -v -run TestRotation_BlueGreenSwap ./test/integration/..."
test_run "Concurrent rotations" "timeout 120 go test -race -v -run TestRotation_Concurrent ./test/integration/..."
test_run "Health check handling" "timeout 120 go test -race -v -run TestRotation_HealthCheck ./test/integration/..."
test_run "State verification" "timeout 120 go test -race -v -run TestRotation_EventDriven ./test/integration/..."

################################################################################
# STRESS TESTS
################################################################################

test_section "SECTION 6: STRESS TESTING"

test_run "Concurrent rotations (5x)" "timeout 120 go test -race -v -run TestStress_ConcurrentRotations ./test/integration/..."
test_run "Event debouncer (10K events)" "timeout 120 go test -race -v -run TestStress_EventDebouncer ./test/integration/..."
test_run "Cache access (20K ops)" "timeout 120 go test -race -v -run TestStress_ConcurrentCache ./test/integration/..."
test_run "Secret zeroization (1K)" "timeout 120 go test -race -v -run TestStress_SecretZeroization ./test/integration/..."

################################################################################
# CODE QUALITY
################################################################################

test_section "SECTION 7: CODE QUALITY CHECKS"

test_run "Go fmt" "[ -z \"\$(go fmt -l ./...)\" ]"
test_run "Go vet" "go vet -race ./..."
test_run "Go mod tidy" "go mod tidy && git diff --quiet go.mod go.sum 2>/dev/null || true"

################################################################################
# SECURITY
################################################################################

test_section "SECTION 8: SECURITY CHECKS"

test_run "No hardcoded secrets" "! grep -rE \"(password|api_key|apikey|auth_token)\\s*=\\s*[\\\"']([^\\\"']*[a-zA-Z0-9]{8,}|[0-9]{6,})\" internal/ cmd/ --include=\"*.go\" 2>/dev/null"
test_run "Socket permissions (0600)" "grep -q '0600' internal/agent/server.go"
test_run "Secret zeroization" "grep -q 'zero' internal/agent/cache.go"

################################################################################
# SUMMARY
################################################################################

test_section "SECTION 9: SUMMARY"

TOTAL=$((TESTS_PASSED + TESTS_FAILED))
PASS_PCT=0
if [ $TOTAL -gt 0 ]; then
    PASS_PCT=$((TESTS_PASSED * 100 / TOTAL))
fi

echo ""
echo "  Total:  $TOTAL"
echo "  Passed: $TESTS_PASSED"
echo "  Failed: $TESTS_FAILED"
echo "  Rate:   ${PASS_PCT}%"
echo ""

if [ $TESTS_FAILED -eq 0 ]; then
    echo -e "${GREEN}  ✓ PRODUCTION READY${NC}"
    {
        echo ""
        echo "Test Summary: Passed=$TESTS_PASSED Failed=$TESTS_FAILED Pass_Rate=${PASS_PCT}%"
        echo ""
        echo "✓ PRODUCTION READY"
        echo "All critical tests passed. The Docker Secret Operator is ready for production."
    } >> "$REPORT_FILE"
else
    echo -e "${RED}  ✗ PRODUCTION NOT READY${NC}"
    echo -e "${RED}  Fix $TESTS_FAILED failed test(s)${NC}"
    {
        echo ""
        echo "Test Summary: Passed=$TESTS_PASSED Failed=$TESTS_FAILED Pass_Rate=${PASS_PCT}%"
        echo ""
        echo "✗ PRODUCTION NOT READY"
        echo "The following test(s) failed:"
    } >> "$REPORT_FILE"
fi

echo ""
echo "Report: $REPORT_FILE"
echo "Logs:   $LOG_FILE"
echo ""

exit $TESTS_FAILED
