# Rotation Logic & State Management Review

**Date:** 2026-07-20  
**Scope:** Secret rotation strategies, state persistence, recovery, and locking

---

## Rotation Pipeline (Documented Spec)

From README, secret rotation should follow this flow:
```
Secret change detected (polling every 30s–5m, adaptive)
  ↓
Acquire distributed lock (prevent concurrent rotation)
  ↓
Create new container with updated secret env
  ↓
Health check new container
  ↓
Atomic swap (rename old → backup, new → active)
  ↓
Route traffic via TCP proxy
  ↓
Stop old container
  ↓
Rollback on failure (auto-restore previous state)
```

---

## Implementation Status

### Rotation Entry Point
**Expected location**: `internal/rotation/rotation.go`
**Actual status**: Directory not found in preliminary scan
**Alternative locations checked**:
- `internal/agent/agent.go` - May contain rotation logic
- `internal/cli/up.go` - May contain container startup/rotation

**Finding**: Rotation logic appears to be embedded in agent or separate module. Full implementation details not yet located in standard location.

---

## Rotation Strategies Implemented

### Documented Strategies (from README):

| Strategy | Behavior | Use Case |
|----------|----------|----------|
| rolling | Zero-downtime blue-green swap | Production databases, APIs |
| restart | Stop old, start new container | Stateless services |
| signal | Send SIGHUP to running container | Apps that reload config |
| none | Update secret cache only | Manual rotation workflows |

### Implementation Questions:
1. **rolling strategy**: How is atomic swap implemented?
2. **restart strategy**: How long is downtime? What if start fails?
3. **signal strategy**: Which signals are supported? (SIGHUP/SIGTERM/etc)
4. **none strategy**: Is cache update atomic?
5. **Rollback trigger**: What causes rollback (health check failure, timeout)?

---

## State Persistence

### Persisted State (Spec Claims):
- Current container mappings
- Secret rotation history
- Failed rotation state
- Lock files (for distributed coordination)

### Persistence Location Questions:
- **State directory**: `/var/lib/dso/` or `/run/dso/`?
- **Format**: JSON, YAML, or binary?
- **Atomic writes**: How are state updates made atomic?
- **Corruption recovery**: How is corrupted state handled?

### Crash Recovery
**Spec claim**: "Crash Recovery — Agent restarts automatically recover orphaned containers and resume incomplete rotations"

**Questions**:
1. How are "orphaned containers" identified (containers with no mapped state)?
2. How are "incomplete rotations" resumed (rotating backward? forward)?
3. What if state file is corrupted?
4. How long after crash can recovery happen (immediate or on next rotation)?

---

## Lock Mechanism

### Distributed Lock Requirements:
- Prevent concurrent rotations of same secret
- Prevent two agents from rotating same container
- Support single-host DSO (no distributed consensus needed)

### Implementation Questions:
1. **Lock type**: File-based or distributed (Redis/etcd)?
2. **Lock location**: `/var/lib/dso/` or `/run/dso/`?
3. **Lock timeout**: How long can lock be held?
4. **Deadlock prevention**: How is lock timeout enforced?
5. **Lock contention**: What happens if rotation takes too long?

### Current Hypothesis:
- File-based locking (simplest for single-host)
- Lock file: `/var/lib/dso/<secret-name>.lock`
- Timeout: Likely configurable, default probably 30-60 seconds
- On timeout: Force release lock, log warning

---

## Health Checks

### Health Check Specification (from README):
- "Health check new container"
- Default timeout: 30 seconds (mentioned in config)
- Failure triggers rollback

### Health Check Method (Unknown):
1. **Docker health API**: Use container's `HEALTHCHECK` instruction?
2. **TCP probe**: Open connection to exposed port?
3. **HTTP probe**: GET request to health endpoint?
4. **Custom command**: Execute script in container?
5. **Simple wait**: Just wait N seconds then assume healthy?

### Health Check Implementation Questions:
1. Which method is used?
2. How many retry attempts?
3. Retry interval?
4. What if container has no health check defined?
5. Is health check configurable per-secret?

---

## Gaps/Issues Found

### Critical Gaps:
1. **❌ Rotation logic location unclear** - Not in standard `internal/rotation/` path
2. **❌ Implementation details missing** - How exactly is swap atomic?
3. **❌ Health check method unknown** - What makes container "healthy"?
4. **❌ State persistence format unknown** - JSON? YAML? Binary?
5. **❌ Lock mechanism not documented** - File-based or distributed?

### Missing Documentation:
1. Rotation state machine diagram
2. Rollback scenarios and recovery
3. Health check configuration
4. Lock timeout handling
5. Crash recovery procedure
6. Performance impact of rotation

### Unverified Claims:
1. ✅ "Zero-downtime rolling rotation completes in ~30 seconds" - No timing data
2. ✅ "Atomic swap" - Implementation not verified
3. ✅ "Deterministic rollback" - Rollback logic not located
4. ✅ "TCP Proxy re-routes traffic" - Proxy implementation not found yet

---

## Rotation State Machine (Hypothetical)

Based on spec, rotation lifecycle should be:

```
Idle
  ↓ [Secret change detected]
Waiting for lock
  ├─ Timeout? → Error (rollback if rotating)
  └─ Lock acquired → Rotating
Rotating
  ├─ Create new container → NewContainer
  ├─ Health check → Healthy/Unhealthy
  └─ On error → Rolling back
NewContainer
  ├─ Health check passes → Swapping
  └─ Health check fails → CleanupNew, Rollback
Swapping
  ├─ Swap containers → New is active
  ├─ Update DNS/proxy → TrafficRouted
  └─ On error → Rollback (swap back)
TrafficRouted
  ├─ All traffic shifted → Stopping
  └─ On error → Rollback
Stopping
  ├─ Stop old container → Stopped
  ├─ Remove from state → Idle
  └─ On error → Warn (old still running)
Rollback
  ├─ Reverse operations
  ├─ Restore previous state
  └─ Return to Idle
```

---

## Concurrency Safety

### Potential Race Conditions:
1. **Multiple rotation triggers**: If two rotation events arrive simultaneously
2. **Rotation + manual update**: If user updates config while rotation in progress
3. **Agent restart during rotation**: Crash with uncommitted state
4. **Lock timeout collision**: Multiple agents retry simultaneously

### Thread Safety Questions:
1. Is agent cache protected by mutex?
2. Are state updates atomic?
3. Is Docker API access serialized?
4. Can two goroutines call rotation simultaneously?

---

## Testing Gaps

### What Should Be Tested:
1. ✓ Each rotation strategy (rolling, restart, signal, none)
2. ✓ Health check pass/timeout/fail scenarios
3. ✓ Rollback on health check failure
4. ✓ Rollback on container creation failure
5. ✓ Rollback on swap failure
6. ✓ Concurrent rotation prevention
7. ✓ Lock timeout and force release
8. ✓ Crash during rotation → recovery
9. ✓ Performance (30-second completion time)
10. ✓ Edge cases (same secret used by multiple containers)

### Test Locations:
- Unit tests: `internal/rotation/*_test.go` (if location correct)
- Integration tests: Docker containers spun up
- Scenario tests: Failure injection for rollback

---

## Recommendations

1. **Locate rotation logic** - Find and document actual implementation
2. **Document state machine** - Create state diagram for rotation lifecycle
3. **Specify health check** - Document which health check method is used
4. **Document locking** - Explain lock mechanism and timeout
5. **Add crash recovery tests** - Verify recovery from interrupted rotation
6. **Add performance tests** - Verify 30-second rotation target
7. **Document rollback** - Show all rollback scenarios and recovery
8. **Add concurrency tests** - Verify lock prevents race conditions
