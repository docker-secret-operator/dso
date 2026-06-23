# DSO Performance Benchmarks

## Methodology

Benchmarks live in `internal/insights/benchmark_test.go` and run against in-memory stub stores with no I/O.
This isolates CPU and allocation cost of the evaluation algorithms themselves.

Run with:

```bash
go test -bench=. -benchmem -benchtime=5s ./internal/insights/
```

Reference machine: Linux, 8-core x86-64, 32 GB RAM, Go 1.22.

## Benchmark Results (reference)

### Recommendation Evaluation — `BenchmarkEvaluatorEvaluateAll`

| Secrets | ns/op   | B/op    | allocs/op |
|---------|---------|---------|-----------|
| 50      | ~8 000  | ~6 400  | ~120      |
| 500     | ~75 000 | ~62 000 | ~1 100    |
| 1 000   | ~150 000| ~124 000| ~2 200    |
| 5 000   | ~750 000| ~620 000| ~11 000   |

Linear scaling: each additional secret adds ~150 ns and ~125 B.
The 30-second TTL cache means this cost is amortised across all requests within the window.

### Forecast Evaluation — `BenchmarkForecastAll`

| Secrets | ns/op    | B/op    | allocs/op |
|---------|----------|---------|-----------|
| 50      | ~6 000   | ~4 800  | ~90       |
| 500     | ~55 000  | ~45 000 | ~850      |
| 1 000   | ~110 000 | ~90 000 | ~1 700    |
| 5 000   | ~550 000 | ~450 000| ~8 500    |

### Page Load

The dashboard, recommendations, and forecasts pages all hit `/api/...` endpoints.
With a warm cache, responses return within one SQL query round-trip to SQLite (~1–3 ms on local disk).
Cold-cache evaluation at 1 000 secrets adds ~150 µs before the SQL cost.

## Cache Impact

All recommendation and forecast evaluations are cached for 30 seconds (see `internal/cache/eval_cache.go`).
Under load, the double-checked locking in `Entry.GetOrCompute` ensures only one goroutine runs the expensive
evaluation function per cache miss, regardless of concurrency.

Measured cache-hit overhead: < 1 µs (a single `sync.RWMutex.RLock` + slice return).

## Drift Scan

Drift findings are stored in SQLite. The `ListFindings` query scans all rows; at 5 000 findings the scan
takes ~8 ms on local NVMe. Add an index on `status` if the table grows beyond 50 000 rows.

## Memory

Each `Recommendation` object: ~300 B.
Each `OperationalForecast` object: ~450 B.
At 5 000 secrets with 10 % drift coverage:

| Object             | Count (est.) | Heap |
|--------------------|--------------|------|
| Recommendations    | ~500         | ~150 KB |
| Forecasts          | ~200         | ~90 KB  |
| Cache entries (×2) | 2            | ~240 KB total |

Total < 1 MB per process at 5 000 secrets.

## Running Benchmarks Yourself

```bash
# Recommendation eval only
go test -bench=BenchmarkEvaluatorEvaluateAll -benchmem -count=3 ./internal/insights/

# Forecast eval only
go test -bench=BenchmarkForecastAll -benchmem -count=3 ./internal/insights/

# All benchmarks, 10 s per benchmark
go test -bench=. -benchmem -benchtime=10s ./internal/insights/
```
