# SEC-1.1: Redaction Performance Optimization (Backlog)

**Status**: Not started — deliberately deferred from SEC-1  
**Priority**: Low (no correctness impact; documented, accepted trade-off)  
**Origin**: Measured during SEC-1's Phase 9/final review, not fixed there per explicit reviewer instruction to keep the security fix and performance optimization separate.

---

## Problem

Every log call now passes through `redactingCore`, adding measured overhead:

| Path | ns/op | B/op | allocs/op |
|---|---|---|---|
| No redaction (String field) | 759 | 128 | 1 |
| With redaction (String field) | 14,809 | 1,777 | 70 |
| With redaction (Reflect field) | 31,869 | 2,965 | 90 |

~19.5x slower, 70x more allocations for the common case.

## Root Cause (likely)

`security.RedactString`/`RedactError` run **11 regex patterns sequentially** via `pattern.ReplaceAllString()` — once per pattern, per string, per field, per log call. Each `ReplaceAllString` call does its own internal scan and allocates a result buffer, even when there's no match. With ~11 patterns applied to both the message and every field, this compounds quickly.

## Candidate Optimizations (NOT implemented — for future evaluation)

1. **Single combined regex**: Merge the 11 patterns into one alternation (`pattern1|pattern2|...`) and do a single pass with a replace-callback that redacts based on which alternative matched. Reduces N scans to 1, at the cost of a more complex callback and losing per-pattern replacement text customization (currently all patterns replace with the same `[REDACTED]`, so this loss is likely zero).
2. **Fast bail-out pre-check**: Before running the full pattern set, do one cheap substring/byte scan (e.g. `strings.ContainsAny` on lowercase keywords) to skip the expensive path entirely for fields/messages that can't possibly match. Risk: must stay in sync with the actual patterns or it becomes a silent bypass.
3. **Skip redaction for known-safe field types before allocating**: `redactFields` currently always allocates a new slice even when no field needs modification. A pre-scan that detects "nothing to redact" could return the original slice unchanged, avoiding the allocation for the common all-safe case (e.g., `zap.Int`, `zap.Duration` — already fast, but the wrapping slice allocation still happens redundantly).
4. **Cache compiled `RedactionPatterns` at the package level** instead of per-core (currently: harmless since it's already only constructed once per `newRedactingCore` call, not per log call — this item may be a non-issue, worth confirming during implementation rather than assuming).

## Why This Wasn't Fixed in SEC-1

Per explicit reviewer guidance: mixing a security fix with a performance optimization risks:
- Obscuring the security fix's diff with unrelated changes
- Introducing new correctness risk (a "faster" redaction path that redacts *less* would be a regression disguised as an optimization) into an already carefully-reviewed security control
- Blocking SEC-1's merge on a non-blocking concern (current cost is acceptable for DSO's actual log volume — rotation/error events, not a high-throughput request path)

## Acceptance Criteria (when this is picked up)

- [ ] Benchmark improvement demonstrated with the same `BenchmarkRedaction_StringField`/`BenchmarkRedaction_ReflectField` harness already in `pkg/observability/redaction_test.go`
- [ ] Full existing redaction test suite still passes unchanged (`TestRedaction_Matrix`, `TestRedaction_DoesNotOverRedactSecretIdentifierFields`, `TestRedaction_PreservesZapBehavior`, `TestRedactReflected_SafetyProperties`, `TestRedaction_StringerType`, `TestRedaction_KnownLimitations`) — proves the optimization doesn't change *what* gets redacted, only *how fast*
- [ ] Race detector clean
- [ ] Before/after benchmark numbers included in the commit message (matching the evidence-based discipline established for SEC-1)

## Estimated Effort

Small-medium (1-2 days) — the change is contained to `pkg/observability/redaction.go` and `pkg/security/redaction.go`; existing tests provide a strong regression safety net for correctness.
