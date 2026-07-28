# START HERE: Audit Implementation Execution Guide

**Current Status**: Framework complete. Ready for execution.  
**Next Action**: Capture baseline metrics (Phase 0.5)  
**Estimated Time**: 2-4 hours

---

## The Big Picture (30 seconds)

An audit found 15 material issues in DSO. We've built a disciplined engineering process to fix them:

```
Verify (✅ done) → Measure (👈 you are here) → Implement → Validate → Document → Release
```

This document tells you exactly what to do next.

---

## Right Now: Execute Phase 0.5

### What is Phase 0.5?
Capture objective metrics before any changes. This is your baseline for measuring success.

### Why Now?
- You can't prove fixes work without a baseline
- Benchmarks mean nothing without "before" data
- Documentation is only useful with numbers

### What to Do

**Read this first** (5 min):
- `docs/audit/BASELINE_CAPTURE_CHECKLIST.md`

**Then execute** (1-2 hours):
1. Run each command in the checklist
2. Copy actual output into `docs/audit/2026-07-baseline.md`
3. **Replace every `[TBD]` and `[placeholder]` with real numbers**
4. Commit the baseline

**Example** (don't use placeholders):
```markdown
❌ WRONG:
- PASS: [count to be captured]

✅ RIGHT:
- PASS: 127
```

### Commands You'll Run
```bash
go test ./...
go test -race ./...
golangci-lint run ./...
gosec ./...
govulncheck ./...
go test ./... -cover
[and a few others]
```

All commands are listed in `BASELINE_CAPTURE_CHECKLIST.md`.

### After Baseline is Done
```bash
git add docs/audit/2026-07-baseline.md
git commit -m "docs(audit): Phase 0.5 baseline captured [2026-07-28]"
git tag baseline-2026-07-28
```

**Do not proceed to Phase 1 until baseline is committed.**

---

## Then: Execute Phase 1 Issues in Order

After baseline is captured, fix issues in this order:

### 1️⃣ BUG-1: Fix Race Condition (1-2 hours)
**File**: `internal/agent/agent.go` (lines 281-446)  
**Problem**: Tickers map accessed without synchronization → daemon crash risk  
**Deliverable**: 
- [ ] Code fix
- [ ] Race test (proves fix eliminates race)
- [ ] Regression test
- [ ] Docs updated
- [ ] Commit message includes `[BUG-1]`

**After merge**: Re-run `go test -race ./internal/agent/...` and verify 0 races

---

### 2️⃣ SEC-1: Wire Redaction Engine (1 day)
**File**: `pkg/observability/log.go` + production call sites  
**Problem**: Redaction engine exists but isn't wired; secrets appear in logs  
**Deliverable**:
- [ ] Redaction integrated into logger
- [ ] Regression test (secrets never leak)
- [ ] All `zap.Error()` calls go through redaction
- [ ] Documentation updated
- [ ] Commit message includes `[SEC-1]`

**After merge**: Verify SECURITY.md guarantee is now implemented

---

### 3️⃣ SEC-2: Mandatory Plugin Hash Verification (2 days)
**File**: `pkg/provider/load.go`  
**Problem**: Hash verification is optional (default: skip)  
**Deliverable**:
- [ ] Verification enforced by default
- [ ] Manifest required to load plugins
- [ ] Test: bad hash is rejected
- [ ] Test: missing manifest is rejected
- [ ] Commit message includes `[SEC-2]`

---

### 4️⃣ SEC-3: Harden Plugin Directory (1 day)
**File**: `Dockerfile`  
**Problem**: Plugin directory writable by daemon user  
**Deliverable**:
- [ ] Plugin directory root-owned, read-only
- [ ] Dockerfile updated
- [ ] Docker build verified
- [ ] Test: daemon cannot write to plugins dir
- [ ] Commit message includes `[SEC-3]`

---

### 5️⃣ PRODUCT-1: Dashboard Decision (⏳ Async, Don't Block)
**Scope**: `feature/web-ui` branch  
**Decision Needed**: Merge (2-4d work) or Archive (1h)?

**This is a product/stakeholder decision, not engineering.**  
Don't wait for this before starting Phase 2.

---

## Pause After BUG-1 (Important)

After BUG-1 is merged, **pause briefly** (15-30 min):

1. Did the workflow work smoothly?
2. Were estimates reasonable?
3. Did the process cause friction?
4. Any tooling/automation improvements?

If the process works, continue unchanged for SEC-1-3.  
If the process caused friction, make small adjustments.

**Do NOT redesign the framework.** Fix only broken things.

---

## Effort Tracking (Do This as You Go)

**Track actual time** for each issue:

1. Note start date/time
2. Note end date/time
3. Record in `docs/audit/EFFORT_TRACKING.md`
4. Compare to estimate

Example:
```markdown
| Issue | Estimated | Actual | Variance | Notes |
|-------|-----------|--------|----------|-------|
| BUG-1 | 1.5h | 2.5h | +67% | Race test required many runs |
```

After Phase 1, use this data to improve future estimates.

---

## Decision Recording (When You Make a Choice)

If you face a choice like:

- "Should we use mutex A or mutex B?"
- "Fail open or fail closed?"
- "Merge the branch or archive it?"

Record it in `docs/audit/DECISION_LOG.md` with:
- What you chose
- Why you chose it
- What trade-offs you accepted

Future maintainers will thank you.

---

## Validation Gates for Every Issue

Before merging, verify:

```bash
✅ go fmt ./...               # Code formatted
✅ go vet ./...               # No vet errors
✅ go test ./...              # All tests pass
✅ go test -race ./...        # Zero races
✅ golangci-lint run ./...    # No lint warnings
✅ gosec ./...                # No security issues
✅ Documentation updated      # README/SECURITY.md current
✅ Changelog entry added      # CHANGELOG.md updated
✅ Commit has issue ID        # Message includes [BUG-1] etc
```

**Nothing merges without all checkmarks.**

---

## Measuring Success (After Phase 1)

After BUG-1, SEC-1, SEC-2, SEC-3 are merged:

1. **Re-run baseline commands**:
   ```bash
   go test -race ./...           # Should still be 0 races
   go test ./...                 # Should still pass
   golangci-lint run ./...       # Should still be clean
   ```

2. **Compare to baseline**:
   - Coverage: Should increase (especially proxy layer from testing)
   - Performance: Should not regress
   - Test count: Should increase
   - Races: Should stay at 0

3. **Document results**:
   ```markdown
   # Phase 1 Results
   
   - Races eliminated: 1 (BUG-1)
   - Coverage improved: +[%] (from [x]% to [y]%)
   - Performance: No regression
   - Security issues fixed: 3 (SEC-1, SEC-2, SEC-3)
   ```

---

## The Roadmap (For Reference)

| Phase | Work | Duration | Gate |
|-------|------|----------|------|
| **0.5** | Baseline capture | 2-4h | ✅ Complete |
| **1a** | BUG-1 | 1-2h | Race test passes |
| **1b** | SEC-1 | 1d | Redaction wired |
| **1c** | SEC-2 | 2d | Hash enforced |
| **1d** | SEC-3 | 1d | Dir hardened |
| **1e** | PRODUCT-1 | Async | Stakeholder decision |
| **2-7** | Remaining phases | 2-3 weeks | Follow same pattern |

---

## FAQ

**Q: Do I have to follow this exact order?**  
A: For Phase 1 issues (BUG-1, SEC-1, SEC-2, SEC-3), yes. These are ship-blockers. PRODUCT-1 can happen async.

**Q: What if I find additional issues while implementing?**  
A: Document them in a ticket, but don't fix them in the same PR as the audit fix. Keep each PR focused.

**Q: What if an estimate is way off?**  
A: That's fine. Record the actual time. Estimates improve with data. No blame—just learning.

**Q: Can I skip the tests?**  
A: No. Tests are part of the definition of "done." Every fix includes unit + regression tests.

**Q: What if something breaks after merge?**  
A: Use the rollback plan documented in the fix. Git revert. Investigate. Try again.

**Q: When do we release?**  
A: After all phases complete, Phase 7 release readiness checklist. Not before.

---

## Success Looks Like

**After 1 week**:
- Baseline captured ✅
- BUG-1 merged (race eliminated) ✅
- Workflow validated (no major friction) ✅

**After 2 weeks**:
- SEC-1, SEC-2, SEC-3 merged ✅
- Coverage improved ✅
- Performance stable ✅

**After 4 weeks**:
- All phases complete ✅
- Release checklist passed ✅
- Production deployment approved ✅

---

## Last Thing Before You Start

Read the executive summary (`docs/audit/EXECUTIVE_SUMMARY.md`).  
It takes 15 minutes and gives you the complete picture.

Then: **Stop reading. Start executing.**

**The framework is solid. The ROI now is in implementation, not refinement.**

---

## Questions?

- **On the process**: `docs/audit/IMPLEMENTATION_RULES.md`
- **On specific issues**: `docs/audit/2026-07-PHASE_0_VERIFICATION.md`
- **On roadmap**: `docs/audit/EXECUTIVE_SUMMARY.md`
- **On baseline**: `docs/audit/BASELINE_CAPTURE_CHECKLIST.md`

All documents assume you're executing, not designing.

**Now go capture that baseline. 🚀**
