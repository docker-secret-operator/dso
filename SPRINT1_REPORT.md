# DSO Stability Hardening Sprint 1 — Implementation Report

**Date**: June 2, 2026  
**Engineer**: Senior Go Engineer (verification-first)  
**Version**: v3.5.18 → v3.5.19-rc  
**Test result**: `go test ./internal/... ./pkg/... -short` — **all pass**  
**Race detector**: `go test ./internal/agent/... ./internal/events/... ./pkg/config/... -race` — **zero races**

---

## Verification Summary

### C1 — LimitEnforcingCache.Delete does not delete secrets

**Status: CONFIRMED**

**Evidence**:
- `SecretCache.Delete()` exists at `internal/agent/cache.go:106-114` and correctly zeros + removes entries
- `LimitEnforcingCache.Delete()` at `internal/agent/cache_limits.go:161-166` calls `lec.cache.Get()` for size accounting then has a comment: *"cache doesn't have Delete, so we'd need to add it for proper cleanup"* — the comment is factually wrong; the method exists but was never called
- Net effect: secrets are evicted from the size counter but remain in the `sync.Map`-backed `items` map indefinitely

**Impact**:
- Stale secrets persist across rotations; old credentials remain accessible via `Get()`
- Size accounting underflows: `currentCacheSize` decrements but entries stay, allowing new secrets to pass capacity checks even when the cache is effectively full
- Memory grows unboundedly under continuous rotation

**Fix applied** (`internal/agent/cache_limits.go`):
```go
func (lec *LimitEnforcingCache) Delete(key string) {
    if data, ok := lec.cache.Get(key); ok {
        lec.limiter.RemoveSecretSize(data)
    }
    lec.cache.Delete(key)  // ← added
}
```

**Tests added** (`internal/agent/cache_limits_test.go`):
- `TestLimitEnforcingCache_Delete_RemovesEntry` — verifies entry gone after delete
- `TestLimitEnforcingCache_Delete_NonExistent` — safe no-op on missing key
- `TestLimitEnforcingCache_Delete_SizeAccounting` — counter returns to zero; re-add succeeds

---

### C2 — DecryptProviderConfig write-back bug

**Status: FALSE POSITIVE**

**Evidence**:
- The audit assumed `provider.Config[key] = decrypted` writes to a detached copy. This is incorrect for Go maps.
- `config.Providers` is `map[string]ProviderConfig`. When iterating `for _, provider := range config.Providers`, `provider` is a struct copy, but `provider.Config` and `provider.Auth.Params` are map headers — reference types. Mutations to map entries (`map[key] = value`) are visible through the original because both the copy and the original point to the same underlying hash table.
- `EncryptProviderConfig` writes back `config.Providers[name] = provider` because it may *assign a new map* (`provider.Auth.Params = make(...)`), a struct-field change that requires write-back. `DecryptProviderConfig` never creates new maps, only mutates existing entries — no write-back needed.
- **Proof**: `TestEncryptDecryptProviderConfig` in `pkg/config/crypto_test.go` (line 85-130) has existed and passed since before this sprint. It round-trips encrypt → decrypt and asserts the decrypted values are correct. If the write-back bug existed, this test would fail.

**Fix applied**: None required.

---

### C3 — Cache limits bypassed during rotation

**Status: CONFIRMED**

**Evidence**:
- `TriggerEngine` holds `Cache *SecretCache` (line 20, `internal/agent/trigger.go`)
- `ExecuteRotation` at line 240 calls `t.Cache.Set(cacheKey, secretData)` directly — bypasses `LimitEnforcingCache.SetWithLimits()` entirely
- `LimitEnforcingCache` is defined and functional but was never wired into `TriggerEngine`; the limit enforcement could never fire on the hot rotation path

**Impact**:
- A single oversized secret can exhaust memory; the configured limits have no effect during rotation
- Per-secret size limits are also bypassed, defeating the OOM protection

**Fix applied** (`internal/agent/trigger.go`):
```go
// New field on TriggerEngine:
LimitCache *LimitEnforcingCache  // optional; when set, writes go through limit enforcement

// New method:
func (t *TriggerEngine) SetLimitCache(lc *LimitEnforcingCache) { t.LimitCache = lc }

// In ExecuteRotation (replaces direct t.Cache.Set):
if t.LimitCache != nil {
    if err := t.LimitCache.SetWithLimits(cacheKey, secretData); err != nil {
        t.Logger.Error("Secret exceeds cache limits, rotation aborted", ...)
        t.secretHashes.Delete(cacheKey)  // allow retry on next poll
        return
    }
} else {
    t.Cache.Set(cacheKey, secretData)  // backward-compatible fallback
}
```

Backward-compatible: existing code that does not call `SetLimitCache` is unaffected.

**Tests added** (`internal/agent/cache_limits_test.go`):
- `TestExecuteRotation_CacheLimitEnforced` — oversized secret not cached; cache empty after rejected rotation
- `TestExecuteRotation_NormalSecretWithLimitCache` — normal secret cached correctly
- `TestExecuteRotation_NoLimitCache` — nil `LimitCache` → legacy `Cache.Set` path works

---

### H1 — TimeoutController context leak on concurrent secrets

**Status: CONFIRMED**

**Evidence**:
- `CreateSecretContext` at `internal/agent/timeout_controller.go:38`: `tc.timers[secretName] = cancel`
- When Op1 and Op2 call `CreateSecretContext` with the same `secretName`, Op2 overwrites `timers[name]` without calling Op1's cancel. Op1's context lives until its natural timeout (potentially 30+ seconds)
- Also identified: Op1's cleanup closure called `delete(tc.timers, secretName)` blindly, which would remove Op2's cancel from the map — breaking `CancelSecret()` for the new operation

**Fix applied** (`internal/agent/timeout_controller.go`):
- Added `timerEntry` struct with `cancel` + monotonic `gen` (uint64 generation counter)
- Before overwriting: `if existing, ok := tc.timers[secretName]; ok { existing.cancel() }`
- Cleanup closure guards the delete: only removes its own entry (`if stored.gen == gen`)

```go
type timerEntry struct {
    cancel context.CancelFunc
    gen    uint64
}

// In CreateSecretContext:
if existing, ok := tc.timers[secretName]; ok {
    existing.cancel()  // prevent leak
}
gen := atomic.AddUint64(&tc.nextGen, 1)
ctx, cancel := context.WithTimeout(parentCtx, timeout)
tc.timers[secretName] = timerEntry{cancel: cancel, gen: gen}

return ctx, func() {
    tc.mu.Lock()
    defer tc.mu.Unlock()
    cancel()
    if stored, ok := tc.timers[secretName]; ok && stored.gen == gen {
        delete(tc.timers, secretName)  // only if we are still the owner
    }
}
```

**Tests added** (`internal/agent/timeout_controller_test.go`):
- `TestTimeoutController_NoCancelLeak_H1` — first context cancelled when second is created with same name
- `TestTimeoutController_ConcurrentSameName_H1` — 20 goroutines racing on same key; no race detector hits

---

### H4 — Webhook path uses context.Background()

**Status: CONFIRMED**

**Evidence**:
- `HandleWebhook` at `internal/agent/trigger.go:419`: `val, err = provCtx.GetSecretWithContext(context.Background(), sec.Name)`
- Agent shutdown calls `t.cancel()` which cancels `t.ctx`. Webhook-triggered provider fetches using `context.Background()` are immune to this cancellation and block until the provider's own timeout (30s+)

**Fix applied** (`internal/agent/trigger.go`):
```go
// Before:
val, err = provCtx.GetSecretWithContext(context.Background(), sec.Name)

// After:
val, err = provCtx.GetSecretWithContext(t.ctx, sec.Name)
```

One-line fix. The polling path (`StartPolling`) already correctly used `t.ctx` — webhook was the only outlier.

---

### L2 — BoundedEventQueue.Stop() double-close panic

**Status: CONFIRMED**

**Evidence**:
- `Stop()` at `internal/events/backpressure.go:222`: `close(beq.stopCh)` with no guard
- Any caller that calls `Stop()` twice (deferred call + error path, or two concurrent shutdown goroutines) triggers a panic on a closed channel

**Fix applied** (`internal/events/backpressure.go`):
```go
// Added field:
stopOnce sync.Once

// Stop() before:
func (beq *BoundedEventQueue) Stop() {
    close(beq.stopCh)
    beq.wg.Wait()
}

// Stop() after:
func (beq *BoundedEventQueue) Stop() {
    beq.stopOnce.Do(func() { close(beq.stopCh) })
    beq.wg.Wait()
}
```

`wg.Wait()` on subsequent calls blocks until workers have drained, which is semantically correct.

**Tests added** (`internal/events/backpressure_test.go`):
- `TestBoundedEventQueue_Stop_Idempotent` — two sequential Stop() calls do not panic
- `TestBoundedEventQueue_Stop_ConcurrentSafe` — 10 goroutines call Stop() concurrently; race detector clean

---

## Files Changed

| File | Change |
|------|--------|
| `internal/agent/cache_limits.go` | Fix: add `lec.cache.Delete(key)` to `LimitEnforcingCache.Delete()` |
| `internal/agent/trigger.go` | Fix C3: add `LimitCache` field + `SetLimitCache()` + guarded write in `ExecuteRotation`; Fix H4: `context.Background()` → `t.ctx` in `HandleWebhook` |
| `internal/agent/timeout_controller.go` | Fix H1: `timerEntry` type with gen counter; call existing cancel before overwrite; owner-guarded cleanup delete |
| `internal/events/backpressure.go` | Fix L2: add `stopOnce sync.Once`; guard `Stop()` with `stopOnce.Do` |
| `internal/agent/cache_limits_test.go` | New: C1 and C3 regression tests |
| `internal/agent/timeout_controller_test.go` | Added: H1 regression tests (NoCancelLeak, ConcurrentSameName) |
| `internal/events/backpressure_test.go` | Added: L2 regression tests (Stop_Idempotent, Stop_ConcurrentSafe) |

---

## Diff Summary

```
internal/agent/cache_limits.go     +3  -2
internal/agent/trigger.go          +22 -2
internal/agent/timeout_controller.go +30 -15
internal/events/backpressure.go    +4  -3
internal/agent/cache_limits_test.go  +130 new
internal/agent/timeout_controller_test.go +47 added
internal/events/backpressure_test.go +40 added
```

**Total**: ~280 lines changed/added across 7 files. All changes are additive or minimal surgical fixes with no API breakage.

---

## Risk Assessment

| Finding | Change Risk | Rationale |
|---------|------------|-----------|
| C1 | Low | One-liner. `SecretCache.Delete()` was already tested and correct. |
| C2 | None | False positive — no code changed. |
| C3 | Low–Medium | Additive: new optional field. Nil check preserves all existing behavior. The only risk is callers that previously relied on unconstrained cache writes; they still work via the fallback path. |
| H1 | Low | Strictly safer: any scenario where the old code was correct still works (single-caller per key). The new behavior (cancel-on-overwrite) is the correct contract. |
| H4 | Low | One-line context substitution. `t.ctx` is a long-lived context cancelled only on `Stop()`. No change to happy-path behavior. |
| L2 | Low | `sync.Once` wrapping `close()` is the standard Go idiom for idempotent channel close. No behavior change for single-caller path. |

---

## Sprint 2 Recommendations

These findings were confirmed but deferred from Sprint 1:

### H2 — `goto ROLLBACK/FINISH` across lock boundaries (`internal/watcher/controller.go`)

Extract `executeRollback(ctx, containerID)` helper. Call it from both the explicit rollback path and the rolling fallback path. Replace all `goto` with explicit `return` after calling the helper. Eliminates the double-unlock risk and the ~60-line code duplication.

**Estimated effort**: 2-3 hours.

### H3 — `StateTracker.StartRotation` called with empty container IDs (`internal/agent/trigger.go:287`)

```go
t.StateTracker.StartRotation(providerName, secretName, "", "")
```

Pass the actual container IDs. Requires threading container IDs from the reloader's rotation result back to `ExecuteRotation`. May need a `RotationResult` return type from `TriggerReload`.

**Estimated effort**: 3-4 hours (involves controller.go interface changes).

### H5 — `recentDSOActions sync.Map` unbounded growth (`internal/watcher/controller.go`)

Replace the plain `sync.Map` with a time-bounded cache (e.g., store `time.Time` values, purge entries older than 5 minutes in a background goroutine or on write). Alternatively use a third-party TTL map.

**Estimated effort**: 1-2 hours.

### L5 — Rolling fallback duplicates restart logic (~60 lines) (`internal/watcher/controller.go`)

Extract `executeRestart(ctx, containerID, envs) error` shared helper. Fixes L5 and is a prerequisite for safely addressing H2 (the `goto` removal is much cleaner when the rollback logic is extracted first).

**Estimated effort**: 1-2 hours. Do this before H2.

**Recommended Sprint 2 order**: L5 → H2 → H3 → H5

---

*Sprint 1 complete. All 5 confirmed findings fixed. Tests pass. Race detector clean.*
