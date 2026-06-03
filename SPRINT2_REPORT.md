# DSO Stability Hardening — Sprint 2 Report

**Date:** 2026-06-03  
**Scope:** L5 (Shared restart helper), H2 (goto elimination), H3 (StateTracker container IDs), H5 (sync.Map TTL eviction)  
**Validation:** `go build ./...` ✅ · `go vet ./...` ✅ · `go test ./... -race` ✅

---

## 1. Verification Summary

### L5 — Extract `executeSimpleRestart` (Code Quality)

| Field | Detail |
|-------|--------|
| **Status** | ✅ Fixed |
| **Evidence** | `internal/watcher/controller.go`: the rolling strategy's fallback error-handler (previously ~lines 707–751) contained a full ~40-line inline container-recreation block that duplicated the outer restart path without the TAR injection, health check, exec probe, proxy swap, or 3-retry rollback. Confirmed via code review before implementation. |
| **Fix** | Extracted `func (r *ReloaderController) executeSimpleRestart(ctx, containerID, envOverrides)` — a self-contained helper that: inspects the container, renames it to a temp name, creates a new container with env overrides applied, stops the old one, starts the new one, then removes the old one. The fallback path is now a single call: `r.executeSimpleRestart(ctx, target.ID, newEnvs)`. The primary rotation path (`TriggerReload`) retains its full logic unchanged. |
| **Tests** | `TestExecuteSimpleRestart_HappyPath` — verifies new container started and old removed. `TestExecuteSimpleRestart_InspectFailure` — verifies error propagated. `TestExecuteSimpleRestart_StartFailureRollsBack` — verifies original container restarted when new container fails to start. |
| **Risk** | Low. Fallback path was previously untested dead code; behavior is preserved. The helper is used only from the rolling strategy error handler. |

---

### H2 — Eliminate `goto` labels (High Severity)

| Field | Detail |
|-------|--------|
| **Status** | ✅ Fixed |
| **Evidence** | `internal/watcher/controller.go` contained `ROLLBACK:` and `FINISH:` labels with three `goto ROLLBACK` jumps (ContainerStart failure, WaitHealthy failure, ExecProbe failure) and one `goto FINISH` jump (success path). Go's `goto` across variable declarations is undefined behavior and makes control flow unanalyzable by the race detector and static tools. |
| **Fix** | Extracted `func (r *ReloaderController) executeRollback(ctx, createdID, originalID, originalName, serviceName)` — encapsulates the 3-attempt retry loop that: removes the new container, renames the original back, starts the original. Each failure site now calls `r.executeRollback(...)` then `releaseLock(); return`. The `FINISH:` label (which contained only `releaseLock()`) was eliminated by calling `releaseLock()` directly at the success site before `return`. Both `ROLLBACK:` and `FINISH:` labels are gone from the file. |
| **Tests** | `TestExecuteRollback_SuccessOnFirstAttempt` — verifies no degraded state on first-attempt success. `TestExecuteRollback_AllAttemptsFail` — verifies `degraded` map populated after all 3 retries fail. `TestExecuteRollback_SucceedsOnThirdAttempt` — verifies retry loop counts and no degraded state on eventual success. |
| **Risk** | Medium at implementation time (goto elimination touches 4 control-flow sites); low post-validation. Race detector pass confirms no new data races introduced. |

---

### H3 — Thread real container IDs into `StateTracker` (High Severity)

| Field | Detail |
|-------|--------|
| **Status** | ✅ Fixed |
| **Evidence** | `internal/agent/trigger.go` `ExecuteRotation` called `t.StateTracker.StartRotation(providerName, secretName, "", "")`, `t.StateTracker.MarkRollback(providerName, secretName, "")`, and `t.StateTracker.CompleteRotation(providerName, secretName, "")` — all three with empty container ID strings. `StateTracker` persists rotation state for crash recovery; empty IDs make recovery impossible since there is no target to restart. Confirmed via direct code inspection. |
| **Fix** | Added `func (t *TriggerEngine) collectContainerIDsForSecret(secretName string) string` — ranges over `t.Reloader.Targets` (a `sync.Map` of `*watcher.TargetContainer`), collects the `.ID` field of every container whose `.Secrets` slice contains the given secret name, and returns all IDs joined with commas. The result is captured before Docker operations begin and threaded into all three `StateTracker` calls. Added `"strings"` to the import block. |
| **Tests** | `TestCollectContainerIDsForSecret_OneMatch` — single matching container returns its ID. `TestCollectContainerIDsForSecret_MultipleContainersSameSecret` — two containers sharing a secret both appear in the result. `TestCollectContainerIDsForSecret_NoMatch` — unregistered secret returns empty string. `TestCollectContainerIDsForSecret_NilReloader` — nil Reloader returns empty string without panic. |
| **Risk** | Low. `collectContainerIDsForSecret` is read-only over the `Targets` sync.Map. The result is informational for crash recovery; if it returns empty (nil Reloader, no matching containers), behavior degrades gracefully to the previous state rather than crashing. |

---

### H5 — TTL eviction for `recentDSOActions` sync.Map (High Severity)

| Field | Detail |
|-------|--------|
| **Status** | ✅ Fixed |
| **Evidence** | `internal/watcher/event_handler.go` `RecordDSOAction` called `recentDSOActions.Store(identifier, time.Now())` on every rotation action, with no corresponding delete or expiry. In long-running deployments with frequent rotations, this map grows without bound, accumulating one entry per unique container ID or compose project name indefinitely. Confirmed no purge path existed in the original file. |
| **Fix** | Added `const actionEntryTTL = 5 * time.Minute` (safely larger than the 15-second ignore window in `ProcessEvent`). Added `purgeStaleActions()` — ranges the map and deletes entries whose stored timestamp is older than `actionEntryTTL`. Added `startActionPurge()` — launches a goroutine with a 1-minute ticker calling `purgeStaleActions()`. Added `var purgeOnce sync.Once` — `RecordDSOAction` calls `purgeOnce.Do(startActionPurge)` on first write, so the goroutine starts lazily without requiring a constructor or context injection. |
| **Tests** | `TestPurgeStaleActions_RemovesOldEntries` — stale entry purged, fresh entry retained. `TestPurgeStaleActions_EmptyMap` — no panic on empty map. `TestRecordDSOAction_StoresEntry` — timestamp stored within last second. `TestRecordDSOAction_Concurrent` — 50 concurrent writers plus concurrent purge; race detector passes. `TestPurgeStaleActions_OnlyPurgesExpired` — boundary test: entry at TTL+100ms purged, entry at TTL-30s retained. |
| **Risk** | Low. The purge is conservative (TTL = 5 min vs ignore window = 15 sec). The sync.Once ensures the goroutine starts exactly once. The goroutine is intentionally not context-cancellable — it is acceptable for a daemon-lifetime background task; the ticker fires infrequently and exits cleanly if the process exits. |

---

## 2. Rotation Concurrency Audit

### 2.1 Same Secret Rotated Twice Concurrently

**Mechanism:** `TriggerEngine.ExecuteRotation` calls `t.LockManager.AcquireLock(secretName)` before any Docker operation. `LockManager` (`internal/rotation/lock_manager.go`) holds a per-secret `sync.Mutex` and tracks lock ownership via a `map[string]bool`. `AcquireLock` returns an error after a 5-second timeout if the lock is not available.

**Assessment:** Safe. The second rotation for the same secret blocks for up to 5 seconds then returns an error that is logged and surfaced. No two rotations for the same secret can execute Docker operations concurrently.

**Gap:** The 5-second timeout is hardcoded. If rotation regularly takes longer (large images, slow networks), the second attempt always fails rather than queuing. Recommend making the timeout configurable (Sprint 3 candidate).

---

### 2.2 Webhook and Polling Overlap on Same Secret

**Mechanism:** Both webhook events (`internal/server/rest.go`) and polling triggers (`internal/agent/agent.go` poll loop) call `TriggerEngine.ExecuteRotation` with the same arguments when a secret version changes.

**Assessment:** Safe. Both paths converge on the same `LockManager.AcquireLock(secretName)` call, so only one rotation proceeds at a time. The 15-second debounce in `recentDSOActions` additionally suppresses the resulting Docker events from the first rotation being misread as an external trigger for the second.

**Gap:** If polling detects a version change and enqueues a rotation while a webhook-triggered rotation is mid-flight and the polling interval is shorter than rotation duration, the poll will fail to acquire the lock and silently drop the rotation request. The secret will remain correctly rotated by the webhook trigger, but the polling failure is not surfaced in metrics. Recommend adding a counter for lock acquisition timeouts (Sprint 3).

---

### 2.3 Rollback During Active Rotation

**Mechanism:** `executeRollback` (post-H2 fix) runs synchronously inside the goroutine that holds the `rotationLocks` entry for the service name (`ReloaderController.rotationLocks`, a `sync.Map` used to prevent concurrent `TriggerReload` calls on the same service). No new goroutine is spawned for rollback.

**Assessment:** Safe. The lock on the service is held for the entire duration including rollback. A new rotation for the same service cannot begin until rollback completes. `executeRollback` uses its own context (passed from the caller) and does not touch the `LockManager` — there is no nesting between the two lock layers.

**Gap:** If rollback itself takes > the `LockManager` timeout (5 seconds), a concurrent rotation attempt on the same secret will time out and drop. This is acceptable behavior — better to drop than to double-rotate during an unstable rollback.

---

### 2.4 StateTracker Consistency Under Concurrent Rotations

**Mechanism:** `StateTracker` (`internal/agent/state_tracker.go`) protects its state map with a `sync.RWMutex`. `StartRotation`, `MarkRollback`, and `CompleteRotation` all take the write lock.

**Assessment:** Safe. Concurrent rotations for different secrets serialize on the StateTracker write lock, which is held briefly (map write + optional disk flush). The H3 fix ensures the stored container IDs are collected before `StartRotation` is called, so the state written to disk contains actionable recovery information.

**Gap:** `StateTracker` does not detect if a rotation for the same `(provider, secret)` pair is already in-flight when `StartRotation` is called — it overwrites silently. In practice, `LockManager` prevents this, but the StateTracker has no independent guard. If `LockManager` is bypassed (e.g., direct API call), the state entry for the in-flight rotation is clobbered. Low risk in current architecture; document the invariant (Sprint 3).

---

### 2.5 Two-Level Locking Summary

DSO uses two independent lock scopes:

- **`LockManager` (per-secret, in `TriggerEngine`):** Serializes the decision to rotate a given secret. Timeout: 5 seconds.
- **`rotationLocks` sync.Map (per-service/container, in `ReloaderController`):** Serializes Docker operations on a given container. No timeout — callers return immediately on contention.

The two layers guard different granularities. A rotation of secret `db-pass` affecting services `app` and `worker` acquires one `LockManager` lock on `db-pass` and two `rotationLocks` entries (`app`, `worker`). There is no nesting of these two lock types within a single call path, so deadlock is not possible.

---

## 3. Files Changed

| File | Change Type | Lines (post-fix) | Delta |
|------|------------|-----------------|-------|
| `internal/watcher/controller.go` | Modified | 1086 | +~120 (added `executeRollback`, `executeSimpleRestart`, removed inline goto blocks) |
| `internal/watcher/controller_test.go` | Modified | 952 | +~200 (6 new test functions + helpers) |
| `internal/watcher/event_handler.go` | Modified | 100 | +~40 (TTL const, `purgeStaleActions`, `startActionPurge`, `purgeOnce`) |
| `internal/watcher/event_handler_test.go` | New | 106 | +106 (5 new test functions) |
| `internal/agent/trigger.go` | Modified | 484 | +~30 (`collectContainerIDsForSecret`, updated 3 StateTracker call sites) |
| `internal/agent/trigger_test.go` | Modified | 612 | +~120 (4 new test functions + 2 helpers) |

---

## 4. Validation Results

```
go build ./...                          PASS (no output)
go vet ./...                            PASS (no output)
go test ./internal/watcher/...          ok  (all subtests pass)
go test ./internal/agent/...            ok  (all subtests pass)
go test ./internal/watcher/... -race    PASS (zero races detected)
go test ./internal/agent/... -race      PASS (zero races detected)
```

All Sprint 2 regression tests pass with the race detector enabled.

---

## 5. Risk Assessment

| Finding | Implementation Risk | Regression Risk | Residual Risk |
|---------|--------------------|-----------------|----|
| L5 — executeSimpleRestart | Low | Low | None — fallback path was previously untested |
| H2 — goto elimination | Medium (4 control-flow sites) | Low — race detector validates | None post-validation |
| H3 — container ID threading | Low | Low | Minimal — graceful degradation if IDs unavailable |
| H5 — sync.Map TTL | Low | Low | Goroutine non-cancellable (acceptable for daemon) |

No breaking API changes. No behavior changes outside Sprint 2 scope. Backward compatible.

---

## 6. Sprint 3 Recommendations

### ~~6.1 ProviderSupervisor — Integrate (extend)~~ → **DELETED**

**Superseded by Sprint 3 deletion audit.** Full call-graph analysis confirmed zero production call sites. Functionality fully covered by `SecretStoreManager` inline tracking (`ConsecFails`/`MaxFailures`). Five permanently-zero Prometheus metrics (`dso_provider_restarts_total`, `_crashes_total`, `_uptime_seconds`, `_health_status`, `_heartbeat_failures_total`) removed from binary. Files deleted: `internal/providers/supervisor.go`, `internal/providers/supervisor_test.go`.

---

### ~~6.2 CircuitBreaker — Integrate (minor fix)~~ → **DELETED**

**Superseded by Sprint 3 deletion audit.** Zero production call sites confirmed. Half-open transition race (`IsAvailable` TOCTOU on `atomic.Value`, not CAS) confirmed non-fixable without rewrite. `SecretStoreManager` inline failure counting provides equivalent protection. Files deleted: `internal/providers/circuit_breaker.go`, `internal/providers/circuit_breaker_test.go`.

---

### 6.3 PluginVerifier — **Integrate (complete stub)**

**Location:** `internal/providers/plugin_verifier.go`

**Current state:** Hash registration, SHA256 file hashing, manifest file loading, and `VerifyPluginBinary` are fully implemented and production-quality. `LoadTrustedHashesFromFile` implements its own line/field parsing rather than using `bufio.Scanner` and `strings.Cut`, but is correct.

**Gap:** `VerifyPluginSignature` is a stub — it reads the signature file and certificate but does not verify the signature. The comment says "implement using crypto/x509 for production." If this method is called in production code paths, it silently passes all plugins regardless of signature validity.

**Gap:** `trustedHashes` is a plain `map[string]string` with no mutex. If `RegisterTrustedHash` and `VerifyPluginBinary` are called concurrently (e.g., dynamic plugin registration during operation), this is a data race.

**Recommendation:** Integrate after: (1) either implementing `VerifyPluginSignature` using `crypto/ecdsa` or marking it `NotImplemented` and gating callers, (2) protecting `trustedHashes` with a `sync.RWMutex`. Do not ship with the silent stub in a code path that claims to verify signatures. Effort: ~4 hours for proper ECDSA implementation, ~1 hour for stub gating.

---

### ~~6.4 ZombieReaper — Rewrite (current implementation non-functional)~~ → **DELETED**

**Superseded by Sprint 3 deletion audit.** Non-functional on every code path: `killChildProcesses` discarded `pgrep` output and returned nil unconditionally; `KillProcessByPID` created zombies (missing `process.Wait()`); `countZombies` used `ps aux` unavailable in Alpine. Root problem solved by `hashicorp/go-plugin`'s `Kill()` which calls `Wait` internally — `SecretStoreManager.Shutdown()` already invokes it. Files deleted: `internal/providers/zombie_reaper.go`, `internal/providers/zombie_reaper_test.go`.

---

### ~~6.5 RecoveryManager — Integrate (minor gaps)~~ → **DELETED**

**Superseded by Sprint 3 deletion audit.** `internal/daemon` package had zero importers — never compiled into the main binary. `agent.go` inline reconnect loop (lines 82–135) covers equivalent behavior and is already tested. Latent data race on `consecutiveFailures` (mixed mutex/atomic: `MarkHealthy` locks + `atomic.StoreInt32`, `MarkFailure` bare `atomic.AddInt32`) eliminated by deletion rather than fixed. Files deleted: `internal/daemon/recovery.go`, `internal/daemon/recovery_test.go`. Directory `internal/daemon/` removed entirely.

---

## 7. Sprint 3 Priority Order

| Priority | Component | Action | Effort |
|----------|-----------|--------|--------|
| ~~P0~~ | ~~ZombieReaper~~ | ~~Rewrite~~ | ~~1 day~~ · **DELETED** |
| ~~P1~~ | ~~CircuitBreaker~~ | ~~Fix half-open race + consolidate mutexes~~ | ~~3 hrs~~ · **DELETED** |
| ~~P1~~ | ~~RecoveryManager~~ | ~~Fix mixed atomic/mutex; wire re-subscription~~ | ~~2 hrs~~ · **DELETED** |
| P2 | PluginVerifier | Implement or gate signature verification; add mutex | 4 hrs |
| ~~P2~~ | ~~ProviderSupervisor~~ | ~~Fix jitter; fix health-on-restart~~ | ~~2 hrs~~ · **DELETED** |
| P3 | LockManager | Make acquisition timeout configurable | 1 hr |
| P3 | StateTracker | Document/enforce invariant: no concurrent StartRotation for same key | 1 hr |
