# Central store — Milestone 1 (persistent storage and key management)

Status: **experimental, local-only, not production-ready.** M1 provides a
durable, encrypted, versioned secret store and the key-management primitives
under it. It has **no network API, no authentication, no authorization (RBAC)
and no audit enforcement** — those are M2/M3. Unit tests do not establish
production readiness (see "Known limitations").

Specification: `docs/design/central-server-design.md` (§ Encryption and key
management, § Versioning, § Backup, Milestone 1). Deviations and open
decisions are listed at the end.

## Packages

| Package | Role |
|---|---|
| `internal/crypto` | AES-256-GCM `Seal/Open`, AAD builders, `KeyProvider` interface, `file` and `passphrase` providers, exclusive key installation |
| `internal/store` | `Store` interface and metadata types. No SQL, no crypto |
| `internal/store/sqlite` | SQLite (WAL) implementation, embedded forward-only migrations |
| `internal/centralsvc` | `Service`: composes Store + KeyProvider; the only place plaintext exists |
| `internal/cli/central.go` | `dso central …` local administration |

Driver: `modernc.org/sqlite v1.50.0` (pure Go, `CGO_ENABLED=0` compatible, as the
design recommends). It is the only new direct dependency; the version was chosen
because newer releases require a newer Go directive than this module declares.

## Threat model (what M1 does and does not protect)

Protects against: theft or leakage of the **database file alone** (backups,
snapshots, a read-only SQL bug, a stray copy) — it contains only ciphertext and
wrapped keys; row-level tampering or swapping of ciphertext between
projects/environments/secrets/versions (AAD binding, fails closed); silent
corruption (GCM tag); another local user reading the files (0600/0400 modes
enforced, insecure modes refused).

Does **not** protect against: compromise of the host account that runs DSO (it
holds the unwrapped keys in process memory while serving a request, and can read
the key file); theft of database **and** root key together (file provider); an
attacker who can *replace* the database file with an older valid copy
(rollback/replay of whole-DB state is not detected); anyone who can run
`dso central` as the owning OS user. Secret **names, project/environment slugs,
timestamps and actor strings are metadata and are stored in clear.**

### Key-custody trade-off (needs your decision)

| | `file` | `passphrase` |
|---|---|---|
| Root key at rest | 256-bit random, `0400`, in a file | **never stored**; Argon2id-derived (t=3, 64 MiB, p=4, 16-byte salt, params + key-id check value in a `0400` file) |
| Unattended restart | yes | **no** — operator supplies passphrase at each start |
| Defends | DB/backup leak when the key file is stored elsewhere | additionally, theft of the whole host disk *including* the key file |
| Weakness | key file next to DB (or readable by the service account) = full compromise; `init` refuses same-directory placement unless `--allow-colocated-key` | passphrase strength (offline guessing against DB + params file, slowed by Argon2id; ≥12 chars enforced at init); passphrase lands in a file if `--passphrase-file` is used (kept `0600`, refused otherwise) |

Neither is HSM-grade. There is **no default provider**: `--key-provider` must be
chosen explicitly. A KMS provider can be added by implementing `KeyProvider`
(`ID/Wrap/Unwrap`) — storage logic does not change.

## Key hierarchy and lifecycle

```
root key (KeyProvider; never in the DB)
  └─ wraps → per-project DEK (random 256-bit; AES-GCM, AAD = project|key-id|dek-version)
        └─ encrypts → each secret VERSION (AES-256-GCM, random 96-bit nonce,
             AAD = project|environment|secret-id|name|version|dek-version)
```

* Create project: generate DEK → wrap → store `wrapped_dek`, `dek_key_id`,
  `dek_version`. The plaintext DEK is zeroed after use and never cached.
* Each write: fresh random nonce per seal (`crypto/rand`; entropy failure =
  error). Random 96-bit nonces under one DEK are safe far beyond any realistic
  version count (NIST bound 2³² messages per key); a per-DEK counter or DEK
  rotation is a later hardening.
* Read: check `dek_key_id` equals the provider's id (else `ErrWrongRootKey`),
  unwrap, open with the row's own identity as AAD. Any authentication failure
  returns `ErrIntegrity` with no detail.
* Key files are installed with temp file (`0400`) + fsync + `link(2)` + dir
  fsync: they are never overwritten or truncated and concurrent installers
  produce exactly one winner. Loading refuses symlinks, non-regular files,
  files not owned by the user, any group/other permission bit, malformed or
  missing material.
* **Not in M1:** root-key rotation (re-wrap DEKs), DEK rotation (re-encrypt a
  project), root-key backup/escrow tooling. `dek_version` and `dek_key_id` are
  stored so these can be added without a format change. **Losing the root key
  makes all data unrecoverable; back it up separately from the database.**

## Schema and migrations

Tables (`migrations/0001_init.sql`): `projects`, `environments`, `secrets`,
`secret_versions`, `schema_migrations`. Timestamps are UTC unix seconds.

* Foreign keys `ON DELETE RESTRICT`; `secrets` references
  `(environment_id, project_id)` so an environment from another project is
  rejected by the database.
* `UNIQUE(project.slug)`, `UNIQUE(project_id, env slug)`, partial unique index
  on live `(environment_id, name)` (a soft-deleted name can be reused),
  `PRIMARY KEY(secret_id, version)`, `CHECK`s on nonce length and non-empty ciphertext.
* **Immutability is enforced by the database**: triggers abort any `UPDATE` of a
  version row except the one-way `NULL → set` of `disabled_at/disabled_by`, and
  abort every `DELETE`. This is defence in depth against application bugs; an
  attacker with raw write access can drop triggers, but cannot forge a valid
  ciphertext without the keys, and the AAD check still fails closed.
* Latest version = highest **enabled** version. Version numbers are never reused.
* Deletion = soft delete of the secret (`deleted_at/by`), versions retained.
  **Purge is deferred** (needs its own audited migration because triggers forbid
  deleting versions).
* Open: creates the file `0600` in a `0700` directory (exclusive create), refuses
  an existing file with group/other bits, enables `journal_mode=WAL`,
  `foreign_keys=ON`, `synchronous=FULL`, `busy_timeout=5s`, `_txlock=immediate`
  (writers take the write lock up front — no lock-upgrade deadlocks). WAL/SHM
  inherit the main file's mode.
* Migrations are forward-only, embedded, each applied in its own transaction
  (re-checked under the lock, so concurrent processes are safe). Before changing
  a non-empty database a restorable copy `<db>.pre-migration-v<N>.bak` (`0600`)
  is made with `VACUUM INTO`; if that fails the migration is not attempted. A
  database from a **newer** schema is refused untouched (`ErrSchemaTooNew`).
* Concurrency: SQLite's own locking serialises writers across goroutines and
  processes; the next version number is computed inside the write transaction,
  and `PRIMARY KEY(secret_id, version)` is the backstop.

## `dso central` (local administration; experimental)

```
dso central init   --db D --key-provider file|passphrase --key-file K [--passphrase-file F]
dso central project|environment create|list …
dso central secret create|add-version <project> <env> <name> [--value-stdin] [--comment …]
dso central secret get <project> <env> <name> --reveal [--version N]
dso central secret list|versions|disable-version|delete …
```

Security assumptions: runs as the invoking OS user directly against the files;
**no authentication, no network listener**; file permissions are the only access
control. Values are never accepted as arguments (shell history / `ps`): they
come from a hidden terminal prompt or `--value-stdin`. Plaintext is printed only
by `get --reveal`. Passphrases come from a hidden prompt or a `0600`
`--passphrase-file`, never from argv or the environment. Nothing is wired into
the REST API or WebUI. Logging by the service layer records ids, names,
versions and actor only (a test asserts values and key material never appear).

## Known limitations

* No authn/authz: any process with the OS user's file access can do everything.
  `created_by/disabled_by` are caller-supplied attribution strings, **not** an
  audit log and not tamper-evident; audit chain and enforcement are M2.
* Whole-database rollback to an older valid copy is not detected.
* In-memory plaintext/DEK zeroing is best-effort (Go may copy buffers; no `mlock`).
* Secret names are not encrypted; AAD includes the name, so **renaming a secret
  later requires re-encrypting its versions**.
* Single node, SQLite; no backup/restore/verify commands yet; no import-vault.
* Windows/non-unix builds compile but skip ownership checks and are unsupported.
* Not load-tested, not externally reviewed, not fuzzed.

## Deviations from / gaps in the design document

1. Design: rollback "copies a ciphertext reference". With version in the AAD that
   would fail authentication; rollback must decrypt and re-encrypt as a new
   version. Not implemented in M1 (not requested); the design needs this fixed.
2. Design binds `project|environment|secret_id|version`; M1 additionally binds
   the secret `name` and `dek_version` (per the M1 task). Consequence above.
3. Design Q5 (key custody default) is unanswered: both providers are implemented
   and neither is the default.
4. Design lists `dso server init|backup|restore|import-vault` and key-rotation
   commands in M1; the M1 task scope excludes them → deferred; the CLI is
   `dso central` to avoid implying a server exists.
5. Design suggests UUIDv7 ids; M1 uses random 128-bit hex ids (no dependency).
6. Design says the key file should be on a different volume; M1 checks only
   that it is not in the DB's directory.
7. Design's `current_version_id` pointer is replaced by "highest enabled version".
8. Audit HMAC key derivation (HKDF from root) is not implemented (M2).
