# Draft decision record: central audit-chain integrity (Gate 3)

Status: **Draft; proposed position; not ratified.** Written 2026-10-30. The owner confirmed on 2026-10-30 that "Retain both" (this position and the audit-chain / audit-failure counterpart) was the owner's own selection. It is an **owner-selected proposal and is not ratified**; the selection is recorded in working-session conversation, not in a repository decision record, and ratification requires the owner's explicit approval of a separate durable decision record (see `README.md`). This is a **new** draft record, not an approval. It does **not** reproduce the original Phase 2 audit-chain design
(`docs/reviews/2026-10-25-central-store-phase2-audit-chain-design.md`), which could not be recovered: it is absent
from every local ref, tag, stash, dangling object and backup that was searched. Earlier "Gate" numbers appear only
in source comments, so they are cited here as claims made by those comments, not as verified history.

Reading guide: see `README.md` ("Reading guide and option labels"). Statements marked *verified* describe current
code; *proposed* items are recommendations; *owner-selected* means chosen in a working-session discussion with no
repository record; *ratified* means formally approved. Nothing in this directory is ratified.

## Proposed position (scope: the current milestone only; not ratified)

Proposed: retain the **unkeyed SHA-256 hash chain** for `audit_events`: `event_hash = SHA-256(prev_hash || canonical(fields))`.
No MAC, signing key, key management or external anchor is added in this milestone.

## Alternatives considered

| Option (label) | Gain | Cost / reason not selected now |
|---|---|---|
| CHAIN-A. Retain unkeyed chain (**owner-selected proposal; not ratified**) | No new key material; simple; already implemented and tested | Does not establish authenticity against anyone who can write the database |
| CHAIN-B. HMAC with a wrapped random audit key (as in the unauthorized M2 draft) | Forgery needs the root key | Key storage, rotation and recovery; new event schema version; possible interaction with the availability-first position (for example if the audit key were unavailable when an event must be written) is a trade-off to analyze, not an established conflict; not authorized |
| CHAIN-C. Add an external anchor of the chain head | Detects tail deletion and rollback | Needs a trusted external store; reopens the deferred rollback-anchor decision |

## Security limitations (unchanged and stated plainly)

- A party who can modify the database file can recompute the whole chain; a successful verification is **not** proof
  of authenticity.
- Deletion of the newest events, and replacement of the database by an older internally valid copy, are **not** detected.
- Triggers that forbid updating or deleting rows can be dropped by anyone with write access to the database.
- `dso central audit verify` verifies a staged copy and is not a transactionally consistent snapshot of a database
  that is being written.

## Deferred work (not authorized by this record)

HMAC or any keyed chain, key management, external anchoring, rollback protection, quarantine.

## Code and tests

- `internal/store/sqlite/audit.go`: `canonicalizeAuditEvent`, `computeEventHash`, `AppendAuditEvent`, `VerifyAuditChain`
- `internal/store/sqlite/migrations/0003_audit_events.sql` (its comment cites Gates 1, 3 and 5 only; see "Gate citations found")
- `internal/store/sqlite/audit_verify.go`; command `dso central audit verify` (`internal/cli/central.go`)
- Tests: `internal/store/sqlite/audit_test.go` (`TestAuditChain_*`, including `TestAuditChain_TailDeletionNotDetected`),
  `audit_verify_test.go`, `audit_verify_unix_test.go`, `internal/cli/central_audit_verify_test.go`

## Gate citations found

These are claims made by source comments, listed so a reader can see exactly what exists. **No approval is inferred
from them.** The decision-resolution document they cite is unrecoverable.

| Gate | Where cited |
|---|---|
| Gate 1 (tail deletion and rollback detection deferred) | `0003_audit_events.sql`, `audit.go` |
| Gate 2 (business mutation and success event atomic via `WithTx`) | `service.go` (6 comments) and `internal/store/sqlite/audit_test.go:278` (7 occurrences found by a text search of `*.go` files under `internal/`; non-exhaustive for other file types) |
| Gate 3 (no separate MAC or signing key) | `0003_audit_events.sql`, `audit.go` |
| Gate 4 | **no citation found** in any inspected file |
| Gate 5 (append-only, no deletion) | `0003_audit_events.sql` (trigger message) |
| Gate 6 (log-only, availability-first) | `service.go`, `central.go`, `central_audit_logger_test.go` |

## Superseded or related drafts

`docs/design/m2-identity-authorization-audit-design.md` §2 (HMAC audit key) is an unreviewed draft; the M2 plan states
that M2.2 onward is not authorized. It is **not** adopted by this record.

## Unresolved

Whether a stronger mechanism is ever scheduled, and the exact wording of the original Gates 1, 2 and 5 (known only
from code comments; Gate 4 has no trace in the inspected evidence).
