# Central store: permanent purge (design decision record)

Status: **proposed**. DESIGN ONLY. **Not authorized to implement.** ADR-011 explicitly excludes permanent purge of
secret versions. This document changes no code, schema, trigger or behaviour.

Evidence read: `internal/store/sqlite/migrations/0001_init.sql`, `0003_audit_events.sql`,
`internal/store/sqlite/{sqlite,backup,displace}.go`, `internal/centralsvc/restore*.go`,
`internal/crypto/{envelope,keyprovider}.go`, `docs/design/central-server-design.md` section 4
("Versioning, rollback, deletion"; "Backup, restore, upgrade").

## 1. Current state
- `secret_versions` rows are immutable: trigger `secret_versions_immutable` (BEFORE UPDATE) allows only the
  one-way `disabled_at/disabled_by` transition, and `secret_versions_no_delete` (BEFORE DELETE) aborts every
  delete. Migration 0001 comments that purge is "a later, explicit, audited operation with its own migration".
- Secrets are soft-deleted (`secrets.deleted_at`); versions remain. FKs are `ON DELETE RESTRICT`.
- Project DEK: random, stored in `projects.wrapped_dek`, wrapped by the root key via a `KeyProvider`
  (file or passphrase; root key never in the DB). DEK is not rotated or destroyable by any current command.
- `audit_events` is append-only (update/delete triggers), unkeyed hash chain (ratified R-1, R-3).
- SQLite runs in WAL mode (`sqlite.go`). Backups use `VACUUM INTO`; restore moves the replaced database aside
  (`displace.go`, "nothing is deleted") and takes a mandatory safety snapshot of the active store.

The server-design intent (section 4) is soft delete, retention (default 30 days), then purge, and
"cryptographic erasure on project deletion = destroy the project DEK". This note tests that intent against the
implementation.

## 2. Two distinct operations
- **Version purge**: remove selected ciphertext rows (for example old or disabled versions) of a live secret.
- **Secret purge**: remove a soft-deleted secret and all its versions and its `secrets` row.
Both must keep audit history. A third, project purge, is out of this proposal (project delete exists and does
not cascade; project purge would add DEK destruction, see section 6).
Proposed rule: purge applies only to soft-deleted secrets (secret purge) or to disabled, non-latest versions
(version purge); the latest version of a live secret is never purgeable. Version numbers are never reused, so
`secret_versions` gaps are expected and the `(secret_id, version)` history of audit rows stays meaningful.

## 3. Soft-delete retention
Soft delete today has no retention clock and nothing consumes `deleted_at` for expiry. Proposal: purge is an
explicit operator command only; no automatic retention job (a background deleter would be a new subsystem and a
silent data-loss path). A minimum age since `deleted_at` may be a flag, default none. Open question Q2.

## 4. Audit preservation
Purge must not touch `audit_events`. Audit rows carry ids and names (never values); they remain after purge, so
the chain stays verifiable (`audit verify`) and ratified decisions R-1..R-7 (unkeyed chain, no anchor) are
untouched. Purge appends its own event (`secret.purge` / `secret.version.purge`: resource ids, names, version
numbers, counts, actor, never values) inside the same `Store.WithTx` as the delete. Consequence to state to
operators: the audit chain proves a purge happened, but because it is unkeyed and not tamper-proof it cannot
prove that nothing else was removed.

## 5. What SQL DELETE does and does not do (SQLite)
A `DELETE` is logical removal. It is **not secure erasure**:
- In WAL mode the deleted row's old page images remain in the `-wal` file until a checkpoint, and after a
  checkpoint the bytes remain in the main file's freed pages (free list) until they are overwritten by later
  inserts or the file is rebuilt. Page images from earlier transactions can remain in the WAL until it is
  reset/truncated.
- `PRAGMA secure_delete=ON` zeroes deleted content in the main database pages (and, depending on version and
  mode, FAST zeroes only when cheap). Limits: it does not reach already-checkpointed older copies, WAL frames
  written earlier, backups, the displaced/safety copies, filesystem journals, SSD wear-levelling remapping, or
  copy-on-write/snapshot filesystems. It adds write cost.
- `VACUUM` rewrites the database file without free pages and `wal_checkpoint(TRUNCATE)` (already used in
  `backup.go`) empties the WAL. Limits: the previous file's blocks are released to the filesystem, not wiped;
  they may be recoverable from the device until reused. `VACUUM` needs temporary space and an exclusive
  window.
- These are partial mitigations that reduce residue in the live file. None is a guarantee of unrecoverability.
  Documentation and CLI text must never say a purged value is "securely erased".

## 6. Backups, displaced copies, and key lineage: can cryptographic erasure work?
Every ciphertext copy that exists is decryptable by the project DEK, which is stored wrapped by the root key.
Cryptographic erasure of a project's data is achievable only if **all** of the following are destroyed:
1. every copy of `projects.wrapped_dek` for that project, in the live DB, WAL, free pages, every `VACUUM INTO`
   backup, every restore safety snapshot and every displaced `<db>.replaced-<UTC>-<hex>` directory
   (restore never deletes these by design), plus any off-host backup copies; AND
2. every copy of the root key that can unwrap it (key file, passphrase-derived KEK material, key backups) and
   every earlier root key in the lineage. A root key shared by all projects cannot be destroyed to erase one
   project without destroying access to every other project.
Backups and displaced copies defeat this: any retained backup plus the root key restores both DEK and data.
A per-project erase therefore needs either per-project wrapping keys (not the current design; all projects
share one root key) or accepting that erasure is bounded by the oldest retained backup. There is no root-key or
DEK rotation command today. Conclusion: with the current architecture, cryptographic erasure is not achievable
as a guarantee; it is achievable only for a single-project deployment after destroying all backups and key
copies, and it is not detectable or provable by DSO. A single secret's versions cannot be cryptographically
erased at all (one DEK per project, not per secret).

## 7. Required migrations (NOT authorized)
Version/secret purge requires a new migration (0004+) that changes `secret_versions_no_delete` (for example,
permits DELETE only when the parent secret is soft-deleted or the version is disabled and not latest, ideally
gated by a session-scoped marker) and re-states `secret_versions_immutable` if needed. This **must not be done
under the current authorization**: ADR-011 excludes purge, the trigger is the store's immutability guarantee
and is covered by tests, and a weakened trigger is a permanent schema change that cannot be rolled back without
another migration. Forward-only migrations apply with a pre-migration backup; a downgrade to a binary that
expects the old trigger would see a schema-version mismatch and refuse to run.

## 8. Operator confirmation and UX (proposed)
- `dso central secret purge <project> <env> <name>` and `... purge-version`, never default, never implied by
  `delete`.
- Dry-run first: show secret name, version count, and the list of residue the command cannot remove (backups,
  displaced copies, WAL/free pages).
- Typed confirmation of the secret name (not `y`), plus `--yes` only for non-interactive use; refuse when the
  central database lock is held elsewhere.
- Output states: "logically deleted; not securely erased; N backups and M displaced copies may still contain
  the value".

## 9. Failure recovery and compatibility
- One `WithTx` transaction: delete rows plus audit append commit or roll back together. A crash leaves either
  the full pre-state or the full post-state; there is no partially purged secret.
- Optional post-step (`wal_checkpoint(TRUNCATE)`, optionally `VACUUM`) is best effort; failure is reported but
  does not undo the purge.
- Restore of a pre-purge backup resurrects the purged secret; the audit chain in that backup does not contain
  the purge event. Operators must be told that restoring an older backup reintroduces purged data and that the
  restore itself is audited as a forward-only event.
- Compatibility: older binaries refuse a newer schema version; backups taken after migration 0004 cannot be
  restored by a binary that predates it. `--json` output and exit codes for existing commands are unchanged.

## 10. Open owner questions
- Q1. Is purge wanted at all, given that backups and displaced copies cannot be purged and the value is
  logical-only removal? Alternative: document "soft delete + disable only" as the supported model.
- Q2. Retention: explicit-only purge, or a minimum age since `deleted_at`? Any automatic job is out of scope
  unless separately authorized.
- Q3. Is `secure_delete`/VACUUM-after-purge acceptable cost, given it is only a partial mitigation?
- Q4. Should backup and displaced-copy pruning be a separate (also unauthorized) operator command?
- Q5. Is per-project root keys / DEK rotation / DEK destruction wanted? It is a prerequisite for any honest
  cryptographic-erasure claim and is a key-management feature outside ADR-011.
- Q6. Does a purge event name the secret (audit usefulness) or only ids (less residual metadata)?
