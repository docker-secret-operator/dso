
# `dso central` Phase 2 Step 5.3 — Audit-Chain Design (design only, no implementation)

**Date:** 2026-10-25
**Status:** Design document only. No Go source, test, schema, migration, CI workflow, dependency, or configuration file was modified to produce this document. No hash chain, audit table, or any other code was implemented.
**Builds on, and does not reopen:** `docs/reviews/2026-10-24-central-store-phase2-threat-model.md` (Step 5.1), `docs/reviews/2026-10-24-central-store-phase2-authn-authz-design.md` (Step 5.2 + §15 addendum), `docs/central-store-m1.md`, `docs/reviews/2026-10-24-phase0-phase1-security-findings-register.md`. All seven frozen Step 5.2 decisions listed in the task are treated as fixed inputs throughout (see §3).

---

## 1. Purpose and scope

Design — on paper only — a tamper-evident audit mechanism for `dso central` operations: what gets recorded, in what shape, with what integrity guarantee, under what transaction/failure semantics, readable by whom, and retained/backed-up how. This document does not create a table, write a line of Go, or decide anything Step 5.2 already froze. Where this design depends on an unresolved Step 5.2 question, it says so and stops rather than deciding it here.

## 2. Current implementation findings (verified from source)

### 2.1 Operation surface (the complete set of things an audit event could record)

Confirmed directly from `internal/centralsvc/service.go`'s exported methods (`grep -n "^func (s \*Service)"`, re-run this session):

| Method | Line | Nature |
|---|---|---|
| `CreateProject(ctx, slug, name)` | 88 | Write |
| `CreateEnvironment(ctx, projectSlug, slug, name)` | 129 | Write |
| `CreateSecret(ctx, projectSlug, envSlug, name, value, actor, comment)` | 196 | Write (includes encryption) |
| `AddVersion(ctx, projectSlug, envSlug, name, value, actor, comment)` | 239 | Write (includes encryption) |
| `GetValue(ctx, projectSlug, envSlug, name, version)` | 297 | **Read — plaintext reveal** (this is the CLI's `secret get --reveal` path) |
| `ListSecrets(ctx, projectSlug, envSlug)` | 335 | Read (metadata only) |
| `ListVersions(ctx, projectSlug, envSlug, name)` | 347 | Read (metadata only) |
| `DisableVersion(ctx, projectSlug, envSlug, name, version, actor)` | 358 | Write (destructive-adjacent) |
| `DeleteSecret(ctx, projectSlug, envSlug, name, actor)` | 375 | Write (destructive, soft-delete) |
| `ListProjects(ctx)` | 391 | Read |
| `ListEnvironments(ctx, projectSlug)` | 402 | Read |

`resolve`, `unwrapDEK`, and `lookup` (lines 156, 169, 271) are private helpers, not independently callable operations — they do not need their own audit-event type, but their *failure* (e.g., `unwrapDEK` failing) is a key-provider failure that surfaces through whichever public method called them (§4's event matrix accounts for this as a failure outcome of the calling operation, not a separate event type).

`dso central init` (bootstrap: key install + database creation + binding) is **not** a `Service` method — per `docs/central-store-m1.md:212-220`, it runs before any `Service` exists, directly against `internal/crypto`/`internal/securefs`/`internal/store`. This document treats `init` as a distinct lifecycle event category (§4), separate from `Service`'s per-request operations, because it has no transaction to share and happens exactly once per store.

### 2.2 Persistence and transaction model (directly relevant to §7)

Confirmed from `internal/store/sqlite/tx.go` (full file read this session) and its own doc comments:
- **15 operations are plain autocommit** — single-statement calls directly against `*sql.DB`, with no `BEGIN`: `CreateProject`, `GetProjectBySlug`, `GetProject`, `ListProjects`, `CreateEnvironment`, `GetEnvironmentBySlug`, `ListEnvironments`, `GetSecret`, `ListSecrets`, `DeleteSecret`, `GetVersion`, `GetLatestVersion`, `ListVersions`, `GetMeta`, `ListMeta` (tx.go, "the 15 autocommit operations" section).
- **4 operations run inside a single `IMMEDIATE` SQLite transaction** via `Store.WithTx`: `CreateSecret`, `AddVersion`, `DisableVersion`, `PutMetaIfAbsent` (tx.go, "the 4 operations that were transactional in M1" section).
- `WithTx`'s lifecycle (tx.go, `Store.WithTx` doc comment): commits on `fn` returning `nil`; rolls back on an error, a cancelled context, or a panic (which propagates, not swallowed); the store remains usable afterward.
- **This asymmetry is the single most important fact for this document's §7**: an audit event for `CreateSecret`/`AddVersion`/`DisableVersion` *can* be written inside the same existing transaction with zero new transaction-management code. An audit event for `DeleteSecret` (currently autocommit, single statement) **cannot** be made atomic with the business write without first changing `DeleteSecret` to go through `WithTx` — a change to `internal/store`, out of this document's design-only scope, and flagged in §7/§12 as a dependency a future implementation phase must resolve, not something this document decides.

### 2.3 Existing schema conventions this design should match, not reinvent

Confirmed from `internal/store/sqlite/migrations/0001_init.sql` and `0002_store_meta.sql` (grep'd for table/trigger/column names this session):
- `secrets.created_by` (line 29) and `secret_versions.created_by`/`disabled_by` (lines 45, 48) are plain `TEXT NOT NULL`/`TEXT` columns — **caller-supplied, not foreign-keyed to any identity table, not cryptographically bound to anything**. This is the exact gap `docs/central-store-m1.md:225` already names ("not an audit log and not tamper-evident") and that Step 5.1/5.2 both carry forward. Confirmed unchanged.
- `secret_versions_immutable` (line 55) and `secret_versions_no_delete` (line 65) triggers, and `store_meta_no_update`/`store_meta_no_delete` (0002_store_meta.sql:16,21), establish an existing, working pattern: **insert-only tables enforced by SQLite triggers**, with an explicit, narrow exception (the one-way `NULL → set` of `disabled_at`/`disabled_by`, per `secret_versions_immutable`'s own `OR` clause and `docs/central-store-m1.md:159`). This document's recommended audit table (§6) should use the **same pattern** (a trigger-enforced insert-only table), not a new mechanism, for consistency and because it is already a proven, tested convention in this codebase.
- **No audit or history table exists today.** The only hit for "audit" in the migrations is a comment (`0001_init.sql:54`, "a later, explicit, audited operation with its own migration") — i.e., the schema's own authors already anticipated this exact step.

### 2.4 Key hierarchy and existing integrity mechanisms available to reuse

Carried forward from `docs/central-store-m1.md:106-145` (already verified in Step 5.1/5.2, re-cited here for §6): AES-256-GCM with AAD binding project/environment/secret/version/DEK-version/root-key-id identity into every secret-version's ciphertext; a root key → per-project DEK → per-secret-version hierarchy; GCM's own authentication tag already gives DSO a working, tested pattern for "detect tampering of a specific row," which §6 evaluates reusing (via a keyed MAC, not necessarily full AEAD) for audit events rather than inventing a new primitive.

### 2.5 Rollback-detection gap (already known, re-confirmed unchanged)

`docs/central-store-m1.md:61,227`: "Whole-database rollback to an older valid copy is not detected." Step 5.1 §6/§8 flagged this as directly relevant to the audit-chain design. This document's §6 addresses it explicitly, per the task's own requirement not to claim a hash chain solves this without a credible external anchor.

---

## 3. Threat model and security assumptions (inherited, not reopened)

This document inherits Step 5.1's threat model and Step 5.2's seven frozen decisions in full. Restated only where directly load-bearing for the audit-chain design:

- **No authn/authz exists for `dso central` today**; Option B (local Unix socket + peer credentials) is the *approved architecture*, not yet implemented, with its same-UID limitation explicitly acknowledged (Step 5.2 §2, §11; addendum §15 item 1).
- **`created_by`/`disabled_by` remain caller-supplied attribution strings, never authenticated identity** — this document does not change that; §6's event schema explicitly separates a verified-identity field from a caller-supplied label field (§6.2).
- **Restricting `get --reveal`, `disable-version`, and `delete` to "caller #1" is a policy intention only, not enforceable under Option B alone** (Step 5.2 §15 items 3-4) — this document's event matrix (§4) and access-control section (§8) do not claim the audit chain itself can enforce or verify that restriction; an audit event can only ever record *that* an operation of a given type happened and *what OS identity the socket observed*, never *which policy-defined "caller class" (#1 vs #2) performed it*, since Option B cannot distinguish those (confirmed, not reopened).
- **No network listener, RBAC, SSO, hosted control plane, or new application-credential system is in scope** (per the task's explicit exclusions, consistent with ROADMAP.md's Group D exclusions already applied in Steps 5.1/5.2).
- **Root compromise, root-key loss, and the rollback-detection gap (§2.5) remain explicitly out of scope to fully solve** — carried forward, not reopened, addressed only to the extent §6 can honestly state what a chain-based design does and does not do about rollback.

---

## 4. Audit event coverage matrix

| Operation | Event required? | Success case recorded | Failure case recorded | Rationale |
|---|---|---|---|---|
| `dso central init` (key install + DB create + bind) | **Yes, once** | Store initialized: key-provider type, root-key-id (not the key itself), schema version | Init failure (directory-safety rejection, existing-key conflict, etc.) | This is the store's own origin event — without it, every later event's chain has no documented starting point. Recorded outside the normal per-`Service`-call flow since no `Service` exists yet at this point (§2.1) |
| `CreateProject` / `CreateEnvironment` | Yes | Project/environment slug + generated ID | Validation error, uniqueness-constraint violation, DB error | Low-frequency, structurally significant; no reason to omit |
| `CreateSecret` / `AddVersion` | Yes | Secret/version identifier, project/env, actor (both verified-identity and caller-supplied label, §6.2) — **never the value** | Validation error, encryption/`unwrapDEK` failure (key-provider failure — see §2.1), DB error | The core "something changed" event this system exists to produce |
| `GetValue` (`--reveal`) | **Yes — every call, success or failure, not sampled** | Secret/version identifier, project/env, actor | "Not found," "latest version disabled" (`ErrLatestVersionDisabled`), decryption/integrity failure (`ErrIntegrity`) | This is the single highest-value event in the whole system (plaintext was disclosed) — per Step 5.2 §7, it is the operation this design most needs strong evidence for, even though the *caller class* restriction around it is unenforceable (§3) |
| `ListSecrets` / `ListVersions` / `ListProjects` / `ListEnvironments` | **No per-call event; see note below** | — | — | These return metadata only, already disclosed as stored-in-clear (Step 5.1 §8); logging every list call at the same fidelity as a plaintext reveal would flood the chain with low-value events and risks exactly the "misleading duplicates" the task warns against. **Note**: if access-pattern analysis is later judged valuable (§8 flags this as a privacy/utility tradeoff), a *separate*, explicitly lower-fidelity, possibly-sampled mechanism could be designed — not proposed here, since it has no verified requirement yet |
| `DisableVersion` | Yes | Secret/version identifier, project/env, actor | Not-found, already-disabled, DB error | Destructive-adjacent; policy intends this restricted to caller #1 (unenforceable per §3, but still worth recording precisely who/what the socket observed) |
| `DeleteSecret` | Yes | Secret identifier, project/env, actor | Not-found, DB error | Same rationale as `DisableVersion`, and the one write operation that currently cannot share a transaction with its audit event without a store-level change (§2.2, §7) |
| Authentication failure (future Option B socket) | Yes | — (there is no success case for this event type) | Connection from an unresolvable/unauthenticated peer (§6's auth-failure vs. authz-denial distinction, carried from Step 5.2 §6) | Security-relevant by definition; must not be conflated with authorization denials (next row) |
| Authorization denial (future Option B socket) | Yes | — | A resolved peer attempting an operation the running policy denies | Same rationale; kept as a distinct event *type* from authentication failure, not just a different outcome field, so the two can be queried/alerted on independently |
| Key-provider failure (not tied to one request, e.g. provider itself becomes unreachable) | **Design proposal, not yet implementable** — no such standalone failure mode exists in the verified code today; `unwrapDEK` failures (§2.1) already surface as a failure outcome of whichever operation called it | — | — | Flagged as a future event *type* to reserve in the schema's `event_type` enum (§6.1) rather than designed in detail now, since no code path currently produces this independently of a request |
| Audit-subsystem failure itself (the audit write fails) | **Yes, but not as a row in the same table it's reporting on** — see §7's explicit handling | — | — | A table cannot reliably record its own write failure inside itself; this must surface through the existing logging conventions (`internal/audit/audit.go`'s pattern — identifiers only, never values, per the earlier complete-product audit's F-05 finding) and/or a process-level error returned to the caller, not a best-effort self-referential row |

**Explicit non-duplication rule**: a single logical operation produces **exactly one** event, whose `outcome` field (§6.1) is success or a specific failure category — not one event for "the request arrived" and a separate one for "the request succeeded/failed." This directly answers the task's "define event boundaries that avoid misleading duplicates" requirement.

---

## 5. Proposed event schema (design proposal, not implemented)

### 5.1 Fields

| Field | Type | Notes |
|---|---|---|
| `event_id` | opaque identifier (e.g. a ULID or the row's own `rowid`) | Primary key; monotonic-ish ordering is useful but not itself a security property |
| `schema_version` | integer | So a later redesign of this table can be migrated the same way `schema_migrations` already versions the rest of the database (`docs/central-store-m1.md:150`) |
| `event_type` | enum/string | One of: `init`, `project_create`, `environment_create`, `secret_create`, `secret_add_version`, `secret_get_reveal`, `secret_disable_version`, `secret_delete`, `auth_failure`, `authz_denial` (reserving `key_provider_failure` and `audit_persistence_failure` per §4's last two rows, for future use) |
| `outcome` | enum | `success` \| a specific failure category (e.g. `not_found`, `validation_error`, `integrity_error`, `db_error`) — never a free-text message that could leak detail inappropriately |
| `timestamp` | integer (UTC unix seconds, matching the existing convention per `docs/central-store-m1.md:151`) | **Trust limitation, stated explicitly per the task's requirement**: this is wall-clock time from the host DSO runs on; it is not a trusted timestamp authority. An attacker with the privilege already assumed in §3 (compromise of the OS account) can set the system clock before performing an action. This field is useful for ordinary operational review, not as cryptographic proof of *when* something happened — only the event's position in the chain (§6) is tamper-evident, not its wall-clock label |
| `verified_identity` | structured (e.g. `{uid, gid, pid}` once Option B exists) or `null` | **Only ever populated from a source DSO itself authenticated** — today, that's nothing (no socket exists yet), so this field is `null` for every event until Option B ships. This is the field explicitly required to distinguish from the next one |
| `actor_label` | free text, nullable | The existing `created_by`/`disabled_by`-style caller-supplied string, carried through unchanged — **never treated as authenticated**, per §3/Step 5.2's explicit requirement. Kept for operational continuity (it's what operators already see today) but never conflated with `verified_identity` in any query or report this design proposes |
| `operation_target` | structured (project slug, environment slug, secret name, version number — whichever apply) | The resource identifiers already used elsewhere in the schema; no new identifier scheme invented |
| `correlation_id` | opaque identifier, nullable | **Only if one can be generated safely without becoming its own information leak** — e.g., a random value generated at the start of a single CLI invocation or socket request, used to correlate multiple events from one logical operation (relevant mainly once a multi-step operation, like a backup/restore, exists). Proposed as nullable/optional rather than mandatory, since not every event type in §4 needs one |
| `reason_category` | enum, nullable | A closed set of failure categories (same set as `outcome`'s failure values, or a narrower drill-down) — explicitly **not** a raw error string, to avoid leaking implementation detail or (worse) fragments of a value in a poorly-constructed error message |
| `prev_hash` | fixed-length binary | The previous event's `event_hash` (chain linkage — see §6) |
| `event_hash` | fixed-length binary | Computed over this row's own fields plus `prev_hash` (chain linkage — see §6) |
| `mac` | fixed-length binary, **proposed, see §6** | Optional keyed-MAC field, only if §6's recommended integrity mechanism includes one |

### 5.2 What must never appear in any event (explicit, per task requirement)

Secret values, decrypted plaintext, encryption keys (root key, DEKs, nonces are fine as non-secret metadata but the key material itself is not), passphrases, any future application-level credential (§3), or raw error strings that could echo attacker-influenced or secret-adjacent content. This matches the existing, already-audited convention in `internal/audit/audit.go` and `internal/injector/docker.go` (identifiers/names only, confirmed in the earlier complete-product audit's F-05 finding) — this design does not invent a new logging discipline, it extends the existing one to a new table.

### 5.3 Do resource names/metadata themselves need protection?

**Assessed, not resolved here — flagged as an unresolved question (§12).** Per Step 5.1 §8, secret names and project/environment slugs are *already* stored in clear in the primary schema (`docs/central-store-m1.md:64`) — so an audit event repeating a secret's name is not a *new* confidentiality exposure relative to the primary database, it is consistent with the existing, disclosed design. However, the audit table's *access pattern* (who looked at what, how often) is new information the primary schema doesn't carry. §8 addresses this as a privacy/utility tradeoff for audit *readers*, not for the schema fields themselves.

---

## 6. Integrity mechanism comparison

### 6.1 Options evaluated

| | **(a) Append-only, no cryptographic chain** | **(b) Hash chain (`event_hash = H(prev_hash \|\| fields)`)** | **(c) Hash chain + keyed MAC/signature** |
|---|---|---|---|
| **Tamper evidence** | None beyond the existing insert-only trigger pattern (§2.3) — an attacker with raw DB write access (already an assumed-possible threat per §3/Step 5.1 §4) can edit a row and nothing detects it, since there's no cryptographic linkage between rows | Detects **modification of any single row** (its hash won't match its recomputed content) and **detects reordering/insertion** between two existing rows (the chain breaks) — but **does not by itself prove the newest event hasn't been deleted**, and does not detect a **whole-database rollback** to an earlier, internally-consistent, validly-hashed state (explicitly required to be stated per the task) | Same detection properties as (b), plus: a MAC/signature computed with a key **not stored in the same database** means an attacker who has only database-file access (without that external key) cannot even *recompute* a valid replacement chain after tampering — this is strictly stronger against an attacker who has database access but not the signing key, though it does nothing additional against an attacker who has *both* (already an accepted-out-of-scope threat, §3) |
| **Key management/rotation** | N/A | N/A (no key) | Requires a key — reusing the existing root-key/DEK hierarchy (§2.4) is tempting but **conflates "key that protects audit integrity" with "key that protects secret confidentiality"**; this document recommends a **separate** key purpose (even if sourced from the same `KeyProvider` abstraction) specifically so that audit-key compromise and secret-key compromise remain independent failure domains — a genuine new design decision, flagged in §12 for approval since it has operational cost (one more key to manage, back up, and potentially rotate) |
| **Crash consistency / partial writes** | Whatever SQLite's own transaction model already gives (§2.2) | Same — the hash is computed before the row is written in the same transaction as the business operation where that's possible (§7); a crash mid-write is rolled back by SQLite exactly as any other transaction would be, so there's no "half-written hash" state to worry about *within* one transaction | Same, plus the MAC computation must also complete before commit — no new crash-consistency risk beyond "one more field to compute before the same commit point" |
| **Concurrent writers / ordering** | SQLite serializes writers already (§2.2, `docs/central-store-m1.md:187-190`) | **The chain's linear order must match SQLite's actual commit order**, which requires computing `prev_hash` from the *true* latest row at commit time, not at request-start time — under SQLite's existing writer serialization this is achievable (the insert and the "read the current latest hash" must happen in the same transaction), but it is a real implementation constraint this document flags rather than resolves (§12) | Same constraint, no additional concurrency complexity from the MAC itself |
| **Schema migration compatibility** | Trivial — it's just a table | `schema_version` field (§5.1) lets a future redesign add fields without invalidating old rows' hashes, as long as the hash computation itself is versioned alongside | Same, plus the MAC algorithm/key-id should also be versioned if it's ever rotated |
| **Backup/restore** | Backed up exactly like any other table, with the same `.bak` mechanism already used for the rest of the database (`docs/central-store-m1.md:176-186`) — no special handling | Same mechanical backup, **but restoring an older `.bak` is itself indistinguishable from the attack it's meant to help detect** unless the restore is itself an *audited, deliberate operator action* recorded as its own event (in a separate location — see below) and/or cross-checked against an external anchor (next row) | Same, plus: if the MAC key is rotated, a restored backup's MAC must be verifiable against the key version active *at backup time*, requiring the key-id to be recorded per-event (already captured if key-id versioning per §12 is adopted) |
| **Truncation/deletion/rollback attacks** | Not addressed at all | **Explicitly only partially addressed, as the task requires stating plainly**: a hash chain stored *only* inside the same SQLite file it's protecting can be rolled back along with that file — an attacker who restores an older, internally-valid snapshot produces a chain that is 100% self-consistent and passes every integrity check, while silently discarding every event after the restore point. **This is the exact limitation `docs/central-store-m1.md:61,227` already discloses for the database as a whole, inherited unchanged by any in-database chain.** Closing this requires an **external trust anchor** — e.g., periodically exporting the current chain's tip hash to a location outside the rollback domain (a separate file with its own backup policy, a remote log, or simply an operator-run `dso central audit verify` command whose *output* the operator is expected to compare against a previously-recorded value kept elsewhere). **No such external anchor is proposed in detail here** — flagged as an unresolved, approval-gated decision (§12), since it has real operational cost (yet another thing to configure/trust) that should not be decided unilaterally by this document | Same limitation — a MAC/signature does not change this; an attacker with full database-file access who restores an old snapshot restores a chain that was validly MAC'd/signed *at the time*, and nothing in (c) alone detects that the *current* chain is stale relative to a now-missing tip |
| **Operational complexity / testing burden** | Lowest | Low — hashing is cheap, and `internal/agent/peercred_test.go`-style synthetic unit tests (feed known rows, assert known hashes, assert a tampered row's hash mismatches) are straightforward to write once implemented | Moderate — adds key provisioning/rotation to design and test, on top of (b)'s burden |

### 6.2 Recommendation

**Adopt (b), hash chaining, for the current product stage — not (c).** Rationale: (c)'s additional protection (resisting an attacker with database-file access but *not* the external MAC key) is real, but DSO's own stated threat model already treats "attacker with full OS-account/root compromise" as explicitly out of scope (§3) — and that is precisely the attacker (c) would need to be *not* fully compromised to gain anything over (b). Introducing a second, audit-purpose key before there is a concrete operator need for that specific threat boundary adds real key-management burden (§6.1's "Key management" row) for a security property that mostly matters against a threat class this product has already, elsewhere, decided not to defend against at this stage. **This recommendation does not resolve the rollback/external-anchor question** (§6.1's "Truncation/deletion/rollback" row) — that remains open regardless of (b) vs. (c), and is listed in §12 as its own approval gate, independent of which chaining mechanism is chosen.

**Explicit, required statement, per the task**: *a hash chain, by itself, under either (b) or (c), does not prove that the latest event has not been deleted, and does not prove the database has not been rolled back to an earlier, internally-consistent state. Closing that gap requires a trust anchor outside the database file, which is not designed in this document.*

---

## 7. Transaction and failure semantics

### 7.1 What can be made atomic today, without any store-level change

For `CreateSecret`, `AddVersion`, and `DisableVersion` — the three write operations that already run inside `Store.WithTx` (§2.2) — **an audit-event insert can be added as one more statement inside that same existing transaction's callback**. This gives true atomicity (the business row and its audit event commit or roll back together) with no new transaction-management code, only a new statement inside an existing one. This is this document's recommended default for these three operations.

### 7.2 What cannot be made atomic without a store-level change

`CreateProject`, `CreateEnvironment`, `DeleteSecret`, and all 11 other currently-autocommit operations (§2.2) do **not** currently run inside any transaction at all. Adding an atomic audit event for these requires first changing them to go through `WithTx` (or an equivalent) — **a change to `internal/store`'s existing method signatures/behavior, which this document does not propose implementing**, since Step 5.3's scope is design, and this specific change has its own blast radius (every existing caller and test of those 15 methods) that deserves its own review rather than being folded silently into the audit-chain design. **Flagged in §12 as a required approval-gated decision**: either (a) accept non-atomic audit recording for these operations (the business write can succeed while the audit write fails, or vice versa — see §7.3) as an initial, disclosed limitation, or (b) approve extending `WithTx` coverage to all write operations as a prerequisite, which is more invasive than this step's stated scope.

### 7.3 Explicit failure-case semantics (per the task's required list)

| Scenario | Proposed behavior | Rationale |
|---|---|---|
| Business operation succeeds, audit persistence fails | **For the three `WithTx`-covered operations (§7.1): impossible by construction** — they're the same transaction, so either both commit or both roll back. **For the twelve autocommit operations (§7.2), until/unless that gap is closed**: the business operation's result must not be withheld from the caller merely because the audit write failed (availability-over-audit-completeness for these operations specifically) — but the audit *write failure itself* must be surfaced through the existing logging convention (§4's last row) at a severity that is operationally visible, not silently dropped. This is an explicit availability-vs-audit-completeness trade-off, and it is **asymmetric** across the two operation classes — flagged for product-owner awareness (§12), not hidden |
| Audit data committed, business operation fails | Not applicable for the `WithTx`-covered three (same transaction, both-or-neither). For the autocommit twelve, this direction of inconsistency is *less* likely in practice if the audit write is attempted only after the business write already succeeded (a sequencing choice this document recommends: write business state first, then audit, for the autocommit operations — minimizing the chance of a committed audit event describing something that didn't actually happen) |
| Process crashes between related steps | For `WithTx`-covered operations: SQLite's own crash recovery (WAL replay) handles this exactly as it already does for any other transaction (§2.2) — no new behavior. For autocommit operations with a separate audit write: a crash between the two leaves exactly the asymmetric gap described above; no reconciliation mechanism is proposed in this document (flagged, §12) |
| Database full, corrupt, or locked | Should fail the triggering operation's audit write the same way any other write to this database would fail under those conditions — this document does not propose a different error-handling path for audit writes than the rest of `internal/store` already has, to avoid inventing a second failure-handling convention |
| Integrity check detects a broken chain | **This is a detection event, not a write-path failure** — it should be surfaced via an explicit, operator-invoked verification command (not designed in Go here, consistent with scope) and should **not** silently auto-repair or re-derive a "corrected" chain, since doing so would itself be indistinguishable from an attacker's tampering-cover-up. The correct response to a detected break is "stop and alert the operator," not "fix it automatically" |

**No atomicity is promised across SQLite and the key provider** (per the task's explicit caution): `unwrapDEK`'s failure (§2.1) is already a failure of the *business* operation itself (it happens before any write), not a separate audit-reconciliation problem — the operation simply fails, and that failure is recorded as a failure-outcome event (§4), not as a success-then-rollback.

---

## 8. Access control and audit privacy

- **Who can append events**: only `internal/centralsvc.Service` itself, as an internal side effect of its own method calls — never a caller-supplied event. This is a direct consequence of §2.1/§2.2's finding that `Service` is "the only place plaintext exists" (`docs/central-store-m1.md:23`) and should, by the same logic, be the only place audit events are minted.
- **Who can read audit history**: under the current, unimplemented state, the same boundary as everything else in `dso central` — OS-level file access to the database (§3). Once Option B (the approved socket architecture) exists, reading audit history should be gated by the same per-operation authorization model Step 5.2 designed, with a new `audit_read` entry added to that matrix at implementation time — **not designed in detail here**, since Step 5.2's matrix is frozen and this document does not reopen it, only notes that it will need one more row eventually.
- **Who can verify chain integrity**: should not require write access to the database — a verification operation only needs to read and recompute hashes. This document recommends it be available to the same identity class as audit-history reads, not restricted further, since verification without the ability to act on a detected problem is of limited use.
- **Export/retention operations**: **not designed here** — proposed future operations only (§9), pending the external-anchor decision in §6.
- **Does the audit chain solve the same-UID identity problem?** **No, explicitly** — restated per the task's requirement: the audit chain can record *that* the socket observed a given UID/PID performing an operation (via `verified_identity`, §5.1), but it cannot retroactively distinguish which *process* among several sharing that UID actually did it, any more than Option B itself can (§3). The audit chain is a *record* of what the authorization layer observed, not a *stronger* authorization layer itself.
- **Could audit readers infer sensitive structure?** Yes, partially, and this is a genuine privacy/utility tradeoff, not a solved problem: a complete audit history of `secret_create`/`add_version`/`get_reveal` events reveals *access patterns* (which secrets are read how often, by roughly what cadence) even though it never reveals *values*. This is new information beyond what the primary schema already discloses (§5.3) — this document flags it as worth product-owner awareness but does not propose restricting event content further, since doing so would reduce the audit trail's actual security value (§4's whole point is to have strong evidence for exactly these operations).

---

## 9. Retention, backup, restore, and migration

- **Retention/growth**: audit events are append-only and never pruned by default in this proposal — pruning a tamper-evident chain necessarily breaks verifiability for the pruned range (the next remaining event's `prev_hash` would point at a gone row), so this document does not propose a pruning mechanism. If storage growth becomes a real operational problem, the correct future design is an **archival export** (move old events to a separate, independently-verifiable file, verified before the live table is trimmed) rather than silent deletion — not designed in detail here, flagged in §12.
- **Backup**: the audit table is backed up by the exact same mechanism already protecting the rest of the database (`docs/central-store-m1.md:176-186`'s pre-migration `.bak` snapshots, and whatever Step 5.4 eventually designs for on-demand backup) — no new backup mechanism is proposed, consistent with reusing existing conventions (§2.3).
- **Restore**: restoring a backup restores the audit chain exactly as it was *at backup time* — this is correct and expected, but per §6.1's rollback-attack row, a restore is **operationally indistinguishable from an attack** unless it is itself a deliberate, recorded operator action. This document recommends (not implements) that any future `dso central restore` command (Step 5.4) **itself emit an audit event marking the restore**, recorded wherever the external anchor (§6.1, if adopted) lives — otherwise a legitimate restore and a malicious rollback leave identical evidence.
- **Schema upgrades**: follow the existing forward-only, embedded-migration convention already used for the rest of the schema (`docs/central-store-m1.md:173-175`) — `schema_version` (§5.1) exists specifically so a future migration can be written against it the same way.
- **Integrity verification after restore**: should be run automatically (or at minimum strongly recommended to the operator) immediately after any restore, before the store is used — verifying the chain is internally consistent is necessary but, per §6.1, **not sufficient** to detect that the restore itself discarded events; that requires the external-anchor comparison (§6.1, §12), not just internal verification.
- **Recovery from damaged audit data**: if the audit table itself is corrupted (but the rest of the database is intact), this document recommends failing closed on audit-dependent operations rather than silently proceeding without an audit trail — but does not specify the exact mechanism (e.g., does `Service` refuse all writes, or only flag the gap?) as an unresolved decision (§12), since it trades availability against audit-completeness in a way only the product owner should decide.

---

## 10. Testing and verification strategy (design only — no tests created)

A future implementation would need, at minimum:
- Event-creation tests for every operation in §4's matrix, both success and each listed failure outcome, asserting the correct `event_type`/`outcome`/`operation_target` and the **absence** of any secret value or key material in the row (a direct, automatable assertion — grep the serialized row for known test-fixture secret values and fail if found).
- Hash-chain verification tests: a tampered single row is detected; a reordered/inserted row breaks the chain; a **deleted tail row is NOT detected by the chain alone** (this must be an explicit test asserting the *limitation*, not just the chain's positive properties, per §6's required honesty) — proving the documented gap is real and that any future external-anchor mechanism (§6.1, §12) is actually needed, not hypothetical.
- Concurrent-writer tests: multiple goroutines appending events simultaneously produce a single, valid, totally-ordered chain with no two events claiming the same `prev_hash` (a direct test of §6.1's "ordering" constraint).
- Transaction-rollback/crash-interruption tests for the three `WithTx`-covered operations (§7.1): killing the process (or simulating a `fn` panic/error) mid-operation leaves neither the business row nor its audit event committed.
- Explicit tests for the asymmetric gap in the twelve autocommit operations (§7.2/§7.3): construct the "business succeeds, audit write fails" scenario deliberately and assert the documented behavior (business result still returned; failure logged) rather than assuming it.
- Disk-full/locked-database/corruption-injection tests, reusing whatever test harness `internal/store/sqlite`'s existing `sqlite_dirsafety_test.go`/`sqlite_tx_test.go` already use for analogous conditions on the primary tables (not designed in detail here, but these existing test files are the right starting point for a future implementer, per §2's "do not infer from filenames alone" instruction — their actual content was not read in this pass and should be before writing new tests against the same conditions).
- Schema-migration/version-compatibility tests analogous to the existing migration test suite.
- Backup/restore verification tests, including the "restore marks itself as an audit event" behavior proposed in §9.
- Timestamp-field tests asserting it is recorded but **never treated as a trust input** in any verification logic (a negative test: verification must still pass/fail correctly even with a deliberately wrong timestamp, proving the field is informational only, per §5.1).
- Authentication-failure and authorization-denial event tests, once Option B exists, including that the two are recorded as distinct `event_type`s (§4) and never conflated.
- **Same-UID limitation documentation tests**: a test (or a CI documentation-lint check) asserting that no shipped documentation string claims `get --reveal`/`disable-version`/`delete` restrictions are *enforced* — only that they are *intended policy* — directly operationalizing Step 5.2 §15's requirement not to misrepresent what Option B can do.

---

## 11. Risks, limitations, and residual threats

- **The rollback-detection gap is not closed by this design** (§6.1, §6.2) — this is the single most important limitation to carry forward honestly into any future implementation's documentation.
- **The twelve-operation transaction gap (§7.2)** means, until resolved, roughly half of `dso central`'s write surface has only best-effort (not atomic) audit recording — a real, disclosed limitation, not a defect introduced by this design (it reflects the existing `internal/store` transaction boundaries, §2.2).
- **The same-UID identity limitation is inherited, not solved, and not solvable by an audit mechanism** (§8) — recording *what was observed* is not the same as *authenticating who did it* at finer granularity than the OS already provides.
- **A second, audit-purpose key (if (c) were later adopted) is a new key-management surface** this document explicitly did not recommend introducing yet (§6.2), precisely because it would add real operational burden without a correspondingly concrete threat it closes at this product stage.
- **Audit-log-induced privacy exposure (§8)** — access-pattern metadata is new information beyond what the primary schema discloses, and this document surfaces that rather than dismissing it.

## 12. Unresolved questions and approval gates

1. **External trust anchor for rollback detection (§6.1, §6.2)** — not designed here; needs an explicit decision on whether/how to anchor the chain outside the rollback domain, and whether that is in scope for the audit-chain milestone or deferred again.
2. **Extending `WithTx` coverage to the twelve currently-autocommit write operations (§7.2)** — accept the disclosed non-atomicity, or approve the larger `internal/store` change required to close it.
3. **Separate audit-purpose signing/MAC key (§6.1's option (c))** — this document recommends against introducing it now (§6.2), but that is a recommendation requiring explicit sign-off, not a decision made unilaterally.
4. **Audit-history read authorization's exact rule** (§8) — deferred to Step 5.2's matrix being extended at implementation time, not decided here; needs confirmation that "same as everything else, until Option B exists, then the same socket policy" is the right placeholder.
5. **Archival/export mechanism for retention (§9)** — not designed; needs a decision on whether unbounded append-only growth is acceptable for the product's target deployment scale, or whether an archival design is a near-term requirement.
6. **Failure semantics when the audit table itself is corrupted (§9's last item)** — fail closed on all writes, or only flag the gap and continue? An explicit availability-vs-audit-completeness product decision, not something this document resolves.
7. **Whether a future restore command must itself emit an audit event marking the restore (§9)** — proposed, not approved; depends on Step 5.4's own design, which has not started.

## 13. Recommendation and acceptance criteria

**Recommendation**: proceed toward implementation with (b) hash chaining (§6.2), atomic audit writes for the three `WithTx`-covered operations (§7.1) as the initial, no-new-transaction-management default, and the twelve autocommit operations' non-atomicity explicitly disclosed rather than silently accepted or silently "fixed" by an unreviewed `internal/store` change. This is the smallest design that gives `dso central` real, tested tamper-evidence for its highest-value operations (secret creation, versioning, and especially plaintext reveal, §4) without taking on a new key-management surface (§6.2) or a larger store-level refactor (§7.2) that this step was not scoped to decide alone.

**Acceptance criteria for a future implementation phase** (stated for that phase's benefit, not satisfied by this document):
- Every operation in §4's matrix produces exactly one event per logical call, with the non-duplication rule honored.
- The hash chain passes all tests in §10, including the explicit negative test proving tail-deletion is *not* detected by the chain alone.
- The three `WithTx`-covered operations' audit writes are verified atomic by a crash-interruption test; the twelve autocommit operations' documented asymmetric failure behavior (§7.3) is verified by a deliberate test, not assumed.
- No event, in any success or failure case, ever contains a secret value, key material, or raw error string sourced from potentially sensitive input.
- Documentation accompanying the implementation states the rollback-detection limitation (§6.2's required explicit statement) and the same-UID limitation (§8) as plainly as this design document does — not softened for a release-notes audience.
- Every item in §12 has been explicitly decided by the product owner before the corresponding part of the implementation proceeds.

---

## 14. Repository state and close-out

- **Branch/HEAD at the start and end of this task**: `feature/webui-phase1` @ `ccfc0b55b7ce8630d395e9dd1e29d301d6010e61` — unchanged.
- **Pre-existing modifications in the working tree, not created by this task**: `.claude-flow/*` (10 files, predate this entire session), `.github/workflows/coverage.yml` and `pkg/config/config.go` (the two Phase 1 close-out fixes, still uncommitted from earlier in this session), and five untracked `docs/reviews/*.md` files from this session's earlier Phase 1/Step 5.1/Step 5.2 work.
- **The only file created by this task**: `docs/reviews/2026-10-25-central-store-phase2-audit-chain-design.md` (this document).
- **No existing file was edited by this task.** No Go source, test, schema, migration, CI workflow, dependency, or configuration file was touched.
- **Nothing was staged, committed, pushed, tagged, or published.**
- **The two Phase 1 validation items remain open, not claimed complete**: Linux CI validation of the `pkg/config` symlink fix and the `internal/agent` Unix-socket tests, and live fork-PR validation of the Codecov guard. Neither was touched, tested, or newly evidenced in this task.
- **Checks actually run in this task** (read-only; no formatters, no code changes): `git branch --show-current`, `git rev-parse HEAD`, `git status --short` (run at the start of this task); file-existence checks for the four prerequisite documents (all confirmed present); `ls internal/store/`, `ls internal/store/sqlite/`, `grep -n "^func (s \*Service)" internal/centralsvc/service.go`, a full read of `internal/store/sqlite/tx.go`, and `grep` of the two migration files for table/trigger/column names — all read-only repository reconnaissance, no execution of `go build`/`go test`/`go vet` was performed or claimed in this task.
