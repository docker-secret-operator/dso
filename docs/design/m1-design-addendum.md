# Central store — M1 design addendum (corrections and decisions needed before M2)

Date: 2026-10-16 · Status: **proposal for approval** (nothing here is implemented
beyond what `docs/central-store-m1.md` describes).

The original specification, `docs/design/central-server-design.md`, is an
**untracked file in the repository owner's working tree** and was deliberately
not modified (that checkout must stay untouched). This addendum records where
that document is wrong or silent, so that it can be corrected when it is
committed. Where the two disagree, this addendum describes the intended
behaviour.

## 1. Rollback must re-encrypt (corrects "rollback copies a ciphertext reference")

The design says rollback creates a new version "copying an old version's
ciphertext reference". Because each version's AAD includes its **version
number** (and secret id), a ciphertext copied into a new version row fails
authentication. Rollback therefore is:

1. read the old version (`GetValue(…, n)`), which authenticates it;
2. create a **new** version with that plaintext (`AddVersion`), sealed under the
   new version's own AAD, with `comment = "rollback to vN"`;
3. the old version is untouched (immutable).

This also means a rollback is only possible while the target version is still
enabled and decryptable. Cost: the plaintext is briefly in process memory;
benefit: no ciphertext is ever valid in two places.

## 2. Renaming a secret requires re-encryption

The AAD binds `project | environment | secret id | **name** | version | dek
version`. The name was added (the secret id was already in the design) so a
ciphertext is bound to what an operator sees. Consequence: **a rename cannot be a
metadata-only update**; it must re-encrypt every version under the new name
(or the AAD must drop the name). Recommendation: keep the name in the AAD and
implement rename as "create new secret, copy versions by decrypt/re-encrypt in
one transaction, soft-delete the old name"; do not offer an in-place rename until
that exists. Moving a secret between environments or projects has the same
property.

## 3. Latest-version semantics (decided in the remediation)

"Latest" = the newest version number. If it is disabled, reads of "latest" fail
(`ErrLatestVersionDisabled`) — there is no fallback to an older enabled version.
The design's `current_version_id` pointer is **not** reintroduced: with rollback
implemented as "new version" (§1) the newest version is always the intended
current one, which removes a mutable pointer and a class of inconsistency.

## 4. The audit key cannot be "HKDF from the root key" for KMS providers

The design derives the audit HMAC key from the root key with HKDF. That requires
the root key bytes. The `KeyProvider` contract (`ID/Wrap/Unwrap`) exists so that
a KMS/Vault-transit provider — whose root key is **non-exportable** — can be
added without changing storage logic; such a provider cannot give DSO its key
bytes, so HKDF cannot be performed. Options:

| Option | How | Pros | Cons |
|---|---|---|---|
| **A. Wrapped random audit key (recommended)** | At init generate a random 256-bit audit key, store it in `store_meta` wrapped by the root key (same `Wrap`/`Unwrap` calls as DEKs, AAD `dso/audit-key/v1\|key-id`). The service unwraps it at start and keeps it in memory | Works unchanged for file, passphrase and every KMS; independent of root-key bytes; can be rotated by re-wrapping or issuing a new audit-key generation; one extra row | A DB copy + root key yields the audit key (same as DEKs); losing the wrapped row makes the existing chain unverifiable, so it belongs in backups; random not derivable (cannot be recomputed from the root key alone) |
| B. Provider `Derive` method | Add `Derive(ctx, label) ([]byte, error)` to `KeyProvider`; local providers HKDF their KEK, KMS providers use `GenerateMac`/transit HMAC | Deterministic, no extra stored secret | Not all providers can do it (semantics differ between AWS KMS HMAC keys, Vault transit HMAC, Azure Key Vault); forces every future provider to implement a MAC primitive; a provider swap changes the audit key and breaks chain verification |
| C. Separate audit key outside the root hierarchy | Operator-supplied audit key file | Separation of duties | Another secret to manage and back up; weakens "one root of trust" |

**Recommendation: A**, decided at the start of M2, with the binding canary
(`binding.*` in `store_meta`) as the model: wrapped, authenticated by the
provider, stored insert-only. M1 does not store an audit key.

## 5. Other corrections to the design document

* **Different directory ≠ different volume.** "On a different path/volume than
  the DB" can only be enforced as "different directory" (checked, symlinks
  resolved) plus safe-directory rules; volume separation is an operator duty.
* **Database-to-key binding** (new, required): a canary in `store_meta` ties the
  database to its root key; without it a second key can create projects in an
  initialised database. Specified in `docs/central-store-m1.md`.
* **Directory trust** (new): key/database directories must not be writable by
  group/others (see `internal/securefs`).
* **SQLite WAL is not supported on NFS/SMB** and the database must be on local
  storage — absent from the design's "SQLite vs PostgreSQL" table.
* **Platform statement:** cross-compilation success is not runtime support;
  only Linux is tested and the central store fails closed on non-unix platforms.
* **Backup:** every migration attempt produces a new verified snapshot; the
  design's single "automatic backup" is insufficient when attempts are retried.
* Items from the design's M1 list that are **deliberately deferred**:
  `dso server backup|restore|import-vault`, root-key and DEK rotation, purge.
  Root-key backup/escrow remains **required before any real data is stored**.
