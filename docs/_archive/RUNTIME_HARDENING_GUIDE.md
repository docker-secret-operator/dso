# Runtime Hardening & Production Stability Guide

This guide documents the comprehensive runtime hardening and production validation implementations for the Docker Secret Operator (DSO), covering: event backpressure, provider supervision, event deduplication, logging audit, and resource stability validation.

## Part 1: Overview

DSO has been hardened across four critical areas:

1. **Event Backpressure** (`internal/events/backpressure.go`) - Prevent memory exhaustion under container churn
2. **Provider Supervision** (`internal/providers/supervisor.go`) - Watchdog monitoring, crash detection, restart policy
3. **Event Deduplication** (`internal/events/deduplication.go`) - Prevent replay amplification during daemon reconnects
4. **Logging Audit** (`pkg/security/logging_audit.go`) - Runtime validation that secrets never appear in logs
5. **Resource Stability** (`internal/testing/resource_stability_test.go`) - 24h+ validation of memory, goroutine, queue stability

## Part 2: Event Backpressure Integration

### Overview
Bounded event queue with configurable worker pool prevents memory exhaustion when container events exceed processing capacity.

### Implementation Location
- Core: `internal/events/backpressure.go`
- Tests: `internal/events/backpressure_test.go`

### Integration Points

#### In `internal/agent/agent.go`:
```go
// Initialize bounded event queue (1000 max events, 16 workers)
a.eventQueue = eventqueue.NewBoundedEventQueue(a.logger, 1000, 16, a.handleEventWithContext)
a.eventQueue.Start(ctx)
defer a.eventQueue.Stop()

// Enqueue events with backpressure protection
if !a.eventQueue.Enqueue(msg) {
    log.Printf("⚠️ Event queue full, dropping event: %s/%s", msg.Actor.ID[:12], msg.Action)
}
```

#### In `internal/watcher/controller.go`:
```go
// Initialize BoundedEventQueue in StartEventLoop()
a.eventQueue = eventqueue.NewBoundedEventQueue(a.logger, 2000, 32, a.handleContainerEventWithContext)
a.eventQueue.Start(ctx)
defer a.eventQueue.Stop()
```

### Configuration

```go
type BoundedEventQueue struct {
    maxEvents    int           // Default: 1000, adjust based on peak churn
    workerCount  int           // Default: 16, increase for high concurrency
    timeout      time.Duration // Default: 30s per event
}
```

#### Tuning Guidance:
- **maxEvents**: Set to 1.5x your expected peak event rate. For 10K events/min, use ~2000
- **workerCount**: Set to 2x number of CPU cores. For 4-core systems, use 8-16
- **timeout**: Default 30s sufficient for most operations; increase only for very slow providers

### Monitoring
- Metric: `dso_event_queue_depth` - Queue depth in real-time
- Metric: `dso_events_dropped_total` - Count of dropped events (alert if > 0)
- Metric: `dso_worker_utilization_percent` - Worker pool utilization (alert if > 85%)

### Production Checklist
- [ ] Verify `maxEvents` is sized for expected peak load
- [ ] Verify `workerCount` matches infrastructure (CPU cores)
- [ ] Enable alerting on `dso_events_dropped_total` > 0
- [ ] Enable alerting on `dso_worker_utilization_percent` > 85%
- [ ] Test with `internal/runtime/benchmarks_test.go:TestQueueSaturation`

---

## Part 3: Provider Supervision Integration

### Overview
Crash detection, health monitoring, restart policy with exponential backoff + jitter, and consecutive failure tracking to prevent cascade failures.

### Implementation Location
- Core: `internal/providers/supervisor.go`
- Tests: `internal/providers/supervisor_test.go`

### Integration Points

#### In `internal/providers/store.go`:
```go
// Create supervisor per provider instance
supervisor := providers.NewProviderSupervisor()

// After successful initialization
supervisor.MarkHealthy()

// On heartbeat/health check
if err := provider.Heartbeat(ctx); err != nil {
    supervisor.RecordHeartbeatFailure(err)
}

// On provider crash (detected via monitoring)
supervisor.RecordCrash()

// Calculate restart backoff before reconnect
backoff := supervisor.GetRestartBackoff()
time.Sleep(backoff)

// After successful restart
supervisor.RecordRestart()
```

### Configuration

```go
type ProviderSupervisor struct {
    restartBase         time.Duration // Default: 1s
    restartMax          time.Duration // Default: 30s
    consecutiveFailures int           // Default: 5 before removal
    heartbeatTimeout    time.Duration // Default: 2m staleness check
}
```

#### Backoff Calculation:
```
restart_delay = min(base * 2^(attempt-1) + jitter, max)

Example with base=1s, max=30s:
Attempt 1: 1s + jitter
Attempt 2: 2s + jitter
Attempt 3: 4s + jitter
Attempt 4: 8s + jitter
Attempt 5: 16s + jitter
Attempt 6+: 30s + jitter (capped)
```

### Monitoring
- Metric: `dso_provider_health_status` - 1=healthy, 0=unhealthy
- Metric: `dso_provider_restarts_total` - Cumulative restart count
- Metric: `dso_provider_crashes_total` - Cumulative crash count
- Metric: `dso_provider_heartbeat_failures_total` - Failed health checks
- Metric: `dso_provider_uptime_seconds` - Time since last restart

### Production Checklist
- [ ] Implement crash detection via process monitoring (e.g., exit code tracking)
- [ ] Implement heartbeat checks every 30-60 seconds
- [ ] Alert on `dso_provider_crashes_total` > threshold per hour
- [ ] Alert on `dso_provider_restarts_total` > threshold per hour
- [ ] Test with `internal/providers/supervisor_test.go`

---

## Part 4: Event Deduplication Integration

### Overview
Fingerprint-based deduplication prevents replay amplification during daemon reconnects. Fingerprints are MD5 hashes of `(containerID + action)` to identify semantic identity independent of timestamp.

### Implementation Location
- Core: `internal/events/deduplication.go`
- Tests: `internal/events/deduplication_test.go`

### Integration Points

#### In event processing:
```go
import "github.com/docker-secret-operator/dso/internal/events"

// Create dedup cache at startup (30s TTL, 1000 max entries)
dedupCache := events.NewDedupCache(30*time.Second, 1000)

// Check before processing
if dedupCache.IsDuplicate(msg) {
    // Skip processing - this is a replayed event
    return
}

// Process event...
```

#### For single-stream deduplication (within reconciliation):
```go
// Create immediate tracker (no TTL)
tracker := events.NewImmediateDedup(1000)

for _, event := range reconcileStream {
    if tracker.IsDuplicate(event) {
        // Skip - already applied
        continue
    }
    // Apply operation...
}
```

### Fingerprinting Details

```go
// Fingerprint = MD5(containerID:action)
// Example:
// Event: {Action: "start", Actor: {ID: "abc123def456"}}
// Fingerprint: MD5("abc123def456:start") = "d7f8a2b3c4e5f6..."

// Different containers produce different fingerprints
// Different actions on same container produce different fingerprints
// Same event replayed produces identical fingerprint (detected as duplicate)
```

### Monitoring
- Metric: `dso_events_deduped_total` - Duplicate events suppressed
- Metric: `dso_dedup_cache_size` - Current cache entries

### Production Checklist
- [ ] Integrate into main event processing loop
- [ ] Set TTL to match expected reconnect recovery window (30-60s)
- [ ] Set maxSize to 2-3x expected duplicate events per recovery window
- [ ] Monitor `dso_events_deduped_total` to verify deduplication is working
- [ ] Test with `internal/events/deduplication_test.go:TestDeduplication_ReplayScenario`

---

## Part 5: Logging Audit & Secret Redaction

### Overview
Runtime validation ensures secrets never appear in logs. Uses pattern-based redaction for sensitive data and field-name analysis for structured logging.

### Implementation Location
- Core: `pkg/security/logging_audit.go` (validation framework)
- Redaction: `pkg/security/redaction.go` (pattern-based redaction)
- Tests: `pkg/security/logging_audit_test.go`

### Integration Points

#### Audit error logging before it reaches logs:
```go
import "github.com/docker-secret-operator/dso/pkg/security"

validator := security.NewLoggingAuditValidator()

// Example 1: Provider initialization error
providerErr := providerStore.GetProvider("vault", config)
if providerErr != nil {
    audit := validator.AuditErrorLogging(providerErr, "provider_initialization")
    if !audit.Safe {
        // Log safely redacted version
        redacted := validator.GetRedactedErrorMessage(providerErr)
        logger.Error("Provider initialization failed", zap.String("error", redacted))
    } else {
        logger.Error("Provider initialization failed", zap.Error(providerErr))
    }
}

// Example 2: Daemon reconnect with context
reconnectErr := daemon.Reconnect(ctx)
contextMap := map[string]string{
    "container_id": containerID,
    "project":      project,
}
audit := validator.ValidateErrorWithContext(reconnectErr, "daemon_reconnect", contextMap)
if !audit.Safe {
    logger.Error("Daemon reconnect failed", zap.Strings("leaks", audit.Leaks))
}

// Example 3: Structured logging field safety
fields := map[string]interface{}{
    "container_id": containerID,
    "action":       action,
    "error":        lastError,
}
audit := validator.AuditLogFieldSafety(fields)
if !audit.Safe {
    logger.Warn("Structured fields contain sensitive data", zap.Strings("unsafe", audit.Leaks))
}
```

### Redaction Patterns
The framework detects and redacts:
- API keys (api_key=, API-Key:, x-api-key:)
- Tokens (token=, Bearer, access_token=, refresh_token=)
- Passwords (password=, passwd=, pwd=)
- AWS credentials (AKIA*)
- Private keys (-----BEGIN PRIVATE KEY-----)
- Database connection strings (password=)
- OAuth tokens
- Docker auth configs

### Validation Methods

```go
type LoggingAuditValidator struct {
    // Validate error messages don't leak secrets
    AuditErrorLogging(err, context)
    
    // Validate panic recovery doesn't expose secrets
    AuditPanicPath()
    
    // Validate timeout errors and context are safe
    AuditTimeoutPath(err, contextData)
    
    // Validate RPC/serialization errors and requests
    AuditSerializationError(err, requestSummary)
    
    // Validate error chains are safe
    AuditNestedError(err)
    
    // Validate structured logging fields
    AuditLogFieldSafety(fields)
    
    // Validate error + context together
    ValidateErrorWithContext(err, contextType, contextMap)
    
    // Get safely redacted versions
    GetRedactedErrorMessage(err)
    GetRedactedString(input)
}
```

### Production Checklist
- [ ] Add logging audit validation at all error log sites
- [ ] Audit error paths in: provider initialization, daemon reconnects, RPC failures
- [ ] Add panic recovery with redaction (use `validator.GetRedactedString(panicMsg)`)
- [ ] Audit timeout error paths with context data
- [ ] Test with `pkg/security/logging_audit_test.go`
- [ ] Run regular "grep" audit: `grep -rn "logger.Error" --include="*.go" | grep "fmt.Sprintf.*Error"`

---

## Part 6: Resource Stability Validation

### Overview
Long-duration tests validate memory stability, goroutine stability, queue stability, provider restart stability, and reconnect storm resilience.

### Implementation Location
- Core: `internal/testing/resource_stability_test.go`
- Benchmarks: `internal/runtime/benchmarks_test.go`
- Recovery tests: `internal/runtime/recovery_test.go`

### Test Suite

#### 1. Memory Stability Under Sustained Load
```go
// 5-minute sustained operation test
// Validates memory growth < 50MB
// Checks for accelerating memory growth (leak indicator)
go test -run TestMemoryStabilityUnderSustainedLoad -v
```

#### 2. Goroutine Stability Under Churn
```go
// 3-minute high goroutine churn test
// Validates goroutine growth < 50 
// Detects goroutine leaks
go test -run TestGoroutineStabilityUnderChurn -v
```

#### 3. Queue Stability Under Saturation
```go
// 2-minute queue saturation test
// Validates memory growth < 100MB during saturation
// Ensures queue backpressure works
go test -run TestQueueStabilityUnderSaturation -v
```

#### 4. Provider Restart Stability
```go
// 2-minute repeated restart cycles
// Validates memory growth < 150MB
// Detects restart resource leaks
go test -run TestProviderRestartStability -v
```

#### 5. Reconnect Storm Stability
```go
// 2-minute rapid disconnect/reconnect cycles
// Validates memory growth < 80MB
// Detects reconnect resource leaks
go test -run TestReconnectStormStability -v
```

#### 6. Combined Operational Load
```go
// 3-minute realistic combined load
// Events + rotations + health checks
// Validates memory < 200MB, goroutines stable
go test -run TestCombinedOperationalLoad -v
```

### Resource Snapshot Framework

```go
type ResourceSnapshot struct {
    Timestamp        time.Time
    MemoryAllocBytes uint64
    MemoryTotalBytes uint64
    GoroutineCount   int
}

type ResourceStabilityTest struct {
    GetMemoryGrowth()       uint64   // Total growth
    GetMaxMemory()          uint64   // Peak memory
    GetAverageMemory()      uint64   // Average across test
    GetGoroutineGrowth()    int      // Goroutine growth
    IsMemoryStable(%)       bool     // Checks non-acceleration
    IsGoroutineStable(max)  bool     // Checks bounded growth
}
```

### Running Full Stability Suite
```bash
# Run all resource stability tests
go test -v ./internal/testing/ -run "Resource|Stability|Churn|Saturation|Storm|Combined"

# Run with output
go test -v -run TestMemoryStabilityUnderSustainedLoad ./internal/testing/

# Run benchmarks
go test -v -run "Benchmark|RunBenchmark" ./internal/runtime/
```

### Production Checklist
- [ ] Run full stability suite in staging before production
- [ ] Document baseline memory/goroutine numbers for your deployment
- [ ] Set monitoring alerts based on stability test thresholds
- [ ] Re-run stability suite after major code changes
- [ ] Run 24h+ extended tests in production-like environment

---

## Part 7: Metrics & Monitoring

### All Integrated Metrics

#### Event & Queue Metrics
- `dso_event_queue_depth` - Queue depth (0 = empty, max = 1000)
- `dso_events_dropped_total` - Dropped events (should be 0)
- `dso_worker_utilization_percent` - Worker pool utilization (0-100)
- `dso_events_deduped_total` - Duplicate events suppressed
- `dso_dedup_cache_size` - Current dedup cache entries

#### Provider Metrics
- `dso_provider_health_status` - Health status (1=healthy, 0=unhealthy)
- `dso_provider_restarts_total` - Total restarts
- `dso_provider_crashes_total` - Total crashes
- `dso_provider_heartbeat_failures_total` - Failed health checks
- `dso_provider_uptime_seconds` - Time since last restart
- `dso_provider_heartbeat_latency_seconds` - Health check latency
- `dso_provider_restart_duration_seconds` - Restart operation duration

#### Daemon & Reconnection Metrics
- `dso_reconnect_attempts_total` - Total reconnection attempts
- `dso_reconnect_duration_seconds` - Reconnect operation duration

#### Reconciliation Metrics
- `dso_reconciliation_drift_seconds` - Time since last reconciliation
- `dso_reconciliation_items_processed_total` - Items processed
- `dso_reconciliation_errors_total` - Reconciliation errors

#### Memory & Resources
- `dso_runtime_memory_usage_bytes` - Current memory usage
- `dso_runtime_goroutine_count` - Current goroutine count
- `dso_runtime_memory_alloc_rate` - Memory allocation rate

### Alerting Rules

```yaml
# Alert if queue is dropping events
- alert: DSO_QueueDropping
  condition: dso_events_dropped_total > 0
  severity: critical
  
# Alert if worker pool is saturated
- alert: DSO_WorkerPoolSaturated
  condition: dso_worker_utilization_percent > 85
  severity: warning
  
# Alert if provider is unhealthy
- alert: DSO_ProviderUnhealthy
  condition: dso_provider_health_status == 0
  severity: critical
  
# Alert if provider keeps restarting
- alert: DSO_ProviderRestartLoop
  condition: dso_provider_restarts_total > 5 in 1h
  severity: critical
  
# Alert if memory is growing unbounded
- alert: DSO_MemoryGrowth
  condition: rate(dso_runtime_memory_alloc_rate[5m]) > threshold
  severity: warning
  
# Alert if goroutines are leaking
- alert: DSO_GoroutineLeak
  condition: rate(dso_runtime_goroutine_count[5m]) > threshold
  severity: warning
```

---

## Part 8: Testing & Validation

### Unit Tests
```bash
# Event backpressure
go test -v ./internal/events/ -run "Backpressure"

# Provider supervision
go test -v ./internal/providers/ -run "Supervisor"

# Event deduplication
go test -v ./internal/events/ -run "Dedup|Deduplicate"

# Logging audit
go test -v ./pkg/security/ -run "LoggingAudit"

# Recovery scenarios
go test -v ./internal/runtime/ -run "Recovery"
```

### Integration Tests
```bash
# Full bounded event queue with backpressure
go test -v ./internal/events/ -run "Panic|Timeout|Concurrent|Overflow"

# Full provider supervision with crashes
go test -v ./internal/providers/ -run "Crash|Backoff|Heartbeat|Concurrent"

# Full event replay safety
go test -v ./internal/events/ -run "Replay|Daemon|Concurrent"

# Recovery completeness
go test -v ./internal/runtime/ -run "Completeness|Partial|Stale"
```

### Performance Tests
```bash
# Sustained churn load
go test -v -run TestSustainedChurnLoad ./internal/runtime/

# Concurrent rotations
go test -v -run TestConcurrentSecretRotations ./internal/runtime/

# Memory stability
go test -v -run TestMemoryStabilityUnderSustainedLoad ./internal/testing/

# Queue saturation
go test -v -run TestQueueSaturationBenchmark ./internal/runtime/
```

---

## Part 9: Deployment Checklist

### Pre-Deployment
- [ ] All Tier 1 hardening tests pass
- [ ] All Tier 2 supervision tests pass
- [ ] Logging audit finds no unredacted errors
- [ ] Resource stability tests show acceptable baselines
- [ ] Metrics exported to Prometheus
- [ ] Alerting rules configured

### Deployment
- [ ] Deploy with event backpressure enabled
- [ ] Deploy with provider supervision enabled
- [ ] Deploy with event deduplication enabled
- [ ] Deploy with logging audit validation enabled
- [ ] Deploy with metrics export enabled

### Post-Deployment (First 24 Hours)
- [ ] Monitor `dso_events_dropped_total` (should be 0)
- [ ] Monitor `dso_worker_utilization_percent` (should stay < 75%)
- [ ] Monitor `dso_provider_health_status` (should stay 1 for all providers)
- [ ] Monitor `dso_runtime_memory_usage_bytes` (should be stable)
- [ ] Monitor `dso_runtime_goroutine_count` (should be stable)

### Post-Deployment (First Week)
- [ ] Run benchmarks under production-like load
- [ ] Collect memory/goroutine baselines
- [ ] Adjust backpressure queue size if needed
- [ ] Adjust provider restart backoff if needed
- [ ] Review and tune alerting thresholds

---

## Part 10: Troubleshooting

### Events Being Dropped
**Symptom**: `dso_events_dropped_total` > 0

**Diagnosis**:
1. Check `dso_worker_utilization_percent` - if > 95%, workers are saturated
2. Check application logs for slow event handlers
3. Check container churn rate (peak events/sec)

**Solutions**:
- Increase `maxEvents` in BoundedEventQueue (max 5000)
- Increase `workerCount` (max 64)
- Optimize event handler performance

### Provider Crashing
**Symptom**: `dso_provider_restarts_total` > 5 in 1 hour, `dso_provider_crashes_total` increasing

**Diagnosis**:
1. Check provider logs for crash reasons
2. Check `dso_provider_heartbeat_failures_total` - high value indicates health check failures
3. Check provider initialization errors

**Solutions**:
- Review provider configuration
- Check provider resource limits (memory, CPU)
- Consider provider process supervisor upgrade

### Memory Growth
**Symptom**: `dso_runtime_memory_usage_bytes` continuously increasing

**Diagnosis**:
1. Run `TestMemoryStabilityUnderSustainedLoad` to baseline
2. Check `dso_event_queue_depth` - if high, events are queuing
3. Check goroutine count - high count indicates resource leak

**Solutions**:
- Profile with `pprof`: `go tool pprof http://localhost:6060/debug/pprof/heap`
- Check for goroutine leaks
- Verify event deduplication is working (check `dso_events_deduped_total`)

### Goroutine Leak
**Symptom**: `dso_runtime_goroutine_count` continuously increasing

**Diagnosis**:
1. Run `TestGoroutineStabilityUnderChurn` to baseline
2. Use `pprof` to analyze goroutine creation sites
3. Check for infinite event processing loops

**Solutions**:
- Profile with goroutine analyzer: `curl http://localhost:6060/debug/pprof/goroutine`
- Review event handler for missing context cancellation
- Check provider supervision for missing cleanup

---

## Part 11: Design Decisions

### Why Event Fingerprinting (Not Persistent)?
- **Persistent approach**: Would require stable storage (disk/database), slow, complex crash recovery
- **Fingerprinting approach**: Memory-only, fast, works with TTL, automatic cleanup
- **Trade-off**: Works for reconnects (30s-60s window), not for long-term replay prevention
- **Rationale**: DSO is event-driven and stateless; persistent dedup not needed

### Why Exponential Backoff with Jitter?
- **Fixed backoff**: All providers restart at same time → cascading failures
- **Exponential backoff**: Reduces collision, spreads restart load over time
- **Jitter**: Adds randomness to prevent thundering herd
- **Cap at 30s**: Balances recovery speed vs. avoiding cascades

### Why TTL-Based Dedup Cache?
- **Unbounded cache**: Grows indefinitely, memory leak
- **TTL-based cache**: Automatic cleanup, bounded memory
- **Default 30s**: Covers reconnect recovery window without keeping stale entries
- **Override in tests**: Can be adjusted for different recovery scenarios

### Why Audit-Based Logging (Not Automatic Redaction)?
- **Automatic redaction**: May miss edge cases, false sense of security
- **Audit framework**: Validates before logging, catches edge cases
- **Pattern-based**: Easy to extend with new patterns
- **Field-aware**: Understands that `password` field = sensitive, even if generic word

---

## Appendix A: Configuration Reference

### BoundedEventQueue
```go
NewBoundedEventQueue(logger, maxEvents, workerCount, handler)

Default config:
- maxEvents: 1000
- workerCount: 16
- timeout: 30 seconds per event
- retry: 3 attempts with exponential backoff

Recommended:
- For 10K events/min: maxEvents=2000, workerCount=32
- For 1K events/min: maxEvents=500, workerCount=8
```

### ProviderSupervisor
```go
NewProviderSupervisor()

Default config:
- restartBase: 1 second
- restartMax: 30 seconds
- consecutiveFailures: 5 (threshold for removal)
- heartbeatTimeout: 2 minutes (staleness check)

Restart backoff progression:
1s → 2s → 4s → 8s → 16s → 30s (capped)
```

### DedupCache
```go
NewDedupCache(ttl, maxSize)

Default config:
- ttl: 30 seconds
- maxSize: 1000 entries

Recommended:
- For daemon reconnects: 30-60s TTL
- For reconciliation: 10-30s TTL
- maxSize: 2-3x expected duplicates per window
```

### LoggingAuditValidator
```go
validator := NewLoggingAuditValidator()

Audits:
- Error messages (direct + chains)
- Panic paths
- Timeout contexts
- Serialization requests
- Structured logging fields

Redaction patterns (built-in):
- API keys, tokens, passwords
- AWS credentials, Docker auth
- Private keys (PEM format)
- Database connection strings
- OAuth tokens
```

---

## Appendix B: Metrics Reference

### Complete Metrics List

| Metric | Type | Labels | Purpose |
|--------|------|--------|---------|
| `dso_event_queue_depth` | Gauge | - | Queue depth |
| `dso_events_dropped_total` | Counter | - | Dropped events |
| `dso_worker_utilization_percent` | Gauge | pool_name | Worker pool % |
| `dso_worker_pool_starvation_total` | Counter | pool_name | Saturated events |
| `dso_worker_task_duration_seconds` | Histogram | pool_name, task_type | Task latency |
| `dso_provider_health_check_status` | Gauge | provider | 1=healthy, 0=unhealthy |
| `dso_provider_restarts_total` | Counter | provider | Total restarts |
| `dso_provider_crashes_total` | Counter | provider | Total crashes |
| `dso_provider_heartbeat_latency_seconds` | Histogram | provider | Health check latency |
| `dso_provider_restart_duration_seconds` | Histogram | provider | Restart duration |
| `dso_provider_heartbeat_failures_total` | Counter | provider | Failed health checks |
| `dso_provider_uptime_seconds` | Gauge | provider | Time since restart |
| `dso_reconnect_attempts_total` | Counter | reason, status | Reconnect attempts |
| `dso_reconnect_duration_seconds` | Histogram | reason | Reconnect duration |
| `dso_reconciliation_drift_seconds` | Gauge | component | Time since last recon |
| `dso_reconciliation_items_processed_total` | Counter | component, status | Items processed |
| `dso_reconciliation_errors_total` | Counter | component, error_type | Reconciliation errors |
| `dso_stale_items_detected_total` | Counter | item_type | Stale items found |
| `dso_stale_cleanup_duration_seconds` | Histogram | item_type | Cleanup duration |
| `dso_events_deduped_total` | Counter | reason | Duplicate events |
| `dso_dedup_cache_size` | Gauge | - | Cache entries |
| `dso_runtime_memory_usage_bytes` | Gauge | - | Current memory |
| `dso_runtime_goroutine_count` | Gauge | - | Current goroutines |
| `dso_runtime_memory_alloc_rate` | Gauge | - | Alloc rate (bytes/sec) |
| `dso_injection_attempts_total` | Counter | injection_type, status | Injection attempts |
| `dso_injection_latency_seconds` | Histogram | injection_type | Injection latency |

---

## Appendix C: Quick Reference Commands

### Run All Hardening Tests
```bash
# All tests
go test -v ./internal/... ./pkg/security/ -run "Backpressure|Supervisor|Dedup|LoggingAudit"

# Specific areas
go test -v ./internal/events/ -run "Backpressure"
go test -v ./internal/providers/ -run "Supervisor"
go test -v ./internal/events/ -run "Dedup"
go test -v ./pkg/security/ -run "LoggingAudit"
```

### Run Stability Tests
```bash
# Memory stability
go test -v -run "MemoryStability" ./internal/testing/

# Goroutine stability
go test -v -run "GoroutineStability" ./internal/testing/

# Queue stability
go test -v -run "QueueStability" ./internal/testing/

# Provider restart
go test -v -run "ProviderRestart" ./internal/testing/

# Reconnect storm
go test -v -run "ReconnectStorm" ./internal/testing/

# Combined load
go test -v -run "CombinedOperational" ./internal/testing/
```

### Run Benchmarks
```bash
# All benchmarks
go test -v -run "Benchmark|Sustained|Concurrent|Provider|Queue|Memory|Reconnect|Latency" ./internal/runtime/

# Specific benchmarks
go test -v -run TestSustainedChurnLoad ./internal/runtime/
go test -v -run TestConcurrentSecretRotations ./internal/runtime/
go test -v -run TestQueueSaturation ./internal/runtime/
```

---

End of Runtime Hardening & Production Stability Guide.
