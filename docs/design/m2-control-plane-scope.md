# Audit-roadmap M2 "Control plane": scope, held items and pending decisions (S0)

Status: **working scope note. NOT an ADR.** Written 2026-11-16 on
`feature/m2-development`; revised the same day to record the owner's resolution of D1-D6
(sections 6, 9, 10). Sections 1-5 and 7-8 are retained as the historical evidence base and restate
`docs/audit/54-M2-SCOPE-INVENTORY.md` (section numbers below refer to it). Where they say
"held" or "pending", sections 6, 9 and 10 supersede them. This note changes no behaviour.

> Provenance note: citations of `docs/audit/...` in this document point to local, intentionally untracked working notes (repository policy, commit 1bf9f6c). Every requirement and decision this document relies on is stated here; the citations only record where it came from.

## 1. Which "M2" this is

Three unrelated things are called M2 (inventory section 1): audit-roadmap M2
(`docs/audit/11-DEVELOPMENT-ROADMAP.md:9`, this document), central-store M2
(identity/authz/audit design, `docs/design/m2-*.md`) and central-server-design M2.
Starting audit-roadmap M2 does **not** unfreeze central-store M2.

## 2. Audit-roadmap M2 scope, as the inventory established it

Roadmap row (`11:9`): "Optional encrypted vault, projects/envs, CRUD/versioning via
API | G024 groundwork, builds on F226-F229 | Vault API with authn/z, backup/restore
tested, no plaintext at rest". Sequencing gate (`11:15`, M0 and M1 accepted) is
satisfied per `47-M1-IMPLEMENTATION-TRACKER.md:133-134`. Notes:

- "G024 groundwork" is ambiguous: G024 (`10:30`) is a dead-code decision; the vault
  track is G027 (`10:33`). See D1.
- The gate has four clauses: Vault API, authn/z, backup/restore tested, no plaintext
  at rest. Only the last two exist as code today.

## 3. Already implemented (evidence; inventory section 3)

| Capability | Where |
|---|---|
| Envelope crypto (AES-256-GCM, per-project DEK wrapped by root key; file and passphrase key providers) | `internal/crypto/envelope.go`, `keyprovider.go` |
| Projects, environments, secrets, immutable versions, disable-version (no fallback), soft delete | `internal/store/store.go`, `internal/store/sqlite/migrations/0001..0003` |
| Service layer (create/list/add-version/get/disable/delete; plaintext zeroing; redacted `Value`) | `internal/centralsvc/service.go` |
| Hash-chained audit events and `audit verify` (unkeyed by ratified R-1) | `internal/store/sqlite/audit*.go`, `internal/cli/central.go` |
| Backup (`VACUUM INTO`, verified, 0600) and restore (staged validation, dry-run, safety snapshot, displacement) | `internal/centralsvc/restore*.go`, `internal/store/sqlite/backup.go`; `docs/central-store-backup-restore.md` |
| Local CLI `dso central ...` (no network, no authn beyond OS file permissions) | `internal/cli/central.go` |

Not implemented (inventory section 5): any HTTP route for projects/envs/secrets/
versions, any role or principal model, rollback/purge/rename/delete of project or
environment, agent consumption of the store, web UI vault screens, key-rotation
commands. `/api/secrets` in `internal/server/rest.go` is the agent runtime, not the
vault.

## 4. What S3 qualifies (test-only)

S3 adds tests, no production change, for two gate clauses that already exist:

- **No plaintext at rest.** A deterministic fixture (multi-version secret, disabled
  version, soft-deleted secret, secret whose name carries a marker, second project)
  is written with synthetic markers; every file in the data directory (DB, `-wal`,
  `-shm`, safety snapshots, preserved/displaced copies, locks), every backup file,
  and captured service-logger output are scanned for value markers, the root key
  and each project DEK. Documented-cleartext metadata (names, slugs, actors) is
  asserted to be present, so the scan cannot pass vacuously.
- **Backup/restore round trip in a fresh directory:** identical list/get metadata
  and plaintext for all versions, disabled stays disabled, soft-deleted stays
  deleted, audit chain verifies, dry-run does not mutate, corrupt/truncated/
  tampered candidates and wrong or missing keys fail closed without touching live
  data, failed restore leaves live data and only documented artefacts, repeat
  restore behaviour, and modes (files 0600, key 0400, directories 0700) under
  umasks 0022/0002/0077/0000.

Files: `internal/centralsvc/atrest_qualification_test.go`,
`internal/store/sqlite/atrest_modes_test.go`. S3 does not cover hosted CI or
real-process crash qualification (`47:127-131`) and does not touch any API.

## 5. Constraints that bind this milestone

- **ADR-002** (`docs/product-evolution/architecture-decisions.md:15-20`): keep `dso
  central`, "no further central milestones (no network API, no remote callers, no
  RBAC) without a new ADR".
- **ADR-010** (`:73-75`) puts out of v1: RBAC/SSO/SCIM, approval workflows,
  multi-host coordination, Kubernetes, dynamic secrets, policy engine, hosted
  control plane and a "remote API for `dso central`". `ROADMAP.md:191` excludes a
  multi-tenancy or RBAC platform.
- Ratified R-1 (unkeyed audit chain) and R-3 (no external anchor) in
  `docs/decisions/2026-11-04-central-decision-record.md`; G009 (per-route authz)
  deferred by the owner (`47:36`); the authn/authz decision draft states
  implementation is not authorized.

**Explicit statement: central-store M2 (identity/authz/audit design), RBAC, SSO,
multi-tenancy and any remote or HTTP vault API are NOT started, and this document
does not unfreeze them.** No code for them exists on this branch.

## 6. Owner decisions D1-D6: RESOLVED

All six questions are resolved by the owner. Section 8 below is the original slice
proposal and is superseded by sections 9 and 10 where they differ.

| # | Decision | Status |
|---|---|---|
| D1 | G024 is treated literally as the dead-code/architecture-disposition item (see `docs/design/g024-disposition.md`). The limited local encrypted-vault work is a separate "G027-lite" track. | RESOLVED |
| D2 | Extending `dso central` locally (rollback, rename, safe delete, CLI completion, custody diagnostics) is approved via ADR-011 (`docs/product-evolution/architecture-decisions.md`). ADR-011 is ACCEPTED and amends ADR-002; it states ADR-010's exclusion of a remote API for `dso central` "remains unchanged and unamended", so ADR-010 is not invalidated. Permanent purge is excluded by ADR-011. | RESOLVED |
| D3 | An HTTP vault API, including a loopback-only one, is deferred to a future, separately authorized milestone. Nothing is built or approved now. | RESOLVED (deferred) |
| D4 | Single operator; the OS file-permission boundary is the access control. The central CLI is not protected by the agent REST authentication. The audit chain stays unkeyed (ratified R-1, R-3) and is not tamper-proof. | RESOLVED |
| D5 | Agent consumption of central secrets, Web UI vault screens, `dso run` and service tokens are deferred. | RESOLVED (deferred) |
| D6 | Doctor checks plus documented separation of root key and database. Separation reduces accidental co-disclosure and supports separate backup custody; it does NOT protect against full host compromise. No KMS/HSM. | RESOLVED |

## 7. Narrow ADR needed for local `dso central` extensions (historical; now ADR-011)

ADR-002 says no further central milestones without a new ADR. Rollback, purge and
project/env delete extend the store and CLI even though they are local and
identity-free, so the text requires an ADR before slice S1 begins. The ADR (name
unassigned; the inventory suggested ADR-011) should state: local-only, no network,
no identity, audited via the existing chain inside `Store.WithTx`; a purge needs
its own migration because migration 0001 forbids version deletion by trigger; and
that central-store M2 identity work stays frozen. That ADR was subsequently written and ratified as ADR-011 (ACCEPTED). It does
not authorize purge, so the purge-migration remark above remains a constraint;
purge is design-only in `docs/design/central-purge-design.md` (status: proposed).

## 8. Original slice order (historical; superseded by sections 9-10)

1. **S3** tests (this change): no owner decision needed, test-only.
2. **S0** this note.
3. S1 version lifecycle in service and store (rollback as a new version; purge,
   rename, delete only if D2 is yes). Needs the narrow ADR.
4. S2 central CLI completion over S1. `internal/cli` is under concurrent edit;
   schedule after other work lands.
5. S4 loopback vault REST API and S5 backup endpoint: only after D3 and an ADR
   amending ADR-010, and after `internal/server` edits settle.
6. Separately, S-G (dead-code decision for `internal/polling`, `pkg/backend`) on D1.

Open follow-ups noticed while qualifying (not slices): fix the F-row ambiguity
between G024 and G027 in `docs/audit/02`; hosted CI and crash qualification of
backup/restore remain uncovered.

## 9. What is implemented (verified against git log 71c15ad..f4b0453)

| Item | Commit(s) | State |
|---|---|---|
| ADR-011 ratified | `71c15ad` | accepted |
| Store: RenameProject/DeleteProject, RenameEnvironment/DeleteEnvironment | `d190a39`, `3234a10` | implemented |
| Service: immutable secret-version rollback (new version; history untouched) | `ce1b32c` | implemented |
| Service: project/environment rename and non-cascading delete | `e838d8c` | implemented |
| CLI: `central secret rollback`, historical-version retrieval tests | `3a5a887` | implemented |
| CLI: `central project|environment rename|delete`; `--json` across central lifecycle commands; exit-code contract | `668daf4`, `c61a836` | implemented |
| Doctor: root-key custody diagnostics DSO-DOCTOR-026..031 (permissions, symlinks, key/DB co-location WARN), rendered in default view | `e9080b6`, `c1ac6c8` | implemented; opt-in: skipped unless `--central-db`/`--central-key-file`/`--central-key-provider` are given |
| Tests: at-rest scan and backup/restore round trip extended to rollback/rename/delete | `f4b0453` (and S3 files in section 4) | implemented |

Not implemented: permanent purge, any HTTP vault route, network authn/z, RBAC, agent consumption, Web UI vault
screens, `dso run`, service tokens, key rotation commands, keyed audit chain.

## 10. Revised M2 acceptance and gate statement

| Criterion | Class | State |
|---|---|---|
| Encrypted persistence (per-project DEK wrapped by a root key held outside the DB) | required | implemented |
| Projects and environments (create, list, rename, non-cascading delete) | required | implemented |
| Secret CRUD with immutable versions | required | implemented |
| Rollback (as a new version) | required | implemented |
| Local CLI (`dso central`, `--json`, exit codes) | required | implemented |
| OS permission boundary (0400/0600/0700, securefs checks) as the access control | required | implemented, no application-level authn |
| Backup/restore qualification | required | implemented at store, service and CLI test level; hosted CI and real-process crash qualification not covered |
| No plaintext secret values in INSPECTED persistence artifacts | required | implemented, see wording below |
| Root-key custody diagnostics | required | implemented (doctor, opt-in flags) |
| Audit correctness (chain verifies across new event types) | required | implemented, chain unkeyed |
| Vault HTTP API | deferred | future roadmap item, own authorization |
| Network authn/z | deferred | future roadmap item |
| Remote disclosure of values | deferred | future roadmap item |
| RBAC | deferred | out of scope (ADR-010) |
| Permanent purge | deferred | design only: `docs/design/central-purge-design.md` |

**The ORIGINAL audit-roadmap M2 gate ("Vault API with authn/z, backup/restore tested, no plaintext at rest",
`docs/audit/11-DEVELOPMENT-ROADMAP.md:9`) is NOT claimed as passed.** Its "Vault API" and "authn/z" clauses are
not implemented. The API is tracked as a future roadmap item that requires its own authorization (and an ADR
amending ADR-010). The revised table above is the gate this milestone is measured against; passing it is not
equivalent to passing the original gate.

Security claim wording (use exactly this scope, not a general claim): tested persistence paths show no plaintext
secret values and no unwrapped key material in the inspected artifacts (database, WAL/SHM, backups, safety and
displaced copies, locks, and captured service logger output). Names, slugs and actors are cleartext metadata.
Nothing here claims protection against a compromised host, a process reading memory, a holder of both the
database and the root key, or tampering with an unkeyed audit chain.
