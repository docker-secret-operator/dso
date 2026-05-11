# Phase 3 Test Validation Guide

## Quick Start

### Basic Test Run
```bash
bash test-phase3.sh
```

### With Coverage Report
```bash
bash test-phase3.sh --coverage
```

### With Race Detection
```bash
bash test-phase3.sh --race
```

### Verbose Output
```bash
bash test-phase3.sh --verbose
```

### All Options
```bash
bash test-phase3.sh --coverage --race --verbose
```

---

## Manual Test Commands

### Run All Phase 3 Tests
```bash
go test -v ./internal/cli -run "TestApply|TestInject|TestSync"
```

### Run Individual Command Tests

**Apply Command:**
```bash
go test -v ./internal/cli -run TestApply
```

**Inject Command:**
```bash
go test -v ./internal/cli -run TestInject
```

**Sync Command:**
```bash
go test -v ./internal/cli -run TestSync
```

### Run Specific Test
```bash
go test -v ./internal/cli -run TestNewApplyCmd
go test -v ./internal/cli -run TestApplyCmd_Flags
go test -v ./internal/cli -run TestFindContainerID
```

---

## Compilation Checks

### Build CLI Package
```bash
go build ./internal/cli
```

### Build Entire Project
```bash
go build ./...
```

### Check Syntax
```bash
go fmt ./internal/cli/apply.go
go fmt ./internal/cli/inject.go
go fmt ./internal/cli/sync.go
```

---

## Coverage Analysis

### Generate Coverage Profile
```bash
go test -coverprofile=coverage.out ./internal/cli
```

### View Coverage Report
```bash
go tool cover -html=coverage.out
```

### Check Coverage Percentage
```bash
go tool cover -func=coverage.out | grep total
```

### Coverage by Function
```bash
go test -coverprofile=apply_coverage.out ./internal/cli -run TestApply
go tool cover -func=apply_coverage.out
```

---

## Race Condition Detection

### Run Tests with Race Detector
```bash
go test -race ./internal/cli -run "TestNewApplyCmd|TestNewInjectCmd|TestNewSyncCmd"
```

### Full Race Test Suite
```bash
go test -race ./internal/cli -timeout 60s
```

---

## Test Count Summary

### Count Apply Tests
```bash
grep "^func Test" internal/cli/apply_test.go | wc -l
```

### Count Inject Tests
```bash
grep "^func Test" internal/cli/inject_test.go | wc -l
```

### Count Sync Tests
```bash
grep "^func Test" internal/cli/sync_test.go | wc -l
```

### Total Test Count
```bash
grep "^func Test" internal/cli/{apply,inject,sync}_test.go | wc -l
```

---

## Linting

### Format Code
```bash
go fmt ./internal/cli/apply.go
go fmt ./internal/cli/inject.go
go fmt ./internal/cli/sync.go
```

### Check for Unused Imports (requires goimports)
```bash
goimports -l internal/cli/apply.go
goimports -l internal/cli/inject.go
goimports -l internal/cli/sync.go
```

### Lint with golangci-lint (if installed)
```bash
golangci-lint run ./internal/cli/...
```

---

## Troubleshooting

### Test Timeout
If tests hang, use a timeout:
```bash
go test -timeout 30s ./internal/cli -run TestApply
```

### Verbose Output for Debugging
```bash
go test -v -x ./internal/cli -run TestApply
```

### Test Specific Package
```bash
go test -v github.com/docker-secret-operator/dso/internal/cli
```

### List All Available Tests
```bash
go test -list . ./internal/cli | grep "^Test"
```

---

## Expected Results

### Test Count
- Apply tests: 17
- Inject tests: 35
- Sync tests: 30
- **Total: 82 tests**

### Coverage Target
- Minimum: 70%
- Target: 80%+

### Pass Rate
- Expected: 100% pass rate
- All tests should pass before deployment

---

## Validation Checklist

- [ ] All tests compile without errors
- [ ] No syntax errors in any file
- [ ] All imports are correct and used
- [ ] Test count matches expected (82 total)
- [ ] All tests pass (100% pass rate)
- [ ] No race conditions detected
- [ ] Code coverage meets target (70%+)
- [ ] Code is properly formatted
- [ ] Functions exist and are properly named
- [ ] Stubs correctly removed
- [ ] Commands registered in root.go

---

## CI/CD Integration

### GitHub Actions Example
```yaml
- name: Run Phase 3 Tests
  run: bash test-phase3.sh --coverage --race

- name: Upload Coverage
  uses: codecov/codecov-action@v3
  with:
    files: ./coverage.out
```

### Pre-commit Hook
```bash
#!/bin/bash
# .git/hooks/pre-commit
bash test-phase3.sh --race
if [ $? -ne 0 ]; then
  echo "Tests failed - commit aborted"
  exit 1
fi
```

---

## Performance Benchmarks

### Run Benchmarks
```bash
go test -bench=. -benchmem ./internal/cli
```

### Compare Benchmarks
```bash
go test -bench=. -benchmem ./internal/cli > new.txt
# Compare with previous: benchstat old.txt new.txt
```

---

## Debug Mode

### Run Single Test with Debug Output
```bash
go test -v -run TestNewApplyCmd ./internal/cli
```

### Run with GDB (requires delve)
```bash
dlv test ./internal/cli -- -test.run TestApply
```

### See Full Test Output
```bash
go test -v ./internal/cli 2>&1 | less
```

---

## Common Issues & Solutions

### Issue: "go: not found"
**Solution:** Install Go from https://golang.org/dl

### Issue: Tests Timeout
**Solution:** Increase timeout with `-timeout 60s` flag

### Issue: Permission Denied
**Solution:** Make script executable: `chmod +x test-phase3.sh`

### Issue: Import Errors
**Solution:** Run `go mod tidy` to resolve dependencies

### Issue: Race Detector False Positives
**Solution:** These are usually safe but review the code to confirm

---

## Next Steps

After successful validation:

1. Run full test suite: `go test ./...`
2. Check overall coverage: `go test -cover ./...`
3. Run integration tests (if available)
4. Deploy to staging environment
5. Perform manual testing
6. Deploy to production

---

**Last Updated**: May 10, 2026
**Phase 3 Status**: Ready for Testing
**Expected Completion**: Same day validation
