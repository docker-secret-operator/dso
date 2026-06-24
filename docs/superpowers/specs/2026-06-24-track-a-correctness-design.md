# Track A — Crash & Correctness Hardening

**Date:** 2026-06-24  
**Status:** Approved for implementation  
**Scope:** 11 bugs (5 CRITICAL, 6 HIGH) that cause process crashes, goroutine leaks, silent data corruption, or exploitable correctness failures  
**Approach:** 4 focused PRs, each independently reviewable and mergeable

---

## Background

A full four-agent codebase audit identified 5 CRITICAL and 18 HIGH severity issues. Track A addresses the subset that causes crashes, goroutine leaks, or exploitable correctness failures — bugs that can take down a production agent or corrupt secret state. Security hardening (Track B) and test coverage (Track C) are separate work streams.

---

## PR1 — Panic & Crash Fixes

**Files:** `pkg/config/crypto.go`, `internal/agent/trigger.go`, `internal/bootstrap/agent.go`, `internal/rotation/lock_manager.go`, `internal/agent/agent.go`, `internal/watcher/event_handler.go`, `internal/proxy/manager.go`

### C1 — `DecryptProviderConfig` never writes back

**Problem:** `provider` is a value copy from `config.Providers[name]`. Decrypted values are written to the copy and discarded. Every provider using encrypted credentials silently passes raw `enc:` ciphertext to external APIs.

**Fix:** Add `config.Providers[name] = provider` after the decryption loop in `DecryptProviderConfig`, mirroring the correct pattern in `EncryptProviderConfig` at line 144.

**Test:** Assert that after `DecryptProviderConfig`, the returned config's provider credential fields no longer carry the `enc:` prefix.

### C2 — `NewTriggerEngine` panics instead of returning error

**Problem:** `panic(fmt.Sprintf("rotation lock manager initialization failed: %v", err))` on line 67. Any unavailable lock directory at startup crashes the agent unrecoverably with no chance for the caller to handle gracefully.

**Fix:** Change signature to `func NewTriggerEngine(...) (*TriggerEngine, error)`. Replace `panic` with `return nil, fmt.Errorf("rotation lock manager initialization failed: %w", err)`. Update call sites in `internal/cli/agent.go` to check the error and call `log.Fatalf` or return — keeping the crash behaviour but making it explicit and handleable.

**Test:** Inject an invalid lock directory path; assert `NewTriggerEngine` returns a non-nil error rather than panicking.

### C3 — Unguarded type assertions in bootstrap

**Problem:** `providerConfig["name"].(string)` at lines 224–231 in `internal/bootstrap/agent.go` panics if any key is missing or holds a non-string value. `getProviderConfigWithMetadata` can return a map with unexpected types on any provider detection path.

**Fix:** Replace each bare `.(string)` with the two-value form:
```go
v, ok := providerConfig["name"].(string)
if !ok {
    return fmt.Errorf("provider config missing required field %q", "name")
}
```
Apply to all 4–5 assertion sites in sequence.

**Test:** Pass a map with a missing key and a map with a wrong-type value; assert an error is returned, not a panic.

### C5 — `ReleaseLock` panics on unlock of unlocked mutex

**Problem:** `ReleaseLock` calls `mutex.Unlock()` unconditionally. In `trigger.go:270`, `defer t.LockManager.ReleaseLock(secretName)` runs even when `AcquireLock` returned an error (mutex was never locked), producing `sync: unlock of unlocked mutex` panic.

**Fix:** Add `acquired map[string]bool` field to `LockManager`. Set `acquired[id] = true` only after `mutex.Lock()` succeeds in `AcquireLock`. In `ReleaseLock`, check `acquired[id]` before calling `mutex.Unlock()`; return early if false, then delete the entry.

**Test:** Call `ReleaseLock` for an ID that was never locked; assert no panic and no error.

### H11 — `containerID[:12]` panics on short IDs

**Problem:** Multiple sites across `internal/agent/agent.go`, `internal/watcher/event_handler.go`, and `internal/proxy/manager.go` slice container IDs with `id[:12]` without a length guard. Docker API does not guarantee minimum ID length.

**Fix:** Move the `shortID` helper that already exists in `internal/agent/recovery.go` to a shared `internal/util/ids.go`:
```go
func ShortID(id string) string {
    if len(id) < 12 {
        return id
    }
    return id[:12]
}
```
Replace all 7 bare slice sites across the 3 packages with `util.ShortID(id)`.

**Test:** Pass IDs of length 0, 6, 11, 12, and 64; assert `ShortID` never panics and returns the full string for inputs shorter than 12.

---

## PR2 — Goroutine & Context Fixes

**Files:** `internal/agent/server.go`, `internal/server/rest.go`, `internal/server/eventstore.go`, `internal/server/hub.go`

### C4 — Provider goroutine leak on fetch timeout

**Problem:** In `internal/agent/server.go:128`, `prov.GetSecret` is spawned in a goroutine *before* `fetchCtx` is created. The goroutine never receives a context, so it runs until the provider's own timeout (or forever for blocking providers). Under repeated timeouts, goroutines accumulate unboundedly.

**Fix:**
1. Create `fetchCtx, fetchCancel := context.WithTimeout(ctx, 30*time.Second)` before spawning the goroutine.
2. Pass `fetchCtx` into the goroutine: use `prov.GetSecretWithContext(fetchCtx, req.Secret)` where the interface supports it; otherwise use a `select { case <-fetchCtx.Done(): return; default: }` guard inside the goroutine.
3. Call `defer fetchCancel()` immediately after creating the context.

**Test:** Mock a provider that blocks indefinitely. Cancel the context. Assert the goroutine exits within 100ms and no goroutine leak is detected under `-race`.

### H12 — WebSocket concurrent write race

**Problem:** `handleEventWS` in `internal/server/rest.go:191-195` writes directly to `conn` via `conn.WriteJSON` before `writePump` starts. `*websocket.Conn` is not goroutine-safe. A concurrent ping tick from `writePump` racing the initial event loop causes undefined behaviour.

**Fix:** Remove all direct `conn.WriteJSON` calls in `handleEventWS`. Register the client with the hub first. Push historical events exclusively through `client.send` channel. Start `writePump` as the sole goroutine that calls `conn.WriteJSON`. Order: register → queue historical events → start writePump.

**Test:** Connect a WebSocket client; verify it receives historical events without a `-race` flag violation.

### H13 — EventStore mutex released before broadcast + blocking send

**Problem:** `s.mu.Unlock()` fires before `s.hub.broadcast <- e` in `internal/server/eventstore.go`. Two goroutines calling `Add` concurrently can reorder hub dispatch relative to in-memory insertion. The unbuffered broadcast channel blocks the entire event ingestion loop if the hub is slow.

**Fix (two sub-fixes):**
1. Move `s.hub.broadcast <- e` inside the mutex scope (before `s.mu.Unlock()`) to preserve ordering.
2. Make the hub's `broadcast` channel buffered (size 64). In `Add`, use `select { case s.hub.broadcast <- e: default: /* drop, increment counter */ }` to never block.
3. Add a `droppedEventsTotal` counter metric that increments on drop.

**Test:** Call `Add` from 10 concurrent goroutines; assert all events appear in the in-memory slice in insertion order and no deadlock occurs.

### H14 — Hub `readPump` goroutine leak on shutdown

**Problem:** `c.hub.unregister <- c` in `hub.go:109` blocks forever on an unbuffered channel if Hub's `Run` goroutine has exited. The `readPump` goroutine leaks permanently.

**Fix:** Thread a context into `readPump`. Replace the blocking send with:
```go
select {
case c.hub.unregister <- c:
case <-ctx.Done():
    return
}
```
Pass the hub's root context (already available) into `readPump` at the call site.

**Test:** Start a hub, connect a client, cancel the hub context, verify `readPump` exits within 100ms with no goroutine leak.

---

## PR3 — Security-Correctness Fixes

**Files:** `internal/injector/inject.go`, `internal/agent/recovery.go`, `internal/agent/multi_secret_updater.go`, `internal/cli/agent.go`

### H6 — Path traversal in file injection

**Problem:** `destPath := "/run/secrets/dso/" + fileName` with no sanitization. A crafted key like `../../etc/cron.d/evil` writes to an arbitrary path inside the container.

**Fix:**
```go
fileName = filepath.Base(fileName)
if fileName == "" || fileName == "." || strings.Contains(fileName, "/") {
    return fmt.Errorf("invalid secret file name %q", fileName)
}
destPath := "/run/secrets/dso/" + fileName
```
`filepath.Base` strips directory components. The subsequent checks reject edge cases (`"."`, empty string, any residual slash).

**Test:** Table-driven test with inputs `../../etc/passwd`, `../evil`, `valid-name`, `.`, `""`. Assert error for all traversal inputs and success for `valid-name`.

### H7 — Recovery matches unowned containers

**Problem:** `recoverSingleRotation` in `internal/agent/recovery.go:77-79` scans ALL containers by name pattern `_dso_backup_`/`_dso_new_`. On a shared Docker host this forcibly removes containers DSO does not own.

**Fix:** Replace the bare name-pattern scan with a label-filtered Docker API query using `label=dso.reloader=true`, the same filter used in `watcher/controller.go` and `injector/docker.go`. Only containers carrying this label are candidates for recovery.

**Test:** Mock two containers — one with `dso.reloader=true` and one without — both matching the backup name pattern. Assert only the labelled container is acted on.

### H8 — Local mutex in `ApplyTransaction` provides no protection

**Problem:** `var mu sync.Mutex; mu.Lock(); defer mu.Unlock()` in `internal/agent/multi_secret_updater.go:251` creates a mutex only the current goroutine can see. It provides zero mutual exclusion and misleads readers.

**Fix:** Remove the three lines entirely. Add a comment: `// Atomicity across validate+apply is guaranteed by the caller holding the project rotation lock in trigger.go. No additional lock needed here.`

**Test:** No behavioral change; verify under `-race` that concurrent `ApplyTransaction` calls on the same updater do not race (relying on the rotation lock).

### H15 — Signal context created after servers start

**Problem:** `signal.NotifyContext` in `internal/cli/agent.go:117` is created after several goroutines using `context.Background()` are already running. A SIGTERM received during startup is lost.

**Fix:** Move `ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)` and `defer stop()` to the first two lines of `RunE`, before any goroutine is spawned. Pass this `ctx` as the root context to all downstream components.

**Test:** Verify the command exits cleanly when SIGTERM is sent immediately after startup. Verify `stop()` is always called (no signal handler leak).

---

## PR4 — Silent Failure Fixes

**Files:** `internal/agent/cache.go`, `pkg/config/config.go`, `internal/cli/setup.go`, `internal/server/rest.go`

### H9 — `Cache.Clear` cannot zeroize secrets

**Problem:** Secrets are stored by content hash with no project association. `Cache.Clear` removes project entries but secret byte slices remain in memory indefinitely — secret material never cleared.

**Fix:** Add `projectSecrets map[string][]string` reverse index (project → []contentHash) to `Cache`. Populate it in `Set` inside the existing lock. In `Clear`, iterate the project's hashes, zero each `[]byte` in `c.secrets`, then delete from both `c.secrets` and `projectSecrets`.

**Test:** Set secrets for two projects. Clear one project. Assert its secret byte slices are zeroed and the other project's secrets are untouched.

### H10 — Non-deterministic default provider selection

**Problem:** `for name := range c.Providers { providerName = name; break }` in `pkg/config/config.go:276-281` selects a random provider. On restart a secret may silently route to a different provider.

**Fix:**
- If exactly one provider is configured: use it deterministically (no change in common case).
- If multiple providers are configured and `sec.Provider` is empty: return a validation error: `"secret %q: provider field is required when multiple providers are configured"`.

**Test:** Config with two providers and a secret with no `provider` field; assert `Validate()` returns an error.

### H16 — Silent `os.ReadFile`/`os.WriteFile` failures in setup

**Problem:** `configData, _ := os.ReadFile(configPath)` and `os.WriteFile(configPath, configData, 0664)` in `internal/cli/setup.go:219,255` discard errors. User config is silently lost on I/O failure.

**Fix:**
- `os.ReadFile`: handle error, log a warning `"could not back up existing config at %s: %v — proceeding without backup"`, set `configData = nil`.
- `os.WriteFile`: return `fmt.Errorf("failed to restore config to %s: %w", configPath, err)` — propagate to caller as a fatal setup error.

**Test:** Mock a read-only path; assert setup returns an error from the write path and logs a warning from the read path.

### H18 — Fabricated secret metadata in REST API

**Problem:** `handleListSecrets` in `internal/server/rest.go:344-350` hardcodes `LastSyncedAt: time.Now()`, constant `InjectionType`/`Version`, `Status: "synced"`. API returns false state to all callers.

**Fix:** Wire `handleListSecrets` to read from `SecretCache`:
- `LastSyncedAt`: use the cache entry's actual last-updated timestamp (add `UpdatedAt time.Time` field to cache item if not present).
- `InjectionType`: derive from whether the secret has env or file mappings.
- `RotationEnabled`/`AutoSyncEnabled`: read from the agent's config for that secret.
- `Status`: return `"synced"` only if the cache entry is fresh (within polling interval); otherwise `"stale"`.
- `Version`: omit (`omitempty`) until actually tracked.

**Test:** Assert that after a cache update, `GET /secrets` reflects the actual update timestamp, not `time.Now()` at request time.

---

## Implementation Order

Each PR is independent. Recommended merge order to minimize conflicts:

1. **PR1** first — establishes `util.ShortID`, removes panics, safest changes
2. **PR3** next — H15 signal context touches `cli/agent.go` which PR4 also touches; merge PR3 first
3. **PR2** — server package changes isolated from other PRs
4. **PR4** last — cache and config changes, depends on no other PR

---

## Success Criteria

- `go build ./...` passes with zero new warnings
- `go test -race ./...` passes with no new race conditions detected
- All new regression tests pass (red → green verified)
- No `panic` calls remain in constructor or rotation paths
- `go vet ./...` clean
