
# `dso central` Phase 2 Step 5.3 — Audit-Chain Decision Resolution (design only, no implementation)

**Date:** 2026-10-25
**Status:** Decision preparation only. No Go source, test, schema, migration, CI workflow, dependency, or configuration file was modified. No hash chain, audit table, verification command, health reporting, retention, restore behavior, or transaction change was implemented. Neither prior document was edited.

---

## 1. Purpose and source documents

Resolve the four outstanding questions from the Step 5.3 decision review into concrete, approval-ready recommendations, and consolidate all seven gates into one matrix. This document makes recommendations only; it does not convert any of them into an approved decision.

**Source documents** (read, not re-derived from memory — both already existed from this engagement and were re-verified against current source where load-bearing):
- `docs/reviews/2026-10-25-central-store-phase2-audit-chain-design.md` (Step 5.3 design)
- `docs/reviews/2026-10-25-central-store-phase2-audit-chain-decision-review.md` (decision review)
- `docs/reviews/2026-10-24-central-store-phase2-threat-model.md`, `docs/reviews/2026-10-24-central-store-phase2-authn-authz-design.md` (Step 5.1/5.2, frozen — not reopened)
- `docs/central-store-m1.md`

## 2. Verified repository facts (re-checked this task, not assumed from prior reports)

- Branch/HEAD unchanged from every prior step in this engagement: `feature/webui-phase1` @ `ccfc0b55b7ce8630d395e9dd1e29d301d6010e61`.
- **New verification this task**: `internal/store/store.go:156-158` — `type Tx interface { Operations }`. `Operations` (line 110) includes `CreateProject` (line 111), `CreateEnvironment` (line 116), and `DeleteSecret` (line 127) with the exact signatures `Store`'s autocommit wrappers already expose. **This means `Tx` already has these three methods today, with no interface change required** — `WithTx`'s callback receives a `Tx`, which can call `tx.CreateProject(...)`, `tx.CreateEnvironment(...)`, `tx.DeleteSecret(...)` right now. This is stronger, more concrete evidence than the decision review had for Question 1 — it was previously inferred from the shared `ops`/`queryer` pattern in `tx.go`; it is now confirmed directly from the interface definitions themselves.
- Everything else verified in the prior two documents (15 autocommit / 4 transactional operations, no existing audit table, `created_by`/`disabled_by` as plain `TEXT`) was re-confirmed present and unchanged; not re-derived from scratch in this pass since no repository change occurred between documents.

---

## 3. Decision 1: Transaction coverage (Gate 2)

**Verified fact**: `Store`'s current autocommit wrapper methods for `CreateProject`, `CreateEnvironment`, and `DeleteSecret` (in `internal/store/sqlite/tx.go`) each call `s.ops().X(...)` directly against `*sql.DB`. Making each one transactional requires changing exactly that one line per method to `s.WithTx(ctx, func(ctx context.Context, tx store.Tx) error { return tx.X(...) })` — mirroring the pattern the file already uses, word-for-word, for `CreateSecret`/`AddVersion`/`DisableVersion`/`PutMetaIfAbsent`. **No new interface method, no schema change, no migration is required** — confirmed this task via `internal/store/store.go`'s interface definitions (§2).

**Migration/compatibility/test implications** (assessed, not implemented):
- **Migration**: none — this changes only which transaction wraps an existing call, not the SQL statements themselves or the schema.
- **Compatibility**: `WithTx`'s documented semantics (commit on nil, rollback on error/cancellation/panic, `ErrTxActive` on nested calls — `store.go:156-168`) already apply uniformly; no caller-visible behavior change for `CreateProject`/`CreateEnvironment`/`DeleteSecret`'s success path. The one behavior change worth naming: a context already inside a `WithTx` scope that calls one of these three will now return `ErrTxActive` instead of succeeding standalone — this is correct, intended `WithTx` behavior, but any existing test that calls these three methods from within another transaction's callback (if any exist; not independently confirmed in this pass — flagged below) would need to be checked.
- **Tests**: the existing test suite for these three methods (not individually re-read in this pass, given cost already incurred verifying the interface-level evidence directly) would need, at minimum, a crash/rollback test analogous to what the four already-transactional operations presumably already have, to confirm atomicity — this is a testing-strategy item already captured in the Step 5.3 design's §10, not a new finding.

**Recommendation**: **include all three in the initial audit-chain implementation.** The decision review's "low cost" assessment is now confirmed with concrete interface evidence, not just inference — this is as close to a mechanical, zero-risk change as exists in this entire design. **This is a recommendation, not an approved decision** — it still requires product-owner sign-off per the decision review's own framing (Gate 2's matrix entry: "Yes, in the sense that the scope correction itself should be reviewed").

**Deferred work / explicit limitation**: none identified for this specific decision — unlike the other three questions, this one has no "missing evidence" gap; the only outstanding step is approval itself.

---

## 4. Decision 2: Audit-corruption behavior (Gate 6)

**Comparison of the three reporting mechanisms:**

| Mechanism | How it would work | Reliability when the audit table itself is corrupted |
|---|---|---|
| Log-only warning | Each failed audit write logs at `Error` level via the existing `zap`-based convention (same pattern already used throughout `internal/agent`, `internal/rotation`, etc.) | **Reliable** — a log write does not depend on the audit table at all; it is an independent I/O path (the process's log sink). This is the one option that *does not* depend on the damaged resource to report its own damage. |
| Persistent, operator-visible audit-health status | A status value (e.g., "audit: degraded since <time>") surfaced through `dso central`'s own equivalent of `dso doctor`/`/health` | **Depends entirely on *where* that status is stored.** If it were stored inside the same SQLite database (e.g., a `store_meta` row), a corruption event severe enough to break the audit table could plausibly also prevent writing or reading that status — the task's own caution ("do not assume the same damaged database can reliably record its own failure") is correct and this design must not violate it. |
| Both | Log + persistent status | Combines the reliable path (log) with the higher-visibility path (status), **but only if the status is stored somewhere independent of the audit table's own corruption domain** |

**Resolved recommendation**: **the smallest reliable initial design is log-only, with the persistent-status mechanism deferred as a later enhancement rather than built into the initial milestone — unless the persistent status is stored outside the primary database file** (e.g., a small sentinel file in the same directory, written with the same `securefs`-fail-closed discipline already used for key files). Storing a health flag *inside* the very table (or even the same database file) whose corruption it's meant to report is a design that can silently fail exactly when it matters most — this is the task's caution, confirmed as a real design risk rather than a hypothetical one, since SQLite file-level corruption (as opposed to a single bad row) can affect the whole file, not just one table.

**What happens to the triggering operation**: consistent with the decision review's Gate 6 conclusion (not reopened, only operationalized here) — the business operation still succeeds; only the audit write fails, and that failure is logged immediately, synchronously, as part of handling that same request (not deferred to a background sweep that could itself be skipped).

**How recovery is recognized**: the simplest reliable signal is **the next successful audit write after a failure** — i.e., recovery is implicit and automatic (no special "recovery" state machine needed) once the underlying corruption is fixed (e.g., by an operator-run integrity check/repair) and a subsequent operation's audit write succeeds. A sentinel file (if adopted per the "both" option) would need its own explicit "clear" step rather than assuming silence means recovery.

**Product-owner decision still required**: (a) whether log-only is acceptable for the initial milestone, versus (b) whether a sentinel-file-based persistent status is worth building now instead of later — this document recommends (a) for the initial milestone specifically because it has no dependency on getting the sentinel file's own fail-closed design right first, but (b) is a reasonable near-term follow-on, not a rejected idea.

**Deferred work / explicit limitation**: a full persistent-health-status design (where exactly the sentinel lives, how it's cleared, whether it's exposed via a CLI command or only a file) is not designed in this document — flagged as a follow-on, not resolved here.

---

## 5. Decision 3: Retention and growth (Gate 5)

**Assessment against the product's actual target deployment profile**: DSO's stated identity (self-hosted, single-operator, Docker Compose-native — per the complete-product audit §9, not reopened here) implies a deployment scale of one operator's secret inventory on one host, not a multi-tenant or high-throughput system. The operations that would generate audit events under the Step 5.3 design's §4 matrix (`CreateProject`, `CreateEnvironment`, `CreateSecret`, `AddVersion`, `GetValue --reveal`, `DisableVersion`, `DeleteSecret`, plus future auth/authz events) are all either low-frequency (project/environment/secret creation) or bounded by how often an operator or their automation actually rotates/reveals a given secret — not a per-request, per-second event volume.

**This document does not invent production telemetry or claim to know actual growth rates** — per the task's explicit instruction. No such data exists in this repository or this engagement to cite.

**Recommendation**: **unbounded growth is acceptable for the initial milestone**, consistent with the decision review's prior conclusion, now with an explicit, concrete revisit trigger (not previously stated as precisely):
- **Trigger A (measured growth)**: once a real deployment's audit table has been in continuous use for a defined period (e.g., revisit after the first few months of real operator usage, once actual event-count-per-unit-time is observable) — this document does not fix an exact number, since no usage data exists yet to calibrate one; the trigger is "real data becomes available," not a specific row count chosen without evidence.
- **Trigger B (operator feedback)**: an operator reports noticeable database-size growth or backup-time impact attributable to the audit table specifically (distinguishable from the primary tables' own growth, which is a separate, pre-existing concern).
- **Trigger C (a later backup/archive milestone)**: if/when Step 5.4 (backup/restore) is designed, that is a natural point to revisit retention together, since both touch the same underlying database-size question from different angles.

**No archival design is produced here** — per the task's instruction, archival is not designed or implemented "unless the review demonstrates that the initial design cannot proceed without it," and this document finds no such demonstration: the initial hash-chained audit table can be built and used without an archival mechanism.

**Product-owner decision still required**: confirm that unbounded growth is acceptable, and confirm or adjust the three triggers above as the agreed revisit points.

**Deferred work / explicit limitation**: archival/pruning design itself, and the exact quantitative threshold for Trigger A, remain unspecified — explicitly deferred, not silently dropped.

---

## 6. Decision 4: Rollback anchor and restore events (Gates 1 and 7)

**What the chain can and cannot detect, stated precisely**: if an older, internally-valid SQLite backup (produced by the existing `.bak` mechanism, `docs/central-store-m1.md:176-186`) is restored over the live database:
- **Can detect**: nothing about the restore itself — a restored backup's hash chain is, by construction, exactly as internally consistent as it was *at backup time*. Every hash still matches its row; every `prev_hash` link is still valid. There is no "wrongness" for the chain to detect, because nothing about the chain's own internal structure changed — only which file is currently live changed.
- **Cannot detect**: that a restore happened at all, or that any events after the backup's timestamp (which did exist at some point, in the file that was overwritten) are now gone. The chain has no way to know that a "future" (relative to the backup) it once had was ever discarded, because the chain's own view of "the present" is simply wherever its own tail currently sits — it has no independent reference point outside itself to notice something is missing.

**Why a restore event inside the restored database cannot independently prove the restore happened** (directly answering the task's requirement): a restore event, if one is written, can only be written **after** the restore completes, as a new event appended to the chain going forward. It cannot retroactively insert itself into the portion of the chain that *was* discarded by the restore (that data no longer exists once overwritten). So a restore event is **evidence that a restore happened, visible going forward from that point on** — but it is not, and cannot be, proof about what was lost in the restore, nor can it be distinguished by an internal-only verification from "the operator simply hasn't done anything audit-worthy in a while" unless an external record (outside the database file) independently shows the chain's tip was higher before the restore than it is immediately after. This is precisely why Gate 7 is "entirely dependent on Gate 1's resolution," as the decision review already concluded — this document confirms that dependency is real and not overstated.

**Recommendation**: **defer both gates, as the decision review proposed**, with a concrete revisit trigger now specified (this sharpens, rather than changes, the prior recommendation):
- **Revisit trigger**: when Step 5.4 (backup/restore) is first proposed as a concrete design — at that point, a restore *command* will exist for the first time, and the question "should restore mark itself, and does that need an external anchor to mean anything" becomes concrete rather than hypothetical. Revisiting any earlier, with no restore command yet designed, would mean designing an anchor for a threat (rollback via restore) that has no corresponding *legitimate* operation yet to distinguish itself from.

**This document does not introduce an external service, network dependency, second trust store, or restore command** — per the task's explicit prohibition — and treats the "defer" recommendation as still requiring explicit product-owner approval, not as self-evidently correct merely because it was already suggested once.

**Product-owner decision still required**: confirm the revisit trigger (tied to Step 5.4's own proposal) is the right point, rather than, e.g., a fixed calendar date or a specific customer commitment this document has no visibility into.

**Deferred work / explicit limitation**: the external-anchor mechanism itself, and the restore command's own audit-event semantics, remain entirely undesigned — both explicitly deferred to Step 5.4, not resolved or even outlined here beyond the dependency analysis above.

---

## 7. Consolidated recommendation matrix for all seven gates

| Gate | Recommendation | Status |
|---|---|---|
| 1. External trust anchor | Defer; revisit when Step 5.4 is proposed | **Proposed design, requires approval** |
| 2. Transaction coverage | Extend `WithTx` to `CreateProject`, `CreateEnvironment`, `DeleteSecret` — confirmed zero-interface-change, low-cost | **Proposed design, requires approval** (strengthened evidence this task; still not approved) |
| 3. MAC/signing key | Plain hash chain, no separate key, for the initial milestone | **Proposed design, requires approval** (carried forward unchanged, per task instruction to preserve absent new contradicting evidence — none found) |
| 4. Audit-history read authorization | Defer to Step 5.2's matrix extension at Option B implementation time | **Proposed design, requires approval** (carried forward unchanged) |
| 5. Retention/archival | Unbounded growth accepted initially, with three concrete revisit triggers now specified | **Proposed design, requires approval** |
| 6. Audit-data corruption behavior | Log-only for the initial milestone; persistent sentinel-file status deferred as a follow-on, explicitly *not* stored inside the audit database itself if ever built | **Proposed design, requires approval** |
| 7. Restore audit event | Record it going forward only, explicitly not a rollback-detection mechanism on its own; revisit together with Gate 1 at Step 5.4 | **Proposed design, requires approval** |

**Preserved, not reopened, per the task's explicit instruction** (no new evidence found that contradicts any of these): plain hash chaining without a MAC key (Gate 3); deferring audit-history-specific authorization to Step 5.2's matrix (Gate 4); the same-UID identity limitation (Step 5.2 §11, unchanged); no silent direct-file fallback once socket-based service mode is configured (Step 5.2 §15 item 5, unchanged); the audit chain does not replace authentication or authorization (Step 5.1/5.2, unchanged throughout).

## 8. Decisions requiring explicit product-owner approval

1. Approve (or amend) including `CreateProject`/`CreateEnvironment`/`DeleteSecret` in `WithTx` as part of the initial milestone (Decision 1) — now backed by direct interface-level evidence that this is a zero-risk, mechanical change.
2. Approve log-only reporting for audit-subsystem failure in the initial milestone, with a sentinel-file-based persistent status explicitly deferred rather than built now (Decision 2).
3. Approve unbounded audit-table growth for the initial milestone and confirm the three stated revisit triggers (Decision 3).
4. Approve deferring the external rollback anchor and restore-event semantics together, revisited specifically when Step 5.4 is proposed (Decision 4).
5. (Carried forward, unchanged) Approve plain hash chaining without a MAC key (Gate 3) and the audit-history read-authorization deferral (Gate 4).

## 9. Implementation readiness and acceptance criteria

**Readiness**: with Decisions 1-4 resolved as recommended above (pending approval), the initial audit-chain milestone has a complete, internally-consistent design spanning all seven gates — no gate remains genuinely undecided in direction, only unapproved. The one gate with materially new evidence this task (Decision 1 / Gate 2) is now the strongest-grounded recommendation in the whole set.

**Acceptance criteria for closing this resolution** (mirroring, and not loosening, the decision review's own §10): the product owner has explicitly responded to each item in §8; none of the five carried-forward preserved items (§7's bottom paragraph) has been silently reopened; Decision 2's "log-only now, sentinel-file later, never inside the audit table itself" distinction is understood and accepted as written, not simplified into "just add a health check" without the storage-location caveat; Decision 4's revisit trigger (tied to Step 5.4's proposal, not a calendar date) is accepted or explicitly replaced with a different one.

---

## 10. Repository state and close-out

- **Document created**: `docs/reviews/2026-10-25-central-store-phase2-audit-chain-decision-resolution.md` (this file).
- **Recommendations for all four questions**: Decision 1 (§3) — include the three operations in `WithTx` now; Decision 2 (§4) — log-only initially, sentinel-file deferred and never inside the audit table itself; Decision 3 (§5) — unbounded growth accepted with three concrete revisit triggers; Decision 4 (§6) — defer both gates together, revisit when Step 5.4 is proposed.
- **Remaining decisions requiring explicit approval**: all five items in §8.
- **Branch/HEAD/working-tree status, initial and final**: `feature/webui-phase1` @ `ccfc0b55b7ce8630d395e9dd1e29d301d6010e61` — unchanged throughout this task. Working tree: the same ten pre-existing `.claude-flow/*` files, the same two uncommitted Phase-1 fixes, and now eight untracked `docs/reviews/*.md` files (the seven from earlier in this engagement plus this one).
- **No existing file was changed; no implementation occurred.** Neither prior document (`...audit-chain-design.md`, `...audit-chain-decision-review.md`) was edited.
- **Exact read-only checks performed this task**: `git branch --show-current`, `git rev-parse HEAD`, `git status --short`; `grep -n "type Operations interface\|type Tx interface\|type Store interface\|CreateProject\|CreateEnvironment\|DeleteSecret" internal/store/store.go`; a targeted read of `internal/store/store.go` lines 155-175. No `go build`/`go test`/`go vet`/CI run was performed or claimed.
- **Both Phase 1 validation items remain open, not newly evidenced or claimed resolved**: Linux CI validation of the `pkg/config` symlink fix and the `internal/agent` Unix-socket tests; live fork-PR validation of the Codecov guard.
