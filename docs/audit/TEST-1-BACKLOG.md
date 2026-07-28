# TEST-1: Convert Warning-Only ("Toothless") Tests into Enforcing Assertions

**Status**: Not started — backlog item, not a security fix  
**Priority**: Medium (test-suite reliability, not a production vulnerability itself)  
**Origin**: Discovered during SEC-4 — `TestIsSafePathSymlinkEscapeAttempt` created a real, working exploit of the SEC-4 vulnerability but only called `t.Logf()` on failure, never `t.Error()`/`t.Fatal()`. The test had been silently observing the bug, not catching it, for an unknown period before this audit.

---

## Problem

A test that logs a warning instead of asserting failure provides **negative value**: it looks like coverage exists (the scenario is exercised, the code path runs, the test file has a plausible name), but it can never turn a build red. This is worse than no test at all, because it creates false confidence — a reviewer skimming test names sees `TestIsSafePathSymlinkEscapeAttempt` and reasonably assumes the escape is blocked and enforced.

## Known Instances

| File | Test | Status |
|---|---|---|
| `pkg/config/config_test.go` | `TestIsSafePathSymlinkEscapeAttempt` | ✅ Fixed during SEC-4 (converted to `t.Fatal`) |
| `pkg/config/config_test.go` | `TestIsSafePathEmptyPaths` | ⚠️ Found during SEC-4, not fixed (out of scope) — logs `"error=%v, want error=%v"` via `t.Logf` instead of asserting. Flagged separately (see spawned follow-up task) |
| *(unknown — not yet searched)* | | A full-repo sweep has not been performed |

## Scope of This Backlog Item

1. **Search the full repository** for the anti-pattern: a test that computes an expected-vs-actual comparison but calls `t.Logf`/`t.Log` instead of `t.Error`/`t.Fatal`/`t.Errorf`/`t.Fatalf` when the comparison fails. A reasonable heuristic: grep for `t.Logf` calls inside a conditional block that also references a `want`/`expected`/`should` variable, or that follows an `if ... != ...` comparison.
2. **For each instance found**: determine whether the underlying code actually has a defect (as SEC-4's did) or whether the test's own expectation was simply wrong (e.g. `TestIsSafePathEmptyPaths`'s `wantErr: true` for `IsSafePath("", "")` may or may not reflect intended behavior — needs its own investigation, not an assumption).
3. **Fix the test to actually assert**, either by:
   - Correcting the production code if a real defect is confirmed (following the same audit discipline as SEC-4: reproduce, minimal fix, regression test), or
   - Correcting the test's expectation if the current behavior is intentional, with a comment explaining why.
4. Do **not** treat "found a toothless test" as automatic license to change production behavior — each instance needs the same verify-before-fix discipline as any other audit item.

## Why This Is a Separate Backlog Item, Not Folded Into Security Audits

Per the audit framework's own discipline: security fixes (SEC-N) should stay narrowly scoped to the specific vulnerability being addressed. Discovering an unrelated toothless test while fixing SEC-4 is exactly the kind of adjacent finding that belongs in a flagged follow-up, not an expansion of that fix's diff. Sweeping the whole repository for this anti-pattern is itself a nontrivial task deserving its own verify → fix → validate cycle, not a rider on a security commit.

## Acceptance Criteria

- [ ] Full-repo search performed and documented (list of files/tests found, not just the 2 known instances)
- [ ] Each instance individually triaged: real defect vs. wrong test expectation
- [ ] Every instance converted to a real assertion (`t.Error`/`t.Fatal`) with a passing test suite afterward
- [ ] No production code changed without independent verification that a defect actually exists (matching the discipline used for SEC-4)
- [ ] `go test ./...` and `go test -race ./...` pass after all conversions

## Estimated Effort

Small-medium (0.5–1.5 days), most of which is the initial repo-wide search and per-instance triage rather than the mechanical test-code changes.
