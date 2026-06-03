# DSO Codebase Audit — Full Report

**Mode**: Codebase Audit (Senior Engineer Mode 1)  
**Date**: June 2, 2026  
**Version**: v3.5.18  
**Coverage**: ~95 Go files across all packages

---

## Architecture Map

```
CLI (internal/cli/)
    │  IPC socket / REST
    ▼
Agent (internal/agent/trigger.go — TriggerEngine)
    ├── SecretStoreManager (internal/providers/store.go)
    │       └── LoadProvider (pkg/provider/load.go)
    │               └── ProviderRPC (pkg/provider/provider.go)  ← FIXED in v3.5.18
    ├── ReloaderController (internal/watcher/controller.go)
    │       └── RollingStrategy (internal/rotation/rolling_strategy.go)
    ├── SecretCache / LimitEnforcingCache (internal/agent/cache.go, cache_limits.go)
    ├── StateTracker (internal/agent/state_tracker.go)
    ├── BoundedEventQueue (internal/events/backpressure.go)
    └── TimeoutController (internal/agent/timeout_controller.go)

Support packages (built, NOT wired):
    CircuitBreaker    — deleted (Sprint 3 deletion audit)
    ProviderSupervisor — deleted (Sprint 3 deletion audit)
    PluginVerifier (internal/providers/plugin_verifier.go)     ← unused, integrate in Sprint 3
    ZombieReaper      — deleted (Sprint 3 deletion audit)
    RecoveryManager   — deleted (Sprint 3 deletion audit)

Local vault: pkg/vault/vault.go (AES-256-GCM, atomic writes, checksums — solid)
```

---

## Problem Inventory

Problems are ordered by severity. Each has: what it is, where it lives, why it matters.

---

### 🔴 CRITICAL — Data corruption or security bypass

---

#### C1. `LimitEnforcingCache.Delete` never deletes the secret

**File**: `internal/agent/cache_limits.go:161`

```go
func (lec *LimitEnforcingCache) Delete(key string) {
    if data, ok := lec.cache.Get(key); ok {
        lec.limiter.RemoveSecretSize(data)
    }
    // Note: cache doesn't have Delete, so we'd need to add it for proper cleanup
}
```

`SecretCache` (backed by `sync.Map`) does not expose a `Delete` method, so the secret stays in memory. Only the size accounting is decremented — which means the limiter now thinks it has more room than it actually has, future sets may incorrectly pass the capacity check, and stale secrets are never evicted. The self-admitted comment confirms this is known but unfixed.

**Fix**: Add `Delete(key string)` to `SecretCache`, call `lec.cache.Delete(key)` here.

---

#### C2. `DecryptProviderConfig` decrypts into a copy, never writes back

**File**: `pkg/config/crypto.go` — `DecryptProviderConfig` function

The function iterates over `pCfg.Auth.Params` (a `map[string]string`), decrypts each value into a local `decrypted` variable, but the map is iterated by value. The decrypted result is never stored back. Provider credentials remain encrypted at runtime when the agent tries to use them.

**Fix**: Store back after decryption: `pCfg.Auth.Params[k] = decrypted`.

---

#### C3. `TriggerEngine.ExecuteRotation` bypasses `LimitEnforcingCache`

**File**: `internal/agent/trigger.go`

The agent's rotation path calls `t.Cache.Set()` directly on the underlying `*SecretCache`, skipping `LimitEnforcingCache.SetWithLimits`. All the capacity and per-secret size checks in `cache_limits.go` are dead code on the hot path. A single oversized secret can consume unbounded memory.

**Fix**: Route `ExecuteRotation`'s cache writes through the `LimitEnforcingCache` wrapper.

---

### 🟠 HIGH — Logic bugs with production impact

---

#### H1. `TimeoutController.CreateSecretContext` leaks contexts on concurrent secrets

**File**: `internal/agent/timeout_controller.go:30`

```go
tc.timers[secretName] = cancel  // overwrites any existing cancel for this name
```

If two concurrent operations start for the same secret name (e.g., a polling tick and a webhook fire simultaneously), the second call overwrites the first `cancel` function without calling it. The first context and its goroutine leak until the underlying deadline fires. Under load with many webhook hits, this leaks goroutines.

**Fix**: Call the existing cancel before overwriting: `if existing, ok := tc.timers[secretName]; ok { existing() }`.

---

#### H2. `goto ROLLBACK / goto FINISH` across lock boundaries

**File**: `internal/watcher/controller.go` — `ExecuteRotation` goroutine

`goto` jumps in the rolling rotation path cross `defer releaseLock()` statements. In Go, `goto` does not skip deferred calls — but if the code path through the `goto` also reaches a `return` that has its own deferred unlock, the lock may be released twice. This is a latent data race / double-unlock condition that is hard to reason about and will corrupt the rotation state machine under specific failure sequences.

**Fix**: Replace `goto` with explicit `return` after calling a named `executeRollback()` helper.

---

#### H3. `StateTracker.StartRotation` called with empty container IDs

**File**: `internal/agent/trigger.go`

```go
t.StateTracker.StartRotation(providerName, secretName, "", "")
```

Both container ID arguments are empty strings. The state key is built from these values, so all rotations from the same provider/secret collide to the same key. Concurrent rotations for different containers of the same secret will overwrite each other's state — the tracker believes only one rotation is active when multiple may be running.

**Fix**: Pass the actual container IDs being rotated.

---

#### H4. `HandleWebhook` uses `context.Background()` instead of agent context

**File**: `internal/agent/trigger.go` — `HandleWebhook`

Webhook-triggered secret fetches use `context.Background()`. When the agent shuts down, these fetches are not cancelled — they block until the provider times out (potentially 30+ seconds), delaying clean shutdown and potentially causing data races as shutdown proceeds while fetches are still in flight.

**Fix**: Use `t.ctx` (the agent's root context) for all provider calls.

---

#### H5. `recentDSOActions sync.Map` grows without bound

**File**: `internal/watcher/controller.go`

Every DSO action (container start, stop, rotation) stores an entry in `recentDSOActions` by container ID. There is no purge, TTL, or eviction logic. On a long-running agent with frequent rotation, this map grows indefinitely. On a busy host rotating 100+ containers daily, this will accumulate thousands of entries.

**Fix**: Purge entries older than ~5 minutes using a background ticker or a `time.AfterFunc` on insert.

---

### 🟡 MEDIUM — Dead code and disconnected subsystems

These are complete, well-written subsystems that were built but never wired into the production code path. They carry ongoing maintenance cost with zero operational benefit.

---

#### ~~M1. `ProviderSupervisor` is never instantiated or used~~

**Resolved**: Deleted in Sprint 3 deletion audit. Functionality covered by `SecretStoreManager` inline tracking (`ConsecFails`/`MaxFailures` on `StoreEntry`). Five phantom Prometheus metrics (`dso_provider_restarts_total`, `_crashes_total`, `_uptime_seconds`, `_health_status`, `_heartbeat_failures_total`) removed from the binary.

---

#### ~~M2. `CircuitBreaker` is never used~~

**Resolved**: Deleted in Sprint 3 deletion audit. Half-open transition race (`IsAvailable` read-check-store, not CAS) confirmed non-fixable without rewrite. `SecretStoreManager` inline failure counting covers equivalent protection.

---

#### M3. `PluginVerifier` is never called — plugins load unverified

**File**: `internal/providers/plugin_verifier.go` / `pkg/provider/load.go`

`PluginVerifier` supports SHA256 hash manifests and binary verification. `LoadProvider` in `load.go` loads the plugin binary and executes it without calling the verifier. Any binary named like a provider binary will be executed.

**Action**: Call `PluginVerifier.VerifyPluginBinary` in `LoadProvider` before spawning the plugin process.

---

#### M4. `PluginVerifier.VerifyPluginSignature` is a non-functional stub

**File**: `internal/providers/plugin_verifier.go:137`

Reads a signature file and certificate, parses the certificate, then logs "verification skipped" and returns nil. No signature is verified. This creates a false sense of security.

**Action**: Implement using `crypto/ecdsa` or `ed25519`, or remove the method entirely and document hash-only verification.

---

#### ~~M5. `ZombieReaper.reaperLoop` cannot be stopped promptly~~

**Resolved**: Deleted in Sprint 3 deletion audit. `ZombieReaper` was non-functional — `killChildProcesses` was a no-op, `KillProcessByPID` created zombies (missing `process.Wait()`), and the problem is fully addressed by `hashicorp/go-plugin`'s `Kill()` which calls `Wait` internally.

---

#### ~~M6. `ZombieReaper.killChildProcesses` is a stub~~

**Resolved**: Deleted alongside ZombieReaper in Sprint 3 deletion audit. See M5.

---

#### ~~M7. `daemon/recovery.go:ResubscribeEvents` is a stub~~

**Resolved**: Deleted in Sprint 3 deletion audit. `RecoveryManager` was never imported by any production file (`internal/daemon` package had zero importers). `agent.go` inline reconnection loop covers equivalent behavior. Latent data race on `consecutiveFailures` (mixed mutex/atomic) eliminated by deletion.

---

#### M8. `SecretCache.maxSize/currentLen` fields are dead code

**File**: `internal/agent/cache.go`

Two fields declared (`maxSize int64`, `currentLen int64`), never read or written in `Set`, `Get`, or the cleanup goroutine. Capacity enforcement is entirely in `LimitEnforcingCache`, making these fields misleading dead weight.

**Action**: Remove the fields.

---

### 🔵 LOW — Code quality and maintainability

---

#### L1. ANSI escape codes hardcoded in `StrategyDecision.Report`

**File**: `internal/strategy/decision_engine.go:76`

```go
analyzerLog := fmt.Sprintf("\033[1;36m[DSO ANALYZER]\033[0m\n...")
```

This field is returned via API or written to structured logs. In JSON output or non-TTY environments, these appear as literal escape characters — garbage in Splunk, ELK, or any log aggregator.

**Fix**: Use plain text in `Report`; apply ANSI only when rendering to a TTY in the CLI layer.

---

#### L2. `BoundedEventQueue.Stop()` panics on double call

**File**: `internal/events/backpressure.go:222`

`close(beq.stopCh)` with no guard. If any caller calls `Stop()` twice (e.g., from a deferred call plus an error path), the process panics.

**Fix**: Wrap with `sync.Once`.

---

#### L3. `health_verifier.go` redundant check + non-ctx-aware sleep

**File**: `internal/rotation/health_verifier.go:30`

`VerifyContainerHealth` calls `containerExists` once before the retry loop, then calls it again inside the loop — duplicate work on the first iteration. More importantly, `time.Sleep(retryDelay)` inside the retry loop has no `ctx.Done()` check, so a cancelled context doesn't abort the health check promptly.

**Fix**: Remove the pre-loop check; use `select { case <-time.After(retryDelay): case <-ctx.Done(): return false }` inside the loop.

---

#### L4. `config.go:UnmarshalYAML` silently swallows decode errors

**File**: `pkg/config/config.go`

First-pass decode errors on `SecretMapping` are silently discarded (`_ = value.Decode(&v2)`). A malformed mapping silently falls through to the legacy format path with no warning. This makes misconfigured secrets very hard to debug.

**Fix**: Log a warning on the first-pass decode failure before attempting legacy fallback.

---

#### L5. Rolling fallback re-implements restart logic inline

**File**: `internal/watcher/controller.go`

When rolling strategy fails, the fallback path re-implements the entire restart logic inline (~60 lines duplicated). Combined with the `goto` control flow, this section is the highest complexity in the codebase and the most likely place for future bugs to hide.

**Fix**: Extract `executeRestart(containerID, envs)` helper; call it from both the explicit restart path and the rolling fallback.

---

## Refactoring Priority

| Priority | Fix | Effort | Impact |
|----------|-----|--------|--------|
| 1 | C2: `DecryptProviderConfig` write-back | 2 lines | Credentials actually work |
| 2 | C1: `SecretCache.Delete` + `LimitEnforcingCache` fix | ~10 lines | Memory safety |
| 3 | C3: Route `ExecuteRotation` through `LimitEnforcingCache` | ~5 lines | Cache limits actually enforced |
| 4 | H1: Fix leaked context in `TimeoutController` | ~3 lines | Stop goroutine leaks |
| 5 | H2: Replace `goto` with helper function | ~40 lines | Eliminate lock hazard |
| 6 | H3: Pass real container IDs to `StateTracker` | ~5 lines | State tracking accuracy |
| 7 | H4: Use `t.ctx` in `HandleWebhook` | 1 line | Clean shutdown |
| 8 | H5: Purge `recentDSOActions` with TTL | ~15 lines | Prevent memory leak |
| 9 | ~~M1+M2: Wire or delete `ProviderSupervisor`/`CircuitBreaker`~~ | — | ✅ Deleted (Sprint 3) |
| 10 | M3: Call `PluginVerifier` in `LoadProvider` | ~5 lines | Plugin supply chain security |
| 11 | L1: Strip ANSI from `Report` field | ~5 lines | Log sanity |
| 12 | L2: `sync.Once` in `BoundedEventQueue.Stop` | ~5 lines | Panic prevention |
| 13 | L3: Fix health check retry loop | ~10 lines | Correct ctx propagation |

---

## What's Solid

- `pkg/vault/vault.go` — atomic writes, AES-256-GCM, SHA256 integrity checksum, correct mutex discipline, path traversal protection. This is the best-written file in the codebase.
- `internal/rotation/rolling_strategy.go` — the atomic swap with verification, partial-rename recovery, and rollback on health failure is genuinely well-designed.
- `internal/events/backpressure.go` — worker pool with backpressure, Prometheus metrics, panic recovery per event. Good structure (minus the double-close bug).
- `internal/providers/store.go` — retry with exponential backoff, crypto-random jitter, stale connection detection.
- `pkg/provider/provider.go` — fixed in v3.5.18. The `WatchSecret` goroutine now has correct ctx cancellation and backoff.

---

## Quick Wins (can ship in one PR)

These are one-to-five line fixes with outsized impact:

```go
// C2: pkg/config/crypto.go — write decrypted value back
pCfg.Auth.Params[k] = decrypted

// C1: internal/agent/cache.go — add Delete to SecretCache
func (sc *SecretCache) Delete(key string) { sc.mu.Delete(key) }
// internal/agent/cache_limits.go — call it
lec.cache.Delete(key)

// H1: internal/agent/timeout_controller.go — cancel before overwrite
if existing, ok := tc.timers[secretName]; ok { existing() }
tc.timers[secretName] = cancel

// H4: internal/agent/trigger.go — use agent context in webhook
val, err := prov.GetSecret(t.ctx, name)  // not context.Background()

// L2: internal/events/backpressure.go — guard Stop
var stopOnce sync.Once
func (beq *BoundedEventQueue) Stop() {
    beq.stopOnce.Do(func() { close(beq.stopCh) })
    beq.wg.Wait()
}
```

---

*Report generated by senior-engineer skill — Mode 1: Codebase Audit*
