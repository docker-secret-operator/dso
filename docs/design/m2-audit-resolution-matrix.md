# M2 consistency audit — resolution matrix (F-01 … F-28, N-01 … N-11)

Date: 2026-10-21 (fifth pass) · Status: **documentation only; unreviewed by the owner.** Nothing in this file is
implemented. Only M2.1 (`Store.WithTx`, commit `8c3763c`) exists. **M2.2 and later are not authorized.** No
production-readiness claim.

**Source documents available for this revision:** `m2-identity-authorization-audit-design.md` (D),
`m2-implementation-plan.md` (P), this matrix, and `docs/reviews/2026-10-16-m0-m1-independent-review.md` (an
M0/M1 review, not the M2 audit). **The original M2 audit report (F-01…F-28) is not in the repository** and was
not recreated: the "Finding" wording below restates the findings as they were delivered in the review
conversation and cannot be verified against a primary artifact. Statuses judge whether the documented
resolution is sufficient and consistent. Severities are the audit's.

**Status labels.** `RESOLVED`: demonstrably addressed by the documents (and, for M1/M2.1 facts, the source).
`PARTIALLY_RESOLVED`: incomplete, accepted-with-residual, or the finding's resolution is the content of a
decision the owner has not been able to confirm (blocked behind D21 or B-1). `OPEN`: unaddressed (none).
`UNVERIFIABLE`: evidence insufficient (none individually; see the limitation above). `ON_HOLD`: explicitly
deferred by the owner (D21). `BLOCKED`: a design conflict created by an owner decision that the owner has not
resolved (B-1). **"RESOLVED" concerns the documents only: no acceptance criterion in this file has been
executed.**

Document references: **D** = design, **P** = plan.

## Owner decisions integrated in this revision (recorded as given; nothing else implied approved)

| Decision | Owner decision | Where | Consequence |
|---|---|---|---|
| D24 | Approved; N-01 and N-02 accepted as documented limitations; heartbeat excluded (future option) | D §6.4, §0, §9; P §6 | F-01 RESOLVED; visible in AC-CK-17, R34, R35 |
| D23 | Approved **restricted**: Mode R = proof the process can unwrap the root-key canary, not a credential, database-owner-equivalent; read-only verification and incident-flagging only; `--record`, `rotate-key` session-only; **must not release quarantine**; read-only use unrecorded | D §5.8, §8; P AC-VR-19/23; R32 | creates **B-1 (BLOCKED)** |
| OQ-D20-1 | Metadata listings allowed while quarantined; secret reads and mutations denied; exact commands/fields defined | D §5.8; P AC-VR-21/25; R37 | F-21, N-03 RESOLVED |
| D20 release policy | Identity-state findings block release; revalidation in the same transaction as the release | D §5.8 T5; P AC-VR-13/24 | F-14 RESOLVED; authorization path BLOCKED (B-1) |
| D13, D15, D22 | D13 not confirmable until D21 is resolved; D15 aligned with D24 but depends on the authenticated `created` (D13); D22 requirement (precise "counted", deterministic invariant) integrated | D §5.7, §5.9, §6.4; P AC-FL-16 | D13/D15 confirmation PENDING; D22 confirmation PENDING |
| D21 | **ON_HOLD** (unchanged) | P §6 "Remaining" item 2 | F-19, commitment part of F-09 not resolved |

## F-01 … F-28

Columns: **Sev** (audit) · **Section** · **Invariant / requirement that resolves it** · **Criteria** ·
**Counterexample / limit found** · **Demonstrable from the documents?**

| ID | Sev | Section | Invariant / requirement | Status | Criteria | Counterexample / limit found | Demonstrable? |
|---|---|---|---|---|---|---|---|
| F-01 | Blocker | D §5.2, §5.4, §6.4, §7.1, §7.3 | `ts_high_water` durable, monotone, equals the newest event's `ts`; events stamped `max(now,hw)`; `now_eff=max(now,hw)`; strict expiry; only `clock_reset` lowers `hw` | RESOLVED | AC-CK-01/02/04/13/14/16/17/18, AC-AU-09, AC-TR-08 | N-01 (rollback later than the newest event), N-02 (forward jump) — **accepted by the owner**; no claim of revival-proofness under every rollback | yes (documents) |
| F-02 | High | D §5.9, §7.2, App. B | structure replayed; M1 rows adopted at `upgrade` and `reset-epoch`; no false `ST-ORPHAN` | PARTIALLY_RESOLVED | AC-MG-03/09, AC-ST-17/23 | secrets/versions are not replayed (out of scope, stated); parent decision **D13 unconfirmed (blocked by D21)** | yes; confirmation blocked |
| F-03 | High | D §5.8, §7.3 | reset order: key row → `epoch_start` → head jump → +1 per adopted event → incident | RESOLVED | AC-VR-14/18, AC-TR-07 | none after ordering fix | yes |
| F-04 | High | D §7.3, §5.8 | bookkeeping columns (`quarantined_at`, `auth_*`) independent of head consistency | RESOLVED | AC-TR-08, AC-VR-12/13/19 | a raw writer can still clear the flag (stated: defence in depth) | yes |
| F-05 | High | D §5.8, §6.4, §8 | Mode S or restricted Mode R; login refused when no append possible | PARTIALLY_RESOLVED | AC-VR-19/23, AC-CK-02 | **B-1**: Mode R may not release and no session can be obtained while quarantined ⇒ no permitted release path after session expiry | partly; release path BLOCKED |
| F-06 | Med | D §6.4 | rule (b) revokes tokens created within the last δ before a backward step (stated) | PARTIALLY_RESOLVED | AC-CK-05/10 | over-revocation is deliberate; depends on the authenticated `created` (D13) | yes; D15 confirmation blocked |
| F-07 | Med | D §6.4 | valid iff `now_eff < expires_at`; idle likewise; frozen-time bound ≤ tolerance | RESOLVED | AC-CK-14/17 | N-05 bound | yes |
| F-08 | Med | D §5.9, §6.4, App. B | `created` authenticated by `token.issue` and compared; rules (b)/(c) read it | PARTIALLY_RESOLVED | AC-CK-15, AC-ST-24 | forging `created` plus its event needs the audit key (out of scope); **D13 unconfirmed** | yes; confirmation blocked |
| F-09 | Med | D §5.8, §5.9 | `--state` never runs on a non-`OK` chain; unavailable key ⇒ epoch `UNVERIFIABLE`, exit 11 | PARTIALLY_RESOLVED | AC-ST-12/15, AC-VR-22 | the commitment part is **D21 (ON_HOLD)** | refusal rule yes; commitments not |
| F-10 | Med | D §5.8 | one exit-code table, precedence 13>12>10>11>15>14>0 | RESOLVED | AC-VR-02, AC-CL-06, AC-AU-11 | higher-precedence codes hide lower ones in the code only (all printed in JSON) | yes |
| F-11 | Med | D App. B, §5.9 | schema keys aligned (`old_hw`, `new_hw`, `*_a/_b/_c/tokens_kept`, `created`, `replaced_by`); `clock_reset` count rule | RESOLVED | AC-ST-24/25 | none | yes |
| F-12 | Med | D §2.5 | recovery text uses `audit.epoch_start`/`HISTORICAL` | RESOLVED | AC-VR-15 | none | yes |
| F-13 | Med | D §5.8 | boundary definition, `BOUNDARY`, cross-epoch gapless `seq`, corruption table | RESOLVED | AC-VR-07/14/20 | `MAX(seq)=2^63−1` refused (documented limit) | yes |
| F-14 | Med | D §5.8 T5, T2 | release = verify + `--state` + clear in **one** `IMMEDIATE` transaction; `--record` re-checks head; identity-state findings block release | RESOLVED | AC-VR-03/13/24 | write lock held during verification (stated cost); authorization of release is F-05/B-1 | yes |
| F-15 | Med | D §5.7 | "counted" defined; `A_c ≤ total ≤ A_c+K`; deterministic cases | RESOLVED | AC-FL-12/13/14/16, AC-FL-08 | stalled window under clock regression (stated cost) | yes; D22 confirmation pending |
| F-16 | Med | D §5.9 | `USE-*` only for `outcome=success` operator/service-token actors | PARTIALLY_RESOLVED | AC-ST-22 | scope preserved as the owner required; **D13 unconfirmed** | yes; confirmation blocked |
| F-17 | Med | D §1 fact 1/8, §6.1 | M2.1 facts as committed (4 `s.tx`, 15 autocommit, `_txlock=immediate`, pool 8, 5 s, busy not mapped; marker scope) | RESOLVED | AC-TX-01…06 (P M2.1 table) | none; facts re-checked against source | yes (source) |
| F-18 | Med | P §2, §6 | M2.1 implemented; M2.2 unauthorized; approvals separate | RESOLVED | — | none | yes |
| F-19 | Low | D §5.9, App. B | labels `pw`/`sess-sc`/`tok-sc` | ON_HOLD | AC-ST-14 | lives inside D21 | n/a |
| F-20 | Low | D §5.8 | canary-first classification | RESOLVED | AC-VR-12, AC-MG-08 | none | yes |
| F-21 | Low | D §5.8 | quarantine refuses secret reads, mutations, break-glass appends; listings allowed (fixed fields); bookkeeping continues | RESOLVED | AC-VR-21/25 | listings authorize from identity tables the chain can no longer vouch for (stated) | yes |
| F-22 | Low | D §4.2 | `instance_admin` holds `member:grant/revoke` as §4.2.1 | RESOLVED | AC-DL-15 | none | yes |
| F-23 | Low | D §4.2.2 | tokens persist after issuer loses grant; `emergency revoke` refuses the last admin | RESOLVED | AC-DL-16 | persisted tokens are stated | yes |
| F-24 | Low | P §2, AC lists, R24/R31 | placeholders for AC-AU-08/AZ-04; one `0003` file; R31 retired | RESOLVED | — | an earlier claim was unwritten (N-07, fixed) | yes |
| F-25 | Low | P AC-MG-08 | restore keeps `audit_keys`; verify under right/wrong key | RESOLVED | AC-MG-08 | restore tooling and escrow outside M2 | yes |
| F-26 | Low | D §5.8 | `verify` never writes; the service continues until latched | RESOLVED | AC-VR-04/11 | none | yes |
| F-27 | Low | D §7.2, §7.3 | initial `audit_state`, normal-branch genesis, `INSERT` rule | RESOLVED | AC-MG-03, AC-TR-09 | none | yes |
| F-28 | Low | P AC-VR-01 | status vs quarantine flag | RESOLVED | AC-VR-01 | none | yes |

Counts (from the Status column): **RESOLVED 21**, **PARTIALLY_RESOLVED 6** (F-02, F-05, F-06, F-08, F-09, F-16),
**ON_HOLD 1** (F-19), OPEN 0, UNVERIFIABLE 0 — total 28.

## N-01 … N-11 (new findings from the independent re-review and this pass)

| ID | Sev | Finding | Status | Section / criteria | Limit / dependency |
|---|---|---|---|---|---|
| N-01 | High | Guard cannot see a rollback that stays later than the newest event; "cannot revive" overclaimed | PARTIALLY_RESOLVED (claim corrected; residual **accepted by owner** under D24) | D §6.4, §0, §9; AC-CK-06/17; R35 | heartbeat is a future option, not decided |
| N-02 | Med | Forward jump + any append pins `hw`; `clock reset` revokes legitimate credentials | PARTIALLY_RESOLVED (documented; **accepted by owner**) | D §6.4; AC-CK-05/09; R34 | NTP monitoring is an operator duty |
| N-03 | Med | "Nothing can run while quarantined" was false for unaudited listings | RESOLVED (OQ-D20-1 decided) | D §5.8; AC-VR-21/25; R37 | identity listings denied by assumption (P §6 item 10) |
| N-04 | Low | Issuance time base | RESOLVED | D §6.4; AC-CK-18 | — |
| N-05 | Low | Frozen-time extension | RESOLVED (bounded by tolerance) | D §6.4; AC-CK-17(ii) | — |
| N-06 | Low | Sequence start vs reset on an empty table | RESOLVED | D §5.8; AC-VR-14/20 | — |
| N-07 | Low | Matrix claimed unwritten plan text | RESOLVED | P §2 | — |
| N-08 | Low | Exit 0 vs requested checks; release policy | RESOLVED (policy decided) | D §5.8; AC-VR-13 | — |
| N-09 | Low | Previous key row on reset | RESOLVED | D §5.8 step 2 | — |
| N-10 | Info | Mode R could release a quarantine | RESOLVED (forbidden by D23) | D §5.8 | superseded by N-11 |
| N-11 | High | **Owner decisions conflict:** Mode R may not release, login is refused while quarantined, sessions idle out in 30 min ⇒ no permitted release path | **BLOCKED (B-1)** | D §5.8 "BLOCKED item B-1"; AC-VR-13/19/23; R36 | needs the owner's choice among (a) step-up password re-authentication, (b) unrecorded login (rejected), (c) accept the limitation |

## Remaining blocked / pending (summary)

* **BLOCKED:** B-1 / N-11 (release authorization path). Affects F-05, AC-VR-13/19/23.
* **ON_HOLD:** D21 (key lifecycle, epoch baseline, `--state` refusal rule, labels, 128-bit truncation). Unblocks
  F-19 and the commitment part of F-09; then D13 and D15 can be confirmed. Artifacts needed are listed in P §6.
* **Pending owner confirmation:** D13, D15 (behind D21), D22, the rest of D20.
* **Implementation not performed:** everything except M2.1 (M2.2–M2.11).
* **Tests executed:** only the M2.1 criteria recorded in P "M2.1". **Tests not executed:** every criterion added
  or revised in the documentation passes, including AC-VR-13/19/21/23/24/25, AC-FL-16, AC-CK-13…18, AC-ST-22…25,
  AC-TR-07…09, AC-MG-03/08/09, AC-DL-15/16. **AC-TX-04b-2 remains UNPROVEN (skipped, never passed).**

## Outstanding prerequisites (unchanged)

Root-key escrow/recovery, verified backup/restore tooling, dependency-vulnerability remediation, external audit
anchoring, M3 identity-row protection. M2 approval would not make DSO production-ready.
