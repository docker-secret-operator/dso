# Decision records

These are **new decision records documenting proposals and prior discussion; none is ratified.** They do not
reproduce documents that could not be recovered from the repository, its history or local backups. "Draft" and
"DRAFT" mean the owner has not formally ratified the content, including the proposed invariants and options.

## Reading guide and option labels

Every statement in these documents is one of these kinds:

1. **Verified current behavior**: read from the code in this repository (cited by file, function or line).
2. **Proposed recommendation**: an option or classification suggested for the owner to consider.
3. **Owner-selected or owner-confirmed**: chosen or clarified by the owner. These are recorded in working-session
   conversation, not in a repository decision record, and are **not ratification**.
4. **Ratified**: formally approved through the process below. **Nothing in this directory is ratified.**

Option labels are unique across documents:

| Label | Meaning | Defined in |
|---|---|---|
| ARCH-A / ARCH-B / ARCH-C | Direct CLI with an OS-user boundary / key-holding local service / defer the decision | architecture draft |
| POLICY-HYBRID | Peer credentials plus per-operation policy (a policy layer, not an architecture) | authn/authz record |
| CHAIN-A / CHAIN-B / CHAIN-C | Retain the unkeyed chain / HMAC chain / external anchor | audit-chain record |
| AUDITFAIL-A / AUDITFAIL-B / AUDITFAIL-C | Availability-first / fail closed for reveal only / fail closed for all | audit-failure record |
| REVEAL-A / REVEAL-B / REVEAL-C | Keep best-effort / persist before disclosure / configurable | per-operation draft |

Studying or designing an option does not authorize implementing it.

## Ratification process (defined by the owner, 2026-10-30)

A decision becomes ratified only when the owner reviews and explicitly approves a **separate durable decision
record** that lists each decision with its scope, rationale, caveats and status. No such record has been approved.
Until it is, every document here stays unratified. The owner's clarifications of 2026-10-30 clarify the historical
record; they do not authorize implementation, Git operations, or immediate ratification.

## Documents

| Record | Status |
|---|---|
| [Audit-chain integrity (Gate 3)](2026-10-30-central-audit-chain-integrity.md) | Draft; position CHAIN-A (retain unkeyed SHA-256 chain); **owner-selected proposal** ("Retain both" was the owner's selection); not ratified |
| [Audit-failure policy (Gate 6)](2026-10-30-central-audit-failure-policy.md) | Draft; position AUDITFAIL-A (availability-first); **owner-selected proposal** (same selection); not ratified |
| [Authentication and authorization](2026-10-30-central-authn-authz.md) | Draft; **owner-confirmed: analyze and design ARCH-B only**; architecture selection and implementation pending; not ratified |
| [Authorization architecture and threat model](2026-10-30-central-authz-architecture-threat-model-DRAFT.md) | Design draft, not approved; no architecture selected; ARCH-B is the design focus; bypass paths, hardening questions, identity, socket lifecycle and proposed invariants |
| [Per-operation audit-failure policy](2026-10-30-central-audit-failure-per-operation-DRAFT.md) | Design draft, not approved; attribution gaps, logging metadata, missing-event paths and the audit-append concurrency verification item |
| [Step 5.4 decisions and Step 5.5 questions](2026-10-30-central-step54-step55-register-DRAFT.md) | Register draft; the six Step 5.4 decisions are **substantively approved per the owner's clarification, with formal ratification pending**; scope and unverified caveats recorded; Step 5.5 questions unresolved |

## Owner clarifications recorded (2026-10-30)

- **Gates 3 and 6:** "Retain both" was the owner's selection; the audit-chain and audit-failure positions are
  owner-selected proposals and remain unratified.
- **Authorization architecture:** proceed with analysis and design of the key-holding service (ARCH-B) only;
  architecture selection and implementation remain pending.
- **Integration base:** `origin/feature/m1-webui-reconcile` is recorded as the *proposed* base, subject to a separate
  reconciliation review (see below).
- **Step 5.4 decisions:** substantively approved earlier; the register preserves the scope and caveats of all six;
  formal ratification is pending.
- **Ratification:** a separate durable decision record that the owner reviews and explicitly approves.

## Facts from the 2026-10-30 session

- `feature/webui-phase1` was pushed to its own upstream and was synchronized with it when last checked (no pull
  request opened).

## Proposed integration base

- `origin/feature/m1-webui-reconcile` (13 commits, 259 files against it; 56 commits, 469 files against `main`) is
  recorded by the owner as the **proposed** base, subject to a separate reconciliation review. It is not confirmed
  as the integration line. No fetch, merge, rebase or push is authorized by this record.

## Still undecided

Open questions, each tracked in the document named; none is approved or implemented.

- Authorization architecture selection (ARCH-A/B/C; analysis and design of ARCH-B is under way), service account
  model, break-glass, principal and role model, operation policy, how backup/restore/verify work under a service:
  [architecture draft](2026-10-30-central-authz-architecture-threat-model-DRAFT.md) sections 3, 4 and 11.
- Process hardening, supported platforms and filesystems: architecture draft sections 5 and 7.
- Logger configuration (whether, destination, format, level) and permitted log metadata: per-operation draft
  sections 0, 3 and 6 ([per-operation draft](2026-10-30-central-audit-failure-per-operation-DRAFT.md)).
- Reveal audit failure policy (REVEAL-A/B/C), a decision separate from the logger: per-operation draft sections 0
  and 4b.
- Actor attribution and its schema implications (reveal, project/environment creation and init currently record no
  actor; no schema change is proposed): per-operation draft section 1.
- Audit of backup, restore failure, dry run, wrong-key and denied attempts; restore and rollback semantics (an older
  backup drops newer events from the active chain): per-operation draft sections 4c and 6; architecture draft
  section 4; [audit-chain record](2026-10-30-central-audit-chain-integrity.md).
- Verification item: multi-process audit-append behavior (per-operation draft section 2).
- Quarantine and its release path (B-1); deferred until quarantine itself is approved.
- Formal ratification of each of the six Step 5.4 decisions through the separate decision record (scope caveats that
  could not be verified are flagged in the register), and the Step 5.5 project/environment lifecycle (rename, soft
  versus hard delete, archive, key scope, audit identifiers):
  [register](2026-10-30-central-step54-step55-register-DRAFT.md).
- Ratification of the audit-chain and audit-failure records and of the authorization design direction, through the
  same separate record.
