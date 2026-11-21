# Central store — Milestone 2 design: identity, authorization and audit

Date: 2026-10-19 · Status: **DRAFT, revised after the 2026-10-19 design review. Design only — nothing in this
document is implemented, and no cryptography has been written.** Revision summary: §4.2 delegation rules;
§5.7 audit-write failure semantics; §5.8 verification states and recovery; §5.9 identity-state replay (D13);
§6.4 clock guard (D15); §7.2 D9; §7.3 trigger guarantees; Appendix B.
Second-review pass: §5.7 (D22 concurrency/failure), §5.8 (D20 state machine; reset from an
inconsistent database), §5.9 (D21 key lifecycle; epoch baseline; `ST-UNVERIFIABLE`), §6.4 (precise
token rule for `clock reset`).
Third pass (M2.1 documentation only, F-17/F-18 of the final audit): §1 fact 1 and §6.1 (exact `WithTx`
scope and nesting behaviour).
**Fourth pass (resolution of the final consistency audit, findings F-01…F-28; documentation only):**
§1 fact 8 and §6.1 (M2.1 as committed); §2.5; §4.2 and §4.2.2; §5.2, §5.4, §5.7 (D22 accounting);
§5.8 (authentication in degraded states, exit-code table, transitions, reset order, epoch boundaries);
§5.9 (labels, project/environment replay, baseline, `USE-*` scope, `--state` on an untrustworthy chain);
§6.4 (monotone high-water mark, expiry evaluation, `clock reset` rules); §7.1–§7.3 (schema, genesis,
triggers); §8; Appendix B. Traceability: `m2-audit-resolution-matrix.md`. **Implementation status: only
M2.1 (`Store.WithTx`, commit `8c3763c`) exists; nothing else in this document is implemented, and M2.2
onward is not authorized.**
**Fifth pass (owner decisions integrated; documentation only):** the owner decided, for this revision, D24
(monotone timestamps, with N-01 and N-02 accepted as documented limitations), D23 (restricted Mode R),
OQ-D20-1 (read-only metadata listings allowed while quarantined) and the D20 release policy (identity-state
findings block release). Touched: §5.8 (Mode R scope, release transaction, listings, BLOCKED item B-1), §6.4
(accepted limitations, wording), §8. **D21 remains ON_HOLD; D13 and D15 are not confirmed (they depend on
D21); D22 awaits confirmation. One item is BLOCKED (B-1: how `quarantine --release` is authorized once Mode R
is forbidden from releasing).** See the plan §6 and `m2-audit-resolution-matrix.md`.
Baseline of the M1 facts below: `feature/m1-storage-keys` @ `58e711f` (M0 remediation `c95911c`);
M2.1 is `8c3763c`.
Companion: `m2-implementation-plan.md` (sequence, checklist, acceptance criteria,
risk register, open decisions).

Read together with `docs/central-store-m1.md` (what exists) and
`docs/design/m1-design-addendum.md` (decisions M2 builds on).

## 0. Scope, non-goals and an honest statement of what M2 can guarantee

**In scope:** operator and service-token identities; project/environment
authorization with deny-by-default; a persistent, tamper-evident audit log;
audit-key management; the transaction structure that keeps protected mutations
and their audit events from diverging; SQLite migration/upgrade; a local CLI.

**Explicitly out of scope (and must stay out unless separately approved):**
remote/network secret distribution or any listener; WebUI integration; agent
integration; root-key or DEK rotation; backup/restore commands; purge; OIDC/TOTP;
HA/PostgreSQL; **any claim of production readiness.**

**The central caveat.** In M1 and M2 the process that enforces policy runs *as
the same OS account that owns the database and can read the root key*. Anyone who
can run code as that account — or `root` — can read the key file, decrypt every
secret, mint valid audit entries and bypass every check in this document by not
going through the service at all. M2 therefore delivers (a) the **enforcement
machinery and data model** that M3's server will rely on, (b) **accountability
and least privilege among cooperating operators**, and (c) **tamper evidence
against parties who can modify the database but do not hold the keys**. It does
**not** deliver a security boundary between a local user and the key holder.
That boundary only exists once keys live inside a server process that clients can
reach only over an authenticated API (M3). Service tokens in M2 are exercised and
tested but, because a holder still needs local access to the database and key to
use them, **they are not yet an access-control boundary** and must not be
presented as one.

**Time is a stated limit (accepted by the owner, D24).** The monotone `ts_high_water` stops a clock
rollback from reviving credentials that had expired as of the newest recorded event, but it cannot see a
rollback that stays later than that event, and a transient forward jump can force a break-glass
`clock reset` that revokes legitimate credentials (§6.4). Heartbeat events are a future option only. No
statement in these documents means that an expired credential can never be revived under every rollback.
**Root-key proof (Mode R) is database-owner-level authority** and its read-only use is not recorded in the
chain (§5.8, D23).

## 1. Facts about the M1 baseline that constrain M2

Verified in the committed code (not from reports):

1. **Store methods do not share a transaction, and most do not open one.** The
   `store.Store` interface has 19 data/metadata operations (plus `SchemaVersion`
   and `Close`). In `internal/store/sqlite`, **4** use the private helper `s.tx`
   (`CreateSecret`, `AddVersion`, `DisableVersion`, `PutMetaIfAbsent`); the
   remaining **15** (`CreateProject`, `GetProject`, `GetProjectBySlug`,
   `ListProjects`, `CreateEnvironment`, `GetEnvironmentBySlug`, `ListEnvironments`,
   `GetSecret`, `ListSecrets`, `DeleteSecret`, `GetVersion`, `GetLatestVersion`,
   `ListVersions`, `GetMeta`, `ListMeta`) and `SchemaVersion` call `s.db`
   directly, i.e. SQLite autocommit. Of those, `GetVersion`, `GetLatestVersion`
   and `ListVersions` issue **two** separate autocommit statements (`liveSecret`,
   then the read). The DSN sets `_txlock=immediate`, so `s.db.BeginTx` takes the
   write lock at `BEGIN`; autocommit statements do not begin a transaction. The
   pool allows up to 8 connections (`SetMaxOpenConns(8)`), `busy_timeout`
   defaults to 5 s, and `mapErr` (not-found, unique/primary-key → `ErrConflict`,
   foreign key → `ErrNotFound`) does not map busy errors. There is no way to
   compose "authorise + mutate + audit" atomically today. M2 needs a unit-of-work
   API (§6); this is the single largest refactor.
2. **`created_by` / `disabled_by` / `deleted_by` are caller-supplied strings**
   (`actor` parameters). Under M2 they must be *derived from the authenticated
   principal*, never accepted from the caller.
3. **`KeyProvider` offers only `ID/Wrap/Unwrap`.** That is sufficient for the
   recommended audit-key design (§2) and for any KMS.
4. **`Service.Bind` is process-global and cached** (`bound`), and every data
   method is exported on `*Service`. M2 must separate "unauthenticated service"
   from "authenticated handle" (§4.3).
5. **`store_meta` is insert-only** (triggers); `secret_versions` are immutable
   except the one-way disable. M2 reuses these patterns for audit tables.
6. **Binding already ties the database to one root key**; audit keys will be
   wrapped under that same key and verified through the same canary.
7. Reading a secret is currently a plain read. Under M2 a read of a secret value
   is an audited event and therefore a **write transaction** (serialises readers;
   acceptable for a local milestone, flagged as a scaling risk for M3).
8. **M2.1 is implemented and committed (`8c3763c`, verified in `internal/store/store.go`,
   `internal/store/sqlite/tx.go`, `sqlite.go`).** `Store.WithTx(ctx, fn func(ctx, Tx) error)` and a
   transaction-scoped `Tx` cover the 19 operations (`store.Operations`). The four former `s.tx` operations
   delegate to `WithTx`; the 15 autocommit operations keep their M1 statements (the three two-statement
   reads still send two separate statements outside `Tx`). Nested `WithTx`, or a standalone operation, called
   with the context the callback received (or one derived from it) on the **same `Store` instance** while the
   transaction is open returns `ErrTxActive` without touching the database. The marker does **not** detect an
   unrelated or discarded context, a different `Store` instance, or another process (OQ-TX-1 option (a)).
   `AC-TX-04b-2` is **unproven** (skipped, never passed). Nothing of M2.2 or later exists.

## 2. Audit-key design decision

### 2.1 Options

**A — derive the audit key from the root key with HKDF.**
**B — generate a random audit key, store it wrapped by the `KeyProvider`, with
explicit key ids and versions.**
**C — extend `KeyProvider` with a derivation capability (`Derive(label)`).**

### 2.2 Comparison

| Criterion | A: HKDF(root) | B: random, wrapped | C: provider `Derive` |
|---|---|---|---|
| Works with non-exportable KMS roots | **No.** Needs the root key bytes; AWS KMS / Azure Key Vault / Vault-transit never release them | **Yes.** Uses only `Wrap/Unwrap`, already required of every provider | Partly. Needs a MAC/derive primitive: AWS KMS `GenerateMac` (HMAC keys only), Vault transit HMAC, Azure Key Vault (no equivalent for all key types). Semantics differ per provider |
| Root-key rotation | **Breaks verification.** New root ⇒ new audit key ⇒ old events fail unless old roots are kept forever | **Free.** Re-wrap the audit-key rows; MACs never change | Same break as A unless the provider derives from a stable key |
| Audit-key rotation (independent) | Impossible without a version input; adds a parameter that is really a second key | **Native.** New version, chain continues | Possible via a label/version string |
| Recovery from losing the DB row | Recomputable from the root key alone (the one real advantage) | **Not recomputable.** The wrapped row must be backed up; if lost, history is unverifiable (§2.5) | Recomputable if the provider is intact |
| Recovery from losing the root key | Everything lost anyway | Everything lost anyway | Same |
| Migration of an M1 database | Trivial (no stored state) | One-time creation + genesis, inside the M2 upgrade (§7.2) | Trivial, but requires changing every provider first |
| Testability | Needs the key bytes in tests | **Excellent:** works with a fake provider that hides its key, proving KMS compatibility without a KMS | Each provider needs a derive double |
| Blast radius if the audit key leaks | Also reveals the root key's derivation input — **the root key itself if HKDF input is mishandled**; coupling | Only audit forgery | Provider-dependent |
| Domain separation from data keys | By info string only | Independent random key; HKDF is then used *over that random key* for per-purpose subkeys | By label |
| Complexity | Lowest | Low–medium (one table, one rewrap path, one rotation path) | Highest (interface change + per-provider semantics + migration for providers) |
| Security if DB + root key are stolen | Attacker derives key ⇒ forges chain | Attacker unwraps key ⇒ forges chain | Same |

All three are equivalent in the *worst* case (DB + root key stolen ⇒ the chain can
be forged); they differ in how they behave with KMS, rotation and recovery, and
there B dominates.

### 2.3 Recommendation: **B — confirmed**

Your preference for B is correct, for these technical reasons:

1. **It is the only option compatible with the interface M1 already committed to.**
   `KeyProvider` was designed so a non-exportable root key can be added without
   touching storage; A defeats that and C changes the contract.
2. **Rotation behaves correctly.** Root-key rotation (a planned later milestone)
   is a re-wrap of N small blobs for B; for A it silently invalidates the entire
   history.
3. **It separates the audit trust domain from the data-key hierarchy** with an
   independent random key, instead of a deterministic function of the same
   secret.
4. **It is testable without a KMS** (a provider test double that never exposes
   its key proves the design works for KMS).

Refinements I require (these are where a naive B goes wrong):

* **Dedicated table, not `store_meta`.** `audit_keys(version, key_id,
  wrapped_key, root_key_id, created_at, activated_seq, retired_at)`; insert-only
  except `retired_at`. Old versions are retained (wrapped) so history stays
  verifiable.
* **Explicit ids.** `key_id` is 8 random bytes (hex), **not** derived from the key
  material; `version` is a monotonically increasing integer. Both are in every
  event's MAC input (so an attacker cannot relabel an event to a different key).
* **Wrap AAD binds purpose and identity:** `enc("dso/audit-key/v1", root_key_id,
  key_id, version)`. An audit-key blob can therefore not be mistaken for a DEK or
  the binding canary, nor moved between versions.
* **Subkeys by HKDF *over the random audit key*:** `K_event = HKDF-SHA256(ikm =
  audit_key, salt = key_id, info = "dso/audit/event-mac/v1")`, and distinct
  `K_head` (`"…/head-seal/v1"`) and `K_state` (`"…/state-commit/v1"`, §5.9). Using HKDF here is sound because the input is a
  uniformly random 256-bit key (unlike A, where the input is the root key).
* **Held in memory only**, unwrapped once on first need, zeroed on shutdown
  (best-effort in Go). With a KMS provider this is one `Unwrap` call at start.
* **Chain epoch id.** A random `chain_id` (16 bytes) created at upgrade is part of
  every MAC, so events cannot be spliced between databases or epochs.
* **Algorithm identifier** (`mac_alg = "hmac-sha256-v1"`) is stored per event and
  MACed, so the scheme can change without ambiguity.

Remaining weakness of B, stated plainly: the wrapped audit key lives in the same
database as the data. **DB + root key ⇒ audit forgery**, exactly as for A/C. B
does not fix that; external anchoring (§5.6) mitigates it.

### 2.4 Rotation

* **Audit-key rotation (in M2).** `audit rotate-key` (requires `audit:admin`):
  in one transaction (1) append event `audit.key_rotated` **MACed with the old
  key**, carrying the new `key_id`/`version`; (2) insert the new `audit_keys` row
  with `activated_seq = that event's seq + 1`; (3) set the old row's `retired_at`.
  The next event is MACed with the new key and chains from the rotation event's
  MAC, so the chain is continuous and a verifier follows key changes by `version`.
  Old keys are never deleted.
* **Root-key rotation (later milestone, designed-for).** Re-wrap every
  `audit_keys.wrapped_key` (and DEKs, and the binding canary) under the new root
  key with new AAD (`root_key_id`); event MACs are unaffected. No M2 code path
  depends on the root key staying the same *except* the AAD, which is why `root_key_id`
  is stored per row.
* **Compromise response.** If the audit key may have leaked: rotate immediately,
  record the suspected-compromise window in an operator note event, and treat
  events in that window as *integrity-unproven* (an attacker with the old key
  could have rewritten them) — cross-check against the last external anchor.

### 2.5 Recovery

| Failure | Consequence | Procedure |
|---|---|---|
| `audit_keys` row for version *n* corrupted/deleted, root key intact | Events of version *n* cannot be verified; new events cannot be sealed if *n* is the active key | Restore the database from backup (backups include `audit_keys`). If no backup: break-glass `audit reset-epoch` (proof of root-key possession + typed confirmation; §5.8) starts a **new epoch**: new `chain_id`, new key version and an `audit.epoch_start` event carrying only *observations* of the old chain (not proof, §5.8). Every older epoch is permanently labelled `HISTORICAL — not trusted as authentic`. It is loud, audited and irreversible by design |
| Root key lost | All data and audit keys unrecoverable | Same as M1: escrow of the root key is a **precondition to storing real data** |
| Active audit key cannot be unwrapped (wrong/unavailable provider) | Service refuses every audited operation (fail closed) | Restore the provider |
| Whole DB rolled back to an older copy | Not detectable from inside the DB | Compare against the external anchor (§5.6) |

### 2.6 What would justify Ed25519 instead of HMAC (Open Decision D2)

HMAC is symmetric: **whoever can verify can forge.** An auditor given the audit
key can rewrite history. Ed25519 (private key wrapped exactly as in B; public
key published in `audit_keys` and in anchors) lets third parties verify without
forging capability. It does not help against a compromised signing host (the
process holds the private key), so it matters only once there is a separate
auditor party — M3 or later. Recommendation: **HMAC in M2**; keep `mac_alg` in the
format so Ed25519 can be added without a schema break.

## 3. Principals, credentials and trust separation

Two classes of principal, **disjoint by construction**:

| | Operator (human) | Service token (machine) |
|---|---|---|
| Purpose | administer projects, grants, tokens; optionally read/write secrets | read secret values (and metadata) in a fixed scope |
| Table | `operators` (+ `operator_sessions`) | `service_tokens` (+ `service_token_envs`) |
| Credential | password (Argon2id) → short-lived session token `dso_op_<id>.<secret>` | bearer token `dso_st_<id>.<secret>` |
| Management permissions | via grants/roles | **never** — no role, no code path, `CHECK` on the permission column |
| Created by | another operator with `member:grant` / `operator:manage` | an operator with `token:issue` for that project |
| Can act as the other class | no | no |
| Typical lifetime | session ≤ 8 h | ≤ 90 days default (policy, D5) |

Separation is enforced at four layers: distinct token **prefixes** that are
checked before any lookup; distinct **tables** and `Principal` types; a
**permission set** per class in code, with the service-token set a strict subset
(`secret:read`, `secret:list`) enforced again by a `CHECK`; and **API shape**:
management operations are methods that accept only an operator principal at the
type level.

### 3.1 Operators

* `username` unique (case-folded, `[a-z0-9._-]{3,64}`), `password_hash` as an
  Argon2id PHC string (parameters t=3, m=64 MiB, p=4 and the same floors/ceilings
  as the passphrase provider), minimum password length 12, no default credentials
  anywhere.
* Verification: hash comparison outside the DB transaction (CPU-heavy), then
  session creation inside one. **Backoff on failure:** per operator
  `failed_count`/`locked_until` with exponential delay capped at 15 minutes;
  persisted, so restarts do not reset it. Lock-out is a delay, not a permanent
  state (a permanent lock lets an attacker deny the operator). A uniform
  "authentication failed" is returned for unknown user, wrong password, disabled
  operator and locked operator (a constant-time dummy hash is run for unknown
  users).
* `disabled_at` takes effect on the next operation of every session (checked in
  the operation's own transaction), not on the next login.
* **Bootstrap.** `dso central upgrade` (or `init` on a new database) creates the
  first operator with `instance_admin`, password from a hidden prompt or a
  `securefs`-checked `--password-file`. There is no "initial password".

### 3.2 Operator sessions (CLI)

`dso central login` verifies the password and issues a random 256-bit session
token `dso_op_<session_id>.<secret>`; the database stores only `SHA-256(secret)`,
`created_at`, `expires_at` (absolute, default 8 h, maximum 24 h), `last_seen_at`
(idle limit 30 min, D3), `revoked_at`. The token is written to a `0600` file at a
path checked by `securefs` (default under the user's state directory; never an
argument, never the environment). `logout` revokes server-side. Threat: the file
is a bearer credential — theft equals session theft until expiry/revocation —
which is why lifetimes are short and `operator session revoke` exists. Without
sessions every command would prompt for the password, which pushes users toward
`--password-file`, a worse outcome.

### 3.3 Service tokens

* **Format:** `dso_st_<token_id>.<secret>`; `token_id` is 16 random bytes (hex),
  `secret` is 32 random bytes (base64url, no padding). Generated with
  `crypto/rand`; failure is an error, never a fallback.
* **Storage:** `token_id`, `secret_hash = SHA-256(secret)` (the secret is 256-bit
  random, so a fast hash is correct — the same reasoning as the design's token
  section), project, scope, permission set, name, `created_by` (operator),
  `created_at`, `expires_at`, `last_used_at`, `revoked_at/by`, `replaced_by`.
  The plaintext is **shown once** on creation (stdout, or `--output-file` created
  `O_EXCL 0600` via `securefs`) and is not recoverable.
* **Verification:** parse prefix and lengths (reject anything malformed before
  touching the DB) → lookup by `token_id` → constant-time compare of
  `SHA-256(secret)` → check `revoked_at`, `expires_at`, and scope — **all inside
  the operation's transaction**, so revocation and expiry take effect on the next
  request, with no caching.
* **Scope:** exactly one project and a non-empty set of environments of that
  project (composite foreign key), permissions ⊆ {`secret:read`, `secret:list`}.
  Optional source-CIDR restriction is deferred to M3 (meaningless without a
  network).
* **Expiry:** `expires_at` is mandatory; a non-expiring token requires an
  explicit `--no-expiry` that is audited and flagged in `token list` (D5).
* **Rotation:** `token rotate <id> [--grace 24h]` issues a replacement with the
  same scope/permissions/TTL and sets the old token's `expires_at =
  min(current, now+grace)` and `replaced_by`. Both work during the overlap; the
  old one can be revoked earlier. Revocation is permanent (a trigger forbids
  clearing `revoked_at`).
* **`last_used_at`** is updated in the same transaction as the audited read (no
  extra event).

### 3.4 What is *not* a credential

Environment variables never carry passwords or tokens (only paths to
`securefs`-checked files may); nothing secret is accepted as an argument.

## 4. Authorization model

### 4.1 Rules

1. **Deny by default.** No grant ⇒ no access. There are no implicit grants, no
   "owner" fallback, no default project.
2. **Allow-only grants**, evaluated per request inside the operation's
   transaction. No `deny` effect in M2 (kept out to avoid precedence bugs;
   revisit only with a concrete need).
3. **Permissions are fixed in code**, roles map to permission sets in code, and
   neither is user-editable (the design's `role_permissions` table is not
   introduced).
4. **Instance administration is separate from data access.** `instance_admin` can
   create projects, manage operators and grants, read audit logs and verify
   them — but holds **no** `secret:read`. Reading values requires a project grant
   even for an instance admin. (This is a policy statement about the service; the
   OS-level caveat in §0 still applies and is documented next to it.)
5. **Least surprise on denial:** the error for "no such project/environment/
   secret" and "not permitted" is **the same** (`ErrAccessDenied`, message
   "access denied or not found") so the API is not an existence oracle. The
   *reason* is recorded in the audit event only.
6. Cross-project and cross-environment access is impossible by construction:
   every lookup resolves `(project, environment)` first, authorises that exact
   pair, and only then touches secrets by ids that belong to it (the composite
   foreign keys from M1 back this up).

### 4.2 Permissions, roles and delegation (D4 — revised after design review)

Permissions: `project:create`, `env:create`, `secret:create`, `secret:add-version`,
`secret:read`, `secret:list`, `secret:disable-version`, `secret:delete`,
`member:grant`, `member:revoke`, `token:issue`, `token:list`, `token:revoke`,
`audit:read`, `audit:verify`, `audit:admin` (rotate/quarantine), `operator:manage`
(create/disable/enable/set-password/set-admin).

| Role (scope) | Permissions |
|---|---|
| `instance_admin` (an attribute of an operator, set only by `operator set-admin`) | `project:create`, `operator:manage`, `audit:read`, `audit:verify`, `audit:admin`, `member:grant` and `member:revoke` **limited exactly as §4.2.1 states** (assign `project_admin` only; revoke any grant); **no secret permissions** |
| `project_admin` (a project, or one environment of it) | `env:create`, `member:grant/revoke` **bounded by the delegation table (§4.2.1)**, `token:list/revoke`, `token:issue` **bounded by the hold-it rule (§4.2.2)**, `secret:list`, `audit:read` (own project). **No `secret:read` and no secret mutation** |
| `writer` | `secret:create`, `secret:add-version`, `secret:disable-version`, `secret:delete`, `secret:list` |
| `reader` | `secret:read`, `secret:list` |
| `auditor` | `audit:read` (own project), `secret:list` (metadata only) |
| service token | `secret:read` and/or `secret:list` within its environments — nothing else |

A grant is `(operator, project, environment | all, role)`; an environment-scoped
grant authorises only that environment. Roles are additive across grants.

#### 4.2.1 Delegation rules (who may assign which role)

The earlier draft said "a principal can never grant a permission it does not hold".
Taken literally that stops a `project_admin` (who holds no secret permissions) from
ever assigning `reader` or `writer`, which defeats project-level access management.
It is **replaced** by this explicit table. `member:grant` means "may assign the
roles listed for the grantor's authority, inside the grantor's scope, to *another*
operator" — it never confers the permissions of the assigned role on the grantor.

| Grantor's authority | May assign | Scope limit | Never |
|---|---|---|---|
| `instance_admin` | `project_admin` only | any project, project-wide or one environment | `reader`/`writer`/`auditor`; a grant to **themselves**; `instance_admin` (not a grantable role) |
| `project_admin` on project P (all environments) | `project_admin`, `writer`, `reader`, `auditor` on P | P: all environments or any single environment | any other project; instance level; a grant to themselves; a scope wider than their own |
| `project_admin` on P / environment E | the same four roles | E only | project-wide grants on P; other environments; other projects; themselves |
| `writer`, `reader`, `auditor`, service tokens | nothing | — | — |

Further rules (each has tests, §10/plan AC-DL-xx):

1. **No self-grants.** The grantee must differ from the grantor, so nobody can
   raise their own privileges, and an `instance_admin` cannot obtain secret access
   by granting themselves a role (they would need another operator to grant it;
   single-person installations create a second account and accept that this
   separation is procedural — see §4.2.3).
2. **`instance_admin` is not a role grant.** It is an operator attribute changed only
   by `operator set-admin` (requires `operator:manage`, i.e. an existing
   `instance_admin`). `project_admin`s cannot set it. The **last live
   `instance_admin` cannot be disabled, demoted or have its attribute cleared**
   (refused and audited), so the store cannot lock itself out through normal
   operations.
3. **Revocation** follows the same authority: a principal may revoke a grant it
   could have created (regardless of who created it), within its scope. An
   `instance_admin` may revoke **any** grant (removing access is monotone-safe)
   but still cannot create `reader`/`writer`/`auditor` grants. Anyone may revoke
   their own grants, except that the last `instance_admin` attribute cannot be
   removed this way.
4. Grants only ever name existing, enabled operators, existing projects and (if
   scoped) existing environments of that project (composite foreign key).
5. Every grant/revoke is an audited, replayable event (§5.9) carrying the grant id,
   grantee, project, environment (or `*`) and role.

#### 4.2.2 Service tokens obey a stricter rule: the issuer must hold what it delegates

A token is a bearer credential: whoever holds the string exercises its permissions
without any further accountability, and the issuer *sees* the string at issuance.
If a `project_admin` (no `secret:read`) could mint a read token they would simply
read secrets with it. Therefore `token:issue` and `token rotate` require that the
issuer **holds `secret:read` (and/or `secret:list`) on every environment in the
token's scope**, and the token's permission set must be a subset of the issuer's.
Listing and revoking tokens remain `project_admin` powers (monotone-safe).

*Lifetime of the rule (F-23).* The hold-it rule is checked **at issuance and rotation only**. If the
issuer later loses the grant, tokens it already issued **remain valid** until they expire or are
revoked (a token is not tied to its issuer's current grants; `token list` shows `created_by` so an
administrator can review and revoke them). The alternative — automatic revocation of an operator's
tokens when the operator loses the grant — is not part of M2; it is recorded as a possible M3 hardening.
Likewise, the break-glass `emergency revoke --kind operator` **refuses** to disable the last live
`instance_admin` (the same last-admin protection as §4.2.1 rule 2); the way to add an administrator when
none is usable is `operator recover`.

#### 4.2.3 Residual: separation of duties relies on distinct people

The rules prevent privilege escalation by a single operator account. They cannot
prevent two *colluding* accounts, or one human using two accounts, because M2 does
not force a two-person workflow and, per §0, anyone with OS-level access to the
database and key bypasses all of it. A `project_admin` who can name a second
operator they control can obtain reader access through that account. This is
documented as a limitation, not claimed as prevented.

### 4.3 Enforcement points

Authorisation is not a middleware that can be forgotten; it is part of the
transaction body of **every** data operation:

1. `Service` (unauthenticated) exposes only: `Bind`, `Upgrade`, `Authenticate*`.
2. `Authenticate*` returns a **sealed `Principal`** (unexported fields; only the
   package can construct one) bound to a session id or token id.
3. `svc.As(p)` returns a `Handle`; **all data operations are methods on `Handle`**.
   Each does, inside one transaction: re-validate the principal (session/token
   exists, unexpired, unrevoked, operator enabled) → resolve scope → authorise →
   operate → append audit → commit.
4. A reflection test enumerates `Handle`'s exported methods and fails unless each
   has a row in the authorisation matrix test (§10); another asserts `Service`
   exposes none of the data-operation names.
5. **Future API boundary (M3):** HTTP handlers may only obtain a `Handle` through
   `Authenticate*`; a test (import-graph check) forbids handler packages from
   importing `internal/store`. This is an acceptance criterion for M3, stated now
   so M2's package layout supports it.

### 4.4 Time-of-check/time-of-use

Because the principal check, grant check and mutation share one `IMMEDIATE`
transaction, there is no window in which a revoked token or removed grant is
honoured by an operation that started later. (An operation already *inside* its
transaction when a revocation is issued completes first — revocation is
serialised, not pre-empting. Documented; tested.)

## 5. Audit log

### 5.1 What is audited

| Class | Events (action codes) | Outcome values |
|---|---|---|
| Authentication | `auth.login` (creates a session), `auth.logout`, `auth.session_revoked`, `auth.failed`, `auth.failures_suppressed` | success / auth_failed |
| Secret values | `secret.read` | success / denied / error |
| Secret mutations | `secret.create`, `secret.version_add`, `secret.version_disable`, `secret.delete` | success / denied / error |
| Structure | `project.create`, `env.create` | same |
| Identity admin | `operator.create`, `operator.disable`, `operator.enable`, `operator.set_password`, `operator.set_admin`, `grant.add`, `grant.revoke` | same |
| Tokens | `token.issue`, `token.revoke`, `token.rotate`, `token.use_denied` | same |
| Audit itself | `audit.key_rotated`, `audit.epoch_start`, `audit.verify_recorded` (only after an explicit `--record` on a verification that succeeded, §5.8), `audit.anchor_exported` | success / error |
| System / break-glass | `system.upgrade`, `system.genesis`, `system.bind`, `system.recover`, `system.clock_reset`, `system.emergency_revoke` | success |

Identity-changing events are **replayable**: their `detail` follows the per-action
schema in Appendix B and is the input to `audit verify --state` (§5.9).
Metadata-only listings (`secret:list`, `project list`) are **not** audited by
default (volume; D12) but denied attempts are. Integrity failures on read
(`ErrIntegrity`) are audited as `secret.read` / `error` / `integrity_failure`.
The result of a *failed or unverifiable* verification is **never** an event in the
chain (§5.8).

### 5.2 Event schema

`audit_events` (append-only):

| Column | Notes |
|---|---|
| `seq` INTEGER PRIMARY KEY | assigned by the application as `head.seq + 1` inside the `IMMEDIATE` transaction (not `AUTOINCREMENT`); gapless |
| `ts_unix_nano` | the **stamped** time `max(wall clock, audit_state.ts_high_water)` (§6.4), so it never decreases along the chain; the one exception is a `system.clock_reset` event, which may set a lower value (its `old_hw`/`new_hw` detail is MACed). **Ordering authority is still `seq`, not time** |
| `chain_id` | 16 random bytes; identical for the epoch |
| `key_version` | which audit key MACs this event |
| `mac_alg` | `hmac-sha256-v1` |
| `actor_type` | `operator` / `service_token` / `system` / `anonymous` |
| `actor_id` | operator id or token id (never a secret) |
| `actor_label` | username or token name **at the time** (survives renames/deletes) |
| `session_id` | for operator events |
| `action` | code from §5.1 (CHECK against an allowlist) |
| `outcome` | `success` / `denied` / `error` / `auth_failed` |
| `reason` | short code (`no_grant`, `token_revoked`, `token_expired`, `integrity_failure`, `locked`, …) |
| `project_id`, `environment_id` | nullable |
| `resource_type`, `resource_id`, `resource_name` | e.g. secret id/name; for failed auth, the *attempted* username/token-id (length-capped), never the secret |
| `secret_version` | nullable |
| `request_id` | random 8 bytes per CLI invocation / operation |
| `origin` | `local:uid=1000` style string in M2 (no network yet) |
| `detail` | canonical `k=v` pairs, keys and formats fixed **per action** (Appendix B); validated before append; **never** values, keys, tokens, hashes or free text (credential changes appear only as keyed commitments, §5.9) |
| `prev_mac` | 32 bytes (all-zero for the genesis event, which also MACs `chain_id`) |
| `mac` | 32 bytes |

`audit_state` (single row): `chain_id`, `last_seq`, `last_mac`, `active_key_version`,
`ts_high_water` (the monotone time reference of §6.4; always equal to the `ts_unix_nano` of the newest
event, checked by the tail check and by `verify`),
`updated_at`, `quarantined_at` (NULL normally; set by `audit verify --latch` or
`audit quarantine`, §5.8), and the persisted failed-authentication window used by
the rate bound in §5.7 (`auth_window_start`, `auth_window_count`,
`auth_suppressed`, `auth_suppressed_window_start`). The row is updated in the same transaction as each append.

`audit_incidents` (informational, **not part of the chain, not MACed**): `id`,
`detected_at`, `chain_id`, `kind`, `first_bad_seq`, `detail`, `recorded_by` — written
only by `verify --latch`, `quarantine [--release]` and `reset-epoch`. It exists so an operator who comes back later
can see *that* a verification failed; the authoritative evidence is the verifier's
own output, never this table (anyone who can write the database can edit it).

What the triggers on these tables do and do not enforce is specified exactly in §7.3;
they are defence in depth, not a security boundary (§5.5).

### 5.3 Integrity chain

`mac_n = HMAC-SHA256(K_event[key_version], canon(fields_n) ‖ prev_mac_n)` where
`canon` is a fixed-order, **length-prefixed** (u32 big-endian) encoding of every
column above except `mac` (appendix A), with an explicit domain string and
version, so no two distinct events share a canonical form. `prev_mac_n = mac_{n-1}`.
Appending computes the MAC inside the transaction from `audit_state` read under
the write lock (so concurrent appenders serialise and cannot fork the chain); the
store receives a **sealer callback** (it never sees a key).

### 5.4 Ordering

Total order = `seq` (gapless, assigned under the lock). Because a mutation and
its success event are in one transaction, the event's `seq` position is exactly
the commit order of the mutation relative to every other audited operation.
Stamped timestamps are non-decreasing by construction (§5.2, §6.4), except at a `system.clock_reset`
event. `verify` reports a decrease anywhere else as the finding `TS-DECREASE` (an integrity finding,
exit 10: a monotone reference that was lowered outside a recorded reset is exactly the attack the guard
exists to stop). A wall clock that has gone backwards is **not** itself a verification failure: it is
absorbed by stamping (`ts = max(now, ts_high_water)`) and surfaces as the clock guard (§6.4). Time is
never the ordering authority.

### 5.5 What the chain guarantees — and what it does not

**Detectable by `audit verify`** by a party who can read the database but does
**not** hold the audit key:

* modification of any field of any event;
* deletion, insertion or reordering of events *followed by at least one later
  event* (the `prev_mac` links and gapless `seq` break);
* relabelling an event to a different key version, algorithm or chain;
* splicing events from another database/epoch (`chain_id`);
* a missing/forged `audit_state` head that disagrees with the last event;
* truncation, **if** a newer head was exported/anchored elsewhere (§5.6).

**Not detectable, or not prevented:**

* **Anyone holding the audit key can forge a perfectly valid chain.** The
  audit key is recoverable by anyone with the database *and* the root key — i.e.
  the DSO OS account, `root`, or a backup thief who also took the key. A local
  HMAC chain is therefore **tamper-*evident* against a database-only attacker, not
  tamper-*proof* against the database owner or host administrator.**
* **Tail truncation / whole-database rollback** without an external anchor: the
  attacker deletes the last *k* events and restores a previous coherent
  `audit_state` (copied from an old backup/WAL). All remaining links verify.
* **Bypass:** reads/writes performed by reading the key file and database
  directly, or by a modified binary, produce **no events at all**. The log
  records what went through the service, not what happened to the data.
* **Best-effort events:** success events share their mutation's transaction, so a
  mutation without its event cannot occur. *Denied* and *failed-authentication*
  events are committed separately and can be missing if the database is
  unwritable or the process dies at that instant (§5.7); the operation was still
  denied. Rate-bounded failed-authentication events are summarised, not individually
  kept, above the ceiling.
* **Denial of service:** deleting `audit_keys` or corrupting `audit_state` is
  detected (verification fails, the service refuses to run) but cannot be
  prevented.
* **Clock integrity:** stamped timestamps are covered by the MAC but derive from an unvalidated host clock, bounded below by the monotone `ts_high_water` (§6.4); only `seq` orders events.
* Triggers (immutability, structural checks) are removable or satisfiable by anyone
  with raw write access; they stop accidents and unsophisticated tampering, not an
  administrator (§7.3).

### 5.6 External anchoring (mitigation, not cure)

`audit head` prints `{chain_id, seq, mac, key_id, ts}` plus a head seal
(`HMAC(K_head, …)`); `audit export --since seq` emits JSON Lines. Shipping these
to an append-only system the DSO host cannot rewrite (syslog server, object lock
bucket, a witness a human reviews) is what turns "tamper-evident" into
"truncation-evident". `audit verify --anchor seq:mac` fails if the database's
chain does not contain that exact entry. M2 provides export/verify; **operating
an anchor is an operator duty** and is documented as such. Anchoring cadence is
an operator decision (D10).

### 5.7 Audit-write failure semantics (revised)

**Terminology.** An event is *recorded* only after the SQLite transaction that
contains it has **committed successfully**. An event whose transaction failed,
was rolled back, or whose commit outcome is unknown is *not recorded*, and no
message, log line or counter may claim otherwise.

The cases below are distinguished by **what failed**, because what can still be
done differs:

| # | What failed | Protected mutation / secret read | Denied operation or failed authentication | What is reported |
|---|---|---|---|---|
| F1 | **Audit-specific** failure while the database still works: audit key cannot be unwrapped, sealer error, tail/head check fails (§5.8), chain quarantined, event fails validation | **Refused** (`ErrAuditUnavailable` / `ErrAuditIntegrity`): rolled back or never started; **no plaintext** | Still **denied**. No event can be appended; nothing is recorded | stderr: "denied; audit event NOT recorded (`<class>`)" (for denials) or the error (for refusals) |
| F2 | **General database write failure**: disk full, I/O error, read-only filesystem, lock timeout, corruption | Fails with an error; nothing committed; **no plaintext** | Still **denied**; **nothing recorded** | stderr as in F1. A *process-local, in-memory* counter `dropped_since_start` is kept and printed at CLI exit when non-zero. It is **diagnostic only** and is lost with the process. **There is no persisted `audit_dropped` counter** (an earlier draft proposed one): persisting it needs the very resource that just failed |
| F3 | **`COMMIT` returns an error, or the process dies around the commit** | SQLite atomicity: after recovery **either** mutation + event exist **or** neither. The caller sees an error (or no reply): the outcome is *unknown to the caller*. No plaintext was released before a successful commit | n/a | The CLI prints "outcome unknown — check `audit list --since <seq>`" and exits non-zero. Success messages for mutations are printed **only after** a successful commit |
| F4 | Commit succeeded, **delivery failed** (crash/pipe closed after commit) | Event exists, caller got nothing (over-reporting) — the safe direction | n/a | none |
| F5 | **Rate bound on failed authentication** (D6) | n/a | Individual `auth.failed` events are dropped *by policy* above the ceiling | see below |

**Failed-authentication rate bound (D6, D22 — made precise).** CLI processes are
short-lived, so a per-process limiter would be meaningless; the bound lives in
`audit_state` (`auth_window_start`, `auth_window_count`, `auth_suppressed`,
`auth_suppressed_window_start`) and is changed **only inside the `IMMEDIATE`
transaction that also appends the event it governs**, so concurrent processes are
serialised by the database lock. Procedure, executed atomically per failed attempt:

```
now' = max(clock, ts_high_water, auth_window_start)   # a clock that moved BACK never reopens a window
if auth_window_start == 0 or now' >= auth_window_start + 60:      # window rolled
    if auth_suppressed > 0:  append auth.failures_suppressed{count, window_start = auth_suppressed_window_start}
                             auth_suppressed = 0
    auth_window_start = now'; auth_window_count = 0
if auth_window_count < 60:  append auth.failed;  auth_window_count++
else:                       if auth_suppressed == 0: auth_suppressed_window_start = auth_window_start
                            auth_suppressed++        # no event; committed in this small transaction
commit
```

* **Atomicity.** The summary event and the zeroing of `auth_suppressed` are in one
  transaction, so a crash can never zero the counter without the summary, nor emit
  a summary and keep the count. A crash before commit leaves the previous state:
  the attempt is simply *not counted and not recorded* (and the caller was told it
  was denied, not that it was recorded).
* **Concurrency at the window boundary.** Two processes straddling the boundary are
  serialised by the lock: the first rolls the window and emits the summary for the
  closed one; the second sees the new window. Exactly one summary is emitted per
  suppressed batch (tested with many processes).
* **Eventual summarisation without further failures.** Every audit append (any
  action) runs the *flush half only* of the procedure first: if `auth_suppressed > 0`
  and `now' >= auth_window_start + 60`, it appends `auth.failures_suppressed` and zeroes
  the counter in the same transaction; it does **not** start a new window (a window starts
  only at the next failed attempt). So ordinary traffic flushes a pending
  count. With no traffic at all, the pending count stays in `audit_state` and is
  shown by `audit status` and `audit verify` ("N suppressed failed attempts since
  `<ts>` not yet summarised") — durable, not lost on restart.
* **Database write failure (F2).** If the transaction cannot commit, the attempt is
  still denied, **nothing is claimed as counted or recorded**, and only the
  in-memory diagnostic counter moves. After the database recovers, the persisted
  numbers are exactly those of committed attempts.
* **Clock regression.** `max(clock, ts_high_water, auth_window_start)` means a backwards clock can
  neither reset the ceiling nor produce a negative window. While the guard is
  tripped (§6.4) the attempt is refused with `ErrClockRegression` and goes through
  the **same bounded path** (reason `clock_regression`), so refusals cannot flood
  the table either. **Cost, stated plainly:** while the clock is behind, the window cannot
  roll, so after the 60th failure of the current window the remaining attempts are only
  counted (not individually recorded) until the clock (or `ts_high_water`) passes the window
  end; the count is preserved and later summarised, so nothing is lost, only coarser.
* **What "counted" means (F-15).** An attempt is *counted* iff the transaction that updates the audit
  bookkeeping for it (the `auth.failed` event, or the `auth_suppressed` increment) **committed**. Attempts
  denied while auditing is unavailable (F1/F2) are **not** counted and are outside the invariant; the
  per-operator lock-out (`failed_count`, §3.1) lives in `operators` and commits in the *same* transaction
  as the audit bookkeeping when auditing works, and in a transaction of its own when it does not (so
  lock-out still bounds guessing, AC-FL-08). The attempt's caller is told "denied" in every case, and
  "recorded"/"counted" is only ever printed after a successful commit.
* **Accounting invariant** (property-tested), per window: `A_c ≤ auth.failed events + Σ
  auth.failures_suppressed counts + pending auth_suppressed ≤ A_c + K`, where `A_c` is the number of
  attempts whose caller observed a **committed** outcome and `K` the number of attempts whose process was
  killed or whose commit outcome was *unknown* to the caller (each can have committed or not, at most
  once). With `K = 0` the bound is an equality. This oracle needs no production test seam: the harness
  counts the commit confirmations it actually received.

Per-operator lock-out backoff (§3.1) is separate persisted state that bounds guessing
regardless of audit.

**Denial path** (§6.2): the denied event is committed in a transaction that
contains nothing else (except, when due, the pending `auth.failures_suppressed` summary flushed
ahead of it). If that commit fails the caller still receives
`ErrAccessDenied`; the failure is reported per F1/F2. A denial is never converted
into an allow, and an allow is never granted because auditing is unavailable.

**Crash mid-operation:** SQLite rolls back; either (mutation + event) exist or
neither.

### 5.8 Verification, quarantine and recovery: states, transitions and the `audit.verify` question

Three different things are easy to conflate and are kept apart:

* **Verification status** is a *computed property* of the data (a chain epoch is
  `OK`, `BROKEN`, `UNVERIFIABLE` or `HEAD_INCONSISTENT`). Computing it changes nothing.
* **Quarantine** is a *flag* (`audit_state.quarantined_at`) that stops the service
  appending. It is set only by explicit operator action.
* **An epoch** is a *partition of the event table* (all events sharing a `chain_id`).
  A new epoch is started only by an explicit break-glass operation; earlier epochs
  keep their events byte-for-byte.

#### Statuses

| Status | Meaning | How it is found |
|---|---|---|
| `OK` | every event of the epoch recomputes; head matches; all keys available; stamped timestamps non-decreasing; boundaries valid | `audit verify` |
| `BROKEN` | a MAC/link/sequence/field/boundary check fails, a timestamp decreases outside a recorded `clock_reset` (`TS-DECREASE`), or a supplied anchor is not contained in the chain | `audit verify` (exit 10: first bad `seq`, epoch, reason) |
| `UNVERIFIABLE` | an audit key version needed is unavailable: **environmental** (provider down / wrong root key / unreadable binding canary) or **data** (`audit_keys` row missing/corrupt) — classified by the procedure below | `audit verify` (exit 11) and the service (cannot seal) |
| `HEAD_INCONSISTENT` | `audit_state` disagrees with the newest event row (seq, MAC, chain id, key version, `ts_high_water`), or recomputing the newest event's MAC fails | the service on **every** append (cheap tail check) and `audit verify` (exit 12) |

`verify` also reports non-chain findings: pending suppressed-failure counts and clock
information (informational, never affect the exit code) and trigger-inventory differences
(`SCHEMA-TAMPER`, §7.3; an integrity finding because a protective control was removed; exit 15).

**Classifying `UNVERIFIABLE` (environmental vs data) — ordered, deterministic.** (1) Obtain the
binding canary and unwrap it with the configured provider. If the provider errors, the canary
does not unwrap (wrong root key), or the canary row is corrupt (`ErrBindingCorrupt`), the status
is `UNVERIFIABLE (environmental)`: the correctness of the root key cannot be established, so
nothing about the audit keys is concluded and `--latch` does not latch. (2) Only if the canary
unwraps is the root key known good; then a missing `audit_keys` row, or a wrapped audit key that
fails authenticated unwrapping, is `UNVERIFIABLE (data)`. A wrong root key is therefore never
reported as corruption, and corruption is never hidden behind a provider error.

#### Who may run these commands, including in degraded states (F-05, decision D23)

Operator login appends `auth.login`, so it is **refused** whenever the chain cannot take an
append (quarantined, `HEAD_INCONSISTENT`, `UNVERIFIABLE`, clock guard tripped) with the uniform
authentication error. Recovery therefore does not depend on a new login — **with one exception that the
owner's D23 decision creates and that is BLOCKED (B-1, below)**: `quarantine --release` may not use Mode R.
Two authentication modes exist:

* **Mode S (session):** a valid operator session holding the permission (`audit:read`,
  `audit:verify`, `audit:admin`). For read-only commands the session is validated *read-only*
  (no `last_seen_at` update, no append). Mode S consults expiry, so it is refused while the clock
  guard is tripped (§6.4).
* **Mode R (root-key proof):** proof that **the calling process can unwrap the store's root-key binding
  canary** with the configured `KeyProvider` (the same proof `reset-epoch` already uses). It is **not a user
  credential**: nothing is presented to or stored by the service, so there is nothing to replay or to
  expire, and its scope is exactly the command table below. It needs no session, no clock decision and no
  append. **Mode R is equivalent to database-owner-level authority** (§0): whoever can run it can also edit
  the database file and read the key.

| Command | Mode S | Mode R | Appends to the chain? |
|---|---|---|---|
| `audit status`, `head`, `list`, `export`, `verify`, `verify --state`, `verify --anchor` | yes (`audit:read`/`audit:verify`) | **yes** (read-only) | no |
| `audit verify --latch`, `audit quarantine` (incident-flagging: *set* the quarantine) | yes (`audit:admin`) | **yes** | no (flag + `audit_incidents` row only; `recorded_by` = operator id or `root-key-proof`) |
| `audit quarantine --release` (T5) | yes (`audit:admin`) — **the only permitted path; its availability is BLOCKED (B-1)** | **no — refused** (owner decision D23) | no (clears the flag + `kind=released` incident row) |
| `audit verify --record`, `audit rotate-key` | yes (`audit:verify` / `audit:admin`) | no | yes — healthy chain only |
| `operator recover`, `audit reset-epoch`, `clock reset`, `emergency revoke` | no | **yes** (existing break-glass) | yes (`reset-epoch`/`recover` as their sections state) |

**Authorization is decided per command by its most privileged flag**: `verify --latch` needs the
`--latch` row, `verify --record` the Mode-S-only row, and combining `--record` with `--latch` is a usage
error (exit 2); a Mode R invocation of any Mode-S-only flag is refused with the uniform authorization error
before any work. Mode R's **read-only** uses are not recorded in the chain (the chain may be exactly what is
unavailable): **this is a stated evidence gap, not a claim of auditing.** The flag commands leave an incident
row and a stderr line. `reset-epoch` also clears the flag, but as part of starting a new epoch (T6, the
documented break-glass with its own typed confirmation and adoption caveats); that is not the release
operation and does not make Mode R a release path. D23 records the decision (owner-approved, restricted).

**BLOCKED item B-1 — authorization path for `quarantine --release`.** D23 forbids Mode R from releasing,
so only Mode S (an `audit:admin` session) can. But a session cannot be created while quarantined (login
appends `auth.login`, which quarantine refuses), and an existing session expires after the 30-minute idle
limit (read-only validation does not extend it) or the absolute limit. After that **no permitted path to
release exists**; the only way out is `reset-epoch`, which adopts the damaged state. The owner has not
decided which of these to choose, so none is adopted: (a) *step-up re-authentication*: the command takes the
operator's username and password directly (hidden prompt), verifies the Argon2id hash without creating a
session or appending anything, requires `audit:admin`, bounds guessing with the existing persisted lock-out;
(b) *login while quarantined* with the session creation not recorded in the chain — rejected as a draft
option because every session row would then be an `ST-ORPHAN` and session creation would escape the audit
chain; (c) *accept the limitation*: release is available only while a pre-quarantine `audit:admin` session
is still valid, otherwise `reset-epoch`. Recommendation (not a decision): (a). Affected: T5, AC-VR-13,
AC-VR-19, AC-VR-23, F-05.

#### Exit codes (normative; `audit verify`, `verify --state`, `--anchor`, `--all-epochs`)

| Code | Meaning |
|---|---|
| 0 | `OK`: no finding of any kind in **the checks that were requested** (always chain, boundaries and trigger inventory; the state comparison only with `--state`) and nothing unverifiable |
| 10 | `BROKEN` (including `TS-DECREASE`, `BOUNDARY`, and an `--anchor` that is not contained in the chain) |
| 11 | `UNVERIFIABLE` (environmental or data) |
| 12 | `HEAD_INCONSISTENT` |
| 13 | the verification could not complete (I/O error); no verdict |
| 14 | `--state` findings (`ST-*` other than the chain-level conditions) on an otherwise `OK` chain |
| 15 | `SCHEMA-TAMPER` (trigger inventory differs) on an otherwise `OK` chain |

**Precedence** when several apply (the highest wins; **all** findings are still printed in text and
in `--json`): 13 > 12 > 10 > 11 > 15 > 14 > 0. `--state` on a chain whose status is not `OK`
**does not run the comparison** and returns that chain's code (10/11/12): events whose MACs cannot be
verified are never used as replay input. With `--all-epochs` the exit code is the highest-precedence
code over every epoch (the per-epoch codes are in the JSON). By default it is the code of the **current
epoch**. `ST-USE-AFTER-EXPIRY` and informational lines never change the code. These codes belong to
the `audit` read commands; all other commands use the codes of AC-CL-06 (1, 2, 3, 4, 5, 6).

#### `audit verify` is read-only — in every state

`audit verify` (and `head`, `export`, `status`) runs in a read transaction and **writes
nothing, appends nothing, repairs nothing**. Its result is the exit code (table above) plus
stdout/stderr (text or `--json`). The output always lists every earlier epoch as `HISTORICAL — not
trusted as authentic` with the status it had when it was closed (epoch ledger, below) and, if
possible, its present status. A failed or unverifiable verification therefore cannot extend or
"heal" a suspect chain. (The *service* may keep appending to a chain that `verify` reports as `BROKEN`
until someone latches it, because it only checks the tail — see the behaviour summary; `verify` itself
never writes unless a flag in the transition table asks it to.)

#### Is a successful verification recorded? (`audit.verify_recorded`)

Only on request and only when everything is clean: `audit verify --record` (Mode S,
`audit:verify`) verifies the whole current epoch in a read snapshot at head `H = (seq, mac)`;
**iff** the exit code is 0 and the chain is not quarantined, a **separate** `IMMEDIATE` transaction
re-checks that `quarantined_at IS NULL` and that the head is still `H` and then appends one event
`audit.verify_recorded{through_seq=N, head_mac=…}`; if the head or flag changed it appends nothing and
exits 1 ("chain changed during verification; re-run"). This is not recursive: the event lies *after* the
range it reports on, and the next verification covers it like any other event. It adds **no evidence**
(it says that the service's own code found the chain intact) — an independent auditor must run their
own `verify`. In every other state `--record` appends nothing and exits with the status's code.
`--record` together with `--latch` is a usage error (exit 2).

#### State transitions

| # | From | Trigger (who) | Guard | Writes | To |
|---|---|---|---|---|---|
| T1 | any | `audit verify` (Mode S or R) | — | **nothing** | status computed; no state change |
| T2 | verified | `audit verify --record` (Mode S) | exit code 0, not quarantined, head unchanged at append time | one `audit.verify_recorded` event, separate tx | unchanged |
| T3 | any | `audit verify --latch` (Mode S `audit:admin`, or R) | status `BROKEN` or `HEAD_INCONSISTENT`, or `SCHEMA-TAMPER`, or `UNVERIFIABLE (data)`. *Not* for `UNVERIFIABLE (environmental)` (a provider outage / wrong or unreadable root key; latching would only add a second problem) | `quarantined_at`; one `audit_incidents` row (no chain event); permitted by the `audit_state` trigger even when the head is inconsistent (§7.3) | Quarantined |
| T4 | any | `audit quarantine` (Mode S `audit:admin`, or R) | — | same as T3 | Quarantined |
| T5 | Quarantined | `audit quarantine --release` (Mode S `audit:admin` **only**; availability BLOCKED, B-1) | **all of the following inside ONE `IMMEDIATE` transaction** (D20 release policy): (a) `quarantined_at` is set and the head is the one that was read; (b) a full `verify` of the **current epoch** has exit code 0 (chain, boundaries, trigger inventory); (c) `verify --state` of the current epoch reports **no identity-state finding** — identity-state findings (`ST-*`, exit 14) **block** release; the one warning class `ST-USE-AFTER-EXPIRY` never changes the exit code and does not block, and no other class may be treated as non-blocking without a separately authorized policy; (d) only then the flag is cleared. Any failure rolls back, prints the verifier output and exits with the verification code | clears `quarantined_at`; adds an incident row `kind=released` | Verified |
| T6 | **any** (OK, BROKEN, UNVERIFIABLE, HEAD_INCONSISTENT, Quarantined) | `audit reset-epoch` (Mode R + typed confirmation) | database writable; the refusal cases of the corruption table | one transaction, below | **New epoch**; latch cleared for it |
| T7 | New epoch | `audit verify` | — | nothing | New epoch `OK` (or not) **independently of every old epoch** |

Because verification and the clearing happen in the same transaction, the head, the flag and the findings
are revalidated at the moment of the decision; nothing can change between them. Costs, stated: the write lock
is held for the duration of the verification (the service cannot append while quarantined, but other
writers wait), the audit-key provider is called inside the transaction (a provider failure ⇒ no release),
and identity-state findings **cannot be dismissed by an operator**: they are resolved only by restoring a
good backup or by `reset-epoch`, which adopts the current state and records `obs_state=findings:n` (§5.9). A
writer who can alter the database file can still clear the flag directly (§"latch and incident rows").

Not allowed, by construction: Suspect/Quarantined → `OK` by any route other than T5
(proof by fresh verification) or T6 (new epoch); any transition caused by `verify`
itself except the explicit flags T2/T3; appending while Quarantined except T6;
deleting or editing an old epoch. There is no `repair` command (CLI-tree test).

#### Epoch boundaries and the ledger (F-13)

The event table is one sequence: `seq` is gapless **across** epochs, starting at 1 with
`system.genesis` (`prev_mac` = 32 zero bytes) — or, only when a `reset-epoch` ran on an *empty* event table
(`obs_status=empty`), with that `audit.epoch_start` at `seq` 1. A new epoch begins at, and only at, an event with action
`audit.epoch_start`, which must have `prev_mac` = 32 zero bytes, a `chain_id` different from every
earlier event's, and `seq` = (previous event's `seq`) + 1. The verifier reports **`BOUNDARY`** (status
`BROKEN`, exit 10) for: a `chain_id` change at any event that is not an `epoch_start`; a `chain_id`
reappearing after a different one (non-contiguous partition); an `epoch_start` with a non-zero
`prev_mac` or a reused `chain_id`; two `epoch_start` events with the same `chain_id`. A `seq` gap or
reorder immediately before an `epoch_start` is attributed to the **earlier** epoch (its tail is what is
missing) so one tampered old epoch does not turn the current epoch `BROKEN`; `--all-epochs` exposes it.
Each epoch is verified independently of the others once its boundary conditions hold.

`verify` and `export` always print a ledger: `epoch 1 — HISTORICAL — not trusted as
authentic — closed with observed status BROKEN at seq 4021`; `epoch 2 — CURRENT — OK`.
The labels cannot be removed by a later reset or verification.

#### `reset-epoch` (T6) — also when the database is already inconsistent

Preconditions: Mode R (the binding canary unwraps with the configured provider; otherwise refused
**before any write**) and a typed confirmation that lists the findings below; no operator session is
needed. Nothing is assumed about the old chain. **Ordering inside the one `IMMEDIATE` transaction
(written to satisfy the §7.3 triggers):**

1. **Observe (read-only):** compute the old epoch's status with the verifier; read `obs_last_seq =
   MAX(seq)` and `obs_last_mac` = the stored `mac` of that event **directly from `audit_events`** (not
   from `audit_state`, which may be the broken part); read the head recorded in `audit_state`
   (`state_head_seq`, `state_head_mac`, possibly missing); run `verify --state` and keep only its
   classification (`clean | findings(n) | unverifiable`); when the chain is not `OK` the comparison does not run
   (§5.9) and the classification is recorded as `unverifiable`.
2. **Insert the new audit key** (`audit_keys`): version = `max(MAX(audit_keys.version),
   MAX(audit_events.key_version)) + 1` (so a deleted key row can never cause a version to be reused),
   random, wrapped under the current provider — no access to any old audit key is needed. The previous active
   key row, if present, gets `retired_at` set (NULL → value, allowed by the `audit_keys` trigger).
3. **Insert `audit.epoch_start`** (the epoch-start branch of the `audit_events` trigger): `seq =
   MAX(seq)+1` (or 1 for an empty table), `prev_mac` = 32 zero bytes, a fresh random `chain_id` that
   differs from every existing `chain_id` (re-drawn on collision), `ts` = `max(now, ts_high_water)`
   (never lowered), carrying only **observations**: `obs_status`, `obs_first_bad_seq`, `obs_last_seq`,
   `obs_last_mac`, `obs_state_head_seq`, `obs_state_head_mac`, `obs_state`
   (`clean|findings:n|unverifiable`), `reason` (`tamper|key_lost|head_inconsistent|operator_request`),
   and `baseline=` counts (§5.9).
4. **Move the head:** `UPDATE audit_state` (or `INSERT` it if the row is missing) to the
   `epoch_start` event — new `chain_id`, `last_seq`, `last_mac`, `active_key_version`,
   `ts_high_water`, `quarantined_at = NULL` (allowed because the head event is an `epoch_start`).
5. **For each baseline entity (§5.9):** `INSERT` its adopted event (now the *normal* insert branch
   applies: `seq = last_seq+1`, `prev_mac = last_mac`, `chain_id = audit_state.chain_id`), then
   `UPDATE audit_state` by exactly +1. The number of adopted events must equal the `baseline=` counts.
6. Add the `audit_incidents` row; `COMMIT`. Any failure rolls everything back (F2); the previous situation
   — including a quarantine, the old key set and the old head — remains, and no new key version or event
   exists (AC-VR-18).

**Corruption cases and their deterministic outcomes.**

| Case | Outcome |
|---|---|
| root key wrong / provider unavailable | refused before any write |
| empty `audit_events` | allowed; `seq` starts at 1; `obs_status=empty`; `obs_last_*` omitted |
| malformed old rows (NULL or wrong-length `mac`, bad field types) | observations are read tolerantly (`obs_last_mac` = hex of a 32-byte blob, else the literal `invalid`); the old epoch is reported `BROKEN`; reset proceeds |
| `MAX(seq) = 2^63−1` (no next number) | **refused**; restore a backup (documented limit) |
| duplicate / non-contiguous / inconsistent epoch boundaries (`BOUNDARY`) | old data reported `BROKEN`; reset proceeds with a `chain_id` unlike every existing one |
| `audit_state` row missing | step 4 inserts it |
| `audit_keys` row missing or corrupt | new key does not need it; version rule of step 2 |
| already quarantined | allowed; flag cleared by step 4 |

**What the recorded observations are, precisely.** They are what the *resetting
process* saw at that moment. That process runs with root-key power, so the values are
**not proof** that the old chain was authentic, nor that it had been tampered with, and
they are not tamper-evident against the same host administrator. Their purpose is an
operator-visible note attached, with a valid MAC, to the new epoch. A reset never makes
older history trustworthy: even `obs_status=ok` only means "looked intact to this
verifier at this time"; only an independently held anchor (§5.6) can raise trust in
older events.

#### The latch and incident rows are defence in depth

Anyone who can write the database can clear the latch or edit `audit_incidents`. They
make a *detected* problem stop the service from piling more events onto a suspect
chain; they are neither evidence nor a security boundary. The evidence is the
verifier's own output.

#### `operator recover` and the other break-glass operations; quarantine (F-21)

`operator recover` (new `instance_admin`), `clock reset` and `emergency revoke` (§6.4) append events, so
they are **refused while quarantined**: reset the epoch first (T6). `clock reset` and `emergency revoke`
are, in addition, permitted while the clock guard is tripped. **Why refusing is safe:** every use of a
session or token operation that **reads a secret value or mutates anything** is itself an audited append,
and quarantine refuses appends, so no credential — genuine or forged — can read a secret value or change
anything while the chain is quarantined; revocation can wait for the reset, after which `emergency revoke`
works. Alternatives considered and **not** adopted: revoking while
quarantined with only an incident row (the replay would then see an unrecorded revocation), and
appending to a quarantined chain (defeats the purpose). Recorded under D20.

#### Quarantine and metadata listings (decided: OQ-D20-1)

While the chain is quarantined — and in the other degraded states in which appends are refused — the
service distinguishes degraded-state diagnostics from normal operation as follows.

* **Denied:** every secret **read** (`secret get`, token reads), every **mutation** of projects,
  environments, secrets, grants, tokens, operators and sessions (all need an audited append), `login`, and
  the appending break-glass operations except `reset-epoch`. Security **bookkeeping** that bounds guessing
  and abuse — the per-operator lock-out counters and the failed-auth window counters — is not a policy
  mutation and continues, because refusing it would weaken protection. The only other writes allowed are
  the recovery operations of the transition table (T3, T4, T5, T6).
* **Allowed (read-only, unaudited metadata listings):** exactly `project list`, `environment list` and
  `secret list`, for a principal that is currently valid and holds `secret:list` on the scope, by session
  or by service token. **Fields returned, and no others:** project `id, slug, name, created_at`;
  environment `id, project_id, slug, name, created_at`; secret `id, project_id, environment_id, name,
  created_at, created_by, latest_version, latest_version_disabled, version_count`. **Never** returned:
  secret values, ciphertext, nonces, wrapped keys or key ids, password or secret hashes, commitments,
  token or session secrets, audit keys, root-key material, version comments or any free text. Listings do
  not update `last_seen_at` or `last_used_at` (no write), so they cannot extend a session.
* **Not available while quarantined:** identity and administrative listings (`operator list`, `grant
  list`, `token list`) and `secret` version listings — a conservative choice by this revision (the owner
  fixed only that metadata listings are allowed and what they must not expose); widening it needs a
  separate decision.
* **Audit behaviour:** listings are not audited (D12) in any state. A *denied* listing normally writes a
  `denied` event; while quarantined it cannot (F1), so the refusal is reported on stderr as "denied; audit
  event NOT recorded" and nothing is claimed as recorded.
* **Broken or untrusted chain:** listings keep working when the chain is `BROKEN`, `UNVERIFIABLE`,
  `HEAD_INCONSISTENT` or quarantined, but their authorization (grants, sessions, tokens) is read from
  identity tables that the chain can no longer vouch for; the exposure is limited to the metadata fields
  above. When the clock guard is tripped, session and token authentication is refused, so listings are
  refused too (§6.4).

#### Behaviour summary

| Situation | `verify` | Service (appending operations) | Recovery |
|---|---|---|---|
| `OK` | exit 0 | normal | — |
| `BROKEN` | exit 10, first bad seq; writes nothing (unless `--latch`) | **continues** — the service checks only the tail, so a break in the middle is not seen by it — until latched (**recommended: latch**) | restore backup (then T5), or T6 |
| `UNVERIFIABLE` — environmental | exit 11 | refuses (`ErrAuditUnavailable`) | restore the provider/key; no database change |
| `UNVERIFIABLE` — data | exit 11 | refuses | restore backup, or T6 (new key; root key still needed) |
| `HEAD_INCONSISTENT` | exit 12 | refuses on the next append (`ErrAuditIntegrity`), automatically | restore backup (then T5), or T6 |
| Quarantined | whatever the data says | refuses secret reads, mutations, `login` and every append except T6; the three metadata listings keep working (§5.8) | T5 (Mode S only; verification, `--state` and clearing in one transaction; availability BLOCKED, B-1) or T6 |

### 5.9 Identity-state replay: `audit verify --state` (D13 — specified)

**Purpose and limit.** Detect, after the fact, that identity tables no longer match
the history recorded in the chain. It is *detective*: it does not stop a database
writer, and it is exactly as trustworthy as the chain it replays (§5.5): a party
holding the audit key can forge rows **and** matching events.

#### Credential commitments: what they are and their complete key lifecycle (D21)

Events never contain a password hash, a token/session secret, or the stored hash of
one. They contain a **commitment**:

```
commit = HMAC-SHA256( K_state[v],  enc("dso/state-commit/v1") ‖ enc(label) ‖ enc(id)
                                   ‖ u32(v) ‖ material )   truncated to 128 bits (hex)
```

* **Which key.** `K_state[v]` is HKDF-SHA256 over the audit key **of version `v`**,
  with `salt = key_id[v]` and `info = "dso/audit/state-commit/v1"`: a third subkey,
  domain-separated from the event-MAC subkey (`…/event-mac/v1`) and the head-seal subkey
  (`…/head-seal/v1`). **No separate key is introduced**: a separate commitment key would
  need its own wrapping, rotation, backup and recovery paths and would add no isolation
  (an attacker who can obtain the audit key from the database + root key can obtain any
  key wrapped the same way). `label` is the credential kind and is **distinct per entity type**
  (F-19): `pw` (operator password hash), `sess-sc` (session secret hash), `tok-sc` (service-token
  secret hash); the `detail` key stays `pw`/`sc` and the label is fixed by the event's action. `id` is
  the owning entity id, `material` the exact stored `password_hash` string or 32-byte `secret_hash`.
  A commitment therefore cannot be moved between a session and a token even if their ids collided.
* **Version stored with the event.** `v` is the event's own `key_version` column — already
  covered by the event MAC and mixed into the commitment. The commitment scheme is implied
  by the event's `mac_alg` (`hmac-sha256-v1` ⇒ the scheme above); a future `mac_alg` defines
  its own scheme. A commitment can therefore not be re-labelled to another version, id,
  kind or algorithm.
* **After an audit-key rotation (version 1 → 2).** Nothing is re-committed. A credential
  last set while version 1 was active stays committed under `K_state[1]`; `verify --state`
  derives `K_state[1]` from the **retained** audit key v1 (audit keys are never deleted,
  §2.4). Credentials set after the rotation commit under `K_state[2]`. Comparison always
  uses the version recorded with *that credential's latest event*.
* **After an epoch reset.** The reset generates a new key version and the **adopted
  baseline events are committed under the new version, computed from the table values as
  they are at reset** (§ "Epoch baseline" below). The current epoch therefore never needs
  the previous epoch's audit keys to verify identity state — which is exactly the case
  where an old key may have been lost.
* **Missing or unwrappable key (F-09, deterministic).** Verifying the chain and verifying a
  commitment need the same audit-key versions: an epoch is `OK` only if every version it uses is
  available. If any required key version cannot be obtained (provider down, wrong root key,
  `audit_keys` row missing/corrupt) the current epoch is `UNVERIFIABLE` (classified environmental or
  data, §5.8) and **`verify --state` does not run the comparison**: events whose MAC cannot be checked
  are never used as replay input, so no partial "clean" report can exist. It prints which key versions
  are unavailable and which entities' latest events were written under them (informational; taken from
  the unauthenticated `key_version` column), reports `ST-UNVERIFIABLE`, and exits **11**, never 0. A
  commitment that cannot be checked can therefore never be summarised as clean (R27).
* **Backup and recovery.** The only key material involved is the audit key, which is
  already in `audit_keys` (wrapped by the root key) and therefore part of every database
  backup; its loss is handled exactly as in §2.5 (restore, or `reset-epoch`, whose new
  baseline is verifiable with the new key).
* **Rotation policy.** Retired keys remain wrapped and available; the cost is that a leak
  of an old audit key still allows forging old-version commitments/MACs — the same exposure
  as for event MACs, and covered by the same mitigation (rotate, anchor).
* **Truncation to 128 bits.** A commitment is compared for equality with a value the
  verifier recomputes under a key the attacker lacks; forging one requires a 2⁻¹²⁸
  guess, and birthday effects between commitments are irrelevant (they are never used as
  identifiers).
* **What a commitment reveals.** To anyone without the audit key: nothing about the hash.
  To anyone with it: they already hold the means to forge the whole chain (§5.5).

Rehashing a password in place (e.g. to raise Argon2 parameters) is **not** done silently
in M2; any change to `password_hash` goes through `operator set_password`, hence an event.

#### Replay state machine

State `S` (per epoch; starts empty at `audit.genesis`, or from the *adopted* events
of an `audit.epoch_start`). Events are applied in `seq` order; a precondition
violation (e.g. revoking something never created) is itself a finding
(`ST-SEQUENCE`). Fields are those of Appendix B.

| Event (success only) | Precondition | Effect on `S` | Detail fields used |
|---|---|---|---|
| `operator.create` | id unseen | `Op[id] = {username, admin, disabled=false, pw}` | `username`, `admin`, `pw` |
| `operator.set_password` | `Op[id]` exists | `Op[id].pw = pw` | `pw` |
| `operator.set_admin` | exists | `Op[id].admin = admin` | `admin` |
| `operator.disable` / `enable` | exists | `Op[id].disabled = true/false` | — |
| `auth.login` | `Op[operator]` exists, not disabled; session id unseen | `Sess[id] = {operator, exp, sc, revoked=false}` | `operator`, `exp`, `sc` |
| `auth.logout`, `auth.session_revoked` | `Sess[id]` exists | `Sess[id].revoked = true` | — |
| `grant.add` | `Op[operator]` exists; grant id unseen | `Grant[id] = {operator, project, env, role, revoked=false}` | `operator`, `project`, `env`, `role` |
| `grant.revoke` | `Grant[id]` exists | `Grant[id].revoked = true` | — |
| `token.issue` | token id unseen | `Tok[id] = {project, envs, perms, name, created, exp, sc, replaced_by=none, revoked=false}` | `project`, `envs`, `perms`, `name`, `created`, `exp`, `sc` |
| `token.rotate` | `Tok[old]` exists, `Tok[new]` was issued in the same transaction | `Tok[old].exp = old_exp (≤ previous)`, `Tok[old].replaced_by = new` | `new`, `old_exp` |
| `token.revoke` | exists | `Tok[id].revoked = true` | — |
| `system.emergency_revoke` | exists | revokes the named token/session or disables the operator (never the last live `instance_admin`) | `kind`, `id` |
| `system.clock_reset` | the `*_a/_b/_c/_kept` counts equal the number of following `token.revoke` events of the matching reason in the same transaction (otherwise `ST-SEQUENCE`) | none by itself: its effects are the individual `auth.session_revoked` / `token.revoke` events that follow it | `old_hw`, `new_hw`, `sessions_revoked`, `tokens_revoked_a`, `tokens_revoked_b`, `tokens_revoked_c`, `tokens_kept` |
| `system.recover` | — | followed in the same transaction by `operator.create` | — |
| `project.create` | project id unseen | `Proj[id] = {slug}` | `slug` |
| `env.create` | `Proj[project]` exists; env id unseen | `Env[id] = {project, slug}` | `project`, `slug` |
| any event with `adopted=1` | — | same effect as the creating event, **marked adopted** | as above |

Operators are never deleted, sessions and tokens are never purged, and grants are
only revoked in M2, so "row absent" is never legitimate.

#### What is compared (live tables vs `S`)

| Entity | Columns compared | Not compared (volatile) |
|---|---|---|
| `operators` | id set, `username`, `instance_admin`, disabled state, commitment of `password_hash` | `failed_count`, `locked_until`, `password_changed_at` |
| `operator_sessions` | id set, `operator_id`, `expires_at`, commitment of `secret_hash`, revoked state | `last_seen_at`, `origin` |
| `grants` | id set, `operator_id`, `project_id`, `environment_id` (`*` for NULL), `role`, revoked state | `created_by` |
| `service_tokens` (+ `service_token_envs`) | id set, `project_id`, the **set** of environment ids, `perm_set`, `name`, `created_at` (F-08: the clock-reset rules (b)/(c) read it, so it is authenticated by the `token.issue` event's `created`), `expires_at`/`no_expiry`, commitment of `secret_hash`, revoked state, `replaced_by` | `last_used_at` |
| `projects`/`environments` | id set, `slug`, and for environments the owning project id (F-02; structure only) | wrapped keys, names |

*Projects and environments (F-02).* They are replayed like everything else: `project.create` and
`env.create` events create them, and rows that existed before M2 are brought into the genesis epoch as
**adopted** events written by `upgrade` (§7.2), and into any later epoch by the reset baseline. Hence
legacy M1 rows are never `ST-ORPHAN`, a project/environment inserted afterwards without an event is, and
a deleted one is `ST-MISSING`.

A revoked state, once present in `S`, can never revert: a live-looking row for a
revoked entity is a finding regardless of what the row says.

#### Epoch baseline (what `verify --state` compares against after a reset)

`verify --state` replays **the current epoch only**. The state it starts from is:

* **genesis epoch:** empty, then the events written by `upgrade` (§7.2): `system.genesis`, one
  *adopted* `project.create`/`env.create` per pre-existing M1 project and environment, the first
  `instance_admin` through a normal `operator.create`, and `system.upgrade`;
* **any later epoch:** the **baseline** appended by `audit reset-epoch`, in the same
  transaction as `audit.epoch_start`.

**What the baseline contains.** One *adopted* creating event (`adopted=1`, Appendix B) for
**every row** of `projects`, `environments`, `operators`, `operator_sessions`, `grants` and
`service_tokens` (with its environment set and `replaced_by`) — live,
revoked and disabled alike, so legitimate pre-reset history is not mistaken for forgery
(a revoked row carries `revoked=1`/`disabled=1`; a restored-live copy of it later is still
a rollback finding). Each adopted event carries **exactly the compared fields** of §5.9's
table, including commitments computed under the **new** key version from the current
table values.

**How it is authenticated.** Each adopted event is an ordinary chain event (MACed,
linked, sequenced), appended in one transaction directly after `audit.epoch_start`.
`epoch_start` carries `baseline=proj:<n>,env:<n>,op:<n>,sess:<n>,grant:<n>,tok:<n>`; the verifier requires
exactly that many adopted events of each type to follow, and **only** adopted events
there. Omitting, adding or altering a baseline entity afterwards is therefore a chain
break (a missing link, a count mismatch, or a MAC failure) — no separate digest is needed.

**How subsequent events replay against it.** `S₀` = the fold of the baseline; later events
apply per the state machine above; the final `S` is compared with the tables. A legitimate
pre-reset entity appears in `S₀` and is neither `ST-ORPHAN` nor `ST-MISSING`; a row
inserted after the reset is `ST-ORPHAN`; a baseline row deleted after the reset is
`ST-MISSING`.

**What adoption does *not* establish (explicit non-detection).** The baseline records the
tables **as they were at reset time**. Tampering that happened **before** the reset —
forged rows, replaced hashes, cleared revocations — is **adopted as legitimate** and is not
reported by later `verify --state` runs. Two mitigations only: the reset first runs
`verify --state` and the confirmation prompt shows its findings; and `epoch_start` records
the classification `obs_state=clean|findings:n|unverifiable` as an *observation* (§5.8).
Operators who find findings at reset time should revoke and re-issue the affected
credentials rather than adopt them. `verify --state` reports adopted entities as
`adopted` in its output.

#### Findings (each with a code, the entity, the column and the first relevant `seq`)

| Code | Meaning | Typical cause |
|---|---|---|
| `ST-ORPHAN` | row exists, no creating event | forged/inserted operator, session, grant, token or token-env row |
| `ST-MISSING` | creating event exists, row absent | deleted row (removes access or hides a credential) |
| `ST-ATTR` | a compared column differs (names the column) | edited role/scope/expiry/admin flag/username; stale row restored |
| `ST-CRED` | commitment of the current credential value ≠ the latest event's | password hash or token/session secret hash replaced, or an older valid value restored |
| `ST-REVOKE-ROLLBACK` | event history says revoked/disabled, row is live | `revoked_at` cleared, or an old pre-revocation copy restored |
| `ST-USE-UNISSUED` | a **successful** event's actor (operator/session/token) has no creating event before it | a forged credential was *used* through the service |
| `ST-USE-AFTER-REVOKE` | a **successful** event's actor was revoked/disabled earlier in the chain | a revoked credential was resurrected and used |
| `ST-USE-AFTER-EXPIRY` (warning; never changes the exit code) | a **successful** event's actor was used at or after its recorded `exp` by the event's stamped timestamp | clock tampering or expiry bypass; stamped timestamps are non-decreasing (§5.4, §6.4) |

*Scope of the `USE-*` findings (F-16).* They consider only events with `outcome = success` whose
`actor_type` is `operator` or `service_token`. Denied and failed events (`token.use_denied`,
`auth.failed`, `secret.read` with `outcome=denied`, …) legitimately name revoked, expired or
non-existent credentials as the *attempted* actor and are **ignored** by these three checks
(AC-ST-22).
| `ST-SEQUENCE` | impossible transition in the chain itself | bug or tampering with event content that still verifies (needs the key) |
| `ST-UNVERIFIABLE` | a needed key version cannot be obtained, so the epoch is `UNVERIFIABLE` and no comparison runs | environmental or data loss; **never reported as clean** (exit 11) |

Exit codes are the single normative table in §5.8 (0 only when there are **no findings and nothing
unverifiable**; adopted entities are listed informationally; 14 for `ST-*` findings on an `OK` chain;
10/11/12 when the chain itself cannot be trusted, in which case the comparison does not run).

#### What this can and cannot establish — claims limited to the recorded data

Can establish (when the chain verifies and the attacker lacks the audit key):
forged/inserted rows; deleted rows; edited scopes, roles, flags, expiries; password
or secret-hash replacement or rollback; cleared revocations and restored stale rows
(**including the case where the attacker later re-deletes or re-reverts the row**,
as long as the *use* left events: `ST-USE-*`).

**Cannot** establish, and the documentation must not claim:

* anything about **volatile columns** (`last_used_at`, `last_seen_at`, `failed_count`,
  `locked_until`): a database writer can reset lock-out counters undetected;
* a **coherent rollback**: restoring old table rows *and* truncating the chain tail
  to the matching point (the revocation event is gone with them) is invisible without
  an external anchor (§5.6);
* anything done by a party holding the audit key (forged events + rows);
* *who* changed a row or *when*; whether a forged credential was used **before** the
  evidence of its use was truncated;
* the state of entities **adopted** at an epoch reset (trusted as found);
* whether a secret was read by bypassing the service (§0);
* correctness of a password hash itself (only that it matches what was recorded).

#### Completeness obligation (so legitimate use produces zero findings)

Every `Tx` method that writes an identity table is reachable only from service
functions that append the corresponding event **in the same transaction** (a test
lists the identity-writing `Tx` methods and the events that must accompany them; a
randomised legitimate workload followed by `verify --state` must report no findings).

## 6. Transactions

### 6.1 Unit of work (M2.1 — implemented, commit `8c3763c`)

This section describes what is **committed**, verified against `internal/store/store.go`,
`internal/store/sqlite/tx.go` and `sqlite.go`; what M2.1 does *not* contain is stated explicitly.

```go
// store.Store
WithTx(ctx context.Context, fn func(ctx context.Context, tx Tx) error) error   // one IMMEDIATE transaction
```

* **`Tx`** is `store.Operations`: **the 19 existing data/metadata operations**, same semantics and
  `mapErr` error mapping, running inside the open transaction. `SchemaVersion` and `Close` are not
  in `Tx`. The per-operation bodies are written once against the method set that `*sql.DB` and
  `*sql.Tx` share, so a `Tx` method and the corresponding standalone method send the same statements.
* **Lifecycle.** `WithTx` opens the transaction with the existing `s.tx` helper (`BeginTx` with the DSN's
  `_txlock=immediate`): commit when `fn` returns nil; roll back on an error from `fn`, on a cancelled
  context, and on a panic (which is propagated, not swallowed); the store stays usable afterwards.
* **The 4 operations that used `s.tx` in M1** (`CreateSecret`, `AddVersion`, `DisableVersion`,
  `PutMetaIfAbsent`) now delegate to `WithTx`; input validation still precedes the transaction and
  `DisableVersion` still returns its errors unmapped, as in M1.
* **The 15 autocommit operations keep their M1 behaviour**: the same statements, no added `BEGIN`. The
  three two-statement reads (`GetVersion`, `GetLatestVersion`, `ListVersions`) still send two separate
  autocommit statements outside `Tx` (a version committed between them is observed; a version disabled
  between them gives `ErrVersionDisabled`); inside `Tx` they share the transaction's snapshot.
* **Context marker (OQ-TX-1 option (a)).** `WithTx` hands `fn` a derived context marked with the open
  transaction of **this `Store` instance**. A nested `WithTx`, or any of the 19 standalone operations,
  called with that context (or one derived from it, from any goroutine) while the transaction is open
  returns `store.ErrTxActive` before touching the database. After the transaction ends the context is
  no longer rejected. **Not detected, by design:** an unrelated or discarded context, a different
  `Store` instance, another process. These remain independent callers with M1 behaviour: a standalone
  write waits for the write lock and then either succeeds (if the transaction ended first) or fails with
  the raw driver busy error at `busy_timeout` (default 5 s; the wait ends somewhat earlier than the
  configured value); a standalone read does not wait (WAL) and sees committed state, outside the
  transaction. The pool allows up to 8 connections; no gate, retry loop or lock protocol was added.
* **Evidence status.** Tests were executed and passed for AC-TX-01, 01a, 02, 03, 04, 04a, 04b-1, 04b-3,
  04c, 04d, 04e, 05 and 06 (independent review of M2.1). **AC-TX-04b-2 is unproven and its test is
  skipped, never passed:** no deterministic observation of SQLite's internal lock wait exists without a
  production seam outside M2.1.
* **Not in M2.1** (added only when the milestone that needs it is separately authorized): principal,
  grant and token queries; `AppendAudit`; tables, triggers, migrations. Authorizing or committing M2.1
  does **not** authorize M2.2.

Later milestones use `WithTx` as the transaction template of §6.2; the service layer must obtain the
store only through `WithTx` for audited operations — a convention backed by tests in those milestones
(not enforced by M2.1, which cannot detect an unrelated context).

### 6.2 Operation templates

*Protected mutation:*
```
WithTx:
  p := validatePrincipal(tx, principal)          // session/token live? operator enabled?
  scope := resolve(tx, project, env)             // missing ⇒ deny uniformly
  authorize(tx, p, perm, scope)                  // else ⇒ ErrAccessDenied (see below)
  apply mutation (e.g. AddVersion with seal callback)
  AppendAudit(success event)                     // MAC computed inside the tx
commit  ⇒ mutation and event become visible together, or neither does
```

*Denial:* `fn` returns a typed `denied{reason}`; the wrapper opens (or reuses) a
transaction that contains **only** the denied event and commits it, then returns
`ErrAccessDenied`. No mutation can have happened, so nothing can diverge.

*Secret read:* authorise → fetch ciphertext → append `secret.read` event →
commit → decrypt → return plaintext. Plaintext exists only after the event is
durable. A crash between commit and return over-reports (event without delivery),
which is the safe direction. A decryption/integrity failure afterwards appends a
follow-up `error` event (best-effort; the caller gets the error regardless).

*Token use:* the same template; `last_used_at` updates in the same transaction.

### 6.3 Why this is sufficient

* Gapless `seq` + `prev_mac` are computed under the write lock, so no two events
  share a predecessor.
* Authorisation and mutation share a snapshot, closing TOCTOU.
* SQLite's `IMMEDIATE` lock gives cross-goroutine and cross-process serialisation
  (M1 already tests multi-process writers).

Cost: audited reads serialise. Acceptable for a local milestone; M3 must revisit
(group commit, read-event batching with explicit semantics) **before** any
throughput claims.

### 6.4 Time and the clock-regression guard (D15 — revised; D24)

**Definitions (used identically everywhere in both documents).**

| Symbol | Meaning |
|---|---|
| `now_c` | the host wall clock (`time.Now` through the injectable clock); never trusted, never validated against NTP |
| `hw` | `audit_state.ts_high_water`: a durable, **monotone** time reference, always equal to the stamped `ts_unix_nano` of the newest audit event |
| `now_eff` | `max(now_c, hw)`: the only time used for expiry decisions |
| `tolerance` | 5 minutes (fixed in M2; D15 number) |
| `L_max` | 365 days: the maximum token lifetime at issuance (D5) |

**Invariants (F-01).**

1. **Monotone reference.** `hw` is initialised by the genesis event (`hw = max(now_c, 0)` = the genesis event's
   stamped time), advanced by **every** append in the same transaction as the append, and never decreases
   except through `system.clock_reset`. The `audit_state` trigger rejects a decrease unless the head event is
   a `system.clock_reset` (§7.3); `verify` reports a decrease anywhere else as `TS-DECREASE` (§5.4); the tail
   check requires `hw` = the newest event's `ts`.
2. **Stamping.** Every event is stamped `ts = max(now_c, hw)` (so an event appended with an old clock reading
   cannot lower `hw`; appending refused attempts, denials, logouts and break-glass events while the clock is
   behind leaves `hw` unchanged). The only event stamped below `hw` is the `system.clock_reset` event, with
   `ts = new_hw`, and its MACed detail names `old_hw` and `new_hw`.
3. **Expiry evaluation.** A session or service token is valid iff `now_eff < expires_at` (strict: at
   `now_eff == expires_at` it is expired); the session idle limit is `now_eff < last_seen_at + 30 min`, with
   `last_seen_at` written from `now_eff`. Every place that decides expiry — authentication, `token list`,
   the clock-reset rules — uses this one definition. **Issuance** uses the same time base: `created =
   now_eff` and `expires_at = created + TTL`, so a stored lifetime is exact. Because `now_eff ≥ hw`, a clock
   that has been set back, **including by less than the tolerance**, cannot make a credential valid that had
   already expired **as of `hw`** (the newest recorded event). **Stated limit (independent re-review N-01):**
   `hw` only advances when an event is appended, so a credential that expired *after* the newest event and
   before a rollback that stays later than that event is protected only by the real clock, not by `hw`. The
   guard cannot see such a rollback. Mitigation is limited to the fact that every credential *use* appends an
   event (so `hw` follows active credentials closely); an optional periodic heartbeat event would narrow the
   window and is recorded as an alternative under D24, not adopted. **Frozen-time bound (N-05):** while the
   clock catches up after a backward step, `now_eff` stays pinned at `hw`; operations are refused beyond the
   tolerance (guard tripped), so any extension of a credential's real-time validity is at most `tolerance`
   (5 minutes).
4. **Guard.** The guard is *tripped* iff `now_c < hw − tolerance`.

**The risk.** Setting the clock back would make expired sessions and tokens look valid again. Invariants
1–3 prevent that for credentials that had expired **as of `hw`**, and the guard makes a regression below
`hw − tolerance` visible and blocks the operations whose result would be misleading. They do **not**
remove the risk in every rollback scenario (the accepted limitations below).

**Accepted limitations (owner decision D24, N-01 and N-02).** (1) *N-01:* a rollback that stays later than
the newest recorded event cannot be detected, because `hw` advances only when an event is appended; a
credential that expired after that event is protected only by the real clock until further evidence is
recorded. (2) *N-02:* a transient forward jump followed by any append pins `hw` ahead and can require the
break-glass `clock reset`, which revokes every session and token its rules cover, including legitimate ones.
(3) *Heartbeat events are not part of this revision.* A periodic heartbeat that would narrow (1) is recorded
as a **future option** needing a separate cost/benefit evaluation (detection gap and write overhead must
first be quantified); it is not designed, scheduled or decided. These limits are kept visible in the
security model (§0, §9), risk R34/R35 and AC-CK-17.

**Which operations are affected when tripped** (`ErrClockRegression`):

| Operation | Tripped | Why |
|---|---|---|
| login; session or token authentication; **every** operation by a session or token principal (secret read/write, structure, grants, token issue, …) | **refused** | all consult expiry; their result would be wrong |
| `logout` of the caller's own session (no expiry decision needed, only reduces access) | allowed | monotone-safe |
| revoking tokens/sessions/grants, disabling operators **through the normal path** | refused (needs an authenticated session) | session validity cannot be judged |
| the same revocations through **`emergency revoke`** (break-glass, Mode R) | allowed | monotone-safe; audited as `system.emergency_revoke` |
| `audit verify`, `head`, `export`, `status`, `--state`, and the flag commands `--latch`/`quarantine` | allowed **by Mode R (root-key proof)**; the session path (Mode S) is refused because it consults expiry | no expiry decision is needed (F-05) |
| `--record`, `rotate-key` | refused (Mode S only) | need an authenticated session |
| `operator recover`, `audit reset-epoch` (break-glass) | allowed | Mode R; neither consults expiry |
| `clock reset` (below) | allowed — it *is* the recovery | Mode R |

Refused attempts, denials and break-glass events written while tripped are stamped `max(now_c, hw)` (invariant 2).

**Recovery — two safe paths; no path simply lowers `hw`.**

1. **Wait.** If the skew is small (VM restore, brief wrong clock), real time passes `hw − tolerance` and the
   guard clears itself. No state changed, and during the wait `now_eff` stayed at `hw`, so nothing expired
   could be revived.
2. **`dso central clock reset` (break-glass, Mode R).** For a large or unexplained skew (for example the
   clock was set far ahead, credentials were issued, and the clock was then corrected).
   * **Authorization and validation.** Proof of root-key possession and a typed confirmation that shows
     `now_c`, `hw`, the gap, and the counts each rule below would revoke. It is **refused unless the guard
     is tripped** (`now_c < hw − tolerance`; otherwise there is nothing to reset). By running it the
     operator asserts that `now_c` is the correct time; the procedure treats `now_c` as truth.
   * **Atomicity and audit.** One transaction; one `system.clock_reset{old_hw, new_hw, sessions_revoked,
     tokens_revoked_a, tokens_revoked_b, tokens_revoked_c, tokens_kept}` event plus one replayable
     revocation event per credential (§5.9). The event is refused while quarantined (§5.8).
   * **Order (so nothing is ever revivable):** (1) the revocations below are performed first; (2) the
     `system.clock_reset` event is appended with `ts = new_hw = now_c`; (3) `hw := new_hw`.
   * **Sessions:** every session is revoked (`auth.session_revoked`, reason `clock_reset`).
   * **Tokens — revoked (permanently, never re-validated), classified from the authenticated `created`
     and `exp` of the `token.issue` event (replay compares the table columns against them, F-08):**
     * **(a) no revival.** `expires_at ≤ hw` (old `hw`) — the token had already expired as of the highest
       time ever recorded; its expiry is *materialised as revocation* (`token.revoke`, reason
       `expired_at_reset`), so no clock setting can bring it back.
     * **(b) no extension.** `created_at > now_c + tolerance` — it was issued while the recorded time was
       *ahead* of the corrected time, so its absolute expiry was computed from a clock that `now_c` now
       contradicts; it is revoked (reason `clock_skew`). **Stated consequence (F-06):** because `now_c` is
       treated as truth, rule (b) also revokes a token legitimately issued shortly *before* a backward
       step (δ), if its `created_at` lies more than `tolerance` after `now_c`. That is deliberate and
       conservative (re-issue); the confirmation prompt shows the count so the operator can abort. The
       alternative — an operator-chosen "trusted" direction — is recorded as a rejected option (more
       surface, easy to choose wrongly).
     * **(c) policy violation.** `expires_at − created_at > L_max + tolerance` for a token that is not
       `no_expiry`. The service never issues such a token, so this indicates a forged or corrupted row
       (reason `policy`). It is a sanity rule, **not** a clock rule.
   * **Tokens — left valid, expiry untouched:** every other token (`created_at ≤ now_c + tolerance` and
     `expires_at > hw`). Their absolute `expires_at` is **neither extended nor shortened**. Because
     `expires_at > old hw > new_hw`, none of them had expired as of the highest recorded time, so no
     expired credential becomes valid; `no_expiry` tokens are not touched by (a) or (c) but are subject to
     (b). The remaining tokens are **listed** in the output and recorded as counts; `--revoke-tokens`
     revokes all of them.
   A *backwards* step therefore revokes tokens that expired within the highest time ever seen (rule (a))
   and tokens created within the last δ (rule (b)); a *forward* error revokes everything issued during it
   (rule (b)). Both err on the side of re-issuing. Nothing in the procedure extends a lifetime or revives
   an expired credential.

**Limits.** A clock set *forward* is not detected as a regression (it advances `hw`, so it would expire
credentials early — a nuisance, not a bypass — and the guard then trips once the clock is corrected, which
is the case `clock reset` exists for). **Availability consequence (N-02):** a transient forward jump of Δ
followed by *any single append* (even a failed login) pins `hw` Δ ahead; after the clock is corrected the
guard stays tripped until the root-key-proof `clock reset` (or Δ of waiting), and that reset revokes every
session and, by rule (a), every token with `expires_at ≤ hw` — i.e. **all tokens expiring within Δ, including
legitimate ones issued before the jump** — plus, by rule (b), those issued during it. This is deliberate
(never revive, never extend) but costly; the confirmation shows the counts. A rollback that stays later than
the newest event is not detected at all (invariant 3). A host administrator can still lie about time to the process; the
guard defends against honest mistakes and casual rollback, not against `root`.

## 7. Schema and migration

### 7.1 Migration `0003_identity_audit.sql` (forward-only)

New tables: `operators`, `operator_sessions`, `grants`, `service_tokens`,
`service_token_envs`, `audit_keys`, `audit_events`, `audit_state`. Sketch
(final DDL is written test-first in the plan):

```sql
operators(id PK, username UNIQUE, password_hash NOT NULL, instance_admin INT NOT NULL DEFAULT 0,
          disabled_at, failed_count INT DEFAULT 0, locked_until, created_at, created_by,
          password_changed_at)
operator_sessions(id PK, operator_id FK, secret_hash BLOB(32) NOT NULL, created_at,
          expires_at NOT NULL, last_seen_at, revoked_at, origin)
grants(id PK, operator_id FK, project_id FK, environment_id NULL, role CHECK(role IN
          ('project_admin','writer','reader','auditor')), created_at, created_by, revoked_at, revoked_by,
          FOREIGN KEY(environment_id, project_id) REFERENCES environments(id, project_id))
          -- live uniqueness: partial unique index (operator, project, environment, role) WHERE revoked_at IS NULL
service_tokens(id PK (hex), secret_hash BLOB(32) NOT NULL, project_id FK, name, perm_set CHECK(perm_set IN
          ('read','read+list')), created_by FK operators, created_at, expires_at, no_expiry INT DEFAULT 0,
          last_used_at, revoked_at, revoked_by, replaced_by,
          CHECK(expires_at IS NOT NULL OR no_expiry = 1))
service_token_envs(token_id FK, environment_id, project_id, PRIMARY KEY(token_id, environment_id),
          FOREIGN KEY(environment_id, project_id) REFERENCES environments(id, project_id))
audit_keys(version PK, key_id UNIQUE, wrapped_key NOT NULL, root_key_id NOT NULL, created_at,
          activated_seq NOT NULL, retired_at)
audit_events(seq PK, ts_unix_nano, chain_id, key_version FK, mac_alg, actor_type, actor_id, actor_label,
          session_id, action CHECK(...), outcome CHECK(...), reason, project_id, environment_id,
          resource_type, resource_id, resource_name, secret_version, request_id, origin, detail,
          prev_mac BLOB(32), mac BLOB(32))
audit_state(id PK CHECK(id=1), chain_id, last_seq, last_mac, active_key_version, ts_high_water, updated_at,
          quarantined_at, auth_window_start, auth_window_count, auth_suppressed,
          auth_suppressed_window_start)
audit_incidents(id PK, detected_at, chain_id, kind, first_bad_seq, detail, recorded_by)   -- informational, not in the chain
```

Triggers: the exact invariants and their limits are specified in §7.3.

### 7.2 Upgrade states (fail closed)

`store_meta` gains `m2.upgraded` (insert-only). After migration `0003` and before
the upgrade step completes the database is **"M2-pending"**: every data
operation returns `ErrUpgradeRequired` (the same fail-closed stance as
`ErrSchemaTooNew`). `dso central upgrade` then, in one transaction (and only
after `Bind` verifies the root key):

1. creates `audit_keys` v1 (wrapped, AAD per §2.3), `chain_id`, and the single `audit_state` row with its
   **initial values** (F-27): `last_seq = 0`, `last_mac` = 32 zero bytes, `active_key_version = 1`,
   `ts_high_water = 0`, `quarantined_at = NULL`, window columns 0;
2. writes the `system.genesis` event (actor `system`) through the **normal** insert branch of the
   trigger (§7.3): `seq = last_seq+1 = 1`, `prev_mac = last_mac` (zeros), `chain_id = audit_state.chain_id`,
   `ts = max(now, 0)`; the `audit_state` update then advances to `last_seq = 1` and `ts_high_water = ts`;
3. **adopts the existing M1 structure (F-02):** one `project.create` and one `env.create` event with
   `adopted=1` for every project and environment row that exists (none of their rows is modified);
4. creates the first `instance_admin` operator (credential from prompt/file) with its `operator.create`
   event;
5. writes `m2.upgraded` and a `system.upgrade` event.

A fresh `init` performs the same steps. The M1 pre-migration backup mechanism
takes a verified snapshot before `0003` as usual. **Existing M1 `created_by`
strings are left untouched** (they predate authenticated actors; docs say so).
Rollback = restore the snapshot; the M1 binary refuses the newer schema
(`ErrSchemaTooNew`).

**D9 — decided direction (approved in principle by the review): in-place upgrade
of M1 databases is supported, conditional on verification.** The upgrade (a) refuses
to start unless the pre-migration backup has been created **and verified**
(`integrity_check` ok, schema version recorded) — the M1 mechanism already
guarantees this, and the test in the plan proves the migration does not proceed
without it; (b) is exercised by an explicit **restore drill** test (restore the
snapshot, run the upgrade again); (c) leaves a database that no M1 binary will open.
No M1 database should hold real data — root-key escrow and backup/restore procedures
remain outstanding preconditions for that, independent of M2.

### 7.3 What the SQLite triggers enforce — and what they do not

Triggers see the rows being written and can read other tables; they cannot know
**who** is writing, cannot compute HMACs (no key in SQL), and cannot tell the
application's legitimate append path from any other writer that produces
structurally valid rows. Therefore:

**Invariants enforced by triggers (structural, row-relational):**

| Table | Enforced |
|---|---|
| `audit_events` | `UPDATE` aborts; `DELETE` aborts; `INSERT` aborts unless `length(mac) = 32` and either (normal event, **including `system.genesis`**) `seq = audit_state.last_seq + 1`, `prev_mac = audit_state.last_mac`, `chain_id = audit_state.chain_id` — **or** (`action = 'audit.epoch_start'`) `seq = COALESCE(MAX(seq in audit_events), 0) + 1`, `prev_mac` = 32 zero bytes and `chain_id` different from every existing `chain_id`. The epoch-start branch deliberately does not trust `audit_state`, so a reset works when `audit_state` is wrong or missing |
| `audit_state` — **head columns** (`chain_id`, `last_seq`, `last_mac`, `active_key_version`, `ts_high_water`) | `DELETE` aborts. An `UPDATE` **that changes any head column** aborts unless the new `last_seq` equals `MAX(seq)` in `audit_events` and the new `last_mac` equals that event's `mac`, with the head advancing by exactly 1 — **or** the head event is an `audit.epoch_start` (the head may jump and `chain_id`/`active_key_version` may change; the reset sequence of §5.8 moves the head to the `epoch_start` first and then advances by +1 per adopted event). `ts_high_water` may not decrease unless the head event is a `system.clock_reset`, and must equal the head event's `ts_unix_nano`. An `INSERT` is allowed only if no row exists and either the event table is empty (upgrade) or the newest event is an `audit.epoch_start` whose values the row carries (reset with a missing row) |
| `audit_state` — **bookkeeping columns** (`quarantined_at`, `updated_at`, `auth_*`) | **independent of head consistency** (F-04): an `UPDATE` that leaves every head column unchanged is always allowed, so `--latch`, `quarantine`, `--release` and the failed-auth window bookkeeping work exactly when the head is inconsistent. They are covered by the accounting invariant (§5.7) and by the transition rules (§5.8), not by the trigger |

| `audit_keys` | `DELETE` aborts; only `retired_at` may change (NULL → value) |
| `audit_incidents` | `UPDATE`/`DELETE` abort |
| `service_tokens` | immutable: id, `secret_hash`, project, `perm_set`, `name`, `created_*`; `revoked_at/by` and `replaced_by` NULL → value only; `expires_at` may only decrease; `last_used_at` free; `DELETE` aborts |
| `service_token_envs` | `UPDATE`/`DELETE` abort |
| `grants` | immutable except `revoked_at/by` NULL → value; `DELETE` aborts |
| `operator_sessions` | immutable except `revoked_at` NULL → value, `last_seen_at`; `DELETE` aborts |
| `operators` | `username`, `created_*` immutable; `DELETE` aborts |

**Enforced only by application code (the trigger cannot):** that a `password_hash`,
`instance_admin`, `disabled_at` or similar change came from `set_password`/`set_admin`/
`disable`; that a **valid MAC** accompanies an event (a forged `INSERT` with correct
`seq`/`prev_mac`/`chain_id` and a garbage `mac` is accepted by the trigger); that every
identity mutation is accompanied by its event; the hold-it and delegation rules;
principal validation; expiry. These are covered by the service's transaction
template, `verify` (MACs, links) and `verify --state` (identity rows vs events).

**Triggers are defence in depth against bugs and unsophisticated tampering. They are
not a security boundary against anyone who can write the database file**: such a
party can `DROP TRIGGER`, edit pages directly, replace the file, or `INSERT` rows that
satisfy the structural checks.

**Detecting removal.** `status` and `verify` compare the triggers present in
`sqlite_master` with the embedded expected set (name and a digest of the SQL) and
report any difference as an integrity finding (`SCHEMA-TAMPER`, exit 15 for
`verify` on an otherwise `OK` chain; precedence in §5.8). This catches casual removal; an attacker who also edits the check's
inputs (the file) is outside what a local check can detect.

**Tests (named in the plan, AC-TR-xx):** (1) with triggers: each forbidden `UPDATE`/
`DELETE`/mis-sequenced `INSERT` aborts; (2) with triggers **dropped** (as an attacker
would): the same tampering succeeds at SQL level, and `verify` / `verify --state` /
the trigger inventory each report it; (3) a structurally valid event with a wrong MAC
is **accepted by the triggers and rejected by `verify`** (pins the limitation);
(4) triggers never reject any legitimate operation (the full operation suite and a
randomised workload run with triggers on).

## 8. CLI

Command groups (all still local-only, all authenticated except `init`/`upgrade`/
`login` and the break-glass group, which authenticate by proof of the root key; the read-only `audit`
commands and the flag commands `--latch`/`quarantine` (setting) accept **either** a session **or** the
root-key proof, so they stay usable when login is impossible; `quarantine --release`, `--record` and
`rotate-key` are **session-only** — the table in §5.8, decision D23, with the release path BLOCKED, B-1):
`login | logout | whoami`; `operator create|list|disable|enable|set-password|set-admin`;
`grant add|revoke|list`; `token issue|list|revoke|rotate` (`--service-token-file`
on `secret get` for the machine path); `audit status|head|list|verify|export|rotate-key|quarantine [--release]`
(`verify` flags: `--state`, `--anchor seq:mac`, `--record`, `--latch`, `--all-epochs`, `--json`);
**break-glass (root-key proof, typed confirmation, always audited):**
`operator recover`, `audit reset-epoch`, `clock reset`, `emergency revoke`;
existing `project|environment|secret` commands, now acting as the logged-in
operator.

Rules (each a test):

* Passwords: hidden prompt or `securefs`-checked `--password-file`. Never argv,
  never env. Session token: `0600` file, path flag only.
* Token issuance prints the token **once** to stdout (or `O_EXCL 0600` file);
  nothing else on stdout; it is never logged and never in error text. The Go type
  holding it implements a redacting `String`/`GoString`/`MarshalJSON`.
* Errors for authentication/authorisation are the two uniform messages; details go
  to the audit log only. `--verbose` never adds secrets or reasons that distinguish
  "unknown user" from "bad password".
* `audit` output contains ids, labels, names and codes — no values, no keys, no
  hashes of secrets.
* `secret get --reveal` remains the only command that prints plaintext.
* Exit codes distinguish usage, authentication, authorisation and internal
  errors (stable, documented).

## 9. Threat model (M2)

Assets: secret values; DEKs/audit key/root key; operator and token credentials;
the audit log; authorization state.

| Adversary | Can | M2 outcome |
|---|---|---|
| Local user without file access | run `dso central` | no credential ⇒ authentication fails; throttled; audited |
| Operator with `reader` on project A | try other projects/envs, IDOR by ids, token minting | uniform denial; no existence oracle; audited |
| Holder of a stolen/expired/revoked service token | replay | rejected on next request; audited |
| Holder of a stolen session file | act as that operator until expiry/revocation | possible; bounded by lifetime; revocable; audited |
| Database-**read** attacker (stolen copy/backup, no keys) | read ciphertext, hashes | cannot decrypt secrets; token secrets are 256-bit random (SHA-256 hash is not invertible); operator passwords are Argon2id |
| Database-**write** attacker against a *live* service (can edit the file, cannot read the key) | insert a service-token row with a secret they chose, add a grant, set an operator's password hash, replay an old copy of a revoked row; then authenticate to the running service | **Not prevented in M2** (see "Authorization-state integrity" below). Detectable after the fact by `audit verify --state` while the chain is intact |
| Tamperer of audit rows only (no audit key) | edit/delete/insert/reorder events | detected by `audit verify` (§5.5); tail truncation needs an anchor |
| DB owner / root / host admin | everything | **not defended against**; audit forgeable; bypass leaves no trace (§0, §5.5) |
| Malicious operator with `member:grant` | self-escalation; granting beyond their scope | no self-grants; delegation table bounds roles and scope (§4.2.1); token issuance needs the issuer to hold the permission (§4.2.2); audited. Collusion/second accounts are **not** prevented (§4.2.3) |
| Concurrency attacker | race revoke vs use | serialised; tested |

**Authorization-state integrity (decision D13 — specified in §5.9).** `operators`,
`operator_sessions`, `grants` and `service_tokens` are ordinary rows. Anyone who can
*write* the database file can mint credentials or grants that the running service
will honour — a bypass that does not need the root key (the service has it).

* **Per-row MACs are rejected for M2:** they need a refresh on every legitimate
  update, must exclude volatile fields, and **cannot stop an attacker re-inserting
  an older, validly MACed row** (nothing proves freshness without state outside the
  database). They would be a large mechanism with an incomplete property.
* **M2 mechanism — detective replay (§5.9).** Every identity change is an audited
  event carrying the attributes and *credential commitments* (keyed MACs of the
  stored hashes, never the hashes) needed to reconstruct the expected state;
  `audit verify --state` compares it with the tables and also flags *use* of
  credentials that were never issued or were already revoked. It is **detective,
  not preventive**, covers the columns listed in §5.9 only, and is defeated by a
  coherent rollback of rows plus chain tail without an external anchor, and by
  anyone holding the audit key. §5.9 lists what it can and cannot establish.
* M3 (where the database may sit behind different permissions than the key) must
  decide on preventive controls.

**Accepted time and access limits (D23, D24).** Rows 'DB owner / root / host admin' above include Mode R:
the root-key proof is not a credential and grants database-owner power, with read-only use unrecorded. The
clock guard's blind spot (a rollback later than the newest event) and the forward-jump recovery cost are
accepted limitations (§6.4), tracked as R34/R35.

What reduces exposure in M2 without new mechanism: the database and its directory
must be owned by the service account and not writable by others (`securefs`), and
backup integrity (checksums/signatures, restricted storage) is an operator duty
because a tampered backup that is later restored is a realistic route to forged rows.

Residual operational risks: audit-table growth (no pruning in M2), serialised
audited reads, lock-out DoS, clock changes, dependency vulnerabilities (see
review), key escrow still unbuilt.

## 10. Test strategy (summary; details and acceptance criteria in the plan)

* **Authorization matrix** (generated): every `Handle` method × principal class ×
  relation to the resource (own env, sibling env, other project, no grant,
  revoked grant, disabled operator, expired/revoked token, wrong prefix) ⇒ expected
  allow/deny **and** expected audit event. A reflection test fails when a method
  has no matrix row.
* **Negative security tests:** privilege escalation via grant/ token scope;
  token ↔ operator credential confusion; IDOR by id and by name; SQL-shaped names;
  timing/uniformity of auth errors (statistical bound, not a hard timing claim);
  token/ session replay after revocation within one request; concurrent
  revoke-vs-use; oversized/malformed credentials; fuzzing of token parsing.
* **Audit integrity:** tamper each field; delete first/middle/last; reorder;
  insert; splice from another chain; key-version relabel; wrong/absent key; head
  mismatch; truncation with and without an anchor (**the "without" case is
  asserted to be undetected and documented**); concurrent appenders from N
  goroutines and M processes yield a single gapless chain; SIGKILL mid-operation
  leaves mutation and event both or neither.
* **Failure injection:** audit append failing ⇒ mutation rolled back and no
  plaintext released; audit key unavailable ⇒ fail closed.
* **KMS compatibility:** the audit flows run against a `KeyProvider` test double
  that hides its key.
* **Migration:** M1 database with data → `0003` → pending state refuses data
  operations → `upgrade` → genesis + first operator; backup produced; wrong key
  refused; double upgrade refused; concurrent upgrade has one winner.
* **Log/leak scans** across stdout, stderr, logs, audit rows, errors for planted
  secrets, tokens and passwords.
* **Mutation testing** of authorisation and chain code (as done for M0/M1).
* Race detector and multi-process tests on every new package.
* **Identity-state replay (§5.9):** one test per replayed event type (legitimate path
  ⇒ zero findings), plus forged rows, credential replacement and rollback, cleared
  revocations, restored stale rows, deleted rows, use of unissued/revoked credentials,
  adopted epochs, and the **documented non-detections** (coherent rollback without
  anchor, volatile columns, key-holder forgery) asserted as such.
* **Verification states (§5.8):** each of OK / BROKEN / UNVERIFIABLE / HEAD_INCONSISTENT /
  QUARANTINED: exit code, "verify writes nothing" (database byte-identical before
  and after), latch behaviour, refusal to append while quarantined, epoch reset
  semantics, `--record` only on OK.
* **Triggers (§7.3):** direct-SQL tampering with triggers present and with triggers
  dropped; trigger inventory; limitation test (valid shape, bad MAC).
* **Failure semantics (§5.7):** injected audit-specific failures vs general write
  failures; no "recorded" claim without commit; rate-bound window and suppressed
  counts; commit-error outcome message.
* **Clock guard (§6.4):** each operation class while tripped; wait recovery;
  `clock reset` revokes what the lower clock could revive; expired credentials never
  valid after reset.
* **Delegation (§4.2.1–2):** every cell of the delegation table, allowed and forbidden,
  including self-grant, cross-project, wider-scope, last-admin protection, and the
  token hold-it rule.
* **Commitment key lifecycle (§5.9):** replay across an audit-key rotation (old and new
  versions), after an epoch reset (new-version baseline), with a missing/unwrappable
  key (`ST-UNVERIFIABLE`, exit 11, never clean), relabelled version/id/kind.
* **Epoch baseline (§5.9):** all rows (live, revoked, disabled) adopted; zero false
  findings for legitimate pre-reset entities; post-reset forgery and deletion detected;
  pre-reset tampering adopted (asserted); baseline count mismatches rejected.
* **Mode R scope and release (§5.8, D23, D20 release policy):** Mode R can run the read-only audit commands
  and the flag-setting commands but is refused for `--release`, `--record` and `rotate-key`; release runs
  verification, `--state` and the clearing in one transaction and is blocked by any identity-state finding;
  release authorization path is BLOCKED (B-1) and has no test until decided.
* **Quarantine and listings (§5.8, OQ-D20-1):** secret reads and mutations denied, the three listings
  allowed with exactly the stated fields, identity listings denied, no `last_seen_at`/`last_used_at` writes.
* **State transitions (§5.8):** each of T1–T7, forbidden transitions, release only
  after a fresh `OK`, reset from each kind of inconsistent database (head ahead of
  events, head behind, MAC mismatch, missing `audit_state`, missing key row,
  quarantined), observations recorded but not endorsed, old epochs permanently
  `HISTORICAL`.
* **Failed-auth bound under concurrency/failure (§5.7):** many processes across a window
  boundary, SIGKILL at every step of the window-roll transaction, injected write
  failures, clock regression, restart persistence, and the accounting invariant.
* **Clock reset rule (§6.4):** forward-skew-then-correct and backward-step scenarios;
  rules (a)/(b)/(c) and the untouched-expiry guarantee.
* **Monotone time reference (§6.4, F-01/F-07/F-08):** appends under a tripped guard never lower
  `ts_high_water`; `now_eff` and the strict expiry boundary; revival within the tolerance impossible;
  `created_at` edits detected; `TS-DECREASE`.
* **Degraded-state authentication (§5.8, F-05):** every read-only and flag command with all sessions
  expired, with `HEAD_INCONSISTENT`, quarantined, and with the guard tripped (Mode R); login refused.
* **Exit codes, boundaries, reset order (§5.8, F-03/F-10/F-13/F-14):** precedence table; `BOUNDARY`
  cases; reset with the triggers installed; release/`--record` head re-check.
* **Project/environment replay, genesis (§5.9, §7.2, F-02/F-27):** M1 structure adopted without
  `ST-ORPHAN`; initial `audit_state`; genesis through the normal trigger branch.
* **Triggers vs quarantine (§7.3, F-04):** latch/release/window updates succeed with the head inconsistent.

## 11. Production-readiness statement

M2, even complete and green, is **not** production-ready: no network boundary,
OS-level bypass, local HMAC chain, no key escrow/rotation/backup tooling, not
externally reviewed, not fuzzed at scale. Any wording to the contrary requires a
separate approval after an independent review of the M2 code.

## Appendix A — canonical encoding (normative sketch)

`canon(event) = enc(domain="dso/audit-event/v1") ‖ enc(mac_alg) ‖ u64(seq) ‖ i64(ts) ‖
chain_id ‖ u32(key_version) ‖ enc(actor_type) ‖ enc(actor_id) ‖ enc(actor_label) ‖
enc(session_id) ‖ enc(action) ‖ enc(outcome) ‖ enc(reason) ‖ enc(project_id) ‖
enc(environment_id) ‖ enc(resource_type) ‖ enc(resource_id) ‖ enc(resource_name) ‖
i64(secret_version or -1) ‖ enc(request_id) ‖ enc(origin) ‖ enc(detail)`, where
`enc(s) = u32(len(s)) ‖ s`, nullable strings use a 1-byte presence flag, and
`detail` is canonicalised (sorted `k=v`). The MAC input appends `prev_mac`.
Test vectors are part of the plan (AC-AU-01) so independent implementations agree.

## Appendix B — `detail` schemas of state-bearing events (normative sketch)

`detail` is a canonical, sorted list of `key=value` pairs; each action admits only
the keys below; values match `[A-Za-z0-9._:/+=,*-]{0,256}` (no spaces, no free text);
`pw`/`sc` are 32-hex-char credential commitments under the event's own key version (§5.9); the commitment label is fixed by the action (`pw`, `sess-sc` for `auth.login`, `tok-sc` for `token.issue`). Subject = the event's
`resource_type`/`resource_id`.

| Action | Subject | Keys |
|---|---|---|
| `operator.create` | operator | `username`, `admin` (0/1), `pw`, [`reason`=`bootstrap`/`recover`], [`adopted`=1, `disabled`=0/1] |
| `operator.set_password` | operator | `pw` |
| `operator.set_admin` | operator | `admin` |
| `operator.disable` / `enable` | operator | — |
| `auth.login` | session | `operator`, `exp` (unix), `sc`, [`adopted`=1, `revoked`=0/1] |
| `auth.logout` / `auth.session_revoked` | session | [`reason`=`logout`/`admin`/`operator_disabled`/`clock_reset`] |
| `grant.add` | grant | `operator`, `project`, `env` (id or `*`), `role`, [`adopted`=1, `revoked`=0/1] |
| `grant.revoke` | grant | [`reason`] |
| `token.issue` | token | `project`, `envs` (sorted comma list of ids), `perms` (`read`/`read+list`), `name`, `created` (unix; equals the table `created_at`, F-08), `exp` (unix or `none`), `sc`, [`rotated_from`], [`adopted`=1, `revoked`=0/1, `replaced_by`=<id>] |
| `token.rotate` | old token | `new`, `old_exp` |
| `token.revoke` | token | [`reason`=`operator`/`rotation`/`clock_skew`/`expired_at_reset`/`policy`/`emergency`] |
| `project.create` | project | `slug`, [`adopted`=1] |
| `env.create` | environment | `project`, `slug`, [`adopted`=1] |
| `audit.key_rotated` | audit key | `new_version`, `new_key_id` |
| `audit.epoch_start` | chain | `obs_status` (`ok`/`broken`/`unverifiable`/`head_inconsistent`/`empty`), [`obs_first_bad_seq`], [`obs_last_seq`, `obs_last_mac` — omitted when the event table was empty; `obs_last_mac` = hex or `invalid`], [`obs_state_head_seq`, `obs_state_head_mac`], `obs_state` (`clean`/`findings:n`/`unverifiable`), `reason`, `baseline` (`proj:n,env:n,op:n,sess:n,grant:n,tok:n`) — all are **observations by the resetting process**, not proof |
| `audit.verify_recorded` | chain | `through_seq`, `head_mac` |
| `auth.failures_suppressed` | — | `count`, `window_start` (the window the count belongs to) |
| `system.clock_reset` | — | `old_hw`, `new_hw`, `sessions_revoked`, `tokens_revoked_a`, `tokens_revoked_b`, `tokens_revoked_c`, `tokens_kept` (a/b/c = the rules of §6.4) |
| `system.emergency_revoke` | named | `kind` (`token`/`session`/`operator`), `id` |
| `system.recover` | — | `operator` |
| `system.upgrade` / `system.genesis` | — | `schema` |

Names (usernames, token names) are restricted to `[a-z0-9._-]` so they can appear
here unambiguously. A conformance test asserts that every key written by the service
is on this list and that no value can contain a hash, token or secret.
