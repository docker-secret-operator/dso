# N19: managed file delivery through a startup gate (design and implementation note)

Status: **IMPLEMENTED — REAL DOCKER QUALIFICATION DEFERRED** on `feature/m2-development`, uncommitted at the time of writing. **N19 is NOT closed.**
The premise (Docker's archive endpoint may write into a created container's persistent writable layer instead of
the tmpfs) is **RUNTIME-UNVERIFIED**; this change removes the archive endpoint from the managed path and makes the
replacement fail closed. Real Docker qualification is deferred (no approved isolated Docker environment).
Labels: **[S]** read in source, **[T]** covered by a Docker-free test, **[U]** runtime-unverified.

Builds on `docs/audit/45-N19-STARTGATE-REUSE-REVIEW.md` (section 12 design F1+F2) and `docs/design/m0-04-recovery-design.md`
(section 6, N21). Owner authorisation (N19 + N22) covers exactly this: reuse the local-mode `dsofile://` gate idea and
the hardened exec injector inside the existing managed lifecycle. No sidecar, daemon, runtime, second journal or external
orchestration was added.

> Provenance note: citations of `docs/audit/...` in this document point to local, intentionally untracked working notes (repository policy, commit 1bf9f6c). Every requirement and decision this document relies on is stated here; the citations only record where it came from.

## 1. What changed in one paragraph

For config-driven `inject.type: file` secrets, the three managed paths (rolling `RollingStrategy.Execute`, restart
branch of `TriggerReload`, rolling fallback `executeSimpleRestart`) no longer call `StreamSecretToContainer`
(archive PUT). The candidate is **created with its ENTRYPOINT wrapped by a fixed shell gate**, started, **verified**
(identity, gate process, tmpfs at every destination), **delivered through the injector** (`injectOneFileOpts`: stdin,
atomic rename, `RequireTmpfs`), **probed**, its delivery evidence (keyed versions) is made **durable in the rotation-ID
journal**, and only then a release flag is written so the gate `exec`s the original process. Readiness, routing handoff,
retirement and ledger reconciliation are unchanged and still follow the existing managed-operation contract.
`StreamSecretToContainer` remains in the tree (used by the N19 diagnostic scenarios G-I and tests) but no managed path calls it.

## 2. Lifecycle phases and journal transitions

Phases are not interchangeable. Reused transitions are the existing `Tr*`; six are additive (no existing name or meaning
changed). Every row is journalled in the same rotation-ID record (`StateTracker.Record`); no second journal exists.

| # | Phase | Transition (write style) | Meaning, written when | Crash here: recovery treatment |
|---|---|---|---|---|
| 1 | container created | `candidate_created` + `Gated=true` (ack) | candidate exists, gate armed, **not started** | unverified candidate; created-not-started |
| 2 | gate started | `gate_started` **new** (ack) | started AND verified by inspect: ID, `dso.rotation.id`, `dso.gate`, `Path==/bin/sh`, wrapped ENTRYPOINT, running, tmpfs/mount per destination | unverified; application never ran |
| 3 | delivery requested | `intent_deliver_secrets` **new** (intent: write failure aborts) | before the first byte is delivered | unverified; "delivery may or may not have happened" |
| 4 | partial delivery | `delivery_partial` **new** (ack, best effort) | a later spec failed after an earlier one was written | unverified; never promoted |
| 5 | delivery verified | `secrets_delivered` **new** (intent style: write failure aborts, gate stays closed) + `DeliveredVersions` | every file delivered AND probed; keyed versions recorded | still unverified until candidate_ready; sets `DeliveryVerified` |
| 6 | gate released | `intent_release_gate` **new** (intent) then `gate_released` **new** (ack) | flag written; the application may now start | unverified: released but readiness not acknowledged |
| 7 | application started | not separately journalled | not distinguishable from "released" without an application signal | folded into 6 (documented, honest) |
| 8 | application ready | `candidate_ready` (existing) | WaitReady/WaitHealthy + post-readiness probe passed | verified **only if** `DeliveryVerified` (rule below) |
| 9 | traffic handoff verified | `intent_handoff`/`handoff_done` (existing, rolling; restart proxy swap as before) | `RoutingVerifier`/proxy handoff, unchanged | existing rules |
| 10 | operation committed | `swap_committed`/`intent_cleanup`/`completed` (existing) | unchanged | existing rules |
| 11 | ledger reconciled | ledger bridge (existing; outside the journal) | `RecordRotationOutcome` / startup reconcile | see section 8 |

**Verification rule (the one place evidence is decided).** `candidateVerified(transition)` is unchanged (transition-based).
`recordCandidateVerified(rec)` (`internal/agent/recovery.go`) = `candidateVerified(rec.Transition)` AND (`!rec.GatedDelivery`
OR `rec.DeliveryVerified`). It is the single predicate used by recovery (`unverifiedCandidate`, `journalPermits`), the N21
rollback (`planRollback`) and the ledger bridge (`journalEvidenceKind`, `JournalRecordFor`). `GatedDelivery` is set by the
`Gated` flag on `candidate_created`; `DeliveryVerified` and `DeliveredVersions` only by `secrets_delivered`;
`intent_create_candidate` resets all three (a fallback candidate never inherits another candidate's evidence). Records written
before this change have none of the fields (omitempty) and keep the plain rule. `Record` refuses (before merging anything)
a `DeliveredVersions` entry that is not `hmac:<key id>:<mac>` (never plaintext, never an unkeyed hash).

`rollbackTransition` (N21) lists the six new transitions as pre-ready, so the controlled rollback applies to them exactly as
to `replacement_started`. The table-driven ledger test (`lbTransitions`) gained six rows, all `false`.

## 3. Arming the gate for a candidate created by rotation

`rotation.PrepareGatedCandidate(cfg, hostConfig, specs, timeout)` runs before any Docker call or journal write (a refusal costs
nothing): `ValidateFileSecretSpecs` -> `VerifyDeclaredTmpfs` -> `VerifyUserOwnership` -> `ArmGate`. It operates on the clone that
`PrepareShadowConfig`/`cloneContainerConfig` produced (new slices assigned, inputs never mutated).

**Contract preservation.** Docker's inspect reports the *effective* ENTRYPOINT and CMD (image defaults merged with overrides), so
no image inspect is needed. The wrapper is `Entrypoint = [/bin/sh -c GATE dso-gate <timeout> <flag> <N> <file1..N> <orig ENTRYPOINT...>]`
and `Cmd` is left exactly as it was. At runtime argv is `Entrypoint ++ Cmd`, so `exec "$@"` re-creates exactly
`origENTRYPOINT ++ CMD` with every argument boundary intact (no re-quoting, no interpolation). `User`, `WorkingDir`, `Env`,
`HostConfig` (including `ReadonlyRootfs`, restart policy, caps) are not touched. Cases [T]: CMD only, ENTRYPOINT only, both, compose
overrides (they are already reflected in inspect), arguments containing spaces/quotes/newlines/`$`/`;`/`--`. Both empty is **refused**
(the original process cannot be preserved). An ENTRYPOINT that is already a gate (clone of a promoted container) is unwrapped and
re-wrapped for the current file set, so rotations never nest.

**Gate script** (`rotation.GateScript()`, a fixed constant; no secret and no user value is ever in its text [T]): traps TERM
(exit 143) and INT (130); waits until the release flag **and** every expected file exist; bounded by the timeout (default 120 s,
exit 75 with a message); then `shift N; exec "$@"`. **PID 1 and signals:** `exec` replaces the shell, the application is PID 1 (or
`tini`'s child if `HostConfig.Init`), no fork; before release PID 1 is `sh` and `docker stop` ends it immediately through the trap.
The wait polls `sleep 0.1`, falling back to `sleep 1` ticks if fractional sleep is unsupported.

**What the agent writes:** secret files first (through the injector), the flag last (also through the injector, name
`.dso-gate-release`, content `released`, no secret), into the first sorted destination directory. The gate does not trust the flag alone.

**Health checks and restart policies.** Docker's first HEALTHCHECK probe runs one `interval` after start; delivery normally finishes
in well under a second, so no probe observes the gate. If delivery hangs, the container may report unhealthy and `WaitReady` rejects
it; the candidate is discarded. A candidate cloned with `always`/`unless-stopped`/`on-failure` that exits at the gate timeout is
restarted by the daemon and the gate re-arms on an empty tmpfs: the Docker `start` event handler and the bounded reconciliation pass (`redeliverGated` / `ReconcileGatedContainers`, sections 10 and 11) re-deliver from the cache. [U]

## 4. Verifying the running container and the mount before delivery

Before the candidate exists (`VerifyDeclaredTmpfs`, from the *declared* config): every destination must be covered by a declared tmpfs
(`HostConfig.Tmpfs` or a `Mounts` entry of type tmpfs), no more specific bind mount, volume or `Config.Volumes` entry may shadow it,
and a read-only tmpfs is refused. DSO does **not** add a mount or change runtime settings on the operator's behalf: a destination
without a tmpfs makes the rotation fail closed with a message naming the fix (`tmpfs: [<path>]`). Cloud-mode compose generation already
declares `/run/secrets/dso`.

After start (`VerifyRunningGate`, from `ContainerInspect`): container ID equals the created ID; label `dso.rotation.id` equals this
operation; `dso.gate` present; ENTRYPOINT is the gate; `inspect.Path == /bin/sh` (the process Docker started is the gate shell, not the
application); running and not restarting; the gate's flag path equals the one that will be written; for every destination the
longest-prefix entry of `inspect.Mounts` has `Type == tmpfs` and `RW`. A destination covered by a bind/volume/none is refused (no
writable-layer path exists). The in-container injector still re-checks `/proc/mounts` (`RequireTmpfs`, exit 66) as the authoritative
runtime check. How Docker reports tmpfs in `Mounts` for every version is **[U]**; when `Mounts` lacks an entry the declared config is
re-checked instead.

## 5. Delivery: the hardened injector, ownership, users, images

Reused unchanged: `injector.InjectFilesWithOptions(Dir: spec.Path, RequireTmpfs: true)`: per file one exec as the container's
configured user, secret on stdin (base64 + sha256 line), `mktemp` O_EXCL temp, sha256 verified before publish, `chown` then `chmod
0400`, atomic `mv`, symlink/dir destinations refused. Names are validated twice and **a name that is not already a plain base name
is refused** (the injector would normalise it while the gate waits for the original). Reserved name `.dso-gate-release`.
Only exported helpers `injector.CheckFileName`/`CheckDir` were added. Mode is 0400 (the injector does not support 0440).

- **Ownership.** The exec runs as the container user and cannot chown to another uid. Root (or unset) user: chown to `inject.uid:gid`
  works with `CAP_CHOWN`; a numeric non-root user must equal `inject.uid` (checked **before** any change, `VerifyUserOwnership`);
  a named non-root user is left to the injector, which fails closed on a chown it cannot do. This is a behaviour change from the
  archive path where the daemon applied the header ids [S].
- **Root entrypoint that drops privileges:** supported (exec as root writes, app uid reads).
- **Read-only root filesystem:** supported; only the tmpfs is written.
- **Supported images:** `/bin/sh`, and for the injector `awk mktemp base64 sha256sum cut chmod mv rm` (+`chown` for non-root ids), and for the probe `/bin/sh`.
  busybox/alpine/debian/ubuntu families satisfy this [S][U].
- **UNSUPPORTED, fail closed, no plaintext fallback ever:** images without `/bin/sh` (distroless, scratch): `ContainerStart` of the gate fails
  (or the gate cannot run), the candidate is discarded, nothing is delivered; images lacking the injector tools: the gate stays closed, delivery
  fails (exit 127), the candidate is discarded; containers with neither ENTRYPOINT nor CMD; containers with no declared tmpfs at the destination;
  destinations on a bind mount/volume/`/proc`/`/sys`/`/dev`; a numeric non-root user that differs from `inject.uid`. Supporting shell-less images
  needs a static helper binary (design alternative F4) and is **not authorised** here.

## 6. Where plaintext never goes

Persistent container layers (no archive upload; destination must be a tmpfs, verified twice); host temp files (none: stdin stream only);
journal and ledger (only keyed `hmac:` identities and counts; file names and values are not journalled); environment variables
introduced by delivery (none; file secrets are excluded from `Config.Env` as before); command-line arguments (gate args hold only paths,
counts and the timeout; the injector passes the secret on stdin); generated startup scripts (the gate script is a constant); logs and
metrics (error values carry exit reasons, never content). The candidate's `docker inspect` shows the wrapped ENTRYPOINT (paths only).
Residual: `docker inspect` of a promoted container keeps the wrapper; the probe checks existence, non-empty and (section 11) readability by the user the probe execs as; for a root container that last check is trivially true. [T for the constant script, create body, journal; U for runtime]

## 7. Strategy differences and honest downtime

- **Rolling** (`RollingStrategy.Execute`): the original keeps serving untouched until the swap; delivery, probe, gate release and readiness all happen
  on the candidate beside it; a failure discards the candidate. Zero-downtime only when the DSO proxy owns the ports (existing rule); file delivery adds
  no downtime. Without a HEALTHCHECK readiness is "process stable" only (existing honesty).
- **Restart** (primary branch) and **fallback** (`executeSimpleRestart`): NOT zero downtime unless the DSO proxy owns the ports. Without the proxy the
  original is stopped before the candidate starts, so downtime now **includes** gate start, verification, delivery, probe and readiness (typically seconds,
  bounded by the health timeout). A failure after the original was stopped rolls back (rename back, restart the original) and the service is down for that time.
- **Agent crash mid-rotation is not zero-downtime**: a crash after the original was stopped leaves the service down until recovery or an operator acts. The
  gate means the application is never running without its secret in that window. N21 rollback restores the original when its safety conditions hold; otherwise
  recovery preserves everything and escalates (`critical_error`).
- Rolling failures that are deterministic refusals (`ErrDeliveryRefused`) skip the fallback restart (it would be refused identically); health-class failures
  (`ErrUnhealthy`, including a probe rejection) already skipped it.

## 8. Recovery, rollback and the ledger

Recovery treats every phase 1-7 as an **unverified candidate** (never the name holder that justifies retiring the original), through the existing machinery: `holderState`
refuses it, `journalPermits` refuses retirement of the original, `planRollback` includes the six transitions. With `AutomaticRollback` off (default) it preserves and
escalates; with it on, the N21 rollback moves the candidate aside and restores the original (nothing deleted) and records exactly what was not rolled back. A gated
record that shows `candidate_ready` without `DeliveryVerified` is never verified. Repeated recovery is idempotent; `critical_error` is never downgraded by
recovery. Rolling candidates (`_dso_new_`) are retired by the existing `retireSafeArtifacts` rules.

Ledger "delivery verified" only for a record that passes `recordCandidateVerified`, so a gated operation needs the complete chain (`secrets_delivered` before
`candidate_ready`). `RecordRotationOutcome`/startup reconcile are unchanged (`startManagedOperation`/`finishRotation`, the evidence rule is strengthened, not weakened).
`DeliveredVersions` are recorded and, since section 11 (B), CROSS-CHECKED: the ledger applies the dispatch's own `SecretVersion` (O-2) only when the journal's per-candidate delivery evidence names that version for every delivering candidate. Interrupted reconciliation converges
(W6) from the journal exactly as before [T]. The per-spec identity is computed from the exact map handed to the injector with the ledger's key
(`ReloaderController.DeliveredVersion`, wired in `openLedgerWith`); with no ledger the field is empty and the ledger cannot apply a version (existing behaviour).

## 9. N22 (fixed in the same change)

Defect (confirmed in source): the restart branch skipped file injection silently when `Cache.Get` missed, starting a candidate without its file secret (rolling:
`if !found { continue }` had the same shape and then created/started the candidate); and the primary restart probe tested `Inject.Type == "file"`, not the
effective type, so defaulted file secrets were never probed.
Fix: `resolveFileSpecs` resolves every secret whose **effective** inject type is `file` (own type, else `Defaults.Inject`) **before any change**; a cache miss, empty
value or missing `inject.path` returns `ErrFileSecretUnavailable` (rotation rejected, no rename, no candidate, nothing started). The same specs drive delivery and the
post-readiness probe of **every** delivered file in all three paths. Regression tests `TestN22_*` fail on the old code (verified against a `git archive` of `HEAD`).

## 10. Migration, compatibility, remaining work

- Env-mode secrets, signal strategy, `dsofile://` local mode, `docker dso inject`, compose-project recreation: unchanged. Rotations without file secrets: no gate, no new journal phases.
- Existing configs: `inject.type` values unchanged. **Behaviour changes for file secrets:** the destination must be a declared tmpfs (previously silently accepted, and
  possibly persistent); the container needs `/bin/sh` and the injector tools; a numeric non-root user must equal `inject.uid`; both ENTRYPOINT and CMD empty is refused;
  provider file names must be plain names; downtime of the restart strategy grows by the delivery time; a missing cache value now aborts instead of starting a secretless candidate.
- Existing journals load unchanged; records without the new fields keep the legacy rule. A journal written by this version and read by an older binary would not know the
  new transitions (older recovery preserves unknown transitions: fail closed).
- **Re-delivery after a later restart of a promoted gated container** (restart policy, `docker restart`, host reboot): the gate re-arms on an empty tmpfs.
  Details, the guards it now takes and the agent-start reconciliation are in section 11 (A, C). It is a re-delivery, not a rotation: **no journal record, no ledger claim**.
- **Not implemented (remaining work):** (1) real Docker qualification (every [U] fact, including the premise itself); (2) shell-less image support (static helper
  binary, design alternative F4, not authorised); (3) a per-config gate timeout (`DefaultGateTimeout` is a constant); (4) an application-level "started" signal
  (phase 7 stays folded into "released"); (5) provider fetch on re-delivery when the cache is empty (deliberately not done: the cache-warm trigger of section 11 delivers as soon as the
  normal poll caches a value; there is no extra provider request).

## 11. Owner-approved follow-ups (restart re-delivery, version identity, legacy records)

Same status: **IMPLEMENTED — REAL DOCKER QUALIFICATION DEFERRED**. Everything below is [S]/[T]; every behaviour that depends on Docker's runtime is [U]. No new daemon, sidecar, journal,
shell-less helper or plaintext fallback was added.

### A. Restart re-delivery reliability (code: `internal/watcher/managed_delivery.go`)

Actual behaviour BEFORE the change (read from source): the only trigger was the Docker `start` event, and the handler needed a warm cache and a registered target; it took neither the per-service
rotation lock nor the OperationGuard.

| # | Case | Before | Now |
|---|---|---|---|
| 1 | `docker start` before the agent subscribes | never served until the gate exited 75 and a restart policy produced a new start event | the startup pass (after recovery settled) and every reconnect/10-minute reconciliation serve it |
| 2 | agent starts with an empty cache | start event: fail closed, no retry | container is remembered as waiting; the first cache write (`writeCache` -> `NotifyCacheWarm`) triggers one coalesced pass that delivers |
| 3 | provider temporarily unavailable | same as 2 | same as 2; the pass reads the cache only, never the provider; the normal poll's own backoff is unchanged |
| 4 | gate times out (120 s, exit 75) before the first fetch | stopped/restarted by the daemon only | restart policy: the daemon restarts it and the start event re-delivers. No policy: **not restarted by DSO** (see below) |
| 5 | no restart policy | stays stopped silently | stays stopped; ONE warning per exit names the recovery command (below) |
| 6 | repeated restarts while the provider is down | each start event logged an error | same, no provider request, no goroutine per event; waiting entries are one per container |
| 7 | agent restarts while a promoted container waits at the gate | not served (no event) | served by the startup pass |
| 8 | rotation-owned candidate waiting at the gate | protected only by in-memory markers (empty after an agent restart) | in-memory markers + rotation artifact names + the OperationGuard (an unresolved/critical record, or recovery not settled, blocks the service) + the service lock |
| 9 | unresolved journal operation for the same workload | not consulted | the OperationGuard refuses; the gate stays closed; retried by the next pass |
| 10 | unregistered / foreign-owned container | warn only | unchanged (skipped, gate closed); another agent's container is never touched |

Mechanics: `ReconcileGatedContainers` is ONE `ContainerList` (label `dso.gate`, `All: true`) per pass. RUNNING containers and exited (gate-timed-out) artifacts each get their OWN bounded budget of at
most `maxGateScan` = 128 examined, so exited rotation shadows/artifacts (listed via `All: true` so the exit-75 diagnostic can see them) can never crowd out a running container waiting at its gate --
that running-container budget is served first and is what actually keeps an application from starting. Fairness across repeated passes: the running-container scan resumes just after the container ID
it examined last (`gateScanCursor`, wrapping around the sorted ID space) rather than always re-scanning the same lexicographic prefix, so with more than `maxGateScan` running gated containers every
one of them is eventually examined, not only the first `maxGateScan` forever. A running container goes through `redeliverGated`; an exited one is only diagnosed. `gateWaiting` bookkeeping (which
containers are still owed a re-delivery) is derived from the full running set the single `ContainerList` call already returned, not from what the bounded scan reached this pass, so it converges every
pass regardless of truncation. `RequestGateReconcile` coalesces any number of triggers (startup, reconnect/periodic reconciliation, cache writes while something waits) into ONE worker; `NotifyCacheWarm`
costs nothing when no container waits. `redeliverGated` (also the Docker `start` handler): takes the same per-service lock `TriggerReload` takes (never waits; a busy service is retried by the next pass), asks the
same OperationGuard, re-checks the rotation ownership markers after taking them, resolves the cached specs (private copies), skips a gate whose release flag already exists (idempotent), verifies
identity/gate/tmpfs/file-set (`VerifyRunningGate`) and delivers. It never starts a stopped container, never retries a failed delivery in a loop and never fetches from a provider.

**The reconciliation worker (`RequestGateReconcile`).** One background goroutine exists for gated-container reconciliation. It is a deliberate, bounded design, not an incidental one.

*Why a worker is needed.* Reconciliation passes run synchronously at three fixed points: startup once recovery settles, a Docker reconnect, and the periodic tick. A gated container waiting for its secret can become deliverable strictly *between* those points: the cache is populated by a provider fetch at an arbitrary time, and `NotifyCacheWarm` fires from the cache-write path. That path must not block on a full pass, so the request is handed to a worker.

*Bounded and coalescing.* At most one worker runs at a time: it is started only when `gateScanBusy` moves false to true by compare-and-swap. A request stores `gateScanPending` first and then attempts that swap. Any number of requests during a running pass collapse into exactly one further pass, so triggers never accumulate goroutines, Docker calls or provider calls. Each pass examines at most `maxGateScan` (128) containers per class (running, exited) and resumes from a rotating cursor so later containers are not starved.

*No lost wakeup.* The worker clears `gateScanBusy` before re-reading `gateScanPending` and re-claims by compare-and-swap. A request that arrives as the worker finishes is therefore either started by the requester's own swap or seen by the worker's re-check, and both cannot fail. (This ordering argument is backed by a 1,500-cycle stress test; it is not provable by a single mutation test because the window is very narrow.)

*Cancellation and shutdown.* The worker uses the agent context. It checks `ctx.Err()` per container and before re-claiming, releases `gateScanBusy` first when it exits, and every Docker call it makes (list, inspect, probe, execute) takes that context. After cancellation it issues no further Docker operations. A coalesced request runs under the context of the worker that was already running, which in practice is the single agent context.

*Synchronization.* State shared with other paths is the two atomic flags and the scan cursor, which is mutex-guarded. Nothing else is shared; the worker holds no lock while waiting for another request.

*Operation ownership is unchanged.* Re-delivery from the worker takes the same per-service lock key and the same operation-guard keys (service name, container id, container name) as `TriggerReload`, and it re-checks ownership after taking them. It refuses a gate owned by a rotation (`gateOwned`, proxy-pending, rotation artifacts, another agent). It never releases a gate that an unresolved operation owns.

*No retry storm.* There is no internal retry loop. One pass runs per consumed request, `resolveFileSpecs` reads only the in-memory cache and never calls a provider, and a failure leaves the container waiting until the next trigger or cache warm.

Tests: `TestN19Reconcile_BoundedAndCoalesced`, the `TestN19ReconcileLifecycle_*` tests (no worker accumulation, request during a running pass, no lost wakeup, cancel stops the worker, cancel mid-pass), and the rotation-ownership tests; the `internal/watcher` suite is race-clean. This worker supersedes the earlier working constraint of "no new goroutines" for this one mechanism only.

**Timed-out gate, no restart policy.** DSO does not restart a stopped container automatically: an operator's stop must stand, and exit status 75 cannot be told apart from an application's own status
with certainty. The warning (once per container exit, from the `die` event or the pass) names the recovery: once the secret provider is reachable run `docker start <container>`; the running agent
then delivers the cached secrets and releases the gate. (There is no `dso` command for this.) Remaining limitation: with an empty cache a container restarted by policy may exhaust several 120 s gate
cycles before the first successful provider fetch; it is served at the first cache write after that.

### B. Dispatch identity vs actual delivery (code: `ledger_bridge.go`, `state_tracker.go`)

Actual behaviour found: the dispatch version (journal `SecretVersion`, from the value observed at dispatch) and the delivered values were NOT bound. The strategies resolve their delivery specs from the
cache inside `TriggerReload`, after the observation; a newer observation in between (cache moved V2 -> V3) delivered V3 while the ledger recorded V2, because the bridge used `SecretVersion`/hint and only
stored `DeliveredVersions`. Defect: **yes**. (Within one `TriggerReload` the injector map, `FileSecretSpec.Data` copy and `Version` are one value: the identity is computed from the exact copy handed to
the injector.)

Fix: the journal keeps the keyed identities per delivering candidate (`DeliveredByCandidate`, never reset by a later candidate; additive, omitempty) and the bridge (`deliveredEvidenceMatches`, used by
`EvidenceForRotation` and `JournalRecordFor`) accepts a version as delivered only if EVERY delivering candidate's evidence names it. A mismatch, or a gated delivery that named no identity, is "no evidence":
applied never advances and the entry stays unresolved (the next observation redelivers the desired version through the managed path). A newly fetched value is never used to prove an earlier delivery;
partial delivery (`delivery_partial`) never sets `DeliveryVerified`. Recovery's own notion (`recordCandidateVerified`) is unchanged: a candidate that delivered V3 is still a healthy, verified workload.

### C. Re-delivery vs the ledger

A re-delivery makes no journal record and no ledger claim (the watcher has no ledger handle). Safe behaviour under the M1 ledger contract, by case: same verified version: nothing to record, applied
stays valid. A different currently-cached version (observed, its rotation pending/deferred): the restarted container receives it, the ledger's applied version stays at the older verified one (stale, never
ahead), `NeedsRotation` keeps the desired version pending and the next managed operation replaces and records it. Applied version unknown: unchanged (unknown never means changed). Partial multi-target / multi-replica:
each replica is served on its own start/pass with the then-cached copy; the ledger never advances from a re-delivery. The focused correction: a re-delivery used to run beside a rotation and beside an unresolved
operation; it now holds the service lock and honours the OperationGuard (section 11 A), so a different version is never delivered while a managed operation or an unresolved one owns the service. Limitation: a
re-delivery of a newer cached value to one replica before its rotation is a delivery the ledger does not see; the entry remains "applied < desired" until the managed rotation runs.

### D. Legacy (pre-N19) journal records

Actual behaviour found: a record without `GatedDelivery` took the plain transition rule, so an unresolved legacy file-mode record at `candidate_ready` or a committed swap counted as delivery evidence (recovery
retired the original, the ledger advanced applied). Defect: **yes** (the archive upload it implies was never verified to reach a tmpfs). Fix: the tracker derives, when it hands a record out (`StateTracker.view`),
`legacyFileUnverified` for an UNRESOLVED restart/rolling record without the gate flag whose secret is file-delivered per the CURRENT configuration (`StateTracker.FileModeSecret`, wired by the trigger engine).
"Unresolved" here is the conservative check `view()` actually runs: any status OTHER THAN `completed`/`recovered` -- which includes the three named failure/in-flight statuses (`in_progress`,
`rollback_required`, `critical_error`) but also an empty or otherwise unrecognized status, treated as unresolved rather than assumed safe. `recordCandidateVerified` is then false, so recovery preserves every workload and the evidence, does not promote the candidate or retire the
original, escalates to `critical_error` (the OperationGuard keeps the service blocked) and the ledger bridge/reconcile do not advance applied. Old records keep loading; completed/recovered historical records
stay valid (not flagged); env-mode secrets and bare trackers keep the plain rule. Limitation: a secret switched from env to file since the legacy record was written is judged by today's configuration
(conservative: the operation stays blocked for an operator). A fallback candidate never inherits another candidate's evidence (`intent_create_candidate` resets the verdict fields; per-candidate identities are
kept only for the section-B cross-check).

### Bounded checks

Release marker vs user file: `.dso-gate-release` is reserved (`ValidateFileSecretSpecs`). Stale marker: the gate also waits for every file, the flag path must equal the one this delivery writes, and (new)
every file the gate waits for must be written by this delivery (`VerifyRunningGate`). Exact container identity and rotation label are checked before any byte is written (re-delivery has no rotation
label to check; identity, `dso.gate`, shell PID 1 and the tmpfs are). All destination files are probed before the flag is written; the flag is written only after `secrets_delivered` is durable (rotation) or
after probing (re-delivery has no journal). Ownership: files are `0400`, written as the container user, chown to `inject.uid` only when that user can; a numeric non-root user must equal `inject.uid` (checked
before any change); the probe now also requires readability by the probing user (meaningful for a non-root app user, trivially true for root); never world-readable (injector `umask 077` + `chmod 0400` before
publish). A gate cannot be released by a concurrent unrelated operation: the service lock and OperationGuard are held/consulted, and rotation candidates are marked in memory and by name. Repeated recovery and
repeated passes are idempotent (an existing release flag ends the attempt).

## 12. Convergence after re-delivery (verified guarantee)

A promoted gated container that restarts is re-delivered whatever the cache currently holds, and re-delivery writes no journal record and makes no ledger claim. The applied version therefore never moves ahead of what a verified managed operation recorded, and the guarantee is that **a discrepancy converges without the provider changing again** (no later value V4 is required):

- The ledger keeps durable desired and applied versions per service. `NeedsRotation` (`internal/ledger/transitions_decision.go`) returns rotate whenever desired differs from applied.
- The in-memory "already handled" hash in `commitObservation` (`internal/agent/trigger.go`) is consulted before the ledger, so convergence depends on every outcome that did not apply the version rolling that hash back. The covered outcomes do: failure or rollback, deferral, guard block, lock timeout, journal-start failure and cache-limit refusal. After an agent restart the hash is empty and the persistent ledger decides, so the same unchanged value dispatches.
- An unresolved `critical_error` journal record blocks the run (the operation guard stays authoritative) and applied stays at the old version; after the record is resolved the same value converges.
- Multi-replica: the entry is per service and applied moves only in `ledgerTerminal` after a verified managed operation. A replica that received the newer value through re-delivery does not make the workload report it as applied.
- Two paths intentionally keep the hash: a rotation that reached no targets (`ErrNoTargetsRotated`, the ledger records a block cleared by a configuration change) and the "another operation holds the entry" skip (not reachable as a successful-but-unrecorded dispatch because the per-service lock defers the second operation).

Re-delivery is never treated as application adoption. Deterministic tests: `TestConvergence_*` in `internal/agent/ledger_bridge_test.go`.
