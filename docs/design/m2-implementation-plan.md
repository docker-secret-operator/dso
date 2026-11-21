# Central store — Milestone 2 implementation plan and checklist

Date: 2026-10-19 · Status: **DRAFT, revised four times (design reviews 1 and 2; M2.1 pass; resolution of the
final consistency audit, findings F-01…F-28). Only M2.1 (`Store.WithTx`, commit `8c3763c`) is implemented
and committed; M2.2 and every later step are NOT authorized and nothing of them exists.** The fourth pass
is documentation only; the **fifth pass integrates the owner's decisions** (D24, restricted D23, OQ-D20-1, the D20 release policy; D21 stays ON_HOLD; one item BLOCKED, B-1) and is also documentation only. Traceability of every finding: `m2-audit-resolution-matrix.md`. `AC-TX-04b-2`
remains UNPROVEN (skipped, never passed). Nothing here is a production-readiness claim.
Baseline of the M1 facts: `feature/m1-storage-keys` @ `58e711f` (includes M0 remediation); M2.1 = `8c3763c`.
Design: `m2-identity-authorization-audit-design.md` (section numbers below refer to it).

## 0. Review log (2026-10-19)

Reviewer outcome: *approve in principle; revise before implementation.* What changed:

| # | Review item | Where it is now |
|---|---|---|
| 1 | D13 needs an implementable replay specification (blocker) | design §5.9 (state machine, commitments, compared columns, findings, limits) and Appendix B (per-action fields); ACs AC-ST-01…11 |
| 2 | `audit.verify` vs a broken/unavailable chain (blocker) | design §5.8: `verify` is read-only in every state; failed verification never writes to the chain; `--record` only after `OK`; explicit latch/quarantine; recovery = restore or `reset-epoch`; ACs AC-VR-01…10 |
| 3 | D15 clock rollback recovery | design §6.4 (operation table, wait / `clock reset`, expiry materialised as revocation); ACs AC-CK-01…08 |
| 4 | What SQLite triggers enforce | design §7.3 (exact invariants vs application rules; removal detection); ACs AC-TR-01…06 |
| 5 | Audit-failure rules | design §5.7 (F1–F5; "recorded" = committed; no persisted `audit_dropped`; persisted failed-auth window); ACs AC-FL-01…08 |
| 6 | D4: project admin cannot delegate reader/writer | design §4.2.1 (delegation table, no self-grants, last-admin protection), §4.2.2 (token issuer must hold what it delegates), §4.2.3 (collusion residual); ACs AC-DL-01…14 |
| 7 | D9 inconsistent between the documents | design §7.2 and §6 here both state: **in-place upgrade supported, conditional on verified backup and a restore drill** (AC-MG-01/06/07) |

Left unchanged by the first review (its outcome was "approve"; current decision statuses are those in §6, and
nothing here approves D1–D22 as a whole): B (wrapped random audit key), HMAC-SHA256, deny-by-default with
sealed `Principal` + `Handle`, atomic mutation+event and audit-before-plaintext, local-only boundaries, and
`WithTx` as the first implementation step.

### Second review (D21 held; D20/D22 conditional)

| # | Review item | Where it is now |
|---|---|---|
| 1 | D21 commitments need a complete key lifecycle | design §5.9 "Credential commitments": key = HKDF subkey `K_state[v]` of the audit key of the **event's own** `key_version`; behaviour across rotation, epoch reset, missing key (`ST-UNVERIFIABLE`, exit 11, never clean), backup/recovery; ACs AC-ST-13…16, 21 |
| 2 | Baseline at an epoch reset | design §5.9 "Epoch baseline": adopted events for **every** row (live/revoked/disabled) appended with `epoch_start` in one transaction, delimited by authenticated `baseline=` counts, committed under the new key version; explicit non-detection of pre-reset tampering; ACs AC-ST-17…19 |
| 3 | D20 states and transitions | design §5.8: verification status vs quarantine flag vs epoch; transition table T1–T7 (incl. release only after a fresh `OK`); `reset-epoch` from an already-inconsistent database (observations read from `audit_events` directly; trigger exception so it works when `audit_state` is wrong); observations are not proof; `HISTORICAL` ledger; ACs AC-VR-11…18 |
| 4 | D22 concurrency and failure | design §5.7: atomic window procedure, boundary concurrency, crash, write failure, clock regression, restart reporting, accounting invariant; ACs AC-FL-09…15 |
| 5 | D15 token-lifetime rule | design §6.4 (the reference was later replaced by the monotone high-water mark `hw`, fourth review): rules (a) no revival `expires_at ≤ hw` (formerly `T_ref`), (b) no extension `created_at > now_c + tolerance`, (c) policy sanity; all other tokens keep their absolute expiry untouched; ACs AC-CK-05, 09…12 |

### Fifth review (owner decisions integrated)

Owner decisions recorded for this revision (source: the owner's instruction for this pass; nothing else is
implied as approved): **D24** approved with N-01 and N-02 accepted and heartbeat events deliberately excluded;
**D23** approved *restricted* (Mode R: read-only verification and incident flagging only; session-only for
`--record`, `rotate-key` and `--release`); **OQ-D20-1** decided (metadata listings allowed, secret reads and
mutations denied while quarantined); **D20 release policy** (identity-state findings block release; revalidated
in the same transaction). **D21 ON_HOLD.** D13/D15 confirmation blocked by D21; D22 requirement integrated. The
design conflict this created is recorded as **B-1 (BLOCKED)**: Mode R may not release a quarantine and a session
cannot be obtained while quarantined (design §5.8). Where: design §5.8, §6.4, §8; plan AC-VR-13/19/21/23/24/25,
AC-FL-16, §6 decision table, Remaining decisions, risks R32/R36/R37.

### Fourth review (resolution of the final consistency audit, F-01…F-28)

The audit text for F-01…F-28 exists only in the review conversation (it was not saved under `docs/reviews/`);
the full traceable matrix, with each finding's original wording, severity, root cause, resolution, status
and acceptance criteria, is `m2-audit-resolution-matrix.md`. Summary of where the resolutions live:

| Findings | Design | Plan (new/changed criteria) |
|---|---|---|
| F-01 (monotone time reference), F-06/F-07/F-08 | §5.2, §5.4, §6.4, §7.1, §7.3 | AC-CK-01/02/04/05/10, AC-CK-13…16, AC-AU-09, AC-TR-08 |
| F-02, F-11, F-16 (replay of structure, schema, `USE-*`) | §5.9, §7.2, Appendix B | AC-MG-03/09, AC-ST-17/22/23/24/25 |
| F-03, F-04, F-27 (reset order, triggers, genesis) | §5.8, §7.1–§7.3 | AC-VR-14, AC-TR-07/08/09, AC-MG-03 |
| F-05 (degraded-state authentication) | §5.8, §6.4, §8 | AC-VR-19, AC-CK-02 |
| F-09, F-10 (state on untrusted chain, exit codes) | §5.8, §5.9 | AC-ST-12/15, AC-VR-02/22, AC-CL-06 |
| F-12, F-13, F-14, F-20, F-21, F-26, F-28 (recovery text, boundaries, release, classification) | §2.5, §5.8 | AC-VR-01/03/04/12/13/14/20/21 |
| F-15 (D22 accounting) | §5.7 | AC-FL-12/13/14 |
| F-17, F-18 (M2.1 facts, approval boundary) | §1 fact 8, §6.1 | section 2, M2.1 status table |
| F-19, F-22, F-23 (labels, permissions, lifetime rules) | §4.2, §4.2.2, §5.9 | AC-ST-14, AC-DL-15/16 |
| F-24, F-25 (numbering, restore verification) | plan only; §2.5 | placeholders AC-AU-08/AC-AZ-04, AC-MG-08, R24/R31 merge |

## 1. Summary of recommendations

* **Audit key: option B** — a random 256-bit key stored wrapped by the configured
  `KeyProvider`, with an explicit random `key_id`, a `version`, and a wrap AAD binding
  purpose, root-key id, key id and version. (Design §2.)
* **Enforcement structure:** a sealed `Principal`, an unauthenticated `Service` that can
  only authenticate, and a `Handle` on which every data operation authorises, mutates
  and audits inside **one** `IMMEDIATE` transaction (design §4.3, §6).
* **Delegation:** explicit table; no self-grants; `instance_admin` assigns only
  `project_admin`; `project_admin` assigns reader/writer/auditor/project_admin inside
  its scope without gaining their permissions; token issuers must hold what they delegate
  (design §4.2).
* **Tamper evidence, honestly:** a local HMAC chain is tamper-*evident* against a
  database-only attacker, not tamper-proof against the DB owner/host admin; tail
  truncation and rollback need an external anchor; service bypass leaves no trace
  (design §5.5).
* **Identity-state integrity:** not prevented in M2; detected after the fact by a
  fully specified replay (`audit verify --state`) using keyed credential commitments
  — never hashes or secrets in events; limits stated in design §5.9.
* **Verification never writes to a suspect chain;** recovery is explicit and never
  makes old history trustworthy (design §5.8).

## 2. Proposed implementation sequence

Each step ends green (gofmt, vet, `-race`, full suite) and is independently
reviewable. Sizes: S ≤ 1 day, M ≈ 2–3 days, L ≈ 4–6 days of focused work.

| # | Step | Size | Depends on | Output |
|---|---|---|---|---|
| M2.0 | Decision gate: approve D1–D22 (section 6); root-key escrow remains a precondition for real data | S | — | decisions recorded |
| M2.1 | Unit of work: `Store.WithTx` / `Tx` over the **19 existing** store operations only, **behaviour-preserving**; no identity/audit/schema content (see "M2.1 scope" in section 3) | M | — (not gated on D1–D22; see below) | M1 suite unchanged and green |
| M2.2 | Audit core: canonical encoding + vectors, sealer/verifier, `audit_keys`, rotation, head, structural triggers | L | M2.1 | `internal/audit`, migration part A |
| M2.3 | Identity: operators, sessions, password hashing, throttling, sealed `Principal`, clock source + regression guard | L | M2.1 | `internal/identity`, migration part B |
| M2.4 | Authorization: permissions, roles, delegation engine, grants, `Handle` skeleton, matrix + reflection tests | L | M2.3 | `internal/authz`, `Handle` |
| M2.5 | Audited operations: wire every `Handle` operation (authorise → mutate → audit in one tx), read path, denial path, failure semantics, failed-auth rate bound | L | M2.2, M2.4 | TX/FL ACs |
| M2.6 | Service tokens (issue/verify/expire/revoke/rotate, hold-it rule), machine read path | M | M2.5 | token ACs |
| M2.7 | Upgrade/init: migration `0003`, pending state, `upgrade`, genesis, first operator, M1 fixtures, restore drill | M | M2.2–M2.6 | MG ACs |
| M2.8 | CLI: command groups, credential hygiene, exit codes | M | M2.7 | CL ACs |
| M2.9 | Audit tooling and recovery (Mode S/Mode R authentication, exit-code table): `status/head/list/verify/export/rotate-key`, `--anchor/--record/--latch/--all-epochs`, `quarantine [--release]`, **break-glass** (`reset-epoch`, `operator recover`, `clock reset`, `emergency revoke`) | L | M2.5 | VR/CK/AU ACs |
| M2.10 | Identity-state replay (`verify --state`), trigger inventory | M | M2.5, M2.6, M2.9 | ST/TR ACs |
| M2.11 | Hardening gate: mutation tests, fuzz, race/multi-process, leak scans, docs, vuln scan, **independent review** | L | all | review report; fix pass |

The migration `0003_identity_audit.sql` is **one forward-only file**, developed incrementally across M2.2 (audit
tables), M2.3 (identity tables) and M2.4–M2.6 and finalised in M2.7; until M2.7 completes it, it is never applied
to a database that holds real data. (F-24)

M2 is complete only after the review in M2.11 has no open Critical/High findings. No step
may add a network listener, WebUI/agent coupling, or root-key/DEK rotation.

**Two separate approvals; neither implies the other.**

1. **M2.1 (`Store.WithTx`) — implemented and committed (`8c3763c`)** on the explicit authorization of a
   human reviewer, which was limited to the behaviour-preserving transaction refactor of the existing 19 store
   operations (design §6.1). It was evaluated independently of D1–D22 because it introduces no identity,
   authorization, audit, clock or schema content. The authorization covered M2.1 only.
2. **Approval of the overall M2 design (the M2.0 decision gate, D1–D24) and of every later milestone
   (M2.2 onward).** Not given. The owner decided D24, restricted D23, OQ-D20-1 and the D20 release policy for
   the documents; D21 is ON_HOLD; D13 and D15 cannot be confirmed until D21 is resolved; D22 and the rest of D20
   await confirmation; B-1 is BLOCKED; the resolutions of F-01…F-28 are documentation only and
   unreviewed (see `m2-audit-resolution-matrix.md`). Having implemented M2.1 does **not** approve any
   decision in section 6 and does **not** authorize M2.2 or any later step; each requires its own explicit
   approval after the decision gate.

### 2.1 New and changed packages (proposed; D17)

| Package | Responsibility | Notes |
|---|---|---|
| `internal/audit` | canonical encoding, MAC, sealer, verifier, replay (`--state`), anchors, vectors | pure; no SQL; keys via an interface |
| `internal/authz` | permissions, roles, delegation table; pure `Allowed` / `MayAssign` | no I/O; exhaustively table-tested |
| `internal/identity` | operators, passwords (Argon2id), sessions, service tokens, sealed `Principal`, throttling, clock source | uses `store.Tx` |
| `internal/store` (+`sqlite`) | `WithTx`/`Tx` (M2.1: the 19 existing operations only); new tables, triggers, migration `0003` (M2.2 onward) | store never sees keys; receives sealer callbacks |
| `internal/centralsvc` | `Service` (authenticate/bind/upgrade only) and `Handle` (all data ops) | existing `Bind` reused |
| `internal/cli/central*.go` | new command groups | credentials via prompt/`securefs` files only |
| `docs/central-store-m2.md` | operator-facing description, limits, procedures | written with the code |

## 3. Checklist with testable acceptance criteria

Each **AC-xx** maps to at least one named automated test fixed in the implementing PR;
security properties also map to a mutation that must be caught (M2.11). "Verify" means the
implementation's own `audit verify`.

### M2.0 — Decision gate  *(gates M2.2 and later; it does not gate M2.1 — see section 2)*
- [ ] D1–D22 answered in writing (section 6).
- [ ] Root-key escrow/backup procedure confirmed as a documented precondition for any
      real data (not built; tracked).
- [ ] Dependency-vulnerability remediation (review report §1) scheduled as a separate
      pre-release task; it is **not** part of M2 and not a reason to skip it.

### M2.1 — Unit of work (`WithTx`)  *(IMPLEMENTED and committed as `8c3763c`; the only M2 step that exists)*

| Criterion | Status (after independent review of M2.1) |
|---|---|
| AC-TX-01, 01a, 02, 03, 04, 04a, 04b-1, 04b-3, 04c, 04d, 04e, 05, 06 | **executed and passed** (store, `centralsvc`, `cli` suites also under `-race`) |
| AC-TX-04b-2 | **UNPROVEN — test skipped, never passed.** No deterministic observation of SQLite's lock wait exists without a production seam outside M2.1 |
| Context marker | detects only the same `Store` instance's marked/derived context; unrelated contexts, other instances and processes are outside its scope (OQ-TX-1 option (a), as implemented) |
**M2.1 scope (exact; agrees with design §6.1).** In scope: `Store.WithTx(ctx, fn)` and the
transaction-scoped `Tx` covering the **19 existing data/metadata operations** of `store.Store`
(`SchemaVersion`/`Close` excluded), with identical semantics and `mapErr` error mapping.
Current facts (verified in source, design §1 fact 1): 4 operations use `s.tx`
(`CreateSecret`, `AddVersion`, `DisableVersion`, `PutMetaIfAbsent`; these delegate to `WithTx`);
15 use direct autocommit statements (`GetVersion`, `GetLatestVersion`, `ListVersions` use two
statements); DSN `_txlock=immediate`; up to 8 pooled connections; `busy_timeout` 5 s default.
The 15 direct-autocommit operations **retain their existing standalone behaviour in M2.1**
(same statements, no added `BEGIN`, no snapshot change; `GetVersion`, `GetLatestVersion` and
`ListVersions` keep their two separate autocommit statements). Shared internal helpers are allowed;
the shared-transaction behaviour belongs to `Tx`. Any intentional behaviour change needs separate
explicit approval and is not part of M2.1. **Out of scope:** principal/grant/token queries,
`AppendAudit`, new tables or triggers, migrations, schema change, CLI change, new dependencies,
existing tests must not be modified except for necessary imports; new tests required by the M2.1 acceptance criteria are in scope; no unrelated test refactoring.
- [ ] `store.Tx` exposes the 19 existing operations with identical semantics;
      `Store.WithTx(ctx, fn)` runs `fn` in one `IMMEDIATE` transaction; the 4 existing `s.tx` operations delegate to it.
- AC-TX-01 All M1 tests pass **unchanged** (no edits to existing tests except imports; new tests may be added).
- AC-TX-01a **Standalone semantics preserved (characterization):** tests written and passing against
      `58e711f` *before* the refactor, then passing unchanged after it. For each of the 15 standalone
      operations they assert the same results and error values as baseline and that no transaction is
      begun (statement trace through a test-only seam). For `GetVersion`, `GetLatestVersion` and
      `ListVersions` they assert two separate autocommit statements, with a deterministic interleaving
      (seam fires between the two statements, so no timing): `GetLatestVersion`/`ListVersions` return a
      version committed in the gap; `GetVersion` returns `ErrVersionDisabled` for a version disabled in the
      gap. A single-snapshot implementation fails these. If the seam cannot be added without production
      code beyond the refactor, the PR reports that for the reviewer instead of weakening the test.
- AC-TX-02 Two operations in one `WithTx` commit together; an error in the second rolls back the first.
- AC-TX-03 A panic inside `fn` rolls back and the store stays usable.
- AC-TX-04 Nested `WithTx` from inside `fn`, called with the context `fn` received, returns `ErrTxActive`
      immediately (no database access, well under `busy_timeout`; the test asserts completion within 1 s
      with `busy_timeout` set to 5 s), and the outer transaction still commits or rolls back normally.
- AC-TX-04a **Standalone method inside a transaction:** for each of the 19 operations (table-driven),
      calling the standalone `Store` method with the context `fn` received — from the `fn` goroutine and
      from a second goroutine — returns `ErrTxActive` within 1 s (`busy_timeout` 5 s), for reads and writes
      alike, never a busy error and never a result read outside the transaction; no row written by the
      outer `fn` is visible to, or changed by, the rejected call; afterwards the outer commit succeeds.
- AC-TX-04b..e **Documented limitation (applies if OQ-TX-1 option (a) is chosen).** For a standalone call made
      during an open transaction with an unrelated context. Ordering is forced by channels, never by sleeps;
      the oracle is never "who was faster":
  - AC-TX-04b *write, outer finishes first* — two explicit cases, not one:
      - **04b-1 (commit before G starts):** G is started only after `WithTx` has returned (commit confirmed);
        long `busy_timeout`. G's write succeeds, its row and the outer row are both present.
      - **04b-2 (G demonstrably blocked, then commit):** `fn` starts G, waits until G is **observed blocked on
        the held write lock**, and only then returns (commit) well before `busy_timeout`; G's write
        succeeds and both rows are present. A channel signalling "G is about to call" does **not** prove the
        block and must not be used as the observation. Needed: a test-only seam that observes the lock wait.
        Finding (inspected, `modernc.org/sqlite` v1.50.0): the driver exposes the wait only through the
        `busy_timeout` pragma, with no busy-handler callback or wait notification, and `database/sql` offers
        none; a wrapper around the driver sees that G's statement *started*, not that it is *blocked*. So
        04b-2 cannot be proven without a production-code seam (for example a store-side retry loop with
        `busy_timeout(0)`), which would change concurrency behaviour and is outside M2.1. **Therefore 04b-2 is
        not claimed unless the implementation PR supplies a seam that proves the block without production
        changes beyond M2.1; if it cannot, the PR reports 04b-2 as an untested limitation for the reviewer.**
      - **04b-3 (ordering-independent outcome, claims no ordering):** `fn` signals "G about to call" and
        returns; whichever order occurs, G's write succeeds and both rows are present. This only shows the
        outcome does not depend on the race; it is not evidence that the lock wait was exercised.
  - AC-TX-04c *write, timeout first:* `busy_timeout` short (200 ms). `fn` starts G and then **waits for G to
      return** (so the lock cannot be released before G's wait ends). G returns the raw busy error, not
      `ErrTxActive`; G's row is absent. Only then does `fn` return, and the outer transaction commits
      (and, in a variant, rolls back) as normal; its outcome is asserted in both variants.
  - AC-TX-04d *read:* `fn` writes a row (uncommitted), starts G with a standalone read of that row, and
      waits for G to return. G does not block and does not see the uncommitted row (and sees the previous
      committed value for an updated row). After the outer commit a standalone read sees the new state;
      after a rolled-back variant it never does.
  - AC-TX-04e Each of the above asserts the outer transaction's final state (committed or rolled back)
      explicitly.
  See OQ-TX-1.
- AC-TX-05 Context cancellation mid-`fn` rolls back; no leaked transaction/goroutine.
- AC-TX-06 `-race` clean; the M1 multi-process writer test passes on `WithTx`.

### M2.2 — Audit core (`internal/audit`, tables, keys)
- [ ] Canonical encoding (design Appendix A) with published vectors; `Sealer`, `Verifier`, `Head`.
- [ ] `audit_keys`; generation, wrapping, unwrapping, versioning, rotation.
- [ ] `AppendAudit(ev, seal)` on `Tx`; gapless `seq` under the lock; **tail check** on every append
      (recompute the newest event's MAC and compare with `audit_state`; design §5.8).
- AC-AK-01 Audit key is 32 bytes from `crypto/rand`; entropy failure ⇒ error, no key.
- AC-AK-02 `key_id` is random 8 bytes, **not a function of the key**; re-wrapping keeps it.
- AC-AK-03 Wrap AAD binds purpose, `root_key_id`, `key_id`, `version`: changing any one fails
      unwrapping; DEK blobs and the binding canary cannot be unwrapped as audit keys or vice versa.
- AC-AK-04 The full audit flow works against a `KeyProvider` double that hides its key
      (only `ID/Wrap/Unwrap`) — KMS compatibility.
- AC-AK-05 Unwrap failure / unavailable provider ⇒ audited operations return
      `ErrAuditUnavailable`; read-only diagnostics still run.
- AC-AK-06 Rotation: `audit.key_rotated` is MACed by the old key, the next event by the new key
      and chains from it; `verify` passes across versions; old events still verify; old key row
      retained with `retired_at`.
- AC-AK-07 Root-key-change simulation (test helper re-wraps `audit_keys` under a second
      provider): MACs and `verify` results unchanged (the property that rules out option A).
- AC-AK-08 A deleted/corrupt `audit_keys` row ⇒ `verify` exit 11 naming the missing version; the
      service refuses to append; **recovery is restore or `reset-epoch` (AC-VR-06…09), never
      automatic**.
- AC-AU-01 Canonical-encoding vectors published; distinct events never share an encoding
      (field-boundary property test over every adjacent string field pair).
- AC-AU-02 **Tamper matrix:** mutating any column of `audit_events` at a chosen `seq` makes
      `verify` fail at exactly that `seq` (table-driven over the column list; adding a column
      without a MAC input fails the test).
- AC-AU-03 Delete first / middle / last-with-later-events; reorder; insert forged row; relabel
      `key_version`/`mac_alg`/`chain_id`; splice from another database ⇒ all detected with the
      offending `seq`.
- AC-AU-04 **Documented non-guarantee:** truncating the last *k* events and restoring a prior
      coherent `audit_state` is **not** detected without an anchor (the test asserts this) and
      **is** detected with `--anchor seq:mac`.
- AC-AU-05 32 goroutines × 50 appends plus 4 processes × 50 appends yield one gapless chain;
      `verify` passes; no duplicate `prev_mac`.
- AC-AU-06 SIGKILL during a transaction that includes an append leaves (mutation + event) or neither.
- AC-AU-07 An injected append failure rolls back the surrounding mutation and returns an error.
- AC-AU-08 *(identifier intentionally unassigned — withdrawn during the first review; kept so existing
      references stay stable. There is no criterion AC-AU-08.)*
- AC-AU-09 Stamped timestamps are non-decreasing along the chain (`ts = max(now_c, ts_high_water)`); a
      wall-clock regression alone never makes `verify` fail (it is absorbed by stamping and surfaces only as
      the clock guard); a timestamp decrease anywhere except at a `system.clock_reset` event is reported as
      `TS-DECREASE` (exit 10); `seq` remains the only ordering authority.

### M2.3 — Identity (operators, sessions, clock)
- [ ] `operators`, `operator_sessions`; Argon2id; sealed `Principal`; `AuthenticateOperator/Session`;
      injectable clock; regression guard (design §6.4).
- AC-ID-01 Passwords < 12 characters rejected; hashes are PHC strings with parameters inside the
      accepted range; tampered parameters rejected.
- AC-ID-02 Unknown user, wrong password, disabled and locked operator return the **identical**
      error value/message; unknown-user path performs a dummy hash.
- AC-ID-03 Backoff is persisted (`failed_count/locked_until`), survives restart, caps at 15
      minutes, resets on success, is never permanent.
- AC-ID-04 Disabling an operator takes effect on that operator's **next operation on every live
      session** (checked in the operation's own transaction).
- AC-ID-05 Session absolute and idle expiry with an injectable clock; only `SHA-256(secret)` is
      stored (a scan of the database file for the secret finds nothing).
- AC-ID-06 A `Principal` cannot be constructed outside the package (negative compile test).
- AC-ID-07 `dso_st_…` presented as a session and vice versa is rejected before any DB access.
- AC-ID-08 Concurrent password verifications are bounded (semaphore); 200 parallel attempts under a
      memory cap do not exhaust memory.
- AC-ID-09 Silent rehash of a password hash does not exist: every `password_hash` change goes
      through `set_password` and an event (supports AC-ST-03).

### M2.4 — Authorization (incl. delegation)
- [ ] Permission/role tables in code; `grants`; engine with deny-by-default; delegation engine
      (`MayAssign(grantor, grantee, role, scope)`); `Handle` exposes every data operation.
- AC-AZ-01 A new operator with no grant is denied **every** `Handle` operation (matrix row).
- AC-AZ-02 **Generated authorization matrix:** operations × {instance_admin, project_admin
      (project-wide / env-scoped), writer, reader, auditor (project / env-scoped), service token,
      disabled operator, revoked grant, expired/revoked token} × {own env, sibling env, other
      project, nonexistent} ⇒ exact expected allow/deny.
- AC-AZ-03 Reflection test: every exported `Handle` method has a matrix entry; `Service` exports
      none of the data-operation names; adding a method without a row fails CI.
- AC-AZ-04 *(identifier intentionally unassigned — withdrawn; there is no criterion AC-AZ-04.)*
- AC-AZ-05 `instance_admin` without a project grant cannot read secret values; `project_admin`
      cannot read or mutate secrets.
- AC-AZ-06 Denial for "missing" and for "forbidden" is **byte-identical** (error type and message)
      for projects, environments and secrets.
- AC-AZ-07 Revocation/removal of a grant is effective for the next operation; concurrent
      revoke-vs-use is serialised.
- AC-AZ-08 Every denial writes a `denied` event with a reason code.
- **Delegation matrix (D4/D18), one automated case per cell of design §4.2.1:**
- AC-DL-01 `project_admin` (project-wide) may assign `reader`, `writer`, `auditor`, `project_admin`
      on its own project, project-wide or to one environment, to another operator; the grantor
      **gains none of the assigned permissions**.
- AC-DL-02 Env-scoped `project_admin` (E) may assign those roles on E only; project-wide grants,
      other environments and other projects are denied.
- AC-DL-03 `project_admin` of A cannot grant, revoke or list grants on project B.
- AC-DL-04 No operator can create a grant for themselves (any role, any scope) — including
      `instance_admin` → `project_admin` and `project_admin` → `reader`.
- AC-DL-05 `instance_admin` may assign **only** `project_admin`; attempts to assign
      `reader`/`writer`/`auditor` are denied.
- AC-DL-06 There is no way to assign `instance_admin` through `grant add`; `operator set-admin` is
      refused for non-`instance_admin` callers.
- AC-DL-07 The last live `instance_admin` cannot be disabled, demoted or self-revoked (refused,
      audited); with two admins, each of those operations succeeds on one of them.
- AC-DL-08 `writer`, `reader`, `auditor` and service tokens cannot grant or revoke anything.
- AC-DL-09 Revocation authority: a principal can revoke exactly the grants it could create (even
      if another admin created them); `instance_admin` can revoke any grant; anyone can revoke
      their own grant (not the last admin attribute).
- AC-DL-10 Grants name only existing, enabled operators and existing project/environment pairs;
      cross-project environment ids are rejected.
- AC-DL-11 Token issuance/rotation requires the issuer to hold `secret:read` (and `secret:list`
      for `read+list`) on **every** environment of the token's scope: a `project_admin` without a
      reader grant cannot issue; with a reader grant on E only, cannot issue for E2.
- AC-DL-12 A token's permission set is a subset of the issuer's; escalation attempts denied.
- AC-DL-13 `token list/revoke` remain available to `project_admin` within scope without the
      hold-it rule.
- AC-DL-14 Every grant/revoke produces a replayable event (supports AC-ST-01).
- AC-DL-15 `instance_admin` holds `member:grant`/`member:revoke` limited exactly as design §4.2.1 (assign
      `project_admin` only; revoke any grant); the permission table in code and the delegation table agree
      (table-driven cross-check). (F-22)
- AC-DL-16 A token stays valid after its issuer loses the grant that allowed issuing it, until it expires or is
      revoked; `token list` shows `created_by`. `emergency revoke --kind operator` refuses the last live
      `instance_admin` (refused and audited). (F-23)

### M2.5 — Audited operations and failure semantics
- [ ] All data operations use the template (design §6.2); denial path commits only its event; read path
      appends `secret.read` before releasing plaintext; failed-auth rate bound (design §5.7).
- AC-TX-07 Property test: every successful mutation adds exactly one event in the same transaction
      (row counts across a randomised operation sequence).
- AC-TX-08 Failure injection: audit append error ⇒ mutation absent **and** no plaintext from `GetValue`.
- AC-TX-09 Ordering hook: the `secret.read` event is committed before the plaintext leaves the service.
- AC-TX-10 `created_by/disabled_by/deleted_by` come from the principal; no caller-supplied actor
      parameter exists on `Handle` (reflection on parameter names/types).
- AC-TX-11 Denied/errored operations leave no partial state (snapshot comparison).
- AC-FL-01 *Audit-specific failure* (key unwrap error, sealer error, quarantine, tail-check failure) with a
      working database: mutations/reads refused, no plaintext; denials stay denials; stderr says
      "audit event NOT recorded".
- AC-FL-02 *General write failure* (injected `SQLITE_FULL`/`IOERR`/read-only): mutations fail, nothing
      committed; denials stay denials; nothing claims to be recorded; the in-memory
      `dropped_since_start` counter is non-zero and printed at CLI exit.
- AC-FL-03 **No persisted `audit_dropped` counter exists** (schema test): the only counters persisted
      are the failed-auth window columns in `audit_state`.
- AC-FL-04 Commit error / crash around commit: after reopening, mutation and event are both present or
      both absent (crash test); the CLI prints "outcome unknown" and exits non-zero; success messages
      appear only after a successful commit.
- AC-FL-05 No code path logs, prints or returns "recorded" unless the transaction committed (grep-style
      test over message constants plus failure-injection assertions).
- AC-FL-06 Failed-auth bound: the 61st failure in a 60 s window is not individually appended; `auth_suppressed`
      increments durably in the same transaction; the first failed-auth/login/any-append after the window
      appends `auth.failures_suppressed{count, window_start}` then its own event in one transaction; the
      pending count is visible in `audit status`; limits hold across **separate processes** (design §5.7).
- AC-FL-07 A denial is never converted into an allow when auditing fails, and an allow is never granted
      because auditing is unavailable (matrix repeated under injected audit failure).
- AC-FL-08 Persisted per-operator lock-out still bounds guessing when audit is unavailable.
- AC-FL-09 **Boundary concurrency:** 8 processes × 100 failures straddling an injected window boundary:
      exactly one `auth.failures_suppressed` per suppressed batch, no lost or double-counted attempt.
- AC-FL-10 **Crash injection:** SIGKILL before the counter update, between summary append and zeroing,
      and before commit: after recovery there is never a zeroed counter without its summary, never a
      summary with the count retained, never a duplicate summary.
- AC-FL-11 **Write failure while updating counts** (injected `SQLITE_FULL`/`IOERR`): the attempt is denied,
      nothing is claimed as counted, only the in-memory counter moves; after recovery the persisted numbers
      equal exactly the committed attempts.
- AC-FL-12 **Clock regression:** a backwards clock neither reopens an old window nor resets the ceiling
      (`now' = max(clock, ts_high_water, window_start)`); while the guard is tripped, refusals go through the
      same bounded path (reason `clock_regression`); after the ceiling, further attempts in a stalled window are
      only counted and are summarised once the window can roll (documented cost).
- AC-FL-13 **Restart:** pending suppressed counts survive restart, appear in `audit status` and `audit
      verify` ("N suppressed … not yet summarised"), and are flushed by the next append of **any** action
      once the window has closed; a non-failure append performs the flush half only and does not start a window.
- AC-FL-14 **Accounting invariant** (property test over randomised goroutine/process interleavings and
      injected faults): per window, `A_c ≤ auth.failed events + Σ summary counts + pending auth_suppressed ≤
      A_c + K`, with `A_c` = attempts whose caller observed a committed outcome and `K` = attempts killed or
      with unknown commit outcome (equality when `K = 0`). The oracle uses only confirmations the harness
      received; no production test seam. Attempts denied while auditing was unavailable are outside the
      invariant, and lock-out still increments for them (AC-FL-08). (F-15)
- AC-FL-15 When the database is unavailable no message claims that suppressed attempts were counted or
      recorded (message-constant test + failure injection).
- AC-FL-16 **Deterministic accounting cases (D22; no randomness, no timing):** (1) exactly 60 failures in one window ⇒ 60
      `auth.failed`, 0 pending; (2) 65 failures ⇒ 60 events, pending 5, then the first append after the window
      closed adds `auth.failures_suppressed{count=5}` and pending 0; (3) an injected *rollback* at the 10th failure
      ⇒ the caller sees an error and the totals are exactly 9 (`K=1`, bound holds with the lower value); (4) an
      injected *lost reply after commit* at the 10th ⇒ totals exactly 10 (bound holds with the upper value); (5) a
      clock set back mid-window after the 60th ⇒ further attempts only counted, none individually recorded, and
      all summarised once the window can roll; (6) auditing unavailable (F1) ⇒ 0 counted, lock-out increments.
      Each asserts `A_c ≤ total ≤ A_c + K` with the exact expected `total`.
### M2.6 — Service tokens
- [ ] Issue/verify/expire/revoke/rotate; machine read path; `service_token_envs`; hold-it rule.
- AC-TK-01 Token format `dso_st_<32 hex>.<43 base64url>`; secret from `crypto/rand`; printed once;
      not recoverable from the DB.
- AC-TK-02 DB stores only the hash; a scan of DB, WAL, audit rows, logs and CLI output for the
      plaintext secret finds nothing.
- AC-TK-03 Scope: a token reads only listed environments of its project; tampered scope rows are
      *detected by `verify --state`* (AC-ST-04) and the composite FK prevents cross-project environments.
- AC-TK-04 Expiry is mandatory unless `--no-expiry`; default 90 d, maximum 365 d; expired tokens are
      rejected on the next request (injectable clock); `no_expiry` tokens are flagged in `list` and audited.
- AC-TK-05 Revocation is immediate and permanent (trigger forbids clearing).
- AC-TK-06 Rotation: both tokens valid during the grace window; old expires at `min(old, now+grace)`;
      `replaced_by` recorded; events present and replayable.
- AC-TK-07 A service token can never perform a management operation (type-level + matrix); the
      `perm_set` `CHECK` rejects any other set.
- AC-TK-08 Fuzz: malformed tokens never panic, never reach the DB, return the uniform error.
- AC-TK-09 With the clock guard tripped, token authentication is refused (AC-CK-01).

### M2.7 — Migration and upgrade (D9)
- [ ] Migration `0003`; "M2-pending" state; `upgrade`; fresh `init` creates the same state.
- AC-MG-01 An M1 database with projects/secrets/versions migrates; all M1 data and `created_by` strings
      are intact; a **verified** pre-migration backup exists.
- AC-MG-02 Between `0003` and `upgrade`, every data operation returns `ErrUpgradeRequired`
      (table-driven); read-only diagnostics work.
- AC-MG-03 `upgrade` is atomic: audit key v1, `chain_id`, the initial `audit_state` row (`last_seq=0`, zero
      `last_mac`, `active_key_version=1`, `ts_high_water=0`), the genesis event (accepted by the *normal* trigger
      branch), one adopted `project.create`/`env.create` per pre-existing M1 project and environment, first
      `instance_admin` (with its `operator.create` event), `m2.upgraded` appear together or not at all (fault
      injection at each step). No M1 row is modified. (F-02, F-27)
- AC-MG-04 Wrong root key ⇒ `upgrade` refused before any write; second `upgrade` refused; 8 concurrent
      upgraders have exactly one winner.
- AC-MG-05 The M1 binary refuses the upgraded database (`ErrSchemaTooNew`), database untouched.
- AC-MG-06 **Restore drill:** restore the pre-migration snapshot, confirm M1 data and an M1 binary
      work, re-run the upgrade successfully.
- AC-MG-07 The migration does **not proceed** if the backup step fails or its verification fails
      (fault-injected), leaving the database at its old schema (D9 condition).
- AC-MG-08 **Backup/restore verification of audit keys (manual procedure; no restore tooling in M2):** take a
      snapshot of an upgraded database that has rotated audit keys, restore it to a fresh path, and with the
      same root key `audit verify` and `audit verify --state` exit 0 and `audit_keys` is complete; with a
      different root key they exit 11 classified *environmental*; with the `audit_keys` row removed they exit 11
      classified *data*. (F-25, D21 backup requirement)
- AC-MG-09 An M1 database with N projects/M environments, after `upgrade`, gives `verify --state` zero
      findings; a project row inserted afterwards without an event is `ST-ORPHAN`. (F-02)

### M2.8 — CLI
- AC-CL-01 No flag, on any command, accepts a password, token, session secret or secret value
      (reflection over the cobra tree).
- AC-CL-02 Session/password/token-output files must pass `securefs`; a loose file is refused with the fix.
- AC-CL-03 `token issue` writes the token to stdout or an `O_EXCL 0600` file exactly once; nothing else on
      stdout; stderr never contains it.
- AC-CL-04 Leak scan: run every command (success and failure) with planted secrets/tokens/passwords;
      none appears in stdout, stderr, audit rows or logs.
- AC-CL-05 Authentication and authorization failures print exactly the two uniform messages;
      `--verbose` adds nothing that distinguishes causes.
- AC-CL-06 Stable exit codes: 0 ok, 1 other, 2 usage, 3 authentication, 4 authorization/not found,
      5 audit unavailable/integrity (refusals), 6 clock regression. The `audit` read commands (`verify`,
      `verify --state`, `--anchor`, `--all-epochs`) use **only** the separate codes 0/10/11/12/13/14/15 of the
      single normative table in design §5.8, with its precedence 13 > 12 > 10 > 11 > 15 > 14 > 0; the two code
      sets never overlap (table-driven test).
- AC-CL-07 End-to-end scenario: upgrade → login → project/env → grant (delegation) → issue token →
      read via token → rotate → revoke → verify chain and `--state` (zero findings).

### M2.9 — Audit tooling, recovery and break-glass
- AC-VR-01 **`audit verify` writes nothing:** for each verification status (OK, BROKEN, UNVERIFIABLE,
      HEAD_INCONSISTENT), each with and without the quarantine flag set, the database file is byte-identical (including WAL contents after checkpoint) before and
      after; no event, state or incident row is created.
- AC-VR-02 Exit codes exactly as the normative table of design §5.8 (0/10/11/12/13/14/15) including the
      precedence 13 > 12 > 10 > 11 > 15 > 14 > 0, `--all-epochs` (worst code over epochs; default = current
      epoch), `--anchor` mismatch ⇒ 10, `SCHEMA-TAMPER` alone ⇒ 15, findings never hidden in `--json`; first
      bad `seq`, epoch and reason in text and `--json` forms. (F-10)
- AC-VR-03 `--record` appends `audit.verify_recorded{through_seq, head_mac}` only when the exit code is 0 and
      the chain is not quarantined, in a separate transaction that first re-checks `quarantined_at IS NULL` and
      that the head is still the verified `(seq, mac)`; if either changed it appends nothing and exits 1. In
      every other state it appends nothing. `--record` with `--latch` is a usage error (exit 2). The recorded
      event is itself verified by the next run (no recursion issue). (F-14)
- AC-VR-04 `--latch` sets the latch and an `audit_incidents` row only for BROKEN, HEAD_INCONSISTENT,
      SCHEMA-TAMPER or UNVERIFIABLE(data); `audit quarantine` does so on request; while latched, every appending operation is
      refused with `ErrAuditIntegrity`; read-only commands work.
- AC-VR-05 Tail check: tampering with the newest event or `audit_state` makes the **next append** fail
      (HEAD_INCONSISTENT) without any explicit `verify`.
- AC-VR-06 `reset-epoch` requires root-key proof and typed confirmation (wrong key ⇒ refused before any write).
- AC-VR-07 `reset-epoch` appends `audit.epoch_start` carrying only observations (`obs_status`, `obs_first_bad_seq`,
      `obs_last_seq`, `obs_last_mac`, `obs_state_head_*`, `obs_state`, `reason`, `baseline`), creates a new `chain_id` and key
      version (`max(MAX(audit_keys.version), MAX(events.key_version)) + 1`), continues `seq` at `MAX(seq)+1` (1 for
      an empty table), stamps `ts = max(now, ts_high_water)` (never lower), deletes/modifies nothing, and clears the
      latch for the new epoch only.
- AC-VR-08 After a reset, `verify` still reports the old epoch as BROKEN/UNVERIFIABLE (permanently) and
      the new epoch as OK; `export` tags epoch and key version; old history is never reported as trusted.
- AC-VR-09 `operator recover` is refused while quarantined and works after `reset-epoch`; it is audited
      (`system.recover` + `operator.create`).
- AC-VR-10 There is no command that repairs or re-links a broken chain (CLI tree test).
- AC-VR-11 **Transition table (design §5.8) T1–T7:** one test per transition, plus the forbidden ones
      (nothing returns to Verified except T5 or T6; `verify` writes only through the explicit flags;
      appends refused while Quarantined except T6; old epochs never modified or deleted).
- AC-VR-12 `--latch` latches on BROKEN, HEAD_INCONSISTENT, SCHEMA-TAMPER and key-row-missing/corrupt, and does
      **not** latch on provider outage, wrong root key or a corrupt binding canary. The classification is ordered
      (design §5.8): canary first; only with a good canary is a missing/failing audit-key row `data`. (F-20)
- AC-VR-13 **Release (T5), owner policy D20:** `quarantine --release` is permitted for Mode S (`audit:admin`) only and
      runs in **one `IMMEDIATE` transaction**: head and flag revalidated, full current-epoch `verify` exit 0,
      `verify --state` with **no identity-state finding**, then the flag is cleared and a `released` incident row
      is written; any failure rolls back, prints the verifier output and exits with the verification code. A seeded
      identity-state finding (e.g. a raw-SQL forged token) blocks release; a seeded `ST-USE-AFTER-EXPIRY` warning
      alone does not. A concurrent head change injected between the verification and the clearing statement aborts
      the release. Old epochs do not prevent a release. No operator command dismisses a finding. (F-14)
- AC-VR-14 `reset-epoch` succeeds, with root-key proof, from each inconsistent start: `audit_state` head
      ahead of the events; behind the events; newest-event MAC altered; `audit_state` row missing;
      `audit_keys` row missing or corrupt; BROKEN in the middle; already quarantined. In each case the
      observations are read from `audit_events` directly, `seq` continues, and a hash of the old events
      table is identical before and after. Also: empty event table (⇒ `obs_status=empty`, `seq` 1); malformed
      rows (NULL / wrong-length `mac`) ⇒ `obs_last_mac=invalid`, reset proceeds; `MAX(seq)=2^63−1` ⇒ refused;
      duplicate/non-contiguous boundaries ⇒ reset proceeds with a unique `chain_id`; a deleted key row never
      causes a key version to be reused. The reset runs with all §7.3 triggers installed. (F-03, F-13)
- AC-VR-15 The `epoch_start` detail keys are exactly the `obs_*`/`reason`/`baseline` set (schema test); CLI
      and docs say "observed", never "verified"/"ok"; every earlier epoch is labelled `HISTORICAL — not
      trusted as authentic` even when `obs_status=ok`.
- AC-VR-16 After a reset the new epoch verifies `OK` while the old epoch is still reported
      BROKEN/UNVERIFIABLE; the default exit code reflects the current epoch; `--all-epochs` is non-zero.
- AC-VR-17 Latch and incident rows are not trusted: clearing them with raw SQL does not change any
      `verify`/`--state` result, and the docs lint forbids calling them evidence.
- AC-VR-18 `reset-epoch` is atomic: injected failure at each step leaves the previous state (including a
      quarantine) unchanged and no new key version or event.
- AC-VR-19 **Degraded-state authentication (F-05, D23 restricted):** with every session expired and the chain
      `HEAD_INCONSISTENT` (and separately quarantined, and separately with the clock guard tripped), `status`,
      `head`, `list`, `export`, `verify`, `verify --state`, `--latch` and `quarantine` (setting) succeed by root-key
      proof (Mode R); the same commands by session (Mode S) work when healthy and are refused while the guard is
      tripped; `login` is refused with the uniform authentication error whenever the chain cannot take an append.
      **`quarantine --release`, `verify --record` and `rotate-key` are refused for Mode R** with the uniform
      authorization error before any work (see AC-VR-23). Mode R is documented as not a user credential,
      database-owner-equivalent, and its read-only use appends nothing and is documented as unrecorded; flag
      commands leave an incident row with `recorded_by=root-key-proof`. Authorization is decided by the most
      privileged flag; `--record` with `--latch` is a usage error.
- AC-VR-20 **Epoch boundaries (F-13):** table-driven `BOUNDARY` cases — `chain_id` change at a non-`epoch_start`
      event; reappearing `chain_id`; `epoch_start` with non-zero `prev_mac` / reused `chain_id`; duplicate
      `epoch_start` — each ⇒ `BROKEN`, exit 10; a `seq` gap before an `epoch_start` is attributed to the earlier
      epoch (default exit code stays 0 for an intact current epoch; `--all-epochs` is non-zero).
- AC-VR-21 **Quarantine policy (F-21, N-03, OQ-D20-1 decided):** while quarantined (and in the other states that refuse
      appends): `operator recover`, `clock reset` and `emergency revoke` are refused; no session or token can read a
      secret value or mutate anything; `login` is refused; lock-out and failed-auth window bookkeeping still update;
      after `reset-epoch` the break-glass operations work.
- AC-VR-22 `verify --state` on a chain whose status is not `OK` returns that chain's exit code and runs no
      comparison; events whose MAC cannot be verified are never used as replay input. (F-09)
- AC-VR-23 **Mode R cannot release (D23):** `quarantine --release` with Mode R only is refused before any work, the
      flag and incident table are unchanged, in every chain state, with a valid session absent and present.
      **BLOCKED (B-1):** whether any permitted way to run `--release` exists once no `audit:admin` session can be
      obtained (login is refused while quarantined; sessions idle out after 30 minutes) is undecided; the criterion
      for *availability* of the Mode S path cannot be written until the owner chooses among the options in design
      §5.8. Until then release by Mode S is testable only with a pre-quarantine session that is still valid.
- AC-VR-24 **Release transaction atomicity (D20 policy):** fault injection at each step of the release transaction
      (key provider failure during `--state`, I/O error, head change) leaves the flag set and writes nothing.
- AC-VR-25 **Metadata listings while quarantined (OQ-D20-1):** `project list`, `environment list` and `secret list`
      succeed for a valid principal with `secret:list`, return exactly the documented fields (design §5.8) and
      nothing else — a scan of their output with planted secrets, hashes, commitments, wrapped keys and version
      comments finds none — and write nothing (`last_seen_at`/`last_used_at` unchanged, database byte-identical);
      `operator list`, `grant list`, `token list` and version listings are refused; a refused listing prints
      "denied; audit event NOT recorded"; the same listings keep working when the chain is BROKEN, UNVERIFIABLE or
      HEAD_INCONSISTENT and are refused when the clock guard is tripped.
*(AC-CK-01…17: the guard is built in M2.3 and the break-glass `clock reset` in M2.9; these criteria are
      exercised once the operations they cover exist, i.e. at M2.9. F-24)*
- AC-CK-01 Guard tripped (`now < ts_high_water − 5 min`): login, session/token authentication, and every operation
      by a session or token principal return `ErrClockRegression`; nothing is mutated.
- AC-CK-02 Tripped: `logout` of the caller's own session succeeds; read-only audit commands succeed **by
      root-key proof (Mode R)** and report the regression; the same commands by session are refused. (F-05)
- AC-CK-03 Tripped: normal-path revocations are refused; `emergency revoke` (root-key proof) succeeds and is
      audited (`system.emergency_revoke`).
- AC-CK-04 Wait recovery: advancing the clock past `ts_high_water − tolerance` clears the guard with no state change.
- AC-CK-05 `clock reset` requires root-key proof and a typed confirmation showing `now_c`, `ts_high_water` (`hw`), the gap
      and the per-rule counts, and is **refused unless the guard is tripped**; in one
      transaction it revokes all sessions and the tokens matching **exactly** the rules of design §6.4:
      (a) `expires_at ≤ hw` (reason `expired_at_reset`), (b) `created_at > now_c + tolerance` (reason
      `clock_skew`), (c) `expires_at − created_at > L_max + tolerance` and not `no_expiry` (reason `policy`);
      every other token keeps its **absolute `expires_at` unchanged**; the remaining tokens are listed and
      `--revoke-tokens` revokes them; the revocations come first, then the `system.clock_reset` event (stamped
      `new_hw = now_c`, MACed `old_hw`/`new_hw`/counts), then `ts_high_water := new_hw`, all in one transaction.
- AC-CK-06 **No revival within the `hw`-protected range (not under every rollback; see AC-CK-17):** scenario test — create sessions/tokens, let some expire **at or before the newest
      recorded event (`hw`)**, roll the clock back below `hw − tolerance`, run `clock reset`; every credential
      that had expired as of `hw` is rejected afterwards, and none is re-validated.
- AC-CK-17 **Documented non-guarantees (N-01, N-05), asserted not hidden:** (i) a credential that expired after
      the newest event and a rollback that stays later than that event: the test asserts it is *not* detected
      by the guard (the real clock decides) and the docs say so; (ii) during catch-up after a backward step of
      less than the tolerance, a credential's real-time validity is extended by at most `tolerance`; beyond the
      tolerance operations are refused.
- AC-CK-18 Issuance uses `now_eff`: `created = now_eff`, `expires_at = created + TTL` exactly, including when
      `now_c < hw` within the tolerance. (N-04)
- AC-CK-07 Break-glass operations (`operator recover`, `reset-epoch`, `clock reset`) work while the guard
      is tripped and none of them consult credential expiry.
- AC-CK-08 `seq` remains the ordering authority: a clock regression never reorders or rejects `verify`.
- AC-CK-09 **Forward-skew scenario:** clock jumps ahead by 2 years; tokens/sessions are issued; the clock is
      corrected; the guard trips; `clock reset` revokes every credential issued during the skew by rule (b),
      none of them is ever valid again, and no credential's remaining validity is longer than before the reset.
- AC-CK-10 **Backward-step scenario** (VM restore, δ = 3 h): tokens whose `expires_at ≤ hw` are revoked by rule (a);
      tokens **created within the last δ (more than the tolerance after `now_c`) are revoked by rule (b)** — `now_c`
      is treated as truth, a documented conservative consequence (F-06); every other token keeps its `expires_at`
      byte-for-byte. The test fixes the expected revoked set explicitly.
- AC-CK-11 For every token surviving a reset, `expires_at` is identical before and after, and
      `expires_at − now_c` equals what a correct clock would give (property test; nothing extended or shortened).
- AC-CK-12 `no_expiry` tokens are untouched by rules (a) and (c) and are subject to rule (b); rule (c) never
      fires for tokens issued by the service (only for forged/corrupted rows, asserted by raw-SQL insertion).
- AC-CK-13 **Monotone reference (F-01):** denials, failed logins, refused attempts, `logout` and break-glass events
      appended while the guard is tripped are stamped `max(now_c, hw)` and leave `ts_high_water` unchanged; the guard
      stays tripped; a token that expired at or before `hw` stays rejected throughout; an `audit_state` update that
      lowers `ts_high_water` outside a `system.clock_reset` is aborted by the trigger.
- AC-CK-14 **Expiry evaluation (F-07):** validity is `now_eff < expires_at` with `now_eff = max(now_c, hw)`; at
      `now_eff == expires_at` the credential is expired; at `now_c = hw − 4 min` (inside the tolerance) a token with
      `expires_at = hw − 1 min` is rejected; idle expiry uses the same definition.
- AC-CK-15 `created_at` of a token is authenticated: editing it is reported (`ST-ATTR created_at`); rules (b)/(c)
      read the verified value, so a forged row cannot dodge them undetected. (F-08)
- AC-CK-16 `clock reset` when the guard is not tripped is refused and writes nothing; `ts_high_water` is lowered only
      by an accepted reset and `verify` reports any other decrease as `TS-DECREASE`.
- AC-AU-10 `export` is JSON Lines with a stable, documented schema (epoch, key version, seq, all columns),
      containing no values, keys, hashes or tokens.
- AC-AU-11 `head` output (chain_id, seq, mac, key_id, head seal) is reproducible; `verify --anchor` exits 10
      on mismatch/absence.
- AC-AU-12 `rotate-key` requires `audit:admin`, is audited, and leaves the chain verifiable (AC-AK-06).

### M2.10 — Identity-state replay and trigger inventory (D13, D21)
- [ ] `audit verify --state` implements design §5.9 exactly; trigger inventory implements §7.3.
- AC-ST-01 **One test per replayed event type** (operator.create/set_password/set_admin/disable/enable,
      auth.login, auth.logout/session_revoked, grant.add/revoke, token.issue/rotate/revoke,
      system.emergency_revoke, system.clock_reset, system.recover, adopted events): legitimate path
      ⇒ zero findings, and the reconstructed state equals the tables column by column.
- AC-ST-02 **Forged rows:** a raw-SQL inserted operator, session, grant, token or token-env row ⇒
      `ST-ORPHAN`; a deleted row ⇒ `ST-MISSING`.
- AC-ST-03 **Unauthorized credential change:** replacing `password_hash`, a session `secret_hash` or a token
      `secret_hash`; restoring an *older valid* value; setting `instance_admin = 1`; changing `username`
      ⇒ `ST-CRED` / `ST-ATTR` naming the column.
- AC-ST-04 **Scope/role/expiry edits:** changed grant role/project/environment, token env set, `perm_set`,
      `name`, `expires_at`/`no_expiry`, `replaced_by` ⇒ `ST-ATTR`.
- AC-ST-05 **Revocation rollback:** revoke a token/grant/session/operator, then (a) clear `revoked_at`/
      `disabled_at`, (b) restore a full pre-revocation copy of the row ⇒ `ST-REVOKE-ROLLBACK`.
- AC-ST-06 **Stale-row replay after rotation:** restoring the old `expires_at`/hash of a rotated token ⇒
      `ST-ATTR`/`ST-CRED`.
- AC-ST-07 **Use detection:** a forged token used through the service ⇒ `ST-USE-UNISSUED`; a resurrected
      revoked token used ⇒ `ST-USE-AFTER-REVOKE` **even if the row is later restored to the revoked state**.
- AC-ST-08 **Documented non-detections are asserted:** (a) coherent rollback of rows + chain-tail
      truncation without an anchor ⇒ not detected (detected with `--anchor`); (b) edits to volatile columns
      (`last_used_at`, `last_seen_at`, `failed_count`, `locked_until`) ⇒ not detected; (c) forged rows
      *and* events by a party holding the audit key (test harness) ⇒ not detected.
- AC-ST-09 **Adopted epochs:** after `reset-epoch`, adopted entities are labelled in the output;
      tampering before the reset is not reported (asserted); tampering after the reset is.
- AC-ST-10 **Completeness:** a list of identity-writing `Tx` methods is checked against the events that must
      accompany them; a randomised legitimate workload (mixed operations, rotations, revocations, clock
      reset) followed by `verify --state` reports **zero** findings.
- AC-ST-11 No password hash, secret hash, token or session secret (or any byte string equal to one) appears in
      any event, export or `--state` output (scan with planted values).
- AC-ST-12 `--state` on a chain that is BROKEN/UNVERIFIABLE/HEAD_INCONSISTENT refuses to run the comparison
      and returns that chain's exit code (10/11/12), instead of claiming "no findings"; this includes an
      `UNVERIFIABLE` chain (one unavailable key version): no partial comparison is performed or printed as a
      result (consistent with AC-ST-15). (F-09)
- AC-ST-13 **Commitments across audit-key rotation:** a credential set under key v1 still verifies after
      rotation to v2 using the retained v1 key; credentials set under v2 verify with v2; rotation re-commits
      nothing; tampering after rotation is detected for both versions.
- AC-ST-14 **Commitment binding:** moving a commitment/hash between entity ids, credential kinds or key
      versions fails — the labels `pw`, `sess-sc`, `tok-sc` are distinct, so a session commitment cannot be
      replayed as a token commitment even for equal ids (F-19); the state subkey output differs from the event-MAC
      subkey output for identical input (domain separation); a commitment does not depend on anything outside the
      documented inputs.
- AC-ST-15 **Missing/unwrappable key ⇒ `ST-UNVERIFIABLE`:** for each failure mode (provider down, wrong root
      key, corrupt binding canary, `audit_keys` row missing, wrapped key corrupt) the epoch is `UNVERIFIABLE`
      (classified environmental/data), **no comparison runs**, the output names the unavailable key versions, and
      the exit code is 11 — **never 0** (property over all modes). (F-09)
- AC-ST-16 **After an epoch reset:** baseline commitments use the new key version; `--state` succeeds with the
      old epoch's audit keys deliberately unavailable in the test provider.
- AC-ST-17 **Baseline completeness:** every row of projects/environments/operators/sessions/grants/tokens (live, revoked, disabled)
      is adopted; legitimate pre-reset entities give zero findings; `baseline=` counts must match the adopted
      events that follow, otherwise a chain break / `ST-SEQUENCE`; adoption does not modify any table row.
- AC-ST-18 **Post-reset detection:** a row inserted after the reset ⇒ `ST-ORPHAN`; a baseline row deleted ⇒
      `ST-MISSING`; a pre-reset *revoked* row restored live ⇒ `ST-REVOKE-ROLLBACK`.
- AC-ST-19 **Pre-reset tampering is adopted (asserted non-detection):** tampering before the reset is not
      reported afterwards; `obs_state=findings:n` is recorded when `--state` had findings at reset time, and
      the confirmation prompt shows them.
- AC-ST-20 Documentation lint for D21: the docs state the key used, where its version is stored, behaviour
      across rotation, reset and a missing key, and backup/recovery.
- AC-ST-21 **Mutation:** changing the handling of `ST-UNVERIFIABLE` to "success" must fail at least one test.
- AC-ST-22 **`USE-*` scope (F-16):** denied/failed events naming a revoked, expired or non-existent credential as the
      attempted actor (`token.use_denied`, `auth.failed`, denied `secret.read`) produce **no** `ST-USE-*` finding;
      the same actor succeeding after revocation does.
- AC-ST-23 **Projects/environments (F-02):** `project.create`/`env.create` replay; adopted M1 structure at
      `upgrade` and at `reset-epoch` gives no `ST-ORPHAN`; inserted ⇒ `ST-ORPHAN`, deleted ⇒ `ST-MISSING`.
- AC-ST-24 **Adopted token fidelity (F-11):** the baseline event of a rotated token carries `replaced_by` and the
      post-rotation `exp`, and of every token `created`; a baseline of rotated/no-expiry/revoked tokens gives
      zero findings; the conformance test accepts every key of design Appendix B (including `system.clock_reset`
      `old_hw/new_hw/…_a/_b/_c/tokens_kept`) and rejects any other.
- AC-ST-25 **`system.clock_reset` replay (F-11):** the `*_a/_b/_c/_kept` counts equal the following revocation
      events by reason, else `ST-SEQUENCE`.
- AC-TR-01 With triggers present, every forbidden `UPDATE`/`DELETE`/mis-sequenced `INSERT` listed in
      design §7.3 aborts.
- AC-TR-02 With triggers **dropped**, the same tampering succeeds at SQL level and is then reported by
      `verify` (events), `verify --state` (identity rows) and the trigger inventory (`SCHEMA-TAMPER`).
- AC-TR-03 A structurally valid event with a wrong MAC is **accepted by the triggers and rejected by
      `verify`** (pins the limitation; the docs must not claim triggers prevent forgery).
- AC-TR-04 No trigger rejects any legitimate operation (full suite and randomised workload with triggers on).
- AC-TR-05 The trigger inventory (names + SQL digests) matches the embedded expected set on a healthy
      database and reports each missing/altered trigger individually.
- AC-TR-07 **Reset order vs triggers (F-03):** the §5.8 sequence (key row, `epoch_start`, head jump, +1 per adopted
      event) succeeds with all triggers installed; any other order (e.g. adopted events before the head jump, or one
      final head update after all of them) is aborted.
- AC-TR-08 **Bookkeeping vs head (F-04):** with `audit_state` head ahead of, behind, or different from the events,
      `UPDATE`s of `quarantined_at`/`auth_*` succeed (latch, release, window) while any change of a head column that
      violates §7.3 aborts; `ts_high_water` cannot decrease except with a `system.clock_reset` head event.
- AC-TR-09 **Genesis and missing row (F-27):** `system.genesis` is accepted by the normal insert branch against the
      initial `audit_state`; an `INSERT` into `audit_state` is accepted only when the table is empty and the event table
      is empty or its newest event is an `epoch_start` carrying the same values.
- AC-TR-06 Documentation test: the words "boundary"/"prevent" never appear next to "trigger" in
      `docs/central-store-m2.md` without the qualification in §7.3 (simple lint over the doc).

### M2.11 — Hardening gate
- [ ] Mutation tests on authz/delegation engine, chain code, replay, token verification, clock guard,
      transaction template (as for M0/M1; survivors explained).
- [ ] `-race` on all new packages; multi-process and crash tests stable (≥ 20 repeated runs).
- [ ] Leak scans of the whole test output.
- [ ] `govulncheck`/`gosec` on new packages; `go mod tidy -diff` empty; no new dependency expected
      (`x/crypto/hkdf`, `argon2`, stdlib).
- [ ] `docs/central-store-m2.md` and CHANGELOG written; every guarantee has a test, every
      non-guarantee is listed; no "production-ready" wording.
- [ ] Independent review (fresh reviewer, same format as 2026-10-16); all Critical/High fixed; Medium fixed
      or explicitly accepted by you.

## 4. Failure semantics (normative summary — details in design §5.7, §5.8, §6.4)

| Event | Result |
|---|---|
| Audit-specific failure (key, sealer, tail check, quarantine) | mutations/reads refused, no plaintext; denials stay denials; nothing recorded; stderr says so |
| General database write failure | mutations/reads fail; denials stay denials; nothing recorded; in-memory counter only; **no persisted `audit_dropped`** |
| `COMMIT` error / crash around commit | both mutation and event, or neither; caller told "outcome unknown"; no plaintext before a successful commit |
| Failed-auth above 60/min | summarised via `audit_state` counters; summary event after the window |
| `verify` finds BROKEN / UNVERIFIABLE / HEAD_INCONSISTENT | exit 10/11/12; **writes nothing**; `--latch` stops further appends; recovery = restore or `reset-epoch` |
| Head inconsistent at append time | automatic refusal (`ErrAuditIntegrity`) |
| M2-pending / newer schema | refuse (`ErrUpgradeRequired` / `ErrSchemaTooNew`) |
| Binding mismatch / corrupt | refuse (`ErrWrongRootKey` / `ErrBindingCorrupt`) |
| Clock earlier than `ts_high_water − 5 min` | operations that consult expiry refused (`ErrClockRegression`); expiry decisions always use `max(now, ts_high_water)`; appended events never lower `ts_high_water`; recovery by waiting or `clock reset` (revokes what a lower clock could revive) |

## 5. Test strategy overview

1. **Pure unit tests** for `authz` (exhaustive tables, incl. `MayAssign`) and `audit` (vectors, tamper
   matrix, replay) — no database.
2. **Store tests** with real SQLite: transactions, triggers (present and dropped), migration, byte-identity
   of read-only commands.
3. **Service tests:** generated authorization matrix, delegation matrix, failure injection (audit, key
   provider, SQLite errors), concurrency (goroutines **and** processes), crash tests (SIGKILL), clock tests.
4. **CLI tests:** argv/flag reflection, leak scans, exit codes, end-to-end scenario.
5. **Negative/security suite** (`TestNeg_*`): escalation, confusion, IDOR, replay-after-revoke, forged rows,
   tamper, truncation (documented non-guarantee), clock rollback, SQL-shaped inputs, oversized inputs.
6. **Mutation testing** at M2.11 on every security-critical branch.
7. **No test relies on timing for correctness**; uniformity checks use statistical bounds and are advisory.

## 6. Decisions requiring your approval

"Review" = the 2026-10-19 design review outcome.

This table is the decision gate for the overall M2 design and for M2.2 onward. It is **separate from the
authorization of M2.1** (section 2): M2.1 does not depend on any row here, and no row here is approved by
authorizing M2.1. Statuses below are unchanged by the third pass.

**Open question OQ-TX-1 (M2.1; reviewer decision required, not selected by this document).** A context marker
only recognises contexts derived from the one `fn` received. Go offers no goroutine identity and standalone
methods see only a `ctx`, so a standalone call with an unrelated context during a transaction cannot be told
apart from a legitimate independent caller (nor can a second `Store` instance or process).

| Option | Contract | Consequences |
|---|---|---|
| **(a) Accept the limitation — recommended** | Marked context ⇒ `ErrTxActive` immediately (AC-TX-04, 04a). Unrelated context ⇒ M1's existing behaviour: a write waits, then succeeds if the transaction ended first or fails with the raw busy error at `busy_timeout`; a read runs outside the transaction on committed state (AC-TX-04b…e) | No API or concurrency change; M1 behaviour preserved. Residual risk: careless code inside `fn` can still use a standalone method. Later milestones keep this safe by convention and tests (the service layer reaches the store only through `WithTx`), not enforced by M2.1 |
| (b) Make only `WithTx` reachable | Remove or unexport standalone methods from the interface used by the service | Interface change affecting `centralsvc`, the CLI and many tests; exceeds the "tests unchanged" criterion; larger than M2.1 |
| (c) Store-level gate | Serialise all standalone calls behind an open transaction | Changes concurrency behaviour (readers stop being non-blocking; latency and deadlock-in-`fn` consequences); not behaviour-preserving |

Recommendation: (a). If the reviewer chooses (b) or (c), M2.1's scope and AC-TX-01/04b…e must be rewritten
before authorization.

| ID | Decision | Review | Recommendation / status |
|---|---|---|---|
| D1 | Audit-key design | approve B | **B** |
| D2 | MAC scheme | approve, with explicit limits | **HMAC-SHA256**; documented: whoever can verify can forge; `mac_alg` keeps Ed25519 open |
| D3 | Operator CLI sessions | approve | 8 h absolute (max 24 h), 30 min idle, `0600` file via `securefs`, server-side revocation |
| D4 | Roles/permissions and delegation | **revise → revised** | design §4.2.1–4.2.3; tests AC-DL-01…14 |
| D5 | Service-token policy | approve | mandatory expiry, default 90 d, max 365 d, `--no-expiry` exceptional, visible in `list`, audited; read/list only |
| D6 | Failed-auth volume | approve in principle; define precisely | **defined:** persisted 60 s window, ceiling 60, summary event (design §5.7) |
| D7 | Uniform denial errors | approve | yes |
| D8 | Backfill M1 `created_by` | approve | no backfill |
| D9 | In-place M1 upgrade | approve conditionally | **supported, conditional on a verified backup and a restore drill** (AC-MG-01/06/07); same wording in both documents |
| D10 | External anchoring | approve tooling | `head`/`export`/`verify --anchor`; operating an anchor is an operator duty |
| D11 | Audit retention | approve | none in M2; growth documented |
| D12 | Metadata listing audit | approve | not audited by default; denials, value reads, mutations are |
| D13 | Identity-state integrity | **revise → revised** | design §5.9 replay + commitments; ACs AC-ST-01…25 (structure replay, `created`, `USE-*` scope). **Owner: cannot be confirmed until D21 is resolved; confirmation PENDING (dependency D21, ON_HOLD)** |
| D14 | Break-glass recovery | approve in principle, define behaviour on unverifiable chain | `reset-epoch`, `operator recover`, `clock reset`, `emergency revoke` (design §5.8, §6.4); old history never becomes trusted |
| D15 | Clock-regression guard | **revise → revised** (three times) | aligned with the **owner-approved D24**: `now_eff = max(now_c, hw)`, strict expiry boundary, issuance on `now_eff`, rule (b) also revokes tokens created within the last δ before a backward step (stated), N-01/N-02 accepted limits (design §6.4). **Not fully confirmed:** rules (b)/(c) rely on the authenticated `created`, which is part of the D13 replay (blocked by D21). Confirmation PENDING |
| D16 | Reader CLI access | approve | yes, read event committed before plaintext |
| D17 | Package layout | approve in principle | `audit`, `authz`, `identity`, `centralsvc` |
| **D18** | No self-grants; `instance_admin` assigns only `project_admin`; last-admin protection | approve in principle; keep last-admin protection, document two-account requirement | **approved in principle**; documented in design §4.2.1/§4.2.3 |
| **D19** | Token issuer must hold the permission it delegates | approve | **approved** |
| **D20** | Quarantine latch + informational incident records; read-only verification never writes; only explicitly requested `--record` (after `OK`), flag commands and recovery operations write | approve in principle, subject to state-transition clarification | **Owner decided this revision:** (1) release policy — identity-state findings **block** release until resolved or classified by a separately authorized policy, revalidated in the same transaction as the release (design §5.8 T5; AC-VR-13/24); (2) OQ-D20-1 — metadata listings allowed while quarantined, secret reads and mutations denied (design §5.8; AC-VR-21/25). **Still pending confirmation:** the rest of the state machine, boundary rules and break-glass restrictions. **BLOCKED B-1:** the authorization path for `--release` (Mode R forbidden, Mode S unobtainable after session expiry) |
| **D21** | Credential commitments in events | **HOLD** until key lifecycle and epoch baseline are specified | **specified, still on hold for your approval:** key = `K_state[v]` (HKDF of the audit key of the event's version), rotation/reset/missing-key behaviour, baseline (design §5.9). **Not to be implemented before approval** |
| **D22** | Persisted failed-auth window | approve in principle, subject to concurrency/failure semantics | **revised twice:** atomic window procedure, "counted" defined, bounded invariant `A_c ≤ total ≤ A_c+K`, now with deterministic cases (AC-FL-16) (design §5.7). **Owner requirement integrated; owner confirmation PENDING** |
| **D23** | Authentication in degraded states: **restricted Mode R** — proof that the process can unwrap the root-key binding canary; not a user credential; database-owner-equivalent; read-only use unrecorded; allowed for read-only audit verification and incident-flagging (`--latch`, `quarantine`); **forbidden** for `quarantine --release`, `--record`, `rotate-key` | — | **APPROVED by the owner (this revision, as restricted).** Consequence: **B-1 BLOCKED** (design §5.8) — the only permitted release path (a session) cannot be obtained once sessions expire and login is refused while quarantined; options (a) step-up password re-authentication, (b) unrecorded login (rejected), (c) accept the limitation; recommendation (a), **no option chosen** |
| **D24** | Event timestamps are *stamped* `max(wall clock, ts_high_water)`; the durable monotone `ts_high_water` is the single time reference | — | **APPROVED by the owner (this revision) with N-01 and N-02 accepted as documented limitations.** Heartbeat events: **not included**; recorded as a future option needing separate cost/benefit evaluation |

### Remaining unresolved security decisions after this revision

Decided by the owner in this revision (recorded, not re-opened): **D24**, **D23** (restricted), **OQ-D20-1**, and the
**D20 release policy**. Everything below is still open.

1. **B-1 — BLOCKED.** How `quarantine --release` is authorized now that Mode R is forbidden from releasing and no
   session can be created while quarantined. Options (a) step-up re-authentication, (b) unrecorded login (rejected),
   (c) accept the limitation (release only with a live pre-quarantine session, else `reset-epoch`). Needed: the
   owner's choice; then AC-VR-13/19/23 are completed.
2. **D21 — ON_HOLD.** To unblock it the owner needs to approve, together: the commitment key lifecycle (`K_state[v]`,
   rotation, reset, missing key); the epoch baseline (which entities, adopted flags, non-detection of pre-reset
   tampering); the `--state` refusal on an unverifiable chain (F-09); the commitment labels (F-19); and the
   truncation to 128 bits. Artifacts: canonical commitment test vectors (to be produced with M2.2), AC-ST-13…16,
   20, 21 and AC-MG-08 executed. Alternative: choose "no credential-change detection" (then `ST-CRED` and those
   criteria disappear and the limits widen). F-19 and the commitment part of F-09 stay ON_HOLD/PARTIAL until then.
3. **D13 and D15 confirmations** — blocked behind D21 (D13 directly; D15 through the authenticated `created`).
4. **D20 remainder and D22 confirmations** — the state machine, boundary rules and break-glass restrictions; the
   "counted" definition and the invariant with its deterministic cases.
5. Whether a **future heartbeat** (D24 future option) is worth its write cost — to be evaluated separately.
6. Whether M3 will require **preventive** protection of identity rows (D13 stays detective).
7. Root-key escrow/backup and restore tooling design (outside M2, still a precondition for real data).
8. The numbers: 5-minute clock tolerance, 365-day maximum token lifetime, 60 failed-auth events per minute,
   8 h/30 min session limits (design §6.4, §5.7, §3.2, D5).
9. Anchoring cadence and where anchors are shipped (operator responsibility, D10).
10. Whether identity and administrative listings (`operator/grant/token list`) should be available while quarantined
    (this revision conservatively denies them).

## 7. Risk register

| ID | Risk | Likelihood | Impact | Mitigation |
|---|---|---|---|---|
| R1 | `WithTx` refactor regresses M1 behaviour | M | H | behaviour-preserving step; M1 suite unchanged (AC-TX-01); mutation tests |
| R2 | Audited reads serialise (write transaction per read) | H | M | local milestone; measure; redesign before any M3 throughput claim |
| R3 | Audit table grows without bound | H | L–M | no pruning in M2; documented (D11) |
| R4 | Docs/UI overstate the chain as tamper-proof | M | H | wording discipline; AC-AU-04, AC-ST-08, AC-TR-03/06 assert the non-guarantees; review checklist |
| R5 | Admin lock-out / lost admin password | M | M | non-permanent backoff; `operator recover` (root-key proof); last-admin protection (AC-DL-07) |
| R6 | Session file theft | M | M | short lifetimes, `0600` + `securefs`, revocation, audited |
| R7 | DB-write attacker forges identity rows | M | H | not prevented in M2; `verify --state` (design §5.9) with stated limits; `securefs`; backup integrity is an operator duty; M3 decides on preventive controls |
| R8 | Fail-closed audit bricks operations | L | H | precise errors, diagnostics work, documented recovery (restore / `reset-epoch`), backups include `audit_keys` |
| R9 | Authorization/delegation bug (escalation, IDOR) | M | H | generated matrix, delegation matrix, reflection coverage, uniform denial, mutation testing, independent review |
| R10 | Root key lost before escrow tooling exists | M | Critical (data) | escrow is a hard precondition for real data; stated in every doc |
| R11 | Clock manipulation resurrects expired credentials | M | M | guard + `clock reset` that materialises expiry as revocation (AC-CK-05/06) |
| R12 | Pre-existing dependency vulnerabilities (stdlib/x-net/grpc/docker) | H | M | separate pre-release fix task; not caused by M2 |
| R13 | Login floods exhaust CPU/memory via Argon2id | M | M | concurrency semaphore, throttling (AC-ID-08) |
| R14 | Audit-key leak (DB + root key) | L–M | H | cannot be prevented locally; `rotate-key`; external anchors; documented |
| R15 | Scope creep into M3/WebUI/agent | M | M | boundary section; review gate rejects listeners/UI coupling |
| R16 | Partial or failed upgrade of an M1 database | L | M | atomic upgrade, verified backup, pending state, restore drill (AC-MG-02/03/06/07) |
| R17 | Flaky multi-process/crash tests | M | L | helper-process pattern from M1; repeat-run gate in M2.11 |
| R18 | Plaintext/keys in memory dumps or swap | L | H | unchanged from M1; no `mlock` claim |
| R19 | Service bypass (direct DB + key use) produces no audit trail | H (by design) | H | stated up front (§0); resolved only by M3 architecture |
| R20 | **Replay (`--state`) false positives block operations or erode trust** | M | M | completeness obligation + randomised workload test (AC-ST-10); findings never change service behaviour |
| R21 | **Replay mistaken for prevention** | M | H | documentation and CLI text say "detective"; AC-ST-08 asserts the non-detections |
| R22 | **Break-glass paths abused** (equivalent to DB-owner power) | L | H | always audited, typed confirmation, root-key proof; documented as equal to §0's assumption |
| R23 | **Latch/incident rows tampered with** | M | L | informational only; evidence is the verifier output (design §5.8) |
| R24 | **`clock reset` revokes too much / too little** | M | M | explicit rules (a)/(b)/(c), listing of survivors, `--revoke-tokens`, scenarios AC-CK-05/06/09/10; rule (b) also revokes tokens created within the last δ before a backward step (documented, confirmation shows the count) |
| R25 | **Collusion / second accounts defeat delegation limits** | M | M | documented residual (design §4.2.3); no two-person enforcement in M2 |
| R26 | **Trigger removal goes unnoticed** | M | M | inventory check in `status`/`verify` (AC-TR-02/05); documented as defence in depth |
| R27 | **A commitment that cannot be checked is reported as clean** | L | H | `ST-UNVERIFIABLE`, exit 11 never 0; property test over every failure mode; mutation test (AC-ST-15/21) |
| R28 | **Epoch adoption legitimises pre-reset tampering** | M | M | stated as a non-detection; `verify --state` runs before the reset and its result is shown and recorded as an observation; operators told to re-issue affected credentials |
| R29 | **False-positive quarantine blocks operations** | L | M | latch only on data problems, not outages; release after a fresh `OK`; `reset-epoch` as last resort |
| R30 | **Suppressed-failure counters drift under crash/concurrency** | M | L | single-transaction procedure; accounting invariant property test; counters shown by `status` |
| R31 | *(retired — duplicate of R24; kept so references stay stable. R24 now carries the rules (a)/(b)/(c), untouched expiries, scenario tests AC-CK-05/06/09/10 and the backward-step consequence of rule (b).)* | — | — | — |
| R34 | **A transient forward clock jump pins `ts_high_water` ahead; recovery needs the break-glass `clock reset`, which revokes all sessions and every token expiring within the jump** (design §6.4, N-02) | L–M | M | documented consequence; confirmation shows counts; wait recovery for small jumps; the optional heartbeat is a D24 alternative; NTP monitoring is an operator duty |
| R36 | **No permitted way to release a quarantine once sessions expire** (B-1, BLOCKED) | M | M | until decided, the only exit is `reset-epoch` (adopts damaged state, records `obs_state`); owner decision required |
| R37 | **Metadata listings stay available while the chain is untrusted** (OQ-D20-1) | M | L | fixed field list, no writes, identity listings denied, authorization reads unverified tables (stated) |
| R35 | **A rollback that stays later than the newest event is invisible to the guard** (N-01) | L | M | stated non-guarantee (AC-CK-17); credential use appends events so `hw` tracks active credentials; periodic heartbeat is a D24 alternative |
| R32 | **Mode R is database-owner-equivalent authority, and its read-only use is not recorded in the chain** (design §5.8, D23 approved restricted) | M | M | stated evidence gap; Mode R forbidden for release, record and rotate-key; flag commands leave incident rows; operators told |
| R33 | **`ts_high_water` is a stored value a database writer could lower** | M | M | trigger refuses a decrease except at a `system.clock_reset`; tail check requires `hw` = newest event ts; `TS-DECREASE`; a writer with the audit key is out of scope (§5.5) |

## 8. Definition of done for M2

All ACs above pass on Linux; the independent review leaves no open Critical/High; docs list every
guarantee with its test and every non-guarantee explicitly; no listener, no WebUI/agent coupling; the
wording "production-ready" appears nowhere. Moving to M3 requires a separate approval.

## 9. What this plan deliberately does not do

Remote or network distribution of secrets; WebUI/agent integration; OIDC/TOTP; PostgreSQL/HA;
root-key and DEK rotation; backup/restore commands; purge; per-row MACs on identity tables (rejected,
design §9); preventive protection of identity rows (M3); asymmetric audit signatures; automatic anchoring
to external systems; the dependency-vulnerability remediation and root-key escrow (both tracked separately
and required before release / before real data).
