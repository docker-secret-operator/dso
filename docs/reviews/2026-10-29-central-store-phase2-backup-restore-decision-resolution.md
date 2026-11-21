
# `dso central` Phase 2 Step 5.4 — Backup/Restore Decision Resolution (design only, no implementation)

**Date:** 2026-10-29
**Status:** Decision preparation only. No Go source, SQL migration, CI workflow, dependency, or configuration file was modified to produce this document, beyond the separate, unrelated one-line comment fix in `internal/cli/central.go` performed alongside this task (see §0). No backup command, restore command, external anchor, socket, credential, or runtime behavior was implemented. **Implementation requires a separate, explicit authorization after this document is reviewed — this document does not grant that authorization.**

---

## 0. Companion fix performed alongside this document (not part of the decision set)

`internal/cli/central.go`, in the doc comment above `centralAuditLoggerOverride` (around line 168): corrected a stale test-filename reference from `internal/cli/central_audit_test.go` (a file that does not exist — confirmed this task via a directory listing of `internal/cli/*test*`) to `internal/cli/central_audit_logger_test.go` (the actual file, confirmed present). This is a comment-only change: no identifier, function signature, logger construction, `Sync()` handling, or logging behavior was touched, consistent with the task's explicit instruction that the logger-lifecycle question was already assessed and no change was warranted there.

## 1. Purpose and source documents

Resolve the six open decisions recorded in `docs/reviews/2026-10-25-central-store-phase2-backup-restore-design.md` §13, using the resolutions supplied in this task's instructions, and record them for explicit review — following the style already established by `docs/reviews/2026-10-25-central-store-phase2-audit-chain-decision-resolution.md`. **This document proposes resolutions for approval; it does not itself constitute that approval**, and it does not authorize implementation.

**Source documents** (read this task, not re-derived from memory):
- `docs/reviews/2026-10-25-central-store-phase2-backup-restore-design.md` (Step 5.4 design, §§1–15, all re-read this task).
- `docs/reviews/2026-10-25-central-store-phase2-audit-chain-decision-resolution.md` (Step 5.3 decision resolution, whose five binding decisions are **not reopened** here — see §9).
- `docs/central-store-audit-log.md` and `internal/store/sqlite/audit.go` (re-verified this task where cited below, specifically `VerifyAuditChain`'s documented scope).

## 2. Verified repository facts (this task)

- Branch/HEAD unchanged from the end of the prior commit-and-push task: `feature/webui-phase1` @ `c4d404ca32c8c2d765eaf63f692762d8319cfb07`, confirmed via `git branch --show-current && git rev-parse HEAD && git status --short` at the start of this task.
- Working tree before this task's changes: the same ten pre-existing, unrelated `.claude-flow/*` modifications, and the same nine untracked `docs/**/*.md` files already present after the prior push (the Step 5.3/5.4 documents, the complete-product-audit, P-01 decision, and Phase 0/1 findings register) — none of these were touched by this task except by the addition described below.
- `internal/cli/central.go`'s stale comment, confirmed present at the location the task specified before this task's fix (§0): verified by direct read, not assumed from the prior session's report.
- `internal/store/sqlite/audit.go`'s `VerifyAuditChain` doc comment (lines ~171-181, re-read this task): confirms the exact wording this document's Decision 1 relies on — "this function can only verify the events that are present. It cannot detect that the chain's tail was deleted, or that the whole database file was replaced by an older, internally-valid backup."

## 3. Decision 1 — External rollback-detection anchor

**Question**: should the initial backup/restore implementation include an external (local or remote) rollback-detection anchor, per Step 5.4 design §7's Option C/D analysis?

**Resolution (approved for recording, per this task's instruction)**: **Defer.** The initial backup/restore implementation will not include a local or remote external rollback-detection anchor.

**Rationale**: Option C (a local anchor file outside the database) was already identified in the Step 5.4 design as the most promising future option, but its exact write cadence and failure-handling behavior interacts with Step 5.3's own approved availability-first policy (Gate 6) and was explicitly flagged there as needing its own, separately reviewed design rather than being decided as a side effect of this resolution. Option D (a remote/networked anchor) was already rejected in the Step 5.4 design for the same reasons Step 5.1 rejected a network-accessible `dso central` service — it conflicts with DSO's self-hosted, no-mandatory-external-dependency identity.

**Implications**: the initial backup/restore implementation carries forward, unchanged, the audit chain's own documented verification scope (`internal/store/sqlite/audit.go`, re-verified §2): `VerifyAuditChain` checks only the events present in whatever database (live or candidate-for-restore) it is run against.

**Limitation, stated explicitly per this task's instruction**: audit-chain verification cannot independently detect tail deletion or replacement of the live database with an older, internally-valid snapshot. **This is a known, disclosed limitation of the current design, not a security guarantee that rollback is prevented or detected.** A restore of an older backup over a database with more recent, legitimate activity will pass chain verification cleanly and will not, by itself, be flagged as anomalous.

**Deferred work**: Option C's exact write cadence (synchronous per-audit-append vs. batched), its interaction with Gate 6's availability-first policy, and its exact failure-handling semantics remain fully undesigned, deferred to a future, separately reviewed milestone — not approved, outlined further, or scheduled by this document.

**Not introduced by this decision**: no anchor file, new cryptographic key, service, or dependency.

## 4. Decision 2 — Secret decryption validation during restore

**Question**: should restore require decrypting every stored secret version as part of its validation, or rely on a lighter-weight check?

**Resolution (approved for recording)**: **Require SQLite integrity validation (`PRAGMA integrity_check`) and root-key binding (`Service.Bind`-equivalent against the candidate's own binding canary) as restore's validation gate. Do not require full secret decryption of every stored version during restore.**

**Rationale, stated precisely**:
- `PRAGMA integrity_check` validates SQLite's own structural integrity (page layout, index consistency, etc.) — it is **not** a cryptographic check and does not verify that any individual secret's ciphertext is authentic or decryptable.
- A successful root-key binding (the candidate's `store_meta` binding canary unwrapping correctly under the configured provider's key) proves the **root key matches the one the candidate database was bound to**. It does **not** prove that every `secret_versions.ciphertext`/`nonce` pair in that candidate will successfully pass `crypto.Open`'s AAD/GCM authentication — SQLite-level structural validity and AES-GCM-level ciphertext authenticity are independent properties, and this resolution does not conflate them.
- Restore's own output must state these limits clearly, rather than implying that passing validation proves every secret in the restored database is readable.

**Implications**: post-restore secret usability can be checked through DSO's existing, already-supported operations (e.g., a representative `secret get --reveal` against a known secret, exactly as the Step 5.4 design's §12 runbook already recommends) — this resolution does not change that recommendation, nor does it require it to happen automatically as part of the restore command itself. A dedicated, separately designed bulk-verification capability (checking every secret version's decryptability in one pass) remains a possible future addition, not designed or approved here.

**Limitation**: restore cannot and does not claim that a successful validation pass guarantees every secret in the restored database is decryptable; it guarantees only SQLite-level structural integrity and root-key compatibility.

**Not introduced by this decision**: no new bulk-decryption command, and no change to the crypto model (`internal/crypto`'s `Seal`/`Open`/AAD construction remain exactly as they are). No secret plaintext is logged or exposed by any validation step this resolution describes — validation operates on ciphertext structure and key-binding only, never on decrypted content.

## 5. Decision 3 — CLI names and confirmation UX

**Question**: what command/flag names and confirmation flow should the eventual backup/restore implementation use?

**Resolution (approved for recording, as design decisions only — not implemented by this document)**:
- `dso central backup --out <path>`
- `dso central restore --from <path>`
- A restore dry-run mode, preferably `dso central restore --from <path> --dry-run`

**Required behavior (design requirements, carried forward from the Step 5.4 design §5/§10, restated here as approved constraints on whatever implementation follows)**:
- Candidate validation (the checks in §4 above, plus schema-version and audit-chain-internal-consistency checks already specified in the Step 5.4 design §5) must complete **before** anything about the active store is touched.
- A mandatory safety snapshot of the active store must be taken before replacement — this was already the Step 5.4 design's single strongest recommendation (§5) and is reaffirmed, not reopened, here.
- Explicit confirmation is required before the destructive replacement step begins.
- An explicit confirmation flag must exist for non-interactive restore (e.g., scripted/automation use); **this document does not select the exact flag name** — the task's own instruction defers that to implementation time, "consistently with existing CLI conventions."
- Restore's output (interactive and dry-run) must clearly show the candidate's recorded timestamp and its last audit-chain event (its "tip"), where available, so an operator has the information needed to recognize an older snapshot — this mirrors the usability mitigation already proposed in the Step 5.4 design §8, not a new mitigation invented here.

**Limitation**: naming these commands here does not create them. `dso central`'s command tree (`internal/cli/central.go`) contains no `backup` or `restore` subcommand as of this task — confirmed by this task not modifying that file's command registration in any way beyond the §0 comment fix.

**Not introduced by this decision**: the commands and flags themselves, their flag parsing, their help text, or any confirmation-prompt wording — all deferred to a future, separately authorized implementation task.

## 6. Decision 4 — Backup retention

**Question**: should the initial implementation include automatic pruning of accumulated backup files?

**Resolution (approved for recording)**: **No automatic pruning in the initial backup/restore implementation.**

**Rationale and implications**:
- Backup files, once created (by whatever future `backup` command implements the Step 5.4 design's §4 mechanism), accumulate until an operator removes them — this mirrors the existing, already-shipped behavior of the migration-time `.pre-migration-v<N>...bak` snapshots (`internal/store/sqlite/sqlite.go`, "Backups are never deleted automatically," already true today and unchanged by this resolution).
- The operator is responsible for monitoring disk usage and protecting backup files with the same care as the live database (per the Step 5.4 design §4/§6: a backup is exactly as sensitive as the live database, and the root key/passphrase that makes it useful must be preserved separately from both).
- Automatic retention or pruning policy is explicitly **not** decided here — it requires its own future decision, informed by real storage-growth and operational data once a backup command actually exists to generate that data, consistent with how Step 5.3's own audit-retention decision (Gate 5) was handled: deferred pending real usage evidence, not decided speculatively.

**Not introduced by this decision**: no retention configuration, no scheduled deletion job, and no existing backup file (the migration-time `.bak` files that already exist in this repository's operational history, if any) is deleted or otherwise modified by this task.

## 7. Decision 5 — Caller authorization for backup/restore

**Question**: what authorization posture should `backup`/`restore` have, given Step 5.2's Option B design and its disclosed limitations?

**Resolution (approved for recording)**: **Apply caller-#1-only as a *policy intention* for backup and restore, subject to the existing Option B limitation.**

**Rationale, stated with the same explicitness the task requires**:
- Step 5.2's authorization matrix (`docs/reviews/2026-10-24-central-store-phase2-authn-authz-design.md` §7, §15) already applies this identical "policy intention, not an enforceable control under Option B alone" treatment to `secret get --reveal` and `disable-version`/`delete` — this resolution extends the same, already-approved treatment to `backup`/`restore`, rather than inventing a new authorization category.
- Under Option B (local Unix-domain socket + `SO_PEERCRED`), the connecting process's OS UID/GID can be established, but **two different processes sharing the same UID cannot be distinguished from one another** — this is Step 5.2's own disclosed, unchanged limitation (§11 of that document), and this resolution does not claim backup/restore escapes it.
- **This document does not claim the caller-#1-only restriction on backup/restore is implemented or enforced today.** No socket, listener, or peer-credential check exists in this repository as of this task (confirmed: `internal/cli/central.go`'s command tree and `internal/centralsvc` were not modified by this task beyond §0's comment fix, and no new socket-related file was created).

**Not introduced by this decision**: no new identity mechanism, credential file, RBAC scheme, or authorization check of any kind. The decision records an *intended* policy for a future implementation to honor once Option B (or a stronger mechanism) actually exists; it does not implement anything now.

## 8. Decision 6 — Step 5.3 audit-retention revisit trigger

**Question**: does the existence of the Step 5.4 backup/restore design count as satisfying Step 5.3's Gate 5 Trigger C ("the Step 5.4 backup/restore design, when it is proposed")?

**Resolution (approved for recording)**: **Yes — the Step 5.4 design's existence counts as the documented revisit trigger having occurred. This decision does not itself approve any audit-retention policy.**

**Rationale and implications**:
- Step 5.3's decision-resolution (`docs/reviews/2026-10-25-central-store-phase2-audit-chain-decision-resolution.md` §5) named three revisit triggers for its own "unbounded growth accepted initially" recommendation; Trigger C was worded as "if/when Step 5.4 (backup/restore) is designed, that is a natural point to revisit retention together." The Step 5.4 design document now exists (`...backup-restore-design.md`, written 2026-10-25) — per the task's explicit instruction, this resolution records that the trigger condition has been met.
- **Recording the trigger is not the same as acting on it.** Audit history remains append-only and unbounded exactly as Step 5.3 approved it (Gate 5, unchanged) — this document does not propose, approve, or even sketch a retention policy for audit events.
- Backup-file retention (Decision 4, §6 above) and audit-event retention (Step 5.3's Gate 5) are explicitly treated here as **separate concerns**: one governs how many backup snapshots accumulate on disk, the other governs how large the `audit_events` table itself is permitted to grow inside each live or backed-up database. Resolving one does not resolve the other, and this document resolves neither beyond recording that Gate 5's own named trigger has fired.
- A future, separate, explicit decision is required before any audit-retention policy (archival, pruning, or a row-count/age-based limit) is adopted.

**Not introduced by this decision**: no change to the audit schema (`internal/store/sqlite/migrations/0003_audit_events.sql`, untouched), no event-deletion behavior, and no retention logic of any kind.

## 9. Consistency checks (per the task's Part C requirements)

- **Distinguishes verified facts, approved decisions, proposals, limitations, and deferred work**: each decision section above (§3–§8) separates "Resolution," "Rationale/implications," "Limitation," and "Deferred work"/"Not introduced by this decision" explicitly, rather than blending them.
- **Does not claim Step 5.2 socket authentication is implemented**: confirmed explicitly in §7; no socket, listener, or peer-credential code exists in this repository as of this task.
- **Does not claim backup/restore or rollback-anchor functionality exists**: confirmed explicitly in §3 and §5; no `backup`/`restore` subcommand exists in `internal/cli/central.go`'s command tree, and no anchor file or mechanism was created.
- **Does not reopen the five binding Step 5.3 decisions**: re-verified this task against `docs/reviews/2026-10-25-central-store-phase2-audit-chain-decision-resolution.md` §8 — (1) `WithTx` coverage for `CreateProject`/`CreateEnvironment`/`DeleteSecret`, (2) log-only audit-failure reporting, (3) plain hash chain without a MAC key, (4) audit-history read-authorization deferral to Step 5.2, (5) unbounded audit growth with the three named triggers — none of these five is altered, contradicted, or re-decided by any section above; Decision 6 (§8) only records that one of Decision 5's own named *triggers* fired, which that document itself anticipated and explicitly invited ("the Step 5.4 backup/restore design, when it is proposed").
- **Does not contradict the current Step 5.4 design without documenting the specific resolution**: every decision above resolves, rather than silently overrides, the corresponding open item in the Step 5.4 design's §13; none of the Step 5.4 design's other content (§1–§12, §14–§15) is altered by this document.
- **References actual repository paths and sections; no fabricated citations or line numbers**: all paths and section references above were verified this task by direct reading, consistent with §2's verified-facts list; no line number is cited that was not independently confirmed.
- **States that implementation requires a separate explicit authorization after design review**: stated in this document's header and restated in §10 below.

## 10. Implementation authorization status

**Implementation of backup, restore, the external anchor, or any other runtime behavior discussed above has NOT been authorized by this document.** This document only records proposed resolutions to the Step 5.4 design's six open decisions, for explicit review. A separate, explicit authorization is required before any of Decisions 1–6 is acted on in code.

## 11. Repository state and close-out

- **Document created**: `docs/reviews/2026-10-29-central-store-phase2-backup-restore-decision-resolution.md` (this file). Confirmed, before creation, that no file already existed at this path.
- **Companion change**: the one-line comment fix in `internal/cli/central.go` described in §0 — the only Go source file touched by this task, and the only change to it.
- **No existing document was modified**: `docs/reviews/2026-10-25-central-store-phase2-backup-restore-design.md` and `docs/reviews/2026-10-25-central-store-phase2-audit-chain-decision-resolution.md` were both read, not edited.
- **No SQL migration, test, configuration, or dependency was modified.**
- **No backup command, restore command, external anchor, socket, or credential mechanism was implemented.**
- **No commit, tag, stash, reset, clean, or push occurred during this task.** All pre-existing modified and untracked files (the ten `.claude-flow/*` entries and the nine pre-existing `docs/**/*.md` files) remain exactly as they were at the start of this task, with this document added as a new untracked file and `internal/cli/central.go` now carrying its one-line comment fix as a new, as-yet-uncommitted modification.
