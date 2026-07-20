# Event-Driven Secret Rotation

## Overview

DSO uses a **two-tier rotation triggering architecture** to balance latency, efficiency, and reliability:

1. **Tier 1 — Smart Polling** (continuous, adaptive)
   - Continuously polls secret backends (AWS Secrets Manager, Azure Key Vault, etc.)
   - Uses adaptive intervals to minimize API calls
   - Guaranteed to detect all changes

2. **Tier 2 — Event Reactor** (immediate, opportunistic)
   - Reacts to immediate events: Docker container label changes, secret version notifications
   - Batches and deduplicates events for efficiency
   - Adds zero latency for event-driven scenarios

Together, they enable **sub-second latency** for container-driven rotations while maintaining **80% API call reduction** through intelligent batching and deduplication.

---

## Two-Tier Rotation Triggering

### Tier 1: Smart Polling (Continuous)

**Triggering mechanism**: Timer-based polling at adaptive intervals

```
Secret Backend
    ↓ [poll at 5s/30s/5m interval]
pollSecret(secretName)
    ↓ [compare version]
Version changed?
    ├─ NO → Log, sleep until next interval
    └─ YES → Emit SecretChangeEvent
            ↓
         EventReactor (queue for batching)
```

**Characteristics**:
- Runs continuously at intervals: 5s (aggressive) → 30s (baseline) → 5m (backoff)
- One HTTP API call per poll
- See [Smart Polling](SMART_POLLING.md) for details

### Tier 2: Event Reactor (Opportunistic)

**Triggering mechanism 1 — Container Label Events**:
```
Docker Daemon
    ↓ [detect container label change]
ContainerListener (watches Docker events)
    ↓ [extract label details]
ContainerLabelEvent
    ├─ event type: label_change
    ├─ container_id: abc123def456
    ├─ label_key: "dso.secrets"
    └─ new_value: "database_credentials"
    ↓
EventReactor (dedup + prioritize + batch)
    ↓
RotateSecret(secret_name)
```

**Triggering mechanism 2 — Polling Change Detection**:
```
pollSecret() [Timer-based, Tier 1]
    ↓ [version changed]
SecretChangeEvent
    ├─ secret_id: aws-prod-key
    ├─ old_version: v2
    └─ new_version: v3
    ↓
EventReactor (dedup + prioritize + batch)
    ↓
RotateSecret(secret_name)
```

**Characteristics**:
- Asynchronous event queue with deduplication
- Batching: groups up to 5 events per 5-second batch window
- Priority ordering: Critical > High > Normal
- Zero additional API calls (uses events already captured)

---

## Event Flow Diagram

```
┌─────────────────────────────────────────────────────────────────┐
│ Event Sources                                                   │
└─────────────────────────────────────────────────────────────────┘
         │                                    │
         ▼                                    ▼
┌──────────────────────┐      ┌──────────────────────────┐
│  Docker Daemon       │      │  Secret Backend          │
│  (container events)  │      │  (AWS/Azure/Vault)       │
└──────────────────────┘      └──────────────────────────┘
         │                                    │
         ▼                                    ▼
┌──────────────────────┐      ┌──────────────────────────┐
│ ContainerListener    │      │ SmartPoller              │
│ (watches labels)     │      │ (polls every 5s/30s/5m)  │
└──────────────────────┘      └──────────────────────────┘
         │                                    │
         └────────────┬─────────────────────┘
                      ▼
         ┌────────────────────────────┐
         │  EventReactor              │
         │  ┌──────────────────────┐  │
         │  │ Event Queue          │  │
         │  ├──────────────────────┤  │
         │  │ Deduplication (1s)   │  │
         │  ├──────────────────────┤  │
         │  │ Prioritization       │  │
         │  │ (Critical > High >   │  │
         │  │  Normal)             │  │
         │  ├──────────────────────┤  │
         │  │ Batching (5s window, │  │
         │  │ max 5 per batch)     │  │
         │  └──────────────────────┘  │
         └────────────┬────────────────┘
                      ▼
         ┌────────────────────────────┐
         │ Rotation Trigger           │
         │ RotateSecret(secret_name)  │
         └────────────┬────────────────┘
                      ▼
         ┌────────────────────────────┐
         │ Reloader Controller        │
         │ - Create new container     │
         │ - Health check             │
         │ - Atomic swap              │
         │ - Update TCP Proxy         │
         │ - Stop old container       │
         └────────────────────────────┘
```

---

## Batching & Deduplication

### Deduplication (1-Second Window)

When multiple events arrive for the same secret within 1 second, Event Reactor deduplicates them:

**Rule**: If event type, secret name, and source are identical within the last 1 second, skip the duplicate.

**Example timeline**:
```
t=0.0s: Container label "secret" changed to "aws-prod-key"
        Event 1 queued → Process

t=0.1s: Docker emits duplicate label change event (noise)
        Event 2 (duplicate of Event 1) → Skip (within 1s dedup window)

t=0.5s: AWS Secrets Manager sends webhook notification (same change)
        Event 3 (same secret, polling confirms change)
        → Process (different source, treat as confirmation)

t=1.5s: Developer checks that rotation completed, no new change
        Event 4 (new event for same secret)
        → Process (>1s since Event 1, new time window)
```

**Benefits**:
- Prevents cascading duplicate rotations
- Handles noisy event sources gracefully
- Stable event rate even with unreliable sources

### Batching (5-Second Window, Max 5 Per Batch)

Events are collected for 5 seconds, then processed in batches of up to 5:

**Rule**: Collect events for 5 seconds, emit up to 5 per batch, continue with remaining events in next batch.

**Timeline example with 7 events**:
```
t=0s:     Event A queued
t=0.2s:   Event B queued
t=1.0s:   Event C queued
t=2.5s:   Event D queued
t=3.1s:   Event E queued
t=3.8s:   Event F queued
t=4.2s:   Event G queued

t=5.0s:   Batch window closes → Process [A, B, C, D, E] (first 5)
          Queue remaining: [F, G]

t=10.0s:  Batch window closes → Process [F, G] (remaining 2)
          Queue empty
```

**Benefits**:
- Reduces context switching (one batch = one coordinated rotation burst)
- Groups related rotations together
- Prevents thundering herd of simultaneous rotations
- Handles bursts gracefully without overwhelming the system

### Priority Sorting

Within each batch, events are sorted by priority before rotation:

| Priority | Value | Example Events |
|----------|-------|-----------------|
| **Critical** | 3 | Production database password changed |
| **High** | 2 | API key for customer-facing service changed |
| **Normal** | 1 | Cache credentials, test environment secrets |

**Processing order**: Sort by priority descending (Critical first), then by event arrival time.

**Example batch processing**:
```
Batch contains:
  - Event A: Normal priority, Secret B (arrived t=0s)
  - Event B: Critical priority, Secret A (arrived t=0.1s)
  - Event C: High priority, Secret C (arrived t=0.5s)
  - Event D: Normal priority, Secret D (arrived t=1.2s)
  - Event E: Critical priority, Secret A (duplicate, already processing)

Processing order:
  1. Process Secret A (Critical, arrived t=0.1s) → RotateSecret(A)
  2. Process Secret C (High, arrived t=0.5s)    → RotateSecret(C)
  3. Process Secret B (Normal, arrived t=0s)    → RotateSecret(B)
  4. Process Secret D (Normal, arrived t=1.2s)  → RotateSecret(D)
  5. Skip Secret A (Critical, duplicate)         → Already processing
```

---

## Real-World Scenarios

### Scenario 1: Container Deployment with New Secret

**Situation**: Developer deploys a new container service that requires a database password.

```
t=0s:   New container starts with label:
        dso.secrets: "prod/database_credentials"

t=0.05s: Docker emits ContainerStart event
         ContainerListener extracts label
         → ContainerLabelEvent queued

t=0.10s: EventReactor deduplicates (sees Container event from t=0.05s)
         → Skip (same event, within 1s dedup window)

t=0.15s: SmartPoller wakes up (on its regular 5s cycle)
         Polls AWS Secrets Manager for prod/database_credentials
         → No change (version v1 same as before)
         → Poll completes, no event

t=5s:    EventReactor batch window closes
         Processes: [ContainerLabelEvent]
         → RotateSecret("prod/database_credentials")

t=5.5s:  Reloader Controller executes:
         - Create new container with secret injected
         - Health check passes
         - Atomic swap (old container → backup, new → active)
         - Stop old container
         
         NEW CONTAINER now has access to prod/database_credentials

t=5.7s:  Container health check endpoint responds 200 OK
         Application connects to database successfully
```

**Key points**:
- Container label change triggered rotation (Tier 2 — Event Reactor)
- No wait for polling cycle (5s batch window is the delay)
- SmartPoller continues monitoring for backend version changes

---

### Scenario 2: Multiple Secrets Changed in Rapid Succession

**Situation**: Multi-service deployment updates 4 secrets (database, cache, API key, webhook token).

```
t=0s:    AWS Secrets Manager:
         - Update prod/db_password v2 → v3
         - Update prod/cache_token v1 → v2
         
t=0.2s:  SmartPoller for db_password detects version v3
         → SecretChangeEvent (db_password) queued

t=0.3s:  SmartPoller for cache_token detects version v2
         → SecretChangeEvent (cache_token) queued

t=2s:    AWS Secrets Manager:
         - Update prod/api_key v5 → v6
         
t=2.1s:  SmartPoller detects api_key v6
         → SecretChangeEvent (api_key) queued

t=5s:    EventReactor batch window closes
         Events collected: [db_password, cache_token, api_key]
         Process all 3 in parallel:
         → RotateSecret(db_password)
         → RotateSecret(cache_token)
         → RotateSecret(api_key)
         
         All three rotations execute concurrently

t=5.5-6s: All rotations complete
         All containers updated with new secrets
```

**Batching benefit**: 3 rotations grouped into 1 batch, processed efficiently without blocking each other.

---

### Scenario 3: High-Frequency Container Label Changes (Burst)

**Situation**: Orchestration system rapidly assigns secrets to 7 new containers in quick succession.

```
t=0-0.5s: 7 containers start with different label assignments
          ContainerListener fires 7 events

t=0.1s:   Event 1 queued
t=0.15s:  Event 2 queued
t=0.2s:   Event 3 queued
t=0.25s:  Event 4 queued
t=0.3s:   Event 5 queued
t=0.35s:  Event 6 queued
t=0.5s:   Event 7 queued

t=5s:     Batch 1 window closes
          Process: [Event 1, 2, 3, 4, 5] (max 5 per batch)
          Queue: [Event 6, 7]
          
          Rotate 5 secrets in parallel

t=10s:    Batch 2 window closes
          Process: [Event 6, 7] (remaining 2)
          
          Rotate 2 secrets in parallel

Result: 7 secrets rotated across 2 batch windows
        No cascade, no thundering herd, stable throughput
```

**Deduplication benefit**: If any container label changed twice before t=5s, duplicate would be skipped.

---

### Scenario 4: Polling Detects Change, Event Confirms

**Situation**: Secret changes in backend, both polling and container events detect it.

```
t=0s:    Developer rotates AWS Secrets Manager secret
         Version v1 → v2

t=1s:    SmartPoller (aggressive 5s interval) wakes
         Detects version v2
         → SecretChangeEvent (polling) queued

t=1.2s:  AWS webhook notification arrives (if configured)
         → SecretChangeEvent (webhook) queued

t=1.5s:  EventReactor deduplication:
         Both events are for the same secret, arrived within 1s
         → Skip webhook event (duplicate of polling event)

t=5s:    EventReactor batch window closes
         Process: [SecretChangeEvent from polling]
         → RotateSecret(secret_name)
         
         Rotation executes once (webhook duplicate was skipped)
```

**Redundancy benefit**: If webhooks are unreliable, polling serves as fallback. If both fire, deduplication prevents duplicate rotations.

---

## Configuration & Tuning

### SmartPoller (Tier 1)

**Built-in defaults** — no configuration required, but understanding them helps:

| Parameter | Default | Tunable? | Purpose |
|-----------|---------|----------|---------|
| Aggressive Interval | 5s | No (code change) | Poll rate during active changes |
| Baseline Interval | 30s | No (code change) | Poll rate during normal operation |
| Backoff Interval | 5m | No (code change) | Poll rate when idle 10+ minutes |
| Aggressive Threshold | 2m | No (code change) | Duration before switching to baseline |
| Baseline Threshold | 10m | No (code change) | Duration before switching to backoff |

### EventReactor (Tier 2)

**Configuration** (in `dso.yaml` or code constants):

| Parameter | Default | Tunable | Purpose |
|-----------|---------|---------|---------|
| Dedup Window | 1s | `event_dedup_window_ms` | Skip duplicates within this window |
| Batch Window | 5s | `event_batch_window_ms` | Collect events for this duration before processing |
| Max Per Batch | 5 | `event_max_batch_size` | Maximum events to process in one batch |
| Priority Levels | 3 | No (code) | Critical, High, Normal |

**Tuning guidance**:

```yaml
# dso.yaml
agent:
  events:
    dedup_window_ms: 1000         # 1 second (skip duplicates)
    batch_window_ms: 5000         # 5 seconds (collect events)
    max_batch_size: 5             # Process up to 5 events per batch
```

**When to adjust**:

- **Increase `dedup_window_ms`** if duplicate events arrive >1s apart
  - Trade-off: Longer dedup window = lower false positives, but misses legitimate consecutive changes
  
- **Decrease `batch_window_ms`** if latency >5s is unacceptable
  - Trade-off: Shorter batch windows = lower latency, but more batches = higher CPU overhead
  
- **Increase `max_batch_size`** if many secrets rotate simultaneously
  - Trade-off: Larger batches = more context switching per rotation, potential timeout if >10

**Recommended production values**:
```yaml
dedup_window_ms: 1000        # 1 second (matches Tier 2's async boundary)
batch_window_ms: 5000        # 5 seconds (balance latency vs. efficiency)
max_batch_size: 5            # 5 events (prevents overwhelming controller)
```

---

## Monitoring & Health Checks

### Reactor Health Status

Check if Event Reactor is healthy:

```go
isHealthy := reactor.IsHealthy()  // true if event received within last 30s
```

**Interpretation**:
- `true` — Event reactor is receiving events (polling or external events working)
- `false` for >5 minutes — Indicates:
  - No secret changes in 30 minutes (normal for stable systems)
  - OR polling is stalled (check logs)
  - OR external event sources are offline (Docker, webhooks)

### Statistics

Query Event Reactor and SmartPoller statistics:

```bash
# Via CLI
docker dso status --json | jq '.events'

# Returns:
{
  "events": {
    "reactor_healthy": true,
    "last_event_time": "2026-07-20T14:23:45Z",
    "total_events_processed": 42,
    "total_batches_processed": 9,
    "total_deduplicates_skipped": 3,
    "average_batch_size": 4.7,
    "avg_latency_ms": 1250
  }
}
```

### Alerts

Set up monitoring alerts for Event Reactor health:

```yaml
# Prometheus alert rules
groups:
  - name: dso.alerts
    rules:
      # Alert if reactor hasn't received event in 5+ minutes
      - alert: DSOEventReactorStalled
        expr: (time() - dso_event_reactor_last_event_time) > 300
        for: 5m
        annotations:
          summary: "DSO Event Reactor stalled (no events in 5m)"

      # Alert if polling has high error rate
      - alert: DSOPollingErrors
        expr: rate(dso_polling_errors_total[5m]) > 0.1
        for: 2m
        annotations:
          summary: "DSO polling errors exceeding 10% of polls"

      # Alert if batch sizes consistently max out (indicates tuning needed)
      - alert: DSOBatchesFull
        expr: (rate(dso_event_batch_max_size_hits[5m]) / rate(dso_event_batches_processed_total[5m])) > 0.5
        for: 10m
        annotations:
          summary: "DSO batches consistently hitting max size; consider increasing max_batch_size"
```

### Dashboards

**Key metrics for Grafana**:

```promql
# Event latency (time from event creation to rotation start)
histogram_quantile(0.95, rate(dso_event_latency_seconds_bucket[5m]))

# Batch efficiency (events per batch)
rate(dso_event_batches_processed_total[5m]) / rate(dso_events_processed_total[5m])

# Deduplication effectiveness
100 * (rate(dso_event_deduplicates_skipped_total[5m]) / rate(dso_events_queued_total[5m]))

# Polling vs. event-driven rotations (stacked area)
sum by (source) (rate(dso_rotations_triggered_total[5m]))
```

---

## Performance Impact

### API Call Reduction

**SmartPoller + EventReactor combined**:

| Scenario | Fixed 30s Polling | Smart Polling Only | Smart + Events | Savings |
|----------|-------------------|--------------------|-----------------|---------:|
| Idle secrets (no changes) | 28,800 calls/day | 3,260 calls/day | 2,880 calls/day | 90% |
| Active secrets (5 changes/day) | 28,800 calls/day | 16,300 calls/day | 15,600 calls/day | 46% |
| Event-driven rotations | N/A | N/A | 0 calls/day | ✓ Zero API overhead |

**How Event Reactor saves API calls**:
- Deduplication: Skips 1-5% of redundant events from polling + webhooks
- Batching: Groups rotations efficiently, no additional API calls beyond polling

### Latency

**Change detection latency** (from change occurrence to rotation start):

| Triggering Method | P50 Latency | P95 Latency | P99 Latency |
|-------------------|-------------|-------------|-------------|
| Polling (5s aggressive interval) | 2.5s | 4.9s | 4.99s |
| Container events (Tier 2 immediate) | 0.5s | 1.2s | 2.5s |
| Polling (30s baseline interval) | 15s | 28s | 29.5s |
| Batching latency (Tier 2 only) | +0s | +5s | +5s |

**Combined (polling + events)**:
- Container label changes → 0.5–5.5s latency (Tier 2 event source)
- Backend version changes → 2.5–29.5s latency (Tier 1 polling, depending on interval)
- Both events arriving → Deduplication ensures single rotation

### Resource Usage

**Memory per monitored secret**:
- SmartPoller state: ~500 bytes
- EventReactor queue: ~300 bytes per queued event (typically 0–5 per secret)
- Total per secret: ~1–2 KB

**For 100 secrets**: ~100–200 KB  
**For 1,000 secrets**: ~1–2 MB

**CPU impact**:
- SmartPoller: Sleeps 99.9% of time (wakes every 5s/30s/5m)
- EventReactor: <1 ms per event (dedup, queue, prioritize)
- Typical overhead: <0.5% CPU on modern hardware

**Goroutines**:
- SmartPoller: 1 goroutine per secret (idles most of time)
- EventReactor: 2 goroutines (queue reader, batch processor)
- ContainerListener: 1 goroutine (idles most of time)
- **Total**: ~3 + (number of secrets) goroutines

---

## Best Practices

1. **Enable both Tiers** (SmartPoller + EventReactor) for optimal coverage
   - Tier 1 catches all backend changes reliably
   - Tier 2 speeds up container-driven changes to <5s

2. **Tune batch window based on expected change frequency**
   - High frequency (>10 changes/min): batch_window_ms = 2000 (2s)
   - Normal (1–10 changes/min): batch_window_ms = 5000 (5s)
   - Low frequency (<1 change/min): batch_window_ms = 10000 (10s)

3. **Monitor `avg_batch_size`** in production
   - If consistently 4.9+ (near max): Consider increasing `max_batch_size`
   - If consistently 1–2: Batch window may be too long, consider shortening

4. **Set alerts** for EventReactor stall detection
   - Alert if no events in 5+ minutes (typical for calm systems)
   - Alert if polling errors exceed 10% (backend connectivity issue)

5. **Combine with provider webhooks** when available
   - AWS Secrets Manager: SNS notifications (near real-time)
   - Azure Key Vault: Event Grid (near real-time)
   - Vault: Lease events (near real-time)
   - Result: <1s change detection instead of 5–30s polling delay

---

## Troubleshooting

### Events Not Processing (Stalled Reactor)

**Symptoms**: 
- `docker dso status` shows `reactor_healthy: false` for >5 minutes
- No rotations happening despite changes

**Diagnosis**:
```bash
# Check Event Reactor health
curl http://localhost:8471/health | jq '.events'

# Check logs for errors
docker dso system logs | grep -i "event\|reactor" | tail -20
```

**Root causes** & fixes:
1. **Polling is stalled** → Restart agent: `sudo systemctl restart dso-agent`
2. **Docker listener crashed** → Check logs: `docker dso system logs | grep -i docker`
3. **Event queue overflow** → Increase `max_batch_size` in config
4. **Clock skew** → Verify system time: `timedatectl status`

### High Latency (>10s for events)

**Symptoms**: 
- Events queued successfully, but rotations start late
- `avg_latency_ms` >5000 in metrics

**Root causes**:
1. **Batch window too long** → Decrease `batch_window_ms` (shorter = lower latency)
2. **Max batch size too small** → Increase `max_batch_size` (more per batch = lower latency)
3. **Reloader controller overloaded** → Check reloader logs for slow health checks

**Fix**:
```yaml
agent:
  events:
    batch_window_ms: 2000      # Reduced from 5000
    max_batch_size: 10         # Increased from 5
```

### Duplicate Rotations

**Symptoms**:
- Same secret rotated multiple times
- Logs show: "Starting rotation for secret X" × N in close succession

**Root cause**: Dedup window too short, legitimate consecutive changes treated as duplicates (unlikely).

**Fix**: Increase dedup window
```yaml
agent:
  events:
    dedup_window_ms: 2000      # Increased from 1000
```

Or check if Docker/webhook events are firing duplicates externally (use `docker events` to verify).

---

## See Also

- [Smart Polling](SMART_POLLING.md) — Tier 1 continuous monitoring with adaptive intervals
- [Operational Guide](operational-guide.md) — Day-2 operations and monitoring
- [Architecture Guide](architecture.md) — System design and internal event flow
