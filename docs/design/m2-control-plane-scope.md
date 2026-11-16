# Audit-roadmap M2 "Control plane": scope, held items and pending decisions (S0)

Status: **working scope note. NOT an ADR. NOT ratified.** Written 2026-11-16 on
`feature/m2-development`. It restates `docs/audit/54-M2-SCOPE-INVENTORY.md` (the
evidence base; section numbers below refer to it) and records what is held for
the owner. It changes no behaviour and authorizes no implementation beyond the
test-only slice S3.

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

## 6. Items HELD pending owner decisions

Recommended answers are from inventory section 13; they are recommendations only.

| # | Question | Recommended answer | Status |
|---|---|---|---|
| D1 | Does "G024 groundwork" mean the dead-code decision (G024, `10:30`) or the vault track (G027, `10:33`)? | Treat G024 literally (decide `internal/polling`, `pkg/backend`; ledger is already wired) and scope vault work as "G027-lite" by D2-D4. | OWNER DECISION PENDING |
| D2 | May `dso central` be extended (rollback, purge, project/env delete) despite ADR-002? | Yes, via a narrow new ADR for a local-only, no-identity extension; central-store identity stays frozen. | OWNER DECISION PENDING |
| D3 | Is an HTTP vault API permitted at all (ADR-010 "remote API for dso central")? | Not now. If wanted: loopback only, existing single-operator auth, with an ADR amending ADR-010. Otherwise defer the "via API" clause to roadmap M3. | OWNER DECISION PENDING |
| D4 | Is "authn/z" a single authenticated operator or RBAC? | Single role; RBAC stays deferred with G009/M3. | OWNER DECISION PENDING |
| D5 | Does M2 include agent consumption of central secrets or web UI screens? | No (not in the roadmap row; design M3/M4). | OWNER DECISION PENDING |
| D6 | Root-key custody for a deployed central store (key/DB co-location, doctor check)? | Add a doctor check and documented separation; no KMS (roadmap M5). | OWNER DECISION PENDING |

Held, therefore not started: rollback / restore-version, purge, project/env
rename or delete, central CLI completion (`--json`, exit codes), any HTTP vault
route (including backup endpoints), keyed audit chain, key-holding service,
web UI, `dso run`, service tokens.

## 7. Narrow ADR needed for local `dso central` extensions

ADR-002 says no further central milestones without a new ADR. Rollback, purge and
project/env delete extend the store and CLI even though they are local and
identity-free, so the text requires an ADR before slice S1 begins. The ADR (name
unassigned; the inventory suggested ADR-011) should state: local-only, no network,
no identity, audited via the existing chain inside `Store.WithTx`; a purge needs
its own migration because migration 0001 forbids version deletion by trigger; and
that central-store M2 identity work stays frozen. This note does not draft or
ratify that ADR.

## 8. Proposed slice order (from the inventory; not authorized except S3)

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
