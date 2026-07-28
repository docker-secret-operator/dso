# Effort Tracking & Analysis

**Purpose**: Track actual effort vs estimated effort for all audit fixes.  
Better planning comes from comparing estimates to reality.

---

## Phase 1: Critical Fixes

| Issue | Type | Estimated | Actual | Notes | Status |
|-------|------|-----------|--------|-------|--------|
| BUG-1 | Race Fix | 1-2h | 1.5h | Tickers map synchronization | ✅ Complete |
| SEC-1 | Logging | 1d | ~1.5d | Redaction engine wiring across 2 commits: (1) initial wiring, found and reverted an over-redaction design flaw (key-based matching); (2) final external review round found 2 more genuine defects — sampling silently bypassed (Check() only tested Enabled(), never delegated to wrapped core), and 2 field types (Binary, ArrayMarshaler) unhandled. Neither would have surfaced without a checklist requiring concrete reproduction, not just code reading | ✅ Complete |
| SEC-2 | Security | 2d | [pending] | Plugin hash enforcement | ⏳ Not started |
| SEC-3 | Security | 1d | [pending] | Plugin directory hardening | ⏳ Not started |
| PRODUCT-1 | Product | Async | [pending] | Dashboard merge/archive decision | ⏳ Not started |

---

## How to Record Actual Effort

When starting an issue:
```bash
# At START of work:
git checkout -b fix/BUG-1
date +"%Y-%m-%d %H:%M:%S" > /tmp/bug1-start.txt

# At END of work (before merge):
date +"%Y-%m-%d %H:%M:%S" > /tmp/bug1-end.txt

# Calculate duration (or record manually)
cat /tmp/bug1-start.txt /tmp/bug1-end.txt
```

Update the table above with:
- **Date started**: YYYY-MM-DD
- **Date completed**: YYYY-MM-DD
- **Calendar time**: days from start to merge
- **Actual dev time**: estimated hours spent coding/testing/review
- **Notes**: what took longer or shorter than expected
- **Variance**: (Actual - Estimated) / Estimated × 100%

---

## Example Entries (Hypothetical)

| Issue | Estimated | Actual | Variance | Notes |
|-------|-----------|--------|----------|-------|
| BUG-1 | 1.5h | 2.5h | +67% | Race test was tricky; needed `-race -count=100` to reproduce consistently |
| SEC-1 | 1d (8h) | 1.5d (12h) | +50% | Broader than expected; had to refactor 4 call sites, not 2 |
| SEC-2 | 2d (16h) | 1.5d (12h) | -25% | Simpler than expected; manifest loading was already partially there |

---

## After Phase 1: Adjust Future Estimates

Once you have actual data from 4-5 fixes, use it to improve planning:

```
Average variance: [calculate from table]
If positive: Future estimates should be 20-30% higher
If negative: Future estimates can be tighter
```

Example:
```
Phase 1 average variance: +25%
→ Estimated 2d fix will likely take 2.5d
→ Estimated 1-2h fix will likely take 1.25-2.5h
```

---

## Per-Issue Effort Breakdown (Optional)

For larger issues, track time allocation:

```markdown
## BUG-1 Effort Breakdown

Total: 2.5h

- Code understanding: 30m
- Root cause analysis: 30m
- Implementation: 45m
- Unit testing: 15m
- Race testing: 15m
- Review/feedback: 30m
- Docs/changelog: 15m
```

This helps identify where issues consume unexpected time (e.g., "race testing" is bottleneck).

---

## Reporting

**After Phase 1**, share findings with team:

```markdown
# Phase 1 Effort Analysis

**Total Planned**: 1 week
**Total Actual**: 1.2 weeks (+20% variance)

**Breakdown**:
- BUG-1: +67% (race testing complex)
- SEC-1: +50% (broader scope)
- SEC-2: -25% (simpler than expected)
- SEC-3: -10% (straightforward)
- PRODUCT-1: N/A (async decision)

**Learning for Phase 2**:
- Concurrency fixes need +50% time buffer for race testing
- Security refactors require broader scope estimates
- Simple fixes (SEC-3) remain predictable

**Next Sprint Estimates** (adjusted):
- 1d fix → estimate 1.25d
- 2d fix → estimate 2.5d
```

---

## This is About Learning, Not Blame

Effort tracking isn't "why did you take longer?" It's "how do we estimate better next time?"

- Variance > estimate often means: scope was underestimated, not developer was slow
- Variance < estimate often means: developer had domain knowledge, or fix was simpler than expected

Both are valuable for improving planning accuracy.
