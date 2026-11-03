# PROPOSED ratification record for dso central (not approved; nothing here is ratified)

Status: **Proposed record awaiting the owner's explicit review. Every item below is unratified.** Written
2026-10-30. Under the process the owner defined, a decision becomes ratified only when the owner reviews and
explicitly approves this separate record. Marking a box below is the owner's act; this document does not mark any
item ratified, and it authorizes no implementation, Git operation or new control.

The seven existing decision documents were not changed for this record. They are not yet linked from `README.md`
(that file was deliberately left alone); the links below are to those documents.

How to read it: the items are sorted by how much of their scope the surviving record supports (parts A to D). Part D
items are **not** offered for approval as written, because their original wording or scope cannot be recovered.

## Part A. Ready for explicit approval in principle (scope and limits preserved)

Approving A-1 and A-2 would establish the intended policy for **the current milestone only**. It would not ratify any
other item, and it would not authorize implementing any new control (no keyed chain, no fail-closed reveal, no logger
change, no schema change).

### A-1 (was R-1): retain the unkeyed SHA-256 audit chain (CHAIN-A)

- **Proposed wording:** For the current milestone, `audit_events` keeps the unkeyed hash chain
  `event_hash = SHA-256(prev_hash || canonical(fields))`. No MAC, signing key, key management or external anchor is
  added in this milestone.
- **Provenance:** the owner selected "Retain both" and confirmed on 2026-10-30 that it was their own selection (an
  owner-selected proposal). Recorded in `2026-10-30-central-audit-chain-integrity.md`.
- **Rationale:** no new key material; already implemented and tested.
- **Limitations that travel with the decision (verified in code):** the chain does not detect deletion of the newest
  events or replacement by an older internally valid database; a party who can write the database can drop the
  append-only triggers and recompute the chain; a "verified" result from `dso central audit verify` is not proof of
  authenticity; verification works on a staged copy and is not a consistent snapshot of a database being written.
- **Scope of approval:** approval covers the proposed wording together with the limitations listed here, for the
  current milestone only.
- **Not decided or authorized by this item:** CHAIN-B (keyed chain), CHAIN-C (external anchor), rollback protection,
  any schema or format change.
- Owner: ☐ approve in principle (provisional; not ratified)   ☐ approve (ratify, within the wording and limits shown)   ☐ approve with amendment   ☐ reject

### A-2 (was R-2): retain availability-first audit failure behavior (AUDITFAIL-A)

- **Proposed wording:** For the current milestone, a failure to write an audit event for a read or a failure outcome
  does not change the operation's result; mutations that already run in a transaction record their success event
  atomically.
- **Provenance:** the same "Retain both" selection. Recorded in `2026-10-30-central-audit-failure-policy.md`.
- **Limitations that travel with the decision (verified in code):** in the CLI, best-effort audit failures are
  silently discarded (nil logger becomes a no-op logger); a reveal can succeed with no durable record; reveal,
  project-creation, environment-creation and `init` events carry no actor; failed and dry-run restores, wrong-root-key
  attempts and `backup` write no event; read and list operations are not audited at all; and several failure paths
  return with no failure event, so the failure-event wording above does not mean every failure is recorded (for
  example `CreateSecret` ID, AAD and sealing errors before the transaction, project and environment ID, key and
  wrapping errors, the reveal AAD-build error, and any wrong-root-key `Bind` failure; details in
  `2026-10-30-central-audit-failure-per-operation-DRAFT.md`, section 4).
- **Scope of approval:** approval covers the proposed wording together with the limitations listed here, for the
  current milestone only.
- **Not decided or authorized by this item:** whether to supply a real logger, its destination, format, level or
  metadata; reveal fail-closed behavior (REVEAL-B/C); actor attribution or any schema change; auditing of backup,
  restore failure, dry run, wrong-key or denied attempts. Each remains a separate open decision (see
  `2026-10-30-central-audit-failure-per-operation-DRAFT.md`, section 6).
- Owner: ☐ approve in principle (provisional; not ratified)   ☐ approve (ratify, within the wording and limits shown)   ☐ approve with amendment   ☐ reject

## Part B. Confirmed direction; design task only

### B-1 (was R-3): analyze and design the key-holding service (ARCH-B)

- **Proposed wording:** The owner confirmed on 2026-10-30 that analysis and design of the key-holding local service
  (ARCH-B) may proceed. This authorizes the design task only. It is **not** an architecture selection and **not**
  authorization to implement.
- **Open and unaffected:** whether central gets a service at all (ARCH-A, ARCH-B or ARCH-C); service account model;
  break-glass; principals and roles; operation policy; backup, restore and verify under a service; hardening
  requirements; supported platforms and filesystems
  (`2026-10-30-central-authz-architecture-threat-model-DRAFT.md`, section 11).
- Owner: ☐ confirm design-only   ☐ amend   ☐ reject

## Part C. Proposal only

### C-1 (was R-4): proposed integration base

- **Proposed wording:** `origin/feature/m1-webui-reconcile` is recorded as the *proposed* integration base, subject
  to a separate reconciliation review. It is **not** confirmed as the integration line. No fetch, merge, rebase or
  push is authorized by this record.
- **Counts re-verified on 2026-10-30 from local refs only (no fetch):** the base ref resolves to `5bef463`; HEAD
  `3645b89` is 13 commits ahead of it and 0 behind (the base is an ancestor of HEAD, so it is also the merge base);
  259 files differ between the base and HEAD. For comparison, `origin/main` is 56 commits and 469 files from HEAD.
  The counts will change if either ref moves; they are not a basis for deciding merge scope.
- Owner: ☐ record as proposal only   ☐ amend   ☐ reject

## Part D. Historical Step 5.4 decisions: pending clarification (not offered for approval as written)

The owner clarified on 2026-10-30 that these six decisions were **substantively approved** earlier. The original
design and decision-resolution documents (named in the owner's prompts as
`docs/reviews/2026-10-25-central-store-phase2-backup-restore-design.md` and
`docs/reviews/2026-10-29-central-store-phase2-backup-restore-decision-resolution.md`) are unrecoverable from the
repository, its refs, tags, stashes and backups that were searched. This record therefore does **not** reconstruct them.
For each item it states only the surviving evidence and the question the owner would need to answer or the document
that would need to be recovered. Surviving evidence has two kinds: the owner's wording in Step 5.4 verification
prompts (recorded in the owner's working sessions, not in the repository) and numbered comments in source code
(written in earlier working sessions and not a primary record). The register holds the same detail:
`2026-10-30-central-step54-step55-register-DRAFT.md`.

| Item | Surviving evidence | What is uncertain | Needed before it can be ratified |
|---|---|---|---|
| D-1 (was R-5) rollback anchor | Owner wording: "External rollback anchor remains deferred." and "No external rollback anchor in this step." Code comments: `internal/centralsvc/restore.go:15-17`; Gate 1 comment in `internal/store/sqlite/audit.go`. | "In this step" has no recorded target step or expiry; whether deferral means "not now" or "not planned". | Owner states the intended scope and whether a target milestone exists, or the original document is recovered. |
| D-2 (was R-6) restore validation | Owner wording: "Restore validates SQLite integrity and root-key binding without claiming full decryption of every secret version." Code comment: `internal/centralsvc/restore.go:57-58` ("decision 2"). Current code also validates schema version and the audit chain. | The exact check set that was approved. | Owner states the approved check set, or the original document is recovered. |
| D-3 (was R-7) CLI and confirmation | Owner wording: CLI names and flags remain `dso central backup --out`, `dso central restore --from`, `--dry-run`, and "the approved explicit confirmation/`--force` behavior". Current code: a y/N prompt that `--force` skips. | What "approved confirmation behavior" specifically means (prompt text, defaults, non-interactive rules); the code is not verified to equal it. | Owner states the approved behavior, or the original document is recovered; then compare with the code. |
| D-4 (was R-8) backup pruning | Owner wording: "Automatic backup pruning remains absent." and "Do not implement automatic backup pruning." Code comment: `internal/store/sqlite/backup.go:64` ("Step 5.4 decision 4"). No pruning exists. | Whether any manual or documented retention policy was part of the decision. Backups and safety snapshots accumulate and are operator-managed per the code comment. | Owner states whether a retention policy beyond "no automatic pruning" was intended. |
| D-5 (was R-9) caller restrictions | Owner wording: "Caller-#1-only restrictions remain documented policy intentions, not falsely claimed as enforced authorization." Code comment: `internal/centralsvc/restore.go:239-240` ("decision 5"). No caller restriction is enforced by any command. | The term "Caller-#1" is undefined in any surviving record. Its meaning cannot be verified and is not guessed here. | Owner defines "Caller-#1", or the original authentication/authorization design is recovered. |
| D-6 (was R-10) audit retention | Owner wording: "Step 5.3 audit-retention decisions have not been silently reopened or changed." and "Do not silently change the existing audit-retention decision." No code comment. Code shows append-only triggers (a Gate 5 comment) and no retention or pruning logic. | The content of the Step 5.3 audit-retention decision. This record does not reconstruct it. | Owner states the decision's content, or the original Step 5.3 resolution is recovered. |

For Part D, the owner can choose per item: ☐ state the scope in a later amendment to this record ☐ recover the
original ☐ keep as historical context only (not ratified). **No Part D item is proposed for ratification until its
uncertainty is resolved.**

## Part E. Items outside this record (still unresolved)

Not covered above and not decided anywhere: logger configuration and permitted metadata; reveal audit-failure policy;
actor attribution and event-schema implications; audit events for backup, failed or dry-run restore, wrong-key and
denied attempts; rollback detection, external anchoring and keyed-MAC alternatives; process hardening and supported
platforms and filesystems; Step 5.5 lifecycle and schema questions; quarantine and the B-1 release path. See
`README.md` ("Still undecided").

## Part F. Approval

Nothing is ratified by this document's existence. An item is ratified only if the owner explicitly approves it here
(by marking the box above and stating so), and then only to the extent of the wording and limits shown. Approving
Part A does not approve Parts B to E.

**"Approve in principle" (Part A only):** records the owner's agreement with the direction and the wording shown, as a
provisional response. It is **not** ratification. The item stays unratified until the owner later marks "approve"
on it and states so, under the process above. The definition is based on the owner's own description of these
responses as recommendations that are "not yet formally ratified"; the owner may amend it.
