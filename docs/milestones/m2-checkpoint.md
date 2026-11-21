# M2 checkpoint: local encrypted control-plane foundation

Status: **M2 IMPLEMENTATION CHECKPOINT COMPLETE once the final verification recorded in the durable evidence passes. PRODUCTION QUALIFICATION DEFERRED.** Not production-ready. N19 stays an open production release blocker.

Branch: `feature/m2-development`. The frozen commit and tree are recorded in the verification evidence summary (a document cannot contain the hash of the commit that contains it).

## Scope (ADR-011, owner-approved, local-only)

Implemented: encrypted central-store persistence (AES-256-GCM, per-project DEK under a root key); projects and environments; secret CRUD with immutable versions; historical version retrieval; immutable version rollback; project and environment rename (slug validated before any mutation); safe non-cascading deletion; local CLI management with `--json` metadata output; root-key custody diagnostics in `dso doctor`; transactional audit appends; backup/restore qualification; plaintext-at-rest regression tests.

Also in this checkpoint: N19 follow-ups (bounded reconciliation of gated containers, ledger evidence bound to the delivered version, legacy file-mode journal records are not delivery evidence; see `docs/design/n19-managed-delivery.md` sections 11 and 12) and the G024 disposition (`docs/design/g024-disposition.md`).

## Commands

| Capability | Command |
|---|---|
| Historical retrieval | `dso central secret get <project> <env> <name> --version N --reveal` (fails for a disabled or missing version; `--reveal` required; reveal audit is persisted before disclosure) |
| Version rollback | `dso central secret rollback <project> <env> <name> <version> [--comment]` (appends a new version; history is never rewritten; a disabled source version is rejected) |
| Rename / delete | `dso central project\|environment rename\|delete` (delete refuses a non-empty target, including one holding only soft-deleted secrets) |
| Custody diagnostics | `dso doctor --central-db P --central-key-file P --central-key-provider file\|passphrase` |

`secret get` intentionally has no `--json`: its only successful output is explicit plaintext reveal. No JSON output of any central command contains a secret value.

## Security contract and accepted limitations

- Tested persistence paths show no plaintext secret values and no unwrapped key material in the inspected artifacts (database, WAL/SHM, backups, safety and displaced copies, captured logger output). Names, slugs and actors are cleartext. This is not a claim about memory or every runtime path.
- Access is the operating-system permission boundary on the database and root key; there is no application authentication for `dso central`. The agent REST authentication does not protect it.
- The audit chain is unkeyed and not tamper-proof (ratified decision).
- Rollback restores a stored value only; it does not restore validity at an external service.
- Re-delivery after a container restart may supply a newer value than the ledger records as applied; this converges without another provider change (`n19-managed-delivery.md` section 12).
- The reconciliation worker is the one deliberate goroutine; its contract is in `n19-managed-delivery.md`.

## Deferred (not part of M2)

HTTP or network vault API; network authentication and authorization; remote secret disclosure; central-secret agent provider; vault Web UI; enterprise RBAC, SSO, multi-tenancy; permanent purge (design only: `docs/design/central-purge-design.md`); KMS/HSM; root-key rotation.

The original audit-roadmap M2 gate ("Vault API with authn/z") is **not** claimed as passed; M2 is measured against the revised local-only scope in `docs/design/m2-control-plane-scope.md`.

## Future audit improvements (backlog)

Rollback events do not record the source version and rename events do not record the new slug (the audit event has no field for them; adding one needs an audit-schema change). Backlog: rollback source and created version, previous and new identifiers, structured operation result, request or operation correlation id. Never plaintext values.

See `docs/milestones/deferred-production-qualification.md` for what has not been demonstrated.
