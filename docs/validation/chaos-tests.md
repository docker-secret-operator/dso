# DSO Chaos Test Results

Chaos tests live in two files:
- `internal/api/degradation_test.go` — P10 baseline degradation
- `internal/api/chaos_test.go` — P11 extended chaos scenarios

All tests pass with `go test -race ./internal/api/`.

---

## Failure Scenarios Tested

### Nil Stores (complete dependency absence)

| Test | Store removed | Behaviour |
|---|---|---|
| `TestEvaluator_NilStores` | version, drift, policy | Returns empty `[]`, no panic |
| `TestForecaster_NilStores` | version, drift, compliance | Returns empty `[]`, no panic |
| `TestRecommendationHandler_NilEvaluator_ReturnsEmpty` | evaluator | Falls back to legacy engine |
| `TestForecastHandler_NilForecaster_ReturnsFallback` | forecaster | Falls back to legacy engine |

### Error-Returning Stores

| Test | Error | Behaviour |
|---|---|---|
| `TestEvaluator_DriftStoreFailure` | `fmt.Errorf("store unavailable")` | Empty result, no panic |
| `TestForecaster_DriftStoreFailure` | Same | Empty result, no panic |
| `TestRecommendationHandler_StorageError_HTTP200` | `ECONNREFUSED` | HTTP 200, empty recommendations |

The evaluators and forecasters treat store errors as "no data" — they degrade to empty results rather
than propagating errors as HTTP 500s. This is a deliberate design choice: a failed drift store should
not make the recommendations endpoint unavailable.

### Slow Dependencies

| Test | Delay | Behaviour |
|---|---|---|
| `TestRecommendationHandler_SlowDrift_NoPanic` | 5 ms | HTTP 200, respects context |
| `TestForecastHandler_SlowDrift_NoPanic` | 5 ms | HTTP 200, respects context |

The `slowDriftStore` respects `ctx.Done()`, so if the request context is cancelled (e.g., client
disconnects), the slow store returns immediately with a context error.

### Intermittent Failures

| Test | Pattern | Behaviour |
|---|---|---|
| `TestRecommendationHandler_FlakeDrift_ConsistentResponses` | Even calls succeed, odd fail | Always HTTP 200 across 10 calls |

Even when half of all drift store calls fail, the endpoint always returns HTTP 200 (with potentially
fewer recommendations due to missing drift context).

### Concurrent Chaos

| Test | Goroutines | Behaviour |
|---|---|---|
| `TestRecommendationHandler_ConcurrentInvalidateAndRead` | 30 writer + 30 reader goroutines | No races, always HTTP 200 |
| `TestForecastHandler_ConcurrentInvalidateAndRead` | 30 writer + 30 reader goroutines | No races, always HTTP 200 |

Verified with `-race`. The `Entry[T].GetOrCompute` double-checked locking prevents both races and
duplicate evaluations.

---

## What Was Not Chaos-Tested

1. **SQLite unavailable at binary startup.** The binary fails fast at startup if SQLite cannot be
   opened. This is correct behaviour — the platform cannot serve requests without its primary store.

2. **Partial rotation (rotation fails mid-write).** SQLite transactions ensure atomicity. A partial
   rotation leaves the database in the pre-rotation state.

3. **Configuration rollback.** The config editor (`internal/api/config_editor_handler.go`) uses
   optimistic concurrency. Concurrent edits are rejected; rollback is not automated.

4. **Provider failures during rotation.** Vault/K8s provider failures are tested in `pkg/provider/`
   and are not duplicated here.

---

## Deadlock Found and Fixed (P10)

During P10 development, a deadlock was introduced and found during testing:

**Root cause:** `GetOrCompute` holds the write lock while calling `fn`. The `fn` closure called
`h.recCache.LastEvalDuration()`, which tries to acquire the read lock. Since a write lock
is already held by the same goroutine (Go's `sync.RWMutex` is not reentrant), this blocks forever.

**Symptom:** `TestRecommendationHandler_WithEvaluator_NilDrift_NoError` hung for 10 minutes
(default test timeout) and then panicked.

**Fix:** Moved `status.RecordRecommendation/RecordForecast` outside the `fn` closure.

```go
// Before (deadlock):
all = h.recCache.GetOrCompute(ctx, func(ctx context.Context) []*recommendation.Recommendation {
    result := h.evaluator.EvaluateAll(ctx, inputs)
    h.status.RecordRecommendation(h.recCache.LastEvalDuration(), len(result)) // ← deadlock
    return result
})

// After (correct):
all = h.recCache.GetOrCompute(ctx, func(ctx context.Context) []*recommendation.Recommendation {
    return h.evaluator.EvaluateAll(ctx, inputs)
})
if h.status != nil {
    h.status.RecordRecommendation(h.recCache.LastEvalDuration(), len(all)) // ← outside lock
}
```

This was not caught by the race detector (it's a deadlock, not a data race). It was caught by the
test timeout. The fix is in `recommendation_handler.go:136` and `forecast_handler.go:143`.

---

## Running Chaos Tests

```bash
# All chaos and degradation tests
go test -race -timeout 60s ./internal/api/ -run "Chaos|Slow|Flake|Storage|Concurrent|RBAC|NoSecret|EvalStatus|Repeated|Filter|Nil|Degradation"

# All insights failure tests
go test -race -timeout 60s ./internal/insights/ -run "TestEvaluator|TestForecaster"

# Full API package with race
go test -race -timeout 120s ./internal/api/
```
