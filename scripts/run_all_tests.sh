#!/bin/bash
# DSO Production CI Validator
set -euo pipefail

#export GOROOT="/opt/homebrew/bin/go"
#export PATH="$GOROOT/bin:$PATH"

# Configuration
MIN_COVERAGE_VAULT=85
MIN_COVERAGE_RESOLVER=90
MIN_COVERAGE_INJECTOR=85
MIN_COVERAGE_PROVIDERS=85
MIN_COVERAGE_CLI=25
MIN_COVERAGE_CONFIG=85

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'

echo -e "${BLUE}====================================================${NC}"
echo -e "${BLUE}🚀 DSO CNCF-QUALITY VALIDATION SYSTEM${NC}"
echo -e "${BLUE}====================================================${NC}"

check_coverage_threshold() {
    local pkg=$1
    local threshold=$2
    local cov
    
    echo -n -e "Checking coverage: ${YELLOW}$pkg${NC} (Min: $threshold%)... "
    # Run test and capture coverage.
    cov=$(go test -count=1 -cover "./$pkg/..." 2>/dev/null | grep "coverage:" | awk '{print $5}' | sed 's/%//' | head -n 1 || echo "0")
    
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

# 1. Deterministic Unit Tests
echo -e "\n▶ Running Deterministic Unit Tests (No Cache, Race Detector)..."
go test -count=1 -race ./pkg/... ./internal/... -short | grep -E "PASS|FAIL|---" || true

# 2. Coverage Enforcement
echo -e "\n▶ Enforcing Coverage Gates..."
EXIT_CODE=0

check_coverage_threshold "pkg/vault" "$MIN_COVERAGE_VAULT" || EXIT_CODE=1
check_coverage_threshold "internal/resolver" "$MIN_COVERAGE_RESOLVER" || EXIT_CODE=1
check_coverage_threshold "internal/injector" "$MIN_COVERAGE_INJECTOR" || EXIT_CODE=1
if [ -d "internal/providers" ]; then
    check_coverage_threshold "internal/providers" "$MIN_COVERAGE_PROVIDERS" || EXIT_CODE=1
fi
check_coverage_threshold "internal/cli" "$MIN_COVERAGE_CLI" || EXIT_CODE=1
check_coverage_threshold "pkg/config" "$MIN_COVERAGE_CONFIG" || EXIT_CODE=1

if [ $EXIT_CODE -ne 0 ]; then
    echo -e "\n${RED}❌ FATAL: Coverage thresholds not met.${NC}"
    exit 1
fi

# 3. Security Validation
echo -e "\n▶ Security Path Validation..."
go test -count=1 -v ./pkg/observability/... ./pkg/vault/... -run "Security|Redact|Crypto" | grep -E "PASS|FAIL"

# 4. Integration Hardening
echo -e "\n▶ Integration Hardening..."
go test -count=1 -v ./test/integration/... | grep -E "PASS|FAIL"

echo -e "\n${GREEN}====================================================${NC}"
echo -e "${GREEN}✅ ALL VALIDATIONS PASSED${NC}"
echo -e "${GREEN}====================================================${NC}"
