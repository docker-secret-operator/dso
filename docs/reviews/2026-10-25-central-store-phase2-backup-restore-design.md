
# `dso central` Phase 2 Step 5.4 — Backup and Restore Design (design only, no implementation)

**Date:** 2026-10-25
**Status:** Design document only. No Go source, SQL migration, test, configuration, or runtime behavior was changed to produce this document. No backup or restore command was implemented. No external anchor, key, service, or dependency was added. The audit-chain schema and hash algorithm were not changed. The five Step 5.3 decisions (below) were not reopened. Step 5.5 was not started. Nothing was committed, tagged, or pushed.

---

## 1. Status and scope

This is Step 5.4 of the `dso central` Phase 2 engagement: a design and repository-reconnaissance task only, producing a backup/restore design for review and approval. It builds on, and treats as binding, the five decisions approved in Step 5.3 (`docs/reviews/2026-10-25-central-store-phase2-audit-chain-decision-resolution.md` §8):

1. `CreateProject`/`CreateEnvironment`/`DeleteSecret` run inside `WithTx` so the business mutation and audit event commit or roll back together (**verified fact**, re-confirmed this task — see §2).
2. Audit-write failure is reported log-only, synchronously, at error level; no persistent health-status or sentinel file exists.
3. Audit history is append-only and unbounded; retention is explicitly deferred, with "the Step 5.4 backup/restore design, when it is proposed" named as one of its own revisit triggers (`...decision-resolution.md` §5) — this document is that trigger, and §12 below revisits it as instructed, without re-deciding Gate 5 itself.
4. The external rollback anchor and restore-event semantics (Gates 1 and 7) were explicitly deferred to this step. This document's job is to propose, not silently assume, how to resolve them.
5. The audit chain is a plain SHA-256 hash chain; no MAC/signing key. Not reopened here.

Also binding, carried forward unmodified: Option B (local Unix-domain socket + peer credentials) is the *recommended, not yet implemented* Step 5.2 architecture; the same-UID limitation is accepted and undefended; no network listener; no hosted control plane; DSO remains self-hosted, single-operator, Docker-Compose-native per the complete-product audit's product identity.

## 2. Verified current-state architecture

All of the following were directly read from the current repository this task (file/line references below), not re-derived from prior reports or assumed from documentation alone.

**Repository state**: branch `feature/webui-phase1`, HEAD `ccfc0b55b7ce8630d395e9dd1e29d301d6010e61`, working tree unchanged from every prior step in this engagement — the same pre-existing `.claude-flow/*` modifications, the same two uncommitted Phase 1 fixes (`pkg/config/config.go`, `.github/workflows/coverage.yml`), and the same Step 5.3 implementation files (`internal/store/sqlite/audit.go`, `audit_test.go`, `migrations/0003_audit_events.sql`, `internal/centralsvc/audit_test.go`, `internal/cli/central_audit_logger_test.go`), plus the docs listed in the task. Verified via `git branch --show-current && git rev-parse HEAD && git status --short`. No command failed; no claim below rests on an unexecuted check.

**Database location, open sequence, and file safety** (`internal/store/sqlite/sqlite.go:84-164`): `Open` creates the file exclusively at `0600` inside a `0700`-checked directory (`securefs.EnsureDir`/`securefs.CreateExclusive`/`securefs.OpenPrivateFile`), then opens it via `modernc.org/sqlite` with `_pragma=busy_timeout(5000)`, `foreign_keys(1)`, `journal_mode(WAL)`, `synchronous(FULL)`, and `_txlock=immediate`. `Open` verifies `PRAGMA journal_mode` actually returned `wal` before proceeding (lines 124-128) — **verified fact**: WAL is not merely requested, it is confirmed. `Close` (`sqlite.go:167-170`) runs `PRAGMA wal_checkpoint(TRUNCATE)` best-effort before closing — this folds the WAL back into the main file but is not guaranteed to succeed (its error is discarded) and is **not** invoked by a crash, only by an orderly `Close`.

**Migration-time backup mechanism** (`sqlite.go:172-321`), the only backup capability that exists today: on every migration of a non-empty database, `backupBeforeMigration` creates a **new**, exclusively-created file named `<db>.pre-migration-v<from>-<UTC-time>-<random-hex>.bak` at mode `0600`, populates it with `VACUUM INTO ?` (SQLite's own consistent-snapshot export — it reads the live database through a read transaction and writes a complete, compacted, WAL-free copy, so the result needs no companion `-wal`/`-shm` file), then **verifies** the snapshot by opening it read-only (`_pragma=query_only(1)`) and checking `PRAGMA integrity_check = 'ok'` and that `schema_migrations`'s max version matches the expected pre-migration version (`verifyBackup`, lines 298-321), then `fsync`s the file and its containing directory (lines 282-294). If creation or verification fails, **the migration is not attempted** (`migrate`, lines 197-201, propagating the error). Backups are **never deleted automatically** (`sqlite.go` doc comment, lines 240-246; `docs/central-store-m1.md:184-186`). **No standalone, on-demand backup command exists** — this mechanism only fires as a side effect of opening a database whose schema is behind the binary's, never on operator request.

**Transaction/concurrency model**: `Store.WithTx` (`internal/store/sqlite/tx.go:69-85`) begins one `IMMEDIATE` transaction per call, commits on a nil return, rolls back on error/cancelled-context/panic, and refuses nesting with `store.ErrTxActive` via a context-value guard (`guard`, lines 58-65) — **verified fact**, not inferred: `guard` is called at the top of every `Store` method, including the audit-chain methods added in Step 5.3. The standalone `Store.AppendAuditEvent` wrapper (lines 189-200) itself wraps its own read-latest-hash-then-insert sequence in `WithTx`, specifically to prevent two concurrent standalone appends from forking the chain (the comment at lines 180-188 documents this as a fix made during Step 5.3's implementation, not as a residual gap).

**Audit-chain implementation** (`internal/store/sqlite/audit.go`, `migrations/0003_audit_events.sql` — contents not re-quoted here since both were read verbatim this task and were already summarized accurately in `docs/central-store-audit-log.md`, itself re-read this task): `event_hash = SHA-256(prev_hash || canonical(event fields))` (`audit.go:67-72`), where `canonicalizeAuditEvent` (`audit.go:32-46`) length-prefixes each field so no concatenation ambiguity exists. `VerifyAuditChain` (`audit.go:182-213`) recomputes every hash from the stored `prev_hash` chain and rejects any event whose `SchemaVersion` is not the one binary-supported value (`supportedAuditEventSchemaVersion = 1`, line 24). Its own doc comment (lines 171-181) states plainly, and this document re-verifies rather than merely repeats: **this function can only verify the events that are present. It cannot detect that the chain's tail was deleted, or that the whole database file was replaced by an older, internally-valid backup — both produce a chain that passes this exact check.** This is the single most load-bearing fact for this document's §7/§8.

**Key hierarchy and recoverability** (`docs/central-store-m1.md:106-145`, cross-checked against `internal/centralsvc/service.go:271-285`'s `unwrapDEK` and `CreateProject`'s DEK-wrap call at `service.go:151-165`): the **root key** lives entirely outside the database, supplied at process start by a `crypto.KeyProvider` (`file` or `passphrase` provider) — it is **never stored in the database in any form**. What the database *does* store is: a **binding canary** (`store_meta`: `binding.version`/`binding.root_key_id`/`binding.canary`, wrapped by the root key) that lets `Service.Bind` confirm a presented root key is the *right* one before any operation proceeds; and, per project, a **wrapped DEK** (`projects.wrapped_dek`/`dek_key_id`/`dek_version`) that `unwrapDEK` rejects outright with `ErrWrongRootKey` if the project's `DEKKeyID` doesn't match the configured provider's own `ID()` (`service.go:273-275`) — **verified fact, not assumed**: a restored database is only as useful as the root key presented alongside it; the database alone, however internally consistent, cannot decrypt a single secret value without that external key material. Secret values themselves are AES-256-GCM ciphertext (`secret_versions.ciphertext`/`nonce`), bound by AAD to project/environment/secret/version identity (`crypto.SecretAAD`, called at `service.go:330,399,485`) — so ciphertext moved between rows, even within the same database, fails to decrypt; this is already a defended property, not something backup/restore introduces or must newly guarantee, only must not break.

**No existing backup, export, import, restore, or database-recovery *command*** exists for `dso central` beyond the migration-time mechanism above — confirmed by its absence from `internal/cli/central.go`'s command tree (Step 5.2's own verified command inventory, `...authn-authz-design.md` §2, re-confirmed unchanged by this task's `git status` showing no new modification to that file beyond the Step 5.3 logger work already summarized). "Restore" today is a **manual, undocumented-as-a-command, but documented-as-a-procedure** operation: `docs/central-store-m1.md:183-184` states it as "stop DSO, remove `<db>`, `<db>-wal`, `<db>-shm`, copy the chosen `.bak` to `<db>` with mode `0600`" — this is an operator-performed filesystem operation today, not a `dso central restore` subcommand.

**Operational/filesystem assumptions already established and binding on any backup/restore design**: the database must be on a local filesystem — WAL mode needs working POSIX locks and shared memory, and is **not supported on NFS/SMB** (`docs/central-store-m1.md:40-46`); `securefs`'s directory-chain and symlink checks (`docs/central-store-m1.md:66-89`) already apply to the database file and must apply identically to any backup file, per the Step 5.1 threat model's explicit requirement (`...threat-model.md` §7: "a backup file is exactly as sensitive as the live database and must get the *same* fail-closed permission/directory treatment, not a lesser one").

## 3. Threat model and explicit non-goals

This section does not reopen Step 5.1's threat model; it applies it to backup/restore specifically, per that document's own flag (§7, §8) that this was left for Step 5.4.

**In scope to defend**: theft of a backup file alone (already defended identically to the live database, by construction); an incomplete or corrupted backup being silently trusted (§4); a restore applied without adequate validation, corrupting or compromising the active store (§5); accidental operator error given a chance to be caught before it is irreversible (§5/§12).

**Explicitly out of scope / not solved by this design, stated rather than silently assumed**, carried forward unchanged from Step 5.1/5.3 and M1:

- **Whole-database rollback via restoring an older, internally-valid backup is not detected by this design, by default** (§7 analyzes this precisely).
- **Theft of the database (or a backup of it) together with the root key/passphrase** remains unprotected (`docs/central-store-m1.md:60`).
- **Compromise of the OS account `dso central` runs as, or `root`,** remains unprotected (Step 5.1 §8).
- **No network dependency, external service, or second trust store is proposed.** Where §7 discusses an "external anchor," it means a file or value stored *outside the database*, on the same host — not a remote service. Not recommended for this milestone; only analyzed, as the task requires.
- **Same-UID authorization is unchanged**: backup/restore is proposed as **caller-#1-only, intended-policy, unenforced-under-Option-B-alone** — consistent with Step 5.2 §7's identical treatment of other destructive operations.

## 4. Backup format and consistency strategy

**What constitutes a valid backup**: a single self-contained SQLite file, produced by `VACUUM INTO`, that (a) passes `PRAGMA integrity_check`, (b) has no pending WAL/journal content, and (c) whose `schema_migrations` table reports the exact schema version the live database had at backup time — exactly the checks `backupBeforeMigration`/`verifyBackup` already perform (§2). **Proposal**: reuse that exact mechanism for on-demand backup. Compared against a raw file copy (rejected — reintroduces the WAL-consistency hazard M1 already warns against, `docs/central-store-m1.md:44-46`) and SQLite's Online Backup API (rejected — no advantage at DSO's single-operator scale, and `VACUUM INTO` also compacts the output).

**What must be included**: the full database file — projects, environments, secrets, secret_versions, `store_meta` (binding canary and every `wrapped_dek`), `schema_migrations`, and `audit_events` with its hash chain intact in full. **Proposal**: no table excluded by default; dropping `audit_events` would let an operator restore a "clean" history, worsening §7's gap rather than mitigating it.

**Secret encryption and recoverability of the backup alone**: secret values are decryptable only via the project DEK, itself only decryptable by the root key living outside the database (§2). **Limitation**: a backup alone is as useless to an attacker, and to a legitimate restore without the matching root key, as the live database is. **Proposal**: both backup and restore must explicitly tell the operator the root key/passphrase is not included and must be preserved separately (mirrors `docs/central-store-m1.md:144-145`).

**Backup file protection**: **Proposal**: created via `securefs.CreateExclusive` at `0600` in a directory passing the same chain-safety checks as the database directory, exactly as `backupBeforeMigration` already does — not a lesser-protected path, including for an operator-specified destination.

**Interrupted or incomplete backup**: **Proposal**: write to a new, exclusively-created file; run `verifyBackup`'s checks before reporting success; on failure, remove the partial file (as the existing `fail` closure already does) and report failure — never leave an unverified file at the final path.

## 5. Restore state machine and failure handling

**Proposal — preconditions before touching the active store**:
1. The candidate file must pass `verifyBackup`'s checks before anything about the active store is touched.
2. The candidate's schema version must be no newer than the running binary's supported schema (the same `ErrSchemaTooNew` check `Open`'s `migrate` already performs, §2).
3. `VerifyAuditChain` over the candidate's own `audit_events` must pass. **Limitation**: this proves the candidate's own internal consistency, not that it is the newest legitimate state (§7) — restore must not claim otherwise.
4. The configured root key/passphrase must successfully `Bind` against the candidate's own binding canary before proceeding — a wrong-key candidate fails closed with the `ErrWrongRootKey`-class error, checked in place before the active database is touched.

**Proposal — safe replacement**, following `backupBeforeMigration`'s own durability discipline:
1. Validate the candidate fully, without touching the active path.
2. Take a **safety snapshot of the currently-active database** (same `VACUUM INTO` + `verifyBackup` mechanism) before replacing it — the strongest single recommendation in this document for making restore recoverable from its own failure.
3. Checkpoint and close the active database (`Store.Close`'s existing `wal_checkpoint(TRUNCATE)` + `Close`).
4. Replace the active database file, removing stale `-wal`/`-shm` companions, mirroring the manual procedure already documented (`docs/central-store-m1.md:183-184`).
5. Re-open the restored file through the normal `Open` path — re-confirming WAL mode, re-running the migration-version check, and re-confirming integrity via normal open-time validation.

**Proposal — recovery from a failed restore**: the step-2 safety snapshot is the recovery path if replacement or re-open fails. **Limitation**: no automatic, unattended retry is proposed; a failed restore stops, reports the snapshot path, and requires an explicit operator decision (mirroring `init`'s own report-don't-auto-recover philosophy, `docs/central-store-m1.md:212-220`).

**Proposal — confirmation**: require explicit confirmation (interactive prompt or an explicit non-interactive flag) before step 3 begins, consistent with Step 5.2 §7's treatment of `disable-version`/`delete`. Exact flag/prompt wording not proposed here.

## 6. Key and secret recoverability requirements

1. Backup/restore **must not** embed the root key or passphrase in the backup file — would defeat the key-separation model (`docs/central-store-m1.md:100-105`).
2. A restored database is only decryptable by the root key bound to it at backup time (`DEKKeyID` must match the provider's `ID()`, `service.go:273-275`). **Proposal**: restore's validation must check and report this explicitly, not let the operator discover it via a failed `get --reveal`.
3. **Documentation requirement**: state plainly, in both commands' output, that the root key is the operator's own responsibility to back up separately (mirrors `docs/central-store-m1.md:144-145`) — no new escrow mechanism invented.
4. **Decryptability is not integrity-verifiability**: `Bind` succeeding doesn't guarantee every secret version's AAD/GCM check will pass (SQLite's integrity check is structural, not cryptographic). **Open decision** (§13): spot-check decryption post-restore, or rely on `integrity_check` + `Bind` alone — not decided here.

## 7. Audit-chain verification and rollback-detection options

Restated precisely (verified against `audit.go`'s own doc comment, §2): restoring an older, internally-valid backup produces a chain that **passes `VerifyAuditChain` cleanly** — every hash and link still matches, because the chain's notion of "the present" is simply wherever its own tail sits; it cannot know a longer tail once existed in the file that was overwritten.

**Option comparison**:

| Option | State recorded, where | Updated when | Crash between DB commit and anchor update | Interaction with legitimate restore | Missing/stale/inaccessible anchor | Attacker with write to DB *and* anchor | Operational complexity |
|---|---|---|---|---|---|---|---|
| **A. No external anchor (status quo)** | None | N/A | N/A | Rollback undetectable, as today | N/A | N/A | None |
| **B. Operator-managed external checkpoint** | Chain tip, wherever the operator chooses | Whenever operator checks | N/A | Operator manually compares restore's reported tip (§8) to their own record | No checkpoint — same as A | Outside DSO's control | None for DSO; all for the operator |
| **C. Local anchor file, outside the DB, same host** | Latest `event_hash`, in a dedicated file with `securefs` discipline | After each audit append | Anchor stale (behind) if the DB commits first and the anchor write fails/crashes — a safe, under-claiming direction | Restore must update the anchor to the restored tip; if it doesn't, "DB tip behind anchor" is the one unambiguous rollback signal this option provides | Missing: report "unavailable," never "detected"; stale-ahead: informational only | Defeats it — raises the bar modestly, doesn't close the same-UID gap (§3) | Low — one file, existing conventions |
| **D. Separately protected remote/external anchor** | Latest `event_hash`, off-host | Same as C, over a network | Network failure is a new failure mode: block (regresses Gate 6's availability-first policy) or best-effort (no stronger than C when it fails) | Same tension as C, over a network | Same as C, plus "unreachable" | Genuinely resistant to a single-host attacker | High — new network dependency, against self-hosted identity (Step 5.1 §9) |

**Recommendation**: adopt Option C for a **future** milestone, not this one. It needs no network dependency and no reliance purely on operator diligence, but its exact write cadence (one more synchronous local I/O per audited operation) interacts with Gate 6's availability-first precedent and should not be decided unilaterally here. Shipping §4/§5's core mechanics first, honestly limited, mirrors how Step 5.3 itself shipped. **Open decision** (§13): ship now without any anchor, or include Option C in this milestone.

## 8. Restore-event semantics

**Proposal**: a successful restore appends a `restore` audit event to the restored database, as the first new event after its own tail, via the existing `AppendAuditEvent` mechanism with a new `AuditEventType` (e.g., `AuditRestore`) added to `store.go`'s closed enum (a Go-level addition, not a migration — **Proposal**, not performed here). This is evidence, visible going forward, that a restore happened — explicitly **not** proof of what preceded it in the overwritten file, consistent with `...decision-resolution.md` §6.

Fields should record: the restored backup's own last pre-restore `event_hash`/`seq`, a note that this was explicit and operator-initiated, and the usual caller-supplied (unauthenticated) actor label.

A failed restore that never reaches a usable database has nothing to write an event into — mirroring `init`'s own partial-failure-coverage limitation (`docs/central-store-audit-log.md`) — and should not be forced into the *old* database's chain either, since that chain shouldn't record a restore that never replaced it, absent Option C. This is why Gates 1 and 7 are interdependent, as Step 5.3 already concluded; restore events are recorded only on the success path, failures reported via normal CLI error output.

No claim is made that the chain can atomically span the old database, the new database, and any external anchor. Actual boundaries: (1) the restored file's internal state is whatever it was at backup time; (2) the restore event is appended in its own transaction, strictly after the file becomes active; (3) an Option C anchor update, if adopted, is a third, separate write — a crash between (2) and (3) leaves the chain correct but the anchor stale, the safe direction (§7).

**Distinguishing a verified restore from one knowingly accepting an older snapshot**: §5's checks cannot tell "newest" from "an older one deliberately chosen." **Proposal**: the confirmation prompt should show the backup's recorded timestamp and last-event summary, a usability mitigation, not a cryptographic one.

## 9. Crash-consistency and failure matrix

| Failure point | Consequence | Recovery |
|---|---|---|
| Crash during `VACUUM INTO` | Partial/empty destination file | Remove on next invocation, as the existing `fail` closure already does |
| Crash after `VACUUM INTO`, before `verifyBackup` | Complete but unverified file | Not yet trusted; re-run after removing the stale candidate |
| Crash after verification, before success is reported | Valid backup exists but unreported | No integrity risk; re-running produces a second, equally valid file |
| Crash during restore's safety-snapshot step | Same as backup's own crash cases | Active database untouched; retry restore from the top |
| Crash during restore's replace step, after snapshot succeeded | Active database possibly missing/partial | Restore the safety snapshot — why this step is mandatory |
| Crash during restore's re-open step | Restored file on disk, unconfirmed | `Open`'s existing logic runs again on next start; same recovery if replacement was itself interrupted |
| Crash between restore event append and anchor update (if Option C adopted) | Chain correct; anchor stale | §7's documented safe direction |
| Operator restores the wrong file (human error) | Valid restore of unintended data | Mitigated by §5 confirmation, §8 summary display, and the mandatory safety snapshot (itself recoverable the same way) |

## 10. Proposed CLI/API changes (proposals only, not implemented)

Named only so Step 5.2/5.3's placeholder matrix entries have something to slot into:

- `dso central backup --out <path>` — on-demand snapshot via §4, independent of pending migrations; distinct from, not a replacement for, the automatic pre-migration backup.
- `dso central restore --from <path>` — the §5 sequence, appending the §8 restore event on success.
- A dry-run/verify mode — runs §5's validation without replacing the active database.
- Per Step 5.2 §7: both proposed as **caller-#1-only, policy-intention-only under Option B alone**, the same caveat already applied to `get --reveal`/`disable-version`/`delete`.

## 11. Test strategy, including failure injection and recovery tests

Proposed only, following this repo's table-driven, `-race`-clean conventions:

- Backup of a database with every table type (including a non-trivial audit chain) passes `verifyBackup`.
- Backup of an empty, freshly-initialized database succeeds and opens cleanly.
- Backup to an unsafe directory is rejected by existing `securefs` checks.
- Failure injection at each row of §9 produces the documented recovery, not silent data loss.
- Restore with a mismatched root key fails closed before the active database is touched (asserting it is byte-for-byte unchanged).
- Restore of a newer-schema backup fails closed before replacement.
- Restore of a chain-tampered backup is rejected at validation.
- A companion to `TestAuditChain_TailDeletionNotDetected` that proves, as an expected-pass case, that restoring an older valid backup over newer data succeeds and verifies cleanly — actively proving §7's limitation, not just asserting it.
- A full round-trip: create data/events, back up, create more, restore the earlier backup, assert restored state (not pre-restore state), and that the restore event links correctly as the new tail.
- A concurrency test analogous to `TestAppendAuditEvent_ConcurrentStandaloneAppendsStayLinked`: backup taken during an in-flight write transaction is consistent at a single point in time.

## 12. Operational limitations and recovery runbook outline

**Limitations** (content requirement only):
- Rollback via restore is undetected unless/until Option C is adopted, and even then only against a single-host attacker.
- Root key/passphrase is never in any backup; must be preserved separately.
- Restore replaces the entire active database; only the mandatory safety snapshot protects against restoring the wrong file.
- Backups accumulate without pruning, unchanged from M1. Step 5.3's Gate 5 Trigger C ("the Step 5.4 design, when proposed") is **noted as triggered by this document's existence, not resolved here** — backup retention is an open decision (§13).
- Local-filesystem-only, same NFS/SMB restriction as the live database.

**Runbook outline**:
1. Routine backup on an operator-chosen schedule; verify success; store with the same care as the live database; keep the root key backed up separately.
2. Before restore: dry-run verify the candidate; review its timestamp/last-event summary.
3. Restore; respond to confirmation; note the safety-snapshot path reported.
4. After restore: confirm a representative `secret get --reveal` succeeds; review the new restore event.
5. To undo: restore the safety snapshot from step 3, identically.

## 13. Open decisions requiring explicit user approval

1. Include an external rollback-detection anchor (Option C) in this milestone, or defer it — recommended: defer, not decided here.
2. Spot-check decryption of secret values post-restore, or rely on `integrity_check` + `Bind` alone — not decided here.
3. Exact command/flag names and confirmation UX — placeholders only.
4. Backup-file retention/pruning policy, distinct from the audit table's own retention decision.
5. Whether backup/restore gets the same "policy-intention-only under Option B" caller-#1 treatment as other destructive operations — recommended yes, requires the same approval Step 5.2 §12 item 5 already required.
6. Whether this document's existence counts as Step 5.3 Gate 5's Trigger C being met — surfaced, not resolved here.

## 14. Recommendation, with rationale and rejected alternatives

**Recommendation**: build backup/restore now using the existing `VACUUM INTO` + `verifyBackup` mechanism (§4), the validate/safety-snapshot/replace/reopen restore state machine (§5), and forward-only restore-event semantics (§8) — **without** an external rollback-detection anchor in this milestone, deferring Option C (§7). Document the rollback-detection gap with the same honesty Step 5.3 already modeled.

**Rationale**: delivers the one thing M1 flagged as entirely missing (`docs/central-store-m1.md:186,233`) by reusing mechanisms this repository has already built and tested, rather than inventing a parallel one. Resolves Gates 1/7's interdependency as Step 5.3 predicted, without resolving their substance beyond what this milestone can honestly support.

**Rejected alternatives**:
- **Option C in this same milestone**: not rejected outright (it's the explicit next step, §13 item 1) — deferred because its write-cadence/availability tradeoff needs its own decision.
- **Raw file-copy backup**: rejected — reintroduces the WAL-consistency hazard M1 already warns against.
- **Network-based anchor (Option D)**: rejected for the same reasons Step 5.1 §9 rejected a network-accessible service — higher cost for the same benefit Option C achieves locally.
- **Mandatory full-database decryption on every restore**: deferred (§13 item 2), not rejected — would make restore a bulk-plaintext operation beyond anything else in this codebase's design, for a guarantee already provided incrementally by existing checks.

## 15. Explicit confirmation

No Go code, SQL migration, test, configuration default, or dependency was modified to produce this document. No backup or restore command was implemented. No external anchor, key, service, or network dependency was added. The audit-chain schema and hash algorithm were not changed. None of the five Step 5.3 decisions was reopened. Step 5.5 was not started. Nothing was committed, tagged, stashed, reset, cleaned, or pushed. All pre-existing untracked and modified files remain exactly as they were at the start of this task.
