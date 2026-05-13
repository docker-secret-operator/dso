#!/bin/bash
# DSO Production CI Validator - CNCF-Quality Suite
set -euo pipefail

# Configuration: Minimum Coverage Thresholds (%)
MIN_COVERAGE_VAULT=85
MIN_COVERAGE_RESOLVER=90
MIN_COVERAGE_INJECTOR=85
MIN_COVERAGE_PROVIDERS=44
MIN_COVERAGE_CLI=25
MIN_COVERAGE_CONFIG=85
MIN_COVERAGE_AGENT=15
MIN_COVERAGE_BOOTSTRAP=15
MIN_COVERAGE_EVENTS=70
MIN_COVERAGE_ROTATION=15
MIN_COVERAGE_SECURITY=85
MIN_COVERAGE_OBSERVABILITY=15

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
MAGENTA='\033[0;35m'
CYAN='\033[0;36m'
NC='\033[0m'

echo -e "${BLUE}====================================================${NC}"
echo -e "${BLUE}🚀 DSO CNCF-QUALITY VALIDATION SYSTEM${NC}"
echo -e "${BLUE}====================================================${NC}"

# TRACKER for final summary
FAILED_STEPS=()

check_coverage_threshold() {
    local pkg=$1
    local threshold=$2
    local cov
    
    echo -n -e "  Checking coverage: ${YELLOW}$pkg${NC} (Min: $threshold%)... "
    # Run test and capture coverage.
    cov=$(go test -count=1 -cover "./$pkg" 2>/dev/null | grep "coverage:" | awk '{print $5}' | sed 's/%//' | head -n 1 || echo "0")
    
    if [[ -z "$cov" ]]; then cov="0"; fi
    
    # Check if cov is a valid number
    if [[ ! "$cov" =~ ^[0-9.]+$ ]]; then cov="0"; fi
    
    if (( $(echo "$cov < $threshold" | bc -l) )); then
        echo -e "${RED}FAIL ($cov%)${NC}"
        return 1
    else
        echo -e "${GREEN}PASS ($cov%)${NC}"
        return 0
    fi
}

# 1. Static Analysis
echo -e "\n${CYAN}▶ Phase 1: Static Analysis & Linting${NC}"
echo -n "  Vetting code (go vet)... "
if go vet ./... 2>/tmp/vet_errors; then
    echo -e "${GREEN}PASS${NC}"
else
    echo -e "${RED}FAIL${NC}"
    cat /tmp/vet_errors
    FAILED_STEPS+=("Static Analysis (go vet)")
fi

echo -n "  Checking formatting (go fmt)... "
if [ -z "$(gofmt -l .)" ]; then
    echo -e "${GREEN}PASS${NC}"
else
    echo -e "${RED}FAIL${NC}"
    echo "Files needing formatting:"
    gofmt -l .
    FAILED_STEPS+=("Code Formatting (go fmt)")
fi

# 2. Deterministic Unit Tests
echo -e "\n${CYAN}▶ Phase 2: Deterministic Unit Tests (All Packages)${NC}"
echo -e "  Running tests with -race -short..."
if go test -count=1 -race -short ./... | grep --line-buffered -E "PASS|FAIL|---" | sed 's/^/  /'; then
    echo -e "${GREEN}  ✔ All unit tests passed${NC}"
else
    echo -e "${RED}  ✘ Some unit tests failed${NC}"
    FAILED_STEPS+=("Unit Tests")
fi

# 3. Coverage Enforcement
echo -e "\n${CYAN}▶ Phase 3: Enforcing Coverage Gates${NC}"
GATE_FAILED=0

check_coverage_threshold "pkg/vault" "$MIN_COVERAGE_VAULT" || GATE_FAILED=1
check_coverage_threshold "internal/resolver" "$MIN_COVERAGE_RESOLVER" || GATE_FAILED=1
check_coverage_threshold "internal/injector" "$MIN_COVERAGE_INJECTOR" || GATE_FAILED=1
check_coverage_threshold "internal/providers" "$MIN_COVERAGE_PROVIDERS" || GATE_FAILED=1
check_coverage_threshold "internal/cli" "$MIN_COVERAGE_CLI" || GATE_FAILED=1
check_coverage_threshold "pkg/config" "$MIN_COVERAGE_CONFIG" || GATE_FAILED=1
check_coverage_threshold "internal/agent" "$MIN_COVERAGE_AGENT" || GATE_FAILED=1
check_coverage_threshold "internal/bootstrap" "$MIN_COVERAGE_BOOTSTRAP" || GATE_FAILED=1
check_coverage_threshold "internal/events" "$MIN_COVERAGE_EVENTS" || GATE_FAILED=1
check_coverage_threshold "internal/rotation" "$MIN_COVERAGE_ROTATION" || GATE_FAILED=1
check_coverage_threshold "pkg/security" "$MIN_COVERAGE_SECURITY" || GATE_FAILED=1
check_coverage_threshold "pkg/observability" "$MIN_COVERAGE_OBSERVABILITY" || GATE_FAILED=1

if [ $GATE_FAILED -ne 0 ]; then
    FAILED_STEPS+=("Coverage Gates")
fi

# 4. Security Validation
echo -e "\n${CYAN}▶ Phase 4: Security & Crypto Validation${NC}"
if go test -count=1 -v ./pkg/security/... ./pkg/vault/... -run "Security|Redact|Crypto" | grep -E "PASS|FAIL" | sed 's/^/  /'; then
    echo -e "${GREEN}  ✔ Security checks passed${NC}"
else
    echo -e "${RED}  ✘ Security checks failed${NC}"
    FAILED_STEPS+=("Security Validation")
fi

# 5. Integration Hardening
echo -e "\n${CYAN}▶ Phase 5: Integration Hardening${NC}"
if go test -count=1 -v ./test/integration/... | grep -E "PASS|FAIL" | sed 's/^/  /'; then
    echo -e "${GREEN}  ✔ Integration tests passed${NC}"
else
    echo -e "${RED}  ✘ Integration tests failed${NC}"
    FAILED_STEPS+=("Integration Tests")
fi

# FINAL SUMMARY
echo -e "\n${BLUE}====================================================${NC}"
if [ ${#FAILED_STEPS[@]} -eq 0 ]; then
    echo -e "${GREEN}✅ ALL VALIDATIONS PASSED SUCCESSFULLY${NC}"
else
    echo -e "${RED}❌ VALIDATION FAILED${NC}"
    echo -e "Failed components:"
    for step in "${FAILED_STEPS[@]}"; do
        echo -e "  - $step"
    done
    exit 1
fi
echo -e "${BLUE}====================================================${NC}"
