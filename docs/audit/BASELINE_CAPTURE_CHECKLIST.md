# Phase 0.5: Baseline Capture — Execution Checklist

**Purpose**: Record objective metrics before any audit fixes. No placeholders. Real numbers only.

**Baseline Commit**: [TO BE FILLED: the exact git commit used for baseline]  
**Baseline Date**: [TO BE FILLED: ISO 8601 date/time when captured]  
**Captured By**: [Name]

---

## ⚠️ CRITICAL: No Placeholders Allowed

Do **NOT** commit this baseline with:
- `[count to be captured]`
- `[TBD]`
- `[size]`
- `[time]`
- Any other placeholder text

**Every field must contain actual measured data.**

If a measurement cannot be obtained (e.g., benchmark doesn't exist), document **why** instead of a placeholder:
```markdown
✓ Benchmark Missing: pkg/vault benchmark not implemented yet (tracked as TODO: #123)
```

---

## Execution Steps

Run these commands in order. Copy output directly into the baseline file.

### Step 1: Record Environment

```bash
go version
git describe --tags
git rev-parse HEAD
git branch
uname -a
```

**Record as**:
```markdown
## Environment

Go Version: go version go1.25.0 linux/amd64
Repository: v3.5.21
Commit: 0a3a2fd
Branch: main
System: Linux [exact output]
Captured: 2026-07-28T15:30:00Z
```

### Step 2: Test Results

```bash
go test ./... -v 2>&1 | tee test-output.log
```

**Count and record**:
```bash
grep -c "^ok" test-output.log        # PASS count
grep -c "^FAIL" test-output.log      # FAIL count
grep -c "^skip" test-output.log      # SKIP count
grep "total time" test-output.log    # Duration
```

**Record as**:
```markdown
### Unit Tests

Command: `go test ./...`

Results:
- PASS: 127
- FAIL: 0
- SKIP: 3
- Total Duration: 45.2s
- Status: ✅ All pass
```

### Step 3: Race Detection

```bash
go test -race ./... -v 2>&1 | tee race-output.log
```

**Record**:
```bash
grep -c "^ok" race-output.log        # Should match unit test count
grep "WARNING: DATA RACE" race-output.log | wc -l  # Race count
```

**Record as**:
```markdown
### Race Testing

Command: `go test -race ./...`

Results:
- PASS: 127
- FAIL: 0
- Races Detected: 0
- Duration: 2m 15s
- Status: ✅ No races found
```

### Step 4: Coverage per Package

```bash
go test ./... -cover 2>&1 | grep "^ok" | awk '{print $2, $5}'
```

**Record the full table**:
```markdown
### Coverage by Package

| Package | Coverage | Status |
|---------|----------|--------|
| internal/agent | 34.1% | ✓ |
| internal/events | 78.5% | ✓ |
| internal/polling | 100% | ✓ |
| internal/proxy | 5.9% | ⚠️ CRITICAL |
| internal/rotation | 48.1% | ✓ |
| internal/watcher | 17.6% | ⚠️ LOW |
| pkg/vault | 88.0% | ✓ |
| pkg/security | 88.1% | ✓ |
| pkg/provider | [actual %] | [status] |
| pkg/observability | [actual %] | [status] |

**Overall Coverage**: [actual %]
**Change from Last Release**: [if known]
```

### Step 5: Linter Results

```bash
golangci-lint run ./... 2>&1 | tee lint-output.log
```

**Count warnings**:
```bash
wc -l lint-output.log                # Total lines (warnings)
grep "errcheck" lint-output.log | wc -l
grep "staticcheck" lint-output.log | wc -l
grep "unused" lint-output.log | wc -l
```

**Record as**:
```markdown
### Linter (golangci-lint)

Command: `golangci-lint run ./...`

Results:
- Total Warnings: 0
- By Category:
  - errcheck: 0
  - staticcheck: 0
  - unused: 0
  - govet: 0
  - other: 0
- Status: ✅ Clean
```

### Step 6: Security Scan

```bash
gosec ./... 2>&1 | tee gosec-output.log
```

**Record**:
```markdown
### Security Scan (gosec)

Command: `gosec ./...`

Results:
- Issues Found: 0
- By Severity:
  - HIGH: 0
  - MEDIUM: 0
  - LOW: 0
- Status: ✅ Clean
```

### Step 7: Vulnerability Check

```bash
govulncheck ./... 2>&1 | tee govulncheck-output.log
```

**Record**:
```markdown
### Vulnerability Check (govulncheck)

Command: `govulncheck ./...`

Results:
- Total Vulnerabilities: 1
- Known (Pre-audit):
  - GO-2026-5746 (docker/docker v28.5.2) [acknowledged in CI]
- New Vulnerabilities: 0
- Status: ✅ Expected findings only
```

### Step 8: Binary Sizes

```bash
make build  # or: go build -o docker-dso ./cmd/dso
ls -lh docker-dso
ls -lh cmd/plugins/*/
```

**Record as**:
```markdown
### Binary Sizes

| Binary | Size | Stripped | Status |
|--------|------|----------|--------|
| docker-dso | 24.5 MB | 14.2 MB | ✓ |
| dso-provider-vault | 18.3 MB | 11.1 MB | ✓ |
| dso-provider-aws | 19.1 MB | 11.8 MB | ✓ |
| dso-provider-azure | 20.2 MB | 12.4 MB | ✓ |
| dso-provider-huawei | 19.8 MB | 12.1 MB | ✓ |

Build Command: `make build`
Build Time: 2m 30s
```

### Step 9: Performance Benchmarks

```bash
go test -bench=. -benchmem ./internal/polling/ 2>&1 | grep "Benchmark"
go test -bench=. -benchmem ./pkg/vault/ 2>&1 | grep "Benchmark"
```

**Record actual numbers**:
```markdown
### Performance Benchmarks

#### internal/polling.SmartPoller
```
BenchmarkCalculateInterval-8        2000000    500 ns/op    128 B/op    2 allocs/op
BenchmarkRecordChange-8             1000000    950 ns/op    256 B/op    4 allocs/op
BenchmarkRecordPoll-8               2000000    620 ns/op    96 B/op     1 allocs/op
```

#### pkg/vault (Encryption)
```
BenchmarkEncrypt-8                  100000    12500 ns/op   2048 B/op   18 allocs/op
BenchmarkDecrypt-8                  100000    13200 ns/op   1024 B/op   16 allocs/op
```
```

### Step 10: Git History

```bash
git log --oneline -20
git log --format="%h %s" --grep="fix\|security\|race" -20
git branch -v
```

**Record**:
```markdown
### Git Baseline

Last 20 Commits:
[actual output]

Recent Fixes:
[actual output]

Branch Status:
[actual output]
```

---

## Baseline Completion Checklist

- [ ] Environment captured (Go version, commit, date)
- [ ] Unit tests run and counted (PASS/FAIL/SKIP)
- [ ] Race tests run (0 races recorded)
- [ ] Coverage measured per package (no TBD)
- [ ] Linter clean or warnings counted
- [ ] Security scan results recorded
- [ ] Vulnerability check results recorded
- [ ] Binary sizes measured
- [ ] Performance benchmarks captured
- [ ] Git history recorded
- [ ] **All placeholders replaced with real numbers**
- [ ] File saved to `docs/audit/2026-07-baseline.md`
- [ ] Baseline committed: `git add docs/audit/2026-07-baseline.md && git commit -m "docs(audit): Phase 0.5 baseline captured [2026-07-28]"`
- [ ] Baseline commit SHA recorded above
- [ ] Baseline tagged (optional): `git tag baseline-2026-07-28`

---

## Validation

Before moving to Phase 1, verify baseline is complete:

```bash
# Check baseline file exists and has no placeholders
grep -E "\[.*to be.*\]|\[TBD\]" docs/audit/2026-07-baseline.md
# Should return: (no matches)

# Verify all test sections have numbers
grep -E "^- (PASS|FAIL|SKIP|Races|Warnings|Issues):" docs/audit/2026-07-baseline.md
# Should return: multiple matches with actual counts

# Confirm commit is recorded
git log --oneline -1
# Should show the baseline commit
```

---

## Why This Matters

The baseline is your **objective proof** that you started from a known state. During implementation:

- **BUG-1 fix**: Re-run `go test -race ./...` → compare to baseline
- **SEC-1 fix**: Re-measure coverage → compare to baseline
- **PERF fixes**: Re-run benchmarks → compare to baseline

Without baseline numbers, you have only subjective claims ("we think we improved"). With baseline, you have **measurable evidence**.

---

## Next Step

Once baseline is complete and committed:

1. Tag the baseline commit: `git tag baseline-2026-07-28`
2. Record the commit SHA in this document
3. Proceed to **Phase 1: BUG-1 Implementation**
4. Keep this baseline file for reference during all phases

**Do not proceed to Phase 1 until baseline is complete and committed with real numbers.**
