# Central store — Milestone 1 (persistent storage and key management)

Status: **experimental, local-only, not production-ready.** M1 provides a
durable, encrypted, versioned secret store and the key-management primitives
under it. It has **no network API, no authentication and no authorization
(RBAC)**; identity and authorization are planned for M2 and are not built.
Work after M1 added an append-only, hash-chained audit log, an on-demand
`backup` command, a `restore` command and a read-only `audit verify` command,
then cross-process locking, a read-only database inspection (`InspectDatabase`)
and an initialization check (`CheckInitialized`) that the ordinary commands and
`backup` apply before they open a database; they are described below. (Commit
hashes are not listed in this document because history may be rewritten before
merge; use `git log`.) Their existence does not make the store production-ready, and
unit tests do not establish production readiness (see "Known limitations" and
"Test coverage").

Specification: `docs/design/central-server-design.md` (§ Encryption and key
management, § Versioning, § Backup, Milestone 1; that file is tracked in this
branch). Decisions made after the independent review of 2026-10-16, and every
place M1 deviates from or corrects that document, are in
`docs/design/m1-design-addendum.md`.

## Packages

| Package | Role |
|---|---|
| `internal/securefs` | Verifies directory chains and key/database files (ownership, modes, symlinks) before they are trusted |
| `internal/crypto` | AES-256-GCM `Seal/Open`, AAD builders, `KeyProvider` interface, `file` and `passphrase` providers, exclusive key installation |
| `internal/store` | `Store` interface and metadata types. No SQL, no crypto |
| `internal/store/sqlite` | SQLite (WAL) implementation, embedded forward-only migrations, audit-event append and chain verification, on-demand backup, staged restore-candidate validation, the cross-process advisory lock, and `InspectDatabase`: a read-only inspection of a database file that never creates, migrates or writes the database file and never takes the central advisory lock, although SQLite may create or rewrite the `-wal`/`-shm` sidecars during its WAL-aware view (see "Database inspection and initialization") |
| `internal/centralsvc` | `Service`: composes Store + KeyProvider; binds the database to its root key; the only package that decrypts secrets (the CLI process also holds plaintext it reads with `readValue` and prints with `secret get --reveal`); also hosts the `Restore` state machine and `CheckInitialized`, the read-only classification of whether a file is an initialized database |
| `internal/cli/central.go` | `dso central …` local administration |

Driver: `modernc.org/sqlite v1.50.0` (pure Go, `CGO_ENABLED=0` compatible, as the
design recommends), a direct dependency. The version was chosen because newer
releases require a newer Go directive than this module declares.

## Platform support

**Supported and tested: Linux** (the tests in this repository run there).
Other unix-like systems (macOS, FreeBSD) **cross-compile** (`GOOS=darwin|freebsd
CGO_ENABLED=0 go build` succeeds for the new packages) but their runtime
behaviour has **not been tested**. On platforms without POSIX ownership and mode
semantics (Windows) the code also compiles, but `internal/securefs` refuses
every check, so the central store **fails closed and cannot be used**; this is
deliberate. The vault file lock from M0 is a no-op on non-unix platforms. The
database inspection (below) uses a stronger file signature on Linux than on other
platforms.

**Filesystem requirement:** the database must be on a **local filesystem**.
SQLite WAL mode needs shared memory and working POSIX locks between processes;
it is **not supported on NFS, SMB/CIFS or other network filesystems**, where it
can corrupt the database. Opening the database does not detect this — the
operator must place it on local storage. Likewise back up the database with
`dso central backup`, the pre-migration `.bak` mechanism or SQLite's online
backup, never by copying a live database file without its `-wal` companion.

## Threat model (what M1 does and does not protect)

Protects against: theft or leakage of the **database file alone** (backups,
snapshots, a read-only SQL bug, a stray copy) — it contains only ciphertext and
wrapped keys; row-level tampering or swapping of ciphertext between
projects/environments/secrets/versions (AAD binding, fails closed); a second,
different root key silently taking custody of a database (key binding, below);
silent corruption (GCM tag); other local accounts reading or **replacing** the
key, parameter, passphrase or database files (file *and* directory checks).

Does **not** protect against: compromise of the OS account that runs DSO (it
holds unwrapped keys in process memory while serving a request and can read the
key file); theft of database **and** root key together (file provider); `root`;
an attacker who can *replace* the database file with an older valid copy
(rollback of whole-database state is not detected); anyone who can run
`dso central` as the owning OS user. Secret **names, project/environment slugs,
timestamps and actor strings are metadata and are stored in clear.**

An unkeyed hash-chained audit log now exists, but it is not authenticated
evidence and does not defend against anyone who can write the database; see
"Audit log" below.

### File and directory safety (`internal/securefs`)

A 0400 key file is worthless in a directory another account can write to: they
unlink it and install their own. So before a key file, KDF-parameter file,
passphrase file or the database is used or created, DSO checks:

* **Every directory from `/` down** to the file's parent is owned by root or the
  current user and is not writable by group or others. A sticky world-writable
  directory (like `/tmp`) is accepted only as an *ancestor*, never as the
  immediate parent (others could pre-create `<db>-wal`/`-shm` or sibling files).
  Note: directories created with the common umask `002` are `0775` and are
  **refused**; the tool says which directory and the fix (`chmod go-w`). Missing
  directories are created `0700`; existing directories are never re-moded.
* **Symlinks** in the path are followed one at a time and the directory that
  *holds* each link is checked as well as the target chain, so a link planted in a
  writable directory is rejected. The final file is opened `O_NOFOLLOW` and judged
  on the open descriptor (`fstat`): regular file, owned by the current user, mode
  with no group/other bits (`0600`/`0400`).
* The same rules cover the pre-migration backup files (same directory, created
  exclusively `0600`) and `--passphrase-file`.

Limits, stated plainly: the checks run before use; the *owner of a directory* (or
root) can still race them — inherent to path-based APIs, and SQLite itself opens
the database by path. On non-unix platforms the checks fail closed (above).

### Key custody (a decision for the operator)

| | `file` | `passphrase` |
|---|---|---|
| Root key at rest | 256-bit random, `0400`, in a file | **never stored**; Argon2id-derived (t=3, 64 MiB, p=4, 16-byte salt; params + key-id check value in a `0400` file) |
| Unattended restart | yes | **no** — operator supplies the passphrase at each start |
| Defends | DB/backup leak when the key file is stored elsewhere | additionally, theft of the whole host disk *including* the key file |
| Weakness | key next to DB (or readable by the service account) = full compromise | passphrase strength; a `--passphrase-file` reduces it to file-provider strength |

Neither is HSM-grade. There is **no default provider**; `--key-provider` must be
chosen explicitly. `init` refuses to put the key in the **same directory** as the
database (a copy of that directory would contain both). **Different directories
are not different volumes**: DSO cannot and does not verify that the key is on
separate storage; that remains the operator's responsibility.

## Key hierarchy, binding and lifecycle

```
root key (KeyProvider; never in the DB)
  ├─ wraps → database canary   (binds the DB to this key; AAD = key id)
  └─ wraps → per-project DEK   (random 256-bit; AAD = project|key-id|dek-version)
        └─ encrypts → each secret VERSION (AES-256-GCM, random 96-bit nonce,
             AAD = project|environment|secret-id|name|version|dek-version)
```

* **Key binding.** `store_meta` holds `binding.version`, `binding.root_key_id`
  and `binding.canary` (a random value wrapped by the root key with the key id as
  AAD). Every `Service` operation first calls `Bind`, which asks the *configured
  provider* to unwrap the canary under *its own* id — it never trusts the stored
  key-id text (which is unauthenticated and used only to word the error), so
  editing it cannot make a wrong key pass or a right key fail. A wrong key gets
  `ErrWrongRootKey` before any read or write. First binding is one atomic
  insert-if-absent of all three rows, so concurrent first initialisers have exactly
  one winner; the losers then verify against the winner's binding and fail. A
  binding with missing or damaged parts is `ErrBindingCorrupt` and is **never**
  silently re-created. An *unbound* database (created before this existed, or whose
  rows were deleted) is bound only by a key that can unwrap **every** project key
  already in it. An attacker with write access can at worst replace the binding
  with one for their own key — which locks the legitimate key out (denial of
  service) but grants nothing.
* Create project: generate DEK → wrap → store `wrapped_dek`, `dek_key_id`,
  `dek_version`. The plaintext DEK is zeroed after use and never cached.
* Each write: fresh random nonce (`crypto/rand`; entropy failure = error).
  Random 96-bit nonces are safe far beyond any realistic version count (NIST bound
  2³² messages per key); there is no counter or rotation trigger yet.
* Read: unwrap, open with the row's own identity as AAD. Any authentication
  failure returns `ErrIntegrity` with no detail.
* Key files are installed with temp file (`0400`) + fsync + `link(2)` + dir fsync:
  never overwritten, and concurrent installers produce exactly one winner. A crash
  can leave a `.dso-key-*` temporary copy; `init` **reports** it and never deletes
  it (it may be the only copy of an uninstalled key).
* **Not in M1:** root-key rotation, DEK rotation, root-key backup/escrow tooling.
  `dek_version`/`dek_key_id`/the binding leave room for them (rotation will need a
  migration because `store_meta` is insert-only). **Losing the root key makes all
  data unrecoverable; back it up separately from the database.**

## Schema and migrations

Tables: `projects`, `environments`, `secrets`, `secret_versions`,
`schema_migrations` (migration `0001`), `store_meta` (migration `0002`),
`audit_events` (migration `0003`, see "Audit log"). Timestamps are UTC unix
seconds.

* Foreign keys `ON DELETE RESTRICT`; `secrets` references
  `(environment_id, project_id)` so an environment from another project is
  rejected by the database. `UNIQUE(project.slug)`, `UNIQUE(project_id, env slug)`,
  a partial unique index on live `(environment_id, name)` (a soft-deleted name can
  be reused), `PRIMARY KEY(secret_id, version)`, `CHECK`s on nonce length.
* **Immutability is enforced by the database**: triggers abort any `UPDATE` of a
  version row except the one-way `NULL → set` of `disabled_at/disabled_by`, abort
  every `DELETE`, and make `store_meta` insert-only. This is defence in depth; an
  attacker with raw write access can drop triggers but still cannot forge a valid
  ciphertext or canary without the keys.
* **"Latest" means the newest version.** If it is disabled, reading "latest"
  fails with `ErrLatestVersionDisabled` — there is **no fallback to an older
  version** (it may be the credential being replaced). Add a new version, or ask
  for an explicit version number. Disabled versions are retained but unreadable;
  disabling is not cryptographic erasure. Version numbers are never reused.
* Deletion = soft delete of the secret; versions retained. **Purge is deferred**
  (needs its own audited migration because the triggers forbid deleting versions).
* Open: creates the file `0600` (exclusive, no symlink following) after the
  directory checks above; enables `journal_mode=WAL`, `foreign_keys=ON`,
  `synchronous=FULL`, `busy_timeout=5s`, `_txlock=immediate`. `init` and `restore`
  call it directly; the ordinary commands and `backup` reach it only after the
  existence and initialization checks (see "Database inspection and
  initialization"), so they never create or migrate a missing, empty, foreign or
  uninitialized file.
* **Migrations** are forward-only and embedded, each in its own transaction
  (re-checked under the lock, so concurrent processes are safe). A database from a
  **newer** schema is refused untouched (`ErrSchemaTooNew`).
* **Pre-migration backup.** Before changing a non-empty database, *every* migration
  attempt writes a **new** snapshot
  `<db>.pre-migration-v<N>-<UTC time>-<random>.bak` (created exclusively `0600`,
  `VACUUM INTO`, then verified: `integrity_check` = ok and `schema_migrations`
  at version N, then fsynced). If the snapshot cannot be created and verified the
  migration is **not attempted**. A failed migration followed by more writes and a
  retry therefore never reuses an older snapshot (this was a defect in the first
  version of M1). Restoring = stop DSO, remove `<db>`, `<db>-wal`, `<db>-shm`,
  copy the chosen `.bak` to `<db>` with mode `0600`. Backups are **never deleted
  automatically** (they accumulate and contain ciphertext and wrapped keys —
  treat them like the database). The manual procedure above applies to these
  pre-migration files; whether `dso central restore` accepts a pre-migration
  `.bak` as a candidate was not verified for this document. `dso central backup`
  and `restore` are described below.
* Concurrency: SQLite's locking serialises writers across goroutines and
  processes; the next version number is computed inside the write transaction and
  `PRIMARY KEY(secret_id, version)` is the backstop. Panics inside the sealing
  callback roll the transaction back.

## Audit log (added after M1)

Policy questions about this log (integrity, failure behavior, attribution) are
under discussion in in-progress draft records that are not committed and are not
part of the tracked tree; none of them is ratified. Treat what follows as the
current implementation, not as an approved policy.

Table `audit_events` (migration `0003`): `seq`, `id`, `schema_version`,
`event_type`, `outcome` (`success`|`failure`), `reason_category` (a closed set,
never a raw error string), `actor_label`, `project_slug`, `environment_slug`,
`secret_name`, `version`, `created_at`, `prev_hash`, `event_hash`. Triggers abort
any `UPDATE` or `DELETE` of a row. `event_hash = SHA-256(prev_hash ||
canonical(fields))` with length-prefixed fields; there is **no MAC or signing
key**. Event types: `init`, `project_create`, `environment_create`,
`secret_create`, `secret_add_version`, `secret_get_reveal`,
`secret_disable_version`, `secret_delete`, `backup`, `restore`.

* **Atomic events.** Project, environment and secret creation, version addition,
  version disabling and secret deletion append their success event in the same
  transaction as the change; if the audit insert fails, the change rolls back.
* **Best-effort events.** Failure events and the reveal event (`secret get
  --reveal`) are appended standalone; if that append fails the operation result
  is unchanged. The `dso central` commands pass the Error-level CLI audit logger
  (and `init`, `backup` and `restore` use it directly), so a failed best-effort
  append is logged at Error level; that log line is not durable or tamper-proof
  evidence on its own.
* **After-the-fact event.** The `restore` success event is appended to the
  restored database after it is reopened; replacement and event are not atomic.
  The append is best-effort: if it fails, the failure is logged at Error level
  and the restore still reports success (no restore event id is printed).
* **Backup and restore events.** Only the types `backup` and `restore` are used,
  with the existing `success`/`failure` outcomes and these fixed
  `reason_category` values (never error text or paths); every append is
  best-effort and never changes the operation's result:

  | Event | Outcome | Reason | Written to |
  |---|---|---|---|
  | `backup` | `success` | none, or `sync_warning` (valid backup, directory fsync unconfirmed) | the active database, after the backup is created and verified; **never inside the backup file** |
  | `backup` | `failure` | `destination_invalid`, `destination_exists`, `backup_creation_failed`, `verification_failed` | the active database |
  | `restore` | `success` | none | the restored database |
  | `restore` | `failure` | `snapshot_failed`, `checkpoint_failed` | the original active database, while still open |
  | `restore` | `failure` | `database_replace_failed` | the original active database, only if it can be reopened after the failed replacement |

  The mandatory pre-restore safety snapshot is a plain backup and emits no
  `backup` event of its own. `sync_warning` means the backup file is valid but
  fsyncing its directory was not confirmed; the audit record says `success`
  while the `backup` command still returns that warning as an error (existing
  CLI behavior, unchanged).
* **Not audited.** Listings and reads, `audit verify`, dry-run restores
  (success or failure), a **declined restore confirmation** (recording it would
  open and write to the active database, and a refusal, EOF or closed stdin must
  leave it untouched), wrong-root-key and other candidate-validation failures,
  lock-busy failures, and a failed reopen after replacement (the database is then
  in an uncertain state and is never written to) write no event. A failure
  before the database exists or can be opened cannot be recorded either; this
  includes a refusal by the initialization check (ordinary commands and
  `backup`), because the database is inspected but never opened. Some early error
  paths (for example ID generation, AAD construction and sealing in secret
  creation) return without any failure event.
* **Attribution.** `actor_label` is a caller-supplied string (`local-cli:<user>`)
  and is not authenticated. Reveal, project-creation, environment-creation and
  `init` events carry no actor at all.
* **Appends.** A standalone append reads the previous tip and inserts in one
  immediate transaction; an in-process concurrency test exists. Concurrent
  appends from several processes have no dedicated test (see "Test coverage").
* **Integrity limits (unchanged by any command).** The chain does not detect
  deletion of the newest events or replacement of the database by an older valid
  copy. Anyone who can write the database can drop the append-only triggers and
  recompute a valid chain. A successful `audit verify` is therefore not proof of
  authenticity.

## `dso central` (local administration; experimental)

```
dso central init   --db D --key-provider file|passphrase --key-file K [--passphrase-file F] [--use-existing-key]
dso central project|environment create|list …
dso central secret create|add-version <project> <env> <name> [--value-stdin] [--comment …]
dso central secret get <project> <env> <name> --reveal [--version N]
dso central secret list|versions|disable-version|delete …
dso central backup --db D --out FILE
dso central restore --db D --key-provider … --key-file K --from FILE [--dry-run] [--force]
dso central audit verify --db D [--json]
```

`--db`, `--key-provider`, `--key-file` and `--passphrase-file` are persistent
flags of the `central` group (`internal/cli/central.go`); `backup` and `audit
verify` do not load a key.

**An initialized database is required.** The eleven ordinary commands that share
one open path (`project create|list`, `environment create|list` and the seven
`secret` subcommands) run, in this order: the flag check, `requireExistingDatabase`
(a missing path, a symlink or a directory is refused), `CheckInitialized`, key-provider
construction and `sqlite.Open`. A `--db` that is not an initialized (or adoptable)
database is therefore refused *before* a key file is read, a passphrase is prompted
for, or the database is opened. A refusal neither creates nor migrates the database
file and does not take the central advisory lock. When no sidecars exist and the
immutable view succeeds, no filesystem entry is created at all; when sidecars exist,
or the inspection falls back to the WAL-aware view, SQLite may rewrite the `-shm` file
or create a missing sidecar (SQLite also applies its own file locking to that read,
which is separate from the central lock; see "Known limitations"). The exit status is
1 like any other failure. `backup` applies the same two checks before it opens the
database (see below). `init`, `restore` and `audit verify` are **separate paths**
with their own rules and do not run the initialization check: `init` creates the database, `restore` may restore into a
missing one, and `audit verify` never opens the live database. See "Database
inspection and initialization".

Security assumptions: runs as the invoking OS user directly against the files;
**no authentication, no network listener**; file permissions and the directory
checks above are the only access control. Values are never accepted as arguments
(shell history / `ps`): they come from a hidden terminal prompt or `--value-stdin`
(one trailing newline is removed). Plaintext is printed only by `get --reveal`.
Passphrases come from a hidden prompt or a private `--passphrase-file`, never from
argv or the environment. Nothing is wired into the REST API or WebUI. The
service logs ids, names, versions and actor only (tests assert values and key
material never appear in the audit events and logs they inspect); the `dso
central` commands supply the Error-level-filtered audit logger, so only
audit-write failures are shown.

**`init` and recovery.** `init` checks everything it can *before* writing any key
material (directory safety, an existing database, an existing key). It then
installs the key, creates the database and binds it. If a failure happens after
the key was installed, the key is **kept** (never deleted or overwritten), the
database files that run created are removed, and the error says so. Continue with
`dso central init … --use-existing-key`, which builds the database around the key
that is already there and never modifies it. A plain re-run is refused with these
instructions. Do not delete a key unless you are certain no database was ever
created with it.

**Interrupted `init` (process killed).** The paragraph above covers a failure that
returns an error. A process killed during `init` (`SIGKILL` was tested; power loss is
expected to behave similarly but was not tested) leaves files that no error describes,
and two cases that need attention differ:

* A **zero-byte** database file: `init` treats it as a reusable leftover and
  proceeds.
* A **non-empty** database left mid-initialization. SQLite writes the first schema
  into the `-wal` file, so a process killed after the schema was applied but before
  the binding was committed leaves a main file holding only the header page and a
  `-wal` holding the schema (observed: a 4096-byte main file and a `-wal` of about
  100 KB). A plain `init` is refused ("already exists; refusing to initialise over
  it"), and the ordinary commands and `backup` report it as inconclusive because a
  non-empty `-wal` is present (see "Database inspection and initialization").

A third outcome needs no recovery. If the process is killed *after* the binding was
committed, the binding may exist only in the `-wal`; the resulting database is
initialized and the initialization check accepts it (tested with a `SIGKILL` right
after `Bind`), so the ordinary commands and `backup` get past it; a manual
`project list` against such a database also succeeded. So not every termination
during `init` leaves an unusable database.

Recovery from the second case is a **manual, preserve-first procedure**. dso does
not delete, replace or repair these files, and the guidance it prints says so:

1. Stop every process that uses the database.
2. Copy the main database file, its `-wal` and its `-shm` (whichever of them
   exist) to a safe location.
3. Verify the copies are complete and stored safely.
4. Only then, and only if you intend to abandon the original state, remove those
   files (the database, `-wal` and `-shm`), keeping the `<db>.central.lock` sidecar
   and the root key file, and run `dso central init … --use-existing-key` again.

The copy in step 2 is what makes step 4 safe: a damaged `-wal` can hold transactions
the inspection cannot see, so no inspection result proves such a database
disposable. The guidance is printed only when the database is *provably* an empty
DSO schema (see "Database inspection and initialization"); otherwise the refusal
carries no recovery guidance at all.

The refusal itself is **not completely side-effect-free.** To see a schema that
lives in the `-wal`, the ordinary commands and `backup` inspect such a database
through SQLite's read-only WAL-aware view, which may rewrite the `-shm` file (the
shared wal-index) and, when `-wal` or `-shm` is missing, may create it. In the
tested case the main database file and the `-wal` stayed byte-identical and the
`-shm` was still present, and the procedure above is unaffected because step 2
copies all three files before anything is removed. Treat the three files as live
state that a refused command may touch, not as frozen.

### `backup`, `restore` and `audit verify`

* **`backup --out FILE`.** `--db` must be an existing regular file (a missing
  path, a symlink or a directory is refused), and `backup` then calls
  `CheckInitialized` and refuses every database that is not initialized or
  adoptable: an empty or non-SQLite file, a foreign SQLite file, a DSO schema with
  no binding and no projects, a partial or invalid binding, a newer schema, and any
  inconclusive inspection (a locked database; a non-empty `-wal` with the expected
  schema missing, as after an interrupted `init`). Both checks run before
  `sqlite.Open` and before the destination is handled, so a refused backup creates
  no destination file or directory, no lock sidecar and no `.pre-migration` copy,
  and appends no audit event to the database. There is deliberately no exception
  for a corrupt binding: backing it up would append an event to the corrupt file.
  To preserve a suspect database, copy its files by hand (see "Interrupted
  `init`"). The error is the one `CheckInitialized` returns, unchanged.

  For a database that passes, it writes a point-in-time snapshot (`VACUUM INTO`),
  re-opens it read-only and checks `integrity_check` and the schema version, then
  fsyncs it. The file is created exclusively with mode `0600`; an existing path is
  never overwritten, and a partial file is removed on failure. It contains every
  table including the full audit history and never contains the root key or
  passphrase. Backups are never pruned. Opening an initialized or adoptable
  database is the ordinary open: when the stored schema is older than the binary's
  (a valid legacy schema-1 database with a project is adoptable) it takes a
  pre-migration `.bak` and migrates before the backup is taken (a database already
  at the latest schema is not changed).
* **`restore --from FILE`.** An **offline** operator action. Cross-process
  coordination between `dso central` processes uses an advisory `flock(2)` on
  the sidecar `<db>.central.lock`:
  * *Shared lock.* Every command that opens the database through the store's
    normal open path takes it and holds it until that command closes the store:
    `init`, `project create|list`, `environment create|list`, every `secret`
    subcommand (`create`, `add-version`, `get`, `list`, `versions`,
    `disable-version`, `delete`) and `backup` (after its existence and
    initialization checks pass). Any number of shared holders can coexist.
    `secret create` and `secret add-version` open the database before they read
    the value, so one waiting at its value prompt still holds the shared lock.
  * *Exclusive lock.* A non-dry-run `restore` does not take a shared lock. It
    takes the lock **exclusively** after the candidate has been staged and
    validated (including the root-key check) and before the confirmation
    prompt, and keeps it through the confirmation, the safety snapshot, the
    replacement and the final reopen. The lock is requested without blocking
    and retried every 50 ms, and it is granted only while no other `dso central`
    process holds the lock in any mode.
  * *Contention.* If a shared or exclusive holder is still present when the
    timeout expires (5 s; the CLI does not change this default), `restore`
    fails with `ErrCentralLockBusy` ("acquiring exclusive restore lock on
    ..."). That happens before any prompt, safety snapshot or change: the
    active database is untouched and nothing is audited. Restore is therefore
    **not** guaranteed to obtain the exclusive lock; let the running command
    finish (including one sitting at a value prompt) and retry. Conversely,
    while a restore holds the lock, including while it waits at its
    confirmation prompt, any other command that opens the database fails with
    `ErrCentralLockBusy` after the same 5 s instead of racing the replacement.
  * *No lock.* `restore --dry-run`, a restore whose candidate fails validation
    (both return before the lock is requested) and `audit verify` (which
    verifies a private staged copy and never opens the live database) take no
    central lock. Neither does the initialization inspection that the ordinary
    commands and `backup` run before opening the database: it runs *before* the
    shared lock is requested, so a concurrent `restore` can replace the file
    between the check and the open (see "Known limitations").
  * *Limits.* The lock is advisory and local only: a process that bypasses
    `dso central` (for example the `sqlite3` CLI or a script opening the file
    directly), another host, or a filesystem without working `flock` is not
    coordinated, and a write from such a process during the restore can still
    be lost, so stop every other user of the database anyway. On a build for a
    platform without `flock` support, opening the database fails instead of
    proceeding unlocked. The kernel releases the lock when its holder exits or
    crashes, and the sidecar file is never deleted.

  It stages a private copy of the candidate (and its `-wal`/`-shm`
  sidecars), validates structure, schema version, the audit chain and the
  root-key binding on that copy, and activates that same copy; it does not prove
  every secret version decrypts. Unless `--dry-run`, it asks for confirmation
  (`--force` skips the prompt), writes a verified safety snapshot
  `<db>.pre-restore-<time>-<random>.bak`, checkpoints the active database,
  replaces it and reopens it. Failure modes (from the code and its tests):
  rejected before any change (active database untouched); replacement failed
  before activation (the original database remains active and is not replaced; a `database_replace_failed` event may be appended to it if it reopens successfully, and nothing is written if it does not); replacement activated but the
  reopen failed (**not** automatically recovered; recover from the safety
  snapshot manually); replacement usable but the restore event could not be
  recorded (logged at Error level; the command still succeeds); replacement activated but durability could
  not be confirmed (event recorded, error reported). Restoring an older backup
  drops newer audit events from the active chain; they survive only in the
  safety snapshot.

  `restore` does **not** run the initialization check (it must be able to restore
  into a missing database), so it is not protected like the ordinary commands and
  `backup`. When an active file exists, it is opened through the normal open path
  to take the safety snapshot. Observed once by hand in a review of this document,
  **not covered by a test and not a guarantee**: `restore --force` over a zero-byte
  file and over a foreign SQLite file both succeeded, the file being migrated first
  (the foreign one gained the DSO tables next to its own), so the safety snapshot
  reflects the migrated file. `restore` has no initialization protection; whether it
  should refuse such an active file is flagged as a separate potential follow-up and
  is not changed here.
* **`audit verify`.** Read-only: it never opens the live database through
  SQLite; it verifies a staged copy and retries if the files change while being
  copied. It needs temporary disk space about the size of the database, and
  after `SIGKILL` or a machine failure a `dso-restore-validate-*` temporary
  directory (mode `0700`) can remain and must be removed manually. Exit codes:
  `0` verified, `10` integrity failure, `11` verification unavailable, `1` usage
  or other errors. It is not a consistent snapshot of a database being written
  and not proof of authenticity (see "Audit log"). It does not use the
  initialization check; for example a zero-byte file is reported as verification
  unavailable (exit `11`) and is not modified.

## Database inspection and initialization

Two read-only layers decide whether a `--db` path is an initialized database. They
never create, migrate or write the database file, and `InspectDatabase` never takes
the central advisory lock. Two things are separate from that: SQLite may create or
rewrite the `-wal` and `-shm` sidecars during the WAL-aware view, and SQLite applies
its own file locking to that read (which is why a busy database is retried); neither
is the central lock. They guard against accidental path mistakes and
incomplete initialization; they are **not** a defence against a hostile process or
a concurrent replacement of the file, and they are **not race-free**.

### Inspection: `sqlite.InspectDatabase`

`InspectDatabase(path, probe)` runs a caller-supplied, read-only, idempotent probe
against the file and returns an `InspectionResult`. The probe runs inside the
package, not on a handle returned to the caller, because an immutable SQLite view is
not a snapshot: pages it cached before a writer finished stay stale while pages it
reads later may already be new, and no check made when a handle is opened can
protect queries made afterwards. Only the result of the last probe invocation, and
only when the call returns no error, is valid. The package knows nothing about
bindings, keys or projects.

**File gate.** The path is opened with the same private-file rules as the rest of
the store (`internal/securefs`: directory chain, no final symlink, regular file owned
by the caller with no group/other access), and its first 16 bytes are read from that
descriptor. A zero-byte file is `ErrEmptyDatabase` (even when a `-wal` exists); a
non-empty file without the SQLite header is `ErrNotSQLite`. `ErrInspectionFailed`
covers everything else that prevents a safe inspection: a locked database after the
bounded retries, damage reported by SQLite, a non-regular sidecar, a probe that
failed, or a file that kept changing. **`ErrInspectionFailed` never means "not
initialized".**

**Two views.**

* *Immutable view.* Used when neither `<db>-wal` nor `<db>-shm` exists, so no
  connection can be live and no WAL can be pending. The probe runs in a read-only
  transaction on a `mode=ro&immutable=1` view, which creates and modifies nothing.
  The result is accepted only if, after the probe, the sidecars are still absent,
  the file signature is unchanged, and the path still names the file whose header was
  checked. Otherwise the result is discarded and the whole attempt starts again, up to
  3 attempts in total; persistent change is `ErrInspectionFailed` ("kept changing").
  An immutable read that itself fails without any observed change (for example a
  torn read reported as "malformed") falls back to the WAL-aware view.
* *WAL-aware view.* Used when any sidecar exists or the immutable view could not
  produce a result. The probe runs in **one** read transaction on a `mode=ro` view
  (a consistent snapshot that sees WAL-resident data) with a 1.5 s busy timeout;
  transient `SQLITE_BUSY`/`SQLITE_LOCKED` errors are retried, 3 tries in total, 50 ms
  apart. SQLite may rewrite the `-shm` file (the shared wal-index) for this read and,
  when only one of `-wal` and `-shm` exists, may create the other. The main database
  file and an existing `-wal` are never written.

**Detecting a change during inspection.** The *immutable view* captures a signature
before the probe and compares it after the probe: on **Linux** the device, inode,
size, modification time (mtime) and change time (ctime); on **other platforms** only
size and mtime (`inspect_ctime_other.go`). ctime cannot be set from user space, so it
also catches a writer that restored the old mtime. Device and inode are taken from
the same descriptor before and after, so replacement of the path is detected by
comparing the descriptor with the path (`os.SameFile`) after the probe. This is verification, not a
guarantee: the kernel stamps mtime and ctime from the same clock, so a write that
lands in the same timestamp tick on a **coarse-resolution filesystem** and leaves the
size unchanged can escape detection, and a replacement that happens after the final
comparison, or is swapped back, is not seen. No content fingerprint is taken. The
*WAL-aware view* does not perform this full signature comparison: after its probe it
re-checks only that the path still names the file whose header was checked, and a
replaced path restarts the attempt.

**`InspectionResult`.** `View` is `ViewImmutable` or `ViewWALAware`. `WALPresent` is
true if a **non-empty** `-wal` was observed during the attempt that produced the
result (when it began, just before the database was opened, or just after the
probe); presence seen at any of those points is kept even if the file is gone by the
end. An empty `-wal` does not count: SQLite creates one itself when a read-only
connection opens a WAL database that has none. `SHMPresent` is true if an `-shm`
existed when the attempt began or just before the database was opened; it is not
sampled after the probe, because the WAL-aware read creates an `-shm` itself. The
flags describe only the winning attempt, so a caller never needs (and must not use) a
separate filesystem check afterwards.

**What a negative finding means.**

| View | `WALPresent` | A negative finding ("the expected schema or binding is absent") |
|---|---|---|
| WAL-aware | false | Definitive as of the snapshot. |
| Immutable | false | **Verified stable, not proof.** It is strong evidence, but the immutable view cannot see a write the signature does not reveal (see above), so it is never proof that the database is empty or uninitialized. The messages tell the user to retry. |
| either | true | **Inconclusive.** SQLite ignores WAL frames that fail their checksums, so a truncated, damaged or unreplayed `-wal` looks exactly like missing data. It is reported as inconclusive and never as "not initialized". |

A positive finding (rows exist) is trustworthy from either view.

### Initialization check: `centralsvc.CheckInitialized`

`CheckInitialized(path)` gathers its evidence with one probe through
`InspectDatabase` (table names, migration versions, the three binding keys and the
binding version, project counts, and row counts) and classifies it only *after* the
inspection has returned successfully, so a policy outcome can never be confused with
an inspection failure. It does not read or validate the root key; **key ownership
stays with `Bind`.**

| Evidence | Result |
|---|---|
| Complete binding triple (`binding.version`, `binding.root_key_id`, `binding.canary`) with `binding.version == "1"` | Initialized (`nil`). Also true when the triple exists only in a live or crash-left WAL. |
| No binding, at least one project, every project row well formed (non-empty wrapped data key and key id) | **Adoptable** (`nil`): a pre-binding schema-1 database, or one whose binding rows were removed. `Bind` still has to prove that the configured key unwraps **every** project's data key before it creates a binding; the check decides nothing about ownership. |
| One or two binding rows, an unsupported binding version, or project rows with empty key data | `ErrBindingCorrupt`, returned directly. |
| Schema version in the file newer than the binary's | `store.ErrSchemaTooNew`, returned directly. |
| Empty file, non-SQLite file, no DSO schema, or the schema with no binding and no projects | `*NotInitializedError` (matches `errors.Is(err, ErrNotInitialized)`), with a reason. An immutable-view negative is verified stable, not proof, and says to retry. |
| Inspection failed, a deterministic read failure, or any negative made while a non-empty `-wal` is present | `*InconclusiveError` (matches `errors.Is(err, sqlite.ErrInspectionFailed)`, never `ErrNotInitialized`). |
| A securefs refusal (symlink, unsafe mode) | Passed through unchanged. |

*Precedence.* The rows above follow the implementation's evaluation order, not the row
order alone. An empty file (and likewise a non-SQLite file) is reported as not
initialized even if a `-wal` file exists, because that file-level result is returned
before any sidecar is examined. Every other negative finding made while a non-empty
`-wal` is present is inconclusive, never "not initialized".

A database that earlier versions silently created and bound on a mistyped path is
bound and empty, so it is indistinguishable from a freshly initialized one and is
accepted.

*Query failures.* The probe captures a deterministic query failure (SQLite's generic
`SQLITE_ERROR`, for example "no such column" because a foreign table has the name of
a DSO table, or a scan mismatch) instead of returning it, because returning it would
make the inspection fall back to the WAL-aware view, which can create `-wal` and
`-shm`. The captured failure is reported as inconclusive and the partial evidence is
not classified. Every other SQLite error (busy, locked, corrupt, I/O) is returned from
the probe so the inspection keeps its bounded lock retry and its fall-back from a torn
immutable read. The captured error and the evidence are reset at the start of every
probe invocation, so a discarded or retried attempt cannot contaminate the one
that is used.

*Recovery guidance.* The preserve-first guidance (see "Interrupted `init`") is
attached only when the database is provably an empty DSO schema: the exact expected
tables (`schema_migrations`, `projects`, `environments`, `secrets`, `secret_versions`,
`store_meta`, `audit_events`, plus `sqlite_sequence`), only embedded migration
versions, zero rows in every other table, no binding rows, and no foreign tables. With
a `-wal` present the result is still inconclusive and the guidance says the database
must not be treated as disposable.

### Where the check runs

The eleven ordinary commands and `backup` run `requireExistingDatabase`, then
`CheckInitialized`, before key-provider construction and `sqlite.Open` (see the
`dso central` section). `init`, `restore` and `audit verify` do not call it.

## Known limitations

* No authn/authz: any process with the OS user's file access can do everything,
  including reveal, delete, backup and restore. Actor strings are caller-supplied
  and not authenticated; reveal, project/environment creation and `init` events
  have none. The audit log is unkeyed and not tamper-proof (see "Audit log").
* Whole-database rollback to an older valid copy is not detected, and `restore`
  can introduce one deliberately.
* `restore` is offline, and its exclusive cross-process lock is advisory,
  single-host and covers only `dso central` processes (see above); it does not
  make replacement and its audit event atomic.
* The initialization check runs *before* `Open` takes the central lock, so a
  concurrent `restore` can replace the database between the check and the open.
* A locked database (for example one held with an exclusive lock by another
  process) takes up to about 4.5 s (three 1.5 s busy waits; SQLite can return
  early under load, and 3.8 s was observed) before the inspection reports an
  inconclusive result.
* An immutable-view negative is verified stable, **not proof**; a same-size write
  that lands in the same timestamp tick on a coarse-resolution filesystem can escape
  detection. Off Linux the file signature is only size and mtime. These filesystems
  and platforms were not tested.
* The WAL-aware inspection may rewrite the `-shm` file and may create a missing
  `-wal` or `-shm`. A refusal of an interrupted-`init` database is therefore not
  completely side-effect-free; in the tested case the main database file and the
  `-wal` stayed byte-identical and the `-shm` still existed.
* A damaged `-wal` looks like missing data; it is reported as inconclusive, never as
  "not initialized".
* `backup` of a valid legacy schema-1 database still performs the migration and
  creates a `.pre-migration` copy, as before.
* `restore` is not protected by the initialization check (see above) and can
  migrate an unusual active file before taking its safety snapshot; flagged as a
  separate potential follow-up.
* Read-only filesystems. Tested: with no sidecars the immutable view works; with both
  `-wal` and `-shm` present the WAL-aware view works; with only a `-wal` the
  inspection fails closed as an inconclusive result (SQLite cannot create the `-shm`).
  Other combinations, including an `-shm` without a `-wal`, are **untested**; they are
  expected to fail closed the same way, but that is an expectation, not verified
  behavior.
* Different directories ≠ different volumes (not verified, see above).
* In-memory plaintext/DEK zeroing is best-effort (Go may copy buffers). No
  `mlock`, dumpability or core-dump controls were found in the repository's own
  Go sources (a bounded text search; not a proof of absence elsewhere).
* Secret names are not encrypted; AAD includes the name, so **renaming a secret
  requires re-encrypting its versions** (see the addendum); rollback likewise
  creates a new version by re-encrypting.
* Single node, SQLite, local filesystem only; no import-vault, no root-key or DEK
  rotation, no purge, and no rename, archive or deletion of projects or
  environments.
* Not load-tested, not externally reviewed, not fuzzed. The first independent
  review (2026-10-16) found defects that earlier versions of this document
  contradicted; `docs/reviews/` records them.

## Test coverage

Counts below are `func Test…` declarations found in the source when this section
was last updated (after the `backup` initialization protection), counted per
`*_test.go` file with `grep -c '^func Test'`; subtests are not counted. They
include 6 helper-process declarations (`TestHelperProcess…`), which do nothing
unless a parent test sets an environment variable, so the counts overstate the
number of independent tests. **No tests were run to prepare this section, so it
does not state that any test passes.**

| Area | Test files / functions in source |
|---|---|
| `internal/centralsvc` | 7 files, 107 tests (binding, service, audit events, restore, backup audit, initialization check) |
| `internal/store/sqlite` | 15 files, 152 tests (store, transactions, migrations, audit chain and verification, backup, restore validation, central lock, cross-process tests, database inspection) |
| `internal/securefs` | 1 file, 13 tests |
| `internal/crypto` | 3 files, 24 tests |
| `dso central` CLI | 8 files, 74 tests (init, commands, backup/restore, audit verify, audit logger, missing-database and uninitialized-database state matrices for the ordinary commands and `backup`) |

Present in source: wrong-key and binding tests, tamper and AAD-substitution
tests, an in-process concurrent audit-append test, a multi-process version-writer
test, crash-during-writes and uncheckpointed-WAL tests, restore fault-injection
seams (replacement failure, reopen failure, audit-append failure after
activation), WAL/SHM-sidecar handling, FIFO and symlink sidecar rejection, and a
signal-cleanup helper-process test. Since then: a multi-process audit-append test
(four helper processes append to one chain, which is then verified); database
inspection tests (immutable and WAL-aware views, restart when the file changes or
sidecars appear, crash states left by `SIGKILL`ed helper processes, damaged WALs,
bounded lock retries, a read-only mount, restored-mtime detection through ctime);
initialization-check tests (every classification, preserve-first guidance and its
emptiness proof, probe-failure handling and the per-invocation reset); and CLI state
matrices showing that the ordinary commands and `backup` refuse each uninitialized
state with the original error, without opening or migrating the database, creating a
backup destination or accessing the key provider. SQLite sidecar effects during
inspection are described above.

**Not found in source (unverified or untested):** a process killed at each
restore stage; non-Linux runtime behavior (the inspection package was only
cross-compiled for `darwin` and `freebsd`); network or layered filesystems; the
inspection on filesystems with coarse timestamps; `restore` over an uninitialized
active file (observed once by hand, not covered by a test); memory and core-dump
behavior of the key; any authorization behavior (none exists).

## Implemented versus not implemented

| Item | State |
|---|---|
| Envelope encryption, key providers, key binding, secret versioning, soft delete | Implemented (M1) |
| `init`, project, environment and secret commands | Implemented |
| Forward-only migrations with pre-migration backup | Implemented |
| Hash-chained audit log with atomic mutation events | Implemented; unkeyed, with the limits above |
| `backup`, `restore`, `audit verify` | Implemented; offline restore, see failure modes |
| Read-only database inspection (`InspectDatabase`) | Implemented; verified stable, not race-free, see "Database inspection and initialization" and "Known limitations" |
| Initialization detection (`CheckInitialized`) | Implemented; does not check the root key |
| Ordinary-command protection (eleven commands) and `backup` protection against uninitialized, corrupt and inconclusive databases | Implemented; `init`, `restore` and `audit verify` are separate paths |
| `restore` over an uninitialized active file | **Not protected** (current behavior documented); a separate potential follow-up |
| Authentication, authorization, identity, sessions, tokens (planned M2) | **Not implemented.** Only M2.1 (`WithTx`) exists; M2.2 onward is not authorized |
| Network API, key-holding service, peer-credential identity for central | **Not implemented** (design analysis only) |
| Root-key and DEK rotation, import-vault, purge | Not implemented |
| Project and environment lifecycle (rename, archive, delete) | Not implemented; undecided |
| Quarantine and its release path | Not implemented; undecided |
| Production readiness | **Not claimed** |
