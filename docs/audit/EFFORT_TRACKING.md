# Effort Tracking & Analysis

**Purpose**: Track actual effort vs estimated effort for all audit fixes.  
Better planning comes from comparing estimates to reality.

---

## Phase 1: Critical Fixes

| Issue | Type | Estimated | Actual | Notes | Status |
|-------|------|-----------|--------|-------|--------|
| BUG-1 | Race Fix | 1-2h | 1.5h | Tickers map synchronization | ✅ Complete |
| SEC-1 | Logging | 1d | ~2d | Redaction engine wiring across 3 commits: (1) initial wiring, found and reverted an over-redaction design flaw (key-based matching); (2) review round 2 found sampling silently bypassed and 2 field types (Binary, ArrayMarshaler) unhandled; (3) review round 3 (mandatory exhaustive field-type audit) found StringerType unhandled and exploitable via zap.Any(), contradicting an already-shipped SECURITY.md claim. 3 review rounds each found a genuinely different defect class — none would have surfaced without a checklist requiring concrete reproduction over code reading. Original 1-day estimate assumed "wire an existing engine in" was simple; the real work was closing gaps the wiring itself exposed | ✅ Complete |
| SEC-2 | Security | 2d | ~1d | Plugin hash enforcement. Self-review found no defects (2 files: load.go, provider_plugins.go). An independent final review then found and fixed one severe defect the self-review missed: the Dockerfile (the primary deployment path) never generates a hash manifest, so every container deployment would have failed to load any plugin. Verified via an actual docker build + hash cross-check, not just code reading. Lesson: check every distribution path (installer AND Docker image), not just the one touched first | ✅ Complete |
| SEC-3 | Security | 1d | ~0.5d | Plugin directory hardening. Single, focused Dockerfile change (one `chown -R` line removed). Empirically verified the threat (dso-user could delete+recreate both plugin binaries and the SEC-2 manifest) and the fix (all three attack attempts, including a self-chmod bypass, now fail) via real `docker build`/`docker run` tests, not just permission-bit inspection. Single review round found no new defects | ✅ Complete |
| SEC-4 | Security | Low priority (deferred) | ~0.5d | Path validation symlink resolution. Found an already-existing test in this codebase (`TestIsSafePathSymlinkEscapeAttempt`) that had silently observed the exact gap for some time — it created a real escaping symlink but only `t.Logf`'d a warning instead of asserting failure, so it never blocked CI. Fix scoped narrowly to the one caller with a genuine containment boundary (`FileProvider`), not the 5 CLI-invoked callers where an operator following their own symlink isn't a privilege escalation. Single review round found no new defects | ✅ Complete |
| QUALITY-1 | Quality | High (5.9% coverage on security-sensitive path) | ~0.5d | Proxy test coverage: 5.9% → 86.6%. `registry.go`/`router.go` (the audit's named priority) reached 100%. Pure test-only addition, zero production code changed. While reading previously-untested code, found and flagged (not fixed, correctly out of scope) two pre-existing issues: BUG-4's known goroutine-leak in `SwapBackend`, and a new gosec-flagged integer overflow in `Router.Next()` (practically unreachable at ~9.2 quintillion requests, but a legitimate cheap fix for later). Single review round found no new production-impacting defects | ✅ Complete |
| BUG-4 | Bug Fix | 1-2d | ~0.5d | `Manager.SwapBackend` drain-goroutine lifecycle. Simpler than estimated: `context.Context` + `sync.WaitGroup` is a well-worn Go pattern, no new infrastructure needed. Reproduced the leak empirically (stashed the fix, confirmed the new test fails against pre-fix code) before restoring it — same reproduce-before-fix discipline as SEC-1/SEC-2. Single review round found no new defects | ✅ Complete |
| AUDIT-1 | Feature (wiring) | 1-2d | ~0.5d | Wired `internal/audit.Log` into `AgentServer.GetSecret`, the single choke point every secret fetch passes through. Faster than estimated because the logging engine itself was already fully built and tested (SEC-1) — this was purely call-site wiring plus 2 new tests, not new engine work. Deliberately scoped to one call site (secret fetch); rotation-event auditing and true per-caller (peer-UID) attribution both documented as separate follow-ups rather than bundled in. Single review round found no new defects | ✅ Complete |
| CLEAN-1 | Cleanup | <0.5d | ~0.5d | Deleted confirmed-dead `internal/runtime` and `internal/providers/plugin_verifier.go`. Roughly on estimate for the two deletions themselves, but the `internal/webui` half of the request took longer than a simple "delete it" would have, because the user correctly asked to verify its relationship to `feature/web-ui` first — that investigation (git log/merge-base/gitignore checks) revealed it was untracked, gitignored working-tree cruft unrelated to the real (still-unmerged) web UI branch, not a quick judgment call. Single review round found no new defects | ✅ Complete |
| SEC-5 | Security (CI) | 0.5-1d ("flip a flag") | ~2d | Making `gosec`/`govulncheck` blocking. Substantially larger than the original estimate once the real scope became visible: 84 pre-existing `gosec` findings and 22 `govulncheck` vulnerabilities across the whole repo, not the handful expected from prior per-package spot checks. Most of the time went to individually verifying each finding's actual call chain (confirming SEC-2/SEC-4 protections already covered several `G304`s, confirming two `G704` SSRF findings were Unix-socket dials structurally immune to SSRF, etc.) rather than blanket-suppressing categories, plus a Go toolchain + 4-dependency bump (with full `-race` test verification) to close 19 of 22 `govulncheck` findings for real instead of just tolerating them. An unplanned but valuable side-discovery: CI's pinned `gosec@v2.25.0` caught 5 findings a newer local `@latest` install didn't, which would have shipped a false "all clear" had the exact CI version not been checked. Single review round found no new defects | ✅ Complete |
| AUDIT-2 | Security (follow-up) | 1-2d | ~0.5d | Threading real peer identity into the audit log, closing AUDIT-1's deliberate scope-out. Faster than estimated because the right pattern (`net/rpc`'s own documented per-connection-server idiom) was clear once considered — no custom codec or transport rewrite needed, just a small wrapper type embedding the shared `*AgentServer`. Reproduce-before-fix discipline applied: stashed the fix, confirmed the new end-to-end test (real socket, real `net/rpc` call) fails against pre-fix code, restored it. Single review round found no new defects | ✅ Complete |
| AUDIT-3 | Security (follow-up) | 1-2d | ~0.5d | Extending audit logging to rotation events, closing AUDIT-1's other deliberate scope-out. On estimate: the choke point (TriggerReload's rolling-strategy goroutine) was exactly where the comprehensive review predicted, and the existing `newMockController` Docker-mock test pattern in this package meant no new test infrastructure was needed. Unplanned but valuable: `-race` caught a genuine data race in the new test's own audit-buffer polling helper (fixed with a small mutex-guarded buffer type) — a good reminder that test infrastructure needs the same scrutiny as production code. Reproduce-before-fix discipline applied throughout. Single review round found no new defects | ✅ Complete |
| CLEAN-3 | Cleanup (CI config) | <0.5d | ~0.5d | Excluding vendored `web/node_modules` from `golangci-lint`/`gosec`. On estimate for a config-only change, but the estimate assumed the first config syntax tried would just work — golangci-lint v2's schema moved away from the `run.exclude-dirs`/`issues.exclude-dirs` names most documentation still references, so two candidate configs were tried and empirically disproved (verified against the exact pinned CI binary) before finding `linters.exclusions.paths` actually took effect. Single review round found no new defects | ✅ Complete |
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
