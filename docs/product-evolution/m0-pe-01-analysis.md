# M0 — PE-01 Root-Cause Analysis: Dropped Updates and False Success

**Date:** 2026-11-08 · **Status:** analysis only, nothing changed · Labels as defined in `m0-baseline-and-reconciliation.md`.

## 1. Verdict

**Confirmed defect.** A changed secret value (including a revocation) can be permanently left undelivered while DSO records it as applied, and DSO can emit `RotationSucceeded` when zero containers were rotated. Two independent mechanisms were reproduced by unit probes; a third is inferred.

## 2. The delivery path (as implemented)

```
StartPolling goroutine per (provider, secret)          trigger.go:404-487
  GetSecretWithContext(t.ctx)  (no per-call deadline)  :449-453
  ExecuteRotation(provider, secret, data, sec)         :480
    newHash vs secretHashes[key]  → equal ⇒ return     :257-274
    lastRotations[key] < 30s     ⇒ return (hash NOT stored → retried next poll)  :277-282
    secretHashes[key] = newHash   ← "applied" recorded BEFORE any outcome        :284
    Cache.Set(key, data)                                :290-301
    go func():
      strategy "none" ⇒ return                          :328-331
      LockManager.AcquireLock(secretName, 5s)           :338-342  fail ⇒ return (hash kept)
      defer ReleaseLock   (fires when TriggerReload returns, not when work ends — PE-02)  :343
      StateTracker.StartRotation                        :353-357
      Reloader.TriggerReload(ctx, secret, onComplete)   :390
TriggerReload                                           controller.go:754-1615
  for each target using secret:
     matchedCount++                                     :796
     inspect → serviceName = compose service label      :799-805
     rotationLocks[serviceName] busy (<5min) ⇒ skip, Debug log only   :808-816
     strategy: signal (sync, errors swallowed :893-902) | rolling (async :1078) | restart (async :1188) | compose restart (async, dedup by ComposePath :836-888, :1504-1565)
  matchedCount == 0 ⇒ return error                      :1567-1590
  go { asyncWG.Wait(); onComplete(asyncFirstErr) }      :1594-1612   ← nil if nothing ran
onComplete(nil) ⇒ StateTracker.CompleteRotation + emit RotationSucceeded   trigger.go:382-387
onComplete(err) / TriggerReload err ⇒ secretHashes.Delete(key) (retry)      :372, :394
```

There is **no queue**: a request is either dispatched immediately or abandoned. There is **no desired-vs-applied representation**: the only state is the in-memory `secretHashes` map (`trigger.go:29`), which conflates "last value seen from the provider" with "value running in containers". `StateTracker` (`internal/agent/state_tracker.go:15-24`) records per-secret rotation *attempts* (status `in_progress|completed|rollback_required|…`), not versions, and is optional (`trigger.go:119-125` continues with `nil`).

## 3. Where an update is dropped

| # | Mechanism | Evidence | Label |
|---|---|---|---|
| D1 | **Secret-lock timeout.** Hash stored at `:284`; goroutine fails `AcquireLock` within 5s and returns at `:341` with no hash rollback, no StateTracker record, no event. Every later poll with the same provider value hits `oldHash == newHash` (`:267`) and is skipped forever. | `trigger.go:284,338-342` | **Confirmed (probe)** — `TestM0Probe_PE01_LockTimeoutLosesUpdate` |
| D2 | **Busy service ⇒ silent skip ⇒ false success.** A target whose service lock is held (<5 min) is skipped with a Debug log (`controller.go:814-815`) but still counted in `matchedCount` (`:796`), so the "no containers" error at `:1589` is not returned. If every matched target was skipped, no async job exists, `asyncWG.Wait()` returns at once and `onComplete(nil)` fires (`:1606-1610`) ⇒ `CompleteRotation` + `RotationSucceeded` (`trigger.go:382-387`). The hash stays stored ⇒ the change is never retried. If only *some* targets were skipped, the others' outcome is reported for all and the skipped ones are still never retried. | `controller.go:796,808-816,1594-1612`; `trigger.go:382-387` | **Confirmed (probe)** — `TestM0Probe_PE01_BusyServiceReportsSuccess` |
| D3 | **Cross-project lock collision (PE-30)** makes D2 reachable without any concurrency inside one project: the lock key is the bare service name. | `controller.go:799-805,819` | **Confirmed (probe)** (same probe uses two projects) |
| D4 | **Strategy `none` / `signal`.** `none` stores the hash and returns (`:328-331`) — by design but recorded as "applied". `signal` swallows `ContainerKill` errors (`controller.go:893-902`) and reports success. | Code | Confirmed (code); `none` is intended, `signal` is a judgement call |
| D5 | **Restart path file-secret failure (PE-33)** logged only, container started, success reported. | `controller.go:1393-1395` | Confirmed (code) |
| D6 | **Concurrent callers of `ExecuteRotation` (PE-31).** Poll, webhook and IPC reconciliation can interleave between Load (`:263`) and Store (`:284`). Possible outcomes: duplicate dispatch for one value, or `secretHashes` holding value A's hash while `Cache` holds value B ⇒ B is deployed but A is recorded (or vice versa) and the next poll compares against the wrong baseline. | `trigger.go:262-301`; callers `:480`, `:524`, `server.go:147` | Inferred — needs `-race` stress test |

**Not a drop (for the record):** the 30s cooldown returns *before* storing the hash (`:277-282`), so the next poll retries; `onComplete(err)` and a `TriggerReload` error delete the hash (`:372`, `:394`), so failures are retried. These paths are correct.

## 4. Concurrency picture for the "revocation" scenario

1. t=0: provider value v1 → v2 (routine rotation). Poll stores hash(v2), dispatches; service S starts a 60–90 s rolling rotation (health timeout default 60s, `controller.go:985`, plus drain). The secret lock is released as soon as `TriggerReload` returns (PE-02), the service lock stays held.
2. t=40s: v2 is found compromised; provider value → v3. Next poll (or webhook) sees hash(v3) ≠ hash(v2), cooldown passed ⇒ stores hash(v3), dispatches.
3. `TriggerReload` finds S's lock busy ⇒ skip ⇒ `onComplete(nil)` ⇒ **`RotationSucceeded` for v3**.
4. Rolling rotation of v2 completes. Containers run **v2 (the revoked value)** indefinitely; every later poll returns v3 = stored hash ⇒ no action. Only a *further* provider change or an agent restart (PE-03 baseline loss — accidental recovery) delivers v3.

Can the system report success while real state is stale? **Yes — D2 and D4/D5 do exactly that; D1 records "applied" with no event at all.**

## 5. Root causes (design level)

1. **One variable for two facts:** `secretHashes` is both "observed desired value" and "applied value". It must be split into *desired* (advances on observation) and *applied* (advances only on a verified per-container outcome).
2. **Dispatch-or-drop instead of queue:** busy ⇒ skip. There is no pending slot per service to coalesce into.
3. **Success computed from "absence of errors among things that ran"** rather than "every targeted container reached the desired version".
4. **Lock scopes are wrong:** the secret lock covers dispatch only (PE-02); the service lock covers a service name across projects (PE-30) and is time-reclaimed (PE-23).

## 6. Proposed regression tests (to land in M1, failing first)

All names are proposals; none was added to the repository. Unit tests use the existing mock-Docker helper (`internal/watcher/controller_test.go:709-727`); integration tests use the Docker CI lane (`m0-docker-ci-plan.md`).

| Test | Level | Arrange / Act | Assert (must FAIL today) |
|---|---|---|---|
| `TestTriggerReload_AllTargetsBusy_ReportsNotApplied` | unit (watcher) | Target uses secret; service lock pre-held | `onComplete` receives a non-nil "deferred/not applied" outcome (or the request is queued) — today `nil` |
| `TestTriggerReload_ServiceLockScopedToProject` | unit (watcher) | Same service name in projects A and B; A locked | B is rotated (or queued), not skipped |
| `TestExecuteRotation_LockTimeout_DoesNotAdvanceApplied` | unit (agent) | Secret lock held >5 s | Applied state unchanged; next poll with same value re-dispatches |
| `TestExecuteRotation_ConcurrentCallers_SingleDispatchPerValue` | unit, `-race` | 50 goroutines call with v2/v3 interleaved | Exactly one dispatch per distinct value in arrival order; final desired = last observed value; no race report |
| `TestRotation_ChangeDuringInFlightRolling_EndsOnLatest` | **integration (Docker)** | nginx + HEALTHCHECK with slow start; fake provider v1→v2; at +5 s set v3 | Within bounded time every target's env/file = v3; ≤2 rotations executed; **no** `RotationSucceeded` emitted for a dispatch that rotated 0 containers |
| `TestRotation_Revocation_NoFalseSuccess` | integration | As above, capture `observability.EventStream` | Each `RotationSucceeded` corresponds to ≥1 container verified on that version |

Acceptance for the M1 fix (matches `implementation-backlog.md` BL-03 and `security-improvement-plan.md` S-3): 3 changes during an in-flight rolling rotation ⇒ 100% of targets on the last value, ≤2 rotations executed, 0 success events for no-op dispatches.

## 7. Fix direction (for M1 design approval; not implemented)

- Split state: `desired[secret] = {version|sha256, observed_at}` (in memory + ledger, see `m0-pe-03-pe-05-design.md`) and `applied[(project,service,container,secret)]`.
- Per-(project,service) worker with a single pending slot: a new request **replaces** the pending one (coalescing), never disappears. Lock held until terminal state; no time-based reclaim while the worker goroutine is alive.
- `ExecuteRotation` becomes "observe + enqueue" under a per-key mutex (fixes PE-31); the hash comparison moves to `desired != applied`.
- Outcome = per-container results; `RotationSucceeded` only if every targeted container is on the desired version; partial ⇒ `RotationPartial`/`Failed` with the list.
- Minimal interim option (if M2 queue is far): on busy-skip, return a sentinel error so `onComplete` deletes the hash and the next poll retries. This removes the *false success* and the *permanent drop* with ~10 lines, at the cost of retry latency = poll interval. **Recommended as the M1 PE-01 fix**, with the queue in M2 (owner's milestone table already places the queue in M2).

## Appendix A — probe sources (scratchpad, not in repo)

`watcher` probe (abridged): mock Docker returns an inspect with `com.docker.compose.service=db`, `project=projB`; `r.Targets.Store("c-projB", &TargetContainer{Strategy:"restart", Secrets:["db_password"]})`; `r.rotationLocks.Store("db", &lockInfo{startTime: time.Now()})`; `TriggerReload(...)` ⇒ returns nil and `onComplete(nil)`.

`agent` probe (abridged): `te := &TriggerEngine{Cache: NewSecretCache(time.Hour), Config: &config.Config{}, LockManager: rotation.NewLockManager("", logger), Reloader: nil}`; hold lock `db_password`; `ExecuteRotation("aws","db_password",v2,sec)`; sleep 6 s ⇒ `secretHashes["aws:db_password"] == ComputeHash(v2)`; after 31 s the same value is a no-op (nil `Reloader` is never reached, proving no dispatch).
