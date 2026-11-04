# Central store — Milestone 1 (persistent storage and key management)

Status: **experimental, local-only, not production-ready.** M1 provides a
durable, encrypted, versioned secret store and the key-management primitives
under it. It has **no network API, no authentication and no authorization
(RBAC)**; identity and authorization are planned for M2 and are not built.
Work after M1 added an append-only, hash-chained audit log, an on-demand
`backup` command, a `restore` command and a read-only `audit verify` command
(commits `c4d404c`, `22ee5be`, `ff4d8a5`, `3645b89`); they are described below.
Their existence does not make the store production-ready, and unit tests do not
establish production readiness (see "Known limitations" and "Test coverage").

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
| `internal/store/sqlite` | SQLite (WAL) implementation, embedded forward-only migrations, audit-event append and chain verification, on-demand backup, staged restore-candidate validation |
| `internal/centralsvc` | `Service`: composes Store + KeyProvider; binds the database to its root key; the only package that decrypts secrets (the CLI process also holds plaintext it reads with `readValue` and prints with `secret get --reveal`); also hosts the `Restore` state machine |
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
deliberate. The vault file lock from M0 is a no-op on non-unix platforms.

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
  `synchronous=FULL`, `busy_timeout=5s`, `_txlock=immediate`.
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
  before the database exists or can be opened cannot be recorded either. Some early error
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

### `backup`, `restore` and `audit verify`

* **`backup --out FILE`.** Writes a point-in-time snapshot (`VACUUM INTO`),
  re-opens it read-only and checks `integrity_check` and the schema version, then
  fsyncs it. The file is created exclusively with mode `0600`; an existing path is
  never overwritten, and a partial file is removed on failure. It contains every
  table including the full audit history and never contains the root key or
  passphrase. Backups are never pruned. Opening the database to take a backup
  runs the same open path as every command, so it creates a missing database
  file and, when the stored schema is older than the binary's, takes a
  pre-migration `.bak` and migrates (a database already at the latest schema is
  not changed).
* **`restore --from FILE`.** An **offline** operator action. Every `dso central`
  command that opens the database (`init`, the secret and project commands,
  `backup`, `restore`) takes an advisory `flock(2)` on the sidecar
  `<db>.central.lock` (shared; any number of shared holders). A non-dry-run
  restore takes it **exclusively** after the candidate has been validated and
  before the confirmation prompt, and holds it through the final reopen, so a
  concurrent `dso central` command, or a second restore, against the same path
  fails with `ErrCentralLockBusy` after a 5 s default timeout instead of racing
  the replacement. `--dry-run` and a candidate that fails validation never take
  it. The lock is advisory and local only: a process that bypasses `dso central`
  (for example the `sqlite3` CLI or a script opening the file directly), another
  host, or a filesystem without working `flock` is not coordinated, and a write
  from such a process during the restore can still be lost, so stop every other
  user of the database anyway. The kernel releases the lock when its holder
  exits or crashes, and the sidecar file is never deleted. It stages a private copy of the candidate (and its `-wal`/`-shm`
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
* **`audit verify`.** Read-only: it never opens the live database through
  SQLite; it verifies a staged copy and retries if the files change while being
  copied. It needs temporary disk space about the size of the database, and
  after `SIGKILL` or a machine failure a `dso-restore-validate-*` temporary
  directory (mode `0700`) can remain and must be removed manually. Exit codes:
  `0` verified, `10` integrity failure, `11` verification unavailable, `1` usage
  or other errors. It is not a consistent snapshot of a database being written
  and not proof of authenticity (see "Audit log").

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

Counts below are `func Test…` declarations found in the source on this date
(subtests not counted). **No tests were run to prepare this section, so it does
not state that any test passes.**

| Area | Test files / functions in source |
|---|---|
| `internal/centralsvc` | 5 files, 58 tests (binding, service, audit events, restore) |
| `internal/store/sqlite` | 12 files, 98 tests (store, transactions, migrations, audit chain and verification, backup, restore validation) |
| `internal/securefs` | 1 file, 13 tests |
| `internal/crypto` | 3 files, 24 tests |
| `dso central` CLI | 5 files, 38 tests (init, commands, backup/restore, audit verify, audit logger) |

Present in source: wrong-key and binding tests, tamper and AAD-substitution
tests, an in-process concurrent audit-append test, a multi-process version-writer
test, crash-during-writes and uncheckpointed-WAL tests, restore fault-injection
seams (replacement failure, reopen failure, audit-append failure after
activation), WAL/SHM-sidecar handling, FIFO and symlink sidecar rejection, and a
signal-cleanup helper-process test.

**Not found in source (unverified or untested):** multi-process audit appends;
the CLI's discard of best-effort audit failures (nil logger); a process killed at
each restore stage; non-Linux runtime behavior; network or layered filesystems;
memory and core-dump behavior of the key; any authorization behavior (none
exists).

## Implemented versus not implemented

| Item | State |
|---|---|
| Envelope encryption, key providers, key binding, secret versioning, soft delete | Implemented (M1) |
| `init`, project, environment and secret commands | Implemented |
| Forward-only migrations with pre-migration backup | Implemented |
| Hash-chained audit log with atomic mutation events | Implemented; unkeyed, with the limits above |
| `backup`, `restore`, `audit verify` | Implemented; offline restore, see failure modes |
| Authentication, authorization, identity, sessions, tokens (planned M2) | **Not implemented.** Only M2.1 (`WithTx`, commit `8c3763c`) exists; M2.2 onward is not authorized |
| Network API, key-holding service, peer-credential identity for central | **Not implemented** (design analysis only) |
| Root-key and DEK rotation, import-vault, purge | Not implemented |
| Project and environment lifecycle (rename, archive, delete) | Not implemented; undecided |
| Quarantine and its release path | Not implemented; undecided |
| Production readiness | **Not claimed** |
