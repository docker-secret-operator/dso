# M1 design note: reconciling the applied-version ledger with the M0 StateTracker and journal

Status: DESIGN ONLY. No source, test or configuration is changed by this note, and it authorises no implementation. Items marked **[OWNER]** are recommendations that need owner confirmation before the dependent slice starts.
Branch/baseline: `feature/m1-development`, library `internal/ledger` (commits 71c9d19, c25c272, UNWIRED), N21 rule `candidateVerified` (4052259, 5d83fd5).
Sources read: `internal/ledger/*`, `internal/agent/{state_tracker,recovery,operation_guard,trigger,server,proxy_reconstruction}.go`, `internal/rotation/journal.go`, `internal/watcher/{controller,signal_rotation}.go`, `docs/audit/48-LEDGER-OPEN-QUESTIONS.md`, and the out-of-repo design record `BL-02-ledger-design.rev6.3.md` (sha256 54ea9717…bf81a), `BL-02-S2-owner-decisions.md`, `BL-02-S3-owner-decisions.rev3.md`.
Terms: "journal" = the M0 `StateTracker` (`rotations.json`, written through `rotation.Journal.Record`). "Evidence" = a journal fact a ledger write must cite.

## 0. The binding decision and what it changes in the BL-02 design

| Concern | Authority | Consequence |
|---|---|---|
| Rotation operation identity (`RotationID`), lifecycle, pending intent, phase transitions, commit state, crash recovery, blocking of unresolved operations, recovery decisions | **M0 journal / StateTracker / `journalGuard` / `AutomaticRecovery`** | The ledger holds none of these. |
| Verified desired and applied secret-version state, and per-replica delivery records | **ledger** | The ledger records facts the journal has already established; it never authorises or sequences Docker work. |

Rev 6.3 designed the ledger as a second rotation authority (write-ahead `Intent`, `BeginRotation` authorising Docker, a recovery journal, `Gate()` as a precondition for every mutation, adoption recording `applied = d`). All of that is withdrawn by the owner decision. Where this note differs from rev 6.3 or an `[OA]` item of the S2 record it says so (section 12, items O-3 and O-4).

Invariants of the reconciled design (each is tested, section 11):

* **X1** The ledger never starts, authorises, gates-open or sequences a Docker mutation. A rotation is dispatched only through the existing path (`ExecuteRotation` → `StartRotationWithID` → `TriggerReload` → `journalGuard.RotationBlocked`). The ledger can only make the engine *not want* a rotation or *want* one; the guard still decides whether it may run.
* **X2** `Entry.Applied` / `Replica.Applied` advance only on journal evidence (section 4.2). Never from provider observation, Docker state, `critical_error`, `rolled_back`, `recovered` (alone) or a pre-`candidate_ready` record.
* **X3** A missing, unreadable, corrupt, newer-schema or key-lost ledger yields "unknown", never "changed". It never causes a rotation.
* **X4** There is exactly one rotation journal. No ledger state machine has phases, pending intent, or a recovery plan.
* **X5** `signal_done` is delivery verified, adoption **unverified**. File delivery is whatever the journal says (N19: unverified). The ledger never claims more than the journal.
* **X6** Ledger problems never block dispatch that M0 alone would permit, except that adopt-without-rotate is the first-observation default (section 8).

## 1. Data model: journal versus ledger

### 1.1 What maps to what

The journal record (`RotationState`) is keyed by `RotationID`, one record per `ExecuteRotation` dispatch for a `provider:secret`. The ledger `Entry` is keyed by `(project, service, provider, secret)`. One dispatch fans out to every target container of the secret (`collectContainerIDsForSecret`, `TriggerReload`), so **one journal record corresponds to N ledger entries (N services) and each entry has several replicas.**

| Journal (`RotationState`) | Ledger | Mapping rule |
|---|---|---|
| `RotationID` (`rot-<16 hex>`) | none today (see 1.3) | correlation key written into ledger replica records; copied verbatim |
| `ServiceID` (real container name), `OriginalContainerIDs`, `CandidateContainerIDs` | `Key.Service` / `Replica.ContainerID/Name` | journal IDs name containers; the ledger entry key is derived from Compose labels (section 1.4) |
| `SecretVersion` (never set by production code today) | `Entry.Desired` / `Replica.Applied` (`Version`) | **gap**: the journal does not know which version a rotation delivers (1.3) |
| `Strategy` | evidence rule selector (4.2) | restart, rolling, signal |
| `Transition`, `Seq`, `Status`, `Committed` | evidence only | read-only inputs to `ledger` writes; never mirrored |
| `PendingIntent`, `ExpectedProxyTarget`, `CleanupStatus`, `OperationState`, `ManualRecovery` | nothing | journal-only |
| `critical_error` / inherited `in_progress` | nothing (guard) | the ledger records replica adoption "unknown" for such a record, never a block |

### 1.2 Ledger fields: keep, use, or remove from use

| Ledger item | Decision | Why |
|---|---|---|
| `Entry.Desired`, `DesiredSeq`, `DesiredObservedAt` | **USE** | desired version observed (sole writer: `Observe`) |
| `Entry.Applied`, `Replica.Applied` | **USE, redefined**: "last version whose delivery the journal verified" (not "adopted at first sight") | X2 |
| `Entry.State` `adopted` | **USE** only for "observed, applied UNKNOWN" (applied nil) | rev 6.3 set `applied = d` at adoption; the owner decision forbids that claim |
| `idle`, `pending`, `failed`, `rolled_back`, `blocked` | **USE** | derived from desired vs applied and the last terminal outcome |
| `partial` | **KEEP-UNWIRED (library), not produced by S4 v1** | needs per-replica outcomes the journal cannot attribute (1.5); v1 reports a failed group as `failed` and leaves `applied` unchanged |
| `EntryInProgress` and `Entry.Rotation *Intent` / `Intent` | **REMOVE-FROM-USE** (keep in the v2 decoder so a hand-written file still parses; the S4 facade never sets them; the validator rule "in_progress ⇔ intent" is never exercised) | a second rotation-intent state machine (X4). Also `Intent.RotationID` must be 32 hex while journal IDs are `rot-<16 hex>`: they cannot even be stored there |
| `Entry.Request` / `Redelivery*` / `Store.RequestRedelivery`, cancel-on-open | **KEEP-UNWIRED** | a durable manual request is another intent store. Manual rotation stays the REST/`dso sync` path through the journal. `cancelOnOpen` stays harmless |
| `Ledger.Recoveries`, `RecoveryRecord`, `RecoveryScope/Trigger/State`, `Action*`, `BeginRecovery`, `CompleteRecovery`, `RecoveryRecords` | **REMOVE-FROM-USE** (library kept, import-guarded) | M0 `AutomaticRecovery` and the journal are the recovery authority (R-Q2, R-Q3, R6-Q4 become moot) |
| `LastOutcome` | **USE**: last terminal outcome (`kind`, `reason`, `at`, `strategy`) and, for `blocked`, the block reason (rev 6.3 section 44.1) | no secret material |
| `Replica.Number/ContainerID/ContainerName/Created` | **USE** | per-replica identity (1.4) |
| `Replica.Outcome`, `Reason`, `At` | **USE** with the closed reason vocabulary of section 10.1 | |

### 1.3 Schema amendment required before S4 (call it S2.2) **[OWNER]**

Status (S2.2 implemented, unwired, uncommitted at the time of writing): `SchemaVersion = 3` in `internal/ledger`. Per replica `rotation_id`, `delivery`, `adoption` added as specified; additionally (implementation decision, owner to confirm) the retired `Entry.rotation`/`Intent`, the `in_progress` state and the document-level `recoveries` journal were removed from the v3 schema and the exported API instead of being kept decodable, because a v2 file is rejected anyway. A v2 file takes the existing corrupt path (preserved aside byte for byte, fresh all-UNKNOWN ledger); a newer file stays memory-only and untouched. `Store.Update`/`UpdateMemoryOnly` are now unexported and guarded by `TestRepositoryReferenceGuard_*`.

The ledger must distinguish five things; v2 cannot say all of them without stringly-typed `Reason` conventions, and v2 has no field for the correlation id. Because no ledger exists on disk anywhere (library unwired), the cost of changing the schema now is near zero and later is not. Proposal: schema **version 3**, additive per `Replica`:

| Field | Values | Meaning |
|---|---|---|
| `rotation_id` | `""` or the journal id (validated as 1 to 64 chars `[A-Za-z0-9_-]`, not the 32-hex rule) | the operation that last wrote this replica record |
| `delivery` | `none` (zero), `requested`, `verified` | `requested` is a **correlation hint**, subordinate to the journal; `verified` requires evidence (4.2) |
| `adoption` | `unknown` (zero for adopted replicas), `unverified`, `verified` | `verified`: a fresh process was started with the version (restart/rolling at success). `unverified`: delivered but nothing proves the application loaded it (signal; N19 file secrets). `unknown`: existing workload or interrupted operation, nothing known |

The five owner states map as: desired observed = `Entry.Desired`; delivery requested = `delivery=requested`; delivery verified = `delivery=verified`; adoption verified = `adoption=verified`; adoption unknown = `adoption=unknown` with `Applied=nil` or a not-verified delivery. The decoder stays strict; a v2 file is rejected as invalid exactly as a v1 file is today (no migration exists or is needed). If the owner declines v3, the fallback is encoding the same facts in `Replica.Reason` constants; this note rejects that (untestable by the validator) but the rest of the design is unchanged.

The journal side needs one small M0 change **[OWNER, M0 files]**: carry the target version in the journal record. Today `SecretVersion` exists in `JournalEvent`/`RotationState` but nothing sets it, so a journal-committed rotation cannot say which version it delivered. Proposal: a `StartRotationWithID` variant that also takes `secretVersion` (the ledger `Version` rendered as `kind:key_id:id`, an HMAC or provider id, never a value). With it the journal is self-sufficient for reconciliation and the ledger's `requested` hint becomes an optimisation instead of a dependency (windows W6 and W11).

### 1.4 Per-replica delivery identity in DSO's single-host model

DSO manages containers on one Docker daemon. A "replica" is one container of a Compose service (the Compose `container-number` label) or the single container of a non-Compose target. There is no cluster, node or Swarm task identity.

| Identity | Definition | Use |
|---|---|---|
| Entry key | `(project, service, provider, secret)`; Compose: label project/service; non-Compose: `("", container name)` (rev 6.3 section 3.7, resolves the ledger side of P-3) | stable across DSO rotations, which recreate containers under the original name |
| Replica identity | `(number, container name)`; `number` = Compose container-number, `""` for non-Compose. The name is the stable-name identity | what a delivery record is about |
| `container_id`, `created` | attributes of the replica **at record time** | a change is a signal (the replica was recreated since the record), never a key |
| Artifact names (`_dso_new_`, `_dso_backup_`, `_old_<n>`) | never an identity (`IsDSORotationArtifactName`) | |

Rules: a record about a replica whose current `(container_id, created)` differs from the stored pair, with no journal operation that explains it, drops that replica to `adoption=unknown` and clears its `applied`; it never triggers a rotation (this replaces rev 6.3's "external recreation: rotate once"; a recreated container started from its own Compose configuration, so what it holds is unknown, and a forced rotation of a scaled service is what the owner wants to avoid). Replicas of one Compose service rotate sequentially (`replicaSequencer`); their per-replica journal attribution is limited as described next.

### 1.5 Journal gap J1 (per-service and per-replica attribution) **[OWNER, M0 files]**

`StateTracker.Record` upserts by `RotationID` only, and `ServiceID` is last-write-wins; `OriginalContainerIDs`/`CandidateContainerIDs` are unioned. A dispatch that fans out to several services (or replicas) therefore yields one record whose `Transition` is the last step of whichever target wrote last. The journal **cannot** say that service A reached `candidate_ready` while service B failed.

Decision for S4 v1 (no M0 change): ledger advancement is **all-or-nothing per dispatch**. `applied` advances for every entry the dispatch targeted only when the dispatch finished successfully (`finishRotation` success branch, journal `Status=completed`, `Seq>0`, evidence valid). Any partial or failed fan-out leaves every touched entry at its previous `applied` with `state=failed`, and a later poll re-dispatches (this is today's `rollbackHash` behaviour, so at worst one redundant restart of a service that did succeed). "Partial replica delivery" is therefore recorded conservatively (scenario 11, section 11). Real per-replica `partial` records need journal keying by `(RotationID, ServiceID)`; that is a recommended M0 follow-up, not a dependency of S3 or S4a/S4b.

## 2. Operation-ID correlation

`RotationID` (created in `ExecuteRotation`, put in the context by `rotation.WithRotationID`, used as the journal key) is the **only** correlation key. It is written into the ledger as `Replica.rotation_id` and nowhere else (the ledger never mints an operation id).

* Journal first: the ledger may cite a `RotationID` only if `StartRotationWithID` returned nil (record durable). The facade's `Correlate` refuses an empty id.
* The ledger write `delivery=requested` carries `(RotationID, target desired_seq, Version)`. It is a hint: recovery decisions are made from the journal and Docker, never from the hint. If the hint is missing the journal-carried `SecretVersion` (1.3) supplies the version; if both are missing the rotation cannot be applied to the ledger and reconciliation yields `adoption=unknown`, no advance (safe).
* Reconciliation matches `replica.rotation_id` to journal records by identity, never by provider:secret key (the journal key `provider:secret:ids` is not stable; deferred/no-target dispatches delete the record via `DeleteState`).
* A journal record pruned by `CleanupOldStates` (completed/recovered older than 7 days) can no longer be matched; the ledger reconcile therefore runs **before** `CleanupOldStates` in `StartAll` (section 5), and a ledger record older than the retention horizon is accepted without journal confirmation (rule R-C in 4.3).

## 3. Observation serialisation

Today (source-verified): `ExecuteRotation` → `commitObservation(cacheKey, secretName, newHash, secretData)` takes `keyMutex(cacheKey)` (per `provider:secret`), compares `secretHashes`, applies the cooldown, stores `secretHashes`/`lastRotations`, writes the cache, returns "dispatch?". The dispatch goroutine takes `LockManager.AcquireLock(secretName)` and then `StartRotationWithID`. Poll, webhook and IPC `TriggerReconciliation` all enter through `ExecuteRotation`.

Decision:

1. `Observe` runs **inside `commitObservation`, under the existing per-key mutex**, immediately after the "same hash as last dispatched" early return and **before** the cooldown check. Rationale: the ledger's `desired` must see values in the same order as `secretHashes` (an `Observe` outside the mutex lets A,B arrive at the store as B,A while `secretHashes` ends at A). Recording `desired` during a cooldown is harmless and correct; the dispatch is simply retried by the next poll, as today.
2. The same critical section evaluates `NeedsRotation(entry)` (pure, no I/O). If false: store the hash and write the cache exactly as today (PE-03: the compose restart path pre-injects from the cache) and return "do not dispatch". If true: continue today's path (cooldown, store hash, cache, dispatch).
3. Mutex hold time: one fsynced ledger write (the store retries 0, 250 ms, 1 s on failure; at most about 1.3 s). `Observe` writes only when `desired` actually changes, so the steady state (unchanged value) is a no-write read of `Get`. If the ledger is degraded the facade returns promptly with `ErrPersistenceUnavailable` after the store's own retry budget; the engine switches that entry to memory-only observation (`UpdateMemoryOnly` via the facade) and continues (X6).
4. `Correlate` (delivery requested) runs in the dispatch goroutine **after** `StartRotationWithID` succeeded and **before** `TriggerReload`, outside the per-key mutex but inside the secret lock. A failure is logged and ignored (the journal carries the version, 1.3).
5. Terminal transitions run from `finishRotation` (the single completion point), after the journal's own terminal write (`CompleteRotation`/`MarkRollback`/`DeleteState`).
6. Observation rollback: `rollbackHash` keeps its meaning (forget the in-memory hash so the next poll retries). It never edits the ledger; the ledger's `desired` is "never discarded".

## 4. Commit ordering and failure windows

### 4.1 No cross-file atomicity

`rotations.json` and `ledger.json` are separate files; each is atomic-rename plus fsync (file, then directory). There is no transaction across them and none is claimed. Consequently every ordering below is **journal first, ledger second** for facts that depend on the journal, and every ledger write that follows a journal fact must be re-derivable from the journal after a crash.

### 4.2 The evidence rule (what a ledger "delivery verified" requires)

The journal's rule is `candidateVerified(transition)` (`recovery.go`): the transition is one of `candidate_ready, intent_handoff, handoff_done, intent_swap, swap_committed, intent_cleanup, completed`. The ledger **reuses that single function** (the ledger package defines a typed `Evidence` value; the agent-side adapter builds it from a `RotationState` using `candidateVerified`; a test pins that the two cannot diverge).

| Strategy (journal `Strategy`) | `delivery=verified` requires | `Entry.Applied` advances when | `adoption` |
|---|---|---|---|
| `restart`, `rolling` | `Seq>0` and `candidateVerified(Transition)` **and** the dispatch ended in success: `Status=="completed"`, or `Committed==true` (swap committed) | same condition | `verified` for env-mode secrets; `unverified` for file-mode secrets while N19 is open |
| `signal` | `Seq>0` and `Transition==signal_done` and `Status=="completed"` | same | always `unverified` (a signal reload is never adoption verified; `OperationState` says so) |
| any, `Status` in `critical_error`, `rollback_required`, `recovered` without `Committed` | **no** | **no** | replica `unknown` |
| `Seq==0` (no strategy transition, journal-only start) | **no** | **no** | unchanged |

`recovered` alone proves only that M0 recovery restored a healthy workload, not that the new version was delivered, so it is never evidence. N21 is why `candidate_ready` is a floor: before it a started candidate may never have received the secret.

### 4.3 Ordered steps and every crash window

Steps of one rotation (restart, rolling or signal; `R` = RotationID, D2 the new version, D1 the previous applied version):

1. provider fetch (no durable state) → 2. ledger `Observe` (desired D2) → 3. journal `StartRotationWithID(R, version D2)` → 4. ledger `Correlate` (replica `delivery=requested`, `rotation_id=R`) → 5. strategy: journal `intent_*` / acknowledgements, Docker actions, `candidate_ready` (or `signal_done`) → 6. journal commit: `swap_committed`/`completed`; `finishRotation` → `CompleteRotation` (`Status=completed`) → 7. ledger `RecordDeliveryVerified` + `RecordApplied`.

"Reconcile" below is `ReconcileFromJournal` run at startup after M0 recovery (section 5). Every outcome is idempotent: re-running it on the same stores yields no further change (the facade returns `ErrNoChange`, which the store treats as no write).

| Window | Crash between | Journal (durable) | Ledger (durable) | Recovery outcome (idempotent) |
|---|---|---|---|---|
| W1 | 2 and 3 | no record for R | desired=D2, applied=D1, `pending` | M0 recovery: nothing. First poll: `Observe` unchanged, `NeedsRotation` true → one rotation. Nothing lost |
| W2 | 3 and 4 | R `in_progress`, `Seq=0`, inherited | as W1 | M0 recovery resolves R (`recovered`, no mutation: originals healthy) or marks `critical_error`. Reconcile: no evidence → ledger unchanged; engine re-dispatches (guard permitting) |
| W3 | 4 and first Docker action | R `in_progress`, `Seq=0` | replica `delivery=requested, rotation_id=R`, applied=D1 | reconcile: R has no evidence → clear `requested` (`delivery=none`, `outcome=pending`); D1 stays; re-dispatch via `NeedsRotation` |
| W4 | during 5, before `candidate_ready` (`intent_start_replacement` etc.) | R `in_progress`, last transition below ready | `requested` | M0 recovery applies N21 (preserve both, escalate `critical_error`, guard blocks the service). Reconcile: no evidence → replicas of that service `adoption=unknown`, `delivery=none`, `applied` **unchanged** (never advanced, never trusted as D1: `unknown`), `state=failed`, reason `interrupted`. No dispatch until the operator resolves the record (guard). Afterwards `NeedsRotation` (D2 ≠ D1) rotates once |
| W5 | after `candidate_ready`, before `completed`/`swap_committed` | R transition ≥ ready, `Status` in_progress → M0 recovery → `recovered` (candidate serves) or `critical_error` | `requested` | not "completed": **no advance**. replicas `unknown`. Next poll rotates once more (D-2: an uncertain replacement may cost one extra rotation, never none). If M0 recovery recorded `Committed` + `recovered`, evidence holds → advance (as W6) |
| W6 | 6 and 7 (**journal commit recorded, ledger update interrupted**) | R `completed` (or `Committed`), `Seq>0`, evidence valid, version D2 (journal `SecretVersion` or the hint) | `requested` or older state, applied=D1 | **reconcile the ledger from the journal outcome without rotating again**: `delivery=verified`, `applied=D2`, `state=idle` (or `pending` if desired moved on), `adoption` per 4.2. In the live process the in-memory `secretHashes` already prevents a re-dispatch, so there is no extra rotation before the restart either |
| W7 | during 7 (between the verified write and the applied write) | as W6 | `delivery=verified, rotation_id=R`, applied=D1 | the two writes are **one** ledger `Update` (a single atomic rename), so this window does not exist as a separate state. If the facade is ever split, reconcile treats "verified for R, applied < R" as W6 |
| W8 | during 7, ledger persistence fails (disk, permissions) while the process lives | R `completed` | unchanged | the engine keeps `secretHashes` (no re-dispatch), marks the entry "ledger dirty" in memory and retries the transition at the next poll. Restart: W6 path from the journal (records are kept 7 days) |
| W9 | journal `signal_done` | R `completed`, `Transition=signal_done`, `OperationState` "reload requested, adoption NOT verified" | `requested` | `delivery=verified`, `adoption=unverified`, `applied=D2` (delivered, so no repeat storm). Status shown as "adoption unverified" |
| W10 | `finishRotation` deferred/blocked/no-target (journal `DeleteState`) | record deleted | `requested`, `rotation_id=R` | startup reconcile: R unknown to the journal and no process owns it → clear `requested` (the dispatch never ran). In-process: `finishRotation` clears it |
| W11 | ledger lost or fresh while the journal holds completed R | R `completed` with a version identity | fresh ledger (all UNKNOWN) | records older than the ledger's `created_at` are ignored: **do not replay history into a fresh ledger** (their HMAC key id may differ). Entries stay UNKNOWN |
| W12 | **ledger says applied D2 but the journal lacks delivery evidence** (restored backup, manual edit, W4/W5 mishandled, bug) | R missing, or present without valid evidence | `delivery=verified/applied=D2`, `rotation_id=R` | **not accepted; the journal wins.** Rule R-A: if the journal holds R and R fails 4.2 → downgrade that replica to `adoption=unknown`, `delivery=none`, restore `applied` to the previous verified version if recorded else nil, emit a "ledger corrected" event. Rule R-B: if the journal does not hold R and the record is **newer** than the retention horizon (now minus 7 days) → same downgrade. Rule R-C: if older than the horizon, accept (the journal legitimately pruned it) |
| W13 | ledger write torn mid-`Update` | unaffected | the store's atomic rename leaves either the old or the new complete document; an ambiguous post-rename directory fsync is handled by the store (S2) | restart reads either complete document; W6/W3 rules above converge from the journal |
| W14 | journal write fails at 3 | no record | desired=D2 | `StartRotationWithID` error → today's code logs and continues; with the ledger the engine **must not dispatch** (no journal intent, no ledger citation): it calls `rollbackHash` and returns (already the intent of `RequireJournal` for rolling; extended here) **[OWNER, M0 trigger]** |
| W15 | failed rotation (`rollback_required`) at 6 | R `rollback_required`/`recovered` | `requested` | `finishRotation` failure branch (or reconcile after a crash): `delivery=none`, `state=failed` or `rolled_back`, `applied` unchanged; next poll retries as today |

Corrupt journal (`Corrupt()`): reconcile does **not** run. The guard already blocks every rotation. The ledger is left as is, a warning is emitted, and nothing is advanced; `applied` values written within the retention horizon cannot be re-verified and are reported as "unverified (journal unreadable)".

## 5. Interrupted-operation recovery and who initiates reconciliation

Initiator: **`TriggerEngine`, never the ledger.** The ledger starts no goroutine except its own store health probe and has no recovery journal. Reconciliation is a synchronous startup step in `TriggerEngine` using read-only Docker inspection.

Required order (the first four steps exist today; the ledger steps are new):

| # | Step | Today | With the ledger |
|---|---|---|---|
| 1 | `NewTriggerEngine`: `NewStateTracker`, `LockManager`, `journalGuard` | yes | plus `ledger.Open(<ledger dir>, cfg)`. Any open failure or memory-only mode is not fatal (X6); it is reported (section 6) |
| 2 | `BeginStartup()` arms the guard (`armed=true`, `settled=false`) | yes | unchanged: no dispatch path can mutate until settle |
| 3 | `AttachProxy` (gated scan) | yes | unchanged |
| 4 | `StartAll` → `performAutomaticRecovery`: `ValidateStateOnStartup`, `RecoverFromCrash`, re-flag degraded, `CleanupOrphanedContainers` | yes | unchanged; M0 settles every unresolved record first |
| 5 | **ledger reconcile** (`ReconcileFromJournal` + live-set adoption check) | n/a | **new, last statement inside `performAutomaticRecovery`, before its deferred `settleStartup`**. This is the last point at which the guard still refuses every dispatch (webhook, IPC, REST, watcher), so no rotation can start between M0 settle and reconcile |
| 6 | `settleStartup` (`guard.settled=true`, held proxy candidates admitted) | yes | unchanged |
| 7 | `CleanupOldStates(168h)` | yes | stays **after** step 5 (the 7-day journal must outlive reconcile) |
| 8 | `StartPolling` per secret: the first poll fires immediately | yes | the first `ExecuteRotation` finds a reconciled ledger |

Reconcile is read-only with respect to Docker and to the journal: it never calls `MarkRecovered`/`MarkCritical*`, never edits `rotations.json`, never lifts the guard. It cannot unblock a service. It consults Docker only to compare live `(container_id, created)` with replica records (identity drift, 1.4) and to confirm a verified candidate is the live name holder when 4.2 relies on `Committed`. If `DockerClient==nil` (`performAutomaticRecovery` already returns early) the ledger step is skipped and entries stay as stored.

AutomaticRecovery behaviour is unchanged by this note. `RecoverFromCrash` remains independent of ledger persistence (R-Q1 answered in section 12).

## 6. Corrupt, missing, newer-schema ledger and key loss

The S2 store already classifies these (`Open`, `OpenResult.Reason`). This section fixes what the **engine** does with each result.

| Condition | Store mode | Engine behaviour | Operator-visible |
|---|---|---|---|
| no ledger file (first start) | durable, new ledger | `Observe` adopts every observed key: `desired` recorded, `applied=nil`, `adopted`; no rotation | info event; status shows "adopted (unverified)" |
| ledger corrupt (parse, validation, `body_sha256`, duplicate keys) | durable, file preserved aside (`ReasonCorrupt`) | new ledger, all keys re-adopted UNKNOWN; no rotation (X3) | WARN with the preserved path; `dso status` field |
| newer schema | memory-only, file untouched (`ReasonNewerSchema`) | in-memory adopt per start; `UpdateMemoryOnly` through the facade; no durable write | WARN; run continues |
| key absent, ledger has HMAC identities | durable, new ledger after preservation of the old one (`ReasonKeyMissing`) | all entries UNKNOWN; never a rotation (X3) | WARN naming the preserved ledger |
| valid key with a different id (mismatch) | `.keyloss-` preservation of key and ledger (`ReasonKeyMismatch`), new key, new ledger | as above | WARN |
| key present but unsafe/malformed | memory-only, nothing created or renamed (`ReasonKeyUnsafe/Malformed`) | in-memory adopt; operator must fix | ERROR |
| ledger directory unsafe, locked, unsupported platform, too large | memory-only | as memory-only | ERROR/WARN per reason |
| persistence fails at runtime | durable but degraded | facade falls back to `UpdateMemoryOnly` for the entry; retries the durable write on the store's probe | `persistence_degraded` event, WARN |

Common rules:

* **No mass rotation from any of these** (X3). The only thing that rotates is a genuinely observed change in provider version after a baseline exists, or an explicit operator action (REST rotate, `dso sync`).
* **Memory-only adoption is per process.** After a restart in a memory-only mode every key is adopted again; a provider change made while DSO was down is not detected until the next change. This is the price of "unknown never means changed". The legacy "rotate on start" behaviour (config `rotate_on_start`, rev 6.3 section 10.3) is the explicit opt-in for the other trade-off and belongs to the wiring slice (S6), not here.
* **Key rules for S3 (compatible with the approved S3 record):** the HMAC key is a dedicated file under the ledger directory, mode 0400, owned by the effective UID, never the central-store key (M2, frozen) and never the Web UI secret; `Generate()` runs only on the S2-defined absent/mismatch paths, always preserves an existing key (`.keyloss-*`), and never silently regenerates while an existing ledger depends on the key: S2 refuses to run a ledger against an absent or different key without the preservation recovery and the event above. Key bytes are never logged and never stored in the ledger or the journal. S4 needs read access to the key bytes to build `Version`s; `KeyProvider` (Inspect, Generate) has no such method, so the S3 provider must additionally expose a read-only material accessor (section 12.4) without changing the S2 interface.
* HMAC values made under different key ids are `Incomparable` (`ledger.Compare`); `NeedsRotation` treats `Incomparable` as "unknown", never "changed".

## 7. Adoption of existing managed workloads

First start with no ledger (or after loss): for each observed key the facade records `desired = d` and `applied = nil`, state `adopted`, and one replica per live owned container (`outcome=adopted`, `delivery=none`, `adoption=unknown`, `applied=nil`). **No rotation.** The ledger states exactly what DSO knows: the provider's current version, and nothing about what the running containers hold.

What can and cannot upgrade UNKNOWN:

| Evidence | Verdict | Reason |
|---|---|---|
| a later DSO-driven delivery with valid journal evidence (4.2) | **upgrades** (`delivery=verified`, `applied=Dn`) | the normal path |
| explicit operator redelivery (`dso sync`, REST rotate) | **upgrades**, same path | manual rotation is an ordinary journalled rotation |
| container label `dso.rotation.id=R` (`rotation.LabelRotationID`) matching a journal-`completed` R whose version is known | **upgrades that replica**, only for containers created after the versioned journal exists | a label proves which operation created the container; it cannot exist on pre-existing workloads |
| exec probe of a **file** secret: hash of the file content compared (in DSO memory, as a keyed HMAC) with the version | **delivery verified only; adoption stays `unverified`**; not in S4 | feasible read-only, but depends on the file layout and the N19 redesign; the application may still hold the old inode |
| **environment-variable comparison** via container inspect | **not used** | it is a plaintext comparison (DSO would read secret values out of Docker configuration into its own memory). Not allowed by the owner decision; not reopened here |
| container start time versus provider version time, Compose config hash, image digest | **not used** | heuristics prove nothing about the secret value |
| provider observation alone | **never** | observation is `desired`, not delivery |

Adoption therefore never sets `applied`. For pre-existing workloads the entry shows `adopted / unknown` until the next verified delivery.

## 8. Migration from the in-memory `secretHashes` baseline

### 8.1 First poll today (source-verified, not runtime-verified)

`secretHashes` (`sync.Map`, `provider:secret` → `ComputeHash`, an unkeyed SHA-256 of the JSON) is empty in a new process. `StartPolling` fires immediately; `commitObservation` sees `oldHash == ""`, which differs from the new hash, so it **dispatches a real rotation for every rotation-enabled secret at every agent start** (the code comment says this "catches rotations that happened while the agent was down"), subject to the cooldown, the service locks and the M0 guard (`BeginStartup`: no dispatch before recovery settles; unresolved operations block per service). The M0 first-poll guard semantics are the guard half and are unchanged by this note. The "rotate every secret at every start" half is exactly the mass-rotation hazard the ledger exists to remove.

### 8.2 Layering decision (rev 6.3 section 9.5 kept, adapted)

`secretHashes`, `rollbackHash`, `commitObservation`, the cooldown and the per-key mutex **stay** as the in-process dedupe and retry layer (the M1 tests that use them stay valid). The ledger is consulted at first observation of a key in the process and replaces the "empty baseline means dispatch" rule:

| Situation | Today | With the ledger |
|---|---|---|
| first poll, no ledger | dispatch | **adopt, no dispatch**; store hash and cache |
| first poll, ledger `applied == desired` verified | dispatch | **no dispatch** (the restart-unchanged case); store hash and cache |
| first poll, provider changed while down (`desired` ≠ stored `applied`, or `desired_seq` grew since adoption) | dispatch | dispatch (once) |
| first poll after adopt-UNKNOWN, value unchanged | dispatch | no dispatch (still UNKNOWN) |
| provider unreachable | nothing | nothing; nothing adopted, nothing recorded |
| ledger unusable (memory-only) | dispatch | adopt per start, no dispatch (6) |
| later polls | hash diff | hash diff, and `Observe` records the change |

This is a deliberate behaviour change of the first poll (fewer rotations) **[OWNER]**; it ships only with the BQ-3 decision below.

### 8.3 BQ-3 (`secretHashes` outside the engine, IPC `Rotated`)

Real text: `secretHashes` is read by `server.go:138` to compute the IPC `Rotated` flag; with adopt-without-rotate the flag would report `true` after a restart although nothing rotated. Options in the record: (a) accept and document, (b) make `ExecuteRotation` return its decision and update `server.go`, (c) a read-only `wouldDispatch` pre-check. **Decision (recommended) [OWNER]: (b)**, implemented as a new decision-returning method (`dispatched`, `adopted`, `unchanged`, `deferred`, `ledger_degraded`) with the current `ExecuteRotation` kept as a thin wrapper, so M1 tests that call it are untouched, and `server.go` computing `Rotated` from the decision. Rationale: (c) is check-then-act and duplicates ledger logic; (a) is dishonest in exactly the case operators rely on (`dso sync` while the ledger is degraded). **Coupling rule: adopt-without-rotate must not ship before (b)**, otherwise `Rotated` over-reports. The other `secretHashes` readers (the poll-interval back-off, `rotation_outcome_test.go`) keep working unchanged.

## 9. Ownership boundaries

| Store / datum | May write | Never |
|---|---|---|
| `rotations.json` (journal): records, transitions, status, commit, recovery marks | `StateTracker` via the strategies, `ExecuteRotation`/`finishRotation`, `AutomaticRecovery` | the ledger, the ledger facade, the bridge |
| `ledger.json`: `desired`, `desired_seq` | `Observe` only | anything else |
| `ledger.json`: `applied`, replica `applied/delivery/adoption/rotation_id/outcome` | the narrow transitions of section 10, on journal evidence | `Store.Update` called directly by production code |
| Docker | strategies and `AutomaticRecovery` | the ledger, the bridge, reconcile (read-only inspect only) |
| operation guard | `journalGuard` (reads the journal) | the ledger never lifts, adds or bypasses it |

**`Store.Update(fn)` and `UpdateMemoryOnly(fn)` mutate the whole document**, so production code could bypass every rule above. Decision **[OWNER, library change in S2.2]**: (1) rename them to unexported `update` and `updateMemoryOnly` when S4 lands (all `internal/ledger` tests are in-package, so they keep compiling); (2) production code gets only the S4 facade (`ledger.Versions`, built on `*Store`) whose exported methods are the transitions of section 10; (3) add an import/reference guard test that scans the repository and fails if any non-`internal/ledger` package references `RequestRedelivery`, `BeginRecovery`, `CompleteRecovery`, `RecoveryRecords`, `Entry.Rotation`, `EntryInProgress`, `EntryPartial` (before J1) or the unexported escape hatches by any exported wrapper; (4) `Gate()` stays exported but is advisory (it selects durable vs memory-only, section 6) and is **not** a Docker precondition.

## 10. The transitions S4 implements

Status (S4a implemented, unwired, uncommitted at the time of writing): `internal/ledger/transitions*.go` (facade `ledger.Versions`). Decisions taken where this section is silent are listed in the S4a change report; the main ones: a `Block` transition was added (needed to produce `blocked`), `ambiguous_provider` was added to the 10.1 vocabulary, and without a journal-carried version (O-2) reconcile cannot apply W6 and leaves the replicas unknown (no advance).

All are methods of the facade on top of the store (replacing rev 6.3's `Observe/Adopt/BeginRotation/CompleteRotation/Interrupted/Unblock`; `BeginRotation` disappears because the journal authorises). Each is one `update`, idempotent, returns `ErrNoChange` when nothing would change, and refuses (changes nothing) on an invalid transition. None performs Docker I/O.

### 10.1 Closed reason vocabulary (`Replica.Reason` / `LastOutcome.Reason`)

`adopted_unknown`, `delivery_requested`, `delivery_verified`, `adoption_unverified_signal`, `adoption_unverified_file` (N19), `interrupted`, `identity_changed`, `deferred`, `rolled_back`, `failed`, `unreported`, `no_rotating_strategy`, `strategy_none`, `eligibility_changed`, `journal_unreadable`. Anything else is a validation error in the facade.

### 10.2 Table

| Transition | Preconditions | Effect | Invalid → refused |
|---|---|---|---|
| `Observe(key, version, live)` | `version` is a computable identity; sole writer of `desired` | no entry → create `adopted`, `desired=d`, `seq=1`, `applied=nil`, replicas from `live` (`adoption=unknown`). Entry exists and `Compare(desired, d)==Same` → no write. `Different` → `desired=d`, `seq+1`, state `pending` (from `idle`/`adopted`), others unchanged; `Incomparable` (key change) → adopt that entry UNKNOWN. Replica set differs with no journal explanation → affected replicas `adoption=unknown`, `identity_changed`; **never** a rotation | `version` empty or `Version.validate` fails; `live` empty for a new entry (a zero-replica key is observed, not adopted: desired recorded, nothing else, rev 6.3 C-B) |
| `NeedsRotation(entry)` (pure) | none | true iff `state` not `blocked`, and (`Applied!=nil` and `Compare(desired, applied)==Different`) or (`Applied==nil` and `desired_seq>1`) or a replica `eligibility_changed` (D61-2). False for adopted-unknown with `desired_seq==1`, for `Incomparable`, for `Same` | n/a. The guard is **not** consulted here; `RotationBlocked` is asked per target by `TriggerReload` |
| `Correlate(key, rotationID, desiredSeq, version)` | journal record for `rotationID` exists (the caller just got nil from `StartRotationWithID`); entry exists; `desired_seq==desiredSeq` | targeted replicas `delivery=requested`, `rotation_id=R`. Writes no intent, no phase, no state change other than replicas | empty id; unknown entry; stale `desiredSeq` (`ErrStale`, no change); replica already `verified` for the same R (idempotent no-op) |
| `RecordDeliveryVerified(key, evidence)` | `evidence` valid per 4.2 (strategy-specific kind, `Seq>0`, success status); replica `rotation_id==evidence.RotationID` and `delivery` in {`requested`, `verified`}, or reconcile supplies the version | replica `delivery=verified`, `applied=version`, `adoption` per 4.2 (`verified` / `unverified`), reason per 10.1 | no/forged/pre-ready evidence (`ErrNoEvidence`); signal with `adoption=verified`; `critical_error`/`rolled_back`/`recovered`-only evidence; version older than the replica's current applied (no downgrade) |
| `RecordApplied(key, rotationID)` | every **targeted** replica of the entry `delivery=verified` for `rotationID` (non-targeted `blocked` replicas allowed, rev 6.3 section 27.1); same `Update` as `RecordDeliveryVerified` | `applied=version`, state `idle` (or `pending` if `desired` moved on), `last_outcome=success`; idempotent per `rotationID` | any targeted replica not verified; `rotationID` not the latest correlated; state `blocked` |
| `RecordAdoptionVerified(key, replica, evidence)` | **only** with new evidence: a restart/rolling success (already covered by `RecordDeliveryVerified`), a label-proven container (section 7), or a future file-probe upgrade | `adoption=verified` for that replica | any signal evidence; evidence-less call; env comparison |
| `HandleInterrupted(key, rotationID, cause)` | the journal record is unresolved (`critical_error`, inherited `in_progress`, `rollback_required`) or R is absent from the journal and no process owns it | replicas of the affected service: `delivery=none` (if `requested`), `adoption=unknown`, reason `interrupted`; `applied` **unchanged**; `state=failed`, `last_outcome` set. Never advances, never creates anything the guard must honour | R is the live process's own in-flight rotation (the facade is told by `finishRotation`, not by guessing) |
| `ReconcileFromJournal(journalView, live)` | startup only, after M0 recovery (section 5), journal readable | for every ledger replica with `rotation_id`: apply rules W3-W12 (clear `requested` when the journal lacks R or lacks evidence; `RecordDeliveryVerified`+`RecordApplied` when evidence is complete; rule R-A/R-B downgrade when the ledger claims more than the journal). Drops identity-drifted replicas to `unknown`. One `update` per entry, ordered by key | journal unreadable (skip, warn); records older than the ledger's `created_at` (ignored) |
| `Unblock(key, cause)` (D61-1: two steps) | entry `blocked`; reason in the trigger table below; the trigger holds against the live set; persistence healthy | `blocked → pending` if `desired≠applied`, else `→ idle`. Changes **only** `state`; leaves `desired`, `applied`, replicas, `last_outcome` untouched (I-B4). Step 2 (the rotation) is the ordinary engine path under the guard | any other block reason; no trigger satisfied; called with persistence unavailable |

Unblock triggers: `no_rotating_strategy`: **T1** a new eligible live replica or **T2** a recorded replica whose strategy now dispatches (config change applied at agent start); `strategy_none`: **T3** the secret-level strategy is no longer `none` after a configuration change applied at agent start (specified here for D61-3); `ambiguous_provider` and anything else: no trigger, refused.

Entry state after a dispatch: success → `idle`/`pending`; failure of the dispatch → `failed` or `rolled_back`; deferred/blocked by the guard → `pending`; `ErrNoTargetsRotated` → `blocked` (`no_rotating_strategy`). `partial` and `in_progress` are not produced (1.2).

## 11. Acceptance-test matrix (deterministic, Docker-free)

Harness (all in-package, no Docker, no network): a real `StateTracker` in a temp dir and a real `ledger.Store` with the S2 fake `KeyProvider` in another; a scripted "rotation runner" executes the ordered steps of 4.3 and stops after step *k* (simulated crash = close both stores, reopen from the same directories); a fake live-set (container list) and a fake clock. M0 recovery is represented by directly marking journal records (`MarkRecoveredRecord`, `MarkCriticalRecord`) as `AutomaticRecovery` would, or by running `AutomaticRecovery` against the existing Docker-free fakes where they exist. Every case asserts both stores' durable state, the engine decision (`NeedsRotation`), and that the second run of the same reconcile changes nothing.

### 11.1 Crash windows

| Test | Window | Asserts |
|---|---|---|
| T-W1 | W1 | restart: desired=D2 durable, `NeedsRotation` true, exactly one dispatch |
| T-W2/3 | W2, W3 | `requested` cleared, applied=D1, one re-dispatch, none before guard settle |
| T-W4 | W4 (before ready) | replicas `unknown`, applied unchanged, **never D2**, no dispatch while the record is `critical_error` |
| T-W5 | W5 (after ready, no commit) | no advance; one extra rotation allowed, none skipped |
| T-W6 | W6 (**N21-dependent case 8**: journal commit recorded, ledger interrupted) | after reconcile applied=D2, `idle`, **zero dispatches**, second reconcile no write; the matching M0 test `docs/design/m0-04-recovery-design.md` section 5 row 8 turns green |
| T-W7 | W7 | single-`update` atomicity: no observable "verified but not applied" state |
| T-W8 | W8 | ledger persistence fault injected (store `ops` seam): no re-dispatch in process; restart converges from the journal |
| T-W9 | W9 signal | `adoption=unverified`, `applied` advanced, no loop of signal rotations |
| T-W10 | W10 | `requested` cleared when the journal record was deleted |
| T-W11 | W11 | fresh ledger ignores older journal history |
| T-W12 | W12 | **ledger applied=D2 with journal lacking evidence → not accepted**, corrected to unknown; variants: R absent and recent (downgrade), R absent and older than 7 days (accepted), R present without evidence (downgrade), R `recovered` only (downgrade) |
| T-W13 | W13 | torn write: either complete document; the same final state after reconcile |
| T-W14 | W14 | journal write fails → no dispatch, no ledger citation |
| T-W15 | W15 | failed rotation: applied unchanged, retried next poll |
| T-EV | evidence | `candidateVerified` and the ledger evidence rule agree for every transition name (table-driven over `rotation.Tr*`); signal never yields `adoption=verified`; file-mode yields `unverified` |
| T-GUARD | X1 | no ledger transition changes the guard result; a service with an unresolved record is not dispatched however the ledger reads |
| T-IMPORT | section 9 | repository scan: only the facade is referenced from outside `internal/ledger` |

### 11.2 The owner's 15 trigger scenarios

| # | Scenario | Setup | Expected |
|---|---|---|---|
| 1 | initial startup, no ledger | fresh dirs, provider returns D1 | entry `adopted`, applied nil, replicas `unknown`, **0 dispatches**, hash and cache stored |
| 2 | startup with existing managed workloads | live set has 3 owned replicas, no ledger | same as 1; no rotation; status shows unverified |
| 3 | restart, desired and applied unchanged | applied=desired=D1 verified | no dispatch, no ledger write, cache populated (PE-03) |
| 4 | restart, desired changed | applied=D1, provider D2 | `Observe` → `pending`, one dispatch, then applied=D2 |
| 5 | provider unavailable at startup | fetch error | nothing observed, adopted, recorded or dispatched; retry on backoff |
| 6 | ledger corruption | garbage `ledger.json` | preserved aside, fresh ledger, all UNKNOWN, **0 dispatches**, WARN with the preserved path |
| 7 | key unavailable | key absent / mismatch / unsafe / malformed (S2 fake provider states) | absent/mismatch: preserved, all UNKNOWN, 0 dispatches; unsafe/malformed: memory-only, 0 dispatches, ERROR; no `Generate` for unsafe or malformed |
| 8 | concurrent provider updates | two goroutines observe D2 and D3 for one key | per-key mutex orders them; `desired_seq` monotonic; ledger desired equals `secretHashes` winner; no double dispatch for one value |
| 9 | concurrent manual and automatic rotation | REST/IPC rotate while the poll dispatches | one runs, the other sees the service lock/guard (deferred); ledger written once per `RotationID`; no duplicate `applied` |
| 10 | partial replica delivery | 3 replicas, dispatch ends partial | v1: every targeted entry `failed`, applied unchanged (all-or-nothing, 1.5); next poll re-dispatches |
| 11 | failed rotation | strategy error / rollback | `failed`/`rolled_back`, applied unchanged, retried |
| 12 | interrupted rotation | crash windows W2-W5 | per 4.3 |
| 13 | recovery after delivered-but-ledger-update-interrupted | T-W6 | applied=D2 from the journal, no second rotation |
| 14 | signal sent, adoption unverified | signal strategy to `signal_done` | `delivery=verified`, `adoption=unverified`, applied=D2; status shows unverified |
| 15 | unknown/ambiguous workload identity | recreated container, name reuse, duplicate Compose number, missing labels | replica `unknown` (`identity_changed`), **no rotation**; label-loss makes a new entry (adopt) and leaves the old inert |

Gate for S4a/S4b: T-W*, T-EV, T-GUARD, T-IMPORT and the 15 scenarios pass; `go test -race` clean; no test touches Docker; the existing 120 `internal/ledger` tests stay green except those deliberately adapted by the S2.2 schema change (each named and justified in that change).

## 12. Open questions: what this note resolves, defers, or leaves to the owner

Real text is quoted or closely paraphrased from `docs/audit/48-LEDGER-OPEN-QUESTIONS.md` and the design record. "Resolves" means a recommendation made here; where the record marks the item `[PO]` it is a **recommendation needing owner confirmation**, not a decision.

### 12.1 Resolved by this note (recommendation; owner confirms where `[PO]`)

| ID | Real question | Answer and rationale | Owner confirm |
|---|---|---|---|
| D61-1 | Add an explicit `Unblock` operation (two writes) vs folding unblock into `BeginRotation` (one write, breaks I-B3) | **Two-step `Unblock`** (section 10.2). `BeginRotation` no longer exists; the engine's normal dispatch is step two | yes (`[PO]`) |
| D61-2 | Which reason authorises dispatch after T2 when `desired == applied`: (a) add `eligibility_changed`, (b) extend `external_recreation`, (c) require operator action | **(a)**, as an in-memory `NeedsRotation` reason (`eligibility_changed`) because the replica never received the secret (it was non-rotating). `external_recreation` is not used at all (1.4) | yes |
| D61-3 | How to record a secret whose strategy is `none` (`ExecuteRotation` returns early): (a) blocked with a distinct reason, (b) suppress before the gate, (c) do not track | **(a)** `blocked`, reason `strategy_none`, **with trigger T3 defined** (10.2) so `Unblock` is defined rather than refused | yes |
| R6-Q1 | Is detection of eligibility change/external recreation only at agent start and first observation (runtime detection deferred) acceptable | **Accept the deferral.** Identity drift only downgrades a replica to `unknown`; it never needs prompt detection | yes |
| R6-Q2 | Unreported live replica after completion is recorded `pending`; confirm `pending` | **Confirm `pending`** (outcome `pending`, reason `unreported`, `delivery=none`): conservative; at most one extra rotation | yes |
| R6-Q4 | Are recovery actions independent enough to continue past a failed artifact removal? | **Moot.** The ledger has no recovery actions (REMOVE-FROM-USE). M0 recovery keeps its own halt semantics | no (consequence of the binding decision) |
| R-Q1 | While persistence is unavailable, may recovery restart a stopped original under its normal name without a durable plan? | **Moot/answered.** Recovery is M0's and never depends on ledger persistence; no exemption is needed or created | no |
| R-Q2 | Authorise refactoring `recovery.go` into plan/execute phases with an optional journalling hook | **Not needed.** No ledger recovery journal exists; `recovery.go` is untouched by this design (a separate M0 decision if ever wanted) | no |
| R-Q3 | Ledger-intent recovery trigger ignores the 5-minute age rule | **Moot** (no ledger intent) | no |
| R-Q6 | Acknowledge the caller-level snapshot window between the gate snapshot and the `TriggerReload` scan | **Moot.** `Gate()` is not a Docker precondition any more; the remaining window between `NeedsRotation` and the journal intent is covered by the per-key mutex, the secret lock and the guard | no |
| BQ-3 | `secretHashes` is touched outside the engine; IPC `Rotated` can over-report: (a) accept, (b) return the decision, (c) as in rev 3 | **(b)**, with the coupling rule of 8.3 | yes (touches `trigger.go`/`server.go`) |
| P-3 | non-Compose watcher lock identity must match the ledger's stable-name identity | **Ledger side resolved** (1.4: `("", container name)`). The watcher side (lock key still falls back to the container id) stays a BL-03 prerequisite | no for the ledger side |

### 12.2 Deferred (and why none blocks S3 or S4a/S4b)

| ID | Real question | Why deferred / why not blocking |
|---|---|---|
| R-Q4 | nil `ManualRotator` means the legacy direct path, tests only | REST wiring (S6). Interim state is safe: the manual path bypasses the ledger, so the ledger can only be **stale-low** (an extra rotation), never stale-high |
| R-Q5 | Manual redelivery semantics (verify cache vs ledger desired, 409 stale, no auto retry, cancel at restart) | `RequestRedelivery` is KEEP-UNWIRED. Manual rotation is a journalled rotation; a durable request would be a second intent store |
| R-Q7 | Approve a scoped warning-only `agent.rotation` doctor check as the P-1 design | config/doctor work; independent of ledger transitions |
| P-1 | the config loader is non-strict (`yaml.Unmarshal`, no `KnownFields`), so a typo such as `rotate_on_stat` is silently ignored | independent; matters only when `rotate_on_start` is introduced (S6) |
| P-2 | source investigation of loader/doctor/`dso validate` complete, implementation unauthorised | same as P-1 |
| A-13 | the "revised S1 gate" redefinition of the S1/S1.1/S3 test gates (a `[PO]` proposal, not adopted) | process item. Recommendation: do not adopt it as written; use the section 11 matrix as the S4 gate and the existing passing S1/S2 tests as the S1/S2 gate. Owner decision |

### 12.3 Needs an owner decision before the dependent slice (recommended answer in brackets)

| ID | Decision | Blocks |
|---|---|---|
| O-1 | schema version 3 (`rotation_id`, `delivery`, `adoption` per replica) [yes, S2.2] | S4a |
| O-2 | journal carries the target version: `SecretVersion` set at `StartRotationWithID` [yes] | S4b exactness (not S3) |
| O-3 | the ledger is advisory to dispatch: `Gate()`/degraded persistence does not stop a changed-value rotation M0 would permit (deviates from rev 6.3 D-3 R5 and D-S2-2 "S4 transitions must call `Gate()`") [yes: the ledger's purpose is to prevent redundant rotation, not to stop secret rotation] | S6 |
| O-4 | adoption records `applied = nil` (UNKNOWN), replacing rev 6.3 M2-2's "adopt: `applied = d`" [yes, forced by the binding decision] | S4a |
| O-5 | first poll no longer rotates when ledger or memory-only state has no baseline (section 8.2) [yes, ships with BQ-3 (b)] | S6 |
| O-6 | journal change J1: records keyed by `(RotationID, ServiceID)` for real per-replica `partial` [defer; v1 is all-or-nothing] | not S4a/S4b |
| O-7 | `StartRotationWithID` failure must stop the dispatch when the ledger is wired (W14) [yes] | S6 |
| O-8 | ledger directory separate from the M0 state dir, e.g. `/var/lib/dso/ledger` (own lock, own key, own permission checks) [yes] | S3 (path) |
| O-9 | unexport `Update`/`UpdateMemoryOnly` and add the reference guard (section 9) [yes] | S4a |

### 12.4 What S3 (key management) needs from this design

S3 does **not** depend on S4 and can start now (it still needs its own prior approvals: D3-5 for S2.1, then S3a, S3b, each separately authorised per the S3 record). From this design S3 needs exactly:

1. **Boundary unchanged:** `KeyProvider{Inspect, Generate}` stays as D-S2-1 defines it. The ledger directory is a dedicated one (O-8); the key file lives there, mode 0400, euid-owned; it is never the central-store key and never the Web UI secret (section 6).
2. **A read-only material accessor** on the production provider, outside the S2 interface (for example `KeyMaterial() (keyID string, key []byte, err error)`), returning a copy bound to the same inspected key, so S4 can call `ledger.NewHMACVersion(keyID, key, provider, secret, data)`. The caller must zeroise the copy; the key is never logged. Without it S4 cannot build any HMAC identity.
3. **Semantics S3 must preserve for S4:** key absent or mismatched means a new key id, so every older HMAC becomes `Incomparable`, hence UNKNOWN, never "changed" (X3); an existing key is never overwritten or deleted, only preserved (`.keyloss-*`); `Generate()` is never called for an unsafe or malformed key.
4. **No dependency on rotation state:** S3 reads neither the journal nor the ledger entries; the S2.1 recovery intent (key-recovery crash window, D3-5) is orthogonal to rotation recovery and is unaffected by this note.

### 12.5 Recommended implementation order

| Step | Slice | Depends on | Notes |
|---|---|---|---|
| 0 | owner confirmations O-1..O-9 and the `[PO]` items above | | the only gate for S4 |
| 1 | S2.1 recovery intent (if D3-5 is approved) → S3a → S3b | D3-5, separate authorisations | parallel with 2 and 3; adds the key-material accessor |
| 2 | **S2.2 library amendment**: schema v3, unexport `update`/`updateMemoryOnly`, reference-guard test | O-1, O-9 | library only; no wiring |
| 3 | **S4a facade transitions** (`Observe`, `NeedsRotation`, `Correlate`, `RecordDeliveryVerified`, `RecordApplied`, `RecordAdoptionVerified`, `HandleInterrupted`, `Unblock`), in-package tests with fake journal evidence | 2 | buildable **before any trigger wiring**, and before S3 if it uses fake keys; no changes to `internal/agent` or `internal/server` |
| 4 | **S4b bridge** (`internal/agent/ledger_bridge.go`, new file): journal `RotationState` → `Evidence` (reusing `candidateVerified`), `ReconcileFromJournal`, fake live-set, the W1-W15 and 15-scenario matrix against a real `StateTracker` | 3; O-2 for exact versions | still unwired; N21 case 8 verified here; touches no existing agent file |
| 5 | M0 follow-ups, separately authorised: `SecretVersion` at start (O-2); journal keying J1 (O-6, optional) | owner | |
| 6 | **S6 wiring**: ledger open in `NewTriggerEngine`, `Observe` inside `commitObservation`, `Correlate`/terminal calls in `ExecuteRotation`/`finishRotation`, reconcile at the end of `performAutomaticRecovery`, BQ-3 (b), status/warnings, behind an explicit enable until verified | 3, 4, O-3, O-5, O-7, BQ-3 | the only slice that edits `trigger.go`/`server.go`; the first-poll change ships here, together with BQ-3 (b) |

## 13. Limits of this note

Source-verified (read in this session): the structure and ordering claims about `trigger.go`, `state_tracker.go`, `recovery.go`, `operation_guard.go`, `journal.go`, `controller.go` and `internal/ledger`. Not runtime-verified: the first-poll "dispatch every start" behaviour (read from code, not exercised), the real Docker behaviour of every window, and any timing figure. No test was run and no code changed. Rev 6.3 lives outside git and is read, not edited; pinning it into `docs/` remains the owner's call.

## 14. Directory contract

Every writer of the persistent-state tree must agree with this table. It is pinned by `internal/bootstrap/permissions_contract_test.go`, `internal/ledger/key_contract_linux_test.go` and the setup doctor/repair tests.

| Path | Owner | Mode | Writer component |
|---|---|---|---|
| `/etc/dso` | `root:dso` | 0775 | bootstrap permission step (`dsoDirectoryPlan`) |
| `/var/lib/dso` | `root:dso` (`root:root` if no `dso` group) | 0750 (accepted change b360128; was 0770) | bootstrap; setup repair `REPAIR-STATE-001`; the root agent writes inside |
| `/var/run/dso` = `/run/dso` | `root:dso` | exactly 0750, strict | systemd `RuntimeDirectory`, the agent (`runtimedir`), bootstrap (strict verification) |
| `/var/log/dso` | `root:dso` | 0770 | bootstrap |
| `/var/lib/dso/state`, `/var/lib/dso/locks` | root | agent-created | rotation journal and locks (agent) |
| `/var/lib/dso/ledger` (`ledger.DefaultKeyDir`) | root | 0700, private | ledger key provider (not bootstrap) |
| `ledger/ledger.key` | root | 0400 (0600 accepted) | ledger key provider; never regenerated over a lost, unsafe or corrupt key |
| `ledger/ledger.json`, `ledger.lock` | root | 0600, private | ledger store |
| `/usr/local/lib/dso/plugins` | root | root-owned, no group/world write (0755) | bootstrap provider-plugin installer |

Rules that follow from it: the ledger key provider and the plugin trust checks refuse a group- or world-writable ancestor, so `/var/lib/dso` must stay 0750; nothing under `/run/dso` is durable. Permission repair is non-recursive by construction: `bootstrap.ApplyDirectoryNoFollow` opens the one directory with `O_NOFOLLOW|O_DIRECTORY`, applies `fchown`/`fchmod` on the descriptor, refuses a symlink or non-directory, touches nothing already correct, and never visits files inside (plugins, databases, keys, user content). No code path runs a recursive chmod/chown, and none regenerates a lost key.

An installation upgraded from an earlier build still has `/var/lib/dso` at 0770; the ledger then opens disabled (code `unsafe_permissions`). The agent logs one warning at start, `docker dso status` shows a `LEDGER` block, `docker dso doctor` reports `DSO-DOCTOR-024`/`-025`, and the supported fix is `sudo chown root:dso /var/lib/dso && sudo chmod 0750 /var/lib/dso` (or `sudo docker dso bootstrap agent`, which also rewrites `dso.yaml`) followed by an agent restart.

## 15. Guarantees and degraded mode

**Failure model.** Single host, one agent process, crash-stop (power loss, SIGKILL, OOM) with durable local files. Provider reads, Docker operations and the two durable stores (rotation journal and ledger) are separate files with no cross-file atomicity (section 4.1). No consensus, no clock assumption, no byzantine behaviour.

**What is guaranteed.**
- The rotation journal (M0) is the only rotation authority. The ledger is advisory: it decides whether the engine *wants* a rotation; every dispatch still goes through the journal and the per-target operation guard, and the ledger never bypasses either.
- Rotation is **at-least-once** with journal/ledger reconciliation, not exactly-once. After a crash between the journal's terminal write and the ledger update, the startup reconcile converges the ledger from the journal-carried version (O-2); a crash before the journal start leaves nothing to reconcile and the next poll decides again. Where the journal cannot prove delivery (a record deleted, a claim the journal does not support) the ledger keeps `applied` unknown rather than inventing it, and a re-rotation is possible. There is **no distributed exactly-once guarantee**: a rotation can be repeated after a crash, and consumers must tolerate the same secret version being applied twice.
- A new or recovered ledger with an existing workload records `desired = V` and `applied = UNKNOWN` with every replica adoption unknown; it never records `applied = V` without a verified delivery. A signal reload is delivery-verified but adoption **unverified**, never adoption-verified (N19, file mode likewise).
- An unusable ledger (unsafe permissions, lost or corrupt key, newer schema, lock held, unsupported platform) never causes a rotation, never deletes or rewrites the evidence, and never regenerates a lost key.

**Degraded mode (ledger disabled).** Durable version tracking is unavailable for that process. The first observation of each secret in the process is adopted as the in-memory baseline without rotating (no mass rotation); a later change in the same process rotates as before. **Limitation:** a provider change made *while the agent was down* is not detected on restart, because there is no durable baseline to compare against; it is picked up only at the next change of that secret. The same applies to a rotation interrupted by a crash: the journal still recovers it, but the ledger cannot reconcile or suppress a repeat. The state is announced once at start (warning with code, path, actual versus expected, remediation, degraded behaviour), in `docker dso status` and in `DSO-DOCTOR-024`; it is never silent.
