
# `dso central` Phase 2 Step 5.3 — Audit-Chain Design Review and Decision Preparation

**Date:** 2026-10-25
**Status:** Review and decision-preparation document only. No Go source, test, schema, migration, CI workflow, dependency, or configuration file was modified. No audit table, hash computation, MAC/signature, verification command, retention, or restore behavior was implemented. The original Step 5.3 design document was not edited or replaced.

---

## 1. Purpose and scope

Review `docs/reviews/2026-10-25-central-store-phase2-audit-chain-design.md` (the Step 5.3 design) against the current repository, verify its consequential claims rather than trust them by assumption, and produce a decision-ready assessment of its seven unresolved approval gates (§12 of that document) — alternatives, implications, a recommendation per gate, and the cross-decision dependencies between them. This document makes recommendations only; it does not approve anything on the product owner's behalf.

## 2. Repository state and verification method

**Initial state** (checked before any analysis, per the mandatory safety check):
- Branch: `feature/webui-phase1`
- HEAD: `ccfc0b55b7ce8630d395e9dd1e29d301d6010e61`
- `git status --short`: unchanged from the state recorded at the end of Step 5.3 — ten pre-existing `.claude-flow/*` modifications (predate this entire multi-step engagement), two uncommitted Phase-1 close-out fixes (`.github/workflows/coverage.yml`, `pkg/config/config.go`), and six untracked `docs/reviews/*.md` files (the five produced earlier in this engagement plus the Step 5.3 design itself).
- All five prerequisite documents confirmed present by direct file check (re-run this session; see command list in the close-out section).

**Verification method**: re-ran the exact source checks Step 5.3 itself relied on (not merely re-reading Step 5.3's prose) to confirm no drift since that document was written earlier in this engagement:
- `grep -n "the 15 autocommit\|the 4 operations" internal/store/sqlite/tx.go` — confirms Step 5.3 §2.2's claim of 15 autocommit + 4 `WithTx`-covered operations, unchanged.
- `grep -rn "CREATE TABLE" internal/store/sqlite/migrations/` — confirms exactly five tables (`store_meta`, `projects`, `environments`, `secrets`, `secret_versions`), **no audit table**, consistent with Step 5.3 §2.3's finding.
- File-existence checks on all five prerequisite documents.

No `go build`/`go test`/`go vet` was run in this review — not required to verify documentation claims against static source, and not claimed.

## 3. Review findings

Section-by-section verification of the Step 5.3 design against current source (not just re-stating its prose):

- **§2 (verified implementation findings)** — **Confirmed, re-verified this session.** The 11 `Service` methods, the 15-autocommit/4-transactional split in `internal/store/sqlite/tx.go`, and the absence of any audit table in the two migration files all check out exactly as the design states. No contradiction found.
- **§4 (event coverage, list-operation exclusion)** — **Internally consistent, not independently re-verified against a runtime log of actual call frequency** (no such data exists to check against; this is a design choice, not a factual claim about the codebase). The rationale given (metadata-only, already-disclosed-in-clear, flood-of-low-value-events) is sound and consistent with Step 5.1 §8's already-accepted position that secret names/slugs are stored in clear.
- **§5 (schema, identity attribution)** — **Confirmed.** The `verified_identity` vs. `actor_label` split is a genuinely new design construct (no equivalent field exists in the current schema), correctly distinguished from `created_by`/`disabled_by` (re-confirmed still plain `TEXT`, re-grepped this session). No contradiction.
- **§6 (integrity, rollback limitation)** — **Confirmed, and the required explicit statement (a hash chain does not prove the latest event wasn't deleted, nor that the database wasn't rolled back) is present and correctly worded** — this review does not find it softened or hedged away anywhere in the original document.
- **§7 (transaction atomicity)** — **Confirmed as the single most load-bearing, correctly-grounded section.** The claim that `CreateSecret`/`AddVersion`/`DisableVersion` can gain atomic audit writes "with zero new transaction-management code" is accurate: `Store.WithTx`'s callback signature (`func(ctx context.Context, tx store.Tx) error`) already provides a transaction-scoped handle an audit insert could use. The claim that `DeleteSecret` and the other autocommit operations cannot, without a store-level change, is equally accurate — `DeleteSecret` is explicitly listed among the 15 autocommit operations in `tx.go`'s own section comment, re-confirmed this session.
- **§8 (access control, privacy)** — **Internally consistent with the frozen Step 5.2 decisions** — correctly does not claim the audit chain solves the same-UID problem, correctly treats audit-history read authorization as "not designed here, deferred to Step 5.2's matrix being extended," which this review treats as an open gate (Gate 4 below), not a settled answer.
- **§9 (retention/backup/restore)** — **Internally consistent**, and correctly flags that an unreviewed restore operation is "operationally indistinguishable from an attack" — this is the basis for Gate 7 below.
- **§10 (testing strategy)** — Design-only, as instructed; no code or tests exist to verify against. No contradiction found, but also nothing to independently verify (there is nothing in the repository yet that these proposed tests describe).
- **§11 (residual risks)** — Consistent with, and a fair restatement of, §6/§7/§8's individual limitations; no new unstated risk found.
- **§12 (seven gates)** — Assessed in full in §4 below.
- **§13 (recommendation/acceptance criteria)** — The recommendation (hash chaining, not MAC; atomic for the `WithTx`-covered three; disclosed non-atomicity for the rest) is consistent with, and does not contradict, §6/§7's own analysis. This review's job is to test whether that recommendation holds up gate-by-gate, not merely to note that the document is self-consistent — see §4.

**No finding in the original document was found to be factually unsupported by current source.** Nothing in this review downgrades a prior "Verified" claim to "Unverified." The scrutiny below is therefore about the *decisions*, not about correcting the *facts* — except for one scope correction identified in Gate 2 below, which corrects the *framing* of an already-accurate fact, not the fact itself.

## 4. Assessment of the seven approval gates

### Gate 1 — External trust anchor for rollback detection

- **Decision needed**: is rollback detection in scope for the *initial* audit-chain milestone, or explicitly deferred?
- **Alternatives**: (i) in scope now — design and build an external anchor (a separate file, a remote log, or an operator-run compare-to-previously-recorded-value step) alongside the initial hash chain; (ii) deferred — ship the in-database hash chain alone, with the limitation documented plainly (as the design's §6.2 already requires), and revisit once a concrete rollback-detection need is demonstrated.
- **Security implications**: deferring means the single gap Step 5.1/5.3 both call out explicitly (database-file-alone rollback) remains fully open for however long the deferral lasts. This is not a *new* gap being introduced — it is the same gap the whole-database restore mechanism already has today (`docs/central-store-m1.md:61,227`), so deferring the audit chain's own anchor does not make the product's overall security posture worse than it already is; it simply means the audit chain inherits an existing limitation rather than closing it.
- **Reliability/availability implications**: an external anchor is one more moving part (another file or endpoint that must itself be reachable/writable) — if poorly designed, it could become a new availability dependency for an operation (e.g., every commit requiring a successful write to a remote log) that the current single-SQLite-file design does not have.
- **Implementation complexity/maintenance cost**: non-trivial — this is explicitly flagged in the design's §6.1 as "not proposed in detail" precisely because it is a real design effort in its own right (where does the anchor live, who backs it up, what happens if it's unreachable).
- **Compatibility with existing design**: no conflict either way — the in-database chain (Gate 3's subject) is a prerequisite either way; the anchor is additive.
- **Recommendation**: **defer.** The product's own stated identity (self-hosted, single-operator, Docker Compose-native, deliberately avoiding mandatory external dependencies — per the complete-product audit §9) cuts against introducing a new external dependency (a remote log, a second file an operator must separately protect and back up) for a milestone whose primary, already-substantial value (tamper-evidence for in-place row edits and reordering) does not require it.
- **Residual risk after deferring**: the exact, already-disclosed rollback gap remains open for the audit chain, same as for the rest of the database. This is acceptable *only* if documented with the same clarity the design's §6.2 already uses — this review does not find that documentation lacking.
- **Blocks initial implementation?** **No — safely deferrable.** The initial hash chain delivers real value without it.

### Gate 2 — Transaction coverage (autocommit vs. `WithTx`)

- **Decision needed**: extend `WithTx` coverage to the autocommit *write* operations, or accept documented non-atomic audit recording for the initial milestone?
- **Scope correction found in this review**: re-checking the full 15-item autocommit list in `tx.go` (`CreateProject`, `GetProjectBySlug`, `GetProject`, `ListProjects`, `CreateEnvironment`, `GetEnvironmentBySlug`, `ListEnvironments`, `GetSecret`, `ListSecrets`, `DeleteSecret`, `GetVersion`, `GetLatestVersion`, `ListVersions`, `GetMeta`, `ListMeta`) — only **three** of these fifteen are *writes*: `CreateProject`, `CreateEnvironment`, `DeleteSecret`. The remaining twelve are reads and need no audit-atomicity decision at all, since the design's §4 already excludes most reads from generating events in the first place. **This narrows the design's own §7.2 framing**, which referred to "twelve... write operations" — this review finds that figure conflated the full autocommit-list count with the write subset of it; the real open question concerns 3 operations, not 12.
- **Alternatives**: (i) extend `WithTx` to these 3 operations now; (ii) accept documented non-atomic audit recording for just these 3, initially.
- **Security implications**: non-atomicity here means a narrow, specific window (a project/environment created or a secret soft-deleted, with its audit event failing to persist) — not a confidentiality issue, an availability/completeness-of-evidence issue for exactly 3 operation types.
- **Reliability/availability**: extending `WithTx` to 3 simple, low-contention operations is low operational risk; these are not the hot-path, high-frequency operations (`GetValue`/reads are far more frequent and are reads, not writes, so unaffected either way).
- **Implementation complexity/maintenance cost**: low for 3 operations specifically — contrary to the design's framing as a large, 12-operation refactor, correcting the operation count (above) significantly *lowers* this gate's actual cost. Each of the 3 already has a corresponding `Store` method; wrapping each in `WithTx` is a small, local, well-tested-pattern change (the same pattern the existing 4 transactional operations already use).
- **Compatibility**: fully compatible — `WithTx` already exists and is the established pattern; this is "use it 3 more times," not "invent something new."
- **Recommendation**: **extend `WithTx` coverage to `CreateProject`, `CreateEnvironment`, and `DeleteSecret` as part of the initial milestone**, revising the design's §7.2 framing downward from "12 operations, possibly out of initial scope" to "3 operations, low cost, in scope." This is a stronger recommendation than the design document made, based on the corrected operation count.
- **Residual risk even after extending**: the other 9 verified-read autocommit operations remain outside any transaction, but since they generate no audit event under §4's own design, this is a non-issue, not a residual risk.
- **Blocks initial implementation?** **Does not block** — but given the corrected, low cost, this review recommends doing it as part of the initial milestone rather than deferring, which differs from treating it as a separate, later decision.

### Gate 3 — Separate audit-purpose MAC/signing key

- **Decision needed**: plain hash chain, or hash chain + keyed MAC/signature with a key independent of the existing root-key/DEK hierarchy?
- **Alternatives**: as in the design's §6.1 — (b) hash-only, (c) hash + MAC/signature.
- **Security implications**: a MAC only adds value against an attacker who has database-file access but *not* the MAC key — i.e., a *partial* compromise. DSO's existing, already-approved threat model (Step 5.1 §8, reconfirmed here) treats full OS-account/root compromise as explicitly out of scope; the MAC's marginal benefit applies to an attacker profile narrower than, and in between, "no access" and "the already-accepted out-of-scope full compromise."
- **Reliability/availability**: a new key means a new failure mode — the key file could itself become unreadable/corrupted, which (unlike the data-encryption key, whose loss already makes all secrets permanently unrecoverable by design, per `docs/central-store-m1.md:144-145`) would uniquely disable *audit verification* while leaving secret data itself fully intact and usable. This is a real, distinct operational risk worth naming explicitly: an audit-key outage degrades security posture (no verifiable tamper-evidence) without degrading the product's core function (secret storage/retrieval still works) — a different failure shape than any other key in this system, and one the design doc did not name this precisely.
- **Implementation/maintenance cost**: a new key means new generation, storage (fail-closed per the accepted P-01 policy), backup, and potential rotation lifecycle — real, ongoing cost.
- **Compatibility**: no technical incompatibility with the existing `KeyProvider` abstraction (the design's §6.1 already notes it *could* reuse that abstraction for a new purpose) — the cost is operational, not architectural.
- **Recommendation**: **plain hash chain (b), not MAC (c), for the initial milestone** — agreeing with the design's own recommendation, for the same reason given there (the marginal threat it defends against is narrower than the threat class already accepted as out of scope) plus the newly-named distinct-failure-mode concern above, which further weighs against adding it prematurely.
- **Residual risk after choosing (b)**: an attacker with database-file access (but not full OS compromise) could, in principle, recompute a self-consistent replacement chain after tampering — a real, named, accepted gap, not a hidden one.
- **Blocks initial implementation?** **No — the simpler option is also the recommended starting point**, so there's no blocking tension here; implementing (b) now does not foreclose adding (c) later if a concrete threat later justifies it.

### Gate 4 — Audit-history read authorization

- **Decision needed**: what gates read access to the audit table, given Step 5.2's frozen, approved identity model?
- **Alternatives**: (i) no distinct gate — same OS-file-permission boundary as the rest of the database, today, with a placeholder "extend Step 5.2's matrix later" once Option B exists; (ii) design a new, audit-specific read policy now, ahead of Option B's own implementation.
- **Security implications**: option (ii) would require inventing a policy ahead of the mechanism (Option B) that would actually enforce it — there is no socket yet, so any "new policy" today is purely aspirational, exactly the kind of "policy intention presented as enforced control" Step 5.2 §15 was written specifically to warn against.
- **Reliability/maintenance**: option (i) adds zero new mechanism; option (ii) adds a policy with nothing yet to enforce it, which risks exactly the documentation/reality mismatch Step 5.2 flagged.
- **Recommendation**: **(i) — defer the specific rule to Step 5.2's matrix extension, at the time Option B is actually implemented.** This is consistent with, not a reversal of, the design's §8's own "not designed in detail here" position — this review's job on this gate is to confirm that deferral is the *right* call, not to produce the missing rule itself.
- **What remains enforceable under the approved Step 5.2 identity model today**: nothing beyond the existing OS-file-permission/`securefs` boundary — there is no narrower "audit reader" role distinguishable from "anyone with database file access" until Option B exists.
- **Blocks initial implementation?** **No.** The audit table can be built and populated under today's existing access boundary; a finer read-policy is a Step 5.2-implementation-time concern, not an audit-chain-design blocker.

### Gate 5 — Retention and archival

- **Decision needed**: is unbounded append-only growth acceptable for the initial milestone, and if not, what's needed before pruning/archival?
- **Alternatives**: (i) unbounded growth, no pruning, for the initial milestone; (ii) design and build archival/export now, before any audit event is ever written.
- **Security implications**: pruning before a sound archival design exists directly breaks the hash chain's verifiability for the pruned range (the design's §9 already states this correctly) — so (ii) without first resolving Gate 1's external-anchor question is actually *premature*, since a sound archive needs to know what it's anchored against.
- **Reliability**: unbounded growth is a real, if slow-developing, operational concern for long-lived deployments — but DSO's stated target deployment (single-operator, single-host, Docker Compose-native) is not a hyperscale multi-tenant system; the growth rate of `CreateProject`/`CreateSecret`/`AddVersion`/`GetValue --reveal`/`DisableVersion`/`DeleteSecret` events for a realistic single-operator deployment is almost certainly modest for a long time (this review has no production telemetry to cite — flagged as missing evidence below, not asserted as fact).
- **Recommendation**: **(i) — unbounded growth is acceptable for the initial milestone**, explicitly revisited once (a) real growth data exists from actual usage, or (b) Gate 1's external-anchor design is settled (since a sound archival design depends on it, per the security implication above).
- **Missing evidence**: no production usage data exists to confirm "modest growth" is actually true at scale — this review flags that as an assumption, not a verified fact, and notes it could be obtained by instrumenting a future beta/implementation phase's event-count-per-unit-time, rather than guessed now.
- **Blocks initial implementation?** **No — safely deferrable**, and should be deferred until Gate 1 is settled regardless, since pruning design depends on it.

### Gate 6 — Audit-data corruption behavior

- **Decision needed**: if the audit table itself becomes corrupted while the rest of the database is intact, does `Service` fail closed (refuse further audit-dependent writes) or continue operating while loudly flagging the gap?
- **Alternatives**: (i) fail closed on writes that would normally produce an audit event; (ii) continue, with the audit-write failure surfaced via the existing logging convention (per the design's §4 last matrix row) but the business operation still succeeds.
- **Security implications**: (i) prioritizes audit-completeness (nothing happens without a record) at the cost of availability (a corrupted audit table could halt all secret creation/rotation/deletion); (ii) prioritizes availability at the cost of a documented gap in the record for however long the corruption persists.
- **Reliability/availability**: this is the sharpest trade-off among all seven gates — (i) turns an audit-subsystem fault into a full outage of `dso central`'s write path, which is a severe availability cost for a self-hosted single-operator tool whose core job (per the product's own identity) is reliable secret delivery, not governance.
- **Recommendation**: **(ii) — continue, loudly flag, for the initial milestone**, consistent with the design's §7.3's own proposed asymmetric handling for the (corrected, per Gate 2) 3 autocommit write operations, and explicitly *not* applying (i)'s stricter fail-closed posture to audit specifically — reserving fail-closed behavior (already correctly used elsewhere in this codebase, e.g. the REST API's bind-time check, per the earlier security audit's F-02) for cases where the *security property itself* (not just its record) would otherwise be silently lost, which is not the case here: a secret created without a corresponding audit row is still a correctly-encrypted, correctly-stored secret — only the evidence trail is degraded, not the secret's own confidentiality/integrity.
- **Risk of (ii)**: an operator who doesn't monitor logs could run for a meaningful period with a silently-degrading audit trail — this is why "loudly flag" must mean something more durable than a single log line (e.g., a persistent, operator-visible health-check-style indicator), a detail this review flags as needing product-owner input rather than resolving unilaterally.
- **Blocks initial implementation?** **No**, but the exact mechanism for "loudly flag" (health indicator vs. log-only) should be settled before implementation, not left ambiguous.

### Gate 7 — Restore audit event

- **Decision needed**: must a future restore operation (Step 5.4, not yet designed) itself emit an audit event marking the restore?
- **Alternatives**: (i) yes — restore emits its own event; (ii) no — restore is not itself audited.
- **The dependency the task explicitly asks to be analyzed**: if a restore rolls back to a snapshot taken *before* the restore event would have been written, the restore event itself is necessarily absent from the restored chain — **a restore event cannot retroactively appear in a snapshot taken before it happened.** This means (i) only helps distinguish a *legitimate, forward-looking* restore (one that is itself recorded going forward, after the restore completes, as the first new event in the resumed chain) from a *malicious* rollback that simply stops the chain at an earlier point and never adds anything after — it does **not** help an observer looking only at the restored data itself tell "this chain just happens to end here legitimately" apart from "this chain was truncated by an attacker," **unless** that determination is made via the external anchor (Gate 1), not via anything internal to the post-restore chain.
- **Recommendation**: **(i) — yes, record a restore event, but explicitly only as a forward-looking marker useful for the *next* legitimate operator action, not as a mechanism that by itself closes the rollback-detection gap.** This gate is therefore **entirely dependent on Gate 1's resolution** to be meaningful as a security control rather than a purely operational convenience (knowing "we restored on this date" in future audit queries).
- **Blocks initial implementation?** **Not applicable yet** — restore does not exist (Step 5.4 hasn't started); this is a forward note for that future step, not a gate the current audit-chain milestone needs to resolve now. Recorded here only because the task asked for the dependency analysis.

---

## 5. Cross-decision dependencies (explicit, as required)

| Dependency | Relationship |
|---|---|
| **Transaction atomicity (Gate 2) ↔ audit failure policy (Gate 6)** | Gate 2's fix (extending `WithTx` to 3 operations) *eliminates* the specific failure mode Gate 6 is about, for those 3 operations — the two gates are complementary, not conflicting: Gate 2 shrinks the surface area Gate 6's trade-off has to cover. After Gate 2 is resolved as recommended, Gate 6's "audit write fails independently of the business write" scenario applies to strictly fewer operations than the design originally scoped. |
| **External trust anchor (Gate 1) ↔ backup/restore/rollback detection (Gate 7)** | Gate 7's restore-event recommendation is **only meaningful if Gate 1 is eventually resolved** — recording a restore event without an external anchor produces a record that a sufficiently capable attacker (one who can also roll back the database) can simply omit from their rollback target, making the restore-marker a convenience for honest operators and no obstacle at all to a dishonest one. |
| **Hash chaining vs. MAC/key management (Gate 3) ↔ retention/pruning (Gate 5)** | If Gate 3 ever moves to (c) (MAC/signature) in the future, any archival design under Gate 5 must also carry forward the correct key-id/version per archived event (the design's §6.1 "Backup/restore" row already notes this) — deferring Gate 3 (as recommended) also means Gate 5's eventual archival design doesn't need to solve this yet, simplifying it. |
| **Retention/pruning (Gate 5) ↔ continued chain verifiability** | Any future pruning design must not break verification of the *retained* range — this means archival (if ever built) must preserve enough of the pruned-away tail's final hash to serve as the new "genesis" anchor for the retained chain, which is itself a miniature version of Gate 1's external-anchor problem, applied internally. Resolving Gate 1 first would likely simplify this rather than solving it twice independently. |
| **Audit-history access (Gate 4) ↔ same-UID limitation (Step 5.2 §11)** | Whatever read rule Gate 4 eventually settles on, it inherits the identical same-UID limitation already accepted for every other `dso central` operation under Option B — Gate 4 cannot produce a *stronger* identity guarantee for reading audit history than Step 5.2 already established for everything else; this review explicitly rules out treating Gate 4 as an opportunity to quietly introduce a stronger mechanism through the back door, which would reopen a frozen Step 5.2 decision. |
| **Restore events (Gate 7) ↔ restoring a snapshot that removes the restore event itself** | Already analyzed in detail under Gate 7 above — flagged here again because it is the one dependency that produces a genuinely **contradictory guarantee** if not understood correctly: "every restore is recorded" cannot be true of the *restored* chain's own internal view of itself, only of the chain as continued *after* the restore, and only as strongly as whatever external anchor (if any) exists to cross-check it. |

**No combination identified above resolves itself without a product-owner decision.** Each either depends on another gate's resolution or is independently deferrable, as shown in the matrix below.

---

## 6. Decision matrix

| Gate | Recommended decision | Rationale | Consequence if deferred | Requires approval before implementation? |
|---|---|---|---|---|
| 1. External trust anchor | Defer | Marginal near-term value vs. new external dependency, against product's self-hosted/no-mandatory-external-dependency identity | Rollback-detection gap stays open, same as the rest of the database already has — no new exposure, just an unclosed existing one | No — safely deferrable, but must stay documented, not silently dropped |
| 2. Transaction coverage | Extend `WithTx` to `CreateProject`, `CreateEnvironment`, `DeleteSecret` now (corrected scope: 3 operations, not 12) | Low cost, reuses existing pattern, meaningfully shrinks Gate 6's problem surface | If deferred: all 3 inherit Gate 6's non-atomic-audit trade-off unnecessarily, for no real cost savings | **Yes, in the sense that the scope correction itself should be reviewed** — but the underlying work is low-risk enough to be approved together with the overall milestone, not a separate blocking gate |
| 3. MAC/signing key | Plain hash chain (b), not MAC (c) | Marginal benefit narrower than the already-accepted out-of-scope threat (full compromise); introduces a new, uniquely-shaped failure mode (audit-only outage) | If deferred (i.e., (b) now, revisit (c) later): no loss — (b) is a strict subset of (c)'s guarantees and doesn't block adding (c) later | No — (b) is both the recommended and the lower-cost starting point |
| 4. Audit-history read authorization | Defer to Step 5.2 matrix extension at Option B implementation time | No mechanism exists yet to enforce any rule today; inventing one now risks a policy/reality mismatch | If deferred: today's existing OS-file-permission boundary continues to apply, unchanged, which is already correctly disclosed | No |
| 5. Retention/archival | Unbounded growth accepted initially | Target deployment profile (single-operator, single-host) makes near-term growth concerns speculative without usage data; premature archival design would duplicate Gate 1's anchor problem | If deferred: no immediate harm; revisit once real usage data exists or Gate 1 resolves | No |
| 6. Audit-data corruption behavior | Continue + loudly flag (not fail-closed) | Protects availability of the core secret-management function; the secret's own confidentiality/integrity is never at stake from an audit-table fault alone | If the "loudly flag" mechanism is left vague: risk of silent, extended audit degradation going unnoticed | **Yes — the exact flagging mechanism (persistent indicator vs. log-only) needs explicit product-owner input before implementation**, even though the overall fail-open-with-flagging *direction* is lower-risk to approve now |
| 7. Restore audit event | Record it, but explicitly only as a forward-looking convenience, not a rollback-detection mechanism | A restore event cannot retroactively appear in a snapshot taken before it existed; its security value is entirely contingent on Gate 1 | Not yet applicable — Step 5.4 (restore) hasn't started; revisit together with Gate 1 when Step 5.4 begins | Not yet — deferred by necessity, no restore design exists |

## 7. Recommended approval order

1. **Gates 2 and 3 first, together** — both are low-cost, non-dependent on anything else, and both narrow (not widen) the scope of every later gate (Gate 2 shrinks Gate 6's problem; Gate 3's "no" simplifies Gate 5's eventual archival design). Approving these first gives the clearest, cheapest path to *starting* implementation.
2. **Gate 6 next** — specifically, the exact "loudly flag" mechanism, since it is the one item in the matrix still flagged "requires approval" beyond the general direction, and it has no dependency on Gates 1, 4, 5, or 7.
3. **Gate 4 can be approved in principle at any time (defer), but its concrete rule is not actually needed until Option B's own implementation** — so it need not block this milestone's approval sequence at all; note it as "deferred, no action needed now" rather than sequencing it.
4. **Gates 1 and 5 together, last, and only if/when real deployment data or a concrete rollback-detection need emerges** — Gate 5's archival design is explicitly dependent on Gate 1's resolution (§5), so approving them as a pair avoids having to revisit Gate 5's design twice.
5. **Gate 7 is not yet actionable** — it has no implementation to attach to until Step 5.4 begins, and its security value is entirely contingent on Gate 1 (§5) — defer formally reconsidering it until Step 5.4 is itself proposed, rather than approving it in isolation now.

**Decisions that must be settled together**: Gate 1 + Gate 5 (archival depends on the anchor); Gate 1 + Gate 7 (restore-event security value depends on the anchor). **Decisions that can be independently approved now, with no dependency on anything else**: Gates 2, 3, 4, 6.

## 8. Questions requiring product-owner input

1. Confirm or reject the scope correction in Gate 2 (3 write operations needing `WithTx`, not 12) before approving it as part of the initial milestone.
2. Specify the exact mechanism for Gate 6's "loudly flag audit-subsystem failure" — a persistent, operator-visible health indicator (e.g., surfaced via `dso doctor` or the existing `/health`-style pattern) versus log-only, since "flag it" alone is not yet an implementable spec.
3. Confirm whether Gate 5's "unbounded growth is acceptable initially" assumption is consistent with the product's actual target deployment scale, or whether there is deployment data (even anecdotal) this review does not have access to that would change that assessment.
4. Confirm that Gates 1 and 7 are genuinely to be revisited together, and at what trigger point (a stated future milestone, a concrete customer/operator request, or a fixed timeline) rather than left open-ended.

## 9. Implementation readiness assessment

**The audit-chain design is ready for implementation to begin on Gates 2, 3, 4, and 6's recommended directions**, which together cover the core of what the initial milestone needs: a hash-chained, append-only audit table, atomic writes for the operations that already run inside `Store.WithTx` plus the three newly-identified low-cost additions (Gate 2), no new key-management surface (Gate 3), today's existing access boundary for reads (Gate 4), and an availability-preserving corruption policy (Gate 6, pending the exact flagging-mechanism detail in §8 item 2). **Gates 1, 5, and 7 are not blockers** — they are legitimately deferrable, with their deferral explicitly documented rather than silently assumed, and with the dependency between them (§5) now made explicit so a future revisit doesn't have to rediscover it.

**This document does not itself authorize implementation to begin** — per the task's explicit instruction, it stops short of that and awaits product-owner approval of the directions recommended above.

## 10. Acceptance criteria for closing the design review

This review should be considered closed only once:
- The product owner has explicitly responded to each of §8's four questions.
- The scope correction in Gate 2 (3 operations, not 12) is either confirmed or the original 12-operation framing is explicitly reinstated with a stated reason.
- The approval order in §7 is either accepted as proposed or explicitly reordered.
- Gates 1, 5, and 7's deferral is explicitly acknowledged as a deferral (not silently treated as "resolved: no") by whoever approves the overall milestone.

---

## Close-out

- **Document created**: `docs/reviews/2026-10-25-central-store-phase2-audit-chain-decision-review.md` (this file).
- **Original Step 5.3 design document**: not edited, not replaced.
- **Branch/HEAD, initial and final**: `feature/webui-phase1` @ `ccfc0b55b7ce8630d395e9dd1e29d301d6010e61` — unchanged throughout this task.
- **No existing file was changed.** No Go source, test, schema, migration, CI workflow, dependency, or configuration file was touched. No audit table, hash computation, MAC/signature, verification command, retention, or restore behavior was implemented.
- **Nothing was staged, committed, pushed, tagged, or published.**
- **Exact read-only checks performed this task**: `git branch --show-current`, `git rev-parse HEAD`, `git status --short`; file-existence checks on the five prerequisite documents; `grep -n "the 15 autocommit\|the 4 operations" internal/store/sqlite/tx.go`; `grep -rn "CREATE TABLE" internal/store/sqlite/migrations/`. All read-only. No `go build`/`go test`/`go vet`/CI run was performed or claimed.
- **Both Phase 1 validation items remain open, not newly evidenced or claimed resolved by this task**: Linux CI validation of the `pkg/config` symlink fix and the `internal/agent` Unix-socket tests; live fork-PR validation of the Codecov guard.
