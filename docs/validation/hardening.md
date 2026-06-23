# DSO Platform Hardening

This document describes the reliability measures added in P10.
The goal is *boring software*: predictable, survivable, and transparent when something goes wrong.

---

## Caching (P10 Phase 2)

**File:** `internal/cache/eval_cache.go`

A generic `Entry[T]` wraps any evaluation result with a 30-second TTL.

```
Entry[T]
  ├── value T          — last computed result
  ├── updatedAt        — wall-clock time of last compute
  ├── evalDur          — how long the last compute took
  └── valid bool       — false after Invalidate(), before first compute
```

`GetOrCompute(ctx, fn)` uses double-checked locking:

1. Fast path: `RLock` → return cached value if valid and not stale.
2. Slow path: `Lock` → re-check (another goroutine may have filled it while we waited) → call `fn` → store.

This ensures that under high concurrency (e.g., 50 simultaneous requests on a cold cache) only one goroutine
runs the expensive evaluation. All others wait and receive the freshly computed value.

**Invalidation triggers:**

| Event | Caches invalidated |
|---|---|
| Secret rotation (version created) | Recommendations + Forecasts |
| Drift finding created/updated/deleted | Recommendations + Forecasts |
| Policy created/updated/deleted | Recommendations only |

Code: `internal/server/rest.go` → `invalidateEvalCaches()`, called after each mutation.

---

## Latency Tracking (P10 Phase 3)

**File:** `internal/cache/latency.go`

`LatencyTracker` is a fixed-capacity circular buffer. `Record(d)` stores a sample. `P50()` and `P95()` sort a
snapshot of the buffer and return the percentile value. Capacity defaults to 256 samples.

`EvalMetrics` groups four trackers:

- `RecommendationEval`
- `ForecastEval`
- `DriftScan`
- `ComplianceEval`

These are not yet wired to a Prometheus exporter; `/api/status` exposes the most recent evaluation duration
instead (see Observability below).

---

## Concurrency Safety (P10 Phase 4)

Race detector: `go test -race ./internal/cache/...` passes clean.

Key invariants:

- `Entry[T].mu` guards all reads and writes to `value`, `updatedAt`, `evalDur`, `valid`.
- `EvalStatus.mu` guards all status fields.
- `LatencyTracker.mu` guards `samples`, `head`, `count`.
- Filter operations in handlers use `make([]T, 0, len(all))` (never `all[:0]`) so the cached slice's
  backing array is never aliased by the filtered result.
- Status recording (`EvalStatus.RecordRecommendation/RecordForecast`) is done **after** `GetOrCompute`
  returns, never inside the `fn` closure. `fn` is called while the write lock is held; calling any
  cache method from inside `fn` would deadlock.

Pre-existing bug fixed: `internal/api/audit_explorer_test.go` had 4 call sites for `buildAuditWhere` with
wrong argument count. Fixed before running the race detector to get a clean baseline.

---

## Failure Injection (P10 Phase 5)

**File:** `internal/api/degradation_test.go`

Tests verify that each component degrades gracefully rather than panicking:

| Scenario | Test | Expected behaviour |
|---|---|---|
| Nil version store | `TestEvaluator_NilStores` | Returns empty slice |
| Nil drift store | `TestEvaluator_NilStores` | Returns empty slice |
| Nil policy store | `TestEvaluator_NilStores` | Returns empty slice |
| Drift store returns error | `TestEvaluator_DriftStoreFailure` | No panic; partial/empty result |
| Forecaster with nil stores | `TestForecaster_NilStores` | Non-nil empty slice |
| Forecaster drift store error | `TestForecaster_DriftStoreFailure` | No panic |
| Empty secret list | `TestForecaster_EmptySecrets` | No rotation/compliance forecasts |
| Recommendation handler, no evaluator | `TestRecommendationHandler_NilEvaluator_ReturnsEmpty` | HTTP 200 via legacy engine |
| Recommendation handler, nil drift | `TestRecommendationHandler_WithEvaluator_NilDrift_NoError` | HTTP 200 |
| Forecast handler, no forecaster | `TestForecastHandler_NilForecaster_ReturnsFallback` | HTTP 200 via legacy engine |
| Forecast handler, nil drift | `TestForecastHandler_WithForecaster_NilDrift_NoError` | HTTP 200 |
| Compliance engine, nil inputs | `TestCompliance_SecretInput_NilSafe` | Non-nil result |

---

## Recovery Tests (P10 Phase 6)

**File:** `internal/api/degradation_test.go`

Tests verify that caches survive repeated invalidation cycles without corruption or panic:

| Test | Scenario |
|---|---|
| `TestRecommendationHandler_CacheSurvivesMultipleInvalidations` | 5× invalidate + re-query |
| `TestForecastHandler_CacheSurvivesMultipleInvalidations` | 5× invalidate + re-query |
| `TestEntry_ConcurrentGetOrCompute` (cache package) | 50 goroutines on stale cache → ≤5 evaluations |

Persistent state (secrets, versions, drift findings, policies) lives in SQLite and survives process restarts
by design. The evaluation caches are in-memory and are rebuilt on the first request after restart; the
30-second TTL means cold-start latency is bounded.

---

## Observability (P10 Phase 7)

**Endpoint:** `GET /api/status`

Returns the current evaluation staleness for recommendations and forecasts:

```json
{
  "last_recommendation_eval": "2026-06-23T10:00:00Z",
  "last_forecast_eval":       "2026-06-23T10:00:05Z",
  "recommendation_eval_ms":   42,
  "forecast_eval_ms":         18,
  "recommendation_count":     12,
  "forecast_count":           5,
  "cache_ttl_seconds":        30
}
```

If `last_recommendation_eval` is a zero timestamp, the cache has never been populated (e.g., no requests
have arrived since restart). This is not an error condition — it resolves on the first request.

The `/api/status` path is allowed through the auth middleware without authentication so that health checks
and monitoring scripts can poll it without credentials.

---

## Benchmark Results

See [PERFORMANCE.md](PERFORMANCE.md) for full benchmark results and methodology.

Summary at 1 000 secrets (warm cache):

| Operation | Cost |
|---|---|
| Recommendation eval | ~150 µs |
| Forecast eval | ~110 µs |
| Cache hit | < 1 µs |
| SQLite ListFindings (~1 000 rows) | ~3 ms |

---

## What Is Not Done

- Prometheus `/metrics` endpoint: latency trackers exist (`LatencyTracker`) but are not yet wired to a
  Prometheus registry. Add `prometheus/client_golang` and a `metrics.go` handler when an external scraper
  is available.
- Automated recovery tests that restart the actual binary: the current tests cover the in-process recovery
  of the cache layer. Full restart tests require an integration test harness with a real SQLite file.
