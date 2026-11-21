
# Central store backup and restore (Phase 2 Step 5.4)

Status: **implemented in this milestone, as approved in
`docs/reviews/2026-10-29-central-store-phase2-backup-restore-decision-resolution.md`**.
Builds on `docs/central-store-m1.md` (key hierarchy, migration-time backup)
and `docs/central-store-audit-log.md` (audit chain) -- read those for the
full rationale; this page states only what is actually implemented and what
it does and does not guarantee.

## Commands

```
dso central backup --out <path>
dso central restore --from <path> [--dry-run] [--force]
```

`backup` writes a complete, verified, self-contained snapshot of the active
database to `--out` using SQLite's own `VACUUM INTO`, the same mechanism and
verification (`PRAGMA integrity_check` plus a schema-version check) the
existing automatic pre-migration backup already relies on. It never
overwrites an existing file at `--out`, creates the destination at mode
`0600` in a directory checked with the same `securefs` discipline as the
database and key files, and removes any partial output on failure.

`restore` validates a candidate backup (path safety, SQLite integrity,
schema compatibility, full audit-chain self-consistency, and root-key
binding) and, unless `--dry-run` is given, replaces the active database with
it after creating and verifying a mandatory safety snapshot of the active
database and obtaining explicit confirmation (an interactive `[y/N]` prompt,
or `--force` to skip it for non-interactive use -- the same convention `dso
apply --force` already uses). `--dry-run` performs every validation check
and reports the result without touching the active database, creating a
safety snapshot, or recording a restore event.

## What a backup contains, and what it never contains

A backup contains every table, including the complete audit-event hash
chain -- no table is excluded. It **never contains the root key or
passphrase**: those live entirely outside the database (see
`docs/central-store-m1.md`, "Key hierarchy, binding and lifecycle") and are
the operator's own responsibility to preserve separately. A backup without
the matching root key cannot be used to recover any secret value; both
`backup` and `restore` print a reminder of this.

Backups are **never pruned automatically**. Managing accumulated backup
files -- and their disk-usage impact -- is entirely the operator's
responsibility, exactly like the existing automatic pre-migration `.bak`
files.

## Restore validation: what it proves, and what it does not

A candidate that passes validation has been confirmed to be:
- a safe, regular, appropriately-permissioned file (the same
  `internal/securefs` discipline already applied to the database and key
  files);
- structurally sound SQLite (`PRAGMA integrity_check = "ok"`);
- at a schema version this binary supports (never newer);
- internally self-consistent across its entire audit-event hash chain
  (`VerifyAuditChain`);
- bound to the configured root key (the same canary-unwrap check
  `Service.Bind` performs for a live database, applied to the candidate
  file without modifying it).

**What this does NOT prove, stated plainly:**
- **It does not prove the candidate is the newest legitimate state.**
  Audit-chain verification checks only the events present in the candidate;
  it cannot detect that a database's tail was deleted, or that the whole
  file was replaced by an older, internally valid snapshot. Restoring an
  older backup over a database with more recent legitimate activity
  **succeeds and verifies cleanly** -- this is a known, disclosed limitation,
  not a security guarantee that rollback is detected (see
  `docs/reviews/2026-10-25-central-store-phase2-backup-restore-design.md` §7
  and the Step 5.4 decision resolution, decision 1).
- **It does not prove every secret version's ciphertext is decryptable.**
  `PRAGMA integrity_check` validates SQLite's own structural integrity, not
  AES-GCM ciphertext authenticity; a successful root-key binding proves the
  configured key matches the one the candidate is bound to, not that every
  stored ciphertext will pass its own AEAD check. Restore does not perform
  bulk decryption to verify this (Step 5.4 decision resolution, decision 2).
  Confirm a representative secret with `secret get --reveal` after a
  restore if this matters to you.

## Restore safety: the mandatory safety snapshot

Before replacing the active database, `restore` takes and independently
verifies a **safety snapshot of the currently active database**, using the
exact same mechanism as `backup`. This is the recovery path if any later
step fails: the active database is checkpointed -- and that checkpoint's
success is actually verified, not merely attempted -- then closed; any
`-wal`/`-shm` companion of the *previous* database is removed **before**
the file is replaced, which the verified checkpoint is what makes safe; the
file is then replaced (by copying the candidate's bytes into a new
temporary file and atomically renaming it over the active path -- the
candidate itself is never modified or deleted); and the restored file is
reopened through the normal database-open path (which re-validates
integrity and, if needed, migrates the restored schema forward exactly as
any other database open does).

`restore` never claims success before that reopen has actually succeeded.
Every error on the way reports the safety snapshot's path, if one was taken,
so the previous usable database can be recovered by restoring it the same
way.

## The restore audit event

A successful restore appends one `restore`-type audit event to the
*restored* chain, as the first new event after its own tail, using the
existing hash-chained append mechanism. This event is evidence, visible
going forward, that a restore occurred -- it is **not**, and cannot be, proof
of what the restore discarded: a record written after the restore completes
cannot retroactively describe the portion of the chain that was overwritten.
The event's own `prev_hash` links it to the restored chain's own tip; no
other field is needed to identify that tip.

## Authorization

`backup` and `restore` have **no caller-specific authorization today**.
Anyone able to run `dso central` as the owning OS user can run either
command, identically to every other `dso central` operation (`project
create`, `secret get --reveal`, etc.). The intended policy -- that these
should eventually be restricted to the same "caller #1" class as
`disable-version`/`delete` -- is recorded as a policy intention only (Step
5.4 decision resolution, decision 5); it is **not enforced**, because Step
5.2's local Unix-socket/peer-credential design (`docs/reviews/2026-10-24-central-store-phase2-authn-authz-design.md`)
remains unimplemented.

## Concurrency: restore must not run alongside another process using the same database

**This is a separate limitation from the authorization one above -- one does
not solve the other.** Nothing in `dso central` coordinates access across
processes: no socket, no lock file, no advisory lock beyond what SQLite
itself provides for ordinary reads/writes to one open file. `restore`
replaces the database file at its path with an atomic rename; it does not,
and cannot, know whether another `dso central` process (or, if ever wired
in, `dso agent`) still holds that file open. If one does, that other
process's own file handle keeps pointing at the now-orphaned pre-restore
file content; any write it makes after the rename is not reflected at the
active path and would be effectively lost from that path's perspective, and
its own next read/write may behave unpredictably once the file it has open
is no longer the one any new process would see. **Do not run `backup` or,
especially, `restore` while any other process (an interactive `dso central`
session, a script, a future `dso agent` integration) has the same database
open.** This is purely an operational/sequencing concern; it is unrelated
to, and not addressed by, the same-OS-user authorization limitation above
(which is about *who* may run the command, not about *what else* may be
running against the same file at the same time).

## What is explicitly not implemented in this milestone

- **No external rollback-detection anchor.** Deferred (Step 5.4 decision
  resolution, decision 1) -- the limitation above is accepted, not closed.
- **No automatic backup retention or pruning.** Operator-managed (decision 4).
- **No bulk secret-decryption verification during restore** (decision 2).
- **No Step 5.2 socket authentication.** Unchanged from every other `dso
  central` command.
