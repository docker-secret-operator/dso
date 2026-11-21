# Decision record for dso central (durable form; R-1 to R-7 signed off by the owner 2026-11-04)

Status: **Durable record, written 2026-11-04. The owner reviewed it and approved the seven decisions R-1 to R-7 as
presented on 2026-11-04 (Part 7).** It lists those decisions with their wording, scope, limitations and
implementation evidence. Under the process in `README.md`, a decision is formally ratified when the owner reviews
and explicitly approves a separate durable record; R-1 to R-7 are ratified on that basis, within the wording and
limits shown. **Nothing else is ratified by this record:** Parts 3 and 4 stay pending or open, and the owner's
approval was of the content as presented, not of any later change. Nothing here adds a decision: every item is
taken from the owner's own statements in the working sessions or from verified code, and each says which.

How to read it. *Owner statement* = something the owner wrote in a working-session instruction (stored in that
session, not in this repository). *Verified* = read from code or tests in this checkout. Nothing is inferred beyond
that. Items the owner has not stated, or stated only partly, are in Parts 3 and 4, not in Part 1.

Baseline when written: branch `feature/webui-phase1`, HEAD `036db82`. Implementing commits: `62ab4b2` (REVEAL-B),
`a3b96ec` (CLI audit logger), `036db82` (backup/restore audit events).

## Part 1. Decisions the owner stated as ratified

### R-1 (A-1, CHAIN-A): unkeyed SHA-256 audit chain

- **Decision.** For the current milestone `audit_events` keeps the unkeyed hash chain
  `event_hash = SHA-256(prev_hash || canonical(fields))`. No MAC, signing key, key management or external anchor is
  added. No schema, canonical-hash or verifier change.
- **Source.** Owner statement 2026-11-04 ("ratified policy that must remain unchanged: A-1 / CHAIN-A"); earlier
  owner selection recorded in `2026-10-30-central-audit-chain-integrity.md`.
- **Limitations that travel with it (verified).** The chain does not detect deletion of the newest events or
  replacement by an older internally valid database; anyone who can write the database can drop the append-only
  triggers and recompute the chain; a successful `audit verify` is not proof of authenticity; verification works on
  a staged copy and is not a consistent snapshot of a database being written.
- **Not decided here.** CHAIN-B (keyed chain), CHAIN-C (external anchor), rollback protection.

### R-2 (A-2, AUDITFAIL-A): availability-first for best-effort audit events

- **Decision.** A failure to persist a best-effort audit event never changes the operation's result. Mutations that
  already run in a transaction record their success event atomically and roll back with it. The exceptions are
  R-5 (reveal success) and, by design, the events in R-7 that are explicitly best-effort.
- **Source.** Owner statement 2026-11-04 ("A-2 / AUDITFAIL-A ... Existing atomic mutation-audit behavior remains
  unchanged").
- **Limitations (verified).** Reads and listings are not audited; some early failure paths return without any
  failure event (for example the reveal AAD-build error, project/environment ID and key errors, wrong-root-key
  `Bind` failures); reveal success, project-creation, environment-creation and `init` events carry no actor;
  log-only reporting is not durable or tamper-proof evidence.

### R-3 (D-1): no external rollback anchor or audit sink

- **Decision.** No external audit sink and no external rollback anchor in this milestone.
- **Source.** Owner statement 2026-11-04 ("Do not introduce an external audit sink or rollback anchor. D-1
  remains ratified"); earlier wording "External rollback anchor remains deferred."
- **Consequence (verified).** Restoring an older valid backup is not detected and drops newer events from the
  active chain; they survive only in the safety snapshot. Whether the deferral has a target milestone is not
  recorded (see Part 4).

### R-4 (D-2): restore validation

- **Decision.** Restore validation includes SQLite structural integrity, schema/migration compatibility, full
  audit-chain self-consistency, and root-key binding. It does not require decrypting or authenticating every secret
  ciphertext.
- **Source.** Owner statement 2026-11-04. This states the check set that the 2026-10-30 proposed record listed as
  uncertain for D-2.
- **Verified.** `sqlite.ValidateStagedCandidate` and `validateStagedCandidateKey` perform exactly these checks on
  one staged copy that is also the activated artifact.

### R-5 (REVEAL-B): secret reveal persists its audit event before disclosure

- **Decision.** `secret get --reveal` must persist its success audit event before returning plaintext. If the
  event cannot be persisted, no plaintext is disclosed and the decrypted buffer is zeroed.
- **Source.** Owner statement 2026-11-04; implemented in `62ab4b2`.
- **Verified.** `Service.GetValue` appends the success event synchronously and zeroes the plaintext on append or
  ID-generation failure. Test: `TestAudit_RevealAuditWriteFailureFailsClosedAndDisclosesNothing`. Failure events
  for reveal remain best-effort (R-2).
- **Limitations.** The reveal success event has no actor. No test observes the zeroing directly.

### R-6 (CLI audit logger)

- **Decision.** Best-effort audit persistence failures are surfaced through the Error-level-filtered CLI logger.
  Logging is not broadened and must not expose sensitive material.
- **Source.** Owner statement 2026-11-04; implemented in `a3b96ec`.
- **Verified.** `centralAuditLogger()` (Error level only, Nop fallback) is used by `(*centralFlags).open`, the
  `init` bind call, `backup` and `restore`. Test: `internal/cli/central_audit_logger_test.go`.

### R-7 (D-3 and backup/restore audit coverage)

- **D-3 (CLI and confirmation).** The `backup --out`, `restore --from`, `--dry-run` and `--force` names and
  confirmation semantics are unchanged. Owner statements 2026-11-04: confirmation is an explicit y/N prompt that
  defaults to No; a refusal, EOF or closed stdin leaves the active database byte-for-byte untouched; the
  blocking-pipe limitation is not addressed.
- **Audit coverage (event type `backup` or `restore`; outcomes `success` or `failure`; no new outcome, schema,
  hash-input or verifier change):**

  | Event | Outcome | Reason (fixed constants only) | Written to |
  |---|---|---|---|
  | `backup` | `success` | none, or `sync_warning` | active database, after the backup is created and verified; never inside the backup file |
  | `backup` | `failure` | `destination_invalid`, `destination_exists`, `backup_creation_failed`, `verification_failed` | active database |
  | `restore` | `success` | none | restored database, after replacement and reopen |
  | `restore` | `failure` | `snapshot_failed`, `checkpoint_failed` | original active database, while still open |
  | `restore` | `failure` | `database_replace_failed` | original active database, only if it reopens successfully |

- **Binding rules (owner statements 2026-11-04).**
  1. Every append is best-effort; a failure is logged at Error level and never changes the operation's result.
     A restore that succeeded stays successful if its success event cannot be recorded.
  2. A directory-fsync warning is a successful backup (`sync_warning`). The `backup` command keeps its existing
     behavior of returning that warning as an error; changing that needs separate authorization.
  3. The safety snapshot taken inside restore emits no separate `backup` event.
  4. Never write an audit event to an uncertain or unvalidated database.
  5. **Unaudited, deliberately:** declined confirmation (recording it would open and write the active database;
     the D-3 guarantee takes priority), dry-run success or failure, validation failures, wrong root key,
     lock-busy, and a failed reopen after replacement.
  6. Reason categories are fixed constants; no error text, paths, keys, plaintext or ciphertext in audit metadata.
- **Source and evidence.** Owner decisions and working-session approvals dated 2026-11-04; implemented in `036db82`. Tests include
  real-SQLite trigger-based persistence-failure tests and byte-for-byte declined-restore tests.

## Part 2. Consolidated accepted limitations (not to be changed without approval)

- The chain is unkeyed and cannot detect tail deletion or rollback to an older valid copy (R-1, R-3).
- Audit events for reads and listings do not exist; some failure paths record no event (R-2).
- Actor labels are caller-supplied and unauthenticated; several event types carry none.
- `dso central` has no authentication or authorization; any process running as the owning OS user can reveal,
  disable, delete, back up and restore. There is no listener and no peer-credential check in the central code path.
- Backups, safety snapshots and audit events are never pruned automatically.
- The commands rely on file permissions and a cross-process file lock (`internal/store/sqlite/centrallock.go`), not on a
  key-holding service.
- Failed or refused restores, wrong-key and validation attempts leave no audit trace (R-7 rule 5).

## Part 3. Previously proposed items still not formally resolved

The 2026-10-30 register records these Step 5.4 items as substantively approved earlier but with unverified scope.
They were **not restated** by the owner in the 2026-11-04 instructions, so this record does not promote them.

| Item | Status |
|---|---|
| D-4 no automatic backup pruning | Behavior holds (no pruning exists). Whether a manual retention policy was intended is unanswered. |
| D-5 caller restrictions are intentions, not enforced | Behavior holds. The term "Caller-#1" is still undefined. |
| D-6 Step 5.3 audit retention not reopened | Behavior holds (append-only, no retention logic). The decision's content is still unrecorded. |

## Part 4. Open or deferred (no decision recorded)

- **Authorization architecture (ARCH-A/B/C).** Owner confirmed 2026-10-30 that analysis and design of ARCH-B
  (key-holding service) only may proceed. No architecture is selected and implementation is not authorized.
- **Step 5.5 project/environment lifecycle** (archive/purge, slug mutability, key scope, audit identifiers,
  restore of older lifecycle state, authorization of destructive operations).
- **Actor attribution** for reveal, project/environment creation and `init` (schema implications unproposed).
- **Reveal AAD-build error** currently records no failure event; whether to add one is undecided (A-2 lists it).
- **`backup` on a missing database** currently creates a new empty database and succeeds (observed 2026-11-04).
  Whether `backup` should refuse a missing path is a proposal only and needs explicit approval because it changes
  CLI behavior.
- **Directory-fsync warning exit status** of `backup` (R-7 rule 2).
- **Multi-process audit-append verification** (no cross-process audit test exists).
- **Integration base** `origin/feature/m1-webui-reconcile` is a proposal only.
- **M2** (identity, sessions, `WithTx` follow-ons) beyond M2.1 is not authorized; **quarantine (B-1)** is deferred.
- **Process hardening, supported platforms and filesystems.**

## Part 5. Implementation traceability

| Decision | Commit | Main evidence |
|---|---|---|
| R-5 REVEAL-B | `62ab4b2` | `internal/centralsvc/service.go` `GetValue`; `audit_test.go` |
| R-6 CLI logger | `a3b96ec` | `internal/cli/central.go` `centralAuditLogger`; `central_audit_logger_test.go` |
| R-7 backup/restore audit | `036db82` | `centralsvc/backup_audit.go`, `restore.go`; `backup_restore_audit_test.go`; CLI backup/restore tests |

## Part 6. Existing documents this record updates (not duplicated)

- `2026-10-30-central-ratification-record-PROPOSED.md`: its Part A items and Part D D-1, D-2, D-3 correspond to
  R-1 to R-4 and R-7; a pointer banner is added there, and its boxes remain unmarked.
- `README.md`: status note, document table and "Still undecided" list updated.
- Left unchanged but containing statements that are now out of date (flagged for the milestone that touches them):
  `2026-10-30-central-audit-failure-policy.md` and `2026-10-30-central-audit-failure-per-operation-DRAFT.md` (they
  describe the CLI logger as absent and backup/restore as unaudited); `docs/central-store-m1.md` (partially
  updated; still says restore "takes no cross-process lock" although it takes an exclusive one,
  `internal/centralsvc/restore.go`).

## Part 7. Owner sign-off

On 2026-11-04 the owner stated in a working-session instruction: "I explicitly approve the seven R-1 through R-7
decision entries as presented in the November 4 decision record." The boxes below record that approval, entered
on the owner's instruction; they apply to the wording and limits in Part 1 as reviewed that day, including the
R-7 provenance wording ("working-session approvals dated 2026-11-04"). Approval of this record's content is not
approval of any item in Parts 3 or 4, and none of those is marked.

- R-1 CHAIN-A: ☑ ratify   ☐ amend   ☐ reject
- R-2 AUDITFAIL-A: ☑ ratify   ☐ amend   ☐ reject
- R-3 D-1 no anchor: ☑ ratify   ☐ amend   ☐ reject
- R-4 D-2 restore validation: ☑ ratify   ☐ amend   ☐ reject
- R-5 REVEAL-B: ☑ ratify   ☐ amend   ☐ reject
- R-6 CLI audit logger: ☑ ratify   ☐ amend   ☐ reject
- R-7 D-3 and backup/restore audit coverage: ☑ ratify   ☐ amend   ☐ reject
