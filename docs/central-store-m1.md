# Central store — Milestone 1 (persistent storage and key management)

Status: **experimental, local-only, not production-ready.** M1 provides a
durable, encrypted, versioned secret store and the key-management primitives
under it. It has **no network API, no authentication, no authorization (RBAC)
and no audit enforcement** — those are M2/M3. Unit tests do not establish
production readiness (see "Known limitations").

Specification: `docs/design/central-server-design.md` (§ Encryption and key
management, § Versioning, § Backup, Milestone 1; this file lives in the
repository owner's working tree and is not part of this branch). Decisions made
after the independent review of 2026-10-16, and every place M1 deviates from or
corrects that document, are in `docs/design/m1-design-addendum.md`.

## Packages

| Package | Role |
|---|---|
| `internal/securefs` | Verifies directory chains and key/database files (ownership, modes, symlinks) before they are trusted |
| `internal/crypto` | AES-256-GCM `Seal/Open`, AAD builders, `KeyProvider` interface, `file` and `passphrase` providers, exclusive key installation |
| `internal/store` | `Store` interface and metadata types. No SQL, no crypto |
| `internal/store/sqlite` | SQLite (WAL) implementation, embedded forward-only migrations |
| `internal/centralsvc` | `Service`: composes Store + KeyProvider; binds the database to its root key; the only place plaintext exists |
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
operator must place it on local storage. Likewise back up the database with the
`.bak` mechanism or SQLite's online backup, never by copying a live database file
without its `-wal` companion.

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
`schema_migrations` (migration `0001`), `store_meta` (migration `0002`). Timestamps
are UTC unix seconds.

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
  treat them like the database). A full backup/restore *command* is deferred.
* Concurrency: SQLite's locking serialises writers across goroutines and
  processes; the next version number is computed inside the write transaction and
  `PRIMARY KEY(secret_id, version)` is the backstop. Panics inside the sealing
  callback roll the transaction back.

## `dso central` (local administration; experimental)

```
dso central init   --db D --key-provider file|passphrase --key-file K [--passphrase-file F] [--use-existing-key]
dso central project|environment create|list …
dso central secret create|add-version <project> <env> <name> [--value-stdin] [--comment …]
dso central secret get <project> <env> <name> --reveal [--version N]
dso central secret list|versions|disable-version|delete …
```

Security assumptions: runs as the invoking OS user directly against the files;
**no authentication, no network listener**; file permissions and the directory
checks above are the only access control. Values are never accepted as arguments
(shell history / `ps`): they come from a hidden terminal prompt or `--value-stdin`
(one trailing newline is removed). Plaintext is printed only by `get --reveal`.
Passphrases come from a hidden prompt or a private `--passphrase-file`, never from
argv or the environment. Nothing is wired into the REST API or WebUI. The service
logs ids, names, versions and actor only (tests assert values and key material
never appear).

**`init` and recovery.** `init` checks everything it can *before* writing any key
material (directory safety, an existing database, an existing key). It then
installs the key, creates the database and binds it. If a failure happens after
the key was installed, the key is **kept** (never deleted or overwritten), the
database files that run created are removed, and the error says so. Continue with
`dso central init … --use-existing-key`, which builds the database around the key
that is already there and never modifies it. A plain re-run is refused with these
instructions. Do not delete a key unless you are certain no database was ever
created with it.

## Known limitations

* No authn/authz: any process with the OS user's file access can do everything.
  `created_by/disabled_by` are caller-supplied attribution strings, **not** an
  audit log and not tamper-evident; audit chain and enforcement are M2.
* Whole-database rollback to an older valid copy is not detected.
* Different directories ≠ different volumes (not verified, see above).
* In-memory plaintext/DEK zeroing is best-effort (Go may copy buffers; no `mlock`).
* Secret names are not encrypted; AAD includes the name, so **renaming a secret
  requires re-encrypting its versions** (see the addendum); rollback likewise
  creates a new version by re-encrypting.
* Single node, SQLite, local filesystem only; no backup/restore/verify commands,
  no import-vault, no root-key or DEK rotation, no purge.
* Not load-tested, not externally reviewed, not fuzzed. The first independent
  review (2026-10-16) found defects that earlier versions of this document
  contradicted; `docs/reviews/` records them.
